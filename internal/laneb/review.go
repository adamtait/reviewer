// SPDX-License-Identifier: MIT

package laneb

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"github.com/adamtait/reviewer/internal/model"
	"github.com/adamtait/reviewer/pkg/finding"
)

// prompts ship with the binary. A destination repository can override any of them
// by putting a file of the same name in its prompt directory, which is where
// wording specific to one codebase belongs — this repository's copies stay generic
// enough to publish (ADR-0010).
//
//go:embed prompts/*.md
var prompts embed.FS

// Reviewer runs the model lane over one change.
type Reviewer struct {
	// Provider answers. Never constructed here: the caller decides whether the
	// lane runs at all, and it must be possible to see that decision in one place.
	Provider model.Provider
	// Model is the identifier the destination chose.
	Model string
	// Root is the repository, for loading prompt overrides.
	Root string
	// PromptDir is where overrides live, relative to Root.
	PromptDir string
	// MaxTokens bounds the reply. Zero means the provider's default.
	MaxTokens int
	// Changed is the set of paths this change touches, used to refuse a prompt
	// override the change itself introduces.
	Changed map[string]bool
}

// overridden reports whether the change under review edits this prompt.
//
// A prompt override is a file in the repository, so a pull request can add one —
// and a system prompt supplied by the branch being reviewed is a branch reviewing
// itself. An override that was already there is the repository's own decision and
// is honoured; one that arrives in the diff is ignored.
func (r Reviewer) overridden(name string) bool {
	return r.Changed[strings.TrimPrefix(r.PromptDir+"/"+name, "./")]
}

// response is what the model is asked to return.
type response struct {
	Findings []candidate `json:"findings"`
}

// candidate is a finding as the model states it, before this package decides what
// it is allowed to claim.
type candidate struct {
	RuleID     string `json:"ruleId"`
	Confidence string `json:"confidence"`
	Severity   string `json:"severity"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	EndLine    int    `json:"endLine"`
	Message    string `json:"message"`
	Evidence   string `json:"evidence"`
	Suggestion string `json:"suggestion"`
}

// Review asks for findings and returns the ones this package is willing to stand
// behind.
//
// Every rejection is a warning rather than a silent drop. A lane that quietly
// discards half of what it is told looks identical to one that found nothing, and
// the difference matters when deciding whether the lane earns its cost.
func (r Reviewer) Review(ctx context.Context, in Input) ([]finding.Finding, []string, error) {
	system, err := r.prompt("review.md", nil)
	if err != nil {
		return nil, nil, err
	}

	reply, err := r.Provider.Complete(ctx, []model.Message{
		{Role: model.RoleSystem, Content: system},
		{Role: model.RoleUser, Content: Assemble(in)},
	}, model.Options{
		Model: r.Model,
		// A review wants the same answer twice. This is the one setting where a
		// default other than the provider's matters.
		Temperature: 0,
		MaxTokens:   r.MaxTokens,
		JSON:        true,
	})
	if err != nil {
		// Every provider failure is a warning, never a failed analyzer. A failed
		// analyzer makes the whole run untrustworthy, which suppresses stale-thread
		// resolution in the GitHub reporter — so an unreachable model would change
		// what this tool does to comments a human is reading (ADR-0013).
		//
		// The invalidation pass already worked this way; this one did not, which
		// meant a dead endpoint and a refusal were reported as different kinds of
		// event when they are the same kind: the lane did not run.
		return nil, []string{"the model lane found nothing because it could not run: " + err.Error()}, nil
	}

	return r.parse(reply, in)
}

// parse turns a reply into findings, discarding anything that does not survive.
func (r Reviewer) parse(reply string, in Input) ([]finding.Finding, []string, error) {
	var warnings []string

	var parsed response
	if err := json.Unmarshal([]byte(extractJSON(reply)), &parsed); err != nil {
		// Not an error the run fails on: a malformed reply is a lane that found
		// nothing this time, and the deterministic lane is unaffected (ADR-0013).
		return nil, []string{fmt.Sprintf("the model's reply was not the requested JSON: %v", err)}, nil
	}

	out := make([]finding.Finding, 0, len(parsed.Findings))
	for i, c := range parsed.Findings {
		f := finding.Finding{
			RuleID: strings.TrimSpace(c.RuleID),
			// Declared here, not taken from the reply. A model claiming to be the
			// deterministic lane would bypass every rule that applies to this one.
			Lane:       finding.LaneLLM,
			Confidence: confidence(c.Confidence),
			Severity:   severity(c.Severity),
			File:       strings.TrimSpace(c.File),
			Line:       c.Line,
			EndLine:    c.EndLine,
			Message:    strings.TrimSpace(c.Message),
			Evidence:   strings.TrimSpace(c.Evidence),
			Suggestion: strings.TrimSpace(c.Suggestion),
		}

		capped, known := Cap(f)
		if !known {
			warnings = append(warnings, fmt.Sprintf(
				"findings[%d]: %q is not a category this lane reports; discarded", i, f.RuleID))
			continue
		}
		if err := capped.Validate(); err != nil {
			warnings = append(warnings, fmt.Sprintf("findings[%d] (%s): discarded: %v", i, f.RuleID, err))
			continue
		}
		placed, warning := r.place(capped, in)
		if warning != "" {
			warnings = append(warnings, warning)
			continue
		}
		out = append(out, placed)
	}
	return out, warnings, nil
}

// place puts a finding somewhere a reader will see it, or refuses it.
//
// A finding whose file and line are outside the diff is dropped by the core
// (ADR-0007) — silently, from this package's point of view, which is how three
// analyzers in earlier milestones came to report into a void. The model is asked
// to report on changed lines and mostly does; what it cannot do is report a
// documentation problem on a changed line, because the document is by construction
// one the change did not touch.
//
// So `docs/stale` is relocated onto a line of the code that made the document
// wrong, carrying the document's path in the message. Everything else that lands
// outside the diff is refused with a warning naming it, because a `correctness/bug`
// about a line this change did not touch is a finding about somebody else's work.
func (r Reviewer) place(f finding.Finding, in Input) (finding.Finding, string) {
	if onChangedLine(in, f.File, f.Line) {
		return f, ""
	}

	if f.RuleID == "docs/stale" {
		for _, doc := range in.Docs {
			if doc.Path != f.File {
				continue
			}
			for _, ref := range doc.References {
				if line, ok := firstChangedLine(in, ref); ok {
					f.Message = fmt.Sprintf("`%s` may now be wrong. %s", f.File, f.Message)
					f.File, f.Line, f.EndLine = ref, line, 0
					return f, ""
				}
			}
		}
	}

	return f, fmt.Sprintf(
		"%s at %s:%d is outside this change and was discarded", f.RuleID, f.File, f.Line)
}

func onChangedLine(in Input, file string, line int) bool {
	for _, f := range in.Changed {
		if f.Path != file {
			continue
		}
		for _, r := range f.Ranges {
			if line >= r[0] && line <= r[1] {
				return true
			}
		}
	}
	return false
}

func firstChangedLine(in Input, file string) (int, bool) {
	for _, f := range in.Changed {
		if f.Path == file && len(f.Ranges) > 0 {
			return f.Ranges[0][0], true
		}
	}
	return 0, false
}

// prompt loads a prompt, preferring the destination repository's override.
//
// The data a prompt can reference is passed in rather than assembled here, because
// a placeholder this function does not know about renders as `<no value>` and
// disappears — silently, and looking exactly like a prompt that never had it.
func (r Reviewer) prompt(name string, data map[string]string) (string, error) {
	body, err := r.promptBody(name)
	if err != nil {
		return "", err
	}
	tmpl, err := template.New(name).Parse(body)
	if err != nil {
		return "", fmt.Errorf("prompt %s: %w", name, err)
	}
	if data == nil {
		data = map[string]string{}
	}
	if _, ok := data["Categories"]; !ok {
		data["Categories"] = CategoryList()
	}
	// Missing keys are an error rather than `<no value>`: a prompt missing the
	// thing it is about would otherwise be sent, and answered.
	tmpl = tmpl.Option("missingkey=error")

	out := &strings.Builder{}
	if err := tmpl.Execute(out, data); err != nil {
		return "", fmt.Errorf("prompt %s: %w", name, err)
	}
	return out.String(), nil
}

func (r Reviewer) promptBody(name string) (string, error) {
	if r.Root != "" && r.PromptDir != "" && !r.overridden(name) {
		if body, err := readUnder(r.Root, r.PromptDir+"/"+name); err == nil {
			return body, nil
		}
	}
	body, err := prompts.ReadFile("prompts/" + name)
	if err != nil {
		return "", fmt.Errorf("prompt %s: %w", name, err)
	}
	return string(body), nil
}

// extractJSON pulls the object out of a reply that wrapped it.
//
// The prompt asks for bare JSON and most replies are, but a fenced block or a
// sentence of preamble is common enough that refusing them would throw away good
// findings over presentation. The first `{` to the last `}` is enough: the reply is
// one object, so anything outside those is not part of it.
func extractJSON(reply string) string {
	start := strings.IndexByte(reply, '{')
	end := strings.LastIndexByte(reply, '}')
	if start < 0 || end <= start {
		return reply
	}
	return reply[start : end+1]
}

// confidence and severity fall back rather than reject.
//
// Downwards, always: an unrecognised confidence becomes the lowest and an
// unrecognised severity the mildest, so a reply this package does not fully
// understand cannot claim more than one it does.
func confidence(s string) finding.Confidence {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high":
		return finding.ConfidenceHigh
	case "medium":
		return finding.ConfidenceMedium
	default:
		return finding.ConfidenceLow
	}
}

func severity(s string) finding.Severity {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "error":
		return finding.SeverityError
	case "warning":
		return finding.SeverityWarning
	default:
		return finding.SeverityInfo
	}
}
