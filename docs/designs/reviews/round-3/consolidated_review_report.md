# Consolidated Review Report: review workspace design, round 3

**Target:** `docs/designs/review-workspace.md` (1,129 lines; feature sections 1–691, implementation plan 693–1129), as revised after the second review round.

**Date:** 2026-11-23

Earlier rounds are in `docs/designs/reviews/round-1/` and `docs/designs/reviews/round-2/`.

## Verdict

The revised design does not pass review. The round was not clean: reviewers raised 4 Blocker and 60 Major findings.

The second round's faults are closed. No reviewer re-raised stuck stages, the secrets scanner being switched off by content, public repositories, the diagram library's licence, or the two Go dependencies that could not be added. The reviewers also re-checked, from their own sources and experiments, the facts the revision added: the index schema's field numbers, the driver, the web packages and their licences, the scanner results, and the provider prices and caps. All held.

Four things stand in the way.

1. **One access-control gap.** Cached source is described as held per commit, and nothing binds it to the repository the access check covered. A user who can read one connected repository could be served another's source. The fix is small.
2. **The assistant's tool loop cannot work as defined.** The conversation interface has no way to carry the reasoning data both providers expect back with a tool result, and the existing Google adapter it reuses rewrites model names.
3. **An existing tool for each half of the design was not tried or not weighed.** Three reviewers independently ask for the same half-hour trial of GitHub's web editor, and find that two of the design's reasons for rejecting it do not hold against the sources it cites.
4. **The plan is ahead of its evidence.** A third of the document is file-level plan that assumes three trials pass, and none has been run.

**The count has not fallen across rounds:** 91 findings, then 87, then 89. Each rewrite closed what was raised and added new detail for fresh reviewers to test. The Stage 8 reviewer's reading is that another rewrite of the same size would repeat this. Of about 45 additions the other reports ask for, it judges about 28 avoidable by removing or narrowing something, and it says four decisions settle more than half of the findings: run the trials first, store nothing, move the skill sections out, and shrink the outcome table.

## Reviewer status

| Stage | Reviewer | Scope | Status | Blocker | Major | Nit |
| --- | --- | --- | --- | --- | --- | --- |
| 5 | `egm-zoom-out` | Full file | completed | 0 | 4 | 5 |
| 5 | `egm-inventive-catalyst` | Full file | completed | 0 | 6 | 4 |
| 6 | `egm-comprehension` | Full file, no other files | completed | 0 | 8 | 4 |
| 7 | `egm-design` | Lines 1–691 | completed | 1 | 7 | 2 |
| 7 | `egm-structural` | Lines 1–691 | completed | 0 | 5 | 5 |
| 7 | `egm-style` | Lines 1–691 | completed | 0 | 7 | 3 |
| 8 | `egm-zoom-out` | Full file plus this round's Stage 5, 6 and 7 reports | completed | 1 | 8 | 1 |
| 9 | `egm-implementation-readiness` | Lines 693–1129 | completed | 1 | 8 | 1 |
| 9 | `egm-codebase-details` | Lines 693–1129 | completed | 1 | 6 | 5 |
| 9 | `egm-style` | Lines 693–1129 | completed | 0 | 8 | 2 |

Stage reports: `zoom_out_report.md`, `comprehension_report.md`, `critic_report.md`, `intermediate_zoom_out_report.md` and `implementation_readiness_report.md`. The Stage 9 report merges its three reviewers' 32 raw findings into 18.

After that merge the five reports hold 89 findings: 4 Blocker, 60 Major and 25 Nit. They consolidate into the 23 action items below.

**How this round was run.** Each reviewer was a general-purpose agent that read its reviewer persona first, so that it could search the web, fetch pages and run experiments outside the repository. No reviewer read an earlier round's reports. The Stage 5, 6, 7 and 9 reviewers ran side by side, because none depends on another's output; Stage 8 ran after Stages 5 to 7. The working tree was checked after every stage and was unchanged.

**Checked by the drafter after the reviews.** Four claims about existing code were opened and hold: the Google adapter's model-name rewrite, the 30-second timeout in the GitHub client, the patch parser's assumption of no context lines, and the installer writing the skill file only under `.agent/skills/`.

**What the reviewers could not find out.** How GitHub's web editor behaves on one of the team's pull requests. Whether Claude Code loads a skill installed under `.agent/skills/`. Whether a model provider rejects a tool loop that omits reasoning data, or only answers worse. What Google's API returns at a spend cap. Whether Cloud Run keeps a request running after the browser disconnects. Everything entry conditions E1 to E3 are meant to measure.

## Fix first

### ACT-01. Bind cached source and indexes to their repository - 🔴 Blocker

* **Problem:** Archives, indexes and fetches in flight are shared between users and described as held per commit. The access check is made on the owner and repository in the address, while the commit hash arrives separately. With entries keyed by hash alone, a user who can read connected repository A but not B is served B's source through A's routes. All three of Goal 6's tests pass with that flaw in place.
* **Action:** State that every cache entry and shared fetch is keyed by owner, repository and hash. Add a test in which a user with access to one connected repository asks for another's cached commit and index through both repositories' routes. The Stage 8 reviewer asks for this fix first and alone, because no other decision removes it.
* **Sources:** D-1.

## Decisions needed from the design owner

These are in the order the Stage 8 reviewer recommends, because each can make later items unnecessary.

### ACT-02. Count E2's 50 symbols in GitHub's web editor - 🟡 Major

* **Problem:** E4 was closed without trying the web editor on one of the team's pull requests. The design rejects it on four reasons, and two do not hold against the design's own sources:
  * **"It cannot run a dependency install."** The release notes the design cites say the editor gets typings for packages listed in `package.json`, with project-wide navigation in Chrome, Edge and Firefox. The objection covers only types generated at install.
  * **"The team cannot extend it."** VS Code for the Web runs extensions that can call an HTTP service.
* **What the design owner has already decided:** the team wants a platform it controls. No reviewer asks to re-open that. Three reviewers ask for a measurement beside it.
* **Action:** resolve E2's 50 hand-picked symbols in the web editor, in a desktop browser, on the same repository, and record the score beside the index's. This reuses E2's sample and takes about half an hour. The reviewers differ on what the number should mean: remove navigation from the first version if the editor meets E2's bar; keep navigation and use the score as the baseline Goal 2 lacks; or add "navigation from the editor, build only the assistant" as an option. That choice is the design owner's. Correcting the alternatives row is a fix either way.
* **Sources:** Z-1, I-5, D-7, Z2-3.

### ACT-03. Run the trials before fixing more of the plan - 🔴 Blocker

* **Problem:** About a third of the document is file-level plan for PR-48 to PR-54, written on the stated assumption that E1, E2 and E3 all pass. Each can remove or replan a part, their pass values are the drafter's proposals, and the feedback thresholds are unset. The reports ask for about 45 additions, and five findings change or delete text that about thirty others ask to extend.
* **Action:** run E2 and E3 now, with one hand-written workflow file and one token, and put E1 to the organisation owner. Write the results into the document, without repository names, before fixing file-level detail beyond PR-47. The reviewers ask for four additions to the trials:
  * **E2:** score the 50 symbols through the name check as well, and count the documents the service would drop.
  * **E3:** record a cold time from opening a pull request to seeing the diff.
  * **E3:** run the pinned scanner once, with the planned flags, over the unpacked tree, and record how many files would be withheld.
  * **E2's deadline:** it is "before PR-47 merges" in one place and "before PR-47 starts" in another.
* **Sources:** Z-4, I-4, I-7, C-7, Z2-1.

### ACT-04. Decide whether a conversation must outlive the page - 🟡 Major

* **Problem:** Postgres is justified by two needs: a monthly budget sum that survives a restart, and a conversation that follows the user between devices. The second appears in no goal, and the API has no route by which a user could find a conversation again. The database arrives in PR-48, and nothing uses it until PR-52. The reviewers pull two ways:
  * **Build it out:** a route listing a user's conversations, a stored format for each part, a write order, a rule for a dropped connection, and excerpts stored as references so conversations hold less code.
  * **Take it out:** hold the conversation in the page and send its turns with each question. Write model-call records and "this was wrong" reports as log lines. Bound spend with the per-question limits and the provider's own monthly limit on a workspace or project per repository key.
* **What taking it out removes:** four tables, the driver, migrations, the Postgres CI job, one ADR and one supersession, the token budget and its outcome, two routes, the retention question and the restore drill.
* **What it costs:** a reload or a second device starts a new conversation; a dropped connection loses the answer in progress; reports are readable only by the operator and only for the log retention period; and Google's project cap is softer than the designed budget. The concept asked for configurable budget limits, and a provider-side limit is configurable.
* **If the database stays:** move it to PR-53, where it is first needed, and define how a blocked question is stored. As written, storing one would put the credential in the database.
* **Sources:** Z-3, I-2, I-6, I-8, C-1, S-7, Z2-2, IR-8.

### ACT-05. Move the agent skill sections to their own change, and check which agents read the file - 🟡 Major

* **Problem:**
  * **Which agents read it.** The design says both of the team's agents read the skill file. Claude Code's documentation lists project skills under `.claude/skills/` and not under `.agent/skills/`, where the installer writes. If so, the intent record and the "address review comments" section reach only Antigravity sessions.
  * **Thread text as instructions.** The second section has an agent read review threads, then push and reply with the pull request author's credentials. Open threads include the CLI's own comments and, where the model lane is on, model-written ones. The section does not tell the agent to treat thread text as data, and agent replies carry no marker, so they would mix into the human reviews a later phase learns from.
  * **Claims and checks.** Journey 10 states what the agent does as an outcome, while the mechanism is prose an agent may not follow. Its only test asserts that words are present in a template. Goal 12's check says every existing file is written as before, while the skill file changes.
  * **Existing installs.** `reviewer init` does not overwrite a skill file without `--force`, and the design does not say what a connected repository needs first.
* **Options:** harden the section here, with bot-thread handling, a marker on replies, risk rows and a hand-run with both agents; or move both skill sections, Goal 13 and journey 10 to a separate CLI change with its own design, and keep only the reading side here. The Stage 8 reviewer favours moving them, and showing the description as plain text with no parser. PR-47 would then add one workflow template.
* **Needs the design owner:** the concept put both sections in the first stage, and the second section is the design owner's chosen way to close the fix loop.
* **Sources:** Z-8, D-5, Y-3, Y-4, S-6, C-8, Z2-4.

### ACT-06. Weigh the team's existing agents as a way to ask - 🟡 Major

* **Problem:** The Problem says there is no permitted way to ask. The Background says the team already runs two coding agents from the same two vendors the service would call. The alternatives table weighs neither asking one of them on a checkout of the pull request branch, nor invoking one from a pull request comment through an Actions workflow.
* **Action:** state the gap as what is true: no way to ask from the pull request view without a checkout. Add both rows with their costs. The reviewer lists three: answers would be comments everyone reads, nothing scans for secrets before sending, and the Action defaults to a GitHub App, which may not be allowed.
* **Sources:** Z-2.

### ACT-07. Move to a supported Go release - 🟡 Major

* **Problem:** The design treats "this repository declares Go 1.24" as fixed. It is a stated reason for rejecting two libraries, and the service image is built on Go 1.24. Go 1.26 was released on 2026-02-10 and 1.27 on 2026-08-19, so 1.24 and 1.25 no longer receive fixes. The service would face the internet, carry users' tokens, and parse archives and index files that a pull request author influences. The repository's CI matrix is pinned to those two versions under a comment that says "the two most recent Go releases".
* **Action:** in PR-48, move the CI matrix and the image to the two supported releases. The `go` line is a minimum and need not move unless a dependency requires it. Then restate the two rejections on the reasons that survive: one library still requires an MPL-2.0 module, and the other still adds more modules to inventory.
* **Needs the design owner:** this changes what the CLI is tested on.
* **Sources:** I-3, D-3, IR-17.

### ACT-08. Decide the first open after measuring it - 🟡 Major

* **Problem:** The diff waits for two whole-commit archives, although it needs only the changed files. Memory goes when Cloud Run stops an idle container, within 15 minutes, so most opens after a pause pay that wait. The diff needs two requests per changed file, so a large pull request can reach the platform's default of 80 concurrent requests, at which point a second container starts with empty memory and fetches again. The byte limits do not cover fetches in flight or the scanner's scratch files. There is no target for the time to the diff, no instance settings and no hosting cost.
* **Options:**
  * **A second read path:** draw the diff from per-file reads of the changed files, and load archives behind it for search and navigation.
  * **Settings first:** measure the cold time in E3. Set maximum instances to one and raise the concurrency limit. If the cold open is too slow, keep one instance always running and state its monthly cost.
* **Stage 8 view:** settings first. Per-file reading is already the fallback if E3 fails, and two read paths must agree on text rules.
* **Sources:** I-1, D-2, Z2-5.

## Fixes

### ACT-09. Let the conversation interface carry provider content it does not read - 🔴 Blocker

* **Problem:** The interface has four part types and four event types. None can carry the reasoning data that both providers attach to a tool call and expect back with the tool result. Google documents that its current models refuse the follow-up request when it is missing. Anthropic documents returning it unchanged as required. The tool loop is the point of PR-53 and PR-54, and the hand-written test fixtures would pass regardless. Reasoning is also billed as output, so the 2,000-output-token assumption behind the spend estimate is unsupported.
* **Action:** add an opaque provider part and event that adapters pass through unchanged, say whether the gateway scans it, and store it wherever turns are stored. Add an entry condition before PR-53: one scripted live conversation that streams, makes two tool calls and reads token counts. Use its output figure to redo the estimate. The Stage 8 reviewer asks for this with the first repository's provider only, and for the second adapter to wait until a repository needs it; the concept names both providers, so that is the design owner's call.
* **Sources:** IR-1. The design reviewer raises the same gap inside D-4.

### ACT-10. Correct what the design says about the Google adapter, and decide what the service reuses - 🔴 Blocker

* **Problem:** The design says no model name is compiled in. The existing Google adapter names models and rewrites four configured names to two preview names, and a test pins that. PR-53 reuses this code, so a repository configured with one of those names would be sent to a different model from the one its call record names. The same function builds the path for the non-streaming method.
* **Action:** correct the Background. In PR-53, say how the Google converser builds its path, whether the rewrite applies, which model name is recorded, and which existing test file changes.
* **Sources:** IR-2.

### ACT-11. Classify provider responses by their explicit code, and settle retries - 🟡 Major

* **Problem:** The rule "a rate limit with no retry time means quota is exhausted" fits neither provider. One says not every rate-limit response carries a retry time, and returns a 400 for a spend limit the account sets itself. The other lists several rate-limit codes with no retry delay, and a separate status for depleted credit. A 400 fits no outcome except "could not be reached", which would break Goal 9's promise to name quota as the cause. Stopping at the round or token limit has no outcome at all. Three statements about retries disagree: the adapters share the existing retry code; they do not retry; and the reused code is header, endpoint and redaction only. The quota forms the tests must cover are not listed.
* **Action:** map outcomes from each provider's explicit code or message, and treat an unrecognised rate limit as a rate limit. The Stage 8 reviewer proposes seven outcomes in place of twelve: answered, quota exhausted, limit reached, refused, blocked by a secret, scan failed, and one provider error that passes through the status, code and retry time. Give a table per provider and name each fixture file. State one retry behaviour in all three places.
* **Sources:** I-10, C-4, D-4, Y-6, Z2-6, IR-4, IR-9.

### ACT-12. Define the assistant's tools, prompt and limits - 🟡 Major

* **Problem:** The six tools are given by purpose only: no names, argument shapes, result shapes or size caps. The system prompt has no text. The cost estimate relies on a cap of 60,000 input and 2,000 output tokens, while the configuration gives no values and has no key for output tokens or for a call timeout. `toolRounds` is described as tool calls, which is not rounds. Whether `questionTokens` counts one model call or the whole question is not stated.
* **Action:** add a tool table, the prompt text, and the missing keys, or state which limits are fixed in code. Define `questionTokens` as the total over all rounds of one question.
* **Sources:** C-2, IR-5.

### ACT-13. Narrow Goal 7 to what its checks show, and fix the import rule - 🟡 Major

* **Problem:** Goal 7 says the service starts one program. Its test sees only direct imports of the process-starting package. Three CLI packages the service links import it themselves, and one of them starts two other programs. Separately, the rule that only the gateway may import the model package cannot hold: the gateway's own interface is written in that package's types, so three other packages must import it, and the redaction function the service uses on GitHub errors lives there too. As written the rule fails on PR-49's code and blocks PR-54.
* **Options:** test the whole dependency closure of the binary against a named list; or narrow the claim to what holds by construction. The Stage 8 reviewer favours narrowing: the image contains two executables and no shell, checked in the job that builds it, and the service's own packages never import the process-starting package. Drop the import ban, since the rule that only the binary's entry point hands out provider keys already does that work. Put GitHub token redaction in the GitHub client package, which has its own helper.
* **Sources:** Z-5, I-9, C-5, Y-2, Z2-8, IR-3.

### ACT-14. Complete the navigation rules - 🟡 Major

* **Problem:**
  * **Symbol identifiers.** The design says name parts are separated by spaces. In an index a reviewer built with the planned indexer they are concatenated, typed by a suffix, and may be escaped. The rule for "the name at the end" does not define a parser.
  * **Ties.** "The smallest occurrence" is not unique: a constant and a type with one name share a range.
  * **Constructors.** The name check rejects every `new Foo(...)` lookup, so all of them would fall to text search.
  * **The "outside the repository" label.** The indexer gives every symbol the name and version of the nearest `package.json`, the repository's own code included. The rule would therefore label the repository's own symbols as defined outside it whenever the index is partial.
  * **Several runs per commit.** A commit at the tip of two branches has two index runs, which is routine for a merge base. No rule says which wins, how GitHub's other run results map, or what state a missing or expired artifact gives.
* **Action:** quote the identifier grammar from the schema and give the name rule with examples. Define ties and constructors. For the label, the Stage 8 reviewer favours dropping it: when the index has no definition, say so and show the text matches. State a selection rule for runs, with a row for every status and result.
* **Sources:** D-6, IR-6, IR-7.

### ACT-15. Settle the secrets rule and define the rule file - 🟡 Major

* **Problem:** ADR-0012 stops the whole model lane on any detection and says the rule is never to be weakened. The service withholds one file and carries on, and the new ADR is listed as building on ADR-0012 with no argument for the change. "The service's own rule file" is undefined: the scanner's default rules carry built-in exemptions keyed on content, which would contradict "there is no allowlist". The design does not say whether a credential elsewhere in a file blocks a question about a selection in that file. Goal 8's check claims "each way content could try to switch the scanner off" where four were tested.
* **Action:** let the scanner pass in ACT-03 decide. If it finds nothing or nearly nothing to withhold, adopt ADR-0012's rule unchanged: any hit in what is about to be sent stops the send and names the file. That deletes the per-kind table, the withheld list, its event and its columns. If hits are material, keep per-file withholding and say in the new ADR that it departs from ADR-0012 and why. Either way: use the pinned scanner's default rules, narrow the claim to "no allowlist that repository content can change", list the built-in exemptions, state that a selection is scanned as its whole file, and write "each of the four attempts listed".
* **Not taken:** one reviewer asks for a flagged value to be blocked in every outgoing part. The Stage 8 reviewer advises against it: ADR-0012 rejected "redact and continue", and editing turns already sent conflicts with replaying history unchanged.
* **Sources:** C-3, D-8, Y-5, Z2-7.

### ACT-16. Four planned changes to existing packages cannot work as described - 🟡 Major

* **The GitHub client.** Every client has a 30-second timeout that also bounds reading a streamed body, while E3's proposed pass allows 60 seconds. A fetch could pass the trial and then fail in the service. Both downloads are answered by a redirect to a signed address, which a transport error would print. The paging helper decodes a bare list, and the run and artifact lists are wrapped in an object.
* **The patch parser.** It takes each range from the hunk header, which is right only with no context lines. GitHub's patches carry three: a one-line change at line 135 came back as lines 132 to 138. The plan calls these trusted line ranges. Its old-path field is already filled with the new path for patched files and left empty for a pure rename, so "a rename can be read at its old path" fails for exactly that case. Nothing in the design names a consumer of the ranges.
* **The scanner function.** The planned signature cannot express today's call, which scans one file with the working directory set to the repository root. The working directory matters to the result.
* **The licence tool.** As planned, it reads the web lockfile but does not pass the web package sets to its copyleft check, so a refused licence in a web package would pass. The design relies on that refusal in three places.
* **Action:** state how each change works and list its test cases. For the ranges: name the consumer, or drop the ranges and the dependency on the patch package.
* **Sources:** Z-7, IR-10, IR-11, IR-12, IR-13.

### ACT-17. Define the web application's addresses, its GitHub links and one line convention - 🟡 Major

* **Problem:** Goal 1 requires an address that mirrors GitHub's path, and the plan gives none. The GitHub link form for commenting at a line is not given, and GitHub documents no line anchor for the pull request files page. Lines count from 0 on every route, yet changed ranges count from 1, and the start and end lines of an excerpt, a selection and a location have no stated base. Nothing checks the web application against the running service, so an off-by-one or a wrong link passes every test.
* **Action:** add an address table, the exact link forms for added and removed lines, and one stated convention for every line field.
* **Sources:** C-12, IR-15.

### ACT-18. Give E1 an outcome for every failure - 🟡 Major

* **Problem:** E1 passes only if tokens are approved, no user is an outside collaborator, and there is one organisation. Its "otherwise" covers only which method is approved. Two facts are stated too strongly: a repository admin can by default install an App that asks only for repository permissions, and the 366-day token lifetime is a default an organisation owner can change.
* **Action:** the Stage 8 reviewer favours stating two limits of the first version in place of designing branches: one organisation's repositories, and an outside collaborator cannot sign in. Qualify both facts with "by default".
* **Sources:** S-2, Y-7.

### ACT-19. Put PR-47's workflow in the document, and give the gates values - 🟡 Major

* **Problem:**
  * **PR-47.** Its template is "the file that passed E2", and the document does not hold that file. The description omits the indexer command, the action versions and a setup step the review template uses. On a repository managed with `bun` the index job would fail on every push, because the installer's manual-step note names only the review workflow.
  * **Gates.** Work continues "only if the thresholds are met", with no values, no statement of which measures carry one, and unwritten poll questions. No work remains after PR-54 for the second feedback week to gate.
  * **Checks the goals promise.** Goal 14 names no check. Journey 10 is in no test list. The lookup time is "measured in PR-52", which has no such step. The phone trial of journey 2 is not among the hand checks.
  * **Open Questions** is said to be complete. Four open points sit outside it, and E4 is closed yet still listed.
  * **Verification rows** claim more than their tests show in five places.
* **Action:** paste the passing workflow into the plan with its template variables. For each measure give its source, whether it has a threshold, and what stops if it is missed. The Stage 8 reviewer favours closing the rest by deletion: remove Goal 14 from the goals, remove targets nobody owns, and delete the claims of completeness.
* **Sources:** C-9, S-4, S-5, Z2-9, IR-14, IR-16.

### ACT-20. Make the decisions table, the ADR table and the alternatives agree - 🟡 Major

* **Problem:** The tool-loop decision is said to be recorded in an ADR whose subject is the conversation interface, which has no decisions row and whose alternatives sit under the wrong heading. Three alternatives are filed under the wrong decision. One ADR relation is explained through another ADR's subject. Two alternatives rows say "not evaluated" and "not rejected" under a heading that promises the deciding trade-off. The chosen Postgres driver is named only inside a rejected row.
* **Two related points:**
  * **The type generator** departs from this repository's stated pattern. The plugin's protocol file says a hand-written mirror is deliberate, because a generator and its drift check are more moving parts than the drift they prevent.
  * **The stop path** is not described. By the first feedback week the repository would hold superseded ADRs, new required CI jobs and a web toolchain, and ADRs here are append-only.
* **Action:** fold the conversation interface into the tool-loop decision and refile the rows. Follow the hand-written pattern or say why twenty types change the reasoning. Add a paragraph on what stopping means.
* **Sources:** Z-6, Z-9, C-11, S-1, S-9.

### ACT-21. Complete the risks, and say what the operator can read - 🟡 Major

* **Problem:**
  * **Missing risks.** A merge base that was never the tip of a push has no index. A model statement with no excerpt behind it is shown like any other.
  * **The operator.** "Only that user can read it" holds only through the workspace's routes. The operator runs the database and its backups, and holds the cookie key that opens every sealed token.
  * **Links and rendering.** An answer can carry a link whose address contains repository text, and one click sends it to another host. The rendering rules cover model output and not the agent-written title and description. The content security policy is described for three kinds of content, while the risks table claims nothing can load from another host.
  * **Logs.** Search terms travel in the request address, which the platform's own request log records, while the design says logs never contain source text.
  * **Goal 10's check,** a narrow viewport against a fake API, cannot show that code stays visible above a phone keyboard.
* **Action:** add the rows and scope each "only" to "through the workspace's API". Show links in answers as text. Apply the answer's rendering rules to the description. Write out the whole policy. Send search terms in a request body, or state that the platform log holds them. Add a hand-run on real phones.
* **Sources:** D-9, D-10, S-3, Y-1.

### ACT-22. Correct statements about the existing system, and explain the trial - 🟡 Major

* **Problem:** The change-brief trial and one rejected alternative depend on how the CLI's model lane builds its prompt, which the Background does not describe. A reader cannot tell what "wider review context" holds or who declined the trial. The design says two of six access paths are HTTP adapters; there are four, and it does not say what happens to the two it leaves out. It says the review code drops failed checks without telling the caller; it returns a warning on every failure path, and only the cut of a long diff goes unreported. "CI has one job" reads as a single job; there are seven.
* **Action:** correct the three statements. The Stage 8 reviewer favours cutting the change-brief outline to what is settled over adding Background.
* **Sources:** C-6, S-10, Y-8.

### ACT-23. Terms, absolutes and small facts - 🟢 Nit

* **Terms:** "owner" is used three times for the organisation owner after being reserved for something else. "Intent record" and "operating guide" are used before they are identified. One label has three names. "A signed-in CLI" means another tool than "the CLI".
* **Absolutes contradicted elsewhere:** "for each pushed commit" against the tip of each push; "nothing is stored at rest" against stored conversations.
* **Quantities with no basis:** a duplicate fetch that "costs seconds"; "500 questions a week" beside dozens of pull requests.
* **Small facts:**
  * A fine-grained token has underscores inside it, so a redaction pattern copied from the existing one would leave most of the token in the text.
  * The tool install script has three callers, not two.
  * The Markdown library ships its own types, so its types package is not needed.
  * The report table's free-text note can hold code.
  * The CLI keeps a watermark file in one mode, so "stateless" is too strong.
* **Sources:** C-10, S-8, Y-9, Y-10, Z2-10, IR-18.

## Where the reviewers disagree

| Conflict | One side | Other side | Stage 8 view |
| --- | --- | --- | --- |
| The database | I-2: the same database, two pull requests later | Z-3: none in the first version | None, unless a conversation must outlive the page |
| Stored conversations | C-1, I-6, I-8: a list route, a disconnect rule, reference storage | Z-3: drop the requirement they serve | Drop it |
| The spend bound | The design: a token budget in the service | Z-3: the provider's own money limit | The provider's limit; it is softer for one provider |
| The skill sections | D-5, Y-3, C-9: harden and hand-test here | Z-8: move them out | Move them out |
| The first open | I-1, D-2: a second read path now | The design: one path, chosen by E3 | Measure, then settings |
| A flagged credential | D-8: block the value everywhere | ADR-0012: stop or withhold whole items | No value-level blocking |
| Outcomes | I-10, D-4, C-2, D-9: add three | The design: nine | Seven |
| Providers | D-4: a live run for both | "One repository first" | One provider first; the design owner confirms |
| Goal 7's test | Y-2, I-9: more source rules | Z-5, Z-7: fewer | The existing rule plus one image check |
| The fourth label | D-6: add a package matcher | Goal 2: four labels | Drop the label |
| The web editor trial | Z-1: remove navigation if it passes | I-5: keep navigation, record the score; D-7: add an option | Run the trial; the consequence is the design owner's |
| Checks and open questions | S-4, S-5: add them | The document's claims of completeness | Delete the claims |
| API types | The design: a generator | Z-6: the hand-written mirror the plugin uses | The mirror, unless a reason is given |
| The plan's detail | Stage 9: complete it | Z-4: stop until the trials are in | Trials first |

## Suggested order of work

1. Fix ACT-01.
2. Run E2 with the web-editor count, E3 with its two additions, and put E1 to the organisation owner (ACT-02, ACT-03).
3. Answer two scope questions: whether a conversation must outlive the page (ACT-04), and whether the skill sections move to their own change (ACT-05). Check which agents read the skill file.
4. Run one live conversation with the first repository's provider (ACT-09).
5. Decide ACT-06 to ACT-08 with the trial results in hand.
6. Then revise the document once: apply ACT-10 to ACT-22 to the parts that remain, and ACT-23 last, because about a third of the lines the wording findings cite will have changed or gone.

## Traceability matrix

Each finding maps to one action item. Severity is the reviewer's own.

| Finding | Stage | Severity | Subject | Action |
| --- | --- | --- | --- | --- |
| Z-1 | 5 | Major | The existing navigation tool was not tried | ACT-02 |
| Z-2 | 5 | Major | Existing agents not weighed as a way to ask | ACT-06 |
| Z-3 | 5 | Major | Postgres rests on needs with cheaper answers | ACT-04 |
| Z-4 | 5 | Major | Plan fixed before the trials | ACT-03 |
| Z-5 | 5 | Nit | The import rule fails on the design's own interfaces | ACT-13 |
| Z-6 | 5 | Nit | Type generator departs from the repository's pattern | ACT-20 |
| Z-7 | 5 | Nit | Line ranges have no stated consumer | ACT-16 |
| Z-8 | 5 | Nit | Goal 13 is outside the first version | ACT-05 |
| Z-9 | 5 | Nit | The stop path is not described | ACT-20 |
| I-1 | 5 | Major | The diff waits for two whole archives | ACT-08 |
| I-2 | 5 | Major | The database arrives before anything uses it | ACT-04 |
| I-3 | 5 | Major | Go 1.24 is out of support | ACT-07 |
| I-4 | 5 | Major | Scanner hit rate unknown until the feedback week | ACT-03 |
| I-5 | 5 | Major | The web editor row overstates one reason | ACT-02 |
| I-6 | 5 | Major | A dropped connection has no rule | ACT-04 |
| I-7 | 5 | Nit | E2 does not score through the name check | ACT-03 |
| I-8 | 5 | Nit | Conversations store excerpt text | ACT-04 |
| I-9 | 5 | Nit | The model package can start other programs | ACT-13 |
| I-10 | 5 | Nit | A self-set spend limit returns a different status | ACT-11 |
| C-1 | 6 | Major | No way back to a conversation | ACT-04 |
| C-2 | 6 | Major | Question limits have no outcome or values | ACT-12 |
| C-3 | 6 | Major | A credential elsewhere in the selected file | ACT-15 |
| C-4 | 6 | Major | Three statements about retries disagree | ACT-11 |
| C-5 | 6 | Major | Goal 7 and its check read as a contradiction | ACT-13 |
| C-6 | 6 | Major | The trial's subject is not explained | ACT-22 |
| C-7 | 6 | Major | E2's deadline and what its facts decide | ACT-03 |
| C-8 | 6 | Major | What a repository needs before connection | ACT-05 |
| C-9 | 6 | Nit | Two promised checks have no step | ACT-19 |
| C-10 | 6 | Nit | Terms used before they are defined | ACT-23 |
| C-11 | 6 | Nit | ADR relations not explained | ACT-20 |
| C-12 | 6 | Nit | Line bases and unexplained fields | ACT-17 |
| D-1 | 7 | Blocker | Cached source not bound to its repository | ACT-01 |
| D-2 | 7 | Major | Cloud Run instance behaviour | ACT-08 |
| D-3 | 7 | Major | Alternatives rejected on an unsupported Go version | ACT-07 |
| D-4 | 7 | Major | Outcome rule fits neither provider | ACT-11 |
| D-5 | 7 | Major | Skill section turns thread text into instructions | ACT-05 |
| D-6 | 7 | Major | "Outside the repository" fires for own code | ACT-14 |
| D-7 | 7 | Major | Web editor rejected on unsupported reasons | ACT-02 |
| D-8 | 7 | Major | Departure from ADR-0012; rule file undefined | ACT-15 |
| D-9 | 7 | Nit | Links, rendering and the content policy | ACT-21 |
| D-10 | 7 | Nit | Phone check and search terms in logs | ACT-21 |
| S-1 | 7 | Major | Decisions, ADRs and alternatives disagree | ACT-20 |
| S-2 | 7 | Major | E1 lacks two outcomes | ACT-18 |
| S-3 | 7 | Major | Risks table omits three risks | ACT-21 |
| S-4 | 7 | Major | Four promised checks have no step | ACT-19 |
| S-5 | 7 | Major | Open Questions is not complete | ACT-19 |
| S-6 | 7 | Nit | Overview omits the skill change | ACT-05 |
| S-7 | 7 | Nit | Reports said to be read from logs | ACT-04 |
| S-8 | 7 | Nit | "Owner" and two undefined terms | ACT-23 |
| S-9 | 7 | Nit | Alternatives rows without a deciding reason | ACT-20 |
| S-10 | 7 | Nit | Two of four HTTP adapters, not two of six | ACT-22 |
| Y-1 | 7 | Major | What the operator can read | ACT-21 |
| Y-2 | 7 | Major | Goal 7's check passes with a second way to start a program | ACT-13 |
| Y-3 | 7 | Major | Both agents reading the skill file is unsupported | ACT-05 |
| Y-4 | 7 | Major | Journey 10 states prose as an outcome | ACT-05 |
| Y-5 | 7 | Major | Goal 8's check claims more than was tested | ACT-15 |
| Y-6 | 7 | Major | Provider statements narrower than written | ACT-11 |
| Y-7 | 7 | Major | E1 branches; two facts too strong | ACT-18 |
| Y-8 | 7 | Nit | Statements about the existing system | ACT-22 |
| Y-9 | 7 | Nit | One word for two things | ACT-23 |
| Y-10 | 7 | Nit | Absolutes and unsupported quantities | ACT-23 |
| Z2-1 | 8 | Blocker | The order in which to settle things | ACT-03 |
| Z2-2 | 8 | Major | Additions around the database | ACT-04 |
| Z2-3 | 8 | Major | One navigation trial; drop a label | ACT-02 |
| Z2-4 | 8 | Major | Move the skill sections out | ACT-05 |
| Z2-5 | 8 | Major | A setting and a measurement before a second path | ACT-08 |
| Z2-6 | 8 | Major | Shrink the outcome table | ACT-11 |
| Z2-7 | 8 | Major | One scanner run decides withholding | ACT-15 |
| Z2-8 | 8 | Major | Goal 7: two checks, not five | ACT-13 |
| Z2-9 | 8 | Major | Close by deleting claims | ACT-19 |
| Z2-10 | 8 | Nit | Wording findings last | ACT-23 |
| IR-1 | 9 | Blocker | Interface cannot carry reasoning data | ACT-09 |
| IR-2 | 9 | Blocker | Google adapter rewrites model names | ACT-10 |
| IR-3 | 9 | Major | Import rule cannot hold | ACT-13 |
| IR-4 | 9 | Major | Retry statements disagree | ACT-11 |
| IR-5 | 9 | Major | Tools, prompt and limits undefined | ACT-12 |
| IR-6 | 9 | Major | Symbol identifier rules | ACT-14 |
| IR-7 | 9 | Major | Several index runs per commit | ACT-14 |
| IR-8 | 9 | Major | Conversations: no list, format or write order | ACT-04 |
| IR-9 | 9 | Major | Quota forms not listed | ACT-11 |
| IR-10 | 9 | Major | GitHub client timeout and paging | ACT-16 |
| IR-11 | 9 | Major | Patch parser ranges and old path | ACT-16 |
| IR-12 | 9 | Major | Scanner function's shape | ACT-16 |
| IR-13 | 9 | Major | Licence tool and web packages | ACT-16 |
| IR-14 | 9 | Major | PR-47's template; gates without values | ACT-19 |
| IR-15 | 9 | Major | Addresses, links and line conventions | ACT-17 |
| IR-16 | 9 | Major | Verification rows overclaim | ACT-19 |
| IR-17 | 9 | Major | Service built on unsupported Go | ACT-07 |
| IR-18 | 9 | Nit | Small facts and wording | ACT-23 |
