// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	prs           []ghclient.PullRequest
	files         map[int][]ghclient.ChangedFile
	reviews       map[int][]ghclient.Review
	comments      map[int][]ghclient.ReviewComment
	issueComments map[int][]ghclient.IssueComment
	timeline      map[int][]ghclient.TimelineEvent
	// compare is keyed "base...head".
	compare map[string][]ghclient.ChangedFile
	// mergeBase and mergeBaseErr are keyed "base...head". A merge base not
	// set is the base itself, as it is for a branch up to date with it.
	mergeBase    map[string]string
	mergeBaseErr map[string]error
	// blame and blameErr are keyed "sha:path". A blame asked for and in
	// neither fails, so an unexpected one shows up as not-checked.
	blame    map[string][]ghclient.BlameRange
	blameErr map[string]error
	// pulls maps a commit to the pull requests GitHub associates with it. A
	// commit not set has none, as one pushed straight to the branch does.
	pulls map[string][]int
	// failFilesOf makes reading that pull request's files fail with failErr.
	failFilesOf int
	failErr     error
	// compared, mergeBases, blamed and looked record every comparison,
	// merge base, blame and commit lookup asked for.
	compared   []string
	mergeBases []string
	blamed     []string
	looked     []string
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
func (f *fakeOutcomes) ListIssueComments(_ context.Context, _ ghclient.Repo, n int) ([]ghclient.IssueComment, error) {
	return f.issueComments[n], nil
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
func (f *fakeOutcomes) Compare(_ context.Context, _ ghclient.Repo, base, head string) (string, error) {
	key := base + "..." + head
	f.mergeBases = append(f.mergeBases, key)
	if err := f.mergeBaseErr[key]; err != nil {
		return "", err
	}
	if sha, ok := f.mergeBase[key]; ok {
		return sha, nil
	}
	return base, nil
}
func (f *fakeOutcomes) PullRequestsForCommit(_ context.Context, _ ghclient.Repo, sha string) ([]int, error) {
	f.looked = append(f.looked, sha)
	return f.pulls[sha], nil
}
func (f *fakeOutcomes) Blame(_ context.Context, _ ghclient.Repo, sha, path string) ([]ghclient.BlameRange, error) {
	key := sha + ":" + path
	f.blamed = append(f.blamed, key)
	if err := f.blameErr[key]; err != nil {
		return nil, err
	}
	ranges, ok := f.blame[key]
	if !ok {
		return nil, fmt.Errorf("unexpected blame of %s", key)
	}
	return ranges, nil
}

var (
	outcomesRepo = ghclient.Repo{Owner: "owner", Name: "name"}
	// day0 is when the first pull request in each test merges.
	day0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	// outcomesNow is well past every window, unless a test says otherwise.
	outcomesNow   = day0.AddDate(0, 2, 0)
	outcomesSince = day0.AddDate(0, 0, -30)
)

// mergedPR is a pull request alice opened two days before it merged, from a
// branch of the repository itself. Its merge commit is merge<n> and its base
// commit base<n>.
func mergedPR(number int, title, body string, merged time.Time) ghclient.PullRequest {
	return ghclient.PullRequest{
		Number: number, Title: title, Body: body, State: "closed",
		User:      ghclient.User{Login: "alice", Type: "User"},
		Head:      ghclient.Ref{SHA: fmt.Sprintf("head%d", number)},
		Base:      ghclient.Ref{SHA: fmt.Sprintf("base%d", number)},
		CreatedAt: merged.Add(-48 * time.Hour), UpdatedAt: merged, MergedAt: merged,
		MergeCommitSHA: fmt.Sprintf("merge%d", number),
	}
}

func file(name, status, patch string) ghclient.ChangedFile {
	return ghclient.ChangedFile{Filename: name, Status: status, Patch: patch}
}

func reviewBy(id int64, login, state, commit string, at time.Time) ghclient.Review {
	return ghclient.Review{ID: id, User: ghclient.User{Login: login, Type: "User"},
		State: state, CommitID: commit, SubmittedAt: at}
}

func query(t *testing.T, allow ...string) outcomeQuery {
	t.Helper()
	return outcomeQuery{Since: outcomesSince, Now: outcomesNow, Allow: mustAllow(t, allow...)}
}

func measure(t *testing.T, f *fakeOutcomes, allow ...string) map[int]prOutcome {
	t.Helper()
	report := measureOutcomes(context.Background(), f, outcomesRepo, query(t, allow...))
	if report.Gap != nil {
		t.Fatalf("unexpected gap: %v", report.Gap)
	}
	byNumber := map[int]prOutcome{}
	for _, p := range report.PRs {
		byNumber[p.Number] = p
	}
	return byNumber
}

func markdown(t *testing.T, f *fakeOutcomes, q outcomeQuery) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if err := reportOutcomes(context.Background(), f, outcomesRepo, q, "markdown", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	return stdout.String()
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

// changeBar replaces line 11, inside a hunk whose context covers 8 to 14.
const changeBar = "@@ -8,7 +8,7 @@ func a() {\n" +
	" \tx := 1\n" +
	" \ty := 2\n" +
	" \tfoo()\n" +
	"-\tbar()\n" +
	"+\tbar(ctx)\n" +
	" \tbaz()\n" +
	" \treturn\n" +
	" }"

// changeBaz replaces line 12.
const changeBaz = "@@ -10,3 +10,3 @@ func a() {\n" +
	" \tfoo()\n" +
	" \tbar()\n" +
	"-\tbaz()\n" +
	"+\tbaz(ctx)"

// fooBlame is a.go's blame once lines 10 to 12 are the given commit's.
func fooBlame(commit string) []ghclient.BlameRange {
	return []ghclient.BlameRange{
		{StartLine: 1, EndLine: 9, Commit: "c0"},
		{StartLine: 10, EndLine: 12, Commit: commit},
		{StartLine: 13, EndLine: 40, Commit: "c0"},
	}
}

func TestALaterChangeToItsLinesIsModifiedLater(t *testing.T) {
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Add foo", "", day0),
			mergedPR(2, "Add feature", "Widens bar.", day0.AddDate(0, 0, 3)),
		},
		files: map[int][]ghclient.ChangedFile{
			1: {file("a.go", "modified", addFoo)},
			2: {file("a.go", "modified", changeBar)},
		},
		blame: map[string][]ghclient.BlameRange{"base2:a.go": fooBlame("merge1")},
	}
	got := measure(t, f)
	if p := got[1]; p.Outcome != outcomeModified || p.By != 2 || p.Unchecked != "" {
		t.Errorf("#1 = %s by #%d (%q), want modified-later by #2", p.Outcome, p.By, p.Unchecked)
	}
	// A squash merge's own commit answers without asking GitHub whose it is.
	if len(f.looked) != 0 {
		t.Errorf("looked up %v; P's merge commit needs no lookup", f.looked)
	}
}

// The later pull request saying it is a fix is what turns a modification into
// a hotfix, and a fix is reported over a plain modification when both happen.
func TestAFixToItsLinesIsAHotfix(t *testing.T) {
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Add foo", "", day0),
			mergedPR(2, "Add feature", "", day0.AddDate(0, 0, 3)),
			mergedPR(3, "fix: baz needs a context", "", day0.AddDate(0, 0, 4)),
			// The same change, but after the window: by then it is just code.
			mergedPR(4, "Fix bar again", "", day0.AddDate(0, 0, 25)),
		},
		files: map[int][]ghclient.ChangedFile{
			1: {file("a.go", "modified", addFoo)},
			2: {file("a.go", "modified", changeBar)},
			3: {file("a.go", "modified", changeBaz)},
			4: {file("a.go", "modified", changeBar)},
		},
		blame: map[string][]ghclient.BlameRange{
			"base2:a.go": fooBlame("merge1"),
			// #2 rewrote line 11; line 12 is still #1's.
			"base3:a.go": {
				{StartLine: 1, EndLine: 10, Commit: "merge1"},
				{StartLine: 11, EndLine: 11, Commit: "merge2"},
				{StartLine: 12, EndLine: 12, Commit: "merge1"},
				{StartLine: 13, EndLine: 40, Commit: "c0"},
			},
		},
	}
	got := measure(t, f)
	if p := got[1]; p.Outcome != outcomeHotfixed || p.By != 3 {
		t.Errorf("#1 = %s by #%d, want hotfixed by #3", p.Outcome, p.By)
	}
	// #3 changed line 12, which blame credits to #1, not to #2.
	if p := got[2]; p.Outcome != outcomeClean {
		t.Errorf("#2 = %s by #%d, want clean", p.Outcome, p.By)
	}
	blamed := map[string]int{}
	for _, key := range f.blamed {
		blamed[key]++
	}
	if blamed["base3:a.go"] != 1 {
		t.Errorf("blamed base3:a.go %d times for two pull requests, want once", blamed["base3:a.go"])
	}
	if blamed["base4:a.go"] != 0 {
		t.Error("blamed #4's base, which merged after every window had closed")
	}
}

func TestFixesReadsTitleBodyAndBranch(t *testing.T) {
	pr := func(title, body, branch string) ghclient.PullRequest {
		return ghclient.PullRequest{Title: title, Body: body, Head: ghclient.Ref{Ref: branch}}
	}
	for _, tt := range []struct {
		name string
		pr   ghclient.PullRequest
		want bool
	}{
		{"title", pr("Fixes the build", "", ""), true},
		{"body", pr("Tidy", "This was a regression.", ""), true},
		{"branch", pr("Tidy", "", "alice/hotfix-login"), true},
		{"a bug branch", pr("Tidy", "", "bug/123"), true},
		{"a revert", pr(`Revert "Add foo"`, "", ""), true},
		{"names it", pr("Tidy", "Follows up #12.", ""), true},
		{"names it last", pr("Follow-up to #12", "", ""), true},
		{"a feature", pr("Add feature", "Widens bar.", "alice/feature"), false},
		// Inside a word is not the word.
		{"a prefix", pr("Add a prefix option", "", "alice/debugger"), false},
		// #123 is not #12.
		{"another number", pr("Tidy", "Follows up #123.", ""), false},
	} {
		if got := fixes(tt.pr, 12); got != tt.want {
			t.Errorf("%s: fixes = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// A hunk carries context, so a change to the line after someone's work has
// their lines in its range. Blame of the changed line says whose it was.
func TestAChangeToNeighbouringLinesIsNotModifiedLater(t *testing.T) {
	// Changes line 13, the line after #1's, and inserts a line after it.
	adjacent := "@@ -10,5 +10,6 @@ func a() {\n" +
		" \tfoo()\n" +
		" \tbar()\n" +
		" \tbaz()\n" +
		"-\treturn\n" +
		"+\treturn nil\n" +
		"+\t// done\n" +
		" }"
	// Inserts a line between two of #1's. It removes and changes nothing, so
	// it needs no blame at all.
	inside := "@@ -10,3 +10,4 @@ func a() {\n" +
		" \tfoo()\n" +
		"+\tcheck()\n" +
		" \tbar()\n" +
		" \tbaz()"
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Add foo", "", day0),
			mergedPR(2, "Fix the return", "", day0.AddDate(0, 0, 1)),
			mergedPR(3, "Fix: check first", "", day0.AddDate(0, 0, 2)),
		},
		files: map[int][]ghclient.ChangedFile{
			1: {file("a.go", "modified", addFoo)},
			2: {file("a.go", "modified", adjacent)},
			3: {file("a.go", "modified", inside)},
		},
		blame: map[string][]ghclient.BlameRange{"base2:a.go": fooBlame("merge1")},
	}
	if p := measure(t, f)[1]; p.Outcome != outcomeClean {
		t.Errorf("#1 = %s by #%d, want clean", p.Outcome, p.By)
	}
	for _, key := range f.blamed {
		if strings.HasPrefix(key, "base3:") {
			t.Errorf("blamed %s for a pull request that only added lines", key)
		}
	}
}

// Somebody else rewrote the lines in between, so the later change is to their
// work, not this pull request's — whatever the line numbers say.
func TestBlameCreditingAnotherCommitIsNotModifiedLater(t *testing.T) {
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Add foo", "", day0),
			mergedPR(2, "Fix bar", "", day0.AddDate(0, 0, 3)),
		},
		files: map[int][]ghclient.ChangedFile{
			1: {file("a.go", "modified", addFoo)},
			2: {file("a.go", "modified", changeBar)},
		},
		blame: map[string][]ghclient.BlameRange{"base2:a.go": fooBlame("someone-else")},
	}
	if p := measure(t, f)[1]; p.Outcome != outcomeClean {
		t.Errorf("#1 = %s by #%d, want clean", p.Outcome, p.By)
	}
}

// The file as it was before the later pull request is the one to blame: a
// rename keeps the lines it carries.
func TestARenameThatChangesItsLinesIsModifiedLater(t *testing.T) {
	moved := file("b.go", "renamed", changeBar)
	moved.PreviousFilename = "a.go"
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Add foo", "", day0),
			mergedPR(2, "Move a to b", "", day0.AddDate(0, 0, 3)),
		},
		files: map[int][]ghclient.ChangedFile{
			1: {file("a.go", "modified", addFoo)},
			2: {moved},
		},
		blame: map[string][]ghclient.BlameRange{"base2:a.go": fooBlame("merge1")},
	}
	if p := measure(t, f)[1]; p.Outcome != outcomeModified || p.By != 2 {
		t.Errorf("#1 = %s by #%d, want modified-later by #2", p.Outcome, p.By)
	}
}

// A blame that could not be read is not a clean result. It is said, with why,
// and the rest of the run goes on.
func TestABlameErrorIsReportedAsNotChecked(t *testing.T) {
	// A diff too large to send comes with no patch, only a count.
	huge := file("a.go", "modified", "")
	huge.Deletions = 9000
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Add foo", "", day0),
			mergedPR(2, "Fix bar", "", day0.AddDate(0, 0, 3)),
			mergedPR(3, "Add foo elsewhere", "", day0.AddDate(0, 0, 20)),
			mergedPR(4, "Rewrite a", "", day0.AddDate(0, 0, 21)),
		},
		files: map[int][]ghclient.ChangedFile{
			1: {file("a.go", "modified", addFoo)},
			2: {file("a.go", "modified", changeBar)},
			3: {file("a.go", "modified", addFoo)},
			4: {huge},
		},
		blameErr: map[string]error{
			"base2:a.go": errors.New("Something went wrong while executing your query"),
		},
	}
	got := measure(t, f)
	if p := got[1]; p.Outcome != outcomeUnchecked || !strings.Contains(p.Unchecked, "blaming a.go at #2's merge base") {
		t.Errorf("#1 = %s (%q), want not-checked with the blame's error", p.Outcome, p.Unchecked)
	}
	if p := got[3]; p.Outcome != outcomeUnchecked || !strings.Contains(p.Unchecked, "no patch for a.go in #4") {
		t.Errorf("#3 = %s (%q), want not-checked for the missing patch", p.Outcome, p.Unchecked)
	}

	out := markdown(t, f, query(t))
	for _, want := range []string{
		"| not-checked | 2 |",
		"| clean | 2 |",
		"#1 (blaming a.go at #2's merge base: Something went wrong",
		"#3 (GitHub sent no patch for a.go in #4)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "incomplete") {
		t.Errorf("a blame error is not a partial read:\n%s", out)
	}
}

// In a repository that merges with merge commits, blame credits P's lines to
// the commits of its branch, never to its merge commit. GitHub's answer to
// which pull request introduced that commit is what makes them P's.
func TestALineFromAMergedBranchIsCreditedThroughItsPullRequest(t *testing.T) {
	for _, tt := range []struct {
		name  string
		pulls []int
		want  string
	}{
		{"a commit of P's branch", []int{1}, outcomeModified},
		// L was branched from P's branch, so the commit is L's as much as
		// P's, and L changing it is not somebody else changing P's work.
		{"a commit both carry", []int{1, 2}, outcomeClean},
		// Pushed straight to the branch: nobody's pull request.
		{"a commit no pull request introduced", nil, outcomeClean},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeOutcomes{
				prs: []ghclient.PullRequest{
					mergedPR(1, "Add foo", "", day0),
					mergedPR(2, "Add feature", "", day0.AddDate(0, 0, 3)),
				},
				files: map[int][]ghclient.ChangedFile{
					1: {file("a.go", "modified", addFoo)},
					2: {file("a.go", "modified", changeBar)},
				},
				blame: map[string][]ghclient.BlameRange{"base2:a.go": fooBlame("p1-branch")},
				pulls: map[string][]int{"p1-branch": tt.pulls},
			}
			p := measure(t, f)[1]
			if p.Outcome != tt.want || p.Unchecked != "" {
				t.Errorf("#1 = %s by #%d (%q), want %s", p.Outcome, p.By, p.Unchecked, tt.want)
			}
			if strings.Join(f.looked, " ") != "p1-branch" {
				t.Errorf("looked up %v, want p1-branch once", f.looked)
			}
		})
	}
}

// Each commit looked up is a request, so a run stops looking past a bound and
// says which pull requests that left unchecked, rather than calling them clean.
func TestTheLookupBoundLeavesTheRestNotChecked(t *testing.T) {
	defer func(was int) { commitLookupCap = was }(commitLookupCap)
	commitLookupCap = 1
	// Replaces lines 11 and 12.
	twoLines := "@@ -10,3 +10,3 @@ func a() {\n" +
		" \tfoo()\n" +
		"-\tbar()\n" +
		"-\tbaz()\n" +
		"+\tbar(ctx)\n" +
		"+\tbaz(ctx)"
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Add foo", "", day0),
			mergedPR(2, "Add feature", "", day0.AddDate(0, 0, 3)),
		},
		files: map[int][]ghclient.ChangedFile{
			1: {file("a.go", "modified", addFoo)},
			2: {file("a.go", "modified", twoLines)},
		},
		blame: map[string][]ghclient.BlameRange{"base2:a.go": {
			{StartLine: 1, EndLine: 11, Commit: "x1"},
			{StartLine: 12, EndLine: 40, Commit: "x2"},
		}},
	}
	p := measure(t, f)[1]
	if p.Outcome != outcomeUnchecked || !strings.Contains(p.Unchecked, "bound of 1 commit lookups") {
		t.Errorf("#1 = %s (%q), want not-checked at the lookup bound", p.Outcome, p.Unchecked)
	}
	if strings.Join(f.looked, " ") != "x1" {
		t.Errorf("looked up %v, want only x1 before the bound", f.looked)
	}
}

// L's patch is numbered against the merge base of its branch, not against its
// base commit, which is wherever the base branch stood when GitHub last
// updated L. Blame has to read the file the line numbers describe.
func TestBlameRunsAtTheMergeBase(t *testing.T) {
	both := []ghclient.ChangedFile{file("a.go", "modified", changeBar), file("b.go", "modified", changeBar)}
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Add foo", "", day0),
			mergedPR(2, "Add foo to b", "", day0.AddDate(0, 0, 1)),
			mergedPR(3, "Add feature", "", day0.AddDate(0, 0, 3)),
		},
		files: map[int][]ghclient.ChangedFile{
			1: {file("a.go", "modified", addFoo)},
			2: {file("b.go", "modified", addFoo)},
			3: both,
		},
		mergeBase: map[string]string{"base3...head3": "mb3"},
		blame: map[string][]ghclient.BlameRange{
			"mb3:a.go": fooBlame("merge1"),
			"mb3:b.go": fooBlame("merge2"),
		},
	}
	got := measure(t, f)
	for _, n := range []int{1, 2} {
		if p := got[n]; p.Outcome != outcomeModified || p.By != 3 {
			t.Errorf("#%d = %s by #%d (%q), want modified-later by #3", n, p.Outcome, p.By, p.Unchecked)
		}
	}
	if strings.Join(f.blamed, " ") != "mb3:a.go mb3:b.go" {
		t.Errorf("blamed %v, want both files at the merge base mb3", f.blamed)
	}
	// Two earlier pull requests share the later one: one merge base.
	if strings.Join(f.mergeBases, " ") != "base3...head3" {
		t.Errorf("asked for merge bases %v, want #3's once", f.mergeBases)
	}
	// Each line is a merge commit of the window's, so nothing is looked up.
	if len(f.looked) != 0 {
		t.Errorf("looked up %v; a merge commit needs no lookup", f.looked)
	}
}

func TestAMergeBaseGitHubCannotFindLeavesItNotChecked(t *testing.T) {
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Add foo", "", day0),
			mergedPR(2, "Add feature", "", day0.AddDate(0, 0, 3)),
		},
		files: map[int][]ghclient.ChangedFile{
			1: {file("a.go", "modified", addFoo)},
			2: {file("a.go", "modified", changeBar)},
		},
		mergeBaseErr: map[string]error{"base2...head2": fmt.Errorf("%w (head force-pushed)", ghclient.ErrNotFound)},
	}
	p := measure(t, f)[1]
	if p.Outcome != outcomeUnchecked || !strings.Contains(p.Unchecked, "finding #2's merge base") {
		t.Errorf("#1 = %s (%q), want not-checked for the merge base", p.Outcome, p.Unchecked)
	}
	if len(f.blamed) != 0 {
		t.Errorf("blamed %v with no merge base to blame at", f.blamed)
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

// The native count is what GitHub's own settings would let through: every path
// under the allowlist, whatever was done to it. A removal and a rename within
// it count there and not in the strict count; a rename from outside counts in
// neither, because its old path changed too.
func TestTheNativeCountIncludesRemovalsAndRenames(t *testing.T) {
	within := file("docs/b.md", "renamed", "")
	within.PreviousFilename = "docs/a.md"
	moved := file("docs/ci.yml", "renamed", "")
	moved.PreviousFilename = ".github/workflows/ci.yml"
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Docs", "", day0),
			mergedPR(2, "Remove a page", "", day0.AddDate(0, 0, 1)),
			mergedPR(3, "Rename a page", "", day0.AddDate(0, 0, 2)),
			mergedPR(4, "Move CI", "", day0.AddDate(0, 0, 3)),
			mergedPR(5, "Docs and code", "", day0.AddDate(0, 0, 4)),
		},
		files: map[int][]ghclient.ChangedFile{
			1: {file("docs/a.md", "added", "")},
			2: {file("docs/old.md", "removed", ""), file("docs/index.md", "modified", "")},
			3: {within},
			4: {moved},
			5: {file("docs/a.md", "modified", ""), file("cmd/x.go", "removed", "")},
		},
	}
	got := measure(t, f, "docs/")
	for n, want := range map[int][2]bool{
		1: {true, true}, 2: {false, true}, 3: {false, true}, 4: {false, false}, 5: {false, false},
	} {
		if got[n].DocsOnly != want[0] || got[n].DocsNative != want[1] {
			t.Errorf("#%d strict, native = %v, %v; want %v, %v",
				n, got[n].DocsOnly, got[n].DocsNative, want[0], want[1])
		}
	}
	out := markdown(t, f, query(t, "docs/"))
	for _, want := range []string{"| docs-only candidates, strict | 1 |", "| docs-only candidates, native | 3 |"} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
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

// The merges nobody reviewed are the ones a person samples by hand once
// anything merges unreviewed, so --only-unreviewed lists those and no others.
func TestOnlyUnreviewedListsMergesNoHumanReviewed(t *testing.T) {
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Reviewed", "", day0),
			mergedPR(2, "Straight in", "", day0.AddDate(0, 0, 1)),
			mergedPR(3, "Only a bot looked", "", day0.AddDate(0, 0, 2)),
		},
		files: map[int][]ghclient.ChangedFile{
			2: {file("docs/a.md", "modified", "")},
			3: {file("docs/old.md", "removed", "")},
		},
		reviews: map[int][]ghclient.Review{
			1: {reviewBy(11, "bob", "APPROVED", "head1", day0.Add(-time.Hour))},
			3: {{ID: 31, User: ghclient.User{Login: "ci[bot]", Type: "Bot"}, State: "APPROVED",
				CommitID: "head3", SubmittedAt: day0.Add(47 * time.Hour)}},
		},
	}
	q := query(t, "docs/")
	q.Unreviewed = true
	out := markdown(t, f, q)
	for _, want := range []string{
		"# Merged pull requests with no human review",
		"owner/name, 2 of 3 pull requests merged since",
		"| #2 | 2026-10-02 | yes | yes | clean |",
		"| #3 | 2026-10-03 | no | yes | clean |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "| #1 |") {
		t.Errorf("listed #1, which bob approved:\n%s", out)
	}

	var stdout, stderr bytes.Buffer
	if err := reportOutcomes(context.Background(), f, outcomesRepo, q, "csv", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&stdout).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[1][0] != "2" || rows[2][0] != "3" {
		t.Errorf("want a header and rows for #2 and #3, got %v", rows)
	}
}

func TestOnlyUnreviewedBelongsToOutcomes(t *testing.T) {
	var stdout, stderr bytes.Buffer
	var usageErr errUsage
	for _, args := range [][]string{{"--only-unreviewed"}, {"metrics", "--only-unreviewed"}} {
		if err := run(context.Background(), args, &stdout, &stderr, noEnv); !errors.As(err, &usageErr) {
			t.Errorf("%v: want a usage error, got %v", args, err)
		}
	}
	o, err := parse([]string{"outcomes", "--only-unreviewed"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if !o.unreviewed {
		t.Error("--only-unreviewed was not kept")
	}
}

// G1's check: every merged pull request has a risk record, and only this tool's
// own comment is one.
func TestMergesWithoutARiskRecordAreListed(t *testing.T) {
	fork := mergedPR(4, "From a fork", "", day0.AddDate(0, 0, 3))
	fork.Head.Repo = &ghclient.RefRepo{FullName: "someone/name"}
	own := mergedPR(6, "Own branch", "", day0.AddDate(0, 0, 5))
	own.Head.Repo = &ghclient.RefRepo{FullName: "Owner/Name"}
	self := ghclient.User{Login: "reviewer-bot", Type: "Bot"}
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			mergedPR(1, "Recorded", "", day0),
			mergedPR(2, "Quoted", "", day0.AddDate(0, 0, 1)),
			mergedPR(3, "Nothing", "", day0.AddDate(0, 0, 2)),
			fork,
			mergedPR(5, "Labels list only", "", day0.AddDate(0, 0, 4)),
			own,
		},
		issueComments: map[int][]ghclient.IssueComment{
			1: {{Body: "<!-- rv:risk -->\nTouches auth.", User: self}},
			// Somebody quoting the record has not made one.
			2: {{Body: "> <!-- rv:risk -->\n> Touches auth.\nWhy?", User: ghclient.User{Login: "alice"}}},
			5: {{Body: "<!-- rv:risk-labels risk/auth -->", User: self}},
			6: {{Body: "<!-- rv:risk -->", User: self}},
		},
	}
	q := query(t)
	q.Self = "reviewer-bot"
	out := markdown(t, f, q)
	for _, want := range []string{
		"## Risk records",
		"3 of 6 merged pull requests read have no `<!-- rv:risk -->` comment by `reviewer-bot`: #2, #3 and #5.",
		"Not counted: #4 from a fork",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}

	// With the login unknown, any author's marker counts, as metrics does.
	if !hasRiskRecord(f.issueComments[2], "") {
		t.Error("with no login known, a comment carrying the marker was not taken for a record")
	}
	if hasRiskRecord(f.issueComments[2], "reviewer-bot") {
		t.Error("another account's comment was taken for this tool's record")
	}
}

// G6b's check: per risk/ label, how often it fired and whether the pull
// requests it fired on waited longer for an approval than the rest.
func TestThePerLabelTable(t *testing.T) {
	labelled := func(pr ghclient.PullRequest, names ...string) ghclient.PullRequest {
		for _, n := range names {
			pr.Labels = append(pr.Labels, ghclient.Label{Name: n})
		}
		return pr
	}
	prs := []ghclient.PullRequest{
		labelled(mergedPR(1, "Auth", "", day0), "risk/auth"),
		labelled(mergedPR(2, "Auth and CI", "", day0.AddDate(0, 0, 1)), "risk/auth", "risk/ci"),
		mergedPR(3, "Plain", "", day0.AddDate(0, 0, 2)),
		labelled(mergedPR(4, "CI, unapproved", "", day0.AddDate(0, 0, 3)), "risk/ci"),
		labelled(mergedPR(5, "Docs", "", day0.AddDate(0, 0, 4)), "documentation"),
	}
	approvedAfter := func(pr ghclient.PullRequest, hours int) []ghclient.Review {
		return []ghclient.Review{reviewBy(int64(pr.Number), "bob", "APPROVED", pr.Head.SHA,
			pr.CreatedAt.Add(time.Duration(hours)*time.Hour))}
	}
	f := &fakeOutcomes{
		prs: prs,
		reviews: map[int][]ghclient.Review{
			1: approvedAfter(prs[0], 2),
			2: approvedAfter(prs[1], 10),
			3: approvedAfter(prs[2], 4),
			5: approvedAfter(prs[4], 6),
		},
	}
	out := markdown(t, f, query(t))
	for _, want := range []string{
		"## Risk labels",
		"| label | pull requests | median hours to first approval, with | without |",
		// With: 2 and 10. Without: 4 and 6. Nearest rank takes the lower.
		"| risk/auth | 2 of 5 | 2.0 | 4.0 |",
		// #4 fired the label but was never approved, so it has no time.
		"| risk/ci | 2 of 5 | 10.0 | 4.0 |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "| documentation |") {
		t.Errorf("a label outside risk/ has a row:\n%s", out)
	}

	f.prs = []ghclient.PullRequest{mergedPR(1, "Plain", "", day0)}
	if out := markdown(t, f, query(t)); !strings.Contains(out, "No merged pull request read carried a `risk/` label.") {
		t.Errorf("want the absence of labels said:\n%s", out)
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
	err := reportOutcomes(context.Background(), f, outcomesRepo, query(t), "", &stdout, &stderr)
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
	report := measureOutcomes(context.Background(), f, outcomesRepo, query(t))
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
	out := markdown(t, f, query(t, "docs/**"))
	for _, want := range []string{
		"owner/name, 2 pull requests merged since",
		"| hours to first approval, median | 2.0 |",
		"| hours to first approval, 90th percentile | 10.0 |",
		"| approved by a human | 2 of 2 |",
		"| docs-only candidates, strict | 1 |",
		"| docs-only candidates, native | 1 |",
		"| modified-later | 0 |",
		"| clean | 2 |",
		"| unchanged-after-review | 2 |",
		"| no-review | 0 |",
		"`modified-later` and `hotfixed` find a later change by blame",
		"however that pull request was merged",
		"audit it by hand",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	// Nothing went unchecked, so nothing says so.
	if strings.Contains(out, "| not-checked |") {
		t.Errorf("a not-checked row with nothing unchecked:\n%s", out)
	}
}

func TestOutcomesCSVHasOneRowPerPullRequest(t *testing.T) {
	recorded := mergedPR(1, "Add widget", "", day0)
	recorded.Labels = []ghclient.Label{{Name: "risk/auth"}, {Name: "risk/ci"}, {Name: "documentation"}}
	f := &fakeOutcomes{
		prs: []ghclient.PullRequest{
			recorded,
			mergedPR(2, `Revert "Add widget"`, "Reverts owner/name#1", day0.AddDate(0, 0, 1)),
		},
		reviews: map[int][]ghclient.Review{
			1: {reviewBy(11, "bob", "APPROVED", "head1", day0.Add(-47*time.Hour))},
		},
		issueComments: map[int][]ghclient.IssueComment{
			1: {{Body: "<!-- rv:risk -->", User: ghclient.User{Login: "reviewer-bot"}}},
		},
	}
	var stdout, stderr bytes.Buffer
	if err := reportOutcomes(context.Background(), f, outcomesRepo, query(t), "csv", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&stdout).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(rows[0], ","); got != "number,merged_at,ready_at,first_approval_at,hours_to_first_approval,"+
		"docs_only_strict,docs_only_native,outcome,outcome_by,not_checked,first_review,risk_record,from_fork,risk_labels" {
		t.Fatalf("header = %s", got)
	}
	if len(rows) != 3 {
		t.Fatalf("want a header and two rows, got %v", rows)
	}
	if got := strings.Join(rows[1], ","); got != "1,2026-10-01T09:00:00Z,2026-09-29T09:00:00Z,2026-09-29T10:00:00Z,"+
		"1.0000,false,false,reverted,2,,unchanged-after-review,true,false,risk/auth;risk/ci" {
		t.Errorf("row 1 = %s", got)
	}
	// No approval is an empty field, not a zero a spreadsheet would average in.
	if got := strings.Join(rows[2], ","); got != "2,2026-10-02T09:00:00Z,,,,false,false,clean,,,no-review,false,false," {
		t.Errorf("row 2 = %s", got)
	}
	// A CSV has nowhere to carry the caveat, so it goes to stderr.
	if !strings.Contains(stderr.String(), "merge base") {
		t.Errorf("want the attribution caveat on stderr:\n%s", stderr.String())
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
// blame over GraphQL, and that nothing is written.
func TestOutcomesRunsAgainstTheAPI(t *testing.T) {
	merged := time.Now().AddDate(0, 0, -30).UTC()
	fixed := merged.AddDate(0, 0, 2)
	var writes int
	var blamed []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/graphql" {
			// A GraphQL read is a POST too; only a mutation writes.
			body, _ := io.ReadAll(r.Body)
			var request struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			_ = json.Unmarshal(body, &request)
			if strings.Contains(request.Query, "mutation") {
				writes++
			}
			blamed = append(blamed, fmt.Sprintf("%v:%v", request.Variables["oid"], request.Variables["path"]))
			// c1 is a commit of #1's branch, which a merge commit brought in.
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"repository": map[string]any{
				"object": map[string]any{"blame": map[string]any{"ranges": []map[string]any{
					{"startingLine": 1, "endingLine": 2, "commit": map[string]string{"oid": "c1"}},
				}}}}}})
			return
		}
		if r.Method != http.MethodGet {
			writes++
		}
		switch {
		case r.URL.Path == "/user":
			_ = json.NewEncoder(w).Encode(map[string]string{"login": "reviewer-bot"})
		case strings.HasSuffix(r.URL.Path, "/pulls/1/files"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{"filename": "docs/a.md", "status": "added",
				"patch": "@@ -0,0 +1,2 @@\n+old\n+keep"}})
		case strings.HasSuffix(r.URL.Path, "/pulls/4/files"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{"filename": "docs/a.md", "status": "modified",
				"patch": "@@ -1,2 +1,2 @@\n-old\n+new\n keep", "deletions": 1}})
		case strings.HasSuffix(r.URL.Path, "/pulls/1/reviews"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id": 1, "user": map[string]string{"login": "bob", "type": "User"}, "state": "APPROVED",
				"commit_id": "abc", "submitted_at": merged.Add(-time.Hour).Format(time.RFC3339)}})
		case strings.HasSuffix(r.URL.Path, "/pulls/4/reviews"):
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case strings.HasSuffix(r.URL.Path, "/issues/1/timeline"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"event": "ready_for_review", "created_at": merged.Add(-4 * time.Hour).Format(time.RFC3339)}})
		case strings.HasSuffix(r.URL.Path, "/issues/1/comments"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 7, "body": "<!-- rv:risk -->\nTouches docs.",
				"user": map[string]string{"login": "reviewer-bot", "type": "Bot"}}})
		case strings.HasSuffix(r.URL.Path, "/issues/4/comments"):
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case strings.HasSuffix(r.URL.Path, "/compare/b4...def"):
			_ = json.NewEncoder(w).Encode(map[string]any{"merge_base_commit": map[string]string{"sha": "mb4"}})
		case strings.HasSuffix(r.URL.Path, "/commits/c1/pulls"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{"number": 1}})
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"number": 4, "title": "Correct the docs", "updated_at": fixed.Format(time.RFC3339),
					"merged_at": fixed.Format(time.RFC3339), "created_at": fixed.Add(-time.Hour).Format(time.RFC3339),
					"merge_commit_sha": "m4", "user": map[string]string{"login": "carol"},
					"head": map[string]string{"ref": "carol/fix-typo", "sha": "def"},
					"base": map[string]string{"sha": "b4"}},
				{"number": 1, "updated_at": merged.Format(time.RFC3339), "merged_at": merged.Format(time.RFC3339),
					"created_at": merged.AddDate(0, 0, -1).Format(time.RFC3339), "merge_commit_sha": "m1",
					"labels": []map[string]string{{"name": "risk/docs"}},
					"user":   map[string]string{"login": "alice"}, "head": map[string]string{"sha": "abc"}},
				// Closed without merging: not an outcome.
				{"number": 2, "updated_at": merged.Format(time.RFC3339), "merged_at": nil},
				// Merged before the default ninety days.
				{"number": 3, "updated_at": time.Now().AddDate(0, 0, -100).Format(time.RFC3339),
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
		"owner/name, 2 pull requests merged since",
		"| hours to first approval, median | 3.0 |",
		"| docs-only candidates, strict | 2 |",
		// #4 removed a line blame credits to a commit GitHub says #1
		// introduced, from a fix- branch.
		"| hotfixed | 1 |",
		"| clean | 1 |",
		"1 of 2 merged pull requests read have no `<!-- rv:risk -->` comment by `reviewer-bot`: #4.",
		"| risk/docs | 1 of 2 | 3.0 | n/a |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s\nstderr:\n%s", want, out, stderr.String())
		}
	}
	if strings.Join(blamed, " ") != "mb4:docs/a.md" {
		t.Errorf("blamed %v, want docs/a.md at #4's merge base only", blamed)
	}
	if writes != 0 {
		t.Errorf("made %d writes; outcomes is read-only", writes)
	}
}
