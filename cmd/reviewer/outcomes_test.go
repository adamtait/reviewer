// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/ghclient"
)

// fakeOutcomes stands in for the GitHub reads outcomes makes. Anything not set
// answers empty.
type fakeOutcomes struct {
	prs      []ghclient.PullRequest
	files    map[int][]ghclient.ChangedFile
	reviews  map[int][]ghclient.Review
	comments map[int][]ghclient.ReviewComment
	timeline map[int][]ghclient.TimelineEvent
	// compare is keyed "base...head".
	compare map[string][]ghclient.ChangedFile
	// failFilesOf makes reading that pull request's files fail with failErr.
	failFilesOf int
	failErr     error
	// compared records every comparison asked for.
	compared []string
}

func (f *fakeOutcomes) ListPullRequestsUpdatedSince(context.Context, ghclient.Repo, time.Time) ([]ghclient.PullRequest, error) {
	return f.prs, nil
}
func (f *fakeOutcomes) ListFiles(_ context.Context, _ ghclient.Repo, n int) ([]ghclient.ChangedFile, error) {
	if n == f.failFilesOf {
		return nil, f.failErr
	}
	return f.files[n], nil
}
func (f *fakeOutcomes) ListReviews(_ context.Context, _ ghclient.Repo, n int) ([]ghclient.Review, error) {
	return f.reviews[n], nil
}
func (f *fakeOutcomes) ListReviewComments(_ context.Context, _ ghclient.Repo, n int) ([]ghclient.ReviewComment, error) {
	return f.comments[n], nil
}
func (f *fakeOutcomes) ListTimelineEvents(_ context.Context, _ ghclient.Repo, n int) ([]ghclient.TimelineEvent, error) {
	return f.timeline[n], nil
}
func (f *fakeOutcomes) CompareFiles(_ context.Context, _ ghclient.Repo, base, head string) ([]ghclient.ChangedFile, error) {
	key := base + "..." + head
	f.compared = append(f.compared, key)
	files, ok := f.compare[key]
	if !ok {
		return nil, fmt.Errorf("%w (unexpected comparison %s)", ghclient.ErrNotFound, key)
	}
	return files, nil
}

var (
	outcomesRepo = ghclient.Repo{Owner: "owner", Name: "name"}
	// day0 is when the first pull request in each test merges.
	day0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	// outcomesNow is well past every hotfix window, unless a test says otherwise.
	outcomesNow   = day0.AddDate(0, 2, 0)
	outcomesSince = day0.AddDate(0, 0, -30)
)

// mergedPR is a pull request alice opened two days before it merged.
func mergedPR(number int, title, body string, merged time.Time) ghclient.PullRequest {
	return ghclient.PullRequest{
		Number: number, Title: title, Body: body, State: "closed",
		User:      ghclient.User{Login: "alice", Type: "User"},
		Head:      ghclient.Ref{SHA: fmt.Sprintf("head%d", number)},
		CreatedAt: merged.Add(-48 * time.Hour), UpdatedAt: merged, MergedAt: merged,
	}
}

func file(name, status, patch string) ghclient.ChangedFile {
	return ghclient.ChangedFile{Filename: name, Status: status, Patch: patch}
}

func reviewBy(id int64, login, state, commit string, at time.Time) ghclient.Review {
	return ghclient.Review{ID: id, User: ghclient.User{Login: login, Type: "User"},
		State: state, CommitID: commit, SubmittedAt: at}
}

func measure(t *testing.T, f *fakeOutcomes, allow ...string) map[int]prOutcome {
	t.Helper()
	entries, err := parseAllowlist(allow)
	if err != nil {
		t.Fatal(err)
	}
	report := measureOutcomes(context.Background(), f, outcomesRepo, outcomesSince, outcomesNow, entries)
	if report.Gap != nil {
		t.Fatalf("unexpected gap: %v", report.Gap)
	}
	byNumber := map[int]prOutcome{}
	for _, p := range report.PRs {
		byNumber[p.Number] = p
	}
	return byNumber
}

func TestARevertIsRecognised(t *testing.T) {
	f := &fakeOutcomes{prs: []ghclient.PullRequest{
		mergedPR(1, "Add widget", "", day0),
		mergedPR(2, `Revert "Add widget"`, "Reverts owner/name#1\n\nIt broke the build.", day0.AddDate(0, 0, 1)),
		// The short form a person writes by hand.
		mergedPR(3, "Add gadget", "", day0.AddDate(0, 0, 2)),
		mergedPR(4, `Revert "Add gadget"`, "Reverts #3", day0.AddDate(0, 0, 3)),
	}}
	got := measure(t, f)
	if p := got[1]; p.Outcome != outcomeReverted || p.By != 2 {
		t.Errorf("#1 = %s by #%d, want reverted by #2", p.Outcome, p.By)
	}
	if p := got[3]; p.Outcome != outcomeReverted || p.By != 4 {
		t.Errorf("#3 = %s by #%d, want reverted by #4", p.Outcome, p.By)
	}
	if got[2].Outcome != outcomeClean {
		t.Errorf("the revert itself is %s, want clean", got[2].Outcome)
	}
}

// The title alone is not the convention. A revert of a different pull request
// with the same title, a number that only starts the same, and the same number
// in another repository are three ways to charge the wrong pull request.
func TestARevertOfADifferentPullRequestDoesNotMatch(t *testing.T) {
	f := &fakeOutcomes{prs: []ghclient.PullRequest{
		mergedPR(1, "Add widget", "", day0),
		mergedPR(15, "Add widget", "", day0.AddDate(0, 0, 1)),
		mergedPR(16, `Revert "Add widget"`, "Reverts owner/name#15", day0.AddDate(0, 0, 2)),
		mergedPR(17, `Revert "Add widget"`, "Reverts other/repo#1", day0.AddDate(0, 0, 3)),
		// The body names #1, but this is not a revert.
		mergedPR(18, "Tidy up after #1", "Reverts #1 in spirit only", day0.AddDate(0, 0, 4)),
	}}
	got := measure(t, f)
	if p := got[1]; p.Outcome != outcomeClean {
		t.Errorf("#1 = %s by #%d, want clean", p.Outcome, p.By)
	}
	if p := got[15]; p.Outcome != outcomeReverted || p.By != 16 {
		t.Errorf("#15 = %s by #%d, want reverted by #16", p.Outcome, p.By)
	}
}

// addFoo is a patch that adds lines 10 to 12 of a.go.
const addFoo = "@@ -8,4 +8,7 @@ func a() {\n" +
	" \tx := 1\n" +
	" \ty := 2\n" +
	"+\tfoo()\n" +
	"+\tbar()\n" +
	"+\tbaz()\n" +
	" \treturn\n" +
	" }"

func TestAHotfixOverlapsTheLinesItFixes(t *testing.T) {
	// Replaces line 11, which #1 added, inside a hunk whose context covers 8 to 14.
	fix := "@@ -8,7 +8,7 @@ func a() {\n" +
		" \tx := 1\n" +
		" \ty := 2\n" +
		" \tfoo()\n" +
		"-\tbar()\n" +
		"+\tbar(ctx)\n" +
		" \tbaz()\n" +
		" \treturn\n" +
		" }"
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Add foo", "", day0),
			mergedPR(2, "Fix bar", "", day0.AddDate(0, 0, 3)),
			mergedPR(3, "Add foo elsewhere", "", day0.AddDate(0, 0, 4)),
			// The same change, but after the window: by then it is just code.
			mergedPR(4, "Fix bar again", "", day0.AddDate(0, 0, 25)),
		},
		files: map[int][]ghclient.ChangedFile{
			1: {file("a.go", "modified", addFoo)},
			2: {file("a.go", "modified", fix)},
			3: {file("a.go", "modified", addFoo)},
			4: {file("a.go", "modified", fix)},
		},
	}
	got := measure(t, f)
	if p := got[1]; p.Outcome != outcomeHotfixed || p.By != 2 {
		t.Errorf("#1 = %s by #%d, want hotfixed by #2", p.Outcome, p.By)
	}
	// #3 merged after #2, so #2 cannot have fixed it, and #4 is outside its
	// window.
	if p := got[3]; p.Outcome != outcomeClean {
		t.Errorf("#3 = %s by #%d, want clean", p.Outcome, p.By)
	}
}

// A hunk carries context, so a change to the line after someone's work has their
// lines in its range. That is adjacency, not a fix.
func TestAChangeToAdjacentLinesIsNotAHotfix(t *testing.T) {
	// Changes line 13, the line after #1's, and inserts a line after it.
	adjacent := "@@ -10,5 +10,6 @@ func a() {\n" +
		" \tfoo()\n" +
		" \tbar()\n" +
		" \tbaz()\n" +
		"-\treturn\n" +
		"+\treturn nil\n" +
		"+\t// done\n" +
		" }"
	// Inserts a line directly before line 10: still outside #1's block.
	before := "@@ -8,4 +8,5 @@ func a() {\n" +
		" \tx := 1\n" +
		" \ty := 2\n" +
		"+\tlog()\n" +
		" \tfoo()\n" +
		" \tbar()"
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Add foo", "", day0),
			mergedPR(2, "Return nil", "", day0.AddDate(0, 0, 1)),
			mergedPR(3, "Log", "", day0.AddDate(0, 0, 2)),
		},
		files: map[int][]ghclient.ChangedFile{
			1: {file("a.go", "modified", addFoo)},
			2: {file("a.go", "modified", adjacent)},
			3: {file("a.go", "modified", before)},
		},
	}
	if p := measure(t, f)[1]; p.Outcome != outcomeClean {
		t.Errorf("#1 = %s by #%d, want clean", p.Outcome, p.By)
	}
}

// An insertion between two lines somebody added is a change to their block.
func TestAnInsertionInsideAddedLinesIsAHotfix(t *testing.T) {
	inside := "@@ -10,3 +10,4 @@ func a() {\n" +
		" \tfoo()\n" +
		"+\tcheck()\n" +
		" \tbar()\n" +
		" \tbaz()"
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Add foo", "", day0),
			mergedPR(2, "Check", "", day0.AddDate(0, 0, 1)),
		},
		files: map[int][]ghclient.ChangedFile{
			1: {file("a.go", "modified", addFoo)},
			2: {file("a.go", "modified", inside)},
		},
	}
	if p := measure(t, f)[1]; p.Outcome != outcomeHotfixed {
		t.Errorf("#1 = %s, want hotfixed", p.Outcome)
	}
}

func TestDocsOnly(t *testing.T) {
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Docs", "", day0),
			mergedPR(2, "Docs and code", "", day0.AddDate(0, 0, 1)),
			mergedPR(3, "Remove a page", "", day0.AddDate(0, 0, 2)),
			mergedPR(4, "ADR", "", day0.AddDate(0, 0, 3)),
		},
		files: map[int][]ghclient.ChangedFile{
			1: {file("docs/a.md", "added", ""), file("docs/guide/b.md", "modified", "")},
			2: {file("docs/a.md", "modified", ""), file("cmd/x.go", "modified", "")},
			// Inside the allowlist, but whatever linked to it is outside the diff.
			3: {file("docs/old.md", "removed", "")},
			// The directory's own name is not inside it.
			4: {file("docs", "modified", "")},
		},
	}
	got := measure(t, f, "docs/**")
	for n, want := range map[int]bool{1: true, 2: false, 3: false, 4: false} {
		if got[n].DocsOnly != want {
			t.Errorf("#%d docs-only = %v, want %v", n, got[n].DocsOnly, want)
		}
	}
}

// Moving a workflow into docs/ deletes a workflow. Its new name is under the
// allowlist and its old one is not.
func TestARenameIntoTheAllowlistIsNotDocsOnly(t *testing.T) {
	moved := file("docs/ci.yml", "renamed", "")
	moved.PreviousFilename = ".github/workflows/ci.yml"
	within := file("docs/b.md", "renamed", "")
	within.PreviousFilename = "docs/a.md"
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Move CI", "", day0),
			mergedPR(2, "Rename a page", "", day0.AddDate(0, 0, 1)),
		},
		files: map[int][]ghclient.ChangedFile{1: {moved}, 2: {within}},
	}
	got := measure(t, f, "docs/")
	if got[1].DocsOnly {
		t.Error("a rename out of .github/ into docs/ counted as docs-only")
	}
	// A rename is never docs-only, even within the allowlist.
	if got[2].DocsOnly {
		t.Error("a rename within docs/ counted as docs-only")
	}
	// And the old name alone decides it when the status does not: a file
	// whose previous name sits outside the allowlist is not docs-only.
	if docsOnly([]ghclient.ChangedFile{{Filename: "docs/ci.yml", Status: "modified",
		PreviousFilename: ".github/workflows/ci.yml"}}, mustAllow(t, "docs")) {
		t.Error("previous_filename outside the allowlist was ignored")
	}
}

func mustAllow(t *testing.T, entries ...string) []allowEntry {
	t.Helper()
	allow, err := parseAllowlist(entries)
	if err != nil {
		t.Fatal(err)
	}
	return allow
}

// A draft is not waiting for anybody, so the time it spent as one is not the
// reviewers'.
func TestADraftIsTimedFromReadyForReview(t *testing.T) {
	draft := mergedPR(1, "Draft", "", day0)
	plain := mergedPR(2, "Never a draft", "", day0.AddDate(0, 0, 1))
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{draft, plain},
		reviews: map[int][]ghclient.Review{
			1: {
				// A bot's approval is not a human's, and comes first.
				{ID: 10, User: ghclient.User{Login: "ci[bot]", Type: "Bot"}, State: "APPROVED",
					CommitID: "head1", SubmittedAt: draft.CreatedAt.Add(25 * time.Hour)},
				reviewBy(11, "bob", "APPROVED", "head1", draft.CreatedAt.Add(30*time.Hour)),
			},
			2: {reviewBy(21, "bob", "APPROVED", "head2", plain.CreatedAt.Add(5*time.Hour))},
		},
		timeline: map[int][]ghclient.TimelineEvent{
			1: {
				{Event: "committed"},
				{Event: "ready_for_review", CreatedAt: draft.CreatedAt.Add(24 * time.Hour)},
				// After the approval: not what the approval answered.
				{Event: "ready_for_review", CreatedAt: draft.CreatedAt.Add(40 * time.Hour)},
			},
		},
	}
	got := measure(t, f)
	if h := got[1].hoursToApproval(); h != 6 {
		t.Errorf("draft: %.1f hours to first approval, want 6 (from ready for review)", h)
	}
	if h := got[2].hoursToApproval(); h != 5 {
		t.Errorf("never a draft: %.1f hours to first approval, want 5 (from creation)", h)
	}
}

func TestChangedAfterReview(t *testing.T) {
	pr := mergedPR(1, "Feature", "", day0)
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{pr},
		reviews: map[int][]ghclient.Review{1: {
			reviewBy(11, "bob", "CHANGES_REQUESTED", "c1", day0.Add(-40*time.Hour)),
			reviewBy(12, "bob", "APPROVED", "head1", day0.Add(-2*time.Hour)),
		}},
		comments: map[int][]ghclient.ReviewComment{1: {
			{ID: 1, Path: "cmd/a.go", PullRequestReviewID: 11},
			// A later review's comment does not widen the first review's files.
			{ID: 2, Path: "cmd/b.go", PullRequestReviewID: 12},
		}},
		compare: map[string][]ghclient.ChangedFile{
			"c1...head1": {file("cmd/a.go", "modified", "@@ -1 +1 @@\n-x\n+y")},
		},
	}
	if p := measure(t, f)[1]; p.FirstReview != reviewChanged {
		t.Errorf("first review = %s, want %s", p.FirstReview, reviewChanged)
	}
}

func TestUnchangedAfterReview(t *testing.T) {
	commented := mergedPR(1, "Commits elsewhere", "", day0)
	same := mergedPR(2, "Merged as reviewed", "", day0.AddDate(0, 0, 1))
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{commented, same},
		reviews: map[int][]ghclient.Review{
			1: {
				// The author replying to a thread submits a review of their
				// own. It is not the first review.
				reviewBy(10, "alice", "COMMENTED", "c0", day0.Add(-45*time.Hour)),
				reviewBy(11, "bob", "COMMENTED", "c1", day0.Add(-40*time.Hour)),
			},
			2: {reviewBy(21, "bob", "CHANGES_REQUESTED", "head2", day0.Add(-20*time.Hour))},
		},
		comments: map[int][]ghclient.ReviewComment{1: {
			{ID: 1, Path: "cmd/a.go", PullRequestReviewID: 11},
		}},
		compare: map[string][]ghclient.ChangedFile{
			"c1...head1": {file("cmd/b.go", "modified", "@@ -1 +1 @@\n-x\n+y")},
		},
	}
	got := measure(t, f)
	if got[1].FirstReview != reviewUnchanged {
		t.Errorf("commits to other files: first review = %s, want %s", got[1].FirstReview, reviewUnchanged)
	}
	if got[2].FirstReview != reviewUnchanged {
		t.Errorf("merged head is the reviewed commit: first review = %s, want %s", got[2].FirstReview, reviewUnchanged)
	}
	for _, key := range f.compared {
		if strings.HasSuffix(key, "head2") {
			t.Errorf("compared %s; the reviewed commit is the merged head, so there is nothing to compare", key)
		}
	}
}

func TestNoReview(t *testing.T) {
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{mergedPR(1, "Straight in", "", day0)},
		reviews: map[int][]ghclient.Review{1: {
			// A pending review is invisible to everyone but its author, and a
			// bot's review is not a human's.
			{ID: 1, User: ghclient.User{Login: "bob", Type: "User"}, State: "PENDING", CommitID: "head1"},
			{ID: 2, User: ghclient.User{Login: "reviewer[bot]", Type: "Bot"}, State: "COMMENTED",
				CommitID: "head1", SubmittedAt: day0.Add(-time.Hour)},
		}},
	}
	p := measure(t, f)[1]
	if p.FirstReview != reviewNone {
		t.Errorf("first review = %s, want %s", p.FirstReview, reviewNone)
	}
	if p.approved() {
		t.Error("a pull request nobody approved has a time to first approval")
	}
}

// One pull request that could not be read is not a reason to report nothing
// about the rest, and nobody may read the result as complete.
func TestARateLimitMidwayPrintsWhatWasReadAndSaysWhatWasNot(t *testing.T) {
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Add widget", "", day0),
			mergedPR(2, "Something", "", day0.AddDate(0, 0, 1)),
			mergedPR(3, `Revert "Add widget"`, "Reverts owner/name#1", day0.AddDate(0, 0, 2)),
		},
		failFilesOf: 2,
		failErr:     errors.New("GitHub rate limit reached; reset at 1767225600"),
	}
	var stdout, stderr bytes.Buffer
	err := reportOutcomes(context.Background(), f, outcomesRepo, outcomesSince, outcomesNow, nil, "",
		&stdout, &stderr)
	if err != nil {
		t.Fatalf("a partial measurement is not a failed command: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "This summary is incomplete") || !strings.Contains(out, "1 of 3 merged pull requests were read") {
		t.Errorf("want the gap stated in the summary:\n%s", out)
	}
	if !strings.Contains(out, "Not read: #2 and #3") || !strings.Contains(out, "rate limit") {
		t.Errorf("want what was not read, and why:\n%s", out)
	}
	// #1 was read, and its revert is known from the list even though the
	// revert's own details were not read.
	if !strings.Contains(out, "| reverted | 1 |") {
		t.Errorf("the readable pull request's outcome was lost:\n%s", out)
	}
	if !strings.Contains(stderr.String(), "incomplete") {
		t.Errorf("want the gap on stderr too:\n%s", stderr.String())
	}
}

// A clean outcome next to a gap is a guess: the hotfix may be among what was not
// read.
func TestAGapSaysWhichOutcomesItLeavesUnchecked(t *testing.T) {
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Long ago", "", day0),
			mergedPR(2, "Recent", "", day0.AddDate(0, 0, 20)),
			mergedPR(3, "Unread", "", day0.AddDate(0, 0, 21)),
		},
		failFilesOf: 3,
		failErr:     errors.New("GitHub rate limit reached"),
	}
	report := measureOutcomes(context.Background(), f, outcomesRepo, outcomesSince, outcomesNow, nil)
	// #2's window reaches #3; #1's ended before it.
	if report.HotfixUnchecked != 1 {
		t.Errorf("hotfix unchecked = %d, want 1", report.HotfixUnchecked)
	}
}

func TestTheSummaryStatesItsMeasures(t *testing.T) {
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Docs", "", day0),
			mergedPR(2, "Code", "", day0.AddDate(0, 0, 1)),
		},
		files: map[int][]ghclient.ChangedFile{
			1: {file("docs/a.md", "added", "")},
			2: {file("cmd/a.go", "added", "")},
		},
		reviews: map[int][]ghclient.Review{
			1: {reviewBy(11, "bob", "APPROVED", "head1", day0.Add(-46*time.Hour))},
			2: {reviewBy(21, "bob", "APPROVED", "head2", day0.AddDate(0, 0, 1).Add(-38*time.Hour))},
		},
	}
	var stdout, stderr bytes.Buffer
	if err := reportOutcomes(context.Background(), f, outcomesRepo, outcomesSince, outcomesNow,
		mustAllow(t, "docs/**"), "markdown", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, want := range []string{
		"owner/name, 2 pull requests merged since",
		"| hours to first approval, median | 2.0 |",
		"| hours to first approval, 90th percentile | 10.0 |",
		"| approved by a human | 2 of 2 |",
		"| docs-only candidates | 1 |",
		"| clean | 2 |",
		"| unchanged-after-review | 2 |",
		"| no-review | 0 |",
		"`hotfixed` is approximate",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
}

func TestOutcomesCSVHasOneRowPerPullRequest(t *testing.T) {
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Add widget", "", day0),
			mergedPR(2, `Revert "Add widget"`, "Reverts owner/name#1", day0.AddDate(0, 0, 1)),
		},
		reviews: map[int][]ghclient.Review{
			1: {reviewBy(11, "bob", "APPROVED", "head1", day0.Add(-47*time.Hour))},
		},
	}
	var stdout, stderr bytes.Buffer
	if err := reportOutcomes(context.Background(), f, outcomesRepo, outcomesSince, outcomesNow, nil, "csv",
		&stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&stdout).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rows[0], ",") != "number,merged_at,ready_at,first_approval_at,hours_to_first_approval,docs_only,outcome,outcome_by,first_review" {
		t.Fatalf("header = %v", rows[0])
	}
	if len(rows) != 3 {
		t.Fatalf("want a header and two rows, got %v", rows)
	}
	if got := strings.Join(rows[1], ","); got != "1,2026-10-01T09:00:00Z,2026-09-29T09:00:00Z,2026-09-29T10:00:00Z,1.0000,false,reverted,2,unchanged-after-review" {
		t.Errorf("row 1 = %s", got)
	}
	// No approval is an empty field, not a zero a spreadsheet would average in.
	if got := strings.Join(rows[2], ","); got != "2,2026-10-02T09:00:00Z,,,,false,clean,,no-review" {
		t.Errorf("row 2 = %s", got)
	}
	// A CSV has nowhere to carry the caveat, so it goes to stderr.
	if !strings.Contains(stderr.String(), "approximate") {
		t.Errorf("want the hotfix caveat on stderr:\n%s", stderr.String())
	}
}

// "*.md" also matches markdown that code reads and agent instructions; the
// design allows directories only. Refused before any network work.
func TestAnExtensionInTheAllowlistIsMisuse(t *testing.T) {
	for _, entry := range []string{"*.md", "docs/*.md", "**/*.md", "README.md", "*/docs", "**", "../docs", "/docs"} {
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), []string{"outcomes", "--allowlist", entry}, &stdout, &stderr, noEnv)
		var usageErr errUsage
		if !errors.As(err, &usageErr) {
			t.Errorf("--allowlist %q: want a usage error, got %v", entry, err)
		}
	}
}

func TestAllowlistDirectories(t *testing.T) {
	allow := mustAllow(t, "docs/**", ".github/", "site/v1.2/", "packages/*/docs")
	for name, want := range map[string]bool{
		"docs/a.md":                true,
		"docs/deep/er/b.md":        true,
		".github/CODEOWNERS":       true,
		"site/v1.2/index.html":     true,
		"packages/web/docs/x.md":   true,
		"packages/web/src/x.ts":    false,
		"docsite/a.md":             false,
		"src/docs/a.md":            false,
		"packages/web/docs":        false,
		"site/v1.3/index.html":     false,
		"docs":                     false,
		"README.md":                false,
		"packages/a/b/docs/c.md":   false,
		"packages/web/docs/sub/x":  true,
		"packages/web/docsy/sub/x": false,
	} {
		if got := allowed(name, allow); got != want {
			t.Errorf("allowed(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestOutcomesFlagsBelongToOutcomes(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"--allowlist", "docs"}, &stdout, &stderr, noEnv)
	var usageErr errUsage
	if !errors.As(err, &usageErr) {
		t.Errorf("--allowlist on a review: want a usage error, got %v", err)
	}
	err = run(context.Background(), []string{"metrics", "--allowlist", "docs"}, &stdout, &stderr, noEnv)
	if !errors.As(err, &usageErr) {
		t.Errorf("--allowlist on metrics: want a usage error, got %v", err)
	}
	o, err := parse([]string{"outcomes", "--since", "30d", "--format", "csv", "--repo", "a/b",
		"--allowlist", "docs", "--allowlist", "guides/"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(o.allowlist, " ") != "docs guides/" {
		t.Errorf("allowlist = %v, want both kept", o.allowlist)
	}
}

// Through the real client and the command line: the wiring, the default window,
// and that nothing is written.
func TestOutcomesRunsAgainstTheAPI(t *testing.T) {
	merged := time.Now().AddDate(0, 0, -30).UTC()
	var writes int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/1/files"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{"filename": "docs/a.md", "status": "added"}})
		case strings.HasSuffix(r.URL.Path, "/pulls/1/reviews"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id": 1, "user": map[string]string{"login": "bob", "type": "User"}, "state": "APPROVED",
				"commit_id": "abc", "submitted_at": merged.Add(-time.Hour).Format(time.RFC3339)}})
		case strings.HasSuffix(r.URL.Path, "/issues/1/timeline"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"event": "ready_for_review", "created_at": merged.Add(-4 * time.Hour).Format(time.RFC3339)}})
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"number": 1, "updated_at": merged.Format(time.RFC3339), "merged_at": merged.Format(time.RFC3339),
					"created_at": merged.AddDate(0, 0, -1).Format(time.RFC3339),
					"user":       map[string]string{"login": "alice"}, "head": map[string]string{"sha": "abc"}},
				// Closed without merging: not an outcome.
				{"number": 2, "updated_at": merged.Format(time.RFC3339), "merged_at": nil},
				// Merged before the default ninety days.
				{"number": 3, "updated_at": time.Now().Format(time.RFC3339),
					"merged_at": time.Now().AddDate(0, 0, -120).Format(time.RFC3339)},
			})
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	root := t.TempDir()
	writeConfig(t, root, strings.Join([]string{
		"github:",
		"  apiBaseUrl: " + server.URL,
		"  repo: owner/name",
		"",
	}, "\n"))
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"outcomes", "--root", root, "--allowlist", "docs/**"},
		&stdout, &stderr, func(k string) string {
			if k == "GITHUB_TOKEN" {
				return "ghp_test0123456789"
			}
			return ""
		})
	if err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, want := range []string{
		"owner/name, 1 pull request merged since",
		"| hours to first approval, median | 3.0 |",
		"| docs-only candidates | 1 |",
		"| clean | 1 |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s\nstderr:\n%s", want, out, stderr.String())
		}
	}
	if writes != 0 {
		t.Errorf("made %d writes; outcomes is read-only", writes)
	}
}
