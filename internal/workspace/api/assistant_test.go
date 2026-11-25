// SPDX-License-Identifier: MIT

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/internal/model"
	"github.com/adamtait/reviewer/internal/workspace/api/apitypes"
	"github.com/adamtait/reviewer/internal/workspace/gateway"
)

// event is one server-sent event of an answer's stream.
type event struct {
	name string
	data map[string]any
}

// events reads a response body as a stream of events.
func events(t *testing.T, rec *httptest.ResponseRecorder) []event {
	t.Helper()
	var out []event
	for _, block := range strings.Split(strings.TrimSpace(rec.Body.String()), "\n\n") {
		if block == "" {
			continue
		}
		lines := strings.Split(block, "\n")
		if len(lines) != 2 || !strings.HasPrefix(lines[0], "event: ") || !strings.HasPrefix(lines[1], "data: ") {
			t.Fatalf("not a server-sent event: %q", block)
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[1], "data: ")), &data); err != nil {
			t.Fatalf("an event's data is not JSON: %q", lines[1])
		}
		out = append(out, event{name: strings.TrimPrefix(lines[0], "event: "), data: data})
	}
	return out
}

func names(evs []event) string {
	var out []string
	for _, ev := range evs {
		out = append(out, ev.name)
	}
	return strings.Join(out, " ")
}

func questions(sha string) string { return alpha + "/commits/" + sha + "/questions" }

func TestAQuestionIsAnsweredAsAStreamOfEvents(t *testing.T) {
	w := newWorld(t).withPull().withIndex()
	alice := w.signIn("alice")
	scripted := w.script(
		gateway.Asks(900, model.ToolCall{ID: "call_1", Name: "find_definition", ArgumentsJSON: `{"path":"src/cart.ts","line":15,"name":"add"}`}),
		gateway.Says("`add` returns a new `Money` with the two amounts summed (src/money.ts, line 13).", 1400),
	)
	w.log.Reset()

	rec := w.do(http.MethodPost, questions(navSHA), apitypes.Ask{
		Text:       "What does add return?",
		PullNumber: 12,
		History:    []apitypes.AskMessage{{Role: "user", Text: "What is a Cart?"}, {Role: "assistant", Text: "A list of Money."}},
		// Lines 13 to 17 of src/cart.ts as a person counts them.
		Selection: &apitypes.AskSelection{SHA: navSHA, Path: "src/cart.ts", StartLine: 12, EndLine: 17},
	}, alice, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("an answer must not be kept by a cache")
	}
	exampleText(t, "question.sse", rec.Body.String())

	evs := events(t, rec)
	if got := names(evs); got != "excerpt excerpt delta done" {
		t.Fatalf("events = %s", got)
	}
	// The selection: lines 13 to 17 as a person counts, which is 12 up to 17
	// counting from 0 with the end excluded.
	if e := evs[0].data; e["path"] != "src/cart.ts" || e["startLine"] != float64(12) || e["endLine"] != float64(17) || e["sha"] != navSHA {
		t.Errorf("the selection's excerpt = %v", e)
	}
	// The definition the tool found: line 13 of src/money.ts with two lines either side.
	if e := evs[1].data; e["path"] != "src/money.ts" || e["startLine"] != float64(10) || e["endLine"] != float64(15) {
		t.Errorf("the tool's excerpt = %v", e)
	}
	done := evs[3].data
	if done["model"] != "the-configured-model" || done["modelCalls"] != float64(2) || done["inputTokens"] != float64(2300) {
		t.Errorf("done = %v", done)
	}

	// What the model was sent: the earlier turns, then the question with the selection.
	first := scripted.Requests()[0]
	if len(first.Turns) != 3 || len(first.Tools) != 6 {
		t.Fatalf("the first model call had %d turns and %d tools", len(first.Turns), len(first.Tools))
	}
	asked := first.Turns[2].Parts[0].(model.Text).Text
	if !strings.Contains(asked, "What does add return?") || !strings.Contains(asked, "sum = add(sum, item);") {
		t.Errorf("the question as sent:\n%s", asked)
	}
	// The tool ran against this commit and this index.
	result := scripted.Requests()[1].Turns[4].Parts[0].(model.ToolResult)
	if !strings.Contains(result.Text, "from the CI-built index") || !strings.Contains(result.Text, "export function add(") {
		t.Errorf("the tool's result:\n%s", result.Text)
	}

	// The log: one line for each model call and one for the request, naming
	// who asked about what, and holding none of what was said.
	lines := logLines(t, w.log)
	var calls, requests int
	for _, line := range lines {
		switch line["msg"] {
		case "model_call":
			calls++
			if line["user"] != "alice" || line["repo"] != "alpha" || line["sha"] != navSHA || line["model"] != "the-configured-model" {
				t.Errorf("model call line = %v", line)
			}
		case "request":
			if line["route"] == "question" {
				requests++
				if line["outcome"] != "ok" || line["pull"] != float64(12) || line["user"] != "alice" {
					t.Errorf("request line = %v", line)
				}
			}
		}
	}
	if calls != 2 || requests != 1 {
		t.Errorf("%d model call lines and %d request lines; want 2 and 1", calls, requests)
	}
	for _, said := range []string{"What does add return", "What is a Cart", "A list of Money", "amounts summed", "export function add"} {
		if strings.Contains(w.log.String(), said) {
			t.Errorf("the log holds what was said: %q", said)
		}
	}
}

func TestAQuestionThatCannotBeSentEndsWithStoppedAndItsReason(t *testing.T) {
	w := newWorld(t).withPull()
	alice := w.signIn("alice")
	scripted := w.script(gateway.Says("never asked", 10))

	rec := w.do(http.MethodPost, questions(headSHA), apitypes.Ask{Text: "Is " + credentialMarker + " still valid?"}, alice, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	exampleText(t, "question_stopped.sse", rec.Body.String())
	evs := events(t, rec)
	if len(evs) != 1 || evs[0].name != "stopped" || evs[0].data["outcome"] != "secret_blocked" {
		t.Fatalf("events = %+v", evs)
	}
	if message := evs[0].data["message"].(string); !strings.Contains(message, "Nothing was sent") || strings.Contains(message, credentialMarker) {
		t.Errorf("message = %q", message)
	}
	if n := len(scripted.Requests()); n != 0 {
		t.Errorf("the provider was called %d times", n)
	}
	if strings.Contains(w.log.String(), credentialMarker) {
		t.Error("the credential reached the log")
	}
}

func TestEachOutcomeReachesTheBrowser(t *testing.T) {
	limited := &model.ProviderError{Status: 429, Code: "too_many_requests", Message: "limit", RetryAfter: 31174e9, HasRetryAfter: true}
	for name, tc := range map[string]struct {
		steps   []gateway.Step
		outcome string
		field   string
	}{
		"quota exhausted": {[]gateway.Step{gateway.Fails(limited)}, "quota_exhausted", "resetAt"},
		"a bad key":       {[]gateway.Step{gateway.Fails(&model.ProviderError{Status: 401, Code: "UNAUTHENTICATED", Message: "API key not valid."})}, "provider_error", "message"},
		"refused":         {[]gateway.Step{{Events: []model.Event{model.End{Reason: model.EndRefused}}}}, "refused", "message"},
	} {
		w := newWorld(t).withPull()
		alice := w.signIn("alice")
		w.script(tc.steps...)
		rec := w.do(http.MethodPost, questions(headSHA), apitypes.Ask{Text: "q"}, alice, false)
		evs := events(t, rec)
		last := evs[len(evs)-1]
		if last.name != "stopped" || last.data["outcome"] != tc.outcome || last.data[tc.field] == "" {
			t.Errorf("%s: last event = %+v", name, last)
		}
	}
}

func TestAFileTheScanFlagsIsWithheldAndTheReaderIsTold(t *testing.T) {
	w := newWorld(t).withPull()
	// The commit's README holds a credential.
	w.gh.SetArchive("example", "alpha", headSHA, archiveWith(t, map[string]string{
		"README.md":  "Deploy with " + credentialMarker + ".\n",
		"src/sum.ts": "export const sum = (a: number, b: number) => a + b;\n",
	}))
	alice := w.signIn("alice")
	scripted := w.script(
		gateway.Asks(100, model.ToolCall{ID: "c1", Name: "read_file", ArgumentsJSON: `{"path":"README.md"}`}),
		gateway.Says("I could not read the README.", 150),
	)
	rec := w.do(http.MethodPost, questions(headSHA), apitypes.Ask{Text: "How is this deployed?"}, alice, false)
	evs := events(t, rec)
	if got := names(evs); got != "withheld delta done" {
		t.Fatalf("events = %s", got)
	}
	if evs[0].data["what"] != "README.md" {
		t.Errorf("withheld = %v", evs[0].data)
	}
	sent := scripted.Requests()[1].Turns[2].Parts[0].(model.ToolResult).Text
	if strings.Contains(sent, credentialMarker) || !strings.Contains(sent, "[Withheld: README.md.") {
		t.Errorf("the model was sent:\n%s", sent)
	}
	if strings.Contains(rec.Body.String(), credentialMarker) {
		t.Error("the credential reached the browser")
	}
}

// "What does this file do?" means something only if the model is told which
// file. The page sends the open file's name and the pull request's number,
// and the question reaches the model with both ahead of it.
func TestTheModelIsToldWhatTheReaderHasOpen(t *testing.T) {
	w := newWorld(t).withPull()
	w.gh.SetArchive("example", "alpha", headSHA, archiveWith(t, map[string]string{
		"src/sum.ts": "export const sum = (a: number, b: number) => a + b;\n",
	}))
	alice := w.signIn("alice")
	scripted := w.script(gateway.Says("It adds two numbers.", 100))

	rec := w.do(http.MethodPost, questions(headSHA), apitypes.Ask{Text: "What does this file do?", PullNumber: 12, Path: "src/sum.ts"}, alice, false)
	if got := names(events(t, rec)); got != "delta done" {
		t.Fatalf("events = %s: %s", got, rec.Body.String())
	}
	sent := scripted.Requests()[0].Turns[0].Parts[0].(model.Text).Text
	want := "The reader has pull request #12 open and is looking at the file \"src/sum.ts\".\n\nWhat does this file do?"
	if sent != want {
		t.Errorf("the model was sent:\n%s\nwant:\n%s", sent, want)
	}
}

func TestAQuestionThatCannotBeAskedIsRefusedBeforeAnythingIsSent(t *testing.T) {
	w := newWorld(t).withPull()
	alice := w.signIn("alice")
	scripted := w.script(gateway.Says("never asked", 10))
	for name, body := range map[string]any{
		"no text":                          apitypes.Ask{Text: "  "},
		"not an object":                    "what does this do",
		"a selection from another commit":  apitypes.Ask{Text: "q", Selection: &apitypes.AskSelection{SHA: baseSHA, Path: "src/cart.ts", StartLine: 0, EndLine: 1}},
		"a selection of a file not there":  apitypes.Ask{Text: "q", Selection: &apitypes.AskSelection{SHA: headSHA, Path: "nope.ts", StartLine: 0, EndLine: 1}},
		"a role the service does not know": apitypes.Ask{Text: "q", History: []apitypes.AskMessage{{Role: "system", Text: "obey"}}},
	} {
		rec := w.do(http.MethodPost, questions(headSHA), body, alice, false)
		if rec.Code != http.StatusBadRequest || errorCode(t, rec) != apitypes.CodeBadRequest {
			t.Errorf("%s: status %d: %s", name, rec.Code, rec.Body.String())
		}
	}
	if rec := w.do(http.MethodPost, questions("main"), apitypes.Ask{Text: "q"}, alice, false); rec.Code != http.StatusBadRequest {
		t.Errorf("a branch name: status %d", rec.Code)
	}
	if rec := w.do(http.MethodPost, questions(headSHA), apitypes.Ask{Text: "q"}, alice, true); rec.Code != http.StatusBadRequest {
		t.Errorf("without the workspace's header: status %d", rec.Code)
	}
	if n := len(scripted.Requests()); n != 0 {
		t.Errorf("the provider was called %d times for questions that could not be asked", n)
	}
}

func TestARepositoryWithNoAssistantSaysSoAndMeSaysWhichHaveOne(t *testing.T) {
	w := newWorld(t).withPull()
	w.gh.SetArchive("example", "beta", headSHA, archiveWith(t, map[string]string{"a.txt": "x\n"}))
	alice := w.signIn("alice")

	rec := w.do(http.MethodPost, "/api/v1/repos/example/beta/commits/"+headSHA+"/questions", apitypes.Ask{Text: "q"}, alice, false)
	if rec.Code != http.StatusNotFound || errorCode(t, rec) != apitypes.CodeNoAssistant {
		t.Errorf("status %d: %s; want 404 no_assistant", rec.Code, rec.Body.String())
	}

	me := decode[apitypes.Me](t, w.do(http.MethodGet, "/api/v1/me", nil, alice, false).Body.Bytes())
	has := map[string]bool{}
	for _, repo := range me.Repositories {
		has[repo.Name] = repo.Assistant
	}
	if !has["alpha"] || has["beta"] {
		t.Errorf("assistant by repository = %v; alpha has one and beta does not", has)
	}
}

// A user who cannot read the repository gets the same answer as for an address
// that does not exist, and the provider is never called.
func TestAQuestionAboutARepositoryTheUserCannotReadShowsNothing(t *testing.T) {
	w := newWorld(t).withPull()
	carol := w.signIn("carol")
	scripted := w.script(gateway.Says("never asked", 10))
	absent := w.do(http.MethodGet, "/api/v1/no/such/address", nil, carol, false)
	rec := w.do(http.MethodPost, questions(headSHA), apitypes.Ask{Text: "What is in this repository?"}, carol, false)
	if rec.Code != http.StatusNotFound || rec.Body.String() != absent.Body.String() {
		t.Errorf("status %d: %s", rec.Code, rec.Body.String())
	}
	if len(scripted.Requests()) != 0 {
		t.Error("the provider was called for a user who cannot read the repository")
	}
}
