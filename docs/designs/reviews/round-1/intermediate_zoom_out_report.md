# Goldfish Review Report: Stage 8, Intermediate Zoom Out

**Target:** `docs/designs/review-workspace.md` (full file, lines 1–1221)

**Standard:** `zoom-out-standard.md`

**What this pass did.** The document was not edited after Stage 5. This reviewer read the three earlier reports and judged the fixes they ask for as a set, against the standard's preference for the fewest components.

## Reviewer Status

| Reviewer | Status | Findings |
| --- | --- | --- |
| `egm-zoom-out` | completed | 1 Blocker, 8 Major, 1 Nit |

**Verdict.** Context sufficiency passes. The document still fails the standard on simplicity and on using what exists. Applying the three earlier reports as written would move it further away: together they ask for about twenty new mechanisms or sections, and only the Stage 5 zoom-out findings ask for anything to be removed. Most of the requested mechanisms guard four design choices. Changing those choices answers the same problems with less.

Findings Z8-2 and Z8-5 contest decision 2, which the document records as the author's, so they need the author's ruling. Findings Z8-3 and Z8-4 touch only rows 9 and 10 of the decision table, which the document calls proposals.

The reviewer could not verify GitHub or Cloud Run behaviour from here. Statements about job-level permissions, the merge-commit checkout, artifact downloads with a fine-grained token, and the request size limit are from memory.

## Prioritized Findings (Blockers -> Major -> Nits)

### Z8-1. Fixes are being stacked on pillars that are still unconfirmed - Severity: Blocker

* **Affected Scope:** lines 87–96, 627–631, 633–639, 712, 1211–1215
* **Discrepancy Description**: All ten Stage 5 zoom-out findings stand. Three are cheap checks whose result can delete whole groups of the requested fixes. Under Z-1, line 91 (pack and index size) decides whether I-6 has a problem at all, line 93 (which claims the workflow token carries) decides whether D-4's fix is possible, and lines 89–90 decide whether the index tool survives. Under Z-2, if a hosted editor or GitHub's own features cover navigation, the CI index is not built, and I-1, I-6, D-2, D-3, D-4 and D-6 have nothing to protect. Under Z-3, if the trial shows the reused reviewer cannot give plan-level findings, PR-57 changes shape, and I-5, D-7 and D-8 harden something else.
* **Rationale & Recommendation**: D-10 asks for "a fallback for each open question", which points the opposite way to Z-1. Designing three fallbacks costs more than answering three questions. Answer the questions first. Z-5 belongs here too: with a decision rule at PR-52, PR-53 and PR-56, fixes for the later pull requests need not be designed yet.

### Z8-2. Sending source through CI is the root of the largest group of fixes - Severity: Major

* **Affected Scope:** lines 173, 191–192, 225–234, 272–278, 317, 521–525, 615–619, 763–788
* **Discrepancy Description**: The service gets source only when the pull request's own CI job pushes it. Because the pull request controls that job, the design needs pack verification, an exchange to find missing objects and a merge-base upload. The earlier reports then add a split upload (I-1, D-2), an "upload missing" state and reconciler (I-4), a signed-address transport (I-6), a GitHub check on the base (D-1), a resume path (D-10) and part of the case for a second deployment role (I-3).
* **Rationale & Recommendation**: D-2's second option is the simpler primary: read the diff and files from GitHub with the viewer's credential. That read works wherever the push works, and also where the push cannot: CI running, CI failed, a merge conflict, a lost upload. The parts already exist. The design trusts a GitHub read as the viewer on every open, the token route already asks for contents read, and `internal/ghclient/client.go` already returns head, base and each changed file's patch. Alternatives L and AA reject pulling from GitHub because of a standing credential and polling; fetching on first open as the viewer needs neither, and the document never weighs it. It would remove the pack part of the upload, the `have` call, the `merge_base` kind and the pack verifier, and D-1, C-1, I-1, D-2 and I-6 lose their target. Costs to weigh: a whole-tree fetch on first open for search and the assistant, GitHub rate limits, and nothing prepared when no viewer is present. Rule on it together with Z-4, since both decide the source store. A zero-mechanism option also exists for C-1 and D-2: change CUJ 4 and line 48 to say the view waits for CI and links to GitHub's diff.

### Z8-3. Generating on every push is the root of the spend fixes - Severity: Major

* **Affected Scope:** lines 102, 236, 277, 338, 361, 396–402, 797, 834, 1118–1132
* **Discrepancy Description**: Every upload queues a brief and a review, and every description edit queues another brief. The stored result is a cache filled before anyone asks for it. The earlier reports ask for priority classes, a reserved interactive share, cancelling jobs for superseded commits, and pausing background work on quota (I-5, D-8).
* **Rationale & Recommendation**: D-8 lists "generate on first open" and "reserve a share" as if they add up, and they do not combine. If the brief and review are generated when a reviewer first opens a head commit, and stored as now, no background spend competes with questions, nothing needs cancelling, and no backlog drains a fresh quota window. The "edited" event then has no consumer, and the job runner and Cloud Scheduler are left with one retention sweep. Costs: the first reviewer waits, though the text can stream; CUJ 1's precondition changes; and a pull request nobody has opened has no brief. The standard argues against caches or queues before a bottleneck is shown. Keep D-8's token estimate, which is a number and not a mechanism.

### Z8-4. The stored scan verdict is a cache, and the reports ask for repair machinery around it - Severity: Major

* **Affected Scope:** lines 54, 194, 276, 298, 328–334, 830, 852–863, 1068–1078, 1202
* **Discrepancy Description**: The service scans each new file during upload and stores a verdict per file hash and scanner version, and the gateway only looks verdicts up. That lookup creates the states the reports then repair: no verdict, "could not scan" recorded for good, a verdict from an older scanner, and text with no file hash. The reports ask for a scan at the door on a miss, the real scanner over non-file text, a rescan job, and a scanner-version rule (I-2, D-5, C-2, Y-1).
* **Rationale & Recommendation**: I-2's first item makes the rest redundant. Once a scanner sits at the door, make it the only place that scans. At send time, scan the non-file text and the full content of each file an excerpt comes from. A missing verdict becomes "scan now", a scanner failure is retried on the next call, and an old version is a miss. Line 54's "scans the payload" becomes true as written, the scan stage leaves the upload request, and the check sits in the path, which is the shape ADR-0012 calls structural. No scan time has been measured to justify the cache. I-7's allowlist follows from line 331, where a hit in any changed file switches AI off for the whole commit. If the unchanged-file rule covered both cases, I-7 would lose most of its purpose; ADR-0012 blocks a whole run on any hit, so that is the author's ruling. Hold I-7 until the first repository shows a false positive.

### Z8-5. The split upload and the separate publish job cannot both be applied without a three-job workflow - Severity: Major

* **Affected Scope:** lines 92–93, 223–236, 274, 521–525, 763, 794–796, 962–963
* **Discrepancy Description**: No earlier report names this conflict. I-1 and D-2 put a source upload as the first step after checkout in the review job, so that job must hold `id-token: write`. D-6 says the job that installs and runs pull request code must not hold it, and moves publish to its own job fed by an artifact. Permissions apply to a whole job. Applying both needs one job to publish source, one to install and index, and one to publish the index, written into every repository beside the events workflow.
* **Rationale & Recommendation**: The standard favours neither stacking. D-6's own fix opens a simpler route: it first stores the index as a GitHub artifact, which GitHub ties to a run, an event and a head commit. Weigh having the service fetch that artifact as the viewer on first open. That would remove open questions 92 and 93, the change to ADR-0018's permission list, and D-4's forged-upload cases. The document dismisses artifacts only because of a standing credential. If the push stays, D-6 and D-4 are needed, and I-1 should not be applied inside the review job.

### Z8-6. Drop the bucket (Z-7) against upload through the bucket (I-6): the standard favours Z-7 - Severity: Major

* **Affected Scope:** lines 91, 175, 230, 276, 289, 733, 763, 929–932, 940
* **Discrepancy Description**: I-6 asks for a signed-address upload, a finalise endpoint, and verify, store, scan and index as job stages. That makes the bucket required, adds four stages to the job runner, and gives up the property that a commit is ready when the CI step returns.
* **Rationale & Recommendation**: I-6's premise is unmeasured. It rests on a platform limit recalled from memory and a pack size nobody has taken. After a repository's first upload a pack holds only changed objects, so the large upload happens about once per repository, and a one-off seeding step would cover it. Hold I-6 until line 91 is measured. D-10's "resume path for failed ingest" needs a sentence and not a mechanism: a repeated upload already changes nothing, so re-running the CI job is the resume path.

### Z8-7. Drop the events workflow (Z-6) against guard it (I-4, D-4): the standard favours Z-6 - Severity: Major

* **Affected Scope:** lines 192, 236, 318, 796, 798, 833, 1131–1132, 1166–1167, 1175
* **Discrepancy Description**: I-4 keeps the events workflow and adds a comparison with live GitHub state on each read, because events can be lost. D-4 keeps it and adds claim checks, because any run can send "closed" for another pull request.
* **Rationale & Recommendation**: I-4's comparison is the same live read Z-6 relies on. Once it exists, a lost or absent event has no lasting effect, and the workflow, the App webhook, the `source` column and D-4's event checks buy only earlier arrival of something the next view corrects. Use one path for pull request state: the live read. The one thing only an event provides is removal of data for a pull request that closes and is never opened again. A sweep keyed on the age of the last upload or view needs no pull request state, and also gives default-branch and merge-base commits the retention rule D-10 says they lack.

### Z8-8. The second deployment role (I-3) should wait; one part of it is a removal to apply now - Severity: Major

* **Affected Scope:** lines 175, 247, 264, 326, 648, 748, 879, 1053, 1072–1074
* **Discrepancy Description**: I-3 is right on the facts. Provider keys in environment variables are visible to the whole process, and `internal/analyzers/gitleaks/gitleaks.go` starts the scanner without setting an environment, so it inherits them. The fix asked for is two Cloud Run services with separate service accounts, which one operator must keep in step for ten users.
* **Rationale & Recommendation**: The exposure has three sources, and each has a removal. Parsing CI-built packs beside credentials is answered by Z8-2 or Z-4. The scanner inheriting the environment is answered by starting it with an empty one, which should be applied now. The breadth of credentials held is answered by Z-6 and Z-7. What remains is a size-limited parse of the index file; decide on a second role then. Z-4 and I-3 pull opposite ways on the number of deployables, and Z-4's bare repository would also have `git` parse CI-built packs in the same process tree unless the objects come from GitHub.

### Z8-9. The document additions the critics ask for conflict with Z-10 and Z-1 - Severity: Major

* **Affected Scope:** lines 44–59, 170–183, 419–424, 445–639, 643–668, 716, 1191
* **Discrepancy Description**: The comprehension and critic reports ask for more text. Z-10 and Z-1 ask for about 530 lines of file-level plan to be held. Adding detail now writes more on top of unsettled decisions.
* **Rationale & Recommendation**: In most cases a deletion satisfies the same finding. For C-8, remove the claim on line 716 that these definitions leave no pull request anything to decide. For S-7, C-10 and Y-6, correct lines 44 and 1191, and do not add checks or journeys for the telemetry goal until Z-8 is ruled on. For S-1, D-9 and Y-7, delete "Rejected by author" and promote the Cons bullet that already states the trade-off. For C-7 and S-6, trim lines 419–424. For S-2, a Risks section is a reasonable place to gather what exists; write it after the removals, and let "accepted, re-run CI" be a valid entry. The one addition worth keeping is C-3's short account of the existing CLI, because the reuse claims are where the design is least checked.

### Z8-10. Three requested additions are speculative for a ten-user first version - Severity: Nit

* **Affected Scope:** lines 54, 84, 95, 266–268, 282–288, 342, 355, 712, 836–837, 1107
* **Discrepancy Description**: I-10 (store tree objects or per-commit differences) serves a shared service the document lists as not planned, and rebuilds more of git. I-9 (route the CLI's model lane through the gateway) adds an inbound model proxy, while the lane is off in every generated config and the inventory is unanswered. I-8 (resumable answer streams) guesses at phone behaviour before the PR-56 feedback point.
* **Rationale & Recommendation**: Decline all three for the first version. Y-1's fix, scoping line 54 to calls the service makes, costs one clause. Conversations are already stored and can be read back, so letting a turn finish after a disconnect needs no new store.

## Disposition of each requested addition

| Requested addition | Asked by | Disposition | Finding |
| --- | --- | --- | --- |
| Split upload | I-1, D-2 | Do not apply. Reword CUJ 4, or read from GitHub | Z8-2, Z8-5 |
| "Upload missing" state and view-time reconciler | I-4, D-2 | Keep the live read as the only source of pull request state | Z8-2, Z8-7 |
| Signed-address upload, ingest as job stages | I-6 | Hold until line 91 is measured | Z8-6 |
| Base commit confirmed with GitHub | D-1 | Required if the push stays; no target under Z8-2 | Z8-2 |
| Token-claim binding, repeated-upload rule | D-4 | Required if the push stays; needs line 93 answered | Z8-1, Z8-5 |
| Separate publish job | D-6 | Required if the push stays; conflicts with I-1 | Z8-5 |
| Second deployment role | I-3 | Hold. Apply the empty environment now | Z8-8 |
| Scan at the door | I-2 | Apply, as the only scan | Z8-4 |
| Rescan job, scanner-version rule | I-2, D-5 | Not needed once the upload-time scan is gone | Z8-4 |
| Real scanner over descriptions and typed text | D-5, I-2, C-2, Y-1 | Follows from scanning at send time | Z8-4 |
| Allowlist from the base commit | I-7, D-5 | Hold for evidence | Z8-4 |
| Priority classes, reserved share, cancelling superseded jobs | I-5, D-8 | Not needed if generation waits for first open | Z8-3 |
| "Author says" and "code shows" split in the brief | D-7 | Hold until Z-3 is settled | Z8-1 |
| Resumable streams, CLI lane through the gateway, tree-object storage | I-8, I-9, I-10 | Decline for the first version | Z8-10 |
| Risks section, limits, retention per record, fallbacks | S-2, D-10 | Write after the removals; no fallbacks | Z8-1, Z8-9 |
| More checks, journeys, forms and ADR text | S-3, S-7, S-8, C-8, C-9, C-10 | Correct the claims instead | Z8-9 |

## Small fixes to apply whatever is ruled

None of these adds a moving part.

* **D-3:** build the index from the head commit. The generated workflow checks out with no `ref`.
* **D-7:** block remote resources in rendered model output.
* **I-3:** start the scanner with an empty environment.
* **Z-9:** add the `github_pat_` pattern, which is missing from `internal/model/redact.go`.
* **D-8:** give the token estimate.
* **Wording corrections:** Y-1, Y-2, Y-4, Y-5, Y-8, C-6, C-11, C-12, S-5.

## Suggested order

1. Answer the open questions on lines 89–94 and run the Z-2 and Z-3 trials.
2. Get the author's rulings on Z-4 together with Z8-2, on Z8-3 and Z8-4, and on Z-6, Z-7 and Z-8.
3. Apply the small fixes above.
4. Revisit the held items (I-3, I-6, I-7, D-1, D-4, D-6) against what is left.

## Evidence the reviewer confirmed in the repository

* `ghclient` returns head, base and per-file patches. It has no compare or merge-base call.
* The gitleaks child process inherits the environment.
* The generated workflow holds only `contents: read` and `pull-requests: write`, and ADR-0018 fixes that list.
* `go.mod` requires one module.
* No `cmd/workspace`, `internal/workspace`, `internal/publish` or `web` code exists yet.
