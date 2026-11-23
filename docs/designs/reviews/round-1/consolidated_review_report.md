# Consolidated Review Report: review workspace design

**Target:** `docs/designs/review-workspace.md` (1221 lines; feature sections 1–689, implementation plan 690–1221)

**Date:** 2026-11-23

## Verdict

The design does not pass review as written. Ten independent reviewers ran across five stages, and every stage that issues a verdict returned "does not pass". The reviewers judged the problem statement, the non-goals, the trust rule for CI uploads and the single outbound door for model calls to be sound. The failures are in three places:

* **Unconfirmed foundations.** Sign-in approval, the index tool and the upload address are all still open, and file-level detail has been written on top of them.
* **Mechanisms that do not do what the goals claim.** The diff's base commit, the "nothing unscanned" guarantee and one user journey each fail against the document's own text.
* **An implementation plan that would break the existing build.** Several pull requests cannot go green as described.

The Stage 8 reviewer adds a warning about how to respond. The first three stages together ask for about twenty new mechanisms, and most of those guard four design choices that could be changed instead. Changing them answers the same problems with less. Those four choices are listed first under "Rulings needed from the author", because they decide which of the other fixes are needed at all.

## Reviewer status

| Stage | Reviewer | Scope | Status | Blocker | Major | Nit |
| --- | --- | --- | --- | --- | --- | --- |
| 5 | `egm-zoom-out` | Full file | completed | 1 | 8 | 1 |
| 5 | `egm-inventive-catalyst` | Full file | completed | 0 | 7 | 3 |
| 6 | `egm-comprehension` | Full file, no other files | completed | 0 | 9 | 3 |
| 7 | `egm-design` | Lines 1–689 | completed | 2 | 8 | 0 |
| 7 | `egm-structural` | Lines 1–689 | completed | 1 | 6 | 3 |
| 7 | `egm-style` | Lines 1–689 | completed | 1 | 7 | 2 |
| 8 | `egm-zoom-out` | Full file plus the three earlier reports | completed | 1 | 8 | 1 |
| 9 | `egm-implementation-readiness` | Lines 690–1221 | completed | 4 | 6 | 0 |
| 9 | `egm-codebase-details` | Lines 690–1221 | completed | 4 | 8 | 0 |
| 9 | `egm-style` | Lines 690–1221 | completed | 1 | 6 | 1 |

Stage reports: `zoom_out_report.md`, `comprehension_report.md`, `critic_report.md`, `intermediate_zoom_out_report.md` and `implementation_readiness_report.md`. The Stage 9 report merges the overlapping findings of its three reviewers, so its 30 raw findings appear as 19.

After merging, the five reports hold 91 findings: 13 Blocker, 64 Major and 14 Nit. They consolidate into the 29 action items below.

**Limits of this review.** No reviewer could open URLs or run `go` or `npm`. Statements about GitHub Actions, GitHub's own features, Cloud Run limits and third-party licences are from the reviewers' memory unless a repository file is cited. Five repository claims were spot-checked after the review and all held: config loading rejects unknown keys, `REVIEWER_REQUIRE_TOOLS` is set in both workflows, the credential pattern lacks `github_pat_`, the generated workflow checks out with no `ref`, and no installer golden files exist. One finding was checked and not upheld: `tools/checkadrs` allows gaps in ADR numbering.

## Rulings needed from the author

These nine items are decisions, and several contest choices the design records as the author's. Settle them before editing the document, in this order. Items 2 to 5 decide whether many of the later fixes are needed.

### ACT-01. Answer the questions that can void the design before doing more design - 🔴 Blocker

* **Problem:** Three open questions can each remove a pillar. If no organisation owner approves a credential, nobody can sign in. If `scip-typescript` is unmaintained or fails on `bun`, decision 3 falls. If a public upload address is not acceptable, decision 2 falls. The checks are scheduled after all fifteen pull requests.
* **Also unanswered:** the pack and index size (which decides whether the upload fits in one request), which claims the workflow token carries, and whether GitHub's own features or a browser-hosted VS Code already cover navigation. Alternative Z is recorded as "not rejected, and not evaluated".
* **Action:** make these entry conditions for PR-47. Run a time-boxed trial of the hosted editor on one pull request. Hold further file-level planning until the answers are in.
* **Sources:** Z-1, Z-2, Z8-1, S-4, Y-3, IR-17, and part of D-9.

### ACT-02. Rule on how source code reaches the service - 🔴 Blocker

* **Problem:** The service gets source only when the pull request's own CI job pushes it. Because the pull request controls that job, the design needs pack verification, an exchange to find missing objects and a merge-base upload. The reviewers then found that the base commit is never confirmed with GitHub, that the incremental pack cannot be verified against what is stored, that nothing arrives until the whole CI job ends, and that a lost upload is never noticed.
* **Options the reviewers raised:**
  * **Keep the CI push.** Then ACT-10, ACT-11 and ACT-17 are all required, and the pack verification gap must be closed.
  * **Read from GitHub as the viewer on first open.** The token route already asks for contents read access, and `internal/ghclient` already returns head, base and each file's patch. This removes the pack upload, the verifier, the `have` call and the merge-base upload. Costs: a whole-tree fetch on first open for search and the assistant, GitHub rate limits, and nothing prepared before someone looks.
  * **A bare git repository as the source store.** Git computes object names on receipt, diffs and searches natively. It needs a disk, which conflicts with decision 4.
* **Sources:** Z8-2, Z-4, D-1, D-2, I-1, I-4, I-6, C-1, IR-3, Z8-5, Z8-6.

### ACT-03. Make "nothing unscanned reaches a model" true - 🔴 Blocker

* **Problem:** The goal says the gateway scans the payload. The plan describes a lookup of scan results stored at upload, for file excerpts only. Pull request descriptions and intent records are sent to the model with no check named. Typed text is checked against six key prefixes that do not include `github_pat_`, the token form users are asked to paste. A scanner error is recorded permanently, and no job rescans after a scanner upgrade.
* **Options the reviewers raised:**
  * **Keep stored results and add repair:** a scan at the door on a miss, a rescan job, a scanner-version rule, and the real scanner over non-file text.
  * **Scan only at the door, at send time.** A missing result becomes "scan now", a failure is retried on the next call, and the goal becomes true as written. Scan time is unmeasured.
* **Either way:** state which check each kind of content gets and what a match does, and scope the goal to calls the service makes, since the CLI's own model lane can stay on in CI.
* **Sources:** Y-1, C-2, D-5, I-2, Z8-4, and part of IR-6.

### ACT-04. Rule on when the brief and AI review are generated - 🟡 Major

* **Problem:** Every push queues a brief and a review, and every description edit queues another brief. Agents push in bursts, most intermediate pushes are never opened, and background work shares one budget and rate limit with live questions. The document contains no token estimate.
* **Options:** keep generation on push and add priority classes, a reserved interactive share and cancellation of superseded jobs; or generate when a reviewer first opens a head commit, which removes that machinery and leaves the job runner with one retention sweep. The cost of the second is that the first reviewer waits.
* **Either way:** give a weekly token estimate with its assumptions.
* **Sources:** Z8-3, I-5, D-8.

### ACT-05. Rule on the events workflow, and on building one identity route or two - 🟡 Major

* **Problem:** Pull request title, description and state arrive four ways, and only the live read from GitHub is trusted. The events workflow can be lost without notice, and any workflow run can send "closed" for another pull request. Two identity routes are built although "either one is enough to start", and the App route may never be permitted.
* **Options:** guard the events workflow with claim checks and a view-time comparison; or drop it and use the live read as the only source, with an age-based sweep for removal. Build both identity routes, or only the one an owner approves, keeping the interface.
* **Sources:** Z-6, Z8-7, I-4, D-4.

### ACT-06. Rule on the bucket, the key service and the dependency inventory - 🟡 Major

* **Problem:** The bucket's only stated use is keeping raw uploads in case the stored layout changes. The key service encrypts about ten tokens. `go.mod` requires one module today, and the licence check needs a hand-written row for every transitive module and fails on any MPL or EPL licence. The checking tool also cannot hold two npm trees, because it keys rows by bare package name and two packages appear in both.
* **Options:** drop the bucket and key service from the first version; or keep them and accept the inventory cost in writing, with a new ADR. A separate Go module for the service is one way to keep the CLI's inventory small.
* **Also needed:** confirm the SCIP bindings' import path and licence, give the pinned `scip-typescript` version, and check DOMPurify's licence, which two reviewers believe is dual-licensed with MPL-2.0.
* **Sources:** Z-7, Z8-6, IR-12.

### ACT-07. Rule on telemetry scope for the first version - 🟡 Major

* **Problem:** One pull request builds computed measures, two export formats, a baseline importer, a status page and a banner for ten users. The baseline job has no credential: it runs with no viewer present, and a standing token was rejected.
* **Option:** record usage events only in the first version, since events cannot be backfilled, and take the baseline with the existing `reviewer metrics` command.
* **Sources:** Z-8, and part of IR-11.

### ACT-08. Rule on what the AI review must find, and how the CLI's reviewer is reused - 🟡 Major

* **Problem:** The goal is "an AI review of the plan and the code". The reused CLI code discards any finding that is not on a changed line, in three places, and every finding must carry a file and a line. A remark about the plan or about affected unchanged code is thrown away. The plan also edits the CLI's own model adapters and adds an optional method of a shape ADR-0021 rejected.
* **Options:** say how plan-level and off-diff findings are produced, or drop "plan" from the goal. Use one adapter set inside the gateway and leave `internal/model` unchanged.
* **Sources:** Z-3, Z-9, and part of IR-6.

### ACT-09. Add an outcome measure and a decision at each feedback point - 🟡 Major

* **Problem:** The first version is called a test, and the plan names feedback points, yet all fifteen pull requests are committed up front. Every goal's check confirms that a feature exists. None measures the stated problem, that review is slow and shallow.
* **Action:** add one outcome measure with a number, and say what result at PR-52, PR-53 and PR-56 would stop or reshape the rest.
* **Sources:** Z-5.

## Fixes

### ACT-10. State which commit is uploaded and indexed, and confirm the base with GitHub - 🔴 Blocker

* **Problem:** The generated workflow checks out with no `ref`, which on a pull request run is GitHub's temporary merge commit. The index would describe the merge result while the snapshot shown is the head commit, so positions drift whenever the base branch has moved. This breaks the navigation rule on the normal path with no attacker. The diff's base comes only from the upload, so a pull request that edits its workflow can hide part of its own change and choose the guidance its AI review is held to.
* **Action:** build the index from the head commit and have the upload name the commit it was built from. Fetch the merge base from GitHub with the viewer's credential. Say what a reviewer sees for a symbol defined outside the committed tree, such as a dependency's type declarations.
* **Sources:** D-1, D-3, IR-4, C-4. Needed whenever CI builds the index, whatever ACT-02 decides.

### ACT-11. Resolve the journey that cannot happen - 🔴 Blocker

* **Problem:** CUJ 4 and the search goal promise the diff and text search while CI is still running. Source and index travel in one upload at the end of the CI job, so there is no such state. Some pull requests never deliver at all, for example when an earlier step fails.
* **Action:** follows from ACT-02. Under the CI push, either reword the journey to say the view waits for CI and links to GitHub's diff, or split the upload. Add a visible "CI did not deliver" state.
* **Sources:** C-1, I-1, D-2.

### ACT-12. Rewrite the rejection reasons in Alternatives Considered - 🔴 Blocker

* **Problem:** The standard forbids attributing a rejection to a person without a linked record. 22 of 29 entries open with "Rejected by author", and ten give no trade-off at all. Several reverse the linked brief with no pointer to where that was decided. The decision table has ten rows, but only five have numbered alternative groups, and two rows have no alternatives.
* **Action:** drop the attribution and promote the trade-off from each entry's Cons into its reason. Line up the table rows with the alternative groups. Do not invent links.
* **Sources:** S-1, D-9, Y-7, S-3, C-5.

### ACT-13. Fix the plan steps that break the existing build, CI and release - 🔴 Blocker

* **Problem:**
  * The planned database test helper fails when `REVIEWER_REQUIRE_TOOLS` is set. That variable is set for the whole CI workflow and the release job, which have no Postgres, and macOS runners cannot run one.
  * Go cannot embed `web/dist` from `internal/workspace/web`, and an embed that matches no files is a compile error in every job that does not first build the web application.
  * The Dockerfile builds the web files five pull requests before they exist.
  * Two tools build the image binary, and a second GoReleaser build would land in the archive the Action downloads.
  * The tool installer would put opengrep, which is LGPL, in the image.
* **Action:** state which packages each make target runs and how database tests are gated, where the web build output lives and what is committed so a bare build works, one builder for the image, and an image stage that copies only gitleaks.
* **Sources:** IR-1, IR-2.

### ACT-14. Give existing installs an upgrade route, and real installer tests - 🔴 Blocker

* **Problem:** `reviewer init` leaves existing files alone without `--force`, so it writes neither the new config block nor the workflow change into a repository that already has the CLI. Config loading rejects unknown keys, so an older pinned binary fails on an updated config. The plan modifies installer "golden files" that do not exist, and the test for "existing installs keep working" has no baseline.
* **Action:** describe the upgrade order for an existing install. Create the installer baseline before the templates change. Add `publish` to the subcommand list in `review.sh.tmpl` and its test.
* **Sources:** IR-5, and part of IR-13.

### ACT-15. Redefine the gateway interface so it can carry a conversation and the review job - 🔴 Blocker

* **Problem:** The interface has no field for the model's tool call, the tool's result or earlier turns, and no kind of part for the description, the brief or search results. The stream has no outcome event, so a quota or secret outcome cannot reach the assistant. The CLI's reviewer sends one flat prompt string with no file hashes, so the scan rule cannot apply to it, and it turns every provider error into a warning, so a quota outcome is invisible to the job runner.
* **Action:** define turn types, non-file parts and an outcome event. Define the adapter between the reviewer and the gateway, and how quota surfaces from it. List the files it touches.
* **Sources:** IR-6. Depends on ACT-03 and ACT-08.

### ACT-16. Make goals, checks and the verification table agree - 🔴 Blocker

* **Problem:** The document says each goal names its check, and four of fourteen do not. It says every goal has a named test, and six have no row while four rows are not goals. Several rows state absolutes their tests do not establish, such as "no wrong definitions" and "pull request code is never run". The planned `os/exec` import test would not see the subscription adapters the gateway links.
* **Action:** correct the two sentences or complete the table, and reword each row to the property its test shows.
* **Sources:** IR-7, IR-15, S-7, S-8, C-10, Y-6.

### ACT-17. Bind uploads to the run that sent them, and treat the new workflow permission as a risk - 🟡 Major

* **Problem:** An upload is accepted for any commit and pull request in the repository the token names, so one pull request's run can replace another's index or send "closed" for it. The rule for a repeated upload is undefined. The `id-token: write` permission goes to the job that runs pull request install scripts, and gives that code a GitHub-signed identity usable against anything that trusts these repositories' tokens. ADR-0018 fixes that job's permissions, so the change is a supersession.
* **Action:** required only if the CI push stays. Use the token's own claims, define the repeated-upload rule, and move publish to its own job. The Stage 8 reviewer notes that a separate publish job conflicts with a split upload unless the workflow has three jobs.
* **Sources:** D-4, D-6, Z8-5, and part of IR-4.

### ACT-18. Harden the scanner invocation - 🟡 Major

* **Problem:** The scanner is started without setting an environment, so it inherits provider keys and the database address. It is also started with no rule file, so it uses the scanned tree's own `.gitleaks.toml`, and a pull request could set its own allowlist. Untrusted uploads are parsed in the process that can decrypt every user's token.
* **Action:** start the scanner with an empty environment and an explicit service rule file. Both apply whatever else is ruled. The reviewers disagree on a second deployment role; the Stage 8 reviewer advises holding it until the other removals are decided.
* **Sources:** I-3, Z8-8, IR-8.

### ACT-19. Correct the prompt-injection statement, and block remote content in rendered output - 🟡 Major

* **Problem:** The document says the most injected text can do is produce a wrong answer for the user who asked. The brief and AI review are generated from author-controlled text and shown to every reviewer. A rendered answer containing an image or link can also send text the model read to another host.
* **Action:** state both cases, block remote resources in rendered model output including diagrams, and say that an empty AI review is not evidence of a clean change.
* **Sources:** D-7, Y-4.

### ACT-20. Add a Risks and Limits section with numbers - 🟡 Major

* **Problem:** The standard requires one, and risk content is spread over eight places. No limit carries a number. Default-branch and merge-base commits have no retention rule. Each commit stores a row per file, and search cost grows with history.
* **Action:** gather the risks after the rulings above, with measured or estimated sizes and a retention rule for every record kind. The Stage 8 reviewer advises against designing a fallback for each open question, since answering the questions costs less.
* **Sources:** S-2, D-10.

### ACT-21. Introduce the existing CLI, and define the document's terms - 🟡 Major

* **Problem:** A reader with no other file could not work out what the plugin is, what the model lane is, or what the caps limit, though the design reuses all three. "Base commit" has two possible meanings. Later work is classified four different ways. Several ADRs and CLI conventions are cited by name only. One component has three names.
* **Action:** add a short section on the existing CLI. Use one list of later stages. Pick one term per concept. The Stage 8 reviewer judged this the one addition worth making now.
* **Sources:** C-3, C-4, C-7, C-9, S-6, S-9, S-10, Y-10, IR-19.

### ACT-22. Calibrate claims to their mechanisms - 🟡 Major

* **Problem:** Several statements are firmer than what backs them.
  * "Without wrong answers" and "only when the compiler resolved it", for an index the document says cannot be verified.
  * "Keep pull request code off the server", when the service stores every file's text.
  * Access "on every read", when the answer is cached for some minutes.
  * "Ready when the CI step returns", with no ingest time measured.
  * GitHub "does not have" navigation or AI help, with no recorded check.
  * The "Usable after" column, which assumes a release and a deployment that only the last pull request provides.
  * The component diagram, which assigns scan and index to the job runner against the text.
* **Action:** reword each to what the mechanism delivers.
* **Sources:** Y-2, Y-3, Y-5, Y-8, Y-9, C-11, C-12, S-5, IR-16, IR-18.

### ACT-23. Settle the level of detail in Shared definitions - 🟡 Major

* **Problem:** The section claims to fix everything a pull request would otherwise decide, but gives table columns with no types, keys or allowed values, and routes with no request or response bodies. The readiness reviewers want it completed. The zoom-out reviewer wants it cut back to the two real interfaces, because it rests on unsettled decisions.
* **Action:** decide after ACT-01 and ACT-02. Until then, remove the claim that nothing is left to decide.
* **Sources:** IR-9, Z-10, Z8-9.

### ACT-24. Define the change brief - 🟡 Major

* **Problem:** The intent record's fixed form is never written down, and one pull request reads it before the next defines it. No rule identifies a plan or ADR file. "Unchanged code that refers to changed symbols" has no algorithm. Reading guidance from the base commit needs a config loader that reads only from disk today.
* **Action:** define the intent record format, the input selection and the output shape.
* **Sources:** IR-10, C-8.

### ACT-25. Supply the missing identity mechanisms - 🟡 Major

* **Problem:** The plan requires rejecting a token that carries write permissions, and no reviewer knows a GitHub call that reports a fine-grained token's own permissions. The `ghclient` change is misdescribed: the head commit field exists, and the description, author and timestamps do not. Adding a method to its interface breaks two test fakes that are not listed.
* **Action:** give the mechanism or drop the requirement, and list the real additions and the affected tests.
* **Sources:** IR-11.

### ACT-26. Complete the file lists - 🟡 Major

* **Problem:** The route table, the service wiring, the web route table and the web API client are each listed in one or two pull requests, though most pull requests change them. `docs/architecture.md` states a job count that becomes wrong at the first pull request.
* **Action:** list every file each pull request touches, and name the existing test helpers to reuse.
* **Sources:** IR-13.

### ACT-27. Extend the credential patterns - 🟡 Major

* **Problem:** The shared pattern does not match `github_pat_`, `ghu_`, `ghr_` or `ghs_`, the tokens this service stores. The planned log test can pass on another prefix and prove nothing about them. There is no function that detects a credential in typed text.
* **Action:** add the patterns, list `redact.go` in the plan, and say how typed text is checked.
* **Sources:** IR-14, and part of Z-9.

### ACT-28. Decide how a secrets false positive is cleared, and make CUJ 7 match - 🟡 Major

* **Problem:** A scanner hit in any changed file turns off all AI for that commit, with no way to clear it, and fixture credentials are routine. CUJ 7 matches neither of the gateway's two rules, and the document gives no reason for treating changed and unchanged files differently.
* **Action:** state one rule and the reason. The Stage 8 reviewer advises holding an allowlist until the first repository shows a false positive.
* **Sources:** I-7, C-6.

### ACT-29. Record three requested additions as declined for the first version - 🟢 Nit

* **Problem:** Three reviewer proposals serve needs the first version does not have: resumable answer streams, routing the CLI's model lane through the gateway, and storing git tree objects to save rows.
* **Action:** the Stage 8 reviewer advises declining all three. Record them under Future Improvements.
* **Sources:** I-8, I-9, I-10, Z8-10.

## Where the reviewers disagree

| Conflict | One side | Other side | Stage 8 view |
| --- | --- | --- | --- |
| The bucket | I-6: upload through it by signed address | Z-7: drop it | Favours dropping. I-6's premise is unmeasured |
| The upload and the publish job | I-1, D-2: upload source first, in the review job | D-6: that job must not hold the token permission | Both together need three jobs. Favours neither |
| The events workflow | I-4, D-4: keep it and guard it | Z-6: drop it | Favours dropping. The live read already corrects it |
| Open questions | D-10: design a fallback for each | Z-1: answer them first | Favours answering |
| Level of detail | IR-9, C-8: complete the definitions | Z-10: move them out | Correct the claims; add detail after the rulings |
| Deployables | I-3: two roles with separate accounts | Z-4: fewer components | Hold the second role; apply the empty scanner environment now |

## Suggested order of work

1. Answer the open questions and run the two trials (ACT-01).
2. Rule on ACT-02 to ACT-09.
3. Apply the fixes that hold whatever is ruled: ACT-10, ACT-12, ACT-13, ACT-14, ACT-16, ACT-18, ACT-19, ACT-21, ACT-22 and ACT-27.
4. Apply the fixes that depend on the rulings: ACT-11, ACT-15, ACT-17, ACT-20, ACT-23 to ACT-26 and ACT-28.

## Traceability matrix

Each raw finding maps to one action item. Severity is the reviewer's own.

| Finding | Stage | Severity | Subject | Action |
| --- | --- | --- | --- | --- |
| Z-1 | 5 | Blocker | Unknowns checked after the build | ACT-01 |
| Z-2 | 5 | Major | Off-the-shelf substitute not evaluated | ACT-01 |
| Z-3 | 5 | Major | Reused reviewer cannot give plan-level findings | ACT-08 |
| Z-4 | 5 | Major | Git's object store rebuilt in Postgres | ACT-02 |
| Z-5 | 5 | Major | No result would change the plan | ACT-09 |
| Z-6 | 5 | Major | More identity and state routes than needed | ACT-05 |
| Z-7 | 5 | Major | Bucket and key service not needed | ACT-06 |
| Z-8 | 5 | Major | Telemetry scope for ten users | ACT-07 |
| Z-9 | 5 | Major | Two adapters per vendor | ACT-08 |
| Z-10 | 5 | Nit | Implementation detail in architecture sections | ACT-23 |
| I-1 | 5 | Major | Source arrives only when CI ends | ACT-11 |
| I-2 | 5 | Major | Scan results are a cache with no repair | ACT-03 |
| I-3 | 5 | Major | Uploads parsed beside every credential | ACT-18 |
| I-4 | 5 | Major | CI signals lost without notice | ACT-05 |
| I-5 | 5 | Major | One undivided model budget | ACT-04 |
| I-6 | 5 | Major | Whole upload in one request | ACT-02 |
| I-7 | 5 | Major | No way to clear a false positive | ACT-28 |
| I-8 | 5 | Nit | Answer lives for one connection | ACT-29 |
| I-9 | 5 | Nit | Two model doors remain | ACT-29 |
| I-10 | 5 | Nit | Per-commit file lists not shared | ACT-29 |
| C-1 | 6 | Major | Push-to-upload state contradicts itself | ACT-11 |
| C-2 | 6 | Major | Description outside the scan rule | ACT-03 |
| C-3 | 6 | Major | Existing CLI never introduced | ACT-21 |
| C-4 | 6 | Major | "Base commit" ambiguous | ACT-10 |
| C-5 | 6 | Major | Who decided what is unclear | ACT-12 |
| C-6 | 6 | Major | CUJ 7 matches neither secret rule | ACT-28 |
| C-7 | 6 | Major | Later-stage constraints misaligned | ACT-21 |
| C-8 | 6 | Major | Brief inputs under-described | ACT-24 |
| C-9 | 6 | Major | ADRs cited without content | ACT-21 |
| C-10 | 6 | Nit | Goals and tests misaligned | ACT-16 |
| C-11 | 6 | Nit | "Compiler-resolved" overclaims | ACT-22 |
| C-12 | 6 | Nit | Diagram and "stateless" disagree with text | ACT-22 |
| D-1 | 7 | Blocker | Base commit never confirmed | ACT-10 |
| D-2 | 7 | Blocker | CUJ 4 cannot happen | ACT-11 |
| D-3 | 7 | Major | Index built on a different tree | ACT-10 |
| D-4 | 7 | Major | Upload not bound to its pull request | ACT-17 |
| D-5 | 7 | Major | Four gaps in the scan guarantee | ACT-03 |
| D-6 | 7 | Major | Token permission in the job that runs PR code | ACT-17 |
| D-7 | 7 | Major | Prompt-injection analysis too narrow | ACT-19 |
| D-8 | 7 | Major | Model spend on every push | ACT-04 |
| D-9 | 7 | Major | Alternatives Rule not met | ACT-12 |
| D-10 | 7 | Major | No scalability limits | ACT-20 |
| S-1 | 7 | Blocker | Unlinked attribution of rejections | ACT-12 |
| S-2 | 7 | Major | No Risks section | ACT-20 |
| S-3 | 7 | Major | Decision table and alternatives misaligned | ACT-12 |
| S-4 | 7 | Major | Alternative Z not considered | ACT-01 |
| S-5 | 7 | Major | Diagram disagrees with text | ACT-22 |
| S-6 | 7 | Major | Later work classified four ways | ACT-21 |
| S-7 | 7 | Major | Goals break their own rule | ACT-16 |
| S-8 | 7 | Nit | Goals and journeys not symmetric | ACT-16 |
| S-9 | 7 | Nit | Heading nesting | ACT-21 |
| S-10 | 7 | Nit | Link and reference gaps | ACT-21 |
| Y-1 | 7 | Blocker | "Nothing unscanned" overclaims | ACT-03 |
| Y-2 | 7 | Major | Navigation claims overstate the index | ACT-22 |
| Y-3 | 7 | Major | Absolute claims about GitHub | ACT-22 |
| Y-4 | 7 | Major | Injection bound stated as a maximum | ACT-19 |
| Y-5 | 7 | Major | "Code off the server" is false | ACT-22 |
| Y-6 | 7 | Major | Goals lack checks | ACT-16 |
| Y-7 | 7 | Major | Rejection reasons give no reason | ACT-12 |
| Y-8 | 7 | Major | Access, timing and rollback overstated | ACT-22 |
| Y-9 | 7 | Nit | Problem-section claims | ACT-22 |
| Y-10 | 7 | Nit | Terminology drift | ACT-21 |
| Z8-1 | 8 | Blocker | Fixes stacked on unconfirmed pillars | ACT-01 |
| Z8-2 | 8 | Major | CI push is the root of the largest group | ACT-02 |
| Z8-3 | 8 | Major | Generation on push is the root of spend fixes | ACT-04 |
| Z8-4 | 8 | Major | Stored scan result is a cache | ACT-03 |
| Z8-5 | 8 | Major | Split upload conflicts with publish job | ACT-17 |
| Z8-6 | 8 | Major | Bucket: drop or depend on it | ACT-06 |
| Z8-7 | 8 | Major | Events workflow: drop or guard | ACT-05 |
| Z8-8 | 8 | Major | Second deployment role should wait | ACT-18 |
| Z8-9 | 8 | Major | Requested text conflicts with cutting detail | ACT-23 |
| Z8-10 | 8 | Nit | Three speculative additions | ACT-29 |
| IR-1 | 9 | Blocker | Database test rule breaks CI | ACT-13 |
| IR-2 | 9 | Blocker | Embed, Dockerfile and release conflicts | ACT-13 |
| IR-3 | 9 | Blocker | Incremental pack unverifiable | ACT-02 |
| IR-4 | 9 | Blocker | Which commit `publish` uploads | ACT-10 |
| IR-5 | 9 | Blocker | No installer baseline; no upgrade route | ACT-14 |
| IR-6 | 9 | Blocker | Gateway interface cannot carry the work | ACT-15 |
| IR-7 | 9 | Blocker | "Every goal has a test" is false | ACT-16 |
| IR-8 | 9 | Major | Scan rule file and invocation | ACT-18 |
| IR-9 | 9 | Major | Shared definitions fix names only | ACT-23 |
| IR-10 | 9 | Major | Brief defined by intent only | ACT-24 |
| IR-11 | 9 | Major | Identity mechanisms missing | ACT-25 |
| IR-12 | 9 | Major | Licence inventory understated | ACT-06 |
| IR-13 | 9 | Major | File lists incomplete | ACT-26 |
| IR-14 | 9 | Major | Credential patterns miss stored tokens | ACT-27 |
| IR-15 | 9 | Major | Verification rows state absolutes | ACT-16 |
| IR-16 | 9 | Major | "Usable after" assumes a release | ACT-22 |
| IR-17 | 9 | Major | Hand checks scheduled too late | ACT-01 |
| IR-18 | 9 | Major | Existing rules cited too broadly | ACT-22 |
| IR-19 | 9 | Nit | Terminology | ACT-21 |
