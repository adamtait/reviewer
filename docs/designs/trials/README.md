# Trials for the review workspace design

The design in [`../review-workspace.md`](../review-workspace.md) rests on three entry conditions, E1 to E3, and a third review round asked for four additions to them. This directory holds what is needed to run them, and the results of a rehearsal on this repository.

The trials need one of the team's repositories, a fine-grained token and an organisation owner, so the design owner runs them. They take about half a day in all.

**Record no repository, organisation, file or symbol names here.** This repository is meant to be public (ADR-0010). The tools print counts and timings only, apart from two lists marked below, which stay private.

## What is here

| File | Used in | What it does |
| --- | --- | --- |
| [`review-index.yml`](review-index.yml) | E2 | The index workflow, to copy into one repository. |
| [`scip_lookup.py`](scip_lookup.py) | E2 | Applies the design's navigation rules to a real index: for every reference, or for one position. |
| [`e3.py`](e3.py) | E3 | Makes the GitHub calls the service would make with one token, and measures fetch, unpack, memory and search. |
| [`scan_pass.py`](scan_pass.py) | E3 | Runs the pinned secrets scanner over one commit the way the design's gateway would, and counts the files it would withhold. |

The three tools need Python 3 and nothing else. `scan_pass.py` also needs the pinned scanner, which `make tools` installs.

## E1. Credential approval

Put one request to the organisation owner. Suggested wording:

> We want to try an internal code review tool on *[n]* repositories. It reads from GitHub and never writes. It needs one of the following, and we would like to know which you would approve:
>
> 1. Fine-grained personal access tokens, one per engineer, limited to those repositories, with read access to Contents, Pull requests, Metadata and Actions.
> 2. A GitHub App installed on those repositories with the same four read permissions.

Record:

* Which of the two is approved, if either.
* Whether any intended user is an outside collaborator. Outside collaborators cannot use fine-grained tokens on an organisation's repositories.
* Whether the repositories belong to more than one organisation. One token covers one owner.

## E2. The indexer

1. **Run the workflow.** Copy `review-index.yml` into `.github/workflows/` on a branch of one repository, set its install command, and push. If the team has a repository managed with `bun`, do the same there.
2. **Record from the run:** whether it completed; how long it took; whether the install needed a secret; the artifact's size; the repository's TypeScript major version; and which indexer options were needed, if any.
3. **Score every reference.** Download the artifact, check out the same commit, and run:

   ```text
   python3 scip_lookup.py stats index.scip /path/to/checkout
   ```

   Record the whole output. It names no file or symbol except in its last block, the name-check examples, which stays private.
4. **Score 50 symbols by hand.** Pick 50 uses of a symbol from changed lines of real pull requests, where the definition is in a file the pull request does not touch. For each, note where the definition really is, then:

   ```text
   python3 scip_lookup.py lookup index.scip /path/to/checkout PATH LINE COLUMN
   ```

   Then open the same pull request in GitHub's web editor in a desktop browser (press `.` on the pull request page) and use go-to-definition on the same symbol.

   Keep the sheet private. Its columns: the position; where the definition really is; the tool's label; whether its target is right, wrong or absent; whether the name check discarded a target; and whether the web editor's answer is right, wrong or absent.
5. **Record the totals:** of the 50, how many the index resolves correctly, how many wrongly, how many the name check discarded although the target was right, and how many the web editor resolves correctly and wrongly.

**Pass (proposed in the design):** the workflow completes with the repository's usual install command, at least 40 of the 50 resolve correctly, and none resolves to a wrong place.

The web editor's score is not a pass value. It is the comparison the third review round asked for, and what it should mean is the design owner's decision.

## E3. Reading as the user

1. **Create one fine-grained token** with read access to Contents, Pull requests, Metadata and Actions on the largest repository. Use the head commit of an open pull request there, after the E2 workflow has run for it.
2. **Run the calls:**

   ```text
   GITHUB_TOKEN=... python3 e3.py --repo OWNER/NAME --head FULL_COMMIT_HASH --base DEFAULT_BRANCH --search SOME_COMMON_WORD
   ```

   Record the whole output. It names no repository.
3. **Run the scanner pass** on the same commit:

   ```text
   mkdir tree && git archive FULL_COMMIT_HASH | tar -x -C tree
   python3 scan_pass.py tree
   ```

   Record the five counts. The list of paths stays private.

**Pass (proposed in the design):** every numbered step in the output of `e3.py` reports HTTP 200, the merge base is returned, and one archive takes under 60 seconds to fetch and unpack.

What the other numbers decide:

| Number | Decides |
| --- | --- |
| Both archives, one after the other | Whether the diff can wait for the archives, or needs the changed files read one at a time first. |
| Kept text bytes and peak memory | The container's memory and the cache limit. The tool is written in Python, so its peak is an upper guide and not the service's figure. |
| Search seconds | Whether a plain scan meets the one-second target. |
| Number of index runs for the commit | Whether the service needs a rule for choosing between several. |
| Files withheld under the planned settings | Whether withholding single files is worth its machinery, or the existing rule (any hit stops the send) is enough. |

## Before the model gateway: one live conversation

The third review round found that the design's conversation interface cannot carry the reasoning data that model providers expect back in a tool loop. Before PR-53 is planned again, run one real conversation with the first repository's provider and model that:

* streams its answer;
* makes two tool calls in sequence, with each result sent back;
* reports input and output token counts.

Record whether the second request is accepted when the provider's reasoning data is sent back unchanged and when it is left out, and the output token count, which replaces the 2,000-token assumption in the design's cost estimate. No script is provided for this yet, because it needs a provider key.

## Rehearsal on this repository, 2026-11-23

The tools were run in the drafting environment against this repository's own code, to check that they work and to see how the design's rules behave on a real index. This is not E2 or E3: it is not one of the team's repositories, no symbols were picked by hand, the web editor was not used, and GitHub's API cannot be reached from that environment.

### The indexer and the navigation rules

`scip-typescript` 0.6.2 indexed the TypeScript plugin under `plugins/typescript` (18 source files) in 6 seconds after `npm ci`, and wrote a 586 KB index. `scip_lookup.py stats` gave:

| Measure | Result |
| --- | --- |
| Documents | 18, all with UTF-16 positions, none dropped |
| Range form | The list form for all 5,884 occurrences; none in the typed form |
| References looked up | 4,704 |
| Labelled "from the index" | 2,507 (53.3%) |
| Labelled "outside the repository" | 1,987 (42.2%), every one naming a real dependency |
| Targets the name check discarded | 39 (0.8%) |
| No definition and no package | 171 (3.6%) |
| Positions where two or more symbols share one range | 654 |

What this shows about the design's rules:

* **Ties are common, not rare.** At 654 positions several symbols share one range. Most are a name that is both a type and a value, declared across several library files. The design's "smallest occurrence" rule does not say which to use. It needs to answer with the definitions of all of them.
* **The name check discarded only import paths.** All 39 discards were a module path in an `import` line, whose target is the file itself and has an empty range. No other correct target was discarded. The rule needs a case for a target that is a whole file.
* **"Outside the repository" was right every time here.** The index covered the whole project, so no symbol of the project's own was mislabelled. The mislabelling the review predicts needs a partial index, which a repository with several TypeScript projects would produce. E2 should include one if the team has one.
* **The unresolved 3.6%** are names imported from a dependency and used locally. Text search is the right answer for them.
* **Not exercised:** constructors, because this code has no classes.

### The scanner pass

`scan_pass.py` over this repository's 254 tracked text files took under 3 seconds. The planned settings would withhold 4 files (1.6%): three test files and one fixture, each holding a made-up key. A repository whose tests carry fixture credentials should expect the same.

### The archive tool

`e3.py --archive-file` on an export of this repository: 254 text files, 1.97 MB of kept text, unpacked in under 0.1 seconds, a search in 0.01 seconds, 29 MB peak memory. This repository is small, so these say only that the tool runs. The tool's network calls have not been run.

## Results

Fill in and commit. Counts and timings only.

```text
E1  approved method:                      [tokens | App | both | neither]
    any outside collaborator:             [yes | no]
    more than one organisation:           [yes | no]

E2  workflow completed:                   [yes | no]      minutes:
    install needed a secret:              [yes | no]
    TypeScript major version:
    index size, bytes:
    scip_lookup.py stats output:          (paste, without the last block)
    of 50 symbols, index right / wrong / discarded by the name check:
    of 50 symbols, web editor right / wrong / absent:

E3  e3.py output:                         (paste)
    scan_pass.py counts:                  (paste the first five lines)

Live conversation
    provider reasoning data must be sent back:   [yes | no | not applicable]
    output tokens for a two-tool answer:
```
