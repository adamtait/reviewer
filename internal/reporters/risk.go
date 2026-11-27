// SPDX-License-Identifier: MIT

package reporters

import (
	"fmt"
	"strings"

	"github.com/adamtait/reviewer/internal/risk"
)

// RiskMarker identifies the one comment `reviewer risk` keeps on a pull
// request, so the next run updates it rather than adding another. It is the
// marker `reviewer outcomes` counts records by.
const RiskMarker = "<!-- rv:risk -->"

// riskLabelsOpen and riskLabelsClose bracket the hidden list of the labels
// this tool set, which is the comment's last line.
const (
	riskLabelsOpen  = "<!-- rv:risk-labels "
	riskLabelsClose = " -->"
)

// NoLabelSentence is said on every record, labels or not. A reader who sees no
// label will otherwise take it to mean "safe", and nothing here can say that
// (G2 in docs/designs/risk-gated-merge.md, ADR-0035).
const NoLabelSentence = "No label means no rule matched. It does not mean the change is safe."

// maxReasons is how many reasons one label lists before the rest are counted.
// A change to 300 files under .github/ is one thought, not 300 lines.
const maxReasons = 10

// RiskComment is what the risk record's comment says.
type RiskComment struct {
	// HeadSHA is the commit the record describes. Empty when the pull request
	// could not be read, and the comment then says so.
	HeadSHA string
	// RunURL links the workflow run that wrote the record. Empty outside
	// GitHub Actions.
	RunURL string
	// Record is the labels with their reasons, and every warning: the signals
	// that could not be computed and the calls that failed.
	Record risk.Record
	// Owned is the hidden list: the labels this tool set and keeps. It can
	// differ from Record.Labels: a label a person had already set is not this
	// tool's, and a label kept because its signal could not be computed this
	// time still is.
	Owned []risk.Label
}

// RenderRisk renders the risk record's comment.
//
// Everything a pull request controls (paths, symbols, error text quoting
// them) is escaped and kept to one line, and the hidden list is always the
// last line, which is the only place ParseRiskLabels reads it from. A file
// named to look like the hidden list therefore cannot be read as one.
func RenderRisk(c RiskComment) string {
	var b strings.Builder
	b.WriteString(RiskMarker)
	b.WriteString("\n")

	b.WriteString("**Risk record**")
	if c.HeadSHA != "" {
		fmt.Fprintf(&b, " for `%s`", escapeCode(c.HeadSHA))
	} else {
		b.WriteString(" for an unknown head commit: the pull request could not be read")
	}
	if c.RunURL != "" {
		fmt.Fprintf(&b, " · [run](%s)", c.RunURL)
	}
	b.WriteString("\n\n")
	b.WriteString("Advisory: this says where to look first. It does not decide merging, and nothing in it approves or blocks this pull request.\n\n")

	if len(c.Record.Labels) == 0 {
		b.WriteString("No label applies.\n\n")
	}
	for _, label := range c.Record.Labels {
		reasons := c.Record.Reasons[label]
		shown := reasons
		if len(shown) > maxReasons {
			shown = shown[:maxReasons]
		}
		escaped := make([]string, 0, len(shown))
		for _, r := range shown {
			escaped = append(escaped, escapeText(r))
		}
		fmt.Fprintf(&b, "- `%s`: %s", label, strings.Join(escaped, "; "))
		if more := len(reasons) - len(shown); more > 0 {
			fmt.Fprintf(&b, "; and %d more", more)
		}
		b.WriteString("\n")
	}
	if len(c.Record.Labels) > 0 {
		b.WriteString("\n")
	}
	b.WriteString(NoLabelSentence)
	b.WriteString("\n")

	if len(c.Record.Warnings) > 0 {
		b.WriteString("\n**Not everything could be checked or done on this run**, so a label may be missing:\n\n")
		for _, w := range c.Record.Warnings {
			fmt.Fprintf(&b, "- %s\n", escapeText(w))
		}
	}

	names := make([]string, 0, len(c.Owned))
	for _, l := range c.Owned {
		names = append(names, string(l))
	}
	fmt.Fprintf(&b, "\n%s%s%s\n", riskLabelsOpen, strings.Join(names, ","), riskLabelsClose)
	return b.String()
}

// ParseRiskLabels reads the hidden list from a risk comment's last line. It
// returns only names in the closed label set, and false when the last line is
// not a list at all. Whose comment it is, the caller decides.
func ParseRiskLabels(body string) ([]risk.Label, bool) {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if !strings.HasPrefix(last, riskLabelsOpen) || !strings.HasSuffix(last, riskLabelsClose) {
		return nil, false
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(last, riskLabelsOpen), riskLabelsClose)
	var out []risk.Label
	for _, name := range strings.Split(inner, ",") {
		name = strings.TrimSpace(name)
		for _, known := range risk.All {
			if string(known) == name {
				out = append(out, known)
				break
			}
		}
	}
	return out, true
}

// escapeText makes pull-request-controlled text safe to put in the comment's
// Markdown: one line, and every character Markdown or HTML would act on
// escaped, so "src/__init__.py" does not turn bold and "<!--" opens nothing.
func escapeText(s string) string {
	s = oneLine(s)
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune("\\`*_[]<>|~#!&", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// escapeCode keeps a value that belongs in a code span from closing it.
func escapeCode(s string) string {
	return strings.ReplaceAll(oneLine(s), "`", "'")
}
