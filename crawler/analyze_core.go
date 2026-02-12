package crawler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"code/internal/fetcher"
	"code/internal/limiter"
)

const (
	defaultUserAgent = "hexlet-go-crawler/1.0"
	statusOK         = "ok"
	statusError      = "error"
)

// analyzeReport crawls a site and returns a report.
func analyzeReport(ctx context.Context, opts Options) (Report, error) {
	report := newReport(opts)

	if opts.URL == "" {
		return report, errors.New("url is required")
	}

	baseURL, err := parseRootURL(opts.URL)
	if err != nil {
		page := newPage(opts.URL, 0, opts.Clock.Now())
		page.Status = statusError
		page.Error = fmt.Sprintf("invalid url: %v", err)
		report.Pages = append(report.Pages, page)

		return report, fmt.Errorf("invalid root url: %w", err)
	}

	baseURL.Fragment = ""
	rootURL := baseURL.String()
	report.RootURL = rootURL

	if opts.HTTPClient == nil {
		page := newPage(rootURL, 0, opts.Clock.Now())
		page.Status = statusError
		page.Error = "http client is required"
		report.Pages = append(report.Pages, page)

		return report, errors.New("http client is required")
	}

	rateInterval := rateInterval(opts)
	rateLimiter := limiter.NewWithTimer(rateInterval, opts.Clock)

	fetch := fetcher.New(
		opts.HTTPClient,
		opts.Timeout,
		opts.UserAgent,
		rateLimiter,
		opts.Retries,
		opts.Delay,
		opts.Clock,
	)

	analyzer := newAnalyzer(opts, baseURL, fetch, &report)
	analysisErr := analyzer.run(ctx)

	return report, analysisErr
}

func newReport(opts Options) Report {
	return Report{
		RootURL:     opts.URL,
		Depth:       opts.Depth,
		GeneratedAt: opts.Clock.Now().UTC().Format(time.RFC3339),
		Pages:       []Page{},
	}
}

func newPage(pageURL string, depth int, discoveredAt time.Time) Page {
	return Page{
		URL:          pageURL,
		Depth:        depth,
		HTTPStatus:   0,
		Status:       "",
		Error:        "",
		SEO:          SEO{},
		BrokenLinks:  []BrokenLink{},
		Assets:       []Asset{},
		DiscoveredAt: discoveredAt.UTC().Format(time.RFC3339),
	}
}

func rateInterval(opts Options) time.Duration {
	if opts.RPS > 0 {
		interval := time.Duration(float64(time.Second) / opts.RPS)
		if interval <= 0 {
			return time.Nanosecond
		}
		return interval
	}

	delay := opts.Delay
	if delay < 0 {
		delay = 0
	}
	if delay > 0 {
		return delay
	}

	return 0
}

func parseRootURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("missing scheme or host")
	}
	if parsed.Path == "/" {
		parsed.Path = ""
		parsed.RawPath = ""
	}
	parsed.Fragment = ""
	return parsed, nil
}

func fetchAssetResult(ctx context.Context, fetch *fetcher.Fetcher, absoluteURL string) assetFetchResult {
	result, err := fetch.Fetch(ctx, absoluteURL)
	fetchResult := assetFetchResult{
		statusCode: result.StatusCode,
		sizeBytes:  0,
		err:        "",
	}

	errMsg := ""
	if err != nil {
		errMsg = errorString(err, result.StatusCode)
		if result.StatusCode == 0 {
			fetchResult.err = errMsg
			return fetchResult
		}
	}

	sizeBytes, sizeErr := sizeFromResult(result)
	if sizeErr == nil {
		fetchResult.sizeBytes = sizeBytes
	}

	parts := []string{}
	if result.StatusCode >= http.StatusBadRequest {
		parts = append(parts, fmt.Sprintf("http status %d", result.StatusCode))
	}
	if errMsg != "" {
		parts = append(parts, errMsg)
	}
	if sizeErr != nil {
		parts = append(parts, sizeErr.Error())
	}
	if len(parts) > 0 {
		fetchResult.err = strings.Join(parts, ": ")
	}

	return fetchResult
}

func sizeFromResult(result fetcher.Result) (int64, error) {
	contentLength := result.Header.Get("Content-Length")
	if contentLength != "" {
		value, err := parseContentLength(contentLength)
		if err != nil {
			return 0, err
		}
		return value, nil
	}

	if result.Body == nil {
		return 0, errors.New("unable to determine asset size")
	}

	return int64(len(result.Body)), nil
}

func parseContentLength(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	parsedValue, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid content length %q: %w", trimmed, err)
	}
	if parsedValue < 0 {
		return 0, fmt.Errorf("invalid content length %q: negative value", trimmed)
	}
	return parsedValue, nil
}

func errorString(err error, statusCode int) string {
	if err != nil {
		return err.Error()
	}

	if statusCode >= 400 {
		return statusText(statusCode)
	}

	return ""
}

func errorForStatus(err error, statusCode int) error {
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return errors.New(statusText(statusCode))
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
