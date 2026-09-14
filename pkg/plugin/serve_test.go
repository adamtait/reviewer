// SPDX-License-Identifier: MIT

package plugin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/pkg/finding"
)

type fakeHandler struct {
	descriptors []Descriptor
	analyze     func(id string, req AnalyzeRequest) ([]finding.Finding, []string, error)
	calls       []string
}

func (f *fakeHandler) Name() string    { return "fake" }
func (f *fakeHandler) Version() string { return "0.0.1" }
func (f *fakeHandler) Describe() []Descriptor {
	return f.descriptors
}
func (f *fakeHandler) Analyze(_ context.Context, id string, req AnalyzeRequest) ([]finding.Finding, []string, error) {
	f.calls = append(f.calls, id)
	if f.analyze != nil {
		return f.analyze(id, req)
	}
	return nil, nil, nil
}

// converse drives serve with a scripted host and returns the frames it wrote.
func converse(t *testing.T, h Handler, lines ...string) ([]Frame, string, error) {
	t.Helper()
	var out, logs bytes.Buffer
	err := serve(context.Background(), h, strings.NewReader(strings.Join(lines, "\n")+"\n"), &out, &logs)

	var frames []Frame
	r := NewReader(bytes.NewReader(out.Bytes()))
	for {
		f, readErr := r.Read()
		if errors.Is(readErr, ErrClosed) {
			break
		}
		if readErr != nil {
			t.Fatalf("serve wrote something that is not a frame: %v", readErr)
		}
		frames = append(frames, f)
	}
	return frames, logs.String(), err
}

func TestServeFullConversation(t *testing.T) {
	h := &fakeHandler{
		descriptors: []Descriptor{{ID: "demo", Lane: finding.LaneDeterministic, Order: 10, Available: true}},
		analyze: func(id string, req AnalyzeRequest) ([]finding.Finding, []string, error) {
			if req.Root != "/repo" {
				return nil, nil, fmt.Errorf("want the request root, got %q", req.Root)
			}
			return []finding.Finding{{
				RuleID: "demo/found", Lane: finding.LaneDeterministic,
				Confidence: finding.ConfidenceHigh, Severity: finding.SeverityInfo,
				File: "a.ts", Line: 1, Message: "hello",
			}}, []string{"a warning"}, nil
		},
	}
	frames, _, err := converse(t, h,
		`{"type":"hello","protocol":1,"host":"test"}`,
		`{"type":"describe"}`,
		`{"type":"analyze","analyzer":"demo","request":{"root":"/repo"}}`,
		`{"type":"bye"}`,
	)
	if err != nil {
		t.Fatalf("serve returned an error: %v", err)
	}
	got := make([]FrameType, len(frames))
	for i, f := range frames {
		got[i] = f.Type
	}
	want := []FrameType{TypeHello, TypeDescribe, TypeFindings}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	if frames[0].Protocol != Protocol || frames[0].Plugin != "fake" {
		t.Fatalf("hello frame is wrong: %+v", frames[0])
	}
	if len(frames[2].Findings) != 1 || frames[2].Warnings[0] != "a warning" {
		t.Fatalf("findings frame is wrong: %+v", frames[2])
	}
	if fmt.Sprint(h.calls) != "[demo]" {
		t.Fatalf("want one call to demo, got %v", h.calls)
	}
}

func TestServeRefusesAProtocolMismatch(t *testing.T) {
	frames, _, err := converse(t, &fakeHandler{}, `{"type":"hello","protocol":99}`)
	if err == nil {
		t.Fatal("want an error for an unknown protocol version")
	}
	if !strings.Contains(err.Error(), "protocol 99") {
		t.Fatalf("want the version named, got %v", err)
	}
	if len(frames) != 1 || frames[0].Type != TypeError {
		t.Fatalf("want a single error frame so the host learns why, got %+v", frames)
	}
}

func TestServeRefusesWorkBeforeHello(t *testing.T) {
	frames, _, err := converse(t, &fakeHandler{}, `{"type":"analyze","analyzer":"demo"}`)
	if err == nil || !strings.Contains(err.Error(), "before hello") {
		t.Fatalf("want a handshake-order error, got %v", err)
	}
	if len(frames) != 1 || frames[0].Type != TypeError {
		t.Fatalf("want an error frame, got %+v", frames)
	}
}

// An analyzer that fails must not take the run down (ADR-0013): the host gets an
// error frame for that analyzer and the conversation continues.
func TestServeReportsAnalyzerErrorsAndKeepsGoing(t *testing.T) {
	h := &fakeHandler{
		analyze: func(id string, _ AnalyzeRequest) ([]finding.Finding, []string, error) {
			if id == "broken" {
				return nil, nil, errors.New("the config is missing")
			}
			return nil, nil, nil
		},
	}
	frames, logs, err := converse(t, h,
		`{"type":"hello","protocol":1}`,
		`{"type":"analyze","analyzer":"broken"}`,
		`{"type":"analyze","analyzer":"fine"}`,
		`{"type":"bye"}`,
	)
	if err != nil {
		t.Fatalf("an analyzer error must not fail serve, got %v", err)
	}
	if len(frames) != 3 || frames[1].Type != TypeError || frames[2].Type != TypeFindings {
		t.Fatalf("want hello, error, findings; got %+v", frames)
	}
	if frames[1].Analyzer != "broken" {
		t.Fatalf("the error frame must name the analyzer, got %q", frames[1].Analyzer)
	}
	if !strings.Contains(logs, "the config is missing") {
		t.Fatalf("want the failure on stderr for the run log, got %q", logs)
	}
	if fmt.Sprint(h.calls) != "[broken fine]" {
		t.Fatalf("want both analyzers attempted, got %v", h.calls)
	}
}

func TestServeSurvivesGarbageOnTheWire(t *testing.T) {
	frames, logs, err := converse(t, &fakeHandler{},
		`{"type":"hello","protocol":1}`,
		`oops, a log line leaked onto stdin`,
		`{"type":"bye"}`,
	)
	if err != nil {
		t.Fatalf("a bad line must not fail serve, got %v", err)
	}
	if len(frames) != 2 || frames[1].Type != TypeError {
		t.Fatalf("want hello then an error frame, got %+v", frames)
	}
	if !strings.Contains(logs, "not a protocol frame") {
		t.Fatalf("want the bad line reported on stderr, got %q", logs)
	}
}

func TestServeTreatsAClosedStreamAsANormalExit(t *testing.T) {
	// The host timed out or was killed. There is nothing to clean up and nothing
	// to report, so this is not an error.
	if err := serve(context.Background(), &fakeHandler{}, strings.NewReader(`{"type":"hello","protocol":1}`+"\n"), io.Discard, io.Discard); err != nil {
		t.Fatalf("want a clean exit on a closed stream, got %v", err)
	}
}

func TestServeIgnoresUnknownFrameTypes(t *testing.T) {
	// Additive extensibility: a newer host may send frames this plugin predates.
	frames, logs, err := converse(t, &fakeHandler{},
		`{"type":"hello","protocol":1}`,
		`{"type":"prefetch","analyzer":"demo"}`,
		`{"type":"bye"}`,
	)
	if err != nil {
		t.Fatalf("an unknown frame type must not be fatal, got %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("want only the hello reply, got %+v", frames)
	}
	if !strings.Contains(logs, `unknown frame type "prefetch"`) {
		t.Fatalf("want the unknown type noted on stderr, got %q", logs)
	}
}
