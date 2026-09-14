// SPDX-License-Identifier: MIT

package diff

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/adamtait/reviewer/internal/testfixture"
	"github.com/adamtait/reviewer/pkg/finding"
)

func TestChangedOnTheFixture(t *testing.T) {
	repo := testfixture.Build(t, "tiny-ts-repo")

	files, err := Changed(context.Background(), repo.Root, repo.Base)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]File{}
	for _, f := range files {
		got[f.Path] = f
	}
	want := []string{"src/config.ts", "src/domain/order.ts", "src/infra/http.ts"}
	if len(got) != len(want) {
		t.Fatalf("want %d changed files %v, got %d: %+v", len(want), want, len(got), files)
	}
	for _, path := range want {
		if _, ok := got[path]; !ok {
			t.Fatalf("want %s among the changed files, got %+v", path, files)
		}
	}

	// The new file is reported as added, and every one of its lines is changed.
	cfg := got["src/config.ts"]
	if cfg.Status != StatusAdded {
		t.Fatalf("want src/config.ts added, got %q", cfg.Status)
	}
	if len(cfg.Ranges) != 1 || cfg.Ranges[0][0] != 1 {
		t.Fatalf("want one range starting at line 1, got %v", cfg.Ranges)
	}

	if n := TotalLines(files); n < 10 || n > 30 {
		t.Fatalf("changed-line count %d is implausible for this fixture; the fixture or the parser changed", n)
	}

	// The boundary violation is on the first line of the modified file, and must
	// be inside a changed range or dependency-cruiser's finding would be dropped.
	order := got["src/domain/order.ts"]
	if !order.Contains(1, 1) {
		t.Fatalf("want line 1 of order.ts to be changed, got ranges %v", order.Ranges)
	}
}

func TestFilterDropsFindingsOutsideTheDiff(t *testing.T) {
	files := []File{
		{Path: "a.ts", Status: StatusModified, Ranges: [][2]int{{10, 12}, {40, 40}}},
		{Path: "gone.ts", Status: StatusDeleted},
		{Path: "logo.png", Status: StatusModified, Binary: true},
	}
	at := func(path string, line, endLine int) finding.Finding {
		return finding.Finding{File: path, Line: line, EndLine: endLine, RuleID: "r"}
	}

	kept, dropped := Filter([]finding.Finding{
		at("a.ts", 10, 0),   // first changed line
		at("a.ts", 12, 0),   // last changed line
		at("a.ts", 40, 0),   // second range
		at("a.ts", 9, 0),    // just before
		at("a.ts", 13, 0),   // just after
		at("a.ts", 8, 11),   // straddles: the change is what made it worth saying
		at("b.ts", 1, 0),    // file not in the diff at all
		at("gone.ts", 1, 0), // deleted
		at("logo.png", 1, 0),
	}, files)

	if len(kept) != 4 {
		t.Fatalf("want 4 findings kept, got %d: %+v", len(kept), kept)
	}
	if dropped != 5 {
		t.Fatalf("want 5 dropped, got %d", dropped)
	}
}

func TestParseHandlesTheAwkwardCases(t *testing.T) {
	tests := []struct {
		name string
		diff string
		want []File
	}{
		{
			name: "rename with an edit",
			diff: `diff --git a/old/name.ts b/new/name.ts
similarity index 87%
rename from old/name.ts
rename to new/name.ts
--- a/old/name.ts
+++ b/new/name.ts
@@ -4,0 +5,2 @@ export function f() {
+  const added = 1;
+  return added;
`,
			want: []File{{
				Path: "new/name.ts", OldPath: "old/name.ts", Status: StatusRenamed,
				Ranges: [][2]int{{5, 6}},
			}},
		},
		{
			name: "deletion contributes no range",
			diff: `diff --git a/dead.ts b/dead.ts
deleted file mode 100644
--- a/dead.ts
+++ /dev/null
@@ -1,3 +0,0 @@
-a
-b
-c
`,
			want: []File{{Path: "dead.ts", OldPath: "dead.ts", Status: StatusDeleted}},
		},
		{
			name: "binary file",
			diff: `diff --git a/logo.png b/logo.png
index 1234567..89abcde 100644
Binary files a/logo.png and b/logo.png differ
`,
			want: []File{{Path: "logo.png", OldPath: "logo.png", Status: StatusModified, Binary: true}},
		},
		{
			name: "single-line hunk with no count",
			diff: `diff --git a/x.ts b/x.ts
--- a/x.ts
+++ b/x.ts
@@ -7 +7 @@
-old
+new
`,
			want: []File{{Path: "x.ts", OldPath: "x.ts", Status: StatusModified, Ranges: [][2]int{{7, 7}}}},
		},
		{
			name: "pure deletion hunk inside a modified file",
			diff: `diff --git a/y.ts b/y.ts
--- a/y.ts
+++ b/y.ts
@@ -3,2 +2,0 @@
-gone
-also gone
@@ -9,0 +8,1 @@
+added
`,
			want: []File{{Path: "y.ts", OldPath: "y.ts", Status: StatusModified, Ranges: [][2]int{{8, 8}}}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parse(tc.diff)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("\n got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestToPluginFilesDropsWhatAnalyzersCannotUse(t *testing.T) {
	got := ToPluginFiles([]File{
		{Path: "a.ts", Status: StatusModified, Ranges: [][2]int{{1, 2}}},
		{Path: "gone.ts", Status: StatusDeleted},
		{Path: "logo.png", Status: StatusModified, Binary: true, Ranges: [][2]int{{1, 1}}},
		{Path: "mode-only.ts", Status: StatusModified},
	})
	if len(got) != 1 || got[0].Path != "a.ts" {
		t.Fatalf("want only a.ts, got %+v", got)
	}
}

// Changed uses the merge base, so a commit landing on the base branch after this
// one branched is not attributed to this pull request.
func TestChangedUsesTheMergeBaseNotTheTip(t *testing.T) {
	repo := testfixture.Build(t, "tiny-ts-repo")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo.Root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Move main forward with an unrelated file, then review the feature branch.
	run("branch", "feature")
	run("checkout", "-q", "main")
	run("reset", "-q", "--hard", repo.Base)
	if err := os.WriteFile(filepath.Join(repo.Root, "UNRELATED.md"), []byte("moved on\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "unrelated work on main")
	run("checkout", "-q", "feature")

	files, err := Changed(context.Background(), repo.Root, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Path == "UNRELATED.md" {
			t.Fatalf("a commit on the base branch was attributed to this branch: %+v", files)
		}
	}
	if len(files) != 3 {
		t.Fatalf("want the branch's own 3 files, got %+v", files)
	}
}

func TestChangedRequiresABase(t *testing.T) {
	if _, err := Changed(context.Background(), t.TempDir(), ""); err == nil {
		t.Fatal("want an error when no base ref is given")
	}
}

// Hunk bodies must never be read as headers. With --unified=0 every body line is
// prefixed, so an added line whose text begins "++ " looks exactly like a
// "+++ " header and a removed line beginning "-- " — an ordinary SQL, Lua or
// Haskell comment — looks like a "--- " header. Either one rewrites the file's
// path or status, and every finding for that file is then dropped as outside the
// diff.
func TestHunkBodiesAreNotParsedAsHeaders(t *testing.T) {
	tests := []struct {
		name string
		diff string
		want File
	}{
		{
			name: "added line beginning with plus signs",
			diff: `diff --git a/real.ts b/real.ts
--- a/real.ts
+++ b/real.ts
@@ -1,0 +2,2 @@
++ not a header, just text
+++ b/attacker-controlled.ts
`,
			want: File{Path: "real.ts", OldPath: "real.ts", Status: StatusModified, Ranges: [][2]int{{2, 3}}},
		},
		{
			name: "removed SQL comment",
			diff: `diff --git a/schema.sql b/schema.sql
--- a/schema.sql
+++ b/schema.sql
@@ -4,1 +4,1 @@
-- drop the old column
+-- keep the old column
`,
			want: File{Path: "schema.sql", OldPath: "schema.sql", Status: StatusModified, Ranges: [][2]int{{4, 4}}},
		},
		{
			name: "body line claiming a new file mode",
			diff: `diff --git a/doc.md b/doc.md
--- a/doc.md
+++ b/doc.md
@@ -1,0 +2,1 @@
+new file mode 100644
`,
			want: File{Path: "doc.md", OldPath: "doc.md", Status: StatusModified, Ranges: [][2]int{{2, 2}}},
		},
		{
			name: "body line claiming the file is binary",
			diff: `diff --git a/notes.txt b/notes.txt
--- a/notes.txt
+++ b/notes.txt
@@ -1,0 +2,1 @@
+Binary files a/x and b/x differ
`,
			want: File{Path: "notes.txt", OldPath: "notes.txt", Status: StatusModified, Ranges: [][2]int{{2, 2}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parse(tc.diff)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 {
				t.Fatalf("want one file, got %+v", got)
			}
			if !reflect.DeepEqual(got[0], tc.want) {
				t.Fatalf("\n got %+v\nwant %+v", got[0], tc.want)
			}
		})
	}
}

// Two files in one diff, the first containing a body line that looks like a
// header: the second file's parse must be unaffected.
func TestHeaderRecoveryBetweenFiles(t *testing.T) {
	got, err := parse(`diff --git a/first.ts b/first.ts
--- a/first.ts
+++ b/first.ts
@@ -1,0 +2,1 @@
+++ b/bogus.ts
diff --git a/second.ts b/second.ts
--- a/second.ts
+++ b/second.ts
@@ -9,0 +10,1 @@
+real change
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want two files, got %+v", got)
	}
	if got[0].Path != "first.ts" || got[1].Path != "second.ts" {
		t.Fatalf("want first.ts then second.ts, got %s then %s", got[0].Path, got[1].Path)
	}
	if !reflect.DeepEqual(got[1].Ranges, [][2]int{{10, 10}}) {
		t.Fatalf("want the second file's range intact, got %v", got[1].Ranges)
	}
}
