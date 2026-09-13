// SPDX-License-Identifier: MIT

// Package gitleaks reports credentials committed in the diff.
//
// This is the analyzer the secrets gate depends on (ADR-0012), which makes its
// failure modes unusually important: if it cannot run, the gate must fail closed
// rather than let the model lane proceed on the assumption that a diff is clean.
// It therefore distinguishes "scanned, found nothing" from "could not scan", and
// the run context carries the difference.
package gitleaks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/adamtait/reviewer/pkg/finding"
	"github.com/adamtait/reviewer/pkg/plugin"
)

// ID is the analyzer's name in --only and --skip.
const ID = "gitleaks"

// Order puts this first in the run. It gates the model lane, so nothing that
// could reach a model may run before it (ADR-0013).
const Order = 10

// ErrUnavailable means the scan could not be performed at all. The secrets gate
// treats this as though a secret were found (ADR-0012): an unscanned diff and a
// dirty diff are indistinguishable from the gate's point of view.
var ErrUnavailable = errors.New("gitleaks could not be run")

// Binary is the executable name. Overridden from config when a repository pins a
// path; never a compiled-in absolute path (ADR-0004).
const Binary = "gitleaks"

// report is one entry from gitleaks' JSON output.
type report struct {
	RuleID      string `json:"RuleID"`
	Description string `json:"Description"`
	StartLine   int    `json:"StartLine"`
	EndLine     int    `json:"EndLine"`
	File        string `json:"File"`
	// Secret and Match are deliberately unused: a finding must never carry the
	// credential it found. Posting one to a pull request would publish it to
	// everyone with read access and to every notification email.
}

// Analyze scans the changed files and returns one finding per credential.
func Analyze(ctx context.Context, req plugin.AnalyzeRequest, binary string) ([]finding.Finding, []string, error) {
	if binary == "" {
		binary = Binary
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}

	var paths []string
	for _, f := range req.Changed {
		paths = append(paths, f.Path)
	}
	if len(paths) == 0 {
		return nil, nil, nil
	}

	reports, warnings, err := scan(ctx, path, req.Root, paths)
	if err != nil {
		return nil, warnings, err
	}

	findings := make([]finding.Finding, 0, len(reports))
	for _, r := range reports {
		file := filepath.ToSlash(r.File)
		if rel, err := filepath.Rel(req.Root, file); err == nil && !strings.HasPrefix(rel, "..") {
			file = filepath.ToSlash(rel)
		}
		line := r.StartLine
		if line < 1 {
			line = 1
		}
		end := r.EndLine
		if end < line {
			end = 0
		}
		findings = append(findings, finding.Finding{
			// The rule id namespace is ours, not gitleaks': acceptance is measured
			// per ruleId and must stay stable across gitleaks upgrades.
			RuleID:     "secrets/" + r.RuleID,
			Lane:       finding.LaneDeterministic,
			Confidence: finding.ConfidenceHigh,
			Severity:   finding.SeverityError,
			File:       file,
			Line:       line,
			EndLine:    end,
			// The description, never the secret.
			Message: describe(r),
		})
	}
	return findings, warnings, nil
}

// describe writes the message without the credential in it.
func describe(r report) string {
	d := strings.TrimSpace(r.Description)
	if d == "" {
		d = "a credential-shaped string was found here"
	}
	return fmt.Sprintf("%s Remove it and rotate the credential; the value is not repeated here on purpose.", d)
}

// scan runs gitleaks over the given paths. gitleaks writes its report to a file
// rather than stdout, and exits 1 when it finds something, so neither the exit
// code nor stdout can be used to tell a finding from a failure — only the report.
func scan(ctx context.Context, binary, root string, paths []string) ([]report, []string, error) {
	tmp, err := os.CreateTemp("", "reviewer-gitleaks-*.json")
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	reportPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(reportPath)

	args := []string{"dir", "--no-banner", "--report-format", "json", "--report-path", reportPath, "--exit-code", "1"}
	args = append(args, paths...)

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = root
	var stderr strings.Builder
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	raw, readErr := os.ReadFile(reportPath)
	if readErr != nil {
		// No report at all: the scan did not happen, whatever the exit code said.
		return nil, nil, fmt.Errorf("%w: %v (%s)", ErrUnavailable, runErr, firstLine(stderr.String()))
	}

	// A clean scan writes "[]"; the real tool never leaves the report empty. An
	// empty file therefore means the report was not written — a killed process, a
	// full disk, or a future gitleaks that renamed --report-path. Treating that as
	// clean would make the gate report every diff clean after a silent flag
	// rename, which is the failure this analyzer exists to prevent.
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, nil, fmt.Errorf("%w: wrote no report to --report-path (exit %v, %s)",
			ErrUnavailable, exitCodeOf(runErr), firstLine(stderr.String()))
	}

	var reports []report
	if err := json.Unmarshal(raw, &reports); err != nil {
		return nil, nil, fmt.Errorf("%w: unreadable report: %v", ErrUnavailable, err)
	}

	var warnings []string
	// Exit 1 is "leaks found" and is expected. Anything else with a readable but
	// empty report means the tool failed in a way that produced no findings, and
	// claiming the diff is clean would be a lie the gate depends on.
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) && exitErr.ExitCode() != 1 && len(reports) == 0 {
		return nil, nil, fmt.Errorf("%w: exit %d (%s)", ErrUnavailable, exitErr.ExitCode(), firstLine(stderr.String()))
	}
	if s := firstLine(stderr.String()); s != "" && len(reports) == 0 {
		warnings = append(warnings, fmt.Sprintf("%s: %s", ID, s))
	}
	return reports, warnings, nil
}

// exitCodeOf reports a process's exit status for a message, or -1 when it did not
// run or was signalled.
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// Probe reports whether the scanner can run, and the version it reports. Used by
// Describe so an absent binary is announced once at the top of a run rather than
// discovered when the analyzer is asked to work.
func Probe(binary string) (path, version string, err error) {
	if binary == "" {
		binary = Binary
	}
	path, err = exec.LookPath(binary)
	if err != nil {
		return "", "", fmt.Errorf("%s is not installed or not on PATH", binary)
	}
	out, err := exec.Command(path, "version").Output()
	if err != nil {
		// Present but unrunnable is still unavailable, and worth distinguishing
		// from absent in the message.
		return path, "", fmt.Errorf("%s is installed but would not run: %v", binary, err)
	}
	return path, firstLine(string(out)), nil
}
