// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// templateCodeowners is the shape docs/merge-classes.md tells a repository to
// use: `*` owned first, the allowlist unowned after it, and the agent skill file
// owned again last, because last match wins and a SKILL.md could otherwise be
// added inside the allowlist. Extra rules join the allowlist, from line 6.
func templateCodeowners(extra ...string) string {
	var allow strings.Builder
	for _, rule := range extra {
		allow.WriteString(rule + "\n")
	}
	return "# Every path is owned unless a later rule says otherwise.\n" +
		"* @org/maintainers\n" +
		"\n" +
		"# The documentation allowlist: directories people alone read.\n" +
		"/docs/guides/\n" +
		allow.String() +
		"\n" +
		"# Owned again wherever it appears, including inside the allowlist.\n" +
		"SKILL.md @org/maintainers\n"
}

// ownersRepo writes files into a new repository root.
func ownersRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func runOwnersCheck(t *testing.T, root string) (string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"owners", "check", "--root", root}, &stdout, &stderr, noEnv)
	return stdout.String(), err
}

// wantFailedCheck asserts a failed check (exit 1) whose output names every one
// of want, and returns the violation lines.
func wantFailedCheck(t *testing.T, out string, err error, want ...string) []string {
	t.Helper()
	var checkErr errCheckFailed
	if !errors.As(err, &checkErr) {
		t.Fatalf("want a failed check, got %v\n%s", err, out)
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("want the output to say %q, got:\n%s", w, out)
		}
	}
	var violations []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "  line ") || strings.HasPrefix(line, "  -  ") {
			violations = append(violations, line)
		}
	}
	return violations
}

func TestOwnersCheckPassesTheTemplate(t *testing.T) {
	root := ownersRepo(t, map[string]string{".github/CODEOWNERS": templateCodeowners()})
	out, err := runOwnersCheck(t, root)
	if err != nil {
		t.Fatalf("want a pass, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "passed") || !strings.HasSuffix(out, "ok\n") {
		t.Errorf("want the pass stated, got:\n%s", out)
	}
}

// Each rule of the lint, broken on its own in an otherwise passing file, gives
// exactly one violation, on the line that breaks it.
func TestOwnersCheckEachViolationAlone(t *testing.T) {
	for _, tc := range []struct {
		name     string
		location string
		text     string
		want     string
	}{
		{
			name:     "a line that does not parse",
			location: ".github/CODEOWNERS",
			text:     templateCodeowners("!docs/guides/drafts/ @org/maintainers"),
			want:     "CODEOWNERS line 6",
		},
		{
			name:     "the first rule is not *",
			location: ".github/CODEOWNERS",
			text:     "/src/ @org/backend\n" + templateCodeowners(),
			want:     "line 1    the first rule is \"/src/ @org/backend\"",
		},
		{
			name:     "CODEOWNERS does not own itself",
			location: "docs/CODEOWNERS",
			// /docs/ is unowned; docs/adr/ and SKILL.md are owned again, so
			// the file itself is the only never-reviewed path that matters.
			text: "* @org/maintainers\n/docs/\n/docs/adr/ @org/maintainers\nSKILL.md @org/maintainers\n",
			want: "line 2    CODEOWNERS does not own itself: docs/CODEOWNERS is left unowned by \"/docs/\"",
		},
		{
			name:     "an unowned rule that is not a directory",
			location: ".github/CODEOWNERS",
			text:     templateCodeowners("/docs/*.md"),
			want:     "line 6    \"/docs/*.md\" has no owners and is not a directory",
		},
		{
			// G5's own check: a branch that adds an unowned rule for .claude/.
			name:     "a never-allowlist path left unowned",
			location: ".github/CODEOWNERS",
			text:     templateCodeowners("/.claude/"),
			want:     "line 6    .claude/ (compiled in) is on the never-allowlist, and .claude/x is left unowned",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := ownersRepo(t, map[string]string{tc.location: tc.text})
			out, err := runOwnersCheck(t, root)
			violations := wantFailedCheck(t, out, err, tc.want)
			if len(violations) != 1 {
				t.Errorf("want one violation, got %d:\n%s", len(violations), out)
			}
		})
	}
}

// One run gives the whole fix list: every rule's violation is reported, not
// only the first.
func TestOwnersCheckReportsEveryViolationTogether(t *testing.T) {
	root := ownersRepo(t, map[string]string{".github/CODEOWNERS": strings.Join([]string{
		"/src/ @org/backend",
		"* @org/maintainers",
		"!docs/ @org/docs",
		"/docs/*.md",
		"/.claude/",
		"/.github/",
		"SKILL.md @org/maintainers",
	}, "\n") + "\n"})
	out, err := runOwnersCheck(t, root)
	violations := wantFailedCheck(t, out, err,
		"CODEOWNERS line 3",
		"the first rule is \"/src/ @org/backend\"",
		"CODEOWNERS does not own itself: .github/CODEOWNERS is left unowned by \"/.github/\"",
		"\"/docs/*.md\" has no owners and is not a directory",
		".claude/ (compiled in) is on the never-allowlist",
		".github/ (compiled in) is on the never-allowlist",
	)
	if len(violations) != 6 {
		t.Errorf("want six violations, got %d:\n%s", len(violations), out)
	}
	if !strings.Contains(err.Error(), "6 violations") {
		t.Errorf("want the count in the error, got %v", err)
	}
}

// A CODEOWNERS file added where GitHub looks first replaces the one in use, so
// those places must be owned too.
func TestOwnersCheckCoversWhereANewFileWouldTakeOver(t *testing.T) {
	root := ownersRepo(t, map[string]string{
		"CODEOWNERS": templateCodeowners("/.github/"),
	})
	out, err := runOwnersCheck(t, root)
	wantFailedCheck(t, out, err,
		"CODEOWNERS does not own .github/CODEOWNERS, where a new CODEOWNERS file would replace this one")
}

func TestOwnersCheckWithNoCODEOWNERSFails(t *testing.T) {
	root := ownersRepo(t, map[string]string{"README.md": "# hello\n"})
	out, err := runOwnersCheck(t, root)
	wantFailedCheck(t, out, err)
	if !strings.Contains(err.Error(), "no CODEOWNERS file at .github/CODEOWNERS, CODEOWNERS, docs/CODEOWNERS") {
		t.Errorf("want the places looked named, got %v", err)
	}
}

// With no configuration file, the compiled-in list applies, plus the default
// prompt directory the defaults name.
func TestOwnersCheckWithNoConfiguration(t *testing.T) {
	root := ownersRepo(t, map[string]string{".github/CODEOWNERS": templateCodeowners()})
	out, err := runOwnersCheck(t, root)
	if err != nil {
		t.Fatalf("want a pass, got %v\n%s", err, out)
	}
	want := fmt.Sprintf(", %d never-allowlist paths", len(neverAllowlist)+1)
	if !strings.Contains(out, want) {
		t.Errorf("want %q, got:\n%s", want, out)
	}
}

func TestOwnersCheckReadsTheNeverAllowlistFromConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, config, rule, want string
	}{
		{
			// CUJ 4.
			name:   "a guidance directory",
			config: "guidance:\n  - docs/guidance/\n",
			rule:   "/docs/guidance/",
			want:   "docs/guidance/ (guidance) is on the never-allowlist, and docs/guidance/x is left unowned by \"/docs/guidance/\"",
		},
		{
			name:   "a guidance file",
			config: "guidance:\n  - notes/review-guide.md\n",
			rule:   "/notes/",
			want:   "notes/review-guide.md (guidance) is on the never-allowlist",
		},
		{
			name:   "the prompt directory",
			config: "laneB:\n  promptDir: prompts\n",
			rule:   "/prompts/",
			want:   "prompts/ (laneB.promptDir) is on the never-allowlist, and prompts/x is left unowned",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := ownersRepo(t, map[string]string{
				".review/config.yaml": tc.config,
				".github/CODEOWNERS":  templateCodeowners(tc.rule),
			})
			out, err := runOwnersCheck(t, root)
			violations := wantFailedCheck(t, out, err, tc.want)
			if len(violations) != 1 {
				t.Errorf("want one violation, got %d:\n%s", len(violations), out)
			}
		})
	}
}

// A configuration the decoder refuses leaves the never-allowlist unknown. That
// is misuse, as it is for `rules test`, and it exits non-zero either way.
func TestOwnersCheckRefusesAnUnreadableConfiguration(t *testing.T) {
	root := ownersRepo(t, map[string]string{
		".review/config.yaml": "guidanse:\n  - docs/\n",
		".github/CODEOWNERS":  templateCodeowners(),
	})
	_, err := runOwnersCheck(t, root)
	var usageErr errUsage
	if !errors.As(err, &usageErr) {
		t.Fatalf("want a usage error, got %v", err)
	}
	if exitCode(err) != exitUsage {
		t.Errorf("exit %d, want %d", exitCode(err), exitUsage)
	}
}

func TestOwnersCheckProbesGoEmbedTargets(t *testing.T) {
	root := ownersRepo(t, map[string]string{
		"tool/tool.go":          "package tool\n\nimport \"embed\"\n\n//go:embed templates/*.tmpl\nvar templates embed.FS\n",
		"tool/templates/a.tmpl": "a\n",
		// Directories the go tool ignores, and other people's code, are not
		// scanned: their directives would point under an unowned rule too.
		"vendor/v/v.go":         "package v\n\n//go:embed files\nvar files string\n",
		"vendor/v/files/x":      "x\n",
		"tool/testdata/t.go":    "package t\n\n//go:embed files\nvar files string\n",
		"tool/testdata/files/x": "x\n",
		".github/CODEOWNERS":    templateCodeowners("/tool/templates/", "/vendor/", "/tool/testdata/"),
	})
	out, err := runOwnersCheck(t, root)
	violations := wantFailedCheck(t, out, err,
		"tool/templates/a.tmpl (//go:embed in tool/tool.go:5) is on the never-allowlist, and tool/templates/a.tmpl is left unowned by \"/tool/templates/\"")
	if len(violations) != 1 {
		t.Errorf("want one violation, got %d:\n%s", len(violations), out)
	}
}

func TestEmbedPatterns(t *testing.T) {
	got := embedPatterns(` a.txt "with space.txt"  ` + "`raw.txt`" + ` all:dist	tab.txt`)
	want := []string{"a.txt", "with space.txt", "raw.txt", "dist", "tab.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("embedPatterns: %q, want %q", got, want)
	}
}

// The probe paths for a pattern: what the lint asks CODEOWNERS about.
func TestInstantiate(t *testing.T) {
	for pattern, want := range map[string]string{
		"AGENTS.md":     "AGENTS.md",
		"**/SKILL.md":   "SKILL.md",
		"/docs/guides":  "docs/guides",
		"docs/*.md":     "docs/x.md",
		"docs/**":       "docs/x",
		"a/**/b":        "a/b",
		"*":             "x",
		"/docs/*/notes": "docs/x/notes",
	} {
		if got := instantiate(pattern); got != want {
			t.Errorf("instantiate(%q) = %q, want %q", pattern, got, want)
		}
	}
}

// The skill file is on the never-allowlist at any depth, so an allowlist
// directory that does not own it again fails.
func TestOwnersCheckFindsASkillFileInsideTheAllowlist(t *testing.T) {
	root := ownersRepo(t, map[string]string{
		".github/CODEOWNERS": "* @org/maintainers\n/docs/guides/\n",
	})
	out, err := runOwnersCheck(t, root)
	wantFailedCheck(t, out, err,
		"**/SKILL.md (compiled in) is on the never-allowlist, and docs/guides/SKILL.md is left unowned by \"/docs/guides/\"")
}

// The status each outcome exits with: a failed check is 1, distinct from a
// usage error's 2 and from a review's 0.
func TestOwnersCheckExitCodes(t *testing.T) {
	pass := ownersRepo(t, map[string]string{".github/CODEOWNERS": templateCodeowners()})
	_, err := runOwnersCheck(t, pass)
	if got := exitCode(err); err != nil || got != 0 {
		t.Errorf("a passing file: %v, exit %d; want nil and 0", err, got)
	}

	fail := ownersRepo(t, map[string]string{".github/CODEOWNERS": templateCodeowners("/.review/")})
	_, err = runOwnersCheck(t, fail)
	if got := exitCode(err); got != exitCheckFailed || exitCheckFailed != 1 {
		t.Errorf("a failing file: %v, exit %d; want 1", err, got)
	}

	missing := ownersRepo(t, nil)
	_, err = runOwnersCheck(t, missing)
	if got := exitCode(err); got != exitCheckFailed {
		t.Errorf("no CODEOWNERS: %v, exit %d; want 1", err, got)
	}
}

func TestOwnersNeedsTheCheckVerb(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"owners"}, &stdout, &stderr, noEnv)
	var usageErr errUsage
	if !errors.As(err, &usageErr) || !strings.Contains(err.Error(), "owners needs a verb: check") {
		t.Fatalf("want a usage error naming the verb, got %v", err)
	}
	err = run(context.Background(), []string{"owners", "lint"}, &stdout, &stderr, noEnv)
	if !errors.As(err, &usageErr) || !strings.Contains(err.Error(), `owners "lint" is not a thing; try check`) {
		t.Fatalf("want an unknown verb rejected, got %v", err)
	}
	o, err := parse([]string{"owners", "check", "--root", "x"}, &stderr)
	if err != nil || o.subcommand != "owners check" || o.root != "x" {
		t.Fatalf("want `owners check --root x` parsed, got %+v %v", o, err)
	}
}

// When a pull request's author is the only code owner of what it changes,
// GitHub asks for no review, even with code-owner review required (Q1 in the
// design, observed in a scratch repository). A person may not own anything; a
// team may.
func TestOwnersCheckRejectsAPersonAsOwner(t *testing.T) {
	root := ownersRepo(t, map[string]string{
		".github/CODEOWNERS": "* @alice\n/docs/guides/\n/src/billing/ ops@example.com\nSKILL.md @org/maintainers\n",
	})
	out, err := runOwnersCheck(t, root)
	got := wantFailedCheck(t, out, err, "@alice, a person rather than a team", "ops@example.com, a person")
	if len(got) != 2 {
		t.Errorf("want 2 violations, got %d:\n%s", len(got), strings.Join(got, "\n"))
	}
}

// GitHub merges a symbolic link added under the unowned allowlist with no
// review, wherever it points (Q1). The lint runs on the pull request's tree, so
// such a link fails it. A link on an owned path is someone's to review.
func TestOwnersCheckRejectsASymlinkOnAnUnownedPath(t *testing.T) {
	root := ownersRepo(t, map[string]string{
		".github/CODEOWNERS": templateCodeowners(),
		"src/index.ts":       "export const answer = 42;\n",
		"docs/guides/a.md":   "# A\n",
	})
	if err := os.Symlink("../../src/index.ts", filepath.Join(root, "docs/guides/link.ts")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("index.ts", filepath.Join(root, "src/alias.ts")); err != nil {
		t.Fatal(err)
	}
	out, err := runOwnersCheck(t, root)
	got := wantFailedCheck(t, out, err, `docs/guides/link.ts is a symbolic link to "../../src/index.ts"`)
	if len(got) != 1 {
		t.Errorf("want 1 violation (the owned link is fine), got %d:\n%s", len(got), strings.Join(got, "\n"))
	}
}
