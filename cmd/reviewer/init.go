// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"

	"github.com/adamtait/reviewer/internal/installer"
)

// initialize adapts this tool to a repository it has never seen: it inspects the
// destination, builds a plan, and prints it.
//
// Writing is deliberately a later step. An installer that inspects and writes in
// one pass gives a person no moment at which to disagree with a detection, and
// every detection here is a guess that ends up in a generated config.
func initialize(o options, stdout, stderr io.Writer) error {
	detected, err := installer.Detect(o.root)
	if err != nil {
		return fmt.Errorf("inspecting %s: %w", o.root, err)
	}

	plan := installer.BuildPlan(detected, version, o.force)
	if err := plan.Write(stdout); err != nil {
		return err
	}

	if o.dryRun {
		fmt.Fprintln(stderr, "reviewer: --dry-run, nothing written")
		return nil
	}

	report, err := installer.Install(plan, version)
	if err != nil {
		// Partial writes are left in place rather than rolled back: everything
		// written is a git-tracked file in the destination, so `git checkout` is a
		// better undo than anything this tool could attempt, and a rollback that
		// itself failed would leave a worse state than the one it found.
		fmt.Fprintln(stdout)
		if writeErr := report.Write(stdout); writeErr != nil {
			return writeErr
		}
		return fmt.Errorf("installing into %s: %w", plan.Detected.Root, err)
	}

	fmt.Fprintln(stdout)
	return report.Write(stdout)
}
