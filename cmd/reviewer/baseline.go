// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/adamtait/reviewer/internal/builtin"
	"github.com/adamtait/reviewer/internal/config"
	"github.com/adamtait/reviewer/internal/pluginhost"
	"github.com/adamtait/reviewer/pkg/plugin"
)

// typeCoverageAnalyzer is the analyzer that owns the type-coverage ratchet. Named
// here rather than discovered, because `baseline --write` is a request to record
// one specific measurement, not to run whatever happens to be installed.
const typeCoverageAnalyzer = "type-coverage"

// baseline records the ratchet's mark: the state a future run compares against.
//
// The measurement belongs to the analyzer that performs it, not to the core — the
// core has no TypeScript compiler and no business having one (ADR-0002). So this
// command starts the plugins, finds the one analyzer that can answer, and asks it
// to record. The instruction travels in that analyzer's settings block, which the
// protocol already passes through untouched; a dedicated frame would be a protocol
// change for one command.
func baseline(ctx context.Context, o options, stdout, stderr io.Writer, getenv func(string) string) error {
	if !o.write {
		return errUsage{fmt.Errorf("baseline needs --write; there is nothing else it does yet")}
	}

	cfg, _, err := config.Resolve(o.root, o.config, getenv)
	if err != nil {
		return err
	}

	host := pluginhost.New("reviewer/"+version, stderr)
	if err := host.AddLocal(ctx, builtin.New(cfg, version), cfg.Analyzers.Timeout); err != nil {
		fmt.Fprintf(stderr, "reviewer: %v\n", err)
	}
	defer host.Close()
	if err := host.Start(ctx, cfg); err != nil {
		return err
	}

	var owner string
	for _, registered := range host.Analyzers() {
		if registered.ID == typeCoverageAnalyzer {
			owner = registered.PluginID
			break
		}
	}
	if owner == "" {
		return errCheckFailed{fmt.Errorf(
			"no analyzer named %q is available; the TypeScript plugin provides it, so check it is "+
				"installed and configured in %s", typeCoverageAnalyzer, config.DefaultPath)}
	}

	settings, err := json.Marshal(map[string]bool{"writeBaseline": true})
	if err != nil {
		return err
	}

	// No changed files: the measurement is over the whole program, and a baseline
	// taken from a diff would be a baseline for that diff.
	_, warnings, err := host.Analyze(ctx, owner, typeCoverageAnalyzer, plugin.AnalyzeRequest{
		Root:     cfg.Root,
		Settings: settings,
	})
	if err != nil {
		return errCheckFailed{fmt.Errorf("recording the baseline: %w", err)}
	}

	// The analyzer reports what it wrote, because it is the only component that
	// knows the number.
	if len(warnings) == 0 {
		return errCheckFailed{fmt.Errorf("the analyzer recorded nothing and said nothing")}
	}
	for _, w := range warnings {
		fmt.Fprintln(stdout, w)
	}
	return nil
}
