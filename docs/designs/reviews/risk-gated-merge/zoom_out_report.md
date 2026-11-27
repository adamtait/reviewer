# Goldfish Review Report

**Target:** `docs/designs/risk-gated-merge.md` (`Lines 1-1093`)

Stage 5, Initial Zoom Out Review. GoldfishContext: none. Both reviewers read the full document. They checked claims against `cmd/`, `internal/`, `docs/adr/` and the installer templates. They did not read `docs/designs/briefs/` or `docs/designs/reviews/`. Neither had web access, so every statement below about GitHub behaviour is marked unverified.

## Reviewer Status

| Reviewer | Status |
| --- | --- |
| `egm-zoom-out` | completed |
| `egm-inventive-catalyst` | completed |

Severity counts after deduplication: 1 Blocker, 10 Major, 14 Nit.

## Claims checked and found accurate

Both reviewers checked these against code and found them accurate:

* The subcommand table (`cmd/reviewer/flags.go:101-104`) and `twoWordSubcommands` (`flags.go:110`).
* The `errCheckFailed` exit-1 precedent (`flags.go:230-239`).
* The strict decoder (`internal/config/load.go:70-73`).
* `Writer` has four write methods and one read (`internal/ghclient/write.go:17-23`).
* `ListWorkflowRuns`, `ListRunArtifacts` and `DownloadArtifact` (`internal/ghclient/stream.go:177, 195, 233`).
* `parseSince` (`cmd/reviewer/metrics.go:325`).
* review-index runs on every push (`internal/installer/templates/review-index.yml.tmpl:14-16`).
* `docs/implementation-plan.md` ends at PR-54 (line 839).
* The latest ADR is 0034.

## Prioritized Findings (Blockers -> Major -> Nits)

### Approval bound to the exact head SHA, combined with "branches must be up to date", voids every approval on every merge - Severity: Blocker
* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** `Lines 49-51`, `112-116` (G8, G9), `256-262` (CUJ 5), `475-478`, `496-498`, `513-515`, `674-675`
* **Discrepancy Description**: Two rules in Part 4 combine badly.
  * The human path passes only when an approval's `commit_id` equals the current head SHA (475-476). CUJ 5 says an approval is never carried forward.
  * Revocation relies on the ruleset requiring branches to be up to date (497-498; E1 at 674-675).

  So every merge to `main` puts every other open pull request out of date. Updating the branch creates a new head SHA, and that head has no approval. Every pull request that was already approved then needs a fresh human approval after each merge. Human approval, which the Problem section calls the expensive step, then scales with the number of merges, and the branch drift described at lines 49-51 gets worse.

  G8 also cannot pass as written. It requires "zero disagreements" between the gate's human branch and GitHub's own approval state (112-114). The gate is stricter by design, so it will disagree on every updated branch, and rollout stage 3 can never start. Unverified: if GitHub's stale-approval dismissal does not dismiss an approval when the only new commit merges base into head with no change to the diff, the disagreement is systematic.
* **Rationale & Recommendation**: The up-to-date rule is doing two jobs: keeping the policy fresh, and classifying the exact tree. Neither job needs it.
  * A path-confined class stays path-confined after merging `main`.
  * The human path needs "the reviewed change is what lands", not "the reviewed SHA is the head".

  Recommendations:
  * Bind the human approval to the pull request's own change. Carry it forward when the only commits since the approved SHA come from the base and the change itself is the same. Otherwise fall back to exact-SHA binding. Any merge commit whose tree differs from the clean automatic merge must void the approval, because a hand-made conflict resolution can hide new code.
  * Drop the global up-to-date rule. Have the gate require `behind_by == 0` only on the class path.
  * Revoke on policy changes by re-running the gate on a push to `.review/merge-policy.yaml` on `main`.
  * Make G8's check one-directional: count only cases where the gate passes and GitHub would not.

  This finding applies to Part 4. If the Major finding on Part 4's depth (below) is accepted, carry it into the follow-up gate design as a requirement.

### The later-stage `merge-gate` is designed to implementation depth before its precondition exists - Severity: Major
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** `Lines 32-37`, `107-127`, `203-212`, `256-288`, `438-517`, `562-623`, `643-658`, `667-700`, `721-724`, `763`, `783-788`, `989-1066`
* **Discrepancy Description**: The gate starts only if an organisation owner confirms that a required workflow can be pinned (Q4). If that fails, "Part 4 stops" (line 449). Even so, about 40% of the document specifies the gate in detail:
  * six goals (G7-G12) and four CUJs;
  * exit codes and a `StatusWriter` method signature;
  * a kill-switch variable and a sweep job;
  * six reserved PR numbers (PR-61 to PR-66);
  * four reserved ADRs (ADR-0037 to ADR-0040) that supersede ADR-0009, ADR-0018 and ADR-0026.

  The details depend on answers to Q4, Q5 and Q7, none of which exist yet. Alternative D1 (628-632) says a separate repository "keeps every existing ADR true". It is then rejected with no reason given ("Rejected by author: build it in this repository"). ADR-0009's "Revisit when" section points to a team's own CI step, not to a feature in this tool.
* **Rationale & Recommendation**: Building this before it is needed (YAGNI), at a level of detail the design cannot yet support. The detail will be rewritten after Q4, Q5 and Q7 are answered, but reviewers have to read and argue about it now.
  * Cut Part 4 to a one-page "Later stages" sketch with three parts:
    * the precondition (Q4);
    * the invariants, stated as principles: never weaker than today, bound to head, base and policy SHA, fails closed, break glass (G7-G10);
    * a statement that v1 does not depend on it.
  * Move everything else into a follow-up design written after Q4 is answered.
  * Do not pre-allocate PR or ADR numbers.
  * Give D1 a real reason for rejection, or adopt it.

  Several findings below (the granting gate, the kill switch, gate side effects, and most of the gate Nits) would move into that follow-up.

### The gate is built to grant merges, which is why it needs Q4; a gate that can only block would not - Severity: Major
* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** `Lines 32-37`, `70-73`, `109-111` (G7), `151-153`, `438-517`
* **Discrepancy Description**: The design assumes that merge classes beyond GitHub's native settings need a gate that can grant a pass. A granting gate must be unforgeable, so all of Part 4 waits on Q4. But the Non-Goals rule out every class except docs-only (151-153), and Part 2 already delivers docs-only natively. Part 4 therefore adds no new merge class.

  What Part 4 does add:
  * approvals bound to the exact SHA;
  * rejecting deletions, renames and mode changes;
  * the never-qualify list;
  * decision records.

  It also replaces GitHub's native approval rule with custom code (rollout stage 3, line 515), which is the riskiest step in the plan.
* **Rationale & Recommendation**: Invert the roles: GitHub grants, the code only blocks.
  * Keep native code-owner review at 0 approvals as the only thing that grants a merge.
  * Add the gate as an extra required check that can only fail an allowlisted pull request: deletions, renames, file types outside the allowlist, never-qualify content.
  * A forged pass then gives exactly v1's behaviour and nothing worse. Q4 stops being a hard prerequisite, the native approval rule is never removed, and the "everything merges unreviewed" risk in E2 goes away.
  * Defer a granting gate until someone chooses a class that CODEOWNERS cannot express.

  Trade-offs:
  * The human path keeps GitHub's native approval semantics.
  * A forged veto pass is still possible without pinning, so the veto adds defence in depth, not a guarantee.

### The kill switch has an accepted revocation window and leaves classes on when the variable is missing - Severity: Major
* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** `Lines 117-118`, `179-180`, `187-188`, `282-288`, `496-501`, `726-727`, `1000`, `1059-1060`
* **Discrepancy Description**: The kill switch is a repository variable read only when the gate next runs. Lines 500-501 accept that "a pass already posted can still merge" until a sweep finishes.

  Classes are on unless the variable says `off`, so a deleted or unset variable leaves classes on. That contradicts deny-by-default (179-180) and G10's "missing ... means not eligible" (117-118).
* **Rationale & Recommendation**: The design already contains a rollback with no window. v1's rollback, "set required approvals back to 1" (727), is enforced by GitHub at merge time and takes effect at once.
  * Use that ruleset change as the kill switch at every stage.
  * Keep the variable only as an extra signal, and invert it into an enable switch: classes are evaluated only when `MERGE_GATE_CLASSES=on`.
  * Keep the sweep only to clear stale statuses.

  Trade-offs:
  * Changing a ruleset needs a repository admin. Who can edit variables is unverified.
  * Human pull requests need both a native approval and the gate's approval until the switch is reverted.

### Gate side effects can fail a human-approved pull request, and the listed permissions cannot post the comment - Severity: Major
* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** `Lines 117-120` (G10), `440-442`, `451-465`, `488-489`, `503-504`, `1003-1006`, `1026-1029`, `1049-1050`
* **Discrepancy Description**: Several parts of the gate spec contradict each other.
  * The gate exits non-zero on every error (441-442). Step 4 posts the status, writes the record and posts the `<!-- rv:gate -->` sticky comment before it exits (488, 503-504).
  * The workflow template grants only `contents: read, pull-requests: read, statuses: write` (1049-1050). Unverified, but standard GitHub behaviour: posting an issue comment needs `pull-requests: write` or `issues: write`. As written, every gate run would error, including runs for human-approved pull requests.
  * The gate is given only a one-method `StatusWriter` (452, 1003-1006), yet it has to post a comment.
  * G10 says the human path does not depend on the class path, but the human path does depend on these record writes.
  * The test "status state always matches the exit code" (1028-1029) cannot hold when posting the status itself fails.
* **Rationale & Recommendation**: Separate the decision from its side effects.
  * Make the exit code depend only on the decision and on writing `$GITHUB_STEP_SUMMARY`.
  * Make the comment and the status best-effort, and log their failures.
  * Then do one of these:
    * grant `pull-requests: write` and name the comment method in the gate's import allowlist; or
    * drop the comment, since Q7 already says it is not proof.
  * Consider dropping the commit status too. That removes the need for ADR-0040 to supersede ADR-0018.

### The `risk/reach` signal (and to a lesser degree `risk/history`) brings in more machinery than an advisory v1 needs - Severity: Major
* **Reviewer Lens:** `egm-zoom-out`, `egm-inventive-catalyst`
* **Affected Scope:** `Lines 322-324`, `348-356`, `369`, `716-718`, `723`, `748`, `911-914`, `939-941`
* **Discrepancy Description**: The document says reach is "queried with the reader in `internal/workspace/navigation`". The code does not support that framing.
  * **No suitable query exists.** There is no function that counts the files referencing symbols defined on a line. `FindReferences` works from a position, is capped at `MaxReferences = 500`, and falls back to text search (`internal/workspace/navigation/query.go:29, 191-228`).
  * **It needs the whole base commit.** `Build` needs a `*source.Commit` (`navigation/load.go:94`). That comes from `source.Store.Commit`, which downloads the whole commit tarball (`internal/workspace/source/store.go:66-91`).
  * **Cost and latency are understated.** The tarball is missing from the API-cost line (716-718) and the latency claim (723).
  * **It adds a dependency edge.** `cmd/reviewer` imports nothing under `internal/workspace` today, so reach makes the CLI depend on the hosted workspace's internals.
  * **It drives other v1 complexity.** Line 353 says the `actions: read` artifact download is "why risk is its own job". That leads to the job split, the new action `mode` input, a second installer job, and marker parsing that alternative D2 would not need.
  * **It duplicates existing code.** PR-58 (911-913) re-describes run discovery and artifact download, which `navigation.Service.Lookup`, `classify` and `fetch` already implement (`fetch.go:121-306`).
  * **It may often be unavailable.** `Lookup` matches the exact SHA (`fetch.go:131`). Per ADR-0034 (lines 47-48), commits pushed by a workflow token get no index run. Unverified: a push of several commits starts a run only for the tip. So "reach not available" may be common.
* **Rationale & Recommendation**: This is cost added ahead of evidence, for a TypeScript-only advisory label that no one has shown reviewers would act on. Reach already appears under Future Improvements (748).
  * Ship v1 with the path-map labels (`auth`, `data`, `ci`, `deps`, `infra`), `review-config` and `findings`. That covers CUJ 1 and CUJ 2.
  * Defer `risk/reach`. Consider deferring `risk/history`, which costs about 20 extra API calls per run for a 180-day revert heuristic.
  * Once `actions: read` is no longer needed, weigh D2 (risk inside the review job) against the separate job again.
  * If reach stays:
    * reuse `Service.Lookup`;
    * fall back to the nearest base-branch ancestor that has an index;
    * add the tarball download to the cost and latency figures;
    * state that the CLI-to-workspace dependency is a deliberate change to the package boundary.

### v1 measurement builds outcome labels that only later stages use - Severity: Major
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** `Lines 29-30`, `53-56`, `99-105` (G6), `414-436`, `803-812`, `822-828`, `985-986`
* **Discrepancy Description**: Several v1 measurement pieces exist only to history-test future merge classes and a future scored classifier (430-431, 752-753):
  * `reverted` and `hotfixed` labels, the second using approximate patch-range overlap;
  * the three-way first-review label, which needs per-PR reviews, review comments and a compare call;
  * a 30-item hand-audited sample.

  v1's only merge class, docs-only, rests on "zero escapes plus a permanent audit sample", not on a rate claim (209-210, 694-700). The one-off `gh pr list` query (767-773) already answers v1's questions: the docs-only share and the time to first approval.
* **Rationale & Recommendation**:
  * Limit PR-55 to time to first approval and the count of allowlist candidates. That can be a small extension of `metrics`, or the query committed as a trials script.
  * Move the revert, hotfix and first-review labels and the audit sample into the gate follow-up. That is where a precision target (G12) first needs them.
  * Trim G6 to match.

### `reviewer owners check` tests a case the prescribed CODEOWNERS layout already prevents, and is not made a required check - Severity: Major
* **Reviewer Lens:** `egm-zoom-out`, `egm-inventive-catalyst`
* **Affected Scope:** `Lines 96-98` (G5), `248-254` (CUJ 4), `387-393`, `397-400`, `873-881`, `951-958`, `960-964`
* **Discrepancy Description**: Line 387 makes `* @team` the first CODEOWNERS rule. A new top-level directory such as `scripts/` is therefore owned through `*` and can never be unowned. CUJ 4 ("adds `scripts/`, and no CODEOWNERS rule other than the allowlist matches it") cannot fail the check. G5's check ("fails on a branch that adds an unowned top-level directory") always passes trivially.

  A file can lose its owner only in two ways:
  * a later rule with no owner is too broad;
  * the `*` rule is removed or moved.

  To catch those two cases, the design builds a full gitignore-style matcher (`internal/codeowners`) and runs every `git ls-files` path through it.

  Separately, Part 2's ruleset requires only "existing CI checks" (393). The new `reviewer owners check` is never made a required check, so ADR-0036's argument that "the check is what keeps G5 true" (963-964) does not hold.

  The matcher's other consumer is `risk/wide` ("3 or more CODEOWNERS groups"). In repositories that follow this template, nearly every path belongs to the single `*` team, so that label will rarely fire.
* **Rationale & Recommendation**: A structural lint of the CODEOWNERS file catches the same failures without matching any paths.
  * Lint for these invariants:
    * the first effective rule is `*` with an owner;
    * CODEOWNERS owns itself;
    * every rule with no owner matches an allowlist entry;
    * allowlisted directories contain only allowed file types (see the allowlist finding below).
  * Rewrite CUJ 4 and G5's check around the failures that can actually happen: a rule with no owner that is too broad, or a missing `*` rule.
  * Drop `internal/codeowners` unless a remaining signal needs it.
  * Add the check to Part 2's required checks and to `docs/merge-classes.md`, and say in the rollback steps how to clear it.
  * Unverified, and outside what either approach can catch: whether GitHub treats a nonexistent owner, or an owner without write access, as satisfying code-owner review. The setup guide should cover this.

### The risk record reads `.review/risk.yaml` and CODEOWNERS from the pull request's head when the base branch is easy to read - Severity: Major
* **Reviewer Lens:** `egm-inventive-catalyst`, `egm-zoom-out`
* **Affected Scope:** `Lines 84-86` (G2), `227-236` (CUJ 2), `329-344`, `372`, `908`, `926`
* **Discrepancy Description**: Both files are read from the pull request's head (336-344). A pull request can therefore:
  * remove its own repository-specific risk paths, which CUJ 2 concedes;
  * rewrite CODEOWNERS to merge owner groups and avoid `risk/wide`.

  The design then adds `risk/review-config` and a "computed from configuration this pull request changes" line to flag a problem it created.

  The same design already reads trusted data elsewhere: the review-index artifact at the merge base (349-352), and gate policy read "from the base branch by SHA" (481-482). The action already fetches full history (`action.yml:41`).

  The zoom-out reviewer rated this a Nit and the inventive-catalyst reviewer a Major. It is kept as a Major because it lets a pull request suppress its own labels, which works against G2.
* **Rationale & Recommendation**:
  * Read both files at the merge-base SHA, through the contents API or `git show origin/<base>:<path>`.
  * Keep `risk/review-config` as a plain path label, and drop CUJ 2's special case.
  * The risk job may then need no checkout at all.

  Trade-offs:
  * Two more API calls.
  * Risk paths that a pull request adds take effect only after it merges.
  * Under `pull_request`, the job's workflow definition still comes from the pull request, which can delete the job. Only an audit catches that.

### `needs: review` ties the risk record to the review job's success, and G1's check has no v1 implementation - Severity: Major
* **Reviewer Lens:** `egm-inventive-catalyst`, `egm-zoom-out`
* **Affected Scope:** `Lines 79-82` (G1), `300-305`, `321-323`, `341-343`, `723`, `938-939`, `1053-1058`
* **Discrepancy Description**: Three problems.

  **The record depends on the review job.** The risk job runs with `needs: review` (322). Unverified, but standard Actions behaviour: a job whose dependency fails, times out or is cancelled is skipped unless it uses `if: always()` or `!cancelled()`. The review job can fail on install, checksum, checkout or timeout even though `reviewer` itself exits 0. When it does, no record is posted. That contradicts line 342 ("if the review step ... did not run, the record says 'findings not available'") and G1. It also puts the whole review job, including a slow model lane, in front of signals that do not depend on it. D2 was rejected because "a risk bug can delay review", and `needs:` brings back the reverse coupling.

  **G1's check has nothing behind it in v1.** "The post-merge audit query" (81-82) is defined only in PR-65 (1053-1058), which is a later stage. `reviewer outcomes` (PR-55) does not count risk records.

  **Forks.** Pull requests from forks get a read-only token (ADR-0018, lines 43-45), so "every non-draft pull request" cannot get labels.
* **Rationale & Recommendation**:
  * Run the risk job in parallel, with no `needs:`.
  * Add `risk/findings` in a second short step that waits for review with `if: ${{ !cancelled() }}`, or update the comment when review finishes. The comment must say when findings are not yet known.
  * Give G1 a v1 check: add a record-presence count to `reviewer outcomes`, or a query in the trials file.
  * Limit G1 to pull requests that are not from forks.

### The docs-only allowlist accepts any file type under a directory, and native rules cannot enforce "added or modified" - Severity: Major
* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** `Lines 91-95` (G4), `238-246` (CUJ 3), `387-391`, `397-400`, `956-958`
* **Discrepancy Description**: The allowlist must be directories only, "never a file extension" (389-390), and the owners check fails on extension entries (957-958). A directory-only entry lets any file type under `docs/guides/` merge unreviewed: `.mdx`, `.js`, `package.json`, a `*.test.ts` file that the test runner picks up, or a file that code loads. That contradicts line 390 ("never ... MDX, or markdown that code reads").

  An entry that combines a directory with an extension, such as `docs/guides/**/*.md`, is narrower than either alone. Unverified: whether CODEOWNERS last-match-wins handles these exact patterns as expected.

  G4 says every file is "added or modified", but native CODEOWNERS cannot tell a deletion from an edit, so a pull request that deletes documentation also merges unreviewed. Unverified: how GitHub assigns code owners to the old path of a rename.
* **Rationale & Recommendation**:
  * Allow entries that combine a directory with an extension (`<dir>/**/*.md` plus named image types).
  * Have the owners check reject extension-only entries rather than combined ones, and fail when an allowlisted directory contains a file type outside the list.
  * Reword G4 to match what native settings enforce, or accept deletions explicitly.

  This fits the structural lint proposed in the owners-check finding above.

### `risk/ci` and `risk/review-config` overlap - Severity: Nit
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** `Lines 365`, `372`
* **Discrepancy Description**: Any change under `.github/workflows` gets both labels.
* **Rationale & Recommendation**: Limit `review-config` to `.review/`, or say why both labels are wanted.

### "High-confidence finding" cannot be derived from the markers as described - Severity: Nit
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** `Lines 341-343`, `371`
* **Discrepancy Description**: The `rv:` marker carries only a fingerprint and a rule id (`internal/fingerprint/fingerprint.go:30-39`). Confidence would have to be inferred from where the comment was placed: inline for high confidence, the summary otherwise (README.md:71-73).
* **Rationale & Recommendation**: Say so in the document. Also consider whether the label adds anything beyond the inline comments it restates.

### The stack table's start gates contradict the sequencing text - Severity: Nit
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** `Lines 771-772`, `779-781`
* **Discrepancy Description**: The text says Part 2 and PR-59 may be dropped depending on the one-off query, and that Part 2 depends on Q1 and Q2. Yet PR-57 and PR-59 list "Gate to start: none".
* **Rationale & Recommendation**: List "query result is not negligible" and Q1 as PR-59's start gate, and correct PR-57 to match.

### References to the brief cannot be resolved from this document - Severity: Nit
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** `Lines 193`, `195-212`, `333`, `389`, `404-406`, `455`, `482`, `509`, `738`, `1085`
* **Discrepancy Description**: A reader of this document alone cannot resolve:
  * experiment numbers, including "experiments 1 to 19" used as a verification gate;
  * decision IDs D-A, D-B, D-E, D5 and D6;
  * "brief, Blocker N" and "brief, Major N".

  "Decision D5" in Q6 also clashes with Alternative D5 (659).
* **Rationale & Recommendation**: Add a one-line description inline wherever a reference gates work, and rename the decision IDs so they do not clash with the alternative IDs.

### The Overview's count is wrong - Severity: Nit
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** `Lines 17-30`
* **Discrepancy Description**: "This design adds two things" is followed by three numbered v1 items, plus the later stages.
* **Rationale & Recommendation**: Correct the count, or reword it as "two phases".

### Unneeded hedge and an uncounted cost in PR-55 - Severity: Nit
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** `Lines 719-720`, `841-842`
* **Discrepancy Description**: "Add `PreviousFilename` ... if it is not already decoded" is unnecessary, because it is already decoded (`internal/ghclient/client.go:117-119`). Reading `ReadyForReviewAt` "from the timeline" costs one call per pull request, and the cost line leaves that out.
* **Rationale & Recommendation**: Remove the hedge, and add the timeline call to the cost estimate.

### Per-method retry in a later stage - Severity: Nit
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** `Line 1008`
* **Discrepancy Description**: `ghclient` has no retry anywhere (`do`, `client.go:360-424`). Adding retry one method at a time makes behaviour inconsistent across methods.
* **Rationale & Recommendation**: If retry is needed, add it once in `do` with its own justification. Otherwise drop it along with the rest of Part 4's detail.

### Latency framing - Severity: Nit
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** `Line 723`
* **Discrepancy Description**: `reviewer risk` runs as a separate job after review. It pays its own runner start, checkout and install, so it is not part of the review job's time.
* **Rationale & Recommendation**: State the latency as the time added before the risk record appears.

### The success metric is skewed by the change it measures - Severity: Nit
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** `Lines 1086-1087`
* **Discrepancy Description**: Docs-only pull requests that merge with no approval drop out of the "time to first approval" population, so the median moves by construction.
* **Rationale & Recommendation**: Measure only pull requests outside the allowlist, or measure time to merge across all pull requests.

### Flag validation will reject the flags `outcomes` reuses - Severity: Nit
* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** `Lines 797-799`, `833-834`
* **Discrepancy Description**: `--since`, `--format` and `--repo` are restricted to `metrics` (`cmd/reviewer/flags.go:192-195`). PR-55 says only "add `--allowlist` to `options`".
* **Rationale & Recommendation**: State that PR-55 relaxes those checks for `outcomes`, and adds the matching restriction for `--allowlist` on `outcomes` and `owners check`.

### The G2 label-name test is a fragile substring blacklist - Severity: Nit
* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** `Lines 86-87`, `896-897`
* **Discrepancy Description**: Rejecting names that contain `safe`, `low` or `ok` would also reject harmless future labels: `risk/workflow` contains "low", and `risk/tokens` and `risk/hooks` contain "ok".
* **Rationale & Recommendation**: Test that the label set exactly equals the closed table at lines 361-372. Any new label then needs a deliberate test change.

### "Waiting" (exit 2) shows as a failing required check - Severity: Nit
* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** `Lines 459`, `488-489`, `1020-1021`
* **Discrepancy Description**: Every unapproved pull request will show a red required check, and coding agents that react to red CI may try to "fix" it. Unverified: whether native auto-merge stays armed after a required workflow fails and then picks up a later successful run.
* **Rationale & Recommendation**: Add the auto-merge behaviour as a sandbox experiment. Document for agents that a failing `merge-gate` check means the pull request is waiting for review, not that the build is broken.

### Gaps in the gate's API budget and permissions - Severity: Nit
* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** `Lines 476-477`, `721-722`, `995-1000`
* **Discrepancy Description**: Checking that the approver "did not push to the pull request" needs a commit or timeline listing that is not in the "about 6 calls" estimate. `Variable(ctx, repo, name)` reads a repository variable over REST. Unverified: whether `GITHUB_TOKEN` with the listed permissions can do that.
* **Rationale & Recommendation**: Add the listing call to the estimate. Pass `vars.MERGE_GATE_CLASSES` into the action as an input instead of reading it over REST.

### ADR-0035 should cross-reference ADR-0018's permission statement - Severity: Nit
* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** `Lines 378-381`, `862-869`
* **Discrepancy Description**: ADR-0018 (lines 32-33) describes its permissions as "the minimum the job needs" in general terms. ADR-0035 adds a job that holds `actions: read` and says it "supersedes nothing".
* **Rationale & Recommendation**: Have ADR-0035 explicitly amend or cross-reference ADR-0018's permission sentence, so readers do not see two permission statements that appear to conflict.

## Synthesis notes

* **What both reviewers agree works.** The core split is sound: an advisory signal now, and merge authority later, decided only by rules over inputs the pull request cannot change. Delivering docs-only through GitHub's CODEOWNERS and ruleset enforcement, rather than through a status check that could be forged, was called out as a good choice.
* **How the findings relate.** The two lenses point the same way on the gate. The zoom-out reviewer would cut Part 4 to a sketch. The inventive-catalyst reviewer found that, as written, Part 4 contradicts itself (the Blocker, and the side-effects finding) and could be replaced by a gate that can only block. If Part 4 is cut, carry the Blocker and the gate Majors into the follow-up design as requirements.
* **Duplicates merged.** These pairs were merged into one finding each:
  * owners check: zoom-out Major 4 and inventive-catalyst M6;
  * risk inputs read from the head: zoom-out Nit 5 and inventive-catalyst M3;
  * G1 check: zoom-out Nit 8, folded into inventive-catalyst M4;
  * reach availability: inventive-catalyst N3, folded into zoom-out Major 2.
* **Unverified GitHub behaviour raised in this review:**
  * how `needs:` behaves when the dependency fails;
  * comment-posting permissions;
  * stale-approval dismissal after a base merge;
  * code-owner assignment for renames and deletions;
  * owners that do not exist or lack write access;
  * whether required code-owner review is evaluated against the base branch's CODEOWNERS;
  * auto-merge after a failed required check;
  * whether `GITHUB_TOKEN` can read repository variables.

  The design itself correctly leaves its own GitHub claims open as Q1 to Q4.
