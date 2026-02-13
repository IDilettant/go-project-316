package crawler

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSpec_BrokenLinks_IncludeOnlyBroken_AndUseAbsoluteURL(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}

	client := newFixtureClientWithRoutes(t, map[string]roundTripResponder{
		"/": func(req *http.Request) (*http.Response, error) {
			body := `
				<html><body>
					<a href="/ok">OK</a>
					<a href="/missing">MISSING</a>
				</body></html>
			`

			return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		"/ok": func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusOK, "<html><body>ok</body></html>", nil), nil
		},
		"/missing": func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
		},
	})

	opts := Options{
		URL:         fixtureBaseURL,
		Depth:       0, // not important for broken-links on the page
		Concurrency: 1,
		Retries:     0,
		Timeout:     time.Second,
		UserAgent:   "test-agent",
		HTTPClient:  client,
		Clock:       clock,
	}

	report, err := analyzeReport(context.Background(), opts)
	require.NoError(t, err)

	require.Len(t, report.Pages, 1)

	bl := report.Pages[0].BrokenLinks
	require.Len(t, bl, 1)
	require.Equal(t, fixtureBaseURL+"/missing", bl[0].URL)
	require.Equal(t, 404, bl[0].StatusCode)
	require.Equal(t, "Not Found", bl[0].Error)
}

func TestSpec_BrokenLinks_NetworkError_StatusCodeZero(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}

	client := newFixtureClientWithRoutes(t, map[string]roundTripResponder{
		"/": func(req *http.Request) (*http.Response, error) {
			body := `<html><body><a href="https://cdn.example.com/app.js">X</a></body></html>`

			return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
	})

	// We block external host at transport level, but for broken-link checks it must be requested.
	// We use a separate "external" client: allow any host, but fail with a network error on cdn.
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "example.com" {
			return responseForRequest(req, http.StatusOK, `<html><body><a href="https://cdn.example.com/app.js">X</a></body></html>`, http.Header{"Content-Type": []string{"text/html"}}), nil
		}
		if req.URL.Host == "cdn.example.com" {
			return nil, errors.New(`Get "https://cdn.example.com/app.js": dial tcp: lookup cdn.example.com: no such host`)
		}

		return nil, errors.New("unexpected host")
	})

	opts := Options{
		URL:         fixtureBaseURL,
		Depth:       0,
		Concurrency: 1,
		Retries:     0,
		Timeout:     time.Second,
		UserAgent:   "test-agent",
		HTTPClient:  client,
		Clock:       clock,
	}

	report, err := analyzeReport(context.Background(), opts)
	require.NoError(t, err)

	require.Len(t, report.Pages, 1)

	bl := report.Pages[0].BrokenLinks
	require.Len(t, bl, 1)

	require.Equal(t, "https://cdn.example.com/app.js", bl[0].URL)
	require.Equal(t, 0, bl[0].StatusCode)
	require.Contains(t, bl[0].Error, "no such host")
}

func TestSpec_BrokenLinks_RetriesApply_AndLastAttemptWins(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}
	var missingCalls int

	client := newFixtureClientWithRoutes(t, map[string]roundTripResponder{
		"/": func(req *http.Request) (*http.Response, error) {
			body := `<html><body><a href="/missing">M</a></body></html>`

			return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		"/missing": func(req *http.Request) (*http.Response, error) {
			missingCalls++
			if missingCalls == 1 {
				return responseForRequest(req, http.StatusInternalServerError, "boom", nil), nil
			}

			return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
		},
	})

	opts := Options{
		URL:         fixtureBaseURL,
		Depth:       0,
		Concurrency: 1,
		Retries:     1, // allow 1 retry => total <= 2 calls
		Timeout:     time.Second,
		UserAgent:   "test-agent",
		HTTPClient:  client,
		Clock:       clock,
	}

	report, err := analyzeReport(context.Background(), opts)
	require.NoError(t, err)

	bl := report.Pages[0].BrokenLinks
	require.Len(t, bl, 1)

	require.Equal(t, 404, bl[0].StatusCode)
	require.Equal(t, "Not Found", bl[0].Error)

	require.LessOrEqual(t, missingCalls, 2)
	require.Equal(t, 2, missingCalls, "must call exactly retries+1 when first attempt fails and second returns final state")
}
