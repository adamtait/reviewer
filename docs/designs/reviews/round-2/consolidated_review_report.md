# Consolidated Review Report: review workspace design, round 2

**Target:** `docs/designs/review-workspace.md` (954 lines; feature sections 1–572, implementation plan 573–954), as revised after the first review round.

**Date:** 2026-11-23

The first round's reports are in `docs/designs/reviews/round-1/`.

## Verdict

The revised design does not pass review. The revision did close the first round's design blockers: the base commit now comes from GitHub, the diff no longer waits for CI, and the plan no longer breaks the existing test matrix. The structural reviewer finds that the document's layout now meets the standard.

Three things stand in the way.

1. **Part of what the design builds may already exist.** This round's reviewers could research on the web, and three of them independently found GitHub documentation for features the design's problem statement says are missing. The design's own stop rule, entry condition E4, says to cut the design back to the change brief if existing tools give navigation and a way to ask. That check has not been run.
2. **The revision introduced new faults.** Work that outlives a request has no owner, sign-in accepts any GitHub account, and the secrets scanner can be switched off by the content it scans.
3. **Several facts the design states are wrong or out of date,** and two of its dependencies cannot be added to this repository as written.

The Stage 8 reviewer adds a warning that repeats the first round's. This round's other stages ask for about twenty new mechanisms, and for most of them the problem goes away if something is removed from the first version instead.

## Reviewer status

| Stage | Reviewer | Scope | Status | Blocker | Major | Nit |
| --- | --- | --- | --- | --- | --- | --- |
| 5 | `egm-zoom-out` | Full file | completed | 1 | 8 | 1 |
| 5 | `egm-inventive-catalyst` | Full file | completed | 0 | 5 | 5 |
| 6 | `egm-comprehension` | Full file, no other repository files | completed | 0 | 7 | 5 |
| 7 | `egm-design` | Lines 1–572 | completed | 2 | 7 | 1 |
| 7 | `egm-structural` | Lines 1–572 | completed | 1 | 3 | 6 |
| 7 | `egm-style` | Lines 1–572 | completed | 0 | 9 | 1 |
| 8 | `egm-zoom-out` | Full file plus this round's three earlier reports | completed | 1 | 8 | 1 |
| 9 | `egm-implementation-readiness` | Lines 573–954 | completed | 3 | 7 | 0 |
| 9 | `egm-codebase-details` | Lines 573–954 | completed | 1 | 11 | 0 |
| 9 | `egm-style` | Lines 573–954 | completed | 0 | 7 | 1 |

Stage reports: `zoom_out_report.md`, `comprehension_report.md`, `critic_report.md`, `intermediate_zoom_out_report.md` and `implementation_readiness_report.md`. The Stage 9 report merges its three reviewers' 30 raw findings into 15.

After that merge the five reports hold 87 findings: 8 Blocker, 58 Major and 21 Nit. They consolidate into the 19 action items below.

**How this round was run.** Each reviewer was a general-purpose agent that read its reviewer persona first, so that it could search the web, fetch pages and run read-only commands. The plugin's own reviewer type can only read and search files. These agents could have written files; each was told not to, and the working tree was checked after every stage and was unchanged. No reviewer read the first round's reports. Where a finding rests on an outside fact, the stage report links the page that was checked.

**What the reviewers could not find out.** Whether the team holds GitHub Copilot licences. Whether GitHub's current pull request page still offers its definition and reference lookups. Whether GitHub's web editor navigates a real pull request with generated types, or works on an iPhone. Whether a fine-grained token can fetch one commit by its hash.

## Decisions needed from the author

Settle these before the document is edited again. They are in the order the Stage 8 reviewer recommends, because each can make later items unnecessary.

### ACT-01. Run entry condition E4 now, and restate the problem as the gap that remains - 🔴 Blocker

* **Problem:** The problem statement says GitHub's diff is plain text and that there is no way to ask. GitHub's documentation describes both:
  * **Asking.** Copilot Chat explains a file or selected lines in a pull request and summarises it, for Copilot licence holders.
  * **Navigation.** GitHub documents jump-to-definition and find-all-references for TypeScript, by name matching within one repository. GitHub announced it for the pull request "Files changed" tab in December 2021.
  * **A web editor.** VS Code for the Web has had full-project TypeScript navigation since version 1.93, and GitHub's web editor opens a pull request when `.` is pressed on its page.
  * **Third-party tools** of the same shape exist, such as Devin Review and Sourcegraph's browser extension.
* **What looks distinctive after that:** navigation resolved by the compiler from a diff line into unchanged code, including types generated at install; the intent record; the rule that one configured provider receives the code; and phone use. The design compares on none of these.
* **Action:** run E4 with stated pass criteria. The reviewers put the web editor check at ten minutes on one real pull request. Say whether the one-provider rule rules Copilot out; if it does, "ask" is distinctive for that reason alone. The alternatives table cannot record this option as "not yet evaluated" and meet the standard.
* **Sources:** Z-1, D-1, S-1, Y-1, Z2-1.

### ACT-02. Decide the size of the first version - 🔴 Blocker

* **Problem:** The first version is justified as a test and sized as a product. The first feedback week tests only reading a diff, which GitHub already does. The change brief addresses the first stated problem and ships eighth, with no feedback point after it.
* **The smallest version the Stage 8 reviewer found:**
  * **With no service at all:** the agent skill change that writes the intent record into the pull request description, tested on GitHub's own page; the index workflow, which is also entry condition E2; and one small change to the CLI so the declined trial of wider review context can run on one repository.
  * **Only if E4 leaves a navigation gap:** the first five pull requests, PR-47 to PR-51, with the first feedback week after navigation ships. The gateway, assistant, brief, telemetry and release work would wait.
  * **Smaller still:** the same binary run by each engineer with their own token tests navigation without sign-in, hosting or a shared store. This sets phone use aside for the trial.
* **Action:** choose between that and the ten pull requests as planned.
* **Sources:** Z-2, I-9, Z2-2.

### ACT-03. Decide how stage work runs - 🔴 Blocker

* **Problem:** Fetching source, loading the index and writing the brief all run inside one reviewer's request, and a second reviewer is told to wait. Cloud Run allocates CPU only while a request is in progress, ends a request at its timeout, and can stop an instance at any time. If that happens mid-stage, the stage stays marked "fetching" or "generating", and because commits never change, nothing retries it. A phone dropping its connection can strand the shared brief for everyone.
* **Options the reviewers raised:**
  * **Add ownership:** an owner and a lease on each stage, takeover of an expired stage, and a detached job for the brief and review.
  * **Remove the cause:** stop storing running states. A request that finds no result does the work itself. Storage is keyed by content or commit, so a duplicate write is harmless, and a crashed attempt leaves nothing behind. The cost is an occasional duplicate fetch or brief when two people open the same commit at once.
* **Either way:** state the platform's request timeout against the fetch time E3 measures.
* **Sources:** D-2, I-1, IR-3, C-3, Z2-3.

### ACT-04. Decide storage, and whether `git` is needed at all - 🟡 Major

* **Problem:** To stay stateless, the design rebuilds git's file and tree model in Postgres, with stored diffs, a trigram index and a reference-counting sweep. The design itself calls a git repository on disk "least code". A fetched commit is held in the container's memory. Any text file containing a NUL byte fails the insert and so fails the whole import.
* **A reuse point the revision missed:** the CLI already turns GitHub's per-file patches into changed files and line ranges without `git`, in `cmd/reviewer/run.go`. If files outside the diff are read on demand with the viewer's token, the navigation slice stores no source at all. That removes the fetch, three tables, the retention sweep and the "empty environment" contradiction. The cost is text search at a commit.
* **Options:** read on demand and defer search; one archive per commit searched in memory; or a git repository on a disk. The fetch time measured in E3 should decide.
* **Also undefined:** the retention rule cannot be computed as written, because no pull request state is stored and the sweep has no credential.
* **Sources:** Z-3, I-6, C-4, IR-9, Z2-4.

### ACT-05. Decide whether the workspace reviews changed lines itself - 🟡 Major

* **Problem:** The plan passes the pull request description and intent record to the CLI's review code as "guidance". That code tells the model guidance documents "win". The author's own text, usually written by an agent, lands in the one slot the model is told to obey, which undoes the rule that guidance is read from the merge base. The same code cuts the diff at 120 KB without telling the caller, keeps every unchecked finding if its second pass fails, and turns an unparseable reply into zero findings.
* **Options:**
  * **Repair the reuse:** an author-context input in the review code, typed outcomes from both calls, a truncation flag, and new states for "not checked" and "partly read".
  * **Defer that half:** keep "notes on the plan", which does not use that code, and leave findings on changed lines to the CLI, which already posts them on GitHub where reviewers comment. This needs the author's agreement, because an AI review of the code is a stated goal.
* **Related:** the plan builds a second pair of provider adapters beside the existing pair, and the gateway's request type cannot carry settings the review code uses.
* **Sources:** Z-4, Z-6, I-3, D-5, IR-6, Z2-5.

### ACT-06. Put both sign-in routes to the organisation owner, and close the sign-in exposure - 🟡 Major

* **Problem:** The App route was set aside because it needs an organisation owner. So does the token route: by default an owner must approve each fine-grained token, and such tokens expire within 366 days. Outside collaborators cannot use them, and one token cannot span two organisations. Separately, sign-in accepts any valid GitHub token. Every token can read public repositories, so if a connected repository is public, any GitHub account can see its telemetry and spend its AI budget.
* **Options for the exposure:** a login allowlist; or, with no second access table, make a public repository a configuration error and refuse any token that is not fine-grained.
* **Action:** E1 should put both routes to the owner at once and ask about outside collaborators and the number of organisations.
* **Sources:** Z-5, D-3, I-7, Z2-8.

### ACT-07. Decide what to do about diagrams - 🟡 Major

* **Problem:** The current release of the diagram library depends directly on a package offered under two licences, both of which this repository's licence check refuses. The library also accounts for more than 100 of about 150 packages that would need hand-written inventory rows.
* **Options:** show diagram source as text in the first version; pin the library's previous major line, which lacks that dependency; or choose another renderer.
* **Also:** send the content security policy as a response header. Set in the page, not every control applies.
* **Sources:** Z-9, IR-5, D-10, Z2-10.

### ACT-08. Decide what the brief may claim from the index, and when the index is built - 🟡 Major

* **Problem:** The index is treated as untrusted, yet "what unchanged code depends on this change" is drawn from it and placed in the half of the brief labelled "the code shows". An index that covers part of a repository, which is the default for a repository with several TypeScript projects, yields "nothing depends on this" under the strongest label. Separately, GitHub does not run `pull_request` workflows while a pull request has a merge conflict, so such a pull request never gets an index.
* **Options:** measure index coverage and mark unindexed files; or remove that claim from the brief, label reference results "from the CI-built index", and never show an empty list as "no references".
* **On the trigger:** building the index on every branch push avoids the merge-conflict gap, and makes moot whether a pull request's run reports the head commit.
* **Still undefined:** how the service tells "waiting for CI" from "not produced", and how it finds one commit's artifact when every artifact shares one name.
* **Sources:** I-4, I-5, D-6, D-8, C-6, IR-7, Z2-6.

### ACT-09. Decide how the feedback weeks are measured - 🟡 Major

* **Problem:** The three feedback weeks decide whether work continues, and their measures are usage counts. The code that records those counts arrives after all three weeks. The thresholds are still an open question, and no date is set for fixing them.
* **Options:** move telemetry earlier; or take the counts from request logs, which already carry user, repository and pull request, and take the history baseline with the existing `reviewer metrics` command. The second option lets the telemetry pull request wait, which is the author's call because visible telemetry is a stated want.
* **Sources:** C-1, Z-10, Y-5, IR-12, Z2-9.

## Fixes

### ACT-10. Settle the two Go dependencies - 🔴 Blocker

* **Problem:** Every recent release of the Postgres driver requires Go 1.25, and this repository declares 1.24. The SCIP bindings have moved, require Go 1.25, and bring in a module under a licence the licence check refuses. Either way `make licenses` fails in the navigation pull request. The plan says a refused dependency "is replaced" and names no replacement.
* **Action:** pin both versions and the Go floor, list the CI change, and decide how the index is read: a licence-tool change recorded in an ADR, or a decoder that needs no bindings. One reviewer notes an existing tool that converts an index to SQLite.
* **Sources:** IR-1, Z-8.

### ACT-11. Cut the implementation plan back, or complete it - 🔴 Blocker

* **Problem:** The reviewers disagree on direction and agree that the plan cannot stay as it is. The readiness reviewers find that table columns and every request and response shape are left to the implementer, that the claimed schema validation has no mechanism, that the web tooling list cannot run its own lint and test commands, and that one change breaks an existing test the plan does not list. The zoom-out reviewers want the plan ended before line 573 until the entry conditions are recorded.
* **Action:** cut first. The entry conditions and ACT-01 to ACT-09 decide which pull requests exist. Then complete the plan for those.
* **Sources:** Z-7, IR-2, IR-8, IR-10, IR-11, IR-13, IR-14.

### ACT-12. Stop scanned content from switching the scanner off - 🟡 Major

* **Problem:** One reviewer ran the pinned scanner as the plan describes. A credential on a line carrying an inline allow comment was not reported. An ignore file at the root of the scanned directory suppressed findings even with the flag that disables the comment. The plan tests neither. Scan results are also cached by content while the operator's allowlist works by path, so the same bytes can be cleared at one path and sent from another. The rule for a secret in a description is not stated.
* **Action:** pass the flags that turn the scanner's own exemptions off, fix a scan layout in which no scanned file sits at the scan root, test each case, and remove the operator allowlist until false positives are measured.
* **Sources:** I-2, D-4, C-2, IR-4, Z2-7.

### ACT-13. Correct "empty environment" - 🟡 Major

* **Problem:** The goal, its test, the risks table and the security section all say `git` starts with an empty environment. The same passage says the viewer's token is passed to it through the environment. The existing diff package's `git` helper inherits the whole environment.
* **Action:** say "no inherited environment", list the variables `git` receives, and name the mechanism. One reviewer confirmed that passing configuration through `GIT_CONFIG_COUNT` variables works without a helper program.
* **Sources:** C-5, S-2, Y-2.

### ACT-14. Correct facts that are wrong or out of date - 🟡 Major

* **The indexer.** The design says the latest `scip-typescript` release is 0.4.0 from October 2025. Five releases followed between 21 October and 10 November 2026; the newest, 0.6.2, bundles TypeScript 6 and needs Node 22. Its README lists no `bun` workspace option. The stated fallback also fails for a repository on TypeScript 7.0, which has no programmatic interface.
* **A rejected alternative.** "Reading a whole tree takes one call per file" is wrong; one call returns a commit as an archive. "Search would be lost" does not follow, because the design searches its own copy.
* **Others.** A persistent disk does not require one machine. The claim about an older index format's TypeScript indexer is ambiguous, because there are two.
* **Sources:** D-9, Y-3, Y-7, I-10, S-10.

### ACT-15. State cost in money, and handle spend caps - 🟡 Major

* **Problem:** The estimate of 40 million input tokens a week is about $350 to $1,400 a month at one provider's published prices, before output tokens. That provider's two lowest account tiers cap monthly spend at $500 and $1,000, and at the cap usage pauses until the next month. "Quota exhausted" has several shapes, and the brief's states have no entry for a budget stop.
* **Action:** give a monthly figure in money with output included, check the account's cap before the gateway is built, and map each quota shape to an outcome.
* **Sources:** D-7, I-8.

### ACT-16. Calibrate claims and checks - 🟡 Major

* **Problem:**
  * "The reason for the change is on screen before any line is read", when the flow shows the diff first and the first reader waits for the brief.
  * "An empty review means the model raised nothing", when findings may have been disproved, discarded or never read.
  * Labels such as "compiler-labelled" and "the code shows" for content the service cannot confirm.
  * Goal checks that do not test their goal, and a performance section with no number.
* **Action:** reword each to what the mechanism delivers, and put numeric targets or "not yet set" in the requirements.
* **Sources:** Y-4, Y-6, Y-8, Y-9, S-9.

### ACT-17. Complete the lists of risks and open questions - 🟡 Major

* **Problem:** Five risks the document names are missing from its risks table, among them the encryption key sitting in the process's memory and entry condition E3 having no fallback. Two points that still need the author are missing from Open Questions, so the telemetry scope and four rejected alternatives read as settled.
* **Sources:** S-3, S-4.

### ACT-18. Connect the second agent-skill instruction to something, or remove it - 🟡 Major

* **Problem:** The skill file gains an instruction for agents to read review threads, push fixes and reply. No goal, journey or test covers it, and the document says "nothing else changes" in each repository. Agents in every repository would start pushing and replying on GitHub.
* **Sources:** C-7.

### ACT-19. Terms, counts and cross-references - 🟢 Nit

* **Problem:** "The author" means two people. Four words name the signed-in person. "Three sources" sits above a four-row table. "Five minutes in the example configuration" has no such example. The Background omits several existing parts the plan relies on.
* **Sources:** C-8 to C-12, S-5 to S-8, Y-10, IR-15.

## Where the reviewers disagree

| Conflict | One side | Other side | Stage 8 view |
| --- | --- | --- | --- |
| Stuck stages | I-1, D-2: leases and a detached worker | I-7: no token outside the request | Neither; stop storing running states |
| Source storage | I-6, Y-7: add a disk to Cloud Run | Z-3: `git` on a disk; D-9: no `git` | Least owned storage; E3 decides |
| Scanner exemptions | I-2: apply them in the gateway | D-4: switch them off | Switch them off |
| The partial index | I-4, D-6: measure coverage | C-6, Y-8: relabel | Relabel and remove the claim |
| Feedback measures | C-1: telemetry first | Z-2, Z-10: logs and the existing command | Logs and the existing command |
| Prompt caching | D-7: require it now | Z-2, Y-5: measure first | Measure first |
| Diagrams | D-10: sandbox the library | Z-9: drop it | Drop it |
| The plan's detail | IR-2 and others: complete it | Z-7: cut it | Cut first |

## Suggested order of work

1. Find out whether the team holds Copilot licences and whether the one-provider rule excludes it. Run the web editor check on one pull request. Record the result of E4 (ACT-01).
2. Decide the size of the first version (ACT-02).
3. If a service remains, rule on ACT-03 to ACT-09 and settle the dependencies (ACT-10).
4. Cut the implementation plan to the pull requests that remain, then apply ACT-11 to ACT-19 to them.

## Traceability matrix

Each raw finding maps to one action item. Severity is the reviewer's own.

| Finding | Stage | Severity | Subject | Action |
| --- | --- | --- | --- | --- |
| Z-1 | 5 | Blocker | Problem not established; E4 deferred | ACT-01 |
| Z-2 | 5 | Major | A test sized as a product | ACT-02 |
| Z-3 | 5 | Major | Storage built to avoid a disk | ACT-04 |
| Z-4 | 5 | Major | Author's description passed as guidance | ACT-05 |
| Z-5 | 5 | Major | Identity comparison unsupported | ACT-06 |
| Z-6 | 5 | Major | Second provider stack | ACT-05 |
| Z-7 | 5 | Major | Detail stacked on open conditions | ACT-11 |
| Z-8 | 5 | Major | Thin reuse, costly shared repository | ACT-10 |
| Z-9 | 5 | Major | Diagram library fails the licence rule | ACT-07 |
| Z-10 | 5 | Nit | One-off baseline built as a feature | ACT-09 |
| I-1 | 5 | Major | Shared stage owned by one request | ACT-03 |
| I-2 | 5 | Major | Secrets gate cleared by scanned content | ACT-12 |
| I-3 | 5 | Major | Reused review code hides partial results | ACT-05 |
| I-4 | 5 | Major | Index not bounded against omissions | ACT-08 |
| I-5 | 5 | Major | Index job needs a mergeable pull request | ACT-08 |
| I-6 | 5 | Nit | Whole commit held in memory | ACT-04 |
| I-7 | 5 | Nit | Tokens stored though only used in-request | ACT-06 |
| I-8 | 5 | Nit | One budget for brief and questions | ACT-15 |
| I-9 | 5 | Nit | Brief's premise tested last | ACT-02 |
| I-10 | 5 | Nit | Indexer facts stale; fallback weaker | ACT-14 |
| C-1 | 6 | Major | Feedback depends on later telemetry | ACT-09 |
| C-2 | 6 | Major | Secrets rule misses kinds of content | ACT-12 |
| C-3 | 6 | Major | How stages run is unexplained | ACT-03 |
| C-4 | 6 | Major | Retention rule cannot be stated | ACT-04 |
| C-5 | 6 | Major | "Empty environment" contradiction | ACT-13 |
| C-6 | 6 | Major | Index bounds cover definitions only | ACT-08 |
| C-7 | 6 | Major | Skill instruction connects to nothing | ACT-18 |
| C-8 | 6 | Nit | "Connected repository" undefined | ACT-19 |
| C-9 | 6 | Nit | Background omissions | ACT-19 |
| C-10 | 6 | Nit | Words with two meanings | ACT-19 |
| C-11 | 6 | Nit | Small internal mismatches | ACT-19 |
| C-12 | 6 | Nit | Process terms unexplained | ACT-19 |
| D-1 | 7 | Blocker | Strongest alternative unevaluated | ACT-01 |
| D-2 | 7 | Blocker | Pipeline has no owner after the request | ACT-03 |
| D-3 | 7 | Major | Sign-in open to any GitHub account | ACT-06 |
| D-4 | 7 | Major | Scanned content can switch the scanner off | ACT-12 |
| D-5 | 7 | Major | Review-code reuse breaks two claims | ACT-05 |
| D-6 | 7 | Major | Index claims in the trusted half of the brief | ACT-08 |
| D-7 | 7 | Major | Cost in tokens only; spend caps | ACT-15 |
| D-8 | 7 | Major | Actions behaviour leaves index gaps | ACT-08 |
| D-9 | 7 | Major | False premise and stale fact | ACT-14 |
| D-10 | 7 | Nit | Browser policy; retention rule | ACT-07 |
| S-1 | 7 | Blocker | Alternative recorded as not evaluated | ACT-01 |
| S-2 | 7 | Major | "Empty environment" contradiction | ACT-13 |
| S-3 | 7 | Major | Risks table incomplete | ACT-17 |
| S-4 | 7 | Major | Open Questions incomplete | ACT-17 |
| S-5 | 7 | Nit | Alternatives not grouped as claimed | ACT-19 |
| S-6 | 7 | Nit | Decisions and ADRs not mapped | ACT-19 |
| S-7 | 7 | Nit | Counts and names disagree | ACT-19 |
| S-8 | 7 | Nit | Dangling cross-references | ACT-19 |
| S-9 | 7 | Nit | Performance section has no requirement | ACT-16 |
| S-10 | 7 | Nit | Outside facts need links or correction | ACT-14 |
| Y-1 | 7 | Major | Problem statement contradicted by GitHub's docs | ACT-01 |
| Y-2 | 7 | Major | "Empty environment" contradiction | ACT-13 |
| Y-3 | 7 | Major | Indexer release fact stale | ACT-14 |
| Y-4 | 7 | Major | "Empty review" wording | ACT-16 |
| Y-5 | 7 | Major | Gates have no criteria | ACT-09 |
| Y-6 | 7 | Major | "Before any line is read" | ACT-16 |
| Y-7 | 7 | Major | Rejections overstated | ACT-14 |
| Y-8 | 7 | Major | Labels name unconfirmed sources | ACT-16 |
| Y-9 | 7 | Major | Checks do not measure their goals | ACT-16 |
| Y-10 | 7 | Nit | Wording and terms | ACT-19 |
| Z2-1 | 8 | Blocker | Settle five things first | ACT-01 |
| Z2-2 | 8 | Major | The smallest version | ACT-02 |
| Z2-3 | 8 | Major | Delete the rule, not add leases | ACT-03 |
| Z2-4 | 8 | Major | Decide storage first; existing diff path | ACT-04 |
| Z2-5 | 8 | Major | Defer findings on changed lines | ACT-05 |
| Z2-6 | 8 | Major | Remove the index claim; change the trigger | ACT-08 |
| Z2-7 | 8 | Major | Exemptions off; no allowlist | ACT-12 |
| Z2-8 | 8 | Major | Refuse public repositories | ACT-06 |
| Z2-9 | 8 | Major | Measure before partitioning | ACT-09 |
| Z2-10 | 8 | Nit | Drop the diagram library; cut the plan | ACT-07 |
| IR-1 | 9 | Blocker | Go dependencies cannot be added | ACT-10 |
| IR-2 | 9 | Blocker | Columns and API shapes undefined | ACT-11 |
| IR-3 | 9 | Blocker | Where stage work runs | ACT-03 |
| IR-4 | 9 | Major | Scan can be switched off | ACT-12 |
| IR-5 | 9 | Major | Diagram library licence | ACT-07 |
| IR-6 | 9 | Major | Review reuse and gateway contract | ACT-05 |
| IR-7 | 9 | Major | Index states and lookup rules | ACT-08 |
| IR-8 | 9 | Major | File lists and CI wiring | ACT-11 |
| IR-9 | 9 | Major | Source import failures | ACT-04 |
| IR-10 | 9 | Major | Web tooling list incomplete | ACT-11 |
| IR-11 | 9 | Major | Network install inside a unit test | ACT-11 |
| IR-12 | 9 | Major | Gates without pass values | ACT-09 |
| IR-13 | 9 | Major | "A reviewer can then" overstated | ACT-11 |
| IR-14 | 9 | Major | Guarantees stronger than their checks | ACT-11 |
| IR-15 | 9 | Nit | Wording | ACT-19 |
