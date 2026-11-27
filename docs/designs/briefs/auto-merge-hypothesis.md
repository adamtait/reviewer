<!-- SPDX-License-Identifier: MIT -->
# Risk-gated auto-merge: working hypothesis (brainstorm input)

* **Author:** adamtait
* **Date:** 2026-11-26
* **Status:** Input to a brainstorm review. Not a design.

## The ask

Build, in this repository (`reviewer`), an automated merge classifier and agent. It classifies
every pull request by risk and merges the pull request without human approval when risk is low
and confidence is high, against thresholds the team configures. It must be high precision: merge
only when confidence is high.

## Context from the requester

- About 10 engineers, 4 or more GitHub repositories, dozens of pull requests a week. Coding agents
  (mostly Antigravity, some Claude Code) write more than 90% of pull requests. Code is mostly
  TypeScript, with some Python, bash and Rust.
- **Problem, in priority order:** human review time (most expensive), agent wait time, stale pull
  requests. The requester expects about half of pull requests to qualify eventually.
- **Stakes:** `main` deploys to production. Risk is high. Recovery from a bad merge is a revert
  plus a production deploy. Some systems use feature flags, but changing a flag can also need a
  deploy, so flags do not give instant rollback.
- **Evidence:** the classifier may use any available evidence. Humans can supply labels if a
  labelling tool exists.
- **Authority:** the tool merges without human approval when risk is low and confidence is high (or
  whatever the configured values are). Classifier values must be visible on every pull request,
  not only on those it merges.
- **Shadow period:** accepted, but shorter than 4 weeks is preferred, especially if other human
  labels can make up for it.
- **Test suite:** the requester has low confidence in the existing test suites. They should be
  audited thoroughly.

## Constraints already in this repository

These ADRs bear directly on the idea (see `docs/adr/`):

- ADR-0009 never block a pull request; the tool has no authority, and any gate belongs in a team's
  own CI step reading the output. Auto-merge gives the tool authority in the opposite direction.
- ADR-0008 CLI, not a service; no state between runs. ADR-0016 GitHub comments are the store.
- ADR-0004 repository-specific values live in config in the repository under review;
  ADR-0018 the workflow runs on `pull_request`, so the pull request's own workflow and config are
  what run. A pull request could therefore edit its own thresholds or the workflow that merges it.
- ADR-0017 presentation gated on confidence; ADR-0022 taste categories capped at medium in code;
  ADR-0023 every model finding is disproved before it is emitted; ADR-0024 acceptance measured per
  rule from GitHub thread state.
- ADR-0029 to ADR-0034: the hosted review workspace (`cmd/workspace`) reads as the user with a
  read-only token and stores nothing; ADR-0034 a SCIP navigation index is built in CI on push.

## Working hypothesis

> **Superseded in part by the Round 2 hypothesis at the end of this file.** Item 5 (GitHub App
> merge actor) and the score-based approach in items 1 and 7 were replaced by a required
> `merge-gate` check, native auto-merge and a category-first allowlist.

1. **Classifier.** A new `reviewer risk` subcommand runs in the existing Action on every pull
   request. It posts a GitHub check run carrying two separate numbers and every signal behind them:
   - **Risk:** how bad the change could be if it is wrong.
   - **Confidence:** how much of the evidence needed to judge risk was actually present. Missing
     evidence (an analyzer that declined, CI not finished, tests that did not run) lowers
     confidence; it never counts as zero risk.
2. **Signals that raise risk.** Paths matched against a risk map (auth, payments, permissions,
   crypto, infrastructure as code, migrations, CI config, lockfiles); new or major-bumped
   dependencies; public API or schema changes; deleted, skipped or loosened tests; rewritten
   snapshots; reach (how many places reference the changed symbols, from the SCIP index); crossing
   package or CODEOWNERS boundaries; file revert and hotfix history; churn; author's past revert
   rate; agent plan not matching the diff; deploy freeze, off-hours, open incident; any
   deterministic-lane finding on changed lines; any surviving model-lane finding; a flaky test
   passing on retry.
3. **Test evidence.** Tests lower risk only when they show the changed lines were run and checked:
   changed-line coverage, pre-existing tests passing against the new code, mutation testing on the
   changed lines. New tests written in the same pull request by the same agent count for much less.
   A per-repository **test-trust factor**, measured by mutation testing on a sample of changed
   lines, scales how much credit green CI earns. The test-suite audit itself is a separate project.
4. **Author-controlled text only raises risk.** Pull request descriptions, commit messages, code
   comments and agent session logs are written by the party being judged and can carry prompt
   injection. The model lane may veto a merge and may never approve one.
5. **Merge actor.** A GitHub App identity limited to merging, whose private key is an Actions
   secret, used from a trusted workflow the pull request cannot edit (for example `workflow_run`).
   It reads policy from the base branch only, re-checks that the head commit has not changed since
   classification, and merges through the merge queue. No hosted service.
6. **Policy.** Thresholds are configurable above a fixed floor that no setting can lower. Pull
   requests that touch the policy, workflows, CODEOWNERS or rulesets never qualify. Changes that
   cannot be reverted cleanly (database migrations, data backfills, external API contract changes,
   deletion of persisted state) never qualify, because recovery is revert plus deploy.
7. **Rollout and measurement.** A shadow period scores every pull request and merges none. It is
   shortened by (a) backfilling a year of merged pull request history, labelled by outcome
   (reverted, hotfixed in the same lines within N days, or clean), and (b) reviewer labels collected
   in the review workspace ("I would have merged this as-is" / "I would have asked for changes").
   Merging is then enabled one category at a time. Statistical note: zero failures in n qualifying
   pull requests bounds the failure rate below about 3/n at 95% confidence, so demonstrating under 1%
   needs about 300 clean cases. Backfilled history understates unreviewed failure rates because
   humans caught problems before merge.

## Open questions for the brainstorm

- Should this be built in `reviewer` at all, given ADR-0009 and ADR-0008, or as a separate consumer
  of `reviewer`'s output?
- Is an off-the-shelf tool (GitHub merge queue rules, Mergify, Kodiak, Renovate automerge, Graphite,
  etc.) enough for most of the low-risk categories?
- How is the score computed at the start with no training data: hand-weighted rules, a learned
  model, or a rule-based allowlist of categories?
- How does a merged pull request's outcome get recorded so precision can be measured over time?

## Round 2 hypothesis (2026-11-26), after the first brainstorm review

The first review's brief is `auto-merge-concepts_brief.md` in this folder. Read its
"Iteration 2" section under Key Decisions. The author decided:

- **No GitHub App by default.** The author understands organization policy disallows Apps
  (unconfirmed for about a week). No machine-user token either.
- **Category-first.** A deny-by-default category allowlist with vetoes. The first wave will be
  far smaller than half of pull requests, and that is accepted. Scoring comes later.
- **Built in this repository.** The merge decision code lives in `reviewer`, kept apart from the
  review path (separate subcommand, workflow and permissions).

Revised mechanism to stress-test:

1. The `main` ruleset requires 0 approvals and a required status check `merge-gate`, plus
   existing CI checks.
2. `merge-gate` passes when a human with write access approved the current head commit, OR the
   pull request fits a qualifying category under base-branch policy, computed from inputs the pull
   request cannot control. Otherwise it stays pending with the reason.
3. GitHub's built-in auto-merge does the merging. The author or agent enables it
   (`gh pr merge --auto`). The merge is attributed to them, so pushes to `main` trigger deploys
   (a merge made with `GITHUB_TOKEN` would not trigger other workflows).
4. `merge-gate` must be produced by code the pull request cannot edit: a ruleset required
   workflow pinned to a protected ref, or `pull_request_target` / `workflow_run` running base-branch
   code only and treating the diff as data.
5. It re-runs on `pull_request_review` so a human approval clears it.
6. `reviewer risk` stays advisory on every pull request (neutral check plus label).

Known concerns: the check replaces GitHub's approval rule, so a bug that passes wrongly merges
unreviewed code; failing closed can block approved pull requests (tension with ADR-0009; needs
break-glass); ruleset changes may need an org admin; whoever enables auto-merge carries
accountability, including when agents use a person's token.
