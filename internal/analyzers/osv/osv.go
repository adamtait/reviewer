// SPDX-License-Identifier: MIT

// Package osv reports known vulnerabilities that a pull request introduces.
//
// The word that carries the weight is *introduces*. A repository's lockfile
// accumulates advisories that nobody in this pull request caused and nobody in it
// can fix, and reporting those on an unrelated change is the fastest way to teach
// a team that this tool's comments are noise (ADR-0007). So the lockfile is
// scanned twice — as it is, and as it was — and only the difference is reported.
//
// osv-scanner is spawned, never linked (ADR-0011).
package osv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adamtait/reviewer/pkg/finding"
	"github.com/adamtait/reviewer/pkg/plugin"
)

// ID is the analyzer's name in --only and --skip.
const ID = "osv"

// Order puts this after the pattern matchers. It is the only deterministic
// analyzer that needs the network, so it runs last among them (ADR-0013).
const Order = 40

// Binary is the executable name. Overridden from config when a repository pins a
// path; never a compiled-in absolute path (ADR-0004).
const Binary = "osv-scanner"

// ErrUnavailable means the scan could not be performed.
var ErrUnavailable = errors.New("osv-scanner could not be run")

// errorExit is osv-scanner's documented status for a general failure.
//
// This matters more than it looks. When the vulnerability database cannot be
// reached, osv-scanner prints a report with an empty results array and writes the
// reason only to stderr — so stdout alone says "no vulnerabilities" for a scan that
// never happened. Verified against osv-scanner 2.5.1 with the API unreachable:
// stdout was `{"results": []}` and the exit status was 127. Without this check
// every run behind a restricted network would report every dependency clean.
const errorExit = 127

// lockfiles are the manifests osv-scanner understands, by base name. A changed
// file that is not one of these cannot have introduced a dependency, and the
// scanner is never spawned for it — which is what makes a pull request that
// touches no dependency cost nothing at all.
var lockfiles = map[string]bool{
	"package-lock.json":   true,
	"npm-shrinkwrap.json": true,
	"pnpm-lock.yaml":      true,
	"yarn.lock":           true,
	"bun.lock":            true,
	"go.mod":              true,
	"go.sum":              true,
	"Cargo.lock":          true,
	"poetry.lock":         true,
	"Pipfile.lock":        true,
	"requirements.txt":    true,
	"Gemfile.lock":        true,
	"composer.lock":       true,
	"gradle.lockfile":     true,
	"pubspec.lock":        true,
	"mix.lock":            true,
}

// report is the subset of osv-scanner's JSON this analyzer reads.
type report struct {
	Results []struct {
		Packages []struct {
			Package struct {
				Name      string `json:"name"`
				Version   string `json:"version"`
				Ecosystem string `json:"ecosystem"`
			} `json:"package"`
			Vulnerabilities []vulnerability `json:"vulnerabilities"`
		} `json:"packages"`
	} `json:"results"`
}

type vulnerability struct {
	ID      string   `json:"id"`
	Summary string   `json:"summary"`
	Aliases []string `json:"aliases"`
	Details string   `json:"details"`
}

// advisory is one vulnerability against one package version, which is the unit
// that is or is not new in this pull request.
type advisory struct {
	vulnID    string
	pkg       string
	version   string
	ecosystem string
	summary   string
}

func (a advisory) key() string { return a.ecosystem + "|" + a.pkg + "|" + a.version + "|" + a.vulnID }

// Analyze scans the lockfiles this pull request changed.
func Analyze(ctx context.Context, req plugin.AnalyzeRequest, binary string) ([]finding.Finding, []string, error) {
	if binary == "" {
		binary = Binary
	}

	changed := changedLockfiles(req.Changed)
	if len(changed) == 0 {
		// Never spawned. A pull request that touches no dependency should not pay
		// for a network round trip, and the scanner is the slowest thing here.
		return nil, nil, nil
	}

	path, err := exec.LookPath(binary)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}

	var (
		findings []finding.Finding
		warnings []string
	)
	for _, lockfile := range changed {
		head, warn, err := scanFile(ctx, path, req.Root, filepath.Join(req.Root, filepath.FromSlash(lockfile)))
		warnings = append(warnings, warn...)
		if err != nil {
			return nil, warnings, err
		}
		if len(head) == 0 {
			// Nothing to attribute, so the base scan — a second network round trip
			// — is skipped entirely.
			continue
		}

		before, warn, err := scanBase(ctx, path, req.Root, req.Base, lockfile)
		warnings = append(warnings, warn...)
		if err != nil {
			// The head scan worked and the base scan did not, so "introduced by
			// this pull request" cannot be established. Reporting everything found
			// would be exactly the noise this analyzer exists to avoid.
			warnings = append(warnings, fmt.Sprintf(
				"osv: %s: could not read the previous %s, so nothing is reported for it: %v",
				ID, lockfile, err))
			continue
		}

		for _, a := range introduced(head, before) {
			findings = append(findings, describe(a, lockfile))
		}
	}
	return findings, warnings, nil
}

// changedLockfiles picks the manifests out of the diff, deduplicated and ordered.
func changedLockfiles(files []plugin.ChangedFile) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		path := filepath.ToSlash(f.Path)
		if !lockfiles[filepath.Base(path)] || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// scanBase scans the lockfile as it was before this pull request.
//
// The previous contents are written to a temporary directory under the original
// base name, because osv-scanner chooses its parser from the file name: handing it
// `tmp1234` produces "could not determine extractor suitable to this file".
func scanBase(ctx context.Context, binary, root, base, lockfile string) ([]advisory, []string, error) {
	ref := base
	if ref == "" {
		// A staged review compares the index against HEAD, so HEAD is what the
		// lockfile looked like before.
		ref = "HEAD"
	}

	show := exec.CommandContext(ctx, "git", "show", ref+":"+lockfile)
	show.Dir = root
	previous, err := show.Output()
	if err != nil {
		var stderr string
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr = firstLine(string(exitErr.Stderr))
		}
		if strings.Contains(stderr, "does not exist") || strings.Contains(stderr, "exists on disk, but not in") {
			// The lockfile is new in this pull request, so everything in it is
			// introduced by it. Not an error.
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("git show %s:%s: %v%s", ref, lockfile, err, detail(stderr))
	}

	dir, err := os.MkdirTemp("", "reviewer-osv-*")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)

	target := filepath.Join(dir, filepath.Base(lockfile))
	if err := os.WriteFile(target, previous, 0o600); err != nil {
		return nil, nil, err
	}
	return scanFile(ctx, binary, root, target)
}

// scanFile runs osv-scanner over one manifest.
//
// Exit status decides, not stdout: a report with an empty results array is what an
// unreachable database looks like as well as what a clean lockfile looks like, and
// only the status tells them apart.
func scanFile(ctx context.Context, binary, root, path string) ([]advisory, []string, error) {
	cmd := exec.CommandContext(ctx, binary,
		"scan", "source",
		"--lockfile", path,
		"--format", "json",
		// Anything louder writes a progress narration to stderr on every run, which
		// would make a clean scan look like a degraded one.
		"--verbosity", "error",
	)
	cmd.Dir = root
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) && exitErr.ExitCode() == errorExit {
		return nil, nil, fmt.Errorf("%w: %s", ErrUnavailable, firstLine(stderr.String()))
	}

	var parsed report
	if err := json.Unmarshal([]byte(stdout.String()), &parsed); err != nil {
		return nil, nil, fmt.Errorf("%w: %v (%s)", ErrUnavailable, runErr, firstLine(stderr.String()))
	}

	var warnings []string
	if line := firstLine(stderr.String()); line != "" {
		warnings = append(warnings, "osv-scanner: "+line)
	}

	var out []advisory
	for _, result := range parsed.Results {
		for _, p := range result.Packages {
			for _, v := range p.Vulnerabilities {
				out = append(out, advisory{
					vulnID:    v.ID,
					pkg:       p.Package.Name,
					version:   p.Package.Version,
					ecosystem: p.Package.Ecosystem,
					summary:   summary(v),
				})
			}
		}
	}
	return out, warnings, nil
}

// introduced is what is in head and was not in base, keyed by advisory *and*
// version. A dependency bumped from one vulnerable version to another vulnerable
// version is introduced: the pull request chose the new version.
func introduced(head, base []advisory) []advisory {
	was := map[string]bool{}
	for _, a := range base {
		was[a.key()] = true
	}

	seen := map[string]bool{}
	var out []advisory
	for _, a := range head {
		if was[a.key()] || seen[a.key()] {
			continue
		}
		seen[a.key()] = true
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key() < out[j].key() })
	return out
}

// describe turns an advisory into a finding against the lockfile.
//
// Line 1, deliberately. The lockfile line where a package's entry sits is not
// where a human fixes this — the fix is a version bump in the manifest — and
// pointing at a lockfile's internals invites someone to hand-edit one.
func describe(a advisory, lockfile string) finding.Finding {
	return finding.Finding{
		RuleID:     "deps/" + strings.ToLower(a.ecosystemSlug()),
		Lane:       finding.LaneDeterministic,
		Confidence: finding.ConfidenceHigh,
		Severity:   finding.SeverityError,
		File:       lockfile,
		Line:       1,
		Message: fmt.Sprintf("This change introduces %s@%s, which %s affects. %s "+
			"Bump it in the manifest and regenerate the lockfile.",
			a.pkg, a.version, a.vulnID, a.summary),
	}
}

func (a advisory) ecosystemSlug() string {
	if a.ecosystem == "" {
		return "vulnerable-dependency"
	}
	slug := strings.ToLower(a.ecosystem)
	slug = strings.ReplaceAll(slug, " ", "-")
	return slug
}

func summary(v vulnerability) string {
	if s := strings.TrimSpace(v.Summary); s != "" {
		return s
	}
	if d := firstLine(v.Details); d != "" {
		return d
	}
	return "See the advisory for details."
}

func detail(stderr string) string {
	if stderr == "" {
		return ""
	}
	return ": " + stderr
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// Probe reports whether osv-scanner can be run at all.
func Probe(binary string) (path, version string, err error) {
	if binary == "" {
		binary = Binary
	}
	path, err = exec.LookPath(binary)
	if err != nil {
		return "", "", fmt.Errorf("%s is not installed or not on PATH", binary)
	}
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return path, "", fmt.Errorf("%s is installed but would not run: %v", binary, err)
	}
	return path, firstLine(string(out)), nil
}
