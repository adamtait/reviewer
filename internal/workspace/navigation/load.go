// SPDX-License-Identifier: MIT

// Package navigation answers "where is this defined" and "where is this used"
// for a commit, from the index a CI job built for it.
//
// The index cannot be verified. A pull request's author controls the job that
// writes it, so it may be wrong, partial, or made to mislead. It is therefore
// bounded: it is accepted only from a run GitHub reports for the commit on
// screen, it is checked against the commit's real files, and every result says
// where it came from and never that it is correct. What a false index can do
// is mislead navigation for that commit. It cannot change the diff, the source
// shown, or what text search finds.
package navigation

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/adamtait/reviewer/internal/workspace/navigation/scip"
	"github.com/adamtait/reviewer/internal/workspace/source"
)

// Location is a span in a file of the commit. Lines and characters count from
// 0, the end is exclusive, and characters are UTF-16 code units, as the index
// and the browser's editor both count them.
type Location struct {
	Path                  string
	Line, Character       int
	EndLine, EndCharacter int
}

// occurrence is one place a symbol appears, in a form small enough to hold by
// the hundred thousand.
type occurrence struct {
	rng        scip.Range
	symbol     string
	definition bool
}

// symbolKey identifies a symbol across the index. A local symbol means
// something only inside its own document, so its key carries the document.
type symbolKey struct {
	path   string
	symbol string
}

// Index is a decoded index, checked against the commit's files and arranged
// for lookup. It is never changed once built.
type Index struct {
	ToolName    string
	ToolVersion string
	// ProjectRoot is where the index was built. Tests read it.
	ProjectRoot string
	// Dropped counts what the checks against the commit's files removed.
	Dropped Dropped

	docs map[string][]occurrence // by path, sorted by position
	uses map[symbolKey][]Location
	defs map[symbolKey][]Location
	// own holds the names of the repository's own packages, from every
	// package.json in the commit.
	own  map[string]bool
	size int64
}

// Dropped counts what was removed from an index while it was checked against
// the commit.
type Dropped struct {
	// NotInCommit is documents whose path is not a text file of the commit.
	NotInCommit int
	// NotUTF16 is documents whose positions are not in UTF-16 units.
	NotUTF16 int
	// Occurrences is occurrences whose position is outside their file, or
	// that span more than one line.
	Occurrences int
}

// Documents is how many of the index's documents are in use.
func (ix *Index) Documents() int { return len(ix.docs) }

// Size is what the index costs to hold, in bytes. Like a commit's, it is a
// low estimate.
func (ix *Index) Size() int64 { return ix.size }

// Build checks a decoded index against the commit it claims to describe and
// arranges what is left for lookup.
//
// Three things are dropped. A document whose path is not a text file in the
// commit: the index is describing a file that is not there. A document whose
// positions are not UTF-16 units: they cannot be mapped to what is on screen.
// And an occurrence whose position is not inside its file's text: whatever it
// points at, it is not what the user would be shown.
func Build(decoded *scip.Index, commit *source.Commit) *Index {
	ix := &Index{
		ToolName:    decoded.ToolName,
		ToolVersion: decoded.ToolVersion,
		ProjectRoot: decoded.ProjectRoot,
		docs:        map[string][]occurrence{},
		uses:        map[symbolKey][]Location{},
		defs:        map[symbolKey][]Location{},
		own:         ownPackages(commit),
	}
	// One copy of each symbol's text, however many times it occurs.
	interned := map[string]string{}

	for _, doc := range decoded.Documents {
		if !doc.UTF16 {
			ix.Dropped.NotUTF16++
			continue
		}
		text, ok := commit.Text(doc.Path)
		if !ok {
			ix.Dropped.NotInCommit++
			continue
		}
		if _, seen := ix.docs[doc.Path]; seen {
			// A second document for one path. The first stands; an index that
			// says two things about one file is not improved by merging them.
			continue
		}
		lines := strings.Split(text, "\n")
		kept := make([]occurrence, 0, len(doc.Occurrences))
		for _, o := range doc.Occurrences {
			if !inside(lines, o.Range) {
				ix.Dropped.Occurrences++
				continue
			}
			symbol, ok := interned[o.Symbol]
			if !ok {
				symbol = strings.Clone(o.Symbol)
				interned[symbol] = symbol
				ix.size += int64(len(symbol))
			}
			kept = append(kept, occurrence{rng: o.Range, symbol: symbol, definition: o.Definition})
		}
		sort.SliceStable(kept, func(i, j int) bool {
			a, b := kept[i].rng, kept[j].rng
			if a.StartLine != b.StartLine {
				return a.StartLine < b.StartLine
			}
			return a.StartChar < b.StartChar
		})
		ix.docs[doc.Path] = kept
		ix.size += int64(len(doc.Path)) + int64(len(kept))*occurrenceCost

		for _, o := range kept {
			key := keyOf(doc.Path, o.symbol)
			at := Location{
				Path: doc.Path, Line: int(o.rng.StartLine), Character: int(o.rng.StartChar),
				EndLine: int(o.rng.EndLine), EndCharacter: int(o.rng.EndChar),
			}
			ix.uses[key] = append(ix.uses[key], at)
			if o.definition {
				ix.defs[key] = append(ix.defs[key], at)
			}
		}
	}
	return ix
}

// occurrenceCost is what one occurrence is charged against the cache: itself,
// and its entry in the lists by symbol.
const occurrenceCost = 96

func keyOf(path, symbol string) symbolKey {
	if strings.HasPrefix(symbol, "local ") {
		return symbolKey{path: path, symbol: symbol}
	}
	return symbolKey{symbol: symbol}
}

// inside reports whether a range lies within the file's text. Only a range on
// one line is accepted: every symbol name is, and a range that claims to span
// lines is not something a lookup can use.
func inside(lines []string, r scip.Range) bool {
	if r.StartLine != r.EndLine || r.StartLine < 0 || int(r.StartLine) >= len(lines) {
		return false
	}
	_, ok := slice16(lines[r.StartLine], int(r.StartChar), int(r.EndChar))
	return ok
}

// slice16 returns the part of a line between two offsets counted in UTF-16
// code units. ok is false if either offset is past the line's end or falls in
// the middle of a character.
func slice16(line string, start, end int) (string, bool) {
	if start < 0 || end < start {
		return "", false
	}
	from, to := -1, -1
	units := 0
	for i := 0; ; {
		if units == start && from < 0 {
			from = i
		}
		if units == end {
			to = i
			break
		}
		if i >= len(line) || units > end {
			break
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		if r >= 0x10000 {
			units += 2
		} else {
			units++
		}
		i += size
	}
	if from < 0 || to < 0 {
		return "", false
	}
	return line[from:to], true
}

// lineOf returns one line of a file's text, without its line ending.
func lineOf(text string, n int) (string, bool) {
	if n < 0 {
		return "", false
	}
	for ; n > 0; n-- {
		i := strings.IndexByte(text, '\n')
		if i < 0 {
			return "", false
		}
		text = text[i+1:]
	}
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	return text, true
}

// textAt returns the text a location covers.
func textAt(commit *source.Commit, at Location) (string, bool) {
	if at.Line != at.EndLine {
		return "", false
	}
	text, ok := commit.Text(at.Path)
	if !ok {
		return "", false
	}
	line, ok := lineOf(text, at.Line)
	if !ok {
		return "", false
	}
	return slice16(line, at.Character, at.EndCharacter)
}

// ownPackages returns the package names the commit itself defines: the "name"
// of every package.json outside node_modules.
//
// An index can cover part of a repository. A symbol from the part it left out
// names the repository's own package and has no definition in the index, which
// is exactly how a symbol from an installed dependency looks. Without this
// list such a symbol would be reported as "defined outside the repository".
func ownPackages(commit *source.Commit) map[string]bool {
	own := map[string]bool{}
	for _, entry := range commit.Entries() {
		if entry.Kind != source.KindText || (entry.Path != "package.json" && !strings.HasSuffix(entry.Path, "/package.json")) {
			continue
		}
		if strings.HasPrefix(entry.Path, "node_modules/") || strings.Contains(entry.Path, "/node_modules/") {
			continue
		}
		text, _ := commit.Text(entry.Path)
		if name := packageName(text); name != "" {
			own[name] = true
		}
	}
	return own
}
