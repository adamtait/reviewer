// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/adamtait/reviewer/internal/builtin"
	"github.com/adamtait/reviewer/internal/config"
	"github.com/adamtait/reviewer/internal/diff"
	"github.com/adamtait/reviewer/internal/fingerprint"
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
	cfg, _, err := config.Resolve(o.root, o.config, getenv)
	if err != nil {
		return err
	}

	rep, err := reporter(o.reporter, stdout, stderr)
	if err != nil {
		return err
	}

	run := reporters.Run{Root: cfg.Root, Base: o.base}

	files, err := changedFiles(ctx, o, cfg.Root)
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
	if err := host.AddLocal(ctx, builtin.New(cfg, version), cfg.Analyzers.Timeout); err != nil {
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
		Root:         cfg.Root,
		Changed:      diff.ToPluginFiles(files),
		Projects:     cfg.Projects,
		ContextLines: cfg.Analyzers.ContextLines,
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

	// Shut the plugins down before reading their warnings. Closing is where "did
	// not exit within 5s, killing its process group" is recorded, and a deferred
	// Close would produce it after the report had already been written.
	host.Close()
	run.Warnings = append(run.Warnings, host.Warnings()...)

	if result.Selected == 0 {
		run.Warnings = append(run.Warnings,
			"no analyzers are configured; see .review/config.yaml and `reviewer init`")
	}
	return rep.Report(ctx, run)
}

func changedFiles(ctx context.Context, o options, root string) ([]diff.File, error) {
	switch {
	case o.staged:
		return diff.Staged(ctx, root)
	case o.pr != 0:
		// Reviewing a pull request by number needs the GitHub client, which
		// arrives in PR-17. Until then, say so rather than reviewing the wrong
		// thing silently.
		return nil, errors.New("--pr is not wired up yet; use --base or --staged")
	default:
		return diff.Changed(ctx, root, o.base)
	}
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

func reporter(name string, out, log io.Writer) (reporters.Reporter, error) {
	for _, r := range []reporters.Reporter{
		reporters.Text{Out: out},
		reporters.RDJSON{Out: out, Log: log},
	} {
		if r.Name() == name {
			return r, nil
		}
	}
	return nil, errUsage{fmt.Errorf("unknown reporter %q; want text or rdjson", name)}
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
