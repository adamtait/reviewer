// SPDX-License-Identifier: MIT

package navigation

import (
	"encoding/json"
	"strings"

	"github.com/adamtait/reviewer/internal/scip"
	"github.com/adamtait/reviewer/internal/workspace/source"
)

// The four places a result can come from. A label names the source of a
// result. It never says the result is correct: the index cannot be verified.
const (
	// LabelIndex: from the CI-built index.
	LabelIndex = "index"
	// LabelTextSearch: lines that contain the word, found by searching the
	// commit's text.
	LabelTextSearch = "text_search"
	// LabelExternal: the index places the definition in a package that is not
	// part of the repository. There is nothing to jump to.
	LabelExternal = "external"
	// LabelNone: not found.
	LabelNone = "none"
)

// MaxReferences is the most references one answer lists from the index.
const MaxReferences = 500

// Definition is the answer to "where is this defined".
type Definition struct {
	Label string
	// Targets is set when Label is LabelIndex.
	Targets []Location
	// Package and Version are set when Label is LabelExternal.
	Package string
	Version string
	// Matches is set when Label is LabelTextSearch.
	Matches   []source.Match
	Truncated bool
}

// References is the answer to "where is this used".
//
// An index can cover part of a repository, so its list is never the whole
// truth and the answer never says "no references". The text matches for the
// same word are always given beside it.
type References struct {
	// Label is LabelIndex when the index listed anything, LabelTextSearch when
	// only the search found something, and LabelNone when neither did.
	Label      string
	References []Location
	// Truncated says the index held more references than are listed.
	Truncated bool
	Matches   []source.Match
	// MatchesTruncated says the search stopped at its limit.
	MatchesTruncated bool
}

// packageName reads the "name" from a package.json. Anything that is not a
// JSON object with a string name gives "".
func packageName(text string) string {
	var manifest struct {
		Name string `json:"name"`
	}
	if json.Unmarshal([]byte(text), &manifest) != nil {
		return ""
	}
	return manifest.Name
}

// at returns the occurrences that cover a position and have the smallest
// range among those that do. Several symbols can share that range: a name that
// is both a type and a value has one occurrence for each.
func (ix *Index) at(path string, line, character int) []occurrence {
	if ix == nil {
		return nil
	}
	var tied []occurrence
	smallest := int32(-1)
	for _, o := range ix.docs[path] {
		if int(o.rng.StartLine) > line {
			break // sorted by position
		}
		if int(o.rng.StartLine) != line || int(o.rng.StartChar) > character || character >= int(o.rng.EndChar) {
			continue
		}
		width := o.rng.EndChar - o.rng.StartChar
		switch {
		case smallest < 0 || width < smallest:
			smallest, tied = width, []occurrence{o}
		case width == smallest:
			tied = append(tied, o)
		}
	}
	return tied
}

func locationOf(path string, o occurrence) Location {
	return Location{
		Path: path, Line: int(o.rng.StartLine), Character: int(o.rng.StartChar),
		EndLine: int(o.rng.EndLine), EndCharacter: int(o.rng.EndChar),
	}
}

// FindDefinition answers one go-to-definition.
//
// ix may be nil, when the commit has no index: the answer then comes from text
// search alone. commit is the commit's files, which the index is checked
// against at every step.
//
// The rules, in order:
//
//  1. Find the symbols: every occurrence that covers the position, keeping
//     those with the smallest range.
//  2. Collect their definitions from the index.
//  3. Keep a target only if the text there is the text asked about, or the
//     symbol's own name. Two more are kept: a whole file, when what was asked
//     about is a quoted import path; and a constructor, when what was asked
//     about is the name of the type it builds. If any target is kept, that is
//     the answer.
//  4. If no symbol had a definition, and one names a package that is not one
//     of the repository's own, say the index places it there.
//  5. Otherwise search the commit's text for the word.
//
// Rule 3 is what keeps a wrong or stale index from sending the user somewhere
// unrelated: a target has to at least be the same word.
func FindDefinition(ix *Index, commit *source.Commit, path string, line, character int) Definition {
	tied := ix.at(path, line, character)
	if len(tied) == 0 {
		return searchFor(commit, wordAt(commit, path, line, character))
	}
	asked, _ := textAt(commit, locationOf(path, tied[0]))

	// The position is itself a definition: the answer is where the user is.
	var here []Location
	for _, o := range tied {
		if o.definition {
			here = appendUnique(here, locationOf(path, o))
		}
	}
	if len(here) > 0 {
		return Definition{Label: LabelIndex, Targets: here}
	}

	var (
		kept       []Location
		hadTargets bool
		pkg, ver   string
	)
	for _, o := range tied {
		symbol := scip.ParseSymbol(o.symbol)
		targets := ix.defs[keyOf(path, o.symbol)]
		hadTargets = hadTargets || len(targets) > 0

		for _, target := range targets {
			found, readable := textAt(commit, target)
			// An import path such as './types' points at a whole file, which
			// the index records as an empty range. Only a quoted path may use
			// one: otherwise any name could be sent to the top of any file.
			wholeFile := target.Line == target.EndLine && target.Character == target.EndCharacter && quoted(asked)
			// `new Cart()` points at Cart's constructor, where the text is
			// "constructor" and not "Cart".
			built, isConstructor := symbol.Constructs()
			constructor := isConstructor && readable && found == "constructor" && asked == built
			sameWord := readable && found != "" && (found == asked || (!symbol.IsLocal && found == symbol.LastName()))
			if wholeFile || constructor || sameWord {
				kept = appendUnique(kept, target)
			}
		}
		if len(targets) == 0 && !symbol.IsLocal && symbol.Package != "" && !ix.own[symbol.Package] && pkg == "" {
			pkg, ver = symbol.Package, symbol.Version
		}
	}

	switch {
	case len(kept) > 0:
		return Definition{Label: LabelIndex, Targets: kept}
	case hadTargets:
		// The index had an answer and the name check refused all of it.
		return searchFor(commit, word(asked))
	case pkg != "":
		return Definition{Label: LabelExternal, Package: pkg, Version: ver}
	default:
		return searchFor(commit, word(asked))
	}
}

// FindReferences answers one find-references.
func FindReferences(ix *Index, commit *source.Commit, path string, line, character int) References {
	tied := ix.at(path, line, character)
	var out References
	asked := wordAt(commit, path, line, character)
	if len(tied) > 0 {
		if text, ok := textAt(commit, locationOf(path, tied[0])); ok {
			asked = word(text)
		}
	}

	seen := map[Location]bool{}
	for _, o := range tied {
		for _, use := range ix.uses[keyOf(path, o.symbol)] {
			if seen[use] {
				continue
			}
			seen[use] = true
			if len(out.References) == MaxReferences {
				out.Truncated = true
				break
			}
			out.References = append(out.References, use)
		}
	}

	if asked != "" {
		out.Matches, out.MatchesTruncated = commit.Search(source.Query{Text: asked, CaseSensitive: true, WholeWord: true})
	}
	switch {
	case len(out.References) > 0:
		out.Label = LabelIndex
	case len(out.Matches) > 0:
		out.Label = LabelTextSearch
	default:
		out.Label = LabelNone
	}
	return out
}

// searchFor is the fall-back: lines of the commit that contain the word.
func searchFor(commit *source.Commit, w string) Definition {
	if w == "" {
		return Definition{Label: LabelNone}
	}
	matches, truncated := commit.Search(source.Query{Text: w, CaseSensitive: true, WholeWord: true})
	if len(matches) == 0 {
		return Definition{Label: LabelNone}
	}
	return Definition{Label: LabelTextSearch, Matches: matches, Truncated: truncated}
}

func appendUnique(list []Location, at Location) []Location {
	for _, have := range list {
		if have == at {
			return list
		}
	}
	return append(list, at)
}

func quoted(text string) bool {
	return text != "" && (text[0] == '\'' || text[0] == '"' || text[0] == '`')
}

// word reduces text to something worth searching for: an identifier. A quoted
// import path, an operator or an empty string gives "".
func word(text string) string {
	if text == "" {
		return ""
	}
	for i := 0; i < len(text); i++ {
		if !wordByte(text[i]) {
			return ""
		}
	}
	return text
}

// wordAt returns the identifier under a position in a file, for a position the
// index says nothing about.
func wordAt(commit *source.Commit, path string, line, character int) string {
	text, ok := commit.Text(path)
	if !ok {
		return ""
	}
	lineText, ok := lineOf(text, line)
	if !ok {
		return ""
	}
	// The position is in UTF-16 units; find the byte it falls on.
	before, ok := slice16(lineText, 0, character)
	if !ok {
		return ""
	}
	start, end := len(before), len(before)
	for start > 0 && wordByte(lineText[start-1]) {
		start--
	}
	for end < len(lineText) && wordByte(lineText[end]) {
		end++
	}
	return strings.TrimSpace(lineText[start:end])
}

// wordByte reports whether b can be part of an identifier. Bytes of a
// character outside ASCII count, so that an identifier with one in it is kept
// whole.
func wordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '$' || b >= 0x80
}
