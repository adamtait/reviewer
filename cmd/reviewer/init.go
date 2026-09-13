// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"os"

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
	//
	// But only when there is someone there to answer, and only when the answer is
	// going to be used. `--provider` is honoured in every case; it is the question
	// that is conditional, not the choice.
	provider, err := installer.ChooseProvider(stdin, stdout, o.provider, !shouldAsk(o, stdin))
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
		if o.provider == "" {
			fmt.Fprintln(stderr, "reviewer: the real run asks which model access path to configure, "+
				"or pass --provider to see that part of the plan too")
		}
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

// shouldAsk reports whether the provider question can be both answered and used.
//
// Two ways it cannot. A dry run writes nothing, so the answer is discarded — and
// asking for it is how the first command in the README came to block. And a stdin
// that is not a terminal has nobody behind it: the reader already treats an
// immediate EOF as "not interactive", but an open pipe with no writer never
// reaches EOF, so it waits for input that will never arrive. That is a hang in
// any harness that leaves stdin attached, with no output to explain it, on the
// one command a stranger runs first.
//
// Neither case is an error. The model lane is off either way (ADR-0021); what is
// lost is a configured provider, and `--provider` supplies that without a
// question.
func shouldAsk(o options, stdin io.Reader) bool {
	switch {
	case o.yes, o.dryRun:
		return false
	default:
		return isTerminal(stdin)
	}
}

// isTerminal reports whether a reader is a character device — a terminal, rather
// than a pipe, a file or /dev/null.
//
// Deliberately not golang.org/x/term: this is the whole of what that dependency
// would be used for, and a direct dependency is a thing every consumer of this
// module acquires (ADR-0002).
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
