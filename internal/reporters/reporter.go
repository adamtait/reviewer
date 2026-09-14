// SPDX-License-Identifier: MIT

// Package reporters turns a completed run into output. Each reporter is a
// different audience: a human at a terminal, a machine consuming Reviewdog
// diagnostics, or a pull request (added in PR-18).
package reporters

import (
	"context"
	"time"

	"github.com/adamtait/reviewer/pkg/finding"
)

// Run is everything a reporter is given about one review.
//
// It carries warnings as well as findings because a run where three analyzers
// failed to start is not the same as a clean run, and a reporter that shows only
// findings would present them identically.
type Run struct {
	// Root is the absolute path of the repository under review.
	Root string
	// Base is the ref the diff was taken against, for the human-readable header.
	Base string
	// Findings are already diff-scoped, validated and sorted by the core.
	Findings []finding.Finding
	// Warnings are things that went wrong without failing the run: a plugin that
	// would not start, an analyzer that timed out, a finding that was dropped.
	Warnings []string
	// Skipped names analyzers that declined to run and why, one line each.
	Skipped []string
	// Trustworthy says whether this run is a faithful account of the current
	// state: every selected analyzer ran and none failed. A run that is not
	// trustworthy reports fewer findings than exist, so nothing may be *removed*
	// on the strength of it — in particular, no thread may be resolved.
	Trustworthy bool
	// GateReason is why the model lane did not run, when it did not. Shown above
	// the fold in the summary comment: someone who has just committed a credential
	// needs to know the diff was not sent anywhere without expanding anything.
	GateReason string
	// Timings is how long each analyzer took. Reported by default: latency is the
	// risk this project's kill criteria are written against, so a slow analyzer
	// should be visible without anyone thinking to ask.
	Timings []Timing
}

// Timing is one analyzer's contribution to the run's wall clock.
type Timing struct {
	Analyzer string
	Elapsed  time.Duration
	Findings int
	Failed   bool
}

// Reporter writes a run somewhere.
//
// A reporter must not decide policy: the core has already applied diff scoping
// and the confidence gate. A reporter chooses presentation only.
type Reporter interface {
	// Name is the value accepted by --reporter.
	Name() string
	// Report writes the run. Returning an error does not fail the review — the
	// caller logs it and exits 0 (ADR-0009) — but it is worth reporting, because
	// a reporter that silently writes nothing looks like a clean run.
	Report(ctx context.Context, run Run) error
}

// counts summarises a run for the reporters that show a tally.
type counts struct {
	total                   int
	high, medium, low       int
	errors, warnings, infos int
	deterministic, llm      int
	filesWithFindings       int
}

func summarise(fs []finding.Finding) counts {
	var c counts
	files := map[string]bool{}
	for _, f := range fs {
		c.total++
		files[f.File] = true
		switch f.Confidence {
		case finding.ConfidenceHigh:
			c.high++
		case finding.ConfidenceMedium:
			c.medium++
		case finding.ConfidenceLow:
			c.low++
		}
		switch f.Severity {
		case finding.SeverityError:
			c.errors++
		case finding.SeverityWarning:
			c.warnings++
		case finding.SeverityInfo:
			c.infos++
		}
		switch f.Lane {
		case finding.LaneDeterministic:
			c.deterministic++
		case finding.LaneLLM:
			c.llm++
		}
	}
	c.filesWithFindings = len(files)
	return c
}
