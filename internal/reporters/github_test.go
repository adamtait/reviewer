// SPDX-License-Identifier: MIT

package reporters

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/internal/fingerprint"
	"github.com/adamtait/reviewer/internal/ghclient"
	"github.com/adamtait/reviewer/pkg/finding"
)

// fakeGitHub records everything written, so the assertions are about what would
// reach a real pull request.
type fakeGitHub struct {
	reviewComments []ghclient.ReviewComment
	issueComments  []ghclient.IssueComment
	posted         []ghclient.NewReviewComment
	postedIssues   []string
	updatedIssues  map[int64]string
	resolved       []string
	threads        []ghclient.ReviewThread
	failPost       error
	failResolve    map[string]error
	viewerErr      error
}

func (f *fakeGitHub) Viewer(context.Context) (ghclient.User, error) {
	if f.viewerErr != nil {
		return ghclient.User{}, f.viewerErr
	}
	return ghclient.User{Login: "reviewer[bot]", Type: "Bot"}, nil
}
func (f *fakeGitHub) GetPullRequest(context.Context, ghclient.Repo, int) (ghclient.PullRequest, error) {
	return ghclient.PullRequest{Number: 1, Head: ghclient.Ref{SHA: "head-sha"}}, nil
}
func (f *fakeGitHub) ListPullRequests(context.Context, ghclient.Repo, string) ([]ghclient.PullRequest, error) {
	return nil, nil
}
func (f *fakeGitHub) ListFiles(context.Context, ghclient.Repo, int) ([]ghclient.ChangedFile, error) {
	return nil, nil
}
func (f *fakeGitHub) ListReviewComments(context.Context, ghclient.Repo, int) ([]ghclient.ReviewComment, error) {
	return f.reviewComments, nil
}
func (f *fakeGitHub) ListIssueComments(context.Context, ghclient.Repo, int) ([]ghclient.IssueComment, error) {
	return f.issueComments, nil
}
func (f *fakeGitHub) CreateReviewComment(_ context.Context, _ ghclient.Repo, _ int, c ghclient.NewReviewComment) (ghclient.ReviewComment, error) {
	if f.failPost != nil {
		return ghclient.ReviewComment{}, f.failPost
	}
	f.posted = append(f.posted, c)
	return ghclient.ReviewComment{ID: int64(len(f.posted)), Body: c.Body}, nil
}
func (f *fakeGitHub) CreateIssueComment(_ context.Context, _ ghclient.Repo, _ int, body string) (ghclient.IssueComment, error) {
	f.postedIssues = append(f.postedIssues, body)
	return ghclient.IssueComment{ID: int64(len(f.postedIssues)), Body: body}, nil
}
func (f *fakeGitHub) UpdateIssueComment(_ context.Context, _ ghclient.Repo, id int64, body string) (ghclient.IssueComment, error) {
	if f.updatedIssues == nil {
		f.updatedIssues = map[int64]string{}
	}
	f.updatedIssues[id] = body
	return ghclient.IssueComment{ID: id, Body: body}, nil
}
func (f *fakeGitHub) ResolveReviewThread(_ context.Context, id string) error {
	if err, ok := f.failResolve[id]; ok {
		return err
	}
	f.resolved = append(f.resolved, id)
	return nil
}
func (f *fakeGitHub) ReviewThreads(context.Context, ghclient.Repo, int) ([]ghclient.ReviewThread, error) {
	return f.threads, nil
}

func reporter(f *fakeGitHub, out *bytes.Buffer) GitHub {
	return GitHub{
		Client: f, Writer: f,
		Repo: ghclient.Repo{Owner: "o", Name: "r"}, Number: 1,
		HeadSHA: "head-sha", Out: out, Log: out,
	}
}

func high(fp, file string, line int) finding.Finding {
	return finding.Finding{
		Fingerprint: fp, RuleID: "arch/no-domain-to-infra", Lane: finding.LaneDeterministic,
		Confidence: finding.ConfidenceHigh, Severity: finding.SeverityError,
		File: file, Line: line, Message: "domain must not import infra",
	}
}

func medium(fp, file string, line int) finding.Finding {
	f := high(fp, file, line)
	f.Confidence = finding.ConfidenceMedium
	f.RuleID = "llm/missing-abstraction"
	return f
}

// Only high confidence goes inline. An inline comment is a claim that the reviewer
// should stop and look (ADR-0017).
func TestOnlyHighConfidenceIsPostedInline(t *testing.T) {
	f := &fakeGitHub{}
	var out bytes.Buffer
	run := Run{Findings: []finding.Finding{
		high("aaa111", "a.ts", 3),
		medium("bbb222", "b.ts", 9),
	}}
	if err := reporter(f, &out).Report(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if len(f.posted) != 1 {
		t.Fatalf("want one inline comment, got %d: %+v", len(f.posted), f.posted)
	}
	if f.posted[0].Path != "a.ts" {
		t.Fatalf("want the high-confidence finding posted, got %+v", f.posted[0])
	}
	if !strings.Contains(out.String(), "1 in the summary") {
		t.Fatalf("want the deferred finding accounted for, got %q", out.String())
	}
	if len(f.postedIssues) != 1 {
		t.Fatalf("want the medium-confidence finding in one summary comment, got %v", f.postedIssues)
	}
}

// The failure that makes a reviewer bot intolerable: reposting on every push.
func TestAlreadyPostedFindingsAreNotRepeated(t *testing.T) {
	f := &fakeGitHub{
		reviewComments: []ghclient.ReviewComment{
			{ID: 1, Body: "domain must not import infra\n" + fingerprint.Marker("aaa111", "")},
		},
	}
	var out bytes.Buffer
	run := Run{Findings: []finding.Finding{high("aaa111", "a.ts", 3), high("ccc333", "c.ts", 1)}}
	if err := reporter(f, &out).Report(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if len(f.posted) != 1 {
		t.Fatalf("want only the new finding posted, got %+v", f.posted)
	}
	if !strings.Contains(f.posted[0].Body, "ccc333") {
		t.Fatalf("want the unseen finding, got %q", f.posted[0].Body)
	}
	if !strings.Contains(out.String(), "1 deduped") {
		t.Fatalf("want the dedupe reported, got %q", out.String())
	}
}

// A finding that was in the summary and is now high confidence must become an
// inline comment.
//
// This is the promotion path: a category whose acceptance rate justifies it gets
// raised from medium to high, and the finding should move from the collapsed
// summary to an inline comment. An earlier version suppressed the inline post
// because the fingerprint appeared in the summary, *and* dropped the finding from
// the regenerated summary because it was no longer medium — so it vanished from
// the pull request entirely.
func TestAPromotedFindingMovesFromTheSummaryToInline(t *testing.T) {
	f := &fakeGitHub{
		issueComments: []ghclient.IssueComment{{
			ID:   9,
			Body: SummaryMarker + "\n- a thing " + fingerprint.Marker("aaa111", ""),
		}},
	}
	var out bytes.Buffer
	run := Run{Findings: []finding.Finding{high("aaa111", "a.ts", 3)}}
	if err := reporter(f, &out).Report(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if len(f.posted) != 1 {
		t.Fatalf("a promoted finding must be posted inline, got %+v", f.posted)
	}
	if fingerprint.ParseMarker(f.posted[0].Body) != "aaa111" {
		t.Fatalf("want the promoted finding, got %q", f.posted[0].Body)
	}
}

// Two findings that normalise to one identity within a single run must post once.
func TestDuplicateFingerprintsWithinOneRunPostOnce(t *testing.T) {
	f := &fakeGitHub{}
	var out bytes.Buffer
	run := Run{Findings: []finding.Finding{high("aaa111", "a.ts", 3), high("aaa111", "a.ts", 40)}}
	if err := reporter(f, &out).Report(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if len(f.posted) != 1 {
		t.Fatalf("want one comment, got %d", len(f.posted))
	}
}

func TestDryRunPostsNothing(t *testing.T) {
	f := &fakeGitHub{}
	var out bytes.Buffer
	r := reporter(f, &out)
	r.DryRun = true
	if err := r.Report(context.Background(), Run{Findings: []finding.Finding{high("aaa111", "a.ts", 3)}}); err != nil {
		t.Fatal(err)
	}
	if len(f.posted) != 0 {
		t.Fatal("a dry run must post nothing")
	}
	if !strings.Contains(out.String(), "would comment on a.ts:3") {
		t.Fatalf("want the payload printed, got %q", out.String())
	}
	if !strings.Contains(out.String(), "would post 1") {
		t.Fatalf("want the summary to say it would post, got %q", out.String())
	}
}

// One comment failing — most often because the line is not in the diff GitHub
// thinks it is reviewing — must not lose the others.
func TestOneFailedCommentDoesNotLoseTheRest(t *testing.T) {
	f := &fakeGitHub{failPost: errors.New("line must be part of the diff")}
	var out bytes.Buffer
	run := Run{Findings: []finding.Finding{high("aaa111", "a.ts", 3), high("bbb222", "b.ts", 4)}}
	if err := reporter(f, &out).Report(context.Background(), run); err != nil {
		t.Fatalf("a failed comment must not fail the report, got %v", err)
	}
	if strings.Count(out.String(), "could not comment") != 2 {
		t.Fatalf("want both failures reported, got %q", out.String())
	}
}

func TestBodyShape(t *testing.T) {
	f := high("aaa111", "a.ts", 3)
	f.Suggestion = "  await send(request)"
	body := Body(f)

	if !strings.HasPrefix(body, "domain must not import infra") {
		t.Fatalf("the message must lead; a banner competes with human comments: %q", body)
	}
	if !strings.Contains(body, "```suggestion") {
		t.Fatal("want a GitHub suggested change")
	}
	if !strings.Contains(body, "arch/no-domain-to-infra") {
		t.Fatal("want the rule id, which is what someone searches for to argue with the rule")
	}
	if fingerprint.ParseMarker(body) != "aaa111" {
		t.Fatal("want the fingerprint marker so the next run recognises this comment")
	}
	for _, banned := range []string{"🤖", "Automated", "AI-generated"} {
		if strings.Contains(body, banned) {
			t.Fatalf("the comment should read like a review comment, not an announcement: %q", body)
		}
	}
}

func TestMultiLineSpanIsAnchoredCorrectly(t *testing.T) {
	f := &fakeGitHub{}
	var out bytes.Buffer
	span := high("aaa111", "a.ts", 9)
	span.EndLine = 11
	if err := reporter(f, &out).Report(context.Background(), Run{Findings: []finding.Finding{span}}); err != nil {
		t.Fatal(err)
	}
	got := f.posted[0]
	// GitHub anchors a multi-line comment at `line` and needs `start_line` before it.
	if got.Line != 11 || got.StartLine != 9 {
		t.Fatalf("want line 11 with start_line 9, got %+v", got)
	}
	if got.Side != "RIGHT" {
		t.Fatalf("want the post-change side, got %q", got.Side)
	}
}

func TestWarningsGoToTheLogNotThePullRequest(t *testing.T) {
	f := &fakeGitHub{}
	var out, log bytes.Buffer
	r := reporter(f, &out)
	r.Log = &log
	run := Run{Warnings: []string{"knip timed out"}, Skipped: []string{"eslint: no config"}}
	if err := r.Report(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if len(f.posted) != 0 || len(f.postedIssues) != 0 {
		t.Fatal("a warning is not a review comment")
	}
	if !strings.Contains(log.String(), "knip timed out") {
		t.Fatalf("want the warning in the log, got %q", log.String())
	}
}

func TestSummaryShape(t *testing.T) {
	findings := []finding.Finding{
		medium("bbb222", "b.ts", 9),
		medium("ccc333", "c.ts", 4),
	}
	findings[1].Evidence = "four call sites repeat the same wrapper"
	body := summary(findings, "")

	if !strings.HasPrefix(body, SummaryMarker) {
		t.Fatalf("want the sticky marker first so the next run finds this comment: %q", body)
	}
	// Collapsed, so it costs nothing to scroll past.
	if !strings.Contains(body, "<details>") || !strings.Contains(body, "</details>") {
		t.Fatalf("want a collapsed block, got %q", body)
	}
	// Leading with a count is what makes the decision to expand an informed one.
	if !strings.Contains(body, "2 further observations") {
		t.Fatalf("want a count in the summary line, got %q", body)
	}
	// Grouped by rule: five instances of one rule is one thought, not five.
	if strings.Count(body, "**llm/missing-abstraction**") != 1 {
		t.Fatalf("want one heading per rule, got %q", body)
	}
	if !strings.Contains(body, "four call sites repeat the same wrapper") {
		t.Fatal("evidence is why a model finding is worth showing; it must not be hidden further")
	}
	for _, f := range findings {
		if !strings.Contains(body, fingerprint.Marker(f.Fingerprint, f.RuleID)) {
			t.Fatalf("every listed finding needs its marker so it is not later posted as new: %q", body)
		}
	}
}

// A developer who has just committed a credential must see that the diff was not
// sent anywhere without expanding anything.
func TestTheGateReasonIsAboveTheFold(t *testing.T) {
	body := summary(nil, "a credential was found in src/config.ts, so the model lane did not run — the diff was not sent anywhere")
	if body == "" {
		t.Fatal("a gate reason alone must still produce a summary")
	}
	beforeDetails := body
	if i := strings.Index(body, "<details>"); i >= 0 {
		beforeDetails = body[:i]
	}
	if !strings.Contains(beforeDetails, "not sent anywhere") {
		t.Fatalf("the gate reason must precede any collapsed block, got %q", body)
	}
}

func TestSummaryIsUpsertedNotAppended(t *testing.T) {
	existing := summary([]finding.Finding{medium("bbb222", "b.ts", 9)}, "")
	f := &fakeGitHub{issueComments: []ghclient.IssueComment{{ID: 7, Body: existing}}}
	var out bytes.Buffer

	// A second run with a different finding updates the same comment.
	run := Run{Findings: []finding.Finding{medium("ccc333", "c.ts", 4)}}
	if err := reporter(f, &out).Report(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if len(f.postedIssues) != 0 {
		t.Fatalf("want no new comment, got %v", f.postedIssues)
	}
	if _, ok := f.updatedIssues[7]; !ok {
		t.Fatalf("want the existing comment updated, got %v", f.updatedIssues)
	}
}

// Rewriting an unchanged comment bumps its timestamp and re-notifies every
// subscriber for nothing.
func TestAnUnchangedSummaryIsNotRewritten(t *testing.T) {
	findings := []finding.Finding{medium("bbb222", "b.ts", 9)}
	f := &fakeGitHub{issueComments: []ghclient.IssueComment{{ID: 7, Body: summary(findings, "")}}}
	var out bytes.Buffer

	if err := reporter(f, &out).Report(context.Background(), Run{Findings: findings}); err != nil {
		t.Fatal(err)
	}
	if len(f.updatedIssues) != 0 {
		t.Fatalf("want no update for an identical body, got %v", f.updatedIssues)
	}
	if !strings.Contains(out.String(), "unchanged") {
		t.Fatalf("want the no-op reported, got %q", out.String())
	}
}

// A comment that vanishes leaves a reader wondering whether the tool ran.
func TestAnEmptySummaryClearsRatherThanDisappears(t *testing.T) {
	f := &fakeGitHub{issueComments: []ghclient.IssueComment{{ID: 7, Body: summary([]finding.Finding{medium("bbb222", "b.ts", 9)}, "")}}}
	var out bytes.Buffer

	if err := reporter(f, &out).Report(context.Background(), Run{}); err != nil {
		t.Fatal(err)
	}
	body, ok := f.updatedIssues[7]
	if !ok {
		t.Fatalf("want the comment rewritten, got %v", f.updatedIssues)
	}
	if !strings.Contains(body, "No further observations") {
		t.Fatalf("want an explicit empty state, got %q", body)
	}
}

func TestNoSummaryIsCreatedWhenThereIsNothingToSay(t *testing.T) {
	f := &fakeGitHub{}
	var out bytes.Buffer
	if err := reporter(f, &out).Report(context.Background(), Run{Findings: []finding.Finding{high("aaa111", "a.ts", 3)}}); err != nil {
		t.Fatal(err)
	}
	if len(f.postedIssues) != 0 {
		t.Fatalf("an all-inline run needs no summary, got %v", f.postedIssues)
	}
}

func TestMultiLineMessagesCannotBreakTheSummaryMarkup(t *testing.T) {
	f := medium("bbb222", "b.ts", 9)
	f.Message = "first line\nsecond line\n\nthird"
	body := summary([]finding.Finding{f}, "")
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "second") || strings.HasPrefix(line, "third") {
			t.Fatalf("a multi-line message broke out of its list item: %q", body)
		}
	}
}

// Resolving a thread is the only action this tool takes on a conversation, so it
// is opt-in and fenced by three conditions. Each is a separate case here because
// each protects a different mistake.
func TestResolveStaleThreads(t *testing.T) {
	ours := func(fp string) ghclient.ReviewThread {
		return ghclient.ReviewThread{
			ID: "thread-" + fp,
			Comments: []ghclient.ReviewComment{{
				Body: "a finding\n" + fingerprint.Marker(fp, ""),
				User: ghclient.User{Login: "reviewer[bot]"},
			}},
		}
	}

	tests := []struct {
		name        string
		threads     []ghclient.ReviewThread
		current     []finding.Finding
		wantResolve []string
	}{
		{
			name:        "a finding that is no longer reported is resolved",
			threads:     []ghclient.ReviewThread{ours("f00d11")},
			wantResolve: []string{"thread-f00d11"},
		},
		{
			name:    "a finding still reported is left open",
			threads: []ghclient.ReviewThread{ours("aaa111")},
			current: []finding.Finding{high("aaa111", "a.ts", 3)},
		},
		{
			name: "a human's thread is never touched",
			threads: []ghclient.ReviewThread{{
				ID:       "human",
				Comments: []ghclient.ReviewComment{{Body: "I think this is fine", User: ghclient.User{Login: "alice"}}},
			}},
		},
		{
			// Marked as ours but written by someone else: a quoted comment, or a
			// human who copied the body.
			name: "our marker in someone else's comment is not ours",
			threads: []ghclient.ReviewThread{{
				ID: "quoted",
				Comments: []ghclient.ReviewComment{{
					Body: "quoting the bot: " + fingerprint.Marker("f00d11", ""),
					User: ghclient.User{Login: "alice"},
				}},
			}},
		},
		{
			name: "an already-resolved thread is left alone",
			threads: []ghclient.ReviewThread{func() ghclient.ReviewThread {
				t := ours("f00d11")
				t.IsResolved = true
				return t
			}()},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeGitHub{threads: tc.threads}
			var out bytes.Buffer
			r := reporter(f, &out)
			r.ResolveStale = true
			if err := r.Report(context.Background(), Run{Findings: tc.current, Trustworthy: true}); err != nil {
				t.Fatal(err)
			}
			if strings.Join(f.resolved, ",") != strings.Join(tc.wantResolve, ",") {
				t.Fatalf("want resolved %v, got %v", tc.wantResolve, f.resolved)
			}
		})
	}
}

// Off by default: the blast radius of resolving someone's thread wrongly is not
// something to opt people into.
func TestResolutionIsOffByDefault(t *testing.T) {
	f := &fakeGitHub{threads: []ghclient.ReviewThread{{
		ID: "t1",
		Comments: []ghclient.ReviewComment{{
			Body: "old finding\n" + fingerprint.Marker("f00d11", ""),
			User: ghclient.User{Login: "reviewer[bot]"},
		}},
	}}}
	var out bytes.Buffer
	if err := reporter(f, &out).Report(context.Background(), Run{}); err != nil {
		t.Fatal(err)
	}
	if len(f.resolved) != 0 {
		t.Fatalf("want nothing resolved without the flag, got %v", f.resolved)
	}
}

func TestDryRunResolvesNothing(t *testing.T) {
	f := &fakeGitHub{threads: []ghclient.ReviewThread{{
		ID: "t1",
		Comments: []ghclient.ReviewComment{{
			Body: "old finding\n" + fingerprint.Marker("f00d11", ""),
			User: ghclient.User{Login: "reviewer[bot]"},
		}},
	}}}
	var out bytes.Buffer
	r := reporter(f, &out)
	r.ResolveStale, r.DryRun = true, true
	if err := r.Report(context.Background(), Run{Trustworthy: true}); err != nil {
		t.Fatal(err)
	}
	if len(f.resolved) != 0 {
		t.Fatal("a dry run must resolve nothing")
	}
	if !strings.Contains(out.String(), "would resolve 1 stale thread") {
		t.Fatalf("want the intent reported, got %q", out.String())
	}
}

// A run that did not complete cleanly reports fewer findings than exist, so
// resolving against it would close every outstanding thread on the pull request.
func TestAnUntrustworthyRunResolvesNothing(t *testing.T) {
	f := &fakeGitHub{threads: []ghclient.ReviewThread{{
		ID: "t1",
		Comments: []ghclient.ReviewComment{{
			Body: "old finding\n" + fingerprint.Marker("f00d11", ""),
			User: ghclient.User{Login: "reviewer[bot]"},
		}},
	}}}
	var out bytes.Buffer
	r := reporter(f, &out)
	r.ResolveStale = true

	// Trustworthy is false: an analyzer failed, or none ran.
	if err := r.Report(context.Background(), Run{Trustworthy: false}); err != nil {
		t.Fatal(err)
	}
	if len(f.resolved) != 0 {
		t.Fatalf("want nothing resolved after an incomplete run, got %v", f.resolved)
	}
	if !strings.Contains(out.String(), "did not complete cleanly") {
		t.Fatalf("want the reason reported, got %q", out.String())
	}
}

// One thread failing to resolve must not skip the rest.
func TestOneFailedResolutionDoesNotSkipTheOthers(t *testing.T) {
	thread := func(id, fp string) ghclient.ReviewThread {
		return ghclient.ReviewThread{ID: id, Comments: []ghclient.ReviewComment{{
			Body: "old\n" + fingerprint.Marker(fp, ""), User: ghclient.User{Login: "reviewer[bot]"},
		}}}
	}
	f := &fakeGitHub{
		threads:     []ghclient.ReviewThread{thread("bad", "f00d11"), thread("good", "f00d22")},
		failResolve: map[string]error{"bad": errors.New("thread is locked")},
	}
	var out bytes.Buffer
	r := reporter(f, &out)
	r.ResolveStale = true
	if err := r.Report(context.Background(), Run{Trustworthy: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.resolved, ",") != "good" {
		t.Fatalf("want the second thread still resolved, got %v", f.resolved)
	}
	if !strings.Contains(out.String(), "thread is locked") {
		t.Fatalf("want the failure reported, got %q", out.String())
	}
}

// The installation token an Action runs with gets 403 from /user, so a configured
// login is the fallback — and with neither, resolution is skipped rather than
// guessed at.
func TestIdentityFallsBackToConfigurationThenSkips(t *testing.T) {
	threads := []ghclient.ReviewThread{{
		ID: "t1",
		Comments: []ghclient.ReviewComment{{
			Body: "old\n" + fingerprint.Marker("f00d11", ""),
			User: ghclient.User{Login: "github-actions[bot]"},
		}},
	}}

	t.Run("configured login is used when the API refuses", func(t *testing.T) {
		f := &fakeGitHub{threads: threads, viewerErr: errors.New("403 Forbidden")}
		var out bytes.Buffer
		r := reporter(f, &out)
		r.ResolveStale, r.SelfLogin = true, "github-actions[bot]"
		if err := r.Report(context.Background(), Run{Trustworthy: true}); err != nil {
			t.Fatal(err)
		}
		if strings.Join(f.resolved, ",") != "t1" {
			t.Fatalf("want the thread resolved via the configured login, got %v", f.resolved)
		}
	})

	t.Run("no identity means no resolution", func(t *testing.T) {
		f := &fakeGitHub{threads: threads, viewerErr: errors.New("403 Forbidden")}
		var out bytes.Buffer
		r := reporter(f, &out)
		r.ResolveStale = true
		if err := r.Report(context.Background(), Run{Trustworthy: true}); err != nil {
			t.Fatal(err)
		}
		if len(f.resolved) != 0 {
			t.Fatalf("want nothing resolved without an identity, got %v", f.resolved)
		}
		if !strings.Contains(out.String(), "github.selfLogin") {
			t.Fatalf("want the fix named in the warning, got %q", out.String())
		}
	})
}

// An already-cleared summary must not be rewritten every run: that re-notifies
// every subscriber to say nothing changed.
func TestAnAlreadyClearedSummaryIsLeftAlone(t *testing.T) {
	cleared := SummaryMarker + "\nNo further observations on the current revision.\n"
	f := &fakeGitHub{issueComments: []ghclient.IssueComment{{ID: 7, Body: cleared}}}
	var out bytes.Buffer
	if err := reporter(f, &out).Report(context.Background(), Run{}); err != nil {
		t.Fatal(err)
	}
	if len(f.updatedIssues) != 0 {
		t.Fatalf("want no rewrite of an already-cleared summary, got %v", f.updatedIssues)
	}
}
