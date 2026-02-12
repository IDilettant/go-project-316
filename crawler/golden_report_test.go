package crawler_test

import (
	"context"
	"encoding/json"
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
		URL:        fixtureBaseURL,
		Depth:      1,
		Workers:    1,
		Retries:    0,
		Timeout:    time.Second,
		UserAgent:  "test-agent",
		IndentJSON: true,
		HTTPClient: client,
		Clock:      clock,
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
		URL:        fixtureBaseURL,
		Depth:      1,
		Workers:    1,
		Retries:    0,
		Timeout:    time.Second,
		UserAgent:  "test-agent",
		HTTPClient: client,
		Clock:      clock,
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

func withIndent(opts crawler.Options, indent bool) crawler.Options {
	opts.IndentJSON = indent
	
	return opts
}
