<!-- SPDX-License-Identifier: MIT -->
# ADR-0020 — Adapt to a repository at install time, not at run time

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-23
- **Supersedes:** —
- **Superseded by:** —

## Context

Every repository this tool reviews differs in ways it has to know about: which package manager owns the
lockfile, whether there are workspaces and where, which test runner the changed-tests analyzer should
name, whether there is a tsconfig to build a program from, whether ESLint has a flat config to read
rules out of. None of that is knowable from the binary.

There are two places the answers can live. They can be worked out on every run, by a binary that
inspects the repository each time it starts. Or they can be worked out once, written into the
destination repository as ordinary configuration, and read from there.

The run-time option is tempting because it needs no install step: point the binary at a repository and
it copes. But it makes detection a permanent dependency of every review. A heuristic that guesses
wrong then guesses wrong forever, silently, on every run, with nowhere for a person to correct it —
and the correction they want to make is almost always "no, use *this* tsconfig", which is a value, not
a better heuristic. It also puts repository-specific knowledge inside a binary that must contain none
of it (ADR-0004, ADR-0010), because the moment detection has a special case for one repository's
layout, that repository's layout is in this repository's source.

## Decision

Detection runs once, in `reviewer init`, and its output is a plan. The plan is printed before anything
is written, and `--dry-run` prints it and stops. What `init` decides ends up in `.review/config.yaml`
in the destination repository, which is a plain file under that repository's version control, editable
by hand and reviewable in a pull request like anything else.

The engine reads config. It does not detect. A value the engine needs and config does not carry is a
missing config key, not a cue to go looking.

Three properties follow, and each is a test:

- **Every detection states its basis.** The plan prints `npm (from package-lock.json)`, not `npm`. A
  wrong line is only correctable by someone who can see why it was concluded.
- **Every absence is reported with what it costs.** "No ESLint flat config: the eslint analyzer
  declines rather than inventing rules." An install that silently reviews less than the reader expects
  is worse than one that refuses.
- **`--dry-run` writes nothing**, asserted by `git status --porcelain` being empty afterwards.

`init` is also where the destination stops being this repository's business. Nothing it writes comes
back here; the templates are generic and public, the rendered output is the destination's.

## Consequences

- **Positive:** the engine has no detection code in it, so there is no path by which a destination
  repository's layout becomes a special case in public source.
- **Positive:** a wrong guess is a one-line edit to a tracked file, made once, visible in review.
  Under run-time detection it is a bug report against a heuristic.
- **Positive:** `--dry-run` gives a person a moment to disagree before anything exists. This is the
  only such moment, which is why the plan prints its reasoning and not only its conclusions.
- **Negative:** an install step exists, and a repository that changes shape — adds workspaces, moves
  its tsconfig — keeps its old config until someone re-runs `init`. Mitigated by `init` being
  idempotent and safe to re-run, and by the generated config being readable enough to edit instead.
- **Negative:** two sources of truth about the repository, the real layout and the config describing
  it, which can drift. Accepted: the drift is visible and fixable, where a bad heuristic is neither.
- **Neutral:** the plan is a value built before any write happens. The writer's job is to apply a plan
  that has already been inspected, so "what would happen" and "what happened" cannot diverge.

## Alternatives rejected

- **Detect on every run.** Makes a heuristic a permanent dependency of correctness, gives a person no
  place to overrule it, and pulls repository-specific knowledge into the binary.
- **Ask for everything interactively, detect nothing.** Correct and unusable: nobody can answer "which
  of your eleven tsconfigs" from memory, and an install nobody finishes reviews nothing.
- **Generate no config; take everything as flags.** Moves the repository-specific values into whoever
  writes the CI invocation, where they are unreviewable and copied wrongly between repositories.
- **Ship per-repository presets in this repository.** Exactly the thing ADR-0010 forbids.

## Revisit when

Re-running `init` after a layout change becomes the common failure — which would mean detection is
cheap and stable enough that running it every time, with config as an override rather than the source
of truth, is worth reconsidering.
