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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/adamtait/reviewer/internal/config"
	"github.com/adamtait/reviewer/internal/ghclient"
)

// outcomes measures what happened to the pull requests that merged in a window:
// how long they waited for a first approval, which of them touched nothing but
// an allowlist, which were reverted or hotfixed afterwards, and whether the
// first human review changed anything.
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
	return reportOutcomes(ctx, client, repo, since, time.Now(), allow, o.format, stdout, stderr)
}

// outcomeReader is the part of the GitHub API outcomes reads. Narrower than
// *ghclient.REST so the tests can stand in for it, and wider than ghclient.Client
// because the timeline and a comparison's files are reads only this command
// needs.
type outcomeReader interface {
	ListPullRequestsUpdatedSince(ctx context.Context, repo ghclient.Repo, since time.Time) ([]ghclient.PullRequest, error)
	ListFiles(ctx context.Context, repo ghclient.Repo, number int) ([]ghclient.ChangedFile, error)
	ListReviews(ctx context.Context, repo ghclient.Repo, number int) ([]ghclient.Review, error)
	ListReviewComments(ctx context.Context, repo ghclient.Repo, number int) ([]ghclient.ReviewComment, error)
	ListTimelineEvents(ctx context.Context, repo ghclient.Repo, number int) ([]ghclient.TimelineEvent, error)
	CompareFiles(ctx context.Context, repo ghclient.Repo, base, head string) ([]ghclient.ChangedFile, error)
}

// The labels, as they appear in the output.
const (
	outcomeReverted = "reverted"
	outcomeHotfixed = "hotfixed"
	outcomeClean    = "clean"

	reviewChanged   = "changed-after-review"
	reviewUnchanged = "unchanged-after-review"
	reviewNone      = "no-review"
	// reviewUnknown is a first review whose commit GitHub can no longer
	// compare, usually because a force-push left it unreachable, or whose
	// comparison was too large to list in full. Neither label would be true.
	reviewUnknown = "unknown"
)

// hotfixWindow is how long after a merge a change to its lines counts as fixing
// it. Later than this, the lines are just code that somebody changed.
const hotfixWindow = 14 * 24 * time.Hour

// maxListedFiles is the most files GitHub lists for one pull request. A pull
// request with this many may have more, and one of the unlisted ones could be
// anything, so it is never docs-only.
const maxListedFiles = 3000

// prOutcome is one merged pull request's record.
type prOutcome struct {
	Number   int
	MergedAt time.Time
	// ReadyAt and ApprovedAt are zero when no human approved it.
	ReadyAt    time.Time
	ApprovedAt time.Time
	DocsOnly   bool
	Outcome    string
	// By is the pull request that reverted or hotfixed this one.
	By          int
	FirstReview string
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
	// HotfixUnchecked counts read pull requests whose hotfix window reaches
	// into the unread ones, so a hotfix of theirs may be among what was missed.
	HotfixUnchecked int
	// WindowOpen counts pull requests merged less than hotfixWindow ago: a
	// hotfix of theirs may not have happened yet.
	WindowOpen int
}

// reportOutcomes measures and prints. A partial measurement is printed, not
// discarded, with the gap stated, for the reason metrics gives: one pull request
// that could not be read is not a reason to report nothing about the rest.
func reportOutcomes(ctx context.Context, client outcomeReader, repo ghclient.Repo, since, now time.Time,
	allow []allowEntry, format string, stdout, stderr io.Writer) error {

	report := measureOutcomes(ctx, client, repo, since, now, allow)
	if report.Gap != nil {
		fmt.Fprintf(stderr, "reviewer: the summary below is incomplete: %v\n", report.Gap)
	}
	if report.Merged == 0 && report.Gap == nil {
		fmt.Fprintf(stderr, "reviewer: no pull requests merged since %s\n", since.Format(time.DateOnly))
	}
	if format == "csv" {
		// A CSV has nowhere to say it, and the caveat must reach whoever
		// reads the numbers.
		fmt.Fprintf(stderr, "reviewer: %s %s\n", outcomeHotfixed, hotfixCaveat)
		return writeOutcomesCSV(stdout, report)
	}
	return writeOutcomesMarkdown(stdout, report, repo, since, allow)
}

// measureOutcomes reads every pull request merged since the cutoff, oldest
// merge first, and labels each one.
func measureOutcomes(ctx context.Context, client outcomeReader, repo ghclient.Repo, since, now time.Time,
	allow []allowEntry) outcomeReport {

	var report outcomeReport
	listed, err := client.ListPullRequestsUpdatedSince(ctx, repo, since)
	if err != nil {
		report.Gap = fmt.Errorf("listing pull requests: %w", err)
		return report
	}

	// A pull request merged in the window was updated in it too, so the
	// update-ordered list contains every one of them.
	var merged []ghclient.PullRequest
	for _, pr := range listed {
		if !pr.MergedAt.IsZero() && !pr.MergedAt.Before(since) {
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
		record, prFiles, err := readOutcome(ctx, client, repo, pr, allow)
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

	for i := range report.PRs {
		p := &report.PRs[i]
		switch by, hot := hotfixOf(*p, report.PRs[i+1:], files); {
		case reverts[p.Number] != 0:
			p.Outcome, p.By = outcomeReverted, reverts[p.Number]
		case hot:
			p.Outcome, p.By = outcomeHotfixed, by
		default:
			p.Outcome = outcomeClean
		}
		if now.Sub(p.MergedAt) < hotfixWindow {
			report.WindowOpen++
		}
		if len(report.Unread) > 0 && p.Outcome == outcomeClean {
			firstUnread := merged[len(report.PRs)].MergedAt
			if !firstUnread.After(p.MergedAt.Add(hotfixWindow)) {
				report.HotfixUnchecked++
			}
		}
	}
	return report
}

// readOutcome reads one pull request's files and reviews and works out every
// label that does not depend on later pull requests.
func readOutcome(ctx context.Context, client outcomeReader, repo ghclient.Repo, pr ghclient.PullRequest,
	allow []allowEntry) (prOutcome, []ghclient.ChangedFile, error) {

	record := prOutcome{Number: pr.Number, MergedAt: pr.MergedAt}

	files, err := client.ListFiles(ctx, repo, pr.Number)
	if err != nil {
		return record, nil, fmt.Errorf("reading the files of #%d: %w", pr.Number, err)
	}
	record.DocsOnly = docsOnly(files, allow)

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
	return record, files, nil
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

// findReverts maps each reverted pull request to the earliest later one that
// reverted it.
//
// Both halves of GitHub's convention are required. The title alone matches a
// revert of a different pull request with the same title, and the body alone
// matches a description that merely mentions one.
func findReverts(merged []ghclient.PullRequest, repo ghclient.Repo) map[int]int {
	mergedAt := map[int]time.Time{}
	for _, pr := range merged {
		mergedAt[pr.Number] = pr.MergedAt
	}
	reverts := map[int]int{}
	for _, pr := range merged {
		if !strings.HasPrefix(pr.Title, `Revert "`) {
			continue
		}
		for _, m := range revertBody.FindAllStringSubmatch(pr.Body, -1) {
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

// hotfixOf finds the first later pull request, merged within hotfixWindow, that
// changed a line this one added.
//
// Approximate, and the output says so. It compares the line numbers each patch
// reports, so a pull request merged in between that moved the lines can hide a
// hotfix or invent one, and a file whose patch GitHub left out (too large, or
// binary) is not compared at all.
func hotfixOf(p prOutcome, later []prOutcome, files map[int][]ghclient.ChangedFile) (int, bool) {
	added := map[string]map[int]bool{}
	for _, f := range files[p.Number] {
		if lines := addedLines(f.Patch); len(lines) > 0 {
			added[f.Filename] = lines
		}
	}
	if len(added) == 0 {
		return 0, false
	}
	for _, q := range later {
		if !q.MergedAt.After(p.MergedAt) || q.MergedAt.Sub(p.MergedAt) > hotfixWindow {
			continue
		}
		for _, f := range files[q.Number] {
			// The file as it was before the later pull request: renamed or not.
			name := f.Filename
			if f.PreviousFilename != "" {
				name = f.PreviousFilename
			}
			if lines, ok := added[name]; ok && touches(f.Patch, lines) {
				return q.Number, true
			}
		}
	}
	return 0, false
}

// hunkHeader reads "@@ -12,3 +14,5 @@": where a hunk starts on each side.
var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// addedLines is the set of line numbers, in the file after the patch, that the
// patch added.
func addedLines(patch string) map[int]bool {
	added := map[int]bool{}
	walkPatch(patch, func(kind byte, _, newLine int) {
		if kind == '+' {
			added[newLine] = true
		}
	})
	return added
}

// touches reports whether a patch changed any of the given lines of the file it
// starts from: removed or replaced one, or inserted between two of them.
//
// Context lines do not count. A hunk carries three lines of context either side,
// so counting the hunk's whole range would call a change to the line after
// someone's work a fix of it.
func touches(patch string, lines map[int]bool) bool {
	hit := false
	walkPatch(patch, func(kind byte, oldLine, _ int) {
		switch kind {
		case '-':
			hit = hit || lines[oldLine]
		case '+':
			// An insertion lands between the old line before it and the
			// old line after it. Inside a block somebody added, it is a
			// change to that block; at its edge, it is only adjacent.
			hit = hit || (lines[oldLine-1] && lines[oldLine])
		}
	})
	return hit
}

// walkPatch calls visit for every removed and added line of a unified diff,
// with the line's number in the old file and in the new one. For a removed line
// the new number is where it would have been; for an added line the old number
// is the old line that follows it.
func walkPatch(patch string, visit func(kind byte, oldLine, newLine int)) {
	oldLine, newLine := 0, 0
	inHunk := false
	for _, line := range strings.Split(patch, "\n") {
		if m := hunkHeader.FindStringSubmatch(line); m != nil {
			oldLine, _ = strconv.Atoi(m[1])
			newLine, _ = strconv.Atoi(m[2])
			inHunk = true
			continue
		}
		if !inHunk || line == "" {
			continue
		}
		switch line[0] {
		case '+':
			visit('+', oldLine, newLine)
			newLine++
		case '-':
			visit('-', oldLine, newLine)
			oldLine++
		case ' ':
			oldLine++
			newLine++
		}
		// Anything else is "\ No newline at end of file", which is no line.
	}
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

// hotfixCaveat is said of the hotfixed label wherever a hotfix count is shown.
const hotfixCaveat = "is approximate: it compares the line numbers each patch reports, " +
	"so a pull request merged in between that moved the lines can hide a hotfix or invent one"

func writeOutcomesMarkdown(w io.Writer, r outcomeReport, repo ghclient.Repo, since time.Time, allow []allowEntry) error {
	b := &strings.Builder{}
	fmt.Fprintf(b, "# Outcomes of merged pull requests\n\n")
	fmt.Fprintf(b, "%s, %s merged since %s.\n\n",
		repo.String(), plural(r.Merged, "pull request", "pull requests"), since.Format(time.DateOnly))

	if r.Gap != nil {
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

	if len(r.PRs) == 0 {
		if r.Gap == nil {
			fmt.Fprintf(b, "No pull requests merged in the window.\n")
		}
		_, err := io.WriteString(w, b.String())
		return err
	}

	var hours []float64
	counts := map[string]int{}
	docs := 0
	for _, p := range r.PRs {
		if p.approved() {
			hours = append(hours, p.hoursToApproval())
		}
		if p.DocsOnly {
			docs++
		}
		counts[p.Outcome]++
		counts[p.FirstReview]++
	}

	fmt.Fprintf(b, "| measure | value |\n")
	fmt.Fprintf(b, "|---|--:|\n")
	fmt.Fprintf(b, "| hours to first approval, median | %s |\n", hoursOf(percentile(hours, 0.5)))
	fmt.Fprintf(b, "| hours to first approval, 90th percentile | %s |\n", hoursOf(percentile(hours, 0.9)))
	fmt.Fprintf(b, "| approved by a human | %d of %d |\n", len(hours), len(r.PRs))
	if len(allow) == 0 {
		fmt.Fprintf(b, "| docs-only candidates | n/a: no --allowlist |\n")
	} else {
		fmt.Fprintf(b, "| docs-only candidates | %d |\n", docs)
	}
	for _, label := range []string{outcomeReverted, outcomeHotfixed, outcomeClean,
		reviewChanged, reviewUnchanged, reviewNone} {
		fmt.Fprintf(b, "| %s | %d |\n", label, counts[label])
	}
	if counts[reviewUnknown] > 0 {
		fmt.Fprintf(b, "| first review %s | %d |\n", reviewUnknown, counts[reviewUnknown])
	}

	fmt.Fprintf(b, "\nHours to first approval run from the last time a pull request was marked ready "+
		"for review, or from its creation if it was never a draft, to the first approval by a human "+
		"other than its author.")
	if len(allow) > 0 {
		names := make([]string, 0, len(allow))
		for _, a := range allow {
			names = append(names, "`"+a.raw+"`")
		}
		fmt.Fprintf(b, " Docs-only means every file was added or modified under %s; a removal or a "+
			"rename never counts.", strings.Join(names, ", "))
	}
	fmt.Fprintf(b, " The first-review labels describe the first human review, and whether later "+
		"commits changed the files it commented on.")
	if counts[reviewUnknown] > 0 {
		fmt.Fprintf(b, " A first review is unknown when GitHub could no longer compare its commit "+
			"with the merged head, or the comparison was too large to list.")
	}
	fmt.Fprintf(b, "\n\n`%s` %s.", outcomeHotfixed, hotfixCaveat)
	if r.WindowOpen > 0 {
		fmt.Fprintf(b, " %s merged less than 14 days ago, so a hotfix may still come.",
			plural(r.WindowOpen, "pull request", "pull requests"))
	}
	fmt.Fprintf(b, "\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func writeOutcomesCSV(w io.Writer, r outcomeReport) error {
	out := csv.NewWriter(w)
	if err := out.Write([]string{"number", "merged_at", "ready_at", "first_approval_at",
		"hours_to_first_approval", "docs_only", "outcome", "outcome_by", "first_review"}); err != nil {
		return err
	}
	for _, p := range r.PRs {
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
			strconv.FormatBool(p.DocsOnly), p.Outcome, by, p.FirstReview,
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
