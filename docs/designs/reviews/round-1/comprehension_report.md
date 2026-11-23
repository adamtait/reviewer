# Goldfish Review Report: Stage 6, Comprehension Test

**Target:** `docs/designs/review-workspace.md` (FeatureRange lines 1–689, DetailedRange lines 690–1221)

**Standard:** none. The test is whether a reader who knows software but not this project can understand the proposal from this document alone. The reviewer opened no other file.

## Reviewer Status

| Reviewer | Status | Findings |
| --- | --- | --- |
| `egm-comprehension` | completed | 0 Blocker, 9 Major, 3 Nit |

**Verdict:** no Blockers. Every section can be paraphrased, and the document is mostly self-contained. The Majors are places where two parts of the document disagree, or where a passage depends on knowledge of the existing CLI, the concepts brief or an ADR that the document does not supply.

Compare the two paraphrases below with what you intended. A wrong paraphrase is itself a finding.

## What this proposes

A hosted web application, the "review workspace", used beside GitHub's pull request page by a 10-person team whose pull requests are mostly written by coding agents. It adds go-to-definition, find-references and text search at the pull request's commit, an AI assistant that answers questions by calling read-only tools, a generated "change brief" (why, how, what it touches, what depends on it) and an AI review. Commenting, approving and merging stay on GitHub.

In each repository's CI job, the existing `reviewer` CLI builds a TypeScript index with `scip-typescript`. It uploads that index, a git pack of the source and the pull request's details under a GitHub-signed token. A Go service on Cloud Run (Postgres plus a bucket) verifies the pack by recomputing git hashes, stores each file once, scans files for secrets, and sends all model calls through one gateway. It checks every read against GitHub with the viewer's own credential and never runs pull request code. Delivery is fifteen pull requests (PR-47 to PR-61) and eight new ADRs.

## How the existing system works, per this document

There is no section on the existing system, so this is pieced together from scattered remarks. `reviewer` is a stateless Go CLI in this repository. It runs in each repository's GitHub Actions workflow and posts lint, type and convention findings on changed lines as pull request comments; GitHub comments are its only store. It has an optional "model lane", off by default, that reviews only the diff using a review prompt, a second pass that tries to disprove each finding, and "caps".

`reviewer init` installs the workflow, `.review/config.yaml` and an agent skill file into a repository. There is also a `gitleaks`-based secrets check, a `reviewer metrics` command, a "plugin" that builds a compiler program on each run, and two release artifacts. The reviewer could not work out what the plugin is, what the two artifacts are, or what the caps limit.

## Prioritized Findings (Blockers -> Major -> Nits)

### C-1. The state between a push and the upload is described two contradictory ways - Severity: Major

* **Affected Scope:** lines 48, 118–122, 225–234, 276, 1003, 1200 (both ranges)
* **Discrepancy Description**: CUJ 4 says that while the CI job is still running, the new commit's diff is shown and text search works. The search goal says search "works while the navigation index is still being built". In the plan, the source pack and the index travel in one `publish` upload, which runs after the index step, and line 276 says diff, search and navigation all become ready together when that step returns. Before the upload the service holds no source for the new commit, and PR-51's test expects "waiting for CI".
* **Rationale**: There is no state where source is present and the index is "still being built". The only source-without-index case is a failed index step, which is "unavailable" and not "being prepared". A reader cannot say what the reviewer sees in CUJ 4.

### C-2. The pull request description has no place in the "nothing unscanned" rule - Severity: Major

* **Affected Scope:** lines 54, 195, 328–334, 353, 363, 852–863, 1126
* **Discrepancy Description**: The gateway's request parts are instructions, typed user text, and excerpts identified by file hash, and scan results are looked up per file hash. The pull request title, description and intent record go into the brief, the review and the "Read brief" tool, but they are not files in the snapshot. The document does not say which part type they are or how they are scanned.
* **Rationale**: The goal at line 54 says the gateway "scans the payload", but the plan describes a lookup of stored per-file results. These are different mechanisms. Line 334 says typed text is "checked", and line 1089 says it is refused. The FeatureRange does not say which.

### C-3. The existing CLI is used as a set of building blocks that are never introduced - Severity: Major

* **Affected Scope:** lines 36, 238, 257, 260–261, 366, 408, 541–551, 1041, 1189
* **Discrepancy Description**: "The existing plugin" in Alternatives O and P "already builds a compiler program each run", but what it is a plugin of is never said, and two alternatives for decision 3 rest on it. "Model lane" is never tied to `internal/laneb`. "The rule in `internal/gate`" is not stated. Line 260 says "category caps" and line 366 says "confidence caps (ADR-0022)", with no way to tell whether these are one thing. "Acceptance figures from `reviewer metrics` as the baseline" is undefined and does not match the baseline at line 408, which comes from GitHub history. `reviewer init` and "the agent skill" first appear at line 238 without introduction.
* **Rationale**: Line 675 defers to `docs/architecture.md`. A short "existing CLI" section would close all of these.

### C-4. "Base commit" has two possible meanings, and the base's index has no stated source - Severity: Major

* **Affected Scope:** lines 225, 229–234, 301, 374, 770, 780, 806
* **Discrepancy Description**: The document uses "base commit", "base commits", "merge base" and "default-branch head" without saying which one the diff and the pull request record use. Line 374 says a lookup from a removed line "uses the base commit". The index is built from the head checkout, and a merge-base snapshot uploaded by a pull request job appears to carry no index.
* **Rationale**: Removed-line navigation seems to work only if that commit was indexed by an earlier default-branch run, and this is not stated. The reason default-branch commits are uploaded at all is never given. Pull requests that target a non-default branch are not addressed.

### C-5. A reader cannot tell which decisions are settled, who settled them, or on what record - Severity: Major

* **Affected Scope:** lines 3–4, 22, 170–183, 197, 266, 311, 445, 477, 503, 537, 557, 577, 605, 611, 613, 637
* **Discrepancy Description**: Line 183 says rows 6 to 10 "are proposals" and the status is "Proposed", but the alternatives for rows 8 to 10 say "approved with the Technical Plan". The document separates "Author" from "Proposed here" and "during drafting", though the author is the only named person. The drafter is never identified, and "the author's delegated judgement" is unexplained. "The finding", "one reviewer", "the concept review" and "the seven-lens review" point to a process recorded only in `concepts_brief.md`.
* **Rationale**: Three passages depend on that file: line 197 says row 8 differs from the concepts brief but explains only row 9, line 266 cites the brief as the source of the one-outbound-door requirement, and line 311 quotes "a local-files form" with no source. Several rejection reasons restate the choice and give no reason (lines 459, 471, 511, 517, 525, 531), which line 445 concedes.

### C-6. CUJ 7 does not match either of the gateway's two secret rules - Severity: Major

* **Affected Scope:** lines 136–140, 330–332, 859–860, 1089
* **Discrepancy Description**: CUJ 7 says the request is not sent and AI help is off "for that content". The gateway says a secret in a changed file turns AI off for the whole commit, and a secret in an unchanged file leaves that file out, which implies the request is still sent.
* **Rationale**: The reason for treating the two cases differently is not given. It is not said whether "AI features are off" also covers the brief and the review, or what the view then shows. The goal at line 50 says every view shows a brief or names the missing inputs.

### C-7. The later-stage constraints do not line up with the later stages - Severity: Major

* **Affected Scope:** lines 20, 67–74, 84, 419–424, 680–686
* **Discrepancy Description**: The Overview promises the constraints of three later stages: learnings and risk labels, commenting, and unread merges. The constraints section covers commenting, learnings, risk labels and "a shared service", and the last is listed under "Not planned". There is no entry for unread merges, for "planning, deployment and landed impact", or for the branch-without-pull-request view.
* **Rationale**: "Learnings", "written risk list", "landed impact" and "send to agent" are named but not defined, so the claim at line 422 that learnings "arrive through the same path" as guidance cannot be checked.

### C-8. The change brief's inputs are under-described - Severity: Major

* **Affected Scope:** lines 50, 83, 240, 363–365, 722–759, 1122, 1140–1142
* **Discrepancy Description**: "Guidance documents named in config" does not say which config or what a guidance document is, and neither config schema under Shared definitions has such a field. Line 50 says "plans and ADRs in the repository", and line 363 says only those "the pull request adds or changes". How a file is recognised as a plan or ADR is not said. The intent record's "fixed fenced form" is not given, although Shared definitions claims to fix what individual pull requests would otherwise decide.
* **Rationale**: The purpose of the intent record's "session identifier" is unclear, given that transcripts are not stored and "nothing links a session to a pull request". Line 83 excludes "Codex or other agents", but the supported coding agent is never named.

### C-9. ADRs and CLI conventions are cited by name without their content - Severity: Major

* **Affected Scope:** lines 434, 439, 469, 665, 692, 905, 912, 953, 962, 1041, 1072, 1176, 1183 (mainly DetailedRange)
* **Discrepancy Description**: ADR-0018 is "extended" and never described. ADR-0026's "two artifacts" are never named. Never explained: "public-history rule", "the history check", "the existing rule for missing tools", "no report means no scan", "the `required` job's list", "subscription paths", and "the existing plan's PR-46". ADR-0009 and ADR-0028 are only inferable from context.
* **Rationale**: Line 428 says each new ADR "supersedes or extends an old one", yet two rows are "New". What "extends" does under a whole-record supersession rule is not said.

### C-10. Goals, their checks and the verification table do not line up - Severity: Nit

* **Affected Scope:** lines 44, 46, 56–59, 381–382, 667, 1191–1209
* **Discrepancy Description**: Line 44 says each goal names its check, but the phone, hotkeys, telemetry and existing-installs goals have none. Line 1191 says every goal has a named test in the table, but two rows are not goals and six goals have no row: open any pull request, search, ask the AI, see the reason, AI review, and telemetry. The "stated delay" at line 46 is never stated and is not under Open Questions.
* **Rationale**: "By link from GitHub" has no mechanism: nothing in the plan puts a workspace link on GitHub, and line 667 says the service writes nothing there. "Tap-to-navigate is included if the touch design holds" reads as conditional, but the action table and PR-52 treat it as committed.

### C-11. "Compiler-resolved" promises more than the trust rule allows - Severity: Nit

* **Affected Scope:** lines 47, 193, 350, 1054, 1199
* **Discrepancy Description**: The goal says a definition is shown "only when the compiler resolved it". The trust table says the index cannot be verified, and that a result is accepted when the name at the target matches.
* **Rationale**: The label therefore means "the uploaded index says so and the name matches". The goal title "without wrong answers" and line 193's "a false index can mislead" should be reconciled. What "the name matches the symbol" compares is not said.

### C-12. The component diagram and the "stateless" claim disagree with the text - Severity: Nit

* **Affected Scope:** lines 207, 247, 254, 276–277, 322, 402, 985
* **Discrepancy Description**: The diagram shows `jobs` doing "scan, index, brief, review". The text says scan and index run inside the upload request and only brief and review are jobs.
* **Rationale**: The service "holds no state between requests", but the access check caches answers "for a few minutes". Where that cache lives is not said.
