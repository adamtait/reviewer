// SPDX-License-Identifier: MIT

package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// interactions is the Converser for Google's `interactions` method.
//
// Every call is stateless: `store` is false, so the provider keeps nothing and
// the whole conversation is sent each time. It is streamed, so the answer can
// be shown as it is written. The model name is sent exactly as configured.
//
// What the method sends and accepts was recorded from the live service; the
// recordings are in testdata/interactions and this adapter's tests replay them.
type interactions struct {
	baseURL string
	apiKey  string
	// client is replaced in tests. Nil means a client with no timeout of its
	// own: a stream lasts as long as the model takes, and the request's
	// context is what bounds it.
	client *http.Client
}

// maxErrorBody bounds how much of a failed response is read.
const maxErrorBody = 64 << 10

// maxEventBytes bounds one stream event. Events are small; the bound is against
// a response that is not what it claims to be.
const maxEventBytes = 8 << 20

// The steps of an interaction, as sent and received. A conversation is a flat
// list of them.
type step struct {
	Type string `json:"type"`
	// user_input
	Content any `json:"content,omitempty"`
	// function_call and function_result
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Result    []content       `json:"result,omitempty"`
}

type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type functionTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type interactionRequest struct {
	Model  string `json:"model"`
	Store  bool   `json:"store"`
	Stream bool   `json:"stream"`
	// Input is a list of steps and of reasoning data passed through as it came.
	Input             []any          `json:"input"`
	Tools             []functionTool `json:"tools,omitempty"`
	SystemInstruction string         `json:"system_instruction,omitempty"`
	GenerationConfig  map[string]any `json:"generation_config,omitempty"`
}

// body builds the request for a conversation.
func (a *interactions) body(req ConvRequest) (interactionRequest, error) {
	out := interactionRequest{Model: req.Model, Store: false, Stream: true, SystemInstruction: req.System, Input: []any{}}
	if req.MaxOutputTokens > 0 {
		out.GenerationConfig = map[string]any{"max_output_tokens": req.MaxOutputTokens}
	}
	for _, tool := range req.Tools {
		if !json.Valid([]byte(tool.SchemaJSON)) {
			return out, fmt.Errorf("tool %q has a schema that is not JSON", tool.Name)
		}
		out.Tools = append(out.Tools, functionTool{
			Type: "function", Name: tool.Name, Description: tool.Description, Parameters: json.RawMessage(tool.SchemaJSON),
		})
	}
	for _, turn := range req.Turns {
		for _, part := range turn.Parts {
			switch p := part.(type) {
			case Text:
				if turn.Role == RoleUser {
					out.Input = append(out.Input, step{Type: "user_input", Content: p.Text})
				} else {
					out.Input = append(out.Input, step{Type: "model_output", Content: []content{{Type: "text", Text: p.Text}}})
				}
			case ToolCall:
				arguments := json.RawMessage(p.ArgumentsJSON)
				if len(arguments) == 0 || !json.Valid(arguments) {
					arguments = json.RawMessage("{}")
				}
				out.Input = append(out.Input, step{Type: "function_call", ID: p.ID, Name: p.Name, Arguments: arguments})
			case ToolResult:
				text := p.Text
				if p.IsError {
					// Said in the text. The method has no documented field for
					// it, and the model reads the text either way.
					text = "The tool could not do this: " + text
				}
				out.Input = append(out.Input, step{
					Type: "function_result", Name: p.Name, CallID: p.CallID, Result: []content{{Type: "text", Text: text}},
				})
			case Opaque:
				// Exactly as it was received. Nothing here knows what is in it.
				if json.Valid(p.Data) {
					out.Input = append(out.Input, p.Data)
				}
			}
		}
	}
	return out, nil
}

// Converse makes one call.
func (a *interactions) Converse(ctx context.Context, req ConvRequest, onEvent func(Event)) error {
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	body, err := a.body(req)
	if err != nil {
		return &ProviderError{Message: err.Error()}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return &ProviderError{Message: "the request could not be written"}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.baseURL, "/")+"/interactions", bytes.NewReader(raw))
	if err != nil {
		return &ProviderError{Message: "the provider's address is not usable"}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("x-goog-api-key", a.apiKey)

	client := a.client
	if client == nil {
		client = &http.Client{}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return &ProviderError{Message: a.clean(transportMessage(ctx, err))}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		failed, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return a.failure(resp.StatusCode, resp.Header, failed)
	}
	return a.stream(ctx, resp.Body, onEvent)
}

// building is one step of the response while its pieces are still arriving.
type building struct {
	kind      string
	raw       map[string]json.RawMessage // the step as it started, for reasoning data
	id, name  string
	arguments strings.Builder
}

// stream reads the events of a successful response.
func (a *interactions) stream(ctx context.Context, body io.Reader, onEvent func(Event)) error {
	steps := map[int]*building{}
	calls := 0
	ended := false

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), maxEventBytes)
	for scanner.Scan() {
		line := scanner.Text()
		data, isData := strings.CutPrefix(line, "data:")
		if !isData {
			continue // "event:" lines repeat what is in the data; blank lines separate
		}
		data = strings.TrimSpace(data)
		if data == "" || data == "[DONE]" {
			continue
		}
		var ev struct {
			EventType string `json:"event_type"`
			Index     int    `json:"index"`
			Step      json.RawMessage
			Delta     struct {
				Type      string `json:"type"`
				Text      string `json:"text"`
				Signature string `json:"signature"`
				Arguments string `json:"arguments"`
			} `json:"delta"`
			Interaction struct {
				Status string `json:"status"`
				Usage  struct {
					Input   int `json:"total_input_tokens"`
					Output  int `json:"total_output_tokens"`
					Thought int `json:"total_thought_tokens"`
				} `json:"usage"`
			} `json:"interaction"`
			Error *errorBody `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return &ProviderError{Status: http.StatusOK, Code: "unreadable_stream", Message: "the provider sent an event that is not JSON"}
		}

		switch ev.EventType {
		case "step.start":
			b := &building{raw: map[string]json.RawMessage{}}
			_ = json.Unmarshal(ev.Step, &b.raw)
			var head struct{ Type, ID, Name string }
			_ = json.Unmarshal(ev.Step, &head)
			b.kind, b.id, b.name = head.Type, head.ID, head.Name
			steps[ev.Index] = b

		case "step.delta":
			b := steps[ev.Index]
			if b == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text":
				if b.kind == "model_output" && ev.Delta.Text != "" {
					onEvent(TextDelta{Text: ev.Delta.Text})
				}
			case "arguments_delta":
				b.arguments.WriteString(ev.Delta.Arguments)
			case "thought_signature":
				signature, _ := json.Marshal(ev.Delta.Signature)
				b.raw["signature"] = signature
			}

		case "step.stop":
			b := steps[ev.Index]
			if b == nil {
				continue
			}
			delete(steps, ev.Index)
			switch b.kind {
			case "function_call":
				arguments := b.arguments.String()
				if arguments == "" {
					// Some calls carry their arguments whole in the step and
					// send no delta.
					arguments = string(b.raw["arguments"])
				}
				if !json.Valid([]byte(arguments)) {
					arguments = "{}"
				}
				calls++
				onEvent(PartDone{Part: ToolCall{ID: b.id, Name: b.name, ArgumentsJSON: arguments}})
			case "thought":
				// Kept whole and unread, to be sent back as it is.
				if whole, err := json.Marshal(b.raw); err == nil {
					onEvent(PartDone{Part: Opaque{Data: whole}})
				}
			}

		case "interaction.completed":
			onEvent(Usage{
				InputTokens: ev.Interaction.Usage.Input, OutputTokens: ev.Interaction.Usage.Output,
				ReasoningTokens: ev.Interaction.Usage.Thought,
			})
			onEvent(End{Reason: endReason(ev.Interaction.Status, calls), Detail: ev.Interaction.Status})
			ended = true

		case "error":
			if ev.Error != nil {
				return a.fromBody(http.StatusOK, nil, *ev.Error)
			}
			return &ProviderError{Status: http.StatusOK, Code: "error", Message: "the provider reported an error without saying what"}
		}
	}
	if err := scanner.Err(); err != nil {
		return &ProviderError{Message: a.clean(transportMessage(ctx, err))}
	}
	if !ended {
		// The connection closed without the event that ends a response.
		if ctx.Err() != nil {
			return &ProviderError{Message: transportMessage(ctx, ctx.Err())}
		}
		return &ProviderError{Status: http.StatusOK, Code: "incomplete_stream", Message: "the provider's response ended before it was complete"}
	}
	return nil
}

// endReason maps the provider's final status onto the four reasons a caller
// acts on.
func endReason(status string, calls int) EndReason {
	switch {
	case status == "requires_action" || (status == "completed" && calls > 0):
		return EndToolUse
	case status == "completed":
		return EndStop
	case status == "incomplete":
		return EndMaxTokens
	default:
		return EndRefused
	}
}

// errorBody is the provider's error, in either of the two shapes it sends:
// {"message", "code": "too_many_requests"} and
// {"code": 429, "message", "status": "RESOURCE_EXHAUSTED"}.
type errorBody struct {
	Message string          `json:"message"`
	Code    json.RawMessage `json:"code"`
	Status  string          `json:"status"`
}

// failure turns a response that was not a success into a ProviderError.
func (a *interactions) failure(status int, header http.Header, body []byte) *ProviderError {
	// A failed call can still answer in the stream's format.
	text := strings.TrimSpace(string(body))
	if strings.HasPrefix(text, "event:") || strings.HasPrefix(text, "data:") {
		for _, line := range strings.Split(text, "\n") {
			if data, ok := strings.CutPrefix(line, "data:"); ok {
				text = strings.TrimSpace(data)
				break
			}
		}
	}
	var parsed struct {
		Error *errorBody `json:"error"`
	}
	if json.Unmarshal([]byte(text), &parsed) == nil && parsed.Error != nil {
		return a.fromBody(status, header, *parsed.Error)
	}
	out := &ProviderError{Status: status, Code: strconv.Itoa(status), Message: http.StatusText(status)}
	out.RetryAfter, out.HasRetryAfter = retryAfter(header, "")
	return out
}

func (a *interactions) fromBody(status int, header http.Header, body errorBody) *ProviderError {
	out := &ProviderError{Status: status, Message: a.clean(body.Message)}
	var word string
	if json.Unmarshal(body.Code, &word) == nil && word != "" {
		out.Code = word
	} else {
		out.Code = body.Status // the numeric shape names the failure in "status"
	}
	if out.Code == "" {
		out.Code = strconv.Itoa(status)
	}
	out.RetryAfter, out.HasRetryAfter = retryAfter(header, body.Message)
	return out
}

// retryIn finds a wait written into a message, as "Please retry in 36s" and
// "Please retry in 8h39m34s" are.
var retryIn = regexp.MustCompile(`(?i)retry in ((?:\d+(?:\.\d+)?(?:h|m|s|ms))+)`)

// retryAfter reads how long the provider said to wait: from the header when
// it is there, and otherwise from the message, which is the only place the
// per-minute limit gave it.
func retryAfter(header http.Header, message string) (time.Duration, bool) {
	if header != nil {
		if value := strings.TrimSpace(header.Get("Retry-After")); value != "" {
			if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds >= 0 {
				return time.Duration(seconds * float64(time.Second)), true
			}
			if at, err := http.ParseTime(value); err == nil {
				if wait := time.Until(at); wait > 0 {
					return wait, true
				}
				return 0, true
			}
		}
	}
	if m := retryIn.FindStringSubmatch(message); m != nil {
		if wait, err := time.ParseDuration(m[1]); err == nil && wait >= 0 {
			return wait, true
		}
	}
	return 0, false
}

// transportMessage describes a failure to reach the provider, or to finish
// reading from it, without the address or the key.
func transportMessage(ctx context.Context, err error) string {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
		return "the call took longer than it is allowed"
	case errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled):
		return "the call was stopped"
	default:
		return "the connection failed"
	}
}

// clean removes the key from text that came from outside, in case a provider
// or a proxy echoes it.
func (a *interactions) clean(text string) string {
	if a.apiKey != "" {
		text = strings.ReplaceAll(text, a.apiKey, "[key]")
	}
	return keyShaped.ReplaceAllString(text, "[key]")
}
