# Hosted review workspace for humans and AI

* **Author:** adamtait
* **Date:** 2026-11-23

## Overview

A web application for reviewing pull requests, used by 10 engineers across 4 or more GitHub repositories. The team opens dozens of pull requests a week, and coding agents write more than 90% of them (mostly Antigravity, some Claude Code). The code is mostly TypeScript, with some Python, bash and Rust.

The first version augments GitHub's pull request page and does not replace it. It adds what GitHub lacks, code navigation and interactive AI help, and reviewers keep using GitHub to comment, approve and merge. The aim is to put those new features in front of the team quickly and collect feedback. This repository's `reviewer` CLI stays as the part that runs in each repository's CI job, and the new system is built in this repository beside it.

It addresses six problems the requester has with review today:

1. It is hard to build a mental model of a change: why it was made, how, and what it does to the rest of the system.
2. GitHub's diff is plain text, with no go-to-definition, no find-references and no view of how changed files relate.
3. There is no way to ask an AI about new or existing code while reviewing.
4. The agent session that produced a change, and the plan inside it, cannot be seen.
5. Review is only comments and approvals. The reviewer is not included in the goals, alternatives, plan and outcome of a change.
6. Reviewing does not get easier or more effective with use.

What the requester asked for:

- A view per pull request or branch, showing the diff or only the new code, with go-to-definition, find-references and code search. Hotkeys on desktop, touch on a phone.
- AI help: select code and ask about it, ask about existing code, generate visualizations, research alternatives.
- An AI review of the plan and the code.
- The reasons for a change, drawn from the pull request title and description, plans and ADRs in the repository, and exported agent sessions.
- Comments posted back to GitHub, and a fix loop in which the authoring agent picks up comments.
- An AI reviewer that learns from human reviews.
- A risk label per pull request, so human reading goes to changes that affect primary data models, workflows or user experience.

Seven independent reviewers examined the original concept, which set out to replace GitHub's page. They found six problems that block a design. The author has since decided on each one. The Analysis records the findings and their outcome, and the last section lists the decisions a design document should start from.

## Analysis

### Review status

| Lens | Reviewer | Status |
| --- | --- | --- |
| Architecture, cost and quota | `egm-design` | completed |
| Operations and debugging | `egm-codebase-details` | completed |
| Scope and simplicity | `egm-zoom-out` | completed |
| Usability | `egm-comprehension` | completed |
| Migration and rollout | `egm-implementation-readiness` | completed |
| Long-term maintenance | `egm-structural` | completed |
| Contradictions and inventive patterns | `egm-inventive-catalyst` | completed |

Each reviewer ran in a fresh context and saw only the requester's stated requirements and this repository. None saw the others' reports. Every proposal below is a reviewer's proposal. The next section says which ones the author has accepted.

Token, memory and volume figures are reviewers' estimates under the assumptions stated beside them. They are not measurements.

### Outcome of the review

The author answered the reviewers' open questions on 2026-11-23. The problems are kept below as the reviewers found them, because their requirements still apply to the design.

| Blocking problem | Outcome |
| --- | --- |
| 1. Pull request code beside credentials | Resolved by a design choice: each repository's own GitHub Actions job builds the navigation index. The server never installs or executes pull request code. |
| 2. Unread merges on an unvalidated classifier | The author accepted the proposal: a written risk list, labels in shadow mode, and unread merges only after a numeric exit criterion per repository. |
| 3. No first-version boundary | The author changed the bar: the first version augments GitHub with navigation and interactive AI. GitHub keeps commenting, approval and merge. |
| 4. Private data crossing two boundaries | Still a design requirement: one outbound door for model calls, and a permission check on every read. Only the provider configured for a repository may receive its code. |
| 5. Agent sessions with no workflow | The author accepted the proposal: the agent writes an intent record into the pull request, and the author's side pulls fixes on an explicit human action. |
| 6. Review state in three places | Mostly avoided in the first version, which writes no human comments to GitHub. The ownership rules apply from the stage that adds commenting. |

### Blocking problems

#### 1. The server would run code from the pull request beside every credential it holds

Raised by four lenses (architecture, migration, maintenance, inventive).

The concept carries one protection: install each pull request's dependencies with install scripts disabled. That closes one path. The components proposed for reuse open several more:

- The TypeScript plugin loads `typescript` and `eslint` from the checkout's own `node_modules` (`plugins/typescript/src/program.ts`, `plugins/typescript/src/analyzers/eslint.ts`). A pull request controls `package.json`, so it controls what those names resolve to.
- The ESLint analyzer executes the checkout's `eslint.config.js`. The `changed-tests` analyzer runs the pull request's tests.
- `.review/config.yaml` is read from the checkout. Its plugin commands are executed as written, and its `github.apiBaseUrl` decides where the GitHub token is sent.
- Every plugin process inherits the host's whole environment. The only separation is a process group.

This was acceptable in a short-lived CI job whose token could only read contents and write comments (ADR-0018). ADR-0005 already says a no-network sandbox "is the right answer if this were a hosted service". The hosted machine would hold a GitHub credential for every engineer, model keys, agent transcripts, and checkouts of repositories some engineers cannot see. With most pull requests written by agents, one poisoned dependency or one injected instruction is enough. No malicious engineer is needed.

**Proposed resolution.** Split the system into two parts joined by a data-only interface.

- A control part holds credentials and the store, and talks to GitHub and model vendors. It never loads or executes repository content.
- Disposable workers clone, install, type-check and index one commit at a time. They hold no credentials, have no network beyond a package registry proxy, and return only data: an index and findings.
- Nothing in the pull request chooses what executes. Commands, endpoints and tool versions come from server settings or the base branch, never the pull request head.
- Running a pull request's tests on the server is out of the first version.

A variant removes server-side execution entirely: each repository's own CI job, which already has a checkout with dependencies, builds the index and uploads it. The server then downloads data and nothing else.

**Cost.** Navigation is unavailable for some minutes after each push. Index files need storage. The index is written by pull request code, so the server must parse it defensively. The CI variant depends on GitHub Actions being enabled and spends CI minutes.

#### 2. Pull requests would merge unread on a classifier that cannot be validated

Raised by all seven lenses.

- **No labels exist.** Nothing records today which pull requests were high risk, so there is nothing to train on or test against.
- **The volume cannot prove recall.** With zero observed misses, showing a miss rate under 5% needs about 60 known high-risk pull requests per repository, and under 1% needs about 300. At this team's volume the first figure takes roughly six months per repository, which is outside the three-month window.
- **The design hides its own misses.** A high-risk change labelled low risk is never read, so it never gets a correct label. Measured recall drifts upward.
- **The party being classified writes the input.** Title, description and diff come from the agent that wants to merge. Author, reviewer and classifier may share a model family and its blind spots.
- **The target is ambiguous.** "High recall to label pull requests as low risk" can be read two opposite ways. The costly error is a high-risk change labelled low risk.
- **The effect of the label is unstated.** The concept does not say who or what approves an unread pull request, or how branch protection is satisfied. The existing tool was built so it cannot approve or merge.
- **The inherited failure mode is fail-open.** In the existing model lane a provider outage becomes "found nothing", and a diff over 120 KB is partly read. Both are tolerable for advice and unsafe for a gate.
- **It works against a stated goal.** The requester wants a more thorough mental model, and the cited research says reading changes is how reviewers learn a system.

**Proposed resolution.**

- A written list per repository of paths and patterns that always need a human: schemas and migrations, authentication, exported data-model types, workflow and route definitions, user-facing components, dependency manifests, CI and deploy configuration, and the review rules themselves. It is deterministic, and a miss is fixed by adding a line.
- A model may raise a risk label above that floor and may never lower it. This is the one-directional cap ADR-0022 already uses.
- "Could not classify", "diff truncated" and "provider unavailable" are shown as unclassified and treated as needing a human.
- The first version runs in shadow mode: labels are shown and recorded, and nothing skips review. Leaving shadow mode needs a numeric exit criterion per repository.
- After that, a permanent random sample (the architecture lens suggests 10–20%) of low-risk pull requests goes to full human review, to keep producing correction data.
- Each label is bound to a head commit, classifier version and inputs. A new push clears it. A durable list records every pull request merged with no human review, so a bad classifier version can be followed by targeted re-review. One switch per repository returns to "humans decide".
- Every revert or incident traced to a low-risk pull request becomes a new line in the list.
- Pull requests nobody reads still feed a digest per subsystem, so knowledge of the system keeps spreading.

**Cost.** The list over-flags at first, so early time savings are small. The list goes stale as code moves. Audit sampling is a permanent reviewer cost.

#### 3. The first version has no boundary

Raised by four lenses (scope, usability, maintenance, architecture).

The stated bar is to replicate GitHub's pull request page so the requester stops opening it. That bar is defined by another product and is all-or-nothing: the requester keeps opening GitHub until the last missing function exists. The concept does not list those functions. The requester's own workflow ends with "approves or requests changes", which appears nowhere in the requested behaviour. Also absent are replying to and resolving threads, seeing comments made directly on GitHub, CI status, changes since the last review, marking files viewed, and merging.

None of the six problems is about how GitHub handles those functions. Meanwhile the concept contains about thirteen subsystems, does not assign any of them to a version, and has one operator.

"Changes since my last review" matters more here than on GitHub. A fix loop pushes new commits after every round of comments, so each pull request is revisited several times.

**Proposed resolution.**

- Replace "replicate GitHub" with a closed list of reviewer tasks, each marked as in the first version or deferred. Every deferred task gets a one-click link to the exact place on GitHub.
- Name a core: diff, comments, navigation and GitHub sync. The core works with every other capability switched off and with no model vendor reachable.
- Every other capability is a module that depends on the core through a declared interface, and never on another optional module.

The scope lens goes further and proposes not replacing GitHub at all in the first step. See "Where the reviewers disagree".

#### 4. Private data would cross two boundaries with no single point of enforcement

Raised by five lenses (architecture, inventive, operations, usability, migration).

**To model vendors.** Today one path leads to a model and one gate stands in front of it (ADR-0012). That gate inspects only changed lines. The concept adds six or more places that call a model, and they send unchanged files and agent transcripts. A transcript holds whatever the agent saw: command output, environment values, files from other repositories, pasted tokens. With many call sites the guarantee becomes a check each one must remember, which is the shape ADR-0012 rejected.

**To colleagues.** "Access mirrors GitHub" covers the pull request page. It says nothing about derived data. A transcript, a stored AI answer or an index can show material from a repository the viewer cannot read.

**Through the AI.** The existing defence against instructions hidden in a diff assumes one question and no tools. An AI that can search repositories and post under a human's name can be steered by text in a diff or transcript.

**Proposed resolution.**

- One outbound door for all model calls. It is the only component holding model keys. It scans the full payload for secrets, refuses to send when it cannot scan, logs what was sent, and enforces a list of permitted vendors per repository.
- Each commit's files and each transcript are scanned once on arrival, with the result stored by content hash, so interactive questions do not wait for a scan.
- Every stored record carries a repository identifier, and every read is checked against the requesting user's current GitHub permission.
- AI tool calls run with the requesting user's permissions, never the server's.
- Text is posted under a human's name only after that human confirms the final wording.

**Cost.** Every AI feature gains one more hop and a single point of failure. Failing closed will visibly switch AI off for some pull requests and transcripts.

#### 5. Agent sessions have no workflow in either direction

Raised by six lenses (usability, scope, maintenance, migration, inventive, architecture).

**Inbound, for intent.**

- Sessions sit on each engineer's laptop in two private formats that other teams own and can change without notice.
- Nothing links a session to a pull request.
- A per-pull-request export step will be skipped at this volume, and the pull requests with missing sessions will not be a random sample.
- The reviewer cannot tell "no plan existed" from "nobody exported it".
- One bad exposure of a transcript stops the team exporting.

**Outbound, for fixes.**

- A hosted server cannot resume a session on a laptop, and an exported transcript is a record, not a resumable session.
- If each comment triggers a fix, a ten-comment review means ten agent runs and ten pushes under a reviewer who is still writing.
- AI comments feeding an AI fixer with no human between them can cycle without limit.
- A comment becomes an instruction to an agent with push rights.

**Proposed resolution for intent.**

- Have the agent supply intent when it opens the pull request. `reviewer init` already writes an agent skill into each repository. That skill can tell agents to write the goal, plan, rejected alternatives and a session identifier into the pull request description or a plan file. The text then inherits GitHub's access rules and history, and the linking problem disappears.
- Where more is wanted, an exporter on the author's machine extracts an intent record: the human's prompts, the plan and the recorded decisions. The author previews it, it is scanned for secrets, and the full transcript stays on the author's machine.
- The system defines its own versioned session record. One importer per agent tool is the only code that knows a vendor format.
- "No session" is a supported and common case. The pull request view always says one of: attached, none attached, or attached but not visible to you.

**Proposed resolution for fixes.**

- The GitHub review thread is the queue and the record. The comment is the work item, the agent's commit and reply are the result, and resolving the thread is the acknowledgement.
- The author's side pulls the work, on an explicit human action applied to a submitted batch of comments. The server neither runs agents nor pushes in the first version.
- AI-written comments never trigger a fix by themselves. Automatic cycles per pull request are capped (two is the suggested start).
- Each thread shows its state: requested, picked up, pushed, declined with a reason, or no agent available. When no agent is available, the tool offers the comments formatted for pasting into one.

**Cost.** A plan written by the agent is a self-report and may differ from what the session did. An extract loses detail. Nothing happens while the author's machine is off.

#### 6. Review state would live in three places with no declared owner

Raised by four lenses (maintenance, architecture, migration, operations).

The concept keeps comments on GitHub, adds a server store, and allows optional storage on capable devices. GitHub's copy changes without the tool: people and agents edit, resolve and force-push there. GitHub rejects a comment anchored to a commit that is no longer the head. Keeping copies aligned needs a two-way sync with retries, de-duplication and conflict rules.

The existing tool avoided all of this by making GitHub the only store (ADR-0016), and that ADR says the answer to scale is a read-only cache, "not a move of the source of truth". The existing GitHub client holds one token, polls, treats a rate limit as an error, and cannot submit a review.

Once engineers stop opening GitHub's page, a stale workspace is invisible to its user, and the uptime of one machine becomes the team's review uptime.

**Proposed resolution.**

- GitHub stays authoritative for everything it can represent: comments, suggestions, review verdicts, thread resolution and permissions.
- The server is authoritative only for what GitHub cannot hold: session records, learning candidates, indexes, risk labels, unsent drafts, AI conversations and the usage ledger.
- Every store is classified as derived (safe to drop and rebuild) or authoritative (backed up, schema-versioned, with a restore tested before data exists only there).
- One sync component owns all GitHub reads and writes, and its operations are safe to repeat. It uses a GitHub App with per-user identity, webhooks plus a periodic reconcile, and submits comments as one batched review.
- Every pull request view shows the commit on screen and when it last synced, and warns before a comment or approval is sent against a commit that is no longer the head.
- Every outbound write has a visible delivery state: pending, delivered, or failed with the reason.
- Organisation and repository identifiers go on every record from the first version, so a later shared service is not a rewrite.
- Device storage is a disposable read cache.

### Major problems

#### 7. "Go-to-definition must never be wrong" needs a type-checked copy of every commit

Raised by all seven lenses.

- **The existing plugin is not a navigation backend.** It builds one batch program per run, handles a single `tsconfig.json`, returns findings only, and discards the program because a stale one "reports findings about code that no longer exists" (ADR-0014).
- **A live language service per open pull request does not fit one machine.** The architecture lens estimates about 1 GB each, so about 30 GB for 30 open pull requests, plus 0.3–1.5 GB of dependencies per checkout.
- **Disabled install scripts will remove generated types.** Database clients and generated API types are often produced during install. Those are the data-model types the requester cares most about. The two carried assumptions have not been tested together.
- **A diff has two sides.** A removed line belongs to the base commit and an added line to the head.
- **The three outcomes must look different.** Otherwise a text match is mistaken for a definition.

**Proposed resolution.**

- Build an immutable index once per push, keyed by repository, commit, lockfile hash and indexer version. Serve it from storage, so a phone needs no live process. A commit never changes, so the staleness that ruled out caching in ADR-0014 cannot occur.
- Every answer carries the commit it was computed for and how it was obtained: resolved by the compiler, text search, or not found. Only the first may be shown as a definition. "Never wrong" becomes a property of the data, not of interface logic.
- Never answer from an index whose commit differs from the commit on screen.
- Show navigation status per pull request: ready, preparing, or unavailable for named files with the cause. Text search works while navigation is preparing.
- Navigation from a removed line uses the base version. Back and forward follow browser history, and one action returns to the diff.
- New languages arrive through an established open standard for language servers or code indexes, not by extending the analyzer protocol.
- Before scope is fixed, run "install without scripts, then type-check" on each repository and record the unresolved imports.

The same index also answers "which unchanged code calls the code this change touches", which is the impact view the first stated problem asks for.

#### 8. The existing model interface cannot meter, stream, or survive a quota window

Raised by six lenses.

- The provider interface is one call that returns text. It has no streaming, no conversation turns, no tool use and no usage figures (ADR-0021), so a budget has nothing to count.
- It retries for about three seconds and gives up, which is shorter than any per-minute quota reset. The review then reports that it found nothing.
- Two of its six access paths drive a signed-in personal CLI and cannot run on a server.
- The architecture lens estimates input tokens rising from about 12 million a week today to 150–200 million with raw transcripts, interactive questions and fix-loop runs. That assumes 40 pull requests a week and 4 pushes each. Batches of agent pushes can exhaust a per-minute quota and starve interactive questions.

**Proposed resolution.**

- The outbound door from problem 4 is also the model gateway. It owns vendor choice, retries, budgets and a usage ledger recording tokens per call with user, repository and feature.
- Diff, navigation, search and commenting need zero tokens.
- The AI review, change summary and risk label are computed once per commit and stored. During an outage the stored result is shown with its commit and model named.
- Three priority classes (interactive, review, background), with a share of per-minute quota reserved for interactive use. Background work waits in a durable queue and resumes when quota returns.
- Every AI surface shows one of: not started, running, finished with results, finished with nothing found, or could not run with the reason and what happens next.
- A transcript is summarised once at intake into a short intent digest. The raw text is fetched only on demand.

#### 9. The learning loop can learn from itself, be edited by the change under review, and has no signal

Raised by six lenses.

- **Contamination.** AI-drafted comments posted under a human's name look like human review. So do the old CLI's comments wherever its poller posted with a personal token.
- **Self-editing.** Guidance is read from the pull request's own checkout, under a config the pull request can change. A change can weaken the rules it is reviewed against.
- **Growth.** Guidance shares a 24 KB budget that cuts off in config order, so newer learnings silently vanish.
- **A third home for the same thing.** Team conventions already live in pattern rules under `.review/rules/` and in guidance documents named in config.
- **No signal.** ADR-0024 records that half the model's categories, including architecture judgement, have no outcome signal, and that "resolved" does not mean "right".
- **Re-learning.** Without a record of where a learning came from, one that someone removes is learned again from the same old comment.

**Proposed resolution.** Two stages in two places.

- Candidates are captured automatically into an inbox tied to the repository. The AI proposes them from human comments when a review is submitted.
- A human accepts a candidate in one action. Accepted learnings become files in the repository, added by an ordinary batched pull request, and are read from the base branch only.
- Each learning is a short plain statement with an identifier, a link to the comments it came from, who accepted it, its scope (one repository and a set of paths by default), its date and how often it has been applied. Removals are kept so a removed learning is not learned again.
- Each accepted learning is converted to its cheapest form. "Do not re-implement vector similarity" plausibly fits a pattern rule with test cases, which needs no model. "How subsystem X works" becomes a note included only when a change touches those paths.
- Every AI-written comment carries a machine-readable origin marker, including those posted under a human's name. The existing `rv:` markers are parsed permanently and treated as machine-written.
- Every AI comment that relied on a learning names it and links to it, with "this is wrong" one action away. Learnings are retired on that evidence.

Files in the repository meet every property the requester listed: anyone on the project can see, correct and remove them, coding agents read them without a new client, and access follows the repository.

#### 10. The comment model leaves four things undefined

Raised by the usability lens, with support from architecture and migration.

- **When a comment leaves the tool.** GitHub's convention is a pending batch submitted once. Posting each comment at once sends a notification per comment.
- **Who confirms AI-written text.** If one tap posts, text the human never read is published as theirs. This is most likely on a phone.
- **Where a comment goes when GitHub cannot anchor it.** Navigation takes the reviewer into files the pull request does not touch, and GitHub has no anchor there. A branch with no pull request has nowhere to post at all.
- **Whether the AI's own review goes to GitHub** automatically, on request, or never.

**Proposed resolution.** Comments are held as a pending batch per user on the server, with one submit. AI-drafted text always lands in an editable draft. Every annotation shows its author kind (human, AI on request, AI's own, tool) and its state. A comment outside the diff becomes a whole-change comment carrying a permanent link to the file and line, and the user is told before posting.

#### 11. Touch and keyboard input are required but unspecified

Raised by the usability lens.

Three actions want the same gesture on a line of code: place a comment, navigate, and select text to ask about. Touch has no hover, modifier-click or right-click, and long-press belongs to the system's own text selection. A phone held upright cannot show a side-by-side diff, and the on-screen keyboard hides the code being asked about. On desktop, browsers reserve keys an IDE user will reach for, and single-letter shortcuts fire while a comment is being typed.

**Proposed resolution.** One table mapping each core action to a keyboard shortcut and a touch gesture. On touch, tapping a token or line number opens one action sheet. Narrow screens use a unified diff. Single-key shortcuts are inactive while a text field has focus. A command palette reaches every action.

#### 12. The service has no way to be observed, and inherits a design that hides failure on purpose

Raised by the operations lens.

The existing tool was designed so there is nothing to operate, and it exits 0 whatever happens. Its diagnostics are free text on standard error with no user, pull request or request identity. A search of the Go sources found no structured logging, tracing or metrics library. The concept names no logs, health checks, alerts or owner.

**Proposed resolution.**

- Structured logs carrying a request identifier, user, repository, pull request and commit, and never source code or transcript text.
- A health check per dependency, and an alert that fires when no sync has succeeded for a set time.
- A status panel per pull request showing each component's state: synced, indexed, AI review done, skipped or failed, with the reason.
- One record per model call: feature, user, repository, commit, vendor, model, an inventory of what was sent, the scan decision and the outcome.
- An access log for transcript views, and a change log for learnings.
- A named person who receives alerts, and a tested backup and restore.

#### 13. Success cannot be judged as the criteria are written

Raised by the operations lens. See "Measuring success" below for the proposed measures.

- GitHub does not record time spent reading, so "less review time" has no figure from before the tool unless one is captured now.
- Production issues, ease of resolution and refactoring all need incidents traced back to pull requests. That is the "landed impact" work placed after the first version, and three months of incidents at this volume is a small sample.
- The mental-model criterion has no measure. No study was found that measures a reviewer's comprehension directly.

#### 14. There is no plan for what the CLI already installed

Raised by the migration and maintenance lenses.

`reviewer init` leaves seven tracked files and a devDependency in each reviewed repository. "Removed or archived" fails in two different ways:

- **Remove.** The generated workflow downloads its binary from this repository's releases. Deleting releases, renaming the repository or making it private turns the `review` check red on every pull request. Unpublishing the plugin's npm package breaks `npm ci` wherever a lockfile names it.
- **Archive.** The old workflow keeps commenting beside the new AI reviewer: two bots, duplicate findings, and model spend outside the new budget.

Neither option touches the agent skill, which tells coding agents to run `reviewer` before every push, or any local poller an engineer set up.

Reuse also imports assumptions:

- A finding's identity is six hex digits, safe only within one pull request's comments. As a key in a central store, two different findings have an even chance of sharing an identity at about 4,800 records.
- A finding must have a file and a line. Plan reviews and whole-change comments do not fit.
- The core discards anything outside the changed lines (ADR-0007). That can stay as a rule for posted comments, and it cannot stay as a rule for what the AI reads.

**Proposed resolution.**

- Inventory each repository: workflow, skill, devDependency, poller, secrets, and whether `review` is a required check.
- Fix the order. The new service proves itself on one repository. Then one pull request per repository removes the workflow, skill and devDependency. Releases and the npm package are touched last, and the package is deprecated, never unpublished.
- Decide whether the new system lives in this repository or a new one. This repository is intended to be public (ADR-0010), so deployment details and samples of session formats must stay out of it either way.
- New ADRs supersede ADR-0008, ADR-0016 and ADR-0018. The ADR log is append-only and a required CI job enforces that.

#### 15. The case for replacing the CLI rests on a symptom the configuration produces

Raised by the scope lens.

"A non-lint finding is rare" is what the current setup is built to produce:

- The model lane is off by default.
- When on, it sees only the diff, clipped at 120 KB, plus named guidance files. It does not fetch the pull request title or description.
- Its prompt restricts it to changed lines and tells it to prefer silence.
- Four of its eight categories are capped below the threshold for an inline comment.

The concept cites only external studies. It gives no `reviewer metrics` figures from the requester's repositories, although the command exists. The ADRs are about ten weeks old, so those measurement windows have barely elapsed.

**Proposed resolution.** Before building anything, confirm whether the model lane was ever enabled in the four repositories, run `reviewer metrics` there, and widen the existing lane's inputs with the pull request description and plan files. That tests the idea behind the whole concept, that context is what the AI reviewer lacks, at almost no cost.

### Where the reviewers disagreed, and how each point was settled

**Replace GitHub's page, or augment it.** Six lenses took a full replacement as given and asked how to build it safely. The scope lens argued for adding to GitHub first: a "change brief" comment per pull request from the existing model lane, then a web view that does what GitHub cannot and links back to GitHub for commenting and approval.

*Outcome:* the author chose to augment. Navigation and interactive AI are required in the first version, and GitHub keeps its existing functions.

The scope lens also proposed evaluating a browser-hosted VS Code with the GitHub Pull Requests extension before building a viewer. It noted that the claim is from memory and unverified, and that the option is weak on a phone.

*Outcome:* not evaluated. The design document should weigh it under alternatives considered.

**Where the index is built.** The architecture and maintenance lenses proposed sandboxed workers operated by the service. The scope and inventive lenses preferred each repository's own CI job.

*Outcome:* CI, under the author's delegated judgement. GitHub Actions is enabled on every repository. A CI job already installs dependencies with the repository's normal `npm` or `bun` command, so generated types are present, and the "install without scripts" question no longer applies to the server.

**What happens when quota runs out.** The architecture and inventive lenses proposed switching between vendors. The scope lens proposed one vendor and an "AI unavailable" state.

*Outcome:* the author chose one provider. Only the provider configured for a repository may be used. When its quota is exhausted, AI features show a message that names quota as the cause.

**Phone scope.** The usability lens specified full touch navigation. The scope lens proposed desktop first, with the phone limited to reading and asking.

*Outcome:* reading and asking is the floor for the first version, and the author prefers navigation as well. An index served from storage makes tap-to-navigate cheap for the server, so it is included if the touch design holds and does not hold up the release.

### Limits no design removes

- **Recall cannot be proven at this volume.** With n audited high-risk pull requests and zero misses, the 95% upper limit on the miss rate is about 3 divided by n.
- **Untrusted code can be contained and cannot be made safe.** Only the amount it can reach can be reduced.
- **A credential already in a transcript has already left.** It reached the vendor that ran the session. The outbound door can only stop it spreading further.

### Reuse of the existing CLI

| Part | Proposal | Assumption that changes |
| --- | --- | --- |
| `internal/gate` (secrets rule, fails closed) | Reuse as the rule at the outbound door | Today it inspects changed lines only; it must cover every payload |
| `internal/diff` | Reuse | None found |
| `internal/fingerprint` | Reuse for de-duplication within one pull request | Never a key outside one pull request; add a new marker type for origin and leave the existing format unchanged |
| `internal/laneb` (review, disprove pass, category caps) | Reuse for batch AI review | Reads the diff only; reads guidance from the pull request checkout, which must become the base branch |
| `internal/model` HTTP adapters | Reuse for batch jobs behind the gateway | No usage figures, streaming or turns; a separate interface is needed for chat |
| `internal/model` subscription paths | Retire on the server | They drive a personal signed-in CLI |
| `internal/ghclient` | Reuse the read calls | One static token; cannot submit a review, reply or approve; no rate-limit handling |
| `internal/pluginhost`, `plugins/typescript` analyzers | Run only inside the sandboxed worker | They execute repository code and inherit the host environment |
| `internal/poller` | Pattern for the author-side watcher in the fix loop | Not suitable for server sync |
| `internal/reporters` summary upsert | Reuse for the change brief | Skips threads started by a different account |
| `internal/installer` agent skill | The channel for intent capture and "address review comments" | It currently tells agents to run the CLI |
| `reviewer metrics` (ADR-0024) | Snapshot per repository before cutover, as the baseline | Counts only threads the tool started |

Decisions that carry forward as requirements: ADR-0012 (no unscanned content reaches a model), ADR-0022 (limits in code, not in the prompt), ADR-0023 (disprove before posting), ADR-0024 (measure and retire on evidence), and the two-lane rule that nothing in the core needs a model.

### Measuring success

| Goal | Proposed measure | Data source | Limit |
| --- | --- | --- | --- |
| Less review time | 75th percentile time a pull request waits on review, paired with active time per reviewer as the guardrail against rubber-stamping ([Meta, 2022](https://engineering.fb.com/2022/11/16/culture/meta-code-review-time-improving/)) | Workspace telemetry; baseline for wait time from GitHub history | No "before" figure exists for active time |
| Review precision | Share of AI comments whose flagged lines later change. Google reports about 40% resolved by a later change against about 10% with explicit feedback ([Google, 2024](https://arxiv.org/abs/2405.13565)). ByteDance reports 26.7% for its AI against 35–46% for humans ([ByteDance, 2025](https://arxiv.org/abs/2501.15134)) | Workspace store and GitHub | A changed line is not proof the comment was right |
| Review recall | Reverts, hotfixes and incidents traced to pull requests; the weekly audit sample of low-risk pull requests | Needs an incident-to-pull-request link | Too few incidents in three months to conclude |
| Time on system evolution | Share of human comments about design, data models and workflows against implementation detail | Comments, classified | Needs agreed categories |
| Fewer production issues, easier resolution, less refactoring | Change failure rate, time to restore, rework within a set number of days of merge ([DORA, 2025](https://services.google.com/fh/files/misc/2025_state_of_ai_assisted_software_development.pdf)) | Deploy and incident data | Lagging; depends on the follow-on work |
| Mental model | Distinct files and subsystems each reviewer has viewed ([Rigby & Bird, 2013](https://www.microsoft.com/research/publication/convergent-software-peer-review-practices/)) | Workspace telemetry | A proxy only; comprehension itself is not measured |

Two further sources shaped the analysis. [Bacchelli & Bird (2013)](https://2013.icse-conferences.org/content/expectations-outcomes-and-challenges-modern-code-review.html) found that understanding the change is the central difficulty of review, and that reviews deliver more knowledge transfer than defect finds. [Duma & Wróblewski (2026)](https://arxiv.org/pdf/2605.02273) found that most AI-written pull requests in open source get little human scrutiny. The Rigby & Bird figure was read from a secondary copy, not the publisher's version.

The author has decided that any user can see and export telemetry. That includes per-reviewer figures, for repositories the user can access. A retention period is still to be set.

### Ideal Final Result

A change arrives with its reason and plan written into the pull request by the agent that made it. Each commit analyses itself in a box that holds nothing worth stealing and hands back a file, so the server never executes a byte from a pull request. When a reviewer opens the change, every "where is this defined" has already been computed and labelled with how it was obtained. High risk is decided by rules people can read, a model can only add caution, and the system keeps measuring its own misses. The reviewer comments once on GitHub, and the session that wrote the code picks the comment up. A lesson is taught once in an ordinary review comment; from then on a rule in the repository enforces it, the AI reviewer and the coding agents both read it, and it is retired when people stop agreeing with it. Losing all model access costs only new AI answers.

## Goals vs. Non-Goals

* **In-Scope Goals, first version:**
  * A web application that augments GitHub's pull request page. Every view links to the matching place on GitHub for commenting, approval, CI status and merge.
  * A view per GitHub pull request or branch, with quick switching between open ones.
  * Diff display by default, with an option to show only the new code.
  * Go-to-definition, find-references and code search for TypeScript, answered from an index that each repository's CI job builds per commit. Every answer says whether the compiler resolved it or it is a text match.
  * Interactive AI help: select code and ask, questions about new or existing code, visualizations, research into alternatives.
  * A change brief and an AI review of the plan and the code, shown in the workspace.
  * Intent context from the pull request title and description, plans and ADRs in the repository, and an intent record the coding agent writes into the pull request.
  * Access that mirrors GitHub repository access, checked on every read.
  * One outbound door for model calls, with a secrets scan that refuses to send when it cannot scan. One configured provider per repository, and a quota-specific message when its quota is exhausted.
  * Desktop hotkeys. On a phone: reading the change brief and the diff, and asking questions. Tap-to-navigate is preferred and not required.
  * Telemetry that any user can see and export, and service notices that reach every user.
* **In-Scope Goals, later stages:**
  * Learnings: candidates in an inbox, and accepted learnings as files in the repository, read from the base branch.
  * A written risk list per repository and risk labels in shadow mode. Unread merges only after a numeric exit criterion.
  * Comments and suggestions written in the workspace and posted to the GitHub pull request, under the human's name or a bot name.
  * A "send to agent" action on a submitted batch of comments, picked up from the author's side.
  * Planning, deployment and landed impact.
* **Out-of-Scope Non-Goals:**
  * Rebuilding GitHub's commenting, approval, thread resolution, CI status or merge in the first version.
  * Installing or executing pull request code on the server.
  * Storing or sharing whole agent transcripts in the first version.
  * The server running coding agents or pushing commits.
  * Switching to a second model provider when quota runs out.
  * Navigation for languages other than TypeScript in the first version. They get text search.
  * OpenAI models, and session support for Codex and other agents, at the start.
  * A shared service run for other teams at the start. Records still carry organisation and repository identifiers, so that it is not a rewrite later.
  * Local-only storage as a requirement.

## Alternatives Weighed

1. **Local-first storage as a requirement.** Rejected by author: a phone cannot hold checkouts or run language servers, so local storage is optional for capable devices.
2. **Extending the existing CLI as it stands.** Rejected by author: the author wants a new system that reuses parts of the CLI where they help.
3. **Replicating GitHub's pull request page as the bar for the first version.** Rejected by author: the first version should add what GitHub cannot do, so the new features can be tested and get feedback quickly. Users keep using GitHub for its existing functions.
4. **A trained risk classifier that lets pull requests merge unread from the first version.** Rejected by author: the author accepted the finding that recall cannot be validated at this volume. A written list and shadow mode come first.
5. **Exporting whole agent transcripts as the source of intent.** Rejected by author: the author accepted the proposal that the agent writes an intent record into the pull request. That record inherits GitHub's access rules and needs no separate link from session to pull request.
6. **The server resuming the original agent session to make fixes.** Rejected by author: a hosted server cannot reach a session on a laptop. The author's side pulls the work on an explicit human action.
7. **A learning store kept only in remote storage.** Rejected by author: the author accepted the two-stage design, because files in the repository are visible, correctable and readable by coding agents.
8. **Switching to a second model provider when quota runs out.** Rejected by author: only the provider configured for a repository may receive its code.
9. **A new repository for the new system.** Rejected by author: the new system lives in this repository.
10. **Sandboxed workers on the server to install and index pull request code.** Set aside under the author's delegated judgement: building the index in each repository's own CI job keeps pull request code off the server entirely, and GitHub Actions is enabled on every repository.
11. **A GitHub App for sign-in, per-user identity and webhooks.** Deferred by author: GitHub Apps are likely not allowed for these repositories. Alternatives are considered first, and an App is investigated only if they fail.

## Key Decisions & Staging for Next Steps

### Decisions the author has made

**The first version**

1. It augments GitHub's pull request page. Users keep using GitHub for the functions it already has.
2. Code navigation and interactive AI features are required in it. The purpose is to test them and get feedback quickly.
3. It is a web application with review and navigation hotkeys on desktop. On a phone, reading and asking is the floor, and navigation is preferred.
4. GitHub is the primary source of changes. Each pull request or branch has its own view. The diff is shown by default, with an option to show only the new code.
5. Navigation is TypeScript first, with other languages later as plugins. Go-to-definition must never be wrong: when it is not confident it shows search results or says it found no definition.
6. The repositories install with their standard `npm` or `bun` install commands.

**Accepted from the review**

7. Risk: a written list per repository of what always needs a human, with labels in shadow mode. A pull request may merge unread only after a numeric exit criterion is met for its repository. High risk means a change to primary data models, workflows or user experience.
8. Intent: the coding agent writes an intent record into the pull request. Whole transcripts are not exported in the first version.
9. Fix loop: the author's side pulls the work on an explicit human action. The server neither runs agents nor pushes.
10. Learnings: candidates go to an inbox, and accepted learnings are files in the repository read from the base branch. Anyone working on the project can see, correct and remove them.

**Constraints**

11. Access mirrors GitHub repository access. Not every engineer can see every repository.
12. Google and Anthropic models are supported at first. Only the provider configured for a repository may be used. Budget limits are configurable. When quota is exhausted, AI features are unavailable and the message says quota is the cause.
13. GitHub Apps are likely not allowed. Alternatives are considered first.
14. The service runs on a Google Cloud project the author owns and operates. It may later be a shared service.
15. The new system lives in this repository. Some of the reviewed repositories already have the existing CLI installed.
16. A baseline may be captured from GitHub history. Any user can see and generate telemetry.
17. Every user receives notices about the service.
18. When the workspace later posts comments: a human's name appears on comments the human wrote and on AI-written comments the human asked for, and a bot name appears on the AI's own comments.
19. Planning, deployment and landed impact follow the first version.

### Choices made under the author's delegated judgement

The author said "use your judgement" on these. Each can be overruled in the design document.

1. **The index is built in CI.** Each repository's GitHub Actions job builds a navigation index for every pull request commit and every default-branch commit, and hands it to the service. A branch with no pull request gets the diff and text search.
2. **The CLI stays as the CI-side component.** It keeps running the deterministic checks, and gains the index build and the change brief. Repositories that already have it installed keep working, which avoids the cutover failures in problem 14. The AI's own comments keep being posted by that job under the Actions bot identity, which needs no GitHub App.
3. **Existing rules and guidance are kept.** Pattern rules under `.review/rules/` stay as the deterministic form of a learning. Existing guidance documents become the first accepted learnings. New accepted learnings are files beside them.
4. **Identity without a GitHub App.** The options are tried in this order, against what the organisation permits:
   1. Each user supplies a fine-grained personal access token limited to the repositories they can read. The service checks access with it and stores it encrypted.
   2. Sign-in through an OAuth App.
   3. A GitHub App, only if both fail.
5. **Events without App webhooks.** A small workflow in each repository tells the service when a pull request changes, and the service also reconciles by polling on a timer.
6. **Notices.** A status banner and a status page inside the application for every user, plus email for outages. Quota exhaustion is shown in the place where the AI feature was used.
7. **Telemetry visibility.** All users see the same telemetry for the repositories they can access, including per-reviewer figures. Known risk: review time that colleagues can see changes how people behave. Meta used it as an aggregate guardrail for that reason.

### To verify in the design document

1. Which identity option the organisation permits, and whether a fine-grained token can be scoped tightly enough.
2. How the CI job hands the index to the service, and how the service authenticates that hand-off without a secret stored in the repository. A workflow's own short-lived identity token is the candidate.
3. The index format and tool for TypeScript, including repositories managed with `bun`.
4. An inventory of which repositories have the CLI installed, by which trigger, and whether the model lane is on.
5. The numeric exit criterion for shadow mode, per repository.
6. Retention periods for telemetry and for stored AI conversations.
7. The browser-hosted VS Code alternative, which no one has evaluated.

### Staging

Three checks cost almost nothing and can run before design work finishes:

1. Capture the baseline: time to first review and to merge, comments per pull request, share merged with no human comment, and `reviewer metrics` per repository.
2. Build an index for one repository in its CI job, and measure build time and index size.
3. Turn on the existing model lane with the pull request description and plan files as input, and add the two example learnings to guidance. Compare the findings with today's.

Then, each stage proven on one repository first:

1. **First version.** A read-only workspace: pull request and branch views, diff or new code, navigation and search from the CI-built index, interactive AI help, the change brief, intent context and the AI review. Everything else links to GitHub. The agent skill is updated to write the intent record and to address review comments when the author asks.
2. **Learnings and risk.** The learning inbox and repository files. The written risk list, with labels in shadow mode.
3. **Commenting.** Comments written in the workspace and posted to GitHub, and the "send to agent" action. The state-ownership rules in problem 6 apply from here.
4. **Unread merges and the wider lifecycle.** Unread merges per repository once its exit criterion is met. Planning, deployment and landed impact.

The existing CLI workflow in each repository is updated in place at stage 1. It is not removed.

This brief is ready for `/egm-draft`.
