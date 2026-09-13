// SPDX-License-Identifier: MIT

// Package laneb is the model-backed half of the review.
//
// Everything here is downstream of the secrets gate (ADR-0012): if a credential
// was found in the diff, or the scan that looks for one could not run, nothing in
// this package is reached at all. That is structural rather than conditional — the
// sequencer runs two passes with the gate between them — so there is no flag in
// here to get wrong.
package laneb

import (
	"fmt"
	"sort"
	"strings"

	"github.com/adamtait/reviewer/internal/diff"
	"github.com/adamtait/reviewer/pkg/finding"
)

// Input is everything the assembler is allowed to see.
//
// A struct rather than a config and a root, because the assembler must be a pure
// function of stated inputs: what gets sent to a model provider is the most
// consequential thing this tool does, and "what is in the prompt" has to be
// answerable by reading one function rather than by auditing everything it could
// reach. Nothing here is discovered — every field is put in by the caller, which
// is what lets a test assert an exact prompt.
type Input struct {
	// Base names what the change is measured against, for the model's benefit.
	// Empty means the staged changes.
	Base string
	// Changed is the diff's file list, for the summary.
	Changed []diff.File
	// Diff is the unified diff with context, already produced.
	Diff string
	// Findings are what the deterministic lane already reported. Included so the
	// model is not asked to rediscover them — and, more usefully, so it is told
	// what is already covered and can spend its attention elsewhere.
	Findings []finding.Finding
	// Guidance is the destination repository's own documents, in the order config
	// named them. The paths come from config and the contents from the working
	// tree; neither ever lives in this repository (ADR-0004, ADR-0010).
	Guidance []Document
	// Docs are documentation files this change may have invalidated.
	Docs []StaleDoc
	// MaxDiffBytes and MaxGuidanceBytes bound the prompt. Zero means the defaults.
	MaxDiffBytes     int
	MaxGuidanceBytes int
}

// Document is one guidance file: a path relative to the repository root, and its
// contents.
type Document struct {
	Path string
	Body string
}

// StaleDoc is a documentation file that references source this change touches.
//
// Not mtime, which the plan suggested: a file's modification time is a fact about
// a checkout rather than about a repository, so it differs between a developer's
// machine and a fresh CI clone and would make this section non-deterministic. What
// is actually actionable is narrower and needs no timestamp at all — this change
// touches code that this document describes, so the document may now be wrong.
type StaleDoc struct {
	// Path is the documentation file.
	Path string
	// References are the changed files it mentions, sorted.
	References []string
}

const (
	defaultMaxDiffBytes     = 120 << 10
	defaultMaxGuidanceBytes = 24 << 10
)

// Assemble builds the material half of the model's input: everything about this
// change, and nothing about this machine.
//
// Deterministic by construction. Every list is sorted, every path is relative, and
// nothing is read from the environment or the filesystem — so the same change
// produces the same bytes on a laptop and on a runner, which is what makes the
// golden file in this package's tests meaningful.
func Assemble(in Input) string {
	b := &strings.Builder{}

	fmt.Fprintf(b, "# The change under review\n\n")
	if in.Base == "" {
		fmt.Fprintf(b, "These are staged changes, not yet committed.\n\n")
	} else {
		fmt.Fprintf(b, "Measured against `%s`.\n\n", in.Base)
	}

	writeFiles(b, in.Changed)
	writeGuidance(b, in.Guidance, or(in.MaxGuidanceBytes, defaultMaxGuidanceBytes))
	writeStaleDocs(b, in.Docs)
	writeFindings(b, in.Findings)
	writeDiff(b, in.Diff, or(in.MaxDiffBytes, defaultMaxDiffBytes))

	return b.String()
}

func writeFiles(b *strings.Builder, files []diff.File) {
	if len(files) == 0 {
		fmt.Fprintf(b, "## Files changed\n\nNone.\n\n")
		return
	}
	sorted := append([]diff.File(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	fmt.Fprintf(b, "## Files changed\n\n")
	for _, f := range sorted {
		fmt.Fprintf(b, "- `%s` (%s)", f.Path, f.Status)
		if f.OldPath != "" {
			fmt.Fprintf(b, ", was `%s`", f.OldPath)
		}
		fmt.Fprintf(b, "\n")
	}
	fmt.Fprintf(b, "\n")
}

func writeGuidance(b *strings.Builder, docs []Document, budget int) {
	if len(docs) == 0 {
		return
	}
	fmt.Fprintf(b, "## This repository's own guidance\n\n")
	fmt.Fprintf(b, "These are the documents this repository asked to be reviewed against. "+
		"Where they disagree with general practice, they win.\n\n")

	// Shared across all guidance files rather than per file: one repository naming
	// eight documents should not send eight times as much as one naming a single
	// document.
	remaining := budget
	for _, doc := range docs {
		body, truncated := clip(doc.Body, remaining)
		remaining -= len(body)
		fmt.Fprintf(b, "### `%s`\n\n%s\n", doc.Path, strings.TrimRight(body, "\n"))
		if truncated {
			fmt.Fprintf(b, "\n_(truncated)_\n")
		}
		fmt.Fprintf(b, "\n")
		if remaining <= 0 {
			break
		}
	}
}

func writeStaleDocs(b *strings.Builder, docs []StaleDoc) {
	if len(docs) == 0 {
		return
	}
	sorted := append([]StaleDoc(nil), docs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	fmt.Fprintf(b, "## Documentation this change may have invalidated\n\n")
	for _, doc := range sorted {
		refs := append([]string(nil), doc.References...)
		sort.Strings(refs)
		fmt.Fprintf(b, "- `%s` describes %s\n", doc.Path, quoteList(refs))
	}
	fmt.Fprintf(b, "\n")
}

func writeFindings(b *strings.Builder, findings []finding.Finding) {
	if len(findings) == 0 {
		return
	}
	sorted := append([]finding.Finding(nil), findings...)
	finding.Sort(sorted)

	fmt.Fprintf(b, "## Already reported by the deterministic analyzers\n\n")
	fmt.Fprintf(b, "Do not repeat these. They are here so you can spend your attention "+
		"on what they cannot see.\n\n")
	for _, f := range sorted {
		fmt.Fprintf(b, "- `%s` at `%s:%d` — %s\n", f.RuleID, f.File, f.Line, oneLine(f.Message))
	}
	fmt.Fprintf(b, "\n")
}

func writeDiff(b *strings.Builder, text string, budget int) {
	fmt.Fprintf(b, "## The diff\n\n")
	body, truncated := clip(text, budget)
	fmt.Fprintf(b, "```diff\n%s\n```\n", strings.TrimRight(body, "\n"))
	if truncated {
		fmt.Fprintf(b, "\n_(the diff was longer than the budget and is cut off here)_\n")
	}
}

// clip cuts text to a byte budget at a line boundary, so a truncated prompt never
// ends mid-token in a way that reads as content.
func clip(s string, budget int) (string, bool) {
	if budget <= 0 {
		return "", s != ""
	}
	if len(s) <= budget {
		return s, false
	}
	cut := s[:budget]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i]
	}
	return cut, true
}

func quoteList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		quoted = append(quoted, "`"+item+"`")
	}
	switch len(quoted) {
	case 0:
		return "nothing in this change"
	case 1:
		return quoted[0]
	default:
		return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
	}
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i]) + " …"
	}
	return s
}

func or(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}
