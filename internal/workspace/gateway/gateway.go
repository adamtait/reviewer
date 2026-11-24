// SPDX-License-Identifier: MIT

// Package gateway is the one way the workspace reaches a model provider.
//
// It is the only package that is given provider keys, and the only one that
// calls a provider. Everything that leaves for a provider leaves through
// Answer, which scans it at the moment of sending: the question, the earlier
// questions and answers, the selected code, and every tool result (ADR-0031).
//
// Each connected repository names one provider and one model. A question
// about a repository reaches that repository's provider and no other.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/adamtait/reviewer/internal/model"
	"github.com/adamtait/reviewer/internal/workspace/obs"
	"github.com/adamtait/reviewer/internal/workspace/scan"
)

// Segment is a piece of text given to the model.
type Segment struct {
	// Path is the file the text quotes, at the commit the question is about.
	// Empty for text that quotes no file.
	Path string
	// Name says what the text is when it quotes no file, such as "the pull
	// request description". It is what the reader is told was withheld.
	Name string
	// StartLine and EndLine are the lines quoted, counted from 1, inclusive.
	// Zero when the text is not a run of lines.
	StartLine, EndLine int
	Text               string
}

// Message is an earlier question or answer.
type Message struct {
	// Role is "user" or "assistant".
	Role string
	Text string
}

// Call is one question.
type Call struct {
	Owner, Repo, SHA string
	UserLogin        string
	// System is the instructions given to the model.
	System   string
	History  []Message
	Question string
	// Where says, in the service's own words, what the reader has open: a
	// pull request, a file. It is sent ahead of the question, so that "this
	// file" means something to the model. Empty when there is nothing to say.
	Where string
	// Selection is the code the reader selected, if any.
	Selection *Segment
	// File returns the whole text of a file at the commit. The gateway scans
	// the whole file behind any lines it sends, because a credential can lie
	// just outside the lines quoted, or straddle them.
	File func(path string) (text string, ok bool)
}

// Tool is something the model may ask to have run.
type Tool struct {
	Spec model.ToolSpec
	// Run does it. A returned error is told to the model as the tool's result,
	// and the question goes on: a tool asked for a file that is not there has
	// not failed the question.
	Run func(ctx context.Context, argumentsJSON string) ([]Segment, error)
}

// Event is something that happened while a question was being answered.
type Event interface{ isEvent() }

// Delta is more of the answer.
type Delta struct{ Text string }

// Excerpt is a run of lines the model was given.
type Excerpt struct {
	Path               string
	StartLine, EndLine int
}

// Withheld is content the scan held back from the model.
type Withheld struct {
	What   string
	Reason string
}

// Waiting says the question is sitting out a wait the provider named.
type Waiting struct {
	Seconds int
	Reason  string
}

func (Delta) isEvent()    {}
func (Excerpt) isEvent()  {}
func (Withheld) isEvent() {}
func (Waiting) isEvent()  {}

// Result is how a question ended.
type Result struct {
	Outcome string
	// Message is what the reader is told, for every outcome but OutcomeOK.
	Message  string
	Withheld []Withheld
	// Model is the model that answered, as configured.
	Model string

	InputTokens     int
	OutputTokens    int
	ReasoningTokens int
	ModelCalls      int

	// RetryAfterSeconds and ResetAt are set for a limit the provider named.
	RetryAfterSeconds int
	ResetAt           string
}

// Limits bound one question.
type Limits struct {
	// ModelCalls is the most calls to the model.
	ModelCalls int
	// QuestionTokens is the most input tokens, as the provider counts them,
	// summed over the question's calls.
	QuestionTokens int
	// AnswerTokens is the output ceiling for one call, reasoning included.
	AnswerTokens int
	// CallTimeout bounds one call.
	CallTimeout time.Duration
	// MaxWait is the longest provider-named wait that is sat out.
	MaxWait time.Duration
}

// maxToolCalls is the most tools run for one model call. A model that asks
// for more is told so for the rest.
const maxToolCalls = 16

// Scanner checks text for credentials.
type Scanner interface {
	Check(ctx context.Context, items []string) ([]scan.Result, error)
}

// Target is where one repository's questions go.
type Target struct {
	Provider  string
	Model     string
	Converser model.Converser
}

// Repository is one repository's provider settings, with its key.
type Repository struct {
	Owner, Name string
	Provider    string
	Model       string
	BaseURL     string
	Key         string
}

// Connect builds each repository's target. It is the one place in the
// workspace that makes a connection to a provider.
func Connect(repos []Repository) (map[string]Target, error) {
	targets := map[string]Target{}
	for _, r := range repos {
		converser, err := model.NewConverser(r.Provider, r.BaseURL, r.Key)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", r.Owner, r.Name, err)
		}
		targets[slug(r.Owner, r.Name)] = Target{Provider: r.Provider, Model: r.Model, Converser: converser}
	}
	return targets, nil
}

func slug(owner, name string) string { return strings.ToLower(owner) + "/" + strings.ToLower(name) }

// Gateway answers questions.
type Gateway struct {
	targets map[string]Target
	scanner Scanner
	limits  Limits
	log     *obs.Logger

	// now and sleep are replaced in tests.
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
}

// New returns a Gateway. targets is keyed as Connect keys it.
func New(targets map[string]Target, scanner Scanner, limits Limits, log *obs.Logger) *Gateway {
	return &Gateway{
		targets: targets, scanner: scanner, limits: limits, log: log,
		now: time.Now,
		sleep: func(ctx context.Context, d time.Duration) error {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-timer.C:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
}

// Model returns the model configured for a repository, and whether it has an
// assistant at all.
func (g *Gateway) Model(owner, repo string) (string, bool) {
	target, ok := g.targets[slug(owner, repo)]
	return target.Model, ok
}

// question is the state of one question as it is answered.
type question struct {
	g       *Gateway
	call    Call
	target  Target
	tools   map[string]Tool
	specs   []model.ToolSpec
	onEvent func(Event)

	turns    []model.Turn
	result   Result
	demanded bool // the one retry for heavy demand has been used
	waited   bool // the one wait for a rate limit has been used
}

// Answer runs the whole of one question: it scans what is to be sent, calls
// the provider, runs the tools the model asks for, scans each tool's result
// before it is sent, applies the limits and the waits, writes one log line
// for each model call, and returns how the question ended.
//
// It never returns an error. Every way a question can end is an outcome.
func (g *Gateway) Answer(ctx context.Context, call Call, tools []Tool, onEvent func(Event)) Result {
	target, ok := g.targets[slug(call.Owner, call.Repo)]
	if !ok {
		return Result{Outcome: OutcomeProviderError, Message: "This repository has no assistant configured."}
	}
	q := &question{g: g, call: call, target: target, tools: map[string]Tool{}, onEvent: onEvent}
	q.result.Model = target.Model
	for _, tool := range tools {
		q.tools[tool.Spec.Name] = tool
		q.specs = append(q.specs, tool.Spec)
	}

	if stop := q.scanWhatTheReaderSent(ctx); stop != nil {
		return q.end(ctx, *stop)
	}
	for {
		if stop := q.step(ctx); stop != nil {
			return q.end(ctx, *stop)
		}
	}
}

// end fills in the totals and returns the result.
func (q *question) end(ctx context.Context, result Result) Result {
	if ctx.Err() != nil && result.Outcome != OutcomeOK {
		result.Outcome, result.Message = OutcomeAbandoned, ""
	}
	result.Model = q.target.Model
	result.Withheld = q.result.Withheld
	result.InputTokens, result.OutputTokens = q.result.InputTokens, q.result.OutputTokens
	result.ReasoningTokens, result.ModelCalls = q.result.ReasoningTokens, q.result.ModelCalls
	if result.ModelCalls == 0 {
		// Nothing was sent, so there is no model-call line. This is the
		// question's only record.
		q.g.log.Event(ctx, slog.LevelInfo, "question_not_sent", q.attrs(slog.String("outcome", result.Outcome))...)
	}
	return result
}

func (q *question) attrs(more ...slog.Attr) []slog.Attr {
	return append([]slog.Attr{
		slog.String("user", q.call.UserLogin), slog.String("owner", q.call.Owner), slog.String("repo", q.call.Repo),
		slog.String("sha", q.call.SHA), slog.String("provider", q.target.Provider), slog.String("model", q.target.Model),
	}, more...)
}

// scanWhatTheReaderSent scans the question, the earlier questions and answers,
// and the selection with the whole file it comes from. A credential in any of
// them stops the question before anything is sent.
func (q *question) scanWhatTheReaderSent(ctx context.Context) *Result {
	type item struct {
		text    string
		blocked string // what the reader is told if this item is flagged
	}
	items := []item{{q.call.Question, "Your question holds what looks like a credential"}}
	for _, m := range q.call.History {
		items = append(items, item{m.Text, "An earlier question or answer in this conversation holds what looks like a credential"})
	}
	if q.call.Where != "" {
		// It holds a file's name, which is the repository's content like any other.
		items = append(items, item{q.call.Where, "The name of the file you have open holds what looks like a credential"})
	}
	if s := q.call.Selection; s != nil {
		where := "The selected code"
		if s.Path != "" {
			where = "The file the selected code comes from, " + s.Path + ","
		}
		items = append(items, item{s.Text, where + " holds what looks like a credential"})
		if whole, ok := q.file(s.Path); ok {
			items = append(items, item{whole, where + " holds what looks like a credential"})
		}
	}

	texts := make([]string, len(items))
	for i := range items {
		texts[i] = items[i].text
	}
	results, err := q.g.scanner.Check(ctx, texts)
	if err != nil {
		return &Result{Outcome: OutcomeScanFailed, Message: "The secrets scan could not be run, so nothing was sent to the model provider."}
	}
	for i, result := range results {
		if result.Flagged {
			return &Result{
				Outcome: OutcomeSecretBlocked,
				Message: items[i].blocked + " (" + strings.Join(result.Rules, ", ") + "). Nothing was sent to the model provider.",
			}
		}
	}

	// Clear to send. Build the conversation: what was said before, as plain
	// text, then where the reader is, the question, and the selected lines.
	for _, m := range q.call.History {
		role := model.RoleUser
		if m.Role == "assistant" {
			role = model.RoleAssistant
		}
		q.turns = append(q.turns, model.Turn{Role: role, Parts: []model.Part{model.Text{Text: m.Text}}})
	}
	text := q.call.Question
	if q.call.Where != "" {
		text = q.call.Where + "\n\n" + text
	}
	if s := q.call.Selection; s != nil {
		text += "\n\n" + selectionHeading(*s) + "\n```\n" + strings.TrimRight(s.Text, "\n") + "\n```"
		if s.Path != "" {
			q.onEvent(Excerpt{Path: s.Path, StartLine: s.StartLine, EndLine: s.EndLine})
		}
	}
	q.turns = append(q.turns, model.Turn{Role: model.RoleUser, Parts: []model.Part{model.Text{Text: text}}})
	return nil
}

func selectionHeading(s Segment) string {
	switch {
	case s.Path == "":
		return "The reader selected this:"
	case s.StartLine > 0 && s.EndLine >= s.StartLine:
		return fmt.Sprintf("The reader selected %s, lines %d to %d:", s.Path, s.StartLine, s.EndLine)
	default:
		return fmt.Sprintf("The reader selected this, from %s:", s.Path)
	}
}

func (q *question) file(path string) (string, bool) {
	if path == "" || q.call.File == nil {
		return "", false
	}
	return q.call.File(path)
}

// step makes one model call and deals with what comes back. It returns nil
// when the question goes on, and the result when it has ended.
func (q *question) step(ctx context.Context) *Result {
	limits := q.g.limits
	if q.result.ModelCalls >= limits.ModelCalls {
		return &Result{Outcome: OutcomeLimitReached,
			Message: fmt.Sprintf("The assistant stopped at its limit of %d model calls for one question.", limits.ModelCalls)}
	}
	if q.result.InputTokens >= limits.QuestionTokens {
		return &Result{Outcome: OutcomeLimitReached,
			Message: fmt.Sprintf("The assistant stopped at its limit of %d input tokens for one question.", limits.QuestionTokens)}
	}

	var (
		parts []model.Part
		text  strings.Builder
		usage model.Usage
		end   model.End
	)
	started := q.g.now()
	q.result.ModelCalls++
	err := q.target.Converser.Converse(ctx, model.ConvRequest{
		Model: q.target.Model, System: q.call.System, Turns: q.turns, Tools: q.specs,
		MaxOutputTokens: limits.AnswerTokens, Timeout: limits.CallTimeout,
	}, func(ev model.Event) {
		switch e := ev.(type) {
		case model.TextDelta:
			text.WriteString(e.Text)
			q.onEvent(Delta{Text: e.Text})
		case model.PartDone:
			// Kept in the order received, reasoning data included, to be sent
			// back with the tools' results. Reasoning data goes nowhere else:
			// it is in no event and no log line.
			parts = append(parts, e.Part)
		case model.Usage:
			usage = e
		case model.End:
			end = e
		}
	})
	q.result.InputTokens += usage.InputTokens
	q.result.OutputTokens += usage.OutputTokens
	q.result.ReasoningTokens += usage.ReasoningTokens

	line := func(outcome string, more ...slog.Attr) {
		q.g.log.Event(ctx, slog.LevelInfo, "model_call", q.attrs(append([]slog.Attr{
			slog.Int("call", q.result.ModelCalls), slog.String("outcome", outcome),
			slog.Int("input_tokens", usage.InputTokens), slog.Int("output_tokens", usage.OutputTokens),
			slog.Int("reasoning_tokens", usage.ReasoningTokens),
			slog.Int64("duration_ms", q.g.now().Sub(started).Milliseconds()),
		}, more...)...)...)
	}

	if err != nil {
		var failure *model.ProviderError
		if !errors.As(err, &failure) {
			failure = &model.ProviderError{Message: "the call failed"}
		}
		line("error", slog.Int("status", failure.Status), slog.String("code", failure.Code))
		return q.afterFailure(ctx, failure)
	}

	switch end.Reason {
	case model.EndStop:
		line("answered")
		return &Result{Outcome: OutcomeOK}
	case model.EndMaxTokens:
		line("cut_off")
		return &Result{Outcome: OutcomeLimitReached,
			Message: fmt.Sprintf("The answer was cut off at the limit of %d output tokens for one model call.", limits.AnswerTokens)}
	case model.EndToolUse:
		// Below.
	default:
		line("refused", slog.String("detail", end.Detail))
		return &Result{Outcome: OutcomeRefused, Message: "The model provider declined to answer."}
	}

	// The model asked for tools. Its turn is everything it sent, in order.
	turn := model.Turn{Role: model.RoleAssistant, Parts: parts}
	if text.Len() > 0 {
		turn.Parts = append(turn.Parts, model.Text{Text: text.String()})
	}
	back, scanned, stop := q.runTools(ctx, parts)
	line("tool_use", slog.String("scan", scanned.summary()), slog.String("withheld", strings.Join(scanned.withheld, " ")))
	if stop != nil {
		return stop
	}
	q.turns = append(q.turns, turn, model.Turn{Role: model.RoleUser, Parts: back})
	return nil
}

// afterFailure decides what a failed call leads to: one more try, or an end.
func (q *question) afterFailure(ctx context.Context, failure *model.ProviderError) *Result {
	if ctx.Err() != nil {
		return &Result{Outcome: OutcomeAbandoned}
	}
	switch classify(failure) {
	case kindLimited:
		// A wait the provider names, within what the service will sit out, is
		// sat out once. The reader is told.
		if failure.HasRetryAfter && failure.RetryAfter <= q.g.limits.MaxWait && !q.waited {
			q.waited = true
			seconds := int(failure.RetryAfter.Round(time.Second)/time.Second) + 1
			q.onEvent(Waiting{Seconds: seconds, Reason: "The model provider is limiting requests."})
			if q.g.sleep(ctx, time.Duration(seconds)*time.Second) != nil {
				return &Result{Outcome: OutcomeAbandoned}
			}
			q.result.ModelCalls-- // the refused call does not count against the limit
			return nil
		}
		result := limited(failure, q.g.now())
		return &result
	case kindDemand:
		if !q.demanded {
			q.demanded = true
			q.onEvent(Waiting{Seconds: int(demandWait / time.Second), Reason: "The model provider is under heavy demand."})
			if q.g.sleep(ctx, demandWait) != nil {
				return &Result{Outcome: OutcomeAbandoned}
			}
			q.result.ModelCalls--
			return nil
		}
	}
	result := failed(failure)
	return &result
}

// scanned says what the scan of one round of tool results found, for the log.
type scanned struct {
	failed   bool
	withheld []string
}

func (s scanned) summary() string {
	switch {
	case s.failed:
		return "failed"
	case len(s.withheld) > 0:
		return fmt.Sprintf("withheld:%d", len(s.withheld))
	default:
		return "clean"
	}
}

// runTools runs each tool the model asked for and scans what they returned.
// What the scan flags is withheld from the model, by file, and named to the
// reader. If the scanner cannot run, nothing more is sent.
func (q *question) runTools(ctx context.Context, parts []model.Part) ([]model.Part, scanned, *Result) {
	type ran struct {
		call     model.ToolCall
		segments []Segment
		err      error
	}
	var runs []ran
	for _, part := range parts {
		call, ok := part.(model.ToolCall)
		if !ok {
			continue
		}
		switch tool, known := q.tools[call.Name]; {
		case len(runs) >= maxToolCalls:
			runs = append(runs, ran{call: call, err: fmt.Errorf("too many tools were asked for at once; ask for at most %d", maxToolCalls)})
		case !known:
			runs = append(runs, ran{call: call, err: fmt.Errorf("there is no tool named %q", call.Name)})
		default:
			segments, err := tool.Run(ctx, call.ArgumentsJSON)
			runs = append(runs, ran{call: call, segments: segments, err: err})
		}
	}

	// One scan for the whole round: each segment's own text, and the whole
	// file behind it.
	var texts []string
	type place struct{ run, segment int }
	var owner []place
	for r, run := range runs {
		for s, segment := range run.segments {
			texts = append(texts, segment.Text)
			owner = append(owner, place{r, s})
			if whole, ok := q.file(segment.Path); ok {
				texts = append(texts, whole)
				owner = append(owner, place{r, s})
			}
		}
	}
	var found scanned
	flagged := map[place][]string{}
	if len(texts) > 0 {
		results, err := q.g.scanner.Check(ctx, texts)
		if err != nil {
			found.failed = true
			return nil, found, &Result{Outcome: OutcomeScanFailed,
				Message: "The secrets scan could not be run on what the assistant read, so nothing more was sent to the model provider."}
		}
		for i, result := range results {
			if result.Flagged {
				flagged[owner[i]] = append(flagged[owner[i]], result.Rules...)
			}
		}
	}

	var back []model.Part
	told := map[string]bool{}
	for r, run := range runs {
		if run.err != nil {
			back = append(back, model.ToolResult{CallID: run.call.ID, Name: run.call.Name, Text: run.err.Error(), IsError: true})
			continue
		}
		var text strings.Builder
		for s, segment := range run.segments {
			if text.Len() > 0 {
				text.WriteString("\n")
			}
			what := segment.Path
			if what == "" {
				what = segment.Name
			}
			if _, bad := flagged[place{r, s}]; bad {
				// Withheld by file: every segment from a flagged file is held
				// back, because the scan judged the file and not the lines.
				text.WriteString("[Withheld: " + what + ". The workspace's secrets scan found what looks like a credential there, " +
					"so its content was not given to you. Tell the reader you could not read it.]")
				if !told[what] {
					told[what] = true
					held := Withheld{What: what, Reason: "The secrets scan found what looks like a credential there, so it was not sent to the model provider."}
					q.result.Withheld = append(q.result.Withheld, held)
					found.withheld = append(found.withheld, what)
					q.onEvent(held)
				}
				continue
			}
			text.WriteString(segment.Text)
			if segment.Path != "" && segment.StartLine > 0 {
				q.onEvent(Excerpt{Path: segment.Path, StartLine: segment.StartLine, EndLine: segment.EndLine})
			}
		}
		if text.Len() == 0 {
			text.WriteString("(nothing)")
		}
		back = append(back, model.ToolResult{CallID: run.call.ID, Name: run.call.Name, Text: text.String()})
	}
	return back, found, nil
}
