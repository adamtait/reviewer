// SPDX-License-Identifier: MIT

package reporters

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/adamtait/reviewer/pkg/finding"
)

// Text writes a run for a human at a terminal. It is the default reporter and the
// only one used on the agent surface, so it optimises for being read top to
// bottom rather than for being parsed.
type Text struct {
	Out io.Writer
}

func (t Text) Name() string { return "text" }

func (t Text) Report(_ context.Context, run Run) error {
	out := t.Out
	if out == nil {
		out = io.Discard
	}

	// Warnings first. A run where half the analyzers failed to start is not a
	// clean run, and burying that under the findings misrepresents it.
	for _, w := range run.Warnings {
		if _, err := fmt.Fprintf(out, "warning: %s\n", w); err != nil {
			return err
		}
	}
	for _, s := range run.Skipped {
		if _, err := fmt.Fprintf(out, "skipped: %s\n", s); err != nil {
			return err
		}
	}
	if len(run.Warnings) > 0 || len(run.Skipped) > 0 {
		fmt.Fprintln(out)
	}

	if len(run.Timings) > 0 {
		var total time.Duration
		for _, t := range run.Timings {
			total += t.Elapsed
		}
		for _, t := range run.Timings {
			status := fmt.Sprintf("%d finding%s", t.Findings, plural(t.Findings))
			if t.Failed {
				status = "failed"
			}
			fmt.Fprintf(out, "  %-24s %8s  %s\n", t.Analyzer, t.Elapsed.Round(time.Millisecond), status)
		}
		fmt.Fprintf(out, "  %-24s %8s\n\n", "total", total.Round(time.Millisecond))
	}

	c := summarise(run.Findings)
	if c.total == 0 {
		_, err := fmt.Fprintln(out, "reviewer: no findings")
		return err
	}

	var currentFile string
	for _, f := range run.Findings {
		if f.File != currentFile {
			if currentFile != "" {
				fmt.Fprintln(out)
			}
			currentFile = f.File
			if _, err := fmt.Fprintf(out, "%s\n", f.File); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(out, "  %6s  %-7s  %s  %s\n",
			lineRef(f), f.Severity, f.RuleID, message(f)); err != nil {
			return err
		}
		// Evidence is the whole reason a model-lane finding is trustworthy enough
		// to show, so it is never hidden behind a flag.
		if f.Evidence != "" {
			fmt.Fprintf(out, "%swhy: %s\n", detailIndent, indentWrapped(f.Evidence))
		}
		if f.Suggestion != "" {
			fmt.Fprintf(out, "%ssuggest: %s\n", detailIndent, indentWrapped(f.Suggestion))
		}
	}

	_, err := fmt.Fprintf(out, "\n%d finding%s in %d file%s (%d high, %d medium, %d low confidence)\n",
		c.total, plural(c.total), c.filesWithFindings, plural(c.filesWithFindings), c.high, c.medium, c.low)
	return err
}

func lineRef(f finding.Finding) string {
	start, end := f.Span()
	if start == end {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d-%d", start, end)
}

// message appends the confidence only when it is not high, so the common case
// stays quiet and a hedged finding announces itself.
// message adds what the reader needs to decide how much weight to give a finding.
//
// The lane is shown as well as the confidence, because they answer different
// questions and only one of them was visible. Confidence says how loudly a finding
// is presented; the lane says whether it came from something that can be wrong
// about what the code says. A `correctness/bug` at high confidence from the model
// lane is allowed (ADR-0022) and, printed without the marker, was indistinguishable
// from a compiler error — which is how a reader ends up rewriting correct code on a
// model's say-so.
func message(f finding.Finding) string {
	var notes []string
	if f.Lane == finding.LaneLLM {
		notes = append(notes, "model")
	}
	if f.Confidence != finding.ConfidenceHigh {
		notes = append(notes, string(f.Confidence)+" confidence")
	}
	if len(notes) == 0 {
		return f.Message
	}
	return fmt.Sprintf("%s (%s)", f.Message, strings.Join(notes, ", "))
}

// detailIndent lines evidence and suggestions up under the message column.
const detailIndent = "          "

func indentWrapped(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n"+detailIndent+"  ")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
