// SPDX-License-Identifier: MIT

// Package plugin defines the protocol every analyzer speaks to the core, and the
// Go SDK for writing one (ADR-0006, ADR-0025).
//
// The wire format is newline-delimited JSON over stdio: one JSON object per line,
// no framing header, no length prefix. A plugin in any language — including a
// shell script — can implement it, which is the point.
//
// # Channel discipline
//
// Stdout carries protocol frames and nothing else. A plugin that prints a log
// line, a progress bar or a stack trace to stdout corrupts the stream. Diagnostics
// go to stderr, which the host captures into the run log.
//
// # Conversation
//
//	host → hello      protocol version and host identity
//	plugin → hello    protocol version, plugin name and version
//	host → describe
//	plugin → describe the analyzers this plugin provides
//	host → analyze    one analyzer at a time, in the order the core decides
//	plugin → findings or error
//	host → bye
//	plugin           exits
//
// The plugin process lives for the whole run. That is what lets one plugin serve
// many analyzers from shared state — the TypeScript plugin builds a compiler
// program once and reuses it for every analyzer that needs one (ADR-0014).
package plugin

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/adamtait/reviewer/pkg/finding"
)

// Protocol is the wire version this package implements. It is bumped
// independently of the product version: what a plugin author needs to know is
// which protocol, not which release (ADR-0026).
const Protocol = 1

// MaxFrameBytes bounds one line. A findings frame from a large repository can be
// hundreds of kilobytes, so this is generous; it exists to stop a plugin that
// never emits a newline from exhausting the host's memory.
const MaxFrameBytes = 16 << 20

// FrameType discriminates a frame. Unknown types are ignored by the host rather
// than fatal, so a newer plugin may send frames an older host does not know.
type FrameType string

const (
	// TypeHello opens the conversation in both directions.
	TypeHello FrameType = "hello"
	// TypeDescribe asks for, then carries, the analyzer list.
	TypeDescribe FrameType = "describe"
	// TypeAnalyze asks one analyzer to run.
	TypeAnalyze FrameType = "analyze"
	// TypeFindings answers TypeAnalyze.
	TypeFindings FrameType = "findings"
	// TypeError answers anything, and is never fatal to the run: the host records
	// it, drops that analyzer's results, and carries on (ADR-0013).
	TypeError FrameType = "error"
	// TypeBye asks the plugin to exit.
	TypeBye FrameType = "bye"
)

// Lane mirrors finding.Lane. A descriptor declares its analyzer's lane so the
// core can block the model lane without trusting the analyzer to behave
// (ADR-0005).
type Lane = finding.Lane

// Descriptor is what a plugin says about one analyzer it provides.
type Descriptor struct {
	// ID is the analyzer's stable name, used in --only/--skip and in warnings.
	ID string `json:"id"`
	// Lane decides whether the secrets gate may block this analyzer.
	Lane Lane `json:"lane"`
	// Order places this analyzer in the run sequence. Lower runs earlier. The
	// core's own convention leaves gaps so plugins can interleave.
	Order int `json:"order"`
	// Available is false when the analyzer cannot run in this repository — no
	// ESLint config, no test runner, a missing binary. The core skips it and
	// reports Unavailable once, rather than failing.
	Available bool `json:"available"`
	// Unavailable explains Available == false, in one line, for the run log.
	Unavailable string `json:"unavailable,omitempty"`
}

// ChangedFile is one file in the diff under review, with the line ranges the diff
// touched. Analyzers must restrict their findings to these ranges; the core
// filters again, but an analyzer that scopes itself is faster.
type ChangedFile struct {
	Path string `json:"path"`
	// Ranges are inclusive [start, end] pairs, 1-indexed.
	Ranges [][2]int `json:"ranges"`
}

// AnalyzeRequest is everything an analyzer is given. It deliberately does not
// carry the whole configuration: a plugin sees the repository, the diff, the
// projects in scope, and its own opaque settings block.
type AnalyzeRequest struct {
	// Root is the absolute path of the repository under review.
	Root string `json:"root"`
	// Changed lists the files and line ranges in the diff.
	Changed []ChangedFile `json:"changed"`
	// Projects narrows a monorepo to the affected workspaces. Empty means all.
	Projects []string `json:"projects,omitempty"`
	// Base is the ref the diff was taken against, so an analyzer that needs the
	// previous contents of a changed file can read them. Empty for a staged diff,
	// where the previous contents are HEAD's. An analyzer that only needs the
	// changed lines never looks at it.
	Base string `json:"base,omitempty"`
	// ContextLines is how much surrounding source to include where an analyzer
	// has a choice.
	ContextLines int `json:"contextLines,omitempty"`
	// Prior carries what the deterministic lane already reported, and is set only
	// for analyzers in the model lane — which run in a second pass, after the
	// secrets gate has decided (ADR-0012). An analyzer that sees it can avoid
	// rediscovering what a cheaper analyzer already found, and spend its attention
	// on what those cannot see.
	//
	// Empty in the first pass, by construction: nothing has been found yet.
	Prior []finding.Finding `json:"prior,omitempty"`
	// Settings is this analyzer's block from the destination repository's config,
	// passed through untouched. The protocol does not know its shape, which is how
	// an analyzer gains an option without a protocol change.
	Settings json.RawMessage `json:"settings,omitempty"`
}

// Frame is one line on the wire. It is a flat envelope rather than a tagged union
// so that a plugin in any language can emit one with string concatenation if it
// has to, and so the JSON Schema stays readable.
type Frame struct {
	Type FrameType `json:"type"`

	// hello, both directions.
	Protocol int    `json:"protocol,omitempty"`
	Host     string `json:"host,omitempty"`
	Plugin   string `json:"plugin,omitempty"`
	Version  string `json:"version,omitempty"`

	// describe, plugin → host.
	Analyzers []Descriptor `json:"analyzers,omitempty"`

	// analyze, host → plugin.
	Analyzer string          `json:"analyzer,omitempty"`
	Request  *AnalyzeRequest `json:"request,omitempty"`

	// findings, plugin → host.
	Findings []finding.Finding `json:"findings,omitempty"`
	Warnings []string          `json:"warnings,omitempty"`

	// error, plugin → host.
	Message string `json:"message,omitempty"`
}

// ErrClosed is returned by Reader.Read when the stream ends cleanly.
var ErrClosed = errors.New("plugin: stream closed")

// ErrFrameTooLarge is returned when a line exceeds MaxFrameBytes, which in
// practice means a plugin wrote non-protocol output to stdout.
var ErrFrameTooLarge = errors.New("plugin: frame exceeds the maximum size")

// Reader decodes frames from a stream.
type Reader struct {
	br *bufio.Reader
}

// NewReader wraps r. The buffer grows to MaxFrameBytes as needed.
func NewReader(r io.Reader) *Reader {
	return &Reader{br: bufio.NewReaderSize(r, 64<<10)}
}

// Read returns the next frame. It reports ErrClosed at end of stream, and a
// decode error — with the offending line quoted and truncated — for a line that
// is not a protocol frame. Callers decide whether a bad line is fatal; the host
// treats it as a warning against that plugin (ADR-0013).
func (r *Reader) Read() (Frame, error) {
	line, err := r.readLine()
	if err != nil {
		return Frame{}, err
	}
	var f Frame
	if err := json.Unmarshal(line, &f); err != nil {
		return Frame{}, fmt.Errorf("plugin: line is not a protocol frame (%w): %s", err, truncate(line, 200))
	}
	if f.Type == "" {
		return Frame{}, fmt.Errorf("plugin: frame has no type: %s", truncate(line, 200))
	}
	return f, nil
}

func (r *Reader) readLine() ([]byte, error) {
	var buf []byte
	for {
		chunk, err := r.br.ReadSlice('\n')
		buf = append(buf, chunk...)
		if len(buf) > MaxFrameBytes {
			return nil, ErrFrameTooLarge
		}
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if len(trimSpace(buf)) == 0 {
				return nil, ErrClosed
			}
			return trimSpace(buf), nil
		case err != nil:
			return nil, err
		}
		if line := trimSpace(buf); len(line) > 0 {
			return line, nil
		}
		buf = buf[:0] // blank line between frames; keep reading
	}
}

// Writer encodes frames to a stream, one line each.
type Writer struct {
	w   io.Writer
	enc *json.Encoder
}

// NewWriter wraps w. Frames are written with a trailing newline and no indentation.
func NewWriter(w io.Writer) *Writer {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &Writer{w: w, enc: enc}
}

// Write encodes one frame. json.Encoder already appends the newline that
// separates frames.
func (w *Writer) Write(f Frame) error {
	if f.Type == "" {
		return errors.New("plugin: refusing to write a frame with no type")
	}
	if err := w.enc.Encode(f); err != nil {
		return fmt.Errorf("plugin: writing %s frame: %w", f.Type, err)
	}
	return nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

func trimSpace(b []byte) []byte {
	start := 0
	for start < len(b) && isSpace(b[start]) {
		start++
	}
	end := len(b)
	for end > start && isSpace(b[end-1]) {
		end--
	}
	return b[start:end]
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}
