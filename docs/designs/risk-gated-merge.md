<!-- SPDX-License-Identifier: MIT -->
# Risk-gated merge

*   **Author:** adamtait
*   **Status:** Proposed
*   **Date:** 2026-11-27 (revised after review the same day)

--------------------------------------------------------------------------------

## Overview

The requester reports that coding agents write more than 90% of the pull requests on this team's
repositories, and a human reads every one before it merges. Human review is the most expensive step in that loop, followed by
agents waiting for a merge and pull requests going stale. `main` deploys to production, and the only
recovery from a bad merge is a revert and another deploy.

This design adds a v1 in three independent parts, and later stages that need an organisation
owner.

**v1 (needs no organisation-level change, unless an organisation ruleset already sets a minimum
approval count; see Q2).**

1. `reviewer risk`, an advisory command that runs as its own job in the review workflow on every
   pull request and attaches a risk record (one comment and one or more labels) that says what in
   the change deserves attention. It never decides or blocks anything, and it exits 0, so ADR-0009
   holds, and the review job keeps the permissions ADR-0018 states.
2. A docs-only merge class delivered by GitHub's own settings, not by code: CODEOWNERS owns every
   path except an explicit documentation allowlist, and the repository ruleset requires code-owner
   review with 0 general approvals. This ships only if a sandbox test shows GitHub still enforces
   code-owner review at 0 approvals (unverified).
3. `reviewer outcomes`, a read-only measurement: how many past pull requests would have qualified,
   how long review takes today, and outcome labels for past merges that later merge classes are
   tested against.

**Later stages (need an organisation owner).** `merge-gate`, a required status check produced by a
pinned required workflow, which passes when a human with write access approved the current head
commit or the pull request fits a deny-by-default merge class under base-branch policy. GitHub's
built-in auto-merge does the merging; nothing in this repository merges. These stages start only
after an org owner confirms that a required workflow can be pinned, because without that the check
can be forged by any workflow or token with write access.

The design is built so that the first version is useful alone and the later stages can be dropped
without undoing it. It was preceded by a brainstorm brief
(`docs/designs/briefs/auto-merge-concepts_brief.md`); the brief is provenance only, and everything
this design needs from it is restated here.

## Background

`reviewer` is this repository's automated code review tool. It runs in each pull request's GitHub
Actions workflow as the "review" job and posts inline comments, plus one collapsed summary comment,
on the lines the pull request changed. Each comment carries a hidden marker
(`<!-- rv:<fingerprint> <rule id> -->`) so a later run can recognise it. It has two lanes: the
deterministic lane (compilers, linters and pattern rules) and the model lane (a language model
reading the diff, off by default). Whatever it finds or fails at, it exits 0 and never blocks a
merge (ADR-0009); its review job holds only `contents: read` and `pull-requests: write`, and
nothing in it can merge, approve, close or push (ADR-0018). Each repository configures it in
`.review/config.yaml`, read by a decoder that rejects unknown keys. A separate workflow,
`review-index.yml`, builds a TypeScript navigation index (a SCIP file) on every push and uploads it
as an artifact (ADR-0034). The review workspace is a separate web service in this repository that
shows a pull request with code navigation; it stores nothing and makes no writes (ADR-0029,
ADR-0032).

## The Problem

**Review time is spent evenly on uneven risk.** A one-line typo fix in `docs/` and a change to the
auth middleware wait in the same queue and get the same human attention. Reviewers have no signal,
other than reading the diff, that tells them which pull requests need a careful read and which need
a glance.

**Agents wait on humans for changes that carry little production risk** (unless the changed
docs are published; see Q8). Documentation changes wait
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
    enabled, every non-draft, non-fork pull request gets one risk comment and its GitHub labels
    within the review workflow's run. Check: `reviewer outcomes` reports no merged pull request in
    the period without a `<!-- rv:risk -->` comment. Fork pull requests get no record, because
    their token cannot post; that gap is stated in Risks and Limits.
*   **G2. The record only adds attention.** Every label names a reason to look ("touches auth",
    "changes CI", "crosses 3 owner groups"). No label or text says a pull request is safe or low
    risk, and the comment says that no label means no rule matched, not that the change is safe.
    The risk map and CODEOWNERS are read from the base branch, so a pull request cannot change its
    own record; a pull request that changes `.review/`, `.github/` or any CODEOWNERS file is still
    labelled as such. Check: the label set in code is a fixed list of names, and a unit test fails
    if a name outside that list is added.
*   **G3. `reviewer risk` never changes the outcome of a run.** It exits 0 whatever it finds and
    whatever fails, as ADR-0009 requires. Check: an error-path test for every failure the command
    can hit.
*   **G4. Docs-only pull requests merge without a human, in one repository, if Q1 holds.** A
    pull request whose every changed path is unowned in CODEOWNERS (the documentation allowlist)
    merges once CI is green, with no approval. This includes deletes and renames inside the
    allowlist, which native settings cannot tell apart from edits; that is an accepted risk. Check:
    in the sandbox and then the first repository, a docs-only pull request merges unreviewed, and a
    pull request touching any other path cannot merge without a code-owner approval.
*   **G5. No path outside the allowlist loses review, and nothing models or agents read is on
    it.** The CODEOWNERS lint (`reviewer owners check`) is a required check and fails unless the
    first rule owns `*`, CODEOWNERS owns itself, every unowned rule is a directory, and no path on
    the never-allowlist (Part 2) is unowned. Check: the lint fails on a branch that adds an unowned
    rule for `.claude/`, or for a directory listed under `guidance:`.
*   **G5b. Docs-only has a failure policy before it goes live.** An escape is a docs-only merge with
    no review that is later reverted, modified by a fix (Part 3), or flagged in the weekly audit
    sample. After the first escape, required approvals go back to 1 until the cause is fixed.
    Check: `docs/merge-classes.md` names the audit owner, and `reviewer outcomes --only-unreviewed`
    lists the merges to sample.
*   **G6. A baseline exists before anything is claimed.** For each enabled repository, record the
    current ruleset, the median and 90th-percentile time from "ready for review" to first approval
    over the last 90 days, the number of merged pull requests that would have fit the docs-only
    class (both the strict count, added or modified files only, and the native count, every path
    unowned), outcome labels (reverted, modified later, hotfixed, or clean), and a first-review
    label (changed after review, unchanged after review, no review, or unknown) for the same
    period. Check: a committed trials file with these numbers and the command that produced them,
    without organisation or repository names.
*   **G6b. We can tell whether risk labels help.** `reviewer outcomes` reports, per `risk/`
    label, how often it fired and the median hours to first approval with and without it. A label
    that fires on more than half of pull requests for 4 weeks is reviewed for deletion, as
    ADR-0024 does for review rules. Check: the report includes the per-label table.

**Later stages (`merge-gate`)**

*   **G7. `merge-gate` cannot be satisfied by anything but the pinned gate.** Check: in a sandbox,
    a pull request that adds a workflow with a job named `merge-gate`, one that posts a
    `merge-gate` commit status, and a status posted with a person's token each fail to satisfy the
    rule (unverified until run).
*   **G8. The human path is never weaker than today.** Before the native approval rule is removed,
    the gate's human branch agrees with GitHub's own approval state, with "dismiss stale approvals"
    on, on every pull request for 4 weeks. Check: zero disagreements in the shadow run's job
    summaries, compared by the post-merge audit.
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
    disallow Apps (unconfirmed, Q0); this design does not depend on one.
*   **Merging inside this repository's code.** No command performs a merge. `ghclient.Writer` gains
    no merge, approve or push method.
*   **Organisation-level changes in v1.** No org ruleset, no required workflow, no `merge-gate`.
*   **Fork pull requests.** They never qualify for a merge class.
*   **Changes that cannot be reverted cleanly.** Database migrations, data backfills, external API
    contract changes and deletion of persisted state never qualify.
*   **Pull requests that touch what decides eligibility.** Policy, workflows, CODEOWNERS, rulesets,
    CI-influencing configuration, `.review/` and agent instruction files never qualify.
*   **Merge classes beyond docs-only, in this design.** Each is ruled out for now:
    comment-only (comments carry directives such as `@ts-expect-error`, so it needs both sides of
    the patch and directive detection); formatting-only (formatter configuration is pull request
    content); generated code (regeneration runs pull request code); lockfile bumps (Renovate
    already covers them); test-only (the test suites are not yet trusted); inert-additive (needs
    reach to prove nothing calls the new code). Dependency patch bumps stay with Renovate.
*   **The test-suite audit, mutation testing and a test-trust factor.** Separate work.
*   **Collecting outcome labels inside the review workspace.** The workspace stores nothing and
    makes no writes (ADR-0029, ADR-0032). Outcome labels come from what already happened on GitHub.
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
    GitHub labels. Advisory only.
*   **GitHub label / outcome label.** A GitHub label is a tag on a pull request, such as
    `risk/auth`. An outcome label is a value `reviewer outcomes` computes for a merged pull
    request, such as `reverted`; it is printed, never attached to the pull request.
*   **Lane.** One of `reviewer`'s two kinds of analysis: deterministic (compilers, linters,
    pattern rules) or model (a language model reading the diff).
*   **Pattern syntax.** One glob grammar for the risk map, the allowlist and CODEOWNERS-style
    matching, implemented once in `internal/glob`: `*` matches within one path segment, `**`
    across segments, a trailing `/` means a directory and everything under it, a leading `!`
    excludes, and the last matching pattern wins.
*   **Break glass.** A documented, audited way for named people to merge when the gate itself is
    broken, which skips only the gate.
*   **Kill switch.** A setting, changeable without a pull request, that turns every merge class off
    and leaves only the human path.
*   **Auto-merge enabler.** The account that turned on native auto-merge for a pull request.

## Open Questions

Each question names the work it holds up. Every statement about GitHub behaviour in this
document is unverified until the sandbox step named here has run.

*   **Q0. Does organisation policy disallow GitHub Apps?** Holds up nothing in v1. Decides,
    later, whether the gate is produced by a required workflow or by an App.
*   **Q1. Does GitHub enforce code-owner review at 0 required approvals, and do "changes
    requested" and stale-approval dismissal still apply?** Sandbox step: with CODEOWNERS owning
    `*` and leaving `docs/guides/` unowned, and the ruleset at 0 approvals with code-owner review
    on, open pull requests that (a) edit, (b) delete, (c) rename within, and (d) add a symlink
    under `docs/guides/`, and (e) edit an owned file; record which can merge without approval.
    Holds up G4 and G5. If code-owner review is not enforced, v1 is `reviewer risk` and
    measurement only.
*   **Q2. Does any organisation ruleset already set a minimum approval count on these
    repositories?** Holds up G4. If so, the docs-only class needs an org owner too.
*   **Q3. Can the risk job add and remove labels with `pull-requests: write`?** Sandbox step: a
    workflow job with only that permission adds a label that does not yet exist, then removes it.
    Holds up G1 and PR-56. The new writer methods are recorded in ADR-0035 either way.
*   **Q4. Is a pinned required workflow available on this plan, who can create and bypass it, and
    does it run on `pull_request_review` events?** Holds up every later stage.
*   **Q5. What exactly counts as a human approval?** Holds up all gate code. The rule in Part 4 is
    the starting point.
*   **Q6. What is each merge class's precision target under the gate, and what counts as a
    failure?** Holds up taking any class live under the gate. Docs-only under native settings
    uses G5b.
*   **Q7. Where are decision records stored, and how is a record shown to be genuine** when every
    workflow posts as the same Actions identity? Holds up G11.
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
*   **Then**: The record is computed with the base branch's risk map, not the pull request's, so
    `risk/auth` appears. The pull request also carries `risk/review-config`, so a reviewer sees that
    it changes how future records are computed.

### CUJ 3: An agent fixes a typo in the docs (v1, if Q1 holds)

*   **Given**: The repository's CODEOWNERS owns `*` and leaves `docs/guides/**` unowned; the
    ruleset requires code-owner review and 0 approvals; the agent enables auto-merge when it opens
    pull requests.
*   **When**: The agent opens a pull request that modifies only `docs/guides/setup.md`, and CI
    passes.
*   **Then**: GitHub merges it with no human approval. If the same pull request also touched
    `src/index.ts`, GitHub would wait for a code-owner approval.

### CUJ 4: Someone adds an unowned rule that would expose instructions (v1)

*   **Given**: The CODEOWNERS lint is a required check, and `.review/config.yaml` lists
    `docs/guidance/` under `guidance:`.
*   **When**: A pull request adds `/docs/guidance/` to the unowned allowlist rules in CODEOWNERS.
*   **Then**: `reviewer owners check` fails and names `docs/guidance/` as a never-allowlist path
    that would be unowned. The pull request needs a code-owner approval anyway, because it changes
    CODEOWNERS, and cannot merge until the lint passes.

### CUJ 5: A human approves, then someone pushes (later stages)

*   **Given**: `merge-gate` is required, approvals are 0, and auto-merge is enabled.
*   **When**: A writer approves head commit `A`; the author then pushes commit `B`.
*   **Then**: The gate run for `B` finds no approval on `B` and does not pass. The approval on `A`
    is not carried over, because `B` changes the pull request's own diff. (Had `B` only merged
    `main` into the branch, the approval would carry over; see Part 4.) The pull request waits, and
    the gate's comment says "waiting: no approval on the current head commit".

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
    already posted can still merge until the maintainer runs the sweep (see "Revocation"), which
    re-runs the gate on them. The incident is recorded as a failure against `docs-only`, which
    stays off until the policy says otherwise.

## Technical Plan (Architecture)

The work has four parts. Parts 1 to 3 are v1. Each part can ship, or be dropped, without the
others.

```text
                 pull request (untrusted: diff, config, workflows, text)
                                     |
       +-----------------------------+------------------------------+
       |                             |                              |
 review job (existing)        risk job (v1, Part 1)         merge-gate (later, Part 4)
 reviewer --pr N              reviewer risk --pr N          pinned required workflow
 inline comments + summary    runs in parallel; reads       runs cmd/merge-gate; reads the
                              changed files, and from the   pull request through the API,
                              merge base: risk map,         policy at the base SHA,
                              CODEOWNERS, history, index    reviews, compare(base...head)
                              writes <!-- rv:risk -->       result = job conclusion
                              + risk/ GitHub labels                 |
                              advisory only, exits 0        required by the ruleset
                                                                    |
 native GitHub (v1, Part 2)                                 native auto-merge merges
 CODEOWNERS owns * except the allowlist                     as the enabler
 ruleset: 0 approvals + code-owner review
 -> code-owner review decides docs-only

 measurement (v1, Part 3): reviewer outcomes, read-only, prints a table
```

### Part 1: `reviewer risk` (v1)

**Where it runs.** A new subcommand in `cmd/reviewer`, added to the subcommand table beside
`metrics` and `baseline` (`cmd/reviewer/flags.go:101`). It runs as its own job, `risk`, in the
review workflow, in parallel with the review job, with its own permissions: `contents: read`,
`pull-requests: write` and `actions: read`. The review job's permissions do not change. The
workflow's trigger gains `types: [opened, synchronize, reopened, ready_for_review]`, and the risk
job skips drafts. The composite action gains an input `mode` (`review`, the default, or `risk`), so
the risk job reuses the same install-and-checksum steps; existing installs change only when they
add the job. Configuration errors found before the first API call exit 2, as every `reviewer`
usage error does; every later error exits 0 with the reason in the job log and, if the API is
reachable, in the risk comment (ADR-0009).

**Inputs.** Changed files come from the pull request. Everything that decides which labels apply
is read from the merge base through the contents API, so a pull request cannot change its own
record (G2).

*   **Changed files** from `ListFiles`, using both `filename` and `previous_filename`, so a rename
    out of a sensitive path is seen at both ends.
*   **The risk map**: built-in defaults compiled into the binary plus repository additions from
    `.review/risk.yaml` at the merge base. The repository file can add patterns to an existing
    label and cannot remove a default or define a new label. It is a separate file, not a new key
    in `.review/config.yaml`, because the config decoder rejects unknown fields
    (`internal/config/load.go:70-73`): a new key would make every older pinned binary reject the
    whole config and skip review. Default patterns, in the pattern syntax (Terms):

    | Label | Default patterns |
    | --- | --- |
    | `risk/auth` | `**/auth/**`, `**/*auth*.*`, `**/permissions/**`, `**/crypto/**`, `**/payments/**`, `**/billing/**` |
    | `risk/data` | `**/migrations/**`, `**/*.sql`, `**/schema.*`, `**/prisma/**` |
    | `risk/ci` | `.github/**`, `.review/**`, `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `**/SKILL.md`, `.claude/**`, `.cursorrules` |
    | `risk/deps` | `**/package.json`, `**/package-lock.json`, `**/pnpm-lock.yaml`, `**/yarn.lock`, `**/go.mod`, `**/go.sum`, `**/Cargo.toml`, `**/Cargo.lock`, `**/requirements*.txt`, `**/pyproject.toml`, `**/poetry.lock` |
    | `risk/infra` | `**/*.tf`, `**/k8s/**`, `**/helm/**`, `**/Dockerfile*`, `**/docker-compose*.yml` |

    A `.review/risk.yaml`:

    ```yaml
    version: 1
    add:
      risk/auth: ["src/session/**", "src/tokens/**"]
      risk/data: ["db/**"]
    ```

    A file with any other top-level key (for example `remove:`), an unknown label, or a missing
    `version` is rejected: the record uses the defaults alone and the comment says why.
*   **Owner groups** crossed, from CODEOWNERS at the merge base. CODEOWNERS is read from
    `.github/CODEOWNERS`, then `CODEOWNERS`, then `docs/CODEOWNERS`, and only the first found is
    used, as GitHub does (unverified).
*   **File history**: for at most the 20 changed files with the most lines changed, whether a
    commit on the base branch in the last 180 days has a subject starting `Revert "` and touches
    that file. Read with a new `ListCommits(path, since)` client call. The cap puts a ceiling on
    the job's API cost.
*   **Reach**: for symbols defined on lines the pull request modifies or removes in existing
    TypeScript files, how many other files on the base branch reference them. It uses the
    `review-index` artifact that `review-index.yml` uploads on every push (ADR-0034), and needs
    four pieces, about 300 lines plus tests and no new dependency:
    1.  The SCIP decoder moves from `internal/workspace/navigation/scip` to `internal/scip`, a pure
        move, so the CLI does not depend on a workspace package.
    2.  The run-selection and unzip helpers (`classify` and `unzipIndex` in
        `internal/workspace/navigation/fetch.go`) move to a new `internal/scipfetch` package that
        both the workspace and `reviewer risk` use.
    3.  `diff.BaseLines(patch string) []int` in `internal/diff` returns the base-side line numbers
        of removed lines, parsed from hunk headers.
    4.  `risk.Reach(ix *scip.Index, path string, baseLines []int) []SymbolReach` returns each
        non-local symbol with a definition occurrence on those lines, and the number of other
        documents holding a non-definition occurrence of it.

    The full commit archive is not needed: `navigation.Build` uses it only to check positions
    against file text for display, while `scip.Decode` alone gives each document's path and
    occurrences with a definition flag. Only an index from the `push` run whose head SHA is exactly
    the merge base is used. Without one (no index workflow, no run for that SHA, or a merge base
    that was never the tip of a push), or for a non-TypeScript change, the comment says "reach not
    available" with the reason. Downloading an artifact needs `actions: read`, which is why risk is
    its own job.

**Outputs.** One sticky issue comment with the marker `<!-- rv:risk -->`, updated in place on each
push, and GitHub labels from a closed set:

| Label | Set when |
| --- | --- |
| `risk/auth` | a changed path matches a `risk/auth` pattern |
| `risk/data` | a changed path matches a `risk/data` pattern |
| `risk/ci` | a changed path matches a `risk/ci` pattern |
| `risk/deps` | a changed path matches a `risk/deps` pattern |
| `risk/infra` | a changed path matches a `risk/infra` pattern |
| `risk/wide` | the changed paths cross 3 or more CODEOWNERS groups, or more than 40 files change |
| `risk/reach` | a changed symbol is referenced from 10 or more other files on the base branch |
| `risk/history` | a changed file had a revert on the base branch in the last 180 days |
| `risk/review-config` | the pull request changes `.review/`, `.github/` or any CODEOWNERS file |

`risk/ci` and `risk/review-config` overlap on purpose: the first says the change affects CI, the
second that it affects how this record and future records are computed.

The comment states the head SHA it describes, links the run, says that risk is advisory and does
not decide merging, and says: "No label means no rule matched. It does not mean the change is
safe." It ends with a hidden list of the labels this tool set, `<!-- rv:risk-labels a,b,c -->`. To
sync labels, the tool reads the pull request's current labels, adds missing ones, and removes only
`risk/` labels listed in its own previous comment, trusted only when that comment's author is the
token's own login. It never removes a label a person set.

**New writer verbs.** `AddLabels` and `RemoveLabel` on `ghclient.Writer`, which today has four
write methods and one read (`internal/ghclient/write.go`). Label names are URL-escaped; adding a
label that does not exist creates it (unverified, Q3). They need no permission beyond
`pull-requests: write` (unverified, Q3). ADR-0035 records these verbs and the risk job's
permissions.

### Part 2: docs-only by native configuration (v1, conditional on Q1 and Q2)

No code decides docs-only merges in v1. GitHub does, from two pieces of repository configuration:

*   **CODEOWNERS** with `*` owned by a team as its first rule, CODEOWNERS owning itself, and the
    allowlist directories listed afterwards with no owner. The allowlist names directories people
    alone read, for example `docs/guides/`. It never includes anything on the never-allowlist:
    *   every path listed under `guidance:` in `.review/config.yaml`, which the model lane reads
        into its prompt (`internal/config/config.go:29`, `internal/laneb/gather.go:39-47`);
    *   `laneB.promptDir`;
    *   agent instruction files: `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `SKILL.md`,
        `.cursorrules`, and anything under `.claude/`;
    *   `.github/`, `.review/`, `docs/adr/`;
    *   any file a `//go:embed` directive or other build step reads;
    *   any directory a docs site publishes (Q8).
*   **The repository ruleset** on `main`: 0 required approvals, "require review from code owners",
    existing CI checks required, and the CODEOWNERS lint required.

Native settings cannot tell a delete, a rename or a symlink from an edit, so any change whose every
path is unowned merges. That is G4 as written, and an accepted risk in Risks and Limits.

This repository supplies two pieces of support:

*   **`reviewer owners check`**, a structural CODEOWNERS lint in the style of `reviewer rules
    test`. It fails (exit 1) unless the file parses with no unsupported syntax, the first rule is
    `*` with an owner, CODEOWNERS owns itself, every rule with no owner is a directory, and no
    never-allowlist path is unowned. It reads the never-allowlist from `.review/config.yaml` and
    from a list compiled into the binary. It is a check, not a review, so the exit-1 precedent of
    `rules test` applies (`cmd/reviewer/flags.go:230-239`), not ADR-0009. The ruleset requires it.
*   **`docs/merge-classes.md`**, a setup guide: the CODEOWNERS template, the ruleset settings, the
    Q1 and Q2 sandbox steps, the failure policy (G5b: the CODEOWNERS team reads a weekly sample of
    5 unreviewed docs-only merges listed by `reviewer outcomes --only-unreviewed`), and the
    rollback (set required approvals back to 1).

`reviewer init` does not write CODEOWNERS or rulesets: it cannot know the teams, and a wrong
CODEOWNERS file removes review without any visible sign.

### Part 3: measurement (v1)

A new `reviewer outcomes` subcommand, read-only, following the `metrics` pattern
(`cmd/reviewer/metrics.go`). It reads merged pull requests over a period through the API and
prints, as markdown or CSV:

*   time from "ready for review" to the first approval by a human other than the author (median and
    90th percentile), measured from the last ready-for-review before that approval;
*   docs-only candidates under a given allowlist, counted two ways: strict (every file added or
    modified under the allowlist) and native (every changed path, including deletes and renames,
    under the allowlist);
*   an outcome label per pull request:
    *   **reverted**: a later merged pull request whose title starts `Revert "` and whose body
        says `Reverts <owner>/<repo>#<n>` or `Reverts #<n>` (GitHub's form), or whose title starts
        `Revert #<n>` (the form people write by hand);
    *   **modified-later**: a pull request L merged within 14 days removes or changes a line of
        this one. Each line L removed is blamed with GitHub's GraphQL `blame` at L's merge base
        (the commit L's patch is numbered against), and the blamed commit is credited to the pull
        request GitHub says introduced it (`GET /repos/{o}/{r}/commits/{sha}/pulls`), so it works
        however this pull request was merged. A commit that is this pull request's merge commit
        skips the lookup. Limits: a line changed again between this pull request and L is credited
        to the change in between, an L that branched before this one merged never saw its lines,
        and at most 200 commit lookups are made per run;
    *   **hotfixed**: `modified-later`, and L's title or head branch contains a fix word
        (`fix`, `fixes`, `fixed`, `hotfix`, `bugfix`, `bug`, `regression`, `revert`, bounded by
        non-letters, so "fixture" does not count), or L's body names this pull request through a
        linking phrase such as `Fixes #<n>` or `regression introduced by #<n>`. A bare `#<n>`
        mention does not count. This is a judgement about intent, so it is audited;
    *   **not-checked**: nothing was found, but a merge base, blame or lookup could not be read
        (for example, GitHub sends no patch for a very large file), or the lookup bound was
        reached; the reason is listed;
    *   **clean**: none of these;
*   a first-review label per pull request, describing the revision a human first reviewed rather
    than the one that merged, so problems a reviewer caught count as evidence. Author and bot
    reviews are ignored:
    *   **changed-after-review**: the first review was `CHANGES_REQUESTED`, or left line comments,
        and later commits changed files it commented on (a `CHANGES_REQUESTED` review with no line
        comments counts if anything changed afterwards);
    *   **unchanged-after-review**: the merged head equals the commit the first review was made
        on, or later commits touched none of the commented files;
    *   **no-review**: merged with no human review;
    *   **unknown**: the comparison from the reviewed commit returned 404 (usually a force-push) or
        hit GitHub's 300-file cap.
*   merged pull requests with no `<!-- rv:risk -->` comment (G1), and, with `--only-unreviewed`,
    only the merges that had no human review (G5b);
*   per `risk/` label, how often it fired and the median hours to first approval with and without
    it (G6b).

A pull request labelled `changed-after-review` is treated as "would have been wrong if merged
unreviewed" when a merge class is tested against history. No outcome label feeds a merge decision
until a hand-audited sample of 30 agrees with it at 90% or better. The command exists (PR-55 and
PR-55b); a pilot run over sister repositories is recorded in
`docs/designs/trials/risk-gated-merge-baseline.md`.
The command writes nothing to GitHub. Results go into a trials file without organisation or
repository names, as `docs/designs/trials/` already does. The first candidate count is taken before
any code, with a one-off `gh pr list` query (see the Detailed Implementation Plan).

### Part 4: `merge-gate` (later stages, after Q4)

**Program.** `cmd/merge-gate`, a separate `main` with its own release artifact. It fails closed: the
only exit 0 is an explicit pass, and every error exits non-zero. An import-allowlist test checks
that it imports only `ghclient`, `diff`, `glob`, its own `policy` package and the standard library,
and never the review lanes, the model lane or plugin hosting.

**How it is required.** A workflow file `merge-gate.yml`, required through a ruleset "required
workflow" rule pinned to a protected ref. Its job installs the gate binary by version and checksum
from an action pinned by commit SHA. It never builds the gate from the pull request and never checks
out pull request code. The required check is that workflow's result, which no other workflow or
token can produce (unverified; G7's sandbox step decides, and if it fails, Part 4 stops). Its
permissions are `contents: read`, `pull-requests: write` (for its comment) and `statuses: write`.
A failure to post the comment or status is logged and never changes the verdict, so a side effect
cannot fail a human-approved pull request.

**Its commit status.** The gate also posts one commit status named `merge-gate` on the head SHA,
through its own one-method writer, `ghclient.StatusWriter`. The status is information, not the
rule: the ruleset requires the pinned workflow, never the status, so a forged status changes
nothing (G7). It exists because a job can only end as success or failure, and a person or agent
needs to tell "waiting for approval" from "the gate is broken":

| Status | Description carries |
| --- | --- |
| `pending` | a reason code, for example `waiting: no approval on the current head commit` |
| `success` | the basis: `approved by <login> at <sha>` or `class: docs-only, policy <sha>` |
| `failure` | `gate error`, the run link, and the break-glass pointer |

A "waiting" result still shows the required workflow as failed, which may turn off auto-merge
(unverified); the gate re-runs on the next event, and the status says why it is waiting. If the
status and the workflow result ever disagree, the workflow result decides, and the post-merge audit
flags the disagreement. ADR-0040 records the status and `statuses: write` (see "Changes to recorded
decisions").

**What triggers it.** Events are a doorbell, not an input. Whatever event starts a run, the gate
takes only the pull request number from the payload and fetches everything else through the API. If
the payload's head SHA differs from the API's, it exits as "waiting: re-run needed" without passing.

**The decision**, in order:

1. Fetch the pull request. Not eligible unless the base is the protected branch, the head
   repository is the base repository (no forks), and the head SHA matches the run's SHA.
2. **Human path.** Pass if some reviewer's latest review is `APPROVED`, that reviewer has write
   permission now, is not a bot, is not the author and did not push to the pull request, and no
   writer has an outstanding "changes requested". The approval must be on the head commit, or on an
   earlier commit A where every commit between A and the head is a merge of the base branch into
   the pull request and the pull request's own changes are identical (the comparisons base...A and
   base...head list the same files with the same patches). Any other commit voids it. Without this,
   "branches must be up to date" would void every open approval on each merge to `main`. This is
   the starting point for Q5.
3. **Class path**, only if the human path did not pass and the kill switch is set to `on`
   (a missing or unreadable variable means off):
    *   read `.review/merge-policy.yaml` at the base branch's current head SHA through the contents
        API, with its own strict decoder and a `version` field;
    *   build the change set from `compare(base_sha...head_sha)`, never from `/pulls/{n}/files`,
        which lists the current head's files rather than the evaluated SHA's; treat a missing patch,
        truncation, a mode change, a removal, a rename or a copy as not eligible;
    *   apply the never-qualify list compiled into the gate, which no policy can switch off, then
        the class predicates from the policy file;
    *   pass only if one enabled class allows every changed file.
4. Post the `merge-gate` status, write the decision record, and exit: 0 on pass; 2 for
   "waiting", with a reason code; 1 for an error, with a run link and the break-glass pointer.

**Policy file.** `.review/merge-policy.yaml`, separate from `.review/config.yaml` for the same
decoder reason as `.review/risk.yaml`. It lists classes, each with `enabled`, its allowlist in the
pattern syntax, its precision target, its definition of failure and its failure policy (G12). A
pull request that changes it never qualifies, so a human reviews every policy change.

**Rulesets for the gate stage.** Two repository rulesets on `main`. The first holds the existing CI
checks, the CODEOWNERS lint, code-owner review and "require branches to be up to date before
merging". The second holds only the `merge-gate` required workflow, so its bypass list (named break
glass holders, pull requests only) skips the gate and never CI. Required approvals start at 1 and
drop to 0 at rollout step 3. Whether code-owner review stays on under the gate is decided at step 3
from the shadow data: if the gate's human path already demands a writer's approval for owned paths,
it is redundant.

**Revocation.** A pass is bound to the head SHA, the base SHA and the policy SHA. The ruleset
requires branches to be up to date before merging (unverified that this forces re-evaluation), so
any commit to `main`, including a policy change, forces an update and a new evaluation; the human
path's carry-over rule (Decision, step 2) keeps that from voiding approvals. The kill switch
(`MERGE_GATE_CLASSES`, a repository variable read at evaluation) needs one more step: a sweep job,
started by the maintainer who flips the variable, re-runs the gate for open pull requests that
passed by class. Until it finishes, a pass already posted can still merge. That window is a stated
risk.

**Decision record.** The gate writes its record to its own run's job summary and to a sticky comment
`<!-- rv:gate -->`. The comment is for people and is not proof, because any workflow can post as the
same identity (Q7). The audit is the check, with its own limits: a scheduled job on `main` finds,
for each merged pull request, the `merge-gate` run for the merged head SHA by workflow file path and
run ID, and flags any merge without a passing run or a recorded bypass. Workflow run records expire
under the repository's retention setting, so the audit runs daily and copies what it found into
its own job summary.

**Rollout order.** At no step is review weaker than today:

1. Shadow: the gate runs as a non-required workflow named `merge-gate (shadow)`; native approvals
   unchanged; G8 is measured for 4 weeks.
2. Required alongside native approvals (1 required approval, as before docs-only shipped, or 0
   with code-owner review where Part 2 is live), with classes off.
3. Native approval rule removed; the gate is the approval rule.
4. One class on, in one repository.

Rollback runs in reverse, and its first step is always re-adding the native approval rule.

## Alternatives Considered

"Rejected by author" marks a choice the author made during the brainstorm or this draft, with the
trade-off that decided it. "Not chosen" marks an option raised in review that this design does not
take, with the reason; those can be reopened. Labels use the `Alt-` prefix so they do not collide
with goal or question numbers.

### How the merge decision is made

#### Alt-1: A risk score with configurable thresholds

*   **Pros**: Covers every pull request, not only those that fit a class; one number per pull
    request; the original ask.
*   **Cons**: About 20 signals with hand-set weights have no calibration, so a threshold has no
    meaning; showing a miss rate under 1% at 95% confidence needs about 300 clean outcomes per
    class, which is months at this volume; ADR-0017 already rejected a combined score for deciding
    where findings appear, because "why was this inline?" becomes unanswerable; a reviewer cannot
    tell why a pull request scored 0.31.
*   **Reason for Rejection**: Rejected by author: merge classes first, checked by rules from inputs
    the pull request cannot change, because a rule can be explained and tested where a score
    cannot. A score may return after it passes a recall test on known-bad pull requests.

#### Alt-2: A learned model from the first version

*   **Pros**: Could find patterns no rule names.
*   **Cons**: No labelled history exists; recall cannot be validated at this volume; the review
    workspace design already deferred it for that reason (`docs/designs/review-workspace.md:530`).
*   **Reason for Rejection**: Rejected by author, with Alt-1: without labelled history its miss
    rate cannot be shown.

#### Alt-3: Evidence produced by the pull request lowering risk

*   **Pros**: Tests, coverage and the model lane carry real information about a change.
*   **Cons**: They run the pull request's code or read its files, so the party being judged produces
    them. Author-written text (descriptions, commit messages, comments, agent session logs) can
    carry prompt injection.
*   **Reason for Rejection**: Rejected by author: such evidence may raise risk or block
    eligibility, never grant it, because evidence the author controls cannot vouch for the author.
    The model lane may veto and never approve.

#### Alt-4: Settings that lower the fixed floor, or auto-merging irreversible or self-editing changes

*   **Pros**: Flexibility per repository.
*   **Cons**: Recovery is a revert plus a deploy, so an irreversible change has no recovery; a pull
    request that edits policy, workflows, CODEOWNERS or rulesets could edit its own gate.
*   **Reason for Rejection**: Rejected by author: the never-qualify list is compiled into the gate,
    because flexibility here is exactly what an attacker would use.

### Who or what merges

#### Alt-5: A merge-only GitHub App run from a trusted workflow

*   **Pros**: A distinct identity that can be a ruleset bypass actor and can be told apart from
    other workflows; the most complete answer to forged checks.
*   **Cons**: Understood to be disallowed by organisation policy (Q0); "merge-only" is not a GitHub
    permission, since merging needs `contents: write` (unverified); a key in a repository secret
    can be read by workflows on agent branches.
*   **Reason for Rejection**: Rejected by author as the default, because a design that waits for an
    unconfirmed policy exception ships nothing. An App stays a later option if Q0 allows it.

#### Alt-6: A machine-user token

*   **Pros**: Works without an App.
*   **Cons**: Broader than an App's scoping; a shared credential that agents could reach.
*   **Reason for Rejection**: Rejected by author: a long-lived credential agents can reach is a
    larger risk than the review time it saves.

#### Alt-7: A trusted workflow merges with `GITHUB_TOKEN`

*   **Pros**: No person's token on the merge path; a compare-and-swap on `sha` closes the
    approve-then-push race.
*   **Cons**: Pushes made with `GITHUB_TOKEN` do not start other workflows, so deploys would need a
    dispatch (unverified); needs `contents: write`; contradicts ADR-0018 and
    `internal/ghclient/write.go`, which say nothing in this tool can merge.
*   **Reason for Rejection**: Not chosen: native auto-merge keeps merging out of this repository's
    code.

#### Alt-8: A hosted service, Mergify, Kodiak, Graphite, Aviator, or a vendor review bot

*   **Pros**: Existing products; some offer queues and condition languages.
*   **Cons**: A hosted service contradicts ADR-0008 (no service to deploy, secure and page someone
    about); Mergify and Kodiak are Apps; Graphite and Aviator address stacking and queueing, not
    risk; Copilot review is not permitted (`docs/designs/review-workspace.md:520`); each sends
    code to a vendor.
*   **Reason for Rejection**: Rejected by author for the hosted service, for ADR-0008's reasons.
    The products are not chosen for the reasons above.

#### Alt-9: Renovate or Dependabot auto-merge for dependency patch bumps

*   **Pros**: Mature, runs in Actions, narrow rules with a minimum release age.
*   **Cons**: Covers only dependency bumps.
*   **Reason for Rejection**: Not rejected. Dependency patch bumps stay with these tools and are not
    a merge class here.

### What produces `merge-gate`

#### Alt-10: A `pull_request_target`, `workflow_run` or `pull_request_review` workflow

*   **Pros**: Available today with no organisation change.
*   **Cons**: A required check is matched by name, so any workflow a pull request adds, or any
    token with write access, can post a check with the same name (unverified; G7's sandbox step).
    `pull_request_target` also runs whatever workflow sits on an attacker-created base branch
    (ADR-0018 says "Never" in that direction); `pull_request_review` probably runs the pull
    request's own workflow file (unverified).
*   **Reason for Rejection**: Not chosen. These events may wake the gate; they never produce it.

#### Alt-11: Allow GitHub Actions to approve pull requests, with a trusted approving workflow

*   **Pros**: Keeps the native approval rule in place.
*   **Cons**: Any same-repository workflow could approve (unverified), which moves the forgery
    problem to the approval layer.
*   **Reason for Rejection**: Not chosen, for that reason.

### Where the code lives

#### Alt-12: The merge decision in a separate repository or consumer

*   **Pros**: The shape ADR-0009 describes for any gate ("that team's own CI step reading the
    rdjson output"); keeps every existing ADR true.
*   **Cons**: Another repository and release to keep in step with `reviewer`'s packages.
*   **Reason for Rejection**: Rejected by author: build it in this repository, because the gate
    reuses `ghclient`, `diff` and `glob`, and one repository keeps those and their releases in step.

#### Alt-13: `reviewer risk` as a reporter inside the main review run

*   **Pros**: Reuses the diff already in memory; no extra job; `reviewer --staged` would show risk
    locally.
*   **Cons**: Shares a process and configuration with review, so a risk bug can delay or break
    review comments; cannot run risk without review.
*   **Reason for Rejection**: Rejected by author: a separate subcommand, run as its own job, so a
    risk failure cannot cost a review comment and risk can hold `actions: read` without the review
    job holding it.

#### Alt-14: `merge-gate` as a `reviewer gate` subcommand

*   **Pros**: One binary and one release artifact.
*   **Cons**: Two opposite exit policies in one `main` (exit 0 always, against fail closed); every
    review release moves the merge path.
*   **Reason for Rejection**: Rejected by author: a separate `cmd/merge-gate` program with its own
    artifact, so a review release never changes what can merge.

#### Alt-15: Merge-class policy compiled into the gate

*   **Pros**: Nothing to fetch or parse at decision time; the pin fixes the policy.
*   **Cons**: Every allowlist change is a gate release and a pin bump in each repository;
    repositories with different layouts turn the constants into a per-repository table.
*   **Reason for Rejection**: Rejected by author: a per-repository `.review/merge-policy.yaml` read
    from the base branch by SHA, because the repositories' layouts differ and a policy change
    should be a reviewed pull request, not a release.

#### Alt-16: New keys in `.review/config.yaml` for the risk map or the policy

*   **Pros**: One configuration file.
*   **Cons**: The decoder rejects unknown fields (`internal/config/load.go:70-73`), so older pinned
    binaries would reject the file and skip review; the file is read from the pull request's head.
*   **Reason for Rejection**: Not chosen: separate files.

### Rollout and storage

#### Alt-17: A merge queue

*   **Pros**: Tests against current `main`; batches merges.
*   **Cons**: The group commit is never classified; `merge_group` may run the group's own workflow
    copy (unverified).
*   **Reason for Rejection**: Not chosen for v1 or the first gate stages. "Require branches to be up
    to date" is used instead, which classifies the exact tree that lands.

#### Alt-18: Replacing native approvals with the gate as the first release

*   **Pros**: Proves the plumbing quickly.
*   **Cons**: Replaces a tested native rule with new code; the instant the approval rule goes, every
    open pull request with auto-merge on and green CI merges unreviewed.
*   **Reason for Rejection**: Not chosen: shadow first, then required alongside native approvals.

#### Alt-19: Check runs, or the review workspace, as the store of records or outcome labels

*   **Pros**: Check runs show in the pull request UI; the workspace already shows the diff.
*   **Cons**: Check runs expire (ADR-0016) and need `checks: write`; the workspace stores nothing and
    makes no writes (ADR-0029, ADR-0032).
*   **Reason for Rejection**: Not chosen: records go in job summaries and comments, checked by the
    post-merge audit; outcome labels are computed from what already happened on GitHub.

#### Alt-20: A short shadow period proving a 1% miss rate

*   **Pros**: Starts merging sooner.
*   **Cons**: Zero failures in n cases bounds the rate below about 3/n at 95% confidence, so 1%
    needs about 300 clean cases per class, which is months at this volume.
*   **Reason for Rejection**: Not chosen. Docs-only rests on rules plus the failure policy in G5b,
    not on a rate claim.

--------------------------------------------------------------------------------

## Non-Functional Requirements

*   **Security.**
    *   v1 adds no permission to the review job: `contents: read` and `pull-requests: write` stay
        as they are. The new risk job holds those two plus `actions: read`, for the navigation
        index artifact. Whether labels fit `pull-requests: write` is Q3.
    *   `merge-gate` never checks out, builds or runs pull request content, and reads head files
        only as data from `compare(base_sha...head_sha)`.
    *   The gate binary is installed by version and checksum from an action pinned by commit SHA.
        Moving a tag cannot change the merge path.
    *   No model call happens anywhere on the merge path.
*   **API cost.**
    *   `reviewer risk`, per run: 1 call for the pull request, 1 or more for files, 1 for the merge
        base, up to 3 contents reads (risk map and CODEOWNERS at the merge base), at most 20 for
        file history, 2 to find the index run and artifact plus one artifact download bounded by
        the workspace's index size limit, 1 or 2 for comments, 1 to read labels, and 1 to 10 to
        change labels.
    *   `reviewer outcomes`: one page of pull requests per 100; per pull request, reviews, files and
        timeline; for exact attribution, one GraphQL blame call per (later pull request, file)
        pair that both touched. It prints what it read when it hits a rate limit, as `metrics`
        does.
    *   `merge-gate`: about 6 calls per evaluation (pull request, reviews, permissions per approver,
        policy file, compare, comment and status).
*   **Latency.** The risk job finishes in under 60 seconds including its runner start, in parallel
    with the review job. `merge-gate` finishes in under 60 seconds, so auto-merge is not held up.
*   **Rollout and rollback.**
    *   `reviewer risk`: remove or comment out the `risk` job.
    *   Docs-only: set required approvals back to 1.
    *   `merge-gate`: the reverse rollout order, re-adding native approvals first.
    *   Each runbook is in `docs/merge-classes.md` and is rehearsed before the stage it protects.
*   **Compatibility.** Older pinned `reviewer` binaries ignore `.review/risk.yaml` and
    `.review/merge-policy.yaml`, because neither is a key in `.review/config.yaml`.

## Risks and Limits (v1)

| Risk | Effect | Mitigation or acceptance |
| --- | --- | --- |
| CODEOWNERS is wrong | A path loses review with no visible sign | The CODEOWNERS lint is a required check (G5) |
| Deletes, renames, symlinks under the allowlist | They merge unreviewed | Accepted (G4); counted by `outcomes` as the native count; covered by the G5b audit |
| A file models or agents read is on the allowlist | Unreviewed text steers later reviews or agents | The never-allowlist, enforced by the lint (Part 2, CUJ 4) |
| An allowlisted directory is published | Rendered docs are production content | That directory leaves the allowlist (Q8) |
| A pull request edits its own risk configuration | Labels it should carry are dropped | The risk map and CODEOWNERS are read from the merge base (G2) |
| No label is read as "safe" | A reviewer skims a risky change | The comment says no label means no rule matched; G6b measures label use |
| Reach is unavailable for some merge bases | `risk/reach` is missing | The comment says why; the 20-pull-request dry run measures how often |
| Fork pull requests | No risk record | Stated; forks never qualify for docs-only either |
| The risk job holds `actions: read` | One more read permission in a job the pull request's workflow file defines | Read-only; the review job is unchanged |
| API rate limits | A partial record or measurement | Bounded calls; partial output says what is missing |

## Changes to recorded decisions

| ADR | Change | When |
| --- | --- | --- |
| ADR-0035 (new) | Records the advisory risk record, its closed label set, the two writer verbs, and the risk job's permissions. Supersedes nothing: ADR-0018's permission sentence is about the review job, which is unchanged. | PR-56 |
| ADR-0036 (new) | Records that the CODEOWNERS lint exits 1, like `rules test`, because it is a check and not a review; ADR-0009 governs reviews. | PR-59 |
| ADR-0037 to ADR-0040 (new, later) | Separate fail-closed gate program (supersedes ADR-0026's two artifacts); policy from the base branch; the gate may block while `reviewer` reviews still never block (supersedes ADR-0009); the gate's informational status and `statuses: write` (supersedes ADR-0018). Each supersession also changes the old ADR's status block, which `tools/checkadrs` checks. | PR-63 |

--------------------------------------------------------------------------------

### Additional Resources

*   `docs/designs/briefs/auto-merge-concepts_brief.md` and `auto-merge-hypothesis.md`: the
    brainstorm that preceded this design. Provenance only; nothing here depends on reading them.
*   `docs/designs/reviews/risk-gated-merge/`: the review of the first draft of this design.
*   ADR-0004, 0008, 0009, 0016, 0017, 0018, 0022, 0024, 0026, 0029, 0032, 0034.
*   `docs/designs/review-workspace.md`: the design this one follows in form, and the earlier
    deferral of a risk classifier (line 530).

### Future Improvements

*   **More merge classes** under the gate, each in shadow first (see Non-Goals for why each waits).
*   **Reach for Python, bash and Rust**: v1 reach covers TypeScript only, because the index
    ADR-0034 builds is TypeScript.
*   **A scored classifier**, only after it passes a recall test on a corpus of known-bad pull
    requests built from `reviewer outcomes` data.
*   **A GitHub App** in place of the required workflow, if Q0 allows it.

--------------------------------------------------------------------------------

## Detailed Implementation Plan

The plan continues the numbering of `docs/implementation-plan.md` (which ends at PR-54) and lands as
a stack of pull requests on top of this design. Every pull request leaves `make check` green and
carries the ADR that its decision needs, in the same pull request, as `docs/adr/README.md` requires.
PR-55 to PR-60 are v1. PR-61 to PR-66 start only when Q4 is answered yes.

### Before any code

The repository admin runs a one-off query in each repository and records the result in the PR-60
trials file: `gh pr list --state merged --limit 500 --json number,files,reviews,createdAt,mergedAt`,
then counts the pull requests in the last 90 days whose files are all under the proposed docs
allowlist, and their time to first approval. Today's ruleset is written down in the same file. If
fewer than 5% of merged pull requests qualify, Part 2 and PR-59 are dropped before anything is
built. (Run once on this repository: 1 of 21 merged pull requests was docs-only, and none had an
approval, because it has one human author.)

The Q1 and Q3 sandbox steps also run before PR-56 and PR-59 are used; PR-60 only records their
results.

### Stack order

| PR | Content | Depends on | Gate to start |
| --- | --- | --- | --- |
| PR-55 | `reviewer outcomes`, first version, and the reads it needs (built) | — | — |
| PR-55b | `outcomes`: exact attribution, native count, unreviewed list, risk-record and per-label reports | PR-55 | — |
| PR-56 | Label verbs on `ghclient.Writer`; ADR-0035 | — | Q3 run in the sandbox |
| PR-57a | Move the SCIP decoder and index-fetch helpers to shared packages (no behaviour change) | — | — |
| PR-57 | `internal/glob`, `internal/codeowners`, `internal/risk`, `diff.BaseLines` | PR-57a | — |
| PR-58 | `reviewer risk` command, client reads, reporter, action `mode`, installer risk job, docs | PR-56, PR-57 | — |
| PR-59 | `reviewer owners check` lint; ADR-0036; `docs/merge-classes.md` | PR-57 | merges any time; used only after Q1 and Q2 pass |
| PR-60 | Trials file: baseline, candidate counts, sandbox results, 30-item audit | PR-55b | a person runs the commands |
| PR-61 | Gate reads and `StatusWriter` | — | Q4 yes |
| PR-62 | `internal/policy`: merge-policy decoder and class predicates | — | Q4 yes |
| PR-63 | `cmd/merge-gate`, import-allowlist test; ADR-0037 to ADR-0040 | PR-61, PR-62 | Q4 and Q5 |
| PR-64 | Gate action, workflow template, release artifact | PR-63 | Q4 |
| PR-65 | Post-merge audit and kill-switch sweep | PR-63 | Q7 |
| PR-66 | Runbooks: rollout, rollback, break glass, kill switch | PR-64, PR-65 | — |

Wherever a pull request adds a method to `ghclient.Client` or `ghclient.Writer`, it also updates
every type that implements them: `internal/poller/poller_test.go`, `internal/reporters/github_test.go`,
and the `clientMethods` list in `internal/ghclient/client_test.go`.

### Target Files to Create/Modify

#### PR-55: measurement, first version (built)

##### 1. `cmd/reviewer/outcomes.go` and `outcomes_test.go` (Create)

*   **Type / Scope**: Go subcommand and tests.
*   **Proposed Changes**: As built on `risk-merge/03-outcomes`. Flags `--since` (default `90d`),
    repeatable `--allowlist` (directories only: `docs`, `docs/`, `docs/**`, `packages/*/docs`;
    file extensions, `..`, absolute paths and leading patterns are usage errors), `--format
    markdown|csv`, `--repo`. Per merged pull request: hours to first human approval (author and bot
    reviews ignored), measured from the last ready-for-review before it; docs-only (strict:
    every file added or modified, `filename` and `previous_filename` both under the allowlist;
    3,000 or more files never counts); `reverted`; `hotfixed` by line-range overlap (replaced in
    PR-55b); first-review label including `unknown`. Partial reads print what was read and what
    was not. Tests use a narrow `outcomeReader` interface and cover every rule above, plus an
    end-to-end run against `httptest` asserting no write calls.

##### 2. `internal/ghclient/client.go` and `client_test.go` (Modify)

*   **Proposed Changes**: As built. `ListReviews` on `Client`, returning `Review` (`ID`, `User`,
    `State`, `CommitID`, `SubmittedAt`); `PullRequest.MergedAt`; `ReviewComment.PullRequestReviewID`;
    `ListTimelineEvents` and `CompareFiles` (with `MaxCompareFiles = 300`) on `*REST` only.
    `ChangedFile.PreviousFilename` was already decoded. Test doubles updated.

##### 3. `cmd/reviewer/flags.go`, `main.go`, `README.md`, `CHANGELOG.md` (Modify)

*   **Proposed Changes**: As built. `measures()` lets `--since`, `--format` and `--repo` apply to
    both `metrics` and `outcomes`; `--allowlist` applies to `outcomes` only.

#### PR-55b: measurement, exact attribution and reports

##### 4. `internal/ghclient/graphql.go` and test (Create or Modify)

*   **Proposed Changes**: `Blame(ctx, repo, sha, path string) ([]BlameRange, error)` on `*REST`,
    using the GraphQL `blame` field on a commit's file. `BlameRange` holds `StartLine`, `EndLine`
    and the commit SHA. Uses the existing `GraphQLURL`. Test against `httptest`. As built, a
    second read, `PullRequestsForCommit(ctx, repo, sha) ([]int, error)`, maps a blamed commit to
    its pull requests. Both response shapes were confirmed by a live, read-only run.

##### 5. `cmd/reviewer/outcomes.go` and `outcomes_test.go` (Modify)

*   **Proposed Changes**:
    *   Replace line-range overlap with `modified-later`: for a later pull request L merged within
        14 days that touches a file this pull request P touched, take L's removed lines
        (`diff.BaseLines` on L's patch), blame that file at L's base commit, and mark P
        `modified-later` if any removed line is blamed to a commit GitHub associates with P and
        not with L. As built, blame runs at L's merge base, not L's base commit.
    *   `hotfixed`: `modified-later` and L's title, body or head branch matches
        `(?i)\b(fix|bug|hotfix|regress|revert)` or contains `#<P>`.
    *   Report the native docs-only count (every changed path, including removals and renames,
        under the allowlist) beside the strict count.
    *   `--only-unreviewed`: list only merges with no human review (G5b).
    *   Report merged pull requests with no `<!-- rv:risk -->` comment by this tool's login (G1).
    *   Per `risk/` label on merged pull requests: count, and median hours to first approval with
        and without it (G6b).
    *   Tests: a later pull request that edits lines P added (modified-later), one that edits only
        neighbouring lines (not), one titled "fix" (hotfixed), one titled "add feature"
        (modified-later only), blame returning another commit (not), a blame error (the pull
        request is listed as not checked), and each new report.

#### PR-56: label verbs

##### 6. `internal/ghclient/write.go` and `client_test.go` (Modify)

*   **Proposed Changes**: `AddLabels(ctx, repo, number int, labels []string) error`
    (`POST /repos/{o}/{r}/issues/{n}/labels`, body `{"labels": [...]}`) and
    `RemoveLabel(ctx, repo, number int, label string) error`
    (`DELETE .../labels/{name}` with the name URL-escaped; 404 is not an error). Adding a label
    that does not exist creates it (unverified, Q3). Update the type comment's list of verbs: post
    an inline comment, post or update a conversation comment, resolve a thread this tool created,
    add or remove a label. Tests: both verbs against `httptest`, a label name with `/` and a space,
    and the 404 case. Update the writer test doubles.

##### 7. `docs/adr/0035-advisory-risk-record.md` (Create) and `docs/adr/README.md` (Modify)

*   **Proposed Changes**: ADR-0035, "Attach an advisory risk record that only adds attention". It
    records: the comment-plus-labels shape; the closed label set; that the comment says no label
    is not "safe"; that labelling inputs are read from the merge base; the two writer verbs; and
    that `reviewer risk` runs as its own job holding `contents: read`, `pull-requests: write` and
    `actions: read`. It supersedes nothing, and cross-references ADR-0018's permission sentence,
    which is about the review job. Add the index row.

#### PR-57a: shared index packages (no behaviour change)

##### 8. `internal/scip/` (Move from `internal/workspace/navigation/scip/`)

*   **Proposed Changes**: Move `decode.go`, `symbol.go` and their tests unchanged; update imports in
    `internal/workspace/navigation`. `go test ./...` passes with no other change.

##### 9. `internal/scipfetch/scipfetch.go` and test (Create); `internal/workspace/navigation/fetch.go` (Modify)

*   **Proposed Changes**: Move `classify` (exported as `Classify(runs, sha, workflowPath)`) and
    `unzipIndex` (exported as `Unzip(archive []byte, limit int64)`) with their state constants and
    tests; `fetch.go` calls them. No behaviour change.

#### PR-57: risk signals

##### 10. `internal/glob/glob.go` and test (Create)

*   **Proposed Changes**: `Compile(pattern string) (Pattern, error)` and `(Pattern) Match(path
    string) bool`, implementing the pattern syntax in Terms (`*`, `**`, trailing `/`, leading `!`),
    and `MatchList(patterns []Pattern, path string) bool` where the last matching pattern wins.
    Table tests for each rule at several depths, and for invalid patterns.

##### 11. `internal/codeowners/codeowners.go` and test (Create)

*   **Proposed Changes**: `Parse(r io.Reader) (File, error)` and `(File) Owners(path string)
    (owners []string, matched bool)`, using `internal/glob` with CODEOWNERS rules: last match
    wins, a rule with no owners means unowned, `#` comments. `Parse` rejects syntax GitHub
    documents as unsupported in CODEOWNERS (`!` negation, `[ ]` ranges, `\#` escapes) with a line
    number. Table tests from GitHub's documented examples, including a later unowned rule
    overriding an earlier owned one.

##### 12. `internal/diff/baselines.go` and test (Create)

*   **Proposed Changes**: `BaseLines(patch string) []int`: the base-side line numbers (1-based) of
    removed lines, from each hunk header's old start and the `-` lines that follow. Tests: one hunk,
    several hunks, a pure addition (empty), a pure deletion, `\ No newline at end of file`.

##### 13. `internal/risk/riskmap.go`, `signals.go`, `reach.go`, `labels.go` and tests (Create)

*   **Proposed Changes**:
    *   `riskmap.go`: the default patterns per label (the table in Part 1) and `Parse(raw []byte)
        (Map, []string, error)` for `.review/risk.yaml`, with `yaml.Decoder.KnownFields(true)`,
        `version: 1`, and only an `add:` key; unknown labels and other keys are errors, and the
        defaults are used alone.
    *   `signals.go`: `Compute(in Input) Record`, a pure function. `Input`: changed files with
        previous paths, the risk map, the parsed CODEOWNERS, per-file revert flags, reach results
        or a "not available" reason. `Record`: the labels, for each label the paths or symbols
        behind it, and `Warnings []string` for anything not available.
    *   `reach.go`: `Reach(ix *scip.Index, path string, baseLines []int) []SymbolReach`, where
        `SymbolReach` is a symbol and the number of other documents with a non-definition
        occurrence of it. Local symbols (`local ` prefix) are skipped. SCIP lines are 0-based;
        `baseLines` are converted.
    *   `labels.go`: the nine labels as constants and `All`, and a test that fails if `All` holds a
        name not in the fixed list (G2).

    Tests: one per label, renames at both ends, the 40-file and 3-owner boundaries (39 and 40, 2
    and 3), the reach boundary (9 and 10 files), reach not available, an invalid `risk.yaml`, an
    empty diff, and a reach fixture built from a small checked-in index.

#### PR-58: the `reviewer risk` command

##### 14. `internal/ghclient/client.go` and tests (Modify)

*   **Proposed Changes**: On `*REST`: `GetContentsAt(ctx, repo, path, sha) ([]byte, bool, error)`
    (false when the file does not exist) and `ListCommits(ctx, repo, path string, since time.Time)
    ([]Commit, error)`. The merge base comes from the existing `Compare`. Tests against `httptest`.

##### 15. `cmd/reviewer/riskcmd.go` and `riskcmd_test.go` (Create)

*   **Proposed Changes**: `func runRisk(ctx, o options, stdout, stderr io.Writer, getenv) error`
    (named so it does not clash with package `risk`). Steps: read the pull request and its files;
    find the merge base; read `.review/risk.yaml` and CODEOWNERS (`.github/`, root, `docs/`, first
    found) at the merge base; read revert history for at most 20 files; for reach, find the
    `review-index.yml` `push` run at the merge base with `scipfetch.Classify`, download its
    artifact, `scipfetch.Unzip` with the workspace's default size limit, `scip.Decode`, and call
    `risk.Reach` per changed TypeScript file with `diff.BaseLines`; call `risk.Compute`; render the
    comment; sync labels (read current labels, add missing, remove only those in this tool's own
    previous `<!-- rv:risk-labels -->` list, trusting that list only on a comment authored by the
    token's login). `--dry-run` prints instead of posting. Configuration errors before the first API
    call return `errUsage` (exit 2), as other subcommands do; every later error is caught, put in
    `Record.Warnings`, and the command exits 0. Tests walk each error (each API call failing, a bad
    `risk.yaml`, no CODEOWNERS, no index run, a corrupt artifact, a 403 on artifacts) and assert
    exit 0 with the warning in the rendered comment.

##### 16. `internal/reporters/risk.go` and test (Create)

*   **Proposed Changes**: Renders the comment: the head SHA and run link; "advisory, does not
    decide merging"; one line per label with its reasons; "No label means no rule matched. It does
    not mean the change is safe."; warnings; and `<!-- rv:risk-labels a,b,c -->`. Golden tests.

##### 17. `action.yml` (Modify)

*   **Proposed Changes**: New input `mode` (default `'review'`). With `'risk'`, the "Review" step is
    skipped and a "Risk" step runs `reviewer risk --pr "${PR_NUMBER}"`. The history check and the
    install-and-checksum steps are shared. The input is documented in the action's `description`.

##### 18. `internal/installer/templates/review.yml.tmpl`, its plan and golden tests (Modify)

*   **Proposed Changes**: The trigger gains `types: [opened, synchronize, reopened,
    ready_for_review]`. A second job, `risk`, is written commented out: no `needs:`, `if:
    github.event.pull_request.draft == false`, its own `permissions:` (`contents: read`,
    `pull-requests: write`, `actions: read`), the same checkout, and the action with `mode: risk`.
    A comment explains it and that reach needs `review-index.yml`. The template's opening comment
    about permissions is updated so it stays true.

##### 19. `cmd/reviewer/flags.go`, `main.go`, `README.md`, `docs/architecture.md`, `CHANGELOG.md` (Modify)

*   **Proposed Changes**: Add `risk` to `subcommands` and `run`; usage text; README section "Risk
    labels"; architecture note that `reviewer risk` is a separate advisory job that reads labelling
    inputs from the merge base.

#### PR-59: CODEOWNERS lint and docs-only setup

##### 20. `cmd/reviewer/owners.go` and `owners_test.go` (Create); `flags.go`, `main.go` (Modify)

*   **Proposed Changes**: `reviewer owners check`, added to `twoWordSubcommands` as `"owners":
    {"check": true}` and dispatched in `run`. It reads CODEOWNERS from the working tree (first of
    `.github/`, root, `docs/`) and `.review/config.yaml`, and returns `errCheckFailed` (exit 1)
    listing every violation: parse errors; first rule is not `*` with an owner; CODEOWNERS does not
    own itself; an unowned rule is not a directory; a never-allowlist path is unowned (the
    `guidance:` paths, `laneB.promptDir`, and the compiled-in list from Part 2). Tests: each
    violation alone, all together, and a passing file.

##### 21. `docs/adr/0036-codeowners-lint-exits-nonzero.md` (Create) and `docs/adr/README.md` (Modify)

*   **Proposed Changes**: ADR-0036, "The CODEOWNERS lint is a check, not a review". It records why
    it exits 1, citing `rules test`, and that at 0 required approvals the lint is what keeps G5 true.

##### 22. `docs/merge-classes.md` (Create)

*   **Proposed Changes**: The CODEOWNERS template (`*` owned first, allowlist unowned last); the
    never-allowlist; the ruleset settings to change and to leave alone; the lint as a required
    check; the Q1 and Q2 sandbox steps with expected results marked unverified; the failure policy
    (escape definition, weekly sample of 5 from `reviewer outcomes --only-unreviewed`, the
    CODEOWNERS team as owner, approvals back to 1 after the first escape); the rollback; the rule
    that a published docs directory leaves the allowlist.

##### 23. `README.md`, `CHANGELOG.md` (Modify)

*   **Proposed Changes**: Link the guide and the new command.

#### PR-60: baseline

##### 24. `docs/designs/trials/risk-gated-merge-baseline.md` (Create)

*   **Proposed Changes**: Today's ruleset; the pre-code query's counts; `reviewer outcomes --since
    90d` output for each enabled repository (time to first approval, strict and native docs-only
    counts, outcome and first-review labels), with names replaced by letters as the trials README
    requires; the 30-item hand audit of outcome labels with its agreement rate; the Q1, Q2 and Q3
    sandbox results with the plan tier and date.

#### PR-61 to PR-66: `merge-gate` (later stages)

##### 25. `internal/ghclient/client.go`, `status.go` and tests (Modify and Create, PR-61)

*   **Proposed Changes**: On `*REST`, kept off the review `Client` interface:
    `CollaboratorPermission(ctx, repo, login) (string, error)`; `GetBranch(ctx, repo, name)
    (Branch, error)`; `Variable(ctx, repo, name) (string, bool, error)`; head and base repository
    identity on `PullRequest`. `GetContentsAt` and `CompareFiles` already exist (PR-58, PR-55).
    A one-method interface in `status.go`, apart from `Writer`: `StatusWriter { CreateStatus(ctx
    context.Context, repo Repo, sha, state, context, description, targetURL string) error }`.
    Gate calls retry up to 3 times with jittered backoff on 5xx and secondary rate limits; the
    review path does not retry.

##### 26. `internal/policy/policy.go`, `predicates.go` and tests (Create, PR-62)

*   **Proposed Changes**: `Decode(raw []byte) (Policy, error)` with `KnownFields(true)` and
    `version: 1`. `Policy` holds classes with `Name`, `Enabled`, `Allow []string` (pattern
    syntax), `PrecisionTarget`, `Failure`, `OnFailure`. `NeverQualify` is a compiled-in list
    `Decode` cannot change. `Evaluate(p Policy, files []ghclient.ChangedFile) (class string,
    reasons []string)` is pure.

##### 27. `cmd/merge-gate/main.go`, `decide.go`, `human.go`, `record.go` and tests (Create, PR-63)

*   **Proposed Changes**: The decision in four steps as in Part 4. Exit codes: 0 pass, 2 waiting
    (with a reason code), 1 error.
    *   `human.go` implements the Part 4 human-path rule, including the carry-over across commits
        that only merge the base branch, as one function with a truth-table test, one row per rule.
    *   `decide_test.go` walks every error path and asserts a non-zero exit.
    *   `imports_test.go` reads the package's imports with `go/parser` and fails on anything outside
        `ghclient`, `diff`, `glob`, `policy` and the standard library; a second test fails if any
        file under `cmd/reviewer` references `StatusWriter`.
    *   `record.go` writes the decision record to `$GITHUB_STEP_SUMMARY`, the `<!-- rv:gate -->`
        comment and the `merge-gate` status; a failure to post is logged and does not change the
        exit code. A test asserts the status state matches the exit code (0 success, 2 pending,
        1 failure).

##### 28. ADR-0037 to ADR-0040 (Create, PR-63); ADR-0009, ADR-0018, ADR-0026 and `docs/adr/README.md` (Modify)

*   **Proposed Changes**: The four ADRs as in "Changes to recorded decisions". Each superseded ADR's
    status block changes to `Superseded` with a link to its successor, which `tools/checkadrs`
    requires; their bodies stay unchanged.

##### 29. `merge-gate/action.yml`, `.goreleaser.yaml`, `.github/workflows/release.yml`, `internal/installer/templates/merge-gate.yml.tmpl` (Create or Modify, PR-64)

*   **Proposed Changes**: A composite action at `merge-gate/` that installs the gate binary by
    version and checksum and runs it; it checks out nothing. A second build and archive in
    `.goreleaser.yaml` (two binaries, `reviewer` and `merge-gate`, beside the npm plugin: three
    release artifacts). A workflow template with `permissions: { contents: read,
    pull-requests: write, statuses: write }` that `docs/merge-classes.md` tells admins to pin by
    commit SHA in the required-workflow rule. `reviewer init` does not install it.

##### 30. `cmd/merge-gate/audit.go`, `sweep.go` and tests (Create, PR-65)

*   **Proposed Changes**:
    *   `merge-gate audit --since 1d`, run daily on `main`: for each merged pull request it finds the
        `merge-gate.yml` run for the merged head SHA (`ListWorkflowRuns`, matched by file path) and
        reports any merge without a passing run or a recorded bypass, copying its findings into its
        own job summary.
    *   `merge-gate sweep`, run by hand after the kill switch flips: it re-runs the gate for open
        pull requests whose last pass was by class.

##### 31. `docs/merge-classes.md` (Modify, PR-66)

*   **Proposed Changes**: Add the runbooks: rollout in four stages with the ruleset state at each,
    rollback in reverse, break glass (who holds the bypass, what it skips, how it is audited), kill
    switch and sweep.

### Verification Plan

*   **Compilation/Build**: `make build`; `goreleaser build --snapshot --clean` once PR-64 adds the
    second binary.
*   **Unit Tests**: `make test`, including the error-path walks for `reviewer risk` (exit 0 after
    the first API call) and `merge-gate` (never exit 0 on error), the glob and CODEOWNERS tables,
    the label-list test for G2, the reach fixture, and the gate's human-path truth table.
*   **Static Analysis & Linting**: `make check` (vet, gofmt, staticcheck, licence headers,
    `tools/checkadrs`, `tools/checklicenses`); `go run ./tools/audithistory --base origin/main`
    on every pull request.
*   **Test Coverage Requirement**: every exported function in `internal/glob`, `internal/codeowners`,
    `internal/risk`, `internal/policy` and `cmd/merge-gate` has a table test; every error return in
    `cmd/merge-gate` is reached by a test.
*   **E2E/Integration**:
    *   The Q1 and Q3 sandbox steps and the Q2 check, before PR-56 and PR-59 are used in a real
        repository.
    *   `reviewer risk --dry-run --pr N` on 20 recent pull requests in one TypeScript repository.
        Pass: a person agrees every label has a true reason, and reach is available on at least
        half of them; otherwise reach is reconsidered.
    *   `reviewer outcomes` on the same repository; the 30-item hand audit of `hotfixed` and the
        first-review labels agrees at 90% or better before any label informs a merge class.
    *   G7's sandbox step, and the Q4 checks, before PR-63 is required anywhere.
*   **Success metrics**: the G1 to G6b checks as written in Goals; after 4 weeks, median time to
    first approval compared with the PR-60 baseline, read alongside the per-label table, since the
    labels themselves may change review time.

--------------------------------------------------------------------------------

<!-- Provenance: Indicates initial structural drafting via EGM. -->

*Drafted with EGM*
