// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

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

	cfg, secrets, err := config.Resolve(o.root, o.config, getenv)
	if err != nil {
		return err
	}

	host := pluginhost.New("reviewer/"+version, stderr)
	if err := host.AddLocal(ctx, builtin.New(cfg, secrets, version), cfg.Analyzers.Timeout); err != nil {
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

	// Recorded before, so success is decided by the file changing rather than by
	// the analyzer having said something. Every failure path in that analyzer also
	// returns a warning — no tsconfig, an unreadable one, nothing to measure — so
	// "it said something" cannot tell a recorded baseline from a refusal.
	before := stamp(cfg.Root)

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
	for _, w := range warnings {
		fmt.Fprintln(stdout, w)
	}

	if after := stamp(cfg.Root); after == before {
		return errCheckFailed{fmt.Errorf(
			"nothing was recorded to %s; the analyzer's reason is above",
			baselinePath)}
	}
	return nil
}

// baselinePath is where the ratchet's mark lives, mirroring the analyzer that
// writes it. Named here only so this command can check that a write happened.
const baselinePath = ".review/type-coverage-baseline.json"

// stamp identifies the baseline file's current state. Size and modification time
// rather than a hash: the question is whether this command's own call wrote it,
// and a rewrite with identical content is a recorded baseline either way.
func stamp(root string) string {
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(baselinePath)))
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d/%d", info.Size(), info.ModTime().UnixNano())
}
