// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/adamtait/reviewer/internal/builtin"
	"github.com/adamtait/reviewer/internal/config"
	"github.com/adamtait/reviewer/internal/diff"
	"github.com/adamtait/reviewer/internal/fingerprint"
	"github.com/adamtait/reviewer/internal/ghclient"
	"github.com/adamtait/reviewer/internal/pluginhost"
	"github.com/adamtait/reviewer/internal/reporters"
	"github.com/adamtait/reviewer/internal/sequencer"
	"github.com/adamtait/reviewer/pkg/finding"
	"github.com/adamtait/reviewer/pkg/plugin"
)

// review is the whole run: resolve configuration, work out what changed, start
// the plugins, ask each analyzer in turn, scope the results to the diff, report.
//
// Nothing in here returns an error for an analysis failure. A plugin that will
// not start, an analyzer that times out and a repository with no configuration
// are all warnings on an otherwise successful run (ADR-0009, ADR-0013).
func review(ctx context.Context, o options, stdout, stderr io.Writer, getenv func(string) string) error {
	cfg, secrets, err := config.Resolve(o.root, o.config, getenv)
	if err != nil {
		return err
	}

	cfg = applyFlags(o, cfg)

	rep, err := reporter(ctx, o, cfg, secrets, stdout, stderr)
	if err != nil {
		return err
	}

	run := reporters.Run{Root: cfg.Root, Base: o.base}

	files, base, notes, err := changedFiles(ctx, o, cfg, secrets)
	run.Warnings = append(run.Warnings, notes...)
	if err != nil {
		// Without a diff there is nothing to scope to, and reporting every
		// whole-repository finding is the one thing this tool must not do
		// (ADR-0007). Say so and report an empty run.
		run.Warnings = append(run.Warnings, fmt.Sprintf("could not determine what changed: %v", err))
		return rep.Report(ctx, run)
	}

	host := pluginhost.New("reviewer/"+version, stderr)
	// The built-in analyzers are a plugin like any other, reached over the
	// protocol through in-memory pipes rather than a subprocess (ADR-0027).
	if err := host.AddLocal(ctx, builtin.New(cfg, secrets, version), cfg.Analyzers.Timeout); err != nil {
		run.Warnings = append(run.Warnings, err.Error())
	}
	// Closed explicitly below so that shutdown warnings reach the report; the
	// deferred call is the safety net for the error paths and is idempotent.
	defer host.Close()
	if err := host.Start(ctx, cfg); err != nil {
		return err
	}

	only, skip := o.only, o.skip
	// A flag overrides the file rather than combining with it: "--only tsc" means
	// only tsc, not "only tsc, and also whatever the file said".
	if len(only) == 0 && len(skip) == 0 {
		only, skip = cfg.Analyzers.Only, cfg.Analyzers.Skip
	}

	req := plugin.AnalyzeRequest{
		Root:    cfg.Root,
		Changed: diff.ToPluginFiles(files),
		// The affected subset, not every declared project: a change confined to one
		// workspace should not make a plugin build the other eleven.
		Projects:     diff.Affected(files, cfg.Projects),
		ContextLines: cfg.Analyzers.ContextLines,
		// The ref the diff was actually taken against — the pull request's own
		// base, not the --base default. An analyzer that reads previous file
		// contents from a different ref than the diff came from answers a
		// different question than the one being reviewed.
		Base:   base,
		Staged: o.staged,
	}

	result := sequencer.Run(ctx, host, req, sequencer.Options{
		Only:             only,
		Skip:             skip,
		Timeout:          cfg.Analyzers.Timeout,
		SecretsAnalyzers: cfg.Gate.SecretsAnalyzers,
		// Scope before the gate decides, so "a credential in the diff" is literal
		// and a blocked lane always has a visible finding explaining it.
		Scope: func(in []finding.Finding) ([]finding.Finding, int) {
			return diff.Filter(in, files)
		},
	})
	run.Warnings = append(run.Warnings, result.Warnings...)
	// A run is a faithful account only if every selected analyzer actually ran and
	// none failed. Anything less reports fewer findings than exist, and nothing may
	// be removed from the pull request on that basis.
	run.Trustworthy = result.Selected > 0 && result.Selected == len(result.Timings) && !anyFailed(result.Timings)
	if result.Gate.Blocked {
		run.GateReason = result.Gate.Reason
	}
	run.Skipped = append(run.Skipped, result.Skipped...)
	run.Timings = timings(result.Timings)

	if result.Dropped > 0 {
		run.Skipped = append(run.Skipped,
			fmt.Sprintf("%d finding(s) outside the diff", result.Dropped))
	}
	all := result.Findings
	// Identity is assigned by the core, not by analyzers: a plugin has no reason
	// to know how dedupe works, and one that guessed would break it (ADR-0015).
	fingerprint.ComputeAll(cfg.Root, all)
	finding.Sort(all)

	run.Findings = all
	run.Skipped = append(run.Skipped, unavailable(host)...)
	// Read before Close, so the reason a run found nothing is about what the
	// plugins offered rather than about a shut-down host.
	offered, unavailable := len(host.Analyzers()), len(host.Unavailable())

	// Shut the plugins down before reading their warnings. Closing is where "did
	// not exit within 5s, killing its process group" is recorded, and a deferred
	// Close would produce it after the report had already been written.
	host.Close()
	run.Warnings = append(run.Warnings, host.Warnings()...)

	if result.Selected == 0 {
		run.Warnings = append(run.Warnings, nothingRanReason(cfg, offered, unavailable))
	}
	return rep.Report(ctx, run)
}

// applyFlags lets the command line override the file, as everywhere else.
//
// A named function rather than four lines inside review, so there is something to
// test. The version inlined there was covered only by a test asserting the flag had
// parsed — which stayed green with the override deleted.
func applyFlags(o options, cfg config.Config) config.Config {
	if o.noInvalidate {
		cfg.LaneB.Invalidate = false
	}
	return cfg
}

// nothingRanReason explains a run with no analyzers. The causes need different
// answers, and the wrong one sends a person to the wrong place: being told to run
// `reviewer init` when the real problem is an uninstalled binary, or being told to
// install something when a skip list in the config removed everything.
//
// The last case is the rarest, not the default: the built-in plugin is always
// registered, so a repository with an empty config still offers analyzers unless
// every one of them declined.
func nothingRanReason(cfg config.Config, offered, unavailable int) string {
	switch {
	case offered > 0:
		return "no analyzers ran: every one offered was removed by only/skip"
	case unavailable > 0:
		return fmt.Sprintf(
			"no analyzers ran: all %d declined, each for the reason listed above",
			unavailable)
	case len(cfg.Plugins) > 0:
		return fmt.Sprintf(
			"no analyzers ran: %d configured plugin(s) offered none; check they are installed",
			len(cfg.Plugins))
	default:
		return "no analyzers are configured; see .review/config.yaml and `reviewer init`"
	}
}

// changedFiles works out what the review is about, and returns the ref it was
// taken against.
//
// The ref is returned rather than re-derived by the caller, because they can
// differ: on a pull request the base is the pull request's own, which is not what
// --base defaults to. An analyzer that reads previous file contents from one ref
// while the diff came from another is answering a different question — and it
// does so silently, because both refs usually exist.
//
// For a pull request the base commit is preferred from the local checkout, which
// gives the same answer as a developer running the tool by hand. When that commit
// is absent — a shallow clone, or a branch the poller has never fetched — the
// API's per-file patches are parsed instead, so a missing `fetch-depth: 0` costs
// accuracy of context rather than the whole review.
//
// It also returns notes for the run's warnings: things about the choice of base
// a reader of the review should know.
func changedFiles(ctx context.Context, o options, cfg config.Config, secrets config.Secrets) ([]diff.File, string, []string, error) {
	switch {
	case o.staged:
		// The index against HEAD, so HEAD is what the files looked like before.
		files, err := diff.Staged(ctx, cfg.Root)
		return files, "", nil, err
	case o.pr != 0:
		return pullRequestFiles(ctx, o, cfg, secrets)
	default:
		files, err := diff.Changed(ctx, cfg.Root, o.base)
		return files, o.base, nil, err
	}
}

func pullRequestFiles(ctx context.Context, o options, cfg config.Config, secrets config.Secrets) ([]diff.File, string, []string, error) {
	client, repo, err := githubClient(cfg, secrets)
	if err != nil {
		return nil, "", nil, err
	}
	pr, err := client.GetPullRequest(ctx, repo, o.pr)
	if err != nil {
		return nil, "", nil, fmt.Errorf("reading pull request %d: %w", o.pr, err)
	}

	// Parents of HEAD tell whether this checkout is GitHub's test merge commit for
	// the pull request. Failing to read them only means it is not known to be.
	parents, _ := diff.Parents(ctx, cfg.Root, "HEAD")
	base, notes := reviewBase(o.base, pr, parents)
	if diff.HasCommit(ctx, cfg.Root, base) {
		files, err := diff.Changed(ctx, cfg.Root, base)
		return files, base, notes, err
	}

	files, err := client.ListFiles(ctx, repo, o.pr)
	if err != nil {
		return nil, "", notes, fmt.Errorf("listing the files of pull request %d: %w", o.pr, err)
	}
	patched := make([]diff.PatchedFile, 0, len(files))
	for _, f := range files {
		patched = append(patched, diff.PatchedFile{Path: f.Filename, Status: f.Status, Patch: f.Patch})
	}
	// The API's patches carry no base commit this checkout can read, so no ref is
	// returned: an analyzer that needs previous contents must say it cannot rather
	// than read them from a ref that is not the one the diff came from.
	parsed, err := diff.FromPatches(patched)
	return parsed, "", notes, err
}

// reviewBase decides which commit a pull request's review is measured from.
//
// An explicit --base wins, as it always has: someone reviewing against a
// different branch than GitHub thinks has a reason. The exception is a --base
// that names the pull request's own base branch, or the trunk of the stack it
// belongs to, while the checkout is GitHub's test merge commit for it. Then the
// merge commit's first parent is the same intent stated exactly: the base with
// every pull request below this one already merged in. It is what a
// pull_request workflow's default checkout gives, and it matters for stacks:
// there the event's base_ref names the stack's trunk, so `--base
// origin/${{ github.base_ref }}` would review every layer below this one too.
// For a pull request outside a stack the first parent is the base branch tip,
// and the review covers the same files it always did.
//
// Without --base the pull request's base.sha is used, as before, unless the
// checkout is the test merge commit, for the same reason.
//
// parents are HEAD's parents in the local checkout, first parent first.
func reviewBase(flagBase string, pr ghclient.PullRequest, parents []string) (base string, notes []string) {
	testMerge := len(parents) == 2 && pr.Head.SHA != "" && parents[1] == pr.Head.SHA
	unset := flagBase == "" || flagBase == defaultBase
	trunk := ""
	if pr.Stack != nil {
		trunk = pr.Stack.Base.Ref
	}
	// Above the bottom of a stack, the pull request's own base is another
	// pull request's branch, not the trunk.
	stackedAbove := trunk != "" && pr.Base.Ref != "" && pr.Base.Ref != trunk
	namesOwnBase := names(flagBase, pr.Base.Ref) || names(flagBase, trunk)

	switch {
	case testMerge && (unset || namesOwnBase):
		if stackedAbove && !unset {
			notes = append(notes, fmt.Sprintf(
				"pull request %d is stacked on %s; reviewing it from its test merge commit's first parent "+
					"(%s) rather than %s, which would also review every pull request below it",
				pr.Number, pr.Base.Ref, short(parents[0]), flagBase))
		}
		return parents[0], notes
	case unset:
		return pr.Base.SHA, nil
	default:
		if stackedAbove && names(flagBase, trunk) {
			notes = append(notes, fmt.Sprintf(
				"pull request %d is stacked on %s, and --base %s also reviews every pull request below it; "+
					"use --base origin/%s, or run on the pull_request checkout",
				pr.Number, pr.Base.Ref, flagBase, pr.Base.Ref))
		}
		return flagBase, notes
	}
}

// names reports whether a --base value refers to the branch called ref, in
// any of the spellings a workflow or a person uses for it.
func names(flagBase, ref string) bool {
	if ref == "" {
		return false
	}
	switch flagBase {
	case ref, "origin/" + ref, "refs/heads/" + ref, "refs/remotes/origin/" + ref:
		return true
	}
	return false
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// githubClient builds the client and repository from configuration. Shared by the
// reporter and the pull-request diff path so they cannot disagree about which
// repository is being reviewed.
func githubClient(cfg config.Config, secrets config.Secrets) (*ghclient.REST, ghclient.Repo, error) {
	client, err := ghclient.New(cfg.GitHub.APIBaseURL, secrets.GitHubToken, "reviewer/"+version)
	if err != nil {
		return nil, ghclient.Repo{}, errUsage{err}
	}
	if cfg.GitHub.GraphQLURL != "" {
		client.GraphQLURL = cfg.GitHub.GraphQLURL
	}
	repo, err := parseRepo(cfg.GitHub.Repo)
	if err != nil {
		return nil, ghclient.Repo{}, errUsage{err}
	}
	return client, repo, nil
}

func unavailable(host *pluginhost.Manager) []string {
	var out []string
	for _, a := range host.Unavailable() {
		reason := a.Unavailable
		if reason == "" {
			reason = "not available in this repository"
		}
		out = append(out, fmt.Sprintf("%s: %s", a.ID, reason))
	}
	return out
}

func reporter(ctx context.Context, o options, cfg config.Config, secrets config.Secrets,
	out, log io.Writer) (reporters.Reporter, error) {

	switch o.reporter {
	case "text":
		return reporters.Text{Out: out}, nil
	case "rdjson":
		return reporters.RDJSON{Out: out, Log: log}, nil
	case "github":
		return githubReporter(ctx, o, cfg, secrets, out, log)
	default:
		return nil, errUsage{fmt.Errorf("unknown reporter %q; want text, rdjson or github", o.reporter)}
	}
}

// githubReporter assembles the reporter that writes to a pull request. Every
// failure here is a usage error rather than a run failure: being asked to comment
// on a pull request and silently not doing it is worse than saying why.
func githubReporter(ctx context.Context, o options, cfg config.Config, secrets config.Secrets,
	out, log io.Writer) (reporters.Reporter, error) {

	if o.pr == 0 {
		return nil, errUsage{errors.New("--reporter github needs --pr to say which pull request to comment on")}
	}
	client, repo, err := githubClient(cfg, secrets)
	if err != nil {
		return nil, err
	}
	pr, err := client.GetPullRequest(ctx, repo, o.pr)
	if err != nil {
		// Deliberately not a usage error. A transient API failure must not exit 2
		// and red the check on a tool that promises never to fail a build
		// (ADR-0009); the run reports the failure and exits 0.
		return nil, fmt.Errorf("reading pull request %d: %w", o.pr, err)
	}
	return reporters.GitHub{
		Client: client, Writer: client, Repo: repo, Number: o.pr,
		HeadSHA: pr.Head.SHA, DryRun: o.dryRun,
		ResolveStale: cfg.GitHub.ResolveStaleThreads,
		SelfLogin:    cfg.GitHub.SelfLogin,
		Out:          out, Log: log,
	}, nil
}

func parseRepo(spec string) (ghclient.Repo, error) {
	owner, name, ok := strings.Cut(strings.TrimSpace(spec), "/")
	if !ok || owner == "" || name == "" {
		return ghclient.Repo{}, fmt.Errorf("github.repo is %q, want owner/name", spec)
	}
	return ghclient.Repo{Owner: owner, Name: name}, nil
}

// timings formats the per-analyzer timings for the report. Latency is the risk
// this project's kill criteria are written against, so a run always says where
// its time went.
func timings(in []sequencer.Timing) []reporters.Timing {
	out := make([]reporters.Timing, 0, len(in))
	for _, t := range in {
		out = append(out, reporters.Timing{
			Analyzer: t.Analyzer,
			Elapsed:  t.Elapsed,
			Findings: t.Findings,
			Failed:   t.Failed,
		})
	}
	return out
}

// anyFailed reports whether an analyzer failed during the run.
func anyFailed(timings []sequencer.Timing) bool {
	for _, t := range timings {
		if t.Failed {
			return true
		}
	}
	return false
}
