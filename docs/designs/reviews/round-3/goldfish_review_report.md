# Goldfish Annotated Review Report, round 3

**Status:** open. One action item is fixed in the design. The other 22 wait for trial results or for the design owner, so that the design is revised once.

**Target:** `docs/designs/review-workspace.md` (full file)

**Input:** the 23 action items in [`consolidated_review_report.md`](./consolidated_review_report.md), which trace back to 89 findings across five review stages.

## What the design owner decided

On 2026-11-23 the design owner accepted the drafter's recommendation for this round: fix the access-control gap now, run the trials, answer the two scope questions, and revise the document once against the results. No other edit to the design was approved.

## What was done

1. **ACT-01 is fixed in the design.** Every cache entry and fetch in flight is keyed by owner, repository and commit hash. A conversation is looked up by its identifier, repository and user. Goal 6 gains a test in which a user who can read only one of two connected repositories asks for the other's cached commit, index and conversation.
2. **The design's status line and Record of decisions now say this revision is not to be built from,** and name the three things in it known to be wrong.
3. **A trial kit is in [`../../trials/`](../../trials/README.md):** the index workflow to copy, and three tools that measure what the trials need. Each tool was run in the drafting environment against this repository's own code.
4. **This round's reports moved** from the repository root to this directory.

## What the rehearsal showed

The rehearsal is described in full in the trial kit's README. It is not a trial: it used this repository, no hand-picked symbols and no web editor, and GitHub's API cannot be reached from the drafting environment. Five results bear on this round's findings.

| Finding | What the reviewers said | What the rehearsal showed |
| --- | --- | --- |
| IR-6 (ties) | "The smallest occurrence" is not unique | Confirmed, and common: 654 positions in 18 files have two or more symbols on one range. The rule must answer with the definitions of all of them. |
| I-7 (name check) | The check can discard a correct target | It discarded 39 of 4,704 lookups, 0.8%. Every one was an import path whose target is a whole file. The rule needs that one case. |
| D-6 ("outside the repository") | The label would fire for the repository's own code | Not on a complete index: all 1,987 uses of the label named a real dependency. The fault needs a partial index. The label was the right answer for 42% of all references, which weighs against simply dropping it. |
| IR-6 (range form) | The indexer writes the older list form | Confirmed for all 5,884 occurrences. |
| I-4 (scanner hit rate) | Unknown until the feedback week | On this repository, 4 of 254 files would be withheld, 1.6%: three test files and one fixture with made-up keys. |

## Disposition of each action item

| Action | Severity | Suggestion | State, and what it waits for |
| --- | --- | --- | --- |
| ACT-01. Bind cached source to its repository | Blocker | PROPOSE FIX | **Applied.** |
| ACT-02. Count E2's 50 symbols in the web editor | Major | ESCALATE | The trial kit's E2 steps include the web editor column. Needs one of the team's pull requests. What the score should mean is the design owner's decision. |
| ACT-03. Run the trials before fixing more of the plan | Blocker | PROPOSE FIX | Kit written and rehearsed. The trials need a team repository, a token and the organisation owner. |
| ACT-04. Whether a conversation must outlive the page | Major | ESCALATE | The design owner's answer. It decides whether the first version has a database. |
| ACT-05. The agent skill sections | Major | ESCALATE | The design owner's answer, and a check of whether Claude Code loads a skill installed under `.agent/skills/`. |
| ACT-06. The team's existing agents as a way to ask | Major | PROPOSE FIX | Two alternatives rows, at the revision. Whether either changes the plan is the design owner's decision. |
| ACT-07. A supported Go release | Major | ESCALATE | The design owner's agreement, because it changes what the CLI is tested on. |
| ACT-08. The first open | Major | PROPOSE FIX | E3's timings. |
| ACT-09. Provider content in the conversation interface | Blocker | PROPOSE FIX | One live conversation, which needs a provider key. Flagged in the design now as known to be wrong. |
| ACT-10. The Google adapter | Blocker | PROPOSE FIX | The revision. Flagged in the design now as known to be wrong. |
| ACT-11. Outcomes and retries | Major | PROPOSE FIX | The revision, after the live conversation. |
| ACT-12. The assistant's tools, prompt and limits | Major | PROPOSE FIX | The revision. |
| ACT-13. Goal 7 and the import rule | Major | PROPOSE FIX | The revision. |
| ACT-14. Navigation rules | Major | PROPOSE FIX | E2's results, with the rehearsal's three findings above. |
| ACT-15. The secrets rule | Major | PROPOSE FIX | The scanner pass in E3. |
| ACT-16. Four changes to existing packages | Major | PROPOSE FIX | The revision. |
| ACT-17. Addresses, links and line conventions | Major | PROPOSE FIX | The revision. |
| ACT-18. E1's outcomes | Major | PROPOSE FIX | E1's answer. |
| ACT-19. PR-47's workflow and the gates | Major | PROPOSE FIX | E2, which produces the workflow file, and the design owner's thresholds. |
| ACT-20. Decisions, ADRs and alternatives | Major | PROPOSE FIX | The revision. |
| ACT-21. Risks, and what the operator can read | Major | PROPOSE FIX | The revision. |
| ACT-22. Statements about the existing system | Major | PROPOSE FIX | The revision. |
| ACT-23. Terms, absolutes and small facts | Nit | PROPOSE FIX | The revision, last. |

No action item is contested or declined.

## Counts

* Applied: 1.
* PROPOSE FIX, waiting: 18.
* ESCALATE: 4 (ACT-02, 04, 05, 07).
* DECLINE: none.
* CONTEST: none.

## What the design owner is asked for

1. **Run the trials** in the kit: E1, E2 with the web editor column, and E3 with the scanner pass.
2. **Answer two scope questions.** Must a conversation outlive the page? Do the agent skill sections move to their own change?
3. **Agree or decline the move to supported Go releases.**
4. **Set the thresholds** for the two feedback weeks.

A fourth review round follows the revision.
