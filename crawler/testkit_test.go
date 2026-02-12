package crawler_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const fixtureBaseURL = "https://example.com"

var fixtureTime = time.Date(2024, time.June, 1, 12, 34, 56, 0, time.UTC)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func readFixture(t *testing.T, parts ...string) []byte {
	t.Helper()

	path := filepath.Join(append([]string{"..", "testdata"}, parts...)...)
	b, err := os.ReadFile(path)
	require.NoError(t, err, "failed to read fixture: %s", path)
	
	return b
}

func newFixtureClient(t *testing.T) *http.Client {
	t.Helper()

	rootHTML := readFixture(t, "pages", "root.html")
	logo := readFixture(t, "assets", "logo.bin")

	return &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			if path == "" {
				path = "/"
			}

			switch path {
			case "/":
				return responseWithBody(http.StatusOK, rootHTML, http.Header{
					"Content-Type": []string{"text/html"},
				}), nil
			case "/missing":
				return responseWithBody(http.StatusNotFound, []byte("missing"), http.Header{}), nil
			case "/static/logo.png":
				h := http.Header{}
				h.Set("Content-Length", strconv.Itoa(len(logo)))
				h.Set("Content-Type", "image/png")
				
				return responseWithBody(http.StatusOK, logo, h), nil
			default:
				return responseWithBody(http.StatusNotFound, []byte("not found"), http.Header{}), nil
			}
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

type testClock struct {
	now time.Time
}

func (c *testClock) Now() time.Time { return c.now }

func (c *testClock) Sleep(ctx context.Context, duration time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
