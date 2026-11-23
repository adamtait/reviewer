# Review workspace: code navigation and AI help beside GitHub pull requests

*   **Author:** adamtait
*   **Status:** Proposed (revised after review)
*   **Date:** 2026-11-23

--------------------------------------------------------------------------------

## Overview

The review workspace is a web application used beside GitHub's pull request page. It adds two things the team reports missing there: code navigation inside a pull request (go-to-definition, find-references, search), and an AI assistant that can answer questions about the change and the code around it. Reviewers keep using GitHub to comment, approve and merge.

It serves a team of 10 engineers across 4 or more repositories. The team opens dozens of pull requests a week, and coding agents write more than 90% of them. The code is mostly TypeScript.

The system has two parts:

*   **One CI job per repository.** It builds a navigation index for each pull request commit and saves it as a workflow artifact. It needs no secret and no new permission.
*   **The workspace service.** When a reviewer opens a pull request, the service fetches the source from GitHub with that reviewer's own credential, loads the index, and serves the web application. It never runs code from a pull request.

This document designs the first version only. The later stages are listed once, under Non-Goals.

**Record of decisions.** The concept and its first review are in [`concepts_brief.md`](../../concepts_brief.md). This revision follows a five-stage review, summarised in [`consolidated_review_report.md`](reviews/round-1/consolidated_review_report.md). What was changed in response, and which points still need the author, is in [`goldfish_review_report.md`](reviews/round-1/goldfish_review_report.md).

## The Problem

The requester reviews each pull request line by line on GitHub's pull request page, and reports three problems. These are the requester's account of daily use. Whether GitHub's current features already cover part of them is entry condition E4 below.

**It is hard to build a mental model of a change.** The reviewer needs to know why the change was made, how it works, what it does to the rest of the system, and which data models and workflows it touches. One study of code review at Microsoft found that understanding the change is the main difficulty reviewers report (Bacchelli & Bird, 2013).

**The diff is plain text.** The requester finds no go-to-definition, no find-references and no view of how changed files relate. To follow a call, the reviewer leaves for an IDE, fetches and switches branch, and often loses the diff.

**There is no way to ask.** The requester cannot select a block of code and ask what it does, ask how an existing subsystem works, or ask for a diagram of a changed flow.

Agent-written code adds to the first problem. The person who opened the pull request did not type the code, so the reasoning lives in an agent session nobody else can see.

**The proposed solution.** Give the reviewer a view of each pull request in which symbols can be followed, the reason for the change is on screen before any line is read, and an AI assistant can be asked about anything in the repository at that commit. Build it beside GitHub, so the team can try the new features without giving up anything GitHub already does.

The first version is also a test. The implementation plan names three points at which the team tries a feature, and what result would stop or reshape the rest.

## Background: the existing CLI

This repository holds `reviewer`, a command-line tool that reviews a pull request and posts comments. The design reuses parts of it, so a reader needs these facts:

*   **How it runs.** It is a stateless binary. `reviewer init` writes a GitHub Actions workflow, a config file (`.review/config.yaml`) and an agent skill file into a repository. The workflow runs the tool on every pull request. GitHub comments are its only store.
*   **Two lanes.** The deterministic lane runs analyzers: a secrets scanner (`gitleaks`), type checks, lint and convention rules. The model lane sends the diff to a language model for review. It is off by default, reads the diff and the guidance documents named in config, and reports only on changed lines.
*   **The secrets gate.** If the scanner finds a credential in the diff, or cannot run, the model lane does not run. ADR-0012 records this rule.
*   **Checks on model findings.** Each model finding is put back to the model to be disproved before it is shown (ADR-0023), and some categories of finding are capped at medium confidence in code (ADR-0022).
*   **The TypeScript plugin.** A separate Node process that runs the TypeScript analyzers. It builds a compiler program on each run and answers no navigation queries.
*   **Releases.** One tag publishes two artifacts: the binary and the plugin's npm package (ADR-0026).
*   **Rules this repository enforces.** No address, organisation or model name appears in Go source (ADR-0004). Nothing internal enters this repository's history, because it is meant to be public (ADR-0010). The tool exits 0 whatever it finds (ADR-0009).

## Goals

Each goal names how it is checked. The Verification Plan maps each one to a test.

1.  **Open any pull request in the workspace.** A user opens a pull request from a list of open pull requests, or at an address that mirrors GitHub's own path, and sees the diff by default or only the new code. Draft pull requests count. *Checked by:* every open pull request GitHub lists for a connected repository appears in the workspace list.
2.  **Label every navigation result with how it was obtained.** Go-to-definition and find-references work on changed and unchanged TypeScript code at the commit on screen. A result is shown as a definition only when the index built in CI resolved it and the name at the target matches. Anything else is labelled as text search, as defined outside the repository, or as not found. A compiler-labelled result is as correct as the index CI built. *Checked by:* a "this was wrong" report on every jump, counted per repository; and the share of lookups in each class.
3.  **Search the repository at the pull request's commit.** *Checked by:* text search returns matches from unchanged files, and works when no index is available.
4.  **Ask the AI about code.** A user selects code and asks about it, or asks about any code in the repository at that commit, and can ask for a diagram or for alternatives. *Checked by:* every answer links to each excerpt the model was given.
5.  **See the reason for a change before reading it.** Each pull request has a change brief: why the change was made, how, which data models and workflows it touches, and which unchanged code depends on what changed. *Checked by:* every pull request view shows a brief, or one named state in its place: being prepared, not sent because of a secret, waiting on quota, or failed with a reason.
6.  **See an AI review.** The review has two parts: notes on the plan, and findings on changed lines. *Checked by:* each part is shown with the commit and model that produced it.
7.  **Mirror GitHub access.** A user sees a repository's code, briefs, AI answers and telemetry only if GitHub says they can read that repository. The answer is cached for at most the configured period, five minutes in the example configuration. *Checked by:* one test that walks every route carrying repository data as a user without access.
8.  **Never execute pull request code on the service.** The service runs two programs, `git` and the secrets scanner, over file contents. It never checks out a working tree, installs dependencies, or runs a repository's tools or tests. *Checked by:* a test that the only programs the service starts are those two, each with an empty environment.
9.  **Scan everything the service sends to a model.** Every model call the service makes passes one gateway, which scans the outgoing content at the moment of sending and does not send what it cannot scan. Only the provider configured for a repository receives its content. *Checked by:* every model call record carries a scan result; a test sends each kind of content with a planted credential.
10. **Work without a model.** Diff, navigation and search need no model. When the provider's quota is exhausted, AI features say that quota is the cause. *Checked by:* a test that runs the workspace with no provider reachable.
11. **Work on a phone for reading and asking.** A user can read the change brief and the diff and ask questions on a phone. *Checked by:* CUJ 9 at a phone screen size. Touch navigation is an open question below.
12. **Support desktop shortcuts.** Every core action has a keyboard shortcut. *Checked by:* a test that no shortcut uses a key on the reserved list in the implementation plan, and that single-key shortcuts do not fire in a text field.
13. **Show telemetry to every user.** Any user can see and export usage measures for the repositories they can read. *Checked by:* the export matches a fixed set of recorded events.
14. **Leave existing installs working.** A repository that already runs the CLI keeps its checks and comments. *Checked by:* this design changes neither the review workflow template, the config template nor the published Action; a test confirms a default `reviewer init` writes those files as before.

## Non-Goals

**Left to GitHub in the first version**

*   Writing comments and suggestions, approving, requesting changes, resolving threads, showing CI status, and merging. Each view links to the matching place on GitHub.

**Later stages, each with its own design document**

| Stage | Contents |
| --- | --- |
| 2 | Learnings: candidates drawn from human reviews, and accepted learnings as files in the repository. A written list per repository of changes that always need a human, with labels shown and nothing skipped. |
| 3 | Comments written in the workspace and posted to GitHub. A "send to agent" action on a batch of comments. |
| 4 | Pull requests that merge with no human having read them, per repository. Planning, deployment and what happened after a change landed. |
| Unscheduled | A view for a branch that has no pull request. Navigation for Python, bash and Rust. A second sign-in route through a GitHub App. |

**Not planned**

*   Installing or executing pull request code on the service.
*   Storing or sharing whole agent transcripts.
*   The service running coding agents or pushing commits.
*   Switching to a second model provider when quota runs out.
*   OpenAI models, and intent records from agents other than the two the team uses (Antigravity and Claude Code).
*   A shared service for other teams.
*   Storage on the user's device.

## Open Questions

### Entry conditions

These four are answered before the first pull request (PR-47) is started. Each can remove a pillar of the design, and each is cheap to answer.

*   **E1. Credential approval.** Will an organisation owner approve fine-grained personal access tokens with read access to contents, pull requests, metadata and Actions? If not, the App route must be built first, and it needs its own approval.
*   **E2. The indexer.** Run `scip-typescript` in CI on one repository, including one managed with `bun`. Record whether it works, the build time, and the index size. The latest release found is 0.4.0, from October 2025; confirm it supports the TypeScript versions in use.
*   **E3. Fetching as the viewer.** With a fine-grained token, confirm three things: `git` can fetch a single commit by its hash; the Actions API lists an artifact by name with the commit its run was for; and the artifact can be downloaded. Record the time to fetch and import one repository.
*   **E4. Existing tools.** Spend a fixed, short time checking whether GitHub's current features, or a browser-hosted VS Code with GitHub's pull request extension, already give navigation and a way to ask. If they do, stop and reduce this design to the change brief.

### Other open questions

*   **Thresholds for the three feedback points.** The implementation plan says what is measured. The author sets the numbers.
*   **Touch navigation.** The first version includes tapping a symbol to navigate. The test is CUJ 2 at a phone screen size during the PR-51 feedback week. If it fails there, the phone keeps reading and asking only.
*   **Retention.** How long are stored commits, conversations and usage events kept? The service will not start without these values.
*   **Existing installs.** Which repositories have the CLI installed, and is the model lane on in any of them?
*   **Refreshing the agent skill.** `reviewer init` does not overwrite an existing skill file without `--force`. Is a narrower refresh needed?

## Critical User Journeys (CUJs)

### CUJ 1: Understand a change before reading it

*   **Given**: A pull request nobody has opened in the workspace yet.
*   **When**: A reviewer opens it.
*   **Then**: The service fetches the source and shows the diff. The change brief is written while the reviewer watches and is stored for later readers. It separates what the author says from what the code shows, and each statement links to its source.

### CUJ 2: Follow a symbol into unchanged code and return

*   **Given**: A pull request view whose index is loaded.
*   **When**: The reviewer triggers go-to-definition on a symbol in an added line.
*   **Then**: The view jumps to the definition, which may be in a file the pull request does not touch, and marks that file as outside the change. One action returns the reviewer to the same place in the diff.

### CUJ 3: A lookup the index could not resolve

*   **Given**: A symbol with no resolved definition in the index, such as one in a Python file, or one defined in an installed dependency.
*   **When**: The reviewer triggers go-to-definition on it.
*   **Then**: The reviewer sees text search results labelled as such, or a note that the symbol is defined in a named dependency outside the repository, or a message that nothing was found. Nothing is presented as a definition.

### CUJ 4: Open a pull request just after a push

*   **Given**: A new commit was pushed a moment ago, and its index job is still running.
*   **When**: The reviewer opens the pull request.
*   **Then**: The diff for the new commit is shown and text search works, because source comes from GitHub and not from CI. The view says the index for this commit has not arrived. Nothing is answered from the previous commit's index.

### CUJ 5: Ask about selected code

*   **Given**: A pull request view, and a provider configured for the repository.
*   **When**: The reviewer selects a block of code and asks what it does and what else calls it.
*   **Then**: The answer streams in, links to each excerpt the model was given, and names the model that produced it.

### CUJ 6: Quota is exhausted

*   **Given**: The configured provider is rejecting requests because quota has run out.
*   **When**: The reviewer asks the AI a question.
*   **Then**: The reviewer sees a message that quota is exhausted, with the reset time when the provider gives one. A stored brief and review are still shown. Diff, navigation and search are unaffected.

### CUJ 7: A secret is found

*   **Given**: A file in the repository at this commit contains a credential.
*   **When**: The reviewer asks a question, and the model asks to read that file.
*   **Then**: That file is not sent. The model is told it was withheld, the answer continues without it, and the reviewer is told which file was withheld and why. If the credential is in the diff itself, the brief and the review are not generated, and the view says so.

### CUJ 8: A user without access

*   **Given**: A user who cannot read a repository on GitHub.
*   **When**: That user follows a link to one of its pull requests in the workspace.
*   **Then**: The workspace shows nothing from that repository, and the response is the same whether or not the pull request exists.

### CUJ 9: Read and ask on a phone

*   **Given**: A reviewer on a phone.
*   **When**: The reviewer opens a pull request.
*   **Then**: The change brief and a single-column diff are readable. The reviewer can tap a line and ask about it, and the code in question stays visible while the keyboard is open.

### CUJ 10: Leave a comment

*   **Given**: A reviewer reading a changed line in the workspace.
*   **When**: The reviewer chooses to comment.
*   **Then**: GitHub opens at that file and line of the pull request, and the reviewer writes the comment there.

### CUJ 11: Ask for a fresh review

*   **Given**: A stored brief and review, and a pull request description the author has since edited.
*   **When**: The reviewer opens the pull request.
*   **Then**: The view says the description changed after the brief was written. The reviewer can ask for the brief and review to be generated again.

## Technical Plan (Architecture)

### Decisions this plan rests on

| # | Question | Choice |
| --- | --- | --- |
| 1 | How the service knows who a user is and what they can read | One interface with one route built: a fine-grained personal access token per user. A GitHub App route can be added behind the same interface. |
| 2 | How source reaches the service | The service fetches it from GitHub with `git`, using the viewer's credential, the first time someone opens a commit. |
| 3 | How the navigation index is built and delivered | `scip-typescript` in a CI job, saved as a workflow artifact, downloaded by the service with the viewer's credential. |
| 4 | Where the service runs and keeps data | A stateless container on Cloud Run, and managed Postgres. |
| 5 | How the AI assistant gets code | The model calls read-only tools that the service runs. |
| 6 | Where secrets are scanned | In the model gateway, at the moment of sending. |
| 7 | When the brief and review are generated | When a reviewer first opens a commit, and again on request. |
| 8 | Language and shape of the service | Go, as a second binary in this repository. |
| 9 | Web application | TypeScript, React and CodeMirror 6, built into static files and embedded in the service binary. |

Alternatives for each row are under Alternatives Considered, grouped by these numbers.

### The trust rule

The service relies on three sources, each to a stated extent.

| Data | Source | How far it is relied on |
| --- | --- | --- |
| A pull request's head commit, base, title, description and state | GitHub's API, read with the viewer's credential on every open | Trusted. |
| The merge base a diff is measured from | GitHub's compare call, read the same way | Trusted. |
| Source code at a commit | `git fetch` from GitHub with the viewer's credential | Trusted. Git names every object by a hash of its content and checks it on receipt. |
| The navigation index | A workflow artifact from the repository's CI | Not verifiable. A pull request can change its own workflow, so the index is treated as written by the pull request's author, and bounded as described under Navigation. |

Nothing else arrives from CI. The service has no upload address.

### Components

```text
  Repository on GitHub                               Google Cloud project
 +---------------------------------+               +---------------------------------------------+
 | index workflow                  |               | workspace service (Cloud Run)               |
 |   check out the head commit     |               |                                             |
 |   install, run scip-typescript  |               |   identity -----> sign-in, user tokens      |
 |   save the index as an artifact |               |   access -------> can this user read this   |
 +---------------------------------+               |   catalog ------> pull request list, view   |
                                                   |   source -------> fetch, files, diff, search|
  GitHub API and git over HTTPS  <---------------- |   navigation ---> index load, lookups       |
    pull request, merge base,      as the viewer   |   scan ---------> secrets scanner           |
    source, artifact                               |   gateway ------> the one door to a model   | ---> configured
                                                   |   assistant ----> tool loop                 |      provider
                                                   |   brief --------> change brief, AI review   |
  Browser, desktop or phone  <------ HTTPS ------> |   telemetry, status                         |
                                                   |   web ----------> static files, JSON API    |
                                                   +----------------------+----------------------+
                                                                          |
                                                                  Postgres (all records)
```

### The CI side

`reviewer init` writes one new workflow file into a repository where it detects TypeScript. The existing review workflow, the config file and the published Action are not changed.

The index workflow runs on pull requests and on pushes to the default branch. Its one job:

1.  Checks out the pull request's head commit. The default checkout on a pull request is a temporary merge of the pull request into its base, so the job names the head commit explicitly. The index then describes the same files the service shows.
2.  Installs dependencies with the repository's usual command.
3.  Runs `scip-typescript` at a pinned version.
4.  Saves the index file as a workflow artifact with a fixed name, beside a small file naming the commit and the tool version.

The job holds read access to contents and nothing else. If any step fails, the workspace shows that navigation is unavailable for that commit.

The agent skill that `reviewer init` writes gains two instructions:

*   When opening a pull request, write an intent record into its description: the goal, the plan, the alternatives that were rejected, and a session identifier the author can use to find the session again.
*   When the author asks, read the open review threads from GitHub, make the changes, push, and reply on each thread.

### The workspace service

One Go binary, `cmd/workspace`, serves the API and the web application. It keeps no durable state in the process. The table lists its parts and what each reuses from the CLI.

| Part | What it does | Reuses |
| --- | --- | --- |
| `identity` | Signs users in and holds their GitHub token, encrypted. | `internal/ghclient` |
| `access` | Answers "can this user read this repository", by asking GitHub and caching the answer in memory for the configured period. | `internal/ghclient` |
| `catalog` | Lists and views pull requests from GitHub's live data. | `internal/ghclient` |
| `source` | Fetches a commit with `git`, stores its files, computes the diff, serves files and text search. | `internal/diff` for the diff |
| `navigation` | Downloads and loads the index, answers definition and reference lookups. | Nothing |
| `scan` | Runs the secrets scanner over content about to be sent. | `internal/analyzers/gitleaks` |
| `gateway` | The only code that calls a model provider. Scans, enforces budgets, records usage. | Credential patterns from `internal/model` |
| `assistant` | Runs a conversation: sends the question, runs the tools the model asks for, streams the answer. | Nothing |
| `brief` | Generates the change brief and the AI review. | `internal/laneb` for findings on changed lines |
| `telemetry` | Records usage events and serves the measures. | Nothing |
| `web` | Serves the built web application and the JSON API. | Nothing |

### Opening a pull request

1.  The reviewer opens a pull request. The service asks GitHub, with the reviewer's credential, whether they can read the repository, and for the pull request's head commit, title, description and state.
2.  The service asks GitHub for the merge base of the head commit and the base branch.
3.  For the head commit and the merge base, if not already stored, the service fetches that one commit with `git` into scratch space, stores each new file, and records which path holds which file. It computes the diff between the two with `git diff` and stores it. The scratch copy is then deleted.
4.  The service looks for an index artifact from a workflow run for the head commit. If one exists and is not yet loaded, it downloads and loads it.
5.  The diff is shown. Search works from this point, and navigation works if the index loaded.
6.  If no brief is stored for this commit, the service generates the brief and then the review, streaming the brief to the reviewer as it is written.

Each of the four stages has a visible state per commit:

| Stage | States |
| --- | --- |
| Source | fetching, ready, failed |
| Index | waiting for CI, loading, ready, not produced, failed |
| Brief | none, generating, ready, not sent (secret), waiting on quota, failed |
| Review | the same as the brief |

A second reviewer who opens the same commit while a stage is running sees its state and does not start it again.

Nothing is prepared before someone opens a commit. The first reviewer waits for the fetch and for the brief. The fetch time is measured in entry condition E3.

### Storing source and index

*   **Files.** A file's text is stored once per repository, keyed by its git hash. A commit is a list of paths, each pointing at a file hash. Opening a new commit stores only the files that changed.
*   **Text search.** File text is indexed by trigram in Postgres. A search at a commit joins that commit's path list to the stored text.
*   **Diff.** The diff between a merge base and a head is computed once by `git` and stored as changed files, changed line ranges and patch text.
*   **Index.** A SCIP file holds one document per source file, listing each symbol occurrence with its position and whether it is a definition or a reference. Each document is stored once, keyed by a hash of the document, and a commit's index is a list of paths pointing at document hashes.

Every record that belongs to a repository carries the organisation and repository identifiers, and is read only through the access check.

`git` runs with an empty environment, with hooks disabled, against a bare scratch directory. It never checks out a working tree. The viewer's token is passed to it through the environment for that one command and is not written to disk.

### Navigation

The index cannot be verified, so it is bounded:

*   **Binding.** An index is accepted only from a workflow run that GitHub reports as being for the commit on screen: a pull request run for a head commit, or a default-branch push for a base commit. One pull request cannot supply another's index.
*   **Size and shape.** The artifact and each document are size-limited. A path that is not in the stored commit is dropped, and a position outside its file is rejected.
*   **Name check.** A definition is returned as resolved only if the name at the target matches the symbol. This catches a target that points at unrelated text. It does not catch a target that points at a different symbol with the same name.
*   **Outside the repository.** A definition in an installed dependency is reported as "defined in" that package. The file is not in the repository, so there is nothing to jump to.
*   **Fallback.** Any other case returns text search results, labelled as such.

A lookup from a removed line uses the merge base, and any other lookup uses the head commit. A merge base often has no index, because CI indexes default-branch pushes and a merge base may be an older commit. Lookups from removed lines then fall back to text search.

A false index can mislead readers of its own pull request. It cannot change the diff, the source shown or search results.

### Identity and access

The identity interface has three operations: sign a user in, say whether that user can read a repository, and return a GitHub client that acts as that user.

**The token route.** On first visit the user pastes a fine-grained personal access token with read access to contents, pull requests, metadata and Actions. The service calls GitHub to learn whose token it is, encrypts it with a key supplied through the environment, stores it, and issues a session cookie. The service makes no write call to GitHub, whatever the token allows. When the token expires or is revoked, the user is asked for a new one.

**The access check.** Before any read of a repository's data, the service asks GitHub whether the signed-in user can read that repository, using the user's own token. The answer is cached in the process's memory for the configured period. Access removed on GitHub therefore takes effect within that period.

### The model gateway

The gateway is the only package that holds provider keys or opens a connection to a provider. Each repository names one provider and model, and its content is sent nowhere else.

**At the moment of sending**, the gateway takes everything in the request that has not already been cleared in this conversation, and does the following:

1.  It collects the text to scan: the pull request description, the user's typed text, tool results, and for every code excerpt the full file the excerpt comes from.
2.  It runs the secrets scanner over that text, with the service's own rule file and an empty environment. Results are remembered by content hash, scanner version and rule file, so the same content is not scanned twice.
3.  If the scanner cannot run, nothing is sent.
4.  If a secret is found in a file or a tool result, that item is withheld. The model receives a note in its place, and the user is told what was withheld.
5.  If a secret is found in content the request cannot do without, nothing is sent. That is the diff for the brief and review, and the user's own typed text for a question.
6.  It checks the repository's budget, calls the provider, and records the call: feature, user, repository, commit, provider, model, what was sent, the scan result, tokens used and the outcome.

There is one rule for every file, changed or not. A secret in the diff stops the brief and review for that commit, which is what ADR-0012 requires of a review.

The gateway has one adapter per provider, for the Anthropic and Google APIs. Each supports conversation turns, tool calls, streamed output and token counts. The brief and review use the same adapters with no tools. `internal/model`, the CLI's provider package, is not changed apart from its credential patterns.

A provider response that reports exhausted quota becomes a distinct outcome carrying the reset time when the provider gives one. A request that would pass the repository's budget is refused with a message naming the budget.

The CLI's own model lane runs in CI, outside this service, and is off unless a repository turns it on. This design does not change it. Goal 9 covers the calls the service makes.

### The assistant

A question starts a conversation tied to a repository, a commit and a user. The gateway sends the question with a description of the tools. The model may answer or ask for a tool. The service runs the tool, passes the result through the gateway, and returns it to the model. This repeats until the model answers, or a limit on rounds or tokens is reached.

The six tools are read-only and act on the commit the user is viewing: search text, read a range of a file, find a definition, find references, list changed files, and read the brief and description.

Text inside code, descriptions and tool results can contain instructions aimed at the model. The tools cannot write, post or reach the network. What such text can still do is listed under Risks and Limits.

### The change brief and the AI review

Both are generated when a reviewer first opens a commit, and stored with the commit, the model, the inputs used and the time. Any reader can ask for them to be generated again. The view offers this when the description has changed since they were written, when the index arrived after they were written, or when the last attempt hit quota.

**Inputs.** The diff; the pull request title and description, including the intent record; files under the repository's documentation and ADR folders that the pull request adds or changes; the guidance documents named in the repository's `.review/config.yaml`, read from the merge base and not from the pull request; and, when an index is loaded, the unchanged code that refers to symbols defined on changed lines.

**The brief** has two halves. "The author says" restates the description, intent record and plan. "The code shows" states what the diff and index show: how the change works, which data models and workflows it touches, and what unchanged code depends on it. Each statement links to its source. A missing input is named.

**The review** has two parts:

*   **Notes on the plan.** Gaps, risks, and places where the code and the stated plan differ. These are not tied to a line. They are shown as the model's judgement, with no second pass.
*   **Findings on changed lines.** Produced by the CLI's review code: its prompt, its second pass that tries to disprove each finding, and its confidence caps. That code keeps only findings on changed lines, which is what this part is for.

Neither part is posted to GitHub in the first version. An empty review means the model raised nothing, and the view says so in those words.

### The web application

One page application with three main views: the list of open pull requests, the pull request view (brief, diff, review, assistant panel), and the code view for files outside the diff.

*   **Code and diff** are drawn with CodeMirror 6, which supports touch input. The default is the diff, and a toggle shows only the new version of each changed file.
*   **Navigation.** Back and forward follow browser history, and every location has its own address.
*   **Narrow screens** get a single-column diff. Tapping a symbol or a line number opens one sheet listing every action available there, so comment, navigate and ask do not compete for one gesture.
*   **Shortcuts** follow the `j`/`k` and `g`-prefix conventions that GitHub and Gerrit use. The table of keys is in the implementation plan.
*   **Rendered model output.** Briefs and answers are drawn as Markdown with raw HTML disabled, images disabled, and links shown with their full address. Diagrams are drawn from Mermaid text with script execution disabled. A content security policy stops the page loading anything from another host.

### Retention

A command, `workspace sweep`, removes stored commits that no open pull request refers to and that nobody has viewed within the configured period. It also removes conversations and usage events past their own periods. It is run on a schedule by the platform, as a job using the same container image. The service has no background work of its own.

A pull request is seen to be closed when someone next opens the list. One that is closed and never looked at again is removed by the age rule.

### Telemetry and notices

The web application reports usage events to the service: pull request opened, file viewed and for how long, each navigation lookup with its class, each search, each AI question, each "this was wrong" report, and each jump to GitHub. Any user can view these measures and export them, limited to repositories they can read.

A user can start a one-off import of a repository's pull request history from GitHub, using their own credential. It records time from opening to first review and to merge, and comments per pull request, as the figures to compare against.

A banner and a status page show the health of GitHub, the provider and the database. Outage email is sent by Google Cloud's monitoring to a list of addresses, and is set up in the deployment, outside this repository.

### Logs and health

Every log line is structured and carries a request identifier, user, repository, pull request and commit. Logs never contain source code, descriptions or conversation text. Each stage records its duration and outcome on the commit. A health address reports the state of the database.

### What the later stages require of this version

*   **Commenting (stage 3).** GitHub stays the record for comments and approvals. This version stores no copy of them. The identity interface already returns a client that acts as the user.
*   **Learnings (stage 2).** The brief and review read guidance from the merge base. Accepted learnings would arrive by the same route.
*   **Risk labels (stage 2).** The per-commit stage list can take one more stage, and "could not run" is already distinct from "found nothing".

These are the constraints known today. The later stages are not designed.

### Changes to recorded decisions

ADRs in this repository are append-only. The checking tool allows only whole-record supersession, so a superseding ADR restates the part of the old decision that still holds. "Builds on" below means the new ADR cites the old one and leaves it in force.

| New ADR | Decision | Relation to existing ADRs |
| --- | --- | --- |
| 0029 | Add a hosted workspace service beside the CLI | Supersedes ADR-0008, and restates that the CLI stays a stateless binary |
| 0030 | The service keeps its own store; GitHub stays the record for comments and approvals | Supersedes ADR-0016, and restates that GitHub comments stay the CLI's store |
| 0031 | Fetch source from GitHub as the viewer; treat the CI-built index as untrusted | Builds on ADR-0018 |
| 0032 | One gateway for the service's model calls, scanning at the moment of sending | Builds on ADR-0012 |
| 0033 | Identity behind one interface, with the token route built first | New |
| 0034 | A conversation interface for providers, inside the gateway | Builds on ADR-0021 |
| 0035 | SCIP as the navigation index format, built in CI and delivered as a workflow artifact | New |
| 0036 | Release a container image as a third artifact | Supersedes ADR-0026, and restates its two artifacts |

ADR-0018 fixes the review workflow's trigger and permissions. This design leaves that workflow alone, and the new index workflow holds only read access to contents.

## Alternatives Considered

Each entry gives the trade-off that decided it. Who decided what, and when, is in the files named under "Record of decisions" in the Overview.

### Scope and product shape

| Alternative | For | Against, and why it was not chosen |
| --- | --- | --- |
| Replicate GitHub's pull request page first | One place for all review work | All-or-nothing: reviewers keep opening GitHub until commenting, approval, CI status and merge all exist, and those would be built before navigation or AI help. |
| Extend the CLI only, posting a change brief as a comment | No service to host | A comment cannot offer navigation or a conversation. |
| Storage on the reviewer's own machine | Nothing to host | A phone cannot hold a repository, and nothing is shared between reviewers. |
| A new repository for the service | No existing decisions to supersede | The service reuses the CLI's packages, and two repositories would need releases kept in step. |
| A trained risk classifier that lets pull requests merge unread | The largest possible saving in review time | With zero observed misses, showing a miss rate under 5% needs about 60 known high-risk pull requests per repository. No labelled history exists, and a miss is never seen because nobody reads it. Deferred to stage 4. |
| Export whole agent transcripts as the source of intent | The complete record of how a change was made | A transcript holds whatever the agent saw, including other repositories' content and pasted credentials. An intent record in the pull request inherits GitHub's access rules. |
| The service resumes the original agent session to make fixes | The agent that wrote the code makes the fix | The session lives on the author's machine, which a hosted service cannot reach. |
| Try the CLI's model lane with wider inputs before building | Tests at low cost whether more context improves AI findings | Not run. It delays the build by the length of the trial. The accepted risk is that this premise is first tested when the brief ships in PR-54. |
| GitHub's own features, or a browser-hosted VS Code with GitHub's extension | Possibly no viewer to build | Not yet evaluated. Entry condition E4 evaluates it before any work starts. |

### Decision 1: identity

| Alternative | For | Against, and why it was not chosen |
| --- | --- | --- |
| OAuth App sign-in | A familiar sign-in button; users handle no token | An OAuth App cannot ask for narrow permissions. The only scope that reads private repositories also grants write access to every repository the user can reach, and approval covers the whole organisation. |
| Google sign-in with one read-only service token | Users handle no GitHub credential | That one token reads every connected repository, and a hand-kept table maps accounts to GitHub logins. A wrong row grants the wrong access. |
| Build the GitHub App route now, beside the token route | Sign-in without pasting a token | It needs an organisation owner to install an App, which may not be allowed. One route is enough to start, and the interface leaves room for the second. |

### Decision 2: how source reaches the service

| Alternative | For | Against, and why it was not chosen |
| --- | --- | --- |
| The CI job pushes a git pack to the service | Source is ready before anyone opens the pull request | The pull request controls its own CI job, so the service needs a pack verifier, an exchange to find missing objects, a public upload address and a new workflow permission. Nothing arrives until the job ends, and the diff's base comes from the upload. This was the first design, and review found each of these problems. |
| Keep a bare git repository on disk and read everything from it | Git already stores, diffs and searches; least code | It needs a persistent disk, which means one machine in place of a stateless container. Removing old commits means repacking with a single writer. |
| Read through GitHub's REST API only, with no `git` | No program to run | Reading a whole tree takes one call per file, and GitHub's code search does not cover a pull request's commit, so search would be lost. |
| Sandboxed workers on the service install and index pull request code | No dependence on GitHub Actions | The service must operate a sandbox, and installs must run with scripts disabled, which leaves generated types missing. |
| Poll GitHub on a timer with a designated token | Changes are seen without a viewer | A standing credential that reads every connected repository. |
| A second workflow that reports pull request events | The service learns of a close or an edit at once | A second workflow file per repository, and a lost event goes unnoticed. The live read on every open already gives the current state. |

### Decision 3: the navigation index

| Alternative | For | Against, and why it was not chosen |
| --- | --- | --- |
| The CLI's TypeScript plugin exports an index in its own format | No new tool in CI; one compiler build | The most code to write and own, a format nothing else reads, and a separate exporter for each later language. |
| The plugin exports SCIP | No new tool in CI; a published format | The exporter is still this project's to own, and must reproduce SCIP's symbol naming. It is the fallback if entry condition E2 fails. |
| The LSIF format | An older published format | The documentation for its TypeScript indexer recommends moving to `scip-typescript`. |
| The CI job uploads the index to the service | The index is loaded before anyone opens the pull request | An upload address, a signed-token check, and a workflow permission granted to a job that runs pull request code. |
| Build the index inside the existing review job | No second dependency install | That job checks out a temporary merge of the pull request, so the index would describe different files from the ones shown. |

### Decision 4: hosting and storage

| Alternative | For | Against, and why it was not chosen |
| --- | --- | --- |
| One virtual machine with SQLite and a disk | Fewest parts; a disk for a git repository | One machine to patch, a restart on every upgrade, and data lost if the disk fails between snapshots. |
| Cloud Run with a document database | No always-on database charge | Telemetry needs aggregate queries that a document database handles poorly. |
| A storage bucket for raw index files | The stored form could be rebuilt without CI | Source can be fetched again and an index rebuilt by re-running CI, so the bucket would hold nothing that cannot be recovered. |
| A cloud key service for stored tokens | The key never enters the process | A client library and its dependencies, for about ten tokens. The key is supplied through the environment from the platform's secret store. The cost is that the key is in the process's memory. |
| A second database engine for a developer's machine | No container needed locally | Text search uses Postgres features, and a second engine would need its own version. |

### Decision 5: how the assistant gets code

| Alternative | For | Against, and why it was not chosen |
| --- | --- | --- |
| Embedding retrieval | One model call per question | An embedding call for every changed piece of code, a vector store, and more content sent to the provider. It misses questions such as "what calls this", which the index answers. |
| Context assembled by fixed rules | One model call, and an exact record of what was sent | It cannot follow a chain beyond what the rules fetch. |
| A second provider when quota runs out | AI features keep working through an outage | A repository's code would go to a second vendor. |

### Decision 6: where secrets are scanned

| Alternative | For | Against, and why it was not chosen |
| --- | --- | --- |
| Scan each file when it is stored, and look results up at send time | Questions never wait for a scan | It creates states that need repair: no result, a failure recorded for good, a result from an older scanner. It also does not cover text that is not a file, such as a description. |
| Use the repository's own scanner allowlist | Fixture credentials do not block AI features | A pull request could add an entry that clears its own secret. An allowlist read from the base branch is listed under Future Improvements. |

### Decision 7: when the brief and review are generated

| Alternative | For | Against, and why it was not chosen |
| --- | --- | --- |
| Generate on every push, from a job queue | The brief is ready when the reviewer arrives | Most intermediate commits are never opened, the work competes with live questions for the same quota, and it needs a queue, a scheduler and priority rules. |
| Generate in CI with the CLI | The CLI already has a model lane | A pull request can change its own workflow and so its own brief, and a model key would live in every repository. |

### Decisions 8 and 9: the service and the web application

| Alternative | For | Against, and why it was not chosen |
| --- | --- | --- |
| Write the service in TypeScript | One language for service and web application | It could not reuse the CLI's Go packages for diffs, GitHub calls, the scanner and review. |
| The Monaco editor for code and diff | The editor VS Code uses | Its documentation says it does not support mobile browsers. |
| Pages rendered on the server with no client application | Less code in the browser | Navigation history, the assistant panel and streamed answers need state in the browser. |

## Risks and Limits

Figures marked as estimates rest on the stated assumptions and are replaced by measurements as they arrive.

| Risk or limit | What is known | Mitigation |
| --- | --- | --- |
| Model spend | Estimate: about 40 million input tokens a week. Assumes 80 commits opened a week at 120,000 tokens each for brief and review, and 500 questions a week at 60,000 tokens each. | Nothing is generated for a commit nobody opens. A cap per question and a monthly budget per repository. The usage ledger gives the real figure from the first week. |
| The first reviewer waits | The first open of a commit fetches source and writes the brief. The time is unmeasured. | Entry condition E3 measures it. Each stage shows its state. Later commits store only changed files. |
| Memory for a fetch | A fetched commit is held in the container's memory until it is stored. | A size limit in configuration; a fetch over the limit fails with that reason. One fetch at a time per container. |
| The index cannot be verified | A pull request can build a false index for itself. | Binding to the workflow run for that commit, size limits and the name check. Residual risk: a target with the same name as the real one. The diff, source and search do not depend on the index. |
| Instructions hidden in code or descriptions | Such text can produce a wrong answer, steer the brief and review that every reader sees, and use up a question's tool rounds. | The tools are read-only. The brief separates what the author says from what the code shows. Rendered output cannot load anything from another host. |
| Stored user tokens | The service cannot check which permissions a pasted token carries. A breach of the service exposes whatever those tokens can read. | The sign-in page asks for read-only permissions. Tokens are encrypted at rest. `git` and the scanner start with an empty environment. The service makes no write call to GitHub. |
| Secrets false positives | A fixture credential in a changed file stops the brief and review for that commit. How often this happens is unknown. | An allowlist in the service's configuration, kept by the operator. A base-branch allowlist is deferred until a false positive is seen. |
| Database growth | Estimate: one path row per file per stored commit, about 500,000 rows a month for a 5,000-file repository at 100 commits a month. | The retention sweep removes commits nobody has viewed within the configured period. |
| Search speed | The target is under one second. It is unmeasured. | Measured in PR-49 against the largest connected repository, before the web application is built on it. |
| `scip-typescript` | Its upkeep and its support for `bun` are unconfirmed. | Entry condition E2. The fallback is the plugin exporting SCIP, which leaves the service's reader unchanged. |
| One operator | The service is run by one person for ten users. | It writes nothing to GitHub, so review continues on GitHub when it is down. |

## Non-Functional Requirements

### Security and privacy

*   The service starts only `git` and the secrets scanner, each with an empty environment, and never checks out a working tree.
*   Index artifacts are parsed with size limits, and paths that are not in the stored commit are dropped.
*   User tokens are encrypted at rest. Provider keys and the encryption key arrive through the environment. None of them appears in logs; the credential patterns are extended to cover GitHub's token formats.
*   Access is checked on every read of repository data, with the cache period stated in Goal 7.
*   Only the configured provider receives a repository's content, and only after the scan described under the model gateway.

### Performance

*   Navigation and search answer from Postgres with no model call.
*   AI answers and the brief stream as they are written.
*   Token use is bounded per question by a limit on tool rounds and tokens, and per repository by its budget.

### Rollout and rollback

*   **One repository first.** The index workflow is added to one repository, and the others follow after the first has been used for review.
*   **What is added to a repository:** one workflow file, and two sections in the agent skill file. Nothing else changes there.
*   **Rollback** is deleting that workflow file and reverting the skill file.
*   **The service can be switched off** without affecting GitHub, because it writes nothing there.
*   **Database changes** are versioned migrations applied at deploy time. A backup and restore is tested before the first repository is connected.

## Additional Resources

*   [`concepts_brief.md`](../../concepts_brief.md), [`consolidated_review_report.md`](reviews/round-1/consolidated_review_report.md) and [`goldfish_review_report.md`](reviews/round-1/goldfish_review_report.md): the concept, the review, and the response to it.
*   [`docs/architecture.md`](../architecture.md), [`docs/adr/`](../adr/) and [`docs/providers.md`](../providers.md): the existing CLI.
*   Bacchelli and Bird, "Expectations, Outcomes, and Challenges of Modern Code Review", ICSE 2013: <https://2013.icse-conferences.org/content/expectations-outcomes-and-challenges-modern-code-review.html>
*   GitHub documentation consulted: [personal access token policy](https://docs.github.com/en/organizations/managing-programmatic-access-to-your-organization/setting-a-personal-access-token-policy-for-your-organization), [workflow artifacts API](https://docs.github.com/en/rest/actions/artifacts), [comparing two commits](https://docs.github.com/en/rest/commits/commits).
*   `scip-typescript` and the SCIP format: <https://github.com/sourcegraph/scip-typescript>

## Future Improvements

*   **The later stages** listed under Non-Goals.
*   **Branch views.** Source is now fetched by the service, so a branch with no pull request could be shown with a diff and text search at no cost in CI.
*   **A scanner allowlist read from the base branch**, if false positives prove common.
*   **Answers that survive a dropped connection**, if phone use shows the need.
*   **Routing the CLI's model lane through the gateway**, if any repository has that lane on.
*   **Posting the workspace's review to GitHub**, once it is trusted.

--------------------------------------------------------------------------------

## Detailed Implementation Plan

**Gate.** No pull request starts until entry conditions E1 to E4 are answered and their results are written into this document. File-level detail below assumes all four pass.

The work is ten pull requests, numbered on from the existing plan's PR-46. Each leaves `make check` passing.

| PR | Adds | ADRs | A reviewer can then |
| --- | --- | --- | --- |
| PR-47 | Service skeleton: binary, config, migrations, logging, health address, container image, CI job | 0029, 0030 | Nothing yet |
| PR-48 | Identity (token route), sessions, access check | 0033 | Sign in |
| PR-49 | Source: fetch as the viewer, file store, diff, text search, pull request list and view | 0031 | Nothing visible yet |
| PR-50 | Web application: sign-in, pull request list, diff and new-code views, code view, GitHub links, shortcuts, touch sheet | None | **Read a diff and search, beside GitHub** |
| PR-51 | Navigation: index workflow, artifact download, index load and lookup | 0035 | **Navigate code** |
| PR-52 | Model gateway: scan at send, two provider adapters, budgets, usage ledger | 0032, 0034 | Nothing visible yet |
| PR-53 | Assistant: tool loop, streaming, assistant panel | None | **Ask questions about code** |
| PR-54 | Change brief and AI review on first open; agent skill additions | None | Read a brief and a review |
| PR-55 | Telemetry, history import, status page and notices | None | See measures and notices |
| PR-56 | Image release, retention sweep, documentation | 0036 | First version complete |

**What "can then" assumes.** After each pull request merges, the operator builds the image from that commit with the Dockerfile and deploys it. The release pipeline for the image arrives in PR-56. The index workflow needs no CLI release: until one includes the new template, the file is copied by hand from `examples/review-index.yml`.

**Feedback points.** After PR-50, PR-51 and PR-53 the team uses the workspace for one working week. The measures are: engineers who opened at least one pull request in it, pull requests opened, lookups and questions per pull request, "this was wrong" reports, and a two-question poll on whether it helped. Work continues to the next group only if the thresholds the author sets are met. Otherwise it stops and this document is revised.

**Outcome measure for the whole version.** Median time from a pull request being opened to its first human review, before and after, taken from GitHub history. It is read beside the share of reviewed pull requests that were opened in the workspace.

### Shared definitions

These are the interfaces that more than one pull request depends on. Everything else is decided in the pull request that builds it.

#### Service configuration

A YAML file named by `--config`, plus environment variables for every secret. No address, organisation or model name is compiled in (ADR-0004).

```yaml
listen: ":8080"
publicUrl: ""
github:
  apiBaseUrl: ""
  webBaseUrl: ""
  gitBaseUrl: ""                 # where `git fetch` reads from
indexArtifactName: ""            # the artifact name the index workflow uses
accessCacheSeconds: 0
limits: { fetchBytes: 0, fileBytes: 0, indexBytes: 0, toolRounds: 0, questionTokens: 0 }
retention: { commitDays: 0, conversationDays: 0, eventDays: 0 }
scan: { allowPaths: [] }         # operator-kept allowlist for the secrets scanner
repositories:
  - owner: ""
    name: ""
    provider: ""                 # anthropic or gemini
    model: ""
    baseUrl: ""
    apiKeyEnv: ""                # the NAME of the environment variable holding the key
    monthlyTokenBudget: 0
```

Environment variables: `WORKSPACE_DATABASE_URL`, `WORKSPACE_TOKEN_KEY`, and one variable per repository's `apiKeyEnv`. A zero in `accessCacheSeconds`, `limits`, `retention` or `monthlyTokenBudget` is a configuration error, so no value is chosen on the operator's behalf.

#### Index artifact

A workflow artifact named by `indexArtifactName`, holding two files:

*   `index.scip`: the output of `scip-typescript`.
*   `meta.json`: `{ "commit": "<40 hex>", "tool": "scip-typescript", "toolVersion": "" }`.

The service accepts it only if GitHub reports the artifact's workflow run as being for the same commit as `meta.json` and as the commit on screen.

#### Intent record

A block in the pull request description, between two comment markers that GitHub does not display:

```text
<!-- review-intent v1 -->
**Goal:** one or two sentences
**Plan:** the steps taken
**Alternatives rejected:** each with its reason
**Session:** an identifier the author can use to find the agent session
<!-- /review-intent -->
```

The brief reads the text between the markers. A description with no markers is treated as having no intent record.

#### Gateway interface

```text
Part     = Instruction(text)
         | Text(text, origin)                       # origin: typed | description | derived
         | Excerpt{path, commit, startLine, endLine, text}
         | ToolCall{id, name, arguments}
         | ToolResult{id, parts}
Turn     = { role: user | assistant | tool, parts }
Request  = { repository, user, feature, commit, turns, tools, maxTokens }
Event    = TextDelta(text) | ToolCall{id, name, arguments} | Done{inputTokens, outputTokens}
         | Stopped{outcome}

Converse(ctx, Request) -> stream of Event
AsProvider(repository, user, feature, commit) -> model.Provider   # one call, no tools

Outcome  = ok | quota_exhausted{resetAt} | rate_limited{retryAfter} | budget_blocked{budget}
         | secret_blocked{what} | scan_failed | refused | timed_out | malformed
```

`AsProvider` lets the CLI's review code call the gateway through the interface it already uses. Its whole prompt is sent as one `Text` part and scanned as such. After the call, the caller reads the outcome from the gateway, because the review code reports a provider failure only as a warning.

Withheld files are not an outcome: the call proceeds, and the list of withheld paths is returned beside the answer.

#### Database

Migrations are the source of truth for columns and types. These keys and value sets are fixed here because several pull requests read them.

| Table | Key | Fixed values |
| --- | --- | --- |
| `repository` | `org`, `name` | |
| `commit` | `repository_id`, `sha` | |
| `commit_stage` | `repository_id`, `sha`, `stage` | `stage`: source, index, brief, review. `state`: the states listed under "Opening a pull request" |
| `path_entry` | `repository_id`, `sha`, `path` | |
| `file` | `repository_id`, `hash` | `is_text`: false for content that is not valid UTF-8 or is over `limits.fileBytes`; such files store no text |
| `change` | `repository_id`, `base_sha`, `head_sha`, `path` | `status`: added, modified, removed, renamed |
| `index_entry` | `repository_id`, `sha`, `path` | |
| `occurrence` | `repository_id`, `document_hash`, position | `role`: definition, reference |
| `scan_result` | `content_hash`, `scanner_version`, `rules_hash` | `state`: clean, secret |
| `model_call` | `id` | `feature`: assistant, brief, plan, review. `outcome`: as in the gateway interface |
| `brief`, `ai_review` | `repository_id`, `sha`, `pull_number` | |
| `conversation`, `message` | `id` | `role`: user, assistant, tool |
| `app_user`, `credential`, `session` | `id`; `user_id`; `id_hash` | |
| `usage_event` | `id` | `kind`: the events listed under Telemetry and notices |
| `navigation_report` | `id` | |

#### HTTP API

Request and response shapes live in one file, `schema/workspace-api.schema.json`. Each pull request that adds a route adds its shapes there. The service's handler tests and the web application's fake API are both validated against it, so the two cannot drift apart unnoticed.

| Method and path | Purpose |
| --- | --- |
| `POST /auth/token`, `POST /auth/signout`, `GET /api/v1/me` | Sign in and out |
| `GET /api/v1/repos` | Repositories the user can read |
| `GET /api/v1/repos/{owner}/{repo}/pulls` | Open pull requests, from GitHub |
| `GET /api/v1/repos/{owner}/{repo}/pulls/{number}` | Head and merge base, stage states. Starts the fetch if needed |
| `GET /api/v1/repos/{owner}/{repo}/commits/{sha}/tree` | Paths at a commit |
| `GET /api/v1/repos/{owner}/{repo}/commits/{sha}/files?path=` | One file's text |
| `GET /api/v1/repos/{owner}/{repo}/compare?base=&head=` | Changed files and their hunks |
| `GET /api/v1/repos/{owner}/{repo}/commits/{sha}/search?q=&path=` | Text search |
| `GET /api/v1/repos/{owner}/{repo}/commits/{sha}/definition?path=&line=&col=` | Definition, with its class |
| `GET /api/v1/repos/{owner}/{repo}/commits/{sha}/references?path=&line=&col=` | References |
| `POST /api/v1/repos/{owner}/{repo}/navigation/reports` | "This was wrong" |
| `GET /api/v1/repos/{owner}/{repo}/pulls/{number}/brief?commit=` | Stored brief and review |
| `POST /api/v1/repos/{owner}/{repo}/pulls/{number}/brief?commit=` | Generate, or generate again; streams the brief |
| `POST /api/v1/repos/{owner}/{repo}/conversations` | Start a conversation |
| `GET /api/v1/repos/{owner}/{repo}/conversations/{id}` | Read a conversation back |
| `POST /api/v1/repos/{owner}/{repo}/conversations/{id}/messages` | Ask; the answer streams |
| `POST /api/v1/repos/{owner}/{repo}/events` | Report usage events |
| `GET /api/v1/repos/{owner}/{repo}/measures?from=&to=&format=` | Telemetry and its export |
| `POST /api/v1/repos/{owner}/{repo}/history-import` | Import pull request history |
| `GET /api/v1/status` | Health of GitHub, the provider and the database. Carries no repository data |
| `GET /healthz` | Liveness |

Every route under `/api/v1/repos/{owner}/{repo}` passes the access check before any data is read. Lines and columns are zero-based, as in the index format.

#### Shortcuts

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

Reserved keys that no shortcut may use: `F5`, `F11`, `F12`, and `Ctrl` or `Cmd` with `T`, `W`, `N`, `R`, `L`, `F`, `P` or `S`. Single-key shortcuts are off while a text field has focus.

#### Dependencies

Direct dependencies only. Before a pull request that adds one is merged, the full set of modules or packages it brings is added to `THIRD_PARTY_LICENSES.md`, as ADR-0028 and `make licenses` require. A licence the check refuses stops the pull request, and the dependency is replaced.

| Dependency | Used for | Licence |
| --- | --- | --- |
| `github.com/jackc/pgx/v5` | Postgres | MIT |
| SCIP Go bindings and `google.golang.org/protobuf` | Reading the index. The import path and licence are confirmed in entry condition E2 | Apache-2.0, BSD-3-Clause (to confirm) |
| `react`, `react-dom`, `react-router-dom` | Web application | MIT |
| `@codemirror/state`, `view`, `merge`, `language`, `lang-javascript`, `lang-python`, `lang-rust` | Code and diff display | MIT |
| `markdown-it` | Rendering briefs and answers, with raw HTML off | MIT |
| `mermaid` | Diagrams | MIT. Its own dependencies are checked in PR-53; one is believed to be dual-licensed |
| `typescript`, `vite`, `vitest`, `eslint`, `@playwright/test` | Build and test only | MIT, Apache-2.0 |

`git` and `gitleaks` are programs in the container image, not linked code. The HTTP server, routing, logging, migrations, encryption and archive reading use the Go standard library.

### Target Files to Create/Modify

Every pull request that adds a route also modifies `internal/workspace/api/router.go`, `cmd/workspace/serve.go` and `schema/workspace-api.schema.json`. Every pull request that adds a dependency or an ADR also modifies `THIRD_PARTY_LICENSES.md` or `docs/adr/README.md`. These are listed once here and not repeated in each table.

#### 1. PR-47: service skeleton

| File | Action | What changes and why |
| --- | --- | --- |
| `cmd/workspace/main.go`, `serve.go`, `main_test.go` | Create | Entry point with subcommands `serve`, `migrate` and `version`. A separate binary keeps the CLI free of database and web code. |
| `internal/workspace/wsconfig/config.go`, `config_test.go` | Create | The configuration above, a secrets type that never prints its values, and validation that reports every problem at once. |
| `internal/workspace/store/store.go`, `migrate.go`, `migrate_test.go` | Create | Connection pool, and a runner that applies embedded, numbered SQL files once each. |
| `internal/workspace/store/migrations/0001_core.sql` | Create | `repository`, `commit`, `commit_stage`. Enables the trigram extension. |
| `internal/workspace/store/repos.go`, `repos_test.go` | Create | Syncs configured repositories into `repository`. A repository removed from configuration keeps its rows and stops being served. |
| `internal/workspace/obs/log.go`, `log_test.go` | Create | Structured logger carrying request, user, repository, pull request and commit. |
| `internal/workspace/api/router.go`, `middleware.go`, `api_test.go` | Create | Route table, request identifier, logging and panic recovery. Starts with `/healthz`. |
| `internal/workspace/testdb/testdb.go` | Create | Connects to `WORKSPACE_TEST_DATABASE_URL`. Skips when it is unset, and fails when `WORKSPACE_REQUIRE_DATABASE` is set. This is a new variable, so the existing test matrix and release job, which set `REVIEWER_REQUIRE_TOOLS` and have no database, are unaffected. |
| `Dockerfile`, `.dockerignore` | Create | Builds the Go binary, and installs `git` and the pinned `gitleaks`. |
| `.github/scripts/install-tools.sh` | Modify | Accept tool names as arguments, so the image installs `gitleaks` alone. With no argument it installs all three, as today. |
| `examples/workspace.config.yaml` | Create | An example configuration with placeholder values. |
| `go.mod`, `go.sum` | Modify | Add `pgx`. |
| `Makefile` | Modify | A `workspace-test` target that runs the tests under `internal/workspace` and `cmd/workspace` with the database required. It is not part of `check`. `make test` still runs those packages, with database tests skipped. |
| `.gitignore` | Modify | Ignore the `workspace` binary. |
| `.github/workflows/ci.yml` | Modify | A `workspace` job on Linux with a Postgres service container, running `make workspace-test` and a container build. Added to the `required` job's list. |
| `docs/architecture.md` | Modify | Correct the count of CI jobs. |
| `docs/adr/0029-add-a-hosted-workspace-service.md`, `0030-service-store-beside-github.md` | Create | As described under "Changes to recorded decisions". |
| `docs/adr/0008-cli-not-service.md`, `0016-github-comments-as-the-store.md` | Modify | Metadata only: status `Superseded`, and the superseding ADR. |
| `docs/implementation-plan.md` | Modify | Append a pointer to this design for PR-47 to PR-56. |

#### 2. PR-48: identity and access

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/workspace/identity/identity.go`, `token.go`, `session.go`, `crypt.go`, `identity_test.go` | Create | The interface, the token route, session cookies (only a hash of the session identifier is stored), and encryption of stored tokens with the key from the environment. |
| `internal/workspace/access/access.go`, `middleware.go`, `access_test.go` | Create | `CanRead`, cached per user in memory for `accessCacheSeconds`. The same response for "no access" and "does not exist". |
| `internal/workspace/store/migrations/0002_identity.sql`, `store/identity.go`, `identity_test.go` | Create | `app_user`, `credential`, `session`. |
| `internal/workspace/api/auth.go` | Create | `POST /auth/token`, `POST /auth/signout`, `GET /api/v1/me`. |
| `internal/workspace/api/middleware.go` | Modify | Session lookup, and a same-site cookie plus a required custom header on requests that change state. |
| `internal/ghclient/client.go`, `client_test.go` | Modify | Add `GetRepo` as a method on `REST`, and add description, author and created and merged times to `PullRequest`. The `Client` interface is not changed, so the fakes in `internal/poller` and `internal/reporters` still compile. |
| `internal/model/redact.go`, `internal/model/provider_test.go` | Modify | Add the GitHub token prefixes `github_pat_`, `gho_`, `ghu_`, `ghs_` and `ghr_` to the credential pattern, and export a function that reports whether text contains a credential. |
| `docs/adr/0033-identity-behind-one-interface.md` | Create | |

#### 3. PR-49: source

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/workspace/source/fetch.go` | Create | Runs `git fetch` for one commit into a bare scratch directory, with hooks disabled, an empty environment, and the viewer's token passed through the environment. Enforces `limits.fetchBytes`. |
| `internal/workspace/source/import.go` | Create | Lists the commit's tree, stores files not already held, and writes path entries, in one transaction. |
| `internal/workspace/source/files.go`, `search.go` | Create | Tree, file text, and trigram search with a path filter and a fixed result limit of 200. |
| `internal/workspace/source/source_test.go` | Create | Fetches from a local repository built with `internal/testfixture`. Covers added, removed, renamed, binary and oversized files, and a match in an unchanged file. |
| `internal/diff/diff.go`, `diff_test.go` | Modify | Add `Between`, which diffs two commits in a bare repository and returns changed files with line ranges and patch text. The existing functions diff a working tree. |
| `internal/workspace/catalog/pulls.go`, `catalog_test.go` | Create | Lists and views pull requests from GitHub as the viewer, and resolves the merge base. |
| `internal/ghclient/client.go`, `client_test.go` | Modify | Add `Compare`, returning the merge base of two commits. |
| `internal/workspace/store/migrations/0003_source.sql`, `store/source.go`, `source_test.go` | Create | `path_entry`, `file`, `change`. |
| `internal/workspace/api/pulls.go`, `source.go` | Create | Handlers for repositories, pull requests, tree, file, compare and search. |
| `internal/workspace/api/api_test.go` | Modify | The access test: every route under a repository returns the "not found" response for a user without access. The test reads the route table, so later routes are covered without editing it. |
| `schema/workspace-api.schema.json` | Create | The shapes of the routes so far. |
| `docs/adr/0031-fetch-source-as-the-viewer.md` | Create | |

#### 4. PR-50: web application

| File | Action | What changes and why |
| --- | --- | --- |
| `web/package.json`, `package-lock.json`, `tsconfig.json`, `vite.config.ts`, `eslint.config.js`, `playwright.config.ts`, `index.html` | Create | Build, lint and test setup. The build writes into `internal/workspace/web/dist/`. `index.html` carries the content security policy. |
| `web/src/main.tsx`, `App.tsx`, `routes.tsx` | Create | Start-up and routes for sign-in, pull request list, pull request view and code view. |
| `web/src/api/client.ts`, `types.ts` | Create | Typed calls to the API, with one place that handles "signed out" and "no access". |
| `web/src/views/SignIn.tsx`, `PullList.tsx`, `PullView.tsx`, `CodeView.tsx` | Create | The four views. Sign-in lists the read-only permissions to grant. |
| `web/src/components/CodePane.tsx`, `DiffView.tsx`, `StageList.tsx`, `ActionSheet.tsx`, `CommandPalette.tsx`, `ShortcutHelp.tsx` | Create | Code and diff display, stage states, the touch sheet, the palette and the shortcut list. |
| `web/src/keys/keymap.ts`, `useHotkeys.ts`, `web/src/github/links.ts`, `web/src/styles/app.css` | Create | The shortcut table above, links to a file and line on GitHub, and layout for wide and narrow screens. |
| `web/src/keys/keymap.test.ts`, `web/src/github/links.test.ts`, `web/src/components/DiffView.test.tsx` | Create | No shortcut uses a reserved key; single keys do not fire in a text field; links carry the right file and line; the right commit is chosen for removed and added lines. |
| `web/e2e/journeys.spec.ts`, `web/e2e/fake-api.ts` | Create | CUJ 4, 8, 9 and 10 at a desktop and a phone screen size. The fake API's responses are validated against the schema file. |
| `internal/workspace/web/web.go`, `web_test.go`, `dist/.keep` | Create | Embeds the `dist` directory beside it, including the tracked `.keep` file, so the package compiles on a checkout where the web application has not been built. Serves a "not built" page in that case. |
| `.gitignore` | Modify | Ignore everything under `internal/workspace/web/dist/` except `.keep`. |
| `Dockerfile` | Modify | Add a stage that builds the web application before the Go binary. |
| `Makefile` | Modify | `web` and `web-test` targets. |
| `.github/workflows/ci.yml` | Modify | A `web` job: install, lint, unit tests, journeys. Added to `required`. The `workspace` job builds the web application before the container. |
| `tools/checklicenses/main.go`, `main_test.go` | Modify | Read a second package tree, `web`, into its own two sections, and key rows by section and name so a package in both trees does not collide. Accept a dual licence when one of its alternatives is permitted, and record which. Run `npm ci` for the tree it reads, since the CI job that calls it has no Node step today. |

#### 5. PR-51: navigation

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/installer/templates/review-index.yml.tmpl`, `examples/review-index.yml` | Create | The index workflow described under "The CI side", with the `scip-typescript` version found in entry condition E2. |
| `internal/installer/plan.go`, `write.go` | Modify | Write `.github/workflows/review-index.yml` when a `tsconfig` is detected. A new file, so `reviewer init` on an existing install adds it and leaves the other files alone. |
| `internal/installer/plan_test.go`, `write_test.go` | Modify | Assert the file is planned and rendered with TypeScript, absent without it, and that the review workflow and config outputs are unchanged. |
| `internal/ghclient/client.go`, `client_test.go` | Modify | Add `ListArtifacts` by name and `DownloadArtifact`. |
| `internal/workspace/navigation/fetch.go` | Create | Finds the artifact for a commit, checks its run against the commit, downloads it and unpacks it within `limits.indexBytes`. |
| `internal/workspace/navigation/load.go`, `query.go`, `navigation_test.go` | Create | Load and lookup as described under Navigation. Tests: a definition in an unchanged file, an interface with two implementations returned as a list, a definition in a dependency, a position outside its file, and a tampered index whose target name does not match. |
| `testdata/workspace/tsproject/` | Create | A small TypeScript project, its `index.scip`, and a `README.md` giving the command and tool version that produced it. |
| `internal/workspace/store/migrations/0004_index.sql`, `store/index.go`, `index_test.go` | Create | `index_entry`, `occurrence`, `navigation_report`. An unchanged document is not stored twice. |
| `internal/workspace/api/nav.go` | Create | Definition, references and "this was wrong". |
| `web/src/nav/navigation.ts`, `navigation.test.ts`, `web/src/components/References.tsx` | Create | Lookup calls, the base-or-head rule, history entries, and the result list labelled by class. |
| `web/src/components/CodePane.tsx`, `ActionSheet.tsx`, `web/src/keys/keymap.ts`, `web/src/api/client.ts`, `types.ts`, `web/e2e/journeys.spec.ts`, `fake-api.ts` | Modify | Wire navigation in; CUJ 2 and 3. |
| `go.mod`, `go.sum` | Modify | Add the SCIP bindings and protobuf. |
| `docs/adr/0035-scip-index-as-a-workflow-artifact.md` | Create | |

#### 6. PR-52: model gateway

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/analyzers/gitleaks/gitleaks.go`, `gitleaks_test.go` | Modify | Export `ScanDir(ctx, binary, dir, configPath)` and its report type. It passes the rule file explicitly and starts the scanner with an empty environment. The existing analyzer keeps its behaviour. |
| `internal/workspace/scan/scan.go`, `rules.toml`, `scan_test.go` | Create | Writes the content to a private temporary directory, runs `ScanDir` with the service's embedded rule file plus the configured allowlist, and remembers results. Tests use the fixture credential at `testdata/tiny-ts-repo/head/src/config.ts`, and confirm a `.gitleaks.toml` inside the scanned content has no effect. |
| `internal/workspace/gateway/gateway.go`, `provider.go`, `outcome.go`, `asprovider.go`, `fake.go` | Create | The steps under "The model gateway", the conversation interface, outcomes, the `model.Provider` wrapper, and a scripted provider for tests. |
| `internal/workspace/gateway/anthropic.go`, `gemini.go`, `anthropic_test.go`, `gemini_test.go`, `testdata/*.json` | Create | One adapter per provider. Fixtures are written by hand from each provider's published response format: text, a tool call, usage figures and a quota error. |
| `internal/workspace/gateway/gateway_test.go` | Create | Each kind of content with a planted credential: typed text and the diff stop the call; a file and a tool result are withheld and named. A scanner failure sends nothing. A budget is enforced before the call. A request for one repository reaches only that repository's adapter. Every call writes one record. |
| `internal/workspace/store/migrations/0005_ai.sql`, `store/ai.go`, `ai_test.go` | Create | `scan_result`, `model_call`, `conversation`, `message`, `brief`, `ai_review`. |
| `docs/adr/0032-one-gateway-scanning-at-send.md`, `0034-conversation-interface-for-providers.md` | Create | |

#### 7. PR-53: assistant

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/workspace/assistant/assistant.go`, `tools.go`, `prompts/system.md`, `assistant_test.go` | Create | The loop and the six tools, each bound to the conversation's repository and commit. Tests: a tool chain to an answer, the round limit, a tool asked for another commit refused, a quota outcome reaching the caller. |
| `internal/workspace/api/assistant.go` | Create | Start a conversation, read one back, post a message and stream the answer as server-sent events named `delta`, `withheld`, `done` and `stopped`. |
| `web/src/api/stream.ts`, `stream.test.ts` | Create | Reads the event stream and handles a dropped connection. |
| `web/src/components/AssistantPanel.tsx`, `Markdown.tsx`, `Markdown.test.tsx` | Create | The question box with the selected code quoted above it, the streamed answer, the model's name, links to each excerpt, and distinct messages for quota, budget and secrets. Markdown with raw HTML and images off. Tests that a script tag, a remote image and a script-bearing diagram are inert. |
| `web/src/views/PullView.tsx`, `web/src/components/ActionSheet.tsx`, `web/src/keys/keymap.ts`, `web/src/api/client.ts`, `types.ts`, `web/e2e/journeys.spec.ts`, `fake-api.ts`, `web/package.json` | Modify | "Ask" from a selection, a line and the palette; CUJ 5, 6, 7 and 9; add `markdown-it` and `mermaid`. |

#### 8. PR-54: change brief, AI review and the agent skill

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/workspace/brief/inputs.go`, `intent.go` | Create | Gathers the inputs listed under "The change brief and the AI review", and parses the intent record. Dependent code is the references, from the index, to symbols defined on changed lines, capped at 50 references with five lines of context each. |
| `internal/workspace/brief/brief.go`, `plan.go`, `review.go`, `prompts/brief.md`, `prompts/plan.md` | Create | The brief's two halves, the notes on the plan, and the findings on changed lines through `internal/laneb` and the gateway's `AsProvider`. The description and intent record are passed to `laneb` as guidance documents, which its input already supports, so `laneb` is not changed. |
| `internal/workspace/brief/brief_test.go`, `review_test.go` | Create | Guidance changed in the pull request is ignored in favour of the merge base's. A missing intent record is named. A disproved finding is not shown. A quota outcome is stored as "waiting on quota". A secret in the diff stores "not sent". |
| `internal/config/load.go`, `config_test.go` | Modify | Export a function that parses a config from bytes, so the guidance list can be read from a stored `.review/config.yaml`. |
| `internal/workspace/api/brief.go` | Create | Read the stored brief and review; generate, or generate again, streaming the brief. A second request while one is running returns the running state. |
| `internal/installer/templates/SKILL.md.tmpl`, `internal/installer/skill_test.go` | Modify | Two new sections: write the intent record in the form above when opening a pull request; and, when the author asks, read open review threads, fix, push and reply. |
| `web/src/components/Brief.tsx`, `AiReview.tsx` | Create | The brief at the top of the view with its two halves, the review's two parts, each labelled with commit and model, and the "generate again" action. |
| `web/src/views/PullView.tsx`, `web/src/api/client.ts`, `types.ts`, `web/e2e/journeys.spec.ts`, `fake-api.ts` | Modify | Show both; CUJ 1 and 11. |

#### 9. PR-55: telemetry, history import, status and notices

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/workspace/telemetry/events.go`, `measures.go`, `history.go`, `telemetry_test.go` | Create | Accepts usage events, computes the measures listed under the feedback points, exports them as CSV and JSON, and imports a repository's pull request history with the requesting user's credential. |
| `internal/ghclient/client.go`, `client_test.go` | Modify | Add `ListReviews`, for time to first review. |
| `internal/workspace/status/status.go`, `status_test.go` | Create | Health of GitHub, each provider and the database. |
| `internal/workspace/store/migrations/0006_telemetry.sql`, `store/telemetry.go`, `telemetry_test.go` | Create | `usage_event`, `history`. |
| `internal/workspace/api/telemetry.go`, `status.go` | Create | Handlers. |
| `web/src/telemetry/report.ts`, `web/src/views/Telemetry.tsx`, `Status.tsx`, `web/src/components/NoticeBanner.tsx` | Create | Event reporting in batches, the measures view with export, the status page and the banner. |
| `web/src/routes.tsx`, `App.tsx`, `web/src/api/client.ts`, `types.ts`, `web/e2e/fake-api.ts` | Modify | Add the two routes and their calls. |

#### 10. PR-56: release, retention and documentation

| File | Action | What changes and why |
| --- | --- | --- |
| `cmd/workspace/sweep.go`, `internal/workspace/store/retention.go`, `retention_test.go` | Create | The `sweep` subcommand and the removal rule under Retention. A file still referenced by a kept commit is kept. |
| `.github/workflows/release.yml` | Modify | A job that builds the image from the tag with the Dockerfile and publishes it to GitHub's container registry at the same version. `.goreleaser.yaml` is not changed, so the CLI archive is unaffected. |
| `docs/adr/0036-release-three-artifacts.md` | Create | |
| `docs/adr/0026-two-release-artifacts.md` | Modify | Metadata only. |
| `docs/workspace.md` | Create | Operating guide: configuration, the cloud resources needed, connecting a repository, running locally, backup and restore. It names no project-specific value. |
| `docs/architecture.md`, `README.md`, `CONTRIBUTING.md`, `docs/providers.md`, `CHANGELOG.md` | Modify | Describe the workspace, how to develop it, and which provider access paths it supports. |
| `SECURITY.md` | Modify | State what the service stores, how it is encrypted, and how the secrets rule applies to it. The CLI's own statements are unchanged. |

### Verification Plan

*   **Compilation/Build**: `make build` for both binaries on a clean checkout; `npm --prefix web run build`; `docker build .`.
*   **Unit Tests**: `make test`; `make workspace-test` with a database; `npm --prefix web test`; the existing `make plugin-test`.
*   **Static Analysis & Linting**: `make vet`, `make staticcheck`, `make fmt-check`, `make license-check`, `make licenses`, `npm --prefix web run lint`, `go run ./tools/checkadrs`.
*   **Test Coverage Requirement**: each goal has the test named below. No percentage target is set.
*   **E2E/Integration**: `npm --prefix web run e2e`; the source test fetches from a real local git repository.

| Goal | Test | What it shows |
| --- | --- | --- |
| 1. Open any pull request | `catalog` | Every pull request the fake GitHub lists appears, with the head commit GitHub reports |
| 2. Label navigation results | `navigation` | A tampered index whose target name does not match yields text search, and each class of result is labelled |
| 3. Search at the commit | `source` | A match in an unchanged file is found with no index loaded |
| 4. Ask the AI | `assistant` | Every excerpt given to the model is listed with the answer |
| 5. Reason for a change | `brief` and CUJ 1 | A brief is stored, or one of the named states |
| 6. AI review | `brief` | Each part is stored with its commit and model |
| 7. Mirror GitHub access | `api` | Every route under a repository refuses a user without access, and access removed on GitHub takes effect after the cache period |
| 8. Never execute pull request code | `source` and `scan` | The only programs started are `git` and the scanner, each with an empty environment and no working tree |
| 9. Scan everything sent | `gateway` | Each kind of content with a planted credential is stopped or withheld, and every call record carries a scan result |
| 10. Work without a model | `api` and CUJ 6 | With no provider reachable, diff, navigation and search answer |
| 11. Phone | CUJ 9 | Passes at a phone screen size |
| 12. Shortcuts | `keymap.test.ts` | No reserved key is used, and no single key fires in a text field |
| 13. Telemetry | `telemetry` | The export matches a fixed event set, and a user sees no events from a repository they cannot read |
| 14. Existing installs | `installer` | A default `reviewer init` writes the review workflow and config as before |

Checks to run by hand:

1.  **Before PR-47:** entry conditions E1 to E4.
2.  **Before PR-50:** search time on the largest connected repository.
3.  **Before the first repository is connected:** restore the database from a backup into an empty instance and open a pull request view.

--------------------------------------------------------------------------------

<!-- Provenance: Indicates initial structural drafting via EGM. -->

*Drafted with EGM*
