// SPDX-License-Identifier: MIT

package model

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// anthropicVersion is the API version header this adapter is written against.
//
// Pinned rather than omitted: the header is required, and a version that drifts
// with the server would change the reply shape underneath a parser written for a
// different one. When it needs to move, it moves here, deliberately.
const anthropicVersion = "2023-06-01"

// anthropicShape is the messages request.
//
// Written over net/http rather than through the vendor SDK, which is a deviation
// from the plan. The request is one JSON POST; the SDK is a transitive dependency
// tree that THIRD_PARTY_LICENSES.md must inventory and tools/checklicenses must
// reconcile, in a project whose entire non-test dependency list is one YAML
// parser. The cost is that the wire format is hand-written and has to be kept
// right — which is what the tests in this package are for.
type anthropicShape struct{}

func (anthropicShape) path(string) string { return "/messages" }

func (anthropicShape) header(h http.Header, apiKey string) {
	// Not a bearer token: this API takes the key in its own header.
	h.Set("x-api-key", apiKey)
	h.Set("anthropic-version", anthropicVersion)
}

func (anthropicShape) body(messages []Message, opts Options) any {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	body := struct {
		Model       string    `json:"model"`
		System      string    `json:"system,omitempty"`
		Messages    []message `json:"messages"`
		Temperature float64   `json:"temperature"`
		MaxTokens   int       `json:"max_tokens"`
	}{Model: opts.Model, Temperature: opts.Temperature, MaxTokens: opts.MaxTokens}

	// The system prompt is a top-level field here rather than a turn, which is the
	// one structural difference from the chat-completions shape. Folding it into
	// the messages instead would be accepted and would change how it is weighted.
	var system []string
	for _, m := range messages {
		if m.Role == RoleSystem {
			system = append(system, m.Content)
			continue
		}
		body.Messages = append(body.Messages, message{Role: string(m.Role), Content: m.Content})
	}
	body.System = strings.Join(system, "\n\n")

	if body.MaxTokens == 0 {
		// Required by this API, unlike the others, so a caller that did not care
		// still needs a number. Large enough for a long review and far below any
		// model's ceiling.
		body.MaxTokens = 8192
	}
	if len(body.Messages) == 0 {
		// Also required: a request with only a system prompt is rejected outright.
		body.Messages = []message{{Role: string(RoleUser), Content: "Proceed."}}
	}
	return body
}

func (anthropicShape) text(raw []byte) (string, error) {
	var reply struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Error      struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return "", fmt.Errorf("the reply was not the expected shape: %w", err)
	}
	if reply.Error.Message != "" {
		return "", fmt.Errorf("the provider reported: %s", reply.Error.Message)
	}

	// Content is a list of blocks. Only the text ones are of interest, and
	// concatenating them is what the API's own clients do.
	var text strings.Builder
	for _, block := range reply.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		if reply.StopReason == "max_tokens" {
			return "", fmt.Errorf("the reply hit the token limit before saying anything")
		}
		if reply.StopReason == "refusal" {
			return "", fmt.Errorf("%w: the model declined", ErrRefused)
		}
		return "", fmt.Errorf("the reply was empty")
	}
	return text.String(), nil
}
