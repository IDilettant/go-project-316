package crawler

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSpec_Workers_EnableConcurrentFetchOfPages(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}

	var started int32
	release := make(chan struct{})

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "", "/":
				body := `<html><body><a href="/a"></a><a href="/b"></a></body></html>`

				return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
			case "/a", "/b":
				atomic.AddInt32(&started, 1)
				<-release
				return responseForRequest(req, http.StatusOK, "<html><body>ok</body></html>", nil), nil
			default:
				return responseForRequest(req, http.StatusNotFound, "not found", nil), nil
			}
		}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		opts := Options{
			URL:        fixtureBaseURL,
			Depth:      1,
			Concurrency:    2,
			Retries:    0,
			Timeout:    time.Second,
			UserAgent:  "test-agent",
			HTTPClient: client,
			Clock:      clock,
		}
		_, _ = analyzeReport(ctx, opts)
		close(done)
	}()

	// Wait until two fetches start (concurrently).
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&started) >= 2
	}, 500*time.Millisecond, 10*time.Millisecond,
		"expected two page fetches to start concurrently when Workers=2")

	close(release)

	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatalf("Analyze did not finish")
	}
}
