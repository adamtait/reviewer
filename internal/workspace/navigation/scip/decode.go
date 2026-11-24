// SPDX-License-Identifier: MIT

// Package scip reads a SCIP index: the file a CI job writes with
// scip-typescript, saying where each symbol in a repository is defined and
// used.
//
// The index is protobuf. It is decoded here by hand, with the standard
// library, for three reasons: only a dozen fields are read, the service takes
// no dependency it does not need, and the input is untrusted. A pull request's
// author controls the job that writes it, so every length is checked before it
// is used and nothing is allocated on the file's say-so.
//
// The field numbers are from the SCIP schema at release v0.10.0. Unknown
// fields are skipped.
package scip

import (
	"errors"
	"fmt"
	"math"
	"unicode/utf8"
)

// Index is the part of a SCIP index the workspace reads.
type Index struct {
	ToolName    string
	ToolVersion string
	// ProjectRoot is a path on the machine that built the index. It is read
	// only so that tests can check a fixture was built where they expect.
	ProjectRoot string
	Documents   []Document
}

// Document is one source file's occurrences.
type Document struct {
	// Path is relative to the repository root.
	Path string
	// UTF16 reports that positions count UTF-16 code units from the line's
	// start, which is what the browser's editor counts too. A document in any
	// other encoding cannot be used.
	UTF16       bool
	Occurrences []Occurrence
}

// Occurrence is one place a symbol appears.
type Occurrence struct {
	Range  Range
	Symbol string
	// Definition marks the place the symbol is defined.
	Definition bool
}

// Range is a span of source. Lines and characters count from 0, and the end is
// exclusive.
type Range struct {
	StartLine, StartChar, EndLine, EndChar int32
}

// Empty reports whether the range covers nothing. An index records a whole
// file as an empty range at its start.
func (r Range) Empty() bool { return r.StartLine == r.EndLine && r.StartChar == r.EndChar }

// ErrMalformed means the bytes are not a SCIP index this package can read.
var ErrMalformed = errors.New("the index is malformed")

// Wire types, from the protobuf encoding.
const (
	wireVarint = 0
	wireFixed8 = 1
	wireBytes  = 2
	wireFixed4 = 5
)

const (
	roleDefinition = 0x1 // SymbolRole.Definition
	encodingUTF16  = 2   // PositionEncoding.UTF16CodeUnitOffsetFromLineStart
)

// Limits bound what Decode will build from an index.
type Limits struct {
	// Occurrences is the most occurrences kept, across all documents.
	Occurrences int
}

// ErrTooLarge means the index holds more than Limits allow.
var ErrTooLarge = errors.New("the index holds more than the configured limit")

// field is one field of a message: its number, its wire type, and its value as
// a number or as bytes.
type field struct {
	number int
	wire   int
	value  uint64
	bytes  []byte
}

// next reads one field from the front of buf and returns the rest.
func next(buf []byte) (field, []byte, error) {
	key, rest, err := varint(buf)
	if err != nil {
		return field{}, nil, err
	}
	f := field{number: int(key >> 3), wire: int(key & 7)}
	if key>>3 == 0 || key>>3 > math.MaxInt32 {
		return field{}, nil, fmt.Errorf("%w: field number %d", ErrMalformed, key>>3)
	}
	switch f.wire {
	case wireVarint:
		f.value, rest, err = varint(rest)
		if err != nil {
			return field{}, nil, err
		}
	case wireBytes:
		size, after, err := varint(rest)
		if err != nil {
			return field{}, nil, err
		}
		// Compared as unsigned and against what is really there: a length
		// that claims more than the buffer holds is refused, not trusted.
		if size > uint64(len(after)) {
			return field{}, nil, fmt.Errorf("%w: a field claims %d bytes and %d remain", ErrMalformed, size, len(after))
		}
		f.bytes, rest = after[:size], after[size:]
	case wireFixed8:
		if len(rest) < 8 {
			return field{}, nil, fmt.Errorf("%w: truncated", ErrMalformed)
		}
		rest = rest[8:]
	case wireFixed4:
		if len(rest) < 4 {
			return field{}, nil, fmt.Errorf("%w: truncated", ErrMalformed)
		}
		rest = rest[4:]
	default:
		// Groups, which the schema does not use, and the two unassigned types.
		return field{}, nil, fmt.Errorf("%w: wire type %d", ErrMalformed, f.wire)
	}
	return f, rest, nil
}

func varint(buf []byte) (uint64, []byte, error) {
	var value uint64
	for i := 0; i < len(buf); i++ {
		if i == 10 {
			return 0, nil, fmt.Errorf("%w: a number runs past ten bytes", ErrMalformed)
		}
		b := buf[i]
		value |= uint64(b&0x7F) << (7 * uint(i))
		if b&0x80 == 0 {
			return value, buf[i+1:], nil
		}
	}
	return 0, nil, fmt.Errorf("%w: truncated", ErrMalformed)
}

// text returns a field's bytes as a string, refusing what is not UTF-8. Paths
// and symbols become map keys and reach the browser, so they must be text.
func text(f field) (string, error) {
	if f.wire != wireBytes || !utf8.Valid(f.bytes) {
		return "", fmt.Errorf("%w: field %d is not text", ErrMalformed, f.number)
	}
	return string(f.bytes), nil
}

// Decode reads an index.
func Decode(raw []byte, limits Limits) (*Index, error) {
	index := &Index{}
	kept := 0
	for buf := raw; len(buf) > 0; {
		f, rest, err := next(buf)
		if err != nil {
			return nil, err
		}
		buf = rest
		if f.wire != wireBytes {
			continue
		}
		switch f.number {
		case 1: // metadata
			if err := index.metadata(f.bytes); err != nil {
				return nil, err
			}
		case 2: // documents
			doc, err := document(f.bytes, limits.Occurrences-kept)
			if err != nil {
				return nil, err
			}
			kept += len(doc.Occurrences)
			index.Documents = append(index.Documents, doc)
		}
	}
	return index, nil
}

func (index *Index) metadata(buf []byte) error {
	for len(buf) > 0 {
		f, rest, err := next(buf)
		if err != nil {
			return err
		}
		buf = rest
		switch {
		case f.number == 2 && f.wire == wireBytes: // tool_info
			for tool := f.bytes; len(tool) > 0; {
				t, after, err := next(tool)
				if err != nil {
					return err
				}
				tool = after
				switch t.number {
				case 1:
					if index.ToolName, err = text(t); err != nil {
						return err
					}
				case 2:
					if index.ToolVersion, err = text(t); err != nil {
						return err
					}
				}
			}
		case f.number == 3: // project_root
			if index.ProjectRoot, err = text(f); err != nil {
				return err
			}
		}
	}
	return nil
}

func document(buf []byte, room int) (Document, error) {
	var doc Document
	for len(buf) > 0 {
		f, rest, err := next(buf)
		if err != nil {
			return doc, err
		}
		buf = rest
		switch f.number {
		case 1: // relative_path
			if doc.Path, err = text(f); err != nil {
				return doc, err
			}
		case 2: // occurrences
			if f.wire != wireBytes {
				continue
			}
			occ, ok, err := occurrence(f.bytes)
			if err != nil {
				return doc, err
			}
			if !ok {
				continue
			}
			if len(doc.Occurrences) >= room {
				return doc, ErrTooLarge
			}
			doc.Occurrences = append(doc.Occurrences, occ)
		case 6: // position_encoding
			doc.UTF16 = f.wire == wireVarint && f.value == encodingUTF16
		}
	}
	return doc, nil
}

// occurrence reads one occurrence. ok is false for one that has no symbol or
// no usable range: such an occurrence says nothing a lookup could use.
func occurrence(buf []byte) (occ Occurrence, ok bool, err error) {
	var (
		list          []uint64
		single, multi []byte
		typed         bool
	)
	for len(buf) > 0 {
		f, rest, err := next(buf)
		if err != nil {
			return occ, false, err
		}
		buf = rest
		switch f.number {
		case 1: // range: three or four integers, packed or one at a time
			switch f.wire {
			case wireVarint:
				list = append(list, f.value)
			case wireBytes:
				for packed := f.bytes; len(packed) > 0; {
					v, after, err := varint(packed)
					if err != nil {
						return occ, false, err
					}
					packed = after
					if len(list) < 5 {
						list = append(list, v)
					}
				}
			}
		case 2:
			if occ.Symbol, err = text(f); err != nil {
				return occ, false, err
			}
		case 3: // symbol_roles
			if f.wire == wireVarint {
				occ.Definition = f.value&roleDefinition != 0
			}
		case 8: // single_line_range
			if f.wire == wireBytes {
				single, typed = f.bytes, true
			}
		case 9: // multi_line_range
			if f.wire == wireBytes {
				multi, typed = f.bytes, true
			}
		}
	}
	if occ.Symbol == "" {
		return occ, false, nil
	}

	// A typed form wins over the list when both are present.
	var parts [4]uint64
	switch {
	case typed && single != nil:
		nums, err := numbered(single, 3)
		if err != nil {
			return occ, false, err
		}
		parts = [4]uint64{nums[1], nums[2], nums[1], nums[3]}
	case typed:
		nums, err := numbered(multi, 4)
		if err != nil {
			return occ, false, err
		}
		parts = [4]uint64{nums[1], nums[2], nums[3], nums[4]}
	case len(list) == 3:
		parts = [4]uint64{list[0], list[1], list[0], list[2]}
	case len(list) == 4:
		parts = [4]uint64{list[0], list[1], list[2], list[3]}
	default:
		return occ, false, nil
	}
	for _, p := range parts {
		if p > math.MaxInt32 {
			return occ, false, nil
		}
	}
	occ.Range = Range{int32(parts[0]), int32(parts[1]), int32(parts[2]), int32(parts[3])}
	if occ.Range.EndLine < occ.Range.StartLine ||
		(occ.Range.EndLine == occ.Range.StartLine && occ.Range.EndChar < occ.Range.StartChar) {
		return occ, false, nil
	}
	return occ, true, nil
}

// numbered reads a message whose fields 1 to max are all integers, and returns
// them indexed by field number. A field left out is 0, as protobuf has it.
func numbered(buf []byte, max int) ([5]uint64, error) {
	var out [5]uint64
	for len(buf) > 0 {
		f, rest, err := next(buf)
		if err != nil {
			return out, err
		}
		buf = rest
		if f.wire == wireVarint && f.number >= 1 && f.number <= max {
			out[f.number] = f.value
		}
	}
	return out, nil
}
