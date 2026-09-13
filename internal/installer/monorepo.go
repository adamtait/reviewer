// SPDX-License-Identifier: MIT

package installer

import (
	"io/fs"

	"path/filepath"
	"sort"
	"strings"
)

// maxProjectDepth bounds the walk for Nx projects. Nx puts project.json one or
// two levels down by convention; a deeper search costs a full tree walk on a
// large repository to find nothing.
const maxProjectDepth = 4

// skipDirs are never descended into. node_modules is the important one: it holds
// thousands of package.json files and none of them is this repository's project.
var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, "dist": true, "build": true,
	"out": true, "coverage": true, ".next": true, ".turbo": true, ".nx": true,
}

// Projects is the list written into the generated config: the directories this
// repository is divided into, each of which a change can be confined to.
//
// Workspaces come from the package manager's own declaration, resolved during
// detection. Nx projects come from project.json files, because an Nx repository
// can have projects the package manager's workspace globs do not mention.
func Projects(d Detected) []string {
	found := map[string]bool{}
	for _, workspace := range d.Workspaces {
		found[workspace] = true
	}
	if d.Nx {
		for _, project := range nxProjects(d.Root) {
			found[project] = true
		}
	}

	out := make([]string, 0, len(found))
	for project := range found {
		out = append(out, project)
	}
	sort.Strings(out)
	return out
}

// nxProjects finds the directories holding a project.json. Nx's own graph is not
// consulted: reading it means running Nx, and the project list is the only part
// of it this tool needs — see diff.Affected for why the dependency edges do not
// change what gets reported.
func nxProjects(root string) []string {
	var out []string
	err := filepath.WalkDir(root, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable directory is not a reason to abandon the walk: the
			// project list is advisory, and a partial one is still useful.
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		if entry.IsDir() {
			if p == root {
				return nil
			}
			if skipDirs[entry.Name()] || strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			if len(strings.Split(filepath.ToSlash(rel), "/")) >= maxProjectDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() == "project.json" && filepath.Dir(rel) != "." {
			out = append(out, filepath.ToSlash(filepath.Dir(rel)))
		}
		return nil
	})
	if err != nil {
		return nil
	}
	return out
}

// projectsUnreadable reports whether Nx is declared but no project could be found,
// which is worth saying: the install is correct but slower than it should be, and
// silence would leave nobody to notice.
func projectsUnreadable(d Detected, projects []string) bool {
	return (d.Nx || len(d.WorkspaceGlobs) > 0) && len(projects) == 0
}
