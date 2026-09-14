// SPDX-License-Identifier: MIT

// Package diff works out what a pull request actually changed, and filters
// findings down to it.
//
// Diff scoping is not an optimisation. Reporting a pre-existing problem on an
// unrelated pull request is the fastest way to train a team to ignore the tool
// (ADR-0007), so every analyzer's output passes through Filter before it reaches
// a reporter, whether or not the analyzer scoped itself.
package diff

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/adamtait/reviewer/pkg/finding"
	"github.com/adamtait/reviewer/pkg/plugin"
)

// Status is what happened to a file in the diff.
type Status string

const (
	StatusAdded    Status = "added"
	StatusModified Status = "modified"
	StatusDeleted  Status = "deleted"
	StatusRenamed  Status = "renamed"
)

// File is one changed file and the line ranges the diff touched in its new
// contents. A deleted file has no ranges, and nothing can be reported against it.
type File struct {
	Path string
	// OldPath is set for a rename, so a finding can explain where a file came from.
	OldPath string
	Status  Status
	// Ranges are inclusive [start, end] line pairs in the file's new contents.
	Ranges [][2]int
	// Binary files have no line ranges and are never reported against.
	Binary bool
}

// Contains reports whether any part of [start, end] falls inside a changed range.
// A finding that straddles changed and unchanged lines counts as changed: the
// change is what made the span worth reporting.
func (f File) Contains(start, end int) bool {
	if end < start {
		end = start
	}
	for _, r := range f.Ranges {
		if start <= r[1] && end >= r[0] {
			return true
		}
	}
	return false
}

// Changed returns the files a branch changed relative to base, using the merge
// base rather than the tip so that commits landing on the base branch after this
// one branched do not appear as this pull request's work.
func Changed(ctx context.Context, root, base string) ([]File, error) {
	if base == "" {
		return nil, fmt.Errorf("no base ref given")
	}
	out, err := git(ctx, root, "diff", "--unified=0", "--no-color", "--find-renames",
		"--merge-base", base, "HEAD")
	if err != nil {
		return nil, err
	}
	return parse(out)
}

// Unified returns the diff itself, with context, for the model lane.
//
// The line-range parser works from `--unified=0` because zero context is what
// makes "which lines changed" unambiguous. A model needs the opposite: without
// surrounding lines it cannot tell what the change is part of. So this is a second
// invocation rather than a reinterpretation of the first — two questions, two
// answers, neither derived from the other.
// Unified returns the diff. staged selects the index against HEAD; otherwise base
// must name a ref this checkout has.
//
// An empty base with staged false is refused rather than quietly falling back to
// the index. That fallback existed, and it meant a pull request reviewed on a
// shallow clone sent the developer's local staged changes to a model provider —
// content the secrets gate had never scanned, because the gate saw the pull
// request's files and the prompt carried something else entirely.
func Unified(ctx context.Context, root, base string, staged bool, contextLines int) (string, error) {
	if contextLines < 0 {
		contextLines = 0
	}
	args := []string{"diff", "--no-color", "--find-renames", fmt.Sprintf("--unified=%d", contextLines)}
	switch {
	case staged:
		args = append(args, "--cached")
	case base != "":
		args = append(args, "--merge-base", base, "HEAD")
	default:
		return "", fmt.Errorf("no base ref: this checkout cannot produce the diff for this review")
	}
	return git(ctx, root, args...)
}

// Staged returns the files staged in the index, for the pre-commit and agent
// surfaces where there is no base branch to compare against.
func Staged(ctx context.Context, root string) ([]File, error) {
	out, err := git(ctx, root, "diff", "--unified=0", "--no-color", "--find-renames", "--cached")
	if err != nil {
		return nil, err
	}
	return parse(out)
}

func git(ctx context.Context, root string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return "", fmt.Errorf("git %s: %w%s", strings.Join(args, " "), err, detail(stderr))
	}
	return string(out), nil
}

func detail(stderr string) string {
	if stderr == "" {
		return ""
	}
	return ": " + stderr
}

// ToPluginFiles converts to the protocol's shape. Deleted and binary files are
// dropped: an analyzer cannot say anything useful about either.
func ToPluginFiles(files []File) []plugin.ChangedFile {
	out := make([]plugin.ChangedFile, 0, len(files))
	for _, f := range files {
		if f.Status == StatusDeleted || f.Binary || len(f.Ranges) == 0 {
			continue
		}
		out = append(out, plugin.ChangedFile{Path: f.Path, Status: string(f.Status), Ranges: f.Ranges})
	}
	return out
}

// Filter drops findings that do not land on a changed line, and reports how many
// it dropped so a run can say so rather than silently discarding an analyzer's
// work.
func Filter(findings []finding.Finding, files []File) (kept []finding.Finding, dropped int) {
	byPath := make(map[string]File, len(files))
	for _, f := range files {
		byPath[f.Path] = f
	}
	kept = make([]finding.Finding, 0, len(findings))
	for _, fnd := range findings {
		file, ok := byPath[fnd.File]
		if !ok || file.Status == StatusDeleted || file.Binary {
			dropped++
			continue
		}
		start, end := fnd.Span()
		if !file.Contains(start, end) {
			dropped++
			continue
		}
		kept = append(kept, fnd)
	}
	return kept, dropped
}

// FromPatches builds the changed-file set from a pull request's per-file patches,
// for the case where the base commit is not in the local checkout.
//
// A shallow clone is the normal case on a CI runner that forgot fetch-depth, and
// on a poller reviewing a branch it has never fetched. The API's patch is the same
// unified diff git would have produced, so it goes through the same parser.
func FromPatches(files []PatchedFile) ([]File, error) {
	var out []File
	for _, f := range files {
		if f.Patch == "" {
			// Binary files and very large diffs carry no patch. Recording the file
			// with no ranges means nothing can be reported against it, which is
			// the same treatment a binary file gets locally.
			out = append(out, File{Path: f.Path, Status: statusFrom(f.Status), Binary: true})
			continue
		}
		// The API omits the "diff --git" header, so it is reconstructed: the parser
		// keys file identity off it.
		header := fmt.Sprintf("diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n", f.Path, f.Path, f.Path, f.Path)
		parsed, err := parse(header + f.Patch + "\n")
		if err != nil {
			return nil, fmt.Errorf("parsing the patch for %s: %w", f.Path, err)
		}
		for i := range parsed {
			parsed[i].Path = f.Path
			parsed[i].Status = statusFrom(f.Status)
		}
		out = append(out, parsed...)
	}
	return out, nil
}

// PatchedFile is one file from a pull request's file list.
type PatchedFile struct {
	Path   string
	Status string
	Patch  string
}

func statusFrom(apiStatus string) Status {
	switch apiStatus {
	case "added":
		return StatusAdded
	case "removed":
		return StatusDeleted
	case "renamed":
		return StatusRenamed
	default:
		return StatusModified
	}
}

// HasCommit reports whether a revision is present in the local checkout, which is
// what decides between diffing locally and asking the API.
func HasCommit(ctx context.Context, root, rev string) bool {
	_, err := git(ctx, root, "cat-file", "-e", rev+"^{commit}")
	return err == nil
}

// TotalLines counts the changed lines across every file, for the run summary.
func TotalLines(files []File) int {
	n := 0
	for _, f := range files {
		for _, r := range f.Ranges {
			n += r[1] - r[0] + 1
		}
	}
	return n
}
