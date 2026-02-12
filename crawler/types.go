package crawler

import (
	"net/http"
	"time"

	"code/internal/limiter"
)

// Options configures crawler behavior.
// Depth is the maximum crawl depth from the root (depth=1 includes root and children).
// Delay and RPS control rate limiting; RPS overrides Delay.
// Retries is the number of retries after the first attempt.
// IndentJSON affects formatting only.
type Options struct {
	URL                string
	Depth              int
	Retries            int
	Delay              time.Duration
	Timeout            time.Duration
	RPS                float64
	UserAgent          string
	Workers            int
	MaxConcurrentFetch int
	IndentJSON         bool
	HTTPClient         *http.Client
	Clock              limiter.Timer
}

// Report is the JSON report returned by Analyze.
type Report struct {
	RootURL     string `json:"root_url"`
	Depth       int    `json:"depth"`
	GeneratedAt string `json:"generated_at"`
	Pages       []Page `json:"pages"`
}

// Page describes a crawled page.
type Page struct {
	URL          string       `json:"url"`
	Depth        int          `json:"depth"`
	HTTPStatus   int          `json:"http_status"`
	Status       string       `json:"status"`
	Error        string       `json:"error"`
	SEO          SEO          `json:"seo"`
	BrokenLinks  []BrokenLink `json:"broken_links"`
	Assets       []Asset      `json:"assets"`
	DiscoveredAt string       `json:"discovered_at"`
}

// SEO describes title/description/h1 data for a page.
// Missing elements yield false flags and empty strings; text is HTML-decoded.
type SEO struct {
	HasTitle       bool   `json:"has_title"`
	Title          string `json:"title"`
	HasDescription bool   `json:"has_description"`
	Description    string `json:"description"`
	HasH1          bool   `json:"has_h1"`
}

// BrokenLink describes an unreachable link (4xx/5xx or network error) with an absolute URL.
type BrokenLink struct {
	URL        string `json:"url"`
	StatusCode int    `json:"status_code"`
	Error      string `json:"error"`
}

// Asset describes a fetched asset; SizeBytes falls back to body length if Content-Length is missing.
type Asset struct {
	URL        string `json:"url"`
	Type       string `json:"type"`
	StatusCode int    `json:"status_code"`
	SizeBytes  int64  `json:"size_bytes"`
	Error      string `json:"error"`
}
