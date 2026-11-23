# Goldfish Review Report: Stage 6, Comprehension Test (round 3)

**Target:** `docs/designs/review-workspace.md` (full file, lines 1–1129), as revised after the second review round.

**Standard:** none. The test is whether the document is self-contained for a reader with no other context.

**How it was run:** one reviewer that read only the target file. It opened no other file, followed no link and did no web research.

## Reviewer Status

| Reviewer | Status | Findings |
| --- | --- | --- |
| `egm-comprehension` | completed | 0 Blocker, 8 Major, 4 Nit |

**Verdict:** the reviewer could paraphrase every section, so there is no Blocker. Eight points cannot be worked out from the document alone.

## What this proposes

A web application, the "review workspace", that a team of 10 engineers uses beside GitHub's pull request page. GitHub stays the place to comment, approve and merge.

The first version adds TypeScript go-to-definition, find-references and text search at the pull request's commit, plus an AI assistant that answers questions by calling six read-only tools.

There are two parts:

* **A CI job per repository** runs `scip-typescript` on every push and saves a navigation index as a workflow artifact.
* **A Go service on Cloud Run** reads each commit's source archive and that index from GitHub with the user's own fine-grained token. The token is sealed in the session cookie and not stored. The service holds source and index in memory, serves a React and CodeMirror web app, and never runs pull request code.

Every model call passes one gateway. It scans outgoing content with gitleaks, enforces a monthly token budget per repository, sends only to that repository's one provider, and records the call in Postgres beside conversations and "this was wrong" reports.

The work is eight pull requests, PR-47 to PR-54. It is gated by three trials (E1 credential approval, E2 the indexer, E3 reading archives) and two feedback weeks whose thresholds are not yet set. A model-written "change brief" is deferred to a next step.

## How the existing system works, per this document

The repository holds `reviewer`, a stateless Go command-line tool. `reviewer init` writes a GitHub Actions workflow, a config file and an agent skill file into a repository. The workflow runs the tool on every pull request, and GitHub comments are its only store.

It has two lanes. The deterministic lane runs gitleaks, type checks, lint and convention rules. The model lane is off by default, sends the diff to a model, reports on changed lines only, and does not run if the scanner finds a credential or cannot run.

Model access sits behind a one-method, text-in text-out `Provider` interface over six access paths. Two are HTTP adapters for Anthropic and Google. There is no streaming, tool calling or token counting.

A small REST client and `diff.FromPatches` turn GitHub's per-file patches into changed files and line ranges without `git`.

The repository enforces its rules through `make check` and append-only ADRs: a licence inventory that refuses copyleft, no host or model names compiled in, nothing internal in history, and one `required` CI job.

On GitHub itself, pull request navigation is same-file name matching, and the team may not use Copilot Chat.

## Prioritized Findings (Blockers -> Major -> Nits)

### C-1. How a user gets back to a conversation is not stated - Severity: Major

* **Affected Scope:** lines 143, 447, 519, 569, 688, 866, 884–886, 906
* **Discrepancy Description**: Two alternatives are rejected because a conversation must follow the user between devices. The API has only "create" and "read by id" routes, with no way to find a user's existing conversations for a pull request, and storage on the device is ruled out. The document does not say whether a new commit starts a new conversation, given that a conversation is tied to one commit. It says the browser "reads the conversation back" after a dropped connection, yet Future Improvements lists answers surviving a dropped connection as not built.
* **Rationale**: A reader cannot work out how a user returns to a conversation on another device or after a reload. That is the stated reason for storing conversations at all.

### C-2. The question limits have no outcome and no stated values - Severity: Major

* **Affected Scope:** lines 425–437, 447, 619, 747–754, 799, 904
* **Discrepancy Description**: The assistant loop stops when "a configured limit on rounds or tokens is reached", but the outcome table and the `stopped` event have no outcome for either limit; `max_tokens` is described as the answer being cut short. The spend estimate relies on "the cap of 60,000 input tokens and 2,000 output tokens", but the configuration gives no values, has no key for output tokens, and treats zero as an error. It is not stated whether `questionTokens` counts one model call or the total over all tool rounds.
* **Rationale**: The reader cannot tell what the user sees when a question runs out of rounds, or where the figures behind the cost estimate are set.

### C-3. A credential elsewhere in the selected file: blocked or not? - Severity: Major

* **Affected Scope:** lines 208, 399, 406–408, 423
* **Discrepancy Description**: Step 1 of the gateway says that for a part quoting a file it takes the whole file. The rule for "the code the user selected" says nothing is sent, and CUJ 7 says this happens when the credential is "in the code the user selected". The document does not say whether a credential elsewhere in the same file, outside the selected lines, blocks the question.
* **Rationale**: There is no allowlist, so the two readings differ a lot. Under one, a user can never ask about any selection in a file that holds a fixture credential.

### C-4. Three statements about retries disagree - Severity: Major

* **Affected Scope:** lines 441, 811, 1066
* **Discrepancy Description**: Line 441 says the two HTTP adapters share their existing "request, retry and redaction code" for the conversation interface. Line 811 says "The adapters do not retry". Line 1066 lists the reused code as header, endpoint and redaction only.
* **Rationale**: Whoever implements PR-53 gets opposite instructions. How a rate limit behaves depends on which is right.

### C-5. Goal 7 and its check read as a contradiction - Severity: Major

* **Affected Scope:** lines 100, 328, 646, 987, 1068–1072, 1108
* **Discrepancy Description**: Goal 7 says the service starts one program, the scanner, and that a test fails if any service package imports the standard library's package for starting programs. The document never states how both hold: the start appears to happen inside the CLI's `internal/analyzers/gitleaks` package, outside the two directories the test covers. Only the `git` functions in `internal/diff` are named as forbidden calls, so it is unclear whether the test catches the service importing another CLI package that starts programs.
* **Rationale**: Read on its own, the goal and its check contradict each other. The strength of the "never executes" guarantee rests on the unstated part.

### C-6. The trial's subject is not explained - Severity: Major

* **Affected Scope:** lines 48, 478, 524
* **Discrepancy Description**: The change-brief trial and the rejection of "the workspace makes its own AI findings" both depend on how the model lane builds its prompt: "the repository's guidance documents, which it is told to obey", and review code that "cuts a long diff and drops failed checks without telling the caller". The Background section describes none of this. "Wider review context" and "declined once" at line 48 are not explained: the reader cannot tell what the wider context holds beyond the author's text, or who declined the trial and why.
* **Rationale**: The trial decides whether the next step is built. The reader needs to know what is being compared.

### C-7. When E2 must be answered, and what its recorded facts decide - Severity: Major

* **Affected Scope:** lines 149, 152, 371, 695, 1119
* **Discrepancy Description**: Line 149 says E2 is answered "before PR-47 merges". Line 1119 says it is run "before PR-47 starts", and line 695 says PR-47's template "is the file that passed" E2. E2 also records facts with no pass value or consequence: TypeScript major version, whether a registry secret was needed, build time, index size, and whether documents use UTF-16 positions. The last matters because Navigation drops every document not in UTF-16 units.
* **Rationale**: A reader cannot tell whether PR-47 may be written before E2 passes, or which recorded result would change the design.

### C-8. What a repository needs before it can be connected - Severity: Major

* **Affected Scope:** lines 34, 105, 162–163, 297, 310–315, 666–668, 971
* **Discrepancy Description**: The document does not say whether a connected repository must already have the CLI installed, though the index workflow and skill sections come from `reviewer init` and reuse the review workflow's install step. For existing installs, `reviewer init` does not overwrite the skill file without `--force`, yet Rollout lists the two skill sections as "what is added to a repository". How an existing install gets those sections is left as a question about "a narrower refresh", with no statement of what happens until then.
* **Rationale**: Goals 5 and 13 and journeys 1 and 10 depend on those sections being present. The steps to get a repository ready are assumed, not stated.

### Nits

| ID | Affected scope | Finding |
| --- | --- | --- |
| C-9 | 92, 106, 161, 1114, 1117–1123 | The Verification Plan is said to map each check to a test. Goal 13 names journey 10 as a check, but the plan lists only the skill template test, and nothing runs that journey. The touch test (journey 2 on a user's own phone) is missing from the checks to run by hand. |
| C-10 | 28–36, 98, 103, 140, 172, 312, 356, 441, 996 | "Intent record" is used before it is explained at line 312 and is not in the Terms table. "The operating guide" is used from line 356 but identified as `docs/workspace.md` only at line 996. Goals say "journey 8" where the section is headed CUJ 8. "A signed-in CLI" at line 441 means some other tool, though the Terms table fixes "the CLI" as `reviewer`. |
| C-11 | 251, 498–500, 503, 506 | ADR-0031 is said to build on ADR-0018, and the link between them is not stated. Decision 6 (the model calls read-only tools) is recorded in ADR-0034, whose stated subject is the conversation interface. What ADR-0008 and ADR-0016 decided can only be guessed from file names. |
| C-12 | 364, 808, 849–850, 855, 857, 864, 867, 894 | `line` is said to count from 0 on every route, but `ChangedFile.ranges` is 1-based, and the base for `Excerpt.startLine`, `Ask.selection.startLine` and `Match.line` is not given. `noPatch`, `IndexState.documents` and `ErrQuotaExhausted.known` are not explained. It is not said what GitHub's `ranges` are used for once the browser draws the diff from two file versions. |
