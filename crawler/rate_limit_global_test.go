package crawler

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSpec_RateLimit_RPSOverridesDelay_AndIsGlobal(t *testing.T) {
	t.Parallel()

	clock := &rateClock{now: fixtureTime}
	rec := &requestRecorder{}

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			rec.record(clock.Now())
			if req.URL.Path == "" || req.URL.Path == "/" {
				// 6 internal links
				body := `<html><body>
					<a href="/a"></a><a href="/b"></a><a href="/c"></a>
					<a href="/d"></a><a href="/e"></a><a href="/f"></a>
				</body></html>`
				return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
			}

			return responseForRequest(req, http.StatusOK, "<html><body>ok</body></html>", nil), nil
		}),
	}

	opts := Options{
		URL:        fixtureBaseURL,
		Depth:      1,
		Workers:    4,                // even with workers>1, the limiter must be global
		Delay:      10 * time.Second, // should be ignored
		RPS:        5,                // 200ms interval
		Retries:    0,
		Timeout:    time.Second,
		UserAgent:  "test-agent",
		HTTPClient: client,
		Clock:      clock,
	}

	_, err := analyzeReport(context.Background(), opts)
	require.NoError(t, err)

	times := rec.snapshot()
	require.GreaterOrEqual(t, len(times), 1)

	// Verify that Sleep is used with an interval of ~200ms.
	// We assert the minimum here: all sleepDurations must be == 200ms (except the zero first request).
	for _, d := range clock.sleepDurations() {
		require.Equal(t, 200*time.Millisecond, d)
	}
}

func TestSpec_RateLimit_CancelStopsWaiting(t *testing.T) {
	t.Parallel()

	clock := newBlockingRateClock(fixtureTime)
	rootHit := make(chan struct{})

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "" || req.URL.Path == "/" {
				close(rootHit)
				body := `<html><body><a href="/a"></a></body></html>`
				return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
			}

			return responseForRequest(req, http.StatusOK, "<html><body>ok</body></html>", nil), nil
		}),
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		opts := Options{
			URL:        fixtureBaseURL,
			Depth:      1,
			Workers:    2,
			Delay:      10 * time.Second, // force waiting
			Retries:    0,
			Timeout:    time.Second,
			UserAgent:  "test-agent",
			HTTPClient: client,
			Clock:      clock,
		}
		_, _ = analyzeReport(ctx, opts)
		close(done)
	}()

	<-rootHit
	<-clock.sleepStarted

	cancel()

	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatalf("Analyze did not finish after cancel")
	}
}

type requestRecorder struct {
	mu sync.Mutex
	ts []time.Time
}

func (r *requestRecorder) record(t time.Time) {
	r.mu.Lock()
	r.ts = append(r.ts, t)
	r.mu.Unlock()
}
func (r *requestRecorder) snapshot() []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]time.Time, len(r.ts))
	copy(out, r.ts)
	return out
}

type rateClock struct {
	mu   sync.Mutex
	now  time.Time
	slps []time.Duration
}

func (c *rateClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}
func (c *rateClock) Sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	c.mu.Lock()
	c.slps = append(c.slps, d)
	c.now = c.now.Add(d)
	c.mu.Unlock()

	return nil
}
func (c *rateClock) sleepDurations() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]time.Duration, len(c.slps))
	copy(out, c.slps)

	return out
}

type blockingRateClock struct {
	mu           sync.Mutex
	now          time.Time
	sleepStarted chan struct{}
}

func newBlockingRateClock(now time.Time) *blockingRateClock {
	return &blockingRateClock{now: now, sleepStarted: make(chan struct{})}
}

func (c *blockingRateClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}
func (c *blockingRateClock) Sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-c.sleepStarted:
	default:
		close(c.sleepStarted)
	}

	<-ctx.Done()

	return ctx.Err()
}
