<!-- SPDX-License-Identifier: MIT -->
# Consolidated review report: risk-gated merge

**Target:** `docs/designs/risk-gated-merge.md` at commit `b8db37b` (1,093 lines; FeatureRange
1-757, DetailedRange 758-1093)
**Mode:** consolidated report, all five stages, no checkpoints. GoldfishContext: none.
**Verdict:** not ready. The feature spec needs one security fix and a structural cut, and the
implementation plan fails the zero-decision test for PR-57 and PR-58.

## Stage status

| Stage | Reviewers | Status | Raw findings (B / M / N) | Report |
| --- | --- | --- | --- | --- |
| 5 Initial zoom out | zoom-out, inventive-catalyst | completed | 1 / 10 / 14 | `zoom_out_report.md` |
| 6 Comprehension | comprehension | completed | 0 / 9 / 11 | `comprehension_report.md` |
| 7 Critic | design, structural, style | completed | 2 / 22 / 27 | `critic_report.md` |
| 8 Intermediate zoom out | zoom-out | completed | 1 / 10 / 4 | `intermediate_zoom_out_report.md` |
| 9 Implementation readiness | implementation-readiness, codebase-details, style | completed | 3 / 26 / 19 | `implementation_readiness_report.md` |

159 raw findings, consolidated below into 26 action items. Finding references use the stage number
and the finding's heading or ID in that stage's report (for example `S9-B1`, `S7 "Missing Risks"`).

## Conflicts with decisions the author already made

Two reviewer recommendations reverse choices the author made at reconciliation. They are listed
here and not applied; the author decides.

* **Reach in v1** (author chose to keep it). S5, S8-7 and S9-B1 recommend cutting it: it needs an
  API to list symbols defined on a line that `internal/workspace/navigation` does not have, a full
  archive download of the base commit, base-side line numbers `internal/diff` does not keep, and a
  new dependency from the CLI on a workspace package. Either specify those pieces (A4) or defer
  reach.
* **First-review labels in `reviewer outcomes`** (author chose to add them). S5 and S8-9 recommend
  trimming Part 3 to time to first approval, the candidate count and a list of merges with no
  review. PR-55 is already built with the labels (see "Implementation status").

## Prioritised action items

### Blocker

**A1. The docs allowlist admits files that models and agents read as instructions.**
`guidance:` files feed the model-lane prompt (`internal/config/config.go:29`,
`internal/laneb/gather.go:39-47`, `internal/laneb/assemble.go:42-45`). The exclusion of "agent
instruction files" and "markdown that code reads" (lines 390-391) is undefined and unenforced, so an
injected agent could merge unreviewed text that steers later reviews.
*Fix:* allow only directories people alone read; make `reviewer owners check` fail when any
`guidance:` path, prompt directory or agent instruction file falls under the allowlist; list the
excluded file kinds concretely. *Sources:* S7 "docs allowlist", S8-1.

**A2. Approval on the exact head plus "branches must be up to date" voids every approval on every
merge (Part 4).** Every merge to `main` forces every other open pull request to update, which makes
a new head with no approval. G8 cannot pass and rollout stage 3 can never start.
*Fix:* record as a requirement on the follow-up gate design (for example, an approval stays valid
across commits that only merge the base branch in, verified by comparing trees). *Sources:* S5
Blocker, S7 "G8 cannot pass", S7 "Require branches to be up to date", S6-17.

**A3. No Risks and Limits section.** The feature-spec standard requires one. Risks are scattered or
absent: CODEOWNERS misconfiguration, deletes through the allowlist, the kill-switch window, Q8,
rate limits, the risk job's permissions, label suppression.
*Fix:* add a v1 Risks and Limits table of about seven rows. *Sources:* S7 "Missing Risks", S8-10.

**A4. Reach cannot be built as specified.** See "Conflicts" above.
*Fix (if kept):* add to the file list a symbols-on-line query in the navigation reader (or a new
shared package), base-side line tracking in `internal/diff` or from patches, the archive
download and its cost, and a fallback when the merge base has no index run (the workspace design
already notes such commits exist, `review-workspace.md:615`). *Sources:* S9-B1, S5 reach, S8-7.

**A5. The risk map, the `risk.yaml` schema and the pattern grammar are left to the implementer.**
No built-in patterns are listed; payments and agent instruction files have no label; the repository
has no `**` matcher; the allowlist uses three syntaxes and has no exclusion; the CODEOWNERS location
is unspecified.
*Fix:* list the default patterns per label, show `risk.yaml` with an accepted and a rejected entry,
choose one glob grammar (with `**` and exclusions) shared by the risk map, the allowlist and
CODEOWNERS matching, and state which CODEOWNERS locations are read. *Sources:* S9-B3, S9-M12,
S9-M13, S6-20.

**A6. PR-55's client reads were undefined in the plan.** `merged_at`, review-to-comment linkage, a
files-returning compare and the ready-for-review source were missing.
*Status:* resolved in code by PR-55 (`MergedAt`, `PullRequestReviewID`, `ListReviews`,
`ListTimelineEvents`, `CompareFiles`). *Fix:* update the plan text to match. *Sources:* S9-B2.

### Major

**A7. Cut Part 4 to a sketch.** It is designed to implementation depth before its precondition
(Q4) exists, about 40% of Stage 6-7 findings sit in it, and repairing it in place would bloat the
document. Keep: preconditions, G7-G10 as principles, a statement that v1 does not depend on it, and
a one-line requirements list for the follow-up design that carries A2, the kill-switch window, the
gate's side effects failing approved pull requests, the comment/permission mismatch, waiting shown
as a failing check, and the PR-61 to PR-66 gaps. *Sources:* S5 Part-4 Majors (5), S8-2, S9-M16,
M18, M19, M26, S5 Nits on gate budget and exit 2.

**A8. Docs-only goes live in v1 with no failure policy.** No escape definition, no detection, no
audit owner or cadence, no rollback trigger.
*Fix:* move G12 into v1 for docs-only: an escape is a reverted or hotfixed docs-only merge or one
found in the audit sample; a query lists merges with no review; after the first escape, required
approvals go back to 1. *Sources:* S7 "Docs-only goes live", S8-3.

**A9. Native settings cannot enforce G4's "added or modified".** Unowned paths let deletes,
renames, mode changes and symlinks through, and any file type under an allowed directory.
*Fix:* restate G4 as "every changed path is unowned", list deletes and renames as accepted risks,
add rename, delete and symlink cases to the Q1 sandbox steps, and make the `outcomes` predicate
match what GitHub allows. *Sources:* S5 allowlist, S6-5, S7 "Native settings", S8-5.

**A10. `reviewer owners check` guards a case that cannot occur.** With `*` owned first, CUJ 4
cannot happen and G5 passes trivially; the check is never made required; it misses matcher
differences, rule order and over-broad patterns.
*Fix:* replace path matching with a structural CODEOWNERS lint (first rule owns `*`, CODEOWNERS
owns itself, allowlist entries are directories, no guidance or instruction paths unowned), make it
a required check, and drop `internal/codeowners` if nothing else needs it. *Sources:* S5 owners,
S7 "ownership check", S8-4.

**A11. `risk/findings` depends on a confidence the markers do not carry.**
*Fix:* drop the label, or extend the marker format with confidence (an ADR-0024 change).
*Sources:* S5 Nit, S6-8, S7 Nit, S8-6, S9-M3.

**A12. A pull request can suppress its own risk labels, and a missing label reads as safe.**
*Fix:* read `.review/risk.yaml` and CODEOWNERS from the base branch through the API; flag edits to
any CODEOWNERS location; put the head SHA and run link in the comment; say in the comment that no
label means "nothing matched", not "safe". *Sources:* S5 head-config, S6-14, S7 "suppress", S8-8.

**A13. G1 has no v1 check, and `needs: review` skips the risk job when review fails.**
*Fix:* drop `needs: review` (or use `if: always()` with the dependency, unverified); give G1 a v1
query for merged pull requests without `<!-- rv:risk -->`; state fork and draft behaviour.
*Sources:* S5 needs-review, S6-6, S7 G1, S9-M9, S9-M20.

**A14. ADR relationships are unrecorded and the supersession mechanics would fail.**
`tools/checkadrs` requires the superseded ADRs' status blocks to change, and those files are not
listed; ADR-0039's "never block" wording conflicts with `owners check`; ADR-0018 is handled two ways.
*Fix:* add a two-row "Changes to recorded decisions" table for v1 (ADR-0035, ADR-0036) and move the
Part 4 supersessions into the sketch's requirements. *Sources:* S6-2, S7 ADR relationship, S9-M15,
S9-M24, S5 Nit on ADR-0035.

**A15. References the reader cannot resolve.** Brief findings, experiment numbers, decision IDs
(D-A, D-B, D-E, D5, D6), and "D5" colliding with an Alternative's label.
*Fix:* restate what is needed inline, drop the IDs, and give Alternatives a distinct prefix. Reject
the suggested 19-experiment appendix (S8-14): with Part 4 cut, only Q1-Q3's experiments remain.
*Sources:* S5 Nit, S6-3, S6-4, S6-11, S7 "References into the unreadable brief", S8-12.

**A16. A newcomer lacks background.** `reviewer`, "lane", "review workspace" and "lens" are
undefined; "label" means both GitHub labels and outcome labels.
*Fix:* a short Background section; use "GitHub label" and "outcome label" consistently; define
terms before use. *Sources:* S6-1, S6-9, S6-10, S6-12, S7 Terms, S8-11.

**A17. Part 3's measurement heuristics are noisy and partly undefined.** `hotfixed` reuses line
numbers across unrelated patches (S9-M11), first-review and outcome rules leave cases open
(S9-M10), and the smoke run of PR-55 on this repository labelled 10 of 21 merged pull requests
`hotfixed`: consecutive feature pull requests editing the same files read as hotfixes.
*Fix:* require the hand-audited sample before any number is used, narrow `hotfixed` (for example,
only later pull requests whose title or body references this one, or that change lines this one
added within 3 days), and record the open cases PR-55 resolved (see "Implementation status").
*Sources:* S9-M10, S9-M11, S5 outcome labels, S8-9, smoke run.

**A18. Plan-to-code mismatches that would break the build or tests.** New interface methods break
test fakes not on the file list (`internal/poller`, `internal/reporters`, `clientMethods`); token and
repository errors exit 2 through `errUsage`, not 0; `func risk` clashes with package `risk`; flag
validation rejects reused flags; `owners check` wiring is missing; label sync has no hidden-list
format, no authorship check and never reads current labels; label verbs lack escaping and a
label-creation policy; revert-history reads have no client method; retry is unbounded.
*Fix:* add each to the file list with the decision made. PR-55 already handled the fakes and flag
validation for its own changes. *Sources:* S9-M1, M2, M4, M5, M6, M7, M8, M14, M17, S5 Nit on flags.

**A19. "Unverified" is applied inconsistently, and key figures are unsourced.**
*Fix:* mark every GitHub-behaviour claim the same way; source or soften figures such as "90% of
pull requests" and the 300-case bound's confidence level. *Sources:* S7 two Majors, S7 Nits on
statistics.

**A20. Non-Functional Requirements contradict Part 1.** Latency stated for the review job, rollback
via an input that is not an on/off switch, API counts for local reads, missing merge-base call.
*Sources:* S6-15, S7 NFR, S5 Nit on latency.

**A21. Alternatives and Non-Goals lack rationale, and the no-App premise is not an Open Question.**
*Fix:* give each "Rejected by author" a one-line trade-off; point Non-Goals at real rationale; add
"Are GitHub Apps disallowed?" as an Open Question. *Sources:* S7 three Majors, S8-13.

**A22. Nothing measures whether risk labels help.**
*Fix:* count how often each label fires and whether labelled pull requests get longer reviews, in
the ADR-0024 style. *Sources:* S7 "Nothing measures".

**A23. Stack table, pre-code query and baseline file are underspecified.** Q3 evidence lands after
the code that needs it; the gate column mixes placeholders; the query has no owner, output location
or threshold; the baseline file does not cover G6. *Sources:* S5 Nit, S6-19, S9-M21, M22, M23, N19.

**A24. Overview and CUJ overclaims.** "Two things" vs four parts; "no organisation-level change"
vs Q2; "carry no production risk" vs Q8; CUJ 8 overstates revocation; CUJ 2 depends on an
unresolved "if". *Sources:* S6-16, S7 four Majors.

### Nit

**A25. Editorial sweep.** Heading levels, horizontal rules, wrap width, diagram alignment,
parallel table rows, filler, metaphors, overclaims ("proves"), ambiguous referents. Do this once,
after the structural changes. *Sources:* S7 Nits, S8-15, S9-N1 to N19.

**A26. Additions to reject.** S8-14 lists Stage 6-7 suggestions that would bloat the document: a
separate allowlist file, a CI check that knows review state, an appendix of all 19 experiments, a
full ADR table. Recorded so they are not reintroduced. *Sources:* S8-14.

## Implementation status (PR-55)

`reviewer outcomes` was built in parallel with this review on branch `risk-merge/03-outcomes`
(two commits, not yet pushed). `go vet`, `go test ./...`, gofmt, `tools/checkadrs` and the history
audit pass. It resolves A6 in code and settles several cases the plan left open (S9-M10):

* the latest ready-for-review before the first approval is the start time;
* author and bot reviews are ignored when finding the first human review;
* a fourth first-review label, `unknown`, covers a 404 on compare (usually a force-push) or the
  300-file compare cap;
* pull requests with 3,000 or more files are never docs-only;
* `--allowlist` accepts directories only and has no exclusion syntax yet, so "`docs/**` minus
  `docs/adr/**`" cannot be expressed (A5).

The smoke run against this repository is the evidence for A17.

## Traceability matrix

| Action | S5 | S6 | S7 | S8 | S9 |
| --- | --- | --- | --- | --- | --- |
| A1 allowlist admits instruction files | | | Blocker | 1 | |
| A2 approvals voided by up-to-date | Blocker | 17 | G8; up-to-date | | |
| A3 Risks and Limits | | | Blocker | 10 | |
| A4 reach | reach | | Nit (Proposed ADR) | 7 | B1 |
| A5 risk map and grammar | | 20 | | | B3, M12, M13 |
| A6 PR-55 reads | | | | | B2 |
| A7 cut Part 4 | 5 Majors, 3 Nits | | | 2 | M16, M18, M19, M26 |
| A8 docs-only failure policy | | | Docs-only live | 3 | |
| A9 G4 not enforceable | allowlist | 5 | Native settings | 5 | |
| A10 owners check | owners | | Ownership check | 4 | M8 |
| A11 `risk/findings` | Nit | 8 | Nit | 6 | M3 |
| A12 label suppression | head config | 14 | Suppress | 8 | |
| A13 G1 and `needs: review` | needs-review | 6 | G1 | | M9, M20 |
| A14 ADR relationships | Nit | 2 | ADR relationship | | M15, M24 |
| A15 unresolvable references | Nit | 3, 4, 11 | Brief refs | 12 | N17 |
| A16 background and terms | | 1, 9, 10, 12 | Terms | 11 | |
| A17 measurement heuristics | outcome labels | | Nit (heuristics) | 9 | M10, M11 |
| A18 plan-to-code mismatches | Nit (flags) | | | | M1, M2, M4-M8, M14, M17 |
| A19 unverified and figures | | | 2 Majors, 2 Nits | | |
| A20 NFR contradictions | Nit | 15 | NFR | | |
| A21 alternatives and no-App | | | 3 Majors | 13 | |
| A22 label usefulness | | | Nothing measures | | |
| A23 stack table and baseline | Nit | 19 | | | M21-M23, N19 |
| A24 overview and CUJ claims | Nit | 16 | 4 Majors | | |
| A25 editorial | Nits | | Nits | 15 | N1-N19 |
| A26 rejected additions | | | | 14 | |
