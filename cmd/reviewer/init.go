// SPDX-License-Identifier: MIT

package main

import (
	"errors"
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
func initialize(o options, stdin io.Reader, stdout, stderr io.Writer) error {
	detected, err := installer.Detect(o.root)
	if err != nil {
		// Misuse: --root named something that is not a repository to install into.
		// ADR-0009 reserves a non-zero exit for exactly this, and `init` is not a
		// review — a script that runs `reviewer init && git add -A` has to be able
		// to tell a refusal from a success.
		return errUsage{fmt.Errorf("inspecting %s: %w", o.root, err)}
	}

	// Asked before the plan is built, because the answer changes what the plan
	// says. A prompt after the plan would be asking someone to approve a plan and
	// then altering it.
	provider, err := installer.ChooseProvider(stdin, stdout, o.provider, o.yes)
	if err != nil && !errors.Is(err, installer.ErrNoProviderChosen) {
		// A name that is not a provider is misuse: installing something other than
		// what was asked for would be worse than refusing.
		return errUsage{err}
	}

	plan := installer.BuildPlan(detected, version, installer.Options{
		Force:    o.force,
		Provider: provider,
	})
	if err := plan.Write(stdout); err != nil {
		return err
	}

	if o.dryRun {
		fmt.Fprintln(stderr, "reviewer: --dry-run, nothing written")
		return nil
	}
	if len(plan.Problems) > 0 {
		// The plan has already printed them. Refusing is the point: every problem
		// means a write would land somewhere other than where the plan said.
		return errUsage{fmt.Errorf("refusing to install into %s", plan.Detected.Root)}
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
