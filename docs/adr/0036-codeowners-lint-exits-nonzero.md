<!-- SPDX-License-Identifier: MIT -->
# ADR-0036 — Exit 1 when the CODEOWNERS lint fails: it is a check, not a review

- **Status:** Accepted
- **Date:** 2026-11-27
- **Implemented by:** PR-59
- **Supersedes:** —
- **Superseded by:** —

## Context

The docs-only merge class in `docs/designs/risk-gated-merge.md` is delivered by GitHub's own
settings, not by code: CODEOWNERS owns `*` and leaves a documentation allowlist unowned, and the
repository ruleset requires code-owner review with 0 general approvals. Under those settings a path
is reviewed only if CODEOWNERS gives it an owner. A wrong CODEOWNERS file removes review from a
path, and nothing on the pull request shows it.

Goal G5 says no path outside the allowlist loses review, and nothing a model or an agent reads is on
it. With 1 required approval, a mistake in CODEOWNERS costs little, because a human still reads
every pull request. At 0 approvals, nothing else stands between a mistaken rule and an unreviewed
merge. Something has to fail the pull request that introduces the mistake, and GitHub decides what
fails a pull request by the exit status of a required check.

ADR-0009 says `reviewer` exits 0 whatever it finds. That decision is about reviews: findings are
partly judgement, and a tool that blocks on judgement gets turned off. `reviewer rules test` already
exits 1 when a rule pack is not ready (`errCheckFailed` in `cmd/reviewer/flags.go`), on the grounds
that it is a check with a right answer, run by someone who wants to know, and it has to be usable in
a script.

## Decision

`reviewer owners check` exits 1 when CODEOWNERS fails the lint, like `rules test`, and the setup
guide (`docs/merge-classes.md`) tells repositories to make it a required check. It fails when the file
is missing; when a line does not parse; when the first rule is not `*` with an owner; when CODEOWNERS
does not own itself, or a place where a new CODEOWNERS file would replace it; when a rule with no
owners is not a directory; and when a path on the never-allowlist resolves unowned. The
never-allowlist is a compiled-in list (agent instruction files, `.claude/`, `.github/`, `.review/`,
`docs/adr/`), every `guidance:` path and `laneB.promptDir` from `.review/config.yaml`, and every file a
`//go:embed` directive in the working tree names. Every violation is listed, with its CODEOWNERS
line where there is one. A usage error, including a configuration file the decoder refuses, exits 2
as it does everywhere else. Reviews still exit 0: ADR-0009 governs reviews, and this command is not
one.

## Consequences

- **Positive:** at 0 required approvals, the lint is what keeps G5 true. A pull request that adds an
  unowned rule for `.claude/`, or for a directory listed under `guidance:`, cannot merge until the
  rule is fixed, and the output names the line.
- **Positive:** the answer has no judgement in it. The same file gives the same result every time, so
  the reason ADR-0009 gives for never blocking (a false positive on a judgement) does not apply.
- **Negative:** the never-allowlist check is a probe, not a proof. It asks CODEOWNERS about concrete
  paths: each entry, a file under each directory entry, and paths inside each unowned rule. A rule
  that exposes a listed path only at some other path can pass. The rule that every unowned rule is a
  directory narrows what can slip through; it does not close it.
- **Negative:** only Go's `//go:embed` is detected. Files read by other build steps (code generators,
  bundlers, a Dockerfile's `COPY`) have to be kept off the allowlist by hand, and the setup guide says
  so.
- **Negative:** a repository whose configuration a pinned binary cannot decode (a key added by a
  newer release) fails the required check with exit 2 until the binary is updated. That fails closed,
  which is the right direction for a merge control, and it is still a failed check someone has to
  clear.
- **Neutral:** `reviewer` now has two commands that exit 1 on their subject. Both are checks, both are
  named as such in the usage text, and neither is a review.

## Alternatives rejected

- **Exit 0 and print the violations, as a review does** — a required check that always passes
  protects nothing, and at 0 approvals nobody is reading the log.
- **Exit 0 and post a comment** — a pull request that changes CODEOWNERS needs a code owner's
  approval anyway, but the point is that the mistake cannot merge even if that owner misses it. A
  comment does not stop a merge.
- **Have `reviewer init` write CODEOWNERS correctly instead** — it cannot know the teams, and a file
  that is right on the day it is written can be made wrong by any later pull request. The lint checks
  every change.

## Revisit when

GitHub can enforce the same property itself (for example, a ruleset that refuses unowned paths), or
the docs-only class moves under `merge-gate` and its policy file, at which point the lint either
becomes part of the gate or is no longer needed.
