<!-- SPDX-License-Identifier: MIT -->
# ADR-0022 — Cap taste-category confidence in code, not by prompt instruction

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-35
- **Supersedes:** —
- **Superseded by:** —

## Context

Two of the categories the model lane reports are matters of judgement rather than fact.
`arch/missing-abstraction` says a shape is repeated enough that naming it would be better;
`quality/sloppy` says work reads as unfinished. Both are frequently right and neither is checkable. A
reasonable engineer can disagree with either without being wrong.

Confidence is not a description in this system — it is a routing decision. Only `high` confidence goes
inline; everything else lands in one collapsed summary comment (ADR-0017). So the difference between
`high` and `medium` on a taste finding is the difference between interrupting someone on the line they
are reading and appearing in a list they open if they want to.

That makes "how confident is the model about its own taste" a question the model should not be
answering. And it is the question models are least reliable on: an instruction to self-limit competes
with every incentive in the response, and it degrades silently — a prompt edit six months from now,
made for an unrelated reason, can remove the sentence and nothing fails.

## Decision

Each category declares its own ceiling in code, in one table:

```go
{ID: "arch/missing-abstraction", Max: finding.ConfidenceMedium, ...}
{ID: "quality/sloppy",           Max: finding.ConfidenceMedium, ...}
{ID: "correctness/bug",          Max: finding.ConfidenceHigh,   ...}
```

Every finding passes through `Cap` before it is anything. A taste-category finding claiming `high`
becomes `medium`; it is not discarded, and nothing is said about it, because the model was not wrong to
believe it — it was wrong to be sure.

**The ceiling is not in the prompt.** A test asserts the prompt does not state it. That is deliberate:
anything in the prompt is a request, and anyone editing wording can weaken it without noticing. The
prompt describes the categories; the code decides what they may claim.

The same table closes the vocabulary. A finding whose rule id is not in it is discarded with a warning
rather than remapped, because acceptance is measured per rule id (ADR-0024) and a model inventing an id
per finding makes every bucket size one — which would quietly disable the measurement that decides
whether this lane earns its cost.

Two smaller rules follow the same principle of never rounding up: an unrecognised confidence becomes
`low` and an unrecognised severity becomes `info`, so a reply this code does not fully understand
cannot claim more than one it does. And the lane is stamped here rather than read from the reply — a
model claiming to be the deterministic lane would bypass the cap, the evidence requirement and the
invalidation pass in one field.

## Consequences

- **Positive:** taste findings can never interrupt a reviewer inline. That is a property of the
  program, provable by reading one function, rather than a behaviour that holds while a paragraph
  survives.
- **Positive:** the ceiling is where a person would look for it. "Why did this appear in the summary
  rather than inline" is answered by one table.
- **Positive:** the table is the prompt's category list too, so adding a category cannot leave the
  model unaware of it or leave a new category uncapped.
- **Negative:** a taste finding that genuinely is obvious is still capped, and reaches its reader one
  click later than it deserves. Accepted: the cost of that is a click, and the cost of being wrong the
  other way is a reviewer learning to scroll past inline comments.
- **Negative:** a closed vocabulary means a real problem in no category is discarded. Mitigated by the
  warning, which names the id the model wanted — so a category people keep reaching for is visible and
  can be added.
- **Neutral:** the cap is one-directional. Nothing promotes a finding, so a category's ceiling is a
  ceiling and never a floor.

## Alternatives rejected

- **Ask the model to self-limit in the prompt.** Unverifiable, and it drifts with every prompt edit —
  including edits made for unrelated reasons by people who never read this ADR.
- **Drop taste findings entirely.** They are the categories PLAN wanted most; a reviewer who has read a
  thousand pull requests is valuable precisely for the judgement a linter cannot encode.
- **Let confidence through and cap at the reporter instead.** Puts the rule in the presentation layer,
  where the next reporter added would have to reimplement it.
- **A single global ceiling for the model lane.** Would cap `correctness/bug` too, which is checkable
  against the diff and deserves to be seen where it happened.

## Revisit when

The acceptance rate for a taste category in `reviewer metrics` is high enough over a real sample that
its findings are being acted on as reliably as `correctness/bug` — at which point the ceiling is
costing more than it saves, and this ADR is superseded rather than edited.
