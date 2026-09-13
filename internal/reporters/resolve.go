// SPDX-License-Identifier: MIT

package reporters

import (
	"context"
	"fmt"

	"github.com/adamtait/reviewer/internal/fingerprint"
	"github.com/adamtait/reviewer/internal/ghclient"
)

// resolveStale closes threads for findings that are no longer reported.
//
// This is the only action the tool takes on a conversation, which is why it is
// opt-in and why it is fenced by three conditions: the thread must carry one of
// our fingerprint markers, its first comment must have been written by the token's
// own identity, and it must not already be resolved. A thread a human started, or
// replied to and resolved their own way, is never touched.
func (g GitHub) resolveStale(ctx context.Context, current map[string]bool) (resolved int, err error) {
	viewer, err := g.Client.Viewer(ctx)
	if err != nil {
		// Without knowing who we are, we cannot tell our own threads from a
		// human's, and resolving the wrong one is not recoverable by the tool.
		return 0, fmt.Errorf("identifying the token's own account: %w", err)
	}

	threads, err := g.Writer.ReviewThreads(ctx, g.Repo, g.Number)
	if err != nil {
		return 0, err
	}

	for _, thread := range threads {
		if thread.IsResolved || len(thread.Comments) == 0 {
			continue
		}
		first := thread.Comments[0]

		fp := fingerprint.ParseMarker(first.Body)
		if fp == "" {
			continue // a human's thread
		}
		if first.User.Login != viewer.Login {
			// Marked as ours but written by someone else — a quoted comment, or a
			// human who copied the body. Leave it alone.
			continue
		}
		if current[fp] {
			continue // still reported, so still relevant
		}

		if g.DryRun {
			resolved++
			continue
		}
		if err := g.Writer.ResolveReviewThread(ctx, thread.ID); err != nil {
			// One failure must not stop the rest, and is not worth failing a run
			// whose comments were posted successfully.
			return resolved, fmt.Errorf("resolving a thread for %s: %w", fp, err)
		}
		resolved++
	}
	return resolved, nil
}

// currentFingerprints is the set of identities this run reported, which is what
// "no longer reported" is measured against.
func currentFingerprints(run Run) map[string]bool {
	out := make(map[string]bool, len(run.Findings))
	for _, f := range run.Findings {
		out[f.Fingerprint] = true
	}
	return out
}

var _ = ghclient.ReviewThread{} // keep the dependency explicit for readers
