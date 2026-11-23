# Goldfish Review Report: Stage 8, Intermediate Zoom Out (round 3)

**Target:** `docs/designs/review-workspace.md` (full file, lines 1–1129)

**Standard:** `zoom-out-standard.md`

**What this pass did.** The document was not edited after Stage 5. This reviewer read this round's Stage 5, 6 and 7 reports and judged the fixes they ask for as a set, against the standard's preference for the fewest components. It did not read the Stage 9 report or any earlier round. It could research on the web and changed no file.

## Reviewer Status

| Reviewer | Status | Findings |
| --- | --- | --- |
| `egm-zoom-out` | completed | 1 Blocker, 8 Major, 1 Nit |

**Verdict.** The three reports hold 61 findings and ask for about 45 additions: roughly 17 mechanisms or settings, 6 states or outcomes, 9 tests and 12 gates or measurements. The reviewer judges about 28 of those avoidable by removing or narrowing something, and about 14 worth taking. Four decisions settle more than half of the findings: run the trials first, store nothing, move the skill sections out, and shrink the outcome table.

No finding below asks to re-open any of the six decisions the design owner recorded. E4 is closed but is not one of the six; Z2-3 gives the new evidence for re-opening it as a measurement.

## Prioritized Findings (Blockers -> Major -> Nits)

### Z2-1. Settle in this order; D-1 stands whatever else is decided - Severity: Blocker

* **Affected Scope:** lines 149–154, 337–360, 695, 729–1090, 1117–1123
* **Addresses:** Z-4, D-1, and the ordering of all others
* **Discrepancy Description**: The reports ask for about 45 additions, each written as if it stood alone. Five findings (Z-1, Z-3, Z-4, Z-8, I-4) change or delete text that about thirty others ask to extend. Applied in report order, the edits would add routes, states and tests to parts that a later edit removes. D-1 is the only Blocker in these three reports, and no other decision removes its source-cache half.
* **Rationale & Recommendation**:
  1. **Fix D-1 first and alone.** Say every cache entry and shared fetch is keyed by owner, repository and hash, and add the one cross-repository test.
  2. **Run the trials before touching the file-level plan.** E2, with the same 50 symbols counted in the web editor and scored through the name check. E3, with a cold open-to-diff time and one scanner pass over the unpacked tree. And E1. The results decide Z2-3, Z2-5 and Z2-7, and can make D-6, I-7, C-7, I-1, C-3 and most of D-2 and D-8 moot.
  3. **Put two scope questions to the design owner:** stored state (Z2-2) and the skill sections (Z2-4). A "no" to each makes I-2, C-1, I-6, I-8, S-7, D-5, Y-3, Y-4, S-6, C-8, C-9 and parts of S-3, S-4, Z-9 and Y-10 moot.
  4. **Run one live conversation with the first repository's provider** before rewriting the shared definitions for PR-53. That settles D-4, and after it C-2, C-4, I-10 and S-10.
  5. **The Go toolchain, the Goal 7 check and bookkeeping** are independent and follow. Wording findings go last, because about a third of the lines they cite will have changed or gone.

### Z2-2. Five additions around the database serve a requirement no goal states - Severity: Major

* **Affected Scope:** lines 143, 249, 412, 432, 447, 472, 499, 569, 637, 688, 764, 829–838, 884–887, 906, 984–994, 1085, 1123
* **Addresses:** Z-3, I-2, C-1, I-6, I-8, S-7, D-1 (conversation half), S-3 (operator row), Y-10, Z-9
* **Discrepancy Description**: The reports ask for a route to find a user's conversations, an after-disconnect rule with an "interrupted" message, excerpts stored as references, a conversation key and test, and a risk row for what the operator can read. Z-3 and I-2 ask the opposite: remove or delay the database. All five additions serve one requirement, a conversation that outlives the page, which is in no goal. The design already points the other way: the first version is measured from logs, and Future Improvements defers answers that survive a dropped connection.
* **Rationale & Recommendation**: The standard favours Z-3 over I-2. Decide first whether a conversation must outlive the page. If not, the first version has no database:
  * Hold the conversation in the page and send its turns with each question. The gateway already scans every turn at send.
  * Write model-call records and "this was wrong" reports as log lines.
  * Bound spend with the per-question limits and a provider-side monthly limit on a dedicated workspace or project per repository key. Anthropic documents per-workspace spend limits; Google documents a per-project monthly cap, marked experimental, with about ten minutes of lag.

  This removes four tables, the driver, migrations, the test-database helper, the Postgres CI service, one ADR and one supersession, `monthlyTokenBudget`, `budget_blocked`, two routes, the retention question and the restore drill. The cost to state: a reload or a second device starts a new conversation, a dropped stream loses the answer in progress, and a Google repository's cap is softer than the designed budget.

### Z2-3. Navigation: take the one trial three reports ask for, and drop a label instead of adding a matcher - Severity: Major

* **Affected Scope:** lines 61, 95, 152, 154, 371–375, 517, 859–862, 920, 1054
* **Addresses:** Z-1, I-5, D-7, D-6, I-7, C-7, S-5
* **Discrepancy Description**: Three reports ask for the same new entry condition: a count of E2's 50 symbols in GitHub's web editor. D-6 and I-7 each add a measurement, and D-6 adds a mechanism. The reviewer confirmed D-6's premise in the indexer's source: a file inside the project takes the name and version of the nearest `package.json`, so the "outside the repository" rule fires for the repository's own code ([source](https://github.com/sourcegraph/scip-typescript/blob/main/src/Packages.ts)). It also confirmed that the release notes the design cites describe typings for packages listed in `package.json`, which contradicts the "no dependency install" reason on which E4 was closed.
* **Rationale & Recommendation**: Take the web-editor count. It is the only requested gate that can delete a workflow, a decoder, an ADR and a pull request, and it reuses E2's sample. The concept brief lists this evaluation as still to do, so re-opening E4 as a measurement overrides nothing, and the design owner still decides what the number means. For D-6, drop the fourth label instead of adding the matcher: when the index has no definition, answer "not in the index" with the text matches. Let E2 score the 50 with and without the name check, and delete the check if on an honest index it only discards correct targets.

### Z2-4. Skill sections: move them out; do not harden them here - Severity: Major

* **Affected Scope:** lines 84, 98, 105–106, 222–226, 310–315, 667, 775–788, 972–973, 1020, 1114
* **Addresses:** Z-8, D-5, Y-3, Y-4, S-6, C-8, C-9, S-3 (credentials row), S-4 (journey 10)
* **Discrepancy Description**: Z-8 asks to move the "address review comments" section out. D-5 asks to harden it with bot-thread handling, a marker on agent replies, risk rows and a hand-run with both agents. Y-3 asks for a test of whether Claude Code loads the skill and possibly a second install location. The workspace takes no part in that journey and neither feedback week measures it. The intent-record section has the same shape, since the workspace only reads what an agent wrote.
* **Rationale & Recommendation**: The standard favours Z-8. D-5's evidence is the reason the section needs its own design, not a reason to grow this one. Move both skill sections, Goal 13 and journey 10 to a separate CLI change, where Y-3's loading test and C-8's refresh question belong. In this design keep only the reading side and narrow it: show the title and description as plain text, with no parser, since the block is readable as written. PR-47 then adds one workflow template, and Goal 12's check is true as worded. The concept brief put both sections in the first stage, so the design owner confirms the repackaging.

### Z2-5. First open: one setting and one measurement before a second read path - Severity: Major

* **Affected Scope:** lines 153, 337, 347–351, 356, 364, 577, 621–622, 655–660
* **Addresses:** I-1, D-2, Y-10
* **Discrepancy Description**: I-1 and D-2 both ask for a second read path: per-file reads, with archives loaded behind. D-2 adds a bound on fetches in flight, fixed instance settings with cost, and a time-to-diff target. The design already names per-file reading as the E3 fallback that replaces archives, so the requests would make both paths permanent before E3 has produced a number. D-2's second-instance case comes from a platform default, which is a setting.
* **Rationale & Recommendation**: Measure first: add a cold open-to-diff time to E3 and decide on that number. Set maximum instances to 1 and raise concurrency, which removes the second-instance case under ordinary load ([concurrency](https://docs.cloud.google.com/run/docs/about-concurrency), [maximum instances](https://docs.cloud.google.com/run/docs/configuring/max-instances)). If the cold open is too slow, a minimum of one instance is a setting with a monthly cost to state, and it is cheaper to own than two read paths that must agree on text rules. Count fetches in flight and the scanner's scratch files in the sizing rule instead of adding a limiter.

### Z2-6. Gateway: shrink the outcome table; take one live run - Severity: Major

* **Affected Scope:** lines 425–441, 447, 608, 619–620, 747–754, 790–811, 815–825, 896–906
* **Addresses:** D-4, I-10, C-2, C-4, D-9 (limit outcome), Y-6, S-10
* **Discrepancy Description**: Four findings ask to add outcomes to a table that already has nine. C-2 asks for another limit key, and D-4 for an opaque part in the turn model and a new entry condition. D-4 and I-10 are right on the facts: not every 429 carries a retry time, the tier cap is a 429 with a code in the body, and a self-set limit is a 400. The rule "no retry time means quota" would mislabel both.
* **Rationale & Recommendation**: The design owner requires one distinction, that quota is named as the cause. Shrink the table to seven: `ok`; `quota_exhausted`, only on a provider's explicit code or message; `limit_reached`, for rounds, question tokens or the answer's token limit; `refused`; `secret_blocked`; `scan_failed`; and one `provider_error` that passes through the redacted status, code and retry time. Define `questionTokens` as the total over all rounds of one question. Take D-4's live run as the one new gate, for the first repository's provider only: the turn model must carry provider content it does not read, and the run gives the real output figure for the estimate. For C-4, delete "retry" from the gateway section.

### Z2-7. Secret scan: one scanner run decides whether per-file withholding is needed at all - Severity: Major

* **Affected Scope:** lines 101, 399–423, 501, 599–600, 635–636, 1069–1070
* **Addresses:** D-8, C-3, I-4, Y-5
* **Discrepancy Description**: D-8 asks for a new mechanism (block a flagged literal value in every outgoing part), a definition of the service's rule file, a stopword test, a scan-time measurement and two checks. The two "missing" checks are in the gateway tests in the plan, which the critic's scope did not cover. The literal-value mechanism is the "redact and continue" approach ADR-0012 rejected, and for a value already sent in an earlier turn it would edit history the provider expects back unchanged. The CLI runs the scanner with its default rules and no rule file, so "the service's own rule file" is a new artifact.
* **Rationale & Recommendation**: Do I-4's run first, as one scanner pass with the planned flags over the tree E3 unpacks. It gives the hit count and an upper bound on scan time in one step. If hits are zero or near it, adopt ADR-0012's rule unchanged: any hit in what is about to be sent stops the send and names the file. Then delete the per-kind table, the `withheld` list, its event and its columns. If hits are material, keep per-file withholding and say in the new ADR that it departs from ADR-0012 and why. Do not add literal-value blocking or a hand-written rule file: use the pinned version's default rules, narrow "there is no allowlist" to "none that repository content can change", and list the built-in exemptions. C-3 needs one sentence: a selection is scanned as its whole file, because a multi-line key can straddle the selection.

### Z2-8. Goal 7: two checks instead of five, by narrowing the claim - Severity: Major

* **Affected Scope:** lines 100, 646, 849–850, 987, 1018–1020, 1068–1072, 1108
* **Addresses:** Y-2, I-9, C-5, Z-5, Z-7
* **Discrepancy Description**: Y-2 asks for a dependency-closure test and an image check, I-9 for another forbidden function and C-5 for an explanation, while Z-5 and Z-7 ask to delete a rule and an import. Applied together they leave five source rules guarding one sentence. The process-starting package is imported by three CLI packages the service binary would link, so no import rule over the service's own packages can show "the service starts one program".
* **Rationale & Recommendation**: Narrow the claim to what holds by construction: the image contains two executables, the service and the scanner, and no shell, and the service's own packages never import the process-starting package. Keep the existing import rule and add the image check. Drop the `git`-helper clause, the `internal/model` import ban, the extra forbidden function and the closure list. For Z-7, the author must name the consumer of `ranges`; if there is none, `FromPatches` and `internal/diff` leave the binary.

### Z2-9. Entry conditions and checks: close by deleting claims, not by adding rows - Severity: Major

* **Affected Scope:** lines 51, 92, 107, 149–154, 161, 655–660, 695, 989, 1115–1123
* **Addresses:** S-2, S-4, S-5, C-7, C-9, D-10, D-3, I-3
* **Discrepancy Description**: S-2 asks for two more E1 outcomes, S-4 for four checks, S-5 for four open questions with deciders and dates, C-7 for consequences for five recorded facts, and D-3 for a rule to keep Go current. Most exist because the document claims completeness or records a number nobody will act on. D-3 and I-3 are right on the facts: the CI matrix is pinned to Go 1.24 and 1.25 under a comment that says "the two most recent Go releases".
* **Rationale & Recommendation**: Close by deletion where possible.
  * **E1:** state two limits of the first version (one organisation's repositories; an outside collaborator cannot sign in) instead of designing branches.
  * **Checks and open questions:** remove Goal 14 from the goals, remove the two "(proposed)" targets that have no decider and the lookup-time measurement, and list the phone sitting once in the hand checks.
  * **E2:** make all three places say "before PR-47 starts", and cut the recorded facts that decide nothing.
  * **Go:** follow the convention already written down. Move the CI matrix and the image to the two supported releases in PR-48, leave the `go` line unless a dependency needs it, and add no new rule.

### Z2-10. Apply wording findings last; three have a removal smaller than the requested addition - Severity: Nit

* **Affected Scope:** lines 244–257, 463–464, 468–470, 478–481, 494–506, 842, 849–864, 1031–1042
* **Addresses:** S-1, S-7, S-8, S-9, S-10, Y-8, Y-9, Y-10, C-6, C-10, C-11, C-12, D-9, Z-5, Z-6, Z-9
* **Discrepancy Description**: Sixteen findings ask for wording, table or term fixes. After Z2-2 to Z2-4 the lines behind about a third of them are gone.
* **Rationale & Recommendation**: Apply these last. For Z-6, follow the repository's stated convention of a hand-written mirror and drop the generator, its test and its Makefile target, or say why twenty types change the reasoning. For D-9, show links in answers as text, which removes the click-through leak. For C-6, cut the change-brief outline to what is settled instead of adding Background. For S-1, fold the conversation interface into decision 6. For Z-9, the `Proposed` suggestion works only if the new records leave `Supersedes` empty until accepted, because the ADR tool checks the link both ways.

## Where the reports conflict

| Reports | Conflict | The standard favours |
| --- | --- | --- |
| Z-3 against I-2 | No database in the first version, or the same database two pull requests later | Z-3. I-2's timing applies only if a conversation must outlive the page |
| I-6, C-1, I-8 against Z-3 | Add a disconnect rule, a list route and reference-style storage, or drop the requirement they serve | Drop it |
| D-1 against Z-3 | Key and test stored conversations, or have none | D-1 for the source cache in every case; its conversation half is moot without storage |
| Z-3 against the concept's "budget limits are configurable" | A service-side token budget, or a provider-side money limit | Provider-side: it is configurable and counts money. It is softer for Google |
| Z-8 against D-5, Y-3, C-9, S-4 | Move the comment-addressing section out, or harden and hand-test it here | Z-8 |
| I-1, D-2 against the E3 fallback | Two read paths now, or one path chosen by E3's number | One path; settings before mechanisms |
| D-8 against ADR-0012 and D-4 | Block a flagged value everywhere, or stop or withhold whole items | No value-level blocking |
| D-8 against I-4 | Argue the departure from ADR-0012 before knowing the hit rate | I-4 first |
| I-10, D-4, C-2, D-9 against the nine-outcome table | Twelve outcomes, or fewer | Seven, with one pass-through provider error |
| D-4 against "one repository first" | A live run and adapter for both providers, or for the first repository's provider | One provider first. The concept names both, so the design owner confirms |
| Y-2, I-9 against Z-5, Z-7 | More source rules for Goal 7, or fewer | The existing import rule plus one image check |
| D-6 against Goal 2's four labels | Add a package matcher to keep the fourth label, or drop the label | Drop the label |
| Z-1 against I-5 against D-7 | One trial, three consequences | The trial in all three; the consequence is the design owner's |
| D-3 against the repository's convention | A new rule for keeping Go current, or "the two most recent releases" | The existing convention; only the pinned matrix is stale |
| S-4, S-5 against the document's claims of completeness | Add checks and open questions, or delete the claims | Delete |
| Z-6 against the design | A type generator, or the hand-written mirror the plugin uses | The mirror, unless the design gives a reason |

## The smallest first version that still tests both claims

**Keeps:**

* One connected repository in one organisation, on a desktop browser. Phones get reading and asking only.
* One container, one instance, no database: the token sealed in the cookie, the access check, archives in memory keyed by owner, repository and hash, the diff from two texts, literal search, and the title and description shown as text.
* **Claim 1, navigation into unchanged code,** only if the web-editor count leaves a gap: the index workflow file, the decoder, definition and references with three labels, and "this was wrong" as a log line.
* **Claim 2, asking:** one gateway that scans at send; the first repository's provider only; tools for search, read a range, list changed files and read the description, plus the two lookup tools if navigation shipped; the conversation held in the page; seven outcomes; spend bounded by the per-question limits and the provider's own monthly limit.
* Desktop shortcuts and the diff or new-code toggle, both of which the concept requires.
* Measures from request logs, and two feedback weeks with thresholds set first.

**Sets aside:**

* Postgres and its four tables, stored and cross-device conversations, the token budget, retention, the restore drill and one ADR.
* Both skill sections, Goal 13, journey 10 and the intent parser, which move to a separate CLI change.
* The "outside the repository" label, the per-file read path, and per-file withholding if the scanner run finds nothing to withhold.
* The second provider adapter, until a repository needs it.
* Touch navigation, the command palette and the type generator.

## Not verified

* The scanner was not run by this reviewer. The claim that its default rules carry built-in exemptions, and the hit count on the team's repositories, are unchecked.
* How GitHub's web editor, as distinct from vscode.dev, behaves on one of the team's pull requests, and whether the team may use it.
* What a Google project spend cap returns at the cap, and whether its tool loop needs provider parts sent back unchanged.
* That an edited history is rejected by Anthropic's current models. The live page confirms only that earlier reasoning blocks are kept in context and billed.
* What a one-instance Cloud Run service does with requests beyond its concurrency. A minimum instance was not priced, and the default log retention was not checked against the feedback weeks.
* Whether the design owner will accept a conversation that lives only in the page.
* That the assistant's "list changed files" tool is the consumer of `ranges`. The design does not say.
* The tally of about 45 additions and 28 avoidable is the reviewer's own; borderline items could be classed differently.
