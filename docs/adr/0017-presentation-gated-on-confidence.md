<!-- SPDX-License-Identifier: MIT -->
# ADR-0017 — Gate presentation on confidence, not severity

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-18
- **Supersedes:** —
- **Superseded by:** —

## Context

An inline comment on a pull request is not a neutral notification. It is a claim that the reviewer should
stop reading, look at this line, and decide something. Spending that interruption on a finding that turns
out to be wrong is expensive twice: once for the time, and once for the credibility of the next comment.

Findings carry two independent axes. `severity` says how bad the thing would be if real — a type error is
worse than an unused export. `confidence` says how sure the analyzer is that it is real at all — a
compiler diagnostic is certain, a model's opinion about a missing abstraction is not.

Gating on severity is the intuitive choice and the wrong one. A model-produced "this looks like it should
be a port" is plausibly high severity and low confidence; posting it inline spends a reviewer's attention
on a guess.

## Decision

Only `confidence: high` findings are posted as inline comments. Everything else goes into a single
collapsed summary comment (PR-19). Severity affects wording and ordering, never placement.

Analyzers are held to this at the source. Compiler and type-aware lint diagnostics are high confidence
because they are computed, not guessed. The model lane's taste categories — missing abstractions, sloppy
code — are capped at `medium` in code rather than by prompt instruction (ADR-0022), so they cannot reach
an inline comment even if the model claims certainty.

## Consequences

- **Positive:** every inline comment is something the tool is prepared to defend, which is what makes the
  next one worth reading. The summary gives the uncertain findings somewhere to exist without costing
  attention.
- **Negative:** a high-severity, medium-confidence finding — a plausible security issue the model is not
  sure about — is easy to miss in a collapsed summary. That is the accepted cost, and the mitigation is
  measurement: if a category's acceptance rate is high, promote it to high confidence deliberately
  (ADR-0024) rather than making an exception for severity.
- **Neutral:** two axes is more than most tools carry, and analyzer authors have to think about both.
  The plugin descriptor does not enforce a relationship between them, on purpose: they are genuinely
  independent.

## Alternatives rejected

- **Gate on severity** — puts high-severity guesses in front of reviewers, which is the failure this
  decision exists to avoid.
- **Gate on a combined score** — hides the judgement in a formula and makes "why was this inline?"
  unanswerable.
- **Post everything inline and let the reader filter** — the reader's filter is muting the tool.

## Revisit when

A category sustains acceptance above roughly half for a month, at which point promoting it to high
confidence is a deliberate, measured change rather than a loosening of this rule.
