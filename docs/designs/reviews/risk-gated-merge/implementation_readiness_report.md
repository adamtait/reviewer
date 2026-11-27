# Goldfish Review Report

**Target:** `docs/designs/risk-gated-merge.md` (`Lines 758-1093`, Detailed Implementation Plan)

**Stage:** 9, Implementation Readiness Test. Reviewers read lines 1-757 for context only. They checked every path, function, type and line reference against the repository at the design commit. Every statement here about how GitHub's API or Actions behave is **unverified**, because no reviewer had web access.

## Reviewer Status

| Reviewer | Status |
| --- | --- |
| `egm-implementation-readiness` | completed |
| `egm-codebase-details` | completed |
| `egm-style` | completed |

**Severity counts after deduplication:** 3 Blocker, 26 Major, 19 Nit.

**Verdict:** The plan is not ready to implement. The two reviewers who checked it against the code both say it fails the Zero-Decision Rule. An engineer could not build PR-55, PR-57 or PR-58 without making design decisions the plan does not make.

**References confirmed correct** (according to `egm-implementation-readiness` and `egm-codebase-details`):

- **CLI wiring:**
  - `subcommands` (`cmd/reviewer/flags.go:101`), `twoWordSubcommands` (`flags.go:110`) and `errCheckFailed` (`flags.go:230-239`) are where the plan says.
  - Ordinary errors exit 0 (`main.go:53-54`).
  - `parseSince` is at `metrics.go:325`, and the `metrics` signature matches the one proposed for `outcomes`.
- **Client and config:**
  - The strict decoder is at `internal/config/load.go:70-73`.
  - `Writer` has four writes and one read (`write.go:17-23`).
  - `ListWorkflowRuns`, `ListRunArtifacts` and `DownloadArtifact` exist (`stream.go:177,195,233`).
  - `PreviousFilename` is already decoded (`client.go:117-119`).
- **Docs, ADRs and tools:**
  - `docs/implementation-plan.md` ends at PR-54, and the ADRs go up to 0034.
  - `tools/audithistory --base` exists.
  - README has a "Use it" section.
  - The template's review job is named `review`, so `needs: review` resolves.
- **New files:** every file the plan says it will create is absent, as expected.

---

## Prioritized Findings (Blockers -> Major -> Nits)

### B1. The reach signal cannot be built with the navigation package as it stands - Severity: Blocker
* **Reviewer Lens:** `egm-implementation-readiness`, `egm-codebase-details`
* **Affected Scope:** `Lines 907-914` (also the NFR budget at lines 716-723)
* **Discrepancy Description**: The plan says `risk.go` loads the index "with `internal/workspace/navigation`, and counts referencing files for each symbol defined on a modified or removed line". The package cannot do that:
  * **No query for symbols on a line.** It exports only `Build` (`load.go:94`), `FindDefinition` (`query.go:129`), `FindReferences(ix, commit, path, line, character)` (`query.go:191`) and `Service.Lookup` (`fetch.go:121`). Nothing lists the symbols defined on a line. The `docs`, `defs` and `uses` maps are unexported (`load.go:58-60`). `FindReferences` needs an exact character position, stops at `MaxReferences = 500` (`query.go:29`) and always runs a text search.
  * **It needs the whole base commit.** `Build` takes a `*source.Commit`. The only way to get one is `source.Unpack` of a tarball (`source/archive.go:94`), reached through `Store.Commit` and `Archive` (`stream.go:116`). That adds a download per run, which the API-cost and 30-second latency targets do not count. `Service.Lookup` also needs a `source.Cache`, a `source.Store` and an `IndexBytes` limit (`fetch.go:70-79,105`), and `unzipIndex` is unexported (`fetch.go:311`).
  * **No old-side line numbers.** "Modified or removed line" means line numbers in the base file. `internal/diff` records only new-side ranges (`parse.go:144-169`), and a pure deletion yields no range at all (`parse.go:106-110`). The plan also does not say how the merge-base SHA is found: local `git merge-base` against `--base`, or the existing `REST.Compare` (`stream.go:122`).
  * **None of this is on the file list.** It also crosses a boundary the docs state: `docs/architecture.md:140-147` keeps the workspace packages out of a `reviewer` run, and ADR-0034 is still Proposed.
* **Rationale & Recommendation**:
  * Add a Modify entry for `internal/workspace/navigation` with exact signatures and tests, for example `(*Index) DefinitionsOnLines(path string, lines [][2]int) []Symbol` and `(*Index) ReferencingFiles(sym Symbol) int`.
  * Say how the base commit is obtained (tarball or a constructor from the local checkout), the byte limit, and the `Cache`/`Store`/`Config` values.
  * Add an old-side range parser to `internal/diff`.
  * Say how the merge base is computed.
  * Update the API-cost and latency targets and `docs/architecture.md`.

### B2. `reviewer outcomes` (PR-55) needs client reads the plan never defines - Severity: Blocker
* **Reviewer Lens:** `egm-codebase-details`, `egm-implementation-readiness`, `egm-style`
* **Affected Scope:** `Lines 797-812`, `Lines 836-843`
* **Discrepancy Description**: Item 4 adds only `ListReviews` and `ReadyForReviewAt`. The logic in item 1 also needs reads that do not exist:
  * **Merge time and merge commit.** `PullRequest` has neither `merged_at` nor `merge_commit_sha` (`client.go:40-60`), and nothing in the repo decodes `merged_at`. Without them, "merged pull requests in the period" cannot be selected and "later" pull requests cannot be ordered. `ListPullRequestsUpdatedSince` filters on `updated_at` (`client.go:265-269`) and exists only on `*REST`.
  * **Linking comments to a review.** "That review's comments" needs a link from each comment to its review. `ReviewComment` has no review ID or commit ID (`client.go:80-90`), and `ListReviewComments` returns every comment on the pull request.
  * **Compare with files.** `compare(first_review_commit...merged_head)` needs a list of files with patches. The existing `REST.Compare` returns only the merge base and asks for `per_page=1` (`stream.go:120-138`). `CompareFiles` does not arrive until PR-61, which is gated on Q4. The plan also does not say what happens when a force-push has deleted the compare base (404).
  * **Ready-for-review time.** "From the timeline" names no endpoint, method, event or type, and line 842 drops the "if never a draft" condition from line 800. The timeline API's shape is unverified.
* **Rationale & Recommendation**:
  * List each new method or field, with its path and JSON tag: `MergedAt` and `MergeCommitSHA` on `PullRequest`, and a review-ID field on `ReviewComment` (whether GitHub returns `pull_request_review_id` is unverified).
  * Add a compare that returns files: move `CompareFiles` into PR-55.
  * Add a named timeline read with an exact fallback rule ("use the last `ready_for_review` event; if the pull request was never a draft, use `CreatedAt`").
  * Name the listing call, and give the 404 handling.

### B3. The built-in risk map, the `risk.yaml` schema and the pattern grammar are left to the implementer - Severity: Blocker
* **Reviewer Lens:** `egm-implementation-readiness`, `egm-codebase-details`, `egm-style`
* **Affected Scope:** `Lines 887-901`
* **Discrepancy Description**:
  * **No default patterns.** `riskmap.go` holds "the built-in defaults as a table of `{Label, Patterns}`", but no pattern is given anywhere. The Technical Plan names categories only (lines 334-336). Nothing says, for example, which glob gives CUJ 1's `src/auth/session.ts` the `risk/auth` label.
  * **Categories with no label.** Payments and agent instruction files are default categories, but the closed label set (lines 361-372) has no label for them. "Persisted-state paths" for `risk/data` are never defined.
  * **No pattern grammar.** The repo has no `**` matcher: `installer/detect.go:237-273` deliberately narrows `**` to `*`, and Go's `path.Match` cannot do `**`.
  * **No `risk.yaml` schema.** Only `version: 1` is given. "An attempt to remove a default" means nothing without a schema.
  * **No fallback.** The plan does not say what the run uses after an invalid file: defaults only, defaults plus the valid additions, or nothing.
  * **"Owner group" is undefined** for `risk/wide`: a distinct owner string or a distinct owner set? Do unowned files count? Does a rename count once or twice toward 40 files?
* **Rationale & Recommendation**:
  * List every default `{label, patterns}` row literally, and map payments and agent instruction files to a label (or add labels).
  * Name one shared matcher, for example exported from `internal/codeowners`.
  * Give an example `risk.yaml` with its Go struct, and state the fallback after an invalid file ("built-in defaults only, stated in the comment").
  * Define "owner group" and the file-count rule.

---

### M1. Adding methods to `Client` and `Writer` breaks test fakes that are not on the file list - Severity: Major
* **Reviewer Lens:** `egm-implementation-readiness`, `egm-codebase-details`
* **Affected Scope:** `Lines 836-843`, `Lines 851-858`
* **Discrepancy Description**:
  * **Client fakes.** `ghclient.Client` (`client.go:125-133`) is fully implemented by `fakeClient` in `internal/poller/poller_test.go:19-34` and `fakeGitHub` in `internal/reporters/github_test.go:19-50`.
  * **Writer fake.** `Writer` is implemented by the reporters fake (`github_test.go:53-78`), so adding `AddLabels` and `RemoveLabel` breaks it.
  * **Source-text check.** `clientMethods` in `client_test.go:236` is a literal string that must be updated.
  * **Effect.** The promise that every pull request leaves `make check` green (line 761) fails.
  * **Inconsistency.** Putting `ListReviews` on `Client` contradicts PR-61's own rule of keeping new reads off `Client` (line 993). `metrics` already uses concrete `*REST` methods (`metrics.go:128-131`).
* **Rationale & Recommendation**: Make `ListReviews` (and the other PR-55 reads) `*REST` methods. List the poller and reporters fakes and `client_test.go` as Modify in PR-55 and PR-56.

### M2. "Every error path exits 0" does not match the existing helpers, and errors have nowhere to appear - Severity: Major
* **Reviewer Lens:** `egm-implementation-readiness`, `egm-codebase-details`, `egm-style`
* **Affected Scope:** `Lines 889-895`, `Lines 916-920`, `Lines 924-928`
* **Discrepancy Description**:
  * **Some errors exit 2 today.** `githubClient` wraps a missing token or an unparseable repo as `errUsage` (`run.go:243-255`), which exits 2 (`main.go:45-48`). `rulesTest` does the same for config errors (`rules.go:37-39`). The test that asserts exit 0 on every path would fail.
  * **Errors never reach the comment.** A returned error is only printed to stderr (`main.go:44`). Yet the plan wants a malformed `risk.yaml` "reported in the comment" and a "stated reason".
  * **No room in the data shapes.** `Record` has no warnings field, no `FindingsAvailable` and no `ReachAvailable`. Item 11 renders "findings not available" but leaves out "reach not available", which Part 1 requires (line 355).
  * **No degradation rule.** Nothing says whether the comment still posts when one signal fails, for example "history not available" after an API error.
  * **Undefined field.** `ConfigChangedByPR` is not defined against G2's `.review/` or `.github/` rule.
* **Rationale & Recommendation**:
  * Add `Warnings []string` and per-signal availability fields to `Record`, with a matching section in the comment (including "reach not available").
  * State that `risk` catches every error, posts a degraded record, and returns nil.
  * Say how token and repo errors are kept from exiting 2.
  * Define `ConfigChangedByPR`.

### M3. `risk/findings` needs "high-confidence", but markers carry no confidence - Severity: Major
* **Reviewer Lens:** `egm-implementation-readiness`, `egm-codebase-details`, `egm-style`
* **Affected Scope:** `Lines 891-893`, `Lines 907-909`
* **Discrepancy Description**:
  * **What exists.** `Input` holds only "the rule ids of review markers". A `fingerprint` marker holds only a fingerprint and a rule ID (`fingerprint.go:37-39,53,67-70`).
  * **Where confidence lives.** Confidence shows only in where a marker sits. High-confidence findings go inline. Everything else goes into the `<!-- rv:summary -->` comment (`reporters/github.go:20-22,159-168`; `summary.go:16-18`; ADR-0017). The plan never says this.
  * **"This tool's comments" is undefined.** Nothing says how they are recognised. `metrics` does it with a self-login check (`metrics.go:80-85`).
* **Rationale & Recommendation**: Define the input as markers split into inline and summary, read from `ListReviewComments` and `ListIssueComments` and filtered to the tool's own login. State that `risk/findings` means at least one inline marker.

### M4. Label sync: the hidden list has no format, the previous comment is not checked for authorship, and current labels are never read - Severity: Major
* **Reviewer Lens:** `egm-implementation-readiness`, `egm-codebase-details`, `egm-style`
* **Affected Scope:** `Lines 915-916`, `Lines 924-928`
* **Discrepancy Description**:
  * **No format.** Item 11 writes "a hidden list of labels this tool set", and item 10 must parse it back, but no format is given.
  * **No author check.** Nothing ties the previous `<!-- rv:risk -->` comment to the tool's own login. A pull request author could post a fake list and get a label a person set removed. There is also no rule for when several comments carry the marker.
  * **Current labels are never read.** "Add the missing ones" requires knowing which labels the pull request already has. `PullRequest` does not decode `labels` (`client.go:40-60`), and no list-labels read is planned.
  * **Ambiguous wording.** "Remove only `risk/` labels listed in its own previous comment" can be read as removing every listed label, even ones that still apply.
  * **Reuse.** The existing sticky-comment upsert (`reporters/summary.go:96-140`) should be reused or generalised, with an author check added.
* **Rationale & Recommendation**:
  * Specify the exact syntax, for example `<!-- rv:risk-labels risk/auth,risk/ci -->`, with a round-trip test.
  * Require the comment's author to be the tool's own login, and define the rule for duplicates.
  * Add `Labels` to `PullRequest` (or a list call).
  * Restate the rule: "add each computed label not already present; remove each label in the previous list that is not computed now; never touch other labels".

### M5. Revert-history reads have no client method or file-selection rule - Severity: Major
* **Reviewer Lens:** `egm-implementation-readiness`, `egm-codebase-details`, `egm-style`
* **Affected Scope:** `Lines 907-910`
* **Discrepancy Description**:
  * **No API method.** "Commits on the base branch touching each file in 180 days" needs a commits-by-path call. `ghclient` has none, and no file adds one.
  * **Local alternative.** The job already requires full history (`action.yml:41-49`), and `internal/diff` already wraps git (`diff.go:116-128`).
  * **No selection rule.** The plan drops Part 1's criterion ("the 20 changed files with the most lines changed"). Applying it needs line counts, but `ChangedFile` has only `Filename`, `Status`, `Patch` and `PreviousFilename` (`client.go:113-120`).
* **Rationale & Recommendation**:
  * Choose either `(*REST) ListCommits(ctx, repo, sha, path, since)` or local `git log`, and update the API-cost target to match.
  * Add `Additions`/`Deletions` to `ChangedFile`.
  * Restate the selection rule with a tie-break.

### M6. `func risk` and package `risk` clash in the same file - Severity: Major
* **Reviewer Lens:** `egm-codebase-details`
* **Affected Scope:** `Line 907`, `Lines 914-915`
* **Discrepancy Description**: `cmd/reviewer/risk.go` would declare a package-level `func risk(...)` and also import `internal/risk` to call `risk.Compute`. Go does not allow one identifier in both the package block and a file's imports, so this does not compile.
* **Rationale & Recommendation**: Name the function `riskCommand`, or alias the import, in the plan.

### M7. Flag parsing rejects the new subcommands' flags - Severity: Major
* **Reviewer Lens:** `egm-codebase-details`, `egm-implementation-readiness`
* **Affected Scope:** `Lines 797-799`, `Lines 830-834`, `Line 953`
* **Discrepancy Description**:
  * **Validation.** `parse()` returns a usage error (exit 2) when `--since`, `--format` or `--repo` is used with any subcommand other than `metrics` (`flags.go:192-195`). `TestMetricsFlagsBelongToMetrics` (`metrics_test.go:308`) asserts this. So `reviewer outcomes --since 90d` would fail.
  * **Repeatable flag.** A repeatable `--allowlist` needs a custom `flag.Value`. The existing list flags are comma-split strings (`splitList`, `flags.go:143-144,202-214`).
  * **Default.** `parseSince("")` defaults to 7d (`metrics.go:328-330`), so `outcomes` must apply the 90d default itself.
* **Rationale & Recommendation**: List the validation changes and the test updates. Say how `--allowlist` is parsed and which subcommands accept it. Say where the 90d default is applied.

### M8. PR-59 leaves out the CLI wiring for `owners check` - Severity: Major
* **Reviewer Lens:** `egm-codebase-details`
* **Affected Scope:** `Lines 949-958`
* **Discrepancy Description**: A two-word subcommand needs `"owners"` in `subcommands` (`flags.go:101-104`) as well as in `twoWordSubcommands` (`flags.go:110`), because `parse` checks `subcommands[args[0]]` first (`flags.go:116`). It also needs a dispatch case in `run` (`main.go:58-83`). Neither file is in PR-59's list.
* **Rationale & Recommendation**: Add `flags.go` and `main.go` as Modify in PR-59, and say what changes in each.

### M9. With `needs: review`, the risk job is skipped whenever review fails, which breaks G1 - Severity: Major
* **Reviewer Lens:** `egm-implementation-readiness`
* **Affected Scope:** `Lines 930-942`
* **Discrepancy Description**:
  * **Skipped job.** By default a job with `needs:` does not run if the job it needs fails (Actions behaviour, unverified). The shared history-check step also exits 1 (`action.yml:41-49`). So a review failure means no risk record, against G1 ("every non-draft pull request").
  * **No draft rule.** G1 says non-draft, but `review.yml.tmpl` has no draft condition, and the plan does not add one.
* **Rationale & Recommendation**: Specify the risk job's `if:` condition (for example `!cancelled()`) and the draft rule. Say whether risk mode keeps the history check.

### M10. The first-review and outcome label rules leave cases undefined - Severity: Major
* **Reviewer Lens:** `egm-implementation-readiness`, `egm-style`
* **Affected Scope:** `Lines 803-812`, `Lines 822-828`
* **Discrepancy Description**:
  * **Uncovered first-review cases.** A first review that requests changes without inline comments has no "commented files", so the rule silently labels it `unchanged-after-review`. A first review that approves without comments and is followed by more commits fits no label.
  * **No reviewer filter.** "Earliest review" does not exclude bot reviews (including this tool's), the author's own comment-only reviews, or dismissed reviews. "First `APPROVED` review" does not exclude bots.
  * **Unclear hunk side.** It is not said which side of the later pull request's hunks is compared with the earlier pull request's added lines.
  * **Missing rules.** There is no precedence when a pull request is both reverted and hotfixed, and no rule for an empty `patch` (large or binary files).
* **Rationale & Recommendation**: Restate each label as a decision table. Cover the reviewer filter (for example, exclude `User.type == "Bot"`), the hunk side, the precedence and the empty-patch case.

### M11. The obvious reuse for hotfix overlap gives wrong answers - Severity: Major
* **Reviewer Lens:** `egm-codebase-details`
* **Affected Scope:** `Lines 806-808`, `Lines 823-824`
* **Discrepancy Description**: `diff.FromPatches` (`diff.go:181-205`) is the natural reuse for API patches. But its parser assumes `--unified=0` (`parse.go:12-17`) and takes ranges from hunk headers, so context lines count as changed. The test "touches adjacent lines only, must not match" would then fail. Whether API patches carry context lines is unverified, although `diff.go:178-180` claims they match git's output.
* **Rationale & Recommendation**: Specify that overlap counts individual `+` lines from the patch body, not hunk-header ranges.

### M12. The allowlist has three different syntaxes and no way to exclude a path - Severity: Major
* **Reviewer Lens:** `egm-implementation-readiness`, `egm-codebase-details`, `egm-style`
* **Affected Scope:** `Lines 798-799`, `Lines 953-958`, `Lines 1013-1014`
* **Discrepancy Description**:
  * **Three descriptions.** The allowlist is a "repeatable directory glob" in `outcomes`, a `dir` in `owners check`, and `Allow []string (directories)` in the policy.
  * **No exclusion form.** The proposed allowlist, `docs/**` minus `docs/adr/**` (line 388), needs one, so the candidate count would include `docs/adr` changes.
  * **No rejection rule.** "Is a file extension rather than a directory" is not defined.
  * **Two sources of truth.** The plan does not say why `owners check` takes `--allowlist` when CODEOWNERS already leaves those paths unowned, or which wins if they disagree.
* **Rationale & Recommendation**: Define one allowlist grammar with an exclusion form (for example a `!` prefix), implemented by the shared matcher from B3. Give the extension-rejection rule and say which source wins.

### M13. The CODEOWNERS location and unsupported syntax are not specified - Severity: Major
* **Reviewer Lens:** `egm-implementation-readiness`, `egm-codebase-details`, `egm-style`
* **Affected Scope:** `Lines 873-881`, `Lines 907-908`, `Lines 953-958`
* **Discrepancy Description**:
  * **No location.** `Parse(r io.Reader)` leaves finding the file to the caller. Neither `risk` nor `owners check` names the paths searched, their order, or the behaviour when the file is missing or more than one copy exists. GitHub's accepted locations (`.github/`, root, `docs/`) are unverified here, and this repo has no CODEOWNERS file.
  * **Unsupported syntax.** "Gitignore-style" does not say how unsupported constructs (`!`, `[...]`, escapes) are handled. Getting this wrong makes the ownership check pass or fail without warning.
* **Rationale & Recommendation**: Add a `Locate(fsys fs.FS)` function or state the lookup order and the missing or duplicate behaviour. Specify whether unsupported syntax is an error or matched literally, with test rows.

### M14. The label verbs are missing path escaping, a label-creation policy and tests (PR-56) - Severity: Major
* **Reviewer Lens:** `egm-implementation-readiness`
* **Affected Scope:** `Lines 851-858`
* **Discrepancy Description**:
  * **Escaping.** Every label contains `/`, so `DELETE .../labels/{name}` needs `url.PathEscape` (`risk%2Fauth`). Whether GitHub accepts the encoded slash is unverified.
  * **Label creation.** Whether `POST .../labels` creates missing labels, and with what colour, is unverified. The plan does not say who creates the labels.
  * **Tests.** No test cases are listed for item 6.
* **Rationale & Recommendation**: State the escaping and the label-creation policy, and add both to the Q3 sandbox. List tests for success, 404 on remove, an escaped name, and 403.

### M15. The ADR supersessions will fail `tools/checkadrs` - Severity: Major
* **Reviewer Lens:** `egm-codebase-details`, `egm-implementation-readiness`
* **Affected Scope:** `Lines 1031-1043`
* **Discrepancy Description**: `checkadrs` enforces two-way supersession links (`tools/checkadrs/main.go:190-219`) and status sync with the index (`221-249`). ADR-0037, 0039 and 0040 supersede 0026, 0009 and 0018. The metadata blocks of `docs/adr/0009-*.md`, `0018-*.md` and `0026-*.md` must therefore change, but only `docs/adr/README.md` is listed.
* **Rationale & Recommendation**: Add the three ADR files as Modify, limited to the metadata block, which `bodyOf` permits (`main.go:154-162`).

### M16. The gate workflow's permissions do not allow the comment it posts - Severity: Major
* **Reviewer Lens:** `egm-codebase-details`, `egm-implementation-readiness`, `egm-style`
* **Affected Scope:** `Lines 1003-1006`, `Lines 1026-1028`, `Lines 1045-1051`
* **Discrepancy Description**:
  * **Permission conflict.** `record.go` posts a `<!-- rv:gate -->` comment, but the template grants `pull-requests: read`. Every existing comment-writing workflow holds `pull-requests: write` (`review.yml.tmpl:12-14`, ADR-0018:32). Because the gate fails closed, it would exit 1 on every run.
  * **Writer conflict.** The comment needs `Writer.CreateIssueComment`, but the plan says the gate's only writer is `StatusWriter`.
  * **Unverified permissions.** The scopes needed for `Variable`, collaborator permission and the audit's `actions: read` are unverified.
* **Rationale & Recommendation**: Fix the permissions block (and ADR-0040), or drop the comment. Name the writer interface the gate uses. Add the permission questions to the sandbox checks.

### M17. Retry is unbounded and new to the client - Severity: Major
* **Reviewer Lens:** `egm-codebase-details`, `egm-style`
* **Affected Scope:** `Line 1008`
* **Discrepancy Description**: "Retry with backoff on 5xx and secondary rate limits" gives no attempt count, time budget or status list. `ghclient` has no retry today, and `do` treats a rate-limit 403 as terminal (`client.go:405-406`). Changing `do` would also affect the review path. The gate has a 60-second latency target (line 723).
* **Rationale & Recommendation**: State the attempts, backoff, status codes and total budget. Say whether `do` changes or a wrapper is used only by `*REST` gate reads. State that running out of retries means exit 1.

### M18. The release artifact and gate template are underspecified, and the artifact count contradicts itself - Severity: Major
* **Reviewer Lens:** `egm-codebase-details`, `egm-implementation-readiness`, `egm-style`
* **Affected Scope:** `Lines 1033-1035`, `Lines 1045-1051`, `Lines 1069-1070`
* **Discrepancy Description**:
  * **Artifact count.** ADR-0037 says "two release artifacts become three", while item 24 and line 1069 say "second artifact". ADR-0026's two artifacts are the binary and the npm plugin.
  * **Archive name.** The goreleaser `name_template` is a download contract (`.goreleaser.yaml:38-40`). The new archive and binary names are not given, though the merge-gate action needs them.
  * **Template rendering.** `merge-gate.yml.tmpl` is not in the `//go:embed` list (`write.go:26-29`), and `init` does not install it, so nothing renders it.
  * **Unclear edits.** The `release.yml` change is not described. "Create or Modify" does not say which applies to each file.
  * **Unlisted file.** The pinning instruction lives in `docs/merge-classes.md`, which is not on PR-64's list. "Admins" conflicts with the Terms section's "org owner".
* **Rationale & Recommendation**:
  * Settle the count and name the archive and binary.
  * Move the template out of `internal/installer/templates/`, or say how it is rendered.
  * Mark each file as Create or Modify, and describe or drop the `release.yml` change.
  * Add `docs/merge-classes.md` to PR-64, and use "organisation owner".

### M19. The later stages (PR-61 to PR-66) read as actionable but are underspecified - Severity: Major
* **Reviewer Lens:** `egm-implementation-readiness`, `egm-style`, `egm-codebase-details`
* **Affected Scope:** `Lines 989-1065`
* **Discrepancy Description**: Besides M15 to M18, these gaps remain:
  * **Policy types.** No YAML keys, no types for `PrecisionTarget`, `Failure` and `OnFailure`, no definition of `Reason` or `CompareFile`, and no `NeverQualify` patterns.
  * **Decision record.** No format for the decision record (G11).
  * **Subcommands.** No dispatch design for `audit` and `sweep` in `main.go`, and no scheduled audit workflow file listed.
  * **Sweep.** No re-run mechanism (`workflow_dispatch`, the re-run API, `actions: write`).
  * **Audit.** Bypass detection has no named data source. Matching leaves out Part 4's "and run ID" (line 506). `ListWorkflowRuns` requires an `event` argument (`stream.go:177-179`) that the audit never names. "On a schedule" gives no cron, and `--since 1d` risks gaps.
  * **Mode changes.** Detecting a mode change from the compare API is unverified.
* **Rationale & Recommendation**: Mark PR-61 to PR-66 explicitly as "to be re-planned after Q4, Q5 and Q7", or resolve each gap. Fix M15 and M16 either way.

### M20. G1 cannot be checked in v1, and the E2E criteria have no pass bar - Severity: Major
* **Reviewer Lens:** `egm-implementation-readiness`, `egm-style`
* **Affected Scope:** `Lines 1080-1087`
* **Discrepancy Description**: "G1 to G6 checks as written in Goals" includes G1's "post-merge audit query finds no merged pull request without a record". No pull request from PR-55 to PR-60 builds that query. The audit in PR-65 is for the gate. "A person checks that each label has a true reason" names no role and no pass bar. "After 4 weeks" has no start point.
* **Rationale & Recommendation**: Add a risk-record-presence column to `outcomes`, or rewrite G1's check. Name the role and the pass bar ("0 labels without a matching path or rule id"). Anchor the 4 weeks to "docs-only enabled in the first repository".

### M21. The pre-code query has no owner, no output location and no threshold - Severity: Major
* **Reviewer Lens:** `egm-style`, `egm-implementation-readiness`
* **Affected Scope:** `Lines 767-773`
* **Discrepancy Description**:
  * **No owner or output.** "A person runs" and "written down" give no role and no output file.
  * **No threshold.** "If the share is negligible" has no threshold and no one who decides.
  * **Wrong start time.** `createdAt` is not G6's ready-for-review start time.
  * **Unverified fields.** Whether `gh pr list --json files` reports per-file status ("added or modified") is unverified.
  * **Coverage.** `--limit 500` may not cover 90 days.
  * **Effort claim.** "In an afternoon" is an unverified estimate.
* **Rationale & Recommendation**: Name the role and the output (the PR-60 trials file), and set a numeric threshold. Say that the query measures from creation time, and say how file status is found. Remove or justify the effort claim.

### M22. The stack's gate column is inconsistent, and Q3 evidence lands after the code that needs it - Severity: Major
* **Reviewer Lens:** `egm-style`, `egm-implementation-readiness`
* **Affected Scope:** `Line 763`, `Lines 775-788`, `Lines 1081-1082`
* **Discrepancy Description**:
  * **Mixed notation.** The column uses "Q4 yes", "Q4", "Q4 and Q5", "Q7", "—" and "none". Q4 and Q5 are not yes/no questions.
  * **PR-65.** It drops Q4 even though it is a later stage.
  * **PR-59.** It shows "none", although Part 2 depends on Q1 and Q2.
  * **PR-56 and Q3.** PR-56 is gated on Q3, but line 1082 says Q3 matters only before real use. Q3's evidence is committed in PR-60, which depends only on PR-55.
  * **Stack shape.** Line 761 says "a stack", but the dependency column suggests independent branches.
* **Rationale & Recommendation**: Define the column once ("condition that must hold before work starts"), and state the outcome that releases work for each question. Say where Q3's result is recorded before PR-56. Say whether the pull requests form a linear stack.

### M23. The baseline file does not cover what G6 and Part 3 require - Severity: Major
* **Reviewer Lens:** `egm-style`, `egm-implementation-readiness`
* **Affected Scope:** `Lines 982-987`
* **Discrepancy Description**:
  * **Missing items.** G6 (lines 99-105) requires the current ruleset and the query that produced the numbers. Item 19 lists neither.
  * **Partial audit.** The hand-audited sample covers outcome labels only, while Part 3 (line 433) says all three heuristics are checked.
  * **No audit method.** Nothing says who audits, what is compared with what, or the minimum acceptable agreement rate.
  * **README.** The trials README forbids repository names but does not mention replacing them with letters, and its "What is here" table is not updated.
* **Rationale & Recommendation**: Add the ruleset and the query, cover the outcome, first-review and docs-only heuristics, and state the comparison and a minimum rate. Add the trials README to the file list.

### M24. ADR-0039's "never block" wording conflicts with `reviewer owners check` - Severity: Major
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Lines 1037-1039` (against `Lines 953-964`)
* **Discrepancy Description**: ADR-0039 "keeps 'never block' for `reviewer`" and calls the gate "the one explicit, opt-in exception". But `reviewer owners check` is a `reviewer` subcommand that exits 1 to fail CI (ADR-0036), as does `rules test`.
* **Rationale & Recommendation**: Limit "never block" to review runs, and name `owners check`, `rules test` and the gate as the exceptions.

### M25. The outcomes test plan contradicts the code it is modelled on - Severity: Major
* **Reviewer Lens:** `egm-codebase-details`, `egm-implementation-readiness`
* **Affected Scope:** `Lines 819-821`
* **Discrepancy Description**: The tests run "against a fake `ghclient.Client`". But `metrics`, the stated model, takes `*ghclient.REST` (`metrics.go:80,128`), and its tests use an `httptest` server (`metrics_test.go:39-41`). The reads `outcomes` needs are not on `Client` (see B2 and M1).
* **Rationale & Recommendation**: Specify the `httptest`-server pattern from `metrics_test.go`.

### M26. The import-allowlist test is described wrongly and its scope is unclear - Severity: Major
* **Reviewer Lens:** `egm-style`, `egm-codebase-details`
* **Affected Scope:** `Lines 1003-1006`, `Line 1025`
* **Discrepancy Description**:
  * **Wrong claim.** Line 1005 says the import-allowlist test checks that only `cmd/merge-gate` uses `StatusWriter`. It cannot: the test checks what `merge-gate` imports, not who uses `StatusWriter`.
  * **Scope unclear.** Line 1025 does not say whether transitive imports count. Part 4's "never the review lanes" holds only with a transitive check.
  * **No list.** The allowed packages are listed only in the Technical Plan (line 442).
  * **Transitive dependencies.** `internal/diff` imports `pkg/finding` and `pkg/plugin` (`diff.go:18-19`).
  * **Table entry.** The PR-61 row (line 783) leaves out `StatusWriter`.
* **Rationale & Recommendation**: Say the test checks transitive imports, list the allowed packages in item 22, and call it the "import allowlist" every time. Put the `StatusWriter` check in a named `cmd/reviewer` test, and update the table row.

---

### N1. `PreviousFilename` is already decoded, and `previous_filename` may be redundant - Severity: Nit
* **Reviewer Lens:** `egm-codebase-details`, `egm-implementation-readiness`, `egm-style`
* **Affected Scope:** `Lines 801-803`, `Lines 841-842`
* **Discrepancy Description**: `client.go:117-119` already decodes `PreviousFilename`, so "if it is not already decoded" is a needless hedge. And if renames never count as docs-only, the `status` field alone decides it.
* **Rationale & Recommendation**: Drop the hedge. Say whether `previous_filename` is used for anything beyond reporting.

### N2. The writer verb list lives in the type comment, and the comments are already stale - Severity: Nit
* **Reviewer Lens:** `egm-codebase-details`, `egm-style`
* **Affected Scope:** `Lines 855-858`
* **Discrepancy Description**: The verb list is the `Writer` type comment (`write.go:14-16`), not the package comment. It already says three verbs while the interface has five methods. The package comment (`client.go:3-10`) says "six calls". In "It still contains no merge...", the word "It" is ambiguous.
* **Rationale & Recommendation**: Name both comments to update. Write "The package still has no merge, approve, close or push method."

### N3. The installer references are inaccurate - Severity: Nit
* **Reviewer Lens:** `egm-codebase-details`, `egm-implementation-readiness`
* **Affected Scope:** `Lines 936-942`
* **Discrepancy Description**:
  * **No golden tests.** The installer has no golden tests. Its assertions are `strings.Contains` checks (`write_test.go:192-229`, `providers_test.go:164-185`).
  * **Wrong file.** Templates are rendered in `write.go:122-155`, so the change needed in `plan.go` is unclear.
  * **Missing job settings.** The risk job needs `env: GITHUB_TOKEN` and `fetch-depth: 0`, as the review job has (`review.yml.tmpl:20-24,46-47`).
  * **Manual steps.** A second checksum placeholder changes the manual-steps text (`write.go:247-259`).
* **Rationale & Recommendation**: Name the real test files and the rendering site, and list the job's `env` and checkout settings.

### N4. The `action.yml` mode details are incomplete - Severity: Nit
* **Reviewer Lens:** `egm-codebase-details`, `egm-implementation-readiness`, `egm-style`
* **Affected Scope:** `Lines 930-934`
* **Discrepancy Description**:
  * **Dry run.** The plan does not say whether `dry-run` passes through as `${DRY_RUN:+--dry-run}` (`action.yml:85-90`).
  * **Step switch.** It does not say whether "replaced" means two steps with `if: inputs.mode == ...`.
  * **`--base`.** It does not say what `risk` uses `--base` for, and `--base` is not among `risk`'s flags.
  * **History check.** "The history check" is not tied to a named step.
* **Rationale & Recommendation**: Spell out the step conditions, the dry-run pass-through, the full `risk` flag set and the step names.

### N5. The index workflow template's comment becomes false - Severity: Nit
* **Reviewer Lens:** `egm-codebase-details`
* **Affected Scope:** `Lines 911-914`
* **Discrepancy Description**: `review-index.yml.tmpl:4-6` says "Nothing else uses it". After PR-58 that is false, and the template is not on the file list.
* **Rationale & Recommendation**: Add the template to PR-58 and update the comment.

### N6. `make test` is not `go test ./...` - Severity: Nit
* **Reviewer Lens:** `egm-codebase-details`
* **Affected Scope:** `Line 1071`
* **Discrepancy Description**: `make test` runs `go test $(PKGS)`, which excludes `node_modules` (`Makefile:15,83-84`).
* **Rationale & Recommendation**: Correct the parenthetical.

### N7. The strict decoder approach is not named - Severity: Nit
* **Reviewer Lens:** `egm-codebase-details`
* **Affected Scope:** `Lines 887-888`, `Line 1012`
* **Discrepancy Description**: `decodeStrict` is unexported and takes only `*Config`, so it cannot be reused as is.
* **Rationale & Recommendation**: Say "use `yaml.v3` with `KnownFields(true)`, as in `config/load.go:70-73`".

### N8. The relationship between `CompareFiles` and the existing `Compare` is unstated - Severity: Nit
* **Reviewer Lens:** `egm-codebase-details`
* **Affected Scope:** `Lines 997-998`
* **Discrepancy Description**: `CompareFiles` would sit beside the merge-base-only `Compare` (`stream.go:122`).
* **Rationale & Recommendation**: Say whether `Compare` is kept or rebuilt on top of `CompareFiles`.

### N9. Some signatures are not valid Go, and some types are undefined - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Line 797`, `Line 907`, `Lines 1013-1016`
* **Discrepancy Description**: `func outcomes(ctx, o options, stdout, stderr io.Writer, getenv) error` mixes typed and untyped parameters. `func risk(...) error` gives nothing. `CompareFile` is never defined; line 997 returns `CompareResult`.
* **Rationale & Recommendation**: Give full types, or say "same signature as `metrics`".

### N10. The exit behaviour of `outcomes` relies on an unstated convention - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Lines 815-817`
* **Discrepancy Description**: "Returns ordinary errors otherwise" relies on the fact that ordinary errors exit 0 (`main.go:53-54`), but the plan does not say so. It also does not say whether usage errors exit 2 or whether a partial read counts as success.
* **Rationale & Recommendation**: State the exit codes explicitly.

### N11. The label-name test matches substrings - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Lines 896-897`
* **Discrepancy Description**: A substring test for `safe`, `low` or `ok` would reject plausible future names such as `risk/workflows` or `risk/tokens`. No current label is affected. "ok" is not in G2's list (line 87).
* **Rationale & Recommendation**: Match whole words separated by `/` or `-`, and line the word list up with G2.

### N12. The threshold tests do not name boundary values - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Lines 899-900`
* **Discrepancy Description**: The 40-file, 3-owner and 10-file thresholds have no named cases.
* **Rationale & Recommendation**: Name them: 40 against 41 files, 2 against 3 groups, 9 against 10 files.

### N13. The detection rules are missing words and define no placeholder - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Lines 804-808`
* **Discrepancy Description**: "Title starts `Revert "`" is missing "with". `<n>` is undefined. "Within 14 days" does not say of what. "The output says so" does not say where.
* **Rationale & Recommendation**: Add "with", define `<n>` as this pull request's number, add "of this pull request's merge", and name where the output says it.

### N14. The gate section has undefined terms and an imprecise exit assertion - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Lines 1020-1029`
* **Discrepancy Description**:
  * **Undefined terms.** "D-E" is never expanded, and "one row per rule" is unclear.
  * **Missing pointer.** The `failure` status leaves out Part 4's break-glass pointer.
  * **Exit codes.** The status test does not state the mapping (0 = success, 2 = pending, 1 = failure). "Asserts a non-zero exit" should say exit 1.
* **Rationale & Recommendation**: Expand D-E, add the pointer, and state the mapping and the exit code.

### N15. ADR-0035 lands before the code it describes - Severity: Nit
* **Reviewer Lens:** `egm-implementation-readiness`
* **Affected Scope:** `Lines 860-869`
* **Discrepancy Description**: ADR-0035 records the label set and the risk job, which PR-57 and PR-58 implement, but it lands in PR-56. `docs/adr/README.md:10` says an ADR lands with the code that first implements it.
* **Rationale & Recommendation**: Move ADR-0035 to PR-58, or split it.

### N16. Smaller testing gaps - Severity: Nit
* **Reviewer Lens:** `egm-implementation-readiness`
* **Affected Scope:** `Lines 1067-1087`
* **Discrepancy Description**: Nothing checks the 30-second latency target. The location of the "`cmd/reviewer` never references `StatusWriter`" test is not given. The G2 test covers label names but not the comment text.
* **Rationale & Recommendation**: Add a latency check to the dry-run E2E, name the test file, and extend the G2 test to the rendered comment.

### N17. An overbroad coverage claim, unexplained experiment numbers and process metadata - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Lines 1077-1085`, `Lines 1089-1093`
* **Discrepancy Description**: "Every function ... has a table test" cannot be checked. "Experiments 13 and 15" and "1 to 19" are defined only outside this document. The footer's "EGM" is an unexplained acronym and process metadata.
* **Rationale & Recommendation**: Say "every exported function" and name how coverage is measured. List the experiment names inline. Drop the footer.

### N18. Wording that hides the actor or the referent - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Lines 800-801`, `Lines 862-869`, `Lines 877-879`, `Lines 909-917`, `Line 940`, `Lines 954-955`
* **Discrepancy Description**:
  * "Ready for review" is spelled three ways.
  * The ADR-0035 sentence runs over 70 words, and its "says so" is unclear.
  * "And comments" is not a matching behaviour.
  * "This tool's comments", "the existing exit-0 handling" and "a stated reason" name no comment, function or output channel.
  * "The same checkout" has no referent.
  * "Run against the checkout and reading its output only" is hard to parse.
* **Rationale & Recommendation**: Use one spelling. Split the ADR sentence into a list. Write "ignoring comment and blank lines". Name the function and the output channel. Write "the same `actions/checkout` settings as the review job". Rewrite as "runs `git ls-files` in the checkout and uses only its output".

### N19. The "before any code" step does not say where today's ruleset is recorded - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Line 770`
* **Discrepancy Description**: "Today's ruleset is written down at the same time" is passive. It names neither who records the ruleset nor where. This overlaps with M21 and M23.
* **Rationale & Recommendation**: Say the ruleset goes into the PR-60 baseline file.

---

## Process notes

- **Accidental reads (`egm-codebase-details`):**
  - It read the main checkout's copy of `.github/workflows/release.yml` by mistake. It then checked the worktree copy, which matches at the relevant lines.
  - One grep printed matching lines from `docs/designs/reviews/`. The reviewer says it ignored them.
- **Code not read (`egm-style`):** it did not read the code base. Its code-dependent points were checked against the other two reviewers' findings before being merged.
- **Unverified GitHub behaviour** that these findings depend on:
  - the label endpoints, escaping and auto-creation;
  - the timeline or ready-for-review source;
  - `pull_request_review_id` on review comments;
  - context lines in `/pulls/{n}/files` patches;
  - `gh pr list --json files` status;
  - the permissions needed for statuses, variables and collaborator permission;
  - the CODEOWNERS locations;
  - how `needs:` behaves on failure;
  - mode-change data from compare.
