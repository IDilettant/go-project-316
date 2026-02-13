package crawler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"code/internal/fetcher"
	"code/internal/limiter"

	"github.com/stretchr/testify/require"
)

type callTracker struct {
	mu          sync.Mutex
	byRequestID map[string]int
	byHostPath  map[string]int
}

func newCallTracker() *callTracker {
	return &callTracker{
		byRequestID: map[string]int{},
		byHostPath:  map[string]int{},
	}
}

func (c *callTracker) add(req *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()

	reqID := requestID(req)
	c.byRequestID[reqID]++

	host := strings.ToLower(req.URL.Hostname())
	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}

	c.byHostPath[host+"|"+path]++
}

func (c *callTracker) countHostPath(host string, path string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.byHostPath[strings.ToLower(host)+"|"+path]
}

func newTrackedClient(
	t *testing.T,
	routes map[string]roundTripResponder,
) (*http.Client, *callTracker) {
	t.Helper()

	tracker := newCallTracker()
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			tracker.add(req)

			key := requestID(req)
			if handler, ok := routes[key]; ok {
				return handler(req)
			}

			return responseForRequest(req, http.StatusNotFound, "not found", nil), nil
		}),
	}

	return client, tracker
}

func requestID(req *http.Request) string {
	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}

	return strings.ToLower(req.URL.Scheme) + "://" + strings.ToLower(req.URL.Host) + path
}

func routeID(scheme string, host string, path string) string {
	if path == "" {
		path = "/"
	}

	return strings.ToLower(scheme) + "://" + strings.ToLower(host) + path
}

func optionsForContract(rootURL string, depth int, retries int, client *http.Client, clock *testClock) Options {
	return Options{
		URL:         rootURL,
		Depth:       depth,
		Concurrency: 1,
		Retries:     retries,
		Timeout:     time.Second,
		UserAgent:   "test-agent",
		HTTPClient:  client,
		Clock:       clock,
	}
}

func findPageByPath(t *testing.T, report Report, wantPath string) *Page {
	t.Helper()

	for i := range report.Pages {
		parsed, err := url.Parse(report.Pages[i].URL)
		require.NoError(t, err)

		path := parsed.EscapedPath()
		if path == "" {
			path = "/"
		}

		if path == wantPath {
			return &report.Pages[i]
		}
	}

	return nil
}

func totalBrokenLinks(report Report) int {
	total := 0
	for i := range report.Pages {
		total += len(report.Pages[i].BrokenLinks)
	}

	return total
}

func TestSpec_BrokenLinks_DepthBoundary_RootOnlyAtDepthOne(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}
	routes := map[string]roundTripResponder{
		routeID("https", "example.com", "/"): func(req *http.Request) (*http.Response, error) {
			body := `<html><body><a href="/missing">m</a><a href="/child">c</a></body></html>`
			return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		routeID("https", "example.com", "/missing"): func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
		},
		routeID("https", "example.com", "/child"): func(req *http.Request) (*http.Response, error) {
			body := `<html><body><a href="/missing2">m2</a></body></html>`
			return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		routeID("https", "example.com", "/missing2"): func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusNotFound, "missing2", nil), nil
		},
	}
	client, calls := newTrackedClient(t, routes)

	report, err := analyzeReport(context.Background(), optionsForContract(fixtureBaseURL, 1, 0, client, clock))
	require.NoError(t, err)
	require.Len(t, report.Pages, 2)

	root := findPageByPath(t, report, "/")
	require.NotNil(t, root)
	require.Len(t, root.BrokenLinks, 1)
	require.Equal(t, fixtureBaseURL+"/missing", root.BrokenLinks[0].URL)

	child := findPageByPath(t, report, "/child")
	require.NotNil(t, child)
	require.Empty(t, child.BrokenLinks)
	require.Zero(t, calls.countHostPath("example.com", "/missing2"))
}

func TestSpec_BrokenLinks_DepthZero_NoLinkChecks(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}
	routes := map[string]roundTripResponder{
		routeID("https", "example.com", "/"): func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusOK, `<html><body><a href="/missing">m</a></body></html>`, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		routeID("https", "example.com", "/missing"): func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
		},
	}
	client, calls := newTrackedClient(t, routes)

	report, err := analyzeReport(context.Background(), optionsForContract(fixtureBaseURL, 0, 0, client, clock))
	require.NoError(t, err)
	require.Len(t, report.Pages, 1)

	root := findPageByPath(t, report, "/")
	require.NotNil(t, root)
	require.Empty(t, root.BrokenLinks)
	require.Zero(t, calls.countHostPath("example.com", "/missing"))
}

func TestSpec_BrokenLinks_RootErrorPageNullCollections(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}
	routes := map[string]roundTripResponder{
		routeID("https", "example.com", "/"): func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusInternalServerError, "boom", nil), nil
		},
	}
	client, _ := newTrackedClient(t, routes)

	report, err := analyzeReport(context.Background(), optionsForContract(fixtureBaseURL, 1, 0, client, clock))
	require.Error(t, err)
	require.Len(t, report.Pages, 1)
	require.Equal(t, statusError, report.Pages[0].Status)
	require.Nil(t, report.Pages[0].BrokenLinks)
	require.Nil(t, report.Pages[0].Assets)

	raw, marshalErr := json.Marshal(report)
	require.NoError(t, marshalErr)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	pages := decoded["pages"].([]any)

	var pageMap map[string]any
	for _, page := range pages {
		candidate := page.(map[string]any)
		rawURL := candidate["url"].(string)
		parsed, parseErr := url.Parse(rawURL)
		require.NoError(t, parseErr)
		path := parsed.EscapedPath()
		if path == "" {
			path = "/"
		}
		if path == "/" {
			pageMap = candidate
			break
		}
	}

	require.NotNil(t, pageMap)
	require.Nil(t, pageMap["broken_links"])
	require.Nil(t, pageMap["assets"])
}

func TestSpec_BrokenLinks_ChildErrorPageNullCollections_ProcessJob(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}
	client, _ := newTrackedClient(t, map[string]roundTripResponder{
		routeID("https", "example.com", "/child"): func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusInternalServerError, "boom", nil), nil
		},
	})

	opts := optionsForContract(fixtureBaseURL, 1, 0, client, clock)
	baseURL, err := parseRootURL(opts.URL)
	require.NoError(t, err)

	limiter := limiter.NewWithTimer(rateInterval(opts), opts.Clock)
	pageFetcher := fetcher.New(
		opts.HTTPClient,
		opts.Timeout,
		opts.UserAgent,
		limiter,
		opts.Retries,
		opts.Delay,
		opts.Clock,
	)

	report := newReport(opts)
	a := newAnalyzer(opts, baseURL, pageFetcher, &report)

	result := a.processJob(context.Background(), crawlJob{
		url:          fixtureBaseURL + "/child",
		depth:        1,
		discoveredAt: fixtureTime,
	})

	require.Equal(t, statusError, result.page.Status)
	require.Nil(t, result.page.BrokenLinks)
	require.Nil(t, result.page.Assets)
}

func TestSpec_BrokenLinks_ExternalLinksAreIgnored(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}
	routes := map[string]roundTripResponder{
		routeID("https", "example.com", "/"): func(req *http.Request) (*http.Response, error) {
			body := `<html><body><a href="/missing">m</a><a href="https://evil.test/broken">e</a></body></html>`
			return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		routeID("https", "example.com", "/missing"): func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
		},
		routeID("https", "evil.test", "/broken"): func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusNotFound, "evil", nil), nil
		},
	}
	client, calls := newTrackedClient(t, routes)

	report, err := analyzeReport(context.Background(), optionsForContract(fixtureBaseURL, 1, 0, client, clock))
	require.NoError(t, err)

	root := findPageByPath(t, report, "/")
	require.NotNil(t, root)
	require.Len(t, root.BrokenLinks, 1)
	require.Equal(t, fixtureBaseURL+"/missing", root.BrokenLinks[0].URL)
	require.Zero(t, calls.countHostPath("evil.test", "/broken"))
}

func TestSpec_BrokenLinks_ProtocolRelativeExternalIgnored(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}
	routes := map[string]roundTripResponder{
		routeID("http", "example.com", "/"): func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusOK, `<html><body><a href="//evil.test/broken">e</a></body></html>`, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		routeID("http", "evil.test", "/broken"): func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusNotFound, "evil", nil), nil
		},
	}
	client, calls := newTrackedClient(t, routes)

	report, err := analyzeReport(context.Background(), optionsForContract("http://example.com", 1, 0, client, clock))
	require.NoError(t, err)

	root := findPageByPath(t, report, "/")
	require.NotNil(t, root)
	require.Empty(t, root.BrokenLinks)
	require.Zero(t, calls.countHostPath("evil.test", "/broken"))
}

func TestSpec_BrokenLinks_DifferentSchemeSameHostIgnored(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}
	routes := map[string]roundTripResponder{
		routeID("http", "example.com", "/"): func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusOK, `<html><body><a href="https://example.com/missing">m</a></body></html>`, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		routeID("https", "example.com", "/missing"): func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
		},
	}
	client, calls := newTrackedClient(t, routes)

	report, err := analyzeReport(context.Background(), optionsForContract("http://example.com", 1, 0, client, clock))
	require.NoError(t, err)

	root := findPageByPath(t, report, "/")
	require.NotNil(t, root)
	require.Empty(t, root.BrokenLinks)
	require.Zero(t, calls.countHostPath("example.com", "/missing"))
}

func TestSpec_BrokenLinks_DifferentPortIgnored(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}
	routes := map[string]roundTripResponder{
		routeID("http", "example.com", "/"): func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusOK, `<html><body><a href="http://example.com:8080/missing">m</a></body></html>`, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		routeID("http", "example.com:8080", "/missing"): func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
		},
	}
	client, calls := newTrackedClient(t, routes)

	report, err := analyzeReport(context.Background(), optionsForContract("http://example.com", 1, 0, client, clock))
	require.NoError(t, err)

	root := findPageByPath(t, report, "/")
	require.NotNil(t, root)
	require.Empty(t, root.BrokenLinks)
	require.Zero(t, calls.countHostPath("example.com", "/missing"))
}

func TestSpec_BrokenLinks_CanonicalizationCases(t *testing.T) {
	t.Parallel()

	t.Run("dot segments", func(t *testing.T) {
		t.Parallel()

		clock := &testClock{now: fixtureTime}
		routes := map[string]roundTripResponder{
			routeID("https", "example.com", "/"): func(req *http.Request) (*http.Response, error) {
				body := `<html><body><a href="/a/../missing">m1</a><a href="/missing">m2</a></body></html>`
				return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
			},
			routeID("https", "example.com", "/missing"): func(req *http.Request) (*http.Response, error) {
				return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
			},
		}
		client, _ := newTrackedClient(t, routes)

		report, err := analyzeReport(context.Background(), optionsForContract(fixtureBaseURL, 1, 0, client, clock))
		require.NoError(t, err)

		root := findPageByPath(t, report, "/")
		require.NotNil(t, root)
		require.Len(t, root.BrokenLinks, 1)
		require.Equal(t, fixtureBaseURL+"/missing", root.BrokenLinks[0].URL)
	})

	t.Run("exact duplicate href dedup", func(t *testing.T) {
		t.Parallel()

		clock := &testClock{now: fixtureTime}
		routes := map[string]roundTripResponder{
			routeID("https", "example.com", "/"): func(req *http.Request) (*http.Response, error) {
				body := `<html><body><a href="/missing">m1</a><a href="/missing">m2</a></body></html>`
				return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
			},
			routeID("https", "example.com", "/missing"): func(req *http.Request) (*http.Response, error) {
				return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
			},
		}
		client, calls := newTrackedClient(t, routes)

		report, err := analyzeReport(context.Background(), optionsForContract(fixtureBaseURL, 1, 0, client, clock))
		require.NoError(t, err)

		root := findPageByPath(t, report, "/")
		require.NotNil(t, root)
		require.Len(t, root.BrokenLinks, 1)
		require.Equal(t, 1, calls.countHostPath("example.com", "/missing"))
	})

	t.Run("trailing slash non-root dedup", func(t *testing.T) {
		t.Parallel()

		clock := &testClock{now: fixtureTime}
		routes := map[string]roundTripResponder{
			routeID("https", "example.com", "/"): func(req *http.Request) (*http.Response, error) {
				body := `<html><body><a href="/missing">m1</a><a href="/missing/">m2</a></body></html>`
				return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
			},
			routeID("https", "example.com", "/missing"): func(req *http.Request) (*http.Response, error) {
				return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
			},
			routeID("https", "example.com", "/missing/"): func(req *http.Request) (*http.Response, error) {
				return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
			},
		}
		client, _ := newTrackedClient(t, routes)

		report, err := analyzeReport(context.Background(), optionsForContract(fixtureBaseURL, 1, 0, client, clock))
		require.NoError(t, err)

		root := findPageByPath(t, report, "/")
		require.NotNil(t, root)
		require.Len(t, root.BrokenLinks, 1)
		require.Equal(t, fixtureBaseURL+"/missing", root.BrokenLinks[0].URL)
	})

	t.Run("fragment ignored", func(t *testing.T) {
		t.Parallel()

		clock := &testClock{now: fixtureTime}
		routes := map[string]roundTripResponder{
			routeID("https", "example.com", "/"): func(req *http.Request) (*http.Response, error) {
				body := `<html><body><a href="/missing#top">m1</a><a href="/missing#bottom">m2</a></body></html>`
				return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
			},
			routeID("https", "example.com", "/missing"): func(req *http.Request) (*http.Response, error) {
				return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
			},
		}
		client, _ := newTrackedClient(t, routes)

		report, err := analyzeReport(context.Background(), optionsForContract(fixtureBaseURL, 1, 0, client, clock))
		require.NoError(t, err)

		root := findPageByPath(t, report, "/")
		require.NotNil(t, root)
		require.Len(t, root.BrokenLinks, 1)
		require.Equal(t, fixtureBaseURL+"/missing", root.BrokenLinks[0].URL)
	})

	t.Run("default port canonicalized", func(t *testing.T) {
		t.Parallel()

		clock := &testClock{now: fixtureTime}
		routes := map[string]roundTripResponder{
			routeID("http", "example.com", "/"): func(req *http.Request) (*http.Response, error) {
				body := `<html><body><a href="http://example.com/missing">m1</a><a href="http://example.com:80/missing">m2</a></body></html>`
				return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
			},
			routeID("http", "example.com", "/missing"): func(req *http.Request) (*http.Response, error) {
				return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
			},
			routeID("http", "example.com:80", "/missing"): func(req *http.Request) (*http.Response, error) {
				return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
			},
		}
		client, _ := newTrackedClient(t, routes)

		report, err := analyzeReport(context.Background(), optionsForContract("http://example.com", 1, 0, client, clock))
		require.NoError(t, err)

		root := findPageByPath(t, report, "/")
		require.NotNil(t, root)
		require.Len(t, root.BrokenLinks, 1)
		require.Equal(t, "http://example.com/missing", root.BrokenLinks[0].URL)
	})
}

func TestSpec_BrokenLinks_QueryContract(t *testing.T) {
	t.Parallel()

	t.Run("empty query equivalent", func(t *testing.T) {
		t.Parallel()

		clock := &testClock{now: fixtureTime}
		routes := map[string]roundTripResponder{
			routeID("https", "example.com", "/"): func(req *http.Request) (*http.Response, error) {
				body := `<html><body><a href="/missing">m1</a><a href="/missing?">m2</a></body></html>`
				return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
			},
			routeID("https", "example.com", "/missing"): func(req *http.Request) (*http.Response, error) {
				return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
			},
		}
		client, _ := newTrackedClient(t, routes)

		report, err := analyzeReport(context.Background(), optionsForContract(fixtureBaseURL, 1, 0, client, clock))
		require.NoError(t, err)

		root := findPageByPath(t, report, "/")
		require.NotNil(t, root)
		require.Len(t, root.BrokenLinks, 1)
	})

	t.Run("query ordering is distinct", func(t *testing.T) {
		t.Parallel()

		clock := &testClock{now: fixtureTime}
		routes := map[string]roundTripResponder{
			routeID("https", "example.com", "/"): func(req *http.Request) (*http.Response, error) {
				body := `<html><body><a href="/missing?a=1&b=2">m1</a><a href="/missing?b=2&a=1">m2</a></body></html>`
				return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
			},
			routeID("https", "example.com", "/missing"): func(req *http.Request) (*http.Response, error) {
				return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
			},
		}
		client, _ := newTrackedClient(t, routes)

		report, err := analyzeReport(context.Background(), optionsForContract(fixtureBaseURL, 1, 0, client, clock))
		require.NoError(t, err)

		root := findPageByPath(t, report, "/")
		require.NotNil(t, root)
		require.Len(t, root.BrokenLinks, 2)
	})
}

func TestSpec_BrokenLinks_RetriesLastAttemptWins(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}
	var flakyCalls int

	routes := map[string]roundTripResponder{
		routeID("https", "example.com", "/"): func(req *http.Request) (*http.Response, error) {
			body := `<html><body><a href="/flaky">f</a></body></html>`
			return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		routeID("https", "example.com", "/flaky"): func(req *http.Request) (*http.Response, error) {
			flakyCalls++
			if flakyCalls == 1 {
				return responseForRequest(req, http.StatusInternalServerError, "boom", nil), nil
			}

			return responseForRequest(req, http.StatusNotFound, "flaky", nil), nil
		},
	}
	client, _ := newTrackedClient(t, routes)

	report, err := analyzeReport(context.Background(), optionsForContract(fixtureBaseURL, 1, 1, client, clock))
	require.NoError(t, err)

	root := findPageByPath(t, report, "/")
	require.NotNil(t, root)
	require.Len(t, root.BrokenLinks, 1)
	require.Equal(t, 404, root.BrokenLinks[0].StatusCode)
	require.Equal(t, "Not Found", root.BrokenLinks[0].Error)
	require.Equal(t, 2, flakyCalls)
}

func TestSpec_BrokenLinks_AssetsDoNotPolluteBrokenLinks(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}
	routes := map[string]roundTripResponder{
		routeID("https", "example.com", "/"): func(req *http.Request) (*http.Response, error) {
			body := `<html><body><img src="/missing.png"></body></html>`
			return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		routeID("https", "example.com", "/missing.png"): func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusNotFound, "missing image", nil), nil
		},
	}
	client, calls := newTrackedClient(t, routes)

	report, err := analyzeReport(context.Background(), optionsForContract(fixtureBaseURL, 1, 0, client, clock))
	require.NoError(t, err)

	root := findPageByPath(t, report, "/")
	require.NotNil(t, root)
	require.Empty(t, root.BrokenLinks)
	require.Len(t, root.Assets, 1)
	require.Equal(t, fixtureBaseURL+"/missing.png", root.Assets[0].URL)
	require.Equal(t, 404, root.Assets[0].StatusCode)
	require.Equal(t, 1, calls.countHostPath("example.com", "/missing.png"))
}

func TestSpec_BrokenLinks_IntegratedDepthAndScopeContract(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}
	routes := map[string]roundTripResponder{
		routeID("https", "example.com", "/"): func(req *http.Request) (*http.Response, error) {
			body := `<html><body><a href="/missing">m</a><a href="/child">c</a></body></html>`
			return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		routeID("https", "example.com", "/missing"): func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusNotFound, "missing", nil), nil
		},
		routeID("https", "example.com", "/child"): func(req *http.Request) (*http.Response, error) {
			body := `<html><body><a href="/missing2">m2</a><a href="https://evil.test/broken">e</a></body></html>`
			return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		routeID("https", "example.com", "/missing2"): func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusNotFound, "missing2", nil), nil
		},
		routeID("https", "evil.test", "/broken"): func(req *http.Request) (*http.Response, error) {
			return responseForRequest(req, http.StatusNotFound, "evil", nil), nil
		},
	}
	client, calls := newTrackedClient(t, routes)

	report, err := analyzeReport(context.Background(), optionsForContract(fixtureBaseURL, 1, 0, client, clock))
	require.NoError(t, err)

	root := findPageByPath(t, report, "/")
	require.NotNil(t, root)
	require.Len(t, root.BrokenLinks, 1)
	require.Equal(t, fixtureBaseURL+"/missing", root.BrokenLinks[0].URL)

	child := findPageByPath(t, report, "/child")
	require.NotNil(t, child)
	require.Empty(t, child.BrokenLinks)

	require.Zero(t, calls.countHostPath("example.com", "/missing2"))
	require.Zero(t, calls.countHostPath("evil.test", "/broken"))
	require.Equal(t, 1, totalBrokenLinks(report))
}
