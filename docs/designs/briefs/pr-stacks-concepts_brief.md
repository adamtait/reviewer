<!-- SPDX-License-Identifier: MIT -->
# PR stack support in reviewer
* **Author:** adamtait
* **Date:** 2026-11-28
* **Status:** Brainstorm brief, round 2. Not a design. It replaces the round-1 brief.
  - Raw findings by lens: `docs/designs/briefs/pr-stacks-goldfish-findings-round2.md`.
  - Round-1 findings, unchanged: `docs/designs/briefs/pr-stacks-goldfish-findings.md`.
  - Measured GitHub behaviour: `docs/designs/briefs/pr-stacks-sandbox-results.md`, cited below as "sandbox §N".

## Overview

The revised concept, confirmed by the author after the sandbox tests:

- Each PR in a GitHub-native stack keeps its own review page, URL and diff.
- Pages are read inside a **stack snapshot**: the trunk merge base plus the ordered head SHAs of every layer. When GitHub's stack no longer matches the snapshot, the page says so.
- Per-layer diffs are drawn in the workspace.
- Go-to-definition runs at the page's own layer head. References also ask the tip's index, so uses added in higher layers appear, each labelled with the PR that added it.
- The assistant answers questions about the whole stack.
- Layer labels: file-level at once; line-level, which is the author's ideal, worked out when a file is opened.
- Staleness is checked by ancestry.
- Merged layers are shown, labelled as part of the base.
- First, on its own, comes a fix for the CLI/Action defect found in sandbox §7.
- Scale: up to 50 layers.

All seven lenses completed: design, codebase details, zoom-out, comprehension, implementation readiness, structural and inventive catalyst. They were fresh reviewers. They had the round-1 brief and findings as prior art, plus the sandbox results.

**What the sandbox settled.** Six round-1 blockers or risks are now measured facts:

- Discovery works under the client's pinned API version with `pull_requests=read`, and `stack` is already on `GET /pulls/N` (sandbox §1).
- `base.sha` does not follow the base branch, and its refresh rule is unknown: it moved on a restack but not on a plain push to the upper layer (§2).
- Cascades start index runs within seconds (§3).
- Opening a stack costs about 2.4 calls per layer (§4).
- Combined compares silently lose patches; adjacent ones did not in 21 tries (§5).
- The Action reviews every upper layer together with all the layers below it (§7).

The Analysis lists which round-1 claims these make obsolete.

**Where the lenses agree.**

1. **The `base_ref` fix is needed, but it is not a one-token swap. Whether it is correct depends on one thing nobody has measured.**
   - Putting a SHA into `BASE_REF` turns both `action.yml:45` and `action.yml:82` into `origin/<sha>`, so every PR fails the history check.
   - For a stacked PR, the Action diffs `--merge-base <base> HEAD` (`internal/diff/diff.go:67-68`) against GitHub's test merge commit. Nobody has measured whether that commit is built on the parent branch or on the trunk. If it is built on the trunk, the fix swaps one wrong scope for another.
   - (All seven.)
2. **The snapshot lives in the browser.**
   - The service keeps no session. Every data route is already keyed by a full commit hash (`internal/workspace/api/router.go:84-91`), and Cloud Run drops memory after about 15 minutes idle (`docs/workspace.md:630`).
   - What a layer page draws must come from calls keyed by SHA, never by PR number. `catalog.View` reads the PR's current head and current file list (`internal/workspace/catalog/pulls.go:59,83`), which can be newer than the snapshot.
   - (All seven.)
3. **Tip references need no line mapping.** The tip's index can be queried by SCIP symbol rather than by position:
   - Global symbols are keyed without a path (`internal/workspace/navigation/load.go:166-171`).
   - So the symbol at the clicked position in the layer head's index can be looked up directly in the tip's index.
   - That takes line mapping off the critical path for navigation. It is still needed for labels.
   - (All seven.)
4. **Four archives is a cap, not the normal case.**
   - A layer page needs its base and head. The tip loads on the first cross-stack action. The trunk merge base is needed only by the combined view, which should have its own address.
   - So no view holds more than three archives.
   - Four archives plus two indexes is about 1.25 GiB, more than the example 1 GiB cache (`examples/workspace.config.yaml:31-34`). The risk is thrashing, not running out of memory, because `maxLoads = 2` still bounds unpacking (`internal/workspace/source/cache.go:41-47`).
   - (All seven.)
5. **Defer the content-hash store.**
   - The cache charges each entry a fixed size and frees it on eviction (`cache.go:161,185-202`). With shared text, evicting a commit frees nothing the cache thought it freed, unless reference counts are added.
   - If memory pressure is ever measured, the cheap form is to reuse an identical string at the same path from a neighbouring commit of the same repository, and keep charging full size.
   - (All seven.)
6. **If line labels need a diff, compute it in the browser.**
   - `@codemirror/merge` is already a dependency (`web/src/components/CodePane.tsx:5`). It exports `diff()` and `Chunk.build`.
   - Diffing in the browser keeps the recorded rule "the service computes no diff" (`docs/designs/review-workspace.md:349`). The service has no Go text-diff code, and the module allows one dependency (`go.mod:5`, ADR-0028).
   - (Design, codebase, comprehension, catalyst, zoom-out; readiness and structural accept it as one adapter.)
7. **The assistant's commit is undecided, and this is now a blocker.**
   - Per-layer pages put the question's SHA at the layer head (`internal/workspace/api/assistant.go:60,69`).
   - Cross-stack answers need tip text. The gateway scans each segment's whole file at the question's commit (`internal/workspace/gateway/gateway.go:29-30,62-65,549-552`).
   - So a tip excerpt would be scanned against the wrong file and stamped with the wrong SHA (`api/assistant.go:115`).
   - (All seven.)

## Analysis

### Terms

"Session" already means sign-in (`docs/workspace.md:160-168`). "Base" now has five meanings in the code and the API (comprehension). The round-2 terms are below. Bare "base" should not appear in the UI or in the design.

- **Layer**: one PR in the stack, named "#N". Its position ("3 of 7") is extra information only.
- **Trunk**: the branch the bottom layer targets (`stack.base.ref`). It is also what the `pull_request` event calls `base_ref` (sandbox §7).
- **Layer head**: the layer's head SHA as recorded in the snapshot.
- **Layer base commit**: where the layer's own diff starts. It is merge-base(the layer's `base.sha`, layer head), the same as GitHub's own diff and today's `catalog.View` (`pulls.go:79`). When the stack is up to date, it equals the head of the layer below.
- **Own change**: the diff from the layer base commit to the layer head.
- **Tip**: the head of the top open layer in the snapshot. Drafts count.
- **Trunk merge base**: merge-base(trunk, tip), computed when the snapshot is taken.
- **Combined diff**: from the trunk merge base to the tip, shown in its own view.
- **Snapshot**: a value held by one browser tab. It records the stack number, the trunk, the trunk merge base, and an ordered list of (PR, state, merged_at, `base.sha`, head SHA). The tab takes it on the first stack page it opens. Moving to another layer in the same tab reuses it.
- **Review page**: one layer's page, `/pull/N`, read within a snapshot.
- **Up to date / out of date**: layer N is up to date when Compare(reference…head N) is `ahead` with `behind_by` 0. The reference is the head of the layer below. If that layer is merged, the reference is its recorded head when that head is an ancestor of head N, and the trunk merge base otherwise (sandbox §2, §6; catalyst, comprehension).
- **Already merged (#N)**: a merged layer's changes, now in trunk.
- **Changed in #N**: the last layer whose own change modified the line (blame meaning). The hover shows the chain. The concept says "added by", but blame gives "last changed in" (design, comprehension).
- **Unknown**: the label could not be computed. The reason is always shown. "Labels loading" is a separate state.

### What the sandbox made obsolete in round 1

| Round-1 claim | Measured | Now |
| --- | --- | --- |
| The pinned API version `2022-11-28` blocks discovery, so a per-request header is needed (B1, S1) | Stacks endpoints answer under both versions, and `stack` is on `GET /pulls/N` (sandbox §1) | Obsolete. Discovery is one field on a call the page already makes, plus one `GET /stacks/{n}` |
| Token permission unknown | `X-Accepted-GitHub-Permissions: pull_requests=read` (§1) | Mostly settled. A token with exactly the four workspace permissions is still untested |
| An unstacked PR may return 404 | The `stack` key is absent; `?pull_request=` returns `[]` (§1) | Obsolete |
| `base.sha` may follow the lower head without a rebase | It did not follow the base branch; it moved on a restack and not on a plain push to the upper layer (§2) | No refresh rule can be relied on. Use ancestry |
| Staleness = merge-base(head N-1, head N) == head N-1 | Merged heads are pre-squash and compare as `diverged` (§2) | Needs the merged-neighbour rule (Terms) |
| The tip may have no index after a cascade (M3) | Cascade force-pushes started index runs within 3 s (§3) | Obsolete |
| 12,000 calls an hour; target of 60 calls or fewer to open a stack | About 2.4 calls per layer, about 120 at 50 layers (§4) | Obsolete. The new target is per snapshot, not per page |
| Compare caps and patch omission unknown | Combined compare lost about 100 of 232 patches; adjacent compares lost none in 21 (§5) | Never use combined-compare patches. Adjacent compares are a sample, not a guarantee |
| "`--pr` already scopes each layer" (round-1 Agreement 6, C4) | The Action passes the trunk as `--base`, which overrides `base.sha` (§7) | Wrong. This was the most misleading line in round 1 |
| CLI squash re-report dispute; `LayerBase`; superseding ADR-0007 (S6, Disagreement 3) | A failed cascade starts no `pull_request` run (§6). The event's `base.sha` is the layer below (§7) | Obsolete. The fix restores ADR-0007's own rule (`docs/adr/0007-scope-analysis-to-changed-lines.md:28-30`), so no superseding ADR is needed |
| Ref chaining may be the primary discovery path | Native discovery works (§1) | A fallback for plain branch stacks and GHES only |

Overruled by the author, not by measurement:

- The session at the tip, with two archives, as the Ideal Final Result.
- Navigation and the assistant always at the tip.
- Linking out for layer diffs.
- File-level labels as the end state.

### Core contradictions and how to resolve them

**C1. An immutable snapshot, but the stack moves under the reader.**

Where it lives (all seven):
- In the browser, in `history.state` (`web/src/router.ts:106-117`) or in `sessionStorage` keyed by stack, as `web/src/nav/closed.ts:10-38` already does.
- Not in the URL. Fifty full SHAs add about 2 KB.
- One snapshot per tab per stack. The design lens found that taking one per page would cost about 120 calls each, about 6,000 to walk 50 layers, which is more than the 5,000-an-hour budget.

What it contains:
- Every head, including the page's own, comes from the single `GET /stacks/{n}` response (API notes 44-46).
- File lists come from Compare(layer base, layer head), not `ListFiles(number)`.
- Results keyed by a SHA pair never change. They go in the existing byte-bounded cache (`cache.go:21-26`), so they are paid for once per snapshot.

Identity:
- The stack id plus the ordered (number, state, head SHA) list.
- Leave out `stack.base.sha`: it follows the trunk tip (stack 38 reported `main`'s tip, sandbox §1-2). Including it would fire "stack changed" on every push to trunk (design, readiness).

Detection:
- `GET /stacks/{n}` on focus, every 60 s while the tab is visible, and before each question.
- That is about 60 calls an hour per visible tab (readiness).

On mismatch, the lenses split (see Disagreements):
- A blocking "reload", as the concept says.
- A banner that does not block, saying what changed per layer (comprehension, catalyst, design). An old snapshot stays valid because SHAs never change.

Whether a tarball of a pre-restack SHA is still served is unverified. A 404 for a snapshot SHA must read "stack changed", not "not found".

**C2. "Layer N head = layer N+1 base", but not when a layer is stale or above a merged one.**

- The concept's archive sharing holds only while the stack is up to date.
- When the layer below has moved on, merge-base(new lower head, head N) differs from GitHub's diff, which starts at the old lower head (sandbox §2). The upper diff would show the lower layer's new commits reversed.
- Resolution (comprehension, readiness, design, zoom-out, catalyst): the layer base commit is GitHub's, merge-base(`base.sha`, head). Take `base.sha` and head from the snapshot, so the page is still pinned to SHAs (structural, codebase).
- Staleness is a separate check and does not move the diff.
- Consequence: today's PR page already draws the layer's own change in the workspace (`pulls.go:76-79`, `web/src/components/DiffView.tsx:23-38`). Requirement 2 is mostly met by pinning that page to the snapshot (zoom-out, codebase, catalyst).
- **Failed cascade** (sandbox §6, #15): `base.sha` is now the trunk, so `config.ts` from merged #14 shows as #15's change, as it does on GitHub.
  - #14's recorded head is still an ancestor of #15's head, so compare(#14 recorded head, #15 head) identifies `config.ts` as "already merged (#14)".
  - Show it collapsed, under a banner telling the reader to ask for `gh stack rebase` (comprehension, catalyst).
  - This needs #14's recorded head to still be readable, probably via `refs/pull/14/head`. Unverified.

**C3. Navigate at the layer head, but references live at the tip.**

How to query (all seven):
1. Take the symbol at the click from the layer head's index (`ix.at`, `navigation/query.go:76-98`).
2. Look up its uses in the tip's index (`query.go:201-227`), as a new per-commit function, for example `UsesOf(ix, symbol)`.
3. Check each tip use against the tip text, as `FindDefinition` already does (`query.go:157-170`) and `FindReferences` does not (catalyst).

Caveats:
- scip-typescript symbol strings contain the package name and version (`navigation/scip/symbol.go:10-14`). A layer that bumps `package.json` breaks the join (unverified how often). Match ignoring the version, then fall back to labelled text search at the tip.
- Never show "no uses above this layer" from an empty join (design).

Presentation:
- `Location` carries no SHA (`internal/workspace/api/nav.go:128-136`), and `targetHref` builds every link from `context.sha` (`web/src/nav/navigation.ts:72-82`). So a tip line number would open at the layer head, on the wrong code. Every result must carry its own commit (design, comprehension, codebase).
- Show the results as separate groups, not one merged list:
  - "at #N's head (this layer and below)";
  - "added or changed above, at the tip: #104 (3), #106 (1)";
  - optionally "removed above (#105)".
- Drop tip results in files that no layer above N touched. Those files are identical at the tip and at the layer head when the stack is up to date, so the first group already has them (design, zoom-out).
- The code view prints "At commit abc1234" and a single-PR badge (`web/src/views/CodeView.tsx:189,257-258`). It must name the commit ("stack tip, head of #107") and use the layer states.

**C4. Line labels everywhere, but cheaply, and references need labels too.**

The readiness lens found a conflict in the concept:
- Item 3 labels each tip reference with its layer. Up to 500 references can come back (`query.go:29`), in files that are not open.
- Item 4 computes line labels only for opened files.

Options for labelling references:
- File-level chips on references ("in a file changed by #104, #106"), with line labels only where they are already cached (zoom-out, comprehension).
- The file prefilter in C3, which limits work to files touched above N (design).
- A method that labels every file at once (readiness).

The methods:
- **(a) GraphQL blame on the tip.**
  - The "new dependency" is smaller than stated: `ghclient` already sends GraphQL (`internal/ghclient/write.go:89-95`). The workspace has none, and it needs a `graphqlUrl` setting (`wsconfig/config.go:62-65`).
  - "Breaks with squashed history" is mostly wrong (design, zoom-out). A squash-merged layer that has been cascaded is below the trunk merge base, so it is base. What breaks is stale or rewritten history.
  - It labels only, in tip coordinates. It needs each layer's commit list, which `Compare` throws away (`ghclient/stream.go:122-138`).
  - Cost, limits and fine-grained-token support are unverified.
- **(b) single-file reads at the layer heads that touched the file.**
  - It gives labels and line maps in both directions (design).
  - It reopens the read path ADR-0030 rejected: "two read paths that must agree on which files are text" (`docs/adr/0030-read-source-as-the-user.md:80-84`). It must reuse `isText` and the file-size limit (`internal/workspace/source/archive.go:125-143,190-198`).
  - It needs its own small load pool, not the two archive slots (structural, codebase).
  - `export-ignore` files may come back from single reads but are absent from archives (unverified).
  - Diff in the browser, not in the service.
- **(c) compose adjacent-compare patches** (readiness, zoom-out).
  - The compares are already fetched for staleness and file labels, so this labels every file at about zero extra calls.
  - Use single reads only where a patch is missing.
  - It rests on adjacent patches staying complete, which 21 samples suggest but do not prove (sandbox §5).
- **Touch-count split** (catalyst), which works with (b) or (c).
  - If only one layer touched a file (k = 1), the file label is the line label, at no cost.
  - For k ≥ 2, read only the k−2 intermediate texts. The trunk merge base and the tip are already held. Skip texts that are identical to ones already held.

Output contract (structural):
- A standard-library-only package, for example `internal/workspace/stackmap`.
- It gives runs of lines labelled `Layer(#n)`, `Base`, `MergedBase(#n)`, `OneOf{#a,#b}` or `Unknown`.
- Each method is an adapter that feeds it, so the choice can be reversed.
- `OneOf` is exact information taken from file lists, and is better than "unknown" until line labels exist.
- Add a rules test. `rules_test.go:56-69` checks only direct `os/exec` imports, so a package that imports `internal/diff` (which runs git, `diff.go:15,116-117`) would pass it (codebase, structural).

**C5. Four archives per page against a cache sized for two.**

- The sizing rule (`docs/workspace.md:640-652`) bounds unpacking (two at a time), not how many archives are resident. Residency is bounded only by `cacheBytes` with silent LRU eviction (`cache.go:187-202`).
- Worst case for a middle layer: 4 × 256 MiB + 2 indexes ≈ 1.25 GiB, more than the example cache. One reader evicts their own data, and `DiffView` refetches file by file (`DiffView.tsx:49-58`).
- Realistic case: one archive of this repository is 2.2 MB (`docs/workspace.md:215-216`).

Resolution:
- First paint loads the layer base and head only.
- The tip and its index load on the first reference lookup or question.
- The combined view (`#view=stack`) uses the tip and the trunk merge base.
- Load what the page needs before prefetches, because the two load slots are shared by every user (design).
- Do not add a hard minimum `cacheBytes` check. `cacheBytes ≥ 4 × archiveBytes + 2 × indexBytes` rejects the shipped example config, so every deployment that copied it would fail to start (readiness). Instead:
  - document a minimum;
  - log a warning at start-up;
  - add a runtime guard on the observed `Commit.Size()` (`archive.go:69`) that offers cross-stack features only when they fit.

**C6. The content-hash store.**

- The archive format exposes no blob SHA; the pax header is skipped (`archive.go:111`).
- Text is held as one Go string per file in `map[string]string` (`archive.go:45-50,142`).
- The cheapest real-memory gain is to reuse an identical string from a neighbouring commit of the same repository at unpack time, or Go's `unique.Make` (Go 1.26, `go.mod:3`). Keep charging full size.
- Shared, reference-counted accounting changes the `Sized`/`Cache` contract and the sizing rule, and needs eviction and race tests.
- The catalyst proposes "cascade eviction" (evict children with the parent) as a middle path.
- All lenses: measure first, with `kept_bytes` on `archive_fetched` and a new `cache_evicted` log line.

**C7. The assistant: one commit, but cross-stack answers.**

- **Option A** (zoom-out, catalyst, readiness): `Subject` is the tip.
  - A selection on a layer page is restamped to the tip when the file is identical in the two archives, or when no layer above touched it. Otherwise it is refused, with a reason.
  - The gateway contract (ADR-0031) is unchanged.
- **Option B** (design, codebase, structural): every `Segment` carries its own SHA, and the gateway reads `File(sha, path)`.
  - This is cleaner for the long term. It amends ADR-0031 (Proposed) and the excerpt stamping.
  - It reopens round 1's rejected "multi-commit Subject".
- Either way:
  - add metadata tools: `stack_outline` with titles and file counts, not 50 descriptions of up to 20k characters (`assistant/tools.go:27`);
  - add a layer-scoped `list_changed_files`;
  - the snapshot replaces the per-question `catalog.View` calls (`api/assistant.go:86`);
  - key the conversation by snapshot within the tab. Today it is lost on every layer switch (`web/src/nav/conversation.ts:9-14`, `web/src/App.tsx:113-114`).

**C8. Is the `base_ref` fix complete?**

The defect is confirmed in code:
- `action.yml:51,87` set `BASE_REF` from `inputs.base || github.base_ref`.
- `action.yml:82` passes `--base "origin/${BASE_REF}"`.
- `cmd/reviewer/run.go:214-219` lets an explicit `--base` win.
- `--base` counts as unset only when it equals the literal `"main"` (`cmd/reviewer/flags.go:16`). So the Action has never used `pr.Base.SHA`, for any PR (catalyst).
- `self-review.yml:49` does the same.

What the fix must cover:
- **The history check.** `action.yml:45` must not be handed a SHA. Either keep checking `origin/<base_ref>` (readiness), or check `git cat-file -e "$BASE_SHA^{commit}"` (codebase, design, structural). Keep the `origin/` prefix only for an explicit `inputs.base`.
- **The source of the base: measured after this review (sandbox §7a, experiment E1).** For a stacked PR, GitHub builds `refs/pull/N/merge` on top of the layer below's test merge commit, not on the trunk. Against #12's merge commit, the three candidates gave:
  - `--merge-base origin/<base_ref>` (today): #12's file plus #11's. Wrong.
  - `--merge-base <base.sha>` (the event's or REST value; they were equal): #12's file plus a trunk-only file that is in no layer, because `base.sha` lagged. Wrong. This rules out both "pass the event's `base.sha`" and "drop `--base` and use the REST `base.sha`".
  - `HEAD^1`, the merge commit's first parent: #12's file only. Right. For an unstacked PR, `HEAD^1` is the base branch tip, which is what `--merge-base origin/<base_ref>` resolves to today, so behaviour should not change for them (not yet run on an unstacked control).
  `HEAD^1` needs `fetch-depth` of at least 2 in the checkout. The lenses' alternative of checking out `github.event.pull_request.head.sha` remains open; it would need a base that is not `HEAD^1`.
- **`base.sha` missing locally.** For example, the lower branch was force-pushed before the job ran. `run.go:225-237` then falls back to GitHub's file list, which is still the layer's own change. But the model lane skips (`internal/builtin/laneb.go:38-45`), and the osv base scan is lost.
- **Copied workflows.** A new `action.yml` reaches users only when they bump the pinned `uses:` and `version:` (`review.yml.tmpl:42-45`). Workflows copied from `self-review.yml` are never fixed by it. A CLI check for "`--pr` on a stacked PR with `--base` naming the stack trunk" would protect them. The lenses split on whether it should warn or override.
- **`ci.yml`.** Lines 121 and 144 are gates (`checkadrs`, `audithistory`). Checking from the trunk covers more than needed, which is the safe direction. Line 96 is informational (`|| true`).
  - Structural and design say leave them on the trunk deliberately, with a comment.
  - Catalyst says change them; codebase says low priority.
  - Sandbox §7 grouped them with the defect. They post no comments.
- **Real impact** (readiness).
  - Inline comments on lower-layer lines are probably refused by GitHub (logged as warnings, `internal/reporters/github.go:97-101`; unverified).
  - The summary, the analyzer and model cost, and the secrets gate still repeat lower layers on every layer above.
  - After the fix, summaries are rewritten. With `resolveStaleThreads` on (`internal/config/config.go:121-123`), old duplicate threads are resolved on the next clean run. The release note should say so.
- **No ADR is needed.** The fix restores ADR-0007's intent. Update the `base` input description (`action.yml:19`) and "Two CI surfaces" in `docs/architecture.md:95-99`.
- **No regression test exists.** Add:
  - a `cmd/reviewer` test of `--pr` with no `--base` over a trunk→L1→L2 history;
  - a test that fails if `github.base_ref` reaches `reviewer --base` in `action.yml` or `self-review.yml`;
  - a sandbox rerun on stack 13.

**Does the CLI need more?**
- `reviewer watch` never checks out the PR. It diffs each PR's base against the local `HEAD` (`cmd/reviewer/watch.go:33-39`, `diff.go:67-68`). That is a defect for every PR, not only stacked ones (codebase, readiness, catalyst, design).
- Until it is fixed, the failed-cascade question from sandbox §6 cannot be tested meaningfully.
- Code reading predicts that `watch` would post #14's `config.ts` findings on #15, because dedupe works per PR. This was inferred, not run.
- Proposed fixes:
  - skip stacked PRs whose `mergeable_state` is `dirty` (design);
  - store a hash of each PR's own diff in poller state and skip when it is unchanged, since each cascade bumps `updated_at` on every upper PR (`internal/poller/poller.go:89-90`) (catalyst; design in round 1).
- `--dry-run` does not help: `watch` only prints "would review" (`poller.go:96-101`).

### Secondary trade-offs

- **Config upgrade and rollback** (readiness, blocker for rollout).
  - Every limit must be greater than zero (`config.go:185-197`), and unknown keys are parse errors (`config.go:128-131`).
  - Required new keys such as `limits.stackLayers` would stop existing deployments from starting. Adding the keys and then rolling back would fail too.
  - A missing key should mean "stacks off", with a log line.
- **Test fake.** `testgithub` compare answers only a PR's own pair and returns only `merge_base` (`internal/workspace/testgithub/testgithub.go:326-344`). It needs:
  - compares by SHA pair, with status, behind_by, files and missing patches;
  - `/stacks` routes;
  - `stack` in the PR JSON;
  - a contents or GraphQL route.
- **Small loads wait behind archives.** Compares and single-file reads would take the two `maxLoads` slots (`cache.go:149-152`). Give them their own bounded pool, and handle 429 and `Retry-After` (only a 403 is read today, `client.go:405-406`).
- **Compare file cap.** `ghclient` notes "up to 300 files" (`stream.go:128-129`). Whether larger layers paginate is unverified. Mark a truncated list "unknown".
- **"Comment on GitHub"** builds a link to GitHub's current diff (`web/src/github/links.ts:27-30`). If the snapshot is behind, warn first (comprehension). From the combined view there is still no target PR.
- **Polling.** A layer page polls the layer head's index, the tip's index and the stack. That is within budget (structural, readiness).
- **Observability.**
  - Log `stack_loaded` with layers, merged count, stale count, calls, unknown labels and tip index state.
  - Log `cache_evicted`.
  - Add the layer and the answering commit to `navigation_reported_wrong` (`api/nav.go:151-179`).
  - Add a `stack` field on `pull_view` to measure review time per stack.
- **Trust rule.**
  - Reword "for the commit on screen" (`review-workspace.md:355`) to "for the commit the result is located in".
  - Add stack membership as a row in the trust table, used for labels only.
  - A layer page's references can come from the tip's index, which whoever pushed the tip controls (structural).

### Disagreements between lenses

1. **Snapshot checking.**
   - Design and readiness: the server re-reads `/stacks/{n}` (remembered for 10-15 s) and answers 409 `stack_changed`.
   - Structural: treat the snapshot as the user's own claim, since a forged one only mislabels the forger's screen; detect change in the browser.
   - Zoom-out: no snapshot object at all, and no reload banner in v1. Stack data is a field of `pull_view`.
2. **On a change: block or inform.**
   - The concept: "reload".
   - Comprehension and catalyst: a banner that does not block, plus "what changed" as compare(old head, new head).
   - Design: grade it by whether the change is at or below this layer, or above it.
3. **Line-label method.**
   - Design and comprehension: (b) in the browser.
   - Readiness: (c), with a bake-off.
   - Zoom-out: file labels only for now; (a) or (c) later.
   - Catalyst: the touch-count split.
   - Structural: an adapter contract, decided by experiment.
4. **The assistant's commit:** option A (zoom-out, catalyst, readiness) or option B (design, codebase, structural).
5. **The Action's base.** The lenses split between the event's `base.sha`, the REST `base.sha`, and gating on stack membership. Sandbox §7a, run after this review, showed both `base.sha` options review a trunk change when `base.sha` lags, and found `HEAD^1` correct. Gating on stack membership is no longer needed if `HEAD^1` holds for unstacked PRs.
6. **The CLI check on a stacked PR whose `--base` names the trunk.**
   - Catalyst and design: override, and warn.
   - Codebase, readiness, comprehension: warn only, because "explicit `--base` wins" is a stated rule (`run.go:214-215`).
7. **`ci.yml` gates:** keep them on the trunk (structural, design) or change them (catalyst).

### Ideal Final Result

- A review page is today's PR page, pinned to the snapshot's SHAs, plus labels.
- It opens with two archives. It loads the tip only when the reader asks a cross-layer question.
- The server holds no session. Every answer is keyed by SHA, and results for a SHA pair are cached for as long as memory allows.
- Cross-layer references are a symbol lookup in the tip's index, each result carrying its own commit.
- Labels never claim more than the data shows: "unknown", "one of #a, #b" or "#N".
- The CLI needs no stack model. It stops overriding the PR's own base.

### Objections by severity (summary)

**Blockers**
- The `base_ref` fix as worded breaks the history check, and its correctness depends on how the merge commit is built (all seven; `action.yml:45,82`, `diff.go:67-68`).
- Undefined snapshot lifecycle: who takes it, where it lives, per tab or per page, and when change is detected (comprehension, design, structural, readiness).
- The assistant's commit (C7): tip text scanned against the wrong file (codebase, structural, design).
- References labelled by layer conflict with lazy line labels (readiness).
- New config keys break startup on upgrade and rollback (readiness).

**Major** (details in the round-2 findings): the meaning of "layer base" and the failed-cascade case; results carry no commit; the symbol-join caveats; a four-archive working set larger than the example cache; small loads behind archive slots; `watch` diffs the local `HEAD`; copied workflows not fixed; the test fake cannot model stacks; terms that collide; the content-hash accounting.

## Goals vs. Non-Goals

### In-Scope Goals
- The CLI/Action `base_ref` fix, shipped first and alone, with regression tests and a release note.
- Discovery from the `stack` field on `GET /pulls/N` plus `GET /stacks/{n}`. When stack data fails to load, fall back to the single-PR view with a reason, never a page 404 (`internal/workspace/api/pulls.go:45-74`).
- A snapshot per tab per stack. Every layer page is drawn only from SHA-keyed calls.
- A stack strip on every layer page showing state, staleness by ancestry, and merged layers as "already merged (#N)".
- Per-layer diffs in the workspace, matching GitHub's diff for that PR.
- File-level layer labels from adjacent compares, cached by SHA pair. `OneOf` where a file was touched by several layers.
- References from higher layers through a symbol join on the tip's index, each result carrying its commit and layer.
- Assistant tools that answer cross-stack questions and name the layer, after the commit decision (C7).
- Line-level labels, lazily, by the method the experiment chooses. "Unknown", never "unchanged".
- A combined view at its own address, with its file list computed from the two archives, never from a combined compare (sandbox §5).
- Memory per view independent of layer count: at most three archives, with a runtime guard.
- Logging enough to diagnose slow opens, wrong labels and cache thrashing, and to measure review time per stack.

### Out-of-Scope Non-Goals
- Computing a post-merge or post-rebase tree.
- One archive or one index per layer, or merging indexes.
- Any write call to GitHub, including restacking.
- A stack model in the CLI, `LayerBase`, or a superseding ADR for ADR-0007.
- Labels built from combined-compare patches.
- A content-hash store in v1.
- Merged layers' content line by line in v1.
- Stacks that fork into trees.
- GHES or plain branch stacks beyond ref-chaining as a fallback.
- Changing the CLI comment marker (`internal/fingerprint/fingerprint.go:53`).
- Fixing `watch`'s local-`HEAD` defect inside the stacks work. Record it as its own fix.

## Alternatives Weighed

| Option | Status | Technical reason |
| --- | --- | --- |
| Whole stack in one session at the tip (round 1) | Rejected by author | Page per PR. The tip is still the source for higher-layer references and possibly the assistant (C7) |
| Link out (to GitHub or elsewhere) for layer diffs | Rejected by author | Per-layer diffs are drawn in the workspace. Zoom-out, codebase and catalyst note that today's PR page already draws the layer's own change (`pulls.go:76-79`) |
| File-level labels as the end state | Rejected by author | Line level is the ideal. File level ships first |
| Tip-only navigation for lower layers (round 1, all lenses) | Dropped along with the single session | Go-to-definition runs at the layer head. Only references and possibly the assistant reach the tip |
| `base.sha` equality as the staleness detector | Raised by design, codebase, readiness, catalyst (round 1); refuted by sandbox §2 | `base.sha` has no reliable refresh rule (§2). It agreed with ancestry in every case run, but ancestry asks the question directly |
| Per-request API version header for stacks | Raised by all lenses (round 1); obsolete | Works under the pinned version (sandbox §1) |
| Ref chaining as the primary discovery path | Raised by zoom-out, catalyst (round 1); now fallback only | Native discovery works. It misses merged layers |
| Labels or rendering from combined-compare patches | Raised by design (round 1); refuted by sandbox §5 | About 100 of 232 patches silently missing |
| One archive per layer; merging 50 indexes | Raised by design (round 1); still rejected | About 51 archives. The tip's index covers every open layer |
| `LayerBase(stack, n)` in the CLI; superseding ADR-0007 | Raised by catalyst, structural (round 1); obsolete | Sandbox §6-7: the event's `base.sha` is the layer base. The fix restores ADR-0007's own rule |
| Snapshot held by the server, with an id in the page | Raised and rejected by structural | First session state in a service that stores nothing. Lost after about 15 minutes idle (`docs/workspace.md:630`) |
| Snapshot in the URL | Rejected by codebase, comprehension, catalyst | About 2 KB for 50 SHAs. Links should take a fresh snapshot |
| No snapshot object; stack data as a field of `pull_view`; no reload banner in v1 | Raised by zoom-out; open | Simplest. Leaves mixing between layer pages in one tab unresolved |
| Server re-check of the snapshot (409 `stack_changed`) | Raised by design, readiness; structural against | Labels rest on checked data. Costs about one call per stack-wide request |
| Banner that does not block, with "what changed" diffs | Raised by comprehension, catalyst; design (graded) | An old snapshot stays valid. A forced reload loses the conversation (`conversation.ts:12-14`) |
| Each layer's own diff from merge-base(lower head, head) | Raised by structural, codebase; others against | Differs from GitHub's diff when a layer is stale and shows lower changes reversed |
| Line mapping by position for references (`MapForward`) | Raised by readiness as a fallback | The symbol join needs no mapping. Mapping stays for labels and "changed again in #M" marks |
| Method (a): GraphQL blame on the tip | Raised by concept; zoom-out prefers it if line labels are needed; open | One call per file. Labels only, in tip coordinates. Needs commit lists. Cost and limits unverified |
| Method (b): single-file reads, diffed in the service | Raised by concept; design, codebase, zoom-out against diffing in the service | Breaks `review-workspace.md:349`. No Go diff code exists, and ADR-0028 limits dependencies |
| Method (b) diffed in the browser | Raised by design, comprehension, codebase, catalyst; open | Reuses `@codemirror/merge`. Gives line maps both ways. Needs an ADR-0030 amendment for the read path |
| Method (c): compose adjacent-compare patches | Raised by readiness, zoom-out; open | Calls already made. Rests on adjacent patches being complete (21 samples) |
| Touch-count split (k = 1 free) | Raised by catalyst; open | Exact for files one layer touched. Reads only intermediate texts |
| Four archives on every page | Raised by design, zoom-out, structural, catalyst, readiness; rejected | Working set larger than the example cache. Split by view, so at most three |
| Hard minimum `cacheBytes` check | Raised by structural, codebase (document a minimum); readiness against a check | A check would reject the shipped example config. Use a warning plus a runtime guard |
| Required new `limits.*` keys | Raised by round 1; readiness against | Breaks startup on upgrade and rollback. A missing key means "stacks off" |
| Global content-hash store with shared accounting | Raised by concept; all lenses defer | Reference counts, eviction and race tests. No gain in download or unpack time |
| Reuse identical strings from a neighbouring commit, full charging | Raised by catalyst, design, readiness, codebase, structural; deferred | Real-memory gain only, with no contract change |
| Multi-commit assistant (`Segment.SHA`, `File(sha, path)`) | Raised by design, codebase, structural; open (reopened from round 1) | Clean boundary. Amends ADR-0031 |
| Assistant `Subject` at the tip, selections restamped | Raised by zoom-out, catalyst, readiness; open | No gateway change. Selections from rewritten files are refused |
| Action: put `base.sha` into `BASE_REF` as is | Raised by concept; rejected by all lenses | `origin/<sha>` fails the history check (`action.yml:45`) |
| Action: drop `--base`, use REST `base.sha` | Raised by zoom-out, structural, catalyst; design, readiness against | Read at run time, so it can be newer than the event's checkout |
| Action: `inputs.base` or event `base.sha`, with a split history check | Raised by design, codebase; open | One rule for all PRs. Changes the base for unstacked PRs from a ref to a SHA |
| Action: gate on stack membership | Raised by readiness; open | Unstacked PRs unchanged. Whether `stack` is in the Actions event payload is unverified |
| Check out `pull_request.head.sha` | Raised by design, codebase; open | Correct whatever the merge commit is. Changes what analyzers read for every PR |
| CLI overrides a trunk `--base` on stacked PRs | Raised by catalyst, design; codebase, readiness, comprehension prefer a warning | Protects copied workflows. Breaks "explicit `--base` wins" |
| Change `ci.yml` gates to `base.sha` | Raised by catalyst; structural, design against | The gates post nothing. Covering more is safe |
| `watch`: skip dirty stacked PRs; skip when the own-diff hash is unchanged | Raised by design, catalyst; open | Mirrors GitHub's no-run rule. Cuts re-reviews after cascades |

## Key Decisions & Staging for Next Steps

### Decisions made

By the author:
- One page per PR, with its own URL and diff.
- Per-layer diffs drawn in the workspace.
- Line-level labels. They must cover every file of the stack at once, not only opened files, and scale to large codebases and large changes. (This replaces "file-level ships first".)
- The snapshot concept.
- Go-to-definition at the layer head; references also at the tip.
- The assistant works from the current layer's commit and can read content up to the tip. Every excerpt carries its own commit, and the gateway scans each excerpt against the file at that commit. This is option B, and it amends ADR-0031.
- "Unknown", never "unchanged".
- The `base_ref` fix ships first.

Defaults set on the author's behalf (the author asked for good defaults; each can be revisited):
- **Memory.** A layer page holds two archives, as a pull request page does today: the layer's base and head. References into higher layers use the tip's index, not its source, so the tip's archive is loaded only when the user opens a file at the tip. The combined view loads its own two archives (trunk merge base and tip) only when it is opened. At startup the service checks that `limits.cacheBytes` holds three archives at `limits.archiveBytes` plus two indexes. If it does not, stack features stay off and the log says why. The content-hash store waits for experiment E10.
- **Rollout.** One optional configuration block, `stacks: {maxLayers: N}`. When the block is absent, stacks are off and pages behave as today, so existing configurations keep starting after an upgrade. An older version rejects the unknown key on rollback, so the rollback note says to remove the block first. Above `maxLayers`, the page shows the layers nearest the open one and says the stack was cut short. The example configuration sets 50.
- **Line-label method, to be confirmed by E4.** Each layer's own file list from `GET /pulls/N/files`: paginated, up to 3,000 files per layer, with a patch per file. Compose these bottom to top into a label per line for every changed file, once per snapshot, cached by the pair of head SHAs. Files whose patch GitHub omits are read whole at the two heads (a small amendment to ADR-0030). A layer measured from a merged neighbour or a stale base is labelled from the trunk merge base. Anything still unresolved is "unknown". The cost is about one call per 100 changed files per layer, not one call per opened file.
- **CLI check for copied workflows: override, with a note** (open decision 2 below, now settled). On the test merge checkout, a `--base` naming the PR's own base branch or its stack's trunk is measured from `HEAD^1`. On any other checkout, a `--base` naming the trunk on a stacked PR keeps its meaning and adds a warning.

By measurement (sandbox):
- Discovery through the `stack` field under the pinned version.
- Staleness by ancestry, with the merged-neighbour rule.
- Tip-only index polling is within budget.
- No combined-compare patches.
- The Action's base is the test merge commit's first parent (`HEAD^1`). Correct on a stack's top layer, its bottom layer and an unstacked PR; `base.sha` was wrong on all three (sandbox §7a).

Agreed by all lenses:
- The snapshot lives in the browser.
- No server session.
- Results keyed by SHA pair are cached.
- References use the symbol join.
- The content-hash store is deferred.

### Status of T0 (the CLI and Action fix)

Implemented on branch `fix/stacked-pr-base`, pull request #43.
- `cmd/reviewer/run.go`: `reviewBase` decides the base, as described in "Defaults" above.
- `internal/diff`: `Parents`.
- `internal/ghclient`: `PullRequest.Stack`.
- `action.yml`: input description only. The Action's YAML needs no other change, because the binary does the work.
- `docs/architecture.md`, `CHANGELOG.md`.
- Tests: a table test of the decision, and an end-to-end test of the `--pr` path on a repository built like GitHub's stacked test merge commits. With the detection disabled, that test fails the way the defect does.
- Verified with the real binary on the sandbox's own test merge commits. Today's binary gives #12 `api.ts` and `pricing.ts`; the fixed binary gives `api.ts` with a note. Both give unstacked #24 `control.ts`.
- `go test ./...`, `go vet`, `gofmt`, `staticcheck`, `addlicense`, `checkadrs` and `checklicenses` pass.
- Not changed: `ci.yml`'s gates, which check more than needed (the safe direction); and `reviewer watch`, which reviews whatever `HEAD` the local checkout has (filed separately in round 1).

### Decisions still open

1. **Snapshot checking and change handling:**
   - server 409 or client claim;
   - a blocking reload, a banner, or a graded banner;
   - or zoom-out's "no snapshot object" for v1.
2. **The layer base commit:** GitHub's merge base (most lenses) or merge-base(lower head, head), plus the failed-cascade labelling.
3. **How the gateway carries a commit per excerpt** (follows from the author's assistant decision): the segment format, and the ADR-0031 amendment's wording.
4. **Whether stack work waits** for ADRs 0029-0034 to be accepted after the second feedback week (`review-workspace.md:510`).

### Remaining experiments

| # | Experiment | Pass when |
| --- | --- | --- |
| E1 | In `pr-probe`, print `HEAD^1 HEAD^2`, the event's `base.sha`, `toJSON(github.event.pull_request.stack)`, and `git diff --name-only --merge-base $BASE_SHA HEAD`. Run after moving `stacks/trunk` past the fork point, on bottom, middle and top layers and an unstacked control | Each layer's list equals `/pulls/N/files`, and the control is unchanged. Blocks T0. **Partly run** (sandbox §7a): top layer done, `HEAD^1` correct; bottom layer and unstacked control still to run |
| E2 | Fix regression: script-level test of the base choice (input set, stacked, unstacked, empty SHA). Real reviewer on a new 3-layer open stack | Upper layer reports only its own files. Control output identical |
| E3 | `reviewer --pr 15` on stack 16, in a full clone at #15's head and in a clone without its `base.sha` | Records whether `config.ts` is reviewed. Decides P7 |
| E4 | Line-label bake-off (a, b, c, touch-count split) on stacks 29, 38 and a synthetic 50-layer stack, against a local git oracle | 0 wrong labels; ≤2% unknown; ≤3 calls per file at p50; cost of labelling 500 references reported |
| E5 | SCIP symbol stability across layer heads, including a `package.json` version bump | Join hit rate reported; fallback triggers when it misses |
| E6 | Tarball for a pre-restack SHA (`018f960`, `5002070`) | Served, or a 404 is mapped to "stack changed" |
| E7 | Token with exactly the four workspace permissions (from sandbox "Still open") | `/stacks` and `stack` readable |
| E8 | Adjacent compare over 300 files | Truncation detectable, or marked unknown |
| E9 | Single-file read size limits; `export-ignore` files; `If-None-Match` 304 on `/stacks` | Limits recorded |
| E10 | Heap against `Commit.Size()` with four archives of a ~50 MB repo, with and without string reuse | Decides whether P6 happens |
| E11 | `base.sha` after the upper layer is pushed without a restack (sandbox "Still open") | Confirms the predicted refresh |

### Staging

- **T0. CLI and Action fix, a separate track that ships first.**
  - Run E1, then:
    - the split history check;
    - the chosen base source in `action.yml` and `self-review.yml:49`;
    - optionally the `head.sha` checkout;
    - the `cmd/reviewer` test and the workflow guard test (E2);
    - the `action.yml:19` description;
    - `docs/architecture.md:95-99`;
    - a release note: the symptom, "bump `uses:` and `version:` together", "custom workflows: drop `--base origin/${{ github.base_ref }}`", and what happens to existing duplicate threads.
  - No ADR. The `watch` local-`HEAD` defect is filed separately.
- **P1. Plumbing.**
  - In `ghclient`: `PullRequest.Stack`, `GetStack`, a new `CompareRange` (status, ahead/behind, merge base, paginated files), and 429 handling.
  - The pure `stackmap` package: snapshot type, states, staleness and layer-base rules, and the label contract. Tested on fixtures from sandbox stacks 3, 13 and 16.
  - Fake-server routes.
  - Config where an absent key means stacks are off.
- **P2. Stack strip and snapshot on the existing page.**
  - Layer pages draw from snapshot SHAs.
  - Staleness, merged and failed-cascade labels.
  - Change detection.
  - File-level and `OneOf` labels.
  - A separate pool for small loads.
  - No new archives.
- **P3. Cross-layer references.**
  - The symbol join with fallbacks (after E5).
  - `Location.sha` and `layer`.
  - Grouped results and code-view labels.
  - The tip archive loaded lazily, plus the memory guard.
- **P4. Assistant**, after decision 5: `stack_outline`, layer-scoped tools, and the conversation kept by snapshot. The primary success metric is met here.
- **P5. Combined view** at its own address, with its file list from the archives.
- **P6. Line-level labels** by the E4 winner, with an ADR-0030 amendment if (b) wins.
- **P7, only if measured.** String reuse or the content-hash store (E10). `watch` skip rules (E3).

Critical path: T0 in parallel. Main path: decision 3 → P1 → P2 → E5 → P3 → decision 5 → P4. P5, P6 and P7 are off the critical path.

### Documents to change

- **New Proposed ADRs from 0035** (`docs/adr/README.md:51`; old reports reused 0035/0036 as draft numbers):
  - the snapshot as a browser-held value, and the layer-base rule;
  - how layer labels are read, and "unknown";
  - the trust rule for stack data and for index answers from another commit;
  - if option B, an amendment to ADR-0031.
- ADR-0030 is Proposed. Structural says to write a new ADR rather than edit it while it is on trial; codebase and readiness say amend it in place.
- ADR-0007 stays as it is.
- `review-workspace.md`: line 349 (if any diff moves to the service), line 355 and the trust table (257-265), and a pointer under "Later phases" (138-145).
- `workspace.md`: memory sizing (636-652) and reading source (185-228).
- `architecture.md`: the package table (149-162) and "Two CI surfaces" (95-99).

### Open questions

- Is a stacked PR's test merge commit built on the trunk or on the parent branch (E1)?
- Is `stack` present in the Actions event payload, and can `GITHUB_TOKEN` read it?
- How is "average review time per stack" measured? The cheapest option is a `stack` field on `pull_view`.

### Reviewer status

| Reviewer | Status |
| --- | --- |
| `egm-design` | completed |
| `egm-codebase-details` | completed |
| `egm-zoom-out` | completed |
| `egm-comprehension` | completed |
| `egm-implementation-readiness` | completed |
| `egm-structural` | completed |
| `egm-inventive-catalyst` | completed |
