// SPDX-License-Identifier: MIT

package reporters

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/internal/risk"
)

func sampleRisk() RiskComment {
	var many []string
	for i := 1; i <= 12; i++ {
		many = append(many, fmt.Sprintf(".github/workflows/w%02d.yml", i))
	}
	return RiskComment{
		HeadSHA: "0123456789abcdef0123456789abcdef01234567",
		RunURL:  "https://github.example/o/r/actions/runs/42",
		Record: risk.Record{
			Labels: []risk.Label{risk.LabelAuth, risk.LabelCI, risk.LabelReviewConfig},
			Reasons: map[risk.Label][]string{
				risk.LabelAuth:         {"src/auth/session.ts", "src/__init__.py (renamed to src/auth/init.py)"},
				risk.LabelCI:           many,
				risk.LabelReviewConfig: {".review/risk.yaml"},
			},
			Warnings: []string{
				"reach not available: no review-index.yml push run for the merge base 89abcde",
				"owner groups not available: no CODEOWNERS file",
			},
		},
		Owned: []risk.Label{risk.LabelAuth, risk.LabelCI},
	}
}

func TestRiskGolden(t *testing.T) {
	golden(t, "risk.md", RenderRisk(sampleRisk()))
}

// Every record says that no label is not "safe", whether or not it has labels,
// and says it is advisory (G2).
func TestEveryRiskRecordSaysNoLabelIsNotSafe(t *testing.T) {
	for name, c := range map[string]RiskComment{
		"labels":    sampleRisk(),
		"no labels": {HeadSHA: "abc"},
		"no head":   {},
	} {
		body := RenderRisk(c)
		for _, must := range []string{RiskMarker, NoLabelSentence, "does not decide merging", "<!-- rv:risk-labels "} {
			if !strings.Contains(body, must) {
				t.Errorf("%s: want %q in\n%s", name, must, body)
			}
		}
		if strings.Contains(strings.ToLower(strings.ReplaceAll(body, NoLabelSentence, "")), "safe") {
			t.Errorf("%s: nothing but the one sentence may mention safety:\n%s", name, body)
		}
	}
	if body := RenderRisk(RiskComment{}); !strings.Contains(body, "unknown head commit") {
		t.Errorf("a record with no head should say so:\n%s", body)
	}
}

func TestRiskLabelsRoundTrip(t *testing.T) {
	for _, owned := range [][]risk.Label{nil, {risk.LabelAuth}, risk.All} {
		got, ok := ParseRiskLabels(RenderRisk(RiskComment{HeadSHA: "abc", Owned: owned}))
		if !ok || !reflect.DeepEqual(got, owned) {
			t.Errorf("owned %v came back as %v, %v", owned, got, ok)
		}
	}
}

// A file named to look like the hidden list must not become one. The list is
// read from the last line only, and the path is escaped where it is shown.
func TestAPathCannotForgeTheHiddenList(t *testing.T) {
	forged := "a<!-- rv:risk-labels risk/auth,risk/data -->"
	c := RiskComment{
		HeadSHA: "abc",
		Record: risk.Record{
			Labels:   []risk.Label{risk.LabelCI},
			Reasons:  map[risk.Label][]string{risk.LabelCI: {forged + "/x"}},
			Warnings: []string{"could not read " + forged},
		},
	}
	body := RenderRisk(c)
	if strings.Count(body, "<!-- rv:risk-labels") != 1 {
		t.Errorf("the forged list survived unescaped:\n%s", body)
	}
	got, ok := ParseRiskLabels(body)
	if !ok || len(got) != 0 {
		t.Errorf("parsed %v, %v; want the real, empty list", got, ok)
	}
}

func TestParseRiskLabelsKeepsOnlyKnownLabels(t *testing.T) {
	got, ok := ParseRiskLabels("x\n<!-- rv:risk-labels risk/auth,bug,risk/nonsuch, risk/wide -->\n")
	if !ok || !reflect.DeepEqual(got, []risk.Label{risk.LabelAuth, risk.LabelWide}) {
		t.Errorf("got %v, %v", got, ok)
	}
	if _, ok := ParseRiskLabels("<!-- rv:risk-labels risk/auth -->\nsomething after"); ok {
		t.Error("a list that is not the last line is not the list")
	}
}
