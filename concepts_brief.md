# Hosted review workspace for humans and AI

* **Author:** adamtait
* **Date:** 2026-11-23

## Overview

**Status of this brief:** none of the seven reviewer lenses ran (see Analysis). The parts that restate the concept are complete: this overview, the goals, the two rejected alternatives and the requester's decisions. The Analysis holds no reviewer findings. It holds one reading by the synthesizer, marked as such, which does not stand in for the lenses.

The feature is a web application for reviewing pull requests. It replaces GitHub's pull request page for daily review, and it replaces this repository's `reviewer` CLI (a stateless Go binary with a TypeScript analyzer plugin). The requester says the CLI and its documentation can be removed or archived, and that parts should be reused where they improve the new system.

The users are 10 engineers working across 4 or more GitHub repositories, with dozens of pull requests a week. More than 90% of those pull requests are written by AI coding agents, mostly Antigravity and some Claude Code. The code is mostly TypeScript, with some Python, bash and Rust. Not every engineer can see every repository.

What the application does, as requested:

- Shows each pull request or branch as a diff, or as new code only, with go-to-definition, find-references and code search. Hotkeys on desktop, touch on a phone.
- Lets the reviewer select code and ask an AI about new or existing code, generate visualizations and research alternatives.
- Runs an AI review of the plan and of the code.
- Collects the reasons for a change from the pull request title and description, plans and ADRs in the repository, and exported agent sessions.
- Posts comments and suggestions back to the GitHub pull request, and lets the authoring agent session continue from those comments.
- Learns from human reviews, and labels pull requests by risk so that human reading goes to changes that affect primary data models, workflows or user experience.
- Runs on one machine in a Google Cloud project the requester owns, using Google and Anthropic models.

The value the requester expects: the reviewer understands a change (why, how, and what it does to the rest of the system) in less time, spends reading time on the changes that carry risk, keeps a current picture of the system, and stops repeating the same review comments.

## Analysis

### What is wrong first: the seven-lens review did not run

| Lens | Status |
| --- | --- |
| `egm-design` | not run |
| `egm-codebase-details` | not run |
| `egm-zoom-out` | not run |
| `egm-comprehension` | not run |
| `egm-implementation-readiness` | not run |
| `egm-structural` | not run |
| `egm-inventive-catalyst` | not run |

The synthesizer's session exposed tools to read, search and write files. The tool that starts a subagent was not available to it. No reviewer was started, so there was nothing to retry. No lens is counted as having zero findings.

What follows from that:

- Contradictions found by reviewers: none recorded.
- Patterns suggested by reviewers: none recorded.
- Alternatives proposed by reviewers: none recorded.
- Ideal Final Result from the inventive-catalyst lens: none recorded.

This concept has had no independent review in a fresh context. The next step is to run the seven reviewers from a session that can start subagents (the main session can dispatch them directly, without a synthesizer in between) and replace the section below with what they find.

### Synthesizer's own reading (one context; not a Goldfish review)

The notes below come from one reader in one context. The synthesizer read the concept text and the repository files listed at the end of this section. The notes are not independent and have not been cross-checked. Every proposal is the synthesizer's and has not been put to the author. They are recorded so the author has something to react to while the review is rerun.

#### Risks, most serious first

1. **The server would run code from the pull request next to every credential it holds.** The concept carries one protection: install dependencies with install scripts disabled. That closes one path. Others stay open if the existing analyzers, or a TypeScript language service, run on the server against a pull request's checkout. The TypeScript plugin loads the compiler and ESLint from the checkout under review (`plugins/typescript/src/program.ts`, `plugins/typescript/src/analyzers/eslint.ts`). An ESLint flat config is a JavaScript file that runs when it is loaded. The `changed-tests` analyzer runs the pull request's own tests. Each of these runs code the pull request controls, whether or not install scripts ran. Today that code runs in a CI job limited to `contents: read` and `pull-requests: write` (ADR-0018), or on the author's machine. On the hosted machine it would run beside tokens that write to GitHub as each engineer, checkouts of repositories that some engineers cannot see, model API keys and agent transcripts. ADR-0005 already says a network sandbox "is the right answer if this were a hosted service". Most of the pull requests are written by agents, and agents can be steered by text they read, so the fact that the authors are colleagues does not remove the risk.

2. **Pull requests would merge unread on the word of a classifier that cannot yet be checked.** The concept accepts that a pull request labelled low risk may merge with no human having read it. Four things are missing.
   - Labels. Nothing records today which pull requests were high risk, so there is nothing to train on or test against.
   - A target. "High recall" has no number and does not name the class. The costly error is a high-risk change labelled low risk.
   - Volume. Dozens of pull requests a week is roughly 1,000 to 3,000 a year across all repositories, a few hundred per repository, and high-risk changes are a fraction of those. If 100 high-risk pull requests were labelled by hand and the classifier missed none, the data would support a miss rate below about 3% at 95% confidence, and nothing better. Repository-specific training cannot be shown to work for many months.
   - Authority. The concept does not say who or what approves an unread pull request when branch protection requires an approval. The existing tool was built so that it cannot "merge, approve, close or push" (ADR-0018) and never blocks a pull request (ADR-0009).

   The requester's own evidence points the other way: Meta added a time-spent guardrail to detect rubber-stamping, Duma & Wróblewski report that AI-written pull requests get little human scrutiny, and DORA 2025 reports higher delivery instability with AI adoption.

3. **The first version has no boundary.** The stated bar is that the requester stops opening GitHub's pull request page. That page also shows check results, holds pending comments until a review is submitted, submits an approval or a request for changes, resolves threads, applies suggestions, marks files as viewed and merges. Leave any of these out and the requester goes back to GitHub, so the bar is not met. Include all of them and a large build comes before any of the six stated problems is addressed; none of the six is about how GitHub handles those functions. Separately, the concept lists 14 areas of requested behaviour and does not say which belong to the first version. Several rest on mechanisms that do not exist: a link from a session to a pull request, a limit on who sees a transcript, a way to reach a session on the author's laptop for the fix loop, and a store for what is learned. One person operates the service.

4. **Agent transcripts would leave engineers' machines with no rule for who reads them.** A transcript can contain file contents from other repositories, command output, environment values, credentials, and whatever the engineer typed. The concept states that nothing limits who can see a transcript or part of one, and then proposes storing transcripts on a shared server and sending them to models. "Access mirrors GitHub repository access" does not cover a transcript: it is not a repository object, and it may include content from a repository the reader cannot see. The secrets gate that exists today scans the diff (ADR-0012); it does not scan transcripts. The session formats belong to the agent vendors and can change. Being willing to export a session is not the same as agreeing that every colleague with repository access reads all of it.

5. **Success cannot be judged as the criteria are written.** "Less time spent reviewing" needs a number from before the tool exists, and GitHub does not record time spent reading. "Fewer production issues", "easier to resolve" and "less refactoring" need production issues traced back to pull requests, which is the "landed impact" work placed after the first version; at this volume three months of incidents is also a small sample. Review recall needs a count of missed issues and has the same dependency. The reviewer's mental model, which the requester singles out as important, has no measure: the concept's evidence list says no study measures comprehension directly. Two of the listed measures can be collected by the tool once it is in use (time in view, and the share of commented lines that later change), and the Rigby & Bird count of files known can be approximated from files a reviewer has opened. None of these gives a baseline. The cited studies were not checked for this brief.

#### Contradictions, and proposals to resolve them

Each proposal below is the synthesizer's. None has been accepted or rejected by the author.

1. **Less review time against a more complete mental model.** The requester's evidence (Bacchelli & Bird; Rigby & Bird) says reading changes is how reviewers learn the system. Skipping low-risk pull requests removes that exposure.
   - Proposal: separate the two needs by level of detail. Line-by-line reading for high-risk changes only. For the rest, a summary per pull request and per week at the level of components, data models and flows, with time in view recorded as the guardrail.
   - Cost: the summary is written by a model and can be wrong, so it needs spot checks.

2. **Unread merges against no evidence about the classifier** (risk 2).
   - Proposal: separate in time, and allow errors in one direction only. Start with rules in code: path lists for schemas, migrations, workflow definitions and user-facing surfaces set a floor. A model may raise a risk label and may not lower it below the floor. Run the classifier in shadow mode, where labels are shown and nothing is skipped, until its misses have been counted against human judgement. This follows ADR-0022, which puts a ceiling in code instead of asking the model to respect one.
   - Cost: more pull requests are labelled high risk at first, so less time is saved in the first months.

3. **Navigation needs the pull request's dependencies and compiler; the server must not run pull request code near credentials** (risk 1). A second conflict sits beside it: fast switching between pull requests against the memory a warm language server per pull request would use.
   - Proposal: review is read-only, so a live language server is not required. Build a per-commit index of definitions and references (in a published format such as SCIP) inside a sandbox with no network and no credentials. The web server reads only the index. Untrusted code goes in and data comes out. All pull requests with the same base commit share one base index.
   - Cost: a build after each push before navigation is available; features that need a live compiler are limited to what the index stores; a sandbox to operate.

4. **"Go-to-definition must never be wrong" against installs with scripts disabled, and against a diff with two sides.** A definition is trustworthy only when the compiler resolved it in a program whose imports all resolved. With scripts disabled, a package that generates code or types during install (a generated database client is one example) is left without its output, and imports from it fail. The two carried assumptions do not say whether "type-checks from a fresh clone" holds with scripts disabled. A removed line must resolve against the base commit and an added line against the head commit, so each pull request needs two indexed states, and a new head state for every push. The existing plugin builds one `ts.Program` per run and does not keep it between runs, because a stale program reports on code that no longer exists (ADR-0014).
   - Proposal: show a result as a definition only when it comes from the index for a file with no unresolved imports. Otherwise label it as search results. Test `install without scripts, then type-check` on each repository before the design is written.
   - Cost: in repositories that depend on generated code, more lookups fall back to search until generation runs inside the sandbox.

5. **Intent must be visible; transcripts are large, private and not linked to pull requests** (risk 4).
   - Proposal: export an intent record instead of a transcript. It holds the human's prompts, the plan, and the decisions the agent recorded. It is extracted on the author's machine, shown to the author before upload, scanned for secrets, and linked by a session identifier written into the commit message or pull request description. The full transcript stays on the author's machine unless the author shares it.
   - Cost: extraction depends on each agent's storage format; a reviewer cannot question what was left out.

6. **The fix loop wants the original session; the service is hosted and the session is on the author's laptop.** The server cannot continue that session. Either a process on the author's machine watches for comments and resumes it, which works only while that machine is on, or the server starts a new agent from the exported record, which needs an agent runtime on the server and push access to the branch. In both cases a pull request comment becomes an instruction to an agent that can push code.
   - Proposal: use what already exists. Comments are posted to GitHub, so the author's agent can read them from GitHub on the author's machine through an agent skill. The CLI already installs one at `.agent/skills/code-review/`. The server's part is to post comments in a form an agent can parse. The design states whose comments an agent acts on.
   - Cost: the loop starts when the author, or a local watcher such as `internal/poller`, starts it. It is not automatic.

7. **Comments under the human's name against a server holding tokens that write as each person.** Posting as a person needs a token issued for that person. The existing write surface has three verbs and no review submission (`internal/ghclient/write.go`).
   - Proposal: a GitHub App. Its own identity posts the AI's own comments. Short-lived per-user tokens post human comments. An AI draft is posted under a person's name only after that person confirms the text. A hidden marker, as in ADR-0015, records which comments were drafted by the AI.
   - Cost: a sign-in flow, token storage to protect, and one confirmation per AI-drafted comment.

8. **Learning from human reviews against wrong or planted entries.** Three gaps. The AI may learn from its own output, because AI-drafted comments appear under human names. A wrong learned item is applied to every later pull request until someone removes it. Coding agents read what was learned, so text that enters through a pull request comment becomes an instruction to agents with write access.
   - Proposal: store learned items as plain files in each repository, changed only by pull request. The AI proposes an item and a human merges it. Agents already read repository files. Measure acceptance per learned item as ADR-0024 does per rule, and delete on that evidence. Where an item is a pattern, write it as an Opengrep rule with a test, using the CLI's `.review/rules/` mechanism, so no model is needed to check it.
   - Cost: slower than automatic learning; it adds small pull requests; the limits ADR-0024 records carry over (a resolved thread does not prove the comment was right).

9. **The bar of matching GitHub's page against six problems that do not need a match** (risk 3).
   - Proposal: list the pull request page functions the requester uses in a week. Mark each as rebuild or link out to GitHub. The first version rebuilds what the review loop needs (diff, comments, submitting a review) and links out for the rest.
   - Cost: the requester still opens GitHub for the linked functions, which may not meet the stated bar. That is the requester's decision.

10. **"Keep working when quota runs out" against AI in most features.** The phrase is not defined per feature. The existing provider interface is one call that returns text, with no streaming, tool use or token counts (ADR-0021), so budget limits have nothing to count.
    - Proposal: diff, navigation, search and commenting use no model. AI review results and summaries are stored per commit and shown from the store. Interactive questions switch to the other provider or wait in a queue, and the screen says which.
    - Cost: answers differ between providers; token counting has to be added to each provider adapter.

#### Conflicts with decisions recorded in this repository

- ADR-0008 (a CLI, not a service) and ADR-0016 (GitHub comments as the only store) are reversed by this concept. ADR-0008 lists what a service brings: a host, a deployment, a certificate, sign-in, a secret store, upgrades and someone to respond when it stops. The design document should supersede both ADRs by name, and say which data keeps GitHub as its record (comments, review state) and which exists only in the new store (indexes, intent records, learned items, risk labels, usage data).
- ADR-0007 (no finding outside the changed lines) can stay as a rule for posted comments. It cannot stay as a rule for what the AI reads. Answering questions about existing code needs the whole repository, and `internal/laneb/gather.go` reads only the diff, the guidance files named in config, and markdown files beside changed files.
- ADR-0012 guarantees that no diff reaches a model once a credential is found. It holds today because one path leads to a model. The concept adds several: questions, visualizations, research, session intake and review. The guarantee needs one exit point that all of them pass through, and it needs to cover session content.
- ADR-0021's two subscription paths (`claude-code`, `codex`) do not run on a server (`docs/providers.md`). Only the API paths apply.
- Candidates for reuse: `internal/ghclient` (read calls), `internal/fingerprint` (ADR-0015), `internal/diff`, `internal/gate`, `internal/laneb` (review, the disprove pass of ADR-0023, the category caps of ADR-0022), `internal/model` (the HTTP adapters), `internal/poller`, and the acceptance measurement of ADR-0024. `plugins/typescript` type-checks; it does not answer navigation queries.
- The CLI serves three places today: CI, the agent before it pushes, and the engineer's machine (ADR-0008). The web application serves none of them. If the CLI is archived, the repositories lose deterministic checks in CI and the agent's review before push unless something keeps them. With most pull requests written by agents, the agent's own loop is the cheapest place to fix lint and type findings.

#### Secondary trade-offs

- On a phone, selecting code to ask the AI and tapping a symbol to navigate compete for the same touch input. A side-by-side diff does not fit a phone held upright.
- A cached permission goes stale when GitHub access is removed; checking on every request costs API calls. Stored AI answers and summaries need the same per-repository check as the code they describe.
- One machine holds a checkout and installed dependencies per open pull request and an index per pushed commit. It needs an eviction rule. The first open of a pull request is slow unless the build starts when the push arrives.
- Python and bash have no compiler-grade definition lookup, so "never wrong" falls back to search more often there.
- When the service is down, review falls back to GitHub. That works only if GitHub stays the record for comments and approvals.
- Plans and pull request descriptions are mostly written by agents. Checking code against the agent's plan shows that the two agree, not that the plan matches what the human wanted. The human's prompts carry the human's intent.
- The concept does not record which existing tools were tried against problems 2 and 3 (pull request extensions for an IDE, hosted code-navigation indexes, AI review products) or why they fell short. The author has not rejected them, so they are not listed under Alternatives Weighed.
- Problem 5 (review as a shared experience across the whole process) maps to intent context in the first version and to the follow-on work after it. No other requested behaviour addresses it.

#### Ideal Final Result (provisional; the inventive-catalyst lens did not run)

A change arrives with its reason, plan and risk level attached. The reviewer sees its effect on components, data and flows before reading any line. Navigation answers come from the compiler or are labelled as search. A low-risk change costs the reviewer no line reading and still updates the reviewer's picture of the system. Anything a reviewer says twice becomes a file the coding agent reads before it writes, so the comment is not needed a third time. In the ideal case most review happens before the pull request exists.

#### Files the synthesizer read

`README.md`; `docs/architecture.md`; `docs/providers.md`; ADRs 0005, 0006, 0007, 0008, 0009, 0012, 0014, 0015, 0016, 0018, 0021, 0022, 0023, 0024; `internal/ghclient/client.go` and `write.go`; `internal/laneb/gather.go` and `categories.go`; `internal/model/provider.go`; `internal/poller/poller.go` (first 70 lines); `plugins/typescript/src/program.ts`. Other files, including ADR-0002 and `docs/implementation-plan.md`, were not read.

## Goals vs. Non-Goals

* **In-Scope Goals:**
  * A web application that works with hotkeys on desktop and by touch on a phone.
  * A view per GitHub pull request or branch, with quick switching between open ones. First-version bar: the requester stops opening GitHub's pull request page.
  * Diff display by default, with an option to show only the new code.
  * Go-to-definition, find-references and code search for TypeScript. Go-to-definition shows search results or "not found" when it is not confident.
  * AI assistance: select code and ask; questions about new or existing code; visualizations; research into alternatives.
  * An AI review of the plan and of the code changes.
  * Intent context from the pull request title and description, plans and ADRs in the repository, and exported agent sessions.
  * Inline and whole-change comments and suggestions posted to the GitHub pull request, under the human's name or the AI's bot name as the requester specified.
  * A fix loop in which the authoring agent session, or a fork of it, reads a comment, makes the change and responds.
  * Access that mirrors GitHub repository access.
  * An AI reviewer that learns from human reviews, with learned items that anyone on the project can see, correct and remove, and that coding agents can read.
  * Risk labels for pull requests, so humans fully understand changes to primary data models, workflows or user experience.
  * Hosting on one machine in a Google Cloud project the requester owns and operates.
  * Google and Anthropic models, configurable budget limits, and continued operation while token quota is temporarily exhausted.
* **Out-of-Scope Non-Goals:**
  * Planning, deployment and landed impact in the first version. The requester places them after it, as a quick follow-on.
  * Navigation for languages other than TypeScript in the first version. They come later as extensions or plugins.
  * OpenAI models at the start. They are possible later.
  * Session support for Codex and other agents at the start. They may come later as plugins.
  * A shared service run for other teams at the start. It may come later.
  * Local-only storage as a requirement. Local storage is optional for capable devices.
  * Extending the existing CLI as it stands.
  * A hard spending cap. The requester states there is no hard budget.

The concept does not say which in-scope items belong to the first version beyond the stated bar. That is listed as an open question below.

## Alternatives Weighed

1. **Local-first storage as a requirement.** Rejected by author: a phone cannot hold checkouts or run language servers, so local storage is optional for capable devices.
2. **Extending the existing CLI as it stands.** Rejected by author: the author wants a new system that reuses parts of the CLI where they help.

No other option has been rejected. The proposals in the Analysis are proposals.

## Key Decisions & Staging for Next Steps

### Decisions the requester has made

1. The product is a web application. Desktop review and navigation hotkeys must work. It must work on a phone, with code navigation by touch.
2. GitHub is the primary source of changes. Each pull request or branch has its own view, and switching between them is quick.
3. The bar for the first version is to replicate the GitHub pull request experience, so the requester stops opening GitHub's page.
4. The diff is shown by default, with an option to show only the new code.
5. Navigation is go-to-definition, find-references and code search, TypeScript first and other languages later as extensions or plugins. Go-to-definition must never be wrong: when it is not confident it shows search results or says it found no definition.
6. AI assistance covers selecting code and asking about it, questions about new or existing code, visualizations and research into alternatives. The AI also does its own review of the plan and of the code changes.
7. Intent comes from the pull request title and description, plans and ADRs in the repository, and exported agent sessions. Users are expected to export their sessions.
8. Comments and suggestions are placed inline or on the change as a whole and are posted to the GitHub pull request. A human's name appears on comments the human wrote and on AI-written comments the human asked for. A bot name appears on the AI's own comments.
9. After comments, the original agent session or a fork of it continues, reads the comment, makes the change and responds.
10. Access mirrors GitHub repository access.
11. The AI reviewer learns from human reviews. Anyone working on the project can see, correct and remove what was learned. Coding agents should be able to read it. Where it is stored is left open.
12. High risk means a change that affects primary data models, workflows or user experience, and humans must fully understand such changes. A classifier with repository-specific training and high recall labels pull requests as low risk. A low-risk pull request may merge with no human having read it.
13. The service runs first on one machine in a Google Cloud project the requester owns and operates. It may later be a shared service. Context, indexes and settings are stored on the server; local storage is optional.
14. Models are Google and Anthropic first, with OpenAI possible later. Budget limits are configurable. There is no hard budget. The tool must keep working when token quota runs out temporarily.
15. Planning, deployment and landed impact follow the first version.
16. The existing CLI and its documentation can be removed or archived. Parts are reused where they improve the new system.
17. Two assumptions are carried: the repositories install and type-check from a fresh clone with no private registry or secrets, and the server installs each pull request's dependencies with install scripts disabled.
18. Success is judged after three months on six criteria: less review time; review quality that stays high and improves; more review time on system evolution and less on implementation detail; fewer production issues that are easier to resolve; less refactoring; and a more thorough and current mental model of the system.

### Open questions before a design document is drafted

No reviewer raised any question, because no reviewer ran. The first step is to run the seven lenses. The questions below are the synthesizer's and are provisional; the lenses may replace or add to them.

1. **First-version scope.** Which requested behaviours are in the first version? Which GitHub pull request page functions must be rebuilt for the requester to stop opening the page, and which may link out?
2. **Isolation.** Where does pull request code run (install, type-check, lint, tests, index build), and with what credentials and network access? Do the existing deterministic analyzers run on the server at all?
3. **Install without scripts.** Does each repository install and type-check with install scripts disabled? This can be tested now.
4. **Risk.** What is "high risk" in terms a program can check? What is the recall target, and for which class? Where do labels come from? How long is the shadow period? Who or what approves a pull request nobody read, and which human is accountable for it?
5. **Sessions.** What leaves the author's machine: the whole transcript or an extract? Who can read it? How is a session linked to a pull request? How are secrets found and removed? How long is it kept? Who must approve storing these repositories' code and transcripts on this machine and sending them to two model providers, and has that approval been given?
6. **Fix loop.** Where does the continuing agent run? Whose comments can start it? Is it in the first version?
7. **GitHub identity.** Is this a GitHub App with per-user tokens? Where are the tokens kept? Can the AI's own identity approve a pull request?
8. **Learning.** Where are learned items stored? Does an item enter automatically or after a human approves it? How are AI-drafted comments kept out of "human reviews"? How is improvement measured?
9. **Measurement.** Which measures will be used, and how is a baseline captured before the tool is in use? How will the mental-model criterion be judged?
10. **Quota.** What does "keep working" mean for each feature when a provider's quota is exhausted?
11. **The existing tool.** What happens to the CLI's three uses (CI, the agent before push, the engineer's machine)? Which ADRs are superseded and which carry over? Does the server stay in Go?
12. **Operations.** Who responds when the service is down? What are the storage and eviction limits on one machine? Is use by other teams a non-goal for the first version's design?

### Staging

1. Run the seven reviewers from a session that can start subagents, and merge their findings into the Analysis in place of the synthesizer's reading.
2. The author answers the open questions that remain after that review.
3. Draft the design document from this brief once steps 1 and 2 are done.
