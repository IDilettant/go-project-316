package crawler

import (
	"context"
	"errors"
	"net/http"
	"net/url"
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
		Depth:       1,
		Concurrency: 1,
		Retries:     0,
		Timeout:     time.Second,
		UserAgent:   "test-agent",
		HTTPClient:  client,
		Clock:       clock,
	}

	report, err := analyzeReport(context.Background(), opts)
	require.NoError(t, err)

	require.Len(t, report.Pages, 2)

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
			body := `<html><body><a href="/broken">X</a></body></html>`

			return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		"/broken": func(req *http.Request) (*http.Response, error) {
			return nil, errors.New(`Get "https://example.com/broken": dial tcp: lookup example.com: no such host`)
		},
	})

	opts := Options{
		URL:         fixtureBaseURL,
		Depth:       1,
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

	require.Equal(t, fixtureBaseURL+"/broken", bl[0].URL)
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
		Depth:       1,
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

func TestSpec_BrokenLinks_DeduplicatesSameURLOnPage(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}

	client := newFixtureClientWithRoutes(t, map[string]roundTripResponder{
		"/": func(req *http.Request) (*http.Response, error) {
			body := `
				<html><body>
					<a href="/missing">M1</a>
					<a href="/missing">M2</a>
				</body></html>
			`

			return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		"/missing": func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
		},
	})

	opts := Options{
		URL:         fixtureBaseURL,
		Depth:       1,
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
	require.Len(t, report.Pages[0].BrokenLinks, 1)
	require.Equal(t, fixtureBaseURL+"/missing", report.Pages[0].BrokenLinks[0].URL)
}

func TestSpec_BrokenLinks_NotCollectedAtMaxDepth(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}
	var missingCalls int

	client := newFixtureClientWithRoutes(t, map[string]roundTripResponder{
		"/": func(req *http.Request) (*http.Response, error) {
			body := `<html><body><a href="/child">child</a></body></html>`
			return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		"/child": func(req *http.Request) (*http.Response, error) {
			body := `<html><body><a href="/missing">missing</a></body></html>`
			return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		"/missing": func(req *http.Request) (*http.Response, error) {
			missingCalls++
			return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
		},
	})

	opts := Options{
		URL:         fixtureBaseURL,
		Depth:       1,
		Concurrency: 1,
		Retries:     0,
		Timeout:     time.Second,
		UserAgent:   "test-agent",
		HTTPClient:  client,
		Clock:       clock,
	}

	report, err := analyzeReport(context.Background(), opts)
	require.NoError(t, err)
	require.Len(t, report.Pages, 2)

	var childPage *Page
	for i := range report.Pages {
		parsedURL, parseErr := url.Parse(report.Pages[i].URL)
		require.NoError(t, parseErr)
		if parsedURL.Path == "/child" {
			childPage = &report.Pages[i]
			break
		}
	}

	require.NotNil(t, childPage)
	require.Empty(t, childPage.BrokenLinks)
	require.Zero(t, missingCalls)
}

func TestSpec_BrokenLinks_DeduplicatesTrailingSlashVariants(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}

	client := newFixtureClientWithRoutes(t, map[string]roundTripResponder{
		"/": func(req *http.Request) (*http.Response, error) {
			body := `
				<html><body>
					<a href="/missing">M1</a>
					<a href="/missing/">M2</a>
				</body></html>
			`
			return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		"/missing": func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
		},
		"/missing/": func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
		},
	})

	opts := Options{
		URL:         fixtureBaseURL,
		Depth:       1,
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
	require.Len(t, report.Pages[0].BrokenLinks, 1)
	require.Equal(t, fixtureBaseURL+"/missing", report.Pages[0].BrokenLinks[0].URL)
}
