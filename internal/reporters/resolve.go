// SPDX-License-Identifier: MIT

package reporters

import (
	"context"
	"fmt"

	"github.com/adamtait/reviewer/internal/fingerprint"
)

// resolveStale closes threads for findings that are no longer reported.
//
// This is the only action the tool takes on a conversation, which is why it is
// opt-in and why it is fenced by three conditions: the thread must carry one of
// our fingerprint markers, its first comment must have been written by the token's
// own identity, and it must not already be resolved. A thread a human started, or
// replied to and resolved their own way, is never touched.
func (g GitHub) resolveStale(ctx context.Context, current map[string]bool) (resolved int, problems []error) {
	self, err := g.identify(ctx)
	if err != nil {
		// Without knowing who we are, our threads and a human's are
		// indistinguishable, and resolving the wrong one is not something the tool
		// can undo. Skip rather than guess.
		return 0, []error{err}
	}

	threads, err := g.Writer.ReviewThreads(ctx, g.Repo, g.Number)
	if err != nil {
		return 0, []error{err}
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
		if first.User.Login != self {
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
			problems = append(problems, fmt.Errorf("resolving a thread for %s: %w", fp, err))
			continue
		}
		resolved++
	}
	return resolved, problems
}

// identify returns the login this tool posts as.
//
// GitHub's /user endpoint answers this for a personal access token and returns 403
// for the installation token an Action runs with — which is the token the composite
// action uses, so the API alone cannot be relied on. A configured login is used
// when the API refuses, and when neither is available resolution is skipped.
func (g GitHub) identify(ctx context.Context) (string, error) {
	viewer, err := g.Client.Viewer(ctx)
	if err == nil && viewer.Login != "" {
		return viewer.Login, nil
	}
	if g.SelfLogin != "" {
		return g.SelfLogin, nil
	}
	return "", fmt.Errorf(
		"cannot identify the account this token posts as (%v); set github.selfLogin to enable resolving stale threads", err)
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
