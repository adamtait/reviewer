// SPDX-License-Identifier: MIT

package scip

import (
	"errors"
	"testing"
)

// The tests encode indexes by hand, so that what is tested is the decoder's
// reading of the wire format and not its agreement with an encoder written by
// the same hand to the same understanding.

func uvarint(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

func key(number, wire int) []byte { return uvarint(uint64(number)<<3 | uint64(wire)) }

func num(number int, v uint64) []byte { return append(key(number, wireVarint), uvarint(v)...) }

func bytesField(number int, b []byte) []byte {
	return append(append(key(number, wireBytes), uvarint(uint64(len(b)))...), b...)
}

func str(number int, s string) []byte { return bytesField(number, []byte(s)) }

func join(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func packed(values ...uint64) []byte {
	var out []byte
	for _, v := range values {
		out = append(out, uvarint(v)...)
	}
	return out
}

// occ encodes an occurrence with the list form of a range.
func occ(symbol string, roles uint64, rng ...uint64) []byte {
	return bytesField(2, join(bytesField(1, packed(rng...)), str(2, symbol), num(3, roles)))
}

func doc(path string, encoding uint64, occurrences ...[]byte) []byte {
	return bytesField(2, join(str(1, path), num(6, encoding), join(occurrences...)))
}

func metadata(tool, version, root string) []byte {
	return bytesField(1, join(bytesField(2, join(str(1, tool), str(2, version))), str(3, root)))
}

var roomy = Limits{Occurrences: 1000}

func decode(t *testing.T, raw []byte) *Index {
	t.Helper()
	index, err := Decode(raw, roomy)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return index
}

func TestDecodeReadsMetadataDocumentsAndOccurrences(t *testing.T) {
	index := decode(t, join(
		metadata("scip-typescript", "0.6.2", "file:///work/project"),
		doc("src/a.ts", encodingUTF16,
			occ("scip-typescript npm app 1.0.0 src/`a.ts`/total().", roleDefinition, 0, 16, 21),
			occ("local 3", 0, 4, 2, 4, 7),
		),
		doc("src/b.ts", encodingUTF16),
	))
	if index.ToolName != "scip-typescript" || index.ToolVersion != "0.6.2" || index.ProjectRoot != "file:///work/project" {
		t.Errorf("metadata = %q %q %q", index.ToolName, index.ToolVersion, index.ProjectRoot)
	}
	if len(index.Documents) != 2 || index.Documents[0].Path != "src/a.ts" || !index.Documents[0].UTF16 {
		t.Fatalf("documents = %+v", index.Documents)
	}
	got := index.Documents[0].Occurrences
	want := []Occurrence{
		{Range: Range{0, 16, 0, 21}, Symbol: "scip-typescript npm app 1.0.0 src/`a.ts`/total().", Definition: true},
		{Range: Range{4, 2, 4, 7}, Symbol: "local 3"},
	}
	if len(got) != len(want) {
		t.Fatalf("occurrences = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("occurrence %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestDecodeReadsAllThreeRangeForms(t *testing.T) {
	single := bytesField(8, join(num(1, 7), num(2, 3), num(3, 9)))           // line, start, end
	multi := bytesField(9, join(num(1, 7), num(2, 3), num(3, 8), num(4, 1))) // start line, start, end line, end
	unpacked := join(num(1, 2), num(1, 4), num(1, 6))                        // the list, one integer per field

	index := decode(t, doc("a.ts", encodingUTF16,
		bytesField(2, join(str(2, "s single"), single)),
		bytesField(2, join(str(2, "s multi"), multi)),
		bytesField(2, join(str(2, "s three"), bytesField(1, packed(1, 2, 5)))),
		bytesField(2, join(str(2, "s four"), bytesField(1, packed(1, 2, 3, 0)))),
		bytesField(2, join(str(2, "s unpacked"), unpacked)),
		// When a typed form and the list are both present, the typed form wins.
		bytesField(2, join(str(2, "s both"), bytesField(1, packed(90, 90, 99)), single)),
		// A line of zero is left out of the typed form, as protobuf leaves out zeros.
		bytesField(2, join(str(2, "s zero"), bytesField(8, join(num(2, 3), num(3, 9))))),
	))
	want := map[string]Range{
		"s single":   {7, 3, 7, 9},
		"s multi":    {7, 3, 8, 1},
		"s three":    {1, 2, 1, 5},
		"s four":     {1, 2, 3, 0},
		"s unpacked": {2, 4, 2, 6},
		"s both":     {7, 3, 7, 9},
		"s zero":     {0, 3, 0, 9},
	}
	got := index.Documents[0].Occurrences
	if len(got) != len(want) {
		t.Fatalf("%d occurrences, want %d: %+v", len(got), len(want), got)
	}
	for _, o := range got {
		if o.Range != want[o.Symbol] {
			t.Errorf("%s: range %+v, want %+v", o.Symbol, o.Range, want[o.Symbol])
		}
	}
}

func TestDecodeDropsOccurrencesThatSayNothingUsable(t *testing.T) {
	index := decode(t, doc("a.ts", encodingUTF16,
		bytesField(2, bytesField(1, packed(1, 2, 3))),                     // no symbol
		bytesField(2, str(2, "s no range")),                               // no range
		bytesField(2, join(str(2, "s two"), bytesField(1, packed(1, 2)))), // two integers
		bytesField(2, join(str(2, "s five"), bytesField(1, packed(1, 2, 3, 4, 5)))),
		bytesField(2, join(str(2, "s backwards"), bytesField(1, packed(5, 9, 5, 2)))),
		bytesField(2, join(str(2, "s huge"), bytesField(1, packed(1<<40, 0, 1)))),
		occ("s kept", 0, 1, 2, 3),
	))
	got := index.Documents[0].Occurrences
	if len(got) != 1 || got[0].Symbol != "s kept" {
		t.Errorf("occurrences = %+v, want only the usable one", got)
	}
}

func TestDecodeMarksADocumentInAnotherEncoding(t *testing.T) {
	index := decode(t, join(
		doc("utf8.ts", 1, occ("s", 0, 0, 0, 1)),
		doc("utf32.ts", 3, occ("s", 0, 0, 0, 1)),
		bytesField(2, join(str(1, "unstated.ts"), occ("s", 0, 0, 0, 1))),
		doc("utf16.ts", encodingUTF16, occ("s", 0, 0, 0, 1)),
	))
	for _, d := range index.Documents {
		if d.UTF16 != (d.Path == "utf16.ts") {
			t.Errorf("%s: UTF16 = %v", d.Path, d.UTF16)
		}
	}
}

func TestDecodeSkipsFieldsItDoesNotKnow(t *testing.T) {
	fixed8 := append(key(40, wireFixed8), 1, 2, 3, 4, 5, 6, 7, 8)
	fixed4 := append(key(41, wireFixed4), 1, 2, 3, 4)
	index := decode(t, join(
		str(99, "a field from a later schema"),
		fixed8,
		bytesField(1, join(num(1, 1), bytesField(2, join(str(1, "tool"), str(7, "new"))), fixed4)),
		num(50, 12345),
		bytesField(2, join(
			str(1, "a.ts"), num(6, encodingUTF16), str(4, "typescript"), bytesField(3, []byte("symbol information")),
			bytesField(2, join(bytesField(1, packed(0, 0, 1)), str(2, "s"), num(5, 2), fixed8, str(4, "docs"))),
		)),
	))
	if index.ToolName != "tool" || len(index.Documents) != 1 || len(index.Documents[0].Occurrences) != 1 {
		t.Errorf("index = %+v", index)
	}
}

func TestDecodeRefusesTruncatedAndMalformedInput(t *testing.T) {
	whole := join(metadata("tool", "1", "root"), doc("a.ts", encodingUTF16, occ("symbol", 1, 0, 0, 6)))
	// Every prefix is either a shorter valid index or an error. None panics.
	for cut := 0; cut < len(whole); cut++ {
		if _, err := Decode(whole[:cut], roomy); err != nil && !errors.Is(err, ErrMalformed) {
			t.Errorf("cut at %d: %v, want ErrMalformed", cut, err)
		}
	}
	if _, err := Decode(whole[:len(whole)-3], roomy); !errors.Is(err, ErrMalformed) {
		t.Errorf("an index cut short inside a field: %v", err)
	}

	for name, raw := range map[string][]byte{
		"a length far past the end":  join(key(2, wireBytes), uvarint(1<<40)),
		"a length that overflows":    join(key(2, wireBytes), []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}),
		"a number that never ends":   join(key(3, wireVarint), []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80}),
		"a group, which is not used": key(2, 3),
		"field number zero":          {0x00, 0x00},
		"a path that is not text":    bytesField(2, bytesField(1, []byte{0xff, 0xfe})),
		"a symbol that is not text":  doc("a.ts", encodingUTF16, bytesField(2, join(bytesField(1, packed(0, 0, 1)), bytesField(2, []byte{0xc3, 0x28})))),
		"a web page":                 []byte("<!doctype html><html>not found</html>"),
	} {
		if _, err := Decode(raw, roomy); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: err = %v, want ErrMalformed", name, err)
		}
	}
}

func TestDecodeStopsAtTheOccurrenceLimit(t *testing.T) {
	raw := join(
		doc("a.ts", encodingUTF16, occ("s", 0, 0, 0, 1), occ("s", 0, 1, 0, 1)),
		doc("b.ts", encodingUTF16, occ("s", 0, 0, 0, 1), occ("s", 0, 1, 0, 1)),
	)
	if _, err := Decode(raw, Limits{Occurrences: 4}); err != nil {
		t.Errorf("exactly at the limit: %v", err)
	}
	if _, err := Decode(raw, Limits{Occurrences: 3}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("one over the limit: %v, want ErrTooLarge", err)
	}
}

func TestAnEmptyIndexIsValid(t *testing.T) {
	index := decode(t, nil)
	if len(index.Documents) != 0 {
		t.Errorf("index = %+v", index)
	}
}

func FuzzDecode(f *testing.F) {
	f.Add(join(metadata("tool", "1", "root"), doc("a.ts", encodingUTF16, occ("symbol", 1, 0, 0, 6))))
	f.Add([]byte{0x12, 0x02, 0x0a, 0x00})
	f.Fuzz(func(t *testing.T, raw []byte) {
		index, err := Decode(raw, Limits{Occurrences: 100})
		if err != nil {
			return
		}
		for _, d := range index.Documents {
			for _, o := range d.Occurrences {
				r := o.Range
				if o.Symbol == "" || r.StartLine < 0 || r.StartChar < 0 || r.EndLine < r.StartLine {
					t.Fatalf("an unusable occurrence was kept: %+v", o)
				}
			}
		}
	})
}
