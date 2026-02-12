package crawler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const fixtureBaseURL = "https://example.com"

var fixtureTime = time.Date(2024, time.June, 1, 12, 34, 56, 0, time.UTC)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

// roundTripResponder is used by route-based fixture clients.
type roundTripResponder func(*http.Request) (*http.Response, error)

// newFixtureClientWithRoutes returns an http.Client that routes by URL.Path for host "example.com".
// - For requests to https://example.com/... it matches by Path in routes.
// - For any other host, it returns 404 unless caller provided a "*" handler (optional).
func newFixtureClientWithRoutes(t *testing.T, routes map[string]roundTripResponder) *http.Client {
	t.Helper()

	return &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			// Optional wildcard handler for any request
			if h, ok := routes["*"]; ok {
				return h(req)
			}

			// Only handle example.com by default (tests may override Transport manually).
			if !strings.EqualFold(req.URL.Host, "example.com") {
				return responseForRequest(req, http.StatusNotFound, "not found", nil), nil
			}

			path := req.URL.EscapedPath()
			if path == "" {
				path = "/"
			}

			h, ok := routes[path]
			if !ok {
				return responseForRequest(req, http.StatusNotFound, "not found", nil), nil
			}
			return h(req)
		}),
	}
}

func responseWithBody(status int, body []byte, header http.Header) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

func responseForRequest(req *http.Request, status int, body string, header http.Header) *http.Response {
	resp := responseWithBody(status, []byte(body), header)
	resp.Request = req
	return resp
}

type testClock struct {
	now time.Time
}

func (c *testClock) Now() time.Time { return c.now }

func (c *testClock) Sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
