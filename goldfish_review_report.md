# Goldfish Annotated Review Report

**Status:** resolved. The fixes marked "applied" are in `docs/designs/review-workspace.md`. Items marked "needs the author" are not settled.

**Target:** `docs/designs/review-workspace.md` (full file)

**Input:** the 29 action items in [`consolidated_review_report.md`](./consolidated_review_report.md), which trace back to 91 findings across five review stages.

## Rulings

The author ruled on four points on 2026-11-23 and left the method to the drafter's judgement on each. What was chosen, and why:

| Point | Author's ruling | What the revised design does |
| --- | --- | --- |
| How source reaches the service | Reading from git after fetching seems simplest; use judgement if another solution is better | The service fetches each commit from GitHub with `git`, using the viewer's own credential, the first time someone opens it. It stores the files in Postgres and lets `git` compute the diff. A git repository kept on disk was not used as the store, because the hosting decision is a stateless container. |
| Where the secrets scan runs | Use judgement | In the model gateway, at the moment of sending. Everything about to be sent is scanned, including descriptions and typed text. |
| When the brief and review are generated | First open seems reasonable, as long as further reviews can be triggered | On first open of a commit, stored for later readers, and generated again whenever a reader asks. |
| The events workflow and the second sign-in route | Use judgement | The events workflow is dropped. One sign-in route is built, the personal token, behind an interface that leaves room for a GitHub App. |

## Changes that go beyond the four rulings

These follow from the rulings or from review findings, and each changes something approved earlier. The author can overrule any of them.

| Change | Earlier state | Reason |
| --- | --- | --- |
| No upload from CI at all | The CI job pushed a git pack, the index and pull request details | Source now comes from GitHub. The index is saved as a workflow artifact and downloaded by the service. This removes the upload address, the signed-token check, the pack verifier and the new workflow permission. |
| The index is built in its own workflow, at the head commit | Built inside the existing review job | The review job checks out a temporary merge of the pull request, so the index would describe different files from the ones shown (finding D-3). A separate workflow file also means existing installs gain it without overwriting anything. |
| No storage bucket | Decision 4 was "Cloud Run, Postgres, bucket" | The bucket's only use was keeping raw CI uploads. There are none. |
| Tokens encrypted with a key from the environment | A cloud key service | The key service's client library was the largest part of the new dependency graph, for about ten tokens. The cost is that the key is in the process's memory. |
| The GitHub App route is not built in the first version | Decision 1 was to offer both routes | One route is enough to start, and the App may not be permitted. The interface is kept. |
| No job queue or scheduler | A job table run once a minute | With generation on first open, the only background work is a retention sweep, which runs as a scheduled command. |
| Ten pull requests | Fifteen | The ingest, upload, events and App pull requests are gone. |
| Two parts to the AI review | One review reusing the CLI's code | The CLI's code keeps only findings on changed lines, so it cannot review a plan (finding Z-3). Notes on the plan are a separate pass, labelled as unverified. |

## Disposition of each action item

### ACT-01. Answer the questions that can void the design first - Severity: Blocker

* ***AI Suggestion:*** PROPOSE FIX, applied; the answers themselves need the author.
* ***Justification:*** The finding is valid. File-level detail was written on unconfirmed foundations.
* ***Edit:*** "Entry conditions" E1 to E4 under Open Questions, and a gate at the top of the implementation plan. The upload-address question no longer exists. Evaluating GitHub's own features and a browser-hosted VS Code is E4.

### ACT-02. How source reaches the service - Severity: Blocker

* ***AI Suggestion:*** PROPOSE FIX, applied per the author's ruling.
* ***Justification:*** Fetching as the viewer removes the base-commit gap (D-1), the journey that could not happen (D-2, C-1), the unverifiable incremental pack (IR-3) and the unnoticed lost upload (I-4).
* ***Edit:*** Decision 2, the trust rule, "Opening a pull request", and PR-49.

### ACT-03. Make "nothing unscanned reaches a model" true - Severity: Blocker

* ***AI Suggestion:*** PROPOSE FIX, applied per the author's ruling.
* ***Justification:*** The stored-result design could not cover descriptions or typed text and had no repair path.
* ***Edit:*** Goal 9 is scoped to calls the service makes. "The model gateway" lists the six steps at send time. One rule applies to every file.

### ACT-04. When the brief and review are generated - Severity: Major

* ***AI Suggestion:*** PROPOSE FIX, applied per the author's ruling.
* ***Edit:*** Decision 7, CUJ 1 and CUJ 11, and a token estimate with its assumptions under Risks and Limits.

### ACT-05. The events workflow, and one identity route or two - Severity: Major

* ***AI Suggestion:*** PROPOSE FIX, applied per the author's ruling.
* ***Edit:*** Decision 1 and the alternatives under decisions 1 and 2.

### ACT-06. The bucket, the key service and the dependency inventory - Severity: Major

* ***AI Suggestion:*** PROPOSE FIX, applied; needs the author's confirmation because it changes decision 4.
* ***Justification:*** Both existed for needs the revised design does not have.
* ***Edit:*** Decision 4, the Dependencies table, and the changes to `tools/checklicenses` in PR-50. The SCIP bindings' import path and licence move into entry condition E2.

### ACT-07. Telemetry scope for the first version - Severity: Major

* ***AI Suggestion:*** ESCALATE.
* ***Justification:*** The reviewer argues that ten users need only event recording. The author decided earlier that any user can see and export telemetry and that every user receives notices. That decision is kept. One part was fixed: the history import had no credential, so it is now started by a user and uses their own.
* ***For the author:*** keep the measures view, export and status page in the first version, or cut PR-55 back to recording events.

### ACT-08. What the AI review must find - Severity: Major

* ***AI Suggestion:*** PROPOSE FIX, applied.
* ***Edit:*** Goal 6 and "The change brief and the AI review". One adapter set in the gateway. `internal/model` and `internal/laneb` are not changed.

### ACT-09. An outcome measure and a decision at each feedback point - Severity: Major

* ***AI Suggestion:*** PROPOSE FIX, applied; the thresholds need the author.
* ***Edit:*** "Feedback points" and "Outcome measure" in the implementation plan. The numbers are left as an open question.

### ACT-10. Which commit is indexed, and confirming the base - Severity: Blocker

* ***AI Suggestion:*** PROPOSE FIX, applied.
* ***Edit:*** The index workflow checks out the head commit. The merge base comes from GitHub. A definition in a dependency is reported as outside the repository.

### ACT-11. The journey that could not happen - Severity: Blocker

* ***AI Suggestion:*** PROPOSE FIX, applied.
* ***Edit:*** CUJ 4 is now true: source comes from GitHub, so the diff and search work while the index job runs.

### ACT-12. Rejection reasons in Alternatives Considered - Severity: Blocker

* ***AI Suggestion:*** PROPOSE FIX, applied.
* ***Edit:*** Every entry now gives a trade-off and no entry names a person. The section is grouped by the nine decisions, and each decision has entries. The record of who decided what is this file and the two it links to.

### ACT-13. Plan steps that break the existing build - Severity: Blocker

* ***AI Suggestion:*** PROPOSE FIX, applied.
* ***Edit:*** A separate variable gates database tests. The web build output sits beside the package that embeds it, with a tracked placeholder. The Dockerfile gains its web stage in the pull request that adds the web application. One builder makes the image. The tool installer takes tool names, so the image holds `gitleaks` only.

### ACT-14. An upgrade route for existing installs - Severity: Blocker

* ***AI Suggestion:*** PROPOSE FIX, applied.
* ***Edit:*** The index workflow is a new file, so `reviewer init` adds it and leaves existing files alone. The config file and the review workflow are not changed. Installer tests extend the existing assertions; no golden files are claimed. Refreshing an existing skill file is listed as an open question.

### ACT-15. The gateway interface - Severity: Blocker

* ***AI Suggestion:*** PROPOSE FIX, applied.
* ***Edit:*** Turn, tool-call and tool-result parts, a `Stopped` event carrying the outcome, and `AsProvider` for the CLI's review code.

### ACT-16. Goals, checks and the verification table - Severity: Blocker

* ***AI Suggestion:*** PROPOSE FIX, applied.
* ***Edit:*** Fourteen numbered goals, each with a check, and a table with one row per goal stating what its test shows.

### ACT-17. Binding uploads, and the new workflow permission - Severity: Major

* ***AI Suggestion:*** PROPOSE FIX, applied by removal.
* ***Justification:*** There is no upload and no new permission. The index is accepted only from a workflow run GitHub reports as being for the commit on screen.

### ACT-18. The scanner invocation - Severity: Major

* ***AI Suggestion:*** PROPOSE FIX, applied.
* ***Edit:*** An empty environment and an explicit rule file for the scanner, and an empty environment for `git`. A second deployment role is not added: the service no longer parses packs built by CI.

### ACT-19. The prompt-injection statement - Severity: Major

* ***AI Suggestion:*** PROPOSE FIX, applied.
* ***Edit:*** The row under Risks and Limits, the split of the brief into "the author says" and "the code shows", and the restrictions on rendered output.

### ACT-20. A Risks and Limits section - Severity: Major

* ***AI Suggestion:*** PROPOSE FIX, applied.
* ***Edit:*** A new section with eleven rows. Figures that are estimates say so.

### ACT-21. Introduce the existing CLI and define terms - Severity: Major

* ***AI Suggestion:*** PROPOSE FIX, applied.
* ***Edit:*** "Background: the existing CLI". One table of later stages. "Gateway" and "service" are used throughout.

### ACT-22. Calibrate claims - Severity: Major

* ***AI Suggestion:*** PROPOSE FIX, applied.
* ***Edit:*** Goal 2 now says results are labelled, not that they are never wrong. Goal 8 says the service never executes pull request code. Goal 7 states the cache period. Statements about GitHub's features are attributed to the requester.

### ACT-23. Level of detail in Shared definitions - Severity: Major

* ***AI Suggestion:*** PROPOSE FIX, applied.
* ***Justification:*** The reviewers disagreed. The revision keeps the interfaces more than one pull request depends on, fixes keys and value sets, and puts request and response shapes in one schema file that both the service's tests and the web application's fake API are checked against.

### ACT-24. Define the change brief - Severity: Major

* ***AI Suggestion:*** PROPOSE FIX, applied.
* ***Edit:*** The intent record format, the rule for which files count as plans, the cap on dependent code, and the brief and the skill change in the same pull request.

### ACT-25. Missing identity mechanisms - Severity: Major

* ***AI Suggestion:*** PROPOSE FIX, applied.
* ***Edit:*** The requirement to reject a token with write permissions is dropped, because no way to read a token's permissions is known. The risk is recorded. The real `ghclient` additions are listed, and its interface is left unchanged so existing test fakes compile.

### ACT-26. Complete the file lists - Severity: Major

* ***AI Suggestion:*** PROPOSE FIX, applied.
* ***Edit:*** A note names the files every route-adding pull request touches, and each table lists the web files it changes.

### ACT-27. Credential patterns - Severity: Major

* ***AI Suggestion:*** PROPOSE FIX, applied.
* ***Edit:*** PR-48 adds GitHub's token prefixes to `internal/model/redact.go` and exports a detection function.

### ACT-28. Clearing a secrets false positive - Severity: Major

* ***AI Suggestion:*** PROPOSE FIX, applied.
* ***Edit:*** One rule for every file. An operator-kept allowlist in the service's configuration. A base-branch allowlist is deferred until a false positive is seen.

### ACT-29. Three additions declined for the first version - Severity: Nit

* ***AI Suggestion:*** DECLINE.
* ***Justification:*** Resumable answers, routing the CLI's model lane through the gateway, and tree-object storage serve needs the first version does not have. Two are listed under Future Improvements.

## Not upheld

* **ADR numbering order (raw finding P-6).** The reviewer had not read `tools/checkadrs`. Its own comment says the log may have gaps at any moment.

## Points that still need the author

1. **Entry conditions E1 to E4.** Credential approval, the indexer trial, the fetch-and-artifact trial, and the check of existing tools.
2. **Confirm or overrule** the changes listed under "Changes that go beyond the four rulings".
3. **Telemetry scope** (ACT-07).
4. **Thresholds** for the three feedback points.
5. **Retention periods** for stored commits, conversations and usage events.
6. **Branch views.** The author chose pull requests only, because a branch view cost a CI run per push. That cost is gone. The choice is kept, and branch views are listed under Future Improvements.

## Counts

* Applied: 27 action items.
* Escalated: 1 (ACT-07), plus the open points above.
* Declined: 1 (ACT-29).
* Contested: none among the action items. One raw finding was not upheld.

The revised design has not been reviewed. It changes the architecture enough that the earlier review's verdict no longer describes it.
