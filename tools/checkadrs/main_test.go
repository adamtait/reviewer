// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const wellFormed = `<!-- SPDX-License-Identifier: MIT -->
# ADR-0007 — Do the thing

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-07
- **Supersedes:** —
- **Superseded by:** —

## Context

Because.

## Decision

We do the thing.
`

func TestParseADR(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		content string
		want    string // substring of the expected violation; "" means none
	}{
		{"well formed", "0007-do-the-thing.md", wellFormed, ""},
		{
			"bad status",
			"0007-do-the-thing.md",
			strings.Replace(wellFormed, "**Status:** Accepted", "**Status:** Maybe", 1),
			`Status is "Maybe"`,
		},
		{
			"missing implemented by",
			"0007-do-the-thing.md",
			strings.Replace(wellFormed, "- **Implemented by:** PR-07\n", "", 1),
			"missing 'Implemented by' field",
		},
		{
			"heading disagrees with filename",
			"0008-do-the-thing.md",
			wellFormed,
			"heading says ADR-0007 but the filename says 0008",
		},
		{
			"missing heading",
			"0007-do-the-thing.md",
			strings.Replace(wellFormed, "# ADR-0007 — Do the thing", "# Do the thing", 1),
			"missing or malformed",
		},
		{
			"superseded without a successor",
			"0007-do-the-thing.md",
			strings.Replace(wellFormed, "**Status:** Accepted", "**Status:** Superseded", 1),
			"no 'Superseded by' ADR is named",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, violations := parseADR(tc.file, tc.content)
			got := strings.Join(violations, "\n")
			if tc.want == "" {
				if len(violations) != 0 {
					t.Fatalf("want no violations, got:\n%s", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("want a violation containing %q, got:\n%s", tc.want, got)
			}
		})
	}
}

func TestBodyOfIgnoresTheMetadataBlock(t *testing.T) {
	marked := strings.Replace(wellFormed, "**Status:** Accepted", "**Status:** Superseded", 1)
	marked = strings.Replace(marked, "**Superseded by:** —", "**Superseded by:** ADR-0009", 1)
	if bodyOf(marked) != bodyOf(wellFormed) {
		t.Fatal("changing the status block changed the body; marking an ADR superseded must not trip the append-only rule")
	}
	edited := strings.Replace(wellFormed, "We do the thing.", "We do a different thing.", 1)
	if bodyOf(edited) == bodyOf(wellFormed) {
		t.Fatal("editing the Decision section did not change the body; the append-only rule would not fire")
	}
}

func TestCheckNumbering(t *testing.T) {
	dup := []adr{{num: 0, file: "0000-a.md"}, {num: 0, file: "0000-b.md"}}
	if got := checkNumbering(dup); len(got) != 1 || !strings.Contains(got[0], "duplicate ADR number") {
		t.Fatalf("want a duplicate-number violation, got %v", got)
	}
	// Gaps are expected: numbers are reserved by the implementation plan and
	// land with the PR that implements each decision.
	gap := []adr{{num: 0, file: "0000-a.md"}, {num: 26, file: "0026-c.md"}}
	if got := checkNumbering(gap); len(got) != 0 {
		t.Fatalf("gaps in ADR numbering are legitimate, got %v", got)
	}
	ok := []adr{{num: 0, file: "0000-a.md"}, {num: 1, file: "0001-b.md"}}
	if got := checkNumbering(ok); len(got) != 0 {
		t.Fatalf("want no violations, got %v", got)
	}
}

func TestCheckSupersession(t *testing.T) {
	oneWay := []adr{
		{num: 0, file: "0000-a.md", supersededBy: []int{1}},
		{num: 1, file: "0001-b.md"},
	}
	got := checkSupersession(oneWay)
	if len(got) != 1 || !strings.Contains(got[0], "one-way supersession") {
		t.Fatalf("want a one-way supersession violation, got %v", got)
	}

	dangling := []adr{{num: 0, file: "0000-a.md", supersededBy: []int{9}}}
	if got := checkSupersession(dangling); len(got) != 1 || !strings.Contains(got[0], "does not exist") {
		t.Fatalf("want a dangling-reference violation, got %v", got)
	}

	twoWay := []adr{
		{num: 0, file: "0000-a.md", supersededBy: []int{1}},
		{num: 1, file: "0001-b.md", supersedes: []int{0}},
	}
	if got := checkSupersession(twoWay); len(got) != 0 {
		t.Fatalf("want no violations for a two-way link, got %v", got)
	}
}

func TestCheckIndex(t *testing.T) {
	adrs := []adr{{num: 0, file: "0000-a.md", title: "Alpha", status: "Accepted"}}

	index := "| [0000](0000-a.md) | Alpha | Accepted | PR-00 |"
	if got := checkIndex(adrs, index); len(got) != 0 {
		t.Fatalf("want no violations, got %v", got)
	}
	if got := checkIndex(adrs, ""); len(got) != 1 || !strings.Contains(got[0], "not listed") {
		t.Fatalf("want a missing-entry violation, got %v", got)
	}
	stale := "| [0000](0000-a.md) | Alfa | Accepted | PR-00 |"
	if got := checkIndex(adrs, stale); len(got) != 1 || !strings.Contains(got[0], "index title") {
		t.Fatalf("want a title-mismatch violation, got %v", got)
	}
	orphan := index + "\n| [0004](0004-ghost.md) | Ghost | Accepted | PR-04 |"
	if got := checkIndex(adrs, orphan); len(got) != 1 || !strings.Contains(got[0], "which has no file") {
		t.Fatalf("want an orphan-row violation, got %v", got)
	}
}

// TestAppendOnlyAgainstAGitRepo exercises the rule end to end, because the
// interesting failures live in the git plumbing rather than the comparison:
// an untrimmed ref makes every record look new and the check silently passes.
func TestAppendOnlyAgainstAGitRepo(t *testing.T) {
	dir := t.TempDir()
	adrDir := filepath.Join(dir, "docs", "adr")
	if err := os.MkdirAll(adrDir, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(adrDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")

	write("0000-do-the-thing.md", strings.Replace(wellFormed, "ADR-0007", "ADR-0000", 1))
	write("README.md", "| [0000](0000-do-the-thing.md) | Do the thing | Accepted | PR-00 |\n")
	run("add", ".")
	run("commit", "-q", "-m", "accept the decision")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	violations, count, err := check("docs/adr", "main")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || len(violations) != 0 {
		t.Fatalf("unedited record should be clean, got %d ADRs and %v", count, violations)
	}

	// Marking it Superseded rewrites the status block only, which is allowed.
	superseded := strings.Replace(wellFormed, "ADR-0007", "ADR-0000", 1)
	superseded = strings.Replace(superseded, "**Status:** Accepted", "**Status:** Superseded", 1)
	superseded = strings.Replace(superseded, "**Superseded by:** —", "**Superseded by:** ADR-0001", 1)
	write("0000-do-the-thing.md", superseded)
	violations, _, err = check("docs/adr", "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		if strings.Contains(v, "outside its status block") {
			t.Fatalf("marking a record superseded must not trip the append-only rule, got %v", violations)
		}
	}

	// Editing the Decision section is not.
	write("0000-do-the-thing.md", strings.Replace(
		strings.Replace(wellFormed, "ADR-0007", "ADR-0000", 1),
		"We do the thing.", "We do a different thing.", 1))
	violations, _, err = check("docs/adr", "main")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range violations {
		if strings.Contains(v, "outside its status block") {
			found = true
		}
	}
	if !found {
		t.Fatalf("editing an accepted record's body must be a violation, got %v", violations)
	}
}
