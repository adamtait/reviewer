// SPDX-License-Identifier: MIT

// Package opengrep reports violations of this repository's own convention rules.
//
// Opengrep is LGPL-2.1 and is reached only by spawning it (ADR-0011): no Go
// module, no npm package, no cgo, nothing linked. That boundary is not a
// preference — it is what keeps this project MIT-licensable — and a test in this
// package asserts it rather than trusting it.
//
// The rules themselves live in the repository under review, never here. This
// package knows how to invoke a binary and how to read its output; what counts as
// a violation is the destination repository's business (ADR-0004).
package opengrep

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/adamtait/reviewer/pkg/finding"
	"github.com/adamtait/reviewer/pkg/plugin"
)

// ID is the analyzer's name in --only and --skip.
const ID = "opengrep"

// Order puts this after the secrets scan and before the language tooling. Pattern
// matching over changed files is cheap, and cheap-first is the rule (ADR-0013).
const Order = 30

// Binary is the executable name. Overridden from config when a repository pins a
// path; never a compiled-in absolute path (ADR-0004).
const Binary = "opengrep"

// Namespace prefixes every rule id this analyzer emits, unless a rule names its
// own. Acceptance is measured per ruleId (ADR-0024), so the namespace is ours and
// stays stable across Opengrep upgrades and across a rule being renamed upstream.
const Namespace = "conventions/"

// probeTimeout bounds the version check. Generous for a binary that starts, and
// short enough that a wedged one does not stall a review.
const probeTimeout = 10 * time.Second

// ErrUnavailable means the scan could not be performed. Unlike the secrets
// scanner, nothing fails closed on this: a convention rule that could not run
// costs a warning and the run continues (ADR-0013).
var ErrUnavailable = errors.New("opengrep could not be run")

// output is the subset of Opengrep's JSON report this analyzer reads.
//
// The shape is Semgrep's, which Opengrep forked. It has **not** been verified
// against the real binary in this environment — see the note on scan below for
// what that means and how the risk is bounded.
type output struct {
	Results []result `json:"results"`
	Errors  []struct {
		Message string `json:"message"`
		Level   string `json:"level"`
	} `json:"errors"`
}

type result struct {
	CheckID string `json:"check_id"`
	Path    string `json:"path"`
	Start   pos    `json:"start"`
	End     pos    `json:"end"`
	Extra   struct {
		Message  string `json:"message"`
		Severity string `json:"severity"`
		Fix      string `json:"fix"`
		Metadata struct {
			// RuleID lets a rule state the id this tool should report it under,
			// so a rule can be renamed in its file without resetting its
			// acceptance history.
			RuleID string `json:"reviewer-rule-id"`
		} `json:"metadata"`
	} `json:"extra"`
}

type pos struct {
	Line int `json:"line"`
	Col  int `json:"col"`
}

// Analyze runs the repository's rules over the changed files.
func Analyze(ctx context.Context, req plugin.AnalyzeRequest, binary, rulesDir string) ([]finding.Finding, []string, error) {
	if binary == "" {
		binary = Binary
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}

	rules, err := RuleFiles(req.Root, rulesDir)
	if err != nil {
		return nil, nil, err
	}
	if len(rules) == 0 {
		// Not a failure: a repository that has written no rules yet is the normal
		// starting state, and there is nothing to report.
		return nil, nil, nil
	}

	targets := analysable(req.Changed)
	if len(targets) == 0 {
		return nil, nil, nil
	}

	report, warnings, err := scan(ctx, path, req.Root, rulesDir, targets)
	if err != nil {
		return nil, warnings, err
	}

	changed := map[string]bool{}
	for _, t := range targets {
		changed[t] = true
	}

	findings := make([]finding.Finding, 0, len(report.Results))
	for _, r := range report.Results {
		file := relative(req.Root, r.Path)
		// Filtered here as well as by the core's diff filter. If a future Opengrep
		// ignores the target list — the failure mode that a sibling analyzer hit
		// with `gitleaks dir` — this is what stops a rule from reporting on files
		// the pull request never touched.
		if !changed[file] {
			continue
		}
		findings = append(findings, finding.Finding{
			RuleID: ruleID(r),
			Lane:   finding.LaneDeterministic,
			// A pattern either matched or it did not. There is no judgement in the
			// result, so it goes inline (ADR-0017).
			Confidence: finding.ConfidenceHigh,
			Severity:   severity(r.Extra.Severity),
			File:       file,
			Line:       atLeastOne(r.Start.Line),
			EndLine:    endLine(r.Start.Line, r.End.Line),
			Message:    message(r),
			Suggestion: strings.TrimSpace(r.Extra.Fix),
		})
	}

	for _, e := range report.Errors {
		// A rule that failed to compile is a warning, not a finding: the repository
		// has a broken rule file and the run says so without inventing a violation.
		warnings = append(warnings, fmt.Sprintf("opengrep: %s: %s",
			strings.ToLower(strings.TrimSpace(e.Level)), firstLine(e.Message)))
	}
	return findings, warnings, nil
}

// scan runs Opengrep once over every changed file.
//
// One invocation, not one per file: Opengrep compiles the whole rule set before it
// matches anything, so per-file invocation would pay that cost once per file. The
// risk that comes with a multi-target argument list — a tool that silently ignores
// all but the first path and scans the working directory instead — is real, and it
// is why Analyze filters results back down to the changed set itself.
//
// Neither the exit code nor stderr decides anything. Opengrep exits non-zero when
// it finds something, so an exit code cannot distinguish a finding from a failure;
// only a parseable report can. This is the same lesson the secrets scanner
// taught, learned there against the real binary.
//
// The JSON shape and the flag names here are Semgrep's, which Opengrep forked, and
// are NOT verified against the real binary in this environment. A wrong flag makes
// the command fail, which surfaces as a warning naming the flag — diagnosable, and
// the reason stderr's first line is carried into the error.
func scan(ctx context.Context, binary, root, rulesDir string, targets []string) (output, []string, error) {
	args := []string{
		"scan",
		"--json",
		"--quiet",
		// The deterministic lane makes no network egress. If this flag is ever
		// rejected the analyzer fails loudly rather than sending anything.
		"--metrics=off",
		"--config", rulesDir,
		"--disable-version-check",
		// End of options. A repository can track a file whose name begins with a
		// dash — git allows it and the diff parser passes it through verbatim — and
		// without this separator that filename becomes a flag on the scanner's
		// command line. A second `--config` alone would let a fork's pull request
		// supply its own rule pack to the scan reviewing it.
		"--",
	}
	args = append(args, targets...)

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = root
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	var report output
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		return output{}, nil, fmt.Errorf("%w: %v (%s)",
			ErrUnavailable, runErr, firstLine(stderr.String()))
	}
	var warnings []string
	if line := firstLine(stderr.String()); line != "" {
		warnings = append(warnings, "opengrep: "+line)
	}
	return report, warnings, nil
}

// RuleFiles lists the rule files a repository has written. A rules directory that
// does not exist is not an error: it is a repository that has not written a rule
// yet, which is where every repository starts.
func RuleFiles(root, rulesDir string) ([]string, error) {
	if rulesDir == "" {
		return nil, nil
	}
	dir := rulesDir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, filepath.FromSlash(rulesDir))
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: reading %s: %v", ErrUnavailable, rulesDir, err)
	}

	var rules []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".yaml", ".yml":
			rules = append(rules, entry.Name())
		}
	}
	return rules, nil
}

// analysable is the paths to hand the scanner. Deleted and binary files are
// already gone by the time a request reaches here — diff.ToPluginFiles drops both
// — so this only normalises separators and skips an empty path.
func analysable(changed []plugin.ChangedFile) []string {
	var out []string
	for _, f := range changed {
		if f.Path == "" {
			continue
		}
		out = append(out, filepath.ToSlash(f.Path))
	}
	return out
}

// ruleID is the id acceptance is measured against. A rule may name its own via
// metadata; otherwise the last dot-separated segment of Opengrep's check_id is
// used, because a check_id derived from a file path would otherwise change every
// time a rule file moved.
func ruleID(r result) string {
	if declared := strings.TrimSpace(r.Extra.Metadata.RuleID); declared != "" {
		return declared
	}
	id := r.CheckID
	if i := strings.LastIndex(id, "."); i >= 0 {
		id = id[i+1:]
	}
	if id = strings.TrimSpace(id); id == "" {
		return Namespace + "unnamed-rule"
	}
	if strings.Contains(id, "/") {
		// Already namespaced by whoever wrote the rule.
		return id
	}
	return Namespace + id
}

// severity maps Opengrep's levels onto ours. An unrecognised level becomes a
// warning rather than an error: a rule set this tool does not fully understand
// should not be able to raise the loudest thing it can say.
func severity(level string) finding.Severity {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "ERROR":
		return finding.SeverityError
	case "INFO":
		return finding.SeverityInfo
	default:
		return finding.SeverityWarning
	}
}

func message(r result) string {
	if m := strings.TrimSpace(r.Extra.Message); m != "" {
		return m
	}
	return fmt.Sprintf("matched the rule %s, which carries no message", r.CheckID)
}

func relative(root, path string) string {
	path = filepath.ToSlash(path)
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return path
}

func atLeastOne(line int) int {
	if line < 1 {
		return 1
	}
	return line
}

func endLine(start, end int) int {
	if end <= start {
		return 0
	}
	return end
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// Probe reports whether Opengrep can be run at all, so a repository without it is
// told once at the top of the run rather than per analyzer invocation.
func Probe(binary string) (path, version string, err error) {
	if binary == "" {
		binary = Binary
	}
	path, err = exec.LookPath(binary)
	if err != nil {
		return "", "", fmt.Errorf("%s is not installed or not on PATH", binary)
	}
	// Bounded: Probe runs at the top of every review, including every poll of the
	// watch loop, and a binary that hangs on --version would hang the tool with
	// nothing to interrupt it.
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return path, "", fmt.Errorf("%s is installed but would not run: %v", binary, err)
	}
	return path, firstLine(string(out)), nil
}
