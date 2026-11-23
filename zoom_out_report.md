# Goldfish Review Report: Stage 5, Initial Zoom Out (round 3)

**Target:** `docs/designs/review-workspace.md` (full file, lines 1–1129), as revised after the second review round.

**Standard:** `zoom-out-standard.md`

**How it was run:** two reviewers with no history of the document, each able to research on the web and to read the repository. Neither read any earlier review report. Neither changed a file.

## Reviewer Status

| Reviewer | Status | Findings |
| --- | --- | --- |
| `egm-zoom-out` | completed | 0 Blocker, 4 Major, 5 Nit |
| `egm-inventive-catalyst` | completed | 0 Blocker, 6 Major, 4 Nit |

**Verdict.** Context sufficiency passes. Neither reviewer raises a Blocker. The design does not yet meet the standard: an existing tool for each half of the design was either not tried or not weighed, the database arrives two pull requests before anything needs it, and the Go version the design treats as a fixed constraint is out of support.

Findings are prefixed `Z-` for the zoom-out reviewer and `I-` for the inventive catalyst.

## Prioritized Findings (Blockers -> Major -> Nits)

### Z-1. The existing navigation tool was not tried - Severity: Major

* **Affected Scope:** lines 57, 61, 154, 517, 1115
* **Discrepancy Description**: E4 was closed without trying GitHub's web editor on one of the team's pull requests. The design's own Alternatives row says the editor has offered project-wide go-to-definition and find-references for TypeScript since VS Code 1.93 ([release notes](https://code.visualstudio.com/updates/v1_93)). The reasons given for rejecting it do not settle the navigation gap: "no permitted way to ask" is about the assistant, the Safari limit matters mainly on phones, where the design already treats navigation as optional, and "no dependency install" is what a trial would measure. What remains is "the team cannot extend it", which the design itself says is not evidence.
* **Rationale & Recommendation**: Navigation accounts for the index workflow, a second dependency install on every push, the hand-written decoder and all of PR-52. Re-open E4 as a real entry condition: resolve E2's 50 hand-picked symbols in the web editor in a desktop browser, on the same repository, before PR-47. If it meets E2's own bar, take navigation out of the first version and start with asking. If it does not, the count becomes the evidence the Alternatives row lacks.

### I-5. The web editor row overstates one reason, and Goal 2 has no reference point - Severity: Major

* **Affected Scope:** lines 95, 152, 154, 517
* **Discrepancy Description**: The row rests partly on "no compute, so it cannot run a dependency install". The release notes it cites say TypeScript gets typings for packages listed in `package.json`, and project-wide navigation, in Chrome, Edge and Firefox. So the install objection covers only generated types. E4 is closed without the short trial, and Goal 2's target has nothing to compare against.
* **Rationale & Recommendation**: Keep the decision if the design owner wishes, correct the row, and reuse E2's sample: resolve the same 50 symbols in the web editor on the same pull request and record the score beside the index's. Cost: about half an hour by hand, and a result that may show the index earns its CI cost mainly on phones and for the assistant's lookup tools.

### Z-2. The team's existing agents are not weighed as a way to ask - Severity: Major

* **Affected Scope:** lines 59, 84, 516–527
* **Discrepancy Description**: The Problem says "there is no permitted way to ask", yet Background says the team already runs Antigravity and Claude Code, from the same two vendors the service would call. The Alternatives table weighs neither asking one of those agents on a checkout of the pull request branch, nor invoking one from a pull request comment through an Actions workflow ([documentation](https://code.claude.com/docs/en/github-actions)).
* **Rationale & Recommendation**: The asking half is PR-53, PR-54 and the database. State gap 2 as what is true (no way to ask from the pull request view without a checkout), add both rows with their real costs, and decide with them in view. The costs the reviewer can see: answers are comments everyone reads, nothing scans for secrets before sending, and the Action defaults to a GitHub App, which the concept brief says is likely not allowed.

### Z-3. Postgres rests on two needs with cheaper answers - Severity: Major

* **Affected Scope:** lines 249, 412, 432, 569, 764, 829–838, 984–994
* **Discrepancy Description**: The design justifies Postgres by a monthly budget sum that survives a restart and a conversation that follows the user between devices. Both providers already offer a monthly spend limit per workspace or project, in money, and the configuration already gives each repository its own key ([rate limits](https://platform.claude.com/docs/en/api/rate-limits), [billing](https://aistudio.google.com/docs/billing)). Cross-device conversations appear in no goal. The designed budget adds input and output tokens together although output costs five times as much, so it tracks the money risk poorly.
* **Rationale & Recommendation**: The database brings a driver, a migration runner, a test-database helper, a CI job, a monthly charge, a restore drill and an open retention question. Use the provider-side limit and delete `monthlyTokenBudget` and `budget_blocked`. Write model-call records and navigation reports as log lines. Then decide whether stored conversations alone justify Postgres in the first version; if they do, add it in PR-53.

### I-2. The database arrives in PR-48 and nothing uses it until PR-52 - Severity: Major

* **Affected Scope:** lines 249, 472, 569, 702, 722, 725, 887, 984–994
* **Discrepancy Description**: Managed Postgres, migrations, a database CI job and a new Go dependency all arrive in PR-48, and the health address means "the database answers". The first feedback week needs the database for one insert-only table, while its other five measures come from request logs. If that week misses its thresholds work stops, so the database would be built, hosted and backed up for a milestone that never needed it. The health rule also ties diff, navigation and search to the database being up.
* **Rationale & Recommendation**: Move the store, migrations, the database CI job and the driver to PR-53. For the navigation week, write a "this was wrong" report as one structured log line. Let the health address report the process. Cost: PR-53 grows, reports are readable only by the operator and only for the log retention period, and the free-text note must be dropped or capped to keep code out of logs.

### Z-4. File-level plan is fixed before the trials that can void it - Severity: Major

* **Affected Scope:** lines 149–153, 158, 695, 729–1090
* **Discrepancy Description**: About a third of the document is file-level plan for PR-48 to PR-54, written on the stated assumption that E1, E2 and E3 all pass. Each can remove or replan a part, their pass values are the drafter's proposals, and the feedback thresholds are unset. The concept brief said the index trial costs almost nothing and can run before design work finishes.
* **Rationale & Recommendation**: Run E2 and E3 now (one hand-written workflow file, one token), put E1 to the organisation owner, and write the results in before fixing file-level detail beyond PR-47. Record the results without repository names, because this repository is meant to be public.

### I-1. The diff waits for two whole archives - Severity: Major

* **Affected Scope:** lines 153, 337, 351, 546, 621, 624
* **Discrepancy Description**: The diff is not drawn until two whole-commit archives are fetched and unpacked, although it needs only the changed files. The cache dies with the container, and Cloud Run keeps an idle one at most 15 minutes ([container contract](https://docs.cloud.google.com/run/docs/container-contract)), so the first open of each new head commit pays this wait; E3's pass bar allows 60 seconds per archive. The design treats "whole archive" and "one file at a time" as either/or, yet the one-file path must be designed anyway as the E3 fallback. The same choice lets a pull request hide its own changed file from the diff with `export-ignore`.
* **Rationale & Recommendation**: Split by purpose. On a cache miss, the file route reads that one file through GitHub's contents call with the user's token, so the diff appears as fast as the files on screen. The tree route still loads the archive for search and navigation, and the merge-base archive can wait for the first lookup from a removed line. Cost: two GitHub calls per changed file viewed, two read paths that must apply the same text rules, and rare byte differences where `.gitattributes` rewrites content.

### I-3. Go 1.24 is treated as fixed, and it is out of support - Severity: Major

* **Affected Scope:** lines 561, 571, 947, 989
* **Discrepancy Description**: "This repository declares Go 1.24" is a stated reason for rejecting the SCIP Go library and `pgx`, and the image is built on the Go 1.24 image. Go 1.24 left support when 1.26 was released on 2026-02-10, and 1.25 when 1.27 was released on 2026-08-19 ([release history](https://go.dev/doc/devel/release)). The service would parse tar, zip and protobuf input that a pull request author influences, and seal user tokens, with a standard library that no longer gets security fixes.
* **Rationale & Recommendation**: In PR-48, build and test on a supported release, and raise the `go` line as far as dependencies need. Then restate the two alternatives on the reasons that survive: the SCIP bindings still require an MPL-2.0 module, and `pgx` still adds more modules to inventory than `lib/pq`. Cost: a higher minimum Go for anyone building the CLI from source, a CI matrix change, and a toolchain bump about every six months.

### I-4. The scanner's hit rate under the new settings is unknown until the feedback week - Severity: Major

* **Affected Scope:** lines 153, 399, 414–423, 600, 635
* **Discrepancy Description**: The gateway withholds a whole file on any scanner hit, and deliberately turns off the scanner's inline exemption, ignore file and path exemptions and turns decoding on. Each raises the hit rate above what the same scanner reports on these repositories today, so nothing seen so far predicts it. The design records the rate as unknown with no mitigation, and would first learn it in the assistant's feedback week, where a high rate makes a failed week unreadable.
* **Rationale & Recommendation**: E3 already fetches and unpacks the largest repository. Add one run of the pinned scanner with the planned flags over that tree, and record how many files, and which, would be withheld. Decide before PR-53 whether "none in the first version" stands.

### I-6. What happens to an answer when the connection drops is not stated - Severity: Major

* **Affected Scope:** lines 103, 250, 347, 688, 906, 1087
* **Discrepancy Description**: Decision 5 ties all work to the request, and that includes model answers. If the stream drops, "the browser reads the conversation back", but nothing says whether the server finishes and stores the answer or abandons it. The tool sends users to another tab to comment, and phones suspend background pages, so drops mid-answer are routine in the use Goal 10 names. Goal 10's check, a narrow viewport against a fake API, cannot show this.
* **Rationale & Recommendation**: State the rule: the handler finishes the model turn in flight, under the existing limits, whether or not the client is still connected, and stores it. If the platform withdraws CPU after a disconnect, store an explicit "interrupted" message. Cost: tokens spent on answers nobody reads, and one stated exception to "work ends with the request".

### Nits

| ID | Affected scope | Finding and recommendation |
| --- | --- | --- |
| Z-5 | 468, 816–825, 1009, 1072 | The rule that only `gateway` may import `internal/model` fails on the design's own interfaces: `Gateway.Ask` takes `func(model.Event)` and a `Call` made of that package's types, so `assistant` must import it, and PR-49 puts GitHub token prefixes into `internal/model/redact.go`. The key-possession rule already does the work. Drop the import ban, and put GitHub token redaction in `ghclient`, which has its own redaction helper. |
| Z-6 | 842, 1031–1032, 1038, 1042 | The type generator departs from this repository's stated pattern. `plugins/typescript/src/protocol.ts` says a hand-written mirror is deliberate because "a generator plus its drift check is more moving parts than the drift it would prevent", and the Makefile drives the real counterpart in a conformance test. Follow the pattern, or say why about twenty types change the reasoning. |
| Z-7 | 325, 364, 849–850, 987, 1018, 1020 | The service parses GitHub's patches into line ranges, but the browser draws the diff from two file texts, and nothing names a consumer of `ranges`. If none exists, drop `ranges`, `noPatch` and the `FromPatches` dependency. The service then imports nothing from `internal/diff`, and the special clause in the Goal 7 test goes too. |
| Z-8 | 22, 106, 222–226, 313–315, 972, 1114 | Goal 13 is outside the stated first version. The workspace takes no part in it, neither feedback week measures it, and its only test asserts wording in a template. It rides in PR-47 with the index workflow. Move it to a separate small CLI change and keep only the intent-record section. |
| Z-9 | 73, 494–504, 725, 994, 998, 1043, 1078 | The stop path is not described. By the first feedback week the repository has two superseded ADRs, two new required CI jobs, a new Go dependency, a Dockerfile and a web toolchain. Add a paragraph on what stopping means, and consider landing the superseding ADRs as `Proposed` until the feedback weeks pass. |
| I-7 | 152, 373 | E2 counts how many of 50 symbols the index resolves. Users see only targets that also pass the name check, which can discard a correct target. Score the 50 through the name check as well. |
| I-8 | 20, 399, 637, 794, 834, 864 | Stored conversations keep the text of every excerpt, which creates the "conversations hold code" risk. A commit never changes, so excerpts could be stored as references and filled in from the archive when a conversation is sent on. The risk shrinks but does not go away. |
| I-9 | 100, 987, 1072, 1108 | The gateway imports `internal/model`, whose `clisub.go` starts the `claude` and `codex` programs through `model.New`. "The service starts one program" rests on the gateway calling only `NewConverser`, which no test asserts. Add `model.New` to the functions the rule test forbids. |
| I-10 | 164, 431, 439, 620 | A spend limit the account sets for itself returns HTTP 400 with a message, not the 429 the gateway section describes ([rate limits](https://platform.claude.com/docs/en/api/rate-limits)). The outcome table has no row a 400 fits except "could not be reached". Name the form, map it to `quota_exhausted`, and add an outcome for "the provider rejected the request". |

## Checked and found to hold

* **Existing-code claims.** Background and the file tables match the source: `diff.FromPatches`, the installer's TypeScript condition, the "7 files" counts in three tests, PR-46 as the last plan entry, the `required` job, the licence denylist, the scanner pin, the 32 MB cap in `ghclient`, and metadata-only ADR edits.
* **Index facts.** `scip-typescript` 0.6.2 is the latest release, depends on TypeScript 6, needs Node 22 and declares UTF-16 positions. The field numbers under "Reading the index" match the schema at v0.10.0.
* **Rejected libraries.** The SCIP Go bindings declare Go 1.25 and require an MPL-2.0 module. `pgx` v5.9.0 declares Go 1.25, and `lib/pq` v1.12.3 declares Go 1.21 with no requirements.
* **Provider facts.** The two lowest tiers cap spend at $500 and $1,000. The price range holds, and the $300 to $1,200 estimate reproduces from its assumptions.
* **Platform behaviour.** No pull request runs during a merge conflict; pushes with the workflow token start no run; the list-runs call accepts a workflow file, head commit and event; Cloud Run's default timeout is 300 seconds.
* **The unverifiable index is a real bound.** Anything built by running a commit's own install is controlled by that commit, so bounding and labelling is the right response.

## Not verified

* How GitHub's web editor navigates on the team's repositories, and whether it has the features the 1.93 notes describe.
* The four-row scanner table. Neither reviewer had the scanner binary; both confirmed only that the flags exist.
* Whether Cloud Run keeps a handler running with CPU after the client disconnects.
* Which fine-grained permission the archive and artifact calls need. E3 tests it.
* What a Google project spend cap returns at the cap.
* The npm package counts and licences, the Mermaid and `lightningcss` licence claims, Cloud Run's file-locking limit, and the cited study.
