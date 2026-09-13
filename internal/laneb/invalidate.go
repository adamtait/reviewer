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

	var (
		kept  []finding.Finding
		vague int
	)
	for i, f := range candidates {
		v, answered := byIndex[i]
		switch {
		case !answered, v.Stands:
			// Silence keeps the finding. A pass that says nothing has disproved
			// nothing.
			kept = append(kept, f)
		case !specific(v.Because, in.Diff):
			// "This may be intentional" is a shrug, and a shrug is not a reason to
			// delete somebody's finding.
			vague++
			kept = append(kept, f)
		default:
			// Dropped, and deliberately not reported. A finding the second pass
			// disproved is one nobody needs to hear about.
		}
	}

	var warnings []string
	if vague > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%d finding(s) were challenged without a specific reason and were kept", vague))
	}
	return kept, warnings, nil
}

// pointer matches a disproof that points at something: a backticked fragment, or a
// bare file:line.
var pointer = regexp.MustCompile("`([^`\n]{3,})`|([\\w./-]+\\.[A-Za-z0-9]+:\\d+)")

// location is a file:line reference, which may itself be what the backticks hold.
var location = regexp.MustCompile(`^[\w./-]+\.[A-Za-z0-9]+:\d+$`)

// specific decides whether a disproof earns the right to delete a finding.
//
// This is the question the whole pass turns on. A model asked to find fault with a
// claim will always find something to say, so "it said the claim fails" cannot be
// the test — the test has to be whether it pointed at anything.
//
// A quoted fragment counts only when it actually appears in the diff. Otherwise the
// backticks are decoration, and a disproof quoting code that is not there is
// exactly the confident-and-wrong failure this pass exists to remove, arriving from
// the other direction.
func specific(because, diffText string) bool {
	because = strings.TrimSpace(because)
	if len(because) < 20 {
		return false
	}
	for _, m := range pointer.FindAllStringSubmatch(because, -1) {
		candidate := strings.TrimSpace(firstNonEmpty(m[1], m[2]))
		if candidate == "" {
			continue
		}
		// A file:line reference points at something whether or not it is inside
		// backticks, and whether or not that line is in the diff — it is checkable
		// by the reader, which is the whole requirement.
		if location.MatchString(candidate) {
			return true
		}
		if diffText == "" || strings.Contains(diffText, candidate) {
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
