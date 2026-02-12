package crawler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSpec_SameOrigin_MustCompare_SchemeAndHostIncludingPort(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}

	// Root: https://example.com
	// The HTML contains links:
	// - http://example.com/a   (scheme differs) => MUST NOT be crawled
	// - https://example.com:8443/b (port differs) => MUST NOT be crawled
	// - https://example.com/c (same scheme+host) => MAY be crawled when depth>=1
	client := newFixtureClientWithRoutes(t, map[string]roundTripResponder{
		"/": func(req *http.Request) (*http.Response, error) {
			body := `
				<html><body>
					<a href="http://example.com/a">A</a>
					<a href="https://example.com:8443/b">B</a>
					<a href="https://example.com/c">C</a>
				</body></html>
			`

			return responseForRequest(req, http.StatusOK, body, http.Header{
				"Content-Type": []string{"text/html"},
			}), nil
		},
		"/c": func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusOK, "<html><body>ok</body></html>", nil), nil
		},
	})

	opts := Options{
		URL:        "https://example.com",
		Depth:      1,
		Workers:    1,
		Timeout:    time.Second,
		UserAgent:  "test-agent",
		HTTPClient: client,
		Clock:      clock,
	}

	report, err := analyzeReport(context.Background(), opts)
	require.NoError(t, err)

	// Expect only root and /c.
	require.Len(t, report.Pages, 2)
	require.Equal(t, "https://example.com", report.Pages[0].URL)
	require.Equal(t, "https://example.com/c", report.Pages[1].URL)
}
