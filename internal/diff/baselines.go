// SPDX-License-Identifier: MIT

package diff

import (
	"regexp"
	"strconv"
	"strings"
)

// baseHunk reads both ranges of "@@ -12,3 +14,5 @@". A count left out is 1.
var baseHunk = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// BaseLines returns the line numbers, 1-based and in the file before the patch,
// of every line a unified patch removed. A changed line is a removed line
// followed by an added one, so it is here too; a pure addition removes nothing
// and returns none.
//
// It reads one file's patch as GitHub's API sends it, hunks and nothing else,
// and walks each hunk by the counts in its header rather than by the first
// character of each line. A removed line whose text begins "-- " is a SQL or
// Lua comment, not a header, and once a hunk's counts run out nothing more is
// read until the next "@@", so whatever follows a hunk cannot be counted as
// part of it.
func BaseLines(patch string) []int {
	var out []int
	oldLine, oldLeft, newLeft := 0, 0, 0
	for _, line := range strings.Split(patch, "\n") {
		// Every line inside a hunk starts with a space, "+", "-" or "\", so a
		// header is a header wherever it appears.
		if m := baseHunk.FindStringSubmatch(line); m != nil {
			oldLine, _ = strconv.Atoi(m[1])
			oldLeft, newLeft = hunkCount(m[2]), hunkCount(m[4])
			continue
		}
		if oldLeft <= 0 && newLeft <= 0 {
			continue
		}
		switch {
		case strings.HasPrefix(line, `\`):
			// "\ No newline at end of file" is about the line before it, and
			// is no line of either file.
		case strings.HasPrefix(line, "-"):
			out = append(out, oldLine)
			oldLine++
			oldLeft--
		case strings.HasPrefix(line, "+"):
			newLeft--
		default:
			// Context, including an empty line whose leading space a tool
			// along the way trimmed.
			oldLine++
			oldLeft--
			newLeft--
		}
	}
	return out
}

// hunkCount reads a hunk header's line count, which is 1 when left out.
func hunkCount(text string) int {
	if text == "" {
		return 1
	}
	n, err := strconv.Atoi(text)
	if err != nil {
		return 0
	}
	return n
}
