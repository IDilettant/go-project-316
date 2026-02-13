package crawler

import (
	"context"
	"encoding/json"
	"sort"
)

// Analyze crawls a site and returns a JSON report as bytes.
// IndentJSON affects formatting only, and the output always ends with a newline.
func Analyze(ctx context.Context, opts Options) ([]byte, error) {
	report, err := analyzeReport(ctx, opts)

	return marshalReport(report, opts.IndentJSON), err
}

func marshalReport(report Report, indent bool) []byte {
	sortPages(report.Pages)

	var (
		data []byte
		err  error
	)

	if indent {
		data, err = json.MarshalIndent(report, "", "  ")
	} else {
		data, err = json.Marshal(report)
	}

	if err != nil {
		data = []byte(`{"error":"failed to marshal report"}`)
	}

	return ensureNewline(data)
}

func ensureNewline(data []byte) []byte {
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return append(data, '\n')
	}

	return data
}

func sortPages(pages []Page) {
	sort.SliceStable(pages, func(i, j int) bool {
		if pages[i].Depth != pages[j].Depth {
			return pages[i].Depth < pages[j].Depth
		}

		return pages[i].URL < pages[j].URL
	})
}
