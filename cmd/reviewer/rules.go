// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/adamtait/reviewer/internal/analyzers/opengrep"
	"github.com/adamtait/reviewer/internal/config"
)

// rulesTest checks a rule pack: that it loads, that no two rules claim the same
// id, that every rule has both a matching and a non-matching test case, and — when
// Opengrep is installed — that the patterns actually do what those cases say.
//
// The first three are checked here rather than left to Opengrep for two reasons.
// Opengrep refuses the whole run on a duplicate id, so one mistake hides every
// other rule's result and the message does not say which two files collided. And
// "every rule has a negative case" is this project's rule, not Opengrep's: a rule
// with only a positive case is a rule nobody has checked for false positives, and a
// false positive is how a rule pack loses its audience.
func rulesTest(ctx context.Context, o options, stdout, stderr io.Writer) error {
	root, err := filepath.Abs(o.root)
	if err != nil {
		return err
	}
	dir := o.rulesDir
	if dir == "" {
		dir = config.Defaults().Rules.Dir
	}

	pack, err := opengrep.LoadPack(root, dir)
	if err != nil {
		return err
	}
	if len(pack.Rules) == 0 && len(pack.Problems) == 0 {
		return errUsage{fmt.Errorf("no rule files in %s", dir)}
	}

	fmt.Fprintf(stdout, "%s, %s in %s\n",
		count(len(pack.Rules), "rule", "rules"),
		count(pack.Cases(), "annotated case", "annotated cases"),
		dir)

	failed := false
	for _, problem := range pack.Problems {
		failed = true
		fmt.Fprintf(stdout, "  broken   %s\n", problem)
	}
	for _, untested := range pack.Untested() {
		failed = true
		fmt.Fprintf(stdout, "  untested %s\n", untested)
	}

	// The patterns themselves. Delegated, because deciding whether a pattern
	// matches is Opengrep's job and reimplementing the judgement would mean this
	// tool and the analyzer could disagree about the same rule (ADR-0011).
	switch result := runEngineTest(ctx, root, dir, o); {
	case result.skipped != "":
		// Loud, not silent. `rules test` reporting success while never executing a
		// pattern is the one outcome that would make it worse than nothing.
		failed = true
		fmt.Fprintf(stdout, "  skipped  the patterns were not executed: %s\n", result.skipped)
	case result.err != nil:
		failed = true
		fmt.Fprintf(stdout, "  failed   %v\n", result.err)
		if result.output != "" {
			fmt.Fprintln(stderr, indent(result.output))
		}
	default:
		fmt.Fprintf(stdout, "  passed   every pattern behaved as its cases say\n")
	}

	if failed {
		return errCheckFailed{fmt.Errorf("rule pack in %s is not ready", dir)}
	}
	fmt.Fprintln(stdout, "ok")
	return nil
}

// engineResult separates "the engine disagreed with a case" from "there was no
// engine". Both mean the pack is not proven, and they need different next steps.
type engineResult struct {
	skipped string
	err     error
	output  string
}

func runEngineTest(ctx context.Context, root, dir string, o options) engineResult {
	binary := opengrep.Binary
	if o.config != "" {
		// A pinned path comes from config like every other tool path (ADR-0004).
		if cfg, _, err := config.Resolve(root, o.config, func(string) string { return "" }); err == nil {
			if tool, ok := cfg.Tools[opengrep.ID]; ok && tool.Path != "" {
				binary = tool.Path
			}
		}
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return engineResult{skipped: fmt.Sprintf("%s is not installed or not on PATH", binary)}
	}

	cmd := exec.CommandContext(ctx, path, "scan", "--test", "--metrics=off",
		"--disable-version-check", "--config", dir, dir)
	cmd.Dir = root
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return engineResult{err: fmt.Errorf("%s reported a failing case: %v", binary, runErr),
			output: strings.TrimSpace(string(out))}
	}
	return engineResult{}
}

func indent(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i, line := range lines {
		lines[i] = "    " + line
	}
	return strings.Join(lines, "\n")
}

func count(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
