# Goldfish Annotated Review Report, round 2

**Status:** proposed. Nothing in `docs/designs/review-workspace.md` has been changed yet.

**Target:** `docs/designs/review-workspace.md` (full file)

**Input:** the 19 action items in [`consolidated_review_report.md`](./consolidated_review_report.md), which trace back to 87 findings across five review stages.

## What the author has settled

On 2026-11-23 the author answered the two facts the review could not establish:

1. **Copilot.** The team is not permitted to use it. The author also states a goal the design had not recorded: the team wants a review platform it controls, so that it can shape the experience and experiment with its own improvements.
2. **GitHub's pull request navigation.** It shows some references and declarations within the same file only, is often wrong, and amounts to text search within one file.

These close entry condition E4 for GitHub's own features. The author did not report on GitHub's web editor. That check is no longer decisive, because the control goal rules out any tool the team cannot extend. The design should say so plainly: owning the platform is a goal the author has set, and it is not evidence that no existing tool navigates code.

## The shape these proposals lead to

Most of the proposals below follow the Stage 8 reviewer, who recommended removing parts in preference to adding mechanisms. Taken together they change the design in five ways.

| Area | Design under review | Proposed |
| --- | --- | --- |
| First version | Ten pull requests, with the brief, AI review, telemetry and image release all included | Navigation and asking: seven pull requests. The brief follows as the next step |
| Source | Fetched with `git`, stored in Postgres with a search index and a retention sweep | One archive per commit, fetched from GitHub with the viewer's credential and held in memory. No `git` on the service |
| Stage state | "Fetching" and "generating" stored and binding on other readers | No running state stored. A request that finds no result does the work |
| Index reader | A third-party library | A small decoder written against the published format, using the standard library |
| Stored data | Source, index, diffs, tokens, conversations, model calls, usage events | Conversations, model calls and "this was wrong" reports. The token travels in the session cookie |

## Proposals that change something the author asked for

These six need the author's attention, because each defers or narrows a stated want. They are marked ESCALATE below.

1. **The change brief moves out of the first version** and becomes the next step, with its own feedback week. The pull request description and the intent record are shown at the top of the view from the start, with no model involved.
2. **AI findings on changed lines are left to the CLI.** It already posts them on GitHub wherever its model lane is on. The workspace keeps "notes on the plan" for when the brief ships.
3. **Diagrams are shown as text** in the first version. The diagram library fails this repository's licence check and would add more than 100 packages to the hand-written inventory.
4. **Telemetry views wait.** The feedback weeks are measured from request logs, and the history figures come from the existing `reviewer metrics` command.
5. **The trial the author declined is proposed again,** as a condition for building the brief and not for the first version. Two rounds of review recommended it.
6. **A public repository cannot be connected.** Every GitHub token can read public repositories, so connecting one would let any GitHub account sign in.

## Disposition of each action item

| Action | Severity | Suggestion | Proposed edit |
| --- | --- | --- | --- |
| ACT-01. Run E4 and restate the problem | Blocker | PROPOSE FIX | Record the author's two answers. Restate the Problem as the gap that remains: no permitted AI help on pull requests, navigation limited to one file, and no platform the team controls. Add "a platform the team controls and can extend" as a goal. Replace the "not yet evaluated" row with rejections that give reasons, for GitHub's own features, the web editor and third-party hosted tools. |
| ACT-02. Size of the first version | Blocker | ESCALATE | Three steps. **Now, with no service:** the agent skill writes the intent record, and the index workflow is added to one repository. **First version:** skeleton, sign-in, source, web application, navigation, model gateway, assistant. Feedback weeks follow navigation and the assistant. **Next step:** the change brief with notes on the plan, telemetry views, the image release. |
| ACT-03. How stage work runs | Blocker | PROPOSE FIX | Delete the rule that a running stage binds other readers, and store no running states. Work runs inside the request. Results are keyed by commit, so a duplicate is harmless and a crashed attempt leaves nothing behind. State the platform's request timeout against the fetch time measured in E3. |
| ACT-04. Storage, and whether `git` is needed | Major | PROPOSE FIX | Fetch one archive per commit from GitHub as the viewer and hold it in memory, with a small cache per container. Files and text search are served from it. Changed files and line ranges come from GitHub's per-file patches through the existing `ListFiles` and `FromPatches`. The browser draws the diff from the two versions. This removes `git` from the service, three tables, the trigram index and the commit sweep. E3 confirms the archive call accepts a commit hash and measures its time; the fallback is reading files on demand, with search deferred. |
| ACT-05. Whether the workspace reviews changed lines | Major | ESCALATE | Do not call the CLI's review code from the service. Remove the provider wrapper and the Review stage. Extend the two existing provider adapters with conversation turns, tool calls, streaming and token counts, and supersede ADR-0021, whose own revisit conditions have been met. |
| ACT-06. Sign-in routes and exposure | Major | ESCALATE | E1 puts both routes to the organisation owner at once, and asks about outside collaborators and the number of organisations. Build the route that is approved. A public repository in the configuration is an error. Only fine-grained tokens are accepted. The token is sealed into the session cookie and not stored. |
| ACT-07. Diagrams | Major | ESCALATE | Show diagram source as text. Remove the diagram library and the planned change to the licence tool that existed for it. Send the content security policy as a response header. |
| ACT-08. Index claims and trigger | Major | PROPOSE FIX | Build the index on every branch push, so the default checkout is the pushed commit and a merge conflict does not block it. Find a commit's index through its workflow run, which also tells "CI still running" from "no index". Label reference results "from the CI-built index", never show an empty list as "no references", and offer text search beside it. |
| ACT-09. Measuring the feedback weeks | Major | ESCALATE | Take the counts from request logs. The author sets the thresholds before the first pull request starts. |
| ACT-10. The two Go dependencies | Blocker | PROPOSE FIX | Pin the Postgres driver at its last release that supports this repository's Go version, and list every module it adds. Read the index with a small decoder that needs no third-party module. |
| ACT-11. The implementation plan | Blocker | PROPOSE FIX | Cut the plan to the first version's seven pull requests. For those, give full column lists for the three remaining tables, and make one Go package of request and response types the source from which the web application's types are generated, with a check that fails when they differ. List every file each pull request touches, including the existing test that counts installed files. |
| ACT-12. Scanned content switching the scanner off | Major | PROPOSE FIX | Start the scanner with its inline and ignore-file exemptions off and decoding on. Write each item under a generated file name in its own directory, so no ignore file or path exemption applies and a cached result no longer depends on a path. Remove the operator allowlist. Add a planted-credential test for each case, including a description. |
| ACT-13. "Empty environment" | Major | PROPOSE FIX | With `git` gone, the scanner is the only program the service starts. Say "no inherited environment" in the goal, its test, the risks table and the security section. |
| ACT-14. Wrong or stale facts | Major | PROPOSE FIX | Correct the indexer's release state and have E2 record the version tested and each repository's TypeScript major version. Remove the false premise about reading a tree. Name which older index format is meant. |
| ACT-15. Cost and spend caps | Major | PROPOSE FIX | Give a monthly figure in money with output tokens included. Add "check the provider account's spend cap" as a condition before the gateway is built. Map each form of quota response to an outcome. |
| ACT-16. Claims and checks | Major | PROPOSE FIX | Reword the claims the reviewers list to what each mechanism delivers. Give each goal a check that tests it, and put numeric targets or "not yet set" in the requirements. |
| ACT-17. Risks and open questions | Major | PROPOSE FIX | Add the missing risks to the table, and the missing points to Open Questions. |
| ACT-18. The second skill instruction | Major | PROPOSE FIX | Keep the instruction to address review comments when the author asks, because it is the author's chosen way to close the fix loop. Give it a goal, a journey and a test, and correct "nothing else changes". |
| ACT-19. Terms and cross-references | Nit | PROPOSE FIX | One word per person and per thing, corrected counts, and the Background gaps filled. |

No action item is contested. One recommendation is not taken as written: the reviewers asked for the declined trial to run before any build, and proposal 5 above places it before the brief only.

## What would remain open after these edits

1. Entry conditions E1 to E3, which only the author can answer: credential approval, the indexer trial, and the archive and artifact trial.
2. The thresholds for the feedback weeks.
3. How long conversations are kept.

## Counts

* PROPOSE FIX: 14 action items.
* ESCALATE: 5 action items (ACT-02, 05, 06, 07, 09).
* DECLINE: none.
* CONTEST: none.

A third review round would follow these edits, because they change the architecture again.
