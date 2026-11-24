// SPDX-License-Identifier: MIT

package model

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The conversation key used in these tests. Built at run time, so no file
// holds a string in the shape of a real one.
var conversationKey = "test-" + strings.Repeat("k", 24)

func recorded(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "interactions", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// serving returns a Converser whose provider answers each call with the next
// reply, and the bodies it was sent.
type reply struct {
	status int
	header map[string]string
	body   []byte
}

func serving(t *testing.T, replies ...reply) (Converser, *[]map[string]any, *[]http.Header) {
	t.Helper()
	var bodies []map[string]any
	var headers []http.Header
	next := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/interactions" || r.Method != http.MethodPost {
			http.Error(w, "wrong address: "+r.Method+" "+r.URL.Path, http.StatusTeapot)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)
		headers = append(headers, r.Header.Clone())
		if next >= len(replies) {
			http.Error(w, "no reply left", http.StatusTeapot)
			return
		}
		rep := replies[next]
		next++
		for k, v := range rep.header {
			w.Header().Set(k, v)
		}
		if rep.status == 0 {
			rep.status = http.StatusOK
		}
		w.WriteHeader(rep.status)
		_, _ = w.Write(rep.body)
	}))
	t.Cleanup(srv.Close)
	conv, err := NewConverser(ProviderGemini, srv.URL+"/v1beta/", conversationKey)
	if err != nil {
		t.Fatal(err)
	}
	return conv, &bodies, &headers
}

// collect runs one call and returns what it reported.
type outcome struct {
	text  string
	parts []Part
	usage Usage
	end   End
	ended int
}

func collect(t *testing.T, conv Converser, req ConvRequest) (outcome, error) {
	t.Helper()
	var out outcome
	err := conv.Converse(context.Background(), req, func(ev Event) {
		switch e := ev.(type) {
		case TextDelta:
			out.text += e.Text
		case PartDone:
			out.parts = append(out.parts, e.Part)
		case Usage:
			out.usage = e
		case End:
			out.end = e
			out.ended++
		}
	})
	return out, err
}

func decodeJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The whole of a recorded conversation, replayed: a question, two tool calls
// and an answer. At each step the adapter must send what the recording script
// sent, which the live service accepted, and must report what the service
// answered.
func TestARecordedConversationIsReplayedExactly(t *testing.T) {
	requests := []map[string]any{
		decodeJSON(t, recorded(t, "tool_call.request.json")),
		decodeJSON(t, recorded(t, "second_tool_call.request.json")),
		decodeJSON(t, recorded(t, "answer.request.json")),
	}
	conv, sent, headers := serving(t,
		reply{body: recorded(t, "tool_call.sse")},
		reply{body: recorded(t, "second_tool_call.sse")},
		reply{body: recorded(t, "answer.sse")},
	)

	// The question, the instructions and the tools, from the first recorded request.
	first := requests[0]
	req := ConvRequest{Model: first["model"].(string), System: first["system_instruction"].(string)}
	for _, raw := range first["tools"].([]any) {
		tool := raw.(map[string]any)
		schema, _ := json.Marshal(tool["parameters"])
		req.Tools = append(req.Tools, ToolSpec{Name: tool["name"].(string), Description: tool["description"].(string), SchemaJSON: string(schema)})
	}
	question := first["input"].([]any)[0].(map[string]any)["content"].(string)
	req.Turns = []Turn{{Role: RoleUser, Parts: []Part{Text{Text: question}}}}

	// What each tool returned, from the request that carried it back.
	results := map[string]string{}
	for _, request := range requests {
		for _, raw := range request["input"].([]any) {
			if s := raw.(map[string]any); s["type"] == "function_result" {
				results[s["call_id"].(string)] = s["result"].([]any)[0].(map[string]any)["text"].(string)
			}
		}
	}

	var answer outcome
	for round := 0; round < 3; round++ {
		got, err := collect(t, conv, req)
		if err != nil {
			t.Fatalf("call %d: %v", round+1, err)
		}
		if got.ended != 1 {
			t.Fatalf("call %d reported %d endings, want exactly one", round+1, got.ended)
		}
		if !reflect.DeepEqual((*sent)[round], requests[round]) {
			a, _ := json.MarshalIndent((*sent)[round], "", " ")
			b, _ := json.MarshalIndent(requests[round], "", " ")
			t.Fatalf("call %d: the adapter sent\n%s\nand the live service was sent\n%s", round+1, a, b)
		}
		if got.end.Reason != EndToolUse {
			answer = got
			break
		}
		// Continue the loop as a caller does: the assistant's parts in order,
		// then the results.
		var back []Part
		for _, part := range got.parts {
			if call, ok := part.(ToolCall); ok {
				result, known := results[call.ID]
				if !known {
					t.Fatalf("call %d asked for a tool call the recording does not hold: %+v", round+1, call)
				}
				back = append(back, ToolResult{CallID: call.ID, Name: call.Name, Text: result})
			}
		}
		req.Turns = append(req.Turns, Turn{Role: RoleAssistant, Parts: got.parts}, Turn{Role: RoleUser, Parts: back})
	}

	if answer.end.Reason != EndStop || answer.end.Detail != "completed" {
		t.Errorf("the conversation ended with %+v, want stop", answer.end)
	}
	if !strings.HasPrefix(answer.text, "`parseIntent` is defined in **`src/intent.ts`**.") || !strings.HasSuffix(answer.text, "returns **`null`**.") {
		t.Errorf("answer = %q", answer.text)
	}
	if answer.usage != (Usage{InputTokens: 530, OutputTokens: 60, ReasoningTokens: 173}) {
		t.Errorf("usage = %+v", answer.usage)
	}
	// The answering call still carried reasoning data, which a caller drops.
	if len(answer.parts) != 1 {
		t.Errorf("the answering call reported %d parts, want its reasoning data alone", len(answer.parts))
	}
	for _, h := range *headers {
		if h.Get("x-goog-api-key") != conversationKey {
			t.Error("the key was not sent in the provider's header")
		}
		if h.Get("Authorization") != "" {
			t.Error("the key was also sent as a bearer token")
		}
	}
}

func TestAToolCallIsReportedWithItsReasoningData(t *testing.T) {
	conv, sent, _ := serving(t, reply{body: recorded(t, "tool_call.sse")})
	got, err := collect(t, conv, ConvRequest{Model: "any-model-name", Turns: []Turn{{Role: RoleUser, Parts: []Part{Text{Text: "q"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.parts) != 2 {
		t.Fatalf("parts = %+v", got.parts)
	}
	// In the order received: the reasoning data, then the call.
	opaque, isOpaque := got.parts[0].(Opaque)
	call, isCall := got.parts[1].(ToolCall)
	if !isOpaque || !isCall {
		t.Fatalf("parts = %T, %T; want Opaque then ToolCall", got.parts[0], got.parts[1])
	}
	var thought map[string]string
	if err := json.Unmarshal(opaque.Data, &thought); err != nil || thought["type"] != "thought" || len(thought["signature"]) != 532 {
		t.Errorf("reasoning data = %s", opaque.Data)
	}
	if call.Name != "search_text" || call.ArgumentsJSON != `{"query":"parseIntent"}` || !strings.HasPrefix(call.ID, "call_") {
		t.Errorf("call = %+v", call)
	}
	if got.usage != (Usage{InputTokens: 162, OutputTokens: 17, ReasoningTokens: 44}) || got.end.Reason != EndToolUse || got.text != "" {
		t.Errorf("usage %+v, end %+v, text %q", got.usage, got.end, got.text)
	}

	// The model name is sent exactly as configured, and nothing is stored.
	body := (*sent)[0]
	if body["model"] != "any-model-name" || body["store"] != false || body["stream"] != true {
		t.Errorf("request = %v", body)
	}
	if _, has := body["generation_config"]; has {
		t.Error("an output limit was sent although none was asked for")
	}
}

func TestEachObservedFailureIsReadIntoAProviderError(t *testing.T) {
	for name, tc := range map[string]struct {
		file    string
		status  int
		header  map[string]string
		code    string
		wait    time.Duration
		hasWait bool
		message string
	}{
		"a per-minute limit, with the wait only in the message": {
			file: "rate_limit.json", status: 429, code: "too_many_requests", wait: 36 * time.Second, hasWait: true, message: "5 requests per minute"},
		"an exhausted daily quota, with a retry-after header": {
			file: "quota.json", status: 429, header: map[string]string{"Retry-After": "31174"},
			code: "too_many_requests", wait: 31174 * time.Second, hasWait: true, message: "20 requests per day"},
		"the same quota read from its message alone": {
			file: "quota.json", status: 429, code: "too_many_requests", wait: 8*time.Hour + 39*time.Minute + 34*time.Second, hasWait: true},
		"heavy demand":                    {file: "overloaded.json", status: 503, code: "service_unavailable", message: "high demand"},
		"heavy demand in the other shape": {file: "overloaded_older_shape.json", status: 503, code: "UNAVAILABLE", message: "high demand"},
		"reasoning data left out, answered as a stream event": {
			file: "missing_reasoning.sse", status: 400, code: "invalid_request", message: "invalid argument"},
		"a key that is not valid":     {file: "bad_key.json", status: 401, code: "UNAUTHENTICATED", message: "API key not valid"},
		"a model that does not exist": {file: "unknown_model.json", status: 404, code: "not_found", message: "not found"},
	} {
		conv, _, _ := serving(t, reply{status: tc.status, header: tc.header, body: recorded(t, tc.file)})
		got, err := collect(t, conv, ConvRequest{Model: "m"})
		var failed *ProviderError
		if !errors.As(err, &failed) {
			t.Errorf("%s: err = %v, want a *ProviderError", name, err)
			continue
		}
		if failed.Status != tc.status || failed.Code != tc.code || failed.HasRetryAfter != tc.hasWait || failed.RetryAfter != tc.wait {
			t.Errorf("%s: got status %d, code %q, wait %v (%v); want %d, %q, %v (%v)",
				name, failed.Status, failed.Code, failed.RetryAfter, failed.HasRetryAfter, tc.status, tc.code, tc.wait, tc.hasWait)
		}
		if !strings.Contains(failed.Message, tc.message) {
			t.Errorf("%s: message %q does not contain %q", name, failed.Message, tc.message)
		}
		if got.ended != 0 || got.text != "" {
			t.Errorf("%s: a failed call reported events: %+v", name, got)
		}
	}
}

func TestAFailureWithNoReadableBodyStillSaysWhatHappened(t *testing.T) {
	conv, _, _ := serving(t, reply{status: 502, header: map[string]string{"Retry-After": "7"}, body: []byte("<html>Bad Gateway</html>")})
	_, err := collect(t, conv, ConvRequest{Model: "m"})
	var failed *ProviderError
	if !errors.As(err, &failed) || failed.Status != 502 || failed.Code != "502" || failed.RetryAfter != 7*time.Second {
		t.Errorf("err = %+v", failed)
	}
	if strings.Contains(failed.Message, "html") {
		t.Errorf("an unreadable body was passed on as the message: %q", failed.Message)
	}
}

func TestTheKeyAppearsInNoError(t *testing.T) {
	echo := `{"error":{"message":"bad request with key ` + conversationKey + ` and Bearer abcdefghijklmnopqrstuvwx","code":"invalid_request"}}`
	conv, _, _ := serving(t, reply{status: 400, body: []byte(echo)})
	_, err := collect(t, conv, ConvRequest{Model: "m"})
	if err == nil || strings.Contains(err.Error(), conversationKey) || strings.Contains(err.Error(), "abcdefghijklmnop") {
		t.Errorf("the key reached an error: %v", err)
	}

	// A provider that cannot be reached: the error holds neither address nor key.
	conv, err = NewConverser(ProviderGemini, "http://127.0.0.1:1/v1beta", conversationKey)
	if err != nil {
		t.Fatal(err)
	}
	_, err = collect(t, conv, ConvRequest{Model: "m"})
	var failed *ProviderError
	if !errors.As(err, &failed) || failed.Status != 0 {
		t.Fatalf("err = %v, want a ProviderError with status 0", err)
	}
	if strings.Contains(err.Error(), "127.0.0.1") || strings.Contains(err.Error(), conversationKey) {
		t.Errorf("an unreachable provider's error names the address or the key: %v", err)
	}
}

func TestAStreamThatGoesWrongIsAFailureNotAnAnswer(t *testing.T) {
	whole := string(recorded(t, "answer.sse"))
	cut := whole[:strings.Index(whole, "event: interaction.completed")]
	for name, tc := range map[string]struct {
		body string
		code string
	}{
		"cut off before the end":    {cut, "incomplete_stream"},
		"an event that is not JSON": {"event: step.start\ndata: {not json\n\n", "unreadable_stream"},
		"an error event after a good start": {
			cut + "event: error\ndata: {\"error\":{\"message\":\"internal failure\",\"code\":\"internal\"},\"event_type\":\"error\"}\n\n", "internal"},
		"nothing at all": {"", "incomplete_stream"},
	} {
		conv, _, _ := serving(t, reply{body: []byte(tc.body)})
		got, err := collect(t, conv, ConvRequest{Model: "m"})
		var failed *ProviderError
		if !errors.As(err, &failed) || failed.Code != tc.code {
			t.Errorf("%s: err = %v, want code %q", name, err, tc.code)
		}
		if got.ended != 0 {
			t.Errorf("%s: the call was reported as ended although it failed", name)
		}
	}
}

func TestACallIsStoppedAtItsTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	conv, _ := NewConverser(ProviderGemini, srv.URL, conversationKey)

	started := time.Now()
	_, err := collect(t, conv, ConvRequest{Model: "m", Timeout: 80 * time.Millisecond})
	var failed *ProviderError
	if !errors.As(err, &failed) || failed.Status != 0 || !strings.Contains(failed.Message, "longer than it is allowed") {
		t.Errorf("err = %v", err)
	}
	if took := time.Since(started); took > 3*time.Second {
		t.Errorf("the call ran for %v past a timeout of 80ms", took)
	}
}

func TestHowAConversationIsWritten(t *testing.T) {
	conv, sent, _ := serving(t, reply{body: recorded(t, "answer.sse")})
	_, err := collect(t, conv, ConvRequest{
		Model: "m", System: "instructions", MaxOutputTokens: 4096,
		Turns: []Turn{
			// An earlier question and answer, as plain text.
			{Role: RoleUser, Parts: []Part{Text{Text: "an earlier question"}}},
			{Role: RoleAssistant, Parts: []Part{Text{Text: "an earlier answer"}}},
			{Role: RoleUser, Parts: []Part{Text{Text: "this question"}}},
			{Role: RoleAssistant, Parts: []Part{
				Opaque{Data: json.RawMessage(`{"type":"thought","signature":"abc","future_field":[1,2]}`)},
				ToolCall{ID: "call_1", Name: "read_file", ArgumentsJSON: `{"path":"a.ts"}`},
				ToolCall{ID: "call_2", Name: "read_file", ArgumentsJSON: `not json`},
				Opaque{Data: json.RawMessage(`broken`)},
			}},
			{Role: RoleUser, Parts: []Part{
				ToolResult{CallID: "call_1", Name: "read_file", Text: "the file"},
				ToolResult{CallID: "call_2", Name: "read_file", Text: "no such file", IsError: true},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := (*sent)[0]
	want := []any{
		map[string]any{"type": "user_input", "content": "an earlier question"},
		map[string]any{"type": "model_output", "content": []any{map[string]any{"type": "text", "text": "an earlier answer"}}},
		map[string]any{"type": "user_input", "content": "this question"},
		// Reasoning data goes back exactly as it came, fields this code has never heard of included.
		map[string]any{"type": "thought", "signature": "abc", "future_field": []any{1.0, 2.0}},
		map[string]any{"type": "function_call", "id": "call_1", "name": "read_file", "arguments": map[string]any{"path": "a.ts"}},
		// Arguments that are not JSON are sent as an empty object, so one bad call does not make the request invalid.
		map[string]any{"type": "function_call", "id": "call_2", "name": "read_file", "arguments": map[string]any{}},
		map[string]any{"type": "function_result", "call_id": "call_1", "name": "read_file",
			"result": []any{map[string]any{"type": "text", "text": "the file"}}},
		map[string]any{"type": "function_result", "call_id": "call_2", "name": "read_file",
			"result": []any{map[string]any{"type": "text", "text": "The tool could not do this: no such file"}}},
	}
	if !reflect.DeepEqual(body["input"], want) {
		got, _ := json.MarshalIndent(body["input"], "", " ")
		t.Errorf("input =\n%s", got)
	}
	if !reflect.DeepEqual(body["generation_config"], map[string]any{"max_output_tokens": 4096.0}) || body["system_instruction"] != "instructions" {
		t.Errorf("generation_config = %v, system_instruction = %v", body["generation_config"], body["system_instruction"])
	}
}

func TestHowACallEnds(t *testing.T) {
	for status, tc := range map[string]struct {
		calls int
		want  EndReason
	}{
		"completed":       {0, EndStop},
		"requires_action": {1, EndToolUse},
		"incomplete":      {0, EndMaxTokens},
		"failed":          {0, EndRefused},
		"cancelled":       {0, EndRefused},
		"something_new":   {0, EndRefused},
		"":                {0, EndRefused},
	} {
		if got := endReason(status, tc.calls); got != tc.want {
			t.Errorf("status %q: %q, want %q", status, got, tc.want)
		}
	}
	// A provider that says "completed" while having asked for a tool is still waiting for it.
	if got := endReason("completed", 1); got != EndToolUse {
		t.Errorf("completed with a tool call: %q", got)
	}
}

func TestNewConverserNeedsAProviderAnAddressAndAKey(t *testing.T) {
	if _, err := NewConverser("anthropic", "http://127.0.0.1:1", "k"); err == nil {
		t.Error("a provider with no conversation adapter was accepted")
	}
	if _, err := NewConverser(ProviderGemini, "", "k"); err == nil {
		t.Error("an empty address was accepted")
	}
	if _, err := NewConverser(ProviderGemini, "http://127.0.0.1:1", ""); err == nil {
		t.Error("an empty key was accepted")
	}
}

func TestAToolWhoseSchemaIsNotJSONIsRefusedBeforeAnythingIsSent(t *testing.T) {
	conv, sent, _ := serving(t)
	_, err := collect(t, conv, ConvRequest{Model: "m", Tools: []ToolSpec{{Name: "t", SchemaJSON: "{not json"}}})
	if err == nil || len(*sent) != 0 {
		t.Errorf("err = %v, requests sent = %d", err, len(*sent))
	}
}
