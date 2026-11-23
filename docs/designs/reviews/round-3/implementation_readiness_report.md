# Goldfish Review Report: Stage 9, Implementation Readiness Test (round 3)

**Target:** `docs/designs/review-workspace.md` (DetailedRange, lines 693–1129), as revised after the second review round.

**Standard:** `implementation-readiness-standard.md` for the readiness and codebase reviewers. The style reviewer applied its own audit vectors.

**Verdict:** the plan does not meet the standard. PR-48, PR-49 and PR-50 are close to ready. PR-47 waits on a file the document does not hold. PR-51 and PR-52 need rules that are named but not defined. PR-53 and PR-54 cannot be built as written.

## Reviewer Status

| Reviewer | Status | Findings |
| --- | --- | --- |
| `egm-implementation-readiness` | completed | 1 Blocker, 8 Major, 1 Nit |
| `egm-codebase-details` | completed | 1 Blocker, 6 Major, 5 Nit |
| `egm-style` | completed | 0 Blocker, 8 Major, 2 Nit |

The three reviewers overlap, so their 32 raw findings are merged into 18 below. Each entry names the raw findings it came from: `R-n` for readiness, `K-n` for codebase and `P-n` for style.

All three could research on the web and run experiments outside the repository. Between them they resolved the Go driver and the web packages, built a real index with `scip-typescript` 0.6.2, ran the pinned secrets scanner, and ran the existing patch parser on a patch with context lines. None changed a file in the repository.

## Prioritized Findings (Blockers -> Major -> Nits)

### IR-1. The conversation interface cannot carry what both providers need back in a tool loop - Severity: Blocker

* **Reviewer Lens:** `egm-implementation-readiness` (R-1)
* **Affected Scope:** lines 792–811, 834, 1066–1067, 1084
* **Discrepancy Description**: The interface has four part types and four event types. None can carry the opaque reasoning data that both providers attach to a tool call and want returned with the tool result. Google documents that its current models answer the follow-up request with a 400 when the thought signature on a function-call part is not returned ([thought signatures](https://aistudio.google.com/docs/thought-signatures?generate-content=true)). Anthropic documents that thinking is on by default on most current models, and lists returning thinking blocks unchanged within a tool-use turn as required ([thinking](https://platform.claude.com/docs/en/build-with-claude/thinking)). The request type has no thinking setting, and the stored `parts` use the same part types.
* **Rationale & Recommendation**: The tool loop is the point of PR-53 and PR-54. As written it fails on its second round with one provider and breaks a documented requirement of the other, and the hand-written fixtures would pass regardless. Add an opaque provider part and event that adapters pass through unchanged, say whether the gateway scans it, and store it; or fix a thinking setting per provider and prove it with a recorded two-round exchange.

### IR-2. The Google adapter rewrites model names, and its path cannot stream - Severity: Blocker

* **Reviewer Lens:** `egm-codebase-details` (K-1)
* **Affected Scope:** lines 88, 735, 1066
* **Discrepancy Description**: The design says no model name is compiled in. `geminiShape.path` in `internal/model/gemini.go` names six models and rewrites four configured names to two preview names, and `TestGeminiNormalizesPreviewModelNames` pins that. PR-53 reuses this "endpoint code", so a repository configured with one of those names would be sent to a different model from the one the call record and the `done` event name. The same function returns a path for the non-streaming method, while Google's streaming method has a different path.
* **Rationale & Recommendation**: Correct the Background statement. In PR-53, say how the Google converser builds its path, whether the rewrite applies, and which model name is recorded. List `shapes_test.go` if `path` changes.

### IR-3. The import rule cannot hold, and Goal 7's test sees direct imports only - Severity: Major

* **Reviewer Lens:** `egm-implementation-readiness` (R-2), `egm-codebase-details` (K-3), `egm-style` (P-1)
* **Affected Scope:** lines 816–822, 987, 1009, 1072, 1108
* **Discrepancy Description**: PR-53 adds a rule that only `gateway` may import `internal/model`. The gateway's own interface is written in that package's types, so `assistant`, `api` and `store` must import it. `Redact` also lives there, PR-49 extends it for GitHub errors, and those errors arise in `identity`, `access` and `api`. Separately, the Goal 7 test sees only direct imports of `os/exec`. Three CLI packages the service links import it themselves, including `internal/model/clisub.go`, which starts two CLI programs. Nothing checks "the image holds no `git` and no shell".
* **Rationale & Recommendation**: As written the rule fails on PR-49's code and blocks PR-54. Narrow it to "only `gateway` calls `NewConverser`", or have `gateway` re-export the types. Name the package that redacts GitHub errors. Say "imports `os/exec` directly", list which CLI packages each service package may import, forbid `model.New`, and add an image check.

### IR-4. Three statements about retries disagree - Severity: Major

* **Reviewer Lens:** `egm-style` (P-2), `egm-codebase-details` (K-1)
* **Affected Scope:** lines 441, 811, 1066
* **Discrepancy Description**: The gateway section says the conversation adapters share the existing "request, retry and redaction code". The interface says "the adapters do not retry". The file table lists the reused code as header, endpoint and redaction. The existing code retries three times on a rate limit and on a server error.
* **Rationale & Recommendation**: The two readings give different behaviour for `rate_limited` and `unavailable`. State one behaviour in all three places. If there is no retry, say the conversation path bypasses the existing retry loop.

### IR-5. The assistant's tools, prompt and limits are named and not defined - Severity: Major

* **Reviewer Lens:** `egm-implementation-readiness` (R-3), `egm-style` (P-3)
* **Affected Scope:** lines 747–754, 769, 799, 816–820, 838, 904–906, 1084
* **Discrepancy Description**: The six tools are given by purpose only: no names, argument schemas, result shapes, size caps, or which part type each result uses, which the gateway's scan rule depends on. The system prompt has no text. The request needs `maxTokens` and `timeout`, but neither the gateway call nor the configuration supplies them, while the cost estimate assumes 2,000 output tokens. The loop stops at a round or token limit, but a stream must end in a gateway outcome and none covers either limit. `toolRounds` is commented as tool calls, which is not rounds. How `questionTokens` is counted is not stated.
* **Rationale & Recommendation**: Each is a contract with the model or the user that the implementer would have to invent, and the round-limit test has no pass condition. Add a tool table, the prompt text, configuration keys for output tokens and call timeout, and an outcome and message for each limit.

### IR-6. The symbol identifier rules do not define a parser - Severity: Major

* **Reviewer Lens:** `egm-implementation-readiness` (R-4), `egm-codebase-details` (K-12)
* **Affected Scope:** lines 918–920, 1054–1055; rules at 372–375
* **Discrepancy Description**: The plan says name parts are "separated by spaces". In an index the reviewer built with `scip-typescript` 0.6.2 they are concatenated, typed by suffix, and may be escaped with backticks. "The smallest occurrence" is not unique: a constant and a type with the same name give two occurrences with the same range. The name check rejects every `new Circle(...)`, because the text asked about is `Circle`, the target text is `constructor`, and the last name is `<constructor>`. "Bit 1" reads as the second bit, while the schema's definition flag has value 1 and the import flag value 2. A local identifier is any simple identifier, not a number.
* **Rationale & Recommendation**: An implementer following the text gets a wrong parser, an arbitrary tie-break, and constructor lookups that always fall to text search. Quote the descriptor grammar from the schema and give the name rule with examples for each descriptor kind. Define ties and a constructor rule. Write "value 0x1".

### IR-7. A commit can have several index runs, and other run results have no state - Severity: Major

* **Reviewer Lens:** `egm-implementation-readiness` (R-5)
* **Affected Scope:** lines 857, 880, 1052, 1054; states at 338–345, 377
* **Discrepancy Description**: The four-state table assumes one run per commit. A commit at the tip of more than one pushed branch has more than one run, which is routine for a merge base. No rule says which run wins when they differ, or how GitHub's other conclusions (`skipped`, `timed_out`, `neutral`, `stale`, `action_required`) map. Nor is the state given when a successful run has no artifact, the artifact has expired, or the zip holds something other than one `index.scip`.
* **Rationale & Recommendation**: The state shown would depend on list order. State a selection rule, for example the newest successful run with a live artifact, else running, else failed, else none, and give a row for every status and conclusion.

### IR-8. Conversations: no way to find one, no stored format, no write order - Severity: Major

* **Reviewer Lens:** `egm-implementation-readiness` (R-6), `egm-style` (P-8)
* **Affected Scope:** lines 833–834, 846, 865–866, 874, 884–886, 891, 906, 1022, 1085–1087, 1107
* **Discrepancy Description**: No route lists a user's conversations, although following the user between devices was a stated reason for the database. `parts` has no JSON encoding, and the mapping from stored turns to the API's `Message` is not given. When turns are written is not stated, including whether a question stopped as `secret_blocked` is stored. `GET /api/v1/me` returns repository names outside the routes the access rule and the access test cover, and the plan does not say the list holds only repositories the user can read.
* **Rationale & Recommendation**: Storing a blocked question would put the credential in the database and block every later question as an "earlier turn". Add a list route keyed by repository, pull request, commit and user. Define the JSON for each part and the write order for the success, stopped and dropped-connection cases. State how the `Me` list is filtered and add the route to the access test.

### IR-9. The quota forms are not listed, and one maps to the wrong outcome - Severity: Major

* **Reviewer Lens:** `egm-implementation-readiness` (R-7), `egm-codebase-details` (K-10)
* **Affected Scope:** lines 808, 1067, 1070, 1110; rule at 439
* **Discrepancy Description**: Tests must cover "each documented form of exhausted quota", and the forms are not listed. The rule given covers only the tier cap. A spend limit the organisation sets itself returns HTTP 400, which under the interface is "any other error" and becomes `unavailable` ([rate limits](https://platform.claude.com/docs/en/api/rate-limits)). The reset time appears only inside the message text. The fixtures are a glob whose directory is ambiguous.
* **Rationale & Recommendation**: The hand check before PR-53 concerns the account's spend cap, so the 400 form is a likely one in use, and the user would be told the provider could not be reached. Add a table per provider (status, error type and code, header, resulting error), say how the reset time is filled, and name each fixture file and its directory.

### IR-10. The GitHub client cuts off a streamed body at 30 seconds - Severity: Major

* **Reviewer Lens:** `egm-codebase-details` (K-2), `egm-implementation-readiness` (R-10)
* **Affected Scope:** lines 752, 1017, 1019, 1052
* **Discrepancy Description**: `ghclient.New` gives every client a 30-second timeout, which in Go also bounds reading the response body. A streamed archive or artifact therefore fails at 30 seconds whatever `limits.fetchSeconds` says, while E3's proposed pass is 60 seconds. Both calls are answered by a redirect to a signed address, and a transport error after the redirect prints that address with its query string. `paginate` decodes each page as a bare array, but the run and artifact lists are objects wrapping an array. The plan calls the response a "tar archive", and tests build archives in memory, so a wrong assumption about compression or a top-level folder passes them.
* **Rationale & Recommendation**: A fetch between 30 and 60 seconds would pass E3 and then fail in the service. Say the helper uses a client with no total timeout under a deadline from `fetchSeconds`, removes the address from transport errors, and pages the wrapped lists. Record the real archive layout from E3 and test against one small archive in that layout.

### IR-11. The patch parser returns hunk extents, and its old-path field is already filled - Severity: Major

* **Reviewer Lens:** `egm-codebase-details` (K-4, K-5)
* **Affected Scope:** lines 83, 849–850, 1018, 1020
* **Discrepancy Description**: `parse` takes each range from the hunk header, and its own comment says this is right only with no context lines. GitHub's patches carry three lines of context: a one-line change at line 135 came back as lines 132 to 138 in the reviewer's test. The plan passes these on as `ChangedFile.ranges` and calls them trusted line ranges. Separately, `FromPatches` already fills `OldPath` with the new path for every file that has a patch, and a pure rename has no patch and gets no old path, so "a rename can be read at its old path" fails for exactly that case. A change here also alters text the CLI's model lane puts in its prompt.
* **Rationale & Recommendation**: Either document `ranges` as hunk extents, or add a parser that reads hunk bodies, with a test on a patch that has context. Say which consumers use the field. Specify that the old path is set from the file list in both branches and only for renames, and list the test cases.

### IR-12. The exported scanner function cannot express today's call - Severity: Major

* **Reviewer Lens:** `egm-codebase-details` (K-6)
* **Affected Scope:** lines 1068–1069
* **Discrepancy Description**: Today's `scan` scans one file with the working directory set to the repository root. The planned `ScanDir(ctx, binary, dir, options)` has one path, and its options hold no working directory, so "today's arguments" cannot be expressed. The working directory matters: an ignore file there suppressed the fixture finding in the reviewer's run. The existing tests use a stub and would not catch a change. Nothing in the repository defines what `rules.toml` contains.
* **Rationale & Recommendation**: Add the working directory to the options, state that the analyzer still passes the root and a relative file, add a test that records the working directory, and say what the rule file holds.

### IR-13. The licence tool would not refuse a copyleft web package - Severity: Major

* **Reviewer Lens:** `egm-codebase-details` (K-7)
* **Affected Scope:** lines 954–956, 1044
* **Discrepancy Description**: `checkCopyleft` tests an inventory row only when its name is in the Go module set or the plugin's package set passed to it. The plan covers reading the web lockfile and re-keying rows, but not adding the web sets to that check, so a web row listed under a refused licence would pass. `checkBoundary` likewise reads only `go.mod` and the plugin's manifest.
* **Rationale & Recommendation**: The design relies on this refusal in three places. State that both web sets go to both checks and that `web/package.json` joins the manifest list. List the test cases: one name in two sections, a copyleft web row refused, a development-only entry excluded.

### IR-14. PR-47's template is not in the document, and the gates have no values - Severity: Major

* **Reviewer Lens:** `egm-implementation-readiness` (R-8), `egm-style` (P-6, P-7), `egm-codebase-details` (K-9)
* **Affected Scope:** lines 695, 714–727, 773, 966–971, 1117–1123; targets at 657–660
* **Discrepancy Description**: PR-47's template is "the file that passed E2", and nothing says where that file is recorded. The template also renders a different file per repository, while E2 runs a hand-written file on one. The workflow paragraph omits the indexer command and flags, the action versions and the `corepack` step the review template uses. `manualSteps` tells a bun user to add an install step to the review workflow only, so the index job would fail on every push to a bun repository. "Sets Node 22" repeats a value the template already has. E2 is due "before PR-47 merges" in one place and sits under "before PR-47 starts" in another. Thresholds have no values, the "two questions" are not written, two hand checks have no pass value, and the 200 ms lookup target is "measured in PR-52" with no step that measures it. No work remains after PR-54 for the second feedback week to gate.
* **Rationale & Recommendation**: PR-47 cannot be written from this document, and three gates cannot be judged. Paste the passing workflow into the plan with its template variables, and give E2 one deadline. For each measure give its source, whether it has a threshold, and what stops if it is missed.

### IR-15. The web application's addresses, its GitHub links and its line conventions are undefined - Severity: Major

* **Reviewer Lens:** `egm-implementation-readiness` (R-9), `egm-style` (P-10)
* **Affected Scope:** lines 850, 864–867, 894, 1033, 1036–1037, 1039
* **Discrepancy Description**: Goal 1 requires an address that mirrors GitHub's path, but the router is described only as "four views", and the handler has no rule for which paths return the page. The GitHub link form for commenting at a line is not given, and GitHub documents no line anchor for the pull request files page. `line` is 0-based and `ranges` are 1-based, while the start and end lines of an excerpt, a selection and a location have no stated base. A selection on a removed line has no commit field.
* **Rationale & Recommendation**: Nothing checks the web application against the running service, and the same implementer writes the fake API, so an off-by-one or a wrong link passes every test. Add an address table, the exact GitHub link forms for added and removed lines, and one convention for every line field.

### IR-16. Verification rows claim more than the listed tests show - Severity: Major

* **Reviewer Lens:** `egm-style` (P-4)
* **Affected Scope:** lines 969, 972, 1019, 1038, 1054, 1070, 1103, 1109, 1113–1114
* **Discrepancy Description**: Row 12 says "the existing files are written as before", but PR-47 changes the skill template, one of the seven files `reviewer init` writes. Row 13 drops journey 10, which Goal 13 names as its check, and no pull request runs it. Journey 12 is "covered by" a test with no cancelled request. Row 8's "each kind of content" has no case for earlier turns or for a credential elsewhere in a quoted file. Row 2's "each label is produced" has no case for "not found".
* **Rationale & Recommendation**: Reword rows 12 and 13 to what the tests show, and add the missing cases or narrow "each" and "covered".

### IR-17. The service is built on a Go release that is out of support - Severity: Major

* **Reviewer Lens:** `egm-style` (P-5)
* **Affected Scope:** lines 947, 989
* **Discrepancy Description**: The service is built "on the official Go 1.24 image", and `go.mod` "stays at Go 1.24". Go supports a release until two newer ones exist. Go 1.26 came out on 2026-02-10 and 1.27 followed; the 1.24 image was last updated on 2026-02-08 ([release history](https://go.dev/doc/devel/release)).
* **Rationale & Recommendation**: A service that parses archives and artifacts with the standard library would be built on a toolchain that no longer gets security fixes. Build on a supported image, since the `go` line is a minimum and does not force the image, or state why 1.24 is accepted. Name the Go version the `workspace` CI job uses.

### IR-18. Small facts and wording - Severity: Nit

* **Reviewer Lens:** `egm-codebase-details` (K-8, K-10, K-11), `egm-style` (P-9, P-10)
* **Affected Scope:** lines 79, 88, 710, 838, 950–952, 1009, 1011, 1024, 1040, 1067, 1073, 1077–1078
* **Discrepancy Description**:
  * **Token redaction.** A fine-grained token has underscores inside it. A pattern copied from the existing one, which allows only letters and digits, would redact up to the first inner underscore and leave the rest.
  * **The install script** has three callers, not two: `make tools` and both workflows.
  * **`@types/markdown-it`** is not needed, because the current `markdown-it` ships its own type declarations.
  * **`navigation_report`** is said to hold no code, but its `note` column is free text typed by the user.
  * **Background statements.** The CLI is not stateless in every mode: `reviewer watch` keeps a watermark file. The host-name test matches a URL on any line, comments included, and does not catch a bare host name. `make check` already needs Node for the plugin, so "need Node" is not why the web tests sit outside it.
  * **Other.** The `.gitignore` row omits the new tool's binary. The generated types file's licence header is not mentioned. The plan says "CUJ 8" where the feature sections say "journey 8". Four "what changes and why" cells are empty or say only "metadata only".
* **Rationale & Recommendation**: Correct each.

## Checked and found to hold

* **The Postgres driver.** `github.com/lib/pq` v1.12.3 resolves and builds in a scratch module on Go 1.24, declares Go 1.21, is MIT-licensed and adds no other module.
* **Web packages.** A fresh resolution gives 26 distributed packages, all MIT, BSD-2-Clause or PSF-2.0, with `lightningcss` development-only.
* **The indexer and the schema.** `scip-typescript` 0.6.2 exists and needs Node 22. On a small project its documents use UTF-16 positions and the older range form. The field numbers in the plan match the schema at v0.10.0.
* **The secrets scanner.** The pinned version, with the planned flags, layout and an empty environment, reports the fixture credential in plain form, behind an exemption comment and wrapped in base64. Named as a lockfile it is not reported, as the design says.
* **The installer and CI.** The TypeScript condition, the file counts in three tests, the seven CI jobs, the `required` job, and the ADR tool's rules are as the plan describes.
* **The host-name test.** The planned Go code can pass it.

## Not verified

* Live behaviour of GitHub's archive, compare, workflow-run and artifact calls with a fine-grained token. E3 tests these.
* GitHub's archive layout and redirect, and its line anchor on the pull request files page.
* That pushing a second branch at an existing commit starts a second run.
* What Anthropic's API returns when thinking blocks are dropped in a tool-use turn.
* Google's quota error forms and streaming format.
* Cloud Run streaming and timeouts, the Dockerfile images, and which new file types the licence-header check inspects.
* Nothing from the plan was built.
