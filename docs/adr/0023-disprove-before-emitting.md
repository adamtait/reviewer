<!-- SPDX-License-Identifier: MIT -->
# ADR-0023 — Disprove every model-lane finding before emitting it

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-36
- **Supersedes:** —
- **Superseded by:** —

## Context

The model lane exists for the things a pattern matcher cannot see. Everything it reports is therefore
a judgement, and the failure mode of judgement at scale is not being wrong occasionally — it is being
wrong *confidently*, about code somebody has already thought about.

That specific failure is more expensive than it looks. A reviewer who reads three findings and finds
the first two are about deliberate behaviour does not read the third carefully, and does not read the
next pull request's at all. The whole lane's value is spent on the first few comments it posts, and it
cannot be earned back by being right later.

Asking for higher confidence does not help. A single pass produces findings that are individually
plausible — that is exactly what a language model is good at — and the plausible ones are the
dangerous ones, because they survive a skim.

## Decision

Every candidate goes through a second call whose only job is to disprove it.

The judge is given the same diff and the claims, and is asked to find the reason each one fails: it is
factually wrong, it is already handled, it is intended, or it is not about the changed lines. It is
explicitly told that "minor", "I would have written it differently" and "I am not sure" are not
reasons.

**A disproof must point at something.** This is the part that makes the pass work rather than
halve the output at random. A model asked to find fault with a claim will always find something to
say, so "it said the claim fails" cannot be the test. The test is whether the reason quotes a fragment
that actually appears in the diff, or names a `file:line`. A quoted fragment that is *not* in the diff
is rejected — that is the same confident-and-wrong failure arriving from the other direction, and
accepting it would let the pass delete true findings on invented evidence.

Everything else keeps the finding:

- No verdict for a candidate → it stands. Silence has disproved nothing. Verdicts are matched by
  index rather than position, so a reply missing one entry cannot shift every later verdict onto the
  wrong finding.
- A vague challenge → it stands, and the run says how many were challenged without a reason.
- The pass errored, was refused, or returned something unparseable → **everything** stands, and the
  run says the findings are unchecked. A pass that could not run has established nothing about what
  is right, and deleting findings on its failure would be the worst possible reading of it.

A disproved finding is dropped silently. Nobody needs to hear about a finding that was wrong.

One call for all candidates, not one per candidate: the disproof of a claim usually lives in the same
diff as the disproof of its neighbours, so N calls would pay N times for the same context.

`--no-invalidate` switches the pass off for debugging what the first pass produces. A run under it
says its findings are unchecked, because the number of findings is the thing the flag changes and a
reader comparing two runs needs to know which is which.

## Consequences

- **Positive:** the class of finding that destroys trust — confident, wrong, about something
  deliberate — is the exact class this removes, because "it is intended" is one of the four ways a
  claim fails.
- **Positive:** the evidence requirement becomes real. Every emitted finding has survived a specific
  attempt to refute it, which is a stronger statement than the first pass's own confidence.
- **Positive:** failing open in every direction means the pass can only ever remove findings it
  specifically refuted. It cannot silently empty the lane.
- **Negative:** two model calls per review rather than one, so roughly twice the cost and twice the
  latency. Accepted: the lane's cost is dominated by the context, which both calls share, and a
  reviewer's attention is the expensive resource being protected.
- **Negative:** true findings are lost when the judge produces a specific-looking but wrong disproof.
  Unmeasurable from inside the system; this is what `reviewer metrics` per rule id exists to expose
  from the outside (ADR-0024).
- **Negative:** the specificity test is a heuristic — a quote and a line number. A determined model
  could satisfy it while saying nothing. Accepted: it raises the cost of a lazy disproof, which is the
  actual failure mode, and it is checkable by a reader of the code.
- **Neutral:** the judge sees the first pass's stated confidence. It could defer to it. The claims are
  rendered without it for that reason.

## Alternatives rejected

- **Single pass with a confidence threshold.** The precision problem is exactly that confidence is
  self-reported, and this is the one place it cannot be trusted.
- **Ask for self-criticism in the same call.** A model that has just argued for a finding is the worst
  available judge of it, and there is nowhere to enforce specificity.
- **One invalidation call per candidate.** Pays for the same context N times for an answer that is
  usually in the same lines.
- **Drop findings when the judge is merely uncertain.** Halves the output at random, which looks like
  precision and is not.

## Revisit when

`reviewer metrics` shows the surviving findings are accepted at a rate that no longer justifies the
second call — either because the first pass has become good enough, or because the lane is not worth
running at all.
