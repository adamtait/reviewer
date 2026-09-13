// SPDX-License-Identifier: MIT

package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/pkg/finding"
)

func TestRoundTripEveryFrameType(t *testing.T) {
	frames := []Frame{
		{Type: TypeHello, Protocol: Protocol, Host: "reviewer/0.1.0"},
		{Type: TypeHello, Protocol: Protocol, Plugin: "typescript", Version: "0.1.0"},
		{Type: TypeDescribe},
		{Type: TypeDescribe, Analyzers: []Descriptor{
			{ID: "tsc", Lane: finding.LaneDeterministic, Order: 20, Available: true},
			{ID: "knip", Lane: finding.LaneDeterministic, Order: 60, Available: false, Unavailable: "no knip config"},
		}},
		{Type: TypeAnalyze, Analyzer: "tsc", Request: &AnalyzeRequest{
			Root:         "/repo",
			Changed:      []ChangedFile{{Path: "src/a.ts", Ranges: [][2]int{{1, 4}, {9, 9}}}},
			Projects:     []string{"pkg-a"},
			ContextLines: 20,
			Settings:     json.RawMessage(`{"strict":true}`),
		}},
		{Type: TypeFindings, Analyzer: "tsc", Findings: []finding.Finding{{
			RuleID: "types/TS2322", Lane: finding.LaneDeterministic,
			Confidence: finding.ConfidenceHigh, Severity: finding.SeverityError,
			File: "src/a.ts", Line: 3, Message: "type mismatch",
		}}, Warnings: []string{"ran without project references"}},
		{Type: TypeError, Analyzer: "eslint", Message: "flat config not found"},
		{Type: TypeBye},
	}

	var buf bytes.Buffer
	w := NewWriter(&buf)
	for _, f := range frames {
		if err := w.Write(f); err != nil {
			t.Fatalf("writing %s: %v", f.Type, err)
		}
	}
	// One frame per line is the whole framing rule; assert it directly.
	if got, want := strings.Count(strings.TrimRight(buf.String(), "\n"), "\n")+1, len(frames); got != want {
		t.Fatalf("want %d lines, got %d:\n%s", want, got, buf.String())
	}

	r := NewReader(bytes.NewReader(buf.Bytes()))
	for i, want := range frames {
		got, err := r.Read()
		if err != nil {
			t.Fatalf("reading frame %d: %v", i, err)
		}
		wantJSON, _ := json.Marshal(want)
		gotJSON, _ := json.Marshal(got)
		if !bytes.Equal(wantJSON, gotJSON) {
			t.Fatalf("frame %d round-tripped differently:\n want %s\n  got %s", i, wantJSON, gotJSON)
		}
	}
	if _, err := r.Read(); !errors.Is(err, ErrClosed) {
		t.Fatalf("want ErrClosed at end of stream, got %v", err)
	}
}

func TestReaderRejectsNonFrames(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{"not json", "this is a log line\n", "not a protocol frame"},
		{"json without a type", `{"protocol":1}` + "\n", "frame has no type"},
		{"json array", `[1,2,3]` + "\n", "not a protocol frame"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewReader(strings.NewReader(tc.input)).Read()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestReaderSkipsBlankLinesAndHandlesAMissingFinalNewline(t *testing.T) {
	in := "\n\n" + `{"type":"bye"}`
	f, err := NewReader(strings.NewReader(in)).Read()
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != TypeBye {
		t.Fatalf("want a bye frame, got %q", f.Type)
	}
}

// TestReaderHandlesAFrameLargerThanTheBuffer guards the failure that bufio.Scanner
// would have introduced: a findings frame from a real repository easily exceeds
// 64KB, and silently truncating one would drop findings without an error.
func TestReaderHandlesAFrameLargerThanTheBuffer(t *testing.T) {
	big := strings.Repeat("x", 300<<10)
	line := fmt.Sprintf(`{"type":"error","message":%q}`+"\n", big)
	f, err := NewReader(strings.NewReader(line)).Read()
	if err != nil {
		t.Fatalf("a 300KB frame must decode, got %v", err)
	}
	if len(f.Message) != len(big) {
		t.Fatalf("want the whole message, got %d of %d bytes", len(f.Message), len(big))
	}
}

func TestReaderRefusesAnUnboundedLine(t *testing.T) {
	_, err := NewReader(io.LimitReader(endlessX{}, MaxFrameBytes+1024)).Read()
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("want ErrFrameTooLarge, got %v", err)
	}
}

type endlessX struct{}

func (endlessX) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestWriterRefusesATypelessFrame(t *testing.T) {
	if err := NewWriter(io.Discard).Write(Frame{Message: "oops"}); err == nil {
		t.Fatal("want an error for a frame with no type")
	}
}
