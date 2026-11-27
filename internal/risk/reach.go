// SPDX-License-Identifier: MIT

package risk

import (
	"sort"
	"strings"

	"github.com/adamtait/reviewer/internal/scip"
)

// SymbolReach is a symbol a pull request changes the definition of, and how
// many other files on the base branch use it.
type SymbolReach struct {
	// Symbol is the SCIP symbol, as the index writes it.
	Symbol string
	// Files is the number of documents, other than the one that defines the
	// symbol, holding at least one occurrence of it that is not a definition.
	Files int
}

// Reach returns the symbols defined on a file's changed base lines, each with
// the number of other documents that refer to it, sorted by symbol.
//
// baseLines are 1-based, as diff.BaseLines returns them; SCIP counts lines
// from 0. A definition counts if any line its range covers was removed or
// modified. Local symbols (`local ` prefix) mean something only inside their
// own document, so they cannot be reached from another file and are skipped.
//
// A file the index does not describe gives nil. So does a nil index.
func Reach(ix *scip.Index, path string, baseLines []int) []SymbolReach {
	if ix == nil || len(baseLines) == 0 {
		return nil
	}
	// 0-based and sorted, so that a definition's range is checked with one
	// search however many lines it claims: the index is CI output, and a
	// range is not trusted to be small.
	changed := make([]int32, 0, len(baseLines))
	for _, l := range baseLines {
		changed = append(changed, int32(l-1))
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i] < changed[j] })
	touches := func(r scip.Range) bool {
		end := max(r.EndLine, r.StartLine)
		i := sort.Search(len(changed), func(i int) bool { return changed[i] >= r.StartLine })
		return i < len(changed) && changed[i] <= end
	}

	defined := map[string]bool{}
	for _, doc := range ix.Documents {
		if doc.Path != path {
			continue
		}
		for _, occ := range doc.Occurrences {
			if !occ.Definition || occ.Symbol == "" || strings.HasPrefix(occ.Symbol, "local ") {
				continue
			}
			if touches(occ.Range) {
				defined[occ.Symbol] = true
			}
		}
	}
	if len(defined) == 0 {
		return nil
	}

	// Documents are counted by path, so an index that lists a file twice does
	// not count it twice.
	users := map[string]map[string]bool{}
	for _, doc := range ix.Documents {
		if doc.Path == path {
			continue
		}
		for _, occ := range doc.Occurrences {
			if occ.Definition || !defined[occ.Symbol] {
				continue
			}
			if users[occ.Symbol] == nil {
				users[occ.Symbol] = map[string]bool{}
			}
			users[occ.Symbol][doc.Path] = true
		}
	}

	out := make([]SymbolReach, 0, len(defined))
	for symbol := range defined {
		out = append(out, SymbolReach{Symbol: symbol, Files: len(users[symbol])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	return out
}
