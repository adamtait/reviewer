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

*   **Open any pull request in the workspace.** A user opens a pull request by link from GitHub or from a list of open pull requests, and sees the diff by default or only the new code. Draft pull requests count. Checked by: every open pull request in a connected repository appears in the list within a stated delay of being opened or pushed.
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
*   A view for a branch that has no pull request. In this version a branch appears once a pull request, draft or not, is opened for it.

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

ADRs in this repository are append-only, so each change below is a new ADR that supersedes or extends an old one. The checking tool allows only whole-record supersession, so a superseding ADR restates the part of the old decision that still holds.

| New ADR | Decision | Relation to existing ADRs |
| --- | --- | --- |
| 0029 | Add a hosted workspace service beside the CLI | Supersedes ADR-0008, and restates that the CLI stays a stateless binary |
| 0030 | The service keeps its own store; GitHub stays the record for comments and approvals | Supersedes ADR-0016, and restates that GitHub comments stay the CLI's store |
| 0031 | Treat CI uploads as untrusted and verify source by content hash | Extends ADR-0018 |
| 0032 | One outbound door for model calls, with a scan result required for every file sent | Extends ADR-0012 |
| 0033 | Two identity routes behind one interface | New |
| 0034 | A second provider interface for conversations, tools and usage | Extends ADR-0021 |
| 0035 | SCIP as the navigation index format, built in CI by `scip-typescript` | New |
| 0036 | Release a container image as a third artifact | Supersedes ADR-0026, and restates its two artifacts |

ADR-0004 still applies to the service: no address, organisation name or model name appears in Go source. They come from configuration and the environment. ADR-0010 still applies to this repository: deployment values for the Google Cloud project stay out of it.

## Alternatives Considered

Each entry says who set the option aside. "Rejected by author" means the author chose against it, either during the concept review or at one of the five numbered decisions in the Technical Plan. The author did not always state a reason at those decisions, so the drawback recorded when the choice was made is given.

### Scope and product shape

#### Alternative A: Replicate GitHub's pull request page as the first version

*   **Pros**: One place for all review work. No switching between two tools.
*   **Cons**: The bar is all-or-nothing: reviewers keep opening GitHub until commenting, approval, thread resolution, CI status and merge all exist. None of the stated problems is about those functions, and they would be built before navigation or AI help.
*   **Reason for Rejection**: Rejected by author: the first version should add what GitHub cannot do, so the new features can be tested and get feedback quickly. Users keep using GitHub for its existing functions.

#### Alternative B: Extend the existing CLI as it stands

*   **Pros**: No service to host. The CLI already runs in CI, and could post a change brief as a pull request comment.
*   **Cons**: A comment cannot offer navigation or a conversation. The CLI keeps no state between runs and reads only the diff.
*   **Reason for Rejection**: Rejected by author: the author wants a new system that reuses parts of the CLI where they help.

#### Alternative C: Local-first storage as a requirement

*   **Pros**: Data stays on the reviewer's machine. Nothing to host.
*   **Cons**: A phone cannot hold checkouts or run language tools, and nothing is shared between reviewers.
*   **Reason for Rejection**: Rejected by author: a phone cannot hold checkouts or run language servers, so local storage is optional for capable devices.

#### Alternative D: A new repository for the new system

*   **Pros**: A clean start with no existing decisions to supersede, and no public-history rule to respect.
*   **Cons**: The service reuses the CLI's packages, and the CI side is the CLI itself. Two repositories would need releases kept in step.
*   **Reason for Rejection**: Rejected by author: the new system lives in this repository.

#### Alternative E: A trained risk classifier that lets pull requests merge unread from the first version

*   **Pros**: The largest saving in review time.
*   **Cons**: No labelled history exists. Showing a miss rate under 5% needs about 60 known high-risk pull requests per repository, and a miss is never seen because nobody reads the pull request.
*   **Reason for Rejection**: Rejected by author: the author accepted the finding that recall cannot be validated at this volume. A written list and shadow mode come first, in a later stage.

#### Alternative F: Export whole agent transcripts as the source of intent

*   **Pros**: The complete record of how a change was made.
*   **Cons**: A transcript holds whatever the agent saw, including content from other repositories and pasted credentials. Nothing links a session to a pull request, and the formats belong to the agent vendors.
*   **Reason for Rejection**: Rejected by author: the agent writes an intent record into the pull request. That record inherits GitHub's access rules and needs no separate link.

#### Alternative G: The server resumes the original agent session to make fixes

*   **Pros**: The agent that wrote the code, with its full context, makes the fix.
*   **Cons**: The session lives on the author's machine, which a hosted service cannot reach. The server would need push rights and an agent runtime.
*   **Reason for Rejection**: Rejected by author: a hosted server cannot reach a session on a laptop. The author's side pulls the work on an explicit human action.

#### Alternative H: A learning store kept only in remote storage

*   **Pros**: Fast to write, with no pull request per learning.
*   **Cons**: Coding agents cannot read it without a new client, and its contents are not visible in the repository.
*   **Reason for Rejection**: Rejected by author: accepted learnings are files in the repository, because those are visible, correctable and readable by coding agents. This belongs to a later stage.

### Where pull request code runs

#### Alternative I: Sandboxed workers on the server install and index pull request code

*   **Pros**: Does not depend on GitHub Actions. The service controls when an index is built.
*   **Cons**: The service must operate a sandbox with no credentials and no network, and installs must run with scripts disabled, which leaves generated types missing.
*   **Reason for Rejection**: Set aside under the author's delegated judgement, and confirmed when the author approved the Technical Plan. Building the index in each repository's own CI job keeps pull request code off the server entirely, and the normal install there produces generated types.

### Identity (decision 1)

#### Alternative J: OAuth App sign-in

*   **Pros**: A familiar "sign in with GitHub" button. Users handle no token.
*   **Cons**: An OAuth App cannot ask for narrow permissions. The only scope that reads private repositories also grants write access to every repository the user can reach, and approval covers the whole organisation.
*   **Reason for Rejection**: Rejected by author at decision 1, in favour of offering personal tokens and a GitHub App.

#### Alternative K: Google sign-in with one read-only service token

*   **Pros**: Users handle no GitHub credential. One stored token in place of one per user.
*   **Cons**: That one token reads every connected repository. A hand-kept table maps Google accounts to GitHub logins, and a wrong row grants the wrong access. It gives no way to act as the user in a later stage.
*   **Reason for Rejection**: Rejected by author at decision 1, in favour of offering personal tokens and a GitHub App.

### Hand-off from CI (decision 2)

#### Alternative L: The service pulls from GitHub

*   **Pros**: No address for CI to upload to. Git fetches only what changed.
*   **Cons**: Polling delay and rate limits. The service needs a standing credential that can read code and workflow artifacts, which the token route does not provide.
*   **Reason for Rejection**: Rejected by author at decision 2, in favour of CI pushing to the service.

#### Alternative M: CI writes to a cloud storage bucket

*   **Pros**: No upload address on the service. Google verifies the workflow's token.
*   **Cons**: Cloud permission setup for every repository, a bucket name in each repository's config, and pull request events still need another route.
*   **Reason for Rejection**: Rejected by author at decision 2, in favour of CI pushing to the service.

#### Alternative N: Accept a tarball or a ready-made diff from CI

*   **Pros**: Simpler to produce and to parse than a git pack.
*   **Cons**: Neither can be checked against the commit it claims to be. A pull request that edits its workflow could upload different code under a real commit's name.
*   **Reason for Rejection**: Set aside during drafting and approved with the Technical Plan. A git pack is verified by recomputing its hashes, and the service computes the diff from two verified snapshots.

### Navigation index (decision 3)

#### Alternative O: The existing plugin exports its own index in its own format

*   **Pros**: No new tool in CI. The plugin already builds a compiler program each run, so there is one build and not two.
*   **Cons**: The most code to write and own, a format nothing else reads, and a separate exporter for each later language. An edge case in the exporter would produce a wrong definition.
*   **Reason for Rejection**: Rejected by author at decision 3, in favour of `scip-typescript`.

#### Alternative P: The existing plugin exports SCIP

*   **Pros**: One compiler build, no new tool in CI, and a published format that later languages already have indexers for.
*   **Cons**: The exporter is still this project's to own and test, and it must reproduce SCIP's symbol naming rules.
*   **Reason for Rejection**: Rejected by author at decision 3, in favour of `scip-typescript`.

#### Alternative Q: The LSIF format

*   **Pros**: An older published format with existing readers.
*   **Cons**: The documentation for its TypeScript indexer recommends moving to `scip-typescript`.
*   **Reason for Rejection**: Left out during drafting, before decision 3 was put to the author, for the reason above.

### Hosting and storage (decision 4)

#### Alternative R: One virtual machine with SQLite and a disk

*   **Pros**: Fewest parts and lowest cost. The hosted form and the local form are identical.
*   **Cons**: One machine to patch, a restart on every upgrade, and a data migration before any shared service. Data is lost if the disk fails between snapshots.
*   **Reason for Rejection**: Rejected by author at decision 4, in favour of Cloud Run, managed Postgres and a bucket.

#### Alternative S: Cloud Run with Firestore and a bucket

*   **Pros**: No always-on database charge and no schema migrations.
*   **Cons**: Telemetry needs aggregate queries that a document database handles poorly, and the data model is tied to one Google product.
*   **Reason for Rejection**: Rejected by author at decision 4, in favour of Cloud Run, managed Postgres and a bucket.

#### Alternative T: A second database engine for a developer's machine

*   **Pros**: No container needed to run the service locally.
*   **Cons**: Text search and the job queue use Postgres features. A second engine would need its own version of both.
*   **Reason for Rejection**: Set aside during drafting and approved with the Technical Plan. Postgres runs in a container locally, and only the bucket has a local-directory form.

### AI assistant (decision 5)

#### Alternative U: Embedding retrieval

*   **Pros**: One model call per question, with predictable cost.
*   **Cons**: An embedding call for every changed piece of code on every push, a vector store, and more content sent to the provider. Nearest-text retrieval misses questions such as "what calls this", which the index already answers.
*   **Reason for Rejection**: Rejected by author at decision 5, in favour of the model calling tools.

#### Alternative V: Context assembled by fixed rules

*   **Pros**: One model call, and an exact record of what was sent.
*   **Cons**: It cannot follow a chain beyond what the rules fetch, so open-ended questions about distant code get poor answers.
*   **Reason for Rejection**: Rejected by author at decision 5, in favour of the model calling tools.

#### Alternative W: Switch to a second provider when quota runs out

*   **Pros**: AI features keep working through a quota outage.
*   **Cons**: A repository's code goes to a second vendor, and answers change with the model.
*   **Reason for Rejection**: Rejected by author: only the provider configured for a repository may receive its code. AI features show a quota message.

### Where the brief, the review and the scan are produced

#### Alternative X: Produce the change brief, the AI review and the secrets scan in CI

*   **Pros**: The CLI already has a model lane and a secrets scanner in CI. The service would only store and display results. The concepts brief proposed this for the change brief.
*   **Cons**: A pull request can change its own workflow, so it could alter its own brief or report a clean scan. Model keys would live in every repository as well as in the service, with two budgets.
*   **Reason for Rejection**: Set aside during drafting and approved with the Technical Plan. The service generates all three from verified source, behind one outbound door.

#### Alternative Y: A separate queue product for background work

*   **Pros**: Managed retries and rate limits.
*   **Cons**: One more product to configure, and a backlog that cannot be shown beside each pull request without copying its state.
*   **Reason for Rejection**: Set aside during drafting and approved with the Technical Plan. A job table in Postgres is visible on the status page and runs the same way on a developer's machine.

### Settled when the draft was checked against the concepts brief

#### Alternative AA: Poll GitHub on a timer with a designated token

*   **Pros**: Missed events are caught without waiting for a viewer. The concepts brief proposed it.
*   **Cons**: A standing credential that reads every connected repository. It fails without notice if that token expires or is revoked.
*   **Reason for Rejection**: Rejected by author: the service learns of changes from CI uploads and the events workflow, and reads the live state from GitHub with the viewer's credential when a view is opened.

#### Alternative AB: Upload every pushed branch

*   **Pros**: A branch with no pull request gets a diff and text search, as the concepts brief proposed.
*   **Cons**: A CI run for every push to every branch, and more stored commits.
*   **Reason for Rejection**: Rejected by author: the first version shows pull requests only. A branch appears once a pull request is opened for it.

#### Alternative AC: Trial the existing model lane with wider inputs before building

*   **Pros**: Tests at almost no cost whether more context improves the AI reviewer's findings, which is the premise of the change brief and the AI review.
*   **Cons**: A short delay, and a model key in one repository's CI for the trial.
*   **Reason for Rejection**: Rejected by author: the check is left out, and the premise is first tested when the brief and AI review ship in PR-57.

### Not evaluated

#### Alternative Z: A browser-hosted VS Code with the GitHub Pull Requests extension

*   **Pros**: As described by one reviewer from memory: compiler-backed navigation, the pull request diff, comments posted under the reviewer's own name, hotkeys, and GitHub's own access control, with no viewer to build.
*   **Cons**: As described by the same reviewer: weak on a phone. It offers no change brief, no intent context and no assistant tied to the pull request. It also needs a checkout with dependencies per pull request, which is the server-side execution this design avoids.
*   **Reason for Rejection**: Not rejected, and not evaluated. Nobody has tested the claims above. The author chose to build the workspace, which requires phone use and AI help tied to the change. This entry records that the comparison was not made.

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

--------------------------------------------------------------------------------

### Additional Resources

*   [`concepts_brief.md`](./concepts_brief.md): the concept, the seven-lens review and the author's decisions.
*   [`docs/architecture.md`](./docs/architecture.md) and [`docs/adr/`](./docs/adr/): the existing CLI and its recorded decisions.
*   [`docs/providers.md`](./docs/providers.md): the model access paths the CLI supports today.
*   GitHub documentation consulted for the identity and hand-off decisions: personal access token policy, OAuth App restrictions, the collaborators API, the Actions OpenID Connect reference and workflow artifacts.
*   `scip-typescript` and the SCIP format, published by Sourcegraph.

### Future Improvements

*   **Learnings and risk labels**: the second stage. Candidates in an inbox, accepted learnings as repository files, a written risk list and labels in shadow mode.
*   **Commenting from the workspace**: the third stage. Comments as a pending batch posted to GitHub, and a "send to agent" action.
*   **Unread merges, planning, deployment and landed impact**: the fourth stage.
*   **Navigation for more languages**: SCIP indexers exist for other languages. Each needs a workflow step and no change to the service's reader.
*   **Posting the workspace's AI review to GitHub**: once the review is trusted, it can replace the CLI's model lane in each repository.

--------------------------------------------------------------------------------

## Detailed Implementation Plan

The work is fifteen pull requests, numbered on from the existing plan's PR-46. Each leaves `make check` passing. The fourth column says what a reviewer can do once it has merged.

| PR | Adds | ADRs it lands | Usable after |
| --- | --- | --- | --- |
| PR-47 | Service skeleton: binary, config, database migrations, logging, health address, container image, CI job | 0029, 0030 | Nothing yet |
| PR-48 | Verified ingest: token check, pack verification, file store | 0031 | Nothing yet |
| PR-49 | `reviewer publish`, the `workspace` config block, workflow and action changes | None | Commits arrive at the service |
| PR-50 | Identity (token route), sessions, access check | 0033 | Sign-in |
| PR-51 | Source API: pull request list and view, files, changed files, diff, text search | None | Nothing visible yet |
| PR-52 | Web application: sign-in, pull request list, diff and new-code views, code view, GitHub links, hotkeys, touch sheet | None | **A read-only viewer beside GitHub** |
| PR-53 | Navigation: index step in CI, SCIP load and lookup, definition and references in the web application | 0035 | **Code navigation** |
| PR-54 | Secrets scan at ingest | None | Nothing visible yet |
| PR-55 | Model gateway: conversation interface, two provider adapters, checks, budgets, usage ledger | 0032, 0034 | Nothing visible yet |
| PR-56 | Assistant: tool loop, streaming API, assistant panel, Markdown and diagram rendering | None | **AI questions about code** |
| PR-57 | Jobs, change brief, AI review, pull request events | None | **Brief and AI review** |
| PR-58 | Agent skill: intent record and "address review comments" | None | Intent in briefs |
| PR-59 | Telemetry, baseline, status page and notices | None | Measures and notices |
| PR-60 | Identity (App route) and its event receiver | None | Sign-in through the App |
| PR-61 | Release of the container image, retention job, documentation | 0036 | First version complete |

PR-52, PR-53 and PR-56 are the three points at which the team can try a new feature and give feedback.

### Shared definitions

These are fixed here so that no pull request has to decide them.

#### Service configuration

A YAML file named by `--config`, plus environment variables for every secret. No address, organisation or model name is compiled in (ADR-0004).

```yaml
listen: ":8080"
publicUrl: ""             # the address users and CI reach; also the audience CI tokens must carry
github:
  apiBaseUrl: ""
  webBaseUrl: ""          # used to build links to GitHub
actions:
  issuer: ""              # the issuer of GitHub Actions identity tokens
identity:
  routes: [token]         # token, app, or both
  app: { id: "", clientId: "", slug: "" }
blobs: { bucket: "" }     # or { dir: "" } on a developer's machine
keys: { kms: "" }         # or { file: "" } on a developer's machine
scheduler: { issuer: "", audience: "", serviceAccount: "" }
limits: { packBytes: 0, fileBytes: 0, indexBytes: 0, toolRounds: 0, questionTokens: 0 }
retention: { closedPullRequestDays: 0, conversationDays: 0, telemetryDays: 0 }
repositories:
  - owner: ""
    name: ""
    provider: ""          # anthropic or gemini
    model: ""
    baseUrl: ""
    apiKeyEnv: ""         # the NAME of the environment variable holding the key
    monthlyTokenBudget: 0
```

Environment variables: `WORKSPACE_DATABASE_URL`, one variable per repository's `apiKeyEnv`, and for the App route `WORKSPACE_APP_PRIVATE_KEY`, `WORKSPACE_APP_CLIENT_SECRET` and `WORKSPACE_APP_WEBHOOK_SECRET`. A limit or retention value of zero is a configuration error, so no default is chosen on the operator's behalf.

#### Addition to `.review/config.yaml`

```yaml
workspace:
  url: ""                 # empty means publish does nothing
  index:
    typescript:
      command: []         # written by `reviewer init`; runs scip-typescript at a pinned version
      output: index.scip
```

#### Upload format

`POST /ingest/v1/commits` is a multipart request with three parts: `meta` (JSON), `pack` (a git pack) and `index` (a SCIP file, optional).

```json
{
  "schemaVersion": 1,
  "repository": "owner/name",
  "commit": "<40 hex>",
  "kind": "pull_request | default_branch | merge_base",
  "pullRequest": { "number": 0, "title": "", "body": "", "author": "", "state": "open",
                   "baseCommit": "", "headCommit": "" },
  "index": { "tool": "scip-typescript", "toolVersion": "", "language": "typescript" }
}
```

#### How `reviewer publish` builds the pack

```text
haves   = POST /ingest/v1/have with the merge base and the default-branch head
wanted  = objects listed by `git ls-tree -r -t <commit>` plus the root tree and the commit object
known   = union of `git ls-tree -r -t <have>` for each commit the service holds
pack    = `git pack-objects --stdout` fed (wanted minus known)
token   = GET $ACTIONS_ID_TOKEN_REQUEST_URL with audience = workspace.url
upload  = POST /ingest/v1/commits with meta, pack and index, bearer token
```

Entries for submodules are sent as entries with no content. Symbolic links are sent as their link text and are never followed.

#### HTTP API

| Caller | Method and path | Purpose |
| --- | --- | --- |
| CI | `POST /ingest/v1/have` | Which of these commits does the service hold |
| CI | `POST /ingest/v1/commits` | Upload one commit |
| CI | `POST /ingest/v1/events` | Pull request closed, reopened or edited |
| Scheduler | `POST /internal/jobs/run` | Run due jobs |
| GitHub | `POST /hooks/github` | Events from the App, where installed |
| Browser | `POST /auth/token`, `GET /auth/app/start`, `GET /auth/app/callback`, `POST /auth/signout` | Sign in and out |
| Browser | `GET /api/v1/me` | The signed-in user and the routes on offer |
| Browser | `GET /api/v1/repos` | Repositories the user can read |
| Browser | `GET /api/v1/repos/{owner}/{repo}/pulls` | Open pull requests |
| Browser | `GET /api/v1/repos/{owner}/{repo}/pulls/{number}` | Head and base commit, and the stage list |
| Browser | `GET .../commits/{sha}/tree` | Paths at a commit |
| Browser | `GET .../commits/{sha}/files?path=` | One file's content |
| Browser | `GET .../compare?base=&head=` | Changed files and their hunks |
| Browser | `GET .../commits/{sha}/search?q=&path=` | Text search |
| Browser | `GET .../commits/{sha}/definition?path=&line=&col=` | Definition, with how it was obtained |
| Browser | `GET .../commits/{sha}/references?path=&line=&col=` | References |
| Browser | `POST .../navigation/reports` | "This was wrong" |
| Browser | `GET .../pulls/{number}/brief?commit=` and `.../review?commit=` | Stored brief and AI review |
| Browser | `POST .../conversations` | Start a conversation |
| Browser | `POST .../conversations/{id}/messages` | Ask; the answer is a server-sent event stream |
| Browser | `POST /api/v1/events` | Report usage events |
| Browser | `GET .../measures?from=&to=` and `.../measures/export?format=` | Telemetry and its export |
| Browser | `GET /api/v1/status` | Health of each dependency, and current notices |
| Anyone | `GET /healthz` | Liveness |

Every `/api/v1/repos/...` request passes the access check before any data is read.

#### Database tables

| Table | Columns |
| --- | --- |
| `repository` | `id`, `org`, `name`, `provider`, `model`, `base_url`, `api_key_env`, `monthly_token_budget`, `accepting_uploads` |
| `commit` | `repository_id`, `sha`, `tree_sha`, `received_at`, `verified_at` |
| `commit_stage` | `repository_id`, `sha`, `stage`, `state`, `reason`, `started_at`, `finished_at` |
| `path_entry` | `repository_id`, `sha`, `path`, `mode`, `file_hash` |
| `file` | `repository_id`, `hash`, `size`, `is_text`, `content` (trigram index) |
| `file_scan` | `repository_id`, `file_hash`, `scanner_version`, `state`, `rule_ids`, `scanned_at` |
| `index_entry` | `repository_id`, `sha`, `path`, `document_hash`, `tool`, `tool_version` |
| `occurrence` | `repository_id`, `document_hash`, `symbol`, `start_line`, `start_col`, `end_line`, `end_col`, `role` |
| `pull_request` | `repository_id`, `number`, `head_sha`, `base_sha`, `title`, `body`, `author_login`, `state`, `source`, `updated_at` |
| `brief` | `repository_id`, `sha`, `pull_number`, `body`, `model`, `inputs`, `state`, `created_at` |
| `ai_review` | `repository_id`, `sha`, `pull_number`, `findings`, `model`, `state`, `created_at` |
| `conversation` | `id`, `repository_id`, `sha`, `pull_number`, `user_id`, `created_at` |
| `message` | `id`, `conversation_id`, `role`, `content`, `tool_calls`, `created_at` |
| `model_call` | `id`, `repository_id`, `user_id`, `feature`, `sha`, `provider`, `model`, `sent`, `scan_decision`, `input_tokens`, `output_tokens`, `outcome`, `reset_at`, `started_at`, `duration_ms` |
| `app_user` | `id`, `github_id`, `login` |
| `credential` | `user_id`, `route`, `ciphertext`, `refresh_ciphertext`, `expires_at` |
| `session` | `id_hash`, `user_id`, `created_at`, `expires_at` |
| `usage_event` | `id`, `repository_id`, `user_id`, `pull_number`, `sha`, `kind`, `detail`, `at` |
| `navigation_report` | `id`, `repository_id`, `sha`, `path`, `line`, `col`, `symbol`, `user_id`, `at` |
| `job` | `id`, `kind`, `repository_id`, `target`, `state`, `attempts`, `run_after`, `last_error`, `created_at`, `finished_at` |
| `baseline` | `repository_id`, `measured_at`, `measures` |

Files are keyed by repository and hash, not by hash alone. Two repositories that hold the same file each store it, so no record is shared across an access boundary.

#### Gateway interface

```text
Part     = Instruction(text) | UserText(text) | Excerpt{fileHash, path, startLine, endLine, text}
Request  = { repository, user, feature, commit, parts, tools, maxTokens }
Event    = TextDelta(text) | ToolCall{id, name, arguments} | Done{inputTokens, outputTokens}

Converse(ctx, Request) -> stream of Event      # assistant
Complete(ctx, Request) -> text, usage          # brief and review jobs

Outcome  = ok | quota_exhausted{resetAt} | rate_limited{retryAfter} | budget_blocked{budget}
         | secret_blocked{paths} | unscanned{paths} | refused | timed_out | malformed
```

Both calls run the same steps in the same order: check each excerpt's scan result, check typed text, check the budget, call the provider, record the `model_call` row. No other package imports a provider adapter.

#### New dependencies

| Dependency | Used for | Licence |
| --- | --- | --- |
| `github.com/jackc/pgx/v5` | Postgres | MIT |
| `github.com/go-git/go-git/v5` | Reading and verifying a git pack; producing unified diffs | Apache-2.0 |
| SCIP Go bindings and `google.golang.org/protobuf` | Reading a SCIP file. The import path is to be confirmed; the schema names `github.com/scip-code/scip/bindings/go/scip` | Apache-2.0, BSD-3-Clause |
| `github.com/coreos/go-oidc/v3` | Verifying signed tokens from GitHub Actions and Cloud Scheduler | Apache-2.0 |
| `cloud.google.com/go/storage`, `cloud.google.com/go/kms` | Bucket and key access | Apache-2.0 |
| `react`, `react-dom`, `react-router-dom` | Web application | MIT |
| `@codemirror/state`, `view`, `merge`, `language`, `lang-javascript`, `lang-python`, `lang-rust` | Code and diff display | MIT |
| `markdown-it`, `dompurify`, `mermaid` | Rendering briefs, answers and diagrams | MIT, Apache-2.0, MIT |
| `vite`, `vitest`, `@playwright/test`, `typescript` | Build and test only | MIT, Apache-2.0 |

Provider keys reach the service as environment variables, which Cloud Run fills from Secret Manager. No Secret Manager client is linked. The HTTP server, routing, logging and migrations use the Go standard library.

Every new `.go`, `.ts`, `.tsx`, `.css`, `.sql` and `.html` file carries the SPDX header that `make license-check` enforces.

### Target Files to Create/Modify

#### 1. PR-47: service skeleton

| File | Action | What changes and why |
| --- | --- | --- |
| `cmd/workspace/main.go` | Create | Entry point with subcommands `serve`, `migrate` and `version`. A separate binary keeps the CLI free of database and web code. |
| `cmd/workspace/serve.go` | Create | Builds the config, stores and handlers and starts the HTTP server with graceful shutdown. |
| `cmd/workspace/main_test.go` | Create | Usage errors, `version`, and `serve` starting against a test database. |
| `internal/workspace/wsconfig/config.go` | Create | The configuration above, a `Secrets` type that never prints its values, and validation that reports every problem at once. |
| `internal/workspace/wsconfig/config_test.go` | Create | Zero limits rejected; unknown provider rejected; secrets absent from printed output. |
| `internal/workspace/store/store.go` | Create | Connection pool and a transaction helper. |
| `internal/workspace/store/migrate.go` | Create | Applies embedded, numbered SQL files in order and records each in a version table. |
| `internal/workspace/store/migrate_test.go` | Create | Applies all migrations to an empty database twice; the second run changes nothing. |
| `internal/workspace/store/migrations/0001_core.sql` | Create | `repository`, `commit`, `commit_stage`, `job`. Enables the trigram extension. |
| `internal/workspace/store/repos.go` | Create | Syncs the configured repositories into `repository` at start. |
| `internal/workspace/store/repos_test.go` | Create | Added, changed and removed repositories. |
| `internal/workspace/obs/log.go` | Create | Structured logger carrying request, user, repository, pull request and commit. Passes every message through `model.Redact`. |
| `internal/workspace/obs/log_test.go` | Create | A credential-shaped string in a message is redacted. |
| `internal/workspace/api/router.go` | Create | Route table using the standard library's pattern routing. Starts with `/healthz`. |
| `internal/workspace/api/middleware.go` | Create | Request identifier, logging and panic recovery. |
| `internal/workspace/api/api_test.go` | Create | `/healthz` answers; an unknown path returns 404. |
| `internal/workspace/testdb/testdb.go` | Create | Test helper that connects to the database named by `WORKSPACE_TEST_DATABASE_URL`. Skips when unset, and fails when `REVIEWER_REQUIRE_TOOLS` is set, matching the existing rule for missing tools. |
| `Dockerfile` | Create | Builds the binary and the web files, and installs the pinned `gitleaks` with `.github/scripts/install-tools.sh`. |
| `.dockerignore` | Create | Keeps `node_modules`, `dist` and `.git` out of the build context. |
| `examples/workspace.config.yaml` | Create | An example configuration with placeholder values. |
| `go.mod`, `go.sum` | Modify | Add `pgx`. |
| `Makefile` | Modify | Add `workspace` to the build and a `workspace-test` target. Extend the licence ignore list for `web/dist`. |
| `.gitignore` | Modify | Ignore the `workspace` binary and `web/dist`. |
| `.github/workflows/ci.yml` | Modify | Add a `workspace` job with a Postgres service container, and add it to the `required` job's list. |
| `docs/adr/0029-add-a-hosted-workspace-service.md` | Create | Supersedes ADR-0008. Restates that the CLI stays a stateless binary and adds the hosted service beside it. |
| `docs/adr/0030-service-store-beside-github.md` | Create | Supersedes ADR-0016. Restates that GitHub comments stay the CLI's store and the record for comments and approvals, and adds the service's own store for what GitHub cannot hold. |
| `docs/adr/0008-cli-not-service.md` | Modify | Metadata only: status `Superseded`, superseded by ADR-0029. The body is untouched, as `tools/checkadrs` requires. |
| `docs/adr/0016-github-comments-as-the-store.md` | Modify | Metadata only: status `Superseded`, superseded by ADR-0030. |
| `docs/adr/README.md` | Modify | Index rows for the new ADRs and the changed statuses. Repeated in each later pull request that adds an ADR. |
| `THIRD_PARTY_LICENSES.md` | Modify | Inventory the new dependency. Repeated in each later pull request that adds one. |
| `docs/implementation-plan.md` | Modify | Append a section listing PR-47 to PR-61 with a link to this design. |

#### 2. PR-48: verified ingest

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/workspace/oidc/verify.go` | Create | Verifies a signed token against a configured issuer and audience, and returns its claims. Used for GitHub Actions tokens here and for the scheduler later. |
| `internal/workspace/oidc/verify_test.go` | Create | Wrong issuer, wrong audience, expired, bad signature, and a repository not in the configured list are each rejected. |
| `internal/workspace/gitpack/pack.go` | Create | Reads a pack, recomputes each object's hash, checks the commit object matches the claimed commit, and walks the tree to confirm every entry is in the pack or already stored. Enforces the size limits. |
| `internal/workspace/gitpack/pack_test.go` | Create | A valid pack is accepted. A pack with one altered file, a pack for a different commit, an oversized pack and a path that escapes the tree are each rejected. Packs are built in the test with `git`. |
| `internal/workspace/blob/blob.go` | Create | `Store` interface: put, get, delete. |
| `internal/workspace/blob/local.go` | Create | Directory implementation for a developer's machine and for tests. |
| `internal/workspace/blob/gcs.go` | Create | Bucket implementation. |
| `internal/workspace/blob/blob_test.go` | Create | The same test set run against the local implementation, and against the bucket when credentials are present. |
| `internal/workspace/store/migrations/0002_source.sql` | Create | `path_entry`, `file`, `pull_request`. |
| `internal/workspace/store/commits.go` | Create | Insert a commit and its path entries in one transaction; record stage states. |
| `internal/workspace/store/files.go` | Create | Insert new files; look up which hashes a repository already holds. |
| `internal/workspace/store/pulls.go` | Create | Upsert a pull request row, keeping the `source` of each field. |
| `internal/workspace/store/source_test.go` | Create | Tests for the three files above against a real database. |
| `internal/workspace/ingest/ingest.go` | Create | The pipeline for one upload: verify, store raw upload, store files and entries, set stages. |
| `internal/workspace/ingest/handler.go` | Create | `POST /ingest/v1/have` and `POST /ingest/v1/commits`. |
| `internal/workspace/ingest/ingest_test.go` | Create | A full upload end to end; an upload whose token names another repository is refused; a repeated upload changes nothing. |
| `schema/workspace-upload.schema.json` | Create | The upload metadata format. Both the CLI's and the service's tests validate against it. |
| `internal/workspace/api/router.go` | Modify | Mount the ingest routes. |
| `go.mod`, `go.sum` | Modify | Add `go-git`, `go-oidc` and the storage client. |
| `docs/adr/0031-treat-ci-uploads-as-untrusted.md` | Create | Records the trust rule and the verification by content hash. |

#### 3. PR-49: `reviewer publish` and the workflow

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/publish/pack.go` | Create | Lists the objects to send and runs `git pack-objects`, following the steps under Shared definitions. |
| `internal/publish/token.go` | Create | Requests the job's identity token from the address and credential GitHub Actions provides in the environment. |
| `internal/publish/upload.go` | Create | The `have` call and the multipart upload, with a bounded retry. |
| `internal/publish/publish.go` | Create | Orchestrates one publish. Any failure is a warning and the command exits 0 (ADR-0009). |
| `internal/publish/publish_test.go` | Create | Against a fake service: first upload sends the whole tree, the second sends only changed objects, and a rejected token produces a warning and exit 0. |
| `cmd/reviewer/publish.go` | Create | The `publish` subcommand and its `--event` form. |
| `cmd/reviewer/publish_test.go` | Create | Flag handling, and "does nothing" when `workspace.url` is empty. |
| `cmd/reviewer/flags.go` | Modify | Register `publish`, `--event` and `--workspace-url`, and reject them on other subcommands as the existing flags are. |
| `cmd/reviewer/main.go` | Modify | Dispatch `publish`. |
| `cmd/reviewer/main_test.go` | Modify | Cover the new dispatch. |
| `internal/config/config.go` | Modify | Add the `Workspace` block and validate it. |
| `internal/config/config_test.go` | Modify | Tests for the new block. |
| `action.yml` | Modify | New inputs `review` (default true) and `publish` (default false). The history check and the review step run only when `review` is true, so a default-branch push can install the binary and publish without a base ref. |
| `internal/installer/templates/review.yml.tmpl` | Modify | Add `id-token: write`, a `push` trigger for the default branch, and the publish step. Written only when a workspace address is given. |
| `internal/installer/templates/config.yaml.tmpl` | Modify | Write the `workspace` block. |
| `internal/installer/write.go` | Modify | Template data for the workspace address and default branch. |
| `internal/installer/write_test.go` | Modify | Golden files with and without a workspace address. |
| `internal/installer/plan.go` | Modify | Note in the plan output that the workflow gains a permission, and why. |
| `internal/installer/plan_test.go` | Modify | Cover the note. |
| `cmd/reviewer/init.go` | Modify | Accept `--workspace-url` and pass it to the installer. |
| `cmd/reviewer/init_test.go` | Modify | Cover the flag. |
| `testdata/` golden files for the installer | Modify | Regenerated with the `-update` flag. |

#### 4. PR-50: identity (token route), sessions and access

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/workspace/keys/keys.go` | Create | `Cipher` interface: encrypt, decrypt. |
| `internal/workspace/keys/kms.go` | Create | Cloud KMS implementation. |
| `internal/workspace/keys/local.go` | Create | A key-file implementation for a developer's machine. Refuses to start when the config also names a bucket, so it cannot be used in the hosted form by mistake. |
| `internal/workspace/keys/keys_test.go` | Create | Round trip; a tampered ciphertext is rejected. |
| `internal/workspace/identity/identity.go` | Create | The `Route` interface: sign in, client for a user, optional service client. |
| `internal/workspace/identity/token.go` | Create | Token route: validate a pasted token with GitHub, reject one that carries write permissions it does not need, store it encrypted. |
| `internal/workspace/identity/session.go` | Create | Session cookie issue, lookup and sign-out. Only a hash of the session identifier is stored. |
| `internal/workspace/identity/identity_test.go` | Create | Against a fake GitHub: valid token, revoked token, expired session. |
| `internal/workspace/access/access.go` | Create | `CanRead(user, repository)`: asks GitHub with the user's credential, caches the answer for a few minutes, and never caches across users. |
| `internal/workspace/access/middleware.go` | Create | Runs the check for every `/api/v1/repos/...` route, and returns the same response for "no access" and "does not exist". |
| `internal/workspace/access/access_test.go` | Create | Allowed, denied, cache expiry, and access removed on GitHub taking effect after the cache period. |
| `internal/workspace/store/migrations/0003_identity.sql` | Create | `app_user`, `credential`, `session`. |
| `internal/workspace/store/identity.go` | Create | Queries for the three tables. |
| `internal/workspace/store/identity_test.go` | Create | Tests against a real database. |
| `internal/workspace/api/auth.go` | Create | `POST /auth/token`, `POST /auth/signout`, `GET /api/v1/me`. |
| `internal/workspace/api/middleware.go` | Modify | Session lookup, and a same-site and header check on requests that change state. |
| `internal/ghclient/client.go` | Modify | Add `GetRepo`, which reports whether the token can read a repository, and the head-commit field needed by the view. |
| `internal/ghclient/client_test.go` | Modify | Cover `GetRepo`. |
| `go.mod`, `go.sum` | Modify | Add the KMS client. |
| `docs/adr/0033-two-identity-routes-one-interface.md` | Create | Records the interface and both routes. |

#### 5. PR-51: source API

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/workspace/catalog/pulls.go` | Create | Lists and views pull requests. Reads the live list, head commit, title and state from GitHub as the viewer, and joins the stage list from the store. |
| `internal/workspace/catalog/catalog_test.go` | Create | A pull request GitHub knows and the service has not received shows as "waiting for CI". The head commit comes from GitHub, not from the upload. |
| `internal/workspace/source/files.go` | Create | Tree listing and file content at a commit. |
| `internal/workspace/source/diff.go` | Create | Changed files from two path lists, and unified hunks per file. |
| `internal/workspace/source/search.go` | Create | Text search at a commit over the trigram index, with a path filter and a result limit. |
| `internal/workspace/source/source_test.go` | Create | Added, removed, renamed and binary files; search finds a match in an unchanged file. |
| `internal/workspace/api/pulls.go` | Create | Handlers for repositories, pull request list and pull request view. |
| `internal/workspace/api/source.go` | Create | Handlers for tree, file, compare and search. |
| `internal/workspace/api/api_test.go` | Modify | The access test: every route above returns the "not found" response for a user without access. Each later pull request adds its routes to this test. |

#### 6. PR-52: web application

| File | Action | What changes and why |
| --- | --- | --- |
| `web/package.json`, `web/package-lock.json` | Create | Dependencies and scripts: `build`, `test`, `e2e`, `lint`. |
| `web/tsconfig.json`, `web/vite.config.ts`, `web/eslint.config.js`, `web/playwright.config.ts` | Create | Build, lint and test configuration. |
| `web/index.html` | Create | The single page. |
| `web/src/main.tsx`, `web/src/App.tsx`, `web/src/routes.tsx` | Create | Start-up and the route table: sign-in, pull request list, pull request view, code view, telemetry, status. |
| `web/src/api/client.ts`, `web/src/api/types.ts` | Create | Typed calls to the API above, with one place that handles "signed out" and "no access". |
| `web/src/views/SignIn.tsx` | Create | Offers the routes the service reports. For the token route, explains which permissions to grant. |
| `web/src/views/PullList.tsx` | Create | Open pull requests per repository, with their stage state. |
| `web/src/views/PullView.tsx` | Create | The pull request view: header with the commit on screen, file list, diff. |
| `web/src/views/CodeView.tsx` | Create | A file outside the diff, marked as outside the change, with "Back to diff". |
| `web/src/components/CodePane.tsx` | Create | CodeMirror wrapper: read-only, syntax highlighting, in-file search, line and token tap targets. |
| `web/src/components/DiffView.tsx` | Create | Side-by-side on wide screens and single-column on narrow ones. Toggle between diff and new code. Knows which side a position belongs to. |
| `web/src/components/StageList.tsx` | Create | Shows each stage for the commit, with the reason when one failed. |
| `web/src/components/ActionSheet.tsx` | Create | The one sheet opened by tapping a symbol or line number. |
| `web/src/components/CommandPalette.tsx`, `web/src/components/ShortcutHelp.tsx` | Create | Reach every action by name, and list the shortcuts. |
| `web/src/keys/keymap.ts`, `web/src/keys/useHotkeys.ts` | Create | The shortcut table from the Technical Plan, with chords and the rule that single keys are off in text fields. |
| `web/src/github/links.ts` | Create | Builds the address of a file and line on GitHub's pull request page, from the configured web address. |
| `web/src/styles/app.css` | Create | Layout for wide and narrow screens. |
| `web/src/keys/keymap.test.ts`, `web/src/github/links.test.ts`, `web/src/components/DiffView.test.tsx` | Create | No shortcut uses a browser-reserved key; single keys do not fire in a text field; links carry the right file and line; side selection for removed and added lines. |
| `web/e2e/journeys.spec.ts` | Create | Journeys 2, 4, 8, 9 and 10 against a fake API, at a desktop and a phone screen size. |
| `web/e2e/fake-api.ts` | Create | A small fixed-response server for the journeys. |
| `internal/workspace/web/embed.go` | Create | Embeds `web/dist` in the binary and serves it, falling back to `index.html` for application routes. |
| `internal/workspace/web/embed_test.go` | Create | A known asset is served; an API path is not shadowed. |
| `internal/workspace/api/router.go` | Modify | Mount the web files. |
| `Makefile` | Modify | `web` and `web-test` targets; `check` builds the web files before the Go build. |
| `.github/workflows/ci.yml` | Modify | A `web` job: install, lint, unit tests, journeys. Added to `required`. |
| `tools/checklicenses/main.go` | Modify | Inventory the web application's shipped dependencies in full, as it does the plugin's (ADR-0028). |
| `tools/checklicenses/main_test.go` | Modify | Cover the second package tree. |

#### 7. PR-53: navigation

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/publish/index.go` | Create | Runs the configured index command and locates its output. A failure is a warning, and the upload continues without an index. |
| `internal/publish/publish.go` | Modify | Attach the index and its tool version to the upload. |
| `internal/publish/publish_test.go` | Modify | With and without an index; a failing index command. |
| `internal/installer/templates/config.yaml.tmpl` | Modify | Write the index command with a pinned `scip-typescript` version when a `tsconfig` was detected. |
| `internal/installer/write.go`, `internal/installer/write_test.go` | Modify | Template data and golden files for the index command. |
| `internal/workspace/navigation/load.go` | Create | Parses a SCIP file within the size limit, drops documents whose path is not in the commit, checks every position against the file, and stores each document once by hash. |
| `internal/workspace/navigation/query.go` | Create | Definition and references at a position. A definition is returned as compiler-resolved only if the name at the target matches. Otherwise falls back to text search and labels the result. |
| `internal/workspace/navigation/navigation_test.go` | Create | A real index for a fixture project: a definition in an unchanged file, an interface with two implementations returned as a list, a local symbol, a position outside the file rejected, and a tampered index whose target name does not match returning text search. |
| `testdata/workspace/tsproject/` | Create | The small TypeScript fixture project and its checked-in SCIP file. |
| `internal/workspace/store/migrations/0004_index.sql` | Create | `index_entry`, `occurrence`, `navigation_report`. |
| `internal/workspace/store/index.go`, `internal/workspace/store/index_test.go` | Create | Queries for the three tables. An unchanged document is not stored twice across commits. |
| `internal/workspace/ingest/ingest.go` | Modify | Load the index during the upload and set the `indexed` stage. |
| `internal/workspace/api/nav.go` | Create | Definition, references and "this was wrong". |
| `web/src/nav/navigation.ts` | Create | Lookup calls, the base-or-head rule, and history entries so back and forward work. |
| `web/src/components/References.tsx` | Create | The list for references, and for several candidate definitions, labelled by how each was obtained. |
| `web/src/components/CodePane.tsx`, `web/src/components/ActionSheet.tsx`, `web/src/keys/keymap.ts` | Modify | Wire go-to-definition and find-references to keys and to the sheet. |
| `web/src/nav/navigation.test.ts`, `web/e2e/journeys.spec.ts` | Create, Modify | The base-or-head rule; journeys 2 and 3 with real lookups. |
| `go.mod`, `go.sum` | Modify | Add the SCIP bindings and protobuf. |
| `docs/adr/0035-scip-index-built-in-ci.md` | Create | Records the format, the tool and where it runs. |

#### 8. PR-54: secrets scan at ingest

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/analyzers/gitleaks/gitleaks.go` | Modify | Export `ScanDir`, a directory scan built on the existing `scan`, so the service reuses the same invocation and the same "no report means no scan" rule. |
| `internal/analyzers/gitleaks/gitleaks_test.go` | Modify | Cover `ScanDir`. |
| `internal/workspace/scan/scan.go` | Create | Writes a commit's new files to a temporary directory under their real paths, runs the scanner with the service's own rule file, records a result per file, and removes the directory. An error records "could not scan". |
| `internal/workspace/scan/scan_test.go` | Create | A fixture credential is found; a missing binary records "could not scan"; a file already scanned by the same scanner version is skipped. |
| `internal/workspace/store/migrations/0005_scan.sql` | Create | `file_scan`. |
| `internal/workspace/store/scan.go`, `internal/workspace/store/scan_test.go` | Create | Queries for scan results by file hash and scanner version. |
| `internal/workspace/ingest/ingest.go` | Modify | Run the scan during the upload and set the `scanned` stage. |

#### 9. PR-55: model gateway

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/workspace/gateway/gateway.go` | Create | `Converse` and `Complete`, running the fixed steps under Shared definitions. |
| `internal/workspace/gateway/provider.go` | Create | The conversation interface: turns, tool definitions, streamed events, usage. |
| `internal/workspace/gateway/anthropic.go`, `internal/workspace/gateway/gemini.go` | Create | Adapters for the two providers' streaming and tool-call formats. Addresses come from configuration. |
| `internal/workspace/gateway/outcome.go` | Create | The outcome type, including how each provider reports exhausted quota and a reset time. |
| `internal/workspace/gateway/fake.go` | Create | A scripted provider for tests, in the manner of `internal/model/fake.go`. |
| `internal/workspace/gateway/gateway_test.go` | Create | An excerpt with no scan result is refused. A secret in a changed file blocks the whole commit. A secret in an unchanged file leaves that file out and names it. Typed text with a credential is refused. A budget is enforced before the call. Every call writes one `model_call` row. |
| `internal/workspace/gateway/anthropic_test.go`, `internal/workspace/gateway/gemini_test.go` | Create | Recorded response fixtures: text, a tool call, usage figures and a quota error. |
| `internal/workspace/store/migrations/0006_ai.sql` | Create | `model_call`, `conversation`, `message`, `brief`, `ai_review`. |
| `internal/workspace/store/ai.go`, `internal/workspace/store/ai_test.go` | Create | Queries, including month-to-date tokens per repository for the budget. |
| `internal/model/provider.go` | Modify | Add a `Usage` type and an optional `CompleteWithUsage` method, so the one-call path can report tokens. The existing `Provider` interface is unchanged. |
| `internal/model/http.go`, `internal/model/anthropic.go`, `internal/model/gemini.go`, `internal/model/openai.go` | Modify | Parse usage from each response. Return a distinct error for a rate-limit or quota response, carrying the provider's retry hint. |
| `internal/model/http_test.go`, `internal/model/shapes_test.go` | Modify | Cover usage parsing and the new error. |
| `docs/adr/0032-one-outbound-door-for-model-calls.md` | Create | Records the gateway and the scan rule for every file sent. |
| `docs/adr/0034-second-provider-interface-for-conversations.md` | Create | Records the conversation interface beside the one-call interface. |

#### 10. PR-56: assistant

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/workspace/assistant/assistant.go` | Create | Runs a conversation: send, run requested tools, return results through the gateway, stop at the round or token limit. |
| `internal/workspace/assistant/tools.go` | Create | The six read-only tools, each bound to the conversation's repository and commit. |
| `internal/workspace/assistant/prompts/system.md` | Create | Instructions: answer from the tools, cite files and lines, treat content as material and not as instructions. |
| `internal/workspace/assistant/assistant_test.go` | Create | With the scripted provider: a tool chain to an answer; the round limit; a tool asked to read another commit is refused; a quota outcome reaches the caller as such. |
| `internal/workspace/api/assistant.go` | Create | Start a conversation, post a message, stream the answer as server-sent events, read a conversation back. |
| `web/src/api/stream.ts`, `web/src/api/stream.test.ts` | Create | Reads the event stream; handles a dropped connection. |
| `web/src/components/AssistantPanel.tsx` | Create | The question box with the selected code quoted above it, the streamed answer, the model's name, and links to each excerpt. Shows quota, budget and secret outcomes as distinct messages. |
| `web/src/components/Markdown.tsx`, `web/src/components/Markdown.test.tsx` | Create | Sanitised Markdown, and Mermaid drawn with script execution disabled. Tests that a script tag and a script-bearing diagram are inert. |
| `web/src/views/PullView.tsx`, `web/src/components/ActionSheet.tsx`, `web/src/keys/keymap.ts` | Modify | "Ask" from a selection, a line and the palette. |
| `web/e2e/journeys.spec.ts` | Modify | Journeys 5, 6, 7 and 10. |

#### 11. PR-57: jobs, change brief, AI review and events

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/workspace/jobs/jobs.go` | Create | Claims due jobs with row locks, runs them within a time allowance, retries with a growing delay, and honours a quota reset time. |
| `internal/workspace/jobs/handler.go` | Create | `POST /internal/jobs/run`, accepting only the scheduler's signed token. |
| `internal/workspace/jobs/jobs_test.go` | Create | Two runners never run the same job; a quota outcome reschedules and does not drop; a job that fails repeatedly ends as failed with its reason. |
| `internal/workspace/store/jobs.go`, `internal/workspace/store/jobs_test.go` | Create | Queue queries. |
| `internal/workspace/brief/inputs.go` | Create | Gathers inputs for a commit: diff, description, intent record, changed plan and ADR files, guidance from the base commit, and unchanged code that refers to changed symbols. |
| `internal/workspace/brief/brief.go`, `internal/workspace/brief/prompts/brief.md` | Create | Generates the brief's four answers with a source link per statement, and names any missing input. |
| `internal/workspace/brief/review.go` | Create | Runs the CLI's review, its disprove pass and its caps through the gateway's one-call path. |
| `internal/workspace/brief/brief_test.go`, `internal/workspace/brief/review_test.go` | Create | Guidance changed in the pull request is ignored in favour of the base commit's. A missing intent record is reported in the brief. A disproved finding is not shown. |
| `internal/laneb/assemble.go` | Modify | Add optional `Description` and `Intent` fields to `Input` and write them into the prompt, fenced as material. The CLI leaves them empty, so its output is unchanged. |
| `internal/laneb/assemble_test.go`, `internal/laneb/prompts/review.md` | Modify | Cover the new fields; describe them in the prompt. |
| `internal/workspace/ingest/handler.go`, `internal/workspace/ingest/ingest.go` | Modify | `POST /ingest/v1/events`; queue brief and review jobs after an upload and after an edit. |
| `internal/workspace/api/brief.go` | Create | Read the stored brief and review. |
| `cmd/workspace/main.go` | Modify | A `jobs` subcommand that runs the runner on a timer, for a developer's machine. |
| `internal/installer/templates/review-events.yml.tmpl` | Create | The events workflow: on closed, reopened and edited, run `reviewer publish --event`. |
| `internal/installer/plan.go`, `internal/installer/write.go`, their tests and golden files | Modify | Write the events workflow when a workspace address is given. |
| `web/src/components/Brief.tsx`, `web/src/components/AiReview.tsx` | Create | The brief at the top of the pull request view, and review findings beside the diff, each labelled with commit and model. |
| `web/src/views/PullView.tsx`, `web/e2e/journeys.spec.ts` | Modify | Show both; journey 1. |

#### 12. PR-58: agent skill

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/installer/templates/SKILL.md.tmpl` | Modify | Two new sections: write an intent record when opening a pull request, in a fixed fenced form; and, when the author asks, read open review threads, fix, push and reply. |
| `internal/installer/skill_test.go` and its golden file | Modify | Cover the new sections. |
| `internal/workspace/brief/inputs.go`, `internal/workspace/brief/brief_test.go` | Modify | Parse the fixed fenced form out of a description. |

#### 13. PR-59: telemetry, baseline, status and notices

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/workspace/telemetry/events.go` | Create | Accepts batches of usage events and validates their kinds. |
| `internal/workspace/telemetry/measures.go` | Create | Computes the measures: time in review, active time, navigation outcome rates, AI use, and wrong-answer reports. Exports them as CSV and JSON. |
| `internal/workspace/telemetry/baseline.go` | Create | A job that reads a repository's pull request history from GitHub once and stores the baseline figures. |
| `internal/workspace/telemetry/telemetry_test.go` | Create | Measures over a fixed event set; export format; a user sees no events from a repository they cannot read. |
| `internal/workspace/status/status.go`, `internal/workspace/status/status_test.go` | Create | Health of GitHub, the provider, the database and the job backlog, and the time of the last successful upload per repository. |
| `internal/workspace/store/migrations/0007_telemetry.sql` | Create | `usage_event`, `baseline`. |
| `internal/workspace/store/telemetry.go`, `internal/workspace/store/telemetry_test.go` | Create | Queries. |
| `internal/workspace/api/telemetry.go`, `internal/workspace/api/status.go` | Create | Handlers. |
| `web/src/telemetry/report.ts` | Create | Collects events in the browser and sends them in batches. |
| `web/src/views/Telemetry.tsx`, `web/src/views/Status.tsx`, `web/src/components/NoticeBanner.tsx` | Create | The measures view with export, the status page, and the banner. |

#### 14. PR-60: identity (App route)

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/workspace/identity/app.go` | Create | The App's web sign-in flow, token refresh, and installation tokens for background reads. |
| `internal/workspace/identity/app_test.go` | Create | Against a fake GitHub: sign-in, refresh, a revoked installation. |
| `internal/workspace/api/auth.go` | Modify | `GET /auth/app/start` and `GET /auth/app/callback`, with a state value checked on return. |
| `internal/workspace/api/hooks.go`, `internal/workspace/api/hooks_test.go` | Create | `POST /hooks/github`: checks the signature, then updates the pull request row as the events workflow does. An unsigned or wrongly signed request is refused. |
| `internal/workspace/catalog/pulls.go` | Modify | Where a service client exists, refresh pull request rows without waiting for a viewer. |

#### 15. PR-61: release, retention and documentation

| File | Action | What changes and why |
| --- | --- | --- |
| `.github/workflows/release.yml` | Modify | Build the container image from the tag and publish it to GitHub's container registry with the same version. |
| `.goreleaser.yaml` | Modify | Build the `workspace` binary for the image. |
| `internal/workspace/jobs/retention.go`, `internal/workspace/jobs/retention_test.go` | Create | Removes data for pull requests closed longer than the configured period, and conversations and events past theirs. Files still referenced by another commit are kept. |
| `docs/adr/0036-release-three-artifacts.md` | Create | Supersedes ADR-0026, restating the two existing artifacts and adding the image. |
| `docs/adr/0026-two-release-artifacts.md` | Modify | Metadata only: status `Superseded`, superseded by ADR-0036. |
| `docs/workspace.md` | Create | Operating guide: configuration, the cloud resources needed, connecting a repository, running locally, backup and restore. It names no project-specific value. |
| `docs/architecture.md` | Modify | Add the workspace: the two parts, the trust rule and the outbound door. |
| `README.md` | Modify | A short section on the workspace and a link to its guide. |
| `SECURITY.md` | Modify | The CLI's promise that no credential is stored still holds for the CLI. State what the service stores, how it is encrypted, and that the secrets rule now covers every file sent. |
| `CONTRIBUTING.md` | Modify | How to run Postgres and the web application for development. |
| `docs/providers.md` | Modify | Which access paths the service supports, and that the subscription paths do not apply to it. |
| `CHANGELOG.md` | Modify | The release entry. |

### Verification Plan

*   **Compilation/Build**: `make build` for both binaries; `npm --prefix web run build`; `docker build .`.
*   **Unit Tests**: `make test` with `WORKSPACE_TEST_DATABASE_URL` set; `npm --prefix web test`; the existing `make plugin-test`.
*   **Static Analysis & Linting**: `make vet`, `make staticcheck`, `make fmt-check`, `make license-check`, `npm --prefix web run lint`, `go run ./tools/checkadrs`, `make licenses`.
*   **Test Coverage Requirement**: every goal has a named test, listed below. No percentage target is set.
*   **E2E/Integration**: `npm --prefix web run e2e` for the journeys; the ingest test uploads a real pack built with `git`.

Tests that stand for a goal:

| Goal | Test |
| --- | --- |
| Source cannot be forged by a pull request | `gitpack`: a pack with one altered file is rejected |
| No wrong definitions | `navigation`: a tampered index whose target name does not match yields text search, never a definition |
| Nothing answered from another commit | `navigation` and journey 4: a lookup at a commit with no index returns "preparing" |
| Access mirrors GitHub | `api`: one test walks every repository route as a user without access |
| Nothing unscanned reaches a model | `gateway`: an excerpt with no scan result is refused, and the count of `model_call` rows with no scan decision is zero |
| Only the configured provider | `gateway`: a request for a repository reaches only that repository's adapter |
| Works without a model | Journey 6 and an `api` test with no provider reachable: diff, navigation and search still answer |
| Quota is reported as quota | `gateway` fixtures for both providers, and journey 6 |
| Pull request code is never run | A test that fails if any package under `internal/workspace` imports `os/exec`, other than `scan` |
| Existing installs keep working | Installer golden files with no workspace address are unchanged from today |
| Hotkeys | `keymap.test.ts`: no browser-reserved key, and no single key fires in a text field |
| Phone | Journey 10 at a phone screen size |

Checks to run by hand before the first repository is connected:

1.  Restore the database from a backup into an empty instance and open a pull request view.
2.  Run the index step on one repository managed with `bun`, and record build time and file sizes.
3.  Confirm an organisation owner has approved either a fine-grained token or the App.

--------------------------------------------------------------------------------

<!-- Provenance: Indicates initial structural drafting via EGM. -->

*Drafted with EGM*
