<!-- SPDX-License-Identifier: MIT -->
# ADR-0013 — Run analyzers cheap-first in a fixed order, and fail open

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-12
- **Supersedes:** —
- **Superseded by:** —

## Context

A run invokes up to a dozen analyzers whose costs differ by two orders of magnitude: a secrets scan
over changed files takes tens of milliseconds, a type-aware lint on a large repository can take
minutes. They also differ in reliability, because most of them wrap a binary or a library that may be
absent, misconfigured, or new enough to have changed its output format.

Two questions follow. What order, and what happens when one fails.

Running everything in parallel is the obvious answer to the first and is wrong here. The secrets scan
must complete before anything in the model lane starts (ADR-0012), which is an ordering constraint that
parallelism cannot express. And a developer watching a run wants the cheap, certain findings in seconds
rather than everything at once after the slowest analyzer finishes.

On the second question, the default in most tooling is to fail the run. For an advisory reviewer that
is the wrong trade: it converts "Knip is misconfigured" into "you got no review".

## Decision

Analyzers run sequentially, ordered by the `order` their descriptor declares, lowest first, with ties
broken on analyzer id so a run is reproducible whatever order the plugins happened to start in. The
core's own analyzers leave gaps between their orders so a plugin can interleave.

Every analyzer is bounded by a timeout. Exceeding it kills that analyzer's process group and the run
continues. Any failure — timeout, crash, unparseable output, a missing binary — costs that analyzer's
findings and nothing else; it becomes a warning on the report.

The single exception is the secrets analyzer, which fails **closed**: its failure blocks the model lane
(ADR-0012), because an unchecked diff and a clean diff are indistinguishable.

Every run reports per-analyzer timings by default rather than behind a flag. Latency is the risk this
project's own kill criteria are written against, so a slow analyzer should be visible without anyone
thinking to ask.

## Consequences

- **Positive:** a real problem surfaces in seconds. A broken analyzer degrades the review instead of
  cancelling it, which is what makes the tool safe to leave enabled. The timings make the latency kill
  criterion measurable from ordinary output.
- **Negative:** sequential execution means total latency is the sum rather than the maximum. With a
  fifteen-minute budget and one analyzer dominating it, that is affordable — but it is a ceiling this
  design accepts, and the escape hatch is parallelising within the deterministic lane only, after the
  secrets scan.
- **Neutral:** fail-open means a permanently broken analyzer can go unnoticed, since warnings are
  easier to ignore than failures. The per-rule acceptance measurement (ADR-0024) is what eventually
  surfaces an analyzer that never reports anything.

## Alternatives rejected

- **Parallel everything** — cannot express the gate's ordering constraint, and loses fast feedback.
- **Fail the run on any analyzer failure** — turns a misconfiguration into no review at all, and
  teaches people to disable the tool.
- **Timings behind a `--verbose` flag** — the number that matters for the one risk the plan flags as
  most likely to kill a feature should not require knowing to ask for it.

## Revisit when

Total latency approaches the budget with no single analyzer dominating, which is when parallelising the
deterministic lane starts to be worth its complexity.
