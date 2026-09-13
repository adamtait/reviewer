// SPDX-License-Identifier: MIT

package model

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// geminiShape is the generateContent request.
//
// Over net/http rather than through the vendor SDK, for the reasons on
// anthropicShape. This one differs from the other two in three ways that matter:
// the model name is in the path rather than the body, the system prompt has its
// own field with a different spelling again, and a blocked reply comes back as a
// 200 with no candidates and a reason beside them.
type geminiShape struct{}

func (geminiShape) path(model string) string {
	// The model is part of the URL. Escaped, because it comes from configuration
	// and a value with a slash in it would otherwise reach a different endpoint.
	return "/models/" + strings.ReplaceAll(model, "/", "%2F") + ":generateContent"
}

func (geminiShape) header(h http.Header, apiKey string) {
	// In a header, not the query string. The same key in a URL would be written
	// into every proxy log between here and there, and into this process's own
	// error messages.
	h.Set("x-goog-api-key", apiKey)
}

func (geminiShape) body(messages []Message, opts Options) any {
	type part struct {
		Text string `json:"text"`
	}
	type content struct {
		Role  string `json:"role,omitempty"`
		Parts []part `json:"parts"`
	}
	body := struct {
		Contents          []content `json:"contents"`
		SystemInstruction *content  `json:"systemInstruction,omitempty"`
		GenerationConfig  struct {
			Temperature      float64 `json:"temperature"`
			MaxOutputTokens  int     `json:"maxOutputTokens,omitempty"`
			ResponseMimeType string  `json:"responseMimeType,omitempty"`
		} `json:"generationConfig"`
	}{}
	body.GenerationConfig.Temperature = opts.Temperature
	body.GenerationConfig.MaxOutputTokens = opts.MaxTokens
	if opts.JSON {
		body.GenerationConfig.ResponseMimeType = "application/json"
	}

	var system []string
	for _, m := range messages {
		if m.Role == RoleSystem {
			system = append(system, m.Content)
			continue
		}
		// "user" and "model" are the role names here; the system prompt is not a
		// turn at all.
		body.Contents = append(body.Contents, content{Role: "user", Parts: []part{{Text: m.Content}}})
	}
	if len(system) > 0 {
		body.SystemInstruction = &content{Parts: []part{{Text: strings.Join(system, "\n\n")}}}
	}
	if len(body.Contents) == 0 {
		body.Contents = []content{{Role: "user", Parts: []part{{Text: "Proceed."}}}}
	}
	return body
}

func (geminiShape) text(raw []byte) (string, error) {
	var reply struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		PromptFeedback struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return "", fmt.Errorf("the reply was not the expected shape: %w", err)
	}
	if reply.Error.Message != "" {
		return "", fmt.Errorf("the provider reported: %s", reply.Error.Message)
	}

	// A blocked prompt is a 200 with no candidates. Reading that as an empty reply
	// would report "the model said nothing" for a refusal, which sends the reader
	// looking for a bug that is not there.
	if reason := reply.PromptFeedback.BlockReason; reason != "" {
		return "", fmt.Errorf("%w: the prompt was blocked (%s)", ErrRefused, reason)
	}
	if len(reply.Candidates) == 0 {
		return "", fmt.Errorf("the reply contained no candidates")
	}

	candidate := reply.Candidates[0]
	switch candidate.FinishReason {
	case "SAFETY", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
		return "", fmt.Errorf("%w: the reply was blocked (%s)", ErrRefused, candidate.FinishReason)
	case "MAX_TOKENS":
		if len(candidate.Content.Parts) == 0 {
			return "", fmt.Errorf("the reply hit the token limit before saying anything")
		}
	}

	var text strings.Builder
	for _, p := range candidate.Content.Parts {
		text.WriteString(p.Text)
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", fmt.Errorf("the reply was empty")
	}
	return text.String(), nil
}
