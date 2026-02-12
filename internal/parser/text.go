package parser

import (
	"html"
	"strings"
	"unicode"
)

func cleanHumanText(value string) string {
	unescaped := html.UnescapeString(value)
	collapsed := collapseSpaces(unescaped)
	
	return strings.TrimSpace(collapsed)
}

func collapseSpaces(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))

	previousSpace := false
	for _, r := range value {
		if unicode.IsSpace(r) {
			if previousSpace {
				continue
			}
			
			builder.WriteRune(' ')
			previousSpace = true
			
			continue
		}
		
		builder.WriteRune(r)
		previousSpace = false
	}

	return builder.String()
}
