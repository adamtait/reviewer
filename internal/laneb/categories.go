// SPDX-License-Identifier: MIT

package laneb

import (
	"sort"
	"strings"

	"github.com/adamtait/reviewer/pkg/finding"
)

// Category is one thing the model lane is allowed to report, and the most it is
// allowed to claim about it.
//
// A closed set, for two reasons that are easy to underrate.
//
// Acceptance is measured per rule id (ADR-0024) so that a category which is
// routinely dismissed can be deleted on evidence. A model inventing a rule id per
// finding makes every bucket size one, and the measurement — the thing that keeps
// this lane honest — stops working.
//
// And the ceiling has to live here rather than in the prompt. Only `high`
// confidence goes inline (ADR-0017), so a ceiling is the difference between a
// category that can interrupt a reviewer and one that can only appear in a
// collapsed summary. That is a decision about the product, and asking a model to
// respect it is asking it to enforce a rule it has every incentive to round up.
type Category struct {
	// ID is the rule id a finding carries.
	ID string
	// Max is the highest confidence a finding in this category may claim.
	Max finding.Confidence
	// What it covers, for the prompt. Written for the model, and the same text a
	// person reads when asking why a finding exists.
	Description string
}

// categories is the whole vocabulary. Deliberately small: a category nobody can
// describe in one sentence is one nobody can judge the acceptance rate of either.
var categories = []Category{
	{
		ID:          "correctness/bug",
		Max:         finding.ConfidenceHigh,
		Description: "code that does something other than what it plainly intends — an inverted condition, an off-by-one, a case that cannot be reached, a value that is never used.",
	},
	{
		ID:          "correctness/unhandled",
		Max:         finding.ConfidenceHigh,
		Description: "an error, rejection or absent value that is ignored where it can occur, or swallowed in a way that loses information a caller needs.",
	},
	{
		ID:          "correctness/concurrency",
		Max:         finding.ConfidenceHigh,
		Description: "a race, a lock held across a suspension point, shared state mutated without synchronisation, or an await inside a critical section.",
	},
	{
		ID:          "security/risk",
		Max:         finding.ConfidenceHigh,
		Description: "untrusted input reaching somewhere it is trusted: a query, a command line, a path, a template, a redirect. Not a guess about a credential — the secrets scanner does that, and this lane never runs when it has found one.",
	},
	{
		ID:          "tests/missing",
		Max:         finding.ConfidenceMedium,
		Description: "behaviour this change introduces that no test in this change exercises, where a test would be straightforward to write.",
	},
	{
		ID:          "docs/stale",
		Max:         finding.ConfidenceMedium,
		Description: "documentation in this repository that this change makes wrong. Only for documents named above as possibly invalidated.",
	},
	{
		// Taste. Capped in code, not in the prompt.
		ID:          "arch/missing-abstraction",
		Max:         finding.ConfidenceMedium,
		Description: "a shape repeated enough times, in this change, that naming it would be clearly better. Not a suggestion to add a layer; an observation that one already exists without a name.",
	},
	{
		// Taste. Capped in code, not in the prompt.
		ID:          "quality/sloppy",
		Max:         finding.ConfidenceMedium,
		Description: "work that reads as unfinished: a name that says nothing, a comment that contradicts the code, a copied block with one thing changed, debugging left behind.",
	},
}

// byID is the lookup the cap uses.
var byID = func() map[string]Category {
	m := make(map[string]Category, len(categories))
	for _, c := range categories {
		m[c.ID] = c
	}
	return m
}()

// Cap enforces each category's ceiling and rejects anything outside the
// vocabulary.
//
// Rejecting rather than remapping: a finding whose category the model made up is a
// finding whose category nobody defined, and filing it under something plausible
// would put an unmeasurable thing into a measured bucket.
func Cap(f finding.Finding) (finding.Finding, bool) {
	category, known := byID[f.RuleID]
	if !known {
		return f, false
	}
	if rank(f.Confidence) > rank(category.Max) {
		f.Confidence = category.Max
	}
	return f, true
}

func rank(c finding.Confidence) int {
	switch c {
	case finding.ConfidenceHigh:
		return 3
	case finding.ConfidenceMedium:
		return 2
	default:
		return 1
	}
}

// CategoryList renders the vocabulary for a prompt, sorted so the prompt is
// byte-stable across runs.
func CategoryList() string {
	sorted := append([]Category(nil), categories...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	b := &strings.Builder{}
	for _, c := range sorted {
		b.WriteString("- `" + c.ID + "` — " + c.Description + "\n")
	}
	return b.String()
}
