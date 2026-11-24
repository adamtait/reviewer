// SPDX-License-Identifier: MIT

package gateway

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/adamtait/reviewer/internal/model"
	"github.com/adamtait/reviewer/internal/workspace/scan"
)

// Scripted is a model that says what it was told to say, for tests. Each call
// to Converse plays the next step and records the request it was given.
type Scripted struct {
	mu       sync.Mutex
	steps    []Step
	requests []model.ConvRequest
}

// Step is what one call does: report events and end, or fail.
type Step struct {
	Events []model.Event
	Err    error
}

// NewScripted returns a model that plays the steps in order. A call past the
// last step fails, so a test that expects three calls notices a fourth.
func NewScripted(steps ...Step) *Scripted { return &Scripted{steps: steps} }

// Converse plays the next step.
func (s *Scripted) Converse(ctx context.Context, req model.ConvRequest, onEvent func(model.Event)) error {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	n := len(s.requests) - 1
	var step Step
	if n < len(s.steps) {
		step = s.steps[n]
	} else {
		step = Step{Err: &model.ProviderError{Status: 500, Code: "unscripted", Message: "the test's model was called more times than it was scripted for"}}
	}
	s.mu.Unlock()

	if ctx.Err() != nil {
		return &model.ProviderError{Message: "the call was stopped"}
	}
	if step.Err != nil {
		return step.Err
	}
	for _, ev := range step.Events {
		onEvent(ev)
	}
	return nil
}

// Requests returns every request the model was given, in order.
func (s *Scripted) Requests() []model.ConvRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]model.ConvRequest(nil), s.requests...)
}

// Says is a step in which the model answers with text and stops.
func Says(text string, inputTokens int) Step {
	return Step{Events: []model.Event{
		model.PartDone{Part: model.Opaque{Data: json.RawMessage(`{"type":"thought","signature":"for-the-answer"}`)}},
		model.TextDelta{Text: text},
		model.Usage{InputTokens: inputTokens, OutputTokens: 20, ReasoningTokens: 30},
		model.End{Reason: model.EndStop, Detail: "completed"},
	}}
}

// Asks is a step in which the model asks for tools, with reasoning data ahead
// of them as a real provider sends it.
func Asks(inputTokens int, calls ...model.ToolCall) Step {
	events := []model.Event{
		model.PartDone{Part: model.Opaque{Data: json.RawMessage(`{"type":"thought","signature":"before-` + calls[0].ID + `"}`)}},
	}
	for _, call := range calls {
		events = append(events, model.PartDone{Part: call})
	}
	return Step{Events: append(events,
		model.Usage{InputTokens: inputTokens, OutputTokens: 10, ReasoningTokens: 40},
		model.End{Reason: model.EndToolUse, Detail: "requires_action"},
	)}
}

// Fails is a step in which the provider answers with an error.
func Fails(failure *model.ProviderError) Step { return Step{Err: failure} }

// FakeScanner flags any item that contains Marker, and can be made to fail.
type FakeScanner struct {
	Marker string
	Err    error

	mu      sync.Mutex
	scanned []string
}

// Check flags items that contain the marker.
func (f *FakeScanner) Check(_ context.Context, items []string) ([]scan.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	f.scanned = append(f.scanned, items...)
	out := make([]scan.Result, len(items))
	for i, item := range items {
		if f.Marker != "" && contains(item, f.Marker) {
			out[i] = scan.Result{Flagged: true, Rules: []string{"test-credential"}}
		}
	}
	return out, nil
}

// Scanned returns every item the scanner was shown.
func (f *FakeScanner) Scanned() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.scanned...)
}

func contains(text, marker string) bool {
	for i := 0; i+len(marker) <= len(text); i++ {
		if text[i:i+len(marker)] == marker {
			return true
		}
	}
	return false
}
