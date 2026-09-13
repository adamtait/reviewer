<!-- SPDX-License-Identifier: MIT -->
# ADR-0009 — Never block a pull request

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-07
- **Supersedes:** —
- **Superseded by:** —

## Context

Half of what this system reports is model judgement about missing abstractions and code quality. Those
categories are expected to sit well under 100% acceptance, and the plan explicitly allows for deleting
them if they fall below thirty percent.

A tool that can block a merge on a judgement of that accuracy will be turned off, and it will be
turned off in the way that takes the deterministic half with it — because the person disabling it is
unblocking a release, not conducting a review of which analyzers deserve to stay.

The deterministic half is not exempt either. A type error or a floating promise is real, but the
repository's own CI already fails on those. Blocking twice adds nothing and doubles the ways a
transient failure in this tool can stop someone's work.

## Decision

The binary exits 0 whatever it finds and whatever fails while it looks: a plugin that will not start,
an analyzer that times out, a model provider that returns 500, an unreadable configuration file. All
of these are reported and none changes the exit status.

The single exception is a usage error — an unknown flag, contradictory options, an unknown reporter —
which exits 2. That is not a review outcome; it means the invocation was wrong, and silently treating
it as a clean review would hide the fact that nothing ran.

There is no configuration option to make findings blocking.

## Consequences

- **Positive:** the tool cannot become the reason a release is late, so nobody has a reason to disable
  it wholesale. Adoption is a decision about usefulness rather than about risk.
- **Negative:** nothing forces anyone to act on a finding. The only pressure is that the comment is
  visible on the pull request, and the only feedback loop is the acceptance rate (ADR-0024). A team
  that ignores the tool entirely will not be stopped by it.
- **Neutral:** the exit status carries no information about findings, so anything downstream that
  wants to gate must parse the reporter's output. That is deliberate: such a gate is then explicitly
  someone's choice, in their own workflow, and not a default of this tool.

## Alternatives rejected

- **Blocking on high-confidence deterministic findings only** — defensible on the merits, and still
  creates the disable-it-to-ship reflex the first time a false positive lands on a hotfix.
- **A configurable threshold** — a setting that can block will eventually be set to block, and the
  consequence lands on whoever is shipping at the time rather than on whoever set it.

## Revisit when

Acceptance on the deterministic lane has been near-total for a sustained period *and* a team asks for
blocking with the trade understood. Even then the right shape is that team's own CI step reading the
rdjson output, not a flag here.
