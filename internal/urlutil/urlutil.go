package urlutil

import (
	"net/url"
	"strings"
)

// Resolve resolves href against base and returns an absolute HTTP(S) URL.
func Resolve(base *url.URL, href string) (string, bool) {
	trimmed := strings.TrimSpace(href)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}

	if !isSupportedScheme(parsed.Scheme) {
		return "", false
	}

	resolved := resolveReference(base, parsed)
	if !isSupportedScheme(resolved.Scheme) {
		return "", false
	}

	canonicalizeRootPath(resolved)
	resolved.Fragment = ""

	return resolved.String(), true
}

func isSupportedScheme(scheme string) bool {
	return scheme == "" || scheme == "http" || scheme == "https"
}

func resolveReference(base *url.URL, parsed *url.URL) *url.URL {
	if parsed.Scheme == "" {
		return base.ResolveReference(parsed)
	}

	return parsed
}

func canonicalizeRootPath(u *url.URL) {
	if u.Path == "/" {
		u.Path = ""
		u.RawPath = ""
	}
}

// SameOrigin reports whether the URL has the same scheme and host (including port) as base.
func SameOrigin(base *url.URL, raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}

	return parsed.Scheme == base.Scheme && parsed.Host == base.Host
}
