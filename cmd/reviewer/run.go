// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/adamtait/reviewer/internal/analyzers/gitleaks"
	"github.com/adamtait/reviewer/internal/builtin"
	"github.com/adamtait/reviewer/internal/config"
	"github.com/adamtait/reviewer/internal/diff"
	"github.com/adamtait/reviewer/internal/gate"
	"github.com/adamtait/reviewer/internal/pluginhost"
	"github.com/adamtait/reviewer/internal/reporters"
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
	analyzers := selectAnalyzers(host.Analyzers(), only, skip)
	req := plugin.AnalyzeRequest{
		Root:         cfg.Root,
		Changed:      diff.ToPluginFiles(files),
		Projects:     cfg.Projects,
		ContextLines: cfg.Analyzers.ContextLines,
	}

	// Two passes with the gate between them. Every deterministic analyzer runs
	// first; only then, and only if nothing turned up a credential, may anything
	// in the model lane run (ADR-0012). The ordering is structural rather than a
	// check inside the loop, so there is no path from a finding to a model call.
	deterministic, llm := splitByLane(analyzers)

	all, scanned := runPass(ctx, host, deterministic, req, &run)

	gateState := gate.Decide(all, scanned)
	switch {
	case gateState.Blocked && len(llm) > 0:
		run.Skipped = append(run.Skipped, gateState.Reason)
	case gateState.Blocked:
		// Nothing in the model lane to block; saying so would be noise.
	default:
		found, _ := runPass(ctx, host, llm, req, &run)
		all = append(all, found...)
	}

	kept, dropped := diff.Filter(all, files)
	if dropped > 0 {
		run.Skipped = append(run.Skipped,
			fmt.Sprintf("%d finding(s) outside the diff", dropped))
	}
	finding.Sort(kept)

	run.Findings = kept
	run.Skipped = append(run.Skipped, unavailable(host)...)

	// Shut the plugins down before reading their warnings. Closing is where "did
	// not exit within 5s, killing its process group" is recorded, and a deferred
	// Close would produce it after the report had already been written.
	host.Close()
	run.Warnings = append(run.Warnings, host.Warnings()...)

	if len(analyzers) == 0 {
		run.Warnings = append(run.Warnings,
			"no analyzers are configured; see .review/config.yaml and `reviewer init`")
	}
	return rep.Report(ctx, run)
}

// splitByLane separates the analyzers the gate may block from those it may not.
func splitByLane(in []pluginhost.Registered) (deterministic, llm []pluginhost.Registered) {
	for _, a := range in {
		if a.Lane == finding.LaneLLM {
			llm = append(llm, a)
		} else {
			deterministic = append(deterministic, a)
		}
	}
	return deterministic, llm
}

// runPass runs one lane's analyzers. It also reports whether a secrets analyzer
// completed, which is what lets the gate tell "clean" from "not checked".
func runPass(ctx context.Context, host *pluginhost.Manager, analyzers []pluginhost.Registered,
	req plugin.AnalyzeRequest, run *reporters.Run) (found []finding.Finding, secretsScanned bool) {

	for _, a := range analyzers {
		results, warnings, err := host.Analyze(ctx, a.PluginID, a.ID, req)
		if err != nil {
			run.Warnings = append(run.Warnings, fmt.Sprintf("%s: %v", a.ID, err))
			continue
		}
		if a.ID == gitleaks.ID {
			secretsScanned = true
		}
		run.Warnings = append(run.Warnings, warnings...)
		found = append(found, results...)
	}
	return found, secretsScanned
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

// selectAnalyzers applies --only and --skip and puts the survivors in the order
// their descriptors asked for. The ordering policy itself, with timings and the
// fail-open rules, lands in PR-12.
func selectAnalyzers(in []pluginhost.Registered, only, skip []string) []pluginhost.Registered {
	wanted := func(id string) bool {
		if len(only) > 0 {
			return contains(only, id)
		}
		return !contains(skip, id)
	}

	out := make([]pluginhost.Registered, 0, len(in))
	for _, a := range in {
		if wanted(a.ID) {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].ID < out[j].ID
	})
	return out
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

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
