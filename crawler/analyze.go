package crawler

import (
	"context"
	"encoding/json"
)

// Analyze crawls a site and returns a JSON report as bytes.
// IndentJSON affects formatting only, and the output always ends with a newline.
func Analyze(ctx context.Context, opts Options) ([]byte, error) {
	report, err := analyzeReport(ctx, opts)

	return marshalReport(report, opts.IndentJSON), err
}

func marshalReport(report Report, indent bool) []byte {
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
