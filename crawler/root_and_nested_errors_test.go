package crawler

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSpec_RootNetworkError_ReturnsError_ButReportHasPageWithStatusError(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			// Simulate a network error on root.
			return nil, errors.New("dial tcp: no such host")
		}),
	}

	opts := Options{
		URL:         "https://example.com",
		Depth:       1,
		Concurrency: 1,
		Retries:     0,
		Timeout:     time.Second,
		UserAgent:   "test-agent",
		HTTPClient:  client,
		Clock:       clock,
	}

	report, err := analyzeReport(context.Background(), opts)
	require.Error(t, err, "root failure is allowed to be returned as Analyze() error")

	require.Equal(t, "https://example.com", report.RootURL)
	require.Len(t, report.Pages, 1)

	p := report.Pages[0]
	require.Equal(t, "https://example.com", p.URL)
	require.Equal(t, 0, p.HTTPStatus) // 0 when no response received
	require.Equal(t, "error", p.Status)
	require.NotEmpty(t, p.Error)
	require.Nil(t, p.BrokenLinks)
	require.Nil(t, p.Assets)
}

func TestSpec_NestedNetworkError_MustNotReturnAnalyzeError(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}

	client := newFixtureClientWithRoutes(t, map[string]roundTripResponder{
		"/": func(req *http.Request) (*http.Response, error) {
			body := `<html><body><a href="/a">A</a></body></html>`

			return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		"/a": func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("connection reset by peer")
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
	require.NoError(t, err, "nested errors must be captured in report, not returned")

	require.Len(t, report.Pages, 1)

	// root ok
	require.Equal(t, "ok", report.Pages[0].Status)

	// /a is a broken link (network error), not crawled as a page.
	bl := report.Pages[0].BrokenLinks
	require.Len(t, bl, 1)
	require.Equal(t, fixtureBaseURL+"/a", bl[0].URL)
	require.Equal(t, 0, bl[0].StatusCode)
	require.NotEmpty(t, bl[0].Error)
}
