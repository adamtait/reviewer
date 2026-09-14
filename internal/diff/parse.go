// SPDX-License-Identifier: MIT

package diff

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// parse reads a unified diff produced with --unified=0 and returns one File per
// changed path.
//
// --unified=0 matters: with context lines, every hunk header would claim lines
// that the commit did not touch, and findings on unchanged code would be
// reported as if the pull request had caused them.
func parse(out string) ([]File, error) {
	var (
		files   []File
		current *File
		// inHunk guards against reading a hunk's body as a header. With
		// --unified=0 every body line begins with '+', '-' or '\', so an added
		// line whose text begins "++ " looks exactly like a '+++ ' header, and a
		// removed line beginning "-- " — an ordinary SQL, Lua or Haskell comment —
		// looks like a '--- ' header. Either one silently rewrites the file's path
		// or status, and the findings for that file are then dropped as being
		// outside the diff.
		inHunk bool
	)
	flush := func() {
		if current != nil {
			files = append(files, *current)
			current = nil
		}
	}

	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	for sc.Scan() {
		line := sc.Text()

		// A body line can never start at column zero with these, because git
		// always prefixes it. Anything else is inside the hunk and is content.
		if inHunk && !strings.HasPrefix(line, "@@") && !strings.HasPrefix(line, "diff --git ") {
			continue
		}

		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			inHunk = false
			current = &File{Status: StatusModified}
			// The header paths are the fallback when there is no ---/+++ pair,
			// which happens for a pure rename or a mode change.
			if a, b, ok := headerPaths(line); ok {
				current.OldPath, current.Path = a, b
			}

		case current == nil:
			// Output before the first header; nothing to attach it to.

		case strings.HasPrefix(line, "new file mode"):
			current.Status = StatusAdded
			current.OldPath = ""

		case strings.HasPrefix(line, "deleted file mode"):
			current.Status = StatusDeleted

		case strings.HasPrefix(line, "rename from "):
			current.Status = StatusRenamed
			current.OldPath = strings.TrimPrefix(line, "rename from ")

		case strings.HasPrefix(line, "rename to "):
			current.Status = StatusRenamed
			current.Path = strings.TrimPrefix(line, "rename to ")

		case strings.HasPrefix(line, "Binary files "), strings.HasPrefix(line, "GIT binary patch"):
			current.Binary = true

		case strings.HasPrefix(line, "+++ "):
			path := strings.TrimPrefix(line, "+++ ")
			if path == "/dev/null" {
				current.Status = StatusDeleted
				continue
			}
			current.Path = stripPrefix(path)

		case strings.HasPrefix(line, "--- "):
			path := strings.TrimPrefix(line, "--- ")
			if path == "/dev/null" {
				current.Status = StatusAdded
				continue
			}
			if current.OldPath == "" {
				current.OldPath = stripPrefix(path)
			}

		case strings.HasPrefix(line, "@@"):
			inHunk = true
			start, count, err := hunk(line)
			if err != nil {
				return nil, err
			}
			if count == 0 {
				// A pure deletion hunk: nothing exists in the new file to report
				// against, so it contributes no range.
				continue
			}
			current.Ranges = append(current.Ranges, [2]int{start, start + count - 1})
		}
	}
	flush()

	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading diff: %w", err)
	}
	return files, nil
}

// headerPaths pulls both paths out of `diff --git a/x b/y`. It gives up on paths
// containing " b/", which git quotes instead; the ---/+++ lines cover those.
func headerPaths(line string) (old, new string, ok bool) {
	rest := strings.TrimPrefix(line, "diff --git ")
	i := strings.Index(rest, " b/")
	if i < 0 || !strings.HasPrefix(rest, "a/") {
		return "", "", false
	}
	return rest[len("a/"):i], rest[i+len(" b/"):], true
}

// stripPrefix removes git's a/ or b/ prefix and any trailing tab-separated
// timestamp.
func stripPrefix(path string) string {
	if i := strings.IndexByte(path, '\t'); i >= 0 {
		path = path[:i]
	}
	path = strings.TrimPrefix(path, "a/")
	path = strings.TrimPrefix(path, "b/")
	return path
}

// hunk reads the new-file side of `@@ -old,n +new,m @@`. With --unified=0 the
// count is 0 for a pure deletion and defaults to 1 when omitted.
func hunk(line string) (start, count int, err error) {
	i := strings.Index(line, "+")
	if i < 0 {
		return 0, 0, fmt.Errorf("hunk header has no new-file range: %q", line)
	}
	rest := line[i+1:]
	if j := strings.IndexAny(rest, " @"); j >= 0 {
		rest = rest[:j]
	}

	startText, countText, hasCount := strings.Cut(rest, ",")
	start, err = strconv.Atoi(startText)
	if err != nil {
		return 0, 0, fmt.Errorf("hunk header has an unreadable start line: %q", line)
	}
	if !hasCount {
		return start, 1, nil
	}
	count, err = strconv.Atoi(countText)
	if err != nil {
		return 0, 0, fmt.Errorf("hunk header has an unreadable line count: %q", line)
	}
	return start, count, nil
}
