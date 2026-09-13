<!-- SPDX-License-Identifier: MIT -->
# ADR-0024 — Measure acceptance per rule, and delete rules on the evidence

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-42
- **Supersedes:** —
- **Superseded by:** —

## Context

A rule that fires often and is dismissed every time is worse than no rule at all. It costs a reviewer's
attention on every pull request, and — worse — it teaches them that this tool's comments are usually
noise, which is a judgement they then apply to the comments that are not.

That damage is cumulative and invisible. Nobody files a bug saying "rule 14 is wasting my time"; they
stop reading. By the time somebody notices, the tool has a reputation rather than a problem.

Judging it by hand does not work either. "Which rules are earning their keep" asked of a person is a
question about the last thing they remember being annoyed by, and the rules that quietly fire twice a
week on everybody's pull requests are precisely the ones nobody remembers.

## Decision

Acceptance is measured per `ruleId`, from GitHub's own record of what happened to each comment, and a
rule is deleted when the evidence says to.

Three buckets, which are not three shades of the same thing:

- **Resolved** — somebody read the finding and marked the thread dealt with.
- **Open** — somebody read it, or did not, and left it.
- **Outdated** — GitHub detached the comment because the code moved underneath it.

**Acceptance is resolved ÷ (resolved + open).** Outdated threads are counted, shown, and kept out of
the ratio: nobody decided anything about them, so they are evidence about a branch's velocity rather
than about a rule.

**A rule with nothing decided shows `n/a`, never 0%.** Reporting zero is how a rule that has simply not
had the chance yet gets deleted for being new — and the whole point of measuring is to delete the right
ones.

**The rule id travels in the comment marker**, not only in the comment's visible text. The visible text
is markdown a human can edit; a machine-readable field costs nothing and is the difference between a
measurement and a scrape. A comment from before the id was recorded still parses, dedupes, and is
counted under a visible `(rule not recorded)` row rather than dropped — so the totals add up and the
reason that row exists is legible.

**Only threads this tool started are counted.** A human quoting a marker in a reply does not make their
conversation ours to measure.

The table sorts worst-acceptance first, then by how much attention the rule has cost. It exists to
answer "what should I delete", and the answer should not need sorting by hand.

## Consequences

- **Positive:** deleting a rule becomes a decision with evidence behind it, which is the only kind
  anybody will actually make. "It feels noisy" loses arguments; "11 postings, 1 resolved" does not.
- **Positive:** the model lane's categories are a closed set for exactly this reason (ADR-0022), so a
  category that fires constantly is visible as a row rather than as a feeling.
- **Positive:** nothing new is stored. GitHub's thread state is the record, as it is for deduplication
  (ADR-0016), so there is no database to keep consistent and no second source of truth.
- **Negative:** "resolved" means somebody clicked resolve, which is not the same as "the finding was
  right". A team that resolves threads to clear the sidebar produces flattering numbers. Mitigated only
  by the numbers being per rule: the habit is uniform across rules, so the *ordering* survives it even
  when the absolute values do not.
- **Negative:** a finding nobody ever sees — one dropped by the diff filter, or suppressed as a
  duplicate — is not posted and so is never measured. The measurement covers the tool's output, not its
  analysis.
- **Negative, and the sharpest limit here:** only findings posted *inline* carry an outcome. Everything
  below high confidence goes into one collapsed summary comment (ADR-0017), which has no thread, so
  there is nothing to resolve and no record of whether anybody agreed. Those findings are counted in a
  `summarised` column and contribute to no ratio.

  That covers four of the model lane's eight categories — the ones ADR-0022 caps at `medium`, including
  both taste categories. For them this measurement answers "how often does it fire" and not "does
  anybody act on it", which is the more interesting half. Counting them at all is the fix for a worse
  version of the same problem: read from threads alone, a rule that fired two hundred times had no row
  at all and looked like one that never fires. Judging a capped category still needs a different
  signal, and this ADR does not provide one.
- **Negative:** a low-volume rule is statistically meaningless for months. Accepted: the table shows
  the counts beside the percentage so a reader can see when there is not enough to go on.
- **Neutral:** the window is a flag rather than all time, because a rule's acceptance a year ago says
  nothing about whether to keep it now.

## Alternatives rejected

- **Judge by hand, periodically.** The rules worth deleting are the ones nobody remembers.
- **Count postings only.** Volume without outcome is exactly backwards: the most-fired rule may be the
  most valuable one.
- **Count reactions, or ask for feedback.** Asks reviewers to do extra work to tell the tool it is
  wasting their time, which is a request nobody grants twice.
- **Store outcomes in a database.** A second source of truth to keep consistent with GitHub's, for a
  number that GitHub already knows (ADR-0016).
- **A single aggregate acceptance rate.** Tells you the tool is noisy without telling you which part,
  so nobody can act on it.

## Revisit when

Enough rules have been deleted on this evidence that the remaining ones are all above the threshold —
at which point the interesting question becomes which rules are *missing*, and this measurement cannot
answer it.
