// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/adamtait/reviewer/internal/codeowners"
	"github.com/adamtait/reviewer/internal/config"
	"github.com/adamtait/reviewer/internal/diff"
	"github.com/adamtait/reviewer/internal/ghclient"
	"github.com/adamtait/reviewer/internal/installer"
	"github.com/adamtait/reviewer/internal/reporters"
	"github.com/adamtait/reviewer/internal/risk"
	"github.com/adamtait/reviewer/internal/scip"
	"github.com/adamtait/reviewer/internal/scipfetch"
)

// The limits on what one risk run reads, from Part 1 of
// docs/designs/risk-gated-merge.md. Each puts a ceiling on the job's API cost
// or memory.
const (
	// historyFiles is how many changed files have their history read: the
	// most-changed ones, one call each.
	historyFiles = 20
	// historyWindow is how far back a revert counts.
	historyWindow = 180 * 24 * time.Hour
	// indexArtifact is the name review-index.yml saves the index under.
	indexArtifact = "review-index"
	// defaultIndexBytes bounds the index artifact, the file inside it, and so
	// the index decoded from it. The workspace has no compiled-in default (a
	// zero there is an error, by design); this is the value its example
	// configuration ships, examples/workspace.config.yaml, which a test holds
	// this to.
	defaultIndexBytes int64 = 128 << 20
	// occurrenceCost is what the workspace charges one occurrence against its
	// index limit (internal/workspace/navigation), used here the same way to
	// bound how many occurrences a decode keeps.
	occurrenceCost = 96
)

// selfLoginHint is how to make a run able to remove the labels it set before,
// when the token cannot say who it is.
const selfLoginHint = "set github.selfLogin in .review/config.yaml (github-actions[bot] for a workflow's GITHUB_TOKEN)"

// riskGitHub is the part of the GitHub API `reviewer risk` uses. Narrower than
// *ghclient.REST so the tests can stand in for it. The writes are a comment
// and labels, and nothing else.
type riskGitHub interface {
	Viewer(ctx context.Context) (ghclient.User, error)
	GetPullRequest(ctx context.Context, repo ghclient.Repo, number int) (ghclient.PullRequest, error)
	ListFiles(ctx context.Context, repo ghclient.Repo, number int) ([]ghclient.ChangedFile, error)
	Compare(ctx context.Context, repo ghclient.Repo, base, head string) (string, error)
	GetContentsAt(ctx context.Context, repo ghclient.Repo, path, sha string) ([]byte, bool, error)
	ListCommits(ctx context.Context, repo ghclient.Repo, ref, path string, since time.Time) ([]ghclient.Commit, error)
	ListWorkflowRuns(ctx context.Context, repo ghclient.Repo, workflowFile, headSHA, event string) ([]ghclient.WorkflowRun, error)
	ListRunArtifacts(ctx context.Context, repo ghclient.Repo, runID int64, name string) ([]ghclient.Artifact, error)
	DownloadArtifact(ctx context.Context, repo ghclient.Repo, artifactID int64) (io.ReadCloser, error)
	ListIssueComments(ctx context.Context, repo ghclient.Repo, number int) ([]ghclient.IssueComment, error)
	CreateIssueComment(ctx context.Context, repo ghclient.Repo, number int, body string) (ghclient.IssueComment, error)
	UpdateIssueComment(ctx context.Context, repo ghclient.Repo, id int64, body string) (ghclient.IssueComment, error)
	AddLabels(ctx context.Context, repo ghclient.Repo, number int, labels []string) error
	RemoveLabel(ctx context.Context, repo ghclient.Repo, number int, label string) error
}

// runRisk attaches the advisory risk record to a pull request: one comment and
// labels from a closed set, computed from inputs the pull request cannot change
// (ADR-0035).
//
// Named runRisk, not risk, so it does not shadow the risk package.
//
// A mistake on the command line or in the configuration is a usage error,
// found before any call to GitHub, and exits 2 as everywhere else. After the
// first call nothing returns an error: every failure becomes a line in the
// comment and the job log, and the command exits 0 (ADR-0009, G3).
func runRisk(ctx context.Context, o options, stdout, stderr io.Writer, getenv func(string) string) error {
	switch {
	case o.pr == 0:
		return errUsage{errors.New("risk needs --pr N, the pull request to attach the record to")}
	case o.staged:
		return errUsage{errors.New("risk describes a pull request; --staged describes your index")}
	}
	cfg, secrets, err := config.Resolve(o.root, o.config, getenv)
	if err != nil {
		return errUsage{err}
	}
	client, repo, err := githubClient(cfg, secrets)
	if err != nil {
		return err
	}
	riskJob{
		gh: client, repo: repo, number: o.pr, dryRun: o.dryRun,
		selfLogin: cfg.GitHub.SelfLogin, runURL: runURL(getenv),
		now: time.Now(), indexBytes: defaultIndexBytes,
		out: stdout, log: stderr,
	}.run(ctx)
	return nil
}

// runURL is the address of the workflow run, when there is one.
func runURL(getenv func(string) string) string {
	server, repo, id := getenv("GITHUB_SERVER_URL"), getenv("GITHUB_REPOSITORY"), getenv("GITHUB_RUN_ID")
	if server == "" || repo == "" || id == "" {
		return ""
	}
	return strings.TrimRight(server, "/") + "/" + repo + "/actions/runs/" + id
}

// riskJob is one run of `reviewer risk`, after the configuration is known.
type riskJob struct {
	gh         riskGitHub
	repo       ghclient.Repo
	number     int
	dryRun     bool
	selfLogin  string
	runURL     string
	now        time.Time
	indexBytes int64
	out, log   io.Writer
}

// run computes the record and publishes it. It returns nothing: whatever
// fails is said in the comment and the log.
func (j riskJob) run(ctx context.Context) {
	var warnings []string
	warn := func(format string, args ...any) { warnings = append(warnings, fmt.Sprintf(format, args...)) }

	pr, err := j.gh.GetPullRequest(ctx, j.repo, j.number)
	if err != nil {
		// No head, no files, no labels to compare with. The record says why,
		// if the API takes a comment at all, and keeps the earlier list of
		// labels this tool set, so a later run can still remove them.
		warn("could not read the pull request, so nothing was computed and no label was changed: %v", err)
		self, selfWarning := j.identify(ctx)
		comments, err := j.gh.ListIssueComments(ctx, j.repo, j.number)
		if err != nil {
			warn("could not read the pull request's comments: %v", err)
		}
		previous, trusted := findRiskComment(comments, self)
		if selfWarning != "" {
			warnings = append(warnings, selfWarning)
		}
		rec := risk.Record{Warnings: warnings}
		j.publish(ctx, reporters.RiskComment{Record: rec, Owned: trusted}, previous, err == nil, nil, nil)
		return
	}
	if pr.Draft {
		// Drafts get no record: the job skips them, and a draft run by hand is
		// skipped the same way rather than labelling work nobody is asked to
		// review yet. Marking it ready runs the job again.
		fmt.Fprintf(j.out, "reviewer: pull request #%d is a draft; no risk record until it is ready for review\n", j.number)
		return
	}

	// held are the labels this run cannot judge, because the input that would
	// set them could not be read. A label the tool set earlier is never removed
	// on a run that could not have set it: absent is not the same as not
	// applying, and the record only ever adds attention.
	held := map[risk.Label]bool{}
	holdAll := func() {
		for _, l := range risk.All {
			held[l] = true
		}
	}

	var in risk.Input
	files, err := j.gh.ListFiles(ctx, j.repo, j.number)
	switch {
	case err != nil:
		warn("could not list the changed files, so no label was computed and none was removed: %v", err)
		holdAll()
	case pr.ChangedFiles > len(files):
		// GitHub lists at most 3,000. What is listed still adds labels; what
		// is not could hold any of them, so none is removed.
		warn("GitHub listed %d of the %d changed files, so labels from the rest are missing and none was removed", len(files), pr.ChangedFiles)
		holdAll()
	}
	for _, f := range files {
		in.Files = append(in.Files, risk.File{Path: f.Filename, PreviousPath: f.PreviousFilename})
	}

	mergeBase, err := j.gh.Compare(ctx, j.repo, pr.Base.SHA, pr.Head.SHA)
	if err != nil {
		why := fmt.Sprintf("could not find the merge base: %v", err)
		warn("%s; the default risk map was used, and CODEOWNERS, file history and reach were not read", why)
		in.OwnersUnavailable, in.HistoryUnavailable, in.ReachUnavailable = why, why, why
		for _, l := range append([]risk.Label{risk.LabelWide, risk.LabelHistory, risk.LabelReach}, risk.PathLabels...) {
			held[l] = true
		}
	} else {
		var mapProblems []string
		in.Map, mapProblems = j.riskMap(ctx, mergeBase, held)
		warnings = append(warnings, mapProblems...)
		in.Owners, in.OwnersUnavailable = j.owners(ctx, mergeBase, held)
		var historyFailed bool
		in.Reverted, in.HistoryUnavailable, historyFailed = j.history(ctx, mergeBase, files)
		if historyFailed {
			held[risk.LabelHistory] = true
		}
		var judged bool
		in.Reach, in.ReachUnavailable, judged = j.reach(ctx, mergeBase, files)
		if !judged {
			held[risk.LabelReach] = true
		}
	}

	rec := risk.Compute(in)
	rec.Warnings = append(warnings, rec.Warnings...)

	self, selfWarning := j.identify(ctx)
	comments, commentsErr := j.gh.ListIssueComments(ctx, j.repo, j.number)
	if commentsErr != nil {
		rec.Warnings = append(rec.Warnings, fmt.Sprintf(
			"could not read the pull request's comments, so no label from an earlier run was removed: %v", commentsErr))
	}
	previous, trusted := findRiskComment(comments, self)
	if selfWarning != "" {
		rec.Warnings = append(rec.Warnings, selfWarning)
	}

	plan := planLabels(pr.Labels, rec, held, trusted)
	owned, added, removed, labelWarnings := j.applyLabels(ctx, plan)
	rec.Warnings = append(rec.Warnings, labelWarnings...)

	j.publish(ctx, reporters.RiskComment{HeadSHA: pr.Head.SHA, RunURL: j.runURL, Record: rec, Owned: owned},
		previous, commentsErr == nil, added, removed)
}

// riskMap reads .review/risk.yaml at the merge base. Without it, or with one
// that cannot be used, the record uses the defaults alone, as the design says,
// and the path labels are held: the file may have added the pattern that set
// one of them earlier.
func (j riskJob) riskMap(ctx context.Context, mergeBase string, held map[risk.Label]bool) (risk.Map, []string) {
	hold := func() {
		for _, l := range risk.PathLabels {
			held[l] = true
		}
	}
	raw, found, err := j.gh.GetContentsAt(ctx, j.repo, risk.MapPath, mergeBase)
	switch {
	case err != nil:
		hold()
		return risk.Defaults(), []string{fmt.Sprintf("could not read %s at the merge base, so the default risk map was used: %v", risk.MapPath, err)}
	case !found:
		return risk.Defaults(), nil
	}
	m, notes, err := risk.Parse(raw)
	if err != nil {
		hold()
		return risk.Defaults(), []string{fmt.Sprintf("%s at the merge base was not used, so the default risk map was: %v", risk.MapPath, err)}
	}
	var out []string
	for _, n := range notes {
		out = append(out, risk.MapPath+": "+n)
	}
	return m, out
}

// owners reads CODEOWNERS at the merge base, from the first of GitHub's three
// places that has one. A read that fails stops the search: the file may be in
// the place that could not be read, and the next place would be the wrong one.
func (j riskJob) owners(ctx context.Context, mergeBase string, held map[risk.Label]bool) (*codeowners.File, string) {
	for _, loc := range codeowners.Locations {
		raw, found, err := j.gh.GetContentsAt(ctx, j.repo, loc, mergeBase)
		if err != nil {
			held[risk.LabelWide] = true
			return nil, fmt.Sprintf("could not read %s at the merge base: %v", loc, err)
		}
		if !found {
			continue
		}
		file, err := codeowners.Parse(bytes.NewReader(raw))
		if err != nil {
			held[risk.LabelWide] = true
			return nil, fmt.Sprintf("%s at the merge base does not parse: %v", loc, err)
		}
		return &file, ""
	}
	return nil, "no CODEOWNERS file at the merge base (" + strings.Join(codeowners.Locations, ", ") + ")"
}

// history finds which changed files had a revert on the base branch in the
// last 180 days, reading at most historyFiles of them, most-changed first. A
// file the pull request adds has no history to read, and costs no call. The
// first failed read stops the rest: it is most likely a rate limit, which the
// next call would meet too.
//
// The reason is set both for a failure and for the cap; failed is true only
// for a failure. A cap is a choice, not a failure, so it does not hold the
// label, but it is still said, because a label may be missing for it.
func (j riskJob) history(ctx context.Context, mergeBase string, files []ghclient.ChangedFile) (reverted map[string]bool, why string, failed bool) {
	var candidates []ghclient.ChangedFile
	for _, f := range files {
		if f.Status != "added" {
			candidates = append(candidates, f)
		}
	}
	sort.SliceStable(candidates, func(a, b int) bool {
		if candidates[a].Changes != candidates[b].Changes {
			return candidates[a].Changes > candidates[b].Changes
		}
		return candidates[a].Filename < candidates[b].Filename
	})
	if len(candidates) > historyFiles {
		why = fmt.Sprintf("%d of the %d files that existed before were not read; only the %d most-changed are",
			len(candidates)-historyFiles, len(candidates), historyFiles)
		candidates = candidates[:historyFiles]
	}

	reverted = map[string]bool{}
	since := j.now.Add(-historyWindow)
	for _, f := range candidates {
		at := basePath(f)
		commits, err := j.gh.ListCommits(ctx, j.repo, mergeBase, at, since)
		if err != nil {
			return reverted, fmt.Sprintf("could not read the history of %s: %v", at, err), true
		}
		for _, c := range commits {
			if strings.HasPrefix(c.Subject(), `Revert "`) {
				reverted[at] = true
				break
			}
		}
	}
	return reverted, why, false
}

// basePath is where a changed file was at the merge base.
func basePath(f ghclient.ChangedFile) string {
	if f.PreviousFilename != "" {
		return f.PreviousFilename
	}
	return f.Filename
}

// isTypeScript reports whether a path is one the TypeScript index describes.
func isTypeScript(p string) bool {
	switch path.Ext(p) {
	case ".ts", ".tsx", ".mts", ".cts":
		return true
	}
	return false
}

// reach finds, for each TypeScript file the pull request modifies or removes,
// the symbols defined on its changed lines and how many other files on the
// merge base use them. It reads the index review-index.yml built on a push of
// exactly the merge base, and no other: an index of any other commit describes
// other code.
//
// judged is false when reach could not be computed for a TypeScript file the
// pull request changes, so a risk/reach label set earlier must be kept. A pull
// request with no such file is judged: reach cannot apply to it.
func (j riskJob) reach(ctx context.Context, mergeBase string, files []ghclient.ChangedFile) (map[string][]risk.SymbolReach, string, bool) {
	var changed []ghclient.ChangedFile
	for _, f := range files {
		if f.Status != "added" && isTypeScript(basePath(f)) {
			changed = append(changed, f)
		}
	}
	if len(changed) == 0 {
		return nil, "the pull request changes no TypeScript file that existed at the merge base", true
	}

	ix, why := j.index(ctx, mergeBase)
	if ix == nil {
		return nil, why, false
	}
	documents := map[string]bool{}
	for _, doc := range ix.Documents {
		documents[doc.Path] = true
	}

	out := map[string][]risk.SymbolReach{}
	var problems []string
	for _, f := range changed {
		at := basePath(f)
		lines := diff.BaseLines(f.Patch)
		switch {
		case f.Patch == "" && f.Deletions > 0:
			problems = append(problems, fmt.Sprintf("GitHub sent no patch for %s, so its changed lines are not known", f.Filename))
			continue
		case len(lines) == 0:
			continue // only additions: no symbol on the base changed
		case !documents[at]:
			problems = append(problems, fmt.Sprintf("the index has no entry for %s", at))
			continue
		}
		if symbols := risk.Reach(ix, at, lines); len(symbols) > 0 {
			out[at] = symbols
		}
	}
	if len(problems) > 0 {
		return out, strings.Join(problems, "; "), false
	}
	return out, "", true
}

// index fetches and decodes the merge base's index, or says why it cannot.
func (j riskJob) index(ctx context.Context, mergeBase string) (*scip.Index, string) {
	short := shortSHA(mergeBase)
	workflow := path.Base(installer.IndexWorkflowPath)
	runs, err := j.gh.ListWorkflowRuns(ctx, j.repo, workflow, mergeBase, "push")
	if err != nil {
		return nil, fmt.Sprintf("could not list the %s runs: %v", workflow, err)
	}
	state, run := scipfetch.Classify(runs, mergeBase, installer.IndexWorkflowPath)
	switch state {
	case scipfetch.StateNone:
		return nil, fmt.Sprintf("no %s run on a push of the merge base %s", workflow, short)
	case scipfetch.StateRunning:
		return nil, fmt.Sprintf("the index of the merge base %s is still being built", short)
	case scipfetch.StateFailed:
		return nil, fmt.Sprintf("the %s run for the merge base %s did not succeed", workflow, short)
	}

	artifacts, err := j.gh.ListRunArtifacts(ctx, j.repo, run.ID, indexArtifact)
	if err != nil {
		return nil, fmt.Sprintf("could not list the index run's artifacts: %v", err)
	}
	var artifact *ghclient.Artifact
	for i := range artifacts {
		// Asked for by name; what came back is what counts.
		if artifacts[i].Name == indexArtifact && (artifact == nil || artifacts[i].ID > artifact.ID) {
			artifact = &artifacts[i]
		}
	}
	switch {
	case artifact == nil:
		return nil, fmt.Sprintf("the %s run for the merge base %s saved no %s artifact", workflow, short, indexArtifact)
	case artifact.Expired:
		return nil, fmt.Sprintf("the index of the merge base %s has expired on GitHub", short)
	case artifact.SizeInBytes > j.indexBytes:
		return nil, fmt.Sprintf("the index of the merge base %s is larger than %d bytes", short, j.indexBytes)
	}

	body, err := j.gh.DownloadArtifact(ctx, j.repo, artifact.ID)
	if err != nil {
		return nil, fmt.Sprintf("could not download the index: %v", err)
	}
	defer func() { _ = body.Close() }()
	// One byte past the limit, so the size GitHub reported is not trusted.
	archive, err := io.ReadAll(io.LimitReader(body, j.indexBytes+1))
	switch {
	case err != nil:
		return nil, fmt.Sprintf("could not download the index: %v", err)
	case int64(len(archive)) > j.indexBytes:
		return nil, fmt.Sprintf("the index of the merge base %s is larger than %d bytes", short, j.indexBytes)
	}
	raw, problem := scipfetch.Unzip(archive, j.indexBytes)
	if problem != "" {
		return nil, problem
	}
	ix, err := scip.Decode(raw, scip.Limits{Occurrences: int(j.indexBytes / occurrenceCost)})
	switch {
	case errors.Is(err, scip.ErrTooLarge):
		return nil, "the index holds more occurrences than this run reads"
	case err != nil:
		return nil, fmt.Sprintf("the index could not be read: %v", err)
	}
	return ix, ""
}

// identify returns the login this token posts as, and a warning when it cannot
// be told. GitHub's /user answers for a personal token and refuses the
// installation token a workflow runs with; github.selfLogin covers that case.
func (j riskJob) identify(ctx context.Context) (string, string) {
	viewer, err := j.gh.Viewer(ctx)
	if err == nil && viewer.Login != "" {
		return viewer.Login, ""
	}
	if j.selfLogin != "" {
		return j.selfLogin, ""
	}
	return "", fmt.Sprintf("could not tell which account this token posts as (%v), so no label from an earlier run was removed; %s", err, selfLoginHint)
}

// findRiskComment finds the risk comment to update, and the labels a previous
// run says it set.
//
// The list is trusted only in a comment the token's own login wrote. Anybody
// can write the marker and a list in a comment of their own; trusting it would
// let them have this tool remove a label a person set. When the login is not
// known, no list is trusted, and the comment updated is the first marked one a
// bot wrote, so that a person's comment quoting the marker is never edited.
func findRiskComment(comments []ghclient.IssueComment, self string) (*ghclient.IssueComment, []risk.Label) {
	for i := range comments {
		c := comments[i]
		if !strings.Contains(c.Body, reporters.RiskMarker) {
			continue
		}
		if self == "" {
			if c.User.Type == "Bot" {
				return &comments[i], nil
			}
			continue
		}
		if c.User.Login != self {
			continue
		}
		labels, _ := reporters.ParseRiskLabels(c.Body)
		return &comments[i], labels
	}
	return nil, nil
}

// labelPlan is what label sync will do.
type labelPlan struct {
	add    []risk.Label
	remove []risk.Label
	// keep are labels this tool set before and still keeps: still in the
	// record, or held because this run could not judge them.
	keep []risk.Label
}

// planLabels decides the label changes. It adds every label in the record the
// pull request lacks, and removes only a label that a trusted earlier list
// says this tool set, that the record no longer has, and that this run could
// judge. A label already on the pull request that this tool did not set is a
// person's, and stays theirs: it is neither removed nor listed as this tool's.
func planLabels(current []ghclient.Label, rec risk.Record, held map[risk.Label]bool, previous []risk.Label) labelPlan {
	on := map[risk.Label]bool{}
	for _, l := range current {
		on[risk.Label(l.Name)] = true
	}
	was := map[risk.Label]bool{}
	for _, l := range previous {
		was[l] = true
	}
	var p labelPlan
	for _, l := range risk.All {
		switch {
		case rec.Has(l) && !on[l]:
			p.add = append(p.add, l)
		case rec.Has(l) && was[l]:
			p.keep = append(p.keep, l)
		case rec.Has(l):
			// A person's label, which happens to agree.
		case !was[l] || !on[l]:
			// Not ours, or already gone: someone removed it by hand.
		case held[l]:
			p.keep = append(p.keep, l)
		default:
			p.remove = append(p.remove, l)
		}
	}
	return p
}

// applyLabels carries out the plan. It returns the labels this tool now owns
// (the ones it kept, the ones it added, and any it failed to remove, so a
// later run tries again), and what it changed. With --dry-run it changes
// nothing and returns what it would have.
func (j riskJob) applyLabels(ctx context.Context, p labelPlan) (owned, added, removed []risk.Label, warnings []string) {
	own := map[risk.Label]bool{}
	for _, l := range p.keep {
		own[l] = true
	}
	if j.dryRun {
		for _, l := range p.add {
			own[l] = true
		}
		return inOrder(own), p.add, p.remove, nil
	}
	if len(p.add) > 0 {
		names := make([]string, 0, len(p.add))
		for _, l := range p.add {
			names = append(names, string(l))
		}
		if err := j.gh.AddLabels(ctx, j.repo, j.number, names); err != nil {
			warnings = append(warnings, fmt.Sprintf("could not add %s: %v", strings.Join(names, ", "), err))
		} else {
			for _, l := range p.add {
				own[l] = true
			}
			added = p.add
		}
	}
	for _, l := range p.remove {
		if err := j.gh.RemoveLabel(ctx, j.repo, j.number, string(l)); err != nil {
			warnings = append(warnings, fmt.Sprintf("could not remove %s: %v", l, err))
			own[l] = true
			continue
		}
		removed = append(removed, l)
	}
	return inOrder(own), added, removed, warnings
}

func inOrder(set map[risk.Label]bool) []risk.Label {
	var out []risk.Label
	for _, l := range risk.All {
		if set[l] {
			out = append(out, l)
		}
	}
	return out
}

// publish posts or updates the one risk comment, or with --dry-run prints it
// and the label changes. Every warning also goes to the job log, which is
// where it is read when the comment cannot be posted.
//
// canUpsert is false when the comments could not be read: posting then would
// add a second record beside the first, so the comment is printed instead.
func (j riskJob) publish(ctx context.Context, c reporters.RiskComment, previous *ghclient.IssueComment,
	canUpsert bool, add, remove []risk.Label) {

	for _, w := range c.Record.Warnings {
		fmt.Fprintf(j.log, "warning: %s\n", w)
	}
	body := reporters.RenderRisk(c)
	if j.dryRun {
		fmt.Fprintf(j.out, "--- risk record for #%d (dry run: nothing posted) ---\n%s", j.number, body)
		fmt.Fprintf(j.out, "--- labels ---\nwould add: %s\nwould remove: %s\n", labelList(add), labelList(remove))
		return
	}
	if !canUpsert {
		fmt.Fprintf(j.log, "warning: the risk comment was not posted, so as not to add a second one; it would have said:\n%s", body)
		return
	}

	var action string
	var err error
	switch {
	case previous != nil && previous.Body == body:
		// Rewriting it would re-notify everyone subscribed, to say nothing.
		action = "unchanged"
	case previous != nil:
		_, err = j.gh.UpdateIssueComment(ctx, j.repo, previous.ID, body)
		action = "updated"
	default:
		_, err = j.gh.CreateIssueComment(ctx, j.repo, j.number, body)
		action = "created"
	}
	if err != nil {
		fmt.Fprintf(j.log, "warning: could not post the risk comment: %v; it would have said:\n%s", err, body)
		return
	}
	fmt.Fprintf(j.out, "reviewer: risk record for #%d %s; labels added: %s; removed: %s\n",
		j.number, action, labelList(add), labelList(remove))
}

func labelList(labels []risk.Label) string {
	if len(labels) == 0 {
		return "none"
	}
	names := make([]string, 0, len(labels))
	for _, l := range labels {
		names = append(names, string(l))
	}
	return strings.Join(names, ", ")
}
