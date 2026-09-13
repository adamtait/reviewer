// SPDX-License-Identifier: MIT

package reporters

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/adamtait/reviewer/internal/fingerprint"
	"github.com/adamtait/reviewer/internal/ghclient"
	"github.com/adamtait/reviewer/pkg/finding"
)

// GitHub posts findings as inline review comments on a pull request.
//
// Two rules decide what it does, and both are about not being muted:
//
//   - Only `high` confidence goes inline (ADR-0017). Everything else belongs in
//     the collapsed summary, because an inline comment is a claim that the
//     reviewer should stop and look.
//   - Nothing already said is said again. Every comment carries an invisible
//     fingerprint marker, and a run reads the existing comments first (ADR-0015,
//     ADR-0016).
type GitHub struct {
	Client ghclient.Client
	Writer ghclient.Writer
	Repo   ghclient.Repo
	Number int
	// HeadSHA is the revision comments attach to. GitHub rejects a stale one
	// rather than silently attaching to the wrong revision.
	HeadSHA string
	// DryRun prints what would be posted and posts nothing. The default for any
	// invocation a human is watching.
	DryRun bool
	// ResolveStale closes threads whose finding is no longer reported. Opt-in:
	// resolving a thread is the only action this tool takes on a human's
	// conversation (ADR-0020).
	ResolveStale bool
	// SelfLogin is the account this tool posts as, when the API cannot be asked.
	// The installation token an Action runs with gets 403 from /user.
	SelfLogin string
	// Out receives the dry-run payload and the posting summary.
	Out io.Writer
	// Log receives warnings and skips, which have no place in a pull request.
	Log io.Writer
}

func (g GitHub) Name() string { return "github" }

func (g GitHub) Report(ctx context.Context, run Run) error {
	out := g.Out
	if out == nil {
		out = io.Discard
	}
	if g.Log != nil {
		for _, w := range run.Warnings {
			fmt.Fprintf(g.Log, "warning: %s\n", w)
		}
		for _, s := range run.Skipped {
			fmt.Fprintf(g.Log, "skipped: %s\n", s)
		}
	}

	inline, deferred := partition(run.Findings)

	// Read before writing. Without this the tool reposts everything on every
	// push, which is the failure that makes a reviewer bot intolerable.
	//
	// Only *inline* comments suppress an inline post. A finding listed in the
	// summary must still be able to become an inline comment: promoting a category
	// once its acceptance justifies it is the intended path, and suppressing it
	// here made a promoted finding vanish from the pull request entirely — deduped
	// against its own summary entry, and dropped from the regenerated summary.
	existing, err := g.existingInlineFingerprints(ctx)
	if err != nil {
		return fmt.Errorf("reading existing comments: %w", err)
	}

	var posted, deduped int
	for _, f := range inline {
		if existing[f.Fingerprint] {
			deduped++
			continue
		}
		comment := g.commentFor(f)

		if g.DryRun {
			fmt.Fprintf(out, "would comment on %s:%d\n%s\n\n", comment.Path, comment.Line, comment.Body)
			// Recorded in the dry run too, so the preview's counts match what a
			// real run would do rather than double-counting duplicates.
			existing[f.Fingerprint] = true
			posted++
			continue
		}
		if _, err := g.Writer.CreateReviewComment(ctx, g.Repo, g.Number, comment); err != nil {
			// One comment failing — most often because the line is not in the
			// diff GitHub thinks it is reviewing — must not lose the others.
			fmt.Fprintf(logOr(g.Log, out), "warning: could not comment on %s:%d: %v\n", f.File, f.Line, err)
			continue
		}
		// Remember it, so two findings that normalise to one identity within a
		// single run do not both post.
		existing[f.Fingerprint] = true
		posted++
	}

	// The summary carries the findings that did not earn an interruption, plus the
	// gate's reason if it fired — a developer who has just committed a credential
	// needs to see that the diff was not sent anywhere.
	summaryBody := summary(deferred, run.GateReason)
	summaryAction := "skipped (dry run)"
	if !g.DryRun {
		summaryAction, err = g.upsertSummary(ctx, summaryBody)
		if err != nil {
			fmt.Fprintf(logOr(g.Log, out), "warning: could not update the summary comment: %v\n", err)
			summaryAction = "failed"
		}
	} else if summaryBody != "" {
		fmt.Fprintf(out, "would upsert the summary comment:\n%s\n", summaryBody)
	}

	var resolvedNote string
	// Resolution is only safe when this run is a faithful account of the current
	// state. A run where analyzers failed, or where the gate stopped half the
	// system, reports fewer findings than exist — and resolving against it would
	// close every outstanding thread on the pull request.
	switch {
	case g.ResolveStale && !run.Trustworthy:
		fmt.Fprintf(logOr(g.Log, out),
			"skipped: not resolving stale threads because this run did not complete cleanly\n")
	case g.ResolveStale && run.Trustworthy:
		resolved, errs := g.resolveStale(ctx, currentFingerprints(run))
		for _, err := range errs {
			fmt.Fprintf(logOr(g.Log, out), "warning: %v\n", err)
		}
		if resolved > 0 {
			word := "resolved"
			if g.DryRun {
				word = "would resolve"
			}
			resolvedNote = fmt.Sprintf(", %s %d stale thread%s", word, resolved, plural(resolved))
		}
	}

	verb := "posted"
	if g.DryRun {
		verb = "would post"
	}
	fmt.Fprintf(out, "%d finding%s, %s %d inline, %d deduped, %d in the summary (%s)%s\n",
		len(run.Findings), plural(len(run.Findings)), verb, posted, deduped, len(deferred), summaryAction, resolvedNote)
	return nil
}

// partition splits findings by what an inline comment means. High confidence asks
// the reviewer to stop and look; anything less is context, and context that
// interrupts is noise.
func partition(findings []finding.Finding) (inline, deferred []finding.Finding) {
	for _, f := range findings {
		if f.Confidence == finding.ConfidenceHigh {
			inline = append(inline, f)
		} else {
			deferred = append(deferred, f)
		}
	}
	return inline, deferred
}

// existingInlineFingerprints collects the identities already posted as inline
// comments. Summary entries are deliberately excluded; see the call site.
func (g GitHub) existingInlineFingerprints(ctx context.Context) (map[string]bool, error) {
	comments, err := g.Client.ListReviewComments(ctx, g.Repo, g.Number)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(comments))
	for _, c := range comments {
		if fp := fingerprint.ParseMarker(c.Body); fp != "" {
			seen[fp] = true
		}
	}
	return seen, nil
}

func (g GitHub) commentFor(f finding.Finding) ghclient.NewReviewComment {
	start, end := f.Span()
	return ghclient.NewReviewComment{
		Body:     Body(f),
		CommitID: g.HeadSHA,
		Path:     f.File,
		Line:     end,
		// Explicit rather than left to the client's default: RIGHT is the
		// post-change side, and a comment on LEFT would land on code this pull
		// request deleted.
		Side:      "RIGHT",
		StartLine: start,
	}
}

// Body renders one finding as a comment.
//
// Deliberately plain: no heading, no emoji, no "🤖 Automated review" banner. The
// comment competes for attention with human review comments and should read like
// one. The rule id is included because it is what someone searches for when they
// want to argue with the rule, and the marker is what lets the next run recognise
// this comment as its own.
func Body(f finding.Finding) string {
	var b strings.Builder
	b.WriteString(f.Message)

	if f.Evidence != "" {
		b.WriteString("\n\n")
		b.WriteString(f.Evidence)
	}
	if f.Suggestion != "" {
		b.WriteString("\n\n```suggestion\n")
		b.WriteString(strings.TrimRight(f.Suggestion, "\n"))
		b.WriteString("\n```")
	}

	b.WriteString("\n\n<sub>")
	b.WriteString(f.RuleID)
	if f.Confidence != finding.ConfidenceHigh {
		fmt.Fprintf(&b, " · %s confidence", f.Confidence)
	}
	b.WriteString("</sub>\n")
	b.WriteString(fingerprint.Marker(f.Fingerprint))
	return b.String()
}

func logOr(log, fallback io.Writer) io.Writer {
	if log != nil {
		return log
	}
	return fallback
}
