// SPDX-License-Identifier: MIT

package source

import "strings"

// MaxMatches is the most matching lines one search returns.
const MaxMatches = 200

// maxMatchLine bounds one returned line. A minified file is one line of a
// megabyte, and a search result is not the place to send it.
const maxMatchLine = 400

// Match is one line that contains the query.
type Match struct {
	Path string
	// Line counts from 0.
	Line int
	Text string
}

// Query is a text search over one commit.
type Query struct {
	// Text is matched literally. It is not a pattern.
	Text string
	// PathPrefix, if set, limits the search to paths that begin with it.
	PathPrefix string
	// CaseSensitive matches case exactly. The default folds case.
	CaseSensitive bool
}

// Search returns the lines of the commit's text files that contain the query,
// in path order, and whether it stopped at MaxMatches with more to find.
//
// It is a plain scan. Over 90 MB of text a scan took 0.13 seconds in the
// design's trials, which is why there is no index to build, hold or keep right.
func (c *Commit) Search(q Query) (matches []Match, truncated bool) {
	if q.Text == "" {
		return nil, false
	}
	needle := q.Text
	if !q.CaseSensitive {
		needle = strings.ToLower(needle)
	}
	for _, entry := range c.entries {
		if entry.Kind != KindText || !strings.HasPrefix(entry.Path, q.PathPrefix) {
			continue
		}
		text := c.text[entry.Path]
		haystack := text
		if !q.CaseSensitive {
			haystack = strings.ToLower(text)
		}
		if !strings.Contains(haystack, needle) {
			continue
		}
		// Lower-casing can change a string's length in bytes, so lines are
		// matched one at a time against their own folded form, not by offset.
		for number, line := range strings.Split(text, "\n") {
			candidate := line
			if !q.CaseSensitive {
				candidate = strings.ToLower(line)
			}
			if !strings.Contains(candidate, needle) {
				continue
			}
			if len(matches) == MaxMatches {
				return matches, true
			}
			matches = append(matches, Match{Path: entry.Path, Line: number, Text: clip(strings.TrimRight(line, "\r"))})
		}
	}
	return matches, false
}

// clip shortens a long line without cutting a character in half.
func clip(line string) string {
	if len(line) <= maxMatchLine {
		return line
	}
	cut := maxMatchLine
	for cut > 0 && !isRuneStart(line[cut]) {
		cut--
	}
	return line[:cut] + "…"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
