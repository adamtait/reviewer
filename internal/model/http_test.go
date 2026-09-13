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
	"sync/atomic"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/config"
)

const testKey = "sk-test-ABCDEFGH01234567"

func openAICfg(baseURL string) (config.Config, config.Secrets) {
	cfg := config.Defaults()
	cfg.LaneB.Enabled = true
	cfg.LaneB.Provider = "openai-compatible"
	cfg.LaneB.Model = "a-model"
	cfg.LaneB.BaseURL = baseURL
	return cfg, config.Secrets{ModelAPIKey: testKey}
}

func chatReply(content string) string {
	body, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"message":       map[string]string{"role": "assistant", "content": content},
			"finish_reason": "stop",
		}},
	})
	return string(body)
}

// The exit criterion: no network call when there is no endpoint. A provider that
// silently posted to a compiled-in default would put somebody's diff somewhere
// they never named (ADR-0004).
func TestNoEndpointMeansNoProviderAndNoCall(t *testing.T) {
	cfg, secrets := openAICfg("")
	provider, err := New(cfg, secrets)

	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("want ErrDisabled, got %v", err)
	}
	if provider != nil {
		t.Fatal("a provider was constructed with nowhere to send anything")
	}
	if !strings.Contains(err.Error(), config.EnvModelBaseURL) {
		t.Errorf("want the variable named, got %v", err)
	}
}

func TestNoCredentialMeansNoProvider(t *testing.T) {
	cfg, _ := openAICfg("https://example.invalid/v1")
	if _, err := New(cfg, config.Secrets{}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("want ErrDisabled, got %v", err)
	}
}

func TestOpenAICompatibleRoundTrip(t *testing.T) {
	var got struct {
		path, auth, contentType string
		body                    map[string]any
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.auth = r.Header.Get("Authorization")
		got.contentType = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got.body)
		_, _ = io.WriteString(w, chatReply(`{"findings":[]}`))
	}))
	defer server.Close()

	cfg, secrets := openAICfg(server.URL + "/v1")
	provider, err := New(cfg, secrets)
	if err != nil {
		t.Fatal(err)
	}

	text, err := provider.Complete(context.Background(), []Message{
		{Role: RoleSystem, Content: "be careful"},
		{Role: RoleUser, Content: "the diff"},
	}, Options{Model: "a-model", Temperature: 0, JSON: true, MaxTokens: 2048})
	if err != nil {
		t.Fatal(err)
	}
	if text != `{"findings":[]}` {
		t.Errorf("text = %q", text)
	}

	if got.path != "/v1/chat/completions" {
		t.Errorf("path = %q", got.path)
	}
	if got.auth != "Bearer "+testKey {
		t.Errorf("authorization header = %q", got.auth)
	}
	if got.contentType != "application/json" {
		t.Errorf("content type = %q", got.contentType)
	}
	if got.body["model"] != "a-model" {
		t.Errorf("model = %v", got.body["model"])
	}
	messages, _ := got.body["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages = %v", got.body["messages"])
	}
	if first, _ := messages[0].(map[string]any); first["role"] != "system" || first["content"] != "be careful" {
		t.Errorf("first message = %v", messages[0])
	}
	if got.body["response_format"] == nil {
		t.Error("want JSON requested when the caller asked for it")
	}
}

// A 401 body frequently echoes the key that failed, and an error message reaches
// stderr, the run log, and through a warning a public pull request comment.
func TestA401NeverPrintsTheKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"Incorrect API key provided: `+testKey+`"}}`)
	}))
	defer server.Close()

	cfg, secrets := openAICfg(server.URL)
	provider, err := New(cfg, secrets)
	if err != nil {
		t.Fatal(err)
	}

	_, err = provider.Complete(context.Background(), nil, Options{Model: "a-model"})
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), testKey) {
		t.Fatalf("the key reached the error message: %v", err)
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "[redacted]") {
		t.Errorf("want a usable, redacted message, got %v", err)
	}
}

// Retries cover the two failures that are genuinely transient and nothing else. A
// 400 retried three times is three times the wrong answer.
func TestRetriesOnlyWhatCouldSucceed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		want    int32
		wantErr string
	}{
		{name: "rate limited", status: http.StatusTooManyRequests, want: maxAttempts, wantErr: "gave up"},
		{name: "server error", status: http.StatusBadGateway, want: maxAttempts, wantErr: "gave up"},
		{name: "bad request", status: http.StatusBadRequest, want: 1, wantErr: "400"},
		{name: "unauthorized", status: http.StatusUnauthorized, want: 1, wantErr: "401"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, `{"error":{"message":"no"}}`)
			}))
			defer server.Close()

			cfg, secrets := openAICfg(server.URL)
			provider, err := New(cfg, secrets)
			if err != nil {
				t.Fatal(err)
			}
			_, err = provider.Complete(context.Background(), nil, Options{Model: "a-model", Timeout: 5 * time.Second})

			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.wantErr)
			}
			if got := atomic.LoadInt32(&calls); got != tc.want {
				t.Errorf("made %d requests, want %d", got, tc.want)
			}
		})
	}
}

func TestARetrySucceedsOnTheSecondAttempt(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, chatReply("second time"))
	}))
	defer server.Close()

	cfg, secrets := openAICfg(server.URL)
	provider, _ := New(cfg, secrets)
	text, err := provider.Complete(context.Background(), nil, Options{Model: "a-model"})
	if err != nil || text != "second time" {
		t.Fatalf("got %q %v", text, err)
	}
}

// A filtered reply is a refusal: there is nothing to retry, and the caller reports
// it as a warning rather than a failure.
func TestAFilteredReplyIsARefusal(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		body, _ := json.Marshal(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]string{"content": ""}, "finish_reason": "content_filter",
			}},
		})
		_, _ = w.Write(body)
	}))
	defer server.Close()

	cfg, secrets := openAICfg(server.URL)
	provider, _ := New(cfg, secrets)
	_, err := provider.Complete(context.Background(), nil, Options{Model: "a-model"})

	if !errors.Is(err, ErrRefused) {
		t.Fatalf("want ErrRefused, got %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Error("a refusal was retried; the same prompt gets the same answer")
	}
}

// A 200 carrying an error object is a real shape for gateways in this family, and
// reading it as an empty reply would lose the reason.
func TestAnErrorObjectInA200IsReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"error":{"message":"model not found","type":"invalid_request_error"}}`)
	}))
	defer server.Close()

	cfg, secrets := openAICfg(server.URL)
	provider, _ := New(cfg, secrets)
	_, err := provider.Complete(context.Background(), nil, Options{Model: "a-model"})

	if err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("err = %v", err)
	}
}

// A truncated reply is a budget problem, not a model problem, and saying which is
// the difference between a fix and a shrug.
func TestATruncatedReplyNamesTheTokenLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := json.Marshal(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]string{"content": ""}, "finish_reason": "length",
			}},
		})
		_, _ = w.Write(body)
	}))
	defer server.Close()

	cfg, secrets := openAICfg(server.URL)
	provider, _ := New(cfg, secrets)
	_, err := provider.Complete(context.Background(), nil, Options{Model: "a-model"})

	if err == nil || !strings.Contains(err.Error(), "token limit") {
		t.Fatalf("err = %v", err)
	}
}

// The environment overrides the file, so one machine can point elsewhere without
// editing a tracked file.
func TestTheEnvironmentOverridesTheConfiguredEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, chatReply("from the override"))
	}))
	defer server.Close()

	cfg, secrets := openAICfg("https://never.invalid/v1")
	secrets.ModelBaseURL = server.URL
	provider, err := New(cfg, secrets)
	if err != nil {
		t.Fatal(err)
	}
	if text, err := provider.Complete(context.Background(), nil, Options{Model: "a-model"}); err != nil || text != "from the override" {
		t.Fatalf("got %q %v", text, err)
	}
}

func TestAHungProviderIsBoundedByTheCallersTimeout(t *testing.T) {
	// A bounded sleep rather than a wait on the request context: a client that
	// gives up does not reliably cancel the server's context, and httptest's Close
	// blocks on any handler still running — so the obvious version of this test
	// hangs the suite rather than the code under test.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer server.Close()

	cfg, secrets := openAICfg(server.URL)
	provider, _ := New(cfg, secrets)

	start := time.Now()
	_, err := provider.Complete(context.Background(), nil, Options{Model: "a-model", Timeout: 150 * time.Millisecond})
	if err == nil {
		t.Fatal("want a timeout")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("the call was not bounded by the caller's timeout: %v", elapsed)
	}
}

func TestTheHTTPProviderSatisfiesTheInterface(t *testing.T) {
	var _ Provider = (*httpProvider)(nil)
}

// A gateway routinely carries its token in the path. A transport error prints the
// whole URL, which is the one place that token appears verbatim.
func TestTheEndpointIsRedactedFromErrors(t *testing.T) {
	cfg, secrets := openAICfg("http://127.0.0.1:1/proxy/tok_HUNTER2SUPERSECRET/v1")
	provider, err := New(cfg, secrets)
	if err != nil {
		t.Fatal(err)
	}

	_, err = provider.Complete(context.Background(), nil,
		Options{Model: "a-model", Timeout: 2 * time.Second})
	if err == nil {
		t.Fatal("want a transport error")
	}
	if strings.Contains(err.Error(), "tok_HUNTER2SUPERSECRET") {
		t.Fatalf("the endpoint's credential reached the error: %v", err)
	}
}

// Redacted before truncated. The other order cuts the body at 400 bytes and then
// looks for a key, so a credential straddling the boundary survives as a prefix.
func TestALongBodyIsRedactedBeforeItIsTruncated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		// The key sits either side of the 400-byte cut.
		_, _ = io.WriteString(w, strings.Repeat("x", 390)+testKey+strings.Repeat("y", 100))
	}))
	defer server.Close()

	cfg, secrets := openAICfg(server.URL)
	provider, _ := New(cfg, secrets)
	_, err := provider.Complete(context.Background(), nil, Options{Model: "a-model"})

	if err == nil {
		t.Fatal("want an error")
	}
	for _, fragment := range []string{testKey, testKey[:12]} {
		if strings.Contains(err.Error(), fragment) {
			t.Fatalf("%q survived into %v", fragment, err)
		}
	}
}
