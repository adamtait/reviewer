// SPDX-License-Identifier: MIT

package osv

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/pkg/plugin"
)

// The exit criterion: a pull request that touches no manifest must not spawn the
// scanner at all. It is the slowest analyzer here and the only one that needs the
// network, so "no lockfile changed" has to cost nothing.
func TestNoLockfileChangedNeverSpawnsTheScanner(t *testing.T) {
	root := t.TempDir()
	// A binary that fails loudly if it is ever run.
	binary := script(t, "#!/bin/sh\necho 'spawned' >&2\nexit 1\n")

	findings, warnings, err := Analyze(context.Background(), plugin.AnalyzeRequest{
		Root: root,
		Changed: []plugin.ChangedFile{
			{Path: "src/index.ts"},
			{Path: "README.md"},
			// A file that merely mentions a lockfile name is not one.
			{Path: "docs/package-lock.json.md"},
		},
	}, binary)

	if err != nil || len(findings) != 0 || len(warnings) != 0 {
		t.Fatalf("want a silent no-op, got %v %v %v", findings, warnings, err)
	}
}

func TestChangedLockfilesArePickedOutOfTheDiff(t *testing.T) {
	got := changedLockfiles([]plugin.ChangedFile{
		{Path: "src/index.ts"},
		{Path: "package-lock.json"},
		{Path: "packages/api/pnpm-lock.yaml"},
		{Path: "go.sum"},
		{Path: "package-lock.json"},
		{Path: "vendor/notalockfile.json"},
	})
	if strings.Join(got, ",") != "go.sum,package-lock.json,packages/api/pnpm-lock.yaml" {
		t.Errorf("changedLockfiles() = %v", got)
	}
}

// The defect this analyzer is shaped around, asserted against the real binary's
// actual behaviour. osv-scanner prints `{"results": []}` and exits 127 when the
// vulnerability database is unreachable, so stdout alone says "clean" for a scan
// that never happened. Verified against osv-scanner 2.5.1.
func TestAnUnreachableDatabaseIsUnavailableRatherThanClean(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "package-lock.json", "{}")
	binary := script(t, "#!/bin/sh\n"+
		`printf '{"results": []}'`+"\n"+
		"echo 'Error during extraction: request failed: Post \"https://api.osv.dev/v1/querybatch\": Forbidden' >&2\n"+
		"exit 127\n")

	_, _, err := Analyze(context.Background(), request(root, "package-lock.json"), binary)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("an empty report with a 127 exit is a scan that did not happen, got %v", err)
	}
	if !strings.Contains(err.Error(), "api.osv.dev") {
		t.Errorf("want the tool's own reason carried through, got %v", err)
	}
}

// Only what this pull request introduced. A lockfile carries advisories nobody in
// this change caused and nobody in it can fix; reporting those is how a tool
// teaches a team to ignore it.
func TestOnlyIntroducedAdvisoriesAreReported(t *testing.T) {
	root := gitRepo(t)
	// Committed first: this is the lockfile as it was.
	writeFile(t, root, "package-lock.json", "before\n"+multiLockfile)
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "package-lock.json", multiLockfile)

	// The stand-in answers differently depending on which file it is handed: the
	// working tree copy has two advisories, the base copy has one of them.
	binary := script(t, "#!/bin/sh\n"+
		"for arg in \"$@\"; do case \"$arg\" in */package-lock.json) target=\"$arg\";; esac; done\n"+
		"if grep -q before \"$target\" 2>/dev/null; then\n"+
		`  printf '%s' '`+baseReport+`'`+"\n"+
		"else\n"+
		`  printf '%s' '`+headReport+`'`+"\n"+
		"fi\nexit 1\n")

	req := request(root, "package-lock.json")
	findings, _, err := Analyze(context.Background(), req, binary)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("want only the new advisory, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if !strings.Contains(f.Message, "GHSA-new") || !strings.Contains(f.Message, "minimist@1.2.0") {
		t.Errorf("message = %q", f.Message)
	}
	if f.RuleID != "deps/npm" {
		t.Errorf("RuleID = %q", f.RuleID)
	}
	// The finding has to land on a line the change touched, or the core's diff
	// filter drops it and the analyzer reports into a void (ADR-0007).
	if f.File != "package-lock.json" {
		t.Errorf("file = %s", f.File)
	}
	if !onAChangedLine(req, f.File, f.Line) {
		t.Errorf("line %d is outside the diff, where the core drops it", f.Line)
	}
	if err := f.Validate(); err != nil {
		t.Errorf("the finding does not validate: %v", err)
	}
}

// A lockfile this pull request adds has no previous version, and everything in it
// is therefore introduced by it.
func TestANewLockfileIsEntirelyIntroduced(t *testing.T) {
	root := gitRepo(t)
	writeFile(t, root, "README.md", "x\n")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "package-lock.json", multiLockfile)

	binary := script(t, "#!/bin/sh\nprintf '%s' '"+headReport+"'\nexit 1\n")

	findings, warnings, err := Analyze(context.Background(), request(root, "package-lock.json"), binary)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("want both advisories, got %d: %+v", len(findings), findings)
	}
	for _, w := range warnings {
		if strings.Contains(w, "could not read the previous") {
			t.Errorf("a new lockfile is not a failure to read the old one: %v", warnings)
		}
	}
}

// A clean head scan must not pay for a base scan: that is a second network round
// trip for a question nobody asked.
func TestACleanHeadSkipsTheBaseScan(t *testing.T) {
	root := gitRepo(t)
	writeFile(t, root, "package-lock.json", "before\n"+multiLockfile)
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "package-lock.json", multiLockfile)

	calls := filepath.Join(t.TempDir(), "calls")
	binary := script(t, "#!/bin/sh\necho call >> "+calls+"\nprintf '{\"results\": []}'\nexit 0\n")

	findings, _, err := Analyze(context.Background(), request(root, "package-lock.json"), binary)
	if err != nil || len(findings) != 0 {
		t.Fatalf("want a clean result, got %v %v", findings, err)
	}
	if body, _ := os.ReadFile(calls); strings.Count(string(body), "call") != 1 {
		t.Errorf("want exactly one scan, got %q", body)
	}
}

func TestIntroducedTreatsAVersionBumpAsNew(t *testing.T) {
	base := []advisory{{vulnID: "GHSA-1", pkg: "lodash", version: "4.17.15", ecosystem: "npm"}}
	head := []advisory{
		// Same advisory, same version: carried over, not this pull request's.
		{vulnID: "GHSA-1", pkg: "lodash", version: "4.17.15", ecosystem: "npm"},
		// Same advisory, a version this pull request chose: introduced.
		{vulnID: "GHSA-1", pkg: "lodash", version: "4.17.16", ecosystem: "npm"},
	}
	got := introduced(head, base)
	if len(got) != 1 || got[0].version != "4.17.16" {
		t.Fatalf("want the chosen version reported, got %+v", got)
	}
}

// The real binary, if it is installed. The network is usually unreachable in CI,
// which is precisely the case the analyzer must not report as clean.
func TestAgainstTheRealScanner(t *testing.T) {
	if _, err := exec.LookPath(Binary); err != nil {
		t.Skip("osv-scanner is not installed")
	}
	root := gitRepo(t)
	writeFile(t, root, "package-lock.json", realLockfile)
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "package-lock.json", strings.Replace(realLockfile, "1.2.0", "1.2.1", 2))

	_, _, err := Analyze(context.Background(), request(root, "package-lock.json"), "")
	switch {
	case errors.Is(err, ErrUnavailable):
		// The database was unreachable and the analyzer said so rather than
		// reporting every dependency clean. That is the behaviour under test.
		t.Logf("database unreachable, correctly reported: %v", err)
	case err != nil:
		t.Fatalf("unexpected error: %v", err)
	default:
		t.Log("database reachable; the scan completed")
	}
}

const headReport = `{"results":[{"packages":[` +
	`{"package":{"name":"lodash","version":"4.17.15","ecosystem":"npm"},"vulnerabilities":[{"id":"GHSA-old","summary":"Prototype pollution."}]},` +
	`{"package":{"name":"minimist","version":"1.2.0","ecosystem":"npm"},"vulnerabilities":[{"id":"GHSA-new","summary":"Prototype pollution."}]}` +
	`]}]}`

// onAChangedLine mirrors what the core's diff filter will do to a finding.
func onAChangedLine(req plugin.AnalyzeRequest, file string, line int) bool {
	for _, c := range req.Changed {
		if filepath.ToSlash(c.Path) != file {
			continue
		}
		for _, r := range c.Ranges {
			if line >= r[0] && line <= r[1] {
				return true
			}
		}
	}
	return false
}

const baseReport = `{"results":[{"packages":[` +
	`{"package":{"name":"lodash","version":"4.17.15","ecosystem":"npm"},"vulnerabilities":[{"id":"GHSA-old","summary":"Prototype pollution."}]}` +
	`]}]}`

const realLockfile = `{
  "name": "t", "version": "1.0.0", "lockfileVersion": 3, "requires": true,
  "packages": {
    "": {"name":"t","version":"1.0.0","dependencies":{"minimist":"1.2.0"}},
    "node_modules/minimist": {"version":"1.2.0","resolved":"https://registry.npmjs.org/minimist/-/minimist-1.2.0.tgz","integrity":"sha512-y"}
  }
}
`

// A lockfile whose entries sit on their own lines, so each package has a distinct
// line to be anchored at — the shape npm actually writes.
const multiLockfile = `{
  "name": "t",
  "lockfileVersion": 3,
  "packages": {
    "node_modules/lodash": {
      "version": "4.17.15"
    },
    "node_modules/minimist": {
      "version": "1.2.0"
    }
  }
}
`

// Two advisories against two packages must not share a line. Identity is
// {ruleId, file, snippet} with the line excluded (ADR-0015), so two findings on
// one line are one finding and the GitHub reporter posts only the first.
func TestEachPackageIsAnchoredAtItsOwnLine(t *testing.T) {
	root := gitRepo(t)
	writeFile(t, root, "package-lock.json", strings.NewReplacer(
		"4.17.15", "4.17.21", "1.2.0", "1.2.6").Replace(multiLockfile))
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "package-lock.json", multiLockfile)

	binary := script(t, "#!/bin/sh\n"+
		"case \"$1\" in --version) echo stub; exit 0;; esac\n"+
		"for arg in \"$@\"; do case \"$arg\" in */package-lock.json) target=\"$arg\";; esac; done\n"+
		"if grep -q 4.17.21 \"$target\" 2>/dev/null; then printf '{\"results\":[]}'; else\n"+
		`  printf '%s' '`+headReport+`'`+"\nfi\nexit 1\n")

	req := request(root, "package-lock.json")
	findings, _, err := Analyze(context.Background(), req, binary)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("want one finding per package, got %d: %+v", len(findings), findings)
	}
	if findings[0].Line == findings[1].Line {
		t.Errorf("both packages anchored at line %d; the second would never be posted", findings[0].Line)
	}
	for _, f := range findings {
		if !onAChangedLine(req, f.File, f.Line) {
			t.Errorf("%s:%d is outside the diff", f.File, f.Line)
		}
	}
}

// Several advisories against one package become one comment naming all of them,
// rather than several findings that collapse to one by identity.
func TestAdvisoriesAgainstOnePackageAreOneFinding(t *testing.T) {
	root := gitRepo(t)
	writeFile(t, root, "package-lock.json", "{\n  \"node_modules/lodash\": {\n    \"version\": \"4.17.21\"\n  }\n}\n")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "package-lock.json", "{\n  \"node_modules/lodash\": {\n    \"version\": \"4.17.15\"\n  }\n}\n")

	const two = `{"results":[{"packages":[{"package":{"name":"lodash","version":"4.17.15","ecosystem":"npm"},` +
		`"vulnerabilities":[{"id":"GHSA-aaa","summary":"One."},{"id":"GHSA-bbb","summary":"Two."}]}]}]}`
	binary := script(t, "#!/bin/sh\ncase \"$1\" in --version) echo stub; exit 0;; esac\n"+
		"for arg in \"$@\"; do case \"$arg\" in */package-lock.json) target=\"$arg\";; esac; done\n"+
		"if grep -q 4.17.15 \"$target\" 2>/dev/null; then printf '%s' '"+two+"'; else printf '{\"results\":[]}'; fi\nexit 1\n")

	findings, _, err := Analyze(context.Background(), request(root, "package-lock.json"), binary)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("want one comment for the package, got %d: %+v", len(findings), findings)
	}
	for _, id := range []string{"GHSA-aaa", "GHSA-bbb"} {
		if !strings.Contains(findings[0].Message, id) {
			t.Errorf("the message loses %s: %q", id, findings[0].Message)
		}
	}
}

// The diff is taken against the merge base (ADR-0007). Reading the previous
// lockfile from the base branch's tip instead makes an advisory the change
// inherited look like one it introduced.
func TestTheBaseScanUsesTheMergeBaseNotTheBranchTip(t *testing.T) {
	root := gitRepo(t)
	vulnerable := "{\n  \"node_modules/lodash\": {\n    \"version\": \"4.17.15\"\n  }\n}\n"
	writeFile(t, root, "package-lock.json", vulnerable)
	writeFile(t, root, "README.md", "x\n")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "merge base, already vulnerable")

	// The branch changes the lockfile without changing lodash.
	git(t, root, "checkout", "-q", "-b", "work")
	writeFile(t, root, "package-lock.json", vulnerable+"\n")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "unrelated lockfile edit")

	// main then moves on and fixes lodash.
	git(t, root, "checkout", "-q", "main")
	writeFile(t, root, "package-lock.json", "{\n  \"node_modules/lodash\": {\n    \"version\": \"4.17.21\"\n  }\n}\n")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "fix on main")
	git(t, root, "checkout", "-q", "work")

	const report = `{"results":[{"packages":[{"package":{"name":"lodash","version":"4.17.15","ecosystem":"npm"},` +
		`"vulnerabilities":[{"id":"GHSA-old","summary":"Inherited."}]}]}]}`
	binary := script(t, "#!/bin/sh\ncase \"$1\" in --version) echo stub; exit 0;; esac\n"+
		"for arg in \"$@\"; do case \"$arg\" in */package-lock.json) target=\"$arg\";; esac; done\n"+
		"if grep -q 4.17.15 \"$target\" 2>/dev/null; then printf '%s' '"+report+"'; else printf '{\"results\":[]}'; fi\nexit 1\n")

	req := request(root, "package-lock.json")
	req.Base = "main"
	findings, warnings, err := Analyze(context.Background(), req, binary)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("the branch inherited this advisory and cannot fix it; got %+v (%v)", findings, warnings)
	}
}

// --- helpers ---

// request builds an analyze request for one lockfile.
//
// The changed ranges are the lockfile's real entry lines, not `{{1, 1}}`. An
// earlier version of this helper claimed line 1 was changed, which made every
// assertion about a finding's location vacuous — a lockfile's opening brace is
// never in a dependency bump's diff, so findings anchored there are dropped by
// the core's filter and nobody ever sees them.
func request(root string, paths ...string) plugin.AnalyzeRequest {
	req := plugin.AnalyzeRequest{Root: root}
	for _, p := range paths {
		req.Changed = append(req.Changed, plugin.ChangedFile{Path: p, Ranges: entryLines(root, p)})
	}
	return req
}

// entryLines are the lines of a fixture lockfile that name a package, which is
// what a dependency bump's diff actually covers.
func entryLines(root, path string) [][2]int {
	body, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return [][2]int{{1, 1}}
	}
	var out [][2]int
	for i, line := range strings.Split(string(body), "\n") {
		if strings.Contains(line, "\"version\"") || strings.Contains(line, "node_modules/") {
			out = append(out, [2]int{i + 1, i + 1})
		}
	}
	if len(out) == 0 {
		return [][2]int{{1, 1}}
	}
	return out
}

func script(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "osv-scanner")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func gitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-q", "-b", "main")
	return root
}

func git(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
