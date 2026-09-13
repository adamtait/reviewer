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
	"time"

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

// probeTimeout bounds the version check. Generous for a binary that starts, and
// short enough that a wedged one does not stall a review.
const probeTimeout = 10 * time.Second

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

	lines := changedLines(req.Changed)

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

		findings = append(findings, describe(req.Root, introduced(head, before), lockfile, lines[lockfile])...)
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
	} else if merged := mergeBase(ctx, root, ref); merged != "" {
		// The merge base, not the base branch's tip. The diff is taken with
		// `--merge-base` (ADR-0007), and reading the previous lockfile from the
		// tip instead makes the two disagree: when the base branch has moved on,
		// advisories this change inherited look like advisories it introduced,
		// which is exactly the noise this analyzer exists to avoid.
		ref = merged
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

// mergeBase resolves the commit the diff was taken from. An empty result means
// git could not answer — a shallow clone, or a ref this checkout does not have —
// and the caller falls back to the ref as given.
func mergeBase(ctx context.Context, root, ref string) string {
	cmd := exec.CommandContext(ctx, "git", "merge-base", ref, "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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

// describe turns the introduced advisories into findings: **one per package**,
// listing every advisory against it.
//
// Two things force that grouping, and both were found the hard way.
//
// The location has to be a line the change touched, or the core's diff filter
// drops the finding and the analyzer reports into a void (ADR-0007). Line 1 of a
// lockfile is never in the diff — a dependency bump does not change the opening
// brace — so every finding this analyzer produced was discarded.
//
// And identity is `{ruleId, file, snippet}` (ADR-0015), with the line deliberately
// excluded. Two advisories sharing a rule id, a file and a line are therefore the
// same finding, and the GitHub reporter posts one of them. Grouping by package
// makes that correct rather than lossy: one comment per package, naming every
// advisory against it, anchored at the line that package's entry changed on.
func describe(root string, advisories []advisory, lockfile string, changed []intRange) []finding.Finding {
	type group struct {
		pkg, version, ecosystem string
		ids                     []string
		summaries               []string
	}
	order := []string{}
	groups := map[string]*group{}
	for _, a := range advisories {
		key := a.ecosystem + "|" + a.pkg + "|" + a.version
		g, ok := groups[key]
		if !ok {
			g = &group{pkg: a.pkg, version: a.version, ecosystem: a.ecosystem}
			groups[key] = g
			order = append(order, key)
		}
		g.ids = append(g.ids, a.vulnID)
		if a.summary != "" {
			g.summaries = append(g.summaries, a.summary)
		}
	}

	// Anchored, then merged by line. Two packages that still resolve to the same
	// line would be one finding by identity (ADR-0015) and the second would never
	// be posted, so they are combined into one comment that names both rather than
	// silently losing one.
	byLine := map[int][]*group{}
	lineOrder := []int{}
	for _, key := range order {
		g := groups[key]
		line := anchor(filepath.Join(root, filepath.FromSlash(lockfile)), g.pkg, g.version, changed)
		if line == 0 {
			// Nothing in this lockfile's diff mentions the package, so there is no
			// line a reviewer would recognise. Dropping it is honest; reporting it
			// somewhere arbitrary is not.
			continue
		}
		if _, seen := byLine[line]; !seen {
			lineOrder = append(lineOrder, line)
		}
		byLine[line] = append(byLine[line], g)
	}

	out := make([]finding.Finding, 0, len(lineOrder))
	for _, line := range lineOrder {
		at := byLine[line]
		parts := make([]string, 0, len(at))
		var summaries []string
		for _, g := range at {
			parts = append(parts, fmt.Sprintf("%s@%s (%s)", g.pkg, g.version, joinIDs(g.ids)))
			summaries = append(summaries, g.summaries...)
		}
		out = append(out, finding.Finding{
			RuleID:     "deps/" + ecosystemSlug(at[0].ecosystem),
			Lane:       finding.LaneDeterministic,
			Confidence: finding.ConfidenceHigh,
			Severity:   finding.SeverityError,
			File:       lockfile,
			Line:       line,
			Message: fmt.Sprintf("This change introduces %s, %s a known vulnerability. %sBump %s in "+
				"the manifest and regenerate the lockfile.",
				strings.Join(parts, " and "), carries(len(at)), summaryOf(summaries),
				subject(len(at))),
		})
	}
	return out
}

// lookback is how far above a changed line to search for the package it belongs
// to. A lockfile entry is a handful of lines; twenty covers the longest npm entry
// and stops well short of reaching the previous package.
const lookback = 20

// anchor finds the changed line in the lockfile that belongs to one package. Read
// from the working tree, because that is what the diff's line numbers refer to.
//
// Three passes, cheapest and most certain first, because the format varies: go.sum
// puts the module and version on one line, while package-lock.json puts the
// version several lines below the `"node_modules/lodash":` key that names it — so
// a version bump's changed line contains neither the package name nor anything
// else distinguishing it from the next package's.
//
// Getting this right is what stops two packages from sharing a line. Identity is
// `{ruleId, file, snippet}` (ADR-0015), so two findings on one line are one
// finding, and the second one is never posted.
func anchor(lockfile, pkg, version string, changed []intRange) int {
	body, err := os.ReadFile(lockfile)
	if err != nil {
		return firstChanged(changed)
	}
	lines := strings.Split(string(body), "\n")
	at := func(n int) string {
		if n < 1 || n > len(lines) {
			return ""
		}
		return lines[n-1]
	}

	// 1. A changed line naming both the package and the version: the entry itself.
	// 2. A changed line naming the package.
	named := 0
	for _, r := range changed {
		for n := r.start; n <= r.end; n++ {
			line := at(n)
			if line == "" || !strings.Contains(line, pkg) {
				continue
			}
			if strings.Contains(line, version) {
				return n
			}
			if named == 0 {
				named = n
			}
		}
	}
	if named != 0 {
		return named
	}

	// 3. A changed line inside the package's block: the nearest line above it that
	// names the package, within one entry's worth of lines.
	for _, r := range changed {
		for n := r.start; n <= r.end; n++ {
			if at(n) == "" {
				continue
			}
			for back := n - 1; back >= 1 && back >= n-lookback; back-- {
				if strings.Contains(at(back), pkg) {
					return n
				}
			}
		}
	}
	return firstChanged(changed)
}

func firstChanged(changed []intRange) int {
	if len(changed) == 0 {
		return 0
	}
	return changed[0].start
}

// intRange is one changed span, as the protocol delivers it.
type intRange struct{ start, end int }

func changedLines(files []plugin.ChangedFile) map[string][]intRange {
	out := map[string][]intRange{}
	for _, f := range files {
		path := filepath.ToSlash(f.Path)
		for _, r := range f.Ranges {
			out[path] = append(out[path], intRange{start: r[0], end: r[1]})
		}
	}
	return out
}

func joinIDs(ids []string) string {
	if len(ids) == 1 {
		return ids[0]
	}
	return strings.Join(ids[:len(ids)-1], ", ") + " and " + ids[len(ids)-1]
}

func carries(n int) string {
	if n == 1 {
		return "which has"
	}
	return "which have"
}

func subject(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

func summaryOf(summaries []string) string {
	if len(summaries) == 0 {
		return ""
	}
	return summaries[0] + " "
}

func ecosystemSlug(ecosystem string) string {
	if ecosystem == "" {
		return "vulnerable-dependency"
	}
	return strings.ReplaceAll(strings.ToLower(ecosystem), " ", "-")
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
