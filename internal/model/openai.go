// SPDX-License-Identifier: MIT

package model

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// openAIShape is the chat-completions request, which is the closest thing this
// ecosystem has to a lingua franca.
//
// It serves two configured paths. `openai` and `openai-compatible` differ only in
// which endpoint the destination wrote at install time, and pretending otherwise
// would mean two adapters that have to be kept identical.
type openAIShape struct{}

func (openAIShape) path(string) string { return "/chat/completions" }

func (openAIShape) header(h http.Header, apiKey string) {
	h.Set("Authorization", "Bearer "+apiKey)
}

func (openAIShape) body(messages []Message, opts Options) any {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	body := struct {
		Model          string    `json:"model"`
		Messages       []message `json:"messages"`
		Temperature    float64   `json:"temperature"`
		MaxTokens      int       `json:"max_completion_tokens,omitempty"`
		ResponseFormat any       `json:"response_format,omitempty"`
	}{Model: opts.Model, Temperature: opts.Temperature, MaxTokens: opts.MaxTokens}

	for _, m := range messages {
		body.Messages = append(body.Messages, message{Role: string(m.Role), Content: m.Content})
	}
	if opts.JSON {
		// Requested, not relied on. A compatible endpoint may ignore it, and an
		// enforced object can still be semantically wrong, so the caller validates
		// the reply either way.
		body.ResponseFormat = map[string]string{"type": "json_object"}
	}
	return body
}

func (openAIShape) text(raw []byte) (string, error) {
	var reply struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return "", fmt.Errorf("the reply was not the expected shape: %w", err)
	}
	// A 200 carrying an error object is a real shape for gateways in this family.
	if reply.Error.Message != "" {
		return "", fmt.Errorf("the provider reported: %s", reply.Error.Message)
	}
	if len(reply.Choices) == 0 {
		return "", fmt.Errorf("the reply contained no choices")
	}

	choice := reply.Choices[0]
	if strings.EqualFold(choice.FinishReason, "content_filter") {
		return "", fmt.Errorf("%w: the reply was filtered", ErrRefused)
	}
	if strings.TrimSpace(choice.Message.Content) == "" {
		// An empty reply with `length` is a truncated one, which is worth naming:
		// it is a budget problem and not a model problem.
		if strings.EqualFold(choice.FinishReason, "length") {
			return "", fmt.Errorf("the reply hit the token limit before saying anything")
		}
		return "", fmt.Errorf("the reply was empty")
	}
	return choice.Message.Content, nil
}
