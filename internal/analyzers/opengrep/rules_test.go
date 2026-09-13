// SPDX-License-Identifier: MIT

package opengrep

import (
	"path/filepath"
	"strings"
	"testing"
)

// The exit criterion for the example pack, checked in CI. `rules test` cannot prove
// it here — Opengrep is not installable in this environment — but the half that is
// this project's own rule rather than Opengrep's is checkable without it: every rule
// loads, no two share an id, and each has both a matching and a non-matching case.
func TestTheExampleRulePackIsComplete(t *testing.T) {
	pack, err := LoadPack(repoRoot(t), filepath.Join("examples", "rules"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Problems) != 0 {
		t.Fatalf("the example pack does not load cleanly: %v", pack.Problems)
	}
	if len(pack.Rules) != 3 {
		t.Fatalf("want three example rules, got %d", len(pack.Rules))
	}
	if untested := pack.Untested(); len(untested) != 0 {
		t.Errorf("every example rule must ship both cases:\n  %s", strings.Join(untested, "\n  "))
	}

	for _, rule := range pack.Rules {
		if rule.Message == "" {
			t.Errorf("%s has no message: a finding nobody can act on", rule.ID)
		}
		// A message that only says no is a rule people route around. Every example
		// has to model the thing we want people to copy.
		if len(rule.Message) < 60 {
			t.Errorf("%s's message is too short to carry a reason: %q", rule.ID, rule.Message)
		}
		if rule.Severity == "" {
			t.Errorf("%s declares no severity", rule.ID)
		}
		// The examples pin their ids, which is the practice the docs recommend:
		// Opengrep derives check_id from the file, so moving a rule would otherwise
		// reset its acceptance history.
		if !strings.Contains(rule.ReportedID, "/") {
			t.Errorf("%s is reported as %q, which is not namespaced", rule.ID, rule.ReportedID)
		}
	}
	if pack.Cases() < 6 {
		t.Errorf("want at least two cases per rule, got %d", pack.Cases())
	}
}

func TestLoadPackReportsADuplicateID(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "rules/a.yaml", "rules:\n  - id: same\n    message: first\n")
	writeFile(t, root, "rules/b.yaml", "rules:\n  - id: same\n    message: second\n")

	pack, err := LoadPack(root, "rules")
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Problems) != 1 || !strings.Contains(pack.Problems[0], "already declared in a.yaml") {
		t.Fatalf("want the colliding files named, got %v", pack.Problems)
	}
	// The first declaration still loads: one duplicate must not take the rest of
	// the pack down with it, which is what delegating this check to Opengrep would
	// have done.
	if len(pack.Rules) != 1 {
		t.Errorf("want the first rule kept, got %d", len(pack.Rules))
	}
}

func TestLoadPackReportsAMalformedFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "rules/broken.yaml", "rules:\n  - id: x\n   bad indent\n")
	writeFile(t, root, "rules/empty.yaml", "# nothing here\n")
	writeFile(t, root, "rules/nameless.yaml", "rules:\n  - message: no id\n")
	writeFile(t, root, "rules/fine.yaml", "rules:\n  - id: ok\n    message: fine\n")

	pack, err := LoadPack(root, "rules")
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Problems) != 3 {
		t.Fatalf("want one problem per broken file, got %v", pack.Problems)
	}
	// A rule file that failed to parse is a convention nobody is checking, and a
	// run that skipped it silently would look identical to one where it passed.
	if len(pack.Rules) != 1 || pack.Rules[0].ID != "ok" {
		t.Errorf("want the sound rule still loaded, got %+v", pack.Rules)
	}
}

func TestUntestedNamesWhichHalfIsMissing(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "rules/positive-only.yaml", "rules:\n  - id: positive-only\n    message: m\n")
	writeFile(t, root, "rules/positive-only.ts", "// ruleid: positive-only\nconst a = 1;\n")
	writeFile(t, root, "rules/negative-only.yaml", "rules:\n  - id: negative-only\n    message: m\n")
	writeFile(t, root, "rules/negative-only.ts", "// ok: negative-only\nconst b = 2;\n")
	writeFile(t, root, "rules/no-file.yaml", "rules:\n  - id: no-file\n    message: m\n")

	pack, err := LoadPack(root, "rules")
	if err != nil {
		t.Fatal(err)
	}
	untested := strings.Join(pack.Untested(), "\n")
	for _, want := range []string{
		"no-file (no-file.yaml): no test file",
		"negative-only",
		"positive-only",
		"no `ok:` case",
		"no `ruleid:` case",
	} {
		if !strings.Contains(untested, want) {
			t.Errorf("want %q reported:\n%s", want, untested)
		}
	}
}

func TestCountAnnotations(t *testing.T) {
	body := strings.Join([]string{
		"// ruleid: a",
		"# ruleid: a, b",
		"  // ok: a",
		"/* ok: b */",
		"// todoruleid: a",
		"// ruleid: other",
		"const ruleid = 'a';",
	}, "\n")

	positive, negative := countAnnotations(body, "a")
	if positive != 2 || negative != 1 {
		t.Errorf("for a: %d positive, %d negative", positive, negative)
	}
	// todoruleid marks a case that is known not to work yet. Counting it as
	// coverage would let a rule ship with its only case disabled.
	if strings.Contains(body, "todoruleid") && positive > 2 {
		t.Error("todoruleid must not count as coverage")
	}
	positive, negative = countAnnotations(body, "b")
	if positive != 1 || negative != 1 {
		t.Errorf("for b: %d positive, %d negative", positive, negative)
	}
}

func TestReportedID(t *testing.T) {
	for _, tc := range []struct{ id, declared, want string }{
		{id: "plain", want: "conventions/plain"},
		{id: "arch/already-namespaced", want: "arch/already-namespaced"},
		{id: "plain", declared: "arch/renamed", want: "arch/renamed"},
		{id: "plain", declared: "  ", want: "conventions/plain"},
	} {
		if got := reportedID(tc.id, tc.declared); got != tc.want {
			t.Errorf("reportedID(%q, %q) = %q, want %q", tc.id, tc.declared, got, tc.want)
		}
	}
}
