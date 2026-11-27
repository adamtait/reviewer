<!-- SPDX-License-Identifier: MIT -->
Status: resolved

# Goldfish Annotated Review Report

**Resolution (2026-11-27):** the author chose "auto accept". All 23 PROPOSE FIX items were applied
to `docs/designs/risk-gated-merge.md`. A7 (ESCALATE) was bypassed under auto accept, so Part 4 keeps
its depth, with A2 and the Part 4 parts of A13, A14, A18 and A22 fixed in place. A25, A26 and the
leftover Stage 9 Nits were declined. Accepted: 23. Contested: 0 (two partial contests inside A4 and
A10, applied as described). Escalated: 1. Declined: 2 plus the Nit group. Unaddressed: 0.

**Target:** `docs/designs/risk-gated-merge.md` (full file, at commit `b8db37b`)
**Findings list:** the 26 action items in `consolidated_review_report.md` (159 raw findings).
**Author direction given before resolution:** keep `risk/reach` in v1 if the extra pieces are
reasonable to build, and scope them; keep the first-review and outcome labels in `reviewer outcomes`
unless they cannot be made reliable.

Summary: 23 PROPOSE FIX, 1 ESCALATE, 2 DECLINE (plus the leftover S9 Nits, declined as a group). Two fixes include a partial contest, stated in their
justification.

## Escalated

### [A7] [`zoom-out`, `design`, `implementation-readiness`] - Severity: Major
*   **Affected Scope:** Lines 411-517 (Part 4), 775-788 (stack table), 975-1060 (PR-61 to PR-66)
*   **Discrepancy Description:** Part 4 is designed to implementation depth before its
    precondition (Q4) exists; about 40% of Stage 6-7 findings sit in it.
*   **Rationale:** Repairing it in place would bloat a stage that cannot yet be validated.
*   ***AI Suggestion:*** ESCALATE
*   ***Justification:*** The critique is reasonable, but it reverses a decision the author made
    while drafting ("v1 plus merge-gate" scope) that is not recorded in the document, so no reviewer
    could see it. Two ways forward:
    *   **Cut to a sketch** (reviewers' recommendation): Part 4 becomes under a page:
        preconditions, G7-G10 as principles, "v1 does not depend on this", and a requirements list
        for the follow-up design carrying A2, the kill-switch window, gate side effects that can fail
        an approved pull request, the comment/permission mismatch, "waiting" shown as a failing
        check, and the PR-61 to PR-66 gaps. PR-61 to PR-66 leave the plan. About 350 lines go.
    *   **Keep depth and fix in place:** apply A2 and the Part 4 parts of A14, A18 and A22 inside
        the existing Part 4 and PR-61 to PR-66, and record the scope decision in the Overview.
*   ***Proposed Edit:***
    *   Location: Part 4, Detailed Implementation Plan PR-61 to PR-66.
    *   Change: depends on the author's choice above.

## Prioritized Findings & AI Suggestions

### [A1] [`design`, `zoom-out`] - Severity: Blocker
*   **Affected Scope:** Lines 383-406 (Part 2), 91-95 (G4), 1000-1010 (PR-59)
*   **Discrepancy Description:** The docs allowlist can admit files that models and agents read as
    instructions; the exclusion is undefined and unenforced.
*   **Rationale:** `guidance:` files feed the model-lane prompt (`internal/config/config.go:29`,
    `internal/laneb/gather.go:39-47`); an injected agent could merge unreviewed text that steers
    later reviews.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid and the most serious v1 issue. The fix must be enforced by a check,
    not by prose.
*   ***Proposed Edit:***
    *   Location: Part 2 allowlist bullet; G4; PR-59.
    *   Change: Replace "never agent instruction files, ... or markdown that code reads" with a
        concrete never-allowlist: every path listed under `guidance:` in `.review/config.yaml`,
        `laneB.promptDir`, files named `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `SKILL.md`,
        `.cursorrules` and anything under `.claude/`, `.github/`, `.review/`, and any file
        embedded by code (`//go:embed` targets). The allowlist names directories people alone read.
        `reviewer owners check` (A10) fails if any never-allowlist path is unowned.

### [A2] [`inventive-catalyst`, `design`, `comprehension`] - Severity: Blocker
*   **Affected Scope:** Lines 475-476 (human path), 496-498 (revocation), 112-114 (G8)
*   **Discrepancy Description:** Approval only on the exact head, plus "branches must be up to
    date", voids every approval on every merge; G8 cannot pass.
*   **Rationale:** Every merge to `main` forces other pull requests to update, creating a new head
    with no approval.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid. Under either A7 option the rule must change.
*   ***Proposed Edit:***
    *   Location: Part 4 human path (or the sketch's requirements list).
    *   Change: "An approval on commit A still counts for head B when every commit between A and B
        is a merge of the base branch into the pull request and the pull request's own changes are
        identical at A and B (the three-dot comparison of base...A and base...B lists the same files
        with the same patches). Any other commit voids it." G8's check compares against GitHub's
        state with "dismiss stale approvals" on, which treats update merges the same way
        (unverified).

### [A3] [`structural`, `zoom-out`] - Severity: Blocker
*   **Affected Scope:** whole document (no section)
*   **Discrepancy Description:** No Risks and Limits section.
*   **Rationale:** Required by the feature-spec standard; risks are scattered.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid.
*   ***Proposed Edit:***
    *   Location: new section after Non-Functional Requirements.
    *   Change: a v1 table with rows: CODEOWNERS misconfiguration removes review (mitigation: A10
        lint as a required check); deletes and renames inside the allowlist merge unreviewed
        (accepted, A9); instruction files on the allowlist (A1 check); a published docs directory
        is production content (Q8, leaves the allowlist); labels suppressed by the pull request
        (A12, base-branch config); no label read as "safe" (comment wording); reach unavailable
        for some merge bases (stated in the comment, availability measured); risk job holds
        `actions: read` (read-only, own job); API rate limits (bounded calls, partial output).

### [A4] [`implementation-readiness`, `codebase-details`, `zoom-out`] - Severity: Blocker
*   **Affected Scope:** Lines 345-356 (Reach input), 907-914 (PR-58 reach)
*   **Discrepancy Description:** Reach cannot be built with `internal/workspace/navigation` as it
    is: no symbols-on-a-line query, a whole-commit archive is needed, `internal/diff` has no
    base-side line numbers.
*   **Rationale:** None of these is on the file list.
*   ***AI Suggestion:*** PROPOSE FIX (with a partial contest)
*   ***Justification:*** The missing query and base-side lines are valid gaps. The archive claim is
    contested: the archive is needed only by `navigation.Build`, which checks positions against
    file text for display (`load.go:94-108`). Reach needs only the decoded index:
    `scip.Decode(raw, limits)` (`internal/workspace/navigation/scip/decode.go:166`) returns each
    document's path and occurrences with a `Definition` flag (`decode.go:35-53`), which is enough
    to find symbols defined on a line and count the files that reference them. Scope, judged
    reasonable (about 300 lines plus tests, no new dependency):
    1.  Move `internal/workspace/navigation/scip` to `internal/scip` (pure move; update workspace
        imports), so the CLI does not depend on a workspace package.
    2.  Extract `classify` and `unzipIndex` from `navigation/fetch.go` into a new
        `internal/scipfetch` package used by both the workspace and `reviewer risk`.
    3.  Add `diff.BaseLines(patch string) []int` to `internal/diff`: the base-side line numbers of
        removed lines, parsed from hunk headers.
    4.  Add `risk.Reach(ix *scip.Index, path string, baseLines []int) []SymbolReach` in
        `internal/risk`: symbols with a definition occurrence on those lines (non-local only),
        each with the count of other documents holding a non-definition occurrence.
    5.  Only an index built from exactly the merge base is used; otherwise "reach not
        available", with the reason. The dry-run on 20 pull requests measures how often it is
        available.
    Cost per run: list runs (1), list artifacts (1), one artifact download bounded by the same
    size limit the workspace uses.
*   ***Proposed Edit:***
    *   Location: Part 1 Reach bullet; PR-57 and PR-58 file lists; NFR API cost.
    *   Change: replace the Reach bullet and the PR-58 reach sentence with steps 1-5 above; add the
        three packages to the file list; drop "queried with the reader in
        `internal/workspace/navigation`".

### [A5] [`implementation-readiness`, `codebase-details`, `comprehension`] - Severity: Blocker
*   **Affected Scope:** Lines 330-338, 887-901, allowlist mentions at 385-393 and 801
*   **Discrepancy Description:** Built-in risk patterns, `risk.yaml` schema and pattern grammar are
    unspecified; no `**` matcher exists; three allowlist syntaxes; CODEOWNERS location unspecified.
*   **Rationale:** An implementer would have to decide them.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid.
*   ***Proposed Edit:***
    *   Location: Part 1 risk map; new "Pattern syntax" paragraph in Terms; PR-57.
    *   Change: one glob grammar for the risk map, the allowlist and CODEOWNERS-style matching:
        `*` within a segment, `**` across segments, trailing `/` means a directory, leading `!`
        excludes, last match wins; implemented once in `internal/glob` with a table test. A table
        of default patterns per label (for example `risk/auth`: `**/auth/**`, `**/*auth*.{ts,py}`,
        `**/permissions/**`, `**/crypto/**`, `**/payments/**`; `risk/data`: `**/migrations/**`,
        `**/*.sql`, `**/schema.*`; `risk/ci`: `.github/**`, `.review/**`, agent instruction files;
        `risk/deps`: lockfiles and manifests by name; `risk/infra`: `**/*.tf`, `**/k8s/**`,
        `Dockerfile*`). A `risk.yaml` example with `version: 1` and `add: {risk/auth: [...]}`, and a
        rejected entry (`remove:` or an unknown label). CODEOWNERS is read from `.github/`, root,
        then `docs/`, as GitHub does (unverified), and only the first found.

### [A6] [`implementation-readiness`, `codebase-details`] - Severity: Blocker
*   **Affected Scope:** Lines 797-843 (PR-55)
*   **Discrepancy Description:** PR-55 needed client reads the plan did not define.
*   **Rationale:** `merged_at`, review-to-comment linkage, a files-returning compare, the
    ready-for-review source.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid, and already resolved in code on `risk-merge/03-outcomes`.
*   ***Proposed Edit:***
    *   Location: PR-55 entries.
    *   Change: describe what was built: `ListReviews` on `Client`; `MergedAt`,
        `ReviewComment.PullRequestReviewID`; `ListTimelineEvents` and `CompareFiles` on `*REST`
        only; the `unknown` first-review label; author and bot reviews ignored; fakes updated.

### [A8] [`design`, `zoom-out`] - Severity: Major
*   **Affected Scope:** Lines 125-127 (G12), Part 2
*   **Discrepancy Description:** Docs-only goes live in v1 with no failure policy.
*   **Rationale:** No escape definition, detection, audit owner or rollback trigger.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid.
*   ***Proposed Edit:***
    *   Location: G12 moves to v1; Part 2 gains a "When it goes wrong" paragraph.
    *   Change: an escape is a docs-only merge with no review that is later reverted, modified by a
        fix (A17), or flagged in a weekly sample of 5 such merges read by the repository's
        CODEOWNERS team; `reviewer outcomes --only-unreviewed` lists them; after the first escape,
        required approvals go back to 1 until the cause is fixed.

### [A9] [`design`, `comprehension`, `zoom-out`] - Severity: Major
*   **Affected Scope:** Lines 91-95 (G4), 383-393, 801-802
*   **Discrepancy Description:** Native settings cannot enforce "added or modified".
*   **Rationale:** Deletes, renames, mode changes and symlinks inside unowned paths merge.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid.
*   ***Proposed Edit:***
    *   Location: G4; Part 2; Q1; PR-55.
    *   Change: G4 becomes "a pull request whose every changed path is unowned merges once CI is
        green"; deletes and renames are accepted risks (A3); Q1's sandbox steps add a delete, a
        rename and a symlink case; `outcomes` reports both the strict count (added or modified) and
        the native count (every path unowned).

### [A10] [`zoom-out`, `design`, `structural`] - Severity: Major
*   **Affected Scope:** Lines 238-254 (CUJ 4), 96-98 (G5), 394-399, PR-59
*   **Discrepancy Description:** The ownership check guards a case that cannot occur and is never
    required.
*   **Rationale:** With `*` owned first, every path has an owner.
*   ***AI Suggestion:*** PROPOSE FIX (with a partial contest)
*   ***Justification:*** Valid. Contested only on "drop `internal/codeowners`": `risk/wide` counts
    owner groups crossed (Part 1, "Owner groups" input), which needs a parser regardless.
*   ***Proposed Edit:***
    *   Location: CUJ 4, G5, Part 2, PR-59.
    *   Change: `reviewer owners check` becomes a structural lint: the first rule is `*` with an
        owner; CODEOWNERS owns itself; every unowned rule is a directory; no never-allowlist path
        (A1) is unowned; the file parses with no unsupported syntax. It is a required check in the
        ruleset. CUJ 4 is rewritten as "someone adds an unowned rule for `scripts/`".

### [A11] [`comprehension`, `design`, `implementation-readiness`] - Severity: Major
*   **Affected Scope:** Lines 341, 371
*   **Discrepancy Description:** `risk/findings` depends on confidence the markers do not carry.
*   **Rationale:** `fingerprint.Marker(fp, ruleID)` (`internal/fingerprint/fingerprint.go:30`)
    holds no confidence.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid. Dropping the label is cheaper than changing the marker format.
*   ***Proposed Edit:***
    *   Location: Part 1 inputs and label table; PR-57, PR-58.
    *   Change: remove `risk/findings` and the "Review findings" input. The label set becomes nine
        with `risk/reach`.

### [A12] [`zoom-out`, `design`, `comprehension`] - Severity: Major
*   **Affected Scope:** Lines 84-86 (G2), 229-236 (CUJ 2), 330-344
*   **Discrepancy Description:** A pull request can suppress its own labels; a missing label reads
    as safe.
*   **Rationale:** `risk.yaml` and CODEOWNERS are read from the head.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid; reading from the base is one contents API call each.
*   ***Proposed Edit:***
    *   Location: Part 1 inputs; CUJ 2; G2.
    *   Change: read `.review/risk.yaml` and CODEOWNERS at the merge-base SHA through the contents
        API; `risk/review-config` also fires for any CODEOWNERS location; the comment carries the
        head SHA, a run link, and "No label means no rule matched. It does not mean the change is
        safe." CUJ 2's outcome becomes deterministic.

### [A13] [`zoom-out`, `comprehension`, `design`, `implementation-readiness`] - Severity: Major
*   **Affected Scope:** Lines 79-82 (G1), 319-325, 503-507
*   **Discrepancy Description:** G1 has no v1 check; `needs: review` skips the risk job when review
    fails.
*   **Rationale:** The only audit is Part 4's.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid. With A11 the risk job no longer reads review markers, so the
    dependency can go.
*   ***Proposed Edit:***
    *   Location: Part 1 "Where it runs"; G1; PR-58; installer template.
    *   Change: drop `needs: review`; the jobs run in parallel. G1's check: `reviewer outcomes`
        reports merged pull requests with no `<!-- rv:risk -->` comment. Drafts: the job runs on
        `ready_for_review` and `synchronize` (the workflow adds `types:`). Forks: the risk job runs
        with a read-only token and posts nothing; the gap is stated.

### [A14] [`comprehension`, `design`, `implementation-readiness`] - Severity: Major
*   **Affected Scope:** Lines 23, 465, 630, 1034-1043; ADR entries
*   **Discrepancy Description:** ADR relationships unrecorded; supersessions would fail
    `tools/checkadrs`; ADR-0039 wording conflicts with `owners check`.
*   **Rationale:** Superseded ADRs' status blocks must change and are not listed.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid.
*   ***Proposed Edit:***
    *   Location: new "Changes to recorded decisions" table; PR-63 entry (or sketch).
    *   Change: v1 rows: ADR-0035 (advisory risk record, new verbs, risk job permissions; relates
        to ADR-0018), ADR-0036 (ownership lint exits 1; relates to ADR-0009). Later rows list each
        supersession and name the superseded files whose status blocks change. ADR-0039 says
        "`reviewer` reviews never block", which `owners check` (a check, not a review) does not
        contradict.

### [A15] [`comprehension`, `structural`, `zoom-out`] - Severity: Major
*   **Affected Scope:** Lines 110, 193-212, 333, 389, 405, 449, 455, 483, 509, 659, 1022, 1081-1085
*   **Discrepancy Description:** Unresolvable brief references, experiment numbers, decision IDs,
    and a "D5" collision.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid. A fresh reader needs the content, not the pointer.
*   ***Proposed Edit:***
    *   Location: all listed lines; Alternatives headings.
    *   Change: restate each referenced point inline in one clause; remove decision IDs; describe
        each experiment the v1 depends on in one line (Q1: a docs edit, delete, rename and symlink
        under an unowned path, plus an owned-path edit, at 0 approvals with code-owner review on;
        Q3: label add and remove with `pull-requests: write`); Alternatives use `Alt-1`... labels.
        Add one sentence: "The brief is provenance only; everything this design needs from it is
        restated here."

### [A16] [`comprehension`, `structural`, `zoom-out`] - Severity: Major
*   **Affected Scope:** Lines 1-60, Terms
*   **Discrepancy Description:** `reviewer`, "lane", "review workspace", "lens" undefined; "label"
    means two things.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid; a comprehension gap is not contestable.
*   ***Proposed Edit:***
    *   Location: new "Background" section before The Problem; Terms.
    *   Change: Background (about 8 lines): what `reviewer` is, how it runs in CI, its two lanes,
        the always-exit-0 contract, configuration in `.review/`, the navigation index, the review
        workspace in one sentence. Terms: "GitHub label" vs "outcome label", "lane". Remove "lens"
        from Alternatives in favour of "raised in review".

### [A17] [`implementation-readiness`, `zoom-out`; smoke run] - Severity: Major
*   **Affected Scope:** Lines 395-412 (Part 3), 797-812 (PR-55)
*   **Discrepancy Description:** `hotfixed` compares line numbers across unrelated patches; several
    label cases were undefined; the smoke run labelled 10 of 21 merged pull requests `hotfixed`.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid. The labels can be made reliable for the part that is mechanical:
    which earlier pull request introduced a line is exact with blame, and the client already has a
    GraphQL endpoint (`internal/ghclient/client.go:190-216`). Whether a later change is a fix is
    intent, which no line rule decides, so it becomes a separate, audited judgement:
    *   **`modified-later`** (exact): a pull request merged within 14 days removes or changes a line
        that GraphQL `blame` on its base commit attributes to this pull request's merge commit. One
        blame call per (later pull request, file) pair, only for files both touched.
    *   **`hotfixed`** (heuristic, audited): `modified-later` and the later pull request's title,
        body or branch matches `fix|bug|hotfix|regress|revert`, or it references this pull request
        by number.
    *   First-review labels are kept as built; the open cases PR-55 settled are written into the
        plan. Neither label feeds a merge decision until the 30-item hand audit agrees at 90% or
        better.
*   ***Proposed Edit:***
    *   Location: Part 3 outcome bullets; PR-55 entries; new stack entry PR-55b ("outcomes: exact
        line attribution") before PR-56.
    *   Change: as above.

### [A18] [`implementation-readiness`, `codebase-details`] - Severity: Major
*   **Affected Scope:** Lines 820-1010 (PR-55 to PR-59)
*   **Discrepancy Description:** Plan-to-code mismatches: fakes not listed, exit codes, name clash,
    flag validation, missing wiring, label sync format and authorship, label verb details, revert
    reads, unbounded retry.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid; each is a decision the plan must make.
*   ***Proposed Edit:***
    *   Location: PR-56 to PR-59 entries.
    *   Change: list `internal/poller/poller_test.go`, `internal/reporters/github_test.go` and
        `clientMethods` wherever an interface grows; state that configuration errors before the
        first API call still exit 2 as `errUsage` does, and every later error exits 0 with the
        reason in the comment when the API is reachable; name the command file `riskcmd.go` with
        `func runRisk`; extend `measures()` (already done for `outcomes`) and add `owners check`
        to `twoWordSubcommands` and `run`; hidden label list as `<!-- rv:risk-labels a,b,c -->`,
        trusted only on a comment authored by the token's own login, and current labels read before
        syncing; label verbs URL-escape names, create a missing label implicitly (unverified) and
        are tested with httptest; revert history uses `GET /repos/{o}/{r}/commits?path=&since=`
        with a new `ListCommits` read; retry is three attempts with jittered backoff, gate-only.

### [A19] [`style`, `design`] - Severity: Major
*   **Affected Scope:** throughout
*   **Discrepancy Description:** "Unverified" applied inconsistently; unsourced figures.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid.
*   ***Proposed Edit:***
    *   Location: throughout.
    *   Change: every statement of GitHub behaviour carries "(unverified)" until a sandbox result
        replaces it; "more than 90%" is attributed to the requester; the 3/n bound states 95%
        confidence.

### [A20] [`comprehension`, `style`] - Severity: Major
*   **Affected Scope:** Lines 660-724 (NFR)
*   **Discrepancy Description:** NFR contradicts Part 1.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid.
*   ***Proposed Edit:***
    *   Location: NFR.
    *   Change: latency stated for the risk job (under 60 seconds including one runner start);
        rollback is "remove or comment out the `risk` job"; API counts drop the local read and add
        the merge-base and contents calls.

### [A21] [`design`, `structural`, `zoom-out`] - Severity: Major
*   **Affected Scope:** Lines 519-700 (Alternatives), 129-170 (Non-Goals), Open Questions
*   **Discrepancy Description:** "Rejected by author" without trade-offs; Non-Goals point to
    missing rationale; no-App premise not an Open Question.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid.
*   ***Proposed Edit:***
    *   Location: each Alternative; Non-Goals; Open Questions.
    *   Change: each "Rejected by author" gains the deciding trade-off in one line (D1: one
        repository keeps shared packages and releases in step); the non-goal on other merge classes
        lists each class with its reason inline; new Q0 "Does organisation policy disallow GitHub
        Apps?" holding up nothing in v1 and the choice between an App and a required workflow later.

### [A22] [`design`] - Severity: Major
*   **Affected Scope:** Goals, Part 3
*   **Discrepancy Description:** Nothing measures whether risk labels help.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid; it is the review-time goal's only evidence.
*   ***Proposed Edit:***
    *   Location: new goal G6b; Part 3.
    *   Change: `reviewer outcomes` reports, per `risk/` label, how often it fired and median hours
        to first approval with and without it; a label that fires on more than half of pull
        requests for 4 weeks is reviewed for deletion, as ADR-0024 does for rules.

### [A23] [`implementation-readiness`, `comprehension`, `zoom-out`] - Severity: Major
*   **Affected Scope:** Lines 765-788, 1060-1070
*   **Discrepancy Description:** Stack table, pre-code query and baseline file underspecified.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid. The pre-code query has been run once on this repository (1 of 21
    merged pull requests docs-only), which shows the shape of the output.
*   ***Proposed Edit:***
    *   Location: "Before any code" paragraph; stack table; PR-60.
    *   Change: the query's owner is the repository admin, its output goes into the PR-60 trials
        file, and Part 2 is dropped if under 5% of merged pull requests qualify; sandbox runs happen
        before PR-56 and PR-60 only records them; one placeholder ("—") in the gate column; PR-59
        may merge before Q1 but is not used until Q1 and Q2 pass; PR-60 covers every G6 measure.

### [A24] [`style`, `comprehension`, `design`] - Severity: Major
*   **Affected Scope:** Lines 17-38, 46-50, 229-236, 280-290
*   **Discrepancy Description:** Overview count; "no organisation-level change" vs Q2; "no
    production risk" vs Q8; CUJ 8 and CUJ 2 overclaim.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Valid.
*   ***Proposed Edit:***
    *   Location: Overview, Problem, CUJ 2, CUJ 8.
    *   Change: "a v1 in three independent parts, and later stages that need an organisation
        owner"; "needs no organisation-level change unless an organisation ruleset already sets a
        minimum approval count (Q2)"; "changes that carry little production risk, unless the docs
        are published (Q8)"; CUJ 8 says passes already posted can merge until the sweep finishes;
        CUJ 2 rewritten per A12.

### [A25] [`style`, `structural`] - Severity: Nit
*   **Affected Scope:** throughout
*   **Discrepancy Description:** Heading levels, rules, wrap width, diagram, filler, metaphors.
*   ***AI Suggestion:*** DECLINE
*   ***Justification:*** Recorded. Applied only to text rewritten for other fixes, to avoid a
    whole-document churn that makes the substantive changes hard to review.
*   ***Proposed Edit:*** none.

### [A26] [`zoom-out`] - Severity: Nit
*   **Affected Scope:** n/a
*   **Discrepancy Description:** Additions to reject (separate allowlist file, review-state-aware
    CI check, 19-experiment appendix, full ADR table).
*   ***AI Suggestion:*** DECLINE
*   ***Justification:*** Recorded so they are not reintroduced; no edit.
*   ***Proposed Edit:*** none.

### [A6-companion: smaller Nits from S9 N1-N19] - Severity: Nit
*   ***AI Suggestion:*** DECLINE
*   ***Justification:*** Folded into A18 where they affect the build; the rest recorded.
