// SPDX-License-Identifier: MIT

package laneb

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
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
	system, err := r.prompt("review.md")
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
	if errors.Is(err, model.ErrRefused) {
		return nil, []string{"the model declined to review this change: " + err.Error()}, nil
	}
	if err != nil {
		return nil, nil, err
	}

	return r.parse(reply)
}

// parse turns a reply into findings, discarding anything that does not survive.
func (r Reviewer) parse(reply string) ([]finding.Finding, []string, error) {
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
		out = append(out, capped)
	}
	return out, warnings, nil
}

// prompt loads a prompt, preferring the destination repository's override.
func (r Reviewer) prompt(name string) (string, error) {
	body, err := r.promptBody(name)
	if err != nil {
		return "", err
	}
	tmpl, err := template.New(name).Parse(body)
	if err != nil {
		return "", fmt.Errorf("prompt %s: %w", name, err)
	}
	out := &strings.Builder{}
	if err := tmpl.Execute(out, map[string]string{"Categories": CategoryList()}); err != nil {
		return "", fmt.Errorf("prompt %s: %w", name, err)
	}
	return out.String(), nil
}

func (r Reviewer) promptBody(name string) (string, error) {
	if r.Root != "" && r.PromptDir != "" {
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
