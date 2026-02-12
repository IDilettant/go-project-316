package urlutil

import (
	"net/url"
	"strings"
)

// Resolve resolves href against base and returns an absolute HTTP(S) URL.
func Resolve(base *url.URL, href string) (string, bool) {
	trimmed := strings.TrimSpace(href)
	if trimmed == "" {
		return "", false
	}

	if strings.HasPrefix(trimmed, "#") {
		return "", false
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}

	if parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}

	resolved := parsed
	if parsed.Scheme == "" {
		resolved = base.ResolveReference(parsed)
	}

	resolved.Fragment = ""
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return "", false
	}

	return resolved.String(), true
}

// SameOrigin reports whether the URL has the same scheme and host (including port) as base.
func SameOrigin(base *url.URL, raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	
	return parsed.Scheme == base.Scheme && parsed.Host == base.Host
}
