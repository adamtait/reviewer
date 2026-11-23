# Review workspace: code navigation and AI help beside GitHub pull requests

*   **Author:** adamtait
*   **Status:** Draft
*   **Date:** 2026-11-23

--------------------------------------------------------------------------------

## Overview

The review workspace is a web application that sits beside GitHub's pull request page. It adds two things GitHub does not have: code navigation inside a pull request (go-to-definition, find-references, search), and an AI assistant that can answer questions about the change and the code around it. Reviewers keep using GitHub to comment, approve and merge.

It serves a team of 10 engineers across 4 or more repositories. The team opens dozens of pull requests a week, and coding agents write more than 90% of them. The code is mostly TypeScript.

The system has two parts:

*   **The CI side.** The existing `reviewer` CLI already runs in each repository's GitHub Actions job. It gains two outputs per commit: a navigation index and a snapshot of the source.
*   **The workspace service.** A hosted service on a Google Cloud project stores those outputs, generates a change brief and an AI review from them, checks each user's access against GitHub, and serves the web application. It never installs or executes code from a pull request.

This document designs the first version only. Three later stages are named under Non-Goals: learnings and risk labels, commenting from the workspace, and unread merges. Each gets its own design document. This document fixes only the constraints those stages place on the first version, so that none of them forces a rewrite.

The concept, the seven-lens review of it and the author's decisions are recorded in [`concepts_brief.md`](./concepts_brief.md).

## The Problem

Reviewing a pull request today means reading it line by line on GitHub's pull request page. Three things make that slow and shallow for this team.

**It is hard to build a mental model of a change.** The reviewer needs to know why the change was made, how it works, what it does to the rest of the system, and which data models and workflows it touches. GitHub shows changed lines and a description. Research on code review finds that understanding the change is the central difficulty of review, ahead of finding defects (Bacchelli & Bird, 2013).

**The diff is plain text.** There is no go-to-definition, no find-references, and no view of how changed files relate. To follow a call, the reviewer leaves for an IDE, fetches and switches branch, and often loses the diff.

**There is no way to ask.** A reviewer cannot select a block of code and ask what it does, ask how an existing subsystem works, or ask for a diagram of a changed flow.

Agent-written code makes all three worse. The person who opened the pull request did not type the code, so the reasoning lives in an agent session nobody else can see. When no plan was attached, the motivation is invisible. A study of open-source projects found that most AI-written pull requests get little human scrutiny (Duma & Wróblewski, 2026).

The existing `reviewer` CLI does not help with any of this. It reports lint, type and convention findings on changed lines. Its model lane is off by default, reads only the diff, and never sees the pull request description.

**The proposed solution.** Give the reviewer a view of each pull request in which every symbol can be followed, the reason for the change is on screen before any line is read, and an AI assistant can be asked about anything in the repository at that commit. Build it beside GitHub, so the team can try the new features at once without giving up anything GitHub already does.

The first version is also a test. The team wants feedback on navigation and AI help quickly, before investing in the later stages.

## Goals

Each goal names how it will be checked. Where a threshold depends on a measurement that has not been taken, the threshold is listed under Open Questions.

*   **Open any pull request in the workspace.** A user opens a pull request or branch by link from GitHub or from a list of open pull requests, and sees the diff by default or only the new code. Checked by: every open pull request in a connected repository appears in the list within a stated delay of being opened or pushed.
*   **Navigate TypeScript code without wrong answers.** Go-to-definition and find-references work on changed and unchanged code at the commit on screen. A result is shown as a definition only when the compiler resolved it. Otherwise the result is labelled as text search, or says none was found. Checked by: zero confirmed wrong definitions, from a "this was wrong" report on every jump; and the share of lookups resolved by the compiler, reported per repository.
*   **Search the repository at the pull request's commit.** Checked by: text search returns results from unchanged files as well as changed ones, and works while the navigation index is still being built.
*   **Ask the AI about code.** A user selects code and asks about it, or asks about any code in the repository at that commit. The user can ask for a diagram or for alternatives to an approach. Answers cite files and lines. Checked by: every answer links to the code it relied on.
*   **See the reason for a change before reading it.** Each pull request has a change brief: why the change was made, how, which data models and workflows it touches, and which unchanged code depends on what changed. It draws on the pull request title and description, plans and ADRs in the repository, and an intent record the coding agent writes. Checked by: every pull request view shows a brief, or says which inputs were missing.
*   **See an AI review of the plan and the code.** Checked by: the review is shown in the workspace with the commit and model that produced it.
*   **Mirror GitHub access.** A user sees a repository's code, briefs, AI answers and telemetry only if they can read that repository on GitHub. Checked by: an access test for every kind of stored record.
*   **Keep pull request code off the server.** The service never installs dependencies, loads tools or runs tests from a pull request. Checked by: the service has no code path that clones and executes a repository.
*   **Send nothing unscanned to a model.** Every model call passes one outbound point that scans the payload for secrets and refuses to send when it cannot scan. Only the provider configured for a repository receives its code. Checked by: a count of model calls with no recorded scan decision, which must be zero.
*   **Work without a model.** Diff, navigation and search need no model. When the provider's quota is exhausted, AI features say that quota is the cause. Checked by: a test that runs the workspace with no provider reachable.
*   **Work on a phone for reading and asking.** A user can read the change brief and the diff and ask the AI questions on a phone. Tap-to-navigate is included if the touch design holds.
*   **Support desktop hotkeys.** Every core action has a keyboard shortcut that does not collide with a browser-reserved key or fire while the user is typing.
*   **Show telemetry to every user.** Any user can see and export usage and review measures for the repositories they can access. A baseline from GitHub history is captured before the first version is used.
*   **Leave existing installs working.** A repository that already runs the `reviewer` CLI keeps its checks and comments while it gains the new outputs.

## Non-Goals

**Left to GitHub in the first version**

*   Writing comments and suggestions, approving, requesting changes, resolving threads, showing CI status, and merging. Each view links to the matching place on GitHub.

**Later stages, each with its own design document**

*   Learnings: an inbox of candidates drawn from human reviews, and accepted learnings as files in the repository.
*   A written risk list per repository, and risk labels in shadow mode.
*   Comments written in the workspace and posted to GitHub, and a "send to agent" action on a batch of comments.
*   Pull requests that merge with no human having read them.
*   Planning, deployment and landed impact.

**Not planned**

*   Installing or executing pull request code on the server.
*   Storing or sharing whole agent transcripts.
*   The server running coding agents or pushing commits.
*   Switching to a second model provider when quota runs out.
*   Compiler-grade navigation for Python, bash or Rust. Those files get text search.
*   OpenAI models, and intent records from Codex or other agents.
*   A shared service for other teams. Records still carry organisation and repository identifiers, so that this is not a rewrite later.
*   Storage on the user's device as a requirement.

## Open Questions

*   **`bun` and the indexer**: Does `scip-typescript` index a repository whose dependencies were installed with `bun`? To be tested on one such repository before the workflow template is written.
*   **Indexer upkeep**: The latest `scip-typescript` release found is 0.4.0, from October 2025. Is it still maintained, and does it support the TypeScript versions these repositories use?
*   **Build time and size**: How long does the index build take in CI, and how large are the index and the pack? This sets the "ready after push" threshold and the upload size limits.
*   **Public upload endpoint**: The CI job must reach the service over the internet. Is a public HTTPS address that accepts only GitHub-signed tokens acceptable for this Google Cloud project?
*   **Workflow identity token**: Which claims does the token carry on `pull_request` runs in these repositories, and is it issued for pull requests from forks? The design assumes branches in the same repository.
*   **Credential approval**: Will an organisation owner approve fine-grained personal tokens, and can a GitHub App be installed? Either one is enough to start.
*   **Existing installs**: Which repositories have the CLI installed, by which trigger, and with the model lane on or off?
*   **Retention**: How long are telemetry and stored AI conversations kept?

## Critical User Journeys (CUJs)

### CUJ 1: Understand a change before reading it

*   **Given**: A pull request whose CI job has delivered its snapshot and index, and whose change brief has been generated.
*   **When**: A reviewer opens the pull request in the workspace.
*   **Then**: The reviewer sees the change brief first: why, how, the data models and workflows touched, and the unchanged code that depends on what changed. Each statement links to the code or the document it came from. The diff is one action away.

### CUJ 2: Follow a symbol into unchanged code and return

*   **Given**: A pull request view with navigation ready.
*   **When**: The reviewer triggers go-to-definition on a symbol in an added line.
*   **Then**: The view jumps to the definition, which may be in a file the pull request does not touch. The view marks that file as outside the change. One action returns the reviewer to the same place in the diff.

### CUJ 3: A lookup the compiler could not resolve

*   **Given**: A symbol the index holds no compiler-resolved definition for, such as one in a Python file.
*   **When**: The reviewer triggers go-to-definition on it.
*   **Then**: The reviewer sees a list labelled as text search results, or a message that nothing was found. Nothing is presented as a definition.

### CUJ 4: Open a pull request just after a push

*   **Given**: A new commit was pushed a moment ago and its CI job is still running.
*   **When**: The reviewer opens the pull request.
*   **Then**: The diff for the new commit is shown. The view says navigation and the change brief are being prepared for this commit. Text search works. Nothing is answered from the previous commit's index.

### CUJ 5: Ask about selected code

*   **Given**: A pull request view, and a provider configured for the repository.
*   **When**: The reviewer selects a block of code and asks what it does and what else calls it.
*   **Then**: The answer streams in, cites files and lines at the commit on screen, and names the model that produced it. The reviewer can ask for a diagram of the flow.

### CUJ 6: Quota is exhausted

*   **Given**: The configured provider is rejecting requests because quota has run out.
*   **When**: The reviewer asks the AI a question.
*   **Then**: The reviewer sees a message that quota is exhausted, with the reset time when the provider gives one. The stored change brief and AI review for this commit are still shown. Diff, navigation and search are unaffected.

### CUJ 7: A secret is found

*   **Given**: A file in the repository at this commit contains a credential.
*   **When**: The reviewer asks a question whose answer would need that file sent to the provider.
*   **Then**: The request is not sent. The reviewer is told that a credential was found and where, and that AI help is off for that content.

### CUJ 8: A user without access

*   **Given**: A user who cannot read a repository on GitHub.
*   **When**: That user follows a link to one of its pull requests in the workspace.
*   **Then**: The workspace shows nothing from that repository: no code, brief, AI answer or telemetry. The message does not reveal whether the pull request exists.

### CUJ 9: Leave a comment

*   **Given**: A reviewer reading a changed line in the workspace.
*   **When**: The reviewer chooses to comment.
*   **Then**: GitHub opens at that file and line of the pull request, and the reviewer writes the comment there.

### CUJ 10: Read and ask on a phone

*   **Given**: A reviewer on a phone.
*   **When**: The reviewer opens a pull request.
*   **Then**: The change brief and a single-column diff are readable. The reviewer can tap a line and ask the AI about it, and the code in question stays visible while the keyboard is open.

### CUJ 11: The author's agent addresses review comments

*   **Given**: A pull request with review comments on GitHub, and the author working with a coding agent in that repository.
*   **When**: The author asks the agent to address the review comments.
*   **Then**: The agent reads the open threads from GitHub, makes the changes, pushes, and replies on each thread. The workspace shows the new commit after its CI job delivers a new snapshot and index.

## Technical Plan (Architecture)

### Decisions this plan rests on

| # | Question | Choice | Decided by |
| --- | --- | --- | --- |
| 1 | How the service knows who a user is and what they can read | Two routes behind one interface: a personal access token per user, and a GitHub App where the organisation allows one | Author |
| 2 | How each commit's outputs reach the service | The CI job pushes them. A second small workflow reports pull request events | Author |
| 3 | What builds the navigation index | `scip-typescript`, producing the SCIP format | Author |
| 4 | Where the service runs and keeps data | A stateless container on Cloud Run, managed Postgres, and a Cloud Storage bucket | Author |
| 5 | How the AI assistant gets code | The model calls read-only tools that the service runs | Author |
| 6 | Language and shape of the service | Go, as a second binary in this repository, reusing the CLI's packages | Proposed here |
| 7 | Web application | TypeScript, React and CodeMirror 6, built into static files and embedded in the service binary | Proposed here |
| 8 | Form of the source snapshot | A git pack, verified by recomputing every object's hash | Proposed here |
| 9 | Where secrets are scanned, and where the brief and AI review are generated | In the service, not in CI | Proposed here |
| 10 | Background work | A job table in Postgres, run on a timer | Proposed here |

Rows 6 to 10 are proposals. The reasons are given where each appears below.

### The trust rule that shapes the design

A `pull_request` workflow runs the workflow file from the pull request itself. A pull request can therefore change what its own CI job uploads. The service treats everything that arrives from CI as written by the pull request's author, and decides per kind of data how far to rely on it.

| Data | Arrives from | How the service relies on it |
| --- | --- | --- |
| Source code at a commit | CI, as a git pack | Verified. Git names every object by a hash of its content, and a commit's hash covers its whole tree. The service recomputes the hashes and rejects a pack that does not add up to the commit it claims to be. |
| Which commit is a pull request's head; its title, description and state | GitHub, read with the viewer's credential | Trusted. The copy sent by the workflow is used only for background work and is replaced the first time a user opens the pull request. |
| Navigation index | CI | Cannot be verified, so it is bounded. Every path must exist in the verified snapshot and every position must fall inside its file. A definition is shown only if the name at the target matches the symbol. A false index can mislead only readers of its own pull request, and cannot change the diff. |
| Secrets scan result | Computed by the service on the verified snapshot | Trusted. A scan result is never accepted from CI. |
| Change brief and AI review | Generated by the service | Built from verified source and from the pull request description, which is shown as the author's own words. |

This rule is why rows 8 and 9 above differ from the concepts brief, which had the CLI produce the change brief. A brief or a scan result produced in CI could be altered by the pull request it describes.

### Components

```text
  Reviewed repository on GitHub                     Google Cloud project
 +-------------------------------+                 +--------------------------------------------+
 | review workflow               |                 | workspace service (Cloud Run, stateless)   |
 |  - existing checks (as today) |   upload with   |                                            |
 |  - scip-typescript -> index   |   GitHub-signed |  ingest ----> verify pack, store, enqueue  |
 |  - reviewer publish           | --------------> |  jobs ------> scan, index, brief, review   |
 |      git pack + index + PR    |   token         |  identity --> token route | App route      |
 |      title and description    |                 |  access ----> can this user read this repo |
 | events workflow               | --------------> |  source ----> files, diff, text search     |
 |  - closed / reopened / edited |                 |  navigation -> definitions, references     |
 +-------------------------------+                 |  assistant -> tool loop                    |
            ^                                      |  gateway ---> the one door to the provider |---> configured
            |  GitHub API, as the viewer           |  telemetry, notices                        |     model provider
            +--------------------------------------|  web application (static files)            |
                                                   +---------+----------------------+-----------+
  Browser, desktop or phone  <------ HTTPS ------------------+                      |
                                                              Postgres (records)   Bucket (raw uploads)
```

### The CI side

The existing workflow keeps its checks and comments unchanged. It gains one permission, `id-token: write`, which lets a job ask GitHub for a signed token naming its repository. No secret is added to any repository.

**The review workflow** runs for each pull request commit and each default-branch commit. After the existing steps it runs two new ones:

1. **Index.** `scip-typescript` runs against the installed checkout and writes one SCIP file. If the tool is missing or fails, the step says why and the upload continues without an index. The workspace then shows that navigation is unavailable for this commit, with the reason.
2. **Publish.** A new subcommand, `reviewer publish`, does the following:
   1. It asks the service which base commits of this repository it already holds.
   2. It builds a git pack of the commit's objects that are missing from those commits. A first upload sends the whole tree, and later ones send only what changed.
   3. It requests the job's signed token, with the service as the audience.
   4. It uploads the pack, the SCIP file and the pull request's number, title, description, author, base commit and head commit.

For a pull request whose merge base the service does not hold, `publish` uploads that commit's snapshot as well, so the diff can be computed.

**The events workflow** runs when a pull request is closed, reopened or edited. It calls `reviewer publish --event`, which sends only the pull request's current title, description and state. The service uses "closed" to schedule that pull request's data for removal, and "edited" to regenerate the change brief.

**The agent skill** that `reviewer init` writes into each repository gains two instructions:

*   When opening a pull request, write an intent record into its description: the goal, the plan, the alternatives that were rejected, and a session identifier.
*   When the author asks, read the open review threads from GitHub, make the changes, push, and reply on each thread.

**Configuration.** `.review/config.yaml` gains a `workspace` block holding the service address and the index tool settings. This file is read from the pull request's checkout, so a pull request could point it elsewhere. That gains nothing: the author can already read the code being uploaded, and a token issued for another audience is rejected by the real service.

### The workspace service

One Go binary, `cmd/workspace`, serves the API and the web application. It holds no state between requests. The table lists its parts and what each reuses from the CLI.

| Part | What it does | Reuses |
| --- | --- | --- |
| `ingest` | Verifies the signed token and the pack. Stores the raw upload. Queues the work that follows. | Nothing |
| `jobs` | A table of queued work and a runner that claims and runs it. | Nothing |
| `identity` | Signs users in and holds their GitHub credential. Two implementations: token and App. | `internal/ghclient` read calls |
| `access` | Answers "can this user read this repository", by asking GitHub and caching the answer briefly. | `internal/ghclient` |
| `source` | Serves files at a commit, computes diffs between two commits, and runs text search. | Nothing |
| `navigation` | Stores the SCIP data and answers definition and reference lookups. | Nothing |
| `scan` | Runs the secrets scanner over each new file and records the result. | The rule in `internal/gate`; the `gitleaks` invocation in `internal/analyzers/gitleaks` |
| `gateway` | The only code that calls a model provider. Checks scan results, enforces budgets, records usage. | HTTP plumbing and redaction from `internal/model` |
| `assistant` | Runs a conversation: sends the question, runs the tools the model asks for, streams the answer. | Nothing |
| `brief` | Generates the change brief and the AI review for a commit. | Review prompt, disprove pass and category caps from `internal/laneb` |
| `telemetry` | Records usage events and serves the measures and their export. | Acceptance figures from `reviewer metrics` as the baseline |
| `web` | Serves the built web application and the JSON API. | Nothing |

**Why Go and a second binary.** The service reuses the packages named above directly, and the repository already builds, tests and releases Go. A separate binary keeps the CLI small: the CLI does not link the database, storage or web code.

**Why the scan, the brief and the review moved to the service.** The trust rule above is one reason. The second is that the concepts brief requires one outbound door for model calls. If the CI job also called a model, there would be two doors, two places holding a model key and two budgets.

The CLI's existing model lane is left as it is. A repository that has turned it on keeps getting its comments. Turning it off once the workspace review is in use is a later decision for each repository.

### From a push to a ready view

1. A commit is pushed to a pull request. The review workflow starts.
2. `reviewer publish` uploads the pack, the index and the pull request details.
3. `ingest` checks the token: it must be signed by GitHub, be issued for this service, and name a repository the service has been told to accept.
4. `ingest` unpacks the pack and recomputes every hash. If they do not add up to the commit named in the request, the upload is rejected.
5. Still inside the upload request, the service stores each new file, records which path holds which file at this commit, scans each new file for secrets, and loads the index. When the CI step returns, the diff, search and navigation for this commit are ready.
6. `ingest` queues two jobs: generate the change brief, and generate the AI review. The job runner picks them up within a minute.
7. A reviewer opens the pull request. The service asks GitHub, with the reviewer's credential, whether they can read the repository and what the pull request's head commit is. It then serves the view for that commit.

Each stage has a visible state per commit: received, verified, scanned, indexed, brief ready, review ready, or failed with a reason. The pull request view shows these, so "not ready yet" never looks like "nothing found".

### Storing source and index without copying them per commit

Most files do not change between two commits. The service stores each distinct file once.

*   **Files.** A file's content is stored once, keyed by its git hash. A commit is a list of paths, each pointing at a file hash. A push adds content only for the files it changed.
*   **Text search.** File content lives in Postgres with a trigram index. A search at a commit joins that commit's path list to the content table.
*   **Index.** A SCIP file holds one document per source file, listing each symbol occurrence with its position and whether it is a definition or a reference. The service stores each document once, keyed by a hash of the document, and a commit's index is a list of paths pointing at document hashes. A lookup for a definition joins the commit's list to the stored occurrences.
*   **Raw uploads.** The original pack and SCIP file go to the bucket, so the stored form can be rebuilt if its layout changes.

The main records:

| Record | Key | Holds |
| --- | --- | --- |
| Repository | organisation and name | The model provider, model and budget for this repository; whether uploads are accepted |
| Commit | repository and commit hash | Its tree hash, when it was verified, and the state of each stage |
| Path entry | repository, commit and path | The file hash at that path |
| File | file hash | The text, its size, and its secrets scan result with the scanner version |
| Index entry | repository, commit and path | The document hash at that path, and the tool and version that produced it |
| Occurrence | document hash | Symbol, position and role |
| Pull request | repository and number | Head and base commit, title, description, author, state, and whether the copy came from CI or from GitHub |
| Brief and AI review | repository, commit and pull request | The text, the model that wrote it, and a list of the inputs it was given |
| Conversation and message | conversation identifier | The user, repository, commit, the question, the tool calls and the answer |
| Model call | call identifier | Feature, user, repository, commit, provider, model, what was sent, the scan decision, tokens used and the outcome |
| Credential | user | The user's GitHub token, encrypted with a key held in Cloud KMS |
| Usage event | event identifier | User, repository, pull request, kind of action and time |
| Job | job identifier | Kind, target, state, attempts and the earliest time to run |

Every record that belongs to a repository carries the organisation and repository identifiers.

**Storage on a developer's machine.** The bucket sits behind an interface with a second implementation that writes to a local directory. The database is Postgres in both cases, run in a container locally. This is narrower than "a local-files form" for all storage: text search and the job queue use Postgres features, and a second database engine would double that code.

### Identity and access

Both routes implement one interface with three operations: sign a user in, say whether that user can read a repository, and return a GitHub client that acts as that user.

*   **Token route.** On first visit the user pastes a fine-grained personal access token with read access to contents, pull requests and metadata. The service calls GitHub to learn whose token it is, encrypts it, stores it, and issues a session cookie. When the token expires or is revoked, the user is asked for a new one.
*   **App route.** The user signs in through the GitHub App's web flow. The service stores the user token and its refresh token the same way. Where the App is installed, the service also has its own short-lived installation token. It uses that to learn about pull requests without waiting for a viewer, and it may receive events from GitHub in place of the events workflow.

Uploads from CI work the same way under both routes.

**The access check.** Before any read of a repository's data, the service asks GitHub whether the signed-in user can read that repository, using the user's own credential. The answer is cached for a few minutes. The check covers every kind of record: files, index, briefs, AI reviews, conversations and telemetry. A user without access gets the same response whether or not the pull request exists.

### The model gateway

The gateway is the only package that holds provider keys or opens a connection to a provider. Keys live in Secret Manager and are read only by the gateway.

**Before sending.** Every request is a list of parts: instructions, the user's typed text, and code or document excerpts identified by file hash. For each excerpt the gateway looks up the scan result:

*   No result recorded: the request is refused. An unscanned file is treated like one with a secret, as ADR-0012 requires.
*   A secret in a file the pull request changed: AI features are off for that whole commit of the pull request.
*   A secret in a file the pull request did not change: that file is never sent, and the user is told which file was left out.

Typed text is checked with the credential patterns in `internal/model/redact.go` before it is sent.

**Provider.** Each repository names one provider and model. The gateway never sends a repository's content anywhere else. It implements a new interface with conversation turns, tool calls, streamed output and token counts, for the Anthropic and Google APIs. The existing one-call interface (ADR-0021) stays for the brief and review jobs.

**Budgets and quota.** The gateway records tokens for every call, by user, repository and feature. A request that would pass a configured budget is refused with a message naming the budget. A provider response that reports exhausted quota becomes a distinct outcome carrying the reset time when the provider gives one, and the web application shows it as a quota message. Brief and review jobs that hit quota are put back in the job table with an earliest run time, and are not dropped.

### The assistant

A question starts a conversation tied to a repository, a commit and a user. The gateway sends the question with a description of the tools. The model may answer or ask for a tool. The service runs the tool, passes the result through the gateway's checks, and returns it to the model. This repeats until the model answers or a limit on rounds or tokens is reached.

The tools are all read-only and all act on the commit the user is viewing:

| Tool | Returns |
| --- | --- |
| Search text | Matching lines with paths |
| Read file | A range of lines from one file |
| Find definition | The definition of the symbol at a position, labelled compiler-resolved or text match |
| Find references | The occurrences of that symbol |
| List changed files | The files the pull request changes, with line counts |
| Read brief | The change brief and the pull request description |

The answer streams to the browser as it is produced. Each answer records the model that wrote it and links to every excerpt it was given. Diagrams are returned as Mermaid text and drawn in the browser with script execution disabled.

Text inside code, descriptions and tool results can contain instructions aimed at the model. The tools cannot write, post or reach the network, so the most such text can do is produce a wrong answer for the user who asked.

### The change brief and the AI review

Both are generated once per commit by a job, stored, and shown with the commit and model that produced them.

*   **Inputs.** The diff; the pull request title and description, including the intent record; plan and ADR files the pull request adds or changes; guidance documents named in config; and, from the index, the unchanged code that refers to symbols the pull request changes.
*   **Guidance is read from the base commit.** A pull request cannot change the guidance it is reviewed against.
*   **The brief** answers four questions: why the change was made, how it works, which data models and workflows it touches, and what unchanged code depends on it. Each statement links to its source. When an input is missing, such as no intent record, the brief says so.
*   **The review** reuses the CLI's review prompt, its second pass that tries to disprove each finding (ADR-0023), and its confidence caps (ADR-0022). Findings are shown beside the diff in the workspace. In the first version they are not posted to GitHub.

### The web application

One page application with three main views: the list of open pull requests, the pull request view (brief, diff, AI review, assistant panel), and the code view for files outside the diff.

*   **Code and diff** are drawn with CodeMirror 6. It supports touch input and draws only the visible part of a long file, so the application supplies its own in-file search.
*   **Diff or new code.** The default is the diff. A toggle shows only the new version of each changed file.
*   **Navigation.** A lookup from a removed line uses the base commit, and any other lookup uses the head commit. The view says which commit it is showing. Back and forward follow browser history, and every location has its own address.
*   **Narrow screens** get a single-column diff and an action sheet in place of hover and right-click.

| Action | Keyboard | Touch |
| --- | --- | --- |
| Next or previous change | `j` / `k` | Buttons in the bottom bar |
| Next or previous file | `]` / `[` | File list |
| Go to definition | `g` then `d` | Tap the symbol, then "Definition" |
| Find references | `g` then `r` | Tap the symbol, then "References" |
| Ask about the selection or line | `a` | Tap the line number, then "Ask" |
| Search code | `/` | Search button |
| Toggle diff and new code | `d` | Toggle in the header |
| Comment on GitHub at this line | `c` | Tap the line number, then "Comment on GitHub" |
| Switch pull request | `g` then `p` | Pull request list |
| Return to the diff | `Esc` | "Back to diff" |
| Command palette | `Ctrl` or `Cmd` + `K` | Menu |
| Shortcut help | `?` | Menu |

Single-key shortcuts are off while a text field has focus. The keys follow the `j`/`k` and `g`-prefix conventions that GitHub and Gerrit users already know.

On a phone, tapping a symbol or a line number opens one sheet listing every action available there. This avoids giving comment, navigate and ask the same gesture.

### Background work

Jobs live in a Postgres table with a kind, a target, a state, an attempt count and an earliest run time. Cloud Scheduler calls the service once a minute. That request claims the jobs that are due and runs them until its time allowance ends. A job that fails is retried with a growing delay, and a job that hits quota waits until the reset time.

A table was chosen over a separate queue product because the backlog can then be shown on the status page and in each pull request's stage list, and the same code runs on a developer's machine with a timer in place of Cloud Scheduler.

Jobs in the first version: generate brief, generate review, remove data for a closed pull request after a retention period, and import the baseline from GitHub history.

### Telemetry and notices

The web application reports usage events to the service: pull request opened, file viewed and for how long, each navigation lookup with its outcome (compiler-resolved, text match, not found, index unavailable), each search, each AI question, each "this was wrong" report, and each jump to GitHub. Any user can view these measures and export them, limited to repositories they can read.

A baseline job reads each repository's history from GitHub once: time from opening to first review and to merge, comments per pull request, and the share merged with no human comment.

Notices have two forms. A banner and a status page inside the application show the health of each dependency: GitHub, the provider, the database and the job backlog. Outage email is sent by Google Cloud's own monitoring to a list of addresses, and is set up in the deployment, outside this repository.

### Logs and health

*   Every log line is structured and carries a request identifier, user, repository, pull request and commit. Logs never contain source code, descriptions or conversation text.
*   Each stage of ingest and each job records its duration and outcome on the commit or job record.
*   Each model call writes one record, as listed under the main records.
*   A health address reports the state of the database, the bucket and the provider, and how long ago the last upload succeeded for each repository.

### Constraints the later stages place on this version

*   **Commenting from the workspace.** GitHub stays the record for comments and approvals. This version stores no copy of them, so the later stage starts without a migration. The identity interface already returns a client that acts as the user, which is what posting needs.
*   **Learnings.** The brief and review already read guidance from the base commit. Accepted learnings will arrive through the same path.
*   **Risk labels.** The per-commit stage list has room for one more stage, and "could not run" is already a state distinct from "found nothing".
*   **A shared service.** Every record carries organisation and repository identifiers, and the service process holds no state.

### Changes to recorded decisions

ADRs in this repository are append-only, so each change below is a new ADR that supersedes or extends an old one.

| New ADR | Decision | Relation to existing ADRs |
| --- | --- | --- |
| 0029 | Add a hosted workspace service beside the CLI | Supersedes ADR-0008 for the workspace. The CLI stays a stateless binary. |
| 0030 | The service keeps its own store; GitHub stays the record for comments and approvals | Supersedes ADR-0016 for data GitHub cannot hold |
| 0031 | Treat CI uploads as untrusted and verify source by content hash | Extends ADR-0018 |
| 0032 | One outbound door for model calls, with a scan result required for every file sent | Extends ADR-0012 |
| 0033 | Two identity routes behind one interface | New |
| 0034 | A second provider interface for conversations, tools and usage | Extends ADR-0021 |
| 0035 | SCIP as the navigation index format, built in CI by `scip-typescript` | New |
| 0036 | Release a container image as a third artifact | Extends ADR-0026 |

ADR-0004 still applies to the service: no address, organisation name or model name appears in Go source. They come from configuration and the environment. ADR-0010 still applies to this repository: deployment values for the Google Cloud project stay out of it.

--------------------------------------------------------------------------------

## Non-Functional Requirements

### Security and privacy

*   **No pull request code runs on the service.** It parses a git pack and a SCIP file, and runs the secrets scanner over file contents. It never installs dependencies, loads a repository's tools or runs its tests.
*   **Uploads are parsed defensively.** Size limits apply to the pack, to each file and to the index. Paths that would leave the snapshot are rejected.
*   **Credentials at rest.** User tokens are encrypted with a Cloud KMS key. Provider keys live in Secret Manager. Neither appears in logs.
*   **Least privilege for user tokens.** The token route asks for read-only permissions.
*   **Access on every read**, as described under Identity and access.
*   **Model calls** pass the gateway's scan check, and only the configured provider receives a repository's content.
*   **Rendered content.** The brief, AI answers and pull request descriptions are drawn as sanitised Markdown. Mermaid diagrams are drawn with script execution disabled.

### Performance

*   **Navigation and search** answer from Postgres with no model call. The target is under one second for a lookup, to be confirmed against a real repository.
*   **Ready after push.** The threshold depends on index build time in CI, which is an open question.
*   **AI answers** stream, so the first words appear before the answer is complete.
*   **Token use** is bounded per question by a limit on tool rounds and tokens, and per repository by its budget.

### Rollout and rollback

*   **One repository first.** The workflow change is made in one repository, and the others follow after the first has been used for review.
*   **The workflow change is additive.** If `publish` fails, the existing checks and comments still run and the step reports a warning, in keeping with ADR-0009.
*   **Rollback** is removing the two new steps and the events workflow from a repository. Nothing else in the repository depends on them.
*   **The service can be switched off** without affecting GitHub: it writes nothing there in this version.
*   **Database changes** are versioned migrations applied at deploy time. A backup and restore is tested before the first repository is connected.
