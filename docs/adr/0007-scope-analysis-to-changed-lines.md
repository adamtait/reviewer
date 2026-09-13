<!-- SPDX-License-Identifier: MIT -->
# ADR-0007 — Scope every finding to the changed lines, with no opt-out

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-06
- **Supersedes:** —
- **Superseded by:** —

## Context

The analyzers this system runs report on whole repositories. Pointed at a mature codebase, Knip finds
hundreds of unused exports, type-coverage finds thousands of implicit `any`s, and dependency-cruiser
finds every boundary violation anyone has ever committed. None of that is caused by the pull request
under review.

The failure mode is social rather than technical. A reviewer who sees forty comments, thirty-eight of
them about code the author did not touch, learns within a week that the tool is noise, and from then
on the two real findings are skipped along with the rest. That reputation is far harder to recover
than it is to avoid.

## Decision

Every finding must land on a line the diff changed, or it is dropped before any reporter sees it.
Analyzers are told the changed ranges in their `analyze` request and are expected to scope themselves
for speed, but the core filters again regardless — a plugin's diligence is not part of the guarantee.

There is no configuration option to report whole-repository findings. The diff is taken against the
merge base rather than the base branch tip, so work that landed on the base branch after this branch
diverged is not attributed to this pull request.

A finding whose span straddles changed and unchanged lines is kept: the change is what made the span
worth reporting.

## Consequences

- **Positive:** every comment the tool posts is about code in the diff, which is the only code the
  reviewer has agreed to think about. Runtime falls with it, since analyzers can restrict their work.
- **Negative:** pre-existing problems are invisible, including serious ones directly adjacent to the
  change. A repository-wide audit is a genuinely useful thing this tool will not do, and someone will
  eventually ask for it. The answer is a separate mode with a separate name, never a flag that changes
  what pull request review reports.
- **Neutral:** analyzers whose output cannot be attributed to a line — a project-wide type-coverage
  percentage, for instance — need a representative line chosen for them, which is why the
  type-coverage ratchet reports against the file whose ratio fell.

## Alternatives rejected

- **Whole-repository findings with severity filtering** — the noise returns at whatever threshold
  makes the tool useful, because severity does not correlate with "the author caused this".
- **A configuration flag to widen scope** — a flag that can be turned on will be turned on, and the
  first repository that does becomes the reason the team distrusts the tool.

## Revisit when

Someone wants a scheduled repository-wide audit. That is a different product surface with different
output, not a wider setting on this one.
