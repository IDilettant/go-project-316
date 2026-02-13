package crawler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSpec_HTTPClientRequired_ReturnsError(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: fixtureTime}

	opts := Options{
		URL:         fixtureBaseURL,
		Depth:       0,
		Concurrency: 1,
		Retries:     0,
		Timeout:     time.Second,
		UserAgent:   "test-agent",
		HTTPClient:  nil,
		Clock:       clock,
	}

	report, err := analyzeReport(context.Background(), opts)
	require.Error(t, err)

	require.Equal(t, fixtureBaseURL, report.RootURL)
	require.Len(t, report.Pages, 1)

	page := report.Pages[0]
	require.Equal(t, fixtureBaseURL, page.URL)
	require.Equal(t, 0, page.HTTPStatus)
	require.Equal(t, "error", page.Status)
	require.NotEmpty(t, page.Error)
}
