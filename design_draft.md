# Review workspace: code navigation and AI help beside GitHub pull requests

*   **Author:** adamtait
*   **Status:** Draft
*   **Date:** 2026-11-23

--------------------------------------------------------------------------------

## Overview

The review workspace is a web application that sits beside GitHub's pull request page. It adds two things GitHub does not have: code navigation inside a pull request (go-to-definition, find-references, search), and an AI assistant that can answer questions about the change and the code around it. Reviewers keep using GitHub to comment, approve and merge.

It serves a team of 10 engineers across 4 or more repositories. The team opens dozens of pull requests a week, and coding agents write more than 90% of them. The code is mostly TypeScript.

The system has two parts:

*   **The CI side.** The existing `reviewer` CLI already runs in each repository's GitHub Actions job. It gains two outputs per commit: a navigation index and a change brief.
*   **The workspace service.** A hosted service on a Google Cloud project stores those outputs, mirrors pull request data from GitHub, checks each user's access, and serves the web application. It never installs or executes code from a pull request.

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

*   **Identity without a GitHub App**: GitHub Apps are likely not allowed for these repositories. The service still has to know who a user is on GitHub and which repositories they can read. Which mechanism does the organisation permit?
*   **Hand-off from CI**: How does the CI job deliver the index and brief to the service, and how does the service know the delivery came from that repository's workflow, without a secret stored in the repository?
*   **Pull request events**: Without App webhooks, how does the service learn that a pull request was opened, pushed or closed, and how stale can its copy be?
*   **Index format and tool**: Which index format and indexer serve TypeScript, including repositories managed with `bun`? How long does a build take and how large is the result? This sets the "ready after push" threshold.
*   **Existing installs**: Which repositories have the CLI installed, by which trigger, and with the model lane on or off?
*   **Retention**: How long are telemetry and stored AI conversations kept?
*   **An alternative nobody has evaluated**: a browser-hosted VS Code with the GitHub Pull Requests extension might cover navigation and diff without a new viewer.

## Critical User Journeys (CUJs)

### CUJ 1: Understand a change before reading it

*   **Given**: A pull request whose CI job has finished and delivered its index and change brief.
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
*   **Then**: The agent reads the open threads from GitHub, makes the changes, pushes, and replies on each thread. The workspace shows the new commit after its CI job delivers a new index and brief.
