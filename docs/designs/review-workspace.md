# Review workspace: code navigation and AI help beside GitHub pull requests

*   **Author:** adamtait
*   **Status:** Proposed (second revision after review)
*   **Date:** 2026-11-23

--------------------------------------------------------------------------------

## Overview

The review workspace is a web application used beside GitHub's pull request page. It adds two things: code navigation inside a pull request (go-to-definition, find-references, and search across the repository at the pull request's commit), and an AI assistant that answers questions about the change and the code around it. Users keep using GitHub to comment, approve and merge.

It serves a team of 10 engineers across 4 or more repositories. The team opens dozens of pull requests a week, and coding agents write more than 90% of them. The code is mostly TypeScript.

Nothing described here is built yet. The present tense below describes the design.

The system has two parts:

*   **One CI job per repository.** It builds a navigation index for each pushed commit and saves it as a workflow artifact. It needs no new permission, and no secret beyond any the repository's own dependency install already needs.
*   **The workspace service.** When a user opens a pull request, the service reads the source and the index from GitHub with that user's own token, holds them in memory, and serves the web application. It never runs code from a pull request. It stores conversations with the assistant, a record of each model call, and "this was wrong" reports. It stores no source code outside those conversations and no tokens.

This document designs the first version: navigation and asking. A model-written summary of each change, called the change brief, is the next step. It is outlined here and not designed. Later phases are listed under Non-Goals.

The implementation plan at the end splits the work into eight of this repository's own pull requests, named PR-47 to PR-54. Those names are used throughout. They are not pull requests under review in the workspace.

### Terms

| Term | Meaning |
| --- | --- |
| User | A person signed in to the workspace. |
| Design owner | The person who asked for this tool and decides its open questions. |
| Pull request author | The person who opened a pull request. When an agent wrote the code, this is the person who ran the agent. |
| Operator | The person who deploys and configures the service. |
| Connected repository | A private repository the operator has listed in the service's configuration file. |
| Owner | GitHub's word for the user or organisation a repository belongs to. An *organisation owner* is a person who administers a GitHub organisation. |
| The CLI | The existing `reviewer` command-line tool in this repository. |

### Record of decisions

The concept is in [`concepts_brief.md`](../../concepts_brief.md). The design has been reviewed twice by independent reviewer agents that read it with no knowledge of how it was written. Each round's reports, and the response to them, are in [`reviews/round-1/`](reviews/round-1/) and [`reviews/round-2/`](reviews/round-2/).

On 2026-11-23 the design owner accepted the second round's proposals. Six of them defer or narrow something the design owner had asked for, so they are listed here:

1.  The change brief moves out of the first version and becomes the next step.
2.  AI findings on changed lines are left to the CLI, which posts them on GitHub.
3.  Diagrams are shown as their source text.
4.  Telemetry views wait for the next step. The first version is measured from request logs.
5.  A trial of wider review context, declined once, runs before the change brief is built.
6.  A public repository cannot be connected.

The Open Questions section below is the complete list of points that still need the design owner.

## The Problem

The design owner reviews each pull request line by line on GitHub's pull request page, and reports three gaps. Each is the design owner's account of daily use, set beside what GitHub documents.

**Navigation stops at the file.** On the pull request page, GitHub shows some references and declarations within the same file only. The design owner finds the results often wrong, and describes the feature as text search within one file. GitHub documents jump-to-definition and find-references for TypeScript in its repository file view, matched by name and not by the compiler ([GitHub docs](https://docs.github.com/en/repositories/working-with-files/using-files/navigating-code-on-github)). To follow a call from a changed line into unchanged code, the design owner leaves for an IDE, fetches and switches branch, and loses the place in the diff.

**There is no permitted way to ask.** GitHub offers Copilot Chat on pull requests to licence holders ([GitHub docs](https://docs.github.com/en/copilot/tutorials/using-copilot-to-explore-pull-requests)). The team is not permitted to use it. So there is no way to select a block of code and ask what it does, or to ask how an existing subsystem works.

**There is no platform the team controls.** The team wants to shape the review experience and to try its own improvements, such as the later phases listed under Non-Goals. This is a goal the design owner has set. It is not evidence that no existing tool navigates code, and the Alternatives section treats the existing tools on their merits.

Behind all three is one difficulty: building a mental model of a change. One study of code review at Microsoft found that understanding the change is the main difficulty reviewers report (Bacchelli & Bird, 2013). Agent-written code adds to it, because the person who opened the pull request did not type the code, and the reasoning lives in an agent session nobody else can see.

**The proposed solution.** Give the user a view of each pull request in which:

*   symbols can be followed from a changed line into unchanged code, using an index the repository's CI builds with the TypeScript compiler;
*   an AI assistant, using the one model provider configured for that repository, can be asked about anything in the repository at that commit;
*   the pull request author's stated reason for the change is at the top, read from the pull request description with no model involved.

It is built in this repository, beside GitHub, so the team can try these features without giving up anything GitHub already does.

The first version is also a test. After navigation ships, and again after the assistant ships, the team uses the workspace for one working week. The implementation plan says what is measured. The thresholds are not yet set, and no work starts until the design owner has set them.

## Background: the existing CLI

This repository holds `reviewer`, a command-line tool that reviews a pull request and posts comments. The design reuses parts of it, so a reader needs these facts.

*   **How it runs.** It is a stateless binary. `reviewer init` writes a GitHub Actions workflow, a config file (`.review/config.yaml`) and an agent skill file into a repository. The workflow runs the tool on every pull request, through a GitHub Action published from this repository. GitHub comments are its only store.
*   **Two lanes.** The deterministic lane runs analyzers: a secrets scanner (`gitleaks`), type checks, lint and convention rules. The model lane, whose code is the package `internal/laneb`, sends the diff to a language model for review. It is off by default and reports only on changed lines.
*   **The secrets gate.** If the scanner finds a credential in the diff, or cannot run, the model lane does not run (ADR-0012).
*   **Model access.** The package `internal/model` puts six ways of reaching a model behind one interface, `Provider`, with one method that takes text and returns text (ADR-0021). Two of the six are HTTP adapters for the Anthropic and Google APIs. The interface has no streaming, tool calls or token counts.
*   **GitHub access.** The package `internal/ghclient` is a small REST client. Together with `diff.FromPatches`, it already turns the per-file patches GitHub returns for a pull request into a list of changed files and line ranges, with no `git` program.
*   **The agent skill file.** `.agent/skills/code-review/SKILL.md` tells a coding agent to run the review before pushing and how to read the result. The team's two agents, Antigravity and Claude Code, both read it.
*   **The TypeScript plugin.** A separate Node process that runs the TypeScript analyzers. It answers no navigation queries.
*   **`reviewer metrics`.** A command that walks a repository's pull request history and reports how often each rule's findings were acted on. It does not report review times.
*   **Checks this repository runs.** `make check` builds, vets, tests and lints the Go code, checks the ADR log, and reconciles `THIRD_PARTY_LICENSES.md` against the real dependency graph with `tools/checklicenses`, which refuses copyleft licences. CI has one job, `required`, that names every other job as a dependency. The existing plan, `docs/implementation-plan.md`, numbers this repository's own pull requests; its last is PR-46.
*   **Rules this repository enforces.** A test fails if any non-test Go file names a host (ADR-0004). No model name is compiled in (ADR-0021). Nothing internal enters this repository's history, because it is meant to be public (ADR-0010). The CLI exits 0 whatever it finds (ADR-0009). ADRs are append-only, and a tool checks that.

## Goals

Each goal names the check that tests it. The Verification Plan maps each check to a test.

1.  **Open any open pull request of a connected repository.** A user opens it from a list, or at an address that mirrors GitHub's own path, and sees the diff by default or only the new code. Draft pull requests count. *Checked by:* a test with a fake GitHub in which every pull request it lists appears with the head commit and the changed files GitHub reports.
2.  **Navigate TypeScript at the pull request's commit, with every result labelled by where it came from.** Go-to-definition and find-references work on changed and unchanged code. A result carries one of four labels: from the CI-built index, text search, defined outside the repository, or not found. The service cannot verify the index, so the first label names the source and does not claim the result is correct. *Checked by:* a test for each label, including an index whose target does not match the name. In use: the "this was wrong" reports per repository, and the share of lookups under each label, both read from logs. The target share for the first label is not yet set; entry condition E2 measures it on one repository.
3.  **Search the repository at the pull request's commit.** *Checked by:* a test in which a match in an unchanged file is returned when no index exists.
4.  **Ask the AI about code at that commit.** A user selects code and asks about it, or asks about any code in the repository, and can ask for alternatives. A request for a diagram is answered with the diagram's source as text. *Checked by:* tests with a scripted model: a question about a selection reaches the model with that excerpt; the model reaches an answer through a chain of search, read and lookup tools; and the answer lists every excerpt the model was given.
5.  **Show the pull request author's stated reason at the top of the view.** The title, the description and, when present, the intent record are read from GitHub on every open. No model is involved. *Checked by:* a view test with and without an intent record.
6.  **Mirror GitHub access.** A user sees a connected repository's code and answers only if GitHub says that user can read it. The answer is cached for at most the configured period. *Checked by:* one test that walks every route carrying repository data as a user without access; one that access removed on GitHub is refused within the cache period; and one that a repository GitHub reports as public is refused for everyone.
7.  **Never execute pull request code on the service.** The service starts one program, the secrets scanner, and only over text the service itself wrote to a scratch directory. It never checks out a working tree, installs dependencies, or runs a repository's tools or tests. *Checked by:* a test over the service's own source that fails if any of its packages imports the standard library's package for starting programs, or calls the CLI's `git` helpers; and a test that the scanner is started with no inherited environment. The container image holds no `git` and no shell.
8.  **Scan everything the service sends to a model.** Every model call passes one gateway, which scans the outgoing content at the moment of sending and does not send what it cannot scan. Only the provider configured for a repository receives its content. The scanner recognises known credential formats; it does not recognise every secret. *Checked by:* every model call record carries a scan result, and a planted credential is caught for each kind of content and for each way content could try to switch the scanner off.
9.  **Work without a model.** Diff, navigation and search need no model. When the provider's quota is exhausted, AI features say that quota is the cause. *Checked by:* a test with no provider reachable, and a test for each form of quota response.
10. **Work on a phone for reading and asking.** *Checked by:* journey 8 on a screen 390 points wide. Touch navigation is an open question.
11. **Support desktop shortcuts.** Every action in the shortcut table has a key. *Checked by:* a test per action that its key triggers it, that no reserved key is used, and that single keys do not fire in a text field.
12. **Leave existing installs working.** A repository that already runs the CLI keeps its checks and comments. *Checked by:* a test that a default `reviewer init` writes every existing file as before, and adds the one new workflow file only where it detects TypeScript.
13. **Let an agent address review comments when the pull request author asks.** This closes the loop from review back to code without the service running an agent. *Checked by:* a test of the skill template's new section, and journey 10.
14. **Be a platform the team controls and can extend.** The service lives in this repository under its licence. *Checked by:* no automated check. "What the later phases require of this version" lists the points of extension.

## Non-Goals

**Left to GitHub**

*   Writing comments and suggestions, approving, requesting changes, resolving threads, showing CI status, and merging. Each view links to the matching place on GitHub.

**Left to the CLI**

*   AI findings on changed lines. Where a repository has the CLI's model lane on, the CLI posts them on GitHub.

**The next step, outlined under "The next step: the change brief"**

*   The change brief and the AI's notes on the plan.
*   Telemetry views and export for every user, a status page and notices.
*   A published container image.

**Later phases, each with its own design document**

| Phase | Contents |
| --- | --- |
| 2 | Learnings: candidates drawn from human reviews, and accepted learnings as files in the repository. A written list per repository of changes that always need a human, with labels shown and nothing skipped. |
| 3 | Comments written in the workspace and posted to GitHub. A "send to agent" action on a batch of comments. |
| 4 | Pull requests that merge with no human having read them, per repository. Planning, deployment and what happened after a change landed. |
| Unscheduled | Navigation for Python, bash and Rust. A second sign-in method through a GitHub App, unless entry condition E1 requires it first. |

**Not planned**

*   Installing or executing pull request code on the service.
*   Storing or sharing whole agent transcripts.
*   The service running coding agents or pushing commits.
*   Switching to a second model provider when quota runs out.
*   OpenAI models, and intent records from agents other than Antigravity and Claude Code.
*   Connecting a public repository.
*   A shared service for other teams.
*   Storage on the user's device.

## Open Questions

### Entry conditions

Each of these can remove a part of the design. E2 is answered before PR-47 merges, and E1 and E3 before PR-48 starts. The pass values marked "proposed" are the drafter's, and the design owner can change them before the trial is run.

*   **E1. Credential approval.** Put both sign-in methods to the organisation owner in one request: (a) fine-grained personal access tokens with read access to Contents, Pull requests, Metadata and Actions, which an owner approves per token by default; (b) a GitHub App installed on the connected repositories with the same read permissions. Ask two more things: whether any user is an outside collaborator, because outside collaborators cannot use fine-grained tokens on an organisation's repositories; and whether the connected repositories belong to more than one organisation, because one token covers one owner. *Pass:* (a) is approved, no user is an outside collaborator, and there is one organisation. *Otherwise:* if only (b) is approved, PR-49 is planned again for the App method before it starts, and the identity interface and everything behind it stay as designed. If neither is approved, work stops.
*   **E2. The indexer.** Run `scip-typescript` 0.6.2 in the index workflow on one repository, and on one managed with `bun` if the team has one. That release is the latest on 2026-11-23, bundles TypeScript 6 and needs Node 22. Record: each repository's TypeScript major version; whether the install needed a registry secret; the build time; the index size; whether documents use UTF-16 positions; and, for 50 symbols picked by hand from real pull request lines, how many the index resolves to the right definition. *Pass (proposed):* the workflow completes with the repository's usual install command, at least 40 of the 50 resolve correctly, and none resolves to a wrong place. *Otherwise:* navigation leaves the first version and this document is revised. No fallback indexer is designed.
*   **E3. Reading as the user.** With one fine-grained token, confirm: the archive call returns a commit named by its full hash; the workflow-runs call lists the index workflow's run for a given commit, and that run's artifact downloads; and the compare call returns the merge base. On the largest repository, record the time to fetch and unpack one archive, its unpacked size, and the memory used. *Pass (proposed):* all three calls work, and fetch and unpack take under 60 seconds. *Otherwise:* files are read one at a time through GitHub's contents call, search leaves the first version, and PR-50 is planned again.
*   **E4. Existing tools. Closed on 2026-11-23.** The design owner reports that the team is not permitted to use Copilot, and that GitHub's pull request navigation is same-file text matching that is often wrong. GitHub's web editor was not tried on one of the team's pull requests. The Alternatives section rejects it for reasons that do not depend on that trial.

### Other open questions

*   **Thresholds for the two feedback weeks, and the size of change in the outcome measure that counts.** The implementation plan says what is measured. The design owner sets the numbers before PR-47 starts.
*   **How long conversations are kept.** Conversations hold code excerpts. The service does not start without a value.
*   **Branch views.** The concept asked for a view per branch as well as per pull request. This design covers pull requests only. The index is built for every branch push and source is read by commit, so a branch view needs one more list and one more address. Is it wanted in the first version?
*   **Touch navigation.** The first version includes tapping a symbol to navigate. The test is journey 2 on the user's own phone during the navigation feedback week. If users cannot complete it, phones keep reading and asking only.
*   **Existing installs.** Which repositories have the CLI installed, and is the model lane on in any of them? AI findings on changed lines exist only where it is on.
*   **Refreshing the agent skill.** `reviewer init` does not overwrite an existing skill file without `--force`. Is a narrower refresh needed?
*   **The provider account's spend cap.** Before PR-53 starts, compare the account's monthly cap with the estimate under Risks and Limits.

## Critical User Journeys (CUJs)

### CUJ 1: Read the stated reason, then the diff

*   **Given**: A pull request in a connected repository.
*   **When**: A user opens it in the workspace.
*   **Then**: The title, the description and the intent record appear at the top, followed by the list of changed files. The diff appears when the source has been read from GitHub. If the description has no intent record, the view says so.

### CUJ 2: Follow a symbol into unchanged code and return

*   **Given**: A pull request view whose index is loaded.
*   **When**: The user triggers go-to-definition on a symbol in an added line.
*   **Then**: The view jumps to the definition, which may be in a file the pull request does not touch, marks that file as outside the change, and labels the result "from the CI-built index". One action returns the user to the same place in the diff.

### CUJ 3: A lookup the index could not resolve

*   **Given**: A symbol the index does not resolve, such as one in a Python file, or one defined in an installed dependency.
*   **When**: The user triggers go-to-definition on it.
*   **Then**: The user sees text search results labelled as such, or a note that the index places the symbol in a named dependency outside the repository, or a message that nothing was found. Nothing is presented as a definition.

### CUJ 4: Open a pull request just after a push

*   **Given**: A new commit was pushed a moment ago, and its index job is still running.
*   **When**: A user opens the pull request.
*   **Then**: The diff for the new commit is shown and text search works, because source comes from GitHub and not from CI. The view says CI is still building the index and links to the run. When the run finishes, navigation becomes available without a reload. Nothing is answered from the previous commit's index.

### CUJ 5: Ask about selected code

*   **Given**: A pull request view, and a provider configured for the repository.
*   **When**: The user selects a block of code and asks what it does and what else calls it.
*   **Then**: The answer streams in, lists each excerpt the model was given with a link to it, and names the model that produced it.

### CUJ 6: Quota is exhausted

*   **Given**: The configured provider is refusing requests because quota has run out.
*   **When**: The user asks a question.
*   **Then**: The user sees a message that quota is exhausted, with the time it resets when the provider gives one. Diff, navigation and search are unaffected.

### CUJ 7: A secret is found

*   **Given**: A file in the repository at this commit contains a credential in a format the scanner knows.
*   **When**: The user asks a question, and the model asks to read that file.
*   **Then**: That file is not sent. The model is told it was withheld, the answer continues without it, and the user is told which file was withheld and why. If the credential is in the user's own typed text or in the code the user selected, nothing is sent and the user is told why.

### CUJ 8: Read and ask on a phone

*   **Given**: A user on a phone.
*   **When**: The user opens a pull request.
*   **Then**: The stated reason and a single-column diff are readable. The user can tap a line and ask about it, and the code in question stays visible while the keyboard is open.

### CUJ 9: A user without access

*   **Given**: A signed-in user who cannot read a connected repository on GitHub.
*   **When**: That user follows a link to one of its pull requests in the workspace.
*   **Then**: The workspace shows nothing from that repository, and the response is the same whether or not the pull request exists.

### CUJ 10: An agent addresses review comments

*   **Given**: A pull request with open review threads on GitHub, in a repository whose agent skill file has the new section.
*   **When**: The pull request author tells their coding agent to address the review comments.
*   **Then**: The agent reads the open threads from GitHub, makes the changes, runs the CLI's review, pushes, and replies on each thread saying what changed. It does not resolve threads. It does none of this unless asked. The workspace takes no part; the push produces a new index, and the next open shows the new commit.

### CUJ 11: Leave a comment

*   **Given**: A user reading a changed line in the workspace.
*   **When**: The user chooses to comment.
*   **Then**: GitHub opens at that file and line of the pull request, and the user writes the comment there.

### CUJ 12: Two users open the same new commit at once

*   **Given**: A commit nobody has opened in the workspace.
*   **When**: Two users open its pull request in the same minute, and one closes the page while it loads.
*   **Then**: The other still gets the diff. Nothing is left marked as in progress, because no such mark is stored.

## Technical Plan (Architecture)

### Decisions this plan rests on

| # | Question | Choice | Recorded in |
| --- | --- | --- | --- |
| 1 | Who a user is and what they can read | The user's own fine-grained GitHub token, sealed into the session cookie and not stored. It sits behind one interface, so a GitHub App method can replace or join it. | ADR-0033 |
| 2 | How source reaches the service | One archive per commit, read from GitHub with the user's token when a user opens that commit, and held in memory. | ADR-0031 |
| 3 | How the navigation index is built and delivered | `scip-typescript` in a CI job on every branch push, saved as a workflow artifact, found through the workflow run for that commit, and read by a decoder kept in this repository. | ADR-0035 |
| 4 | Where the service runs and what it stores | A stateless container on Cloud Run. Managed Postgres holds conversations, model call records and navigation reports, and nothing else. | ADR-0029, ADR-0030 |
| 5 | How slow work runs | Inside the request that needs it. No "in progress" state is stored. | ADR-0029 |
| 6 | How the assistant gets code | The model calls read-only tools that the service runs. | ADR-0034 |
| 7 | Which provider receives a repository's content | The one provider configured for that repository, with no second choice. | ADR-0032 |
| 8 | Where secrets are scanned | In the model gateway, at the moment of sending. | ADR-0032 |
| 9 | Language and shape of the service | Go, as a second binary in this repository. | ADR-0029 |
| 10 | Web application | TypeScript, React and CodeMirror 6, built into static files and embedded in the service binary. | No ADR. It is contained in `web/` and can be replaced without changing the service's API. |

Alternatives for each row are under Alternatives Considered, grouped by these numbers after one group on scope.

### The trust rule

The service relies on GitHub for four kinds of data and on CI for one.

| Data | Source | How far it is relied on |
| --- | --- | --- |
| A pull request's head commit, base, title, description and state | GitHub's API, read with the user's token on every open | Trusted. |
| The merge base, and the changed files with their line ranges | GitHub's compare call and its pull request files call | Trusted. |
| Source code at a commit | GitHub's archive of that commit, named by its full hash | Trusted as GitHub's answer. An archive carries no content hash the service can check, and it omits paths the repository marks `export-ignore`. Both are listed under Risks and Limits. |
| Which workflow run built which commit, and whether it succeeded | GitHub's Actions API | Trusted. |
| The navigation index | The artifact of that run | Not verifiable. The commit's own workflow file builds it, so it is treated as written by whoever pushed the commit, and bounded as described under Navigation. |

Nothing reaches the service by upload. It has no upload address.

### Components

```text
  Repository on GitHub                               Google Cloud project
 +---------------------------------+               +---------------------------------------------+
 | index workflow, on every push   |               | workspace service (Cloud Run)               |
 |   check out the pushed commit   |               |                                             |
 |   install, run scip-typescript  |               |   identity -----> sign-in, sealed cookie    |
 |   save the index as an artifact |               |   access -------> can this user read this   |
 +---------------------------------+               |   catalog ------> pull request list, view   |
                                                   |   source -------> archive, files, search    |
  GitHub API  <----------------------------------- |   navigation ---> index, lookups            |
    pull request, merge base,    with the user's   |   scan ---------> secrets scanner           |
    changed files, archive,      own token         |   gateway ------> the one door to a model   | ---> configured
    workflow run, artifact                         |   assistant ----> tool loop                 |      provider
                                                   |   web ----------> static files, JSON API    |
  Browser, desktop or phone  <------ HTTPS ------> |   in memory: archives, indexes, scan results|
                                                   +----------------------+----------------------+
                                                                          |
                                               Postgres: conversations, model call records, reports
```

### The CI side

`reviewer init` writes one new workflow file, `.github/workflows/review-index.yml`, into a repository that has a `package.json` and a root `tsconfig.json`. That is the condition under which it already adds the TypeScript plugin. The existing review workflow, the config file and the published Action are not changed.

The index workflow runs on every branch push. Its one job:

1.  Checks out the pushed commit, which is the default for a push.
2.  Installs dependencies with the same install step the existing review workflow uses for that repository.
3.  Runs `scip-typescript` at a pinned version.
4.  Saves the output, `index.scip`, as a workflow artifact named `review-index`.

The job holds read access to contents and nothing else. If a step fails, the workspace shows that the index job failed for that commit, with a link to the run.

The trigger is a push and not a pull request event, for three reasons. GitHub does not run pull request workflows while a pull request has a merge conflict. The default checkout on a pull request event is a temporary merge and not the commit the workspace shows. And one trigger then covers pull request heads and the default branch alike. The costs are a second dependency install on every push to every branch, no index for a pull request from a fork, and no index for a commit pushed by a workflow with its own token, because GitHub starts no run for such a push.

The agent skill file that `reviewer init` writes gains two sections:

*   **The intent record.** When opening a pull request, the agent writes a block into its description: the goal, the plan, the alternatives that were rejected, and a session identifier the pull request author can use to find the session again.
*   **Addressing review comments.** Only when the pull request author asks, the agent reads the open review threads from GitHub, makes the changes, runs the review, pushes, and replies on each thread. It does not resolve threads.

These sections change what agents do in every repository whose skill file is refreshed. Agents there will write a block into pull request descriptions, and, when asked, will push commits and reply on review threads with the pull request author's own GitHub credentials.

### The workspace service

One Go binary, `cmd/workspace`, serves the API and the web application. The table lists its parts and what each reuses from the CLI.

| Part | What it does | Reuses |
| --- | --- | --- |
| `identity` | Signs a user in with their token, seals the token into the session cookie, and gives each request a GitHub client that acts as that user. | `internal/ghclient` |
| `access` | Answers "can this user read this repository" by asking GitHub, and caches the answer in memory for the configured period. Refuses a public repository. | `internal/ghclient` |
| `catalog` | Lists pull requests and reads one, with its merge base and changed files, from GitHub's live data. | `internal/ghclient`, `diff.FromPatches` |
| `source` | Reads a commit's archive from GitHub, holds its text files in memory, and serves the file list, file text and text search. | Nothing |
| `navigation` | Finds a commit's index run, downloads and decodes the index, holds it in memory, and answers definition and reference lookups. | Nothing |
| `scan` | Runs the secrets scanner over content about to be sent to a model. | `internal/analyzers/gitleaks` |
| `gateway` | The only package that is given provider keys. Scans, enforces the budget, calls the provider, records the call. | `internal/model` |
| `assistant` | Runs a conversation: sends the question, runs the tools the model asks for, streams the answer. | Nothing |
| `store` | Reads and writes the four database tables. | Nothing |
| `web` | Serves the built web application and the JSON API. | Nothing |

### Opening a pull request

1.  The browser asks for the pull request. The service checks access, then asks GitHub, with the user's token, for the pull request, the merge base of its head and base, and its changed files. The stated reason and the file list are shown.
2.  The browser asks for the file list at the head commit and at the merge base. For each, if that commit's archive is not in this container's memory, the service reads it from GitHub within that request and unpacks it into memory. The diff is shown when both have arrived, and search works from then on.
3.  The browser asks for the state of the index at the head commit. The service asks GitHub for the index workflow's run for that commit, and answers with one of four states:

    | GitHub reports | State shown | What the service does |
    | --- | --- | --- |
    | No run for this commit | No index for this commit | Nothing. Lookups use text search. |
    | A run that has not finished | CI is still building the index | Nothing. The browser asks again every 15 seconds while the view is open. |
    | A run that failed or was cancelled | The index job failed, with a link to the run | Nothing. |
    | A run that succeeded | Ready | Downloads the run's `review-index` artifact, decodes it and holds it in memory. |

**How this work runs.** All of it runs inside the request that needs it. No state is stored for any of it. A request that finds nothing in memory does the work itself. Within one container, two requests for the same commit share one fetch. In two containers, each fetches once. A request that ends early leaves nothing behind, and the next request starts again.

**Time limits.** Cloud Run ends a request at the service's configured timeout, which is 300 seconds by default ([request timeout](https://docs.cloud.google.com/run/docs/configuring/request-timeout)). The service sets its own shorter deadline on each fetch from configuration. Entry condition E3 measures the fetch.

**After an idle period** Cloud Run stops the container, and the memory goes with it. The next open of any commit reads from GitHub again.

### Source in memory

*   **Reading an archive.** The service asks GitHub for the commit's tar archive and unpacks the stream in memory. It keeps the path of every entry, and the text of every file that is valid UTF-8, holds no NUL byte, and is within the per-file size limit. Other files are kept as a path with a mark: binary, or too large. If the kept text passes the per-archive limit, the fetch fails with that reason.
*   **The cache.** Archives and decoded indexes share one in-memory cache per container, bounded by a configured number of bytes. When it is full, the entry used longest ago is dropped. The operating guide gives the rule for sizing the container's memory from the cache size and the two per-item limits.
*   **Text search.** A search is a literal substring match over the kept text of one commit, case-insensitive unless the user asks otherwise, with an optional path prefix. It returns at most 200 matching lines and says when it stopped early.
*   **Paths the archive lacks.** GitHub's changed-file list can name a path the archive omits. The view shows that file as not available in the archive, with a link to it on GitHub.

Access to cached source is governed by the access check, which runs before any read.

### The diff

The changed-file list and line ranges come from GitHub. For each changed file, the browser asks for the file's text at the merge base and at the head commit, and draws the diff from those two versions. A renamed file is read at its old path on the merge base. The service computes no diff.

### Navigation

The index cannot be verified, so it is bounded.

*   **Binding.** An index is accepted only from a run that GitHub reports as a push run of the index workflow file for the commit on screen. A run for one commit cannot supply another commit's index.
*   **Size and shape.** The artifact and the decoded index are size-limited. A document whose path is not in the commit's archive is dropped. A document whose positions are not in UTF-16 units is dropped. An occurrence whose position is outside its file is dropped.
*   **Finding the symbol.** A lookup finds the smallest occurrence in the index that covers the position asked about. If there is none, the lookup becomes a text search for the word at that position.
*   **Name check.** A target is returned under the index label only if the text at the target equals the text at the position asked about, or equals the name at the end of the symbol's identifier. This catches a target that points at unrelated text. It does not catch a target that points at a different symbol with the same name. A target that fails the check is not shown, and the lookup becomes a text search.
*   **Outside the repository.** If the index has no definition for a symbol and the symbol's identifier names a package, the result says the index places the definition in that package and version. There is nothing to jump to.
*   **References.** A reference list holds every occurrence of the symbol that passes the name check, at most 500, and is labelled "from the CI-built index". An index can cover only part of a repository, so the view never says "no references". It shows the index's list beside the text matches for the same word.

A lookup from a removed line uses the merge base, and any other lookup uses the head commit. A merge base has an index only if it was once the tip of a push. Otherwise lookups from removed lines use text search.

A false or partial index can mislead users of that commit's navigation, and the assistant when it uses its two lookup tools. Those tools pass the same labels to the model. A false index cannot change the diff, the source shown or text search results.

### Identity and access

The identity interface has three operations: sign a user in, say whether that user can read a repository, and return a GitHub client that acts as that user.

**The token method.** On first visit the user pastes a fine-grained personal access token. The service accepts it only if it has the fine-grained prefix, GitHub confirms whose it is, and it can read at least one connected repository. A classic token is refused, because it can carry write access to every repository the user can reach. A token still waiting for an organisation owner's approval reads nothing private, and the refusal message says so.

**The session cookie.** The service seals the token, the user's login and an expiry time, a configured number of days ahead, with authenticated encryption, using a key supplied through the environment, and sets the result as a cookie that scripts cannot read. Nothing about the session is stored on the server. Three consequences follow. Signing out clears the cookie in that browser only. A copied cookie works until it expires or the token is revoked on GitHub. Changing the key signs everyone out.

The service makes no write call to GitHub, whatever the token allows. It has not been confirmed that a fine-grained token's permissions can be read back, so the service does not check them.

**The access check.** Before any read of a repository's data, the service asks GitHub for the repository with the user's own token. If GitHub answers that the user cannot see it, the response is "not found". If GitHub answers that the repository is public, the service refuses it for every user and logs a configuration error, because every GitHub token can read a public repository. A positive answer is cached in memory for the configured period, so access removed on GitHub takes effect within that period.

### The model gateway

The gateway is the only package that is given provider keys. The binary reads each key once at start and hands it to the gateway alone. Each connected repository names one provider and one model, and its content is sent nowhere else.

**At the moment of sending**, the gateway does the following.

1.  It takes every part of every turn in the request. For a part that quotes a file, it takes the whole file the lines come from.
2.  It runs the secrets scanner over each item not already cleared. Results are remembered in memory by content hash, scanner version and rule file, so an item already cleared costs nothing.
3.  If the scanner cannot run, nothing is sent.
4.  It applies the rule for each kind of content:

    | Content | If a credential is found |
    | --- | --- |
    | The user's typed question | Nothing is sent. The user is told why. |
    | The code the user selected | Nothing is sent. The user is told which file. |
    | A file, or lines from files, that a tool returned | That file's lines are withheld. The model gets a note in their place, and the user is told which file. |
    | The pull request title, description and intent record, when a tool returns them | The description is withheld in the same way. |
    | Earlier turns of the conversation | Each part is treated by its own kind, as above. A part already cleared is not scanned again while its result is remembered. |

5.  It checks the repository's monthly token budget, calls the provider, and records the call: user, repository, commit, provider, model, the scan result, the paths withheld, tokens in and out, and the outcome. The record holds no content.

**How the scanner is run.** Content can try to switch a scanner off, so the gateway runs it in a fixed way. Each item is written under a generated file name in its own numbered directory inside a scratch directory, and the scanner is pointed at the scratch directory. The scanner runs with no inherited environment, with the service's own rule file, with its inline exemption comment disabled, and with decoding of encoded text enabled. These choices were tested against the pinned scanner version, 8.28.0, with a planted credential:

| Attempt | Result without the measure | Measure | Result with it |
| --- | --- | --- | --- |
| An exemption comment on the credential's line | Not reported | The flag that disables the comment | Reported |
| An ignore file beside the scanned content | Not reported when the file is at the scan root | No content is ever written at the scan root | Reported |
| The credential wrapped in base64 | Not reported | Decoding enabled | Reported |
| A file name the default rules exempt, such as a lockfile | Not reported | Generated file names | Reported |

There is no allowlist. A fixture credential in a file therefore keeps that file from the model. How often this happens is unknown, and it is listed under Risks and Limits.

**Outcomes.** A call ends in exactly one outcome, and each has its own message.

| What happened | Outcome | What the user is told |
| --- | --- | --- |
| The provider answered | `ok` | |
| The provider limited the rate and gave a retry time | `rate_limited` | The provider is limiting requests, and when to try again |
| The provider reported a spend cap or usage limit, or limited the rate with no retry time | `quota_exhausted` | Quota is exhausted, with the reset time if the provider gave one |
| The repository's own monthly budget would be passed | `budget_blocked` | The budget and the month |
| A credential in the question or selection | `secret_blocked` | What held it, and that nothing was sent |
| The scanner could not run | `scan_failed` | Nothing was sent |
| The provider declined to answer | `refused` | The provider declined |
| The answer hit the token limit | `max_tokens` | The answer was cut short |
| The provider could not be reached, timed out, or sent something unreadable | `unavailable` | The provider could not be reached |

For the Anthropic API, a spend cap arrives with the same status code as a rate limit and is told apart by an error code in the body and the absence of a retry time ([rate limits](https://platform.claude.com/docs/en/api/rate-limits)). Each adapter's tests hold one recorded example of each form its provider documents.

**Provider adapters.** `internal/model` gains a second interface beside `Provider`, for a conversation: turns, tool calls, streamed text and token counts. The existing Anthropic and Google HTTP adapters implement it and share their existing request, retry and redaction code. The two adapters that drive a signed-in CLI cannot implement it and are not asked to. The CLI's own call sites keep using `Provider` and do not change.

The CLI's model lane runs in CI, outside this service, and is off unless a repository turns it on. This design does not change it. Goal 8 covers the calls the service makes.

### The assistant

A question starts or continues a conversation tied to a repository, a commit and a user. Only that user can read it. The gateway sends the conversation with a description of the tools. The model may answer or ask for a tool. The service runs the tool and returns the result through the gateway. This repeats until the model answers, or a configured limit on rounds or tokens is reached.

The six tools are read-only and act on the commit the conversation is tied to: search text, read a range of a file, find a definition, find references, list changed files, and read the pull request's title, description and intent record.

Text inside code, descriptions and tool results can contain instructions aimed at the model. The tools cannot write, post or reach the network, and the instructions given to the model say that such text is material to read and not direction to follow. What such text can still do is listed under Risks and Limits.

A request for a diagram is answered with diagram source in a code block, which the view shows as text.

### The web application

One page application with four views: sign-in, the list of open pull requests, the pull request view (stated reason, diff, assistant panel), and the code view for files outside the diff.

*   **Code and diff** are drawn with CodeMirror 6, which supports touch input. The default is the diff, and a toggle shows only the new version of each changed file.
*   **Moving around.** Back and forward follow browser history, and every location has its own address.
*   **Narrow screens** get a single-column diff. Tapping a symbol or a line number opens one sheet listing every action available there, so comment, navigate and ask do not compete for one gesture.
*   **Shortcuts** follow the `j`/`k` and `g`-prefix conventions that GitHub and Gerrit use. The table of keys is in the implementation plan.
*   **Model output** is drawn as Markdown with raw HTML disabled, images disabled, and every link shown with its full address. A statement the model makes with no excerpt behind it is shown like any other; the list of excerpts under the answer is what the user can check.
*   **Content security policy.** The service sends it as a response header on every page. It allows scripts, connections and frames from the service's own origin only.

### Logs, measures and health

Every request writes one structured log line with a request identifier, the user's login, the repository, pull request and commit, the route's name, the duration and the outcome. A lookup's line also carries its label. Logs never contain source code, descriptions, conversation text, tokens or keys. Secrets are held in a type that prints only whether each is set, and errors from GitHub and from providers pass through the CLI's existing redaction with the token and key values.

The measures for the feedback weeks are counts over these log lines. The operating guide gives the saved query for each. Only the operator can read the logs.

A health address reports whether the database answers.

### The next step: the change brief

This section records what is settled about the next step. It is not a design.

*   **A trial comes first.** The premise of the brief is that a model given the stated reason and wider context reads a change better. The CLI's review code gains one input for text supplied by the pull request author, shown to the model apart from the repository's guidance documents, which it is told to obey. The trial runs the model lane with and without that input on one repository's pull requests. The brief is built only if the design owner judges the result worth it.
*   **What it holds.** One half restates what the pull request author says. The other is the model's reading of the code, labelled as that. Notes on the plan list gaps and places where code and plan differ. The brief draws on GitHub's data and the source only, and not on the index.
*   **Its states** include one for a budget stop as well as for quota, a withheld secret and failure.
*   **It gets its own feedback week**, and telemetry views, a status page, notices and a published container image are planned with it.

### What the later phases require of this version

*   **Commenting (phase 3).** GitHub stays the record for comments and approvals. This version stores no copy of them. The identity interface already returns a client that acts as the user.
*   **Learnings (phase 2).** Accepted learnings are files in the repository, so they arrive with the source.
*   **The change brief.** The gateway already takes a call that has no tools.
*   **A second sign-in method.** Everything behind the identity interface takes a GitHub client and a login, and does not know how the user signed in.

These are the constraints known today. The later phases are not designed.

### Changes to recorded decisions

ADRs in this repository are append-only. The checking tool allows only whole-record supersession, so a superseding ADR restates the part of the old decision that still holds. "Builds on" means the new ADR cites the old one and leaves it in force.

| New ADR | Decision | Relation to existing ADRs |
| --- | --- | --- |
| 0029 | Add a hosted, stateless workspace service beside the CLI, as a second Go binary; slow work runs inside the request | Supersedes ADR-0008, and restates that the CLI stays a stateless binary |
| 0030 | The service keeps a small store of its own; GitHub stays the record for comments and approvals | Supersedes ADR-0016, and restates that GitHub comments stay the CLI's store |
| 0031 | Read source from GitHub with the user's token and hold it in memory; treat the CI-built index as untrusted | Builds on ADR-0018 |
| 0032 | One gateway for the service's model calls, scanning at the moment of sending, with one provider per repository | Builds on ADR-0012 |
| 0033 | Identity behind one interface; the token method, with the token sealed in the session cookie | New |
| 0034 | A conversation interface beside `Provider`, implemented by the two HTTP API adapters | Supersedes ADR-0021, whose stated reasons to revisit were cost reporting and streaming. Restates the one-method `Provider` for the CLI and the rule that no model name is compiled in |
| 0035 | SCIP as the navigation index format, built in CI on every push, delivered as a workflow artifact, read by a decoder in this repository | New |

ADR-0018 fixes the review workflow's trigger and permissions. This design leaves that workflow alone, and the index workflow holds only read access to contents.

## Alternatives Considered

Each entry gives the trade-off that decided it.

### Scope and product shape

| Alternative | For | Against, and why it was not chosen |
| --- | --- | --- |
| GitHub's pull request page as it is | Nothing to build | Its navigation is same-file text matching that the design owner finds often wrong, and its way to ask is Copilot Chat, which the team is not permitted to use. |
| GitHub's web editor, opened from a pull request | VS Code for the Web has offered full-project TypeScript navigation since version 1.93 ([release notes](https://code.visualstudio.com/updates/v1_93)); nothing to build | The team cannot extend it, and it offers no permitted way to ask. The same release notes say the feature does not work in Safari. The editor has no compute ([GitHub docs](https://docs.github.com/en/codespaces/the-githubdev-web-based-editor)), so it cannot run a dependency install. It was not tried on one of the team's pull requests. |
| Hosted review tools from other vendors, such as Devin Review or Sourcegraph's browser extension | They exist today | The repository's content would go to another vendor, and the team could not extend them. |
| Each engineer runs the binary on their own machine with their own token | No hosting, no sign-in, no database | No phone use, which the design owner requires, and a conversation does not follow the user between devices. |
| Replicate GitHub's pull request page first | One place for all review work | All-or-nothing: users keep opening GitHub until commenting, approval, CI status and merge all exist, and those would be built before navigation or AI help. |
| Extend the CLI only, posting a change brief as a comment | No service to host | A comment cannot offer navigation or a conversation. |
| A new repository for the service | No existing decisions to supersede | The service reuses the CLI's packages, and two repositories would need releases kept in step. |
| The change brief in the first version | It addresses the mental-model difficulty most directly | Its premise is untested, and it adds a second consumer of model quota and a shared, stored output before anything is measured. It is the next step, after a trial. |
| The workspace makes its own AI findings on changed lines | One place to read AI findings | The CLI already posts them on GitHub. Reusing its review code would put the pull request author's text where the model is told to obey, and that code cuts a long diff and drops failed checks without telling the caller. |
| A trained risk classifier that lets pull requests merge unread | The largest possible saving in review time | With zero observed misses, showing a miss rate under 5% needs about 60 known high-risk pull requests per repository. No labelled history exists. Deferred to phase 4. |
| Export whole agent transcripts as the source of intent | The complete record of how a change was made | A transcript holds whatever the agent saw, including other repositories' content and pasted credentials. An intent record in the pull request inherits GitHub's access rules. |
| The service resumes the original agent session to make fixes | The agent that wrote the code makes the fix | The session lives on the pull request author's machine, which a hosted service cannot reach. The skill section in journey 10 gets the same result from the pull request author's side. |

### Decision 1: identity

| Alternative | For | Against, and why it was not chosen |
| --- | --- | --- |
| A GitHub App sign-in method | Users paste no token; an App's permissions are narrow | Not rejected. It needs an organisation owner to install an App, and so does approval of fine-grained tokens. Entry condition E1 puts both to the owner. The token method is the one designed here because it needs no registration. |
| OAuth App sign-in | A familiar sign-in button | An OAuth App cannot ask for narrow permissions. The only scope that reads private repositories also grants write access to every repository the user can reach. |
| Google sign-in with one read-only service token | Users handle no GitHub credential | That one token reads every connected repository, and a hand-kept table maps accounts to GitHub logins. A wrong row grants the wrong access. |
| Store each user's token on the server, encrypted | Sign-out and revocation take effect on the server | A store of credentials to protect and back up. The encryption key sits in the process's memory either way. |
| A list of permitted logins at sign-in | Sign-in is limited to the team | A second access table beside GitHub's, with the same wrong-row risk. Refusing public repositories closes the same exposure with no table. |
| A cloud key service for the cookie key | The key never enters the process | A client library and its dependencies. The key is supplied through the environment from the platform's secret store. The cost is that the key is in the process's memory. |

### Decision 2: how source reaches the service

| Alternative | For | Against, and why it was not chosen |
| --- | --- | --- |
| Fetch with `git` and store files, paths and diffs in Postgres | Git checks every object against its hash; later commits store only changed files | It rebuilds git's storage in Postgres: three tables, a search index and a sweep that removes old commits. The service would start a second program and hand it the user's token. |
| Keep a bare git repository on a disk | Git already stores, diffs and searches | It needs a persistent volume. On Cloud Run that is a network file system, which Cloud Run documents as lacking file locking ([volume mounts](https://docs.cloud.google.com/run/docs/configuring/services/nfs-volume-mounts)); otherwise it needs a virtual machine. |
| Read each file from GitHub when it is viewed, and hold nothing | Nothing in memory; GitHub checks access on every read | No text search at a commit, which needs the whole tree. This is the fallback if entry condition E3 fails. |
| The CI job pushes source to the service | Source is ready before anyone opens the pull request | The pull request controls its own CI job, so the service would need to verify what it receives, a public upload address and a new workflow permission. |
| Sandboxed workers on the service install and index pull request code | No dependence on GitHub Actions | The service would have to operate a sandbox. |
| Poll GitHub on a timer with a designated token | Changes are seen without a user | A standing credential that reads every connected repository. |
| A second workflow that reports pull request events | The service learns of a close or an edit at once | A second workflow file per repository, and a lost event goes unnoticed. Reading the pull request on every open already gives its current state. |

### Decision 3: the navigation index

| Alternative | For | Against, and why it was not chosen |
| --- | --- | --- |
| The CLI's TypeScript plugin exports an index | No new tool in CI | The most code to write and own, and a separate exporter for each later language. |
| The older LSIF index format | An established format | Not evaluated beyond noting that the indexer tried in E2 writes SCIP. |
| Trigger the index job on pull request events | It runs only for commits in a pull request, so it uses fewer CI minutes | No run while a pull request has a merge conflict, and the default checkout is a temporary merge. |
| The CI job uploads the index to the service | The index is loaded before anyone opens the pull request | An upload address, a signed-token check, and a workflow permission granted to a job that runs pull request code. |
| Build the index inside the existing review job | No second dependency install | That job runs on pull request events, with the two problems above. |
| Read the index with the SCIP project's Go library | A maintained decoder | It requires Go 1.25, and this repository declares 1.24. Its dependencies include a module under MPL-2.0, which this repository's licence check refuses. |
| Measure how much of the repository an index covers | The view could say "320 of 400 files indexed" | More mechanism, in defence of a claim ("nothing refers to this") that the first version does not make. |

### Decision 4: hosting and storage

| Alternative | For | Against, and why it was not chosen |
| --- | --- | --- |
| One virtual machine with SQLite and a disk | One part to run; a disk for a git repository | One machine to patch, a restart on every upgrade, and data lost if the disk fails between snapshots. |
| No database: conversations kept in the browser, records in logs | One part fewer | The monthly budget needs a sum that survives a restart, and a conversation would not follow the user from desktop to phone. |
| A document database | No always-on database charge | The budget is a sum over a month of records, which Postgres does in one query. |
| The `pgx` Postgres driver | The driver most Go projects now choose | Its releases from v5.9.0 require Go 1.25. The last release for Go 1.24, v5.8.0, adds 14 modules that each need a licence inventory row. `lib/pq` adds one, is MIT-licensed, and needs Go 1.21. |

### Decision 5: how slow work runs

| Alternative | For | Against, and why it was not chosen |
| --- | --- | --- |
| Store an "in progress" state so a second user waits for the first | No duplicate fetch when two users open one commit | If the first request dies, the state stays and nothing clears it. Repairing that needs an owner, an expiry and a takeover rule. A duplicate fetch costs seconds and is rare at ten users. |
| A job queue with a worker | Work continues after the request ends | A queue, a worker and a credential that outlives the request. |
| Prepare every commit on push | Nothing to wait for on open | A standing credential, and work for commits nobody opens. How many commits go unopened has not been measured. |

### Decision 6: how the assistant gets code

| Alternative | For | Against, and why it was not chosen |
| --- | --- | --- |
| Embedding retrieval | One model call per question | An embedding call for every piece of code, a vector store, and more content sent to the provider. It does not answer "what calls this", which the index can. |
| Context assembled by fixed rules | One model call, and an exact record of what was sent | It cannot follow a chain beyond what the rules fetch. |

### Decision 7: which provider receives content

| Alternative | For | Against, and why it was not chosen |
| --- | --- | --- |
| A second provider when quota runs out | AI features keep working through an outage | A repository's code would go to a second vendor. The design owner allows only the configured provider. |

### Decision 8: where secrets are scanned

| Alternative | For | Against, and why it was not chosen |
| --- | --- | --- |
| Scan each file when it is first read, and look results up at send time | Questions never wait for a scan | It does not cover text that is not a file, such as a question or a description, so a second path would still scan at send time. |
| Honour the repository's own scanner allowlist | Fixture credentials do not block AI features | A pull request could add an entry that clears its own secret. |
| An allowlist of paths kept by the operator | The operator can clear known fixtures | Results are remembered by content, so the same bytes cleared at one path would be sent from another. Left out until false positives have been counted. |

### Decisions 9 and 10: the service and the web application

| Alternative | For | Against, and why it was not chosen |
| --- | --- | --- |
| Write the service in TypeScript | One language for service and web application | It could not reuse the CLI's Go packages for GitHub calls, patches, the scanner and model access. |
| A second pair of provider adapters inside the gateway | `internal/model` and ADR-0021 stay untouched | Two adapters per provider to keep in step. |
| The providers' own client libraries | Streaming and tool calls already written | Each brings a dependency tree to inventory, which ADR-0021 already weighed and declined. |
| The Monaco editor for code and diff | The editor VS Code uses | Its README says it does not support mobile browsers ([Monaco](https://github.com/microsoft/monaco-editor)). |
| Pages rendered on the server with no client application | Less code in the browser | Navigation history, the assistant panel and streamed answers need state in the browser. |
| Draw diagrams with the Mermaid library | The user sees a picture | Its current major version depends on a package offered under EPL-2.0 or GPL-3.0, both of which this repository's licence check refuses, and it brings more than 100 packages to inventory. |

## Risks and Limits

Figures marked as estimates rest on the stated assumptions and are replaced by measurements as they arrive.

| Risk or limit | What is known | Mitigation |
| --- | --- | --- |
| Model spend | Estimate: about $300 to $1,200 a month for questions. It assumes 500 questions a week, each at the cap of 60,000 input tokens and 2,000 output tokens, at one provider's published prices of $2 to $8 per million input tokens and $10 to $40 per million output tokens ([pricing](https://platform.claude.com/docs/en/about-claude/pricing), read on 2026-11-23). Prompt caching is not assumed. The managed database has a fixed monthly charge that is not in this figure. | A cap per question and a monthly budget per repository. The model call records give the real figure from the first week. |
| The provider's own spend cap | That provider's two lowest account tiers cap spend at $500 and $1,000 a month, and at the cap requests are refused until the next calendar month. | The open question on the spend cap is answered before PR-53. Journey 6 covers the message. |
| The first open waits | The first open of a commit in a container reads two archives from GitHub. The time is unmeasured. | Entry condition E3 measures it. The stated reason and file list show before the archives arrive. |
| Memory | A container holds every cached archive and index in memory. A too-small container is stopped by the platform, and every user of it then waits for a fresh fetch. | A byte limit on the cache, on each archive and on each index. The operating guide gives the sizing rule. |
| Source is not content-checked | An archive is trusted as GitHub's answer for that commit hash. The service cannot check it against the hash as `git` would. | None. The same trust is placed in GitHub's API for the pull request itself. |
| Paths missing from an archive | A repository can mark paths `export-ignore`, and GitHub leaves them out of archives. A pull request could add that mark. | A changed file missing from the archive is shown as such, with a link to GitHub. |
| The index cannot be verified, and can be partial | Whoever pushes a commit can build a false index for it. A repository with several TypeScript projects can produce an index that covers one. | Binding to the commit's own run, size limits, the name check, the source label, and text matches beside every reference list. The diff, source and search do not depend on the index. |
| Some commits get no index | A pull request from a fork; a commit pushed by a workflow with its own token; a run whose artifact has passed GitHub's retention period, 90 days by default. | The view says there is no index, and lookups use text search. |
| CI cost | The index job installs dependencies on every push to every branch. | Entry condition E2 records the build time. The team connects one repository first. |
| Instructions hidden in code or descriptions | Such text can produce a wrong answer and use up a question's tool rounds. | The tools are read-only. The model's instructions mark such text as material. Rendered output cannot load anything from another host. An answer is seen only by the user who asked. |
| User tokens | The token travels in the session cookie on every request. A breach of the cookie key, which is in the process's memory, exposes every token presented while it was in use. A copied cookie works until it expires. The service cannot check which permissions a token carries. | The cookie is unreadable to scripts and sent only over HTTPS. The sign-in page asks for read-only permissions, and the service makes no write call. The scanner starts with no inherited environment. Nothing is stored at rest. |
| Tokens expire | A fine-grained token lasts at most 366 days, and the owner's approval can be withdrawn. | The user is asked for a new token. |
| Access removed on GitHub | It takes effect in the workspace within the cache period, not at once. | The period is set by the operator and has no default. |
| Credential approval is refused | Entry condition E1. | The App method, if that is approved. Otherwise the design stops. |
| The archive trial fails | Entry condition E3. | Files are read on demand and search waits. |
| The indexer | `scip-typescript` bundles its own TypeScript 6. A repository on a different major version, or one managed with `bun`, may not index. | Entry condition E2 records both. If it fails, navigation leaves the first version. |
| Secrets false positives | A fixture credential in a file keeps that file from the model. How often this happens is unknown. | None in the first version. The withheld-file message tells the user what happened, and the count is read from model call records. |
| The scanner's reach | It recognises known credential formats, in plain text and in base64. It does not recognise an arbitrary password or internal data. | Stated in Goal 8 and in the operating guide. Only the configured provider receives content. |
| Conversations hold code | A stored conversation contains the excerpts the model was given. | A retention period with no default, applied whenever a new conversation is created. Only the user who started a conversation can read it. |
| Measures are the operator's alone | The logs record which user opened which pull request. Only the operator can read them, and no user can see the measures. | Telemetry views for every user are part of the next step. |
| AI findings depend on the CLI | Findings on changed lines exist only in repositories where the CLI's model lane is on. | The open question on existing installs. |
| One operator | The service is run by one person for ten users. | It writes nothing to GitHub, so review continues on GitHub when it is down. |

## Non-Functional Requirements

### Security and privacy

*   The service starts one program, the secrets scanner, with no inherited environment, over text the service wrote itself.
*   Archives and index artifacts are read with size limits. Index entries that do not match the commit's archive are dropped.
*   No token is stored. The cookie key and the provider keys arrive through the environment, and the values never appear in logs.
*   Access is checked before every read of repository data, with the cache period stated in Goal 6.
*   Only the configured provider receives a repository's content, and only after the scan described under the model gateway.
*   The content security policy is sent as a response header.

### Performance

| Measure | Target |
| --- | --- |
| A text search at a commit, on the largest connected repository | Under 1 second (proposed). Measured in PR-50. |
| A definition or reference lookup with the index in memory | Under 200 milliseconds (proposed). Measured in PR-52. |
| Fetching and unpacking one archive | Under 60 seconds (proposed). Measured in entry condition E3. |
| Time to the first words of an AI answer | Not yet set. |

Navigation and search make no model call. Token use is bounded per question by limits on tool rounds and tokens, and per repository by its monthly budget.

### Rollout and rollback

*   **One repository first.** The index workflow is added to one repository, and the others follow after the navigation feedback week.
*   **What is added to a repository:** one workflow file, and two sections in the agent skill file. The workflow adds a CI job to every push. The skill sections change what agents do, as described under "The CI side".
*   **Rollback** is deleting that workflow file and reverting the skill file.
*   **The service can be switched off** without affecting GitHub, because it writes nothing there.
*   **Database changes** are versioned migrations applied at deploy time. A restore from backup is tested before the assistant's feedback week.

## Additional Resources

*   [`concepts_brief.md`](../../concepts_brief.md), and the review reports under [`reviews/`](reviews/).
*   [`docs/architecture.md`](../architecture.md), [`docs/adr/`](../adr/) and [`docs/providers.md`](../providers.md): the existing CLI.
*   Bacchelli and Bird, "Expectations, Outcomes, and Challenges of Modern Code Review", ICSE 2013: <https://2013.icse-conferences.org/content/expectations-outcomes-and-challenges-modern-code-review.html>
*   GitHub documentation consulted: [managing personal access tokens](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens), [token policy for an organisation](https://docs.github.com/en/organizations/managing-programmatic-access-to-your-organization/setting-a-personal-access-token-policy-for-your-organization), [repository archives](https://docs.github.com/en/rest/repos/contents), [workflow runs](https://docs.github.com/en/rest/actions/workflow-runs), [workflow artifacts](https://docs.github.com/en/rest/actions/artifacts), [comparing two commits](https://docs.github.com/en/rest/commits/commits), [pull request files](https://docs.github.com/en/rest/pulls/pulls), [events that trigger workflows](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows).
*   `scip-typescript`: <https://github.com/sourcegraph/scip-typescript>. The SCIP format's schema: <https://github.com/scip-code/scip/blob/v0.10.0/scip.proto>.
*   The secrets scanner's options: <https://github.com/gitleaks/gitleaks>.

## Future Improvements

*   **The next step and the later phases** listed under Non-Goals.
*   **Branch views**, if the open question is answered yes.
*   **Rendered diagrams**, with a renderer whose licences pass this repository's check.
*   **A scanner allowlist read from the base branch**, if false positives prove common.
*   **Prompt caching**, once the model call records show what it would save.
*   **Answers that survive a dropped connection**, if phone use shows the need.
*   **Routing the CLI's model lane through the gateway**, if any repository has that lane on.

--------------------------------------------------------------------------------

## Detailed Implementation Plan

**Gates.** PR-47 does not start until the design owner has set the thresholds named under Open Questions. PR-47's workflow template is the file that passed entry condition E2. PR-48 does not start until E1 and E3 are answered. The results of all three are written into this document first. The detail below assumes all three pass.

The work is eight of this repository's own pull requests, numbered on from PR-46 in `docs/implementation-plan.md`. PR-47 changes only the CLI's installer. PR-48 to PR-54 build the service.

| PR | Adds | ADRs | Afterwards |
| --- | --- | --- | --- |
| PR-47 | Index workflow template; two sections in the agent skill template | 0035 | In a repository where the files are installed, a push produces an index artifact and agents write intent records |
| PR-48 | Service skeleton: binary, configuration, database migrations, logging, health address, container image, CI job | 0029, 0030 | The operator can deploy the service and read its health address |
| PR-49 | Identity (token method), session cookie, access check | 0033 | Nothing a user can see; the API accepts a sign-in |
| PR-50 | Source: archive fetch, memory cache, files, text search; pull request list and view | 0031 | Nothing a user can see |
| PR-51 | Web application: sign-in, pull request list, stated reason, diff and new-code views, code view, search, GitHub links, shortcuts, touch sheet | None | **A user can sign in, read a diff and search a commit, beside GitHub** |
| PR-52 | Navigation: index lookup, download, decoder, lookups, "this was wrong" | None | **A user can navigate TypeScript, in repositories that have the index workflow, for commits whose run produced an index** |
| PR-53 | Model gateway: scan at send, conversation interface on two adapters, budget, model call records | 0032, 0034 | Nothing a user can see |
| PR-54 | Assistant: tool loop, streaming, assistant panel | None | **A user can ask questions about code** |

**What each pull request must pass.** `make check`, as today. From PR-48, also the `workspace` CI job, which runs the database tests against a real Postgres. From PR-51, also the `web` CI job. `make check` alone does not run the database tests, which skip without a database, or the web tests, which need Node. That is why the two jobs are added to the `required` job.

**Deploying.** After each of PR-48 to PR-54 merges, the operator builds the image from that commit with the Dockerfile and deploys it. No image is published. The operating guide is created in PR-48 and extended by each later pull request, so it matches what is deployed.

**Feedback weeks.** After PR-52 and after PR-54 the team uses the workspace for one working week. The measures, and where each comes from:

| Measure | Source |
| --- | --- |
| Engineers who opened at least one pull request view | Request logs: distinct users on the pull request view route |
| Pull request views | Request logs |
| Lookups per pull request view, and the share under each label | Request logs: the lookup routes, with the label field |
| Questions per pull request view (second week) | Request logs: the message route |
| "This was wrong" reports | The `navigation_report` table |
| Whether it helped | Two questions the design owner puts to the team in its usual chat |

Work continues only if the thresholds are met. Otherwise it stops and this document is revised.

**Outcome measure for the whole version.** Median time from a pull request being opened to its first human review, over the four weeks before the first feedback week and the four weeks after the second. The operator takes it once each time with a query against GitHub's API. It is not code in this repository. It is read beside the share of reviewed pull requests that were opened in the workspace.

### Shared definitions

These are fixed here because more than one pull request depends on them.

#### Service configuration

A YAML file named by `--config`, plus environment variables for every secret. No host, organisation or model name is compiled in.

```yaml
listen: ":8080"
publicUrl: ""
github:
  apiBaseUrl: ""
  webBaseUrl: ""
indexWorkflowFile: "review-index.yml"
indexArtifactName: "review-index"
sessionDays: 0
accessCacheSeconds: 0
limits:
  archiveBytes: 0      # kept text of one commit
  fileBytes: 0         # one file's text
  indexBytes: 0        # one decoded index
  cacheBytes: 0        # all archives and indexes held by one container
  fetchSeconds: 0      # one archive or artifact fetch
  toolRounds: 0        # tool calls in one question
  questionTokens: 0    # input tokens in one question
retention:
  conversationDays: 0
repositories:
  - owner: ""
    name: ""
    provider: ""       # anthropic or gemini
    model: ""
    baseUrl: ""
    apiKeyEnv: ""      # the NAME of the environment variable holding the key
    monthlyTokenBudget: 0
```

Environment variables: `WORKSPACE_DATABASE_URL`; `WORKSPACE_COOKIE_KEY`, 32 random bytes in base64; and one variable per repository's `apiKeyEnv`.

A zero in `sessionDays`, `accessCacheSeconds`, `limits`, `retention` or `monthlyTokenBudget` is a configuration error, so the service chooses no value that costs money, bounds memory or sets retention. The two index names above are the values the workflow template uses, and a test keeps the example configuration equal to the template. Three limits are fixed in code because the API's shape depends on them: 200 search matches, 500 references, and a 15-second interval for asking about a running index job.

#### Index workflow

`.github/workflows/review-index.yml`, triggered by `push` on every branch, with `permissions: contents: read`. It reuses the review workflow template's values for the Node version, the package manager cache and the install command. After the install it sets Node 22 for the indexer, runs the pinned `scip-typescript` to write `index.scip`, and uploads that one file as the artifact `review-index`.

#### Intent record

A block in the pull request description, between two comment markers that GitHub does not display:

```text
<!-- review-intent v1 -->
**Goal:** one or two sentences
**Plan:** the steps taken
**Alternatives rejected:** each with its reason
**Session:** an identifier the pull request author can use to find the agent session
<!-- /review-intent -->
```

The service reads the text between the markers and splits it at the four bold labels. A description with no markers has no intent record. Text between the markers that lacks a label leaves that field empty.

#### Conversation interface in `internal/model`

```text
Part        = Text{text}
            | Excerpt{path, sha, startLine, endLine, text}
            | ToolCall{id, name, argumentsJSON}
            | ToolResult{id, parts, isError}
Turn        = { role: user | assistant, parts }      # a tool result travels in a user turn
ToolSpec    = { name, description, schemaJSON }
ConvRequest = { model, system, turns, tools: [ToolSpec], maxTokens, timeout }
Event       = TextDelta{text}
            | ToolCall{id, name, argumentsJSON}
            | Usage{inputTokens, outputTokens}
            | End{reason: stop | tool_use | max_tokens | refused}

Converser.Converse(ctx, ConvRequest) -> (stream of Event, error)
NewConverser(provider, baseURL, apiKey) -> (Converser, error)     # provider: anthropic | gemini

Errors: ErrRateLimited{retryAfter}, ErrQuotaExhausted{resetAt, known}, and any other error
```

`Converse` returns an error before any event if the provider refuses the request. An error after events have started arrives as the stream's final error. The adapters do not retry.

#### Gateway interface

```text
Call    = { owner, repo, sha, userLogin, system, turns, tools }
Result  = { outcome, withheld: [{ what, reason }], inputTokens, outputTokens, modelCallID,
            retryAfterSeconds, resetAt }
Outcome = ok | rate_limited | quota_exhausted | budget_blocked | secret_blocked | scan_failed
        | refused | max_tokens | unavailable

Gateway.Ask(ctx, Call, onEvent func(model.Event)) -> Result
```

`Ask` scans, checks the budget, calls the provider, passes each event to `onEvent`, writes one `model_call` row, and returns. It returns a `Result` in every case and never an error. `withheld` lists what the scan held back when the call still went ahead. `secret_blocked` means the call did not go ahead, and `withheld` then names what stopped it.

#### Database

One migration, `0001_init.sql`, creates all four tables in PR-48. Timestamps are `timestamptz`. `owner` and `repo` are the GitHub owner and repository name.

| Table | Columns |
| --- | --- |
| `conversation` | `id uuid` primary key; `owner text`; `repo text`; `pull_number int`; `sha text`; `user_login text`; `created_at`. Index on `(owner, repo, user_login)`. |
| `message` | `id bigserial` primary key; `conversation_id uuid` references `conversation` on delete cascade; `seq int`; `role text`, one of `user`, `assistant`; `parts jsonb`, the turn's parts as in the conversation interface; `model_call_id bigint` null; `created_at`. Unique on `(conversation_id, seq)`. |
| `model_call` | `id bigserial` primary key; `owner text`; `repo text`; `sha text`; `user_login text`; `provider text`; `model text`; `outcome text`, one of the gateway outcomes; `scan text`, one of `clean`, `withheld`, `blocked`, `failed`; `withheld jsonb`; `input_tokens int`; `output_tokens int`; `started_at`; `duration_ms int`. Index on `(owner, repo, started_at)`. |
| `navigation_report` | `id bigserial` primary key; `owner text`; `repo text`; `sha text`; `path text`; `line int`; `character int`; `kind text`, one of `definition`, `references`; `label text`; `note text`; `user_login text`; `created_at`. |

The monthly budget check sums `input_tokens + output_tokens` in `model_call` for the repository since the first day of the current month, in UTC. When a conversation is created, conversations older than `retention.conversationDays` are deleted in the same transaction. `model_call` and `navigation_report` hold no code and are kept.

#### HTTP API

The Go package `internal/workspace/api/apitypes` defines every request and response body as a struct. `tools/gentypes` writes `web/src/api/types.gen.ts` from those structs. A Go test regenerates the file in memory and fails if it differs from the committed one, so the two sides cannot disagree about a field without a test failing. Nothing checks the web application against a running service; the journeys run against a fake API built from the same generated types.

```text
Error           = { error: { code, message } }
Me              = { login, repositories: [{ owner, name }] }
PullSummary     = { number, title, author, draft, headSha, updatedAt }
Intent          = { goal, plan, alternatives, session }
ChangedFile     = { path, previousPath, status, ranges: [[start, end]], noPatch }
                  # status: added | modified | deleted | renamed. ranges: 1-based inclusive lines in the new file
PullView        = { number, title, body, author, state, draft, headSha, baseRef, mergeBaseSha,
                    intent: Intent | null, files: [ChangedFile], webUrl }
Tree            = { entries: [{ path, kind }] }           # kind: text | binary | too_large | other (a link or submodule)
File            = { path, kind, text }
Match           = { path, line, text }
SearchResult    = { matches: [Match], truncated }
IndexState      = { state, runUrl, toolVersion, documents }   # state: none | running | failed | ready
Location        = { path, line, character, endLine, endCharacter }
Definition      = { label, targets: [Location], package, version, matches: [Match] }
                  # label: index | text_search | external | none
References      = { label, references: [Location], truncated, matches: [Match] }
                  # label: index | text_search | none
Report          = { sha, path, line, character, kind, label, note }
Excerpt         = { path, sha, startLine, endLine }
Message         = { role, text, excerpts: [Excerpt], withheld: [{ what, reason }], model, outcome, createdAt }
Conversation    = { id, sha, pullNumber, messages: [Message] }
Ask             = { text, selection: { path, startLine, endLine } | null }
```

| Method and path | Request | Response |
| --- | --- | --- |
| `POST /auth/token` | `{ token }` | 204 and the cookie; or 401 with code `not_fine_grained`, `rejected` or `no_repository` |
| `POST /auth/signout` | | 204, clearing the cookie |
| `GET /api/v1/me` | | `Me` |
| `GET /api/v1/repos/{owner}/{repo}/pulls` | | `{ pulls: [PullSummary] }` |
| `GET /api/v1/repos/{owner}/{repo}/pulls/{number}` | | `PullView` |
| `GET /api/v1/repos/{owner}/{repo}/commits/{sha}/tree` | | `Tree`. Fetches the archive if it is not in memory |
| `GET /api/v1/repos/{owner}/{repo}/commits/{sha}/file?path=` | | `File` |
| `GET /api/v1/repos/{owner}/{repo}/commits/{sha}/search?q=&path=&case=` | | `SearchResult`. `q` is at least 2 characters |
| `GET /api/v1/repos/{owner}/{repo}/commits/{sha}/index` | | `IndexState`. Downloads and decodes the index if the run succeeded and it is not in memory |
| `GET /api/v1/repos/{owner}/{repo}/commits/{sha}/definition?path=&line=&character=` | | `Definition` |
| `GET /api/v1/repos/{owner}/{repo}/commits/{sha}/references?path=&line=&character=` | | `References` |
| `POST /api/v1/repos/{owner}/{repo}/navigation/reports` | `Report` | 204 |
| `POST /api/v1/repos/{owner}/{repo}/conversations` | `{ sha, pullNumber }` | `{ id }` |
| `GET /api/v1/repos/{owner}/{repo}/conversations/{id}` | | `Conversation`, for the user who started it; 404 for anyone else |
| `POST /api/v1/repos/{owner}/{repo}/conversations/{id}/messages` | `Ask` | An event stream, described below |
| `GET /healthz` | | 200 when the database answers, otherwise 503 |

Rules that apply to every route:

*   Every route under `/api/v1/repos/{owner}/{repo}` passes the access check before anything is read. A user without access gets 404 with code `not_found`, the same as for a repository that is not connected.
*   Other errors: 400 `bad_request`; 401 `signed_out`; 413 `too_large`, when an archive or index passes its limit; 502 `github_failed`; 504 `github_timeout`. Every error body is an `Error`.
*   A request that changes state must carry the header `X-Workspace-Request: 1`, and the cookie is `SameSite=Lax`, `Secure` and `HttpOnly`.
*   `line` counts from 0. `character` counts UTF-16 code units from 0, which is the unit the browser's editor and the index both use.

The message route answers with `text/event-stream`. Each event has a name and one JSON object:

| Event | Data | Meaning |
| --- | --- | --- |
| `delta` | `{ text }` | More of the answer |
| `excerpt` | `Excerpt` | Lines the model was given |
| `withheld` | `{ what, reason }` | Content held back from the model |
| `done` | `{ model, inputTokens, outputTokens }` | The answer is complete |
| `stopped` | `{ outcome, message, retryAfterSeconds, resetAt }` | The answer ended for the reason given; `outcome` is a gateway outcome other than `ok` |

Exactly one of `done` and `stopped` ends every stream. If the connection drops, the browser reads the conversation back.

#### Reading the index

`internal/workspace/navigation/scip` decodes the protobuf wire format with the standard library and reads only the fields below. The numbers are from the schema at release v0.10.0, read on 2026-11-23. Unknown fields are skipped.

| Message | Fields read |
| --- | --- |
| `Index` | 1 `metadata`; 2 `documents` |
| `Metadata` | 2 `tool_info`; 3 `project_root` |
| `ToolInfo` | 1 `name`; 2 `version` |
| `Document` | 1 `relative_path`; 2 `occurrences`; 6 `position_encoding`, where 2 means UTF-16 |
| `Occurrence` | 2 `symbol`; 3 `symbol_roles`, where bit 1 marks a definition; and the range, in any of three forms: 8 `single_line_range` (`line`, `start_character`, `end_character`); 9 `multi_line_range` (`start_line`, `start_character`, `end_line`, `end_character`); or 1 `range`, a list of three or four integers in those same orders. Form 8 or 9 wins if form 1 is also present |

A symbol identifier is either `local` followed by a number, which is scoped to its document, or a scheme, a package manager, a package name, a version and a chain of name parts, separated by spaces. "The name at the end of the symbol's identifier" is the last name part with its trailing punctuation removed. "The symbol's identifier names a package" means the package name is not `.`, the format's placeholder for none.

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

Every package or module a pull request adds is listed in `THIRD_PARTY_LICENSES.md` in that pull request, as ADR-0028 and `make licenses` require.

| Dependency | Added in | Used for | Licence | Inventory |
| --- | --- | --- | --- | --- |
| `github.com/lib/pq` v1.12.3, through the standard `database/sql` | PR-48 | Postgres | MIT | One row under "Go modules". It has no dependencies of its own and declares Go 1.21, so `go.mod` stays at Go 1.24 |
| `react`, `react-dom` | PR-51 | Web application | MIT | "Web application packages": these two and `scheduler` |
| `@codemirror/state`, `@codemirror/view`, `@codemirror/merge`, `@codemirror/language`, `@codemirror/lang-javascript` | PR-51 | Code and diff display. Files in other languages are shown without colouring | MIT | The same section: these five and the 11 packages they bring, all MIT |
| `markdown-it` | PR-54 | Drawing answers, with raw HTML off | MIT | The same section: it and the 6 packages it brings. Five are MIT or BSD-2-Clause, and `argparse` is PSF-2.0 |
| `typescript`, `vite`, `@vitejs/plugin-react`, `vitest`, `jsdom`, `@testing-library/react`, `eslint`, `typescript-eslint`, `@types/react`, `@types/react-dom`, `@playwright/test` | PR-51 | Build, type-check, lint and test only | MIT, Apache-2.0 | "Web application development dependencies", direct ones only |
| `@types/markdown-it` | PR-54 | Type-check only | MIT | The same section |

These lists were resolved on 2026-11-23 against the package registry: 26 distributed packages in all, none under a licence the check refuses. One fact about the development tools is recorded so it is not a surprise later. The build tool brings `lightningcss`, which is under MPL-2.0. It is not distributed, and ADR-0028 inventories only direct development dependencies, so the check does not look at it.

The web application's routing uses the browser's history API directly. The index decoder, the HTTP server, routing, logging, migrations, the cookie's encryption and archive reading use the Go standard library. `gitleaks` is a program in the container image and is already listed under "External binaries". If a later resolution brings a package the licence check refuses, the pull request stops and this table is revised before work continues.

### Target Files to Create/Modify

Each table lists every file its pull request touches. A pull request that adds a route modifies `internal/workspace/api/router.go` and `internal/workspace/api/apitypes/types.go`, adds a case to `internal/workspace/api/api_test.go`, and regenerates `web/src/api/types.gen.ts` once that file exists; those four are listed where they first appear and not repeated. `docs/adr/README.md` is modified by every pull request that adds an ADR.

#### 1. PR-47: index workflow and agent skill sections

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/installer/templates/review-index.yml.tmpl` | Create | The index workflow, as defined above, with the `scip-typescript` version that passed E2. |
| `internal/installer/plan.go` | Modify | Add `.github/workflows/review-index.yml` to the plan when `HasPackageJSON` is true and `TSConfig` is set, the condition already used for the TypeScript plugin. A new file, so `reviewer init` on an existing install adds it and leaves the others alone. |
| `internal/installer/write.go` | Modify | Embed and render the new template with the values the review workflow template already receives. |
| `internal/installer/plan_test.go`, `write_test.go` | Modify | The file counts rise by one where TypeScript is detected and are unchanged where it is not. Assert the review workflow and config are rendered as before. |
| `cmd/reviewer/main_test.go` | Modify | Its TypeScript fixture now expects "8 files to create" and "8 files created". |
| `examples/review-index.yml` | Create | The rendered workflow, for copying by hand before a release carries the template. A test in `write_test.go` keeps it equal to the rendered template. |
| `internal/installer/templates/SKILL.md.tmpl`, `internal/installer/skill_test.go` | Modify | Two sections: write the intent record in the form above when opening a pull request; and, only when the pull request author asks, read open review threads, fix, run the review, push and reply without resolving. The test asserts both sections and the words "only when". |
| `docs/providers.md` | Modify | Its section on the agent skill describes the two new behaviours. |
| `docs/adr/0035-scip-index-built-on-push.md`, `docs/adr/README.md` | Create, Modify | As described under "Changes to recorded decisions". |
| `docs/implementation-plan.md` | Modify | Append a pointer to this design for PR-47 to PR-54. |
| `CHANGELOG.md` | Modify | Note the new workflow file and skill sections. |

#### 2. PR-48: service skeleton

| File | Action | What changes and why |
| --- | --- | --- |
| `cmd/workspace/main.go`, `serve.go`, `main_test.go` | Create | Entry point with subcommands `serve`, `migrate` and `version`. A separate binary keeps the CLI free of database and web code. |
| `internal/workspace/wsconfig/config.go`, `config_test.go` | Create | The configuration above, a secrets type that prints only whether each value is set, and validation that reports every problem at once. |
| `internal/workspace/store/store.go`, `migrate.go`, `migrate_test.go`, `migrations/0001_init.sql` | Create | A `database/sql` pool, and a runner that applies embedded, numbered SQL files once each. The four tables above. |
| `internal/workspace/obs/log.go`, `log_test.go` | Create | The structured request log described under "Logs, measures and health". |
| `internal/workspace/api/router.go`, `middleware.go`, `api_test.go`, `apitypes/types.go` | Create | Route table, request identifier, logging, panic recovery, the error body. Starts with `/healthz`. |
| `internal/workspace/doc.go`, `rules_test.go` | Create | A test over the service's own source: it fails if any package under `internal/workspace` or `cmd/workspace` imports `os/exec`, or calls one of the functions in `internal/diff` that run `git`. |
| `internal/workspace/testdb/testdb.go` | Create | Connects to `WORKSPACE_TEST_DATABASE_URL`. Skips when it is unset, and fails when `WORKSPACE_REQUIRE_DATABASE` is set. This variable is new, so the existing test matrix and release job, which have no database, are unaffected. |
| `Dockerfile`, `.dockerignore` | Create | Builds the Go binary on the official Go 1.24 image and copies it onto a distroless static base image. Both images are pinned by digest. |
| `examples/workspace.config.yaml` | Create | An example configuration with placeholder values. |
| `go.mod`, `go.sum`, `THIRD_PARTY_LICENSES.md` | Modify | Add `github.com/lib/pq` and its inventory row. |
| `Makefile` | Modify | A `workspace-test` target that runs the tests under `internal/workspace` and `cmd/workspace` with the database required. It is not part of `check`. |
| `.gitignore` | Modify | Ignore the `workspace` binary. |
| `.github/workflows/ci.yml` | Modify | A `workspace` job on Linux with a Postgres service container, running `make workspace-test` and `docker build`. Added to the `required` job's list. |
| `docs/architecture.md` | Modify | Describe the second binary and the new CI job, and raise its count of CI jobs from seven to eight. |
| `docs/workspace.md` | Create | Operating guide: configuration, the cloud resources needed, the rule for sizing memory, deploying, backup and restore. It names no project-specific value. |
| `docs/adr/0029-add-a-hosted-workspace-service.md`, `0030-service-store-beside-github.md` | Create | As described under "Changes to recorded decisions". |
| `docs/adr/0008-cli-not-service.md`, `0016-github-comments-as-the-store.md` | Modify | Metadata only: status `Superseded`, and the superseding ADR. |

#### 3. PR-49: identity and access

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/workspace/identity/identity.go`, `token.go`, `cookie.go`, `identity_test.go` | Create | The interface; the token method with its three sign-in checks; sealing and opening the cookie with AES-256-GCM from the standard library. Tests: a classic token is refused; a token that reads no connected repository is refused; a cookie sealed with another key is refused; an expired cookie is refused. |
| `internal/workspace/access/access.go`, `middleware.go`, `access_test.go` | Create | `CanRead`, cached per user and repository in memory for `accessCacheSeconds`. A repository GitHub reports as public is refused for everyone and logged as a configuration error. The same response for "no access" and "not connected". |
| `internal/workspace/api/auth.go` | Create | `POST /auth/token`, `POST /auth/signout`, `GET /api/v1/me`. |
| `internal/workspace/api/middleware.go` | Modify | Open the cookie, build the request's GitHub client, and require the custom header on requests that change state. |
| `internal/ghclient/client.go`, `client_test.go` | Modify | Add `GetRepo`, returning whether the repository is private, as a method on `REST`. Add description, author, creation time and web address to `PullRequest`. The `Client` interface is not changed, so the fakes in `internal/poller` and `internal/reporters` still compile. |
| `internal/model/redact.go`, `internal/model/provider_test.go` | Modify | Add the GitHub token prefixes `github_pat_`, `gho_`, `ghu_`, `ghs_` and `ghr_` to the credential pattern. |
| `docs/workspace.md`, `SECURITY.md` | Modify | How users create a token and sign in; what the cookie holds; that no token is stored. |
| `docs/adr/0033-identity-behind-one-interface.md` | Create | |

#### 4. PR-50: source and pull requests

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/ghclient/client.go`, `client_test.go` | Modify | Add `Compare`, returning the merge base of two commits; `Archive`, returning a commit's tar archive as a stream; and the previous file name on `ChangedFile`. `Archive` uses a new helper for responses that are not JSON, because the existing one decodes JSON and caps the body at 32 MB. |
| `internal/diff/diff.go`, `diff_test.go` | Modify | `PatchedFile` gains an old path, and `FromPatches` copies it to `File.OldPath`, so a rename can be read at its old path. |
| `internal/workspace/source/archive.go`, `cache.go`, `search.go`, `source_test.go` | Create | Unpack an archive stream into memory under the limits; the shared byte-bounded cache with one fetch per commit at a time within a container; literal search. Tests build archives in memory: text, binary, NUL-bearing, oversized and symlink entries; an archive over the limit; two concurrent requests making one fetch; a search match in a file the pull request does not change. |
| `internal/workspace/catalog/pulls.go`, `intent.go`, `catalog_test.go` | Create | List and view pull requests as the user; the merge base; changed files through `ListFiles` and `FromPatches`; parsing the intent record. |
| `internal/workspace/api/pulls.go`, `source.go` | Create | Handlers for the pull request list and view, tree, file and search. |
| `internal/workspace/api/api_test.go` | Modify | The access test: every route under a repository answers 404 for a user without access. The test reads the route table, so later routes are covered without editing it. |
| `docs/workspace.md` | Modify | The archive and cache limits, and the measured search time on the largest connected repository. |
| `docs/adr/0031-read-source-as-the-user.md` | Create | |

#### 5. PR-51: web application

| File | Action | What changes and why |
| --- | --- | --- |
| `web/package.json`, `package-lock.json`, `tsconfig.json`, `vite.config.ts`, `eslint.config.js`, `playwright.config.ts`, `index.html` | Create | Build, type-check, lint and test setup with the packages listed under Dependencies. Vite builds into `web/dist/`. |
| `tools/gentypes/main.go`, `main_test.go` | Create | Writes `web/src/api/types.gen.ts` from the structs in `apitypes`. The test fails when the committed file differs from what the tool would write. |
| `web/src/api/types.gen.ts`, `web/src/api/client.ts` | Create | The generated types, and typed calls with one place that handles "signed out" and "not found". |
| `web/src/main.tsx`, `App.tsx`, `router.ts` | Create | Start-up, and routing on the browser's history API for the four views. |
| `web/src/views/SignIn.tsx`, `PullList.tsx`, `PullView.tsx`, `CodeView.tsx` | Create | The four views. Sign-in lists the four read permissions to grant and explains each refusal. |
| `web/src/components/StatedReason.tsx`, `CodePane.tsx`, `DiffView.tsx`, `SearchPanel.tsx`, `ActionSheet.tsx`, `CommandPalette.tsx`, `ShortcutHelp.tsx` | Create | The stated reason, code and diff display, search, the touch sheet, the palette and the shortcut list. |
| `web/src/keys/keymap.ts`, `useHotkeys.ts`, `web/src/github/links.ts`, `web/src/styles/app.css` | Create | The shortcut table above, links to a file and line on GitHub, and layout for wide and narrow screens. |
| `web/src/keys/keymap.test.ts`, `web/src/github/links.test.ts`, `web/src/components/DiffView.test.tsx`, `StatedReason.test.tsx` | Create | Each action's key triggers it; no reserved key is used; single keys do not fire in a text field; links carry the right file and line; the merge base is used for removed lines; the stated reason shows with and without an intent record. |
| `web/e2e/journeys.spec.ts`, `web/e2e/fake-api.ts` | Create | CUJ 1, 9 and 11, the diff and search parts of CUJ 4, and the reading part of CUJ 8, at a desktop size and at 390 points wide. The fake API is typed with the generated types. CUJ 12 is covered by the `source` test of two concurrent requests. |
| `internal/workspace/web/web.go`, `web_test.go`, `dist/placeholder.txt` | Create | Embeds the `dist` directory beside it. The tracked placeholder lets the package compile on a checkout where the web application has not been built, and the handler serves a "not built" page when `index.html` is absent. Sets the content security policy header. |
| `.gitignore` | Modify | Ignore `web/dist/`, `web/node_modules/` and everything under `internal/workspace/web/dist/` except the placeholder. |
| `Dockerfile` | Modify | Add a stage on the official Node 22 image, pinned by digest, that builds the web application and copies it into `internal/workspace/web/dist/` before the Go build. |
| `Makefile` | Modify | `web` builds and copies into `internal/workspace/web/dist/`; `web-test` runs lint, type-check and unit tests; `web-types` runs `tools/gentypes`. None is part of `check`. |
| `.github/workflows/ci.yml` | Modify | A `web` job with Node 22: install, `make web-test`, build, install the browser, journeys. Added to `required`. |
| `tools/checklicenses/main.go`, `main_test.go` | Modify | Read `web/package-lock.json` directly, with no install: every non-development package for the section "Web application packages", and the direct development dependencies for "Web application development dependencies". Key inventory rows by section and name, so a package listed for both the plugin and the web application does not collide. The plugin's tree is read as before. |
| `THIRD_PARTY_LICENSES.md` | Modify | The two new sections. |
| `docs/architecture.md`, `docs/workspace.md`, `CONTRIBUTING.md` | Modify | The web application, how to build and test it, and the count of CI jobs from eight to nine. |

#### 6. PR-52: navigation

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/ghclient/client.go`, `client_test.go` | Modify | Add `ListWorkflowRuns` for a workflow file, head commit and event; `ListRunArtifacts`; and `DownloadArtifact`, which uses the helper from PR-50. |
| `internal/workspace/navigation/scip/decode.go`, `decode_test.go` | Create | The decoder defined under "Reading the index". Tests encode small indexes by hand in the test file, covering all three range forms, a document in another position encoding, unknown fields and truncated input. |
| `internal/workspace/navigation/fetch.go`, `load.go`, `query.go`, `navigation_test.go` | Create | Find the run and map it to the four states; download and unzip within `limits.indexBytes`; build the lookup tables; answer lookups as described under Navigation. Tests: a definition in an unchanged file; a symbol with two definitions returned as a list; a local variable; a symbol the index places in a package; an occurrence outside its file; an index whose target text does not match; a reference list beside text matches; a run for another commit refused. |
| `testdata/workspace/tsproject/` | Create | A small TypeScript project, the `index.scip` that `scip-typescript` wrote for it, and a `README.md` giving the command and tool version. The index was built with the project at `/work/tsproject`, and a test fails if the project root recorded in the index is anything else, so a rebuilt fixture cannot carry a private path. |
| `internal/workspace/store/reports.go`, `reports_test.go` | Create | Insert into `navigation_report`. |
| `internal/workspace/api/nav.go` | Create | Index state, definition, references and "this was wrong". Each lookup's label goes into the request log. |
| `web/src/nav/navigation.ts`, `navigation.test.ts`, `web/src/components/References.tsx`, `IndexStatus.tsx` | Create | Lookup calls, the rule for which commit a lookup uses, history entries, the two lists side by side, the four index states and the 15-second re-check. |
| `web/src/components/CodePane.tsx`, `ActionSheet.tsx`, `web/src/keys/keymap.ts`, `web/src/api/client.ts`, `web/e2e/journeys.spec.ts`, `fake-api.ts` | Modify | Wire navigation in; CUJ 2 and 3, and the index part of CUJ 4. |
| `docs/workspace.md` | Modify | The saved log queries for the feedback measures. |

#### 7. PR-53: model gateway

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/model/converse.go`, `anthropic.go`, `gemini.go`, `http.go` | Create, Modify | The conversation interface above. The two HTTP adapters gain request bodies with turns and tools, and a reader for each provider's streamed response. They reuse the existing header, endpoint and redaction code. `Provider` and its callers are not changed. |
| `internal/model/converse_test.go`, `testdata/converse/*.txt` | Create | For each provider, recorded examples written by hand from its published format: streamed text, a tool call, token counts, a rate limit with a retry time, and each documented form of exhausted quota. |
| `internal/analyzers/gitleaks/gitleaks.go`, `gitleaks_test.go` | Modify | Export `ScanDir(ctx, binary, dir, options)` and its report type. The options carry the rule file, the two flags and the environment. The existing analyzer calls it with today's arguments and keeps its behaviour. |
| `internal/workspace/scan/scan.go`, `rules.toml`, `scan_test.go` | Create | Writes each item as `<n>/item` in a private scratch directory, runs `ScanDir` with the embedded rule file, the inline-exemption flag, a decode depth of 2 and no inherited environment, and remembers results in memory. Tests plant the fixture credential from `testdata/tiny-ts-repo/head/src/config.ts` in each of the four ways listed under "How the scanner is run", and confirm a rule file or ignore file inside an item has no effect. |
| `internal/workspace/gateway/gateway.go`, `outcome.go`, `fake.go`, `gateway_test.go` | Create | The steps under "The model gateway" and a scripted converser for tests. Tests: a credential in typed text or a selection stops the call; in a tool result or a description it is withheld and named; a scanner failure sends nothing; the budget is checked before the call; a call for one repository reaches only that repository's adapter; every call writes one record; each error from the conversation interface maps to its outcome. |
| `internal/workspace/store/calls.go`, `calls_test.go` | Create | Insert into `model_call` and sum a month's tokens. |
| `internal/workspace/rules_test.go` | Modify | Two more rules: only `scan` may import `internal/analyzers/gitleaks`, and only `gateway` may import `internal/model`. Provider keys leave the secrets type through one method, and the test fails if any package other than `cmd/workspace` calls it. |
| `.github/scripts/install-tools.sh` | Modify | When `INSTALL_TOOLS_ONLY` names a tool, install only that one. The positional argument stays the destination directory, so both existing callers are unaffected. |
| `Dockerfile` | Modify | A build stage installs the pinned `gitleaks` with that script, and the final image receives that one binary. |
| `.github/workflows/ci.yml` | Modify | The `workspace` job installs the pinned tools before its tests, as the `test` job does. |
| `docs/providers.md`, `docs/workspace.md`, `SECURITY.md` | Modify | Which two access paths the service supports; keys and budgets; what is sent to a provider, what the scan catches and what it does not. |
| `docs/adr/0032-one-gateway-scanning-at-send.md`, `0034-conversation-interface-beside-provider.md` | Create | |
| `docs/adr/0021-six-model-paths-one-provider-interface.md` | Modify | Metadata only. |

#### 8. PR-54: assistant

| File | Action | What changes and why |
| --- | --- | --- |
| `internal/workspace/assistant/assistant.go`, `tools.go`, `prompts/system.md`, `assistant_test.go` | Create | The loop and the six tools, each bound to the conversation's repository and commit. Tests: a question with a selection reaches the model with that excerpt; a chain of tools to an answer; the round limit; a tool asked for another commit refused; a quota outcome reaching the caller; every excerpt given to the model listed with the answer. |
| `internal/workspace/store/conversations.go`, `conversations_test.go` | Create | Create a conversation, deleting expired ones in the same transaction; append and read messages; a conversation is returned only to the user who started it. |
| `internal/workspace/api/assistant.go` | Create | Start a conversation, read one back, and post a message with the event stream above. |
| `web/src/api/stream.ts`, `stream.test.ts` | Create | Reads the event stream, and reads the conversation back after a dropped connection. |
| `web/src/components/AssistantPanel.tsx`, `Markdown.tsx`, `Markdown.test.tsx` | Create | The question box with the selected code quoted above it, the streamed answer, the model's name, the excerpt list, and a distinct message for each outcome. Markdown with raw HTML and images off. Tests that a script tag and a remote image are inert and that a diagram block is shown as text. |
| `web/src/views/PullView.tsx`, `web/src/components/ActionSheet.tsx`, `web/src/keys/keymap.ts`, `web/src/api/client.ts`, `web/e2e/journeys.spec.ts`, `fake-api.ts`, `web/package.json`, `package-lock.json`, `THIRD_PARTY_LICENSES.md` | Modify | "Ask" from a selection, a line and the palette; CUJ 5, 6 and 7, and asking in CUJ 8; add `markdown-it` and its inventory rows. |
| `docs/workspace.md`, `README.md`, `CHANGELOG.md` | Modify | Conversation retention; a short description of the workspace and a link to its guide. |

### Verification Plan

*   **Compilation/Build**: `make build` for both binaries on a clean checkout; `make web`; `docker build .`.
*   **Unit Tests**: `make test`; `make workspace-test` with a database; `make web-test`; the existing `make plugin-test`.
*   **Static Analysis & Linting**: `make vet`, `make staticcheck`, `make fmt-check`, `make license-check`, `make licenses`, `make adrs`, and the web lint and type-check inside `make web-test`.
*   **Test Coverage Requirement**: each goal has the test named below. No percentage target is set.
*   **E2E/Integration**: `npm --prefix web run e2e` against the fake API.

| Goal | Test | What it shows |
| --- | --- | --- |
| 1. Open any pull request | `catalog` | Every pull request the fake GitHub lists appears, with the head commit and changed files GitHub reports |
| 2. Labelled navigation | `navigation`, `scip` | Each label is produced, and an index whose target text does not match yields text search |
| 3. Search at the commit | `source` | A match in an unchanged file is found with no index |
| 4. Ask the AI | `assistant` | A selection reaches the model; tools chain to an answer; every excerpt is listed |
| 5. Stated reason | `StatedReason.test.tsx`, CUJ 1 | Shown with and without an intent record |
| 6. Mirror GitHub access | `api`, `access` | Every route under a repository answers 404 without access; removed access is refused within the cache period; a public repository is refused |
| 7. Never execute pull request code | `internal/workspace/rules_test.go`, `scan` | No service package imports `os/exec` or calls the CLI's `git` helpers; only `scan` reaches the scanner; the scanner's environment is empty |
| 8. Scan everything sent | `gateway`, `scan` | Each kind of content with a planted credential is stopped or withheld; the four switch-off attempts fail; every call record carries a scan result |
| 9. Work without a model | `api`, `model`, CUJ 6 | With no provider reachable, diff, navigation and search answer; each quota form maps to its message |
| 10. Phone | CUJ 8 | Passes at 390 points wide |
| 11. Shortcuts | `keymap.test.ts` | Each key triggers its action; no reserved key; no single key in a text field |
| 12. Existing installs | `installer`, `cmd/reviewer` | The existing files are written as before, and the new one only with TypeScript |
| 13. Addressing comments | `installer/skill_test.go` | The section is present and conditional on being asked |
| 14. A platform the team controls | None | Not testable |

Checks to run by hand, in order:

1.  **Before PR-47 starts:** the design owner sets the thresholds. Entry condition E2 is run with a hand-written workflow file.
2.  **Before PR-48 starts:** entry conditions E1 and E3.
3.  **With PR-50:** the search time on the largest connected repository, written into the operating guide.
4.  **Before PR-53 starts:** the provider account's spend cap.
5.  **After PR-54, before its feedback week:** restore the database from a backup into an empty instance and read a conversation back.

--------------------------------------------------------------------------------

<!-- Provenance: Indicates initial structural drafting via EGM. -->

*Drafted and reviewed with the Elephant-Goldfish Model (EGM) plugin: one drafting agent that holds the full context, and reviewer agents that read the document with none.*
