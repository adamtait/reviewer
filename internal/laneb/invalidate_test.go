// SPDX-License-Identifier: MIT

package laneb

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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

// PR-36's proof: two of three disproved, one survives with its evidence intact.
func TestOnlySpecificallyDisprovedFindingsAreDropped(t *testing.T) {
	f := model.NewFake(verdictReply(
		verdict{Index: 0, Stands: false, Because: "`async total(): Promise<number> {` already returns a promise, so the claim is wrong."},
		verdict{Index: 1, Stands: false, Because: "The guard at `src/domain/order.ts:7` makes this unreachable."},
		verdict{Index: 2, Stands: true},
	))
	r := Reviewer{Provider: f, Model: "a-model"}

	kept, warnings, err := r.Invalidate(context.Background(), Input{Diff: diffText}, candidates(3))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
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

		kept, warnings, err := r.Invalidate(context.Background(), Input{Diff: diffText}, candidates(1))
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
		verdict{Index: 2, Stands: false, Because: "`return 0;` is the whole body, so there is nothing after it."},
	))
	r := Reviewer{Provider: f, Model: "a-model"}

	kept, _, err := r.Invalidate(context.Background(), Input{Diff: diffText}, candidates(3))
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

		kept, warnings, err := r.Invalidate(context.Background(), Input{Diff: diffText}, candidates(3))
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

	if _, _, err := r.Invalidate(context.Background(), Input{Diff: diffText}, candidates(2)); err != nil {
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
		{name: "quotes the diff", because: "The line `return 0;` is the whole body here.", want: true},
		{name: "names a location", because: "The guard at src/domain/order.ts:7 already covers it.", want: true},
		{name: "quotes something absent", because: "The call to `neverAppears()` handles it already.", want: false},
		{name: "a shrug", because: "This may well be intentional behaviour.", want: false},
		{name: "too short", because: "wrong", want: false},
		{name: "empty", because: "", want: false},
	} {
		if got := specific(tc.because, diffText); got != tc.want {
			t.Errorf("%s: specific(%q) = %v, want %v", tc.name, tc.because, got, tc.want)
		}
	}
}

// With no diff to check against, a quoted fragment has to be taken at face value:
// the alternative is refusing every disproof, which would make the pass pointless.
func TestWithNoDiffAQuotedFragmentIsAccepted(t *testing.T) {
	if !specific("The line `return 0;` is the whole body here.", "") {
		t.Error("want the quote accepted when there is nothing to check it against")
	}
}
