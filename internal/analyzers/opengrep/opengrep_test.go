// SPDX-License-Identifier: MIT

package opengrep

import (
	"context"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/pkg/finding"
	"github.com/adamtait/reviewer/pkg/plugin"
)

// The reason this package exists in the shape it does. Opengrep is LGPL-2.1, and
// this project is MIT (ADR-0001): the only lawful coupling is a process boundary
// (ADR-0011). A linked dependency would not announce itself, so it is asserted.
func TestTheOnlyCouplingToOpengrepIsAProcessBoundary(t *testing.T) {
	root := repoRoot(t)

	for _, manifest := range []string{
		"go.mod",
		"go.sum",
		filepath.Join("plugins", "typescript", "package.json"),
		filepath.Join("plugins", "typescript", "package-lock.json"),
	} {
		body, err := os.ReadFile(filepath.Join(root, manifest))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"opengrep", "semgrep"} {
			if strings.Contains(strings.ToLower(string(body)), forbidden) {
				t.Errorf("%s names %q: the boundary must stay a process boundary", manifest, forbidden)
			}
		}
	}

	// And nothing in this package may import anything that could link it. Parsed
	// rather than grepped: a grep over the source would match this package's own
	// name in a comment and pass for the wrong reason.
	pkgs, err := parser.ParseDir(token.NewFileSet(),
		filepath.Join(root, "internal", "analyzers", "opengrep"), nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			for _, imported := range file.Imports {
				path := strings.Trim(imported.Path.Value, `"`)
				if strings.HasPrefix(path, "github.com/adamtait/reviewer/") {
					continue
				}
				if strings.Contains(path, "grep") || strings.Contains(path, "sgrep") {
					t.Errorf("%s imports %q", filepath.Base(name), path)
				}
			}
		}
	}
}

// The analyzer's whole contract with the binary, exercised against a stand-in that
// emits the documented JSON. The real binary is not installed in this environment,
// so this covers the parsing and the mapping; the invocation itself is asserted by
// recording the argument list.
func TestAnalyzeReadsTheReport(t *testing.T) {
	root := t.TempDir()
	writeRule(t, root, "conventions.yaml")
	writeFile(t, root, "src/domain/order.ts", "export const order = 1;\n")

	report := output{Results: []result{{
		CheckID: "conventions.no-raw-fetch-in-domain",
		Path:    "src/domain/order.ts",
		Start:   pos{Line: 5, Col: 3},
		End:     pos{Line: 7, Col: 1},
	}}}
	report.Results[0].Extra.Message = "the domain layer must not call fetch directly"
	report.Results[0].Extra.Severity = "ERROR"
	report.Results[0].Extra.Fix = "inject a client"

	binary, argsLog := fakeOpengrep(t, report, "", 1)

	findings, warnings, err := Analyze(context.Background(), request(root, "src/domain/order.ts"), binary, ".review/rules")
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("want no warnings, got %v", warnings)
	}
	if len(findings) != 1 {
		t.Fatalf("want one finding, got %+v", findings)
	}

	f := findings[0]
	if f.RuleID != "conventions/no-raw-fetch-in-domain" {
		t.Errorf("RuleID = %q", f.RuleID)
	}
	if f.File != "src/domain/order.ts" || f.Line != 5 || f.EndLine != 7 {
		t.Errorf("location = %s:%d-%d", f.File, f.Line, f.EndLine)
	}
	if f.Lane != finding.LaneDeterministic || f.Confidence != finding.ConfidenceHigh {
		t.Errorf("want a deterministic, high-confidence finding, got %s/%s", f.Lane, f.Confidence)
	}
	if f.Severity != finding.SeverityError || f.Suggestion != "inject a client" {
		t.Errorf("severity = %q, suggestion = %q", f.Severity, f.Suggestion)
	}
	if err := f.Validate(); err != nil {
		t.Errorf("the finding does not validate: %v", err)
	}

	// A non-zero exit means "found something" for this class of tool, so it must
	// not be read as a failure — only an unparseable report is one.
	args := readFile(t, argsLog)
	for _, want := range []string{"scan", "--json", "--metrics=off", "src/domain/order.ts"} {
		if !strings.Contains(args, want) {
			t.Errorf("the invocation omits %q: %s", want, args)
		}
	}
}

// The protection against the failure a sibling analyzer actually hit: a tool that
// ignores its target list and scans everything. The core's diff filter would drop
// these too, but this analyzer must not depend on that to be correct.
func TestFindingsOutsideTheChangedSetAreDropped(t *testing.T) {
	root := t.TempDir()
	writeRule(t, root, "conventions.yaml")

	report := output{Results: []result{
		{CheckID: "r.touched", Path: "src/touched.ts", Start: pos{Line: 1}},
		{CheckID: "r.untouched", Path: "src/untouched.ts", Start: pos{Line: 1}},
		{CheckID: "r.elsewhere", Path: "vendor/thing.ts", Start: pos{Line: 1}},
	}}
	binary, _ := fakeOpengrep(t, report, "", 0)

	findings, _, err := Analyze(context.Background(), request(root, "src/touched.ts"), binary, ".review/rules")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].File != "src/touched.ts" {
		t.Fatalf("want only the changed file reported, got %+v", findings)
	}
}

func TestARuleMayNameItsOwnID(t *testing.T) {
	root := t.TempDir()
	writeRule(t, root, "conventions.yaml")

	report := output{Results: []result{{CheckID: "some.generated.path.thing", Path: "a.ts", Start: pos{Line: 1}}}}
	report.Results[0].Extra.Metadata.RuleID = "arch/layer-violation"
	binary, _ := fakeOpengrep(t, report, "", 0)

	findings, _, err := Analyze(context.Background(), request(root, "a.ts"), binary, ".review/rules")
	if err != nil {
		t.Fatal(err)
	}
	// Acceptance is measured per ruleId, so a rule must be able to keep its id
	// when its file is renamed or moved.
	if len(findings) != 1 || findings[0].RuleID != "arch/layer-violation" {
		t.Fatalf("want the declared id, got %+v", findings)
	}
}

// A broken rule file is the repository's problem, and it must be reported as one
// rather than silently producing no findings — or, worse, a finding.
func TestRuleErrorsBecomeWarnings(t *testing.T) {
	root := t.TempDir()
	writeRule(t, root, "conventions.yaml")

	var report output
	report.Errors = append(report.Errors, struct {
		Message string `json:"message"`
		Level   string `json:"level"`
	}{Message: "invalid pattern in conventions.yaml\nat line 4", Level: "ERROR"})
	binary, _ := fakeOpengrep(t, report, "", 1)

	findings, warnings, err := Analyze(context.Background(), request(root, "a.ts"), binary, ".review/rules")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Errorf("a broken rule must not produce a finding, got %+v", findings)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "invalid pattern") {
		t.Errorf("want the rule error reported, got %v", warnings)
	}
}

// Output that is not a report means the scan did not happen, whatever the exit
// code said. This is the lesson the secrets scanner learned against a real binary:
// a rejected flag, a renamed option, a killed process all look like this.
func TestUnparseableOutputIsUnavailableRatherThanClean(t *testing.T) {
	root := t.TempDir()
	writeRule(t, root, "conventions.yaml")
	binary := script(t, "#!/bin/sh\necho 'Error: no such option: --metrics' >&2\nexit 2\n")

	_, _, err := Analyze(context.Background(), request(root, "a.ts"), binary, ".review/rules")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	// The stderr line is what makes a rejected flag diagnosable rather than a
	// mystery, so it has to survive into the error.
	if !strings.Contains(err.Error(), "--metrics") {
		t.Errorf("want the tool's own complaint in the error, got %v", err)
	}
}

func TestNoRulesMeansNothingToDoRatherThanAFailure(t *testing.T) {
	root := t.TempDir()
	binary := script(t, "#!/bin/sh\necho 'should not have been invoked' >&2\nexit 1\n")

	findings, warnings, err := Analyze(context.Background(), request(root, "a.ts"), binary, ".review/rules")
	if err != nil || len(findings) != 0 || len(warnings) != 0 {
		t.Fatalf("a repository with no rules is where every repository starts: %v %v %v", findings, warnings, err)
	}
}

func TestRuleFiles(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.yaml", "b.yml", "notes.md", "c.YAML"} {
		writeFile(t, root, filepath.Join(".review/rules", name), "rules: []\n")
	}
	writeFile(t, root, ".review/rules/nested/d.yaml", "rules: []\n")

	rules, err := RuleFiles(root, ".review/rules")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(rules, ","); got != "a.yaml,b.yml,c.YAML" {
		t.Errorf("RuleFiles() = %q", got)
	}

	// A rules directory that does not exist is not an error.
	missing, err := RuleFiles(root, ".review/nope")
	if err != nil || len(missing) != 0 {
		t.Errorf("want no rules and no error, got %v %v", missing, err)
	}
}

func TestSeverityNeverEscalatesAnUnknownLevel(t *testing.T) {
	for level, want := range map[string]finding.Severity{
		"ERROR":    finding.SeverityError,
		"error":    finding.SeverityError,
		"WARNING":  finding.SeverityWarning,
		"INFO":     finding.SeverityInfo,
		"":         finding.SeverityWarning,
		"CRITICAL": finding.SeverityWarning,
		"whatever": finding.SeverityWarning,
	} {
		if got := severity(level); got != want {
			t.Errorf("severity(%q) = %q, want %q", level, got, want)
		}
	}
}

// --- helpers ---

func request(root string, paths ...string) plugin.AnalyzeRequest {
	req := plugin.AnalyzeRequest{Root: root}
	for _, p := range paths {
		req.Changed = append(req.Changed, plugin.ChangedFile{Path: p, Ranges: [][2]int{{1, 100}}})
	}
	return req
}

// fakeOpengrep writes a script that prints one report and records its arguments.
func fakeOpengrep(t *testing.T, report output, stderr string, exit int) (binary, argsLog string) {
	t.Helper()
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	argsLog = filepath.Join(t.TempDir(), "args")
	quoted := strings.ReplaceAll(string(body), "'", `'"'"'`)
	return script(t, "#!/bin/sh\nprintf '%s' \"$*\" > "+argsLog+"\n"+
		"printf '%s' '"+quoted+"'\n"+
		func() string {
			if stderr == "" {
				return ""
			}
			return "echo '" + stderr + "' >&2\n"
		}()+
		"exit "+strconv.Itoa(exit)+"\n"), argsLog
}

func script(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opengrep")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeRule(t *testing.T, root, name string) {
	t.Helper()
	writeFile(t, root, filepath.Join(".review/rules", name), "rules: []\n")
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

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}
		dir = parent
	}
}
