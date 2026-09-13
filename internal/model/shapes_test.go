// SPDX-License-Identifier: MIT

package model

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/internal/config"
)

// A conformance suite over all three HTTP paths.
//
// The point is equivalence: a caller cannot tell which provider is behind the
// interface, so the behaviours that matter must be identical even though the wire
// formats are not. Three adapters that each handle a refusal slightly differently
// is three different things a reviewer sees for the same event.
func TestEveryHTTPProviderBehavesTheSame(t *testing.T) {
	for _, p := range []struct {
		provider string
		// ok is a successful reply carrying the given text.
		ok func(text string) string
		// refusal is whatever this provider calls declining to answer.
		refusal string
		// truncated is an empty reply that ran out of tokens.
		truncated string
		// errorIn200 is an error object inside a 200.
		errorIn200 string
		// keyHeader is where this provider expects the credential.
		keyHeader string
		// path is where the request lands, for the configured model "a-model".
		path string
	}{
		{
			provider: "openai-compatible",
			ok: func(text string) string {
				body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
					"message": map[string]string{"content": text}, "finish_reason": "stop"}}})
				return string(body)
			},
			refusal:    `{"choices":[{"message":{"content":""},"finish_reason":"content_filter"}]}`,
			truncated:  `{"choices":[{"message":{"content":""},"finish_reason":"length"}]}`,
			errorIn200: `{"error":{"message":"model not found"}}`,
			keyHeader:  "Authorization",
			path:       "/chat/completions",
		},
		{
			provider: "anthropic",
			ok: func(text string) string {
				body, _ := json.Marshal(map[string]any{
					"content":     []any{map[string]string{"type": "text", "text": text}},
					"stop_reason": "end_turn"})
				return string(body)
			},
			refusal:    `{"content":[],"stop_reason":"refusal"}`,
			truncated:  `{"content":[],"stop_reason":"max_tokens"}`,
			errorIn200: `{"error":{"type":"not_found_error","message":"model not found"}}`,
			keyHeader:  "X-Api-Key",
			path:       "/messages",
		},
		{
			provider: "gemini",
			ok: func(text string) string {
				body, _ := json.Marshal(map[string]any{"candidates": []any{map[string]any{
					"content":      map[string]any{"parts": []any{map[string]string{"text": text}}},
					"finishReason": "STOP"}}})
				return string(body)
			},
			refusal:    `{"promptFeedback":{"blockReason":"SAFETY"}}`,
			truncated:  `{"candidates":[{"content":{"parts":[]},"finishReason":"MAX_TOKENS"}]}`,
			errorIn200: `{"error":{"message":"model not found","status":"NOT_FOUND"}}`,
			keyHeader:  "X-Goog-Api-Key",
			path:       "/models/a-model:generateContent",
		},
	} {
		t.Run(p.provider, func(t *testing.T) {
			serve := func(body string) (*httptest.Server, *http.Request, *[]byte) {
				var seen *http.Request
				var payload []byte
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					clone := *r
					seen = &clone
					payload, _ = io.ReadAll(r.Body)
					_, _ = io.WriteString(w, body)
				}))
				return s, seen, &payload
			}

			t.Run("a successful reply", func(t *testing.T) {
				server, _, payload := serve(p.ok("the answer"))
				defer server.Close()
				provider := build(t, p.provider, server.URL)

				text, err := provider.Complete(context.Background(), []Message{
					{Role: RoleSystem, Content: "be careful"},
					{Role: RoleUser, Content: "the diff"},
				}, Options{Model: "a-model", JSON: true, MaxTokens: 4096})
				if err != nil || text != "the answer" {
					t.Fatalf("got %q %v", text, err)
				}

				// Both turns have to reach the provider, whatever it calls them.
				sent := string(*payload)
				for _, want := range []string{"be careful", "the diff"} {
					if !strings.Contains(sent, want) {
						t.Errorf("the request does not carry %q: %s", want, sent)
					}
				}
			})

			t.Run("the credential goes in this provider's own header", func(t *testing.T) {
				var header, path string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					header = r.Header.Get(p.keyHeader)
					path = r.URL.Path
					// The credential must never reach a URL: it would be written
					// into every proxy log between here and there.
					if strings.Contains(r.URL.RawQuery, testKey) {
						t.Errorf("the key is in the query string: %s", r.URL.RawQuery)
					}
					_, _ = io.WriteString(w, p.ok("x"))
				}))
				defer server.Close()

				if _, err := build(t, p.provider, server.URL).Complete(
					context.Background(), nil, Options{Model: "a-model"}); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(header, testKey) {
					t.Errorf("%s = %q, want the key", p.keyHeader, header)
				}
				if path != p.path {
					t.Errorf("path = %q, want %q", path, p.path)
				}
			})

			t.Run("a refusal is ErrRefused, not an empty reply", func(t *testing.T) {
				server, _, _ := serve(p.refusal)
				defer server.Close()

				_, err := build(t, p.provider, server.URL).Complete(
					context.Background(), nil, Options{Model: "a-model"})
				if !errors.Is(err, ErrRefused) {
					t.Fatalf("want ErrRefused, got %v", err)
				}
			})

			t.Run("a truncated reply names the token limit", func(t *testing.T) {
				server, _, _ := serve(p.truncated)
				defer server.Close()

				_, err := build(t, p.provider, server.URL).Complete(
					context.Background(), nil, Options{Model: "a-model"})
				if err == nil || !strings.Contains(err.Error(), "token limit") {
					t.Fatalf("err = %v", err)
				}
			})

			t.Run("an error object inside a 200 is reported", func(t *testing.T) {
				server, _, _ := serve(p.errorIn200)
				defer server.Close()

				_, err := build(t, p.provider, server.URL).Complete(
					context.Background(), nil, Options{Model: "a-model"})
				if err == nil || !strings.Contains(err.Error(), "model not found") {
					t.Fatalf("err = %v", err)
				}
			})

			t.Run("a 401 never prints the key", func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = io.WriteString(w, `{"error":{"message":"bad key: `+testKey+`"}}`)
				}))
				defer server.Close()

				_, err := build(t, p.provider, server.URL).Complete(
					context.Background(), nil, Options{Model: "a-model"})
				if err == nil || strings.Contains(err.Error(), testKey) {
					t.Fatalf("the key survived into %v", err)
				}
			})
		})
	}
}

// A model name is configuration, and Gemini puts it in the URL. A value with a
// slash would otherwise reach a different endpoint entirely.
func TestGeminiEscapesTheModelInThePath(t *testing.T) {
	if got := (geminiShape{}).path("../../v1/other"); strings.Contains(got, "../") {
		t.Errorf("path = %q, want the separators escaped", got)
	}
}

// This API rejects a request with only a system prompt, and requires max_tokens.
// A caller that set neither must still get a valid request rather than a 400.
func TestAnthropicFillsInWhatItsAPIRequires(t *testing.T) {
	raw, err := json.Marshal((anthropicShape{}).body(
		[]Message{{Role: RoleSystem, Content: "only a system prompt"}}, Options{Model: "a-model"}))
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		System    string `json:"system"`
		MaxTokens int    `json:"max_tokens"`
		Messages  []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.System != "only a system prompt" {
		t.Errorf("the system prompt became something else: %q", body.System)
	}
	if body.MaxTokens == 0 {
		t.Error("max_tokens is required by this API and was not set")
	}
	if len(body.Messages) != 1 || body.Messages[0].Role != "user" {
		t.Errorf("messages = %+v; this API rejects a request with no turns", body.Messages)
	}
}

// The system prompt is a top-level field on two of the three, and folding it into
// the turns instead would be accepted and would change how it is weighted.
func TestTheSystemPromptStaysASystemPrompt(t *testing.T) {
	messages := []Message{{Role: RoleSystem, Content: "SYSTEM"}, {Role: RoleUser, Content: "USER"}}

	anthropic, _ := json.Marshal((anthropicShape{}).body(messages, Options{Model: "m"}))
	if !strings.Contains(string(anthropic), `"system":"SYSTEM"`) {
		t.Errorf("anthropic: %s", anthropic)
	}
	gemini, _ := json.Marshal((geminiShape{}).body(messages, Options{Model: "m"}))
	if !strings.Contains(string(gemini), `"systemInstruction"`) {
		t.Errorf("gemini: %s", gemini)
	}
	if strings.Count(string(gemini), "SYSTEM") != 1 {
		t.Errorf("gemini sent the system prompt twice: %s", gemini)
	}
}

func build(t *testing.T, provider, baseURL string) Provider {
	t.Helper()
	cfg := config.Defaults()
	cfg.LaneB.Enabled = true
	cfg.LaneB.Provider = provider
	cfg.LaneB.Model = "a-model"
	cfg.LaneB.BaseURL = baseURL

	p, err := New(cfg, config.Secrets{ModelAPIKey: testKey})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
