# Goldfish Annotated Review Report, round 3

**Status:** resolved on 2026-11-23. All 23 action items are applied in the third revision of `docs/designs/review-workspace.md`.

**Target:** `docs/designs/review-workspace.md` (full file)

**Input:** the 23 action items in [`consolidated_review_report.md`](./consolidated_review_report.md), which trace back to 89 findings across five review stages.

## What the design owner decided

On 2026-11-23, in two instructions:

1.  Fix the access-control gap now, run the trials, and revise the document once against the results.
2.  The trials will not be run on a team repository. The drafter is to run every test it can, judge the entry conditions from the results, proceed on its own recommendations with reasonable thresholds, and move towards implementation.

## What was run

The tests and their results are in [`../../trials/README.md`](../../trials/README.md). In short:

*   The index workflow ran on GitHub's runners for this repository, in 57 seconds, and its artifact was found by commit.
*   The lookup rules were scored against the TypeScript compiler on 2,161 identifiers in two projects, and revised. The revised rules were right for 99.8% of 1,657 in-project definitions and wrong for none.
*   A partial index confirmed the fault the review predicted in the "outside the repository" label, and a check against the repository's own packages removed it.
*   Holding source in memory was measured on a 140 MB tree: under a second to unpack, 0.13 seconds to search.
*   The planned secrets scan was run on two trees: it would withhold 1.6% and 0.2% of files.
*   A live tool-calling conversation was run against the Gemini API on both of its conversation calls. It confirmed the Blocker: the provider refuses a tool loop that omits its reasoning data. It also showed that a finished question needs none.

Not run: the archive call with a fine-grained token, GitHub's web editor, anything on a team repository, and the Anthropic API.

## Disposition of each action item

Every item was PROPOSE FIX or ESCALATE. None was contested or declined. The escalated items were decided by the drafter on the design owner's second instruction, and each is listed in the design's Record of decisions so the design owner can overrule it.

| Action | Severity | How it was applied |
| --- | --- | --- |
| ACT-01. Bind cached source to its repository | Blocker | The cache key is owner, repository and commit. Goal 6 has a test for it. |
| ACT-02. The web editor count | Major | Could not be run: the editor reads repositories through an API the drafting environment cannot reach. The alternatives row now gives only what is known and says the comparison was not measured. The decision to build rests on the design owner's goal of a platform the team controls. |
| ACT-03. Trials before the plan | Blocker | Substitute tests were run and the entry conditions judged from them, with what would overturn each judgement. The one untested call is tested first in PR-50. |
| ACT-04. Whether a conversation outlives the page | Major | It does not. There is no database. A conversation lives in the browser page, records are log lines, and the provider's own limit bounds spend. |
| ACT-05. The agent skill sections | Major | Moved to a separate change to the CLI, described under "Related work". Both agents' documentation was read: the installer's path is not the one either lists first. |
| ACT-06. The team's agents as a way to ask | Major | Two alternatives rows added, and the problem statement now says what is true: there is no permitted way to ask from the pull request. |
| ACT-07. Supported Go releases | Major | PR-48 moves `go.mod`, the CI matrix and the image to Go 1.26 and 1.27. |
| ACT-08. The first open | Major | One instance at most, so every request finds the same memory. Unpacking measured under a second. A hand check after PR-51 times a cold open, with per-file reads as the planned response if it is slow. |
| ACT-09. Provider content in the conversation interface | Blocker | The interface has a part that carries reasoning data unread. It stays inside one question's request and never reaches the browser, because the live test showed a finished question does not need it. |
| ACT-10. The Google adapter | Blocker | The Background states the model-name rewrite. The service's adapter is new code for the provider's current conversation call and sends the configured model name unchanged. |
| ACT-11. Outcomes and retries | Major | Eight outcomes, classified by the provider's status and named wait. A rate limit and an exhausted quota arrive alike from this provider, so a five-minute line on the wait separates them. One retry rule, stated once. |
| ACT-12. Tools, prompt and limits | Major | A table of the six tools, the prompt's text, and five limits with their meanings. |
| ACT-13. Goal 7 and the import rule | Major | The goal is narrowed to what holds by construction and gains a check of the built image. The import ban on the model package is replaced by rules on who may call two functions. |
| ACT-14. Navigation rules | Major | Five rules, a symbol grammar, a rule for several runs per commit, and a state for a missing or expired artifact. The "outside" label is kept, with the own-package check, because it was the right answer for 9% to 42% of references. |
| ACT-15. The secrets rule | Major | Withholding by file is kept, with the measured rates as the reason, and the departure from ADR-0012 is stated. The scanner's default rules are used, and their built-in exemptions are named as a limit. |
| ACT-16. Changes to existing packages | Major | A second GitHub client without a timeout, a redirect followed without the token, wrapped lists read as such. The patch parser is no longer used. The scanner function takes a working directory. The licence tool passes web packages to its copyleft check. |
| ACT-17. Addresses, links and line conventions | Major | An address table, the two link forms, and one convention for every line field. |
| ACT-18. E1's outcomes | Major | Two limits of the first version are stated in place of branches. |
| ACT-19. PR-47's workflow and the gates | Major | The workflow's seven steps are listed, and a copy has run. Each feedback measure has a source and a threshold. Goal 13 says it has no check. Open Questions holds four items. |
| ACT-20. Decisions, ADRs and alternatives | Major | Eight decisions, six ADRs, and alternatives grouped to match. Types are a hand-written mirror with shared example responses. New ADRs land as `Proposed` until the feedback weeks pass. |
| ACT-21. Risks and what the operator can read | Major | Rows added. Links in answers are not clickable unless they point at the service or GitHub. Search text travels in the request body. |
| ACT-22. Statements about the existing system | Major | Corrected: four HTTP adapters and two that drive a command-line program, the watermark file, seven CI jobs. |
| ACT-23. Terms and small facts | Nit | One term for the organisation owner; "intent record" and "operating guide" defined where first used; the token pattern allows underscores. |

## Counts

*   Applied: 23.
*   Contested: none.
*   Declined: none.

## What remains open

The design's Open Questions section lists four: credential approval for an organisation's repositories, the archive call, branch views, and a paid tier for the model provider. None blocks PR-47 to PR-52.

A fourth review round has not been run. Three rounds each found about 90 points, and the third found that most of what was asked for could be met by removing things, which this revision does. The remaining risk is carried by the checks placed inside the plan: the archive test first in PR-50, the compiler comparison for the lookup rules, the image check, and the two feedback weeks.
