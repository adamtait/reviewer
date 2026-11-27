<!-- SPDX-License-Identifier: MIT -->
# Risk-gated auto-merge
* **Author:** adamtait
* **Date:** 2026-11-26
* **Status:** Brainstorm brief, round 2 (the required-check route), narrowed for v1 by the author
  on 2026-11-27. Not a design.

## Overview

This brief collects two seven-lens brainstorms of the working hypothesis in
`docs/designs/briefs/auto-merge-hypothesis.md`. Round 1 reviewed a design built around a
merge-only GitHub App. The author then made three decisions (see "Iteration 2" under Key
Decisions): no App by default, category-first, and built in this repository. Round 2 reviewed the
design that follows from those decisions. This version of the brief describes the round-2 design.
Round-1 material that no longer applies is listed in "Round-1 objections: what round 2 changed".

**v1 scope (Iteration 3, 2026-11-27).** The author narrowed the first version to what needs no
organisation-level change: the advisory `reviewer risk` output, a docs-only path delivered by
native CODEOWNERS and repository settings if a sandbox test shows it works, and measurement. The
`merge-gate` route described below is deferred until the App or required-workflow question is
settled with an org owner. See "Iteration 3" under Key Decisions.

**The round-2 design in one paragraph.** The ruleset on `main` requires 0 approvals, the existing
CI checks, and one more required check, `merge-gate`. `merge-gate` passes when either a human with
write access approved the current head commit, or the pull request fits a deny-by-default category
under policy read from the base branch. Otherwise it stays pending and gives the reason. Nothing in
this system merges. GitHub's built-in auto-merge merges, acting as whoever enabled it (the author,
or an agent using the author's token), so the push to `main` triggers deploys as usual.
`merge-gate` must be produced by code the pull request cannot edit. `reviewer risk` is a separate,
advisory output (a neutral check or comment plus a label) and has no input into `merge-gate`.

**What changed since round 1.**

- There is no App, no private key and no merge actor. The decision moved from an actor that merges
  to a check that GitHub enforces.
- Round-1 Blocker 4 (how a merge without human approval satisfies branch protection) is answered:
  0 required approvals plus `merge-gate`.
- `merge-gate` is now the only approval rule on `main`, for every pull request, human-approved or
  not. It is the security boundary. "Nothing in this repository performs a merge" is true, but the
  authority has moved into the check; it has not gone away.

**Where the round-2 lenses agree.**

1. **A check matched only by its name can probably be forged.** Without an App, every workflow
   posts statuses and check runs under the same GitHub Actions identity, and a person's token can
   post a commit status directly. A pull request from a same-repository branch, which is where agent
   pull requests live, can therefore post `merge-gate = success` itself. The gate's own logic never
   runs. This applies to the `pull_request_target` and `workflow_run` candidates, not only to plain
   `pull_request`. The only candidate that plausibly resists it is a ruleset "required workflow"
   pinned to a workflow file at a protected ref. (All seven lenses; GitHub behaviour unverified.)
2. **The trigger plan does not hold as written.** A workflow triggered by `pull_request_review`
   probably runs the pull request's own copy of the workflow file, and ruleset required workflows may
   not run on review events. A human approval may therefore not clear the gate without an extra
   mechanism. (Six lenses; unverified.)
3. **A status belongs to a commit SHA, and a pass does not expire.** A success can carry across
   pull requests and base branches that share the head SHA, survive a policy change or a kill
   switch, and outlive the moment it was computed. The gate must be a pure function of one head SHA,
   one base, one policy commit and approvals bound to that SHA, and something must revoke passes
   when policy changes. (design, readiness, comprehension, structural, catalyst.)
4. **Setting approvals to 0 removes more than the approval count.** GitHub probably stops
   enforcing "changes requested" blocks, stale-approval dismissal, last-pusher exclusion and
   possibly code-owner review. The gate must reproduce each of these or the design must say they
   are dropped. An agent using a person's token cannot be told apart from that person. (All lenses
   except zoom-out, which raises it as a cost.)
5. **Roll out in an order that never weakens the human path.** Run the gate first as a non-required
   shadow, then as a required check alongside the native approval rule, and only then remove the
   approval rule. Removing the approval rule first would let every pull request with auto-merge on
   and green CI merge unreviewed at that moment. (readiness, catalyst, zoom-out, structural,
   comprehension.)

**What every claim here depends on.** No lens could reach the web. Every statement in this brief
about how GitHub behaves is **unverified** unless it cites a file in this repository. "Sandbox
experiments" under Key Decisions lists what must be tested, in order, before any design work
depends on it.

## Analysis

### The core contradiction

The requester wants more pull requests merged without a human. `main` deploys to production, and
recovery is a revert plus a deploy. Round 1 turned "never merge a bad pull request" into a bounded
product of the rate of bad merges, the blast radius of each and the time to recover. That still
holds.

Round 2 adds a second pair of failure modes, because `merge-gate` now sits in front of every merge:

- **The gate passes wrongly.** Unreviewed code reaches production. Causes: a forged status, a stale
  or misattributed approval, a pass that outlived its policy, or a predicate bug.
- **The gate blocks wrongly.** Approved pull requests, hotfixes included, cannot merge. Causes: the
  gate failing to run, a missed trigger, a cancelled run, or an API error. This is the
  "disable it to ship" reflex ADR-0009 warns about (ADR-0009:16-18).

The design has to make the first impossible by construction and the second recoverable without
removing review. (catalyst, design)

### Resolution

The lenses propose these resolutions. Each GitHub mechanism named here is unverified.

1. **Bind the gate's identity to a path, not a name (extraction).** Make the ruleset "require
   workflows to pass" rule the only producer of `merge-gate`. Pin it by commit SHA to a workflow
   file in a repository that pull request authors, and the agents using their tokens, cannot write
   to. Never also add `merge-gate` as a name-matched required check. As a second layer, give agents a
   fine-grained token with no "Workflows" permission, so they cannot push a spoofing workflow at
   all. (catalyst, design, readiness, zoom-out)
2. **Triggers wake the gate; they are never inputs to it (the "doorbell").** Any event may cause a
   re-evaluation. The gate ignores the event payload, looks the pull request up through the API,
   and recomputes everything. A forged or replayed trigger then causes only a redundant evaluation.
   A scheduled sweep is the backstop for missed events. (catalyst, design, readiness)
3. **The status is a pure function of one SHA (SHA-pure verdict).** The verdict depends only on the
   head SHA S, the base SHA, the policy commit P, and approvals whose `commit_id` equals S. Build
   the diff from SHA-addressed sources, post only to S, re-check head and base before posting, and
   post success only when the pull request's base is the protected branch. A push creates a new SHA
   with no `merge-gate` result, so nothing merges until the gate runs again. (design, catalyst)
4. **Approval first, category second.** Evaluate the human path first, in a small function with no
   configuration, using the reviews API, a permission lookup and the head SHA. Any error on the
   category side means "not eligible", never "gate error". Only a failure to read approvals blocks.
   Gate failures then withhold only the new privilege; they never remove the existing one.
   (catalyst, design)
5. **Prove parity before replacing the native rule (preliminary action).** Run the gate while the
   ruleset still requires one native approval. Every merge GitHub allows must be one the human
   branch passed; every pull request GitHub blocks for lack of review must be one it did not pass.
   Remove the native rule only after an agreed period with zero disagreements. (catalyst,
   readiness, zoom-out, structural)
6. **Revoke passes when the world changes (separation in time).** Either require branches to be up
   to date, so any change to `main` (including a policy change) forces a new head SHA and a fresh
   evaluation; or run a sweep on every policy, kill-switch or breaker change that posts pending and
   disables auto-merge on open pull requests. Make the category branch the last check to turn green
   so time-dependent conditions are read close to merge time. (catalyst, design, structural)
7. **Break glass skips only the gate (segmentation of rulesets).** Put `merge-gate` in its own
   ruleset with a small bypass team in "pull requests only" mode. Keep CI and other rules in a
   ruleset with no bypass beyond org owners. Keep a disabled standby ruleset that requires one
   approval, so an outage falls back to native review rather than to none. A job on every push to
   `main` lists merges whose head has no successful run of the gate workflow (looked up by workflow
   file ID) and opens an issue. That job also detects forgeries after the fact. (catalyst, design,
   readiness, structural)
8. **Keep sensitive paths behind GitHub's own rules (segmentation by path).** If code-owner review
   still works with 0 approvals (unverified), own the whole risk map in CODEOWNERS (auth, payments,
   migrations, infrastructure, `.github/**`, `CODEOWNERS`, `.review/**`, policy files, agent
   instruction files, build and test config). A gate bug can then merge only unowned paths. Path-only
   categories such as docs-only may need no gate code at all. (catalyst, zoom-out)

### Ideal Final Result

`merge-gate` is a pure function of five inputs: the head SHA, the base SHA, the base-branch policy,
GitHub's current review records, and the kill switch. It is produced by a workflow whose identity is
a protected path, not a name, and it is evaluated as late as possible before the merge.

- GitHub's own CODEOWNERS rule still guards every sensitive path.
- The human branch is small, has no configuration, and is shown equivalent to the native rule
  before it replaces it.
- Gate failures withdraw only the category privilege. Break glass skips only the gate, and every
  use is audited.
- Every merge records the authority that allowed it: the approver, or the category and the policy
  commit. The person who enabled auto-merge is recorded as the conduit, not as the decider.
- `reviewer` review output stays advisory and exits 0. `reviewer risk` is advisory and independent
  of the gate.

### Suggested patterns

- **SHA-pure verdict** (Resolution 3). (design, catalyst)
- **Triggers are wake-ups, never inputs** (Resolution 2). (catalyst, design, readiness)
- **Approval first, category second; category errors mean ineligible** (Resolution 4). (catalyst,
  design)
- **Success only for pull requests into the protected base.** Re-evaluate on base `edited`.
  (design, readiness)
- **Explicit success only.** A required result passes only on an explicit pass for the same SHA.
  Cancelled, skipped or errored means not passed. This repository already applies the rule in its
  aggregator job (`.github/workflows/ci.yml:250-271`, comment at 257-259). (codebase, catalyst,
  readiness)
- **Revocation sweep** on policy change, kill switch, breaker and optionally every push to `main`,
  limited to pull requests with auto-merge enabled and a current success. (design, catalyst,
  structural)
- **Split rulesets plus a standby approvals ruleset** (Resolution 7). (catalyst, design, readiness)
- **Post-merge audit by workflow file ID** (Resolution 7). (design, catalyst)
- **Content-based predicates, never identity-based.** Labels, titles, branch names and author
  logins are controlled by the author. (design)
- **Pure decision package plus boundary tests.** A pure `Decide(Inputs) Verdict` with typed reason
  codes, guarded by an import test modelled on `internal/workspace/rules_test.go:20-197`: the gate
  imports no `os/exec`, plugin host, model lane or config-loading packages, and the review command
  does not import the gate. (codebase, zoom-out, structural)
- **Kill switch in tiers.** (i) A repository variable read at every evaluation, which only admins
  can change, plus the sweep. (ii) Change the gate ruleset to require one approval, which GitHub
  enforces at merge time. (iii) Turn off "Allow auto-merge". "Off" means approval-only, never "pass
  everything". (catalyst, readiness, structural)
- **Visible-state table with stable reason codes**, so agents polling `gh pr checks` can tell
  "waiting for approval" from "gate error". (comprehension, readiness, codebase)
- **Conformance suite for GitHub behaviour.** Turn the sandbox experiments into a repeatable script
  run on a schedule and before every gate pin bump, because GitHub's semantics can change.
  (structural)
- Round-1 patterns that still apply: monotone signals, deny-by-default predicates with evidence
  states, a signal registry with provenance, floor in code with config that can only tighten,
  implicit labels from review actions, backfill at the first-review revision, known-bad corpus,
  per-category precision report, advisory risk label. Round-1 patterns that no longer apply are
  listed under "Round-1 objections: what round 2 changed".

### Secondary trade-offs

- **Strict SHA binding vs. approval churn.** Every "Update branch" creates a new head and voids the
  approval. That is stricter than today and will create pressure to loosen the check. If it is
  relaxed later, compare patch IDs of the merge-base-to-head diff, with adversarial tests; never use
  a time window. (design, readiness)
- **"Require up to date" vs. agent wait time.** It classifies exactly the tree that lands and makes
  policy changes invalidate passes natively, but it serialises merges and re-runs CI on each update.
  It also gives one auto-merge per deploy. (catalyst, design)
- **CODEOWNERS curation becomes a security task.** If sensitive paths are kept native, forgetting
  to own a path widens what the category path can merge. (catalyst)
- **Doorbell plumbing vs. simplicity.** More workflow files and seconds to minutes of latency per
  review, plus `actions: write` if the gate re-runs the pinned workflow. (catalyst, design)
- **Sweep vs. rate limit.** About 8 to 15 API calls per evaluation. A sweep over many open pull
  requests can approach the `GITHUB_TOKEN` hourly limit (unverified figure), and rate-limit 403s are
  hard errors with no retry (`internal/ghclient/client.go:405-406`). On a busy day that blocks human
  merges. (design)
- **Agent token without workflow permission vs. agent capability.** Agents can no longer edit CI;
  those pull requests go to humans, which the never-qualify list wants anyway. (catalyst)
- **Release captains vs. self-merging.** Bypass holders can merge their own unreviewed pull
  requests through the gate ruleset. Keep the team small, audit every use, and keep agents off their
  tokens. (catalyst, design)
- **Policy owners as the accountable party.** Recording accountability at the policy needs named
  policy owners and a post-incident process aimed at the policy, not the person. (catalyst,
  comprehension)
- **Pin and release coupling.** A gate fix needs a release of both artifacts (ADR-0026:21-26) and a
  pin bump in every repository's gate workflow or ruleset. One org-level pin avoids drift.
  (structural)

### Points where lenses disagree

- **One command or two.**
  - zoom-out: a separate `cmd/` main package in this repository, because `cmd/reviewer` maps every
    non-usage error to exit 0 (`cmd/reviewer/main.go:53-54`) and ADR-0009:26 says the binary exits 0.
  - codebase: a subcommand of `cmd/reviewer` is cheapest, because only `./cmd/reviewer` is released
    (`.goreleaser.yaml:13-16`); a second main package adds a release artifact (ADR-0026). It needs
    its own exit discipline and boundary tests. Both agree that separate binaries do not isolate
    tokens; GitHub-side pinning does.
- **First release.**
  - readiness: required gate in approval-only mode while native approvals stay required.
  - zoom-out: not required at first; a shadow status with a different name (`merge-gate (shadow)`).
    A required approval-only gate is not "zero new risk": it swaps a rule GitHub tests for our copy
    and adds a blocking failure mode.
  - catalyst and structural combine both: shadow under the native rule, then parity, then cutover.
    The staging table below follows the combination.
- **Whether `merge-gate` is needed for the first category at all.**
  - zoom-out and catalyst: docs-only by directory may be done natively with CODEOWNERS (unowned docs
    paths), 0 approvals and "require review from Code Owners", with no code.
  - readiness, design and comprehension: code-owner enforcement may stop working at 0 approvals
    (unverified), and with 0 approvals plus a gate, round-1 stage S1 no longer merges anything
    without the gate's code. The sandbox decides.
- **Job conclusion or explicit status.**
  - codebase: under a required workflow, the job's conclusion can be the verdict, so the gate needs
    no write call and `internal/ghclient/write.go:14-16` stays literally true.
  - design: checks from `pull_request_target` and `workflow_run` jobs attach to the base or default
    branch SHA (unverified), so the gate must post a commit status explicitly to the head SHA.
  - readiness: a job cannot end "pending". Failing the job may disable auto-merge (unverified);
    posting a separate pending status brings back name matching.
- **Post pending, or post nothing.** zoom-out: post only success or nothing, since a missing
  required check holds the merge. catalyst, comprehension and design: post pending with a reason on
  every head, otherwise GitHub shows "Expected - waiting" and agents cannot learn why.
- **Which ADR round 2 contradicts.** codebase: posting a status is none of "merge, approve, close or
  push" (ADR-0018:32-33), so ADR-0018 holds unless `pull_request_target` is used; ADR-0009 is the
  direct conflict. structural: supersede both, because ADR-0018 fixes the permissions at
  `contents: read` and `pull-requests: write`, and the gate stands in for approval.
- **Whether `reviewer risk` belongs in the first release.** zoom-out: defer it; it is not on the
  merge path and should be scheduled on its own value (ordering the review queue). The author's
  round-2 hypothesis keeps it advisory on every pull request; the staging table treats it as a
  parallel track.
- **Whether the installer writes the gate workflow.** codebase: copy the index-workflow pattern
  (`internal/installer/plan.go:125-134`). readiness: never write the gate workflow into the
  repository it guards, because a pull request can edit it; `reviewer init` only prints the ruleset
  steps (`internal/installer/write.go:247-258`).
- **New brief or in-place edit.** structural recommends freezing the round-1 brief and writing
  round 2 as a new file. This brief was updated in place at the author's request; the author's
  Iteration 2 text and every "Rejected by author" row are kept unchanged.

### Round-2 objections, by severity

Lens tags: design = egm-design, codebase = egm-codebase-details, zoom-out = egm-zoom-out,
comprehension = egm-comprehension, readiness = egm-implementation-readiness,
structural = egm-structural, catalyst = egm-inventive-catalyst. "Unverified" marks GitHub
behaviour no lens could check.

#### Blocker

1. **`merge-gate` can be forged by anyone with write access unless a pinned required workflow
   produces it.** [design, codebase, zoom-out, comprehension, readiness, structural, catalyst]
   - Required checks are matched by name. This repository already relies on that: the required
     check `ci` is a job name (`.github/workflows/ci.yml:246-254`).
   - Forgery routes (all unverified): a pull request adds a workflow with a job named `merge-gate`;
     or a workflow with `statuses: write` posts a `merge-gate` commit status; or a workflow on a
     scratch branch posts success onto another pull request's head SHA, so the target pull request
     never touches `.github/`; or a person's token, which agents hold, POSTs `/statuses` directly.
   - Restricting the check's source to GitHub Actions blocks only the token route. The real gate
     posts under the same Actions identity as every forger.
   - The never-qualify list cannot help, because the forged status never asks the gate.
   - The same forgery applies to the required `ci` check, which today is backed by a human reading
     the diff. At 0 approvals, it is backed only by the gate's never-qualify list (readiness M9).
   - Requirement: strike `pull_request_target` and `workflow_run` as producers in route item 4.
     Make the pinned required workflow the only producer, or state that the gate cannot resist
     write-access holders. First sandbox tests: a job named `merge-gate`, a posted status, a
     token-posted status, a status and a check run with the same name.
2. **The required-workflow rule may not be available, or may need an org owner.** [readiness,
   zoom-out, design]
   - Unverified: it may exist only in org-level rulesets, may need GitHub Enterprise Cloud for
     private repositories, and may be editable only by org owners. That is the same approval channel
     as the App question.
   - If it is unavailable, no remaining producer resists Blocker 1. The fallbacks are native
     CODEOWNERS configuration only, or reopening D1.
   - Requirement: run the sandbox in the production organisation or on the same plan, with a
     private repository and three accounts (writer who opens, writer who approves, read-only).
3. **Re-running on `pull_request_review` breaks the "base-branch code only" rule, or does not
   happen.** [design, zoom-out, comprehension, readiness, structural, catalyst, codebase]
   - Unverified: `pull_request_review` runs the workflow file from the pull request's merge ref, as
     `pull_request` does; there is no base-branch variant; and required workflows may run only on
     `pull_request`-type events and `merge_group`.
   - If so, route item 5 contradicts item 4, and a human-approved pull request stays pending until
     the next push.
   - Requirement: use review events only as an untrusted doorbell (Resolution 2). For each
     producer, list which event triggers the first run, the post-review run, the post-push run and
     the policy-change run, and that each runs base-branch code. Re-running the pinned workflow from
     a trusted job needs `actions: write` (unverified).
4. **A pass is bound to a commit, not to a pull request, a base or a policy.** [design, readiness,
   comprehension, structural, catalyst]
   - Unverified: a commit status attaches to the SHA, so a success posted while evaluating a pull
     request into `scratch` also counts for the same SHA into `main`. Changing a pull request's
     base may keep its approvals.
   - Attack (unverified): push a branch `evil` whose `pull_request_target` workflow posts success,
     open a pull request from head X into `evil`, then open X into `main`. This also contradicts
     ADR-0018:59 ("Never, in the direction of `pull_request_target`").
   - A pass also persists after a policy tightens, a category is suspended, the kill switch flips or
     a freeze starts. Auto-merge fires within seconds of the last check turning green. Re-running an
     old gate run probably runs the old base commit's code and policy (unverified).
   - Requirement: post success only when the base is the protected branch; re-evaluate on `edited`;
     ignore approvals before the latest base change; read policy from the live base head through the
     API; record the policy SHA in the verdict; add the revocation sweep or "require up to date"
     (Resolution 6). Define in the design what a success is bound to and every event that resets it.
5. **The gate can post success on a SHA it did not evaluate.** [design]
   - `ListFiles` calls `/pulls/{n}/files` (`internal/ghclient/client.go:271-273`), which returns the
     current head's files, not those of a given SHA.
   - Attack: push malicious S, then quickly push docs-only Y. The run for S reads head S, lists Y's
     files and posts success on S. A force-push back to S makes it head again, and auto-merge can
     fire before the next run posts pending.
   - Requirement: build the diff from SHA-addressed sources only (compare `base_sha...S`, or fetch S
     by SHA and diff with `--no-ext-diff --no-textconv`), post with `sha=S`, and re-check head and
     base before posting.
6. **`cmd/reviewer` fails open, so a gate built on it passes on errors.** [codebase, zoom-out]
   - `cmd/reviewer/main.go:53-54` exits 0 for every error that is not a usage error or a failed
     check; `cmd/reviewer/flags.go:92` and `main.go:3-8` promise the binary never fails a build.
   - If the job conclusion is the verdict, any API error, rate-limit 403 or crash passes the gate.
   - Requirement: success only on an explicit positive verdict; every other path non-zero or an
     explicit non-success post; a test that walks every error path. The `errCheckFailed` exit 1 at
     `flags.go:230-239` is the precedent.
7. **The human-approval rule is undefined, and setting approvals to 0 drops native protections.**
   [comprehension, readiness, design, catalyst, codebase, structural]
   - Unverified: at 0 required approvals GitHub stops enforcing a writer's "changes requested",
     "dismiss stale approvals", "require approval of the most recent reviewable push" and possibly
     code-owner review.
   - "A human with write access" is not defined. `author_association` is not the collaborator
     permission level. Bob's agent, holding Bob's token, can approve Alice's pull request, and two
     agents can approve each other. Bob can push to Alice's branch and then approve.
   - Requirement, decided before any gate code (decision D-E):
     - count only the reviewer's latest review, with `state == APPROVED` and `commit_id == head`;
     - any outstanding "changes requested" from a writer blocks both paths; re-evaluate on review
       `dismissed` and `edited`;
     - reject `type == Bot` and a base-branch deny-list of service and agent accounts;
     - check write permission at evaluation time;
     - exclude the author and anyone who pushed to the pull request; take the pusher from a trusted
       source such as the `synchronize` sender, never from Git author fields;
     - reproduce CODEOWNERS for owned paths, or record that it is dropped;
     - state the residual risk: an agent with a person's token is that person. The native rule has
       the same weakness today. Add a "never approve" rule to the agent skill
       (`internal/installer/templates/SKILL.md.tmpl`) and agent-tool deny rules, and flag approvals
       within seconds of a push in the audit.
8. **Removing the approval rule before `merge-gate` is required merges open pull requests
   unreviewed.** [readiness; catalyst, zoom-out, structural, comprehension on rollout order]
   - Agents already enable auto-merge on open. At the instant the approval rule goes, every such
     pull request with green CI merges.
   - Requirement: roll forward as shadow, then required alongside native approvals with categories
     off, then remove approvals, then enable categories one at a time. Roll back in reverse order:
     re-add approvals first. Write both as a runbook with a named org owner.
9. **"Pending with the reason" is not a GitHub state the gate can produce cleanly, and a gate error
   looks the same as waiting.** [comprehension, readiness, codebase]
   - A workflow job ends as success or failure, not pending. A red required check may disable
     auto-merge (unverified). A gate that fails to start leaves "Expected - waiting" with no reason,
     which this repository has already seen (`.github/workflows/ci.yml:261-264`, a malformed
     expression ran no job). Agents polling `gh pr checks` read pending as "still running" and may
     "fix" a pull request that is only waiting for approval.
   - Requirement: three distinguishable outcomes (pass, waiting with a reason code, gate error with
     a run link and break-glass instructions) and a visible-state table covering: approved head;
     approval on an older commit; qualifies by category; neither; changes requested; gate failed;
     kill switch on; fork.
10. **Category predicates built on `internal/diff` as it stands would pass dangerous pull
    requests.** [codebase; Blocker for the category path only]
    - Renames hide the source path: `PatchedFile` has no previous path
      (`internal/diff/diff.go:208-212`), `cmd/reviewer/run.go:229-232` drops `PreviousFilename`, and
      `FromPatches` overwrites `Path` (`diff.go:198-201`). A rename out of `.github/workflows/`
      shows only its new path.
    - Deletion-only hunks add no range (`internal/diff/parse.go:106-110`) and line text is not kept
      (`parse.go:44-48`), so "every changed range is a comment" is vacuously true for a pull request
      that only deletes code.
    - A missing patch becomes `Binary: true` (`diff.go:184-189`), though GitHub also omits patches
      for large text files. `copied` maps to modified (`diff.go:214-225`). No file mode is carried.
    - Requirement: test both `filename` and `previous_filename` from `ghclient.ChangedFile`
      (`internal/ghclient/client.go:117-119`); read both sides of the patch or compare blobs; treat
      a missing patch, truncation (`client.go:56-59`) or an unknown mode as not eligible.
11. **This brief contradicted itself after Iteration 2.** [comprehension, structural]
    - Round-1 D1 to D8 repeated the settled decisions with opposite answers (D2 "favoured" a
      separate binary; D1 called native auto-merge a fallback; D3 asked "merger or approver App";
      D6 assumed App-authored markers). The staging table's S0 exit was "the App installed".
    - Addressed in this revision: the round-1 list is replaced by a status-tagged decision table,
      and the staging table is rewritten. The hypothesis file still reads App-first in items 5 and 7;
      a "superseded by Round 2" note there would help (not changed here).

#### Major

12. **Fail-closed conflicts with ADR-0009, and break glass is not designed.** [design, catalyst,
    readiness, structural, comprehension, zoom-out]
    - ADR-0009:26-34 says the binary exits 0 and no option makes findings blocking; ADR-0009:56-58
      puts any gate in "that team's own CI step". The gate now blocks human-approved pull requests
      when it fails to run.
    - The obvious recovery, removing `merge-gate` from the ruleset, leaves `main` with zero
      required review. A combined-ruleset bypass also skips CI (unverified).
    - Requirement: Resolution 7 (split rulesets, pull-requests-only bypass, standby approvals
      ruleset, audit job), Resolution 4 (approval first), and a kill switch whose "off" means
      approval-only. ADR-0013:37-38 already makes the secrets analyzer fail closed, which is the
      precedent for a gate ADR. Write break glass and the kill switch as short runbooks: trigger,
      actor, scope, audit, expiry, and the time until the category path stops.
13. **The ADR plan cannot use "amendment".** [structural, codebase, readiness]
    - `tools/checkadrs` allows only Accepted, Proposed, Rejected or Superseded
      (`tools/checkadrs/main.go:27-29`), parses only `Supersedes` and `Superseded by`
      (`main.go:139-140`), and freezes an Accepted body (`main.go:254-279`). The Iteration 2 phrase
      "ADR-0018 still needs a recorded amendment" cannot be carried out as written.
    - Options: a new ADR that supersedes and restates what survives, or a new ADR that departs from
      an existing one for a named scope (the ADR-0031/ADR-0012 pattern, `review-workspace.md:505`).
    - Proposed set (structural; highest existing is 0034, `docs/adr/README.md:51`):
      - 0035: review on `pull_request`; decide merge eligibility only in a workflow the pull request
        cannot edit (supersedes 0018 if the permission sentence or `pull_request_target` is
        affected; see "Points where lenses disagree");
      - 0036: replace the approval rule with a required `merge-gate`; native auto-merge merges;
      - 0037: never block a review; block a merge only through the ruleset-required gate
        (supersedes 0009, or scopes it);
      - 0038: the gate fails closed; review analyzers still fail open (departs from 0013);
      - 0039: gate policy from the base branch; floor in code; config only tightens;
      - 0040: deny-by-default categories with typed signals and evidence states;
      - 0041: only evidence computed without running pull request code counts toward
        qualification;
      - 0042: gate decisions, outcomes and suspensions stored in GitHub (supersedes or scopes 0016).
    - Timing: the supersessions of 0018 and 0009 must be recorded no later than the change that
      makes `merge-gate` required. Shadow stages can stay Proposed. ADR-0008 is no longer
      contradicted (no App, no service). Old review reports reuse "ADR-0035" and "ADR 0036" for
      renumbered workspace drafts (`docs/designs/reviews/round-2/critic_report.md:163`,
      `round-3/critic_report.md:83`); say so.
14. **Accountability sits with the wrong person.** [comprehension, catalyst, readiness, design]
    - The documents name "the author", "the author or agent" and "whoever enabled auto-merge". An
      agent enables auto-merge at open; the merge may happen hours later with no human seeing it.
      Approving now means merging immediately, which reviewers should be told.
    - Requirement: record authority where authorization happens. Human path: the approver for
      content, the enabler for timing. Category path: the base-branch policy commit and its
      approvers, plus the category. A trusted post-merge job records `merged_by`, the enabler, the
      verdict and its basis, and the gate version.
15. **Auto-merge behaviour the route depends on is unverified.** [readiness, design, catalyst]
    - Who the merge is attributed to and whether the push triggers deploy when the last green check
      came from `GITHUB_TOKEN`; whether auto-merge enabled with `GITHUB_TOKEN` triggers deploy
      (probably not, so agents must enable it with a human's token); whether auto-merge survives
      pushes, a red required check, the enabler losing access, or turning off "Allow auto-merge";
      whether "Update branch" changes the review's `commit_id`.
    - If deploy does not fire, the hypothesis's argument for native auto-merge (hypothesis lines
      121-123) falls.
16. **Merge queue: the group commit was never classified.** [design, catalyst, readiness,
    comprehension]
    - Unverified: required checks must report on the `merge_group` commit, and `merge_group`
      workflows run the group commit's copy of the workflow, which includes the pull request's
      edits. `.github/workflows/ci.yml:3-6` has no `merge_group` trigger.
    - Requirement: no queue in v1; use "require up to date" instead. If a queue is needed later, the
      pinned required workflow must handle `merge_group`, re-derive each pull request in the group
      by SHA, and force group size 1 for category pull requests.
17. **Policy must not live in `.review/config.yaml` or be read from the head.** [codebase,
    readiness, zoom-out, design]
    - `KnownFields(true)` (`internal/config/load.go:70-73`) means a new policy key makes older
      pinned `reviewer` binaries reject the file and silently skip review.
    - `Resolve` reads the working tree (`load.go:41`), and `github.apiBaseUrl` can redirect the token
      (`internal/config/config.go:104-114`). The gate needs the API URL and repository before it can
      read base config, from environment inputs no Go code reads today.
    - Requirement: a separate, versioned policy file with its own strict decoder, read from the base
      SHA through the API; add `config.Decode(raw)` or `ResolveFS(fs.FS)` to keep ADR-0004's single
      channel. For v1 docs-only, zoom-out suggests the policy is a constant in the pinned gate code.
18. **The gate's own supply chain becomes a merge path.** [design, codebase, readiness]
    - The installed workflow pins the action by tag (`internal/installer/templates/review.yml.tmpl:42`),
      and the checksum step lives inside that movable composite action (`action.yml:53-73`). The
      composite action is review-only: it needs `github.base_ref` and passes `--pr` from
      `pull_request` (`action.yml:41-51`, `84-89`).
    - Requirement: a separate gate action pinned by commit SHA; keep the binary checksum; never
      build the gate from the pull request, as `self-review.yml:37-51` does for review; list the
      external repository as a trust root.
19. **`ghclient` lacks the reads the gate needs.** [codebase, zoom-out, readiness]
    - Missing: reviews with `commit_id`, state and user type; collaborator permission per reviewer;
      head and base repository identity (`Ref` holds only ref and SHA, `client.go:73-76`, so "forks
      never qualify" cannot be computed); one file at a ref; listing statuses and check runs;
      rulesets; retry with backoff.
    - Reusable as they are: `REST.do`, pagination, `GetPullRequest`, `ListFiles`, `Compare`,
      `Archive`, `ListWorkflowRuns` (`client.go:219-327,360-424`; `stream.go:116-192`). Add the new
      reads as `*REST` methods, following `GetRepo` (`client.go:210-217`), so the review path's
      `Client` interface is unchanged. Keep `Writer` (`write.go:17-23`) untouched; any gate write
      gets its own one-method interface. Truncation detection already exists in
      `internal/workspace/catalog/pulls.go:104-105`.
20. **Gate health and decisions are invisible.** [codebase, readiness, structural]
    - Reasons are free text today (`internal/reporters/reporter.go:27-31`; `internal/gate/secrets.go:28-29`).
      Check-run data and run logs expire (ADR-0016:52-53; logs unverified).
    - Requirement: enumerated reason codes; every verdict carries head SHA, base SHA, policy commit,
      gate version, category and each predicate's evidence state; a scheduled job flags open pull
      requests with no gate result after N minutes or with a gate error; a drift check compares the
      live ruleset with the expected one (which token can read rulesets is unverified); per-step
      timings as in ADR-0013:40-42.
21. **Skipped, cancelled and missing runs.** [readiness, catalyst, design]
    - Unverified: a skipped required workflow may count as passing; cancelled counts as failing.
      Per-pull-request concurrency cancels runs (`ci.yml:13-15`). Conflicted pull requests get no
      `pull_request` run (`internal/installer/templates/review-index.yml.tmpl:8-10`). Open pull
      requests at switch-over may get no run until the next event.
    - Requirement: no job-level `if:` or `paths:` on the gate; a trusted script that kicks every open
      pull request after the ruleset change; per-pull-request concurrency that also covers sweep
      writes, so an older run cannot post success after the sweep posts pending.
22. **Approval racing a push.** [design]
    - An API review without `commit_id` binds to whatever the head is at that moment (unverified);
      the web UI's behaviour is unverified. An approval can land on code nobody saw.
    - Requirement: test the UI in the sandbox; consider requiring the review to post-date the head's
      arrival; document the remaining window.
23. **Stale base.** [design, catalyst]
    - The merged tree is S on top of B2, while the gate judged S against B1. Base-dependent
      predicates (lockfiles, regenerated code, "nothing reaches it", CODEOWNERS added in B2) break.
    - Requirement: choose "require up to date" or the sweep on every push to `main`, explicitly.
24. **Docs-only must be a directory allowlist, not `*.md`.** [zoom-out]
    - In this repository markdown is often code or policy: embedded model-lane prompts
      (`internal/laneb/review.go:22`), the workspace assistant prompt
      (`internal/workspace/assistant/assistant.go:26`), guidance that steers the model lane
      (`internal/config/config.go:29`; `internal/laneb/gather.go:214-216`), and append-only ADRs.
    - Proposed v1 predicate: every file added or modified (no removals, no renames in or out) under
      an explicit list such as `docs/**` minus `docs/adr/**`, plus root `README.md`; not a fork; file
      count equals `ChangedFiles`. Floor exclusions: agent instruction files, `.github/**`,
      `.review/**`, MDX, and any markdown embedded or read by code.
    - Ruled out of the first category: comment-only (comments carry directives such as
      `@ts-expect-error`, `# type: ignore`, Rust doctests, licence headers checked at `ci.yml:108-109`),
      formatting-only (formatter config is pull request content), generated code (regeneration runs
      pull request code), lockfiles (keep on Renovate), test-only, inert-additive, anything needing
      CI evidence.
    - Docs-only exists to prove the plumbing; expect little time saved.
25. **The installer cannot set this up safely.** [readiness, codebase]
    - `reviewer init` writes workflows into the destination repository
      (`internal/installer/plan.go:114`; `internal/installer/write.go:129-132`). A gate workflow
      written there is pull request content. Rulesets need an org owner.
    - Requirement: `init` prints the ruleset, policy-repository and auto-merge setup through the
      manual-steps list (`plan.go:88-92`; `write.go:247-258`). Existing installs are unaffected until
      an admin opts in. The workflow should hold `contents: read`, `pull-requests: read` and at most
      one write scope, asserted as in `internal/installer/write_test.go:528-530`.
26. **Side duties have no owner now that no actor merges.** [comprehension, structural]
    - Round 1 gave revert preparation, circuit-breaker issues and the decision record to "the merge
      actor". Comments and issues posted with `GITHUB_TOKEN` share the github-actions identity with
      any workflow a pull request adds, so marker authenticity (round-1 D6) no longer holds.
    - Requirement: name the identity for each duty, or drop it; state how a record is shown to be
      genuine, or that it is not.
27. **`reviewer risk` and `merge-gate` will look inconsistent.** [comprehension, zoom-out]
    - A `risk/low` pull request that stays pending, or a `risk/high` docs-only pull request that
      merges, will look like a bug.
    - Requirement: state on the pull request that risk is advisory and does not decide merging. Say
      which artifact each command emits and why (round 1 leaned against check runs; round 2 brings
      one back for `reviewer risk`). If `reviewer risk` posts a check run, the review workflow needs
      `checks: write`, which widens what a pull-request-controlled job holds and makes ADR-0018's
      permission sentence false at that stage.
28. **The brief needs a Terms section.** [comprehension, structural]
    - Define: required status check; commit status vs. check run; ruleset; required workflow; bypass
      actor; native auto-merge; merge queue; `pull_request_target`; `workflow_run`; head commit;
      base-branch policy; qualify; deny-by-default; break glass; kill switch; auto-merge enabler.
      Rename "category", which already means a model-lane category (ADR-0022, ADR-0024), for example
      to "merge class".
29. **GitHub semantics and shared packages can drift under the gate.** [structural, codebase]
    - `ghclient`, `diff` and `config` change for review reasons. GitHub can change rulesets,
      auto-merge or event payloads.
    - Requirement: import-allowlist tests; CODEOWNERS on gate paths and the shared packages it
      imports; gate tests that run when those packages change; the conformance suite; generate the
      design doc's truth table from the code's table-driven test.

#### Nit

30. Name collisions: config key `gate:` and package `internal/gate` already mean the secrets gate
    (`internal/config/config.go:31,93-101`; `internal/gate/secrets.go:3`). Use `internal/mergegate`
    and a distinct policy file name. [codebase, readiness]
31. "Inputs the pull request cannot control" is literally false: the diff is an input. Say "code
    and policy the pull request cannot control; the diff is data only", and list inputs by
    provenance. [comprehension]
32. Commit status descriptions are short (about 140 characters, unverified). Put a stable short code
    in the description and the full reason in the job summary or a marker comment. [readiness,
    design, comprehension]
33. First step of every run: post "pending: evaluating" on the head. Draft pull requests may not use
    auto-merge (unverified). [design]
34. Do not enable "Allow GitHub Actions to create and approve pull requests" so a trusted workflow
    can approve under a native 1-approval rule: any same-repository workflow could then approve
    (unverified). `self-review.yml:37-53` already runs pull-request-built code with
    `pull-requests: write`. Keep the setting off. [catalyst, design]
35. Docs-only merges still trigger a production deploy; confirm it is harmless or path-filtered.
    Consumer repositories whose CI uses `paths-ignore: docs/**` will leave required CI pending on
    docs pull requests (unverified). Auto-merged docs become context for future agents; record that
    as accepted risk. [zoom-out]
36. Citation fixes in round-1 text: `ghclient.Writer` has five methods (four writes plus the
    `ReviewThreads` read), not three verbs (`internal/ghclient/write.go:17-23`); `fetch.go:263-264`
    is `internal/workspace/navigation/fetch.go`; `concepts_brief.md:NNN` refers to the repository-root
    `concepts_brief.md`, not this file; package docs at `write.go:11-12`,
    `client.go:6-8` and `client.go:122-124` are stale. [codebase]
37. Sandbox output must not carry organisation, team or repository names into this repository
    (ADR-0010). Keep the ruleset as a template with placeholders, not a raw export. [structural]

### Round-1 objections: what round 2 changed

| Round-1 item | Round-2 status |
| --- | --- |
| Blocker 1: classification controlled by the author | Changed. Now Blockers 1, 3, 4 and 5 above (forgery, triggers, SHA binding). |
| Blocker 2: merge authority contradicts ADRs | Changed. ADR-0008 is no longer contradicted. ADR-0009 is; ADR-0018 depends on the producer (Major 13). |
| Blocker 3: merge identity, "merge-only" not a permission | Resolved by the no-App decision. Its "never a bypass actor" rule now applies to break-glass design (Major 12). |
| Blocker 4: how a no-approval merge satisfies branch protection | Answered: 0 approvals plus `merge-gate`. That creates Blocker 7 (lost native protections). |
| Blocker 5: decision rule cannot be written down | Narrowed by category-first. Targets (D5) still open. |
| Blocker 6: no durable store | Open (D6). Its App-authored premise is gone (Major 26). |
| Blocker 7: timing and handoff | Changed. The gate never waits on CI, because CI is required separately. Trigger questions are Blocker 3. |
| Pattern "compare-and-swap merge" | Retired. Replaced by the SHA-pure verdict plus "success only into the protected base". |
| Pattern "approver App" | Superseded by the no-App decision. |
| Resolution 5 "merge actor prepares the revert, opens suspension issues" | Open. Needs a named identity (Major 26). |
| Nit 24: `GITHUB_TOKEN` merge does not trigger deploy | Now part of the mechanism. Still unverified (Major 15). |
| Majors 8, 9, 11, 13 to 22 (shadow arithmetic, backfill, never-qualify list, signal sources, scoring, kill switch, override rules, mutation testing, ADR plan, verification plan) | Still apply to later categories. Override rules are now Blocker 7. The ADR plan is Major 13. |

## Goals vs. Non-Goals

**Goals**

- **Reduce human review time** (the requester's first priority), measured against a baseline taken
  before launch. Agent wait time and stale pull requests come second.
- **Show the classifier's verdict and its deciding reasons on every pull request.** Display only:
  `reviewer risk` never blocks a pull request, so ADR-0009 still holds for review output.
- **Merge without a human only when all of the following hold:**
  - the pull request passes a deny-by-default category predicate;
  - the predicate is computed by code the pull request cannot edit, reading the diff only as data;
  - no hard exclusion or veto applies;
  - every required piece of evidence is present.
- **Keep the human path no weaker than today.** The gate's human branch is shown equivalent to the
  native approval rule before it replaces it, including "changes requested" and code-owner review.
- **Set targets per category.** Each category gets a numeric precision target, a written definition
  of failure with N fixed, and a policy for what happens after the first failure.
- **Put the floor and the never-qualify classes in code.** Policy is read from the base branch and
  can only tighten.
- **Fail closed on the category path only.** Missing, stale or malformed evidence means no category
  merge, and the pull request follows the human path. A gate failure never removes the human path.
- **Keep a durable decision record for each merge.** It holds:
  - the head and base SHAs;
  - the policy commit;
  - the gate version;
  - every signal value and its evidence state;
  - the category, or the approver and the approved SHA;
  - the enabler and `merged_by`;
  - the gate run ID.
- **Provide a kill switch** that can be changed without a pull request and drops the gate to
  approval-only, a per-category circuit breaker, a break-glass path that skips only the gate, and a
  permanent random audit sample.

**Non-Goals**

- In v1 (Iteration 3): `merge-gate`, any required status produced by this repository, and any
  organisation-level ruleset or App. The goals above about merging by category describe the
  later stages, not v1.
- The test-suite audit. It is a separate project.
- Fork pull requests. They never qualify by category.
- A learned model in the first version.
- A hosted service.
- Merge authority inside the `reviewer` binary. Nothing in this repository performs a merge. The
  merge-gate subcommand posts one status, and native auto-merge merges (see Iteration 2).
- Auto-merging changes that cannot be reverted, or that touch policy, workflows, CODEOWNERS,
  rulesets, CI-influencing configuration, `.review/` or agent instruction files.
- Categories in Python, bash or Rust that need analysis `reviewer` does not have, in the first
  version.
- Collecting labels in the review workspace in the first version.
- Mutation testing and the test-trust factor in the first version.
- A merge queue in the first version.

## Alternatives Weighed

Status values: Rejected by author; Settled; Settled pending confirmation; Open (with a lean);
Superseded (by what).

| Option | Status | Technical reason |
| --- | --- | --- |
| A hosted service to classify or merge | Rejected by author | The hypothesis requires "No hosted service". This matches ADR-0008. |
| Model lane able to approve a merge | Rejected by author | Author-controlled text can carry prompt injection, so the model lane may veto and never approve. |
| Author-controlled text (descriptions, commits, comments, session logs) lowering risk | Rejected by author | It is written by the party being judged. |
| Settings able to lower the fixed floor | Rejected by author | Thresholds are configurable only above the floor. Comprehension notes the floor must work as a ceiling for risk. |
| Auto-merging non-reversible changes, or pull requests touching policy, workflows, CODEOWNERS or rulesets | Rejected by author | Recovery is revert plus deploy, and a pull request could otherwise edit its own gate. |
| Merge actor as a separate consumer of `reviewer` output (separate binary or repository) | Rejected by author: keep it in this repository; the required-check route removes most of the cost of doing so | This is the shape ADR-0009:56-58 prescribes. It keeps ADR-0018 and `write.go` true. Cost: a second artifact and a schema contract. |
| Machine-user personal access token as a fallback if Apps are refused | Rejected by author: the default route needs neither an App nor a machine user | Its scoping is worse than an App's. Readiness recommends stopping instead. |
| Hand-weighted continuous score over about 20 signals | Rejected by author for the first version: category-first instead | Uncalibrated, so thresholds carry no meaning. Repeats ADR-0017's rejected combined score. Unexplainable. |
| Required `merge-gate` check plus native auto-merge, no App | Settled pending confirmation (D1) | The author's round-2 route. Round 2 found it workable only with a pinned required workflow as producer (Blockers 1 to 3). |
| Ruleset required workflow pinned by SHA to a protected ref, as the only producer of `merge-gate` | Raised by all round-2 lenses, open (favoured; availability unverified) | The only candidate that plausibly binds the check to a workflow file instead of a name. May need an org owner and a specific plan. |
| `pull_request_target` workflow as the producer | Raised by design, readiness, catalyst, codebase, open (leans reject) | Forgeable by name (Blocker 1). Runs the base branch's workflow, which an attacker can create (Blocker 4). ADR-0018:51-53 rejects it and ADR-0018:59 says "Never". |
| `workflow_run` workflow as the producer | Raised by design, readiness, catalyst, open (leans reject as producer; acceptable as a doorbell) | Forgeable by name. Matches upstream workflows by `name:`. Its job checks may attach to the default-branch SHA (unverified). |
| `pull_request_review` workflow as the producer | Raised by design, readiness, catalyst, open (leans reject) | Probably runs the pull request's own workflow file (unverified). Use only as a wake-up trigger. |
| Native rulesets, native auto-merge and CODEOWNERS with unowned safe paths | Raised by zoom-out, catalyst, open (depends on sandbox) | May cover docs-only with no code and keep sensitive paths under GitHub's own rule. Unverified whether code-owner review is enforced at 0 approvals. |
| Renovate automerge (self-hosted in Actions) or Dependabot with `fetch-metadata` | Raised by zoom-out, open (keep for dependency patch bumps) | Narrow `packageRules` with `minimumReleaseAge` cover dependency bumps without an App. |
| Merge queue | Raised by zoom-out, design, open (leans reject for v1) | The group commit is never classified, and `merge_group` may run the group's own workflow copy (unverified). Needs `merge_group` wiring. |
| "Require branches to be up to date" instead of a queue | Raised by catalyst, design, open (favoured for v1) | Classifies exactly the tree that lands; a policy change forces re-evaluation. Serialises merges and re-runs CI. |
| Required approval-only gate as the first release | Raised by readiness, open | Proves the plumbing, but replaces a tested native rule and adds a blocking failure mode (zoom-out). Run it alongside native approvals, not instead of them. |
| Non-required shadow gate (`merge-gate (shadow)`) as the first release | Raised by zoom-out, catalyst, open (favoured) | Proves triggers, SHA binding and latency with no ruleset change and no admin. |
| Split rulesets with pull-requests-only bypass, plus a standby approvals ruleset | Raised by catalyst, design, readiness, open (favoured) | Break glass skips only the gate, never CI, and falls back to native review (toggle behaviour unverified). |
| Agent tokens without the "Workflows" permission | Raised by catalyst, open | Agents cannot push a spoofing workflow. Agents can no longer edit CI. |
| Trusted workflow merges with `GITHUB_TOKEN` (with `sha` compare-and-swap) and deploy triggered by dispatch | Raised by catalyst, open (needs a sandbox test) | No person's token on the merge path. Needs `contents: write` in a trusted workflow and conflicts with ADR-0018:32-33 and `write.go:14-16`. |
| "Allow GitHub Actions to approve pull requests" with a trusted approving workflow | Raised by catalyst, open (leans reject) | Any same-repository workflow could approve (unverified). Repeats Blocker 1 at the approval layer. |
| Gate as a subcommand of `cmd/reviewer` | Raised by codebase, open | No new release artifact. Needs its own exit discipline (Blocker 6) and boundary tests. |
| Gate as a separate main package in this repository | Raised by zoom-out, open | Avoids inheriting the exit-0 contract. Adds a release artifact (ADR-0026) unless run by `go run` at a pinned SHA. |
| Merge actor as a subcommand of `reviewer` | Superseded by Iteration 2 D2 and the required-check route | Nothing in this repository merges. |
| Approver App instead of merger App | Superseded by D1 (no App) | Was raised by design in round 1. |
| Mergify | Raised by zoom-out, open (leans reject) | A hosted App: conflicts with the expectation that Apps are disallowed and with not sending content to vendors. Its conditions can mostly be expressed natively. |
| Kodiak | Raised by zoom-out, open (leans reject) | An App for label-triggered auto-merge, which native auto-merge already provides. |
| Graphite, Aviator | Raised by zoom-out, open (leans reject) | Hosted stacking and queue tools aimed at agent wait and stale pull requests. They do not classify risk. |
| Copilot review and similar vendor bots | Raised by zoom-out, open (leans reject) | Copilot is not permitted (`review-workspace.md:520`), and content would go to a vendor. |
| Advisory `reviewer risk` label and summary line for review priority | Raised by zoom-out, open (parallel track) | Fits ADR-0009, 0016, 0017 and 0024. Not on the merge path; zoom-out suggests scheduling it on its own value. |
| Learned model from the first version | Raised by design, readiness, open (leans reject; see `concepts_brief.md:457`) | No labelled data, and recall cannot be validated at this volume. |
| Rule-based allowlist of categories with vetoes | Settled (Iteration 2 D4) | Each decision traces to a named category and named rules. Can be measured per category, the way ADR-0024 measures per rule. |
| Recomputing evidence in a trusted job by running pull request code | Raised by zoom-out, catalyst, open (leans reject) | Runs untrusted code next to a token with `statuses: write`. This is the `pull_request_target` escalation ADR-0018 forbids. |
| Reading artifacts from the triggering pull request run in the trusted job | Raised by design, catalyst, open (leans reject) | A known poisoned-artifact pattern. Use them for advisory signals only. |
| Merge policy keys in `.review/config.yaml` | Raised by codebase, readiness, open (leans reject) | Strict decoding breaks older pinned binaries; the file is read from the head. Use a separate, versioned policy file read from the base SHA. |
| Check run as the store of record | Raised by codebase, zoom-out, structural, open (leans reject) | Check runs expire (ADR-0016:52-53). Needs `checks: write`. |
| Collecting reviewer labels in the review workspace | Raised by codebase, structural, readiness, catalyst, open (leans reject) | The workspace stores nothing and makes no write call. That would need ADR-0029 or ADR-0032 superseded. Labels from pull requests humans read anyway are a biased sample. |
| Backfill labelled at the merged revision | Raised by catalyst, open (leans reject in favour of the first-review revision) | Hides failures that humans caught. Labelling the first-review revision turns those catches into positive labels. |
| A short shadow period with a 1% bound | Raised by comprehension, zoom-out, readiness, catalyst, open | Not achievable at current volume. Options: a weaker first bound (about 60 cases for under 5%), a recall test on a known-bad corpus, or categories that need no rate claim. |

## Key Decisions & Staging for Next Steps

### Iteration 2: decisions by the author (2026-11-26)

- **D1, partly settled. Design for no GitHub App.** The author understands that organization
  policy disallows GitHub Apps. This can't be confirmed for about a week. The default route needs
  no App and no machine-user token. An App stays a possible later upgrade, not a dependency.
- **D4, settled. Category-first.** The first version uses a deny-by-default category allowlist
  with vetoes. The first wave will be much smaller than half of pull requests, and the author
  accepts that. A weighted or learned score comes later, and only after it passes a recall test
  on a corpus of known-bad pull requests.
- **D2, settled. The merge decision lives in this repository.** The cost the lenses raised was
  that "the review job cannot merge" would become a convention instead of a property. The route
  below reduces that cost, because nothing in this repository performs a merge. The decision code
  is still kept apart from the review path:
  - a separate subcommand;
  - its own workflow file, with its own permissions;
  - its own small writer interface (post one commit status).

  `ghclient.Writer` stays three verbs. ADR-0018 still needs a recorded amendment, because the tool
  now posts a status that gates merging.

**Route without an App: the gate is a required check, not an actor.** These GitHub behaviours were
described from memory and must be checked in a sandbox repository before anything depends on them.

1. The branch ruleset on `main` requires 0 approvals. It requires one status check, named for
   example `merge-gate`, plus the existing CI checks.
2. `merge-gate` passes when **either** a human with write access approved the current head
   commit, **or** the pull request fits a qualifying category under base-branch policy. Otherwise
   it is pending, with the reason. The human path is the same as today; only who decides "approved
   enough" moves into the check.
3. Native auto-merge does the merging. The pull request author enables it, and agents can enable
   it when they open the pull request (`gh pr merge --auto`). The merge is attributed to the author,
   so the push to `main` triggers deploy workflows as usual. A merge made with `GITHUB_TOKEN`
   would not trigger other workflows, which is why the gate does not merge by itself.
4. `merge-gate` must come from code the pull request cannot edit. The candidates, in order:
   - a ruleset "required workflow" pinned to a ref the pull request does not control;
   - a workflow on `pull_request_target` or `workflow_run` that reads only base-branch code and
     treats the diff as data.

   A plain `pull_request` workflow does not qualify: a pull request can edit it, or add another
   workflow that posts a status with the same name.
5. It also re-runs on `pull_request_review`, so a human approval clears the gate.

**Trade-offs of this route that the next review must examine.**

- **It moves the human-approval rule out of GitHub and into our check.** A bug in `merge-gate`
  that passes wrongly merges pull requests without review. Its code is the security boundary and
  needs the adversarial tests listed for S4.
- **It fails closed, and that can block.** If `merge-gate` doesn't run, nothing merges, including
  pull requests a human has approved. This conflicts with ADR-0009 more directly than an advisory
  label does. It needs a break-glass path (an admin bypass on the ruleset) and a decision on how
  ADR-0009 is amended.
- **Changing rulesets may need an org admin**, the same kind of approval as installing an App.
  Confirm who can do it.
- **The author enabling auto-merge carries accountability.** If agents act with a person's token,
  that person is the one who "merged".

**Round-2 notes on the Iteration 2 text** (the author's text above is unchanged):

- Route item 4: round 2 recommends dropping the second candidate as a producer (Blocker 1). It
  remains usable as a wake-up trigger.
- Route item 5: probably not possible as written (Blocker 3).
- Route item 2, "the human path is the same as today": true only if the gate reproduces the
  protections GitHub drops at 0 approvals (Blocker 7).
- D2, "ADR-0018 still needs a recorded amendment": `tools/checkadrs` has no amendment mechanism
  (Major 13). D2, "`ghclient.Writer` stays three verbs": it has five methods today (Nit 36); the
  intent, that the review writer gains nothing, still holds.
- "The adversarial tests listed for S4": S4 is renumbered below; the tests are in Blockers 1 to 5
  and the sandbox list.

### Iteration 3: v1 narrowed to what needs no organisation change (2026-11-27)

**Why.** Round 2 found that without an App, `merge-gate` can be forged by any workflow or by a
token with write access, unless it comes from a ruleset required workflow pinned to a protected ref
(Blocker 1, Blocker 2). Both that and an App need an org owner. The author chose to ship what needs
neither, and to take auto-merge beyond docs up again once the org question is answered.

**v1 contains three things.**

1. **Advisory `reviewer risk`, on every pull request.** Deterministic signals only (paths against
   the risk map, reach, history, deterministic-lane findings, never-qualify classes). It writes one
   marker comment and one label. It exits 0 and never gates anything, so ADR-0009 and ADR-0018 hold
   as written. Whether adding a label fits the current `pull-requests: write` permission and needs a
   new method on `ghclient.Writer` is unverified, and it should be recorded in an ADR if it does.
2. **Docs-only by native configuration, conditional on sandbox experiment 15.** CODEOWNERS owns
   `*` first, then leaves an explicit directory allowlist unowned (Major 24: `docs/**` minus
   `docs/adr/**`, plus root `README.md`; never agent instruction files, `.github/**`, `.review/**`,
   MDX, or markdown read by code). The repository ruleset sets required approvals to 0 with
   "Require review from Code Owners" on. This is a repository-admin change, not code in this
   repository. If experiment 15 shows code-owner review is not enforced at 0 approvals, or an org
   ruleset sets a minimum approval count, this item is dropped and v1 is advisory only.
3. **Measurement, read-only.** The candidate count query (merged pull requests in the last 90 days
   whose files all sit under the allowlist, and their time to first approval), a review-time
   baseline, and backfill labelling at the first-review revision using the `cmd/reviewer/metrics.go`
   pattern. This builds the labelled history that every later category needs.

**Objections to v1 itself.**

- **The advisory label is produced from configuration the pull request controls.** In v1
  `reviewer risk` still runs on `pull_request`, with the pull request's own `.review/config.yaml`.
  A "low risk" label that reviewers use to skim is therefore a label the author can set. Rule: the
  label may only flag risk ("touches auth", "crosses 3 CODEOWNERS groups") and never certify
  safety. A pull request that changes `.review/` or the workflow is labelled as such.
- **Approvals at 0 removes a protection for every unowned path.** Any path CODEOWNERS fails to
  own merges with no review. The CODEOWNERS file must own itself, own `*` by default, and be checked
  in CI so a new top-level directory is owned on arrival. Whether "changes requested" and stale
  approval dismissal still apply at 0 approvals is unverified (experiment 15, 13).
- **Docs are not harmless if `main` deploys a docs site.** Rendered content is production. If any
  allowlisted directory is published, it needs the same thought as code, or it leaves the list.
- **v1 will not reach "half of pull requests".** Docs-only is likely a small share; the candidate
  count says how small. Most of v1's saving on review time comes from reviewers using the risk label
  to order work, which is unmeasured until the baseline exists.

**Deferred until the org question is answered:** `merge-gate`, stages S2 to S5, any status that
gates merging, ADR-0018 and ADR-0009 supersessions, and decisions D-A to D-I.

**v1 order of work.**

1. Run the candidate count and record today's ruleset and review-time baseline. No code.
2. Run sandbox experiments 13 and 15 in a personal sandbox repository.
3. If 15 passes, apply the CODEOWNERS and ruleset change to one repository, with the CODEOWNERS
   ownership check in CI.
4. Build advisory `reviewer risk` for one TypeScript repository (the parallel track in Staging).
5. Backfill labels for that repository.
6. Re-open the org question with the count, the baseline and the sandbox results in hand.

### Decisions, round 2

Each decision has one status. Round-1 IDs are kept and never reused; round-2 decisions use
letters. "Holds up" names the work that cannot start until the decision is made.

| ID | Decision | Status | Holds up |
| --- | --- | --- | --- |
| D1 | Is a GitHub App allowed? | Settled pending confirmation: no App (org owner, about a week). Re-opens if Blocker 2 shows required workflows are unavailable. | Nothing on the default route |
| D2 | Where the decision code lives | Settled: this repository, apart from the review path | D-C |
| D3 | Merger or approver App, and ruleset layout | Superseded (App half). The ruleset half is now D-G. | — |
| D4 | First-version scoring method | Settled: deny-by-default categories with vetoes | Category predicates |
| D5 | Targets per category | Open. For verifier categories, may become "zero escapes plus an audit sample". | Live category merging |
| D6 | Where decisions, outcomes and labels are stored | Open. App-authored markers no longer exist (Major 26). | Outcome recorder |
| D7 | Which signals may lower risk | Open. Draft as ADR 0041. | Categories beyond docs-only |
| D8 | Merge queue or "require up to date" | Open. Round 2 leans: no queue in v1; "require up to date" (Major 16, 23). | `merge_group` wiring, revocation design |
| D-A | Plan, and who administers org rulesets | Open. Ask the org owner in the same request as D1. | Everything below |
| D-B | Which mechanism produces `merge-gate` | Open. Round 2 leans: pinned required workflow only. | Gate input contract, token scopes, ADR 0035 |
| D-C | Where the gate workflow and binary live, and how they are pinned | Open. Same repository or a separate policy repository pinned by SHA; reuse the version-plus-checksum pattern (`action.yml:53-73`). | Release pipeline, pin bumps |
| D-D | How the gate reports pass, waiting and error | Open. Depends on sandbox experiments 9, 10 and 12. | Visible-state table, agent behaviour |
| D-E | The exact human-approval rule | Open (Blocker 7) | All gate code; the category list can start empty |
| D-F | Who may enable auto-merge, and the accountability model | Open (Major 14) | Decision record |
| D-G | Ruleset layout, break-glass holders, kill switch and its fail direction | Open (Major 12) | Required-check cutover |
| D-H | ADR set and timing | Open (Major 13). Supersessions recorded before the gate is required. | Merging any gate code |
| D-I | One command or two | Open (see "Points where lenses disagree") | Code layout |

### Sandbox experiments, in order

Run in the production organisation or on the same plan, in a private repository, with three
accounts (writer who opens, writer who approves, read-only) and a push-triggered "deploy" workflow
on `main` that records `github.actor`. Every expected result below is unverified. Record results in
a trials file linked from the design, without organisation, team or repository names.

1. **Forgery.** A same-repository pull request adds a workflow with a job named `merge-gate`; then
   one that posts a `merge-gate` commit status; then a token-posted status; then a status and a
   check run with the same name. Does any satisfy a name-matched required check, with and without
   source pinning? If yes, name matching is unusable (Blocker 1).
2. **Required workflow availability and administration.** Org-level and repository-level, on the
   production plan. Who can create, edit, bypass or disable it? (Blocker 2)
3. **Required workflow pinning and edits.** Pin by branch, tag and SHA. Can a pull request edit the
   pinned file in the guarded repository? In a separate repository? Is the rule matched by file and
   ref, not by check name?
4. **Required workflow events.** Record `event_name` and `action` for opened, synchronize, reopened,
   review approved, review dismissed, changes requested, base change and `merge_group`. (Blocker 3)
5. **`pull_request_review`.** Which workflow file runs, and with what token permissions, for
   same-repository and fork pull requests? Does a dismissal fire it?
6. **Re-running the pinned workflow from a trusted job** with `actions: write`, and whether a re-run
   reuses the old payload or old base code.
7. **Statuses across pull requests and bases.** Same head SHA in two pull requests with different
   bases; base retarget; the `pull_request_target`-on-attacker-base attack. (Blocker 4)
8. **Which SHA job checks attach to** under `pull_request_target` and `workflow_run`.
9. **Pending, failure and auto-merge.** Does a failing required check disable auto-merge? Does
   auto-merge merge when the gate turns green after a re-run? (Blocker 9)
10. **Skipped, neutral and cancelled** required-workflow results, and whether a newer success
    replaces a cancelled run.
11. **Auto-merge attribution and deploy.** `merged_by`, commit author and committer, and whether the
    deploy fires, when the last green check came from `GITHUB_TOKEN`; and when auto-merge was
    enabled with `GITHUB_TOKEN`. (Major 15)
12. **Auto-merge lifecycle.** New pushes by a writer, an agent and a non-writer; force-push;
    "Update branch"; enabler loses access; "Allow auto-merge" turned off.
13. **Reviews.** `commit_id` on a web-UI review and on an API review with no commit given; latest
    review per reviewer; dismissed reviews; whether approvals survive a base retarget.
14. **Permissions from `GITHUB_TOKEN`.** Collaborator permission endpoint, team membership, reading
    rulesets, reading a repository variable for the kill switch.
15. **CODEOWNERS at 0 approvals.** Is code-owner review still enforced? Can docs-only with unowned
    paths auto-merge with no review? (Decides whether the first category needs gate code.)
16. **Bypass and enforcement toggles.** Is bypass per ruleset? Does "pull requests only" bypass
    leave CI enforced? Can a standby ruleset be enabled quickly? Is bypass use audited? Does
    "Evaluate" mode exist on the plan and report would-block results?
17. **A workflow's `permissions:` block vs. an org read-only default**, and whether agent tokens
    without "Workflows" permission can push workflow files.
18. **Merge queue** (only if D8 chooses one): do required checks report on the group commit, and
    which workflow copy runs?
19. **Gate cannot start.** Broken YAML at the pinned ref, Actions disabled, a missing runner label.
    What does the pull request show?

**Cheapest next actions, needing no new authority.**

- Count candidates with no code: a `gh pr list --state merged --json number,files,reviews` query
  over the last 90 days in each repository, counting pull requests whose files are all added or
  modified under the proposed docs allowlist, and their time to first approval. (zoom-out)
- Run experiments 1, 4, 5, 13 and 15 in a personal sandbox while waiting for D-A. Treat results as
  provisional until repeated on the production plan.
- Ask the org owner D1 and D-A in one request.

### Staging

v1 (Iteration 3) is S0 without D-A to D-I, S1, and the parallel advisory track. S2 to S5 wait for
the org question.

| Stage | Content | Exit criterion |
| --- | --- | --- |
| S0: decisions and sandbox, no code | D-A to D-I; sandbox experiments 1 to 17 recorded in a trials file; ADRs 0035 onward drafted as Proposed; named org owner for rulesets and break glass; candidate count | Producer chosen (D-B) with forgery and trigger tests passed; human-approval rule written (D-E); if experiment 15 passes, docs-only may go native in S1 |
| S1: native configuration (conditional) | If experiment 15 passes: CODEOWNERS owning the risk map, unowned docs allowlist, Renovate rules for dependency patch bumps. No ruleset change to approvals. | Docs-only pull requests merge without new code; or S1 is skipped and docs-only moves to S5 |
| S2: gate in shadow, not required | Gate as a non-required status with a different name (`merge-gate (shadow)`), produced by the chosen pinned workflow; human and category branches both computed; native 1-approval rule unchanged; decision record; health job; reason codes | An agreed period with zero disagreements between the human branch and GitHub's approval state on every pull request; latency within budget; no missed evaluations |
| S3: gate required alongside native approvals | `merge-gate` required in its own ruleset, approval-only mode (categories off), native approvals still required; split rulesets and standby approvals ruleset; break-glass drill; open-pull-request kick script; supersessions of ADR-0018 and ADR-0009 recorded before this stage | Zero disagreements for an agreed period; break-glass drill passed; rollback runbook tested |
| S4: native approval rule removed | Approvals set to 0; the gate is the only approval rule; drift check on the ruleset; post-merge audit job by workflow file ID | No merge without a gate success or an audited bypass; rollback (re-add approvals first) rehearsed |
| S5: first category live, one repository | Docs-only directory allowlist (Major 24) unless delivered natively in S1; kill switch variable plus revocation sweep; "require up to date"; decision record with accountability fields | The criterion from D5 met with zero escapes; any escape returns the category to shadow |
| S6: outcome recorder and backfill | Scheduled trusted job writes outcome markers; identity and authenticity of markers decided (D6); implicit labels from review actions; backfill at the first-review revision; known-bad corpus | A hand-audited sample of labels agrees with the heuristics at an agreed rate |
| S7: expansion | Further categories behind D7 (provenance), each in shadow first; coverage evidence for TypeScript; the test-trust factor and mutation testing after the audit; other languages once analyzers exist | Per-category precision report in the ADR-0024 style |
| Parallel track: advisory `reviewer risk` | Deterministic signals only; base-branch policy; marker comment plus risk label (or a neutral check if ADR 0035 has landed); one TypeScript repository; never an input to the gate | Two weeks with a record on every pull request; reviewers use labels to order work |

**Design document.** The structural lens proposes `docs/designs/risk-gated-merge.md`, modelled on
`review-workspace.md`. For the round-2 route it would contain:

- Terms (Major 28)
- Record of decisions, using the status list above
- Problem, with a baseline, including today's ruleset written down (required approvals, stale
  dismissal, code-owner review, conversation resolution)
- Goals, each with a check
- Non-Goals
- Open questions, each with what it holds up
- Trigger, ref and permission table: one row per event, which workflow file runs, token
  permissions, whether that run can post `merge-gate`, and whether a pull request can add a
  workflow that fires on the same event
- Gate truth table, including time: inputs, outputs with reason codes, and every event that revokes
  a pass
- Threat model for the required check, one sandbox test per row
- Link to the trials file with sandbox results, plan or tier, date, and re-check schedule
- Visible-state table: what a developer or agent sees in each case, and who is accountable
- Accountability model
- Ruleset layout as a template with placeholders, plus the drift check
- Break-glass runbook and kill-switch runbook
- Gate evaluation and auto-merge handoff sequence (replaces round 1's classify-and-merge sequence)
- Boundary rule for the gate, written as tests: the gate never executes pull request content and
  reads head files only as data; no workflow other than the gate host can produce `merge-gate`;
  the review writer gains no verb; the gate writer has at most one; every status names a SHA
- Critical user journeys: a human approves and then someone pushes; an agent enables auto-merge; a
  reviewer requests changes after a category pass; the gate fails to run and break glass is used;
  the kill switch flips while auto-merge is pending; a policy-edit pull request; a fork pull
  request; ruleset drift
- Signal registry and trust-rule table
- Changes to recorded decisions (the table style of `review-workspace.md:501-510`)
- Alternatives
- Risks
- Non-functional requirements, including the API budget for the sweep
- Rollout and rollback, in the order of the staging table
- Migration for existing installs and the other repositories
- An implementation plan numbered after PR-54
- A verification plan, including the conformance suite

## Reviewer Status

Round 1:

| Lens | Status |
| --- | --- |
| egm-design | completed |
| egm-codebase-details | completed |
| egm-zoom-out | completed |
| egm-comprehension | completed |
| egm-implementation-readiness | completed |
| egm-structural | completed |
| egm-inventive-catalyst | completed |

### Round 2 reviewer status

| Lens | Status | Focus |
| --- | --- | --- |
| egm-design | completed | Ways `merge-gate` passes wrongly; fail-closed vs. ADR-0009; break glass |
| egm-codebase-details | completed | Reuse, required changes, code placement, citation checks |
| egm-zoom-out | completed | Smallest first category and release; one command or two |
| egm-comprehension | completed | Newcomer reading of the brief; terms; visible states |
| egm-implementation-readiness | completed | Sandbox experiments; first decisions and their order; operations |
| egm-structural | completed | Design-doc contents; ADR plan; decision record |
| egm-inventive-catalyst | completed | Contradictions in the route and resolutions; break glass |
