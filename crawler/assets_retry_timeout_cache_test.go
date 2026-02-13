package crawler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSpec_Assets_CacheByFullURL_NoDuplicateFetch(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}

	var assetCalls int
	client := newFixtureClientWithRoutes(t, map[string]roundTripResponder{
		"/": func(req *http.Request) (*http.Response, error) {
			body := `
				<html><head>
					<link rel="stylesheet" href="/static/app.css">
					<link rel="stylesheet" href="/static/app.css">
				</head><body>
					<img src="/static/logo.png"/>
					<img src="/static/logo.png"/>
				</body></html>
			`

			return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		"/static/app.css": func(req *http.Request) (*http.Response, error) {
			assetCalls++
			b := []byte("body{}")
			h := http.Header{}
			h.Set("Content-Length", strconv.Itoa(len(b)))
			h.Set("Content-Type", "text/css")

			return responseWithBody(http.StatusOK, b, h), nil
		},
		"/static/logo.png": func(req *http.Request) (*http.Response, error) {
			assetCalls++
			b := []byte("pngdata")
			h := http.Header{}
			h.Set("Content-Type", "image/png")

			return responseWithBody(http.StatusOK, b, h), nil
		},
	})

	opts := Options{
		URL:         fixtureBaseURL,
		Depth:       0,
		Concurrency: 1,
		Retries:     0,
		Timeout:     time.Second,
		UserAgent:   "test-agent",
		HTTPClient:  client,
		Clock:       clock,
	}

	report, err := analyzeReport(context.Background(), opts)
	require.NoError(t, err)

	require.Len(t, report.Pages, 1)
	assets := report.Pages[0].Assets

	require.Len(t, assets, 2)
	require.Equal(t, 2, assetCalls)
}

func TestSpec_Assets_CacheByURL_RespectsTypePerPage(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}

	var assetCalls int
	client := newFixtureClientWithRoutes(t, map[string]roundTripResponder{
		"/": func(req *http.Request) (*http.Response, error) {
			body := `
				<html><body>
					<a href="/a">A</a>
					<img src="/static/shared.bin"/>
				</body></html>
			`

			return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		"/a": func(req *http.Request) (*http.Response, error) {
			body := `<html><body><script src="/static/shared.bin"></script></body></html>`

			return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		"/static/shared.bin": func(req *http.Request) (*http.Response, error) {
			assetCalls++
			b := []byte("bin")
			h := http.Header{}
			h.Set("Content-Length", strconv.Itoa(len(b)))

			return responseWithBody(http.StatusOK, b, h), nil
		},
	})

	opts := Options{
		URL:         fixtureBaseURL,
		Depth:       1,
		Concurrency: 1,
		Retries:     0,
		Timeout:     time.Second,
		UserAgent:   "test-agent",
		HTTPClient:  client,
		Clock:       clock,
	}

	report, err := analyzeReport(context.Background(), opts)
	require.NoError(t, err)

	require.Len(t, report.Pages, 2)
	require.Equal(t, 1, assetCalls)

	var rootAssets []Asset
	var pageAssets []Asset

	for _, page := range report.Pages {
		switch page.URL {
		case fixtureBaseURL:
			rootAssets = page.Assets
		case fixtureBaseURL + "/a":
			pageAssets = page.Assets
		}
	}

	require.Len(t, rootAssets, 1)
	require.Equal(t, "image", rootAssets[0].Type)
	require.Len(t, pageAssets, 1)
	require.Equal(t, "script", pageAssets[0].Type)
}

func TestSpec_Assets_RetriesApply_500Then200_OK(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}
	var calls int

	client := newFixtureClientWithRoutes(t, map[string]roundTripResponder{
		"/": func(req *http.Request) (*http.Response, error) {
			body := `<html><body><script src="/static/app.js"></script></body></html>`

			return responseForRequest(req, http.StatusOK, body, http.Header{"Content-Type": []string{"text/html"}}), nil
		},
		"/static/app.js": func(req *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return responseWithBody(http.StatusInternalServerError, []byte("boom"), nil), nil
			}

			return responseWithBody(http.StatusOK, []byte("ok"), http.Header{"Content-Length": []string{"2"}}), nil
		},
	})

	opts := Options{
		URL:         fixtureBaseURL,
		Depth:       0,
		Concurrency: 1,
		Retries:     1,
		Timeout:     time.Second,
		UserAgent:   "test-agent",
		HTTPClient:  client,
		Clock:       clock,
	}

	report, err := analyzeReport(context.Background(), opts)
	require.NoError(t, err)

	assets := report.Pages[0].Assets
	require.Len(t, assets, 1)

	a := assets[0]
	require.Equal(t, fixtureBaseURL+"/static/app.js", a.URL)
	require.Equal(t, "script", a.Type)
	require.Equal(t, 200, a.StatusCode)
	require.Equal(t, int64(2), a.SizeBytes)
	require.Empty(t, a.Error)

	require.Equal(t, 2, calls, "must call exactly retries+1 when first attempt fails")
}

func TestSpec_Assets_TimeoutApplied_StatusCodeZero_WithError(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "" || req.URL.Path == "/" {
				return responseForRequest(req, http.StatusOK, `<html><body><img src="/static/slow.png"/></body></html>`, http.Header{"Content-Type": []string{"text/html"}}), nil
			}

			return nil, errors.New("context deadline exceeded")
		}),
	}

	opts := Options{
		URL:         fixtureBaseURL,
		Depth:       0,
		Concurrency: 1,
		Retries:     0,
		Timeout:     1 * time.Millisecond,
		UserAgent:   "test-agent",
		HTTPClient:  client,
		Clock:       clock,
	}

	report, err := analyzeReport(context.Background(), opts)
	require.NoError(t, err, "asset failure must be captured in report, not returned")

	require.Len(t, report.Pages, 1)
	assets := report.Pages[0].Assets
	require.Len(t, assets, 1)

	a := assets[0]
	require.Equal(t, fixtureBaseURL+"/static/slow.png", a.URL)
	require.Equal(t, "image", a.Type)
	require.Equal(t, 0, a.StatusCode)
	require.NotEmpty(t, a.Error)
}
