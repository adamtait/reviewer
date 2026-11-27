// SPDX-License-Identifier: MIT

package risk

import "testing"

// The closed set (G2). These are the nine names in the design's label table,
// written out here rather than taken from the constants, so that adding a
// label to All, or renaming one, fails this test and needs a change to the
// design and to this list in the same commit.
var fixedLabels = []string{
	"risk/auth",
	"risk/data",
	"risk/ci",
	"risk/deps",
	"risk/infra",
	"risk/wide",
	"risk/reach",
	"risk/history",
	"risk/review-config",
}

func TestAllHoldsExactlyTheFixedLabels(t *testing.T) {
	fixed := map[string]bool{}
	for _, name := range fixedLabels {
		fixed[name] = true
	}
	seen := map[Label]bool{}
	for _, l := range All {
		if !fixed[string(l)] {
			t.Errorf("All holds %q, which is not one of the fixed labels", l)
		}
		if seen[l] {
			t.Errorf("All holds %q twice", l)
		}
		seen[l] = true
	}
	if len(All) != len(fixedLabels) {
		t.Errorf("All holds %d labels, want the %d fixed ones", len(All), len(fixedLabels))
	}
}

func TestPathLabelsAreLabels(t *testing.T) {
	for _, l := range PathLabels {
		found := false
		for _, a := range All {
			found = found || a == l
		}
		if !found {
			t.Errorf("path label %q is not in All", l)
		}
		if len(defaultPatterns[l]) == 0 {
			t.Errorf("path label %q has no default patterns", l)
		}
	}
	if len(defaultPatterns) != len(PathLabels) {
		t.Errorf("defaults are defined for %d labels, want the %d path labels", len(defaultPatterns), len(PathLabels))
	}
}
