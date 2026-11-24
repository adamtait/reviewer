// SPDX-License-Identifier: MIT

package model

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// The conversation interface.
//
// Provider, in this package, is a prompt in and text out, which is all a
// review needs. A reader asking questions about code needs more: the model
// asks for tools, the answer arrives as it is written, and the tokens are
// counted. Converser is that, beside Provider and not instead of it. The
// command-line tool goes on using Provider unchanged (ADR-0033).

// Part is one piece of a turn: text, a request to run a tool, a tool's result,
// or a provider's own reasoning data.
type Part interface{ isPart() }

// Text is plain text, from the user or the model.
type Text struct{ Text string }

// ToolCall is the model asking for a tool to be run.
type ToolCall struct {
	// ID ties the result to the call. It is the provider's, passed back unread.
	ID   string
	Name string
	// ArgumentsJSON is a JSON object, as the model wrote it.
	ArgumentsJSON string
}

// ToolResult is what a tool returned, sent back to the model.
type ToolResult struct {
	CallID string
	Name   string
	Text   string
	// IsError marks a tool that could not do what was asked. The model is told,
	// and the conversation goes on.
	IsError bool
}

// Opaque is a provider's reasoning data for one model call.
//
// Some providers refuse a tool loop unless the reasoning data from each step
// is sent back with the tool results. Nothing here reads it. It is carried
// from one call to the next inside a single question and then dropped: a
// finished question is replayed to the model as plain text, which the
// provider accepts, so it never needs to leave the server.
type Opaque struct{ Data json.RawMessage }

func (Text) isPart()       {}
func (ToolCall) isPart()   {}
func (ToolResult) isPart() {}
func (Opaque) isPart()     {}

// RoleAssistant is the model's side of a conversation. Role and RoleUser are
// declared beside Provider, which has no use for this one: a review is one
// question and no answer is ever sent back.
const RoleAssistant Role = "assistant"

// Turn is one side's contribution: a question, an answer, a model's tool
// calls, or the results of those calls.
type Turn struct {
	Role  Role
	Parts []Part
}

// ToolSpec describes a tool to the model.
type ToolSpec struct {
	Name        string
	Description string
	// SchemaJSON is a JSON Schema object for the tool's arguments.
	SchemaJSON string
}

// ConvRequest is one call to a model.
type ConvRequest struct {
	// Model is sent to the provider exactly as given.
	Model  string
	System string
	Turns  []Turn
	Tools  []ToolSpec
	// MaxOutputTokens bounds what the model writes in this call, reasoning
	// included. Zero leaves it to the provider.
	MaxOutputTokens int
	// Timeout bounds the whole call, from the request to the last event.
	Timeout time.Duration
}

// Event is something that happened during a call, in the order it happened.
type Event interface{ isEvent() }

// TextDelta is more of the answer.
type TextDelta struct{ Text string }

// PartDone is a finished ToolCall or Opaque part. The caller keeps these, in
// order, to build the assistant's turn for the next call.
type PartDone struct{ Part Part }

// Usage is the provider's count of tokens for the call.
type Usage struct {
	InputTokens     int
	OutputTokens    int
	ReasoningTokens int
}

// EndReason says why a call ended.
type EndReason string

const (
	// EndStop: the model finished its answer.
	EndStop EndReason = "stop"
	// EndToolUse: the model asked for tools and is waiting for their results.
	EndToolUse EndReason = "tool_use"
	// EndMaxTokens: the answer was cut off at the output limit.
	EndMaxTokens EndReason = "max_tokens"
	// EndRefused: the provider ended the call without an answer and without
	// an error: a safety block, or a status this code does not know.
	EndRefused EndReason = "refused"
)

// End is the last event of a call that did not fail.
type End struct {
	Reason EndReason
	// Detail is the provider's own word for how the call ended, for the log.
	Detail string
}

func (TextDelta) isEvent() {}
func (PartDone) isEvent()  {}
func (Usage) isEvent()     {}
func (End) isEvent()       {}

// Converser makes one call to a model and reports what comes back.
//
// A caller continues a tool loop by appending one assistant turn holding every
// PartDone part and the text received, in order, then one user turn holding
// the tool results.
//
// Converse does not retry. Any failure is returned as a *ProviderError; a
// failure to reach the provider at all has Status 0.
type Converser interface {
	Converse(ctx context.Context, req ConvRequest, onEvent func(Event)) error
}

// ProviderError is a call that failed.
type ProviderError struct {
	// Status is the HTTP status, or 0 if the provider was not reached.
	Status int
	// Code is the provider's own code for the failure, in its own spelling:
	// "too_many_requests", or "UNAUTHENTICATED". Providers send two shapes of
	// error body, and this holds whichever word was there.
	Code    string
	Message string
	// RetryAfter is how long the provider said to wait, when it said.
	RetryAfter    time.Duration
	HasRetryAfter bool
}

func (e *ProviderError) Error() string {
	if e.Status == 0 {
		return fmt.Sprintf("the model provider could not be reached: %s", e.Message)
	}
	return fmt.Sprintf("the model provider answered %d %s: %s", e.Status, e.Code, e.Message)
}

// ProviderGemini is Google's Gemini API, called through its interactions method.
const ProviderGemini = "gemini"

// NewConverser returns a Converser for one provider.
//
// baseURL is where the provider's API is, from configuration: no address is
// compiled in (ADR-0004). apiKey is sent with every call and appears in no
// error and no event.
func NewConverser(provider, baseURL, apiKey string) (Converser, error) {
	if baseURL == "" || apiKey == "" {
		return nil, fmt.Errorf("a conversation with %q needs a base URL and a key", provider)
	}
	switch provider {
	case ProviderGemini:
		return &interactions{baseURL: baseURL, apiKey: apiKey}, nil
	default:
		return nil, fmt.Errorf("no conversation adapter for provider %q; the only one is %q", provider, ProviderGemini)
	}
}
