package fetcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"code/internal/limiter"
)

const (
	baseRetryDelay = 100 * time.Millisecond
	maxRetryDelay  = 2 * time.Second
)

var errInvalidRequest = errors.New("invalid request")

// Result contains the HTTP response data.
type Result struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// Fetcher performs HTTP requests with retries and rate limiting.
type Fetcher struct {
	client     *http.Client
	timeout    time.Duration
	userAgent  string
	limiter    *limiter.Limiter
	retries    int
	retryDelay time.Duration
	clock      limiter.Timer
}

// New creates a Fetcher with the provided configuration.
func New(
	client *http.Client,
	timeout time.Duration,
	userAgent string,
	limiter *limiter.Limiter,
	retries int,
	retryDelay time.Duration,
	clock limiter.Timer,
) *Fetcher {
	if retryDelay <= 0 {
		retryDelay = baseRetryDelay
	}

	return &Fetcher{
		client:     client,
		timeout:    timeout,
		userAgent:  userAgent,
		limiter:    limiter,
		retries:    retries,
		retryDelay: retryDelay,
		clock:      clock,
	}
}

// Fetch performs a GET request with retries for temporary failures (network errors, 429, 5xx).
// It returns the result from the last attempt.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (Result, error) {
	attempts := f.retries + 1
	var lastResult Result
	var lastErr error

	for attempt := range attempts {
		result, err := f.fetchOnce(ctx, rawURL)
		lastResult = result
		lastErr = err

		if err == nil && result.StatusCode < http.StatusBadRequest {
			if ctx.Err() != nil {
				return Result{}, ctx.Err()
			}

			return result, nil
		}

		retry, retryErr := f.shouldRetry(ctx, attempt, attempts, result, err)
		if !retry {
			return result, retryErr
		}
	}

	return lastResult, lastErr
}

func (f *Fetcher) fetchOnce(ctx context.Context, rawURL string) (Result, error) {
	if f.limiter != nil {
		if err := f.limiter.Wait(ctx); err != nil {
			return Result{}, err
		}
	}

	return f.doRequest(ctx, rawURL)
}

func (f *Fetcher) shouldRetry(
	ctx context.Context,
	attempt int,
	attempts int,
	result Result,
	err error,
) (bool, error) {
	if ctx.Err() != nil {
		return false, coalesceError(err, ctx.Err())
	}

	if !isRetryable(result.StatusCode, err) || attempt == attempts-1 {
		return false, errorForStatus(err, result.StatusCode)
	}

	sleepDelay := f.retryDelayFor(attempt + 1)

	err = f.clock.Sleep(ctx, sleepDelay)
	if err != nil {
		return false, err
	}

	return true, nil
}

func (f *Fetcher) doRequest(ctx context.Context, rawURL string) (Result, error) {
	requestCtx := ctx
	var cancel context.CancelFunc
	if f.timeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, f.timeout)
	}
	if cancel != nil {
		defer cancel()
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", errInvalidRequest, err)
	}

	if parsedURL.Path == "" {
		parsedURL.Path = "/"
	}

	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", errInvalidRequest, err)
	}

	if f.userAgent != "" {
		request.Header.Set("User-Agent", f.userAgent)
	}

	response, err := f.client.Do(request)
	if err != nil {
		return Result{}, err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return Result{StatusCode: response.StatusCode, Header: response.Header}, fmt.Errorf("read body: %w", err)
	}

	return Result{StatusCode: response.StatusCode, Header: response.Header, Body: body}, nil
}

func isRetryable(statusCode int, err error) bool {
	if err != nil {
		return isRetryableError(err)
	}

	if statusCode == http.StatusTooManyRequests {
		return true
	}

	return statusCode >= http.StatusInternalServerError
}

func isRetryableError(err error) bool {
	if isContextCanceled(err) {
		return false
	}

	if errors.Is(err, errInvalidRequest) {
		return false
	}

	if isEOFLike(err) {
		return true
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return isRetryableURLError(urlErr)
	}

	// Non-url errors are retryable only if they look like a temporary transport/network issue.
	return isNetError(err)
}

func isRetryableURLError(urlErr *url.Error) bool {
	if urlErr == nil {
		return false
	}

	if isContextCanceled(urlErr) {
		return false
	}

	if errors.Is(urlErr, errInvalidRequest) {
		return false
	}

	if isEOFLike(urlErr) {
		return true
	}

	err := urlErr.Err
	for err != nil {
		if isContextCanceled(err) {
			return false
		}

		if errors.Is(err, errInvalidRequest) {
			return false
		}

		if isEOFLike(err) {
			return true
		}

		var inner *url.Error
		if errors.As(err, &inner) {
			err = inner.Err

			continue
		}

		return isNetError(err)
	}

	return false
}

func isContextCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func isNetError(err error) bool {
	var netErr net.Error

	return errors.As(err, &netErr)
}

func isEOFLike(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

func errorForStatus(err error, statusCode int) error {
	if err != nil {
		return err
	}

	if statusCode >= http.StatusBadRequest {
		return fmt.Errorf("%s", statusText(statusCode))
	}

	return nil
}

func statusText(statusCode int) string {
	text := http.StatusText(statusCode)
	if text == "" {
		return fmt.Sprintf("http status %d", statusCode)
	}

	return text
}

func coalesceError(primary, fallback error) error {
	if primary != nil {
		return primary
	}

	return fallback
}

func (f *Fetcher) retryDelayFor(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	delay := f.retryDelay
	for i := 1; i < attempt; i++ {
		if delay >= maxRetryDelay {
			return maxRetryDelay
		}

		delay *= 2
	}

	if delay > maxRetryDelay {
		return maxRetryDelay
	}

	return delay
}
