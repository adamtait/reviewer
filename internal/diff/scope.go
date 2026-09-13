// SPDX-License-Identifier: MIT

package diff

import (
	"path"
	"sort"
	"strings"
)

// Affected returns the projects a change touches, for a repository that declares
// projects at all. An empty result means the whole repository, which is what the
// protocol's empty Projects list already means.
//
// This is a performance hint and nothing more, and that is a claim worth
// defending: narrowing analysis to the affected projects cannot hide a finding.
// Every finding is filtered to the changed lines before it is reported (ADR-0007),
// so a project with no changed line can produce nothing reportable — whatever a
// change elsewhere did to it. A project with a changed line is in this set by
// construction.
//
// That is also why no dependency graph is consulted and no `nx affected` is
// invoked: the graph would add projects whose findings would all be dropped
// again by the diff filter.
func Affected(files []File, projects []string) []string {
	if len(projects) == 0 {
		return nil
	}

	cleaned := make([]string, 0, len(projects))
	for _, project := range projects {
		if p := path.Clean(strings.TrimPrefix(project, "./")); p != "" && p != "." {
			cleaned = append(cleaned, p)
		}
	}

	hit := map[string]bool{}
	for _, f := range files {
		if f.Status == StatusDeleted {
			// A deleted file's project has nothing left to analyze on its account.
			// It stays out unless something else in it changed.
			continue
		}
		owner, ok := ownerOf(f.Path, cleaned)
		if !ok {
			// A changed file belonging to no declared project — a root config, a
			// workflow, a shared script — can affect anything. Widening to the
			// whole repository is the honest answer, and the slow one.
			return nil
		}
		hit[owner] = true
	}

	out := make([]string, 0, len(hit))
	for project := range hit {
		out = append(out, project)
	}
	sort.Strings(out)
	return out
}

// ownerOf finds the project a file sits in. The longest match wins, so a project
// nested inside another is credited with its own files rather than its parent's.
func ownerOf(file string, projects []string) (string, bool) {
	file = path.Clean(file)
	best := ""
	for _, project := range projects {
		if file == project || strings.HasPrefix(file, project+"/") {
			if len(project) > len(best) {
				best = project
			}
		}
	}
	return best, best != ""
}
