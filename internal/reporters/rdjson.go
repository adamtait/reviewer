// SPDX-License-Identifier: MIT

package reporters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/adamtait/reviewer/pkg/finding"
)

// RDJSON writes Reviewdog Diagnostic JSON, which every reviewdog reporter and a
// number of CI annotators already consume. It exists partly for that and partly
// as proof that the Reporter interface fits a machine-readable format as well as
// a human one — a second implementation is the cheapest test of an interface.
type RDJSON struct {
	Out io.Writer
}

func (r RDJSON) Name() string { return "rdjson" }

// rdDiagnosticResult is the top-level document.
type rdDiagnosticResult struct {
	Source      rdSource       `json:"source"`
	Severity    string         `json:"severity,omitempty"`
	Diagnostics []rdDiagnostic `json:"diagnostics"`
}

// rdSource names the tool. Reviewdog also accepts a `url` here; this omits it,
// because a URL literal in the core is exactly what the seam test forbids
// (ADR-0004) and a project homepage is not worth routing through config.
type rdSource struct {
	Name string `json:"name"`
}

type rdDiagnostic struct {
	Message        string         `json:"message"`
	Location       rdLocation     `json:"location"`
	Severity       string         `json:"severity"`
	Code           rdCode         `json:"code"`
	Suggestions    []rdSuggestion `json:"suggestions,omitempty"`
	OriginalOutput string         `json:"original_output,omitempty"`
}

type rdLocation struct {
	Path  string  `json:"path"`
	Range rdRange `json:"range"`
}

type rdRange struct {
	Start rdPosition `json:"start"`
	End   rdPosition `json:"end"`
}

// rdPosition omits column: analyzers here report line spans, and inventing
// column 1 would imply a precision the findings do not have.
type rdPosition struct {
	Line int `json:"line"`
}

type rdCode struct {
	Value string `json:"value"`
}

type rdSuggestion struct {
	Range rdRange `json:"range"`
	Text  string  `json:"text"`
}

func (r RDJSON) Report(_ context.Context, run Run) error {
	out := r.Out
	if out == nil {
		out = io.Discard
	}

	doc := rdDiagnosticResult{
		Source:      rdSource{Name: "reviewer"},
		Diagnostics: make([]rdDiagnostic, 0, len(run.Findings)),
	}
	for _, f := range run.Findings {
		start, end := f.Span()
		rng := rdRange{Start: rdPosition{Line: start}, End: rdPosition{Line: end}}

		d := rdDiagnostic{
			Message:  f.Message,
			Location: rdLocation{Path: f.File, Range: rng},
			Severity: rdSeverity(f.Severity),
			Code:     rdCode{Value: f.RuleID},
		}
		// Confidence and lane have no home in the reviewdog schema, and they are
		// the two things a reader most needs for a model-lane finding. Carrying
		// them in original_output keeps them visible rather than dropping them.
		if f.Lane == finding.LaneLLM || f.Confidence != finding.ConfidenceHigh {
			d.OriginalOutput = fmt.Sprintf("lane=%s confidence=%s", f.Lane, f.Confidence)
			if f.Evidence != "" {
				d.OriginalOutput += "\nevidence: " + f.Evidence
			}
		}
		if f.Suggestion != "" {
			d.Suggestions = []rdSuggestion{{Range: rng, Text: f.Suggestion}}
		}
		doc.Diagnostics = append(doc.Diagnostics, d)
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}

func rdSeverity(s finding.Severity) string {
	switch s {
	case finding.SeverityError:
		return "ERROR"
	case finding.SeverityWarning:
		return "WARNING"
	default:
		return "INFO"
	}
}
