// SPDX-License-Identifier: MIT

// Package installer adapts this tool to a repository it has never seen.
//
// Everything repository-specific lives in the destination, written once at install
// time, so the engine stays free of anything that could not be published
// (ADR-0004, ADR-0020). The installer is the only component that knows what a
// TypeScript repository tends to look like.
package installer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Detected is what the installer worked out about a repository. Every field is a
// guess with a stated basis, so `--dry-run` can explain itself rather than
// presenting conclusions.
type Detected struct {
	Root string

	// PackageManager is npm, pnpm, yarn or bun, from the lockfile present.
	PackageManager string
	// LockfileBasis names the file that decided it, for the explanation.
	LockfileBasis string

	// TSConfig is the path to tsconfig.json relative to the root, if any.
	TSConfig string
	// ESLintConfig is the flat config path, if any. The analyzer uses the
	// repository's own rules and declines without one.
	ESLintConfig string
	// DepCruiserConfig is the dependency-cruiser config path, if any.
	DepCruiserConfig string

	// TestRunner is vitest, jest or "" when neither is a dependency.
	TestRunner string

	// WorkspaceGlobs are the patterns as declared, in package.json or
	// pnpm-workspace.yaml, kept so the plan can say where the answer came from.
	WorkspaceGlobs []string
	// Workspaces are those globs resolved against the filesystem: directories
	// that exist and hold a package.json, relative to the root, sorted.
	Workspaces []string
	// Nx is true when nx.json is present.
	Nx bool

	// Remote is the GitHub repository, as owner/name, from the origin remote.
	Remote string

	// HasPackageJSON is false for a repository that is not a Node project at all,
	// which is not an error: the deterministic analyzers that spawn binaries still
	// work, and the TypeScript plugin declines.
	HasPackageJSON bool
}

// Node is the subset of package.json the installer reads.
type packageJSON struct {
	Name            string            `json:"name"`
	Workspaces      json.RawMessage   `json:"workspaces"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	PackageManager  string            `json:"packageManager"`
}

// Detect inspects a repository. It never fails on a missing file: absence is an
// answer, and the plan it produces says what it could not find.
//
// A root that is not an existing directory is a different matter. `init` creates
// the directories it writes into, so a typo in --root would otherwise produce a
// complete, successful-looking install somewhere nobody will ever look while the
// real repository stays untouched.
func Detect(root string) (Detected, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return Detected{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Detected{}, fmt.Errorf("%s: %w", abs, err)
	}
	if !info.IsDir() {
		return Detected{}, fmt.Errorf("%s is not a directory", abs)
	}
	d := Detected{Root: abs}

	pkg, err := readPackageJSON(abs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// Not a Node project. The binary-spawning analyzers still work.
	case err != nil:
		return Detected{}, fmt.Errorf("reading package.json: %w", err)
	default:
		d.HasPackageJSON = true
		d.WorkspaceGlobs = parseWorkspaces(pkg.Workspaces)
		d.TestRunner = detectTestRunner(pkg)
	}

	d.PackageManager, d.LockfileBasis = detectPackageManager(abs, pkg)
	d.TSConfig = firstPresent(abs, "tsconfig.json")
	d.ESLintConfig = firstPresent(abs,
		"eslint.config.js", "eslint.config.mjs", "eslint.config.cjs", "eslint.config.ts")
	d.DepCruiserConfig = firstPresent(abs,
		".dependency-cruiser.js", ".dependency-cruiser.cjs", ".dependency-cruiser.mjs",
		".dependency-cruiser.json", ".dependency-cruiser.jsonc")

	if _, err := os.Stat(filepath.Join(abs, "nx.json")); err == nil {
		d.Nx = true
	}
	if len(d.WorkspaceGlobs) == 0 {
		d.WorkspaceGlobs = pnpmWorkspaces(abs)
	}
	d.Workspaces = expandWorkspaces(abs, d.WorkspaceGlobs)
	d.Remote = detectRemote(abs)

	return d, nil
}

func readPackageJSON(root string) (packageJSON, error) {
	raw, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return packageJSON{}, err
	}
	var pkg packageJSON
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return packageJSON{}, err
	}
	return pkg, nil
}

// knownManagers is the closed set. The packageManager field is repository
// content — an arbitrary string from a file this tool did not write — and it is
// interpolated into a generated GitHub Actions workflow. A value outside this set
// is discarded rather than passed through, so no package.json can contribute a
// step to a workflow that holds `pull-requests: write` and a token.
//
// The closed set is also simply correct: these four are the managers the
// generated workflow knows how to install with.
var knownManagers = map[string]bool{"npm": true, "pnpm": true, "yarn": true, "bun": true}

// detectPackageManager prefers the lockfile over the packageManager field: the
// lockfile is what is actually there, and a stale packageManager field is common.
func detectPackageManager(root string, pkg packageJSON) (manager, basis string) {
	for _, candidate := range []struct{ lockfile, manager string }{
		{"pnpm-lock.yaml", "pnpm"},
		{"yarn.lock", "yarn"},
		{"bun.lockb", "bun"},
		{"package-lock.json", "npm"},
	} {
		if _, err := os.Stat(filepath.Join(root, candidate.lockfile)); err == nil {
			return candidate.manager, candidate.lockfile
		}
	}
	if name, _, ok := strings.Cut(pkg.PackageManager, "@"); ok && knownManagers[name] {
		return name, "the packageManager field in package.json"
	}
	return "", ""
}

// detectTestRunner looks at declared dependencies rather than at config files: a
// repository can have a vitest.config.ts left over from a migration.
func detectTestRunner(pkg packageJSON) string {
	for _, runner := range []string{"vitest", "jest"} {
		if _, ok := pkg.DevDependencies[runner]; ok {
			return runner
		}
		if _, ok := pkg.Dependencies[runner]; ok {
			return runner
		}
	}
	return ""
}

// parseWorkspaces handles both shapes npm accepts: an array of globs, and an
// object with a "packages" array.
func parseWorkspaces(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var globs []string
	if err := json.Unmarshal(raw, &globs); err == nil {
		return globs
	}
	var object struct {
		Packages []string `json:"packages"`
	}
	if err := json.Unmarshal(raw, &object); err == nil {
		return object.Packages
	}
	return nil
}

// pnpmWorkspaces reads pnpm-workspace.yaml, which keeps its package globs outside
// package.json. Parsed by hand rather than with the YAML decoder, because the
// file's only interesting content is a flat list and the installer should not
// fail on a feature of it we do not understand.
func pnpmWorkspaces(root string) []string {
	raw, err := os.ReadFile(filepath.Join(root, "pnpm-workspace.yaml"))
	if err != nil {
		return nil
	}
	var globs []string
	inPackages := false
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "packages:"):
			inPackages = true
		case inPackages && strings.HasPrefix(trimmed, "- "):
			globs = append(globs, strings.Trim(strings.TrimPrefix(trimmed, "- "), `"'`))
		case inPackages && trimmed != "" && !strings.HasPrefix(trimmed, "#"):
			inPackages = false
		}
	}
	return globs
}

// expandWorkspaces resolves the declared globs against the filesystem. A glob is
// a claim about layout; what the installer needs is the list of packages that are
// actually there, because a repository routinely declares `packages/*` and holds
// two directories under it.
//
// A directory counts as a workspace only if it has a package.json: `packages/*`
// also matches a stray `packages/README.md` and every build output left behind.
//
// `**` is narrowed to `*`. Go's filepath.Glob has no recursive wildcard, and one
// level covers the layouts npm and pnpm actually produce; a deeper nesting is
// reported as zero workspaces rather than guessed at, and the plan says so.
func expandWorkspaces(root string, globs []string) []string {
	var positive, negative []string
	for _, glob := range globs {
		if pattern, ok := strings.CutPrefix(glob, "!"); ok {
			negative = append(negative, pattern)
			continue
		}
		positive = append(positive, glob)
	}

	found := map[string]bool{}
	for _, glob := range positive {
		for _, dir := range matchDirs(root, glob) {
			found[dir] = true
		}
	}
	for _, glob := range negative {
		for _, dir := range matchDirs(root, glob) {
			delete(found, dir)
		}
	}

	out := make([]string, 0, len(found))
	for dir := range found {
		out = append(out, dir)
	}
	sort.Strings(out)
	return out
}

// matchDirs returns the package directories one glob matches, as slash-separated
// paths relative to root.
func matchDirs(root, glob string) []string {
	glob = strings.ReplaceAll(glob, "**", "*")
	matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(glob)))
	if err != nil {
		// The only error Glob reports is a malformed pattern, which is the
		// repository's problem to fix and not a reason to fail the install.
		return nil
	}
	var out []string
	for _, match := range matches {
		if _, err := os.Stat(filepath.Join(match, "package.json")); err != nil {
			continue
		}
		rel, err := filepath.Rel(root, match)
		if err != nil {
			continue
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out
}

// detectRemote reads owner/name from the origin remote, so the installer does not
// have to ask for something git already knows.
func detectRemote(root string) string {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return parseRemote(strings.TrimSpace(string(out)))
}

// parseRemote handles the three forms a GitHub remote takes.
func parseRemote(url string) string {
	url = strings.TrimSuffix(url, ".git")
	switch {
	case strings.HasPrefix(url, "git@"):
		_, path, ok := strings.Cut(url, ":")
		if !ok {
			return ""
		}
		return lastTwoSegments(path)
	case strings.HasPrefix(url, "https://"), strings.HasPrefix(url, "http://"), strings.HasPrefix(url, "ssh://"):
		// The host has to come off before the path is split, or a URL whose path
		// is one segment long reads its own hostname as the owner.
		_, rest, ok := strings.Cut(url, "://")
		if !ok {
			return ""
		}
		_, path, ok := strings.Cut(rest, "/")
		if !ok {
			return ""
		}
		return lastTwoSegments(path)
	default:
		return ""
	}
}

func lastTwoSegments(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	owner, name := parts[len(parts)-2], parts[len(parts)-1]
	if owner == "" || name == "" {
		return ""
	}
	return owner + "/" + name
}

func firstPresent(root string, names ...string) string {
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			return name
		}
	}
	return ""
}
