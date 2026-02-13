package crawler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSpec_HTTPClientNil_UsesDefaultClient(t *testing.T) {
	clock := &testClock{now: fixtureTime}
	originalTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != fixtureBaseURL {
			return responseForRequest(req, http.StatusNotFound, "not found", nil), nil
		}

		return responseForRequest(
			req,
			http.StatusOK,
			"<html><body>ok</body></html>",
			http.Header{"Content-Type": []string{"text/html"}},
		), nil
	})
	t.Cleanup(func() {
		http.DefaultTransport = originalTransport
	})

	opts := Options{
		URL:         fixtureBaseURL,
		Depth:       0,
		Concurrency: 1,
		Retries:     0,
		Timeout:     time.Second,
		UserAgent:   "test-agent",
		HTTPClient:  nil,
		Clock:       clock,
	}

	report, err := analyzeReport(context.Background(), opts)
	require.NoError(t, err)

	require.Equal(t, fixtureBaseURL, report.RootURL)
	require.Len(t, report.Pages, 1)

	page := report.Pages[0]
	require.Equal(t, fixtureBaseURL, page.URL)
	require.Equal(t, http.StatusOK, page.HTTPStatus)
	require.Equal(t, statusOK, page.Status)
	require.Empty(t, page.Error)
}
