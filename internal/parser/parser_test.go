package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseHTML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		htmlFixture string
		wantFixture string
		wantError   bool
	}{
		{
			name:        "extracts seo links and assets",
			htmlFixture: "parse_full.html",
			wantFixture: "parse_full_expected.json",
		},
		{
			name:        "missing seo fields",
			htmlFixture: "parse_missing_seo.html",
			wantFixture: "parse_missing_seo_expected.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			htmlData := readParserFixture(t, tt.htmlFixture)
			want := readParseResultFixture(t, tt.wantFixture)

			got, err := ParseHTML(htmlData)
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !equalParseResult(got, want) {
				t.Fatalf("unexpected parse result:\n got:  %#v\n want: %#v", got, want)
			}
		})
	}
}

func readParserFixture(t *testing.T, filename string) []byte {
	t.Helper()

	path := filepath.Join("..", "..", "testdata", "parser", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %q: %v", path, err)
	}

	return data
}

func readParseResultFixture(t *testing.T, filename string) ParseResult {
	t.Helper()

	data := readParserFixture(t, filename)
	result := ParseResult{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal fixture %q: %v", filename, err)
	}

	return result
}

func equalParseResult(got, want ParseResult) bool {
	if !equalStrings(got.Links, want.Links) {
		return false
	}

	if got.SEO != want.SEO {
		return false
	}

	if len(got.Assets) != len(want.Assets) {
		return false
	}

	for i := range got.Assets {
		if got.Assets[i] != want.Assets[i] {
			return false
		}
	}

	return true
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}

	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}
