package fetcher

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const exampleURL = "https://example.com/"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func newResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
	}
}

type testClock struct {
	sleepFn func(context.Context, time.Duration) error
}

func (c testClock) Now() time.Time {
	return time.Unix(0, 0)
}

func (c testClock) Sleep(ctx context.Context, duration time.Duration) error {
	if c.sleepFn == nil {
		return nil
	}
	return c.sleepFn(ctx, duration)
}

func newTestFetcher(
	client *http.Client,
	retries int,
	sleepFn func(context.Context, time.Duration) error,
) *Fetcher {
	return New(client, time.Second, "", nil, retries, baseRetryDelay, testClock{sleepFn: sleepFn})
}

func TestFetchOK(t *testing.T) {
	t.Parallel()

	sleepFn := func(context.Context, time.Duration) error { return nil }

	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return newResponse(http.StatusOK, "ok"), nil
	})

	client := &http.Client{Transport: rt}
	fetch := newTestFetcher(client, 0, sleepFn)

	result, err := fetch.Fetch(context.Background(), exampleURL)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; want %d", result.StatusCode, http.StatusOK)
	}
	if string(result.Body) != "ok" {
		t.Fatalf("body = %q; want %q", string(result.Body), "ok")
	}
}

func TestFetchGenericTransportError(t *testing.T) {
	t.Parallel()

	sleepFn := func(context.Context, time.Duration) error { return nil }

	expectedErr := errors.New("network")
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, expectedErr
	})

	client := &http.Client{Transport: rt}
	fetch := newTestFetcher(client, 0, sleepFn)

	_, err := fetch.Fetch(context.Background(), exampleURL)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
}

func TestFetchNoRetryOnNonTemporaryTransportError(t *testing.T) {
	t.Parallel()

	sleepCalls := 0
	sleepFn := func(context.Context, time.Duration) error {
		sleepCalls++
		return nil
	}

	calls := 0
	boom := errors.New("boom")
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return nil, boom
	})

	client := &http.Client{Transport: rt}
	fetch := newTestFetcher(client, 2, sleepFn)

	_, err := fetch.Fetch(context.Background(), exampleURL)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("expected error %v, got %v", boom, err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d; want %d", calls, 1)
	}
	if sleepCalls != 0 {
		t.Fatalf("sleep calls = %d; want %d", sleepCalls, 0)
	}
}

func TestFetchRetriesSuccess(t *testing.T) {
	t.Parallel()

	sleepFn := func(context.Context, time.Duration) error { return nil }

	calls := 0
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls < 3 {
			return newResponse(http.StatusInternalServerError, ""), nil
		}
		return newResponse(http.StatusOK, "ok"), nil
	})

	client := &http.Client{Transport: rt}
	fetch := newTestFetcher(client, 2, sleepFn)

	result, err := fetch.Fetch(context.Background(), exampleURL)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; want %d", result.StatusCode, http.StatusOK)
	}
	if calls != 3 {
		t.Fatalf("calls = %d; want %d", calls, 3)
	}
}

func TestFetchRetriesFail(t *testing.T) {
	t.Parallel()

	sleepFn := func(context.Context, time.Duration) error { return nil }

	calls := 0
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return newResponse(http.StatusInternalServerError, ""), nil
	})

	client := &http.Client{Transport: rt}
	fetch := newTestFetcher(client, 2, sleepFn)

	_, err := fetch.Fetch(context.Background(), exampleURL)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 3 {
		t.Fatalf("calls = %d; want %d", calls, 3)
	}
}

func TestFetchNoRetryOnNotFound(t *testing.T) {
	t.Parallel()

	sleepFn := func(context.Context, time.Duration) error { return nil }

	calls := 0
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return newResponse(http.StatusNotFound, ""), nil
	})

	client := &http.Client{Transport: rt}
	fetch := newTestFetcher(client, 2, sleepFn)

	_, err := fetch.Fetch(context.Background(), exampleURL)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 1 {
		t.Fatalf("calls = %d; want %d", calls, 1)
	}
}

func TestFetchNoRetryOnInvalidRequest(t *testing.T) {
	t.Parallel()

	sleepCalls := 0
	sleepFn := func(context.Context, time.Duration) error {
		sleepCalls++
		return nil
	}

	calls := 0
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return newResponse(http.StatusOK, "ok"), nil
	})

	client := &http.Client{Transport: rt}
	fetch := newTestFetcher(client, 2, sleepFn)

	_, err := fetch.Fetch(context.Background(), "http://[::1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 0 {
		t.Fatalf("calls = %d; want %d", calls, 0)
	}
	if sleepCalls != 0 {
		t.Fatalf("sleep calls = %d; want %d", sleepCalls, 0)
	}
	if !strings.Contains(err.Error(), "invalid request") {
		t.Fatalf("expected error to mention invalid request, got: %v", err)
	}
}

func TestFetchNoRetryOnUnsupportedScheme(t *testing.T) {
	t.Parallel()

	sleepCalls := 0
	sleepFn := func(context.Context, time.Duration) error {
		sleepCalls++
		return nil
	}

	calls := 0
	unsupportedErr := errors.New("unsupported protocol scheme")
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return nil, &url.Error{Op: "Get", URL: req.URL.String(), Err: unsupportedErr}
	})

	client := &http.Client{Transport: rt}
	fetch := newTestFetcher(client, 2, sleepFn)

	_, err := fetch.Fetch(context.Background(), exampleURL)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		t.Fatalf("expected url.Error, got %T", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d; want %d", calls, 1)
	}
	if sleepCalls != 0 {
		t.Fatalf("sleep calls = %d; want %d", sleepCalls, 0)
	}
}

func TestFetchRetriesOnURLErrorWithNetError(t *testing.T) {
	t.Parallel()

	sleepCalls := 0
	sleepFn := func(context.Context, time.Duration) error {
		sleepCalls++
		return nil
	}

	calls := 0
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return nil, &url.Error{Op: "Get", URL: req.URL.String(), Err: retryableNetError{}}
	})

	client := &http.Client{Transport: rt}
	fetch := newTestFetcher(client, 2, sleepFn)

	_, err := fetch.Fetch(context.Background(), exampleURL)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 3 {
		t.Fatalf("calls = %d; want %d", calls, 3)
	}
	if sleepCalls != 2 {
		t.Fatalf("sleep calls = %d; want %d", sleepCalls, 2)
	}
}

func TestFetchNoRetryOnNestedURLErrorUnsupportedScheme(t *testing.T) {
	t.Parallel()

	sleepCalls := 0
	sleepFn := func(context.Context, time.Duration) error {
		sleepCalls++
		return nil
	}

	calls := 0
	unsupportedErr := errors.New("unsupported protocol scheme")
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return nil, &url.Error{
			Op:  "Get",
			URL: req.URL.String(),
			Err: &url.Error{Op: "Get", URL: req.URL.String(), Err: unsupportedErr},
		}
	})

	client := &http.Client{Transport: rt}
	fetch := newTestFetcher(client, 2, sleepFn)

	_, err := fetch.Fetch(context.Background(), exampleURL)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 1 {
		t.Fatalf("calls = %d; want %d", calls, 1)
	}
	if sleepCalls != 0 {
		t.Fatalf("sleep calls = %d; want %d", sleepCalls, 0)
	}
}

func TestFetchRetriesOnNestedURLErrorWithNetError(t *testing.T) {
	t.Parallel()

	sleepCalls := 0
	sleepFn := func(context.Context, time.Duration) error {
		sleepCalls++
		return nil
	}

	calls := 0
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return nil, &url.Error{
			Op:  "Get",
			URL: req.URL.String(),
			Err: &url.Error{Op: "Get", URL: req.URL.String(), Err: retryableNetError{}},
		}
	})

	client := &http.Client{Transport: rt}
	fetch := newTestFetcher(client, 2, sleepFn)

	_, err := fetch.Fetch(context.Background(), exampleURL)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 3 {
		t.Fatalf("calls = %d; want %d", calls, 3)
	}
	if sleepCalls != 2 {
		t.Fatalf("sleep calls = %d; want %d", sleepCalls, 2)
	}
}

func TestFetchRetriesOnUnexpectedEOF(t *testing.T) {
	t.Parallel()

	sleepCalls := 0
	sleepFn := func(context.Context, time.Duration) error {
		sleepCalls++
		return nil
	}

	calls := 0
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return nil, io.ErrUnexpectedEOF
	})

	client := &http.Client{Transport: rt}
	fetch := newTestFetcher(client, 2, sleepFn)

	_, err := fetch.Fetch(context.Background(), exampleURL)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 3 {
		t.Fatalf("calls = %d; want %d", calls, 3)
	}
	if sleepCalls != 2 {
		t.Fatalf("sleep calls = %d; want %d", sleepCalls, 2)
	}
}

type retryableNetError struct{}

func (retryableNetError) Error() string { return "temporary network error" }

func (retryableNetError) Timeout() bool { return false }

func (retryableNetError) Temporary() bool { return true }
