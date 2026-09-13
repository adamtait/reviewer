// SPDX-License-Identifier: MIT

// Package testfixture builds the fixture repositories under testdata into real
// git repositories in a temporary directory.
//
// The fixtures are stored as plain files rather than as checked-in git
// repositories because a nested .git directory cannot live inside this
// repository without becoming a submodule, and a submodule would make the
// fixtures invisible to a plain clone.
package testfixture

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Repo is a fixture repository built on disk.
type Repo struct {
	// Root is the working tree.
	Root string
	// Base is the ref of the first commit, the diff's merge base.
	Base string
	// Head is the ref of the second commit.
	Head string
}

// Build materialises testdata/<name> as a two-commit git repository in a
// temporary directory: base/ is committed first, then head/ is copied over it
// and committed, so the diff between the two is the change under review.
//
// The caller's working directory is not changed.
func Build(t *testing.T, name string) Repo {
	t.Helper()

	src := filepath.Join(repoRoot(t), "testdata", name)
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}

	root := t.TempDir()
	git := Git(t, root)

	git("init", "-q", "-b", "main")
	copyTree(t, filepath.Join(src, "base"), root)
	git("add", "-A")
	git("commit", "-q", "-m", "base")
	base := trim(git("rev-parse", "HEAD"))

	copyTree(t, filepath.Join(src, "head"), root)
	git("add", "-A")
	git("commit", "-q", "-m", "the change under review")
	head := trim(git("rev-parse", "HEAD"))

	return Repo{Root: root, Base: base, Head: head}
}

// Destination materialises testdata/<name> as a single-commit git repository: a
// repository the installer is pointed at, rather than a change under review. It
// returns the working tree.
//
// It is committed rather than merely copied because the installer's exit
// criterion is that a dry run leaves `git status --porcelain` empty, and an
// uncommitted tree reports every file as untracked.
func Destination(t *testing.T, name string) string {
	t.Helper()

	src := filepath.Join(repoRoot(t), "testdata", name)
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}

	root := t.TempDir()
	git := Git(t, root)
	git("init", "-q", "-b", "main")
	copyTree(t, src, root)
	git("add", "-A")
	git("commit", "-q", "-m", "the destination repository")
	return root
}

// Git runs git in root with a deterministic identity and no signing, so a fixture
// does not depend on whatever git config the machine happens to have. It fails the
// test on any non-zero exit.
func Git(t *testing.T, root string) func(args ...string) string {
	return func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
			"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
}

// copyTree copies src over dst, overwriting files and creating directories. It
// does not delete files absent from src, which is what makes head/ an overlay on
// base/ rather than a replacement.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, body, 0o644)
	})
	if err != nil {
		t.Fatalf("copying %s: %v", src, err)
	}
}

// repoRoot walks up from the working directory until it finds go.mod, so a test
// in any package can reach testdata without hard-coding how deep it sits.
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
			t.Fatal("could not find the repository root (no go.mod above the working directory)")
		}
		dir = parent
	}
}

func trim(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
