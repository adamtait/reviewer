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

func reviewer(replies ...string) (Reviewer, *model.Fake) {
	f := model.NewFake(replies...)
	return Reviewer{Provider: f, Model: "a-model"}, f
}

func reply(findings ...candidate) string {
	body, err := json.Marshal(response{Findings: findings})
	if err != nil {
		panic(err)
	}
	return string(body)
}

func sound(ruleID, confidence string) candidate {
	return candidate{
		RuleID: ruleID, Confidence: confidence, Severity: "warning",
		File: "src/domain/order.ts", Line: 12,
		Message:  "The two branches disagree about whether id is set.",
		Evidence: "Line 12 checks `!id` and line 14 dereferences it.",
	}
}

// The exit criterion, and the reason ADR-0022 exists. Only high confidence goes
// inline (ADR-0017), so a ceiling is the difference between a category that can
// interrupt a reviewer and one that can only appear in a collapsed summary.
func TestTasteCategoriesCannotClaimHighConfidence(t *testing.T) {
	r, _ := reviewer(reply(
		sound("arch/missing-abstraction", "high"),
		sound("quality/sloppy", "high"),
		sound("correctness/bug", "high"),
	))

	findings, warnings, err := r.Review(context.Background(), Input{})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(findings) != 3 {
		t.Fatalf("want all three kept, got %+v", findings)
	}

	got := map[string]finding.Confidence{}
	for _, f := range findings {
		got[f.RuleID] = f.Confidence
	}
	if got["arch/missing-abstraction"] != finding.ConfidenceMedium {
		t.Errorf("arch/missing-abstraction = %q, want medium", got["arch/missing-abstraction"])
	}
	if got["quality/sloppy"] != finding.ConfidenceMedium {
		t.Errorf("quality/sloppy = %q, want medium", got["quality/sloppy"])
	}
	// A category with something checkable behind it keeps what it claimed.
	if got["correctness/bug"] != finding.ConfidenceHigh {
		t.Errorf("correctness/bug = %q, want high", got["correctness/bug"])
	}
}

// The cap is in code, not in the prompt. Asserted directly, because a prompt
// instruction is a request and this is a rule.
func TestTheCapIsAPropertyOfTheCategoryTable(t *testing.T) {
	for _, id := range []string{"arch/missing-abstraction", "quality/sloppy"} {
		capped, known := Cap(finding.Finding{RuleID: id, Confidence: finding.ConfidenceHigh})
		if !known {
			t.Fatalf("%s is not in the vocabulary", id)
		}
		if capped.Confidence != finding.ConfidenceMedium {
			t.Errorf("%s capped to %q, want medium", id, capped.Confidence)
		}
	}
	// And the prompt does not carry the ceiling, so nobody can weaken it by
	// editing wording.
	r := Reviewer{}
	prompt, err := r.prompt("review.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "never exceed") || strings.Contains(prompt, "at most medium") {
		t.Error("the ceiling is stated in the prompt, where it is a request rather than a rule")
	}
}

// A model inventing a rule id per finding makes every acceptance bucket size one,
// and the measurement that keeps this lane honest stops working (ADR-0024).
func TestAnUnknownCategoryIsDiscardedWithAReason(t *testing.T) {
	r, _ := reviewer(reply(sound("style/naming", "high"), sound("correctness/bug", "high")))

	findings, warnings, err := r.Review(context.Background(), Input{})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].RuleID != "correctness/bug" {
		t.Fatalf("want only the known category, got %+v", findings)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "style/naming") {
		t.Errorf("want the discard explained, got %v", warnings)
	}
}

// Every lane B finding carries evidence (ADR-0023's precondition, enforced by
// finding.Validate). A finding without it is a guess, and a guess posted on
// someone's pull request is worse than silence.
func TestAFindingWithoutEvidenceIsDiscarded(t *testing.T) {
	bare := sound("correctness/bug", "high")
	bare.Evidence = ""
	r, _ := reviewer(reply(bare))

	findings, warnings, err := r.Review(context.Background(), Input{})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("want it discarded, got %+v", findings)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "evidence") {
		t.Errorf("want the reason stated, got %v", warnings)
	}
}

// The lane is declared by this package, never taken from the reply: a model
// claiming to be the deterministic lane would bypass every rule that applies to
// this one — the confidence cap, the evidence requirement, the invalidation pass.
func TestTheLaneCannotBeClaimedByTheModel(t *testing.T) {
	r, _ := reviewer(`{"findings":[{"ruleId":"correctness/bug","lane":"deterministic","confidence":"high",` +
		`"severity":"error","file":"a.ts","line":1,"message":"m","evidence":"e"}]}`)

	findings, _, err := r.Review(context.Background(), Input{})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Lane != finding.LaneLLM {
		t.Fatalf("want the llm lane, got %+v", findings)
	}
}

// A malformed reply is a lane that found nothing this time, not a failed run: the
// deterministic lane is unaffected and must still be reported (ADR-0013).
func TestMalformedJSONYieldsNothingAndOneWarning(t *testing.T) {
	r, _ := reviewer("I'm afraid I can't do that.")

	findings, warnings, err := r.Review(context.Background(), Input{})
	if err != nil {
		t.Fatalf("a bad reply is not a failed run: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want nothing, got %+v", findings)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "not the requested JSON") {
		t.Errorf("want one warning saying so, got %v", warnings)
	}
}

// The prompt asks for bare JSON and most replies are, but a fenced block is common
// enough that refusing it would throw away good findings over presentation.
func TestAFencedReplyIsStillRead(t *testing.T) {
	r, _ := reviewer("Here is what I found:\n\n```json\n" + reply(sound("correctness/bug", "high")) + "\n```\n")

	findings, warnings, err := r.Review(context.Background(), Input{})
	if err != nil || len(findings) != 1 {
		t.Fatalf("got %+v %v %v", findings, warnings, err)
	}
}

// Downwards, always: a reply this package does not fully understand must not be
// able to claim more than one it does.
func TestUnrecognisedLevelsFallBackToTheQuietestThing(t *testing.T) {
	c := sound("correctness/bug", "certain")
	c.Severity = "critical"
	r, _ := reviewer(reply(c))

	findings, _, err := r.Review(context.Background(), Input{})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %+v", findings)
	}
	if findings[0].Confidence != finding.ConfidenceLow {
		t.Errorf("confidence = %q, want low", findings[0].Confidence)
	}
	if findings[0].Severity != finding.SeverityInfo {
		t.Errorf("severity = %q, want info", findings[0].Severity)
	}
}

// A refusal is not a transport error and there is nothing to retry.
func TestARefusalIsAWarningRatherThanAFailure(t *testing.T) {
	f := model.NewFake()
	f.Err = model.ErrRefused
	r := Reviewer{Provider: f, Model: "a-model"}

	findings, warnings, err := r.Review(context.Background(), Input{})
	if err != nil {
		t.Fatalf("want a warning, got an error: %v", err)
	}
	if len(findings) != 0 || len(warnings) != 1 {
		t.Errorf("got %+v %v", findings, warnings)
	}
}

// The prompt carries the vocabulary, so adding a category cannot leave the model
// unaware of it.
func TestThePromptListsEveryCategory(t *testing.T) {
	prompt, err := Reviewer{}.prompt("review.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range categories {
		if !strings.Contains(prompt, c.ID) {
			t.Errorf("the prompt does not mention %s", c.ID)
		}
	}
	// Byte-stable: the category list is sorted, so the prompt does not change
	// between runs and a cached response stays valid.
	again, err := Reviewer{}.prompt("review.md")
	if err != nil || again != prompt {
		t.Error("the prompt is not stable across calls")
	}
}

// Wording specific to one codebase belongs in that codebase, not here.
func TestADestinationCanOverrideAPrompt(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".review/prompts/review.md", "Our own instructions. Categories:\n{{ .Categories }}")

	got, err := Reviewer{Root: root, PromptDir: ".review/prompts"}.prompt("review.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "Our own instructions.") {
		t.Errorf("the override was not used:\n%s", got)
	}
	if !strings.Contains(got, "correctness/bug") {
		t.Error("an override still gets the vocabulary")
	}
}

// What actually crosses the boundary. The assembled context has to reach the model
// or the review is about nothing.
func TestTheAssembledContextIsWhatIsSent(t *testing.T) {
	r, f := reviewer(reply())
	in := Input{Base: "origin/main", Diff: "@@ -1 +1 @@\n-old\n+new\n"}

	if _, _, err := r.Review(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	sent := f.Prompt(0)
	if !strings.Contains(sent, "+new") {
		t.Errorf("the diff did not reach the model:\n%s", sent)
	}
	if !strings.Contains(sent, "Only the changed lines") {
		t.Errorf("the system prompt did not reach the model:\n%s", sent)
	}
	if got := f.Calls()[0].Options; got.Temperature != 0 || !got.JSON || got.Model != "a-model" {
		t.Errorf("options = %+v", got)
	}
}
