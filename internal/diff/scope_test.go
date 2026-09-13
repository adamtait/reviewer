// SPDX-License-Identifier: MIT

package diff

import (
	"strings"
	"testing"
)

func files(paths ...string) []File {
	out := make([]File, 0, len(paths))
	for _, p := range paths {
		out = append(out, File{Path: p, Status: StatusModified, Ranges: [][2]int{{1, 1}}})
	}
	return out
}

func TestAffected(t *testing.T) {
	projects := []string{"packages/pkg-a", "packages/pkg-b", "packages/pkg-b/nested"}

	for _, tc := range []struct {
		name     string
		files    []File
		projects []string
		want     string
	}{
		{
			name:     "one project touched",
			files:    files("packages/pkg-a/src/index.ts"),
			projects: projects,
			want:     "packages/pkg-a",
		},
		{
			name:     "two projects touched",
			files:    files("packages/pkg-a/src/index.ts", "packages/pkg-b/src/index.ts"),
			projects: projects,
			want:     "packages/pkg-a,packages/pkg-b",
		},
		{
			// The longest match wins, or a nested project's files would be
			// credited to its parent and the parent analyzed instead.
			name:     "a nested project owns its own files",
			files:    files("packages/pkg-b/nested/src/index.ts"),
			projects: projects,
			want:     "packages/pkg-b/nested",
		},
		{
			// A root file can affect anything, so narrowing would be a guess.
			name:     "a file outside every project widens to the whole repository",
			files:    files("packages/pkg-a/src/index.ts", "tsconfig.json"),
			projects: projects,
			want:     "",
		},
		{
			name:     "a repository that declares no projects",
			files:    files("src/index.ts"),
			projects: nil,
			want:     "",
		},
		{
			name:     "the project directory's own file",
			files:    files("packages/pkg-a/package.json"),
			projects: projects,
			want:     "packages/pkg-a",
		},
		{
			// A prefix that is not a path boundary is not a match.
			name:     "a sibling with a shared prefix",
			files:    files("packages/pkg-alpha/src/index.ts"),
			projects: []string{"packages/pkg-a"},
			want:     "",
		},
		{
			name:     "declared with a leading ./",
			files:    files("packages/pkg-a/src/index.ts"),
			projects: []string{"./packages/pkg-a"},
			want:     "packages/pkg-a",
		},
		{
			name:     "no files changed",
			files:    nil,
			projects: projects,
			want:     "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := strings.Join(Affected(tc.files, tc.projects), ","); got != tc.want {
				t.Errorf("Affected() = %q, want %q", got, tc.want)
			}
		})
	}
}

// A deleted file leaves nothing to analyze in its project, but it must not widen
// the scope either — a deletion inside a project is still confined to it.
func TestAffectedIgnoresDeletions(t *testing.T) {
	projects := []string{"packages/pkg-a", "packages/pkg-b"}
	changed := []File{
		{Path: "packages/pkg-a/src/index.ts", Status: StatusModified, Ranges: [][2]int{{1, 1}}},
		{Path: "packages/pkg-b/src/gone.ts", Status: StatusDeleted},
	}
	if got := strings.Join(Affected(changed, projects), ","); got != "packages/pkg-a" {
		t.Errorf("Affected() = %q, want only the modified project", got)
	}
}

// A deletion outside every project is the one case where a deleted file could
// widen the scope, and it must not: there is nothing to analyze either way.
func TestAffectedIsNotWidenedByADeletionOutsideEveryProject(t *testing.T) {
	changed := []File{
		{Path: "packages/pkg-a/src/index.ts", Status: StatusModified, Ranges: [][2]int{{1, 1}}},
		{Path: "old-script.sh", Status: StatusDeleted},
	}
	if got := strings.Join(Affected(changed, []string{"packages/pkg-a"}), ","); got != "packages/pkg-a" {
		t.Errorf("Affected() = %q, want the deletion ignored", got)
	}
}
