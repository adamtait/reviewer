// SPDX-License-Identifier: MIT

package fingerprint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/pkg/finding"
)

func repo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		target := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func at(file string, line, endLine int) finding.Finding {
	return finding.Finding{RuleID: "arch/no-domain-to-infra", File: file, Line: line, EndLine: endLine}
}

// The whole reason this package exists: a rebase shifts every line number, and
// identity must survive it.
func TestIdentitySurvivesALineShift(t *testing.T) {
	before := repo(t, map[string]string{"a.ts": "import x from 'y';\nconst a = 1;\n"})
	after := repo(t, map[string]string{"a.ts": "// a new header comment\n// and another\nimport x from 'y';\nconst a = 1;\n"})

	if got, want := Compute(after, at("a.ts", 3, 0)), Compute(before, at("a.ts", 1, 0)); got != want {
		t.Fatalf("the same line at a different position must keep its identity: %s vs %s", got, want)
	}
}

func TestIdentitySurvivesReformatting(t *testing.T) {
	tight := repo(t, map[string]string{"a.ts": "const a = call(x,y);\n"})
	spaced := repo(t, map[string]string{"a.ts": "    const a = call(x,  y);\n"})

	if got, want := Compute(spaced, at("a.ts", 1, 0)), Compute(tight, at("a.ts", 1, 0)); got != want {
		t.Fatalf("reindenting must not orphan a comment: %s vs %s", got, want)
	}
}

func TestIdentityChangesWhenTheCodeChanges(t *testing.T) {
	original := repo(t, map[string]string{"a.ts": "if (a && b) return;\n"})
	edited := repo(t, map[string]string{"a.ts": "if (a || b) return;\n"})

	if Compute(edited, at("a.ts", 1, 0)) == Compute(original, at("a.ts", 1, 0)) {
		t.Fatal("changing an operator must change identity, so the old comment is replaced rather than carried forward")
	}
}

func TestIdentityIsPerRuleAndPerFile(t *testing.T) {
	root := repo(t, map[string]string{"a.ts": "const a = 1;\n", "b.ts": "const a = 1;\n"})

	base := Compute(root, at("a.ts", 1, 0))

	otherFile := Compute(root, at("b.ts", 1, 0))
	if base == otherFile {
		t.Fatal("the same line in a different file is a different finding")
	}

	otherRule := at("a.ts", 1, 0)
	otherRule.RuleID = "logic/no-floating-promises"
	if base == Compute(root, otherRule) {
		t.Fatal("a different rule on the same line is a different finding")
	}
}

// Rewording a rule's message must not orphan every comment it has ever posted.
func TestMessageIsNotPartOfIdentity(t *testing.T) {
	root := repo(t, map[string]string{"a.ts": "const a = 1;\n"})
	a, b := at("a.ts", 1, 0), at("a.ts", 1, 0)
	a.Message, b.Message = "old wording", "new, clearer wording"
	if Compute(root, a) != Compute(root, b) {
		t.Fatal("the message must not affect identity")
	}
}

// Field separation: a rule id and a path that concatenate to the same bytes as a
// different pair must not collide.
func TestFieldsCannotRunTogether(t *testing.T) {
	root := repo(t, map[string]string{"x.ts": "const a = 1;\n"})
	one, two := at("x.ts", 1, 0), at("x.ts", 1, 0)
	one.RuleID, two.RuleID = "arch/a", "arch"
	two.File = "a/x.ts"
	if Compute(root, one) == Compute(root, two) {
		t.Fatal("fields must be separated in the digest")
	}
}

// An unreadable file still yields a stable id: a finding that cannot be deduped
// beats a finding that cannot be posted.
func TestAMissingFileStillYieldsAStableIdentity(t *testing.T) {
	root := t.TempDir()
	first, second := Compute(root, at("gone.ts", 4, 0)), Compute(root, at("gone.ts", 9, 0))
	if first == "" {
		t.Fatal("want a fingerprint even with no readable source")
	}
	if first != second {
		t.Fatal("with no snippet the id depends only on rule and path, so it must not vary by line")
	}
}

func TestMultiLineSpans(t *testing.T) {
	root := repo(t, map[string]string{"a.ts": "one\ntwo\nthree\nfour\n"})
	if Compute(root, at("a.ts", 2, 3)) == Compute(root, at("a.ts", 2, 0)) {
		t.Fatal("a two-line span is not the same finding as a one-line span")
	}
	// A span running past the end of the file must not panic.
	if Compute(root, at("a.ts", 3, 99)) == "" {
		t.Fatal("an over-long span must still produce an id")
	}
}

func TestMarkerRoundTrip(t *testing.T) {
	fp := "a3f9c2"
	body := "Some review text.\n" + Marker(fp) + "\n"
	if got := ParseMarker(body); got != fp {
		t.Fatalf("want %q, got %q", fp, got)
	}
	// A human's comment has no marker, which is how the tool avoids touching it.
	if got := ParseMarker("Looks good to me!"); got != "" {
		t.Fatalf("want no fingerprint from a human comment, got %q", got)
	}
	if !strings.Contains(Marker(fp), "<!--") {
		t.Fatal("the marker must be invisible in rendered markdown")
	}
}

func TestFingerprintLength(t *testing.T) {
	root := repo(t, map[string]string{"a.ts": "const a = 1;\n"})
	if got := Compute(root, at("a.ts", 1, 0)); len(got) != Length {
		t.Fatalf("want %d characters, got %q", Length, got)
	}
}
