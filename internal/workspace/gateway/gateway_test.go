// SPDX-License-Identifier: MIT

package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/model"
	"github.com/adamtait/reviewer/internal/workspace/obs"
)

// marker stands for a credential. The fake scanner flags any text holding it.
const marker = "CREDENTIAL-HERE"

var limits = Limits{ModelCalls: 6, QuestionTokens: 10000, AnswerTokens: 4000, CallTimeout: time.Minute, MaxWait: 30 * time.Second}

// files is the commit the questions are about.
var files = map[string]string{
	"src/sum.ts":    "export function sum(a: number, b: number) {\n  return a + b;\n}\n",
	"src/config.ts": "export const config = {\n  region: 'eu',\n  apiKey: '" + marker + "',\n};\n",
	"src/caller.ts": "import { sum } from './sum';\nsum(1, 2);\n",
}

type world struct {
	t       *testing.T
	model   *Scripted
	scanner *FakeScanner
	gateway *Gateway
	log     *bytes.Buffer
	slept   []time.Duration
	now     time.Time
}

func newWorld(t *testing.T, steps ...Step) *world {
	t.Helper()
	w := &world{t: t, model: NewScripted(steps...), scanner: &FakeScanner{Marker: marker}, log: &bytes.Buffer{},
		now: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	w.gateway = New(map[string]Target{
		"example/alpha": {Provider: "gemini", Model: "model-for-alpha", Converser: w.model},
	}, w.scanner, limits, obs.New(w.log, obs.NewRedactor()))
	w.gateway.now = func() time.Time { return w.now }
	w.gateway.sleep = func(ctx context.Context, d time.Duration) error {
		w.slept = append(w.slept, d)
		return ctx.Err()
	}
	return w
}

func call(question string) Call {
	return Call{
		Owner: "example", Repo: "alpha", SHA: strings.Repeat("1", 40), UserLogin: "alice",
		System: "the instructions", Question: question,
		File: func(path string) (string, bool) { text, ok := files[path]; return text, ok },
	}
}

// readFile is a tool that returns lines of a file, as the assistant's does.
func readFile() Tool {
	return Tool{
		Spec: model.ToolSpec{Name: "read_file", Description: "Read a file.", SchemaJSON: `{"type":"object"}`},
		Run: func(_ context.Context, arguments string) ([]Segment, error) {
			var args struct {
				Path  string `json:"path"`
				Lines int    `json:"lines"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return nil, errors.New("the arguments are not JSON")
			}
			text, ok := files[args.Path]
			if !ok {
				return nil, errors.New("no file at " + args.Path)
			}
			lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
			if args.Lines > 0 && args.Lines < len(lines) {
				lines = lines[:args.Lines]
			}
			return []Segment{{Path: args.Path, StartLine: 1, EndLine: len(lines), Text: strings.Join(lines, "\n")}}, nil
		},
	}
}

func describe() Tool {
	return Tool{
		Spec: model.ToolSpec{Name: "read_description", SchemaJSON: `{"type":"object"}`},
		Run: func(context.Context, string) ([]Segment, error) {
			return []Segment{{Name: "the pull request description", Text: "Deploy with the key " + marker + "."}}, nil
		},
	}
}

func asksFor(id, path string) model.ToolCall {
	return model.ToolCall{ID: id, Name: "read_file", ArgumentsJSON: `{"path":"` + path + `"}`}
}

type seen struct {
	text     string
	excerpts []Excerpt
	withheld []Withheld
	waits    []Waiting
	all      []Event
}

func (w *world) answer(c Call, tools ...Tool) (Result, seen) {
	w.t.Helper()
	var got seen
	result := w.gateway.Answer(context.Background(), c, tools, func(ev Event) {
		got.all = append(got.all, ev)
		switch e := ev.(type) {
		case Delta:
			got.text += e.Text
		case Excerpt:
			got.excerpts = append(got.excerpts, e)
		case Withheld:
			got.withheld = append(got.withheld, e)
		case Waiting:
			got.waits = append(got.waits, e)
		}
	})
	return result, got
}

func (w *world) logLines() []map[string]any {
	w.t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(w.log.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			w.t.Fatalf("log line is not JSON: %s", line)
		}
		out = append(out, m)
	}
	return out
}

// sentText is every piece of text in a request, as the provider would see it.
func sentText(req model.ConvRequest) string {
	var all []string
	for _, turn := range req.Turns {
		for _, part := range turn.Parts {
			switch p := part.(type) {
			case model.Text:
				all = append(all, p.Text)
			case model.ToolResult:
				all = append(all, p.Text)
			case model.ToolCall:
				all = append(all, p.ArgumentsJSON)
			}
		}
	}
	return strings.Join(all, "\n")
}

func TestAQuestionAnsweredThroughATool(t *testing.T) {
	w := newWorld(t, Asks(100, asksFor("call_1", "src/sum.ts")), Says("It adds two numbers.", 180))
	result, got := w.answer(call("What does sum do?"), readFile())

	if result.Outcome != OutcomeOK || result.Message != "" || result.Model != "model-for-alpha" {
		t.Fatalf("result = %+v", result)
	}
	if got.text != "It adds two numbers." {
		t.Errorf("text = %q", got.text)
	}
	if result.ModelCalls != 2 || result.InputTokens != 280 || result.OutputTokens != 30 || result.ReasoningTokens != 70 {
		t.Errorf("totals = %+v", result)
	}
	// Every run of lines the model was given is reported, so the reader can see what the answer rests on.
	if len(got.excerpts) != 1 || got.excerpts[0] != (Excerpt{Path: "src/sum.ts", StartLine: 1, EndLine: 3}) {
		t.Errorf("excerpts = %+v", got.excerpts)
	}

	requests := w.model.Requests()
	if len(requests) != 2 {
		t.Fatalf("%d model calls", len(requests))
	}
	first, second := requests[0], requests[1]
	if first.Model != "model-for-alpha" || first.System != "the instructions" || first.MaxOutputTokens != 4000 ||
		first.Timeout != time.Minute || len(first.Tools) != 1 {
		t.Errorf("first request = %+v", first)
	}
	if len(first.Turns) != 1 || sentText(first) != "What does sum do?" {
		t.Errorf("first request's turns = %+v", first.Turns)
	}
	// The second call carries the model's own turn back, then the tool's result.
	if len(second.Turns) != 3 || second.Turns[1].Role != model.RoleAssistant || second.Turns[2].Role != model.RoleUser {
		t.Fatalf("second request's turns = %+v", second.Turns)
	}
	result1, ok := second.Turns[2].Parts[0].(model.ToolResult)
	if !ok || result1.CallID != "call_1" || result1.Name != "read_file" || result1.IsError || !strings.Contains(result1.Text, "return a + b") {
		t.Errorf("tool result = %+v", second.Turns[2].Parts[0])
	}
}

// Inside one question the provider refuses the next call without the
// reasoning data from the last. It goes back unchanged, and goes nowhere else.
func TestReasoningDataGoesBackToTheProviderAndNowhereElse(t *testing.T) {
	w := newWorld(t, Asks(100, asksFor("call_1", "src/sum.ts")), Says("Done.", 150))
	_, got := w.answer(call("q"), readFile())

	second := w.model.Requests()[1]
	parts := second.Turns[1].Parts
	if len(parts) != 2 {
		t.Fatalf("the assistant's turn has %d parts, want its reasoning data and its call", len(parts))
	}
	opaque, isOpaque := parts[0].(model.Opaque)
	if _, isCall := parts[1].(model.ToolCall); !isOpaque || !isCall || string(opaque.Data) != `{"type":"thought","signature":"before-call_1"}` {
		t.Errorf("the assistant's turn = %+v; want the reasoning data first and unchanged", parts)
	}
	// The answering call's own reasoning data is not kept for anything.
	for _, ev := range got.all {
		if strings.Contains(strings.ToLower(strings.TrimSpace(strings.Join([]string{eventText(ev)}, ""))), "signature") {
			t.Errorf("reasoning data reached an event: %+v", ev)
		}
	}
	if strings.Contains(w.log.String(), "signature") || strings.Contains(w.log.String(), "before-call_1") {
		t.Error("reasoning data reached the log")
	}
}

func eventText(ev Event) string {
	raw, _ := json.Marshal(ev)
	return string(raw)
}

func TestACredentialInWhatTheReaderSentStopsTheQuestion(t *testing.T) {
	selection := func(path, text string) *Segment {
		return &Segment{Path: path, StartLine: 1, EndLine: 2, Text: text}
	}
	for name, tc := range map[string]struct {
		change func(*Call)
		says   string
	}{
		"in the question": {func(c *Call) { c.Question = "Is " + marker + " still valid?" }, "Your question"},
		"in an earlier question": {func(c *Call) {
			c.History = []Message{{Role: "user", Text: "the key is " + marker}, {Role: "assistant", Text: "noted"}}
		}, "An earlier question or answer"},
		"in an earlier answer": {func(c *Call) {
			c.History = []Message{{Role: "user", Text: "hello"}, {Role: "assistant", Text: "you pasted " + marker}}
		}, "An earlier question or answer"},
		"in the name of the open file": {func(c *Call) {
			c.Where = `The reader is looking at the file "keys/` + marker + `.ts".`
		}, "The name of the file you have open"},
		"in the selected lines": {func(c *Call) { c.Selection = selection("src/config.ts", "apiKey: '"+marker+"'") }, "src/config.ts"},
		// The lines selected are clean. The file they come from is not.
		"elsewhere in the file the selection comes from": {func(c *Call) {
			c.Selection = selection("src/config.ts", "export const config = {\n  region: 'eu',")
		}, "src/config.ts"},
	} {
		w := newWorld(t, Says("should never be asked", 10))
		c := call("What does this do?")
		tc.change(&c)
		result, got := w.answer(c, readFile())

		if result.Outcome != OutcomeSecretBlocked {
			t.Errorf("%s: outcome %q, want secret_blocked", name, result.Outcome)
		}
		if !strings.Contains(result.Message, tc.says) || !strings.Contains(result.Message, "Nothing was sent") {
			t.Errorf("%s: message %q", name, result.Message)
		}
		if strings.Contains(result.Message, marker) {
			t.Errorf("%s: the message repeats the credential", name)
		}
		if n := len(w.model.Requests()); n != 0 {
			t.Errorf("%s: the provider was called %d times", name, n)
		}
		if len(got.all) != 0 {
			t.Errorf("%s: events were sent: %+v", name, got.all)
		}
		if strings.Contains(w.log.String(), marker) {
			t.Errorf("%s: the credential reached the log", name)
		}
		if lines := w.logLines(); len(lines) != 1 || lines[0]["msg"] != "question_not_sent" || lines[0]["outcome"] != OutcomeSecretBlocked {
			t.Errorf("%s: log = %v", name, lines)
		}
	}
}

func TestACredentialInWhatAToolReturnsIsWithheldByFileAndNamed(t *testing.T) {
	w := newWorld(t,
		Asks(100, asksFor("call_1", "src/config.ts"), asksFor("call_2", "src/sum.ts")),
		Says("I could not read the configuration.", 200))
	result, got := w.answer(call("What region is configured?"), readFile())

	if result.Outcome != OutcomeOK {
		t.Fatalf("result = %+v: a credential in a file the model asked for ends that file, not the question", result)
	}
	if len(got.withheld) != 1 || got.withheld[0].What != "src/config.ts" || !strings.Contains(got.withheld[0].Reason, "credential") {
		t.Errorf("withheld = %+v", got.withheld)
	}
	if len(result.Withheld) != 1 || result.Withheld[0].What != "src/config.ts" {
		t.Errorf("result.Withheld = %+v", result.Withheld)
	}

	second := w.model.Requests()[1]
	sent := sentText(second)
	if strings.Contains(sent, marker) || strings.Contains(sent, "region: 'eu'") {
		t.Errorf("the flagged file's content was sent:\n%s", sent)
	}
	if !strings.Contains(sent, "[Withheld: src/config.ts.") {
		t.Errorf("the model was not told the file was withheld:\n%s", sent)
	}
	// The other file, asked for in the same step, is sent.
	if !strings.Contains(sent, "return a + b") {
		t.Errorf("a clean file asked for beside a flagged one was not sent:\n%s", sent)
	}
	// Only the file that was sent is an excerpt.
	if len(got.excerpts) != 1 || got.excerpts[0].Path != "src/sum.ts" {
		t.Errorf("excerpts = %+v", got.excerpts)
	}
	lines := w.logLines()
	if lines[0]["scan"] != "withheld:1" || lines[0]["withheld"] != "src/config.ts" {
		t.Errorf("the model call's log line = %v", lines[0])
	}
}

// The lines a tool returns can be clean while the file they come from is not.
// The scan is of the whole file, so those lines are withheld too.
func TestCleanLinesFromAFlaggedFileAreWithheld(t *testing.T) {
	w := newWorld(t,
		Asks(100, model.ToolCall{ID: "call_1", Name: "read_file", ArgumentsJSON: `{"path":"src/config.ts","lines":2}`}),
		Says("ok", 150))
	_, got := w.answer(call("q"), readFile())
	if len(got.withheld) != 1 {
		t.Fatalf("two clean lines of a file that holds a credential were sent: withheld = %+v", got.withheld)
	}
	if sent := sentText(w.model.Requests()[1]); strings.Contains(sent, "region") {
		t.Errorf("sent:\n%s", sent)
	}
}

func TestTheDescriptionIsWithheldTheSameWay(t *testing.T) {
	w := newWorld(t, Asks(100, model.ToolCall{ID: "call_1", Name: "read_description", ArgumentsJSON: `{}`}), Says("ok", 150))
	result, got := w.answer(call("What is this pull request for?"), describe())
	if result.Outcome != OutcomeOK || len(got.withheld) != 1 || got.withheld[0].What != "the pull request description" {
		t.Errorf("result %+v, withheld %+v", result, got.withheld)
	}
	if sent := sentText(w.model.Requests()[1]); strings.Contains(sent, marker) || !strings.Contains(sent, "[Withheld: the pull request description.") {
		t.Errorf("sent:\n%s", sent)
	}
}

func TestIfTheScannerCannotRunNothingIsSent(t *testing.T) {
	// Before the first call.
	w := newWorld(t, Says("should never be asked", 10))
	w.scanner.Err = errors.New("the scanner is not installed")
	result, _ := w.answer(call("q"), readFile())
	if result.Outcome != OutcomeScanFailed || len(w.model.Requests()) != 0 {
		t.Errorf("result %+v after %d model calls", result, len(w.model.Requests()))
	}

	// After a tool has run: its result is not sent, and the question ends.
	w = newWorld(t, Asks(100, asksFor("call_1", "src/sum.ts")), Says("should never be asked", 10))
	broke := false
	tool := readFile()
	run := tool.Run
	tool.Run = func(ctx context.Context, arguments string) ([]Segment, error) {
		w.scanner.Err = errors.New("the scanner stopped working")
		broke = true
		return run(ctx, arguments)
	}
	result, got := w.answer(call("q"), tool)
	if !broke || result.Outcome != OutcomeScanFailed || !strings.Contains(result.Message, "nothing more was sent") {
		t.Errorf("result = %+v", result)
	}
	if n := len(w.model.Requests()); n != 1 {
		t.Errorf("%d model calls; the tool's unscanned result must not be sent in a second", n)
	}
	if len(got.excerpts) != 0 {
		t.Error("lines that were never sent were reported as given to the model")
	}
}

func TestEverythingSentIsScannedFirst(t *testing.T) {
	w := newWorld(t, Asks(100, asksFor("call_1", "src/sum.ts")), Says("ok", 150))
	c := call("the question")
	c.History = []Message{{Role: "user", Text: "an earlier question"}, {Role: "assistant", Text: "an earlier answer"}}
	c.Where = "The reader has pull request #7 open."
	c.Selection = &Segment{Path: "src/caller.ts", StartLine: 2, EndLine: 2, Text: "sum(1, 2);"}
	w.answer(c, readFile())

	scanned := strings.Join(w.scanner.Scanned(), "\n---\n")
	for _, want := range []string{
		"the question", "an earlier question", "an earlier answer", "sum(1, 2);",
		"The reader has pull request #7 open.",
		files["src/caller.ts"], // the whole file behind the selection
		files["src/sum.ts"],    // the whole file behind the tool's lines
	} {
		if !strings.Contains(scanned, want) {
			t.Errorf("not scanned before sending: %q", want)
		}
	}
	// The question as sent carries the selection, and says where it is from.
	first := sentText(w.model.Requests()[0])
	if !strings.Contains(first, "the question") || !strings.Contains(first, "src/caller.ts, lines 2 to 2") || !strings.Contains(first, "sum(1, 2);") {
		t.Errorf("the first request:\n%s", first)
	}
	if got := w.model.Requests()[0].Turns; len(got) != 3 || got[0].Role != model.RoleUser || got[1].Role != model.RoleAssistant {
		t.Errorf("the earlier question and answer were not sent as turns: %+v", got)
	}
	// Where the reader is comes first in the question's own turn, so that
	// the question is read in its light.
	turns := w.model.Requests()[0].Turns
	if text := turns[len(turns)-1].Parts[0].(model.Text).Text; !strings.HasPrefix(text, "The reader has pull request #7 open.\n\nthe question\n\n") {
		t.Errorf("the question's turn:\n%s", text)
	}
}

// With nothing to say about where the reader is, the question is sent as it
// was asked.
func TestAQuestionWithNothingOpenIsSentAsAsked(t *testing.T) {
	w := newWorld(t, Says("ok", 10))
	w.answer(call("the question"), readFile())
	turns := w.model.Requests()[0].Turns
	if text := turns[len(turns)-1].Parts[0].(model.Text).Text; text != "the question" {
		t.Errorf("the question's turn: %q", text)
	}
}

func TestAQuestionReachesOnlyItsOwnRepositorysProvider(t *testing.T) {
	alpha, beta := NewScripted(Says("from alpha's provider", 10)), NewScripted(Says("from beta's provider", 10))
	g := New(map[string]Target{
		"example/alpha": {Provider: "gemini", Model: "model-a", Converser: alpha},
		"example/beta":  {Provider: "gemini", Model: "model-b", Converser: beta},
	}, &FakeScanner{}, limits, obs.New(&bytes.Buffer{}, obs.NewRedactor()))

	c := call("q")
	c.Owner, c.Repo = "Example", "BETA" // spelled as an address might spell it
	var text string
	result := g.Answer(context.Background(), c, nil, func(ev Event) {
		if d, ok := ev.(Delta); ok {
			text += d.Text
		}
	})
	if result.Outcome != OutcomeOK || text != "from beta's provider" || result.Model != "model-b" {
		t.Errorf("result %+v, text %q", result, text)
	}
	if len(alpha.Requests()) != 0 || len(beta.Requests()) != 1 {
		t.Errorf("alpha's provider was called %d times and beta's %d", len(alpha.Requests()), len(beta.Requests()))
	}

	// A repository with no assistant is not sent anywhere.
	c.Repo = "gamma"
	if result := g.Answer(context.Background(), c, nil, func(Event) {}); result.Outcome != OutcomeProviderError {
		t.Errorf("a repository with no provider: %+v", result)
	}
	if model, ok := g.Model("example", "alpha"); !ok || model != "model-a" {
		t.Errorf("Model = %q, %v", model, ok)
	}
	if _, ok := g.Model("example", "gamma"); ok {
		t.Error("a repository with no provider was reported as having an assistant")
	}
}

func TestEachLimitEndsTheQuestionAndSaysWhich(t *testing.T) {
	t.Run("model calls", func(t *testing.T) {
		var steps []Step
		for i := 0; i < 10; i++ {
			steps = append(steps, Asks(10, asksFor("call", "src/sum.ts")))
		}
		w := newWorld(t, steps...)
		result, _ := w.answer(call("q"), readFile())
		if result.Outcome != OutcomeLimitReached || !strings.Contains(result.Message, "6 model calls") || result.ModelCalls != 6 {
			t.Errorf("result = %+v", result)
		}
		if n := len(w.model.Requests()); n != 6 {
			t.Errorf("%d calls made", n)
		}
	})
	t.Run("input tokens", func(t *testing.T) {
		w := newWorld(t, Asks(6000, asksFor("c1", "src/sum.ts")), Asks(6000, asksFor("c2", "src/sum.ts")), Says("never", 10))
		result, _ := w.answer(call("q"), readFile())
		if result.Outcome != OutcomeLimitReached || !strings.Contains(result.Message, "10000 input tokens") || result.InputTokens != 12000 {
			t.Errorf("result = %+v", result)
		}
		if n := len(w.model.Requests()); n != 2 {
			t.Errorf("%d calls made; the third should not have been", n)
		}
	})
	t.Run("an answer cut off at its output ceiling", func(t *testing.T) {
		w := newWorld(t, Step{Events: []model.Event{
			model.TextDelta{Text: "The answer begins and"},
			model.Usage{InputTokens: 50, OutputTokens: 4000},
			model.End{Reason: model.EndMaxTokens, Detail: "incomplete"},
		}})
		result, got := w.answer(call("q"))
		if result.Outcome != OutcomeLimitReached || !strings.Contains(result.Message, "4000 output tokens") {
			t.Errorf("result = %+v", result)
		}
		if got.text != "The answer begins and" {
			t.Errorf("the part of the answer that arrived was not passed on: %q", got.text)
		}
	})
}

func limit(wait time.Duration, has bool) *model.ProviderError {
	return &model.ProviderError{Status: 429, Code: "too_many_requests", Message: "Rate limit exceeded.", RetryAfter: wait, HasRetryAfter: has}
}

func TestProviderLimitsAndWaits(t *testing.T) {
	t.Run("a short wait is sat out once and the call tried again", func(t *testing.T) {
		w := newWorld(t, Fails(limit(12*time.Second, true)), Says("after the wait", 10))
		result, got := w.answer(call("q"))
		if result.Outcome != OutcomeOK || got.text != "after the wait" {
			t.Fatalf("result = %+v", result)
		}
		if len(got.waits) != 1 || got.waits[0].Seconds != 13 || len(w.slept) != 1 || w.slept[0] != 13*time.Second {
			t.Errorf("waits = %+v, slept %v; want one wait of the named 12 seconds and one more", got.waits, w.slept)
		}
		// The refused call does not count against the limit on calls.
		if result.ModelCalls != 1 {
			t.Errorf("ModelCalls = %d", result.ModelCalls)
		}
	})
	t.Run("a second limit is not sat out again", func(t *testing.T) {
		w := newWorld(t, Fails(limit(12*time.Second, true)), Fails(limit(12*time.Second, true)), Says("never", 10))
		result, _ := w.answer(call("q"))
		if result.Outcome != OutcomeRateLimited || result.RetryAfterSeconds != 12 || len(w.slept) != 1 {
			t.Errorf("result = %+v, slept %v", result, w.slept)
		}
	})
	t.Run("a wait longer than the service sits out is a rate limit", func(t *testing.T) {
		w := newWorld(t, Fails(limit(90*time.Second, true)))
		result, got := w.answer(call("q"))
		if result.Outcome != OutcomeRateLimited || result.RetryAfterSeconds != 90 || !strings.Contains(result.Message, "about a minute") ||
			len(w.slept) != 0 || len(got.waits) != 0 {
			t.Errorf("result = %+v, slept %v", result, w.slept)
		}
	})
	t.Run("a wait over five minutes is an exhausted quota, with when it resets", func(t *testing.T) {
		w := newWorld(t, Fails(limit(31174*time.Second, true)))
		result, _ := w.answer(call("q"))
		if result.Outcome != OutcomeQuotaExhausted || result.RetryAfterSeconds != 31174 || result.ResetAt != "2026-11-24T20:39:34Z" {
			t.Errorf("result = %+v", result)
		}
		if !strings.Contains(result.Message, "20:39 UTC on 24 November") {
			t.Errorf("message = %q", result.Message)
		}
	})
	t.Run("exactly five minutes is still a rate limit", func(t *testing.T) {
		w := newWorld(t, Fails(limit(5*time.Minute, true)))
		if result, _ := w.answer(call("q")); result.Outcome != OutcomeRateLimited {
			t.Errorf("result = %+v", result)
		}
	})
	t.Run("a limit with no wait named", func(t *testing.T) {
		w := newWorld(t, Fails(limit(0, false)))
		if result, _ := w.answer(call("q")); result.Outcome != OutcomeRateLimited || result.RetryAfterSeconds != 0 || len(w.slept) != 0 {
			t.Errorf("result = %+v", result)
		}
	})
	t.Run("heavy demand is tried again once, after five seconds", func(t *testing.T) {
		busy := &model.ProviderError{Status: 503, Code: "service_unavailable", Message: "high demand"}
		w := newWorld(t, Fails(busy), Says("second time lucky", 10))
		result, got := w.answer(call("q"))
		if result.Outcome != OutcomeOK || len(w.slept) != 1 || w.slept[0] != 5*time.Second || len(got.waits) != 1 {
			t.Errorf("result = %+v, slept %v", result, w.slept)
		}

		w = newWorld(t, Fails(busy), Fails(busy), Says("never", 10))
		result, _ = w.answer(call("q"))
		if result.Outcome != OutcomeProviderError || !strings.Contains(result.Message, "503 service_unavailable") || len(w.slept) != 1 {
			t.Errorf("twice busy: result = %+v, slept %v", result, w.slept)
		}
	})
}

func TestOtherProviderFailuresAreReportedWithTheirStatusAndCode(t *testing.T) {
	for name, tc := range map[string]struct {
		failure *model.ProviderError
		says    string
	}{
		"a bad key":          {&model.ProviderError{Status: 401, Code: "UNAUTHENTICATED", Message: "API key not valid."}, "401 UNAUTHENTICATED"},
		"an unknown model":   {&model.ProviderError{Status: 404, Code: "not_found", Message: "Model not found."}, "404 not_found"},
		"a rejected request": {&model.ProviderError{Status: 400, Code: "invalid_request", Message: "invalid argument"}, "400 invalid_request"},
		"no connection":      {&model.ProviderError{Message: "the connection failed"}, "could not be reached"},
	} {
		w := newWorld(t, Fails(tc.failure), Says("never", 10))
		result, _ := w.answer(call("q"))
		if result.Outcome != OutcomeProviderError || !strings.Contains(result.Message, tc.says) {
			t.Errorf("%s: result = %+v", name, result)
		}
		if n := len(w.model.Requests()); n != 1 {
			t.Errorf("%s: tried %d times; only limits and heavy demand are tried again", name, n)
		}
	}
}

func TestAProviderThatDeclinesIsRefused(t *testing.T) {
	w := newWorld(t, Step{Events: []model.Event{model.Usage{InputTokens: 10}, model.End{Reason: model.EndRefused, Detail: "failed"}}})
	if result, _ := w.answer(call("q")); result.Outcome != OutcomeRefused {
		t.Errorf("result = %+v", result)
	}
}

func TestAToolThatCannotDoWhatWasAskedDoesNotEndTheQuestion(t *testing.T) {
	w := newWorld(t,
		Asks(100,
			asksFor("call_1", "src/absent.ts"),
			model.ToolCall{ID: "call_2", Name: "delete_repository", ArgumentsJSON: `{}`},
			model.ToolCall{ID: "call_3", Name: "read_file", ArgumentsJSON: `not json`},
		),
		Says("I could not find that file.", 150))
	result, _ := w.answer(call("q"), readFile())
	if result.Outcome != OutcomeOK {
		t.Fatalf("result = %+v", result)
	}
	back := w.model.Requests()[1].Turns[2].Parts
	if len(back) != 3 {
		t.Fatalf("%d results for 3 calls", len(back))
	}
	for i, want := range []string{"no file at src/absent.ts", `no tool named "delete_repository"`, "not JSON"} {
		got := back[i].(model.ToolResult)
		if !got.IsError || !strings.Contains(got.Text, want) {
			t.Errorf("result %d = %+v, want an error containing %q", i, got, want)
		}
	}
}

func TestAModelThatAsksForTooManyToolsAtOnceIsToldSo(t *testing.T) {
	var calls []model.ToolCall
	for i := 0; i < maxToolCalls+3; i++ {
		calls = append(calls, asksFor("call", "src/sum.ts"))
	}
	w := newWorld(t, Asks(100, calls...), Says("ok", 150))
	w.answer(call("q"), readFile())
	back := w.model.Requests()[1].Turns[2].Parts
	refused := 0
	for _, part := range back {
		if r := part.(model.ToolResult); r.IsError && strings.Contains(r.Text, "too many tools") {
			refused++
		}
	}
	if len(back) != maxToolCalls+3 || refused != 3 {
		t.Errorf("%d results, %d refused; every call needs a result, and the extra three an error", len(back), refused)
	}
}

func TestEveryModelCallWritesOneLogLineThatHoldsNoContent(t *testing.T) {
	w := newWorld(t, Asks(100, asksFor("call_1", "src/sum.ts")), Says("It adds two numbers.", 180))
	c := call("What does sum do in this unusual question?")
	w.answer(c, readFile())

	lines := w.logLines()
	if len(lines) != 2 {
		t.Fatalf("%d log lines for 2 model calls", len(lines))
	}
	for i, line := range lines {
		for key, want := range map[string]any{
			"msg": "model_call", "user": "alice", "owner": "example", "repo": "alpha", "sha": c.SHA,
			"provider": "gemini", "model": "model-for-alpha", "call": float64(i + 1),
		} {
			if line[key] != want {
				t.Errorf("line %d: %s = %v, want %v", i+1, key, line[key], want)
			}
		}
	}
	if lines[0]["outcome"] != "tool_use" || lines[0]["scan"] != "clean" || lines[0]["input_tokens"] != float64(100) {
		t.Errorf("first line = %v", lines[0])
	}
	if lines[1]["outcome"] != "answered" || lines[1]["input_tokens"] != float64(180) || lines[1]["reasoning_tokens"] != float64(30) {
		t.Errorf("second line = %v", lines[1])
	}
	log := w.log.String()
	for _, content := range []string{"unusual question", "adds two numbers", "return a + b", "the instructions"} {
		if strings.Contains(log, content) {
			t.Errorf("the log holds content: %q", content)
		}
	}
}

func TestAReaderWhoGoesAwayStopsTheQuestion(t *testing.T) {
	w := newWorld(t, Asks(100, asksFor("call_1", "src/sum.ts")), Says("never read", 10))
	ctx, cancel := context.WithCancel(context.Background())
	tool := readFile()
	run := tool.Run
	tool.Run = func(c context.Context, arguments string) ([]Segment, error) {
		cancel() // the browser disconnects while the tool runs
		return run(c, arguments)
	}
	result := w.gateway.Answer(ctx, call("q"), []Tool{tool}, func(Event) {})
	if result.Outcome != OutcomeAbandoned {
		t.Errorf("result = %+v", result)
	}
}

func TestConnectMakesOneTargetForEachRepository(t *testing.T) {
	targets, err := Connect([]Repository{
		{Owner: "Example", Name: "Alpha", Provider: "gemini", Model: "m", BaseURL: "http://127.0.0.1:1/v1", Key: "k"},
	})
	if err != nil || len(targets) != 1 || targets["example/alpha"].Model != "m" || targets["example/alpha"].Converser == nil {
		t.Errorf("targets = %+v, err %v", targets, err)
	}
	if _, err := Connect([]Repository{{Owner: "example", Name: "alpha", Provider: "something-else", BaseURL: "http://127.0.0.1:1", Key: "k"}}); err == nil {
		t.Error("a provider with no adapter was connected")
	}
}
