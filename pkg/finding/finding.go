// SPDX-License-Identifier: MIT

// Package finding defines the single shape every analyzer, in either lane and in
// any language, normalises to (ADR-0003). It is part of the plugin API: changing
// it changes the protocol, so treat additions as the only compatible edit.
package finding

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Lane says which half of the system produced a finding. It is declared by the
// analyzer's plugin descriptor, not inferred by the core, so that a third-party
// plugin cannot slip model-backed analysis past the secrets gate (ADR-0005).
type Lane string

const (
	// LaneDeterministic covers analysis that needs no model and makes no egress.
	LaneDeterministic Lane = "deterministic"
	// LaneLLM covers analysis that consults a model. Blocked outright when the
	// secrets gate trips (ADR-0012).
	LaneLLM Lane = "llm"
)

// Confidence drives presentation, not severity: only high-confidence findings are
// posted inline, everything else lands in the collapsed summary (ADR-0017).
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

// Severity describes how bad the thing found is, independently of how sure the
// analyzer is that it found it.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Finding is one reported observation about one place in the diff.
//
// Field order here is the field order in schema/finding.schema.json; the parity
// test in this package fails if the two drift apart.
type Finding struct {
	// Fingerprint identifies this finding across force-pushes and rebases. It is
	// derived from the normalised source snippet rather than the line number
	// (ADR-0015) and is filled in by the core, not by analyzers.
	Fingerprint string `json:"fingerprint"`

	// RuleID is namespaced by category: "arch/no-domain-to-infra",
	// "llm/missing-abstraction". Acceptance is measured per RuleID (ADR-0024),
	// so it must be stable across releases.
	RuleID string `json:"ruleId"`

	Lane       Lane       `json:"lane"`
	Confidence Confidence `json:"confidence"`
	Severity   Severity   `json:"severity"`

	// File is always relative to the repository root, with forward slashes.
	File string `json:"file"`
	// Line is 1-indexed.
	Line int `json:"line"`
	// EndLine is 1-indexed and inclusive; zero means the finding is one line.
	EndLine int `json:"endLine,omitempty"`

	Message string `json:"message"`
	// Suggestion, when present, is the replacement text for File[Line:EndLine]
	// and becomes a GitHub suggested change.
	Suggestion string `json:"suggestion,omitempty"`
	// Evidence is why the analyzer believes this. Required in the LLM lane, where
	// it is what the invalidation pass leaves behind (ADR-0023).
	Evidence string `json:"evidence,omitempty"`
}

var (
	errNoRuleID  = errors.New("ruleId is required")
	errNoFile    = errors.New("file is required")
	errNoMessage = errors.New("message is required")
)

// Validate reports every problem with a finding rather than the first, so a
// misbehaving plugin gets one actionable message instead of a series of them.
func (f Finding) Validate() error {
	var problems []error

	if f.RuleID == "" {
		problems = append(problems, errNoRuleID)
	}
	if f.File == "" {
		problems = append(problems, errNoFile)
	}
	if strings.HasPrefix(f.File, "/") {
		problems = append(problems, fmt.Errorf("file %q must be relative to the repository root", f.File))
	}
	if f.Message == "" {
		problems = append(problems, errNoMessage)
	}
	if f.Line < 1 {
		problems = append(problems, fmt.Errorf("line is %d, want 1 or greater", f.Line))
	}
	if f.EndLine != 0 && f.EndLine < f.Line {
		problems = append(problems, fmt.Errorf("endLine %d is before line %d", f.EndLine, f.Line))
	}
	if f.Lane != LaneDeterministic && f.Lane != LaneLLM {
		problems = append(problems, fmt.Errorf("lane is %q, want %q or %q", f.Lane, LaneDeterministic, LaneLLM))
	}
	switch f.Confidence {
	case ConfidenceHigh, ConfidenceMedium, ConfidenceLow:
	default:
		problems = append(problems, fmt.Errorf("confidence is %q, want high, medium or low", f.Confidence))
	}
	switch f.Severity {
	case SeverityError, SeverityWarning, SeverityInfo:
	default:
		problems = append(problems, fmt.Errorf("severity is %q, want error, warning or info", f.Severity))
	}
	// The LLM lane must show its work. A model-produced finding with no evidence
	// is indistinguishable from a guess, and the invalidation pass exists to
	// remove exactly those (ADR-0023).
	if f.Lane == LaneLLM && strings.TrimSpace(f.Evidence) == "" {
		problems = append(problems, errors.New("evidence is required for findings in the llm lane"))
	}

	return errors.Join(problems...)
}

// Span returns the inclusive line range a finding covers.
func (f Finding) Span() (start, end int) {
	if f.EndLine == 0 {
		return f.Line, f.Line
	}
	return f.Line, f.EndLine
}

// Sort orders findings for stable output: by file, then line, then rule. Reporters
// and golden tests depend on this, so it must not become locale- or map-dependent.
func Sort(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.RuleID < b.RuleID
	})
}
