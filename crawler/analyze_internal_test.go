package crawler

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"code/internal/fetcher"
)

func TestRateInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts Options
		want time.Duration
	}{
		{name: "rps priority", opts: Options{RPS: 5, Delay: time.Second}, want: 200 * time.Millisecond},
		{name: "negative delay", opts: Options{Delay: -time.Second}, want: 0},
		{name: "positive delay", opts: Options{Delay: 150 * time.Millisecond}, want: 150 * time.Millisecond},
		{name: "zero config", opts: Options{}, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := rateInterval(tt.opts)
			if got != tt.want {
				t.Fatalf("rateInterval() = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestParseRootURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "valid url", raw: "https://example.com/a", want: "https://example.com/a", wantErr: false},
		{name: "root slash normalized", raw: "https://example.com/", want: "https://example.com", wantErr: false},
		{name: "invalid url", raw: "://broken", wantErr: true},
		{name: "missing host", raw: "https:///a", wantErr: true},
		{name: "missing scheme", raw: "example.com/a", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseRootURL(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.String() != tt.want {
				t.Fatalf("parseRootURL() = %q; want %q", got.String(), tt.want)
			}
		})
	}
}

func TestParseContentLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    int64
		wantErr bool
	}{
		{name: "valid", value: "42", want: 42},
		{name: "trim spaces", value: " 7 ", want: 7},
		{name: "invalid", value: "abc", wantErr: true},
		{name: "negative", value: "-1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseContentLength(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseContentLength() = %d; want %d", got, tt.want)
			}
		})
	}
}

func TestSizeFromResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		result  fetcher.Result
		want    int64
		wantErr bool
	}{
		{name: "header length", result: fetcher.Result{Header: http.Header{"Content-Length": []string{"5"}}, Body: []byte("123")}, want: 5},
		{name: "body length", result: fetcher.Result{Header: http.Header{}, Body: []byte("1234")}, want: 4},
		{name: "invalid content length", result: fetcher.Result{Header: http.Header{"Content-Length": []string{"x"}}}, wantErr: true},
		{name: "no body", result: fetcher.Result{Header: http.Header{}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := sizeFromResult(tt.result)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("sizeFromResult() = %d; want %d", got, tt.want)
			}
		})
	}
}

func TestErrorHelpers(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	if got := errorString(boom, 200); got != "boom" {
		t.Fatalf("errorString(err) = %q", got)
	}
	if got := errorString(nil, 404); got != "Not Found" {
		t.Fatalf("errorString(status) = %q", got)
	}
	if got := errorString(nil, 200); got != "" {
		t.Fatalf("errorString(ok) = %q", got)
	}

	if err := errorForStatus(boom, 500); !errors.Is(err, boom) {
		t.Fatalf("errorForStatus should return original error")
	}
	if err := errorForStatus(nil, 404); err == nil || err.Error() != "Not Found" {
		t.Fatalf("errorForStatus(404) = %v", err)
	}
	if err := errorForStatus(nil, 200); err != nil {
		t.Fatalf("errorForStatus(200) = %v", err)
	}

	if got := statusText(599); got != "http status 599" {
		t.Fatalf("statusText(599) = %q", got)
	}
}

func TestNormalizeHelpers(t *testing.T) {
	t.Parallel()

	if got := normalizeMaxDepth(-1); got != 0 {
		t.Fatalf("normalizeMaxDepth(-1) = %d", got)
	}
	if got := normalizeMaxDepth(2); got != 2 {
		t.Fatalf("normalizeMaxDepth(2) = %d", got)
	}
	if got := normalizeMaxConcurrentFetch(Options{Concurrency: 0, MaxConcurrentFetch: 0}); got != 1 {
		t.Fatalf("normalizeMaxConcurrentFetch() = %d", got)
	}
	if got := linkCheckPoolSize(Options{Concurrency: 10}); got != 2 {
		t.Fatalf("linkCheckPoolSize() = %d", got)
	}
}

func TestResolveLinksSkipsInvalid(t *testing.T) {
	t.Parallel()

	base, err := url.Parse("https://example.com")
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}

	a := &analyzer{baseURL: base}
	got := a.resolveLinks("https://example.com", []string{"/a", "#frag", "mailto:x@y.z", "", "https://example.com/b#f"})
	want := []string{"https://example.com/a", "https://example.com/b"}

	if len(got) != len(want) {
		t.Fatalf("len(resolveLinks) = %d; want %d", len(got), len(want))
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("resolveLinks[%d] = %q; want %q", i, got[i], want[i])
		}
	}
}

func TestBuildLinkResults_DeduplicatesBrokenLinksByCanonicalURL(t *testing.T) {
	t.Parallel()

	results := []linkCheck{
		{
			broken: true,
			link: BrokenLink{
				URL:        "/missing",
				StatusCode: 404,
				Error:      "Not Found",
			},
			url: "https://example.com/missing",
		},
		{
			broken: true,
			link: BrokenLink{
				URL:        "https://example.com/missing",
				StatusCode: 404,
				Error:      "Not Found",
			},
			url: "https://example.com/missing",
		},
	}

	processed := []bool{true, true}

	brokenLinks, crawlLinks := buildLinkResults(results, processed)
	if len(crawlLinks) != 0 {
		t.Fatalf("len(crawlLinks) = %d; want 0", len(crawlLinks))
	}

	if len(brokenLinks) != 1 {
		t.Fatalf("len(brokenLinks) = %d; want 1", len(brokenLinks))
	}

	if brokenLinks[0].URL != "https://example.com/missing" {
		t.Fatalf("brokenLinks[0].URL = %q; want %q", brokenLinks[0].URL, "https://example.com/missing")
	}
}
