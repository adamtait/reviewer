// SPDX-License-Identifier: MIT

package builtin

import (
	"context"
	"fmt"

	"github.com/adamtait/reviewer/internal/diff"
	"github.com/adamtait/reviewer/internal/laneb"
	"github.com/adamtait/reviewer/internal/model"
	"github.com/adamtait/reviewer/pkg/finding"
	"github.com/adamtait/reviewer/pkg/plugin"
)

// ModelLaneID is the model lane's analyzer id, for --only and --skip.
const ModelLaneID = "model-review"

// ModelLaneOrder puts it last. It is the slowest thing here and the only thing
// that sends anything anywhere.
const ModelLaneOrder = 900

// modelLane runs the review and invalidation passes.
//
// It is an analyzer like any other (ADR-0006), and that is the point: the
// sequencer runs the deterministic analyzers, asks the secrets gate, and only then
// runs the model lane. Declaring this analyzer's lane as `llm` is what puts it on
// the far side of that gate — so "no diff leaves the machine when a credential was
// found" is a property of where this sits rather than of a check inside it
// (ADR-0012).
func (h *Handler) modelLane(ctx context.Context, req plugin.AnalyzeRequest) ([]finding.Finding, []string, error) {
	provider, err := model.New(h.cfg, h.secrets)
	if err != nil {
		// Every way of not being ready is a skip with a reason, never a failure.
		return nil, []string{model.Describe(err)}, nil
	}

	if req.Base == "" && !req.Staged {
		// No base means the changed-file list came from the API rather than from
		// git — a pull request whose base commit this checkout does not have. There
		// is no diff to send that corresponds to what the gate scanned, and sending
		// a different one is worse than sending none (ADR-0012).
		return nil, []string{"the model lane needs the base commit in the checkout; " +
			"add `fetch-depth: 0` to actions/checkout"}, nil
	}

	changed := fromPlugin(req.Changed)
	in, warnings := laneb.Gather(ctx, h.cfg, req.Base, req.Staged, changed)
	in.Findings = req.Prior

	reviewer := laneb.Reviewer{
		Provider:  provider,
		Model:     h.cfg.LaneB.Model,
		Root:      h.cfg.Root,
		PromptDir: h.cfg.LaneB.PromptDir,
		Changed:   changedSet(changed),
	}

	candidates, reviewWarnings, err := reviewer.Review(ctx, in)
	warnings = append(warnings, reviewWarnings...)
	if err != nil {
		return nil, warnings, err
	}
	if !h.cfg.LaneB.Invalidate {
		// `--no-invalidate` is a debugging setting, and a run made under it must
		// say so: its findings have not been through the pass the lane's precision
		// depends on (ADR-0023).
		if len(candidates) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"%d model finding(s) were not checked: invalidation is switched off", len(candidates)))
		}
		return candidates, warnings, nil
	}

	kept, invalidateWarnings, err := reviewer.Invalidate(ctx, in, candidates)
	warnings = append(warnings, invalidateWarnings...)
	return kept, warnings, err
}

// fromPlugin converts the protocol's file shape back for the assembler.
//
// The status is carried on the wire now rather than assumed. Flattening every file
// to "modified" was not merely vague: staleDocs skips deleted files, so with the
// status wrong it never did, and a document referencing a file the change deleted
// was offered as one the change "may have invalidated".
func fromPlugin(files []plugin.ChangedFile) []diff.File {
	out := make([]diff.File, 0, len(files))
	for _, f := range files {
		out = append(out, diff.File{
			Path:   f.Path,
			Status: diff.Status(f.Status),
			Ranges: f.Ranges,
		})
	}
	return out
}

func changedSet(files []diff.File) map[string]bool {
	set := make(map[string]bool, len(files))
	for _, f := range files {
		set[f.Path] = true
	}
	return set
}
