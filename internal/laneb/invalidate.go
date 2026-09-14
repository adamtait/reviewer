// SPDX-License-Identifier: MIT

package laneb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/adamtait/reviewer/internal/model"
	"github.com/adamtait/reviewer/pkg/finding"
)

// verdicts is what the invalidation pass returns.
type verdicts struct {
	Verdicts []verdict `json:"verdicts"`
}

type verdict struct {
	Index   int    `json:"index"`
	Stands  bool   `json:"stands"`
	Because string `json:"because"`
}

// Invalidate asks a second time, adversarially, and keeps what survives.
//
// This is the precision mechanism the whole lane rests on (ADR-0023). A single
// pass produces findings that are individually plausible and collectively
// exhausting; the second pass is cheap compared to a reviewer's attention and it
// removes the class of finding that does the most damage — confident, wrong, and
// about code somebody already thought about.
//
// One call, not one per candidate: the disproof of a claim usually lives in the
// same diff as the disproof of its neighbours, and N calls would pay N times for
// the same context.
func (r Reviewer) Invalidate(ctx context.Context, in Input, candidates []finding.Finding) ([]finding.Finding, []string, error) {
	if len(candidates) == 0 {
		return candidates, nil, nil
	}

	system, err := r.prompt("invalidate.md", map[string]string{"Claims": claimList(candidates)})
	if err != nil {
		return nil, nil, err
	}

	reply, err := r.Provider.Complete(ctx, []model.Message{
		{Role: model.RoleSystem, Content: system},
		{Role: model.RoleUser, Content: Assemble(in)},
	}, model.Options{Model: r.Model, Temperature: 0, MaxTokens: r.MaxTokens, JSON: true})

	if errors.Is(err, model.ErrRefused) || err != nil {
		// Fail open. A pass that cannot run must not delete findings the first pass
		// produced: this exists to remove what is wrong, and it has established
		// nothing about what is right (ADR-0013).
		return candidates, []string{fmt.Sprintf(
			"every finding is unchecked: the invalidation pass did not run (%v)", err)}, nil
	}

	var parsed verdicts
	if err := json.Unmarshal([]byte(extractJSON(reply)), &parsed); err != nil {
		return candidates, []string{fmt.Sprintf(
			"every finding is unchecked: the invalidation pass returned %v", err)}, nil
	}

	// Keyed by index rather than positional, because a reply missing an entry must
	// leave that candidate alone rather than shift every verdict after it onto the
	// wrong finding.
	byIndex := map[int]verdict{}
	for _, v := range parsed.Verdicts {
		byIndex[v.Index] = v
	}

	changed := map[string]bool{}
	for _, f := range in.Changed {
		changed[f.Path] = true
	}

	var (
		kept           []finding.Finding
		vague, dropped int
	)
	for i, f := range candidates {
		v, answered := byIndex[i]
		switch {
		case !answered, v.Stands:
			// Silence keeps the finding. A pass that says nothing has disproved
			// nothing.
			kept = append(kept, f)
		case !specific(v.Because, in.Diff, changed):
			// "This may be intentional" is a shrug, and a shrug is not a reason to
			// delete somebody's finding.
			vague++
			kept = append(kept, f)
		default:
			dropped++
		}
	}

	var warnings []string
	if vague > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%d finding(s) were challenged without a specific reason and were kept", vague))
	}
	if dropped > 0 {
		// Said, not silent. A run where the judge deleted everything would
		// otherwise be byte-identical to one where the first pass found nothing,
		// and those are very different things to be told.
		warnings = append(warnings, fmt.Sprintf(
			"%d model finding(s) were disproved by the second pass and dropped", dropped))
	}
	return kept, warnings, nil
}

// pointer matches a disproof that points at something: a backticked fragment, or a
// bare file:line.
//
// The length floor is the part that does work. At three characters `let`, `the`
// and `err` appear in almost any diff, so the test was satisfiable by any disproof
// that put backticks around a common word — which is exactly the lazy answer this
// predicate exists to reject.
var pointer = regexp.MustCompile("`([^`\n]{" + minQuotedLength + ",})`|([\\w./-]+\\.[A-Za-z0-9]+:\\d+)")

// minQuotedLength is how much of the diff a disproof must quote. Long enough that
// it identifies a place rather than a token; short enough for a real line of code.
const minQuotedLength = "12"

// location is a file:line reference, which may itself be what the backticks hold.
var location = regexp.MustCompile(`^([\w./-]+\.[A-Za-z0-9]+):(\d+)$`)

// specific decides whether a disproof earns the right to delete a finding.
//
// This is the question the whole pass turns on. A model asked to find fault with a
// claim will always find something to say, so "it said the claim fails" cannot be
// the test — the test has to be whether it pointed at something checkable.
//
// Three things have to hold, and each of them was missing at some point:
//
//   - A quoted fragment must be long enough to identify a place, and must actually
//     appear in the diff. Backticks around an invented fragment is the same
//     confident-and-wrong failure this pass removes, arriving from the other
//     direction.
//   - A file:line must name a file this change touched. `nosuchfile.go:1` is not a
//     citation, it is a shape.
//   - With no diff to check against, **nothing** is specific. The first version
//     accepted everything in that case, which inverted the fail-open rule: a run
//     where the diff could not be gathered would have had its entire model lane
//     deleted by unverifiable disproofs.
func specific(because, diffText string, changed map[string]bool) bool {
	because = strings.TrimSpace(because)
	if len(because) < 20 || diffText == "" {
		return false
	}
	for _, m := range pointer.FindAllStringSubmatch(because, -1) {
		candidate := strings.TrimSpace(firstNonEmpty(m[1], m[2]))
		if candidate == "" {
			continue
		}
		if where := location.FindStringSubmatch(candidate); where != nil {
			if changed[where[1]] {
				return true
			}
			continue
		}
		if strings.Contains(diffText, candidate) {
			return true
		}
	}
	return false
}

// claimList renders the candidates for the prompt: enough to judge each one, and
// nothing that would let the judge defer to the first pass's own confidence.
func claimList(candidates []finding.Finding) string {
	b := &strings.Builder{}
	for i, f := range candidates {
		fmt.Fprintf(b, "%d. `%s` at `%s:%d`\n   Claim: %s\n   Stated evidence: %s\n\n",
			i, f.RuleID, f.File, f.Line, oneLine(f.Message), oneLine(f.Evidence))
	}
	return strings.TrimRight(b.String(), "\n")
}
