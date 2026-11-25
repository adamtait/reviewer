# Goldfish Review Report: Stage 5, Initial Zoom Out

**Target:** `docs/designs/review-workspace.md` (full file, lines 1–1221)

**Standard:** `zoom-out-standard.md`

**Verdict from the zoom-out reviewer:** the document does not meet the standard as written. Context sufficiency passes. The failures are in simplicity and in using what already exists.

## Reviewer Status

| Reviewer | Status | Findings |
| --- | --- | --- |
| `egm-zoom-out` | completed | 1 Blocker, 8 Major, 1 Nit |
| `egm-inventive-catalyst` | completed | 7 Major, 3 Nit |

Both reviewers read the target in full. Neither could check anything outside the repository. The zoom-out reviewer's dependency sizes are estimates, and the inventive reviewer's Cloud Run limits are from memory.

The zoom-out reviewer notes that findings Z-2, Z-4, Z-6 and Z-8 contest choices the document records as the author's, so they need the author's ruling and not only a redraft.

## Prioritized Findings (Blockers -> Major -> Nits)

### Z-1. Unknowns that can void the architecture are checked after the build - Severity: Blocker

* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** lines 87–96, 174, 276, 690–716, 763, 1211–1215
* **Discrepancy Description**: Eight open questions remain, and three can each remove a pillar of the design. If no organisation owner approves either credential, nobody can sign in. If `scip-typescript` is unmaintained or fails on `bun`, decision 3, ADR-0035 and PR-53 fall back to rejected alternatives. If a public upload address is not acceptable, decision 2 falls. The document gives two deadlines for these checks: line 89 says before the workflow template is written, and the only checklist says before the first repository is connected, which is after all fifteen pull requests. None is a precondition of PR-47. Index and pack size are unmeasured, yet the upload is already fixed as one request with all ingest work done inside it.
* **Rationale & Recommendation**: The concepts brief scheduled these checks before design finished because they cost almost nothing. About 530 lines of file names, columns and routes rest on assumed answers. Make the answers to lines 89–94 entry conditions for PR-47, and hold the file-level plan until they are in.

### Z-2. The one off-the-shelf substitute was never evaluated - Severity: Major

* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** lines 30–32, 633–639
* **Discrepancy Description**: The entry for a browser-hosted VS Code says "Not rejected, and not evaluated." The concepts brief required the design to weigh it. One listed drawback does not carry over: a hosted editor runs the checkout somewhere this service holds nothing, so the credential risk the design avoids does not arise. The Problem section also states that GitHub has no navigation and no way to ask, without recording a check of GitHub's current features.
* **Rationale & Recommendation**: The standard asks for established platforms before bespoke ones, and that cannot be confirmed when the comparison is recorded as not made. If an existing tool covers navigation, the service shrinks to the brief and the assistant. Run a time-boxed trial on one pull request, on desktop and phone, and write the result into the entry.

### Z-3. The AI review reuses a reviewer that cannot do what the goal asks - Severity: Major

* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** lines 36, 51, 260, 363–366, 627–631, 1124–1127
* **Discrepancy Description**: The goal is "an AI review of the plan and the code", fed with unchanged code that depends on the change. The reused CLI code is bound to changed lines in three places: `internal/laneb/prompts/review.md` line 25 ("Only the changed lines"), `internal/laneb/review.go` lines 171–193 (`place()` discards any finding not on a changed line), and `internal/laneb/prompts/invalidate.md` line 20. Every finding must also carry a file and a line. A remark about the plan, or about affected unchanged code, is thrown away by the reused code. The only planned change is two new input fields.
* **Rationale & Recommendation**: That change is the same experiment as Alternative AC, which was rejected in favour of testing the premise at PR-57. The Problem section's case against the CLI describes configuration and not capability. Either say how plan-level and off-diff findings are produced, or drop "plan" from the goal. Run the trial by letting the CLI fill the two new fields on one repository.

### Z-4. The service rebuilds git's object store, diff and search in Postgres - Severity: Major

* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** lines 84, 175, 255, 275, 282–289, 398, 561–565, 777–788, 794, 870, 927, 1004–1006, 1130, 1175, 1206
* **Discrepancy Description**: "A file's content is stored once, keyed by its git hash. A commit is a list of paths, each pointing at a file hash" describes git's own blobs and trees. Around it the design writes a pack verifier, a diff engine, a trigram search, reference-counted deletion, an exchange to find missing objects, and adds `go-git`. A bare git repository on disk does all of this with its own commands and computes object names on receipt, so a pack cannot misname an object. This repository already shells out to `git` in `internal/diff/diff.go`, and the brief marked that package for reuse. The cause is decision 4: a stateless container has no disk. The same choice forces Cloud Scheduler and paired local and hosted implementations for storage and keys.
* **Rationale & Recommendation**: Alternative R, one machine with a disk, was dismissed for a migration before a shared service (which the document lists as not planned) and for disk loss (though nearly all stored data can be rebuilt by re-running CI). The trade-offs are real: a persistent disk, `git` parsing untrusted packs, and unmeasured search speed. Weigh "a bare repository as the source store" explicitly under Alternatives Considered.

### Z-5. The first version is called a test, but nothing says what result would change the plan - Severity: Major

* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** lines 26, 38–40, 44–59, 712, 1149, 1194–1209
* **Discrepancy Description**: Line 40 calls the first version a test and line 712 names three feedback points, yet all fifteen pull requests and eight ADRs are committed up front. No rule says what feedback at PR-52, PR-53 or PR-56 would stop or reshape the rest. Every goal's check confirms that a feature exists. None measures the stated problem, that review is "slow and shallow". The brief's table of measures was not carried over.
* **Rationale & Recommendation**: Add one outcome measure with a number, and a decision at each feedback point. Time a pull request waits on review is one candidate, and GitHub history gives it with no new telemetry.

### Z-6. Identity and pull request state each get more routes than one deployment can use - Severity: Major

* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** lines 94, 172–173, 192, 232, 236, 313–320, 709, 731–732, 796–799, 833, 1002, 1131–1132, 1159–1167
* **Discrepancy Description**: Two identity routes are built, yet line 94 says "Either one is enough to start." PR-60 builds App sign-in, token refresh, installation tokens and a webhook receiver for a route that may never be permitted. Separately, pull request title, description and state arrive four ways: upload metadata, the events workflow, the App webhook and a live read as the viewer. Only the live read is trusted, and a `source` column exists just to track which copy is held. The events workflow delivers two signals that can be derived from the live read.
* **Rationale & Recommendation**: The single interface already keeps the App route open at no cost. Build only the route the owner approves, and drop PR-60 and the events workflow from the first version. The cost is that after a description-only edit the brief regenerates on first open.

### Z-7. A bucket and a key service are added for needs the document does not have - Severity: Major

* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** lines 289, 311, 417, 668, 733–734, 867–879, 918, 929–932, 940, 943, 977–980, 995, 1213
* **Discrepancy Description**: The bucket's only stated use is keeping raw uploads in case the stored layout changes, and re-running CI repeats an upload anyway. The KMS client encrypts about ten read-only tokens, while the document already delivers provider keys as environment variables with no client linked. `go.mod` requires one module today, and ADR-0028 requires the whole Go module graph in the hand-written inventory. The two Google clients bring dozens of transitive modules (the reviewer's estimate). The document also never classifies each store as rebuildable or not, which the brief asked for.
* **Rationale & Recommendation**: Drop the bucket and the KMS client from the first version. Then either say how the CLI's inventory stays small (a separate Go module for the service is one option) or accept the cost in writing.

### Z-8. Telemetry views, a baseline job and a status page for ten users - Severity: Major

* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** lines 58, 261, 402–410, 615–619, 708, 815–816, 845, 1144–1157
* **Discrepancy Description**: PR-59 builds computed measures, CSV and JSON export, a baseline importer with its own job kind and table, a status page and a banner. Ten engineers can be asked directly, and a SQL query answers the rest. `cmd/reviewer/metrics.go` already lists pull requests in a window with CSV output. As designed, the baseline job also has no credential: it runs with no viewer present, and a standing token was rejected under Alternative AA. "Baseline" already names the type-coverage ratchet in `cmd/reviewer/baseline.go`.
* **Rationale & Recommendation**: Only the recording of usage events must be in the first version, because events cannot be backfilled. The first version records events only.

### Z-9. Two adapters per model vendor, and the CLI's model package is changed to get them - Severity: Major

* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** lines 83, 258, 334, 336, 849–863, 900, 1084–1097
* **Discrepancy Description**: PR-55 writes new conversation adapters and also modifies the CLI's adapters to report usage. A one-call completion is a conversation with no tools, so the new adapters can serve both uses, and `laneb.Reviewer` needs only a `model.Provider`, which a wrapper over the gateway can supply. That removes the optional `CompleteWithUsage` method, a shape ADR-0021 rejected, and the edit to `openai.go`, a provider the document lists as not planned. The claimed reuse of "HTTP plumbing" is thin: `httpProvider`, `shape` and `newHTTP` are unexported. `Redact`'s patterns cover `ghp_` and not `github_pat_`, the prefix of the fine-grained tokens this design stores.
* **Rationale & Recommendation**: One adapter set in the gateway, leave `internal/model` alone, and extend the patterns.

### I-1. Source only arrives when the whole CI job ends, which contradicts CUJ 4 - Severity: Major

* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** lines 225–234, 272–280, 763, against line 48 and CUJ 4 (118–122)
* **Discrepancy Description**: The design sends source and index in one `publish` request, placed after the existing checks and after the indexer. Until that step runs the service holds nothing for the commit, and such a pull request shows "waiting for CI". CUJ 4 promises the diff and text search while the CI job is still running, and line 48 promises search while the index is being built. Neither state can occur. Journey 4 runs against a fake API, so the verification plan will not catch this.
* **Rationale & Recommendation**: Each push blanks the workspace for the whole job, while GitHub shows the diff at once. Upload the pack right after checkout, since it needs only git objects, and send the index as a second request for the same commit. Cost: two uploads and tokens per run, and new states for an index that never arrives.

### I-2. The outbound door reads a cache of scan results and cannot repair or extend it - Severity: Major

* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** lines 328–334, 852–863, 276, 1074–1078, 402, against line 54
* **Discrepancy Description**: Three holes. A scanner error records "could not scan"; only new files are scanned, a stored file is never new again, and no job rescans, so one scanner timeout switches AI off for those files for good. Results are keyed by scanner version, and the design does not say how the gateway treats an older version. The pull request description and intent record go to the model, but the `Part` type has no kind for them, so at best they get the typed-text check, which is six key prefixes that do not match a fine-grained GitHub token.
* **Rationale & Recommendation**: Keep the stored results as a cache and add repair. On a missing or stale result the gateway scans that content at the door and stores the result. Non-file parts go through the real scanner on every call. A rescan job works through failed or outdated rows. Cost: the first question touching unscanned content waits for the scanner.

### I-3. Untrusted uploads are parsed in the process that holds every credential - Severity: Major

* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** lines 175, 247–262, 326, 748, 879, 1072–1074, against the trust rule (187)
* **Discrepancy Description**: The trust rule treats every byte from CI as written by the pull request's author. The design then parses those bytes in the process that holds provider keys, the database address and the right to decrypt every user's GitHub token. Keys are said to be "read only by the gateway", but they arrive as environment variables, which are process-wide. `ScanDir` builds on the existing `scan`, which starts gitleaks without setting an environment, so the child process inherits all of them.
* **Rationale & Recommendation**: An import rule is a convention and not a boundary. The concepts brief's fix for blocking problem 1 was a credential part and a data part, and the design applied it to code execution only. Keep one binary and run it in two roles as two Cloud Run services with separate service accounts: an ingest role with no key access, and an app role unreachable with CI tokens. In either case start the scanner with an empty environment. Cost: two deployments to keep in step.

### I-4. Every signal from CI is fire-and-forget, with nothing to notice a loss - Severity: Major

* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** lines 192, 236, 665, 952–953, 402, 1175, Alternative AA (615–619)
* **Discrepancy Description**: `publish` retries a bounded number of times, then warns and exits 0. If the service is mid-deploy or the upload is cut, that commit never arrives, and the view says "waiting for CI" indefinitely. A lost "closed" event means removal is never scheduled. A lost "edited" event leaves a stale brief. The health address reports only the last upload per repository, which hides a single lost commit.
* **Rationale & Recommendation**: The design dropped the timer reconcile and put nothing in its place. The list and view already fetch live state from GitHub as the viewer, so compare it with the store on each read. A head commit not held after some minutes becomes "upload missing" with a re-run link. A pull request closed on GitHub gets removal scheduled, and a changed description requeues the brief. Cost: reconciliation runs only when someone looks.

### I-5. Background generation and interactive questions draw on one undivided budget - Severity: Major

* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** lines 338, 361, 236, 745, 660
* **Discrepancy Description**: Every push queues a brief and a review, the review is two model calls, and each description edit queues another brief. Most intermediate agent pushes are superseded before anyone opens them, yet each spends from the same monthly budget and rate limits as live questions. At a quota reset the backlog runs first, including commits that are no longer a head, and can drain the fresh window before a reviewer's question gets through.
* **Rationale & Recommendation**: The concepts brief proposed priority classes with a reserved interactive share, and the design dropped this without comment. Reserve a share of budget and per-minute capacity for interactive calls, cancel queued jobs for a pull request's older commits when a new head arrives, and treat a quota outcome as a signal to pause background work.

### I-6. The whole upload is carried and processed inside one request to the container - Severity: Major

* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** lines 276, 763, 230–234, 91, Alternative M (527–531)
* **Discrepancy Description**: One multipart request carries pack and index, and the handler verifies, stores, scans and loads the index before answering. As the reviewer recalls Cloud Run's limits, HTTP/1 request bodies are capped at 32 MiB, and a first upload "sends the whole tree". The container's filesystem is memory-backed, so the scan's temporary directory, the unpacked pack and the parsed index all sit in memory at once. A failure late in the request repeats the whole upload.
* **Rationale & Recommendation**: The transport decides whether a large repository can connect at all. Have `publish` present its identity token and receive a short-lived signed address for one bucket object, upload there, then call a small finalise endpoint. Verify, store, scan and index then run as job stages. This conflicts with Z-7, which asks for the bucket to be dropped.

### I-7. A scanner hit in any changed file turns off all AI, with no trusted way to clear a false positive - Severity: Major

* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** lines 330–332, CUJ 7 (136–140), 1074
* **Discrepancy Description**: The scanner runs with one service-wide rule file. Fixture credentials are routine, and this repository allowlists `testdata/` for that reason. A pull request that touches such a file loses the brief, the review and the assistant, and only an operator config change clears it.
* **Rationale & Recommendation**: AI help is half of what the first version tests, so false positives remove evidence. Read a path-scoped scanner allowlist from the base commit, as the design already does for guidance. The gate stays fail-closed. Cost: a merged allowlist entry is a standing hole.

### Z-10. Implementation detail sits in the architecture sections - Severity: Nit

* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** lines 377–392, 714–716, 777–788, 792–817, 821–845
* **Discrepancy Description**: The Technical Plan holds a twelve-row key-binding table. "Shared definitions" fixes column lists for 21 tables, a 24-row route table and pack-building pseudocode.
* **Rationale & Recommendation**: These are implementers' choices and will go stale once Z-1's questions are answered. Keep the two real interfaces, the upload format and the gateway interface, and move the rest into the pull requests.

### I-8. An answer lives only as long as one HTTP connection - Severity: Nit

* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** lines 342, 355, 1107–1108, CUJ 10 (154–158)
* **Discrepancy Description**: The tool loop runs inside the request that streams the answer. On a phone, locking the screen drops the connection, the spent tokens are lost and the user asks again.
* **Rationale & Recommendation**: Persist a turn's events as they arrive, and make the stream a resumable tail of the stored turn.

### I-9. The rationale against two model doors leaves two in place - Severity: Nit

* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** lines 266–268, Alternative X (601–605), 95
* **Discrepancy Description**: Line 266 rejects CI model calls because they mean two doors, two key locations and two budgets. Line 268 then keeps that arrangement wherever the CLI's model lane is on.
* **Rationale & Recommendation**: Moot if the inventory finds the lane off everywhere. Otherwise point the CLI's `laneB.baseUrl` at the gateway, authenticated by the workflow's identity token.

### I-10. Content is stored once, but the per-commit file lists are not - Severity: Nit

* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** lines 282–288, 828, 831, 424
* **Discrepancy Description**: Each commit writes one row per file in the repository, so cost per push grows with repository size. The verified pack already holds git's tree objects, which share unchanged directories, and the design flattens them away.
* **Rationale & Recommendation**: Affordable for ten engineers, and it matters only for the later shared service. Store tree objects keyed by tree hash, or store each commit's list as a difference from its base.

## Reuse claims the zoom-out reviewer confirmed

* An exported `ScanDir` over the existing unexported `scan` in `internal/analyzers/gitleaks/gitleaks.go` is feasible.
* `ghclient.New` takes a token per client, so per-user clients work. No `GetRepo` exists yet, as the plan says.
* `tools/checkadrs/main.go` permits a status-only edit to a superseded ADR.
* PR-46 is the last existing pull request, and every `make` target the plan names exists.
* No navigation index or symbol-lookup code exists in the repository to reuse.

The zoom-out reviewer also judged the trust rule, the non-goals list and the single outbound door to be sound and not over-built.
