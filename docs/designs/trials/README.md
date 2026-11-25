# Trials for the review workspace design

The design in [`../review-workspace.md`](../review-workspace.md) rests on three entry conditions, E1 to E3, and a third review round asked for four additions to them. This directory holds what is needed to run them, and the results of a rehearsal on this repository.

The trials as written need one of the team's repositories, a fine-grained token and an organisation owner. On 2026-11-23 the design owner decided not to run them, and asked for the entry conditions to be judged from tests the drafter could run. Those tests and judgements are in the second half of this page. The steps stay here for anyone who later connects a team repository.

**Record no repository, organisation, file or symbol names here.** This repository is meant to be public (ADR-0010). The tools print counts and timings only, apart from two lists marked below, which stay private.

## What is here

| File | Used in | What it does |
| --- | --- | --- |
| [`review-index.yml`](review-index.yml) | E2 | The index workflow, to copy into one repository. |
| [`scip_lookup.py`](scip_lookup.py) | E2 | Applies the design's navigation rules to a real index: for every reference, or for one position. |
| [`e3.py`](e3.py) | E3 | Makes the GitHub calls the service would make with one token, and measures fetch, unpack, memory and search. |
| [`scan_pass.py`](scan_pass.py) | E3 | Runs the pinned secrets scanner over one commit the way the design's gateway would, and counts the files it would withhold. |
| [`gemini_live.py`](gemini_live.py) | The model gateway | Runs one streamed, two-tool conversation against the Gemini API and reports what the provider requires. |
| [`gemini_responses.md`](gemini_responses.md) | The model gateway | The response bodies observed in those runs, for the adapter's tests. |
| [`compiler_truth.cjs`](compiler_truth.cjs), [`compare_with_compiler.py`](compare_with_compiler.py) | E2 | Ask the TypeScript compiler where a sample of identifiers is defined, and score the lookup rules against its answers. |

The tools need Python 3 and nothing else, except `compiler_truth.cjs`, which runs under Node in a project that has TypeScript installed. `scan_pass.py` also needs the pinned scanner, which `make tools` installs.

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

`gemini_live.py` does this for the Gemini API, and its results are below. No such run has been made against the Anthropic API.

## What was run on 2026-11-23

The design owner decided not to run the trials on a team repository, and asked the drafter to run every test it could and to judge the entry conditions from the results. This section records what was run, and the next one records the judgements.

Nothing here used one of the team's repositories. GitHub's API cannot be reached from the drafting environment except for this repository, through that environment's own GitHub tool.

### The index workflow on GitHub's runners

[`.github/workflows/review-index.yml`](../../../.github/workflows/review-index.yml) runs the index workflow on this repository's TypeScript plugin, for branches under `claude/`. Its first run:

| Measure | Result |
| --- | --- |
| Outcome | Succeeded, 57 seconds in all |
| Install (`npm ci`) | 32 seconds |
| Indexing | 4 seconds |
| Artifact | `review-index`, 75 KB compressed, expires after 90 days |
| Found by commit | Yes. Listing the workflow's runs returned one push run whose head commit was the pushed commit, and the artifact list for that run named the same commit. |

The artifact record also carries a SHA-256 digest of its content.

### Navigation rules, scored against the TypeScript compiler

`scip-typescript` 0.6.2 indexed two projects: this repository's TypeScript plugin (18 files), and the published source of a widely used reactive-programming library (251 files, many classes), each in under 4 seconds. A sample of identifiers in each was put to the TypeScript compiler's own go-to-definition, and the compiler's answer was compared with the answer the design's rules give from the index.

| | Plugin, 18 files | Library, 251 files |
| --- | --- | --- |
| Positions sampled | 724 | 1,437 |
| Defined inside the project, per the compiler | 432 | 1,225 |
| Rules as first stated: right / wrong / not resolved | 431 / 0 / 1 | 1,215 / 0 / 10 |
| Revised rules: right / wrong / not resolved | 431 / 0 / 1 | 1,223 / 0 / 2 |
| Defined outside the project, resolved inside by mistake | 0 | 0 |

Applied to every reference in the library's index, the rules as first stated discarded 9.7% of lookups at the name check. Almost all were import paths, whose target is a whole file, and `new Foo()` calls, whose target is the word `constructor`.

A deliberately partial index, covering 117 of the library's 251 files, tested the label "defined outside the repository". The rule as first stated gave that label to 2,170 references to the library's own code. The revised rule gave it to none of them, and resolved nothing wrongly.

The revised rules, which `scip_lookup.py` now implements beside the first ones:

* **Ties.** Every symbol that shares the smallest range at the position is used, and the answer is all their definitions. Ties are common: 654 positions in the plugin and 510 in the library.
* **Import paths.** A quoted import path may point at a whole file.
* **Constructors.** `new Foo()` may point at the constructor of the type named `Foo`.
* **Outside the repository.** A symbol is outside only if its package name matches no `package.json` in the repository.

Other facts: every document used UTF-16 positions; every occurrence used the list form of a range; a 251-file project gives a 2.3 MB index.

### Holding source in memory

`e3.py --archive-file` was run on an archive of a large source tree, the Go standard library:

| Measure | Result |
| --- | --- |
| Tree | 10,703 files, 140 MB on disk, 28.5 MB compressed |
| Unpack into memory | 0.7 seconds |
| Text kept | 9,960 files, 90 MB |
| Literal search over all of it | 0.13 seconds, 894 matching lines |
| Peak memory of the tool | 173 MB |

The tool's network calls were not run. Fetching another repository's archive from GitHub is refused in the drafting environment.

### The secrets scan

| Tree | Text files | Files the planned settings would withhold | Time |
| --- | --- | --- | --- |
| This repository | 254 | 4 (1.6%): three test files and one fixture | Under 3 seconds |
| The Go standard library | 9,960 | 18 (0.2%): test keys and certificates | About 5 seconds |

### A live conversation with the Gemini API

`gemini_live.py` was run with a key the design owner supplied, on the free tier.

| Question | Result |
| --- | --- |
| Does a streamed two-tool conversation work on the current API (Interactions, stateless)? | Yes. Search, then read, then an answer, in three requests. The first event arrived in about 1.2 seconds each time. |
| Does it work on the older API the CLI's adapter uses (`generateContent`)? | Yes. The provider's documentation now calls that API legacy. |
| Must the provider's reasoning data be sent back inside a tool loop? | Yes, on both APIs. Leaving it out gave HTTP 400 on the second request. |
| Must it be sent back for an earlier, finished question? | No. A follow-up question was answered correctly with the earlier turn replayed as plain text. |
| Token counts for the three requests | Input 162, 249 and 464. Visible output 18, 20 and 151. Reasoning 40, 35 and 165. Reasoning was more than half of all output. |
| A rate limit | HTTP 429, code `too_many_requests`, with the wait in the message ("retry in 36s"). |
| An exhausted daily quota | The same status and code, with a `retry-after` header of 31,174 seconds. Only the length of the wait tells it from a rate limit. |
| A model under heavy demand | HTTP 503, code `service_unavailable`. Seen on three of the newest models. |
| A bad key, an unknown model | HTTP 401 and HTTP 404, each with a message. |

### GitHub's calls, against this repository (2026-11-24)

The drafting environment can reach GitHub's API for this repository, with that environment's own
credential and not a fine-grained token. The calls the service will make were run against it:

| Call | Result |
| --- | --- |
| A commit's archive, named by its full hash | HTTP 200 after one redirect. A gzip tar with a single top-level folder named after the owner, the repository and a short hash. 676 KB in 0.7 seconds |
| Compare the default branch with a head commit | The merge base is returned |
| The index workflow's push runs for a head commit | An object wrapping a list. One run, finished and successful, whose head commit matches |
| That run's artifacts by name | An object wrapping a list. One artifact, not expired, with a digest |
| The artifact's download | HTTP 200 after one redirect. A zip holding exactly `index.scip` |

The archive and compare calls were then run again through the service's own code, once it existed:
the client that follows the redirect without the token, and the unpacking into memory.

| Step | Result |
| --- | --- |
| Fetch and unpack this repository's archive at one commit | 0.97 seconds. 301 entries, all kept as text, 2.2 MB |
| The same commit asked for again | 13 microseconds, from memory |
| A literal search over the kept text | 4 milliseconds; it stopped at its limit of 200 lines |
| Compare, for the merge base | A full commit hash |

What this leaves untested is narrow: that a fine-grained token with the four read permissions is
accepted by the same calls on a private repository. GitHub's documentation lists those permissions
for these calls.

### What could not be run

* **The archive and compare calls with a fine-grained token on a private repository.** Their mechanics were run against this repository; see above.
* **GitHub's web editor.** It reads repositories through the same API from the browser, so it cannot be opened there either.
* **Anything on a team repository:** a monorepo with several TypeScript projects, generated types, or `bun`.
* **The Anthropic API.** No key was supplied.
* **Credential approval.** That is a question for the organisation owner.

## Judgements

Made by the drafter on the design owner's instruction. Each says what it rests on and what would overturn it.

| Condition | Judgement | Rests on | Would be overturned by |
| --- | --- | --- | --- |
| E1. Credential approval | Not answered. It does not block building, because the first connected repository can be a private one on the design owner's own account, where a fine-grained token needs no approval. An earlier version of this page said that repository would be this one; this repository is public, and a public repository cannot be connected. It blocks connecting an organisation's repository. | GitHub's documented rules for fine-grained tokens | Nothing; it is still to be asked |
| E2. The indexer | Passed, with the revised rules. | A real run on GitHub's runners; 99.8% of 1,657 in-project definitions right, none wrong, on two projects; no mislabelling on a partial index | A team repository where the index is partial in a way the rules mislabel, or where the install needs something the workflow lacks |
| E3. Reading as the user | Passed for memory, unpacking, search, the archive call and finding an index by commit. Not tested with a fine-grained token on a private repository. | The measurements above. A 28.5 MB archive is a few seconds of transfer. | The archive call refusing a full commit hash or a fine-grained token, or taking tens of seconds. PR-50 tests this first, against the first connected repository, before anything is built on it. |
| E4. Existing tools | Unmeasured. The decision to build stands on the design owner's goal of a platform the team controls. | The design owner's decision | Nothing; the comparison is no longer a gate |
| Scanner hit rate | Low, and concentrated in tests and fixtures. Withholding single files is kept. | 0.2% and 1.6% of files on two trees | A team repository where most questions touch a flagged file |
| Provider | Google's API, through its current stateless conversation call, is the first and only adapter in the first version. | The live run | A repository that must use another provider |

## Results from a team repository

To be filled in if the trials are later run on a team repository. Counts and timings only.

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
```
