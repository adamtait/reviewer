<!-- SPDX-License-Identifier: MIT -->
# Risk-gated merge

*   **Author:** adamtait
*   **Status:** Proposed
*   **Date:** 2026-11-27

--------------------------------------------------------------------------------

## Overview

Coding agents write more than 90% of the pull requests on this team's repositories, and a human
reads every one before it merges. Human review is the most expensive step in that loop, followed by
agents waiting for a merge and pull requests going stale. `main` deploys to production, and the only
recovery from a bad merge is a revert and another deploy.

This design adds two things to `reviewer`, in two phases.

**v1 (needs no organisation-level change).**

1. `reviewer risk`, an advisory command that runs as its own job in the review workflow on every
   pull request and attaches a risk record (one comment and one or more labels) that says what in
   the change deserves attention. It never decides or blocks anything, and it exits 0, so ADR-0009
   holds, and the review job keeps the permissions ADR-0018 states.
2. A docs-only merge class delivered by GitHub's own settings, not by code: CODEOWNERS owns every
   path except an explicit documentation allowlist, and the repository ruleset requires code-owner
   review with 0 general approvals. This ships only if a sandbox test shows GitHub still enforces
   code-owner review at 0 approvals.
3. Read-only measurement: how many past pull requests would have qualified, how long review takes
   today, and labelled history for later merge classes.

**Later stages (need an organisation owner).** `merge-gate`, a required status check produced by a
pinned required workflow, which passes when a human with write access approved the current head
commit or the pull request fits a deny-by-default merge class under base-branch policy. GitHub's
built-in auto-merge does the merging; nothing in this repository merges. These stages start only
after an org owner confirms that a required workflow can be pinned, because without that the check
can be forged by any workflow or token with write access.

The design is built so that the first version is useful alone and the later stages can be dropped
without undoing it.

## The Problem

**Review time is spent evenly on uneven risk.** A one-line typo fix in `docs/` and a change to the
auth middleware wait in the same queue and get the same human attention. Reviewers have no signal,
other than reading the diff, that tells them which pull requests need a careful read and which need
a glance.

**Agents wait on humans for changes that carry no production risk.** Documentation changes wait
for an approval that adds little, and while they wait, the agent's next task waits too, and the
branch drifts from `main`.

**Nobody knows how much of the queue is low-risk.** The requester expects about half of pull
requests could merge without a human. There is no measurement behind that number, no baseline of
how long review takes today, and no record of which merged pull requests later turned out to be
wrong. Without that record, no automated decision can show its own precision.

**The obvious fix is unsafe in this setting.** A classifier that scores each pull request and
merges the low scores has three problems here:

*   The evidence that would lower a score (tests, coverage, the model lane, the pull request's
    configuration) is produced by running the pull request's own code or read from its own files.
    The party being judged controls the evidence.
*   Showing a low miss rate needs hundreds of clean outcomes per class. At dozens of pull requests a
    week that takes months, and the test suites are not trusted enough to stand in.
*   Without a GitHub App or a pinned required workflow, any status check the repository's workflows
    produce can be imitated by another workflow, or by a person's token that an agent holds.

**What this design does about it.** It separates the two halves of the problem. Telling reviewers
where to look (`reviewer risk`) is safe to ship now, because it only adds attention and never
removes a review. Letting a pull request skip review is restricted to merge classes whose safety
can be checked by rules from inputs the pull request cannot change, starting with docs-only, and
anything beyond what GitHub's own settings can enforce waits for an organisation owner.

## Goals

**v1**

*   **G1. A risk record on every pull request.** In every repository where `reviewer risk` is
    enabled, every non-draft pull request gets one risk comment and its labels within the review
    workflow's run. Check: the post-merge audit query finds no merged pull request without a
    record.
*   **G2. The record only adds attention.** Every label names a reason to look ("touches auth",
    "changes CI", "crosses 3 owner groups"). No label or text says a pull request is safe or low
    risk. A pull request that changes `.review/` or `.github/` is labelled as such, because its own
    record is computed from configuration it controls. Check: the label set in code contains no
    "safe" or "low" label; a unit test fails if one is added.
*   **G3. `reviewer risk` never changes the outcome of a run.** It exits 0 whatever it finds and
    whatever fails, as ADR-0009 requires. Check: an error-path test for every failure the command
    can hit.
*   **G4. Docs-only pull requests merge without a human, in one repository, if experiment 15
    passes.** A pull request whose every file is added or modified under the documentation
    allowlist merges once CI is green, with no approval. Check: in the sandbox and then the first
    repository, a docs-only pull request merges unreviewed, and a pull request touching any other
    path cannot merge without a code-owner approval.
*   **G5. No path outside the allowlist loses review.** CODEOWNERS owns `*` and itself, and a CI
    check fails when a path in the repository has no owner and is not on the allowlist. Check: the
    CI check fails on a branch that adds an unowned top-level directory.
*   **G6. A baseline exists before anything is claimed.** For each enabled repository, record the
    current ruleset, the median and 90th-percentile time from "ready for review" to first approval
    over the last 90 days, the number of merged pull requests that would have fit the docs-only
    class, outcome labels (reverted, hotfixed in the same lines within 14 days, or clean), and a
    first-review label (changed after review, unchanged after review, or no review) for the same
    period. Check: a committed trials file with these numbers and the query that produced
    them, without organisation or repository names.

**Later stages (`merge-gate`)**

*   **G7. `merge-gate` cannot be satisfied by anything but the pinned gate.** Check: sandbox
    experiments 1 to 3 pass (a job named `merge-gate`, a posted commit status, and a token-posted
    status each fail to satisfy the rule).
*   **G8. The human path is never weaker than today.** Before the native approval rule is removed,
    the gate's human branch agrees with GitHub's own approval state on every pull request for an
    agreed period. Check: zero disagreements in the shadow log.
*   **G9. A pass is bound to one head SHA, one base, and one policy commit.** Any change to those
    revokes it. Check: one test per revoking event in the gate's truth table.
*   **G10. The merge-class path fails closed; the human path does not depend on it.** Missing,
    stale or malformed evidence means "not eligible", never "pass". A gate failure leaves a
    documented break-glass path that skips only the gate. Check: an error-path test for every
    failure, and a break-glass drill before the gate is required.
*   **G11. Every merge has a decision record** naming the head and base SHAs, the policy commit,
    the gate version, the basis (approver and approved SHA, or merge class and its predicate
    results), the auto-merge enabler and `merged_by`. Check: the post-merge audit finds a record
    for every merge.
*   **G12. Each merge class has a written precision target and a failure policy before it goes
    live.** Check: the class's entry in the policy file names its target, its definition of
    failure, and what happens after the first failure.

Reducing review time is the reason for this work, but it has no numeric goal here: G6 produces the
baseline, and the target is set against it.

## Non-Goals

*   **Merging by risk score.** There is no weighted or learned score in v1 or the later stages
    designed here. A score may come later, only after it passes a recall test on a corpus of
    known-bad pull requests.
*   **Anything that lowers risk from evidence the pull request produces.** Tests, coverage,
    mutation results, the pull request's own navigation index, model-lane output, and any
    author-written text may raise risk or block eligibility. They never make a pull request
    eligible.
*   **A GitHub App, a machine-user token, or a hosted service.** The organisation is understood to
    disallow Apps; this design does not depend on one.
*   **Merging inside this repository's code.** No command performs a merge. `ghclient.Writer` gains
    no merge, approve or push method.
*   **Organisation-level changes in v1.** No org ruleset, no required workflow, no `merge-gate`.
*   **Fork pull requests.** They never qualify for a merge class.
*   **Changes that cannot be reverted cleanly.** Database migrations, data backfills, external API
    contract changes and deletion of persisted state never qualify.
*   **Pull requests that touch what decides eligibility.** Policy, workflows, CODEOWNERS, rulesets,
    CI-influencing configuration, `.review/` and agent instruction files never qualify.
*   **Merge classes beyond docs-only, in this design.** Comment-only, formatting-only, generated
    code, lockfile bumps, test-only and inert-additive classes are each ruled out for now with a
    reason (see Alternatives Considered). Dependency patch bumps stay with Renovate.
*   **The test-suite audit, mutation testing and a test-trust factor.** Separate work.
*   **Labelling inside the review workspace.** The workspace stores nothing and makes no writes
    (ADR-0029, ADR-0032). Labels come from what reviewers already do on GitHub.
*   **A merge queue.** Not in v1 or the first gate stages.
*   **Python, bash and Rust signals that need analysis `reviewer` does not have.** Path-based
    signals apply to every language; deeper signals start with TypeScript.

## Terms

*   **Required status check.** A check that a ruleset says must pass before a pull request can
    merge. GitHub matches it by name.
*   **Commit status / check run.** Two GitHub APIs for reporting a result on a commit. Both attach
    to a commit SHA, not to a pull request.
*   **Ruleset.** GitHub's branch rules: required checks, required approvals, code-owner review,
    bypass actors. A repository ruleset can be changed by a repository admin; an organisation
    ruleset needs an org owner.
*   **Required workflow.** A ruleset rule that requires a named workflow file, at a pinned ref, to
    pass. It is the only native way, short of an App, to require a check that a pull request cannot
    imitate (unverified; see Open Questions).
*   **Native auto-merge.** GitHub's "merge when ready": once enabled on a pull request, GitHub
    merges it when every rule passes, as the account that enabled it.
*   **Merge class.** A named, rule-checked description of pull requests allowed to merge without a
    human, for example "docs-only". Called "category" in the brief; renamed because "category"
    already means a model-lane category (ADR-0022, ADR-0024).
*   **Qualify.** A pull request qualifies when it fits a merge class and no exclusion applies.
*   **Deny-by-default.** A pull request qualifies only if a merge class explicitly allows every one
    of its changed files; anything unrecognised means "does not qualify".
*   **Base-branch policy.** The merge-class rules as committed on the protected branch, read by SHA
    through the API, never from the pull request's head.
*   **Risk record.** What `reviewer risk` attaches to a pull request: one marker comment and its
    labels. Advisory only.
*   **Break glass.** A documented, audited way for named people to merge when the gate itself is
    broken, which skips only the gate.
*   **Kill switch.** A setting, changeable without a pull request, that turns every merge class off
    and leaves only the human path.
*   **Auto-merge enabler.** The account that turned on native auto-merge for a pull request.

## Open Questions

Each question names the work it holds up. Experiments are numbered as in the brief's sandbox list.

*   **Q1. Does GitHub enforce code-owner review at 0 required approvals, and do "changes
    requested" and stale-approval dismissal still apply?** (experiments 13, 15) Holds up G4 and G5.
    If not, v1 is `reviewer risk` and measurement only.
*   **Q2. Does any organisation ruleset already set a minimum approval count on these
    repositories?** Holds up G4. If so, the docs-only class needs an org owner too.
*   **Q3. Can adding a label be done with the review job's current `pull-requests: write`
    permission, and does it need a new `ghclient.Writer` method?** Holds up G1. A new writer method
    is recorded in an ADR.
*   **Q4. Is a pinned required workflow available on this plan, who can create and bypass it, and
    does it run on review events?** (experiments 2 to 5; decisions D-A, D-B) Holds up every later
    stage.
*   **Q5. What exactly counts as a human approval?** (decision D-E) Holds up all gate code. The
    brief's proposed rule is the starting point.
*   **Q6. What is each merge class's precision target, and what counts as a failure?** (decision
    D5) Holds up taking any class live under the gate. For docs-only under native settings, the
    proposal is "zero escapes plus a permanent audit sample".
*   **Q7. Where are decision records and outcome labels stored, and how is a record shown to be
    genuine** when every workflow posts as the same Actions identity? (decision D6) Holds up G11.
*   **Q8. Does a docs site deploy from any allowlisted directory?** Holds up G4 for that directory.
    Rendered docs are production content.

## Critical User Journeys (CUJs)

### CUJ 1: A reviewer picks what to read first (v1)

*   **Given**: `reviewer risk` is enabled in a repository, and three pull requests are open: a
    README fix, a change to `src/auth/session.ts`, and a lockfile bump.
*   **When**: The reviewer opens the pull request list.
*   **Then**: The auth change carries `risk/auth`, the lockfile bump carries `risk/deps`, and the
    README fix carries no `risk/` label. Each pull request has one risk comment listing the reasons
    behind its labels. Nothing on any pull request says it is safe.

### CUJ 2: A pull request edits the configuration that computes its own record (v1)

*   **Given**: An agent opens a pull request that changes `.review/risk.yaml` to drop the auth
    paths from the risk map, and also changes `src/auth/session.ts`.
*   **When**: `reviewer risk` runs on the pull request.
*   **Then**: The record is computed with the pull request's own risk map. The repository file can
    add paths but cannot remove built-in defaults, so `risk/auth` still appears for `src/auth/**`
    if a default covers it; paths only the repository file listed lose their label. The pull
    request also carries `risk/review-config`, and the comment says the record was computed from
    configuration this pull request changes.

### CUJ 3: An agent fixes a typo in the docs (v1, if Q1 holds)

*   **Given**: The repository's CODEOWNERS owns `*` and leaves `docs/guides/**` unowned; the
    ruleset requires code-owner review and 0 approvals; the agent enables auto-merge when it opens
    pull requests.
*   **When**: The agent opens a pull request that modifies only `docs/guides/setup.md`, and CI
    passes.
*   **Then**: GitHub merges it with no human approval. If the same pull request also touched
    `src/index.ts`, GitHub would wait for a code-owner approval.

### CUJ 4: Someone adds a new top-level directory without an owner (v1)

*   **Given**: The ownership check runs in CI.
*   **When**: A pull request adds `scripts/`, and no CODEOWNERS rule other than the allowlist
    matches it.
*   **Then**: `reviewer owners check` fails the CI job and names `scripts/` as unowned. The pull
    request cannot merge until CODEOWNERS covers it.

### CUJ 5: A human approves, then someone pushes (later stages)

*   **Given**: `merge-gate` is required, approvals are 0, and auto-merge is enabled.
*   **When**: A writer approves head commit `A`; the author then pushes commit `B`.
*   **Then**: The gate run for `B` finds no approval on `B` and does not pass. The approval on `A`
    is not carried over. The pull request waits, and the gate's comment says "waiting: no approval
    on the current head commit".

### CUJ 6: A docs-only pull request merges under the gate (later stages)

*   **Given**: The base-branch policy enables the `docs-only` class; the kill switch is off.
*   **When**: A pull request whose files are all added or modified under the class's allowlist
    reaches green CI.
*   **Then**: The gate passes with basis `class: docs-only` and records the policy commit.
    Auto-merge merges. The post-merge audit finds the gate run for the merged head SHA, produced by
    the pinned workflow file, and records the merge.

### CUJ 7: The gate is broken (later stages)

*   **Given**: A bad release of `merge-gate` fails on every run.
*   **When**: Human-approved pull requests stop merging.
*   **Then**: The gate's job summary links the break-glass runbook. A named bypass holder merges an
    urgent pull request through the gate ruleset's bypass, which skips only `merge-gate`; CI is
    still required. The bypass shows in the post-merge audit. The team pins the previous gate
    version.

### CUJ 8: The kill switch is turned on (later stages)

*   **Given**: A docs-only merge caused a production problem.
*   **When**: A maintainer sets the repository variable `MERGE_GATE_CLASSES` to `off`.
*   **Then**: Every later gate run ignores merge classes and passes only on human approval. Passes
    already posted are revoked by the sweep (see "Revocation"). The incident is recorded as a
    failure against `docs-only`, which stays off until the policy says otherwise.

## Technical Plan (Architecture)

The work has four parts. Parts 1 to 3 are v1. Each part can ship, or be dropped, without the
others.

```text
                 pull request (untrusted: diff, config, workflows, text)
                                     |
       +-----------------------------+-------------------------------+
       |                             |                               |
 review job (existing)        risk job (v1, Part 1)         merge-gate (later, Part 4)
 reviewer --pr N              reviewer risk --pr N          pinned required workflow
 inline comments + summary    needs: review; reads markers, runs cmd/merge-gate
       |                      paths, CODEOWNERS, history,   reads PR via API, policy
       |                      reach (base-branch index)     
       +---- rv: markers ---->|                             at base SHA, reviews,
                              writes <!-- rv:risk -->       compare(base...head)
                              + risk/ labels                result = job conclusion
                              advisory only, exits 0                 |
                                                            required by ruleset
 native GitHub (v1, Part 2)                                          |
 CODEOWNERS owns * except allowlist                         native auto-merge
 ruleset: 0 approvals + code-owner review                   merges as the enabler
 -> code-owner review decides docs-only

 measurement (v1, Part 3): reviewer outcomes, read-only, prints a table
```

### Part 1: `reviewer risk` (v1)

**Where it runs.** A new subcommand in `cmd/reviewer`, added to the subcommand table beside
`metrics` and `baseline` (`cmd/reviewer/flags.go:101`). It runs as its own job, `risk`, in the
review workflow, with `needs: review` so the review markers exist before it reads them, and its own
permissions: `contents: read`, `pull-requests: write` and `actions: read`. The review job's
permissions do not change. The composite action gains an input `mode` (`review`, the default, or
`risk`), so the risk job reuses the same install-and-checksum steps, and existing installs change
only when they add the job. Like every review path, it exits 0 whatever happens (ADR-0009). An error
goes to the job log and, if the API is reachable, into the risk comment.

**Inputs.** All are read from the pull request's checkout or through the API with the job's token.
In v1 none of them is trusted, which is why the record only adds attention (G2).

*   **Changed files** from `ListFiles`, using both `filename` and `previous_filename`, so a rename
    out of a sensitive path is seen at both ends (brief, Blocker 10).
*   **The risk map**: built-in defaults compiled into the binary (auth, permissions, crypto,
    payments, migrations, infrastructure as code, CI configuration, lockfiles and manifests,
    `.review/`, agent instruction files) plus repository additions from `.review/risk.yaml`. The
    repository file can add paths and never remove a default. It is a separate file, not a new key
    in `.review/config.yaml`, because the config decoder rejects unknown fields
    (`internal/config/load.go:70-73`): a new key would make every older pinned binary reject the
    whole config and skip review.
*   **Review findings** from the `rv:` markers the review step posted on this pull request
    (`internal/fingerprint`), counted by rule id. If the review step posted nothing or did not run,
    the record says "findings not available", not "no findings".
*   **Owner groups** crossed, from parsing CODEOWNERS at the pull request's head.
*   **File history**: for at most the 20 changed files with the most lines changed, whether a
    commit on the base branch in the last 180 days reverted an earlier change to that file. The cap
    puts a ceiling on the job's API cost.
*   **Reach**: for symbols defined on lines the pull request modifies or removes in existing
    TypeScript files, how many other files on the base branch reference them. It is read from the
    `review-index` artifact that `review-index.yml` uploads on every push (ADR-0034), taken from
    the `push` run on the base branch whose head SHA is the merge base, found with
    `ghclient.ListWorkflowRuns` and `ListRunArtifacts`, and queried with the reader in
    `internal/workspace/navigation`. Downloading an artifact needs `actions: read`, which is why
    risk is its own job. New files have no base-branch references. If the repository has no index
    workflow, no run for that SHA, or a non-TypeScript change, the record says "reach not
    available".

**Outputs.** One sticky issue comment with the marker `<!-- rv:risk -->`, updated in place on each
push, and labels from a closed set:

| Label | Set when |
| --- | --- |
| `risk/auth` | a changed path matches the auth, permissions or crypto entries of the risk map |
| `risk/data` | migrations, schema files or persisted-state paths |
| `risk/ci` | CI configuration or workflow files |
| `risk/deps` | lockfiles or dependency manifests |
| `risk/infra` | infrastructure-as-code paths |
| `risk/wide` | 3 or more CODEOWNERS groups, or more than 40 files |
| `risk/reach` | a changed symbol is referenced from 10 or more other files on the base branch |
| `risk/history` | a changed file had a revert on the base branch in the last 180 days |
| `risk/findings` | the review step posted a high-confidence finding |
| `risk/review-config` | the pull request changes `.review/` or `.github/` |

The tool removes only `risk/` labels it added earlier, as listed in its own previous comment, so it
never removes a label a person set. The comment states that risk is advisory and does not decide
merging (brief, Major 27).

**New writer verbs.** `AddLabels` and `RemoveLabel` on `ghclient.Writer`, which today has four
write methods and one read (`internal/ghclient/write.go`). They need no permission beyond
`pull-requests: write` (unverified, Q3). ADR-0035 records these verbs and the risk job's
permissions, because ADR-0018 lists what the tool can do.

### Part 2: docs-only by native configuration (v1, conditional on Q1 and Q2)

No code decides docs-only merges in v1. GitHub does, from two pieces of repository configuration:

*   **CODEOWNERS** with `*` owned by a team as its first rule, CODEOWNERS owning itself, and the
    allowlist directories listed afterwards with no owner. The allowlist follows the brief's
    Major 24: named directories only (for example `docs/**` minus `docs/adr/**`), never a file
    extension, never agent instruction files, `.github/**`, `.review/**`, MDX, or markdown that code
    reads.
*   **The repository ruleset** on `main`: 0 required approvals, "require review from code owners",
    existing CI checks required.

This repository supplies two pieces of support:

*   **`reviewer owners check`**, a check command in the style of `reviewer rules test`. It lists
    tracked files in the checkout, matches them against CODEOWNERS, and exits 1 if any file has no
    owner and is not under an allowlisted directory. It is a check, not a review, so the exit-1
    precedent of `rules test` applies (`cmd/reviewer/flags.go:230-239`), not ADR-0009.
*   **`docs/merge-classes.md`**, a setup guide: the CODEOWNERS template, the ruleset settings, the
    sandbox steps that answer Q1 and Q2, and the rollback (set required approvals back to 1).

`reviewer init` does not write CODEOWNERS or rulesets. The brief found the installer cannot do this
safely (Major 25): it cannot know the teams, and a wrong CODEOWNERS file removes review without any
visible sign.

### Part 3: measurement (v1)

A new `reviewer outcomes` subcommand, read-only, following the `metrics` pattern
(`cmd/reviewer/metrics.go`). It reads merged pull requests over a period through the API and
prints, as markdown or CSV:

*   time from "ready for review" to first approval (median and 90th percentile);
*   how many merged pull requests had every file under a given allowlist;
*   an outcome label per pull request:
    *   **reverted**: a later merged pull request reverts it, recognised by GitHub's revert title
        and body convention;
    *   **hotfixed**: a later merged pull request within 14 days changes lines this one added,
        found by comparing patch line ranges;
    *   **clean**: neither.
*   a first-review label per pull request, describing the revision a human first reviewed rather
    than the one that merged, so problems a reviewer caught count as evidence:
    *   **changed-after-review**: the first review was `CHANGES_REQUESTED`, or left review
        comments, and later commits changed lines in the files it commented on;
    *   **unchanged-after-review**: the merged head equals the commit the first review was made
        on, or later commits touched none of the commented files;
    *   **no-review**: merged with no review.

    A pull request labelled `changed-after-review` is treated as "would have been wrong if merged
    unreviewed" when a merge class is tested against history.

All three heuristics are checked against a hand-audited sample before anyone relies on them. The command
writes nothing to GitHub. Results go into a trials file without organisation or repository names,
as `docs/designs/trials/` already does. The first candidate count is taken before this command
exists, with a one-off `gh pr list` query (see the Detailed Implementation Plan).

### Part 4: `merge-gate` (later stages, after Q4)

**Program.** `cmd/merge-gate`, a separate `main` with its own release artifact. It fails closed: the
only exit 0 is an explicit pass, and every error exits non-zero. An import-allowlist test proves it
imports only `ghclient`, `diff`, its own `policy` package and the standard library, and never the
review lanes, the model lane or plugin hosting.

**How it is required.** A workflow file `merge-gate.yml`, required through a ruleset "required
workflow" rule pinned to a protected ref. Its job installs the gate binary by version and checksum
from an action pinned by commit SHA. It never builds the gate from the pull request and never checks
out pull request code. The required check is that workflow's result, which no other workflow or
token can produce (unverified until experiments 1 to 3 pass; if they fail, Part 4 stops).

**Its commit status.** The gate also posts one commit status named `merge-gate` on the head SHA,
through its own one-method writer, `ghclient.StatusWriter`. The status is information, not the
rule: the ruleset requires the pinned workflow, never the status, so a forged status changes
nothing (G7). It exists because a job can only end as success or failure, and a person or agent
needs to tell "waiting for approval" from "the gate is broken" (brief, Blocker 9):

| Status | Description carries |
| --- | --- |
| `pending` | a reason code, for example `waiting: no approval on the current head commit` |
| `success` | the basis: `approved by <login> at <sha>` or `class: docs-only, policy <sha>` |
| `failure` | `gate error`, the run link, and the break-glass pointer |

If the status and the workflow result ever disagree, the workflow result decides, and the
post-merge audit flags the disagreement. The gate workflow needs `statuses: write` for this,
and ADR-0018 is superseded to record it (PR-63).

**What triggers it.** Events are a doorbell, not an input. Whatever event starts a run, the gate
takes only the pull request number from the payload and fetches everything else through the API. If
the payload's head SHA differs from the API's, it exits as "waiting: re-run needed" without passing.

**The decision**, in order:

1. Fetch the pull request. Not eligible unless the base is the protected branch, the head
   repository is the base repository (no forks), and the head SHA matches the run's SHA.
2. **Human path.** Pass if some reviewer's latest review is `APPROVED` with `commit_id` equal to the
   head SHA, that reviewer has write permission now, is not a bot, is not the author and did not
   push to the pull request, and no writer has an outstanding "changes requested". The exact rule
   is decision D-E (Q5); this is the starting point.
3. **Class path**, only if the human path did not pass and the kill switch is off:
    *   read `.review/merge-policy.yaml` at the base branch's current head SHA through the contents
        API, with its own strict decoder and a `version` field;
    *   build the change set from `compare(base_sha...head_sha)`, never from `/pulls/{n}/files`
        (brief, Blocker 5); treat a missing patch, truncation, a mode change, a removal, a rename or
        a copy as not eligible;
    *   apply the never-qualify list compiled into the gate, which no policy can switch off, then
        the class predicates from the policy file;
    *   pass only if one enabled class allows every changed file.
4. Post the `merge-gate` status, write the decision record, and exit: 0 on pass; 2 for
   "waiting", with a reason code; 1 for an error, with a run link and the break-glass pointer.

**Policy file.** `.review/merge-policy.yaml`, separate from `.review/config.yaml` for the same
decoder reason as `.review/risk.yaml`. It lists classes, each with `enabled`, its allowlist, its
precision target, its definition of failure and its failure policy (G12). A pull request that
changes it never qualifies, so a human reviews every policy change.

**Revocation.** A pass is bound to the head SHA, the base SHA and the policy SHA. The ruleset
requires branches to be up to date before merging, so any commit to `main`, including a policy
change, forces a new head and a new evaluation. The kill switch (`MERGE_GATE_CLASSES=off`, a
repository variable read at evaluation) needs one more step: a sweep job, started by the maintainer
who flips the variable, re-runs the gate for open pull requests that passed by class. Until it
finishes, a pass already posted can still merge. That window is a stated risk.

**Decision record.** The gate writes its record to its own run's job summary and to a sticky comment
`<!-- rv:gate -->`. The comment is for people and is not proof, because any workflow can post as the
same identity (Q7). The proof is a scheduled post-merge audit job on `main`. For each merged pull
request it finds the `merge-gate` run for the merged head SHA by workflow file path and run ID, and
flags any merge without a passing run or a recorded bypass.

**Rollout order.** At no step is review weaker than today (brief, Blocker 8):

1. Shadow: the gate runs as a non-required workflow named `merge-gate (shadow)`; native approvals
   unchanged.
2. Required alongside native approvals, with classes off.
3. Native approval rule removed; the gate is the approval rule.
4. One class on, in one repository.

Rollback runs in reverse, and its first step is always re-adding the native approval rule.

## Alternatives Considered

"Rejected by author" marks a choice the author made during the brainstorm or this draft. "Not
chosen" marks an option a review lens raised that this design does not take, with the reason; those
can be reopened. "Open" marks options that wait on a sandbox result.

### How the merge decision is made

#### A1: A risk score with configurable thresholds

*   **Pros**: Covers every pull request, not only those that fit a class; one number per pull
    request; the original ask.
*   **Cons**: About 20 signals with hand-set weights have no calibration, so a threshold has no
    meaning; showing a miss rate under 1% needs about 300 clean outcomes per class, which is months
    at this volume; it repeats the combined score ADR-0017 rejected; a reviewer cannot tell why a
    pull request scored 0.31.
*   **Reason for Rejection**: Rejected by author: merge classes first, checked by rules from inputs
    the pull request cannot change. A score may return after it passes a recall test on known-bad
    pull requests.

#### A2: A learned model from the first version

*   **Pros**: Could find patterns no rule names.
*   **Cons**: No labelled history exists; recall cannot be validated at this volume; the review
    workspace design already deferred it for that reason (`docs/designs/review-workspace.md:530`).
*   **Reason for Rejection**: Rejected by author, with A1.

#### A3: Evidence produced by the pull request lowering risk

*   **Pros**: Tests, coverage and the model lane carry real information about a change.
*   **Cons**: They run the pull request's code or read its files, so the party being judged produces
    them. Author-written text (descriptions, commit messages, comments, agent session logs) can
    carry prompt injection.
*   **Reason for Rejection**: Rejected by author: such evidence may raise risk or block
    eligibility, never grant it. The model lane may veto and never approve.

#### A4: Settings that lower the fixed floor, or auto-merging irreversible or self-editing changes

*   **Pros**: Flexibility per repository.
*   **Cons**: Recovery is a revert plus a deploy, so an irreversible change has no recovery; a pull
    request that edits policy, workflows, CODEOWNERS or rulesets could edit its own gate.
*   **Reason for Rejection**: Rejected by author: the never-qualify list is compiled into the gate.

### Who or what merges

#### B1: A merge-only GitHub App run from a trusted workflow

*   **Pros**: A distinct identity that can be a ruleset bypass actor and can be told apart from
    other workflows; the only route the brief found that fully answers forgery.
*   **Cons**: Understood to be disallowed by organisation policy (not yet confirmed); "merge-only"
    is not a GitHub permission, since merging needs `contents: write`; a key in a repository secret
    can be read by workflows on agent branches.
*   **Reason for Rejection**: Rejected by author as the default: design for no App. An App stays a
    possible later upgrade if policy allows it.

#### B2: A machine-user token

*   **Pros**: Works without an App.
*   **Cons**: Broader than an App's scoping; a shared credential that agents could reach.
*   **Reason for Rejection**: Rejected by author: the route needs neither an App nor a machine user.

#### B3: A trusted workflow merges with `GITHUB_TOKEN`

*   **Pros**: No person's token on the merge path; a compare-and-swap on `sha` closes the
    approve-then-push race.
*   **Cons**: Pushes made with `GITHUB_TOKEN` do not start other workflows, so deploys would need a
    dispatch (unverified); needs `contents: write`; contradicts ADR-0018 and
    `internal/ghclient/write.go`, which say nothing in this tool can merge.
*   **Reason for Rejection**: Not chosen (raised by the inventive-catalyst lens): native auto-merge
    keeps merging out of this repository's code.

#### B4: A hosted service, Mergify, Kodiak, Graphite, Aviator, or a vendor review bot

*   **Pros**: Existing products; some offer queues and condition languages.
*   **Cons**: A hosted service contradicts ADR-0008; Mergify and Kodiak are Apps; Graphite and
    Aviator address stacking and queueing, not risk; Copilot review is not permitted
    (`docs/designs/review-workspace.md:520`); each sends code to a vendor.
*   **Reason for Rejection**: Rejected by author for the hosted service (ADR-0008). The products are
    not chosen (raised by the zoom-out lens) for the reasons above.

#### B5: Renovate or Dependabot auto-merge for dependency patch bumps

*   **Pros**: Mature, runs in Actions, narrow rules with a minimum release age.
*   **Cons**: Covers only dependency bumps.
*   **Reason for Rejection**: Not rejected. Dependency patch bumps stay with these tools and are not
    a merge class here.

### What produces `merge-gate`

#### C1: A `pull_request_target`, `workflow_run` or `pull_request_review` workflow

*   **Pros**: Available today with no organisation change.
*   **Cons**: A required check is matched by name, so any workflow a pull request adds, or any
    token with write access, can post a check with the same name (unverified, experiment 1).
    `pull_request_target` also runs whatever workflow sits on an attacker-created base branch
    (ADR-0018 says "Never" in that direction); `pull_request_review` probably runs the pull
    request's own workflow file.
*   **Reason for Rejection**: Not chosen (raised by every round-2 lens). These events may wake the
    gate; they never produce it.

#### C2: Allow GitHub Actions to approve pull requests, with a trusted approving workflow

*   **Pros**: Keeps the native approval rule in place.
*   **Cons**: Any same-repository workflow could approve (unverified), which moves the forgery
    problem to the approval layer.
*   **Reason for Rejection**: Not chosen (raised by the inventive-catalyst lens).

### Where the code lives

#### D1: The merge decision in a separate repository or consumer

*   **Pros**: The shape ADR-0009 describes for any gate; keeps every existing ADR true.
*   **Cons**: Another repository and release to keep in step with `reviewer`'s packages.
*   **Reason for Rejection**: Rejected by author: build it in this repository.

#### D2: `reviewer risk` as a reporter inside the main review run

*   **Pros**: Reuses the diff and findings already in memory; no extra job time; `reviewer --staged`
    would show risk locally.
*   **Cons**: Shares a process and configuration with review, so a risk bug can delay or break
    review comments; cannot run risk without review.
*   **Reason for Rejection**: Rejected by author: a separate subcommand, run as its own job, reading review
    findings from the comment markers.

#### D3: `merge-gate` as a `reviewer gate` subcommand

*   **Pros**: One binary and one release artifact.
*   **Cons**: Two opposite exit policies in one `main` (exit 0 always, against fail closed); every
    review release moves the merge path.
*   **Reason for Rejection**: Rejected by author: a separate `cmd/merge-gate` program with its own
    artifact.

#### D4: Merge-class policy compiled into the gate

*   **Pros**: Nothing to fetch or parse at decision time; the pin fixes the policy.
*   **Cons**: Every allowlist change is a gate release and a pin bump in each repository;
    repositories with different layouts turn the constants into a per-repository table.
*   **Reason for Rejection**: Rejected by author: a per-repository `.review/merge-policy.yaml` read
    from the base branch by SHA.

#### D5: New keys in `.review/config.yaml` for the risk map or the policy

*   **Pros**: One configuration file.
*   **Cons**: The decoder rejects unknown fields (`internal/config/load.go:70-73`), so older pinned
    binaries would reject the file and skip review; the file is read from the pull request's head.
*   **Reason for Rejection**: Not chosen (raised by the codebase and readiness lenses): separate
    files.

### Rollout and storage

#### E1: A merge queue

*   **Pros**: Tests against current `main`; batches merges.
*   **Cons**: The group commit is never classified; `merge_group` may run the group's own workflow
    copy (unverified).
*   **Reason for Rejection**: Not chosen for v1 or the first gate stages. "Require branches to be up
    to date" is used instead, which classifies the exact tree that lands.

#### E2: Replacing native approvals with the gate as the first release

*   **Pros**: Proves the plumbing quickly.
*   **Cons**: Replaces a tested native rule with new code; the instant the approval rule goes, every
    open pull request with auto-merge on and green CI merges unreviewed.
*   **Reason for Rejection**: Not chosen (raised by the readiness lens): shadow first, then required
    alongside native approvals.

#### E3: Check runs, or the review workspace, as the store of record or labels

*   **Pros**: Check runs show in the pull request UI; the workspace already shows the diff.
*   **Cons**: Check runs expire (ADR-0016) and need `checks: write`; the workspace stores nothing and
    makes no writes (ADR-0029, ADR-0032).
*   **Reason for Rejection**: Not chosen (raised by the codebase and structural lenses): records go
    in job summaries and comments, with the post-merge audit as proof; labels come from what
    reviewers already do on GitHub.

#### E4: A short shadow period proving a 1% miss rate

*   **Pros**: Starts merging sooner.
*   **Cons**: Zero failures in n cases bounds the rate only below about 3/n, so 1% needs about 300
    clean cases per class, which is months at this volume.
*   **Reason for Rejection**: Not chosen. Docs-only rests on rules plus "zero escapes and a permanent
    audit sample", not on a rate claim (Q6).

--------------------------------------------------------------------------------

## Non-Functional Requirements

*   **Security.**
    *   v1 adds no permission to the review job: `contents: read` and `pull-requests: write` stay
        as they are. The new risk job holds those two plus `actions: read`, for the navigation
        index artifact (Q3 confirms that labels fit `pull-requests: write`).
    *   `merge-gate` never checks out, builds or runs pull request content, and reads head files
        only as data from `compare(base_sha...head_sha)`.
    *   The gate binary is installed by version and checksum from an action pinned by commit SHA.
        Moving a tag cannot change the merge path.
    *   No model call happens anywhere on the merge path.
*   **API cost.**
    *   `reviewer risk`: at most 1 call for files, 2 for comments, 1 for CODEOWNERS (local file),
        20 for file history, 2 to find the index run and artifact plus one artifact download,
        and 1 to 10 for labels per run.
    *   `reviewer outcomes`: one page of pull requests per 100, plus reviews and files per pull
        request. It prints what it read when it hits a rate limit, as `metrics` does.
    *   `merge-gate`: about 6 calls per evaluation (pull request, reviews, permissions per approver,
        policy file, compare, comment).
*   **Latency.** `reviewer risk` adds under 30 seconds to the review job. `merge-gate` finishes in
    under 60 seconds, so auto-merge is not held up.
*   **Rollout and rollback.**
    *   `reviewer risk`: turn the action input off.
    *   Docs-only: set required approvals back to 1.
    *   `merge-gate`: the reverse rollout order, re-adding native approvals first.
    *   Each runbook is in `docs/merge-classes.md` and is rehearsed before the stage it protects.
*   **Compatibility.** Older pinned `reviewer` binaries ignore `.review/risk.yaml` and
    `.review/merge-policy.yaml`, because neither is a key in `.review/config.yaml`.

--------------------------------------------------------------------------------

### Additional Resources

*   `docs/designs/briefs/auto-merge-concepts_brief.md`: the brainstorm brief, the round-2 findings
    cited here as "brief, Blocker N" and "brief, Major N", and the 19 sandbox experiments.
*   `docs/designs/briefs/auto-merge-hypothesis.md`: the hypothesis both review rounds worked from.
*   ADR-0004, 0008, 0009, 0016, 0017, 0018, 0022, 0023, 0024, 0026, 0029, 0032, 0034.
*   `docs/designs/review-workspace.md`: the design this one follows in form, and the earlier
    deferral of a risk classifier (line 530).

### Future Improvements

*   **More merge classes** under the gate, each in shadow first: comment-only (needs both sides of
    the patch and directive detection), generated code (needs regeneration in a trusted job),
    inert-additive (needs reach from the base-branch navigation index), test-only (after the
    test-suite audit).
*   **Reach for Python, bash and Rust**: v1 reach covers TypeScript only, because the index
    ADR-0034 builds is TypeScript.
*   **A scored classifier**, only after it passes a recall test on a corpus of known-bad pull
    requests built from `reviewer outcomes` data.
*   **A GitHub App** in place of the required workflow, if organisation policy allows it.

--------------------------------------------------------------------------------

## Detailed Implementation Plan

The plan continues the numbering of `docs/implementation-plan.md` (which ends at PR-54) and lands as
a stack of pull requests on top of this design. Every pull request leaves `make check` green and
carries the ADR that its decision needs, in the same pull request, as `docs/adr/README.md` requires.
PR-55 to PR-60 are v1. PR-61 to PR-66 start only when Q4 is answered yes.

### Stack order

**Before any code**, a person runs a one-off query in each repository and records the result:
`gh pr list --state merged --limit 500 --json number,files,reviews,createdAt,mergedAt`, then
counts the pull requests in the last 90 days whose files are all added or modified under the
proposed docs allowlist, and their time to first approval. Today's ruleset is written down at the
same time. This answers "how much of the queue is docs-only" in an afternoon. If the share is
negligible, Part 2 and PR-59 can be dropped before anything is built. PR-55 then makes the
measurement repeatable and adds the outcome labels.

| PR | Content | Depends on | Gate to start |
| --- | --- | --- | --- |
| PR-55 | `reviewer outcomes` and the reads it needs | — | none |
| PR-56 | Label verbs on `ghclient.Writer`; ADR-0035 | — | Q3 answered in the sandbox |
| PR-57 | `internal/codeowners` and `internal/risk` (map, signals, labels) | — | none |
| PR-58 | `reviewer risk` subcommand, reach, action `mode` input, installer risk job, docs | PR-56, PR-57 | none |
| PR-59 | `reviewer owners check`; ADR-0036; `docs/merge-classes.md` | PR-57 | none |
| PR-60 | Trials file: baseline, candidate count, sandbox results for Q1, Q2, Q3 | PR-55 | a person runs the commands |
| PR-61 | Gate reads on `*ghclient.REST` | — | Q4 yes |
| PR-62 | `internal/policy`: merge-policy decoder and class predicates | — | Q4 yes |
| PR-63 | `cmd/merge-gate`, import-allowlist test; ADR-0037 to ADR-0040 | PR-61, PR-62 | Q4 and Q5 |
| PR-64 | Gate action, workflow template, release artifact | PR-63 | Q4 |
| PR-65 | Post-merge audit and kill-switch sweep | PR-63 | Q7 |
| PR-66 | Runbooks: rollout, rollback, break glass, kill switch | PR-64, PR-65 | — |

### Target Files to Create/Modify

#### PR-55: measurement

##### 1. `cmd/reviewer/outcomes.go` (Create)

*   **Type / Scope**: Go, subcommand.
*   **Proposed Changes**: `func outcomes(ctx, o options, stdout, stderr io.Writer, getenv) error`,
    modelled on `metrics`. Flags: `--since` (default `90d`, parsed by the existing `parseSince`),
    `--allowlist` (repeatable directory glob), `--format markdown|csv`, `--repo`. For each merged
    pull request in the period it computes: time from `ready_for_review` (or creation, if never a
    draft) to the first `APPROVED` review; whether every file sits under the allowlist, using
    `filename` and `previous_filename`, with removals and renames not counting as docs-only; and an
    outcome of `reverted`, `hotfixed` or `clean`. Detection:
    *   `reverted`: a later merged pull request whose title starts `Revert "` and whose body
        contains `Reverts <owner>/<repo>#<n>`.
    *   `hotfixed`: a later merged pull request within 14 days whose patch hunks overlap line
        ranges this pull request added in the same file. Ranges are compared on the post-merge
        line numbers each patch reports, which is approximate, and the output says so.

    It also computes the first-review label (`changed-after-review`, `unchanged-after-review`,
    `no-review`) from the earliest review's `commit_id` and state, that review's comments
    (`ListReviewComments`), and `compare(first_review_commit...merged_head)`.

    It prints a summary table (median and 90th percentile time to first approval, candidate count,
    outcome counts) and, with `--format csv`, one row per pull request. On a partial read it prints
    what it has and says what it could not read, as `metrics` does. It exits 0 on success and
    returns ordinary errors otherwise; it is a measurement, not a review or a check.

##### 2. `cmd/reviewer/outcomes_test.go` (Create)

*   **Type / Scope**: Go unit tests against a fake `ghclient.Client`.
*   **Proposed Changes**: Cases: a pull request with a revert; a revert of a different pull request
    with a similar title (must not match); a hotfix that overlaps and one that touches adjacent
    lines only; a docs-only pull request; a rename out of `.github/` into `docs/` (not docs-only);
    a draft pull request (time measured from ready-for-review); a first review with changes
    requested followed by commits to the commented file (`changed-after-review`); a first review
    with comments followed by commits to other files only (`unchanged-after-review`); a merge with
    no review (`no-review`); a rate-limit error midway (partial output, with the gap stated).

##### 3. `cmd/reviewer/flags.go` and `cmd/reviewer/main.go` (Modify)

*   **Type / Scope**: Go, CLI wiring.
*   **Proposed Changes**: Add `outcomes` to `subcommands`, add `--allowlist` to `options`, dispatch
    in `run`, and extend the usage text.

##### 4. `internal/ghclient/client.go` and `internal/ghclient/client_test.go` (Modify)

*   **Type / Scope**: Go, API client.
*   **Proposed Changes**: Add to `Client`: `ListReviews(ctx, repo, number) ([]Review, error)`,
    where `Review` carries `ID`, `User` (login and type), `State`, `CommitID` and `SubmittedAt`.
    Add `PreviousFilename` to `ChangedFile` if it is not already decoded. Add `ReadyForReviewAt` to
    `PullRequest` from the timeline, or fall back to `CreatedAt`. Tests use the existing
    `httptest` pattern.

##### 5. `README.md` and `CHANGELOG.md` (Modify)

*   **Proposed Changes**: Document `reviewer outcomes` in "Use it"; add a CHANGELOG entry.

#### PR-56: label verbs

##### 6. `internal/ghclient/write.go` and `internal/ghclient/client_test.go` (Modify)

*   **Proposed Changes**: Add `AddLabels(ctx, repo, number, labels []string) error`
    (`POST /repos/{o}/{r}/issues/{n}/labels`) and `RemoveLabel(ctx, repo, number, label string)
    error` (`DELETE .../labels/{name}`; a 404 is not an error). Update the package comment so the
    list of verbs stays accurate: post an inline comment, post or update a conversation comment,
    resolve a thread this tool created, add or remove a label. It still contains no merge, approve,
    close or push.

##### 7. `docs/adr/0035-advisory-risk-record.md` (Create) and `docs/adr/README.md` (Modify)

*   **Proposed Changes**: ADR-0035, "Attach an advisory risk record that only adds attention". It
    records: the comment-plus-labels shape; the closed `risk/` label set; that no label certifies
    safety; that the record is computed from untrusted input in v1 and says so; the two new writer
    verbs and that they need no new permission; and that `reviewer risk` runs as its own job,
    `risk`, holding `contents: read`, `pull-requests: write` and `actions: read` (the last for the
    navigation index artifact), so the review job's permissions stay as ADR-0018 states. It
    supersedes nothing: ADR-0018's permission sentence is about the review job, and its "cannot
    merge, approve, close or push" sentence stays true. Add the index row.

#### PR-57: risk signals

##### 8. `internal/codeowners/codeowners.go` and `codeowners_test.go` (Create)

*   **Type / Scope**: Go library, no new dependency.
*   **Proposed Changes**: `Parse(r io.Reader) (File, error)` and
    `(File) Owners(path string) (owners []string, matched bool)`, implementing GitHub's CODEOWNERS
    matching: last match wins, gitignore-style patterns, a pattern with no owners meaning unowned,
    and comments. Tests are a table of path and pattern cases taken from GitHub's documented
    examples, including leading `/`, trailing `/`, `**` and `*` at different depths, plus a case
    where a later unowned rule overrides an earlier owned one.

##### 9. `internal/risk/riskmap.go`, `signals.go`, `labels.go` and tests (Create)

*   **Type / Scope**: Go library.
*   **Proposed Changes**:
    *   `riskmap.go`: the built-in defaults as a table of `{Label, Patterns}`, and
        `Load(fsys fs.FS) (Map, error)` reading `.review/risk.yaml` with a strict decoder and a
        `version: 1` field. The repository file may add patterns to an existing label and may not
        remove a default or define a new label; an attempt is an error, reported in the comment.
    *   `signals.go`: `Compute(in Input) Record`, a pure function. `Input` holds the changed files
        (with previous paths), the risk map, the parsed CODEOWNERS, the rule ids of review
        markers, per-file revert flags, and per-symbol reference counts (or "not available").
        `Record` holds the labels, and for each label the paths
        or rule ids behind it, plus `ConfigChangedByPR bool`.
    *   `labels.go`: the closed label set as constants, and a test that fails if a label name
        contains `safe`, `low` or `ok` (G2).

    Tests: one per label, renames at both ends, the 40-file and 3-owner thresholds, the 10-file
    reach threshold, reach not available, a repository file trying to remove a default, and an
    empty diff.

#### PR-58: the `reviewer risk` command

##### 10. `cmd/reviewer/risk.go` and `risk_test.go` (Create)

*   **Proposed Changes**: `func risk(...) error`. It reads the pull request through `ghclient`,
    reads the risk map and CODEOWNERS from the checkout, reads `rv:` markers from this tool's
    comments, and fetches revert history for at most 20 files (commits on the base branch touching
    each file in 180 days, matched by the `Revert "` subject convention). For reach it finds the
    `review-index.yml` `push` run whose head SHA is the merge base (`ListWorkflowRuns`), downloads
    its `review-index` artifact (`ListRunArtifacts` and the existing artifact stream), loads it with
    `internal/workspace/navigation`, and counts referencing files for each symbol defined on a
    modified or removed line. It then calls
    `risk.Compute`, renders the comment and syncs labels: add the missing ones, and remove only
    `risk/` labels listed in its own previous comment. `--dry-run` prints instead of posting. Every
    error path returns through the existing exit-0 handling; the test walks each one (API error on
    each call, malformed `.review/risk.yaml`, missing CODEOWNERS, no review markers, no index
    run, a corrupt index artifact, `actions: read` missing) and asserts
    exit 0 and a stated reason.

##### 11. `internal/reporters/risk.go` and test (Create)

*   **Proposed Changes**: Renders the `<!-- rv:risk -->` comment: a one-line statement that the
    record is advisory and does not decide merging; one line per label with its reasons; a
    "computed from configuration this pull request changes" line when `ConfigChangedByPR`;
    "findings not available" when there were no markers; and a hidden list of labels this tool
    set.

##### 12. `action.yml` (Modify)

*   **Proposed Changes**: New input `mode` (default `'review'`). With `'risk'`, the "Review" step
    is replaced by one that runs `reviewer risk --pr "${PR_NUMBER}" --base "origin/${BASE_REF}"`.
    The history check and the install-and-checksum steps are shared.

##### 13. `internal/installer/templates/review.yml.tmpl`, `internal/installer/plan.go` and tests (Modify)

*   **Proposed Changes**: `reviewer init` writes a second job, `risk`, commented out, below the
    review job: `needs: review`, its own `permissions:` block (`contents: read`,
    `pull-requests: write`, `actions: read`), the same checkout and the action with `mode: risk`. A
    comment explains what it does and that reach needs `review-index.yml`. Enabling it means
    uncommenting one block. The installer golden tests are updated.

##### 14. `cmd/reviewer/flags.go`, `main.go`, `README.md`, `docs/architecture.md`, `CHANGELOG.md` (Modify)

*   **Proposed Changes**: Wiring and usage text; a README section "Risk labels"; an architecture
    note that `reviewer risk` is a separate, advisory step that reads the review step's markers.

#### PR-59: ownership check and docs-only setup

##### 15. `cmd/reviewer/owners.go` and `owners_test.go` (Create)

*   **Proposed Changes**: `reviewer owners check [--allowlist dir]...`, a two-word subcommand added
    to `twoWordSubcommands` beside `rules test`. It lists tracked files with `git ls-files`, run
    against the checkout and reading its output only, matches each against CODEOWNERS, and returns
    `errCheckFailed` (exit 1) listing every file that is unowned and not under an allowlisted
    directory. It also fails if CODEOWNERS does not own itself, or if an allowlist entry is a file
    extension rather than a directory. Tests cover each failure and a passing layout.

##### 16. `docs/adr/0036-ownership-check-exits-nonzero.md` (Create) and `docs/adr/README.md` (Modify)

*   **Proposed Changes**: ADR-0036, "The ownership check is a check, not a review". It records why
    it exits 1 when ADR-0009 says reviews exit 0, citing the `rules test` precedent, and that at 0
    required approvals an unowned path merges unreviewed, so the check is what keeps G5 true.

##### 17. `docs/merge-classes.md` (Create)

*   **Proposed Changes**: The setup guide for docs-only:
    *   the CODEOWNERS template, with `*` owned first and the allowlist unowned last;
    *   the ruleset settings to change, and the ones to leave alone;
    *   the CI step for `reviewer owners check`;
    *   the sandbox steps that answer Q1 and Q2, with the expected results marked unverified;
    *   the rollback: set required approvals back to 1;
    *   the rule that any published docs directory leaves the allowlist (Q8).

##### 18. `README.md`, `CHANGELOG.md` (Modify)

*   **Proposed Changes**: Link the guide and the new command.

#### PR-60: baseline

##### 19. `docs/designs/trials/risk-gated-merge-baseline.md` (Create)

*   **Proposed Changes**: The output of `reviewer outcomes --since 90d` for each enabled repository,
    with names replaced by letters, as the existing trials README requires. It includes the
    hand-audited sample of 30 outcome labels with the agreement rate, and the sandbox results for
    Q1, Q2 and Q3 with the plan tier and date.

#### PR-61 to PR-66: `merge-gate` (later stages)

##### 20. `internal/ghclient/client.go` and tests (Modify, PR-61)

*   **Proposed Changes**: `*REST` methods, kept off the review `Client` interface so the review
    path does not grow:
    *   `CollaboratorPermission(ctx, repo, login) (string, error)`;
    *   `GetContentsAt(ctx, repo, path, sha) ([]byte, error)`;
    *   `CompareFiles(ctx, repo, base, head) (CompareResult, error)`, returning files with status,
        previous filename, patch, mode change and a truncation flag;
    *   `GetBranch(ctx, repo, name) (Branch, error)`;
    *   `Variable(ctx, repo, name) (string, bool, error)`;
    *   head and base repository identity on `PullRequest`.

    And a new one-method interface in `internal/ghclient/status.go`, kept apart from `Writer`:
    `StatusWriter { CreateStatus(ctx, repo, sha, state, context, description, targetURL string)
    error }`, implemented by `*REST`. Only `cmd/merge-gate` uses it, which the import-allowlist
    test and a test that `cmd/reviewer` never references `StatusWriter` both check.

    Each method gets retry with backoff on 5xx and secondary rate limits.

##### 21. `internal/policy/policy.go`, `predicates.go` and tests (Create, PR-62)

*   **Proposed Changes**: `Decode(raw []byte) (Policy, error)` with a strict decoder and
    `version: 1`. `Policy` holds classes, each with `Name`, `Enabled`, `Allow []string`
    (directories), `PrecisionTarget`, `Failure` and `OnFailure`. `NeverQualify` is a compiled-in
    list that `Decode` cannot change. `Evaluate(p Policy, files []CompareFile) (class string,
    reasons []Reason)` is a pure function.

##### 22. `cmd/merge-gate/main.go`, `decide.go`, `human.go`, `record.go` and tests (Create, PR-63)

*   **Proposed Changes**: The decision in four steps as in the Technical Plan. Exit codes: 0 pass,
    2 waiting (with a reason code), 1 error.
    *   `human.go` implements the D-E rule as one function with a truth-table test, one row per
        rule.
    *   `decide_test.go` walks every error path and asserts a non-zero exit.
    *   `imports_test.go` fails if the package imports anything outside its allowlist.
    *   `record.go` writes the decision record to `$GITHUB_STEP_SUMMARY` and the `<!-- rv:gate -->`
        comment, and posts the `merge-gate` commit status (`pending` with a reason code, `success`
        with the basis, or `failure` with the run link). A test asserts the status state always
        matches the exit code.

##### 23. ADR-0037 to ADR-0040 (Create, PR-63) and `docs/adr/README.md` (Modify)

*   **Proposed Changes**:
    *   ADR-0037, "Ship the merge gate as a separate, fail-closed program". It supersedes ADR-0026
        (two release artifacts become three).
    *   ADR-0038, "Read merge-class policy from the base branch by SHA".
    *   ADR-0039, "`merge-gate` may block; `reviewer` still may not". It supersedes ADR-0009 with a
        restatement that keeps "never block" for `reviewer` and records the gate as the one
        explicit, opt-in exception, with break glass.
    *   ADR-0040, "The merge gate posts one informational commit status". It supersedes ADR-0018
        with a restatement that keeps everything ADR-0018 decided for the review workflow and adds:
        the gate workflow holds `statuses: write`; the status is information and is never the
        required check; nothing in the tool can merge, approve, close or push.

##### 24. `merge-gate/action.yml`, `.goreleaser.yaml`, `.github/workflows/release.yml`, `internal/installer/templates/merge-gate.yml.tmpl` (Create or Modify, PR-64)

*   **Proposed Changes**: A composite action at `merge-gate/` that installs the gate binary by
    version and checksum and runs it; it checks out nothing. A second build and archive in
    `.goreleaser.yaml`. A workflow template with `permissions: { contents: read,
    pull-requests: read, statuses: write }` that `docs/merge-classes.md` tells admins to pin by
    commit SHA in the required-workflow rule. `reviewer init` does not install it.

##### 25. `cmd/merge-gate/audit.go`, `sweep.go` and tests (Create, PR-65)

*   **Proposed Changes**:
    *   `merge-gate audit --since 1d`, run on a schedule on `main`: for each merged pull request it
        finds the `merge-gate.yml` run for the merged head SHA (`ListWorkflowRuns`, matched by file
        path) and reports any merge without a passing run or a recorded bypass.
    *   `merge-gate sweep`, run by hand after the kill switch flips: it re-runs the gate for open
        pull requests whose last pass was by class.

##### 26. `docs/merge-classes.md` (Modify, PR-66)

*   **Proposed Changes**: Add the runbooks: rollout in four stages, rollback in reverse, break glass
    (who holds the bypass, what it skips, how it is audited), kill switch and sweep.

### Verification Plan

*   **Compilation/Build**: `make build`; `goreleaser build --snapshot --clean` once PR-64 adds the
    second artifact.
*   **Unit Tests**: `make test` (`go test ./...`), including the error-path walks for `reviewer
    risk` (always exit 0) and `merge-gate` (never exit 0 on error), the CODEOWNERS matching table,
    the label-name test for G2, and the gate's human-path truth table.
*   **Static Analysis & Linting**: `make check` (vet, gofmt, staticcheck, licence headers,
    `tools/checkadrs`, `tools/checklicenses`); `go run ./tools/audithistory --base origin/main`
    on every pull request.
*   **Test Coverage Requirement**: every function in `internal/risk`, `internal/codeowners`,
    `internal/policy` and `cmd/merge-gate` has a table test; every error return in `cmd/merge-gate`
    is reached by a test.
*   **E2E/Integration**:
    *   Sandbox experiments 13 and 15 (Q1), the org-ruleset check (Q2) and the label permission
        check (Q3), before PR-56 and PR-59 are used in a real repository.
    *   `reviewer risk --dry-run --pr N` against 20 recent pull requests in one TypeScript
        repository; a person checks that each label has a true reason.
    *   Experiments 1 to 19 before PR-63 is required anywhere.
*   **Success metrics**: G1 to G6 checks as written in Goals; after 4 weeks, median time to first
    approval compared with the PR-60 baseline.

--------------------------------------------------------------------------------

<!-- Provenance: Indicates initial structural drafting via EGM. -->

*Drafted with EGM*
