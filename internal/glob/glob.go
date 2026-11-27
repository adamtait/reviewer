// SPDX-License-Identifier: MIT

// Package glob is the one pattern grammar for paths in a repository: the risk
// map, the merge-class allowlist and CODEOWNERS-style matching all use it, so
// that a pattern means the same thing wherever it is written.
//
// The grammar is small on purpose:
//
//   - `*` matches any run of characters within one path segment, including
//     none, and including a leading dot;
//   - `**`, as a whole segment, matches any number of segments: none in the
//     middle or at the start (`**/x` matches `x`), one or more at the end
//     (`a/**` matches what is under `a`, not a file named `a`);
//   - a trailing `/` means a directory and everything under it, so `a/` is
//     the same as `a/**`;
//   - a leading `!` makes the pattern an exclusion, which MatchList applies;
//   - every other character matches itself, case-sensitively.
//
// A pattern is matched against the whole repository-relative path, as git
// prints it: `AGENTS.md` matches only the file at the root, and `**/AGENTS.md`
// matches one at any depth.
//
// Syntax other tools give a meaning to (`?`, `[...]`, `\`) is refused rather
// than read literally. Someone who writes `src/[a-z]*` expects a range; a
// pattern that silently matches nothing is a rule that silently never fires.
package glob

import (
	"errors"
	"fmt"
	"strings"
)

// Pattern is a compiled pattern.
type Pattern struct {
	raw      string
	negated  bool
	segments []segment
}

// segment is one '/'-separated part of a pattern.
type segment struct {
	// any marks `**`.
	any bool
	// text is the segment as written, for a segment that is not `**`.
	text string
}

// ErrInvalid is wrapped by every error Compile returns.
var ErrInvalid = errors.New("invalid pattern")

// Compile reads a pattern.
func Compile(pattern string) (Pattern, error) {
	fail := func(why string) (Pattern, error) {
		return Pattern{}, fmt.Errorf("%w %q: %s", ErrInvalid, pattern, why)
	}
	p := Pattern{raw: pattern}
	body := pattern
	if strings.HasPrefix(body, "!") {
		p.negated = true
		body = body[1:]
	}
	switch {
	case body == "":
		return fail("it is empty")
	case strings.HasPrefix(body, "!"):
		return fail("only one leading ! is allowed")
	case strings.HasPrefix(body, "/"):
		return fail("patterns are relative to the repository root; drop the leading /")
	}
	if i := strings.IndexAny(body, "?[]\\"); i >= 0 {
		return fail(fmt.Sprintf("%q is not supported", body[i]))
	}

	dir := strings.HasSuffix(body, "/")
	body = strings.TrimSuffix(body, "/")
	for _, part := range strings.Split(body, "/") {
		switch {
		case part == "":
			return fail("it has an empty path segment")
		case part == "." || part == "..":
			return fail("a path never holds a . or .. segment, so it would never match")
		case part == "**":
			p.segments = append(p.segments, segment{any: true})
		case strings.Contains(part, "**"):
			return fail("** must be a whole path segment")
		default:
			p.segments = append(p.segments, segment{text: part})
		}
	}
	if dir {
		p.segments = append(p.segments, segment{any: true})
	}
	return p, nil
}

// MustCompile is Compile for patterns written in the source, which are known
// to be valid. It panics on an invalid one.
func MustCompile(pattern string) Pattern {
	p, err := Compile(pattern)
	if err != nil {
		panic(err)
	}
	return p
}

// String returns the pattern as written.
func (p Pattern) String() string { return p.raw }

// Negated reports whether the pattern began with `!`.
func (p Pattern) Negated() bool { return p.negated }

// Match reports whether the path matches the pattern, leaving aside a leading
// `!`: an exclusion matches the paths it excludes. MatchList is what gives `!`
// its meaning.
func (p Pattern) Match(path string) bool {
	if path == "" || len(p.segments) == 0 {
		return false
	}
	parts := strings.Split(path, "/")
	n, m := len(p.segments), len(parts)
	// matches[i][j]: segments[i:] match parts[j:]. Filled from the end, so
	// that `**` is linear in the path rather than a backtracking search.
	matches := make([][]bool, n+1)
	for i := range matches {
		matches[i] = make([]bool, m+1)
	}
	matches[n][m] = true
	for i := n - 1; i >= 0; i-- {
		for j := m; j >= 0; j-- {
			seg := p.segments[i]
			switch {
			case seg.any && i == n-1:
				// A trailing ** is what is under a directory: at least one
				// more segment.
				matches[i][j] = j < m
			case seg.any:
				matches[i][j] = matches[i+1][j] || (j < m && matches[i][j+1])
			default:
				matches[i][j] = j < m && matchSegment(seg.text, parts[j]) && matches[i+1][j+1]
			}
		}
	}
	return matches[0][0]
}

// matchSegment matches one segment, in which only `*` is special.
func matchSegment(pattern, name string) bool {
	// The usual two-pointer wildcard match: on a mismatch, go back to the last
	// star and let it swallow one more character.
	p, n := 0, 0
	star, mark := -1, 0
	for n < len(name) {
		switch {
		case p < len(pattern) && pattern[p] == '*':
			star, mark = p, n
			p++
		case p < len(pattern) && pattern[p] == name[n]:
			p++
			n++
		case star >= 0:
			p = star + 1
			mark++
			n = mark
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}

// MatchList reports whether a list of patterns selects the path. The last
// pattern that matches decides: selected, or excluded if it began with `!`. A
// path no pattern matches is not selected.
func MatchList(patterns []Pattern, path string) bool {
	selected := false
	for _, p := range patterns {
		if p.Match(path) {
			selected = !p.negated
		}
	}
	return selected
}
