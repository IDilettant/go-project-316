package urlutil

import (
	"net/url"
	"testing"
)

func TestResolve(t *testing.T) {
	t.Parallel()

	base, err := url.Parse("https://example.com/base/path")
	if err != nil {
		t.Fatalf("parse base url: %v", err)
	}

	tests := []struct {
		name     string
		href     string
		wantURL  string
		wantOkay bool
	}{
		{name: "empty href", href: "", wantURL: "", wantOkay: false},
		{name: "fragment only", href: "#section", wantURL: "", wantOkay: false},
		{name: "invalid url", href: "http://[::1", wantURL: "", wantOkay: false},
		{name: "unsupported scheme", href: "mailto:test@example.com", wantURL: "", wantOkay: false},
		{name: "relative path", href: " /docs?a=1#frag ", wantURL: "https://example.com/docs?a=1", wantOkay: true},
		{name: "absolute https", href: "https://golang.org/doc#top", wantURL: "https://golang.org/doc", wantOkay: true},
		{name: "protocol relative", href: "//cdn.example.com/app.js", wantURL: "https://cdn.example.com/app.js", wantOkay: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotURL, gotOkay := Resolve(base, tt.href)
			if gotOkay != tt.wantOkay {
				t.Fatalf("unexpected ok flag: got %v want %v", gotOkay, tt.wantOkay)
			}
			
			if gotURL != tt.wantURL {
				t.Fatalf("unexpected resolved url: got %q want %q", gotURL, tt.wantURL)
			}
		})
	}
}

func TestSameOrigin(t *testing.T) {
	t.Parallel()

	base, err := url.Parse("https://example.com:8443/root")
	if err != nil {
		t.Fatalf("parse base url: %v", err)
	}

	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "same scheme host and port", raw: "https://example.com:8443/a", want: true},
		{name: "different scheme", raw: "http://example.com:8443/a", want: false},
		{name: "different host", raw: "https://other.com:8443/a", want: false},
		{name: "different port", raw: "https://example.com/a", want: false},
		{name: "invalid url", raw: "http://[::1", want: false},
		{name: "relative url", raw: "/local", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := SameOrigin(base, tt.raw)
			if got != tt.want {
				t.Fatalf("unexpected same-origin result: got %v want %v", got, tt.want)
			}
		})
	}
}
