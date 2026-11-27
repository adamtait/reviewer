// SPDX-License-Identifier: MIT

package risk

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/adamtait/reviewer/internal/scip"
)

// The index is encoded by hand in the protobuf wire format and read with
// scip.Decode, so the test goes through the same decoder `reviewer risk` uses
// and depends on no encoder. Field numbers are SCIP's: Index.documents 2;
// Document.relative_path 1, occurrences 2; Occurrence.range 1 (packed),
// symbol 2, symbol_roles 3 (1 = definition).

func uvarint(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

func field(number int, b []byte) []byte {
	out := uvarint(uint64(number)<<3 | 2)
	return append(append(out, uvarint(uint64(len(b)))...), b...)
}

func join(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// occurrence is a symbol at a 0-based line, characters 0 to 5, or across lines
// start to end when end is given.
func occurrence(symbol string, definition bool, line int, end ...int) []byte {
	var rng []byte
	if len(end) == 0 {
		rng = join(uvarint(uint64(line)), uvarint(0), uvarint(5))
	} else {
		rng = join(uvarint(uint64(line)), uvarint(0), uvarint(uint64(end[0])), uvarint(1))
	}
	roles := uint64(0)
	if definition {
		roles = 1
	}
	// 3<<3 is the key of field 3, symbol_roles, with wire type 0 (varint).
	return field(2, join(field(1, rng), field(2, []byte(symbol)), uvarint(3<<3), uvarint(roles)))
}

func document(path string, occurrences ...[]byte) []byte {
	return field(2, join(field(1, []byte(path)), join(occurrences...)))
}

func index(t *testing.T, documents ...[]byte) *scip.Index {
	t.Helper()
	ix, err := scip.Decode(join(documents...), scip.Limits{Occurrences: 10000})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return ix
}

const (
	symParse  = "scip-typescript npm app 1.0.0 src/`lib.ts`/parse()."
	symFormat = "scip-typescript npm app 1.0.0 src/`lib.ts`/format()."
	symConfig = "scip-typescript npm app 1.0.0 src/`lib.ts`/Config#"
	symOther  = "scip-typescript npm app 1.0.0 src/`util.ts`/other()."
)

// users is n documents, each referring to symbol once.
func users(symbol string, n int) [][]byte {
	var out [][]byte
	for i := 0; i < n; i++ {
		out = append(out, document(fmt.Sprintf("src/user%d.ts", i), occurrence(symbol, false, 3)))
	}
	return out
}

func TestReach(t *testing.T) {
	lib := document("src/lib.ts",
		occurrence(symParse, true, 4), // base line 5
		occurrence(symFormat, true, 9),
		occurrence(symConfig, true, 20, 24), // base lines 21 to 25
		occurrence("local 3", true, 4),
		occurrence(symParse, false, 12), // used in its own file: not counted
		occurrence(symOther, false, 4),  // used on a changed line, defined elsewhere
	)
	docs := [][]byte{lib,
		document("src/a.ts", occurrence(symParse, false, 1), occurrence(symParse, false, 7)),
		document("src/b.ts", occurrence(symParse, false, 2), occurrence(symFormat, false, 2)),
		// A second definition elsewhere (a declaration file) is not a use.
		document("src/lib.d.ts", occurrence(symParse, true, 0)),
		// A file the index lists twice is one file.
		document("src/c.ts", occurrence(symConfig, false, 1)),
		document("src/c.ts", occurrence(symConfig, false, 5)),
		document("src/util.ts", occurrence(symOther, true, 0)),
	}
	ix := index(t, docs...)

	for _, tc := range []struct {
		name      string
		path      string
		baseLines []int
		want      []SymbolReach
	}{
		{
			name:      "a definition on a changed line, used in two other files",
			path:      "src/lib.ts",
			baseLines: []int{5},
			want:      []SymbolReach{{symParse, 2}},
		},
		{
			// SCIP's line 4 is the diff's line 5; line 4 of the diff is SCIP's 3.
			name:      "base lines are 1-based",
			path:      "src/lib.ts",
			baseLines: []int{4, 6},
			want:      nil,
		},
		{
			name:      "several definitions, sorted by symbol",
			path:      "src/lib.ts",
			baseLines: []int{5, 10},
			want:      []SymbolReach{{symFormat, 1}, {symParse, 2}},
		},
		{
			name:      "a line inside a definition's range",
			path:      "src/lib.ts",
			baseLines: []int{23},
			want:      []SymbolReach{{symConfig, 1}},
		},
		{
			name:      "the last line of a definition's range",
			path:      "src/lib.ts",
			baseLines: []int{25},
			want:      []SymbolReach{{symConfig, 1}},
		},
		{
			name:      "the line after a definition's range",
			path:      "src/lib.ts",
			baseLines: []int{26},
			want:      nil,
		},
		{
			name:      "no changed line holds a definition",
			path:      "src/lib.ts",
			baseLines: []int{13},
			want:      nil,
		},
		{
			name:      "a file the index does not describe",
			path:      "src/missing.ts",
			baseLines: []int{5},
			want:      nil,
		},
		{
			name:      "no changed lines",
			path:      "src/lib.ts",
			baseLines: nil,
			want:      nil,
		},
	} {
		if got := Reach(ix, tc.path, tc.baseLines); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := Reach(nil, "src/lib.ts", []int{5}); got != nil {
		t.Errorf("a nil index: %v", got)
	}
}

// The reach boundary end to end: an index, Reach, and Compute.
func TestReachBoundaryFromAnIndex(t *testing.T) {
	for _, tc := range []struct {
		users int
		want  bool
	}{{9, false}, {10, true}} {
		docs := append([][]byte{document("src/lib.ts", occurrence(symParse, true, 4))}, users(symParse, tc.users)...)
		reach := Reach(index(t, docs...), "src/lib.ts", []int{5})
		if len(reach) != 1 || reach[0].Files != tc.users {
			t.Fatalf("%d users: Reach gave %v", tc.users, reach)
		}
		rec := Compute(Input{
			Files:  []File{{Path: "src/lib.ts"}},
			Owners: owners(t, ""),
			Reach:  map[string][]SymbolReach{"src/lib.ts": reach},
		})
		if rec.Has(LabelReach) != tc.want {
			t.Errorf("%d other files: risk/reach %v, want %v", tc.users, rec.Has(LabelReach), tc.want)
		}
	}
}
