# Goldfish Review Report: Stage 9, Implementation Readiness Test

**Target:** `docs/designs/review-workspace.md` (DetailedRange, lines 690–1221)

**Standard:** `implementation-readiness-standard.md` for the readiness and codebase reviewers. The style reviewer applied its own audit vectors.

**Verdict:** the detailed plan does not meet the standard. Several findings would stop a pull request from going green or from being built as written, and more leave design choices to the implementer.

## Reviewer Status

| Reviewer | Status | Findings |
| --- | --- | --- |
| `egm-implementation-readiness` | completed | 4 Blocker, 6 Major |
| `egm-codebase-details` | completed | 4 Blocker, 8 Major |
| `egm-style` | completed | 1 Blocker, 6 Major, 1 Nit |

The readiness and codebase reviewers overlap heavily, so their findings are merged below. Each entry names the raw findings it came from: `R-n` for readiness, `K-n` for codebase and `P-n` for style.

Neither reviewer could run `go` or `npm`, so counts of transitive dependencies are not available. The Cloud Run request size limit is recalled from memory.

**What the reviewers found ready or correct**

* Every file marked Modify exists, and no file marked Create exists.
* The eight new ADR filenames fit `tools/checkadrs`, and metadata-only edits with two-way supersession match that tool.
* The Makefile targets named in the Verification Plan exist, and PR-46 is the last entry in the existing plan.
* The upload `meta` format and the service configuration block are specific enough to build from.

## Prioritized Findings (Blockers -> Major -> Nits)

### IR-1. The database test rule fails the existing test matrix and the release job - Severity: Blocker

* **Reviewer Lens:** `egm-implementation-readiness` (R-1), `egm-codebase-details` (K-2)
* **Affected Scope:** lines 692, 905, 910, 912, 1189; `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `Makefile`
* **Discrepancy Description**: The plan's test-database helper skips when `WORKSPACE_TEST_DATABASE_URL` is unset and fails when `REVIEWER_REQUIRE_TOOLS` is set. That variable is set for the whole CI workflow. The existing `test` matrix runs `make test` over every package on Ubuntu and macOS with no Postgres, and GitHub-hosted macOS runners cannot run service containers. The release `test` job sets the same variable and runs `make check`, and the plan does not touch `release.yml` until PR-61.
* **Rationale & Recommendation**: From PR-47 on, the new store and `cmd/workspace` tests fail on all four matrix legs, against the plan's own rule that each pull request leaves `make check` passing. State which packages each target runs, which variable gates database tests in which job, and list the `ci.yml` and `release.yml` edits. "Add `workspace` to the build" is also undefined, since `make build` already compiles every package.

### IR-2. The web files cannot be embedded where the plan puts them, and the build and release steps conflict - Severity: Blocker

* **Reviewer Lens:** `egm-implementation-readiness` (R-2), `egm-codebase-details` (K-1, K-9)
* **Affected Scope:** lines 906–907, 910–911, 1016, 1036–1040, 1173–1174, 1188
* **Discrepancy Description**: `internal/workspace/web/embed.go` is to embed `web/dist`, but Go's embed accepts only paths inside the package's own directory. Moving the output does not fix CI: an embed pattern that matches no files is a compile error, and the output is git-ignored. The `test` matrix runs `make build` and `make vet` before any npm step, the `lint` job has no Node at all, and the release `test` job builds only the plugin. Separately, the PR-47 Dockerfile "builds the web files" five pull requests before `web/` exists. In PR-61 both the Dockerfile and GoReleaser build the workspace binary, and a second GoReleaser build would land in the archive the Action downloads. `install-tools.sh` installs gitleaks, opengrep and osv-scanner with no selector, so the image would ship opengrep, which is LGPL.
* **Rationale & Recommendation**: From PR-52 the required `ci` check fails. Decide where the build output goes, what is committed so a bare `go build` works, which existing jobs gain a web build, one builder for the image, and a build stage that copies only gitleaks.

### IR-3. The incremental pack cannot be verified against what the service stores, and two ingest endpoints have no wire format - Severity: Blocker

* **Reviewer Lens:** `egm-implementation-readiness` (R-3)
* **Affected Scope:** lines 763–788, 794–796, 826–829, 927, 939–940, 1128
* **Discrepancy Description**: The client drops every object reachable from a commit the service holds, including tree objects. An unchanged directory therefore arrives only as a hash inside its parent tree. The service must expand that directory to write a path row for every path and to confirm every entry is in the pack or already stored. The fixed tables hold files and flat per-commit path lists only. There is no tree storage and no lookup by tree hash, and the upload does not name the commits the client used.
* **Rationale & Recommendation**: The implementer must invent a tree table, a rebuild of tree hashes from path rows, or a re-read of raw packs, inside the check the trust rule depends on. `POST /ingest/v1/have` and `POST /ingest/v1/events` have no request or response body. The pseudo-code also calls `have` before it fetches the token; if `have` is unauthenticated, it tells anyone which commits exist in a private repository.

### IR-4. Nothing says which commit `reviewer publish` uploads, where the pull request facts come from, or who vouches for the base commit - Severity: Blocker

* **Reviewer Lens:** `egm-implementation-readiness` (R-4), `egm-codebase-details` (K-6)
* **Affected Scope:** lines 698, 771–773, 777–788, 944, 950–971, 1002–1003, 1048–1052, 1131–1132; `internal/installer/templates/review.yml.tmpl`, `action.yml`
* **Discrepancy Description**: The generated workflow checks out with no `ref`, which on `pull_request` runs is GitHub's temporary merge commit. If `publish` packs `HEAD`, it uploads a commit GitHub never reports as the head, and the view stays at "waiting for CI". If it uses the event's head hash, the pack is right but the index is built on the merge result. The CLI reads no GitHub Actions event data today, and the plan names a source only for the token. Every diff's base comes from the upload, so a pull request that edits its workflow can name any commit as its merge base. The plan does not say which file holds the publish step, how the events workflow installs the binary, or where the generated events workflow is written.
* **Rationale & Recommendation**: Name the commit and the tree, the source of each pull request fact, and the file that runs the step. PR-49 changes the trigger and permissions that ADR-0018's decision text fixes, and lands no ADR.

### IR-5. The installer golden files do not exist, and `init` cannot update an existing install - Severity: Blocker

* **Reviewer Lens:** `egm-codebase-details` (K-3), `egm-implementation-readiness` (R-4, R-10)
* **Affected Scope:** lines 966, 971, 1052, 1132, 1141, 1207
* **Discrepancy Description**: The plan modifies installer "golden files" and regenerates them with `-update`. `testdata/golden/` holds four files, none for the installer. Installer tests assert with `strings.Contains` on a fresh install, and `-update` is declared only in the `laneb` and `reporters` tests. The goal test "golden files with no workspace address are unchanged from today" has no baseline to compare with. Separately, the installer leaves an existing file alone unless `--force` is given, so `reviewer init --workspace-url` on a repository that already has the CLI writes neither the config block nor the workflow change. Config loading rejects unknown keys, so an older pinned binary fails on a config that has gained a `workspace` block.
* **Rationale & Recommendation**: Existing installs are the rollout target, and the plan gives no route for them and no upgrade order. Create the installer golden files before the templates change, or name the real assertions, and describe how an existing install is upgraded.

### IR-6. Running the CLI's review through the gateway does not fit the code, and the gateway interface cannot carry a tool loop - Severity: Blocker

* **Reviewer Lens:** `egm-codebase-details` (K-4), `egm-implementation-readiness` (R-5)
* **Affected Scope:** lines 852–863, 1074–1075, 1084–1093, 1103–1106, 1120–1127
* **Discrepancy Description**: `laneb.Reviewer` sends one flat prompt string. The gateway checks each excerpt by file hash, and a single string has none, so the scan rule has no stated way to apply to the review job. The gateway cannot get a one-call provider from `internal/model` as it stands: `model.New` takes the CLI's config and `newHTTP` is unexported. `laneb.Gather` shells out to git and reads a working tree, findings are dropped unless changed ranges are filled by an unexported parser, and guidance names come from a config loader that reads only from disk. None of those files is listed. `Review` and `Invalidate` turn every provider error into a warning, so the job runner cannot see a quota outcome to reschedule it. The gateway's `Request` has no field for the model's tool call, the tool's result, or earlier turns, and no part kind for the description, the brief or search results. `Converse` returns a stream with no outcome event.
* **Rationale & Recommendation**: Editing `prompts/review.md` changes the prompt the CLI sends, so "its output is unchanged" is true of the assembler only, and covering the new fields changes an unlisted golden file. Scanning lands in PR-54 but commits arrive from PR-49, and a stored file is never new again, so earlier files never get a scan result and no backfill job is listed. Define the adapter, the conversation turn types, the outcome event, and the backfill.

### IR-7. "Every goal has a named test" is false as written - Severity: Blocker

* **Reviewer Lens:** `egm-style` (P-1)
* **Affected Scope:** line 1191 with the table at 1196–1209
* **Discrepancy Description**: The Goals section lists 14 goals and the table has 12 rows. Six goals have no row: open any pull request, search, ask the AI, change brief, AI review and telemetry. Four rows are not goals as the Goals section words them.
* **Rationale & Recommendation**: The sentence is the plan's own coverage requirement. Add the missing rows, for which the plan already names candidate tests, or narrow the sentence to the goals the table covers.

### IR-8. The secrets scan cannot be both "the same invocation" and "the service's own rule file" - Severity: Major

* **Reviewer Lens:** `egm-codebase-details` (K-5), `egm-implementation-readiness` (R-5)
* **Affected Scope:** lines 906, 1072–1075
* **Discrepancy Description**: The existing `scan` passes no `--config`. With none, gitleaks uses the scanned tree's own `.gitleaks.toml`. The service writes pull request files under their real paths into the scan directory, so a pull request that adds its own `.gitleaks.toml` sets its own allowlist.
* **Rationale & Recommendation**: That is the failure the design moved the scan to the service to avoid. The service's rule file has no path, no config key and no Dockerfile entry, and `ScanDir`'s signature is not given. `gitleaks.Probe` already returns the version needed and is not named. A fixture credential already exists at `testdata/tiny-ts-repo/head/src/config.ts`.

### IR-9. The shared definitions fix names and leave the decisions - Severity: Major

* **Reviewer Lens:** `egm-implementation-readiness` (R-6), `egm-codebase-details` (K-12), `egm-style` (P-3)
* **Affected Scope:** lines 714–881, 899, 936, 952, 985, 1006, 1020, 1034–1035, 1107, 1118
* **Discrepancy Description**: The section says its contents are fixed "so that no pull request has to decide them". The tables give column names only: no types, keys, nullability, indexes or delete behaviour, and no allowed values for stage, state, kind, role, outcome or source. `pull_request` has one `source` column, but the upsert is said to keep the source of each field. A Postgres text column cannot hold NUL bytes or invalid UTF-8, and the rule for non-text content is missing. No route except the upload has a request body, response body, status code or error shape. "Read a conversation back" has no route. `/api/v1/events` and `/api/v1/status` carry per-repository data but sit outside the prefix behind the access check. Quantities appear as adjectives with no config key: "a few minutes", "a bounded retry", "a growing delay", "a result limit".
* **Rationale & Recommendation**: PR-51 writes the Go handlers and PR-52 writes the TypeScript types and a fake API. The journeys run only against the fake, so a mismatch with the real server is caught by no listed test. Fill these in, or reword line 716 to say what each pull request still decides.

### IR-10. The change brief's inputs, algorithm and output are described by intent only - Severity: Major

* **Reviewer Lens:** `egm-implementation-readiness` (R-7)
* **Affected Scope:** lines 834–835, 1122–1127, 1140–1142
* **Discrepancy Description**: The intent record's "fixed fenced form" is never written down. PR-57 already gathers and tests the intent record, and the parser arrives in PR-58. No path rule identifies "changed plan and ADR files". Reading guidance from the base commit means reading `.review/config.yaml` from a stored snapshot, but the config loader reads from disk and is not in PR-57's file list. "Unchanged code that refers to changed symbols" has no algorithm. The format of the brief's body, of "a source link per statement" and of the stored findings is missing.
* **Rationale & Recommendation**: Define the intent record format before PR-57, and the brief's input selection and output shape.

### IR-11. Identity and background work rest on mechanisms the plan does not supply - Severity: Major

* **Reviewer Lens:** `egm-implementation-readiness` (R-8), `egm-codebase-details` (K-7)
* **Affected Scope:** lines 981–985, 992–994, 1002, 1118–1120, 1150–1152
* **Discrepancy Description**: The plan requires rejecting a pasted token "that carries write permissions it does not need". Neither reviewer knows of a GitHub call that reports a fine-grained token's own permissions, and the plan gives no mechanism. "The head-commit field needed by the view" already exists in `ghclient`; what is missing is the description, the author and the created and merged times, plus any call that lists reviews. If `GetRepo` joins the `Client` interface, the fakes in `internal/poller/poller_test.go` and `internal/reporters/github_test.go` stop compiling, and neither is listed. The baseline job and the GitHub health check run with no user, and under the token route there is no service credential.
* **Rationale & Recommendation**: Give the permission-check mechanism or drop the requirement, list the real `ghclient` additions and the affected fakes, and say whose credential background jobs use.

### IR-12. The licence inventory work is understated, and the checking tool cannot hold two npm trees - Severity: Major

* **Reviewer Lens:** `egm-implementation-readiness` (R-9), `egm-codebase-details` (K-8)
* **Affected Scope:** lines 757, 865–879, 909, 918, 943, 1041–1042, 1051, 1065
* **Discrepancy Description**: `make licenses` is part of `make check`. It reconciles the inventory against the whole Go module graph in both directions, so every transitive module needs a hand-written row, and its denylist is a substring match that fails the build on a single MPL or EPL module. The inventory holds two rows today, and the plan lists direct dependencies only. The tool reads only `plugins/typescript` and keys the inventory by bare package name; `typescript` and `vitest` appear in both the plugin's tree and the web application's, so one row overwrites the other. `make licenses` runs in a CI job with no Node setup.
* **Rationale & Recommendation**: The SCIP bindings' import path is "to be confirmed", which is an open dependency choice inside the plan. The pinned `scip-typescript` version and command are never given. DOMPurify is recorded as Apache-2.0, and both reviewers believe it is dual-licensed with MPL-2.0. No planned ADR records giving up the short inventory that ADR-0021 calls a property to protect.

### IR-13. The file lists omit files that must change - Severity: Major

* **Reviewer Lens:** `egm-implementation-readiness` (R-10), `egm-codebase-details` (K-10)
* **Affected Scope:** the pull request tables at lines 885–1184; line 1206
* **Discrepancy Description**: `internal/workspace/api/router.go` is modified only in PR-48 and PR-52, though seven other pull requests add routes. `cmd/workspace/serve.go` wiring appears only in PR-47. `web/src/routes.tsx` lists telemetry and status routes in PR-52 for views created in PR-59. `web/src/api/client.ts`, `types.ts` and the fake API are absent from later pull requests. `internal/installer/templates/review.sh.tmpl` hard-codes the subcommand list, with a matching list in `skill_test.go`, so adding `publish` without it makes `review.sh publish` a usage error. `docs/architecture.md` says "Seven jobs", which becomes wrong at PR-47.
* **Rationale & Recommendation**: The "no `os/exec` import" test has no file and misses the point. `scan` would call `gitleaks.ScanDir`, so it needs no exemption, while `gateway` links the subscription adapters in `internal/model` that do spawn a CLI, which a direct-import check does not see. Existing helpers `testfixture.Git`, `testfixture.Build` and `testfixture.RequireToolsVar` are not named for reuse.

### IR-14. `model.Redact` does not recognise the tokens this service stores - Severity: Major

* **Reviewer Lens:** `egm-codebase-details` (K-11)
* **Affected Scope:** lines 900–901, 1089
* **Discrepancy Description**: The pattern covers `sk-`, `gsk_`, `AIza`, `xoxb-`, `ghp_` and bearer headers. The token route stores fine-grained tokens (`github_pat_`), and the App route stores `ghu_`, `ghr_` and `ghs_` tokens. None match. The repository's own installer test already treats `github_pat_` as credential-shaped.
* **Rationale & Recommendation**: "Passes every message through `model.Redact`" does not deliver "never appears in logs" for the main credential, and the planned log test can pass on `sk-` and prove nothing about it. `redact.go` is in no file list. The pattern is unexported and there is no detect function, so the plan does not say how typed text with a credential is recognised.

### IR-15. The verification table states absolutes that its tests do not establish - Severity: Major

* **Reviewer Lens:** `egm-style` (P-2)
* **Affected Scope:** lines 863, 1126–1127, 1198–1207
* **Discrepancy Description**: "No wrong definitions" is tested only for a target name that does not match. "Pull request code is never run" checks `os/exec` imports under one directory. "Nothing unscanned reaches a model" covers file excerpts only. "Access mirrors GitHub" leaves out two routes that carry per-repository data. "Existing installs keep working" covers `reviewer init` output and not the `action.yml` or prompt changes.
* **Rationale & Recommendation**: Reword each label to the property the test shows, or add the test the absolute needs.

### IR-16. The "Usable after" column assumes a release and a deployment that only PR-61 provides - Severity: Major

* **Reviewer Lens:** `egm-style` (P-4)
* **Affected Scope:** lines 692–712
* **Discrepancy Description**: Reviewed repositories install a released binary by pinned version. The plan's only release change, the image publication and the operating guide are all in PR-61. "Commits arrive at the service" and the three feedback points also need a deployed service and a workflow running a build that contains `publish`, and no pull request lists either step. Line 712 names three feedback points while the table bolds four entries.
* **Rationale & Recommendation**: State what "usable" assumes, or name the interim release and deployment.

### IR-17. The hand checks carry one deadline, later than the decisions they inform - Severity: Major

* **Reviewer Lens:** `egm-style` (P-5)
* **Affected Scope:** lines 871, 1211–1215
* **Discrepancy Description**: Indexing a `bun` repository and recording build time and sizes informs the workflow template in PR-49 and PR-53 and the upload limits. Approval of a credential decides whether PR-50's token route can be used. Both are scheduled "before the first repository is connected".
* **Rationale & Recommendation**: Move the index check to a gate before PR-49 or PR-53, the approval check to a gate before PR-50, and the import-path caveat to the PR-53 row with who confirms it.

### IR-18. Five statements cite an existing rule more broadly than the baseline states it - Severity: Major

* **Reviewer Lens:** `egm-style` (P-7)
* **Affected Scope:** lines 867–877, 881, 918, 932, 953, 979
* **Discrepancy Description**: The plan says `make license-check` enforces the header on six file types, while ADR-0001 names `.go` and `.ts` only. "Any failure is a warning and the command exits 0 (ADR-0009)" ignores that a usage error exits 2. "Inventory the new dependency" sits beside a table of direct dependencies. The bucket test does not say whether the `REVIEWER_REQUIRE_TOOLS` rule applies. The local key guard catches only a configuration that also names a bucket.
* **Rationale & Recommendation**: Narrow each statement to what the rule covers.

### IR-19. Terminology and wording - Severity: Nit

* **Reviewer Lens:** `egm-style` (P-8)
* **Affected Scope:** lines 697–701, 736–754, 788, 825, 1028–1035, 1063, 1181, 1207–1208
* **Discrepancy Description**: "Journey N" stands for what the document defines as "CUJ N". "Token" means three things: the Actions identity token, the user's access token and model tokens. Several things have more than one name: "hotkeys" and "shortcuts", "gateway" and "outbound door", `owner` and `org`. "Browser-reserved key" is a test's pass condition and is never listed.
* **Rationale & Recommendation**: Pick one term per concept and define "journey" once.

## Checked and not upheld

* **ADR numbering order (P-6).** The style reviewer noted that the ADRs do not land in number order and that `tools/checkadrs` "enforces numbering", without having read the tool. The tool's own comment says the log "legitimately has gaps at any moment", so landing ADR-0033 before ADR-0032 does not fail the check.
