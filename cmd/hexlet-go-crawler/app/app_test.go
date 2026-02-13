package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"code/crawler"
	"code/internal/limiter"

	"github.com/stretchr/testify/require"
)

const cliFixtureBaseURL = "https://example.com"

func TestCLI_PrintsJSONOnly(t *testing.T) {
	client := newFixtureClient(t)
	clock := fixedClock{now: fixtureTime()}
	args := []string{
		"hexlet-go-crawler",
		"--depth=1",
		"--workers=1",
		"--retries=0",
		"--timeout=1s",
		cliFixtureBaseURL,
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(args, &stdout, &stderr, client, clock)
	require.NoError(t, err)

	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}

	output := stdout.Bytes()
	if !bytes.HasSuffix(output, []byte("\n")) {
		t.Fatalf("expected trailing newline")
	}
	trimmed := bytes.TrimSuffix(output, []byte("\n"))
	if !json.Valid(trimmed) {
		t.Fatalf("stdout is not valid JSON")
	}

	expected := buildExpectedCLIReport(t, client, clock)
	if !bytes.Equal(output, expected) {
		t.Fatalf("cli output does not match library output")
	}
}

func TestCLI_PrintsJSONWhenAnalyzeReturnsError(t *testing.T) {
	t.Parallel()

	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("dial error")
		}),
	}
	clock := fixedClock{now: fixtureTime()}
	args := []string{
		"hexlet-go-crawler",
		"--depth=1",
		"--workers=1",
		"--retries=0",
		"--timeout=1s",
		cliFixtureBaseURL,
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(args, &stdout, &stderr, client, clock)
	require.NoError(t, err)

	output := stdout.Bytes()
	require.NotEmpty(t, output)
	require.True(t, bytes.HasSuffix(output, []byte("\n")))
	require.True(t, json.Valid(bytes.TrimSuffix(output, []byte("\n"))))
}

func buildExpectedCLIReport(t *testing.T, client *http.Client, clock limiter.Timer) []byte {
	t.Helper()

	opts := crawler.Options{
		URL:         cliFixtureBaseURL,
		Depth:       1,
		IndentJSON:  true,
		Concurrency: 1,
		Retries:     0,
		Timeout:     time.Second,
		HTTPClient:  client,
		Clock:       clock,
	}

	data, err := crawler.Analyze(context.Background(), opts)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	return data
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

func (c fixedClock) Sleep(ctx context.Context, duration time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func fixtureTime() time.Time {
	return time.Date(2024, time.June, 1, 12, 34, 56, 0, time.UTC)
}

func readFixture(t *testing.T, parts ...string) []byte {
	t.Helper()

	pathParts := append([]string{"..", "..", "..", "testdata"}, parts...)
	path := filepath.Join(pathParts...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %q: %v", path, err)
	}

	return data
}

func newFixtureClient(t *testing.T) *http.Client {
	t.Helper()

	rootHTML := readFixture(t, "pages", "root.html")
	logo := readFixture(t, "assets", "logo.bin")

	return &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			host := req.URL.Hostname()
			if host == "" {
				host = req.URL.Host
			}

			if !strings.EqualFold(host, "example.com") {
				return nil, fmt.Errorf("unexpected host %q", host)
			}

			path := req.URL.Path
			if path == "" {
				path = "/"
			}

			switch path {
			case "/":
				return responseWithBody(http.StatusOK, rootHTML, http.Header{
					"Content-Type": []string{"text/html"},
				}), nil
			case "/missing":
				return responseWithBody(http.StatusNotFound, []byte("missing"), http.Header{}), nil
			case "/static/logo.png":
				header := http.Header{}
				header.Set("Content-Length", strconv.Itoa(len(logo)))
				header.Set("Content-Type", "image/png")
				return responseWithBody(http.StatusOK, logo, header), nil
			default:
				return responseWithBody(http.StatusNotFound, []byte("not found"), http.Header{}), nil
			}
		}),
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func responseWithBody(status int, body []byte, header http.Header) *http.Response {
	if header == nil {
		header = http.Header{}
	}

	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}
