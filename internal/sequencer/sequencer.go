// SPDX-License-Identifier: MIT

// Package sequencer decides what runs, in what order, and what happens when one
// of them fails.
//
// The policy, in one place because it is the part that is easy to get subtly
// wrong: cheap analyzers first so a real problem surfaces in seconds; every
// analyzer bounded by a timeout; a failure loses that analyzer's findings and
// nothing else (ADR-0013); and the model lane separated from the deterministic
// lane by the secrets gate, structurally rather than by a condition (ADR-0012).
package sequencer

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/adamtait/reviewer/internal/gate"
	"github.com/adamtait/reviewer/internal/pluginhost"
	"github.com/adamtait/reviewer/pkg/finding"
	"github.com/adamtait/reviewer/pkg/plugin"
)

// Host is the part of the plugin manager the sequencer needs. Narrow so the
// sequencer's policy can be tested without starting a process.
type Host interface {
	Analyzers() []pluginhost.Registered
	Analyze(ctx context.Context, pluginID, analyzerID string, req plugin.AnalyzeRequest) ([]finding.Finding, []string, error)
}

// Options select and bound the run.
type Options struct {
	// Only and Skip filter by analyzer id. Only wins when both are set.
	Only []string
	Skip []string
	// Timeout bounds one analyzer. Zero means the host's own default.
	Timeout time.Duration
	// SecretsAnalyzers names the analyzers whose successful completion means the
	// diff was checked. Configured rather than hardcoded, so that a replacement
	// secrets scanner does not leave the model lane permanently blocked.
	SecretsAnalyzers []string
	// Scope narrows a lane's findings to the diff before the gate sees them, so
	// "a credential in the diff" means exactly that. It reports how many it
	// dropped, because a run that discards an analyzer's work should say so rather
	// than look clean. Optional; identity when nil.
	Scope func([]finding.Finding) (kept []finding.Finding, dropped int)
}

// Timing is how long one analyzer took, for the run log. Latency is the risk this
// project's kill criteria are written against, so it is reported by default
// rather than behind a flag.
type Timing struct {
	Analyzer string
	Elapsed  time.Duration
	Findings int
	Failed   bool
}

// Result is everything a run produced.
type Result struct {
	Findings []finding.Finding
	Warnings []string
	Skipped  []string
	Timings  []Timing
	// Gate records the secrets gate's decision, whether or not it blocked.
	Gate gate.State
	// Selected is how many analyzers survived the filters. Distinct from the
	// number that ran: a gate-blocked run selects analyzers it does not invoke,
	// and reporting "no analyzers are configured" in that case is wrong.
	Selected int
	// Dropped counts findings discarded for falling outside the diff.
	Dropped int
}

// Run executes the analyzers the host offers, in order, with the gate between the
// lanes.
func Run(ctx context.Context, host Host, req plugin.AnalyzeRequest, opts Options) Result {
	var res Result

	selected := selectAnalyzers(host.Analyzers(), opts.Only, opts.Skip)
	res.Selected = len(selected)
	deterministic, llm := split(selected)

	found, scanned := run(ctx, host, deterministic, req, &res, opts.SecretsAnalyzers)
	// Scope before the gate decides. Without this the gate would block on a
	// credential that sits in a changed file but outside the changed lines — a
	// finding the report then drops, leaving a blocked lane with nothing visible
	// to explain it.
	if opts.Scope != nil {
		var dropped int
		found, dropped = opts.Scope(found)
		res.Dropped += dropped
	}
	res.Findings = append(res.Findings, found...)

	res.Gate = gate.Decide(res.Findings, scanned)
	switch {
	case res.Gate.Blocked && len(llm) > 0:
		res.Skipped = append(res.Skipped, res.Gate.Reason)
		for _, a := range llm {
			res.Skipped = append(res.Skipped, fmt.Sprintf("%s: not run (model lane blocked)", a.ID))
		}
	case res.Gate.Blocked:
		// Nothing in the model lane to block; reporting it would be noise.
	default:
		found, _ := run(ctx, host, llm, req, &res, opts.SecretsAnalyzers)
		if opts.Scope != nil {
			var dropped int
			found, dropped = opts.Scope(found)
			res.Dropped += dropped
		}
		res.Findings = append(res.Findings, found...)
	}
	return res
}

// run executes one lane and records what happened. It never returns an error: an
// analyzer that fails costs its own findings and nothing else.
func run(ctx context.Context, host Host, analyzers []pluginhost.Registered,
	req plugin.AnalyzeRequest, res *Result, secretsAnalyzers []string) (found []finding.Finding, secretsScanned bool) {

	for _, a := range analyzers {
		start := time.Now()
		results, warnings, err := host.Analyze(ctx, a.PluginID, a.ID, req)
		elapsed := time.Since(start)

		res.Timings = append(res.Timings, Timing{
			Analyzer: a.ID,
			Elapsed:  elapsed,
			Findings: len(results),
			Failed:   err != nil,
		})

		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %v", a.ID, err))
			continue
		}
		if contains(secretsAnalyzers, a.ID) {
			secretsScanned = true
		}
		res.Warnings = append(res.Warnings, warnings...)
		found = append(found, results...)
	}
	return found, secretsScanned
}

// selectAnalyzers applies the filters and sorts by declared order, lowest first.
// Ties break on id so a run is reproducible regardless of plugin start order.
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

func split(in []pluginhost.Registered) (deterministic, llm []pluginhost.Registered) {
	for _, a := range in {
		if a.Lane == finding.LaneLLM {
			llm = append(llm, a)
		} else {
			deterministic = append(deterministic, a)
		}
	}
	return deterministic, llm
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
