// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/adamtait/reviewer/internal/config"
	"github.com/adamtait/reviewer/internal/diff"
	"github.com/adamtait/reviewer/internal/ghclient"
)

// outcomes measures what happened to the pull requests that merged in a window:
// how long they waited for a first approval, which of them touched nothing but
// an allowlist, which were reverted, changed or hotfixed afterwards, whether the
// first human review changed anything, and whether each carried a risk record.
//
// It is the measurement the risk-gated merge design rests on
// (docs/designs/risk-gated-merge.md, Part 3). Before any class of pull request
// merges without a human, there has to be a record of how often that class went
// wrong when a human did look — and a pull request a reviewer sent back counts as
// one that would have been wrong unreviewed. Every heuristic here is checked
// against a hand-audited sample before anyone relies on it.
//
// It writes nothing to GitHub. It is a measurement, not a review or a check, so
// it exits 0 on success and returns ordinary errors otherwise.
func outcomes(ctx context.Context, o options, stdout, stderr io.Writer, getenv func(string) string) error {
	window := o.since
	if window == "" {
		// Ninety days rather than metrics' seven: reverts and hotfixes are rare,
		// and a week of them is not a rate.
		window = "90d"
	}
	since, err := parseSince(window)
	if err != nil {
		return errUsage{err}
	}

	// Validated before any network work, as metrics does.
	switch o.format {
	case "", "markdown", "csv":
	default:
		return errUsage{fmt.Errorf("--format is markdown or csv, not %q", o.format)}
	}
	allow, err := parseAllowlist(o.allowlist)
	if err != nil {
		return errUsage{err}
	}

	cfg, secrets, err := config.Resolve(o.root, o.config, getenv)
	if err != nil {
		return err
	}
	if o.repo != "" {
		cfg.GitHub.Repo = o.repo
	}
	client, repo, err := githubClient(cfg, secrets)
	if err != nil {
		return err
	}
	q := outcomeQuery{
		Since: since, Now: time.Now(), Allow: allow,
		Self:       identify(ctx, client, cfg),
		Unreviewed: o.unreviewed,
	}
	return reportOutcomes(ctx, client, repo, q, o.format, stdout, stderr)
}

// outcomeQuery is what one run measures, and how it reports it.
type outcomeQuery struct {
	Since time.Time
	Now   time.Time
	Allow []allowEntry
	// Self is the login this tool posts as, so that somebody else's comment
	// quoting the risk marker is not taken for a record. Empty when it cannot be
	// worked out, and then any author's marker counts, as metrics does.
	Self string
	// Unreviewed narrows the output to the merges no human reviewed. It
	// changes what is printed, not what is read: a later pull request still
	// has to be read to label an earlier one.
	Unreviewed bool
}

// outcomeReader is the part of the GitHub API outcomes reads. Narrower than
// *ghclient.REST so the tests can stand in for it, and wider than ghclient.Client
// because the timeline, comparisons, blame and a commit's pull requests are
// reads only this command needs.
type outcomeReader interface {
	ListPullRequestsUpdatedSince(ctx context.Context, repo ghclient.Repo, since time.Time) ([]ghclient.PullRequest, error)
	ListFiles(ctx context.Context, repo ghclient.Repo, number int) ([]ghclient.ChangedFile, error)
	ListReviews(ctx context.Context, repo ghclient.Repo, number int) ([]ghclient.Review, error)
	ListReviewComments(ctx context.Context, repo ghclient.Repo, number int) ([]ghclient.ReviewComment, error)
	ListIssueComments(ctx context.Context, repo ghclient.Repo, number int) ([]ghclient.IssueComment, error)
	ListTimelineEvents(ctx context.Context, repo ghclient.Repo, number int) ([]ghclient.TimelineEvent, error)
	CompareFiles(ctx context.Context, repo ghclient.Repo, base, head string) ([]ghclient.ChangedFile, error)
	Compare(ctx context.Context, repo ghclient.Repo, base, head string) (string, error)
	Blame(ctx context.Context, repo ghclient.Repo, sha, path string) ([]ghclient.BlameRange, error)
	PullRequestsForCommit(ctx context.Context, repo ghclient.Repo, sha string) ([]int, error)
}

// The labels, as they appear in the output.
const (
	outcomeReverted = "reverted"
	outcomeHotfixed = "hotfixed"
	outcomeModified = "modified-later"
	// outcomeUnchecked is a pull request a later one may have changed, where
	// the blame that would say so could not be read. Clean would be a guess.
	outcomeUnchecked = "not-checked"
	outcomeClean     = "clean"

	reviewChanged   = "changed-after-review"
	reviewUnchanged = "unchanged-after-review"
	reviewNone      = "no-review"
	// reviewUnknown is a first review whose commit GitHub can no longer
	// compare, usually because a force-push left it unreachable, or whose
	// comparison was too large to list in full. Neither label would be true.
	reviewUnknown = "unknown"
)

// laterWindow is how long after a merge a change to its lines counts against
// it. Later than this, the lines are just code that somebody changed.
const laterWindow = 14 * 24 * time.Hour

// maxListedFiles is the most files GitHub lists for one pull request. A pull
// request with this many may have more, and one of the unlisted ones could be
// anything, so it is never docs-only.
const maxListedFiles = 3000

// riskMarker is the hidden line `reviewer risk` puts in its one comment on a
// pull request (Part 1 of the design). Its labels-list sibling,
// "<!-- rv:risk-labels ... -->", does not contain it.
const riskMarker = "<!-- rv:risk -->"

// riskLabelPrefix starts the name of every GitHub label `reviewer risk` sets.
const riskLabelPrefix = "risk/"

// prOutcome is one merged pull request's record.
type prOutcome struct {
	Number   int
	MergedAt time.Time
	// ReadyAt and ApprovedAt are zero when no human approved it.
	ReadyAt    time.Time
	ApprovedAt time.Time
	// DocsOnly is the strict count: every file added or modified under the
	// allowlist. DocsNative is the native one: every changed path under it,
	// whatever happened to it.
	DocsOnly   bool
	DocsNative bool
	Outcome    string
	// By is the pull request that reverted, hotfixed or modified this one.
	By int
	// Unchecked says why a later change could not be ruled out, when blame
	// could not be read for one. It is set beside modified-later too: a
	// hotfix may be hidden behind what could not be read.
	Unchecked   string
	FirstReview string
	// RiskRecord is whether the pull request carries this tool's risk
	// comment. Fork is whether it came from a fork, whose token cannot post
	// one, so its absence is expected.
	RiskRecord bool
	Fork       bool
	// RiskLabels are its labels under risk/.
	RiskLabels []string
}

// approved reports whether a human approved it, and so whether it has a time
// to first approval at all.
func (p prOutcome) approved() bool { return !p.ApprovedAt.IsZero() }

func (p prOutcome) hoursToApproval() float64 { return p.ApprovedAt.Sub(p.ReadyAt).Hours() }

// outcomeReport is everything one run measured, and what it could not.
type outcomeReport struct {
	// PRs are the pull requests read in full, in the order they merged.
	PRs []prOutcome
	// Merged is how many pull requests merged in the window, read or not.
	Merged int
	// Unread are the merged pull requests not read, because Gap stopped the run.
	Unread []int
	Gap    error
	// HotfixUnchecked counts read pull requests whose window reaches into the
	// unread ones, so a hotfix of theirs may be among what was missed.
	HotfixUnchecked int
	// WindowOpen counts pull requests merged less than laterWindow ago: a
	// change to their lines may not have happened yet.
	WindowOpen int
}

// reportOutcomes measures and prints. A partial measurement is printed, not
// discarded, with the gap stated, for the reason metrics gives: one pull request
// that could not be read is not a reason to report nothing about the rest.
func reportOutcomes(ctx context.Context, client outcomeReader, repo ghclient.Repo, q outcomeQuery,
	format string, stdout, stderr io.Writer) error {

	report := measureOutcomes(ctx, client, repo, q)
	if report.Gap != nil {
		fmt.Fprintf(stderr, "reviewer: the summary below is incomplete: %v\n", report.Gap)
	}
	if report.Merged == 0 && report.Gap == nil {
		fmt.Fprintf(stderr, "reviewer: no pull requests merged since %s\n", q.Since.Format(time.DateOnly))
	}
	if format == "csv" {
		// A CSV has nowhere to say it, and the caveat must reach whoever
		// reads the numbers.
		fmt.Fprintf(stderr, "reviewer: %s and %s %s. %s %s.\n",
			outcomeModified, outcomeHotfixed, attributionCaveat, outcomeHotfixed, intentCaveat)
		return writeOutcomesCSV(stdout, report, q)
	}
	if q.Unreviewed {
		return writeUnreviewedMarkdown(stdout, report, repo, q)
	}
	return writeOutcomesMarkdown(stdout, report, repo, q)
}

// measureOutcomes reads every pull request merged since the cutoff, oldest
// merge first, and labels each one.
func measureOutcomes(ctx context.Context, client outcomeReader, repo ghclient.Repo, q outcomeQuery) outcomeReport {
	var report outcomeReport
	listed, err := client.ListPullRequestsUpdatedSince(ctx, repo, q.Since)
	if err != nil {
		report.Gap = fmt.Errorf("listing pull requests: %w", err)
		return report
	}

	// A pull request merged in the window was updated in it too, so the
	// update-ordered list contains every one of them.
	var merged []ghclient.PullRequest
	for _, pr := range listed {
		if !pr.MergedAt.IsZero() && !pr.MergedAt.Before(q.Since) {
			merged = append(merged, pr)
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if !merged[i].MergedAt.Equal(merged[j].MergedAt) {
			return merged[i].MergedAt.Before(merged[j].MergedAt)
		}
		return merged[i].Number < merged[j].Number
	})
	report.Merged = len(merged)

	// Reverts come from the list alone — title, body, merge time — so they are
	// found even for pull requests whose details were never read.
	reverts := findReverts(merged, repo)

	files := map[int][]ghclient.ChangedFile{}
	for i, pr := range merged {
		record, prFiles, err := readOutcome(ctx, client, repo, pr, q)
		if err != nil {
			report.Gap = err
			for _, rest := range merged[i:] {
				report.Unread = append(report.Unread, rest.Number)
			}
			break
		}
		files[pr.Number] = prFiles
		report.PRs = append(report.PRs, record)
	}

	att := newAttribution(client, repo, merged)
	for i := range report.PRs {
		p := &report.PRs[i]
		if by := reverts[p.Number]; by != 0 {
			// A revert removed every line, so blame has nothing more to say.
			p.Outcome, p.By = outcomeReverted, by
		} else {
			// report.PRs[i] is merged[i]: both are in merge order, and the
			// read stops at the first failure.
			later := merged[i+1 : len(report.PRs)]
			p.Outcome, p.By, p.Unchecked = att.laterOutcome(ctx, merged[i], later, files)
		}
		if q.Now.Sub(p.MergedAt) < laterWindow {
			report.WindowOpen++
		}
		if len(report.Unread) > 0 && p.Outcome != outcomeReverted && p.Outcome != outcomeHotfixed {
			firstUnread := merged[len(report.PRs)].MergedAt
			if !firstUnread.After(p.MergedAt.Add(laterWindow)) {
				report.HotfixUnchecked++
			}
		}
	}
	return report
}

// readOutcome reads one pull request's files, reviews and comments and works
// out every label that does not depend on later pull requests.
func readOutcome(ctx context.Context, client outcomeReader, repo ghclient.Repo, pr ghclient.PullRequest,
	q outcomeQuery) (prOutcome, []ghclient.ChangedFile, error) {

	record := prOutcome{Number: pr.Number, MergedAt: pr.MergedAt}
	// A deleted fork's head repository reads as null, so its pull request
	// counts as the repository's own and its missing record is reported:
	// listing one too many is the safer error for a check that every pull
	// request has one.
	record.Fork = pr.Head.Repo != nil && !strings.EqualFold(pr.Head.Repo.FullName, repo.String())
	for _, l := range pr.Labels {
		if strings.HasPrefix(l.Name, riskLabelPrefix) {
			record.RiskLabels = append(record.RiskLabels, l.Name)
		}
	}

	files, err := client.ListFiles(ctx, repo, pr.Number)
	if err != nil {
		return record, nil, fmt.Errorf("reading the files of #%d: %w", pr.Number, err)
	}
	record.DocsOnly = docsOnly(files, q.Allow)
	record.DocsNative = docsOnlyNative(files, q.Allow)

	reviews, err := client.ListReviews(ctx, repo, pr.Number)
	if err != nil {
		return record, nil, fmt.Errorf("reading the reviews of #%d: %w", pr.Number, err)
	}
	human := humanReviews(reviews, pr.User.Login)

	for _, r := range human {
		if r.State != "APPROVED" {
			continue
		}
		record.ApprovedAt = r.SubmittedAt
		// The timeline is read only when there is an approval to time: it
		// is one more request per pull request, and without an approval
		// there is nothing to measure from it.
		events, err := client.ListTimelineEvents(ctx, repo, pr.Number)
		if err != nil {
			return record, nil, fmt.Errorf("reading the timeline of #%d: %w", pr.Number, err)
		}
		record.ReadyAt = readyAt(pr.CreatedAt, events, r.SubmittedAt)
		break
	}

	if len(human) == 0 {
		record.FirstReview = reviewNone
	} else {
		record.FirstReview, err = firstReviewLabel(ctx, client, repo, pr, human[0])
		if err != nil {
			return record, nil, err
		}
	}

	comments, err := client.ListIssueComments(ctx, repo, pr.Number)
	if err != nil {
		return record, nil, fmt.Errorf("reading the comments of #%d: %w", pr.Number, err)
	}
	record.RiskRecord = hasRiskRecord(comments, q.Self)
	return record, files, nil
}

// hasRiskRecord reports whether one of the comments is this tool's risk record.
//
// Authorship, not just the marker, for the reason metrics gives: a human who
// quotes the record in a comment of their own has not made one.
func hasRiskRecord(comments []ghclient.IssueComment, self string) bool {
	for _, c := range comments {
		if self != "" && c.User.Login != self {
			continue
		}
		if strings.Contains(c.Body, riskMarker) {
			return true
		}
	}
	return false
}

// humanReviews are the submitted reviews by somebody other than the author and
// other than a bot, earliest first.
//
// The author is left out because replying to a review thread submits a review
// of its own, which is not anybody reviewing. Bots are left out because the
// question is what a human caught — this tool's own comments arrive as reviews
// too.
func humanReviews(reviews []ghclient.Review, author string) []ghclient.Review {
	var out []ghclient.Review
	for _, r := range reviews {
		if r.SubmittedAt.IsZero() || r.State == "PENDING" {
			continue
		}
		if r.User.Type == "Bot" || (author != "" && r.User.Login == author) {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SubmittedAt.Before(out[j].SubmittedAt) })
	return out
}

// readyAt is when the pull request started waiting for the review that
// approved it: the last time it was marked ready for review before that
// approval, or its creation if it was never a draft.
//
// The last rather than the first: a pull request sent back to draft was not
// waiting for anybody, and counting that time charges reviewers for it.
func readyAt(created time.Time, events []ghclient.TimelineEvent, approved time.Time) time.Time {
	ready := created
	for _, e := range events {
		if e.Event != "ready_for_review" || e.CreatedAt.IsZero() || e.CreatedAt.After(approved) {
			continue
		}
		if e.CreatedAt.After(ready) {
			ready = e.CreatedAt
		}
	}
	return ready
}

// firstReviewLabel says whether the first human review changed the pull request.
//
// The question is about the revision a human first looked at, not the one that
// merged: a problem the reviewer caught and the author fixed is the evidence
// that matters when a merge class is tested against history.
func firstReviewLabel(ctx context.Context, client outcomeReader, repo ghclient.Repo, pr ghclient.PullRequest,
	first ghclient.Review) (string, error) {

	if first.CommitID == pr.Head.SHA {
		return reviewUnchanged, nil
	}

	comments, err := client.ListReviewComments(ctx, repo, pr.Number)
	if err != nil {
		return "", fmt.Errorf("reading the review comments of #%d: %w", pr.Number, err)
	}
	commented := map[string]bool{}
	for _, c := range comments {
		if c.PullRequestReviewID == first.ID && c.Path != "" {
			commented[c.Path] = true
		}
	}
	// A review that approved or commented without pointing at any line asked
	// for nothing, so nothing that followed was a response to it.
	if first.State != "CHANGES_REQUESTED" && len(commented) == 0 {
		return reviewUnchanged, nil
	}

	changed, err := client.CompareFiles(ctx, repo, first.CommitID, pr.Head.SHA)
	if errors.Is(err, ghclient.ErrNotFound) {
		return reviewUnknown, nil
	}
	if err != nil {
		return "", fmt.Errorf("comparing #%d's first-reviewed commit with its merged head: %w", pr.Number, err)
	}
	for _, f := range changed {
		// Changes requested with no line comments named no file, so any
		// later change answers it.
		if len(commented) == 0 || commented[f.Filename] || (f.PreviousFilename != "" && commented[f.PreviousFilename]) {
			return reviewChanged, nil
		}
	}
	if len(changed) >= ghclient.MaxCompareFiles {
		return reviewUnknown, nil
	}
	return reviewUnchanged, nil
}

// revertBody matches GitHub's revert body, "Reverts owner/repo#123", and the
// short form a person writes by hand, "Reverts #123".
var revertBody = regexp.MustCompile(`\bReverts (?:([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+))?#(\d+)\b`)

// revertTitle matches a revert written by hand that names its target at the
// start of the title: "Revert #34; record ADR 0007", "Revert owner/repo#34".
var revertTitle = regexp.MustCompile(`(?i)^revert(?:s|ed)?:?\s+(?:([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+))?#(\d+)\b`)

// findReverts maps each reverted pull request to the earliest later one that
// reverted it.
//
// Two forms count. GitHub's own needs both halves of its convention: the title
// alone matches a revert of a different pull request with the same title, and
// the body alone matches a description that merely mentions one. A title that
// starts "Revert #N" names its target outright and counts by itself; a pilot
// run found a hand-written revert in that form labelled hotfixed instead.
func findReverts(merged []ghclient.PullRequest, repo ghclient.Repo) map[int]int {
	mergedAt := map[int]time.Time{}
	for _, pr := range merged {
		mergedAt[pr.Number] = pr.MergedAt
	}
	reverts := map[int]int{}
	for _, pr := range merged {
		var found [][]string
		switch {
		case strings.HasPrefix(pr.Title, `Revert "`):
			found = revertBody.FindAllStringSubmatch(pr.Body, -1)
		case revertTitle.MatchString(pr.Title):
			found = revertTitle.FindAllStringSubmatch(pr.Title, 1)
		default:
			continue
		}
		for _, m := range found {
			if m[1] != "" && !strings.EqualFold(m[1], repo.String()) {
				continue
			}
			target, err := strconv.Atoi(m[2])
			if err != nil || target == pr.Number {
				continue
			}
			at, ok := mergedAt[target]
			if !ok || !pr.MergedAt.After(at) {
				continue
			}
			// merged is in merge order, so the first revert seen is the
			// earliest.
			if reverts[target] == 0 {
				reverts[target] = pr.Number
			}
		}
	}
	return reverts
}

// laterOutcome labels a pull request P by what the pull requests merged in the
// fourteen days after it did to its lines: hotfixed, modified-later,
// not-checked or clean, with the later pull request that decided it and, when
// some blame could not be read, why.
//
// A later pull request L counts against P when a line L removed or changed was
// last written by a commit of P's. Blame, run at the merge base L's patch is
// numbered against, names the commit that last wrote each removed line, and
// GitHub names the pull request that introduced that commit: P's squash
// commit, one of its rebased commits, or a commit of the branch a merge commit
// brought in. That replaces comparing the line numbers each patch reports,
// which a pull request merged in between could shift; blame follows the lines
// to where they are when L starts from them.
//
// What it cannot see, it does not claim. A line of P's that somebody changed
// again before L is credited to that change, not to P. A later pull request
// whose merge base predates P's merge never saw P's lines, so changed none of
// them, and is not counted. A merge base GitHub cannot work out, a file blame
// cannot read there, and a commit past the lookup bound leave P not-checked
// rather than clean.
//
// A hotfix is preferred to a modification wherever both occur, because a fix
// is the stronger claim about P.
func (a *attribution) laterOutcome(ctx context.Context, p ghclient.PullRequest, later []ghclient.PullRequest,
	files map[int][]ghclient.ChangedFile) (string, int, string) {

	// Only files P left behind have lines of P's in them.
	touched := map[string]bool{}
	for _, f := range files[p.Number] {
		if f.Status != "removed" {
			touched[f.Filename] = true
		}
	}

	modifiedBy, unchecked := 0, ""
	for _, l := range later {
		if !l.MergedAt.After(p.MergedAt) {
			continue
		}
		if l.MergedAt.Sub(p.MergedAt) > laterWindow {
			// later is in merge order, so every one after this is later too.
			break
		}
		changed, why := a.changesLinesOf(ctx, p, l, files[l.Number], touched)
		if why != "" && unchecked == "" {
			unchecked = why
		}
		if !changed {
			continue
		}
		if fixes(l, p.Number, a.repo) {
			return outcomeHotfixed, l.Number, ""
		}
		if modifiedBy == 0 {
			modifiedBy = l.Number
		}
	}
	switch {
	case modifiedBy != 0:
		return outcomeModified, modifiedBy, unchecked
	case unchecked != "":
		return outcomeUnchecked, 0, unchecked
	}
	return outcomeClean, 0, ""
}

// changesLinesOf reports whether a later pull request L removed or changed a
// line a commit of P's last wrote, and, when it could not tell for some file,
// why.
//
// One blame per file both touched, and only for a file L removed lines from: a
// pure addition changes nobody's lines.
func (a *attribution) changesLinesOf(ctx context.Context, p, l ghclient.PullRequest,
	lFiles []ghclient.ChangedFile, touched map[string]bool) (bool, string) {

	why := ""
	note := func(reason string) {
		if why == "" {
			why = reason
		}
	}
	for _, f := range lFiles {
		// The file as it was before L, which is where P's lines were:
		// renamed or not.
		name := f.Filename
		if f.PreviousFilename != "" {
			name = f.PreviousFilename
		}
		if !touched[name] || f.Status == "added" {
			continue
		}
		removed := diff.BaseLines(f.Patch)
		if len(removed) == 0 {
			// GitHub leaves the patch out of a diff too large to send, and
			// then only the count says lines went.
			if f.Patch == "" && f.Deletions > 0 {
				note(fmt.Sprintf("GitHub sent no patch for %s in #%d", name, l.Number))
			}
			continue
		}
		base, err := a.mergeBase(ctx, l)
		if err != nil {
			note(fmt.Sprintf("finding #%d's merge base: %v", l.Number, err))
			continue
		}
		ranges, err := a.blame(ctx, base, name)
		if err != nil {
			note(fmt.Sprintf("blaming %s at #%d's merge base: %v", name, l.Number, err))
			continue
		}
		credited, err := a.credits(ctx, removed, ranges, p, l)
		if credited {
			return true, ""
		}
		if err != nil {
			note(err.Error())
		}
	}
	return false, why
}

// commitLookupCap bounds how many commits one run asks GitHub to name the pull
// requests of. Each is a request, and a later pull request that rewrites old
// code can name dozens of commits nobody in the window wrote, so without a
// bound one large cleanup could spend the rate limit. A variable so the tests
// can lower it.
var commitLookupCap = 200

// attribution works out whose lines a later pull request removed, and keeps
// what it asked GitHub so that nothing is asked twice: several earlier pull
// requests can share a later one, its merge base, a file, and the commits
// blame names in it.
type attribution struct {
	client outcomeReader
	repo   ghclient.Repo
	// mergeCommits maps the merge commit of every pull request merged in the
	// window, lower-cased, to its number. A blamed commit found here needs no
	// lookup: a merge's own commit belongs to the pull request it merged.
	mergeCommits map[string]int
	mergeBases   map[int]shaResult
	blames       map[string]blameResult
	commitPulls  map[string]pullsResult
	lookups      int
}

type shaResult struct {
	sha string
	err error
}

type blameResult struct {
	ranges []ghclient.BlameRange
	err    error
}

type pullsResult struct {
	numbers []int
	err     error
}

func newAttribution(client outcomeReader, repo ghclient.Repo, merged []ghclient.PullRequest) *attribution {
	a := &attribution{
		client: client, repo: repo,
		mergeCommits: map[string]int{},
		mergeBases:   map[int]shaResult{},
		blames:       map[string]blameResult{},
		commitPulls:  map[string]pullsResult{},
	}
	for _, pr := range merged {
		if pr.MergeCommitSHA != "" {
			a.mergeCommits[strings.ToLower(pr.MergeCommitSHA)] = pr.Number
		}
	}
	return a
}

// mergeBase is the commit L's patch is numbered against: the merge base of
// its base and head, not its base, which is where the base branch stood when
// GitHub last updated L and may hold lines L's patch never saw.
func (a *attribution) mergeBase(ctx context.Context, l ghclient.PullRequest) (string, error) {
	if r, ok := a.mergeBases[l.Number]; ok {
		return r.sha, r.err
	}
	var r shaResult
	if l.Base.SHA == "" || l.Head.SHA == "" {
		r.err = errors.New("GitHub gave no base or head commit")
	} else {
		r.sha, r.err = a.client.Compare(ctx, a.repo, l.Base.SHA, l.Head.SHA)
	}
	a.mergeBases[l.Number] = r
	return r.sha, r.err
}

func (a *attribution) blame(ctx context.Context, sha, path string) ([]ghclient.BlameRange, error) {
	key := sha + "\x00" + path
	if r, ok := a.blames[key]; ok {
		return r.ranges, r.err
	}
	ranges, err := a.client.Blame(ctx, a.repo, sha, path)
	a.blames[key] = blameResult{ranges: ranges, err: err}
	return ranges, err
}

// credits reports whether blame credits any of the removed lines to a commit
// of P's that is not also L's, and, when a commit could not be looked up, why.
//
// A commit both pull requests carry, as when L was branched from P's branch,
// is not P's line being changed by somebody else, so it does not count.
func (a *attribution) credits(ctx context.Context, lines []int, ranges []ghclient.BlameRange,
	p, l ghclient.PullRequest) (bool, error) {

	var commits []string
	seen := map[string]bool{}
	for _, line := range lines {
		commit := strings.ToLower(blamedAt(ranges, line))
		if commit == "" || seen[commit] {
			continue
		}
		seen[commit] = true
		// P's own merge commit answers without a lookup, so it is looked
		// for before anything spends one.
		if strings.EqualFold(commit, p.MergeCommitSHA) {
			return true, nil
		}
		commits = append(commits, commit)
	}

	var failed error
	for _, commit := range commits {
		if _, ok := a.mergeCommits[commit]; ok {
			// Another pull request's merge, and not P's: that was checked.
			continue
		}
		pulls, err := a.pullsOf(ctx, commit)
		if err != nil {
			if failed == nil {
				failed = err
			}
			continue
		}
		if slices.Contains(pulls, p.Number) && !slices.Contains(pulls, l.Number) {
			return true, nil
		}
	}
	return false, failed
}

// pullsOf names the pull requests GitHub associates with a commit, within the
// run's lookup bound.
func (a *attribution) pullsOf(ctx context.Context, commit string) ([]int, error) {
	if r, ok := a.commitPulls[commit]; ok {
		return r.numbers, r.err
	}
	if a.lookups >= commitLookupCap {
		return nil, fmt.Errorf("reached the bound of %d commit lookups in one run before commit %s",
			commitLookupCap, shortSHA(commit))
	}
	a.lookups++
	numbers, err := a.client.PullRequestsForCommit(ctx, a.repo, commit)
	if err != nil {
		err = fmt.Errorf("finding the pull requests of commit %s: %w", shortSHA(commit), err)
	}
	a.commitPulls[commit] = pullsResult{numbers: numbers, err: err}
	return numbers, err
}

// blamedAt is the commit blame credits a line to, or "" when no range holds it.
func blamedAt(ranges []ghclient.BlameRange, line int) string {
	for _, r := range ranges {
		if line >= r.StartLine && line <= r.EndLine {
			return r.Commit
		}
	}
	return ""
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// fixWord is a word in a later pull request's title or branch that says it
// fixes something. Whole words only, bounded by anything that is not a letter,
// so "fix(model):", "fix:", "hotfix/login" and "bugfix-y" count, with a
// branch's "/", "-" and "_" as separators, and "fixture", "prefix" and
// "debugging" do not.
var fixWord = regexp.MustCompile(
	`(?i)(^|[^a-z])(fix|fixes|fixed|hotfix|bugfix|bug|regression|regress|revert|reverts)([^a-z]|$)`)

// fixes reports whether a later pull request says it is a fix: in its title or
// branch, or in its body by a phrase that links it to the pull request whose
// lines it changed. A judgement about intent from a few words, which is why
// the hotfixed label is audited by hand.
//
// The body is held to the linking phrase because a description is long. Run
// over one, a loose word match found "fixture" and "fixing what review found"
// in nearly every milestone's description and called it a fix of the one
// before, and a bare "#12" there is more often "the plan in #12" than a claim
// that #12 was wrong.
func fixes(l ghclient.PullRequest, target int, repo ghclient.Repo) bool {
	if fixWord.MatchString(l.Title) || fixWord.MatchString(l.Head.Ref) {
		return true
	}
	// \b after the number: "#12" names twelve, and "#123" does not.
	linked := regexp.MustCompile(`(?i)\b(fix(es|ed)?|resolve[sd]?|close[sd]?|revert(s|ed)?|` +
		`regression (from|in|introduced (by|in)))\s+(` + regexp.QuoteMeta(repo.String()) + `)?#` +
		strconv.Itoa(target) + `\b`)
	return linked.MatchString(l.Body)
}

// allowEntry is one --allowlist directory, as the path segments it must start
// with. Each segment may be a path.Match pattern.
type allowEntry struct {
	raw      string
	segments []string
}

// parseAllowlist reads the --allowlist directories.
//
// Directories only, never a file extension: the design's allowlist rule is that
// "*.md" also matches markdown that code reads, agent instruction files and
// workflow documentation, and a directory is something a person chose to put
// prose in. The first segment must be a literal name for the same reason — a
// leading wildcard is an extension rule by another route.
func parseAllowlist(entries []string) ([]allowEntry, error) {
	var out []allowEntry
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if strings.Contains(entry, "*.") {
			return nil, fmt.Errorf("--allowlist takes directories, not file extensions: %q", raw)
		}
		dir := strings.TrimSuffix(entry, "/**")
		markedDir := dir != entry || strings.HasSuffix(dir, "/")
		dir = strings.TrimSuffix(dir, "/")
		if dir == "" || strings.HasPrefix(dir, "/") {
			return nil, fmt.Errorf("--allowlist takes a directory relative to the repository root, not %q", raw)
		}
		segments := strings.Split(dir, "/")
		last := segments[len(segments)-1]
		if !markedDir && strings.LastIndex(last, ".") > 0 {
			// "README.md" names a file. A directory with a dot in its name
			// is written with a trailing slash, which says which was meant.
			return nil, fmt.Errorf("--allowlist takes directories, not files: %q (write a directory with a dot in its name as %q)", raw, dir+"/")
		}
		for i, seg := range segments {
			if seg == "" || seg == "." || seg == ".." || strings.Contains(seg, "**") {
				return nil, fmt.Errorf("--allowlist %q: only a trailing /** is supported, and no empty, . or .. segments", raw)
			}
			if _, err := path.Match(seg, ""); err != nil {
				return nil, fmt.Errorf("--allowlist %q: %v", raw, err)
			}
			if i == 0 && strings.ContainsAny(seg, `*?[\`) {
				return nil, fmt.Errorf("--allowlist %q: the first directory must be named, not a pattern", raw)
			}
		}
		out = append(out, allowEntry{raw: entry, segments: segments})
	}
	return out, nil
}

// contains reports whether a file sits somewhere under the directory.
func (a allowEntry) contains(file string) bool {
	parts := strings.Split(file, "/")
	// Strictly under: the directory's own name as a file is not inside it.
	if len(parts) <= len(a.segments) {
		return false
	}
	for i, seg := range a.segments {
		if ok, _ := path.Match(seg, parts[i]); !ok {
			return false
		}
	}
	return true
}

func allowed(file string, allow []allowEntry) bool {
	for _, a := range allow {
		if a.contains(file) {
			return true
		}
	}
	return false
}

// docsOnly reports whether every file was added or modified under the allowlist.
//
// Deny by default. A removal is not docs-only even inside the allowlist: what
// referred to the file is outside the diff. A rename is not either, and its old
// name is checked as well as its new one, because moving a workflow into docs/
// deletes a workflow.
func docsOnly(files []ghclient.ChangedFile, allow []allowEntry) bool {
	if len(allow) == 0 || len(files) == 0 || len(files) >= maxListedFiles {
		return false
	}
	for _, f := range files {
		if f.Status != "added" && f.Status != "modified" {
			return false
		}
		if !allowed(f.Filename, allow) {
			return false
		}
		if f.PreviousFilename != "" && !allowed(f.PreviousFilename, allow) {
			return false
		}
	}
	return true
}

// docsOnlyNative reports whether every changed path was under the allowlist,
// whatever happened to it: what GitHub's own settings can see, which is a path
// and not what was done to it (Part 2 of the design). A removal and a rename
// within the allowlist count; a rename out of anywhere else still does not,
// because its old path is a changed path too.
func docsOnlyNative(files []ghclient.ChangedFile, allow []allowEntry) bool {
	if len(allow) == 0 || len(files) == 0 || len(files) >= maxListedFiles {
		return false
	}
	for _, f := range files {
		if !allowed(f.Filename, allow) {
			return false
		}
		if f.PreviousFilename != "" && !allowed(f.PreviousFilename, allow) {
			return false
		}
	}
	return true
}

// attributionCaveat is said of modified-later and hotfixed wherever they are
// counted, and intentCaveat of hotfixed alone.
const (
	attributionCaveat = "find a later change by blame at the later pull request's merge base, crediting " +
		"each line it removed to the pull request GitHub says introduced the commit that last wrote it, " +
		"however that pull request was merged. A line changed again in between is credited to the change " +
		"in between; a later pull request that branched before this one merged never saw its lines and is " +
		"not counted; and a file blame cannot read at the merge base leaves the pull request not-checked"
	intentCaveat = "also judges intent from the later pull request's title, body and branch, so audit it " +
		"by hand before relying on it"
)

// writeGap says what a partial read left out, when it did.
func writeGap(b *strings.Builder, r outcomeReport) {
	if r.Gap == nil {
		return
	}
	fmt.Fprintf(b, "**This summary is incomplete**: %d of %d merged pull requests were read (%v).",
		len(r.PRs), r.Merged, r.Gap)
	if len(r.Unread) > 0 {
		fmt.Fprintf(b, " Not read: %s.", numberList(r.Unread))
	}
	if r.HotfixUnchecked > 0 {
		fmt.Fprintf(b, " %s could not be checked for a hotfix among them.",
			plural(r.HotfixUnchecked, "read pull request", "read pull requests"))
	}
	fmt.Fprintf(b, "\n\n")
}

func writeOutcomesMarkdown(w io.Writer, r outcomeReport, repo ghclient.Repo, q outcomeQuery) error {
	b := &strings.Builder{}
	fmt.Fprintf(b, "# Outcomes of merged pull requests\n\n")
	fmt.Fprintf(b, "%s, %s merged since %s.\n\n",
		repo.String(), plural(r.Merged, "pull request", "pull requests"), q.Since.Format(time.DateOnly))
	writeGap(b, r)

	if len(r.PRs) == 0 {
		if r.Gap == nil {
			fmt.Fprintf(b, "No pull requests merged in the window.\n")
		}
		_, err := io.WriteString(w, b.String())
		return err
	}

	var hours []float64
	counts := map[string]int{}
	strict, native := 0, 0
	for _, p := range r.PRs {
		if p.approved() {
			hours = append(hours, p.hoursToApproval())
		}
		if p.DocsOnly {
			strict++
		}
		if p.DocsNative {
			native++
		}
		counts[p.Outcome]++
		counts[p.FirstReview]++
	}

	fmt.Fprintf(b, "| measure | value |\n")
	fmt.Fprintf(b, "|---|--:|\n")
	fmt.Fprintf(b, "| hours to first approval, median | %s |\n", hoursOf(percentile(hours, 0.5)))
	fmt.Fprintf(b, "| hours to first approval, 90th percentile | %s |\n", hoursOf(percentile(hours, 0.9)))
	fmt.Fprintf(b, "| approved by a human | %d of %d |\n", len(hours), len(r.PRs))
	if len(q.Allow) == 0 {
		fmt.Fprintf(b, "| docs-only candidates, strict | n/a: no --allowlist |\n")
		fmt.Fprintf(b, "| docs-only candidates, native | n/a: no --allowlist |\n")
	} else {
		fmt.Fprintf(b, "| docs-only candidates, strict | %d |\n", strict)
		fmt.Fprintf(b, "| docs-only candidates, native | %d |\n", native)
	}
	for _, label := range []string{outcomeReverted, outcomeHotfixed, outcomeModified} {
		fmt.Fprintf(b, "| %s | %d |\n", label, counts[label])
	}
	if counts[outcomeUnchecked] > 0 {
		fmt.Fprintf(b, "| %s | %d |\n", outcomeUnchecked, counts[outcomeUnchecked])
	}
	for _, label := range []string{outcomeClean, reviewChanged, reviewUnchanged, reviewNone} {
		fmt.Fprintf(b, "| %s | %d |\n", label, counts[label])
	}
	if counts[reviewUnknown] > 0 {
		fmt.Fprintf(b, "| first review %s | %d |\n", reviewUnknown, counts[reviewUnknown])
	}

	fmt.Fprintf(b, "\nHours to first approval run from the last time a pull request was marked ready "+
		"for review, or from its creation if it was never a draft, to the first approval by a human "+
		"other than its author.")
	if len(q.Allow) > 0 {
		names := make([]string, 0, len(q.Allow))
		for _, a := range q.Allow {
			names = append(names, "`"+a.raw+"`")
		}
		fmt.Fprintf(b, " Docs-only strict means every file was added or modified under %s, and a "+
			"removal or a rename never counts; native means every changed path was under it, removals "+
			"and renames included, which is all GitHub's own settings can tell.", strings.Join(names, ", "))
	}
	fmt.Fprintf(b, " The first-review labels describe the first human review, and whether later "+
		"commits changed the files it commented on.")
	if counts[reviewUnknown] > 0 {
		fmt.Fprintf(b, " A first review is unknown when GitHub could no longer compare its commit "+
			"with the merged head, or the comparison was too large to list.")
	}

	fmt.Fprintf(b, "\n\n`%s` means a pull request merged within 14 days removed or changed a line of "+
		"this one; `%s` means that pull request also says it is a fix, with a word such as fix, bug or revert "+
		"in its title or branch, or a phrase such as \"fixes #N\" naming this one in its description. "+
		"`%s` and `%s` %s. "+
		"`%s` %s.", outcomeModified, outcomeHotfixed, outcomeModified, outcomeHotfixed, attributionCaveat,
		outcomeHotfixed, intentCaveat)
	if r.WindowOpen > 0 {
		fmt.Fprintf(b, " %s merged less than 14 days ago, so a later change may still come.",
			plural(r.WindowOpen, "pull request", "pull requests"))
	}
	fmt.Fprintf(b, "\n")
	writeUnchecked(b, r)
	writeRiskRecords(b, r, q)
	writeRiskLabels(b, r)
	_, err := io.WriteString(w, b.String())
	return err
}

// writeUnchecked names the pull requests whose blame could not all be read, and
// why, so a clean count is not read as a checked one.
func writeUnchecked(b *strings.Builder, r outcomeReport) {
	var listed []string
	n := 0
	for _, p := range r.PRs {
		if p.Unchecked == "" {
			continue
		}
		n++
		if len(listed) < 5 {
			listed = append(listed, fmt.Sprintf("#%d (%s)", p.Number, p.Unchecked))
		}
	}
	if n == 0 {
		return
	}
	fmt.Fprintf(b, "\nBlame could not be read for every later change to %s, so a modification or "+
		"hotfix may be missing: %s", plural(n, "pull request", "pull requests"), strings.Join(listed, "; "))
	if n > len(listed) {
		fmt.Fprintf(b, "; and %d more", n-len(listed))
	}
	fmt.Fprintf(b, ".\n")
}

// writeRiskRecords lists the merged pull requests with no risk record: the
// check on G1, that every pull request gets one.
func writeRiskRecords(b *strings.Builder, r outcomeReport, q outcomeQuery) {
	var missing, forks []int
	for _, p := range r.PRs {
		switch {
		case p.RiskRecord:
		case p.Fork:
			forks = append(forks, p.Number)
		default:
			missing = append(missing, p.Number)
		}
	}
	author := "by any account, because this tool's login is not known (set `github.selfLogin`)"
	if q.Self != "" {
		author = "by `" + q.Self + "`"
	}
	fmt.Fprintf(b, "\n## Risk records\n\n")
	if len(missing) == 0 {
		fmt.Fprintf(b, "Every merged pull request read has a `%s` comment %s.", riskMarker, author)
	} else {
		fmt.Fprintf(b, "%d of %d merged pull requests read have no `%s` comment %s: %s.",
			len(missing), len(r.PRs), riskMarker, author, numberList(missing))
	}
	if len(forks) > 0 {
		fmt.Fprintf(b, " Not counted: %s from a fork, whose token cannot post one.", numberList(forks))
	}
	fmt.Fprintf(b, "\n")
}

// riskLabelRow is one risk/ label's line in the per-label table.
type riskLabelRow struct {
	Label string
	Count int
	// With and Without are the median hours to first approval of the approved
	// pull requests that carried the label, and of those that did not.
	With    float64
	Without float64
}

// riskLabelRows tallies each risk/ label on the merged pull requests read: how
// often it fired, and whether the pull requests it fired on waited longer for
// an approval than the rest (G6b).
func riskLabelRows(prs []prOutcome) []riskLabelRow {
	fired := map[string]int{}
	for _, p := range prs {
		for _, l := range p.RiskLabels {
			fired[l]++
		}
	}
	labels := make([]string, 0, len(fired))
	for l := range fired {
		labels = append(labels, l)
	}
	sort.Strings(labels)

	var rows []riskLabelRow
	for _, label := range labels {
		var with, without []float64
		for _, p := range prs {
			if !p.approved() {
				continue
			}
			if hasLabel(p, label) {
				with = append(with, p.hoursToApproval())
			} else {
				without = append(without, p.hoursToApproval())
			}
		}
		rows = append(rows, riskLabelRow{Label: label, Count: fired[label],
			With: percentile(with, 0.5), Without: percentile(without, 0.5)})
	}
	return rows
}

func hasLabel(p prOutcome, label string) bool {
	for _, l := range p.RiskLabels {
		if l == label {
			return true
		}
	}
	return false
}

func writeRiskLabels(b *strings.Builder, r outcomeReport) {
	fmt.Fprintf(b, "\n## Risk labels\n\n")
	rows := riskLabelRows(r.PRs)
	if len(rows) == 0 {
		fmt.Fprintf(b, "No merged pull request read carried a `%s` label.\n", riskLabelPrefix)
		return
	}
	fmt.Fprintf(b, "| label | pull requests | median hours to first approval, with | without |\n")
	fmt.Fprintf(b, "|---|--:|--:|--:|\n")
	for _, row := range rows {
		fmt.Fprintf(b, "| %s | %d of %d | %s | %s |\n", row.Label, row.Count, len(r.PRs),
			hoursOf(row.With), hoursOf(row.Without))
	}
	fmt.Fprintf(b, "\nWith and without compare the approved pull requests that carried the label "+
		"with the approved ones that did not.\n")
}

// writeUnreviewedMarkdown lists the merges no human reviewed, which are the
// ones a person samples by hand once anything merges without review (G5b).
func writeUnreviewedMarkdown(w io.Writer, r outcomeReport, repo ghclient.Repo, q outcomeQuery) error {
	var unreviewed []prOutcome
	for _, p := range r.PRs {
		if p.FirstReview == reviewNone {
			unreviewed = append(unreviewed, p)
		}
	}
	b := &strings.Builder{}
	fmt.Fprintf(b, "# Merged pull requests with no human review\n\n")
	fmt.Fprintf(b, "%s, %d of %s merged since %s had no human review.\n\n", repo.String(),
		len(unreviewed), plural(r.Merged, "pull request", "pull requests"), q.Since.Format(time.DateOnly))
	writeGap(b, r)

	if len(unreviewed) > 0 {
		fmt.Fprintf(b, "| pull request | merged | docs-only, strict | docs-only, native | outcome |\n")
		fmt.Fprintf(b, "|---|---|---|---|---|\n")
		for _, p := range unreviewed {
			strict, native := yesNo(p.DocsOnly), yesNo(p.DocsNative)
			if len(q.Allow) == 0 {
				strict, native = "n/a", "n/a"
			}
			outcome := p.Outcome
			if p.By != 0 {
				outcome += fmt.Sprintf(" by #%d", p.By)
			}
			fmt.Fprintf(b, "| #%d | %s | %s | %s | %s |\n", p.Number, p.MergedAt.UTC().Format(time.DateOnly),
				strict, native, outcome)
		}
		fmt.Fprintf(b, "\nNo human other than the author approved, requested changes on or commented on "+
			"these; reviews by bots do not count.\n")
	}
	writeUnchecked(b, outcomeReport{PRs: unreviewed})
	_, err := io.WriteString(w, b.String())
	return err
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func writeOutcomesCSV(w io.Writer, r outcomeReport, q outcomeQuery) error {
	out := csv.NewWriter(w)
	if err := out.Write([]string{"number", "merged_at", "ready_at", "first_approval_at",
		"hours_to_first_approval", "docs_only_strict", "docs_only_native", "outcome", "outcome_by",
		"not_checked", "first_review", "risk_record", "from_fork", "risk_labels"}); err != nil {
		return err
	}
	for _, p := range r.PRs {
		if q.Unreviewed && p.FirstReview != reviewNone {
			continue
		}
		ready, approvedAt, hours := "", "", ""
		if p.approved() {
			ready = p.ReadyAt.UTC().Format(time.RFC3339)
			approvedAt = p.ApprovedAt.UTC().Format(time.RFC3339)
			// Four places where the summary shows one, for the reason
			// metrics gives: a spreadsheet re-aggregates, and a rounded
			// number cannot be.
			hours = strconv.FormatFloat(p.hoursToApproval(), 'f', 4, 64)
		}
		by := ""
		if p.By != 0 {
			by = strconv.Itoa(p.By)
		}
		if err := out.Write([]string{
			strconv.Itoa(p.Number), p.MergedAt.UTC().Format(time.RFC3339), ready, approvedAt, hours,
			strconv.FormatBool(p.DocsOnly), strconv.FormatBool(p.DocsNative), p.Outcome, by,
			p.Unchecked, p.FirstReview, strconv.FormatBool(p.RiskRecord), strconv.FormatBool(p.Fork),
			// One field, so a row stays one pull request. A semicolon
			// rather than a comma, so splitting the field is not confused
			// with splitting the row.
			strings.Join(p.RiskLabels, ";"),
		}); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

// percentile is the nearest-rank percentile, or NaN for no values.
//
// Nearest rank rather than interpolated: with a few dozen pull requests the
// answer should be a time some pull request actually waited.
func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	rank := int(math.Ceil(p*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	return sorted[rank]
}

func hoursOf(h float64) string {
	if math.IsNaN(h) {
		return "n/a"
	}
	return strconv.FormatFloat(h, 'f', 1, 64)
}

// numberList writes pull request numbers as "#4, #5 and #9", shortened past a
// handful so a long gap does not bury the table.
func numberList(numbers []int) string {
	const shown = 5
	var parts []string
	for i, n := range numbers {
		if i == shown {
			parts = append(parts, fmt.Sprintf("%d more", len(numbers)-shown))
			break
		}
		parts = append(parts, "#"+strconv.Itoa(n))
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}
