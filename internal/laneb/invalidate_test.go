// SPDX-License-Identifier: MIT

package laneb

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/internal/diff"
	"github.com/adamtait/reviewer/internal/model"
	"github.com/adamtait/reviewer/pkg/finding"
)

func candidates(n int) []finding.Finding {
	out := make([]finding.Finding, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, finding.Finding{
			RuleID: "correctness/bug", Lane: finding.LaneLLM,
			Confidence: finding.ConfidenceHigh, Severity: finding.SeverityError,
			File: "src/domain/order.ts", Line: 10 + i,
			Message:  "Claim number " + string(rune('a'+i)),
			Evidence: "Stated evidence " + string(rune('a'+i)),
		})
	}
	return out
}

func verdictReply(vs ...verdict) string {
	body, err := json.Marshal(verdicts{Verdicts: vs})
	if err != nil {
		panic(err)
	}
	return string(body)
}

const diffText = "@@ -10,3 +10,5 @@\n+  async total(): Promise<number> {\n+    return 0;\n+  }\n"

// changedFiles is the set the specificity test checks a `file:line` against.
var changedFiles = map[string]bool{"src/domain/order.ts": true}

func withDiff() Input {
	return Input{
		Diff:    diffText,
		Changed: []diff.File{{Path: "src/domain/order.ts", Status: diff.StatusModified, Ranges: [][2]int{{10, 14}}}},
	}
}

// PR-36's proof: two of three disproved, one survives with its evidence intact.
func TestOnlySpecificallyDisprovedFindingsAreDropped(t *testing.T) {
	f := model.NewFake(verdictReply(
		verdict{Index: 0, Stands: false, Because: "`async total(): Promise<number> {` already returns a promise, so the claim is wrong."},
		verdict{Index: 1, Stands: false, Because: "The guard at `src/domain/order.ts:7` makes this unreachable."},
		verdict{Index: 2, Stands: true},
	))
	r := Reviewer{Provider: f, Model: "a-model"}

	kept, warnings, err := r.Invalidate(context.Background(), withDiff(), candidates(3))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "disproved by the second pass") {
		t.Fatalf("want the two deletions stated, got %v", warnings)
	}
	if len(kept) != 1 {
		t.Fatalf("want one survivor, got %+v", kept)
	}
	// The exit criterion: every emitted lane B finding carries evidence.
	if kept[0].Evidence == "" {
		t.Error("the survivor lost its evidence")
	}
	if err := kept[0].Validate(); err != nil {
		t.Errorf("the survivor does not validate: %v", err)
	}
}

// The question this pass turns on. A model asked to find fault will always find
// something to say, so "it said the claim fails" cannot be the test.
func TestAVagueDisproofKeepsTheFinding(t *testing.T) {
	for _, because := range []string{
		"This may be intentional.",
		"I think the author probably meant to do this, so it is fine.",
		"no",
		"",
		// Backticks around something that is not in the diff: decoration, and
		// exactly the confident-and-wrong failure this pass removes, arriving from
		// the other direction.
		"`const alreadyHandled = true` covers this case already.",
	} {
		f := model.NewFake(verdictReply(verdict{Index: 0, Stands: false, Because: because}))
		r := Reviewer{Provider: f, Model: "a-model"}

		kept, warnings, err := r.Invalidate(context.Background(), withDiff(), candidates(1))
		if err != nil {
			t.Fatal(err)
		}
		if len(kept) != 1 {
			t.Errorf("%q deleted a finding without pointing at anything", because)
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], "without a specific reason") {
			t.Errorf("%q: want the vague challenge reported, got %v", because, warnings)
		}
	}
}

// A reply missing an entry must leave that candidate alone rather than shift every
// later verdict onto the wrong finding.
func TestAMissingVerdictKeepsItsFinding(t *testing.T) {
	f := model.NewFake(verdictReply(
		verdict{Index: 2, Stands: false, Because: "`async total(): Promise<number> {` is the whole body, so there is nothing after it."},
	))
	r := Reviewer{Provider: f, Model: "a-model"}

	kept, _, err := r.Invalidate(context.Background(), withDiff(), candidates(3))
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 2 {
		t.Fatalf("want the two unanswered claims kept, got %+v", kept)
	}
	for _, k := range kept {
		if k.Line == 12 {
			t.Error("the disproved finding survived; the verdicts were read positionally")
		}
	}
}

// Fail open. A pass that cannot run has established nothing about what is right,
// so it must not delete what the first pass produced.
func TestAFailedInvalidationKeepsEverythingAndSaysSo(t *testing.T) {
	for name, f := range map[string]*model.Fake{
		"a refusal":       {Err: model.ErrRefused},
		"a bad reply":     model.NewFake("I cannot do that"),
		"no reply at all": model.NewFake(),
	} {
		r := Reviewer{Provider: f, Model: "a-model"}

		kept, warnings, err := r.Invalidate(context.Background(), withDiff(), candidates(3))
		if err != nil {
			t.Fatalf("%s: want a warning, got an error: %v", name, err)
		}
		if len(kept) != 3 {
			t.Errorf("%s: want everything kept, got %d", name, len(kept))
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], "unchecked") {
			t.Errorf("%s: want the reader told the findings are unchecked, got %v", name, warnings)
		}
	}
}

func TestNothingToInvalidateCostsNoCall(t *testing.T) {
	f := model.NewFake()
	r := Reviewer{Provider: f, Model: "a-model"}

	kept, warnings, err := r.Invalidate(context.Background(), Input{}, nil)
	if err != nil || len(kept) != 0 || len(warnings) != 0 {
		t.Fatalf("got %+v %v %v", kept, warnings, err)
	}
	if len(f.Calls()) != 0 {
		t.Error("a second call was made with nothing to check")
	}
}

// The judge has to see the claims, and it has to see the change.
func TestTheClaimsAndTheDiffBothReachTheJudge(t *testing.T) {
	f := model.NewFake(verdictReply())
	r := Reviewer{Provider: f, Model: "a-model"}

	if _, _, err := r.Invalidate(context.Background(), withDiff(), candidates(2)); err != nil {
		t.Fatal(err)
	}
	sent := f.Prompt(0)
	for _, want := range []string{"Claim number a", "Claim number b", "async total()", "is not a disproof"} {
		if !strings.Contains(sent, want) {
			t.Errorf("the judge was not sent %q:\n%s", want, sent)
		}
	}
}

func TestSpecific(t *testing.T) {
	for _, tc := range []struct {
		name, because string
		want          bool
	}{
		{name: "quotes the diff", because: "The line `async total(): Promise<number> {` is the whole body here.", want: true},
		{name: "names a changed file", because: "The guard at src/domain/order.ts:7 already covers it.", want: true},
		{name: "quotes something absent", because: "The call to `neverAppearsHere()` handles it already.", want: false},
		{name: "a shrug", because: "This may well be intentional behaviour.", want: false},
		{name: "too short", because: "wrong", want: false},
		{name: "empty", because: "", want: false},
		// A common token appears in almost any diff, so backticks around one is the
		// lazy answer this predicate exists to reject.
		{name: "quotes a common token", because: "I have considered this and the claim is wrong because of `return`.", want: false},
		// A file:line is a shape, not a citation, unless the file is one this
		// change touched.
		{name: "cites a file the change did not touch", because: "This is handled over in nosuchfile.go:1, as you can see.", want: false},
	} {
		if got := specific(tc.because, diffText, changedFiles); got != tc.want {
			t.Errorf("%s: specific(%q) = %v, want %v", tc.name, tc.because, got, tc.want)
		}
	}
}

// With no diff to check against, nothing is specific.
//
// The first version accepted everything in that case, which inverted the fail-open
// rule: a run where the diff could not be gathered would have had its entire model
// lane deleted by disproofs nobody could check.
func TestWithNoDiffNothingIsSpecific(t *testing.T) {
	if specific("The line `async total(): Promise<number> {` is the whole body here.", "", changedFiles) {
		t.Error("an unverifiable disproof must not delete a finding")
	}
}

// A run where the judge deleted every finding must not be byte-identical to one
// where the first pass found nothing. They are very different things to be told.
func TestDroppedFindingsAreCounted(t *testing.T) {
	f := model.NewFake(verdictReply(
		verdict{Index: 0, Stands: false, Because: "`async total(): Promise<number> {` already returns a promise, so this is wrong."},
	))
	r := Reviewer{Provider: f, Model: "a-model"}

	kept, warnings, err := r.Invalidate(context.Background(), withDiff(), candidates(1))
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 0 {
		t.Fatalf("want it dropped, got %+v", kept)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "disproved by the second pass") {
		t.Errorf("want the deletion stated, got %v", warnings)
	}
}
