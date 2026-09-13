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
}

func (f *fakeGitHub) Viewer(context.Context) (ghclient.User, error) {
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
	if !strings.Contains(out.String(), "1 for the summary") {
		t.Fatalf("want the deferred finding accounted for, got %q", out.String())
	}
}

// The failure that makes a reviewer bot intolerable: reposting on every push.
func TestAlreadyPostedFindingsAreNotRepeated(t *testing.T) {
	f := &fakeGitHub{
		reviewComments: []ghclient.ReviewComment{
			{ID: 1, Body: "domain must not import infra\n" + fingerprint.Marker("aaa111")},
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

// A finding listed in the summary comment must not then be posted inline as if it
// were new.
func TestFingerprintsInTheSummaryCountAsAlreadySaid(t *testing.T) {
	f := &fakeGitHub{
		issueComments: []ghclient.IssueComment{{
			ID:   9,
			Body: "<!-- rv:summary -->\n- a thing " + fingerprint.Marker("aaa111") + "\n- another " + fingerprint.Marker("ddd444"),
		}},
	}
	var out bytes.Buffer
	run := Run{Findings: []finding.Finding{high("aaa111", "a.ts", 3), high("ddd444", "d.ts", 2)}}
	if err := reporter(f, &out).Report(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if len(f.posted) != 0 {
		t.Fatalf("both findings were already listed in the summary, got %+v", f.posted)
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
