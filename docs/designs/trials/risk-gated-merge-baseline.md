<!-- SPDX-License-Identifier: MIT -->
# Baseline for the risk-gated merge design: pilot

This file records PR-60 of [`../risk-gated-merge.md`](../risk-gated-merge.md) as far as it can
be done without one of the team's repositories. It is a pilot: it proves the measurement runs on
real history and shows what it finds, but the repositories measured are not the team's, so none of
these numbers is the baseline G6 asks for. Rerun the steps below on a team repository and add its
results as a new section.

**Record no repository, organisation, file or symbol names here** (ADR-0010). Repositories are
letters; only counts appear.

## How it was run

On 2026-11-27, from the `risk-merge/10-baseline` branch, read-only, with a fine-grained token:

```
reviewer outcomes --repo <owner>/<repo> --since 90d --allowlist docs/
reviewer outcomes --repo <owner>/<repo> --since 90d --allowlist docs/ --format csv
```

Repositories with almost no merges in 90 days were rerun with `--since 365d`. Each run used a
throwaway `.review/config.yaml` outside any repository naming only the API base URL and the
repository. Nothing was written to GitHub.

## The repositories

Eight repositories: this one (R) and seven others owned by the same person. All eight have a
single human author and most pull requests are written by coding agents. None has a CODEOWNERS
file, and no pull request in any of them was approved by a human. That makes them useful for the
outcome labels and the docs-only count, and useless for time to first approval and the
first-review labels, which need a repository where people review each other's work.

## Results

| Repo | Window | Merged | Docs-only (strict / native) | Reverted | Hotfixed | Modified later | Not checked | Clean |
| --- | --- | --: | --: | --: | --: | --: | --: | --: |
| R | 90 days | 21 | 1 / 1 | 0 | 2 | 7 | 0 | 12 |
| A | 90 days | 94 | 6 / 6 | 0 | 12 | 33 | 1 | 48 |
| B | 90 days | 48 | 0 / 0 | 0 | 4 | 5 | 10 | 29 |
| C | 90 days | 5 | 0 / 0 | 1 | 0 | 1 | 0 | 3 |
| E | 365 days | 32 | 1 / 1 | 0 | 0 | 11 | 2 | 19 |
| F | 365 days | 47 | 0 / 0 | 0 | 6 | 1 | 0 | 40 |
| G | 365 days | 116 | 5 / 5 | 0 | 30 | 29 | 0 | 57 |
| H | 365 days | 104 | 2 / 2 | 0 | 26 | 40 | 0 | 38 |
| **All** | | **467** | **15 / 15 (3.2%)** | **1** | **80 (17%)** | **127 (27%)** | **13** | **246** |

Time to first approval: no human approval in any of the 467 pull requests, so no figure.
First-review labels: all 467 are `no-review`.

## What the pilot shows

1.  **Docs-only is a small share.** 15 of 467 merged pull requests (3.2%) touched only `docs/`,
    from 0% to 6.4% per repository. The design drops Part 2 if fewer than 5% qualify. On these
    repositories it would be dropped; the decision waits for a team repository, where docs-only
    changes may be more or less common. Either way, docs-only alone will not come near the "about
    half" of pull requests the work started from. Strict and native counts were equal in every
    repository: no docs-only pull request deleted or renamed a file.
2.  **The measurement runs on real history.** The GraphQL blame call, the merge-base comparison
    and the commit-to-pull-request lookup all returned usable answers. 13 pull requests (2.8%) were
    `not-checked`; in repository B the cause was one very large file for which GitHub sends no
    patch, which is the behaviour the design asks for instead of calling them clean.
3.  **The first runs found three defects that unit tests had missed**, now fixed in the stack:
    *   attribution worked only for squash merges (fixed with the commit-to-pull-request lookup and
        blame at the merge base);
    *   the fix-word rule matched "fixture" and any `#N` mention, labelling 9 of 21 pull requests in
        R `hotfixed` (fixed: title and branch only, word-bounded, plus linking phrases in the
        body); after the fix, R has 2;
    *   a hand-written revert titled "Revert #N; …" was labelled `hotfixed` (fixed: such a title is
        recognised as a revert); repository C now shows it as `reverted`.
4.  **`hotfixed` needs the audit before anyone relies on it.** 17% of merges are labelled
    `hotfixed`, and in G and H about a quarter. These repositories use conventional `fix(...)`
    titles for every kind of fix, so the rule counts cosmetic fixes too.

## Pre-screen of `hotfixed` (not the audit)

The design requires a person to audit 30 outcome labels and agree with 90% or better before any
label informs a merge class. That audit has not been done. As a pre-screen, the drafter read the
titles and branch names of 29 `hotfixed` pairs (every pair in A, B and C, and the first 12 in G):

| Judgement from titles and branches | Pairs |
| --- | --: |
| Plausibly a fix to what the earlier pull request introduced | 20 |
| A fix in name, not to a defect in the earlier pull request (log reordering, import sorting, lint) | 6 |
| Ambiguous (a release clean-up whose branch name contains "fixes") | 2 |
| A revert, mislabelled (since fixed) | 1 |

Titles are weak evidence: the audit must read the lines. If the audit confirms a precision near
70%, the likely change is to exclude lint and formatting fixes (titles such as `style:`, `chore:`,
"sort imports", "lint") from `hotfixed`, and keep them as `modified-later`.

## `reviewer risk`, read path

Dry runs (`reviewer risk --pr N --dry-run`) against three non-draft pull requests in R, read-only:

*   One pull request changing TypeScript files: the index run for its merge base was found,
    downloaded and decoded, and reach was computed for every changed file with removed lines. No
    label applied.
*   One infrastructure pull request: `risk/infra`, with each file named as the reason. Reach not
    available: it changes no TypeScript file that existed at the merge base.
*   One large pull request (236 files): `risk/auth`, `risk/ci`, `risk/review-config`, `risk/deps`,
    `risk/infra` and `risk/wide`, each with its reasons, and a warning that file history was read
    for only the 20 most-changed files.

In real GitHub Actions, in the scratch repository: a workflow with `contents: read`,
`pull-requests: write` and `actions: read` installed `reviewer` from the stack's tip commit and ran
`reviewer risk` on a same-repository pull request, three times:

1.  **First run.** It posted one comment and added `risk/auth` (a path the repository's own
    `risk.yaml`, read at the merge base, adds to that label) and `risk/infra` (a Terraform file),
    each with its reason. It said reach was not available, because the scratch repository has no
    index workflow.
2.  **After a commit removed the Terraform file.** It removed `risk/infra`, kept `risk/auth`, and
    updated the same comment for the new head commit.
3.  **After a person added `risk/infra` by hand and another commit was pushed.** The label stayed:
    the tool's own list held only `risk/auth`, so it did not claim the person's label.

`github.selfLogin: github-actions[bot]` was set in the configuration, which is what lets the job
recognise its own earlier comment when `GET /user` returns 403.

The design's pass bar for reach (available on at least half of 20 pull requests in a TypeScript
repository) needs a team repository with the index workflow installed.

## Sandbox questions

| Question | Status |
| --- | --- |
| Q1. Code-owner review at 0 approvals; deletes, renames and symlinks under unowned paths | Set up, not yet answered. A private scratch repository holds CODEOWNERS owning `*` with one documentation directory unowned, and seven open pull requests: an edit, a delete and a rename inside that directory, a symlink inside it pointing at owned code, an edit to an owned file, a move out of the directory into owned code, and an edit to CODEOWNERS. With no ruleset, all seven are mergeable (the control). Creating the ruleset (0 approvals, code-owner review required) needs a token with Administration: write, which the token used here does not yet have. One account is both author and code owner, and an author cannot approve their own pull request, so "blocked" will mean "needs a code owner"; "changes requested" still needs a second account. |
| Q2. An organisation ruleset with a minimum approval count | Not run: needs the team's organisation. |
| Q3. Labels with a workflow token holding only `pull-requests: write` | **Answered: yes.** In the scratch repository, a `pull_request` workflow whose only permission was `pull-requests: write` added a label that did not exist (200, label created), listed labels (200), removed it (200), removed it again (404), added and removed it once more (200, 200), deleted the label definition (204) and posted a comment (201). `GET /user` returned 403, so the job cannot learn its own login and `github.selfLogin` must be set, as PR-58 assumes. Same-repository pull requests only; a fork's token is read-only. |

## To finish PR-60 on a team repository

1.  Record today's ruleset on `main` (required approvals, code-owner review, stale-approval
    dismissal, required checks).
2.  Run the two commands above with `--since 90d` and the proposed allowlist.
3.  Audit 30 outcome labels by reading the lines, and record the agreement rate.
4.  Run Q1 to Q3 in a scratch repository the token can administer, with a second account.
5.  Add the results as a new section here, with the repository as a letter.
