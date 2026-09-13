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

	if !o.dryRun {
		// Not a usage error: the command is right, the writers are not built yet.
		// Exiting 0 keeps ADR-0009's promise that only misuse is non-zero.
		fmt.Fprintln(stderr,
			"reviewer: nothing written — init can only plan so far; run with --dry-run to say so explicitly")
		return nil
	}
	fmt.Fprintln(stderr, "reviewer: --dry-run, nothing written")
	return nil
}
