package crawler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"code/crawler"
)

func TestSpec_Golden_JSON_ExactMatch_Order_Keys_Required(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}
	client := newFixtureClient(t)

	opts := crawler.Options{
		URL:         fixtureBaseURL,
		Depth:       1,
		Concurrency: 1,
		Retries:     0,
		Timeout:     time.Second,
		UserAgent:   "test-agent",
		IndentJSON:  true,
		HTTPClient:  client,
		Clock:       clock,
	}

	got, err := crawler.Analyze(context.Background(), opts)
	require.NoError(t, err)

	want := readFixture(t, "golden", "report.json")
	require.Equal(t, string(want), string(got), "JSON must match golden exactly (including key order and trailing newline)")
}

func TestSpec_IndentJSON_ChangesOnlyFormatting_NotContent(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}
	client := newFixtureClient(t)

	optsBase := crawler.Options{
		URL:         fixtureBaseURL,
		Depth:       1,
		Concurrency: 1,
		Retries:     0,
		Timeout:     time.Second,
		UserAgent:   "test-agent",
		HTTPClient:  client,
		Clock:       clock,
	}

	compact, err := crawler.Analyze(context.Background(), withIndent(optsBase, false))
	require.NoError(t, err)

	pretty, err := crawler.Analyze(context.Background(), withIndent(optsBase, true))
	require.NoError(t, err)

	var a, b any
	require.NoError(t, json.Unmarshal(compact, &a))
	require.NoError(t, json.Unmarshal(pretty, &b))
	require.Equal(t, a, b, "IndentJSON must not change JSON content")
	require.NotEqual(t, string(compact), string(pretty), "IndentJSON must change formatting")
}

func TestAnalyze_ClockNil_NoPanic_GeneratedAtRFC3339UTC(t *testing.T) {
	t.Parallel()

	client := newFixtureClient(t)
	opts := crawler.Options{
		URL:         fixtureBaseURL,
		Depth:       1,
		Concurrency: 1,
		Retries:     0,
		Timeout:     time.Second,
		HTTPClient:  client,
	}

	var data []byte
	var err error
	require.NotPanics(t, func() {
		data, err = crawler.Analyze(context.Background(), opts)
	})
	require.NoError(t, err)

	var report struct {
		GeneratedAt string `json:"generated_at"`
	}

	require.NoError(t, json.Unmarshal(data, &report))
	require.NotEmpty(t, report.GeneratedAt)
	require.True(t, strings.HasSuffix(report.GeneratedAt, "Z"))

	parsedTime, parseErr := time.Parse(time.RFC3339, report.GeneratedAt)
	require.NoError(t, parseErr)
	require.Equal(t, time.UTC, parsedTime.Location())
}

func TestAnalyze_SuccessPage_OmitsEmptyErrorField(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}
	client := newFixtureClient(t)
	opts := crawler.Options{
		URL:         fixtureBaseURL,
		Depth:       1,
		Concurrency: 1,
		Retries:     0,
		Timeout:     time.Second,
		HTTPClient:  client,
		Clock:       clock,
	}

	data, err := crawler.Analyze(context.Background(), opts)
	require.NoError(t, err)

	var report map[string]any
	require.NoError(t, json.Unmarshal(data, &report))

	pages, ok := report["pages"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, pages)

	page, ok := pages[0].(map[string]any)
	require.True(t, ok)

	_, hasError := page["error"]
	require.False(t, hasError, "successful page must omit empty error field")

	assets, ok := page["assets"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, assets)

	asset, ok := assets[0].(map[string]any)
	require.True(t, ok)

	_, hasAssetError := asset["error"]
	require.False(t, hasAssetError, "asset with empty error must omit error field")
}

func TestAnalyze_PagesAreSortedByDepthThenURL(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			if path == "" {
				path = "/"
			}

			switch path {
			case "/":
				return responseWithBody(http.StatusOK, []byte(`<html><body><a href="/b"></a><a href="/a"></a></body></html>`), http.Header{
					"Content-Type": []string{"text/html"},
				}), nil
			case "/a", "/b":
				return responseWithBody(http.StatusOK, []byte(`<html><body>ok</body></html>`), http.Header{
					"Content-Type": []string{"text/html"},
				}), nil
			default:
				return responseWithBody(http.StatusNotFound, []byte("not found"), http.Header{}), nil
			}
		}),
	}

	opts := crawler.Options{
		URL:         fixtureBaseURL,
		Depth:       1,
		Concurrency: 1,
		Retries:     0,
		Timeout:     time.Second,
		HTTPClient:  client,
		Clock:       clock,
	}

	data, err := crawler.Analyze(context.Background(), opts)
	require.NoError(t, err)

	var report crawler.Report
	require.NoError(t, json.Unmarshal(data, &report))
	require.Len(t, report.Pages, 3)
	require.Equal(t, fixtureBaseURL, report.Pages[0].URL)
	require.Equal(t, fixtureBaseURL+"/a", report.Pages[1].URL)
	require.Equal(t, fixtureBaseURL+"/b", report.Pages[2].URL)
}

func withIndent(opts crawler.Options, indent bool) crawler.Options {
	opts.IndentJSON = indent

	return opts
}
