<!-- SPDX-License-Identifier: MIT -->
# Goldfish findings, round 2: PR stack support in reviewer

**Target:** the revised FeatureConcept "PR stack support in reviewer" (brainstorm, round 2), checked against the code in the repository at the commit this brief was written from. Paths below are relative to its root.

**Mode:** BRAINSTORM. **Date:** 2026-11-28.

**Synthesized brief:** `pr-stacks-concepts_brief.md`. **Round-1 findings:** `pr-stacks-goldfish-findings.md`, left unchanged.

**Sources and conventions:**
- Findings are deduplicated across lenses, and each one lists every lens that raised it.
- Measured GitHub behaviour comes from `pr-stacks-sandbox-results.md`, cited as "sandbox §N".
- Anything about GitHub not covered there or in the API notes is marked **unverified**.
- The reviewers were fresh. Their inputs were the round-1 brief and findings as prior art, the sandbox results and the API notes.

## Reviewer Status

| Reviewer | Status |
| --- | --- |
| `egm-design` | completed |
| `egm-codebase-details` | completed |
| `egm-zoom-out` | completed |
| `egm-comprehension` | completed |
| `egm-implementation-readiness` | completed |
| `egm-structural` | completed |
| `egm-inventive-catalyst` | completed |

## Blockers

### B1. The `base_ref` fix as worded breaks every PR, and whether it is correct is unmeasured
* **Lenses:** all seven.
* **Evidence: the defect.**
  - `action.yml:51,87` set `BASE_REF: ${{ inputs.base || github.base_ref }}`.
  - `action.yml:45` checks `git rev-parse --verify "origin/${BASE_REF}"`.
  - `action.yml:82` passes `--base "origin/${BASE_REF}"`.
  - So putting a SHA into `BASE_REF` gives `origin/<sha>` in both places. The check exits 1, or `HasCommit` fails and the run falls back to API patches with an empty base (`cmd/reviewer/run.go:220-237`). Then the model lane skips (`internal/builtin/laneb.go:38-45`).
  - `run.go:214-219`: an explicit `--base` wins, and `--base` counts as unset only when it equals the literal `"main"` (`cmd/reviewer/flags.go:16,138`). So the Action has never used `pr.Base.SHA` for any PR (catalyst).
  - `self-review.yml:49` has the same defect.
* **Evidence: the merge commit (unverified).**
  - The Action diffs `git diff --merge-base <base> HEAD` (`internal/diff/diff.go:67-68`) against the checked-out test merge commit (ADR-0034:22-23).
  - For a stacked PR, it is unverified whether `refs/pull/N/merge` is built on the trunk, which the event's `base_ref` names, or on the parent branch.
  - Sandbox §7 measured the PR head, not the merge commit.
  - If the merge commit is built on the trunk, `--merge-base <base.sha> HEAD` pulls in trunk commits made since the stack forked.
* **Recommendation:**
  - Run E1 first (see Experiments).
  - Split the history check: keep `origin/` only for `inputs.base`, and check a SHA with `git cat-file -e "$SHA^{commit}"`. Readiness would keep the existing check on `origin/<base_ref>` unchanged.
  - Pick a base source (see D5).
  - Consider checking out `github.event.pull_request.head.sha` in `internal/installer/templates/review.yml.tmpl:20-24` and `self-review.yml`. That is correct whatever E1 shows, and makes the analysed lines match the head that comments are anchored to (`run.go:305-307`) (design, codebase).

### B2. The lifecycle of the snapshot is undefined
* **Lenses:** comprehension, design, structural, readiness, codebase, catalyst; zoom-out disputes that a snapshot object is needed (D1).
* **Evidence:**
  - "Taken when a stack page is opened" can mean once per page or once per visit to the stack.
  - Per page: about 120 calls each at 50 layers (sandbox §4), so about 6,000 to walk the stack, which is over the 5,000-an-hour budget. Pages side by side can also come from different stack states (design).
  - The service stores nothing. Data routes are keyed by one full SHA (`internal/workspace/api/router.go:84-91`; `api/source.go:31-39`), and memory goes after about 15 minutes idle (`docs/workspace.md:630`).
  - `Place.pull` holds only number, file and line (`web/src/router.ts:12-17`).
  - `App` remounts `PullView` for each PR (`web/src/App.tsx:113-114`).
  - `CodeView` re-reads the PR live (`web/src/views/CodeView.tsx:94-106`).
  - The term "stack page" is undefined: it could be a layer page or a combined page (comprehension).
* **Recommendation:**
  - One snapshot per tab per stack, held in `history.state` (`router.ts:106-117`) or in `sessionStorage` keyed by stack (pattern in `web/src/nav/closed.ts:10-38`). Never in the URL, where 50 SHAs take about 2 KB.
  - A new tab or a pasted link takes a fresh snapshot.
  - "Reload" must replace the stored snapshot explicitly.
  - The `CodeView` must receive the snapshot.
  - The server keeps no snapshot. Stack-wide requests carry it in a POST body.
  - Identity: the stack id plus the ordered (number, state, head SHA). Leave out `stack.base.sha`, which follows the trunk tip (sandbox §1: stack 38 `d1fa551` = `main`) (design, readiness).
  - Detection: `GET /stacks/{n}` on focus, every 60 s while visible, and before each question, about 60 calls an hour per tab (readiness).

### B3. The assistant is bound to one commit, and per-layer pages put that commit at the layer head
* **Lenses:** codebase, structural, design, readiness, zoom-out, catalyst, comprehension.
* **Evidence:**
  - `Subject` holds one SHA (`internal/workspace/assistant/assistant.go:84-97`).
  - `Segment.Path` means "at the commit the question is about" (`internal/workspace/gateway/gateway.go:29-30`).
  - The whole-file secrets scan reads `q.file(segment.Path)`, which is `subject.Commit.Text` (`gateway.go:549-552`, `assistant.go:126`).
  - Excerpts are stamped with the request SHA (`api/assistant.go:115`), and a selection from another commit is refused (`api/assistant.go:66-71`).
  - So a tip excerpt from a layer page is scanned against the wrong file, or not scanned at all, and stamped with the wrong commit.
* **Recommendation:** decide before the assistant stage. The options are in D4.
  - Either way: `stack_outline` returns titles and file counts, not 50 descriptions of up to 20k characters each (`assistant/tools.go:27`).
  - Add a layer-scoped `list_changed_files`.
  - The snapshot replaces the per-question `catalog.View` (`api/assistant.go:86`).
  - The tool count is asserted in `assistant_test.go:111`.

### B4. Labelling references by layer conflicts with computing line labels lazily
* **Lenses:** readiness; comprehension (M4), design (B3).
* **Evidence:**
  - Item 3 labels each tip reference with its layer. Up to 500 references can come back (`internal/workspace/navigation/query.go:29`), in files that are not open.
  - Item 4 computes line labels only when a file is opened.
  - With method (b), labelling 500 references costs the sum of touching layers over every reference file, which can reach thousands of calls.
* **Recommendation:** choose one:
  - file-level chips on references ("in a file changed by #104, #106"), with line labels only where they are already cached (zoom-out, comprehension);
  - prefilter to files that a layer above N touched (design, zoom-out);
  - a method that labels every file at once, such as composing adjacent-compare patches (readiness), or the touch-count split (catalyst).

### B5. New config keys break start-up on upgrade and on rollback
* **Lenses:** readiness.
* **Evidence:** every limit must be greater than zero (`internal/workspace/wsconfig/config.go:185-197`), and an unknown key is a parse error (`config.go:128-131`).
* **Recommendation:**
  - A missing `limits.stackLayers` (or `metadataConcurrency`, or `github.graphqlUrl`) means "stacks off", with a log line.
  - Release note: add the keys after the new version is stable, and remove them before rolling back.

## Major

### M1. "Layer base" has several meanings; only GitHub's matches GitHub's diff
* **Lenses:** comprehension, readiness, design, zoom-out, catalyst; structural and codebase proposed the alternative.
* **Evidence:**
  - Today's page uses merge-base(`pr.Base.SHA`, head) (`internal/workspace/catalog/pulls.go:79`).
  - When the layer below has been force-pushed without a restack, merge-base(new lower head, head N) differs from that, and the upper diff shows the lower layer's new commits reversed (sandbox §2).
  - Above a merged layer, `base.sha` is the trunk.
  - "Layer N head = layer N+1 base" holds only while the stack is up to date. Otherwise a page needs an extra archive (codebase, readiness, design).
* **Recommendation:**
  - The layer base commit = merge-base(`base.sha`, head), with both taken from `GET /stacks/{n}` (API notes 44-46). The page then matches GitHub and is still pinned to SHAs.
  - Staleness is a separate check.
  - State "at most four archives" as a cap, not as "always shared".

### M2. After a failed cascade, a merged layer's file shows as the upper layer's change
* **Lenses:** comprehension, catalyst, readiness, design.
* **Evidence:**
  - Sandbox §6: #15 was retargeted but not rebased, and GitHub's file list shows #14's `config.ts`.
  - Measuring from the trunk merge base (concept item 5) does the same.
  - #14's recorded head is still an ancestor of #15's head.
* **Recommendation:**
  - If the lower layer is merged and compare(recorded head, this head) is `ahead` with `behind_by` 0, label those files "already merged (#14)" and collapse them.
  - Show a banner: "not rebased since #14 merged; GitHub's automatic rebase hit a conflict; reviewer's Action does not run until it is rebased."
  - Reading #14's recorded head may need `refs/pull/14/head` (unverified).
  - Say on the page that its diff differs from GitHub's file list.

### M3. A layer page's head and file list come from live calls keyed by PR number
* **Lenses:** codebase, structural, design, catalyst.
* **Evidence:**
  - `catalog.View` reads `HeadSHA` from `GetPullRequest` and files from `ListFiles(number)` (`catalog/pulls.go:59,83,100`).
  - Those calls and `GET /stacks/{n}` are separate reads, so a push in between gives a page whose head differs from its snapshot.
* **Recommendation:**
  - Draw every layer page from SHA-keyed calls: archives, and Compare(layer base, head) for the file list.
  - Fill `headSha` and `mergeBaseSha` from the snapshot. Their meaning stays the same, so `DiffView.sources()` (`web/src/components/DiffView.tsx:23-38`) is unchanged.
  - If the PR read and the stack read disagree, show "stack changed" at once.

### M4. Tip references by SCIP symbol, not by position
* **Lenses:** all seven.
* **Evidence:**
  - `keyOf` returns `symbolKey{symbol}` for any symbol not prefixed `local ` (`navigation/load.go:166-171`).
  - `FindReferences` reads `ix.uses[keyOf(path, o.symbol)]` (`query.go:191-214`).
  - The text fallback is by word (`query.go:216-217`).
* **Recommendation:**
  - Add `SymbolsAt`/`UsesOf` (or `FindReferencesBySymbol`) per commit.
  - Take the symbol from the layer head's index (`query.go:76-98`) and look up its uses in the tip's index.
  - Check each use against the tip text, as `FindDefinition` does (`query.go:157-170`) (catalyst).
  - Local symbols, renamed symbols and version bumps (`navigation/scip/symbol.go:10-14`) fall back to version-insensitive matching, then to labelled text search.
  - Never report "no uses above" from an empty join.
  - Line mapping leaves the critical path for navigation.
  - Experiment E5 tests symbol stability.

### M5. Results carry no commit, so tip results would open at the wrong line
* **Lenses:** design, comprehension, codebase.
* **Evidence:**
  - `Location` has no SHA (`internal/workspace/api/nav.go:128-136`; `apitypes/types.go:201-242`).
  - `Context.sha` is "the commit … every target is in", and `targetHref` uses it for every link (`web/src/nav/navigation.ts:44-48,72-82`).
  - Reference rows show `path:line` only (`web/src/components/References.tsx:46-48`).
  - `CodeView` prints "At commit abc1234" and the single-PR badge "Outside this pull request's changes" (`CodeView.tsx:189,257-258`).
* **Recommendation:**
  - Add `sha` and `layer` to each location.
  - Group the results as "at #N's head", "added or changed above, at the tip: #104 (3)…", and optionally "removed above (#105)".
  - Drop tip results in files no layer above N touched.
  - The code view names its commit ("stack tip, head of #107") and uses the states "changed in #N / unchanged by the stack / unknown".

### M6. Methods (a) and (b) are mis-described, and a bake-off is needed
* **Lenses:** all seven.
* **Evidence for (a), GraphQL blame:**
  - `ghclient` already sends GraphQL (`internal/ghclient/write.go:89-95`, `client.go:169-202`). The workspace has none, and there is no `graphqlUrl` in `wsconfig` (`config.go:62-65`). Errors arrive inside a 200 (`write.go:162-165`).
  - Mapping commits to layers needs commit lists, which `Compare` throws away (`per_page=1`, `stream.go:122-138`).
  - "Breaks with squash" is mostly wrong: a cascaded squash-merged layer is below the trunk merge base, so it is base. Stale or rewritten history is what breaks it (design, zoom-out).
  - Blame gives labels in tip coordinates only.
* **Evidence for (b), single-file reads:**
  - It is the alternative ADR-0030 rejected: "two read paths that must agree on which files are text" (`docs/adr/0030-read-source-as-the-user.md:80-84`).
  - "Diffed in the service" contradicts `docs/designs/review-workspace.md:349`.
  - No Go text-diff exists, and `go.mod:5` has one dependency (ADR-0028).
  - `@codemirror/merge` exports `diff()` and `Chunk.build` and is already imported (`web/src/components/CodePane.tsx:5`).
* **Recommendation:**
  - Run E4.
  - If (b): diff in the browser; reuse `isText` and `FileBytes` (`internal/workspace/source/archive.go:125-143,190-198`); cache under `What: "file:"+path`; use its own load pool.
  - Option (c), composing adjacent-compare patches (readiness, zoom-out): the calls are already made, and missing patches show as "unknown".
  - Touch-count split (catalyst): k = 1 is free. For k ≥ 2, read only intermediate texts, deduplicated.

### M7. Line attribution as a standard-library-only package with a fixed output contract
* **Lenses:** structural, codebase, readiness.
* **Evidence:**
  - `internal/diff` runs git (`diff.go:15,116-117`).
  - `internal/workspace/rules_test.go:56-69` checks only direct `os/exec` imports.
  - The image has no git (`ci.yml:188-189`).
* **Recommendation:**
  - Create `internal/workspace/stackmap`, with a rules test that holds it to the standard library.
  - Output: per-line runs labelled `Layer(#n) | Base | MergedBase(#n) | OneOf{#a,#b} | Unknown`.
  - Methods are adapters that feed it.
  - Pin `MapForward(layerK, path, line) -> Mapped | Deleted{#j} | Changed{#j} | Unknown`. Follow `previous_filename`. A file deleted then re-added counts as `Deleted`. Results above a stale layer are approximate (readiness).

### M8. Four archives per page is more than the example cache, and small loads wait behind archives
* **Lenses:** all seven.
* **Evidence:**
  - Sizing rule: `docs/workspace.md:640-652`. `maxLoads = 2` is shared by every user (`source/cache.go:41-47,149-152`).
  - Eviction is silent (`cache.go:187-202`).
  - Example config: cache 1 GiB, archive 256 MiB, index 128 MiB (`examples/workspace.config.yaml:31-34`).
  - Middle layer: 4 archives plus 2 indexes ≈ 1.25 GiB.
  - A tip index loads the tip archive first (`navigation/fetch.go:145-153`).
  - `DiffView` refetches file by file (`DiffView.tsx:49-58`).
  - One archive of this repository is 2.2 MB (`docs/workspace.md:215-216`).
* **Recommendation:**
  - A layer page loads base and head. The tip loads lazily. The combined view is a separate address (`#view=stack`) using the tip and the trunk merge base. At most three archives per view.
  - Compares and single-file reads get their own pool.
  - No hard minimum-`cacheBytes` check, because it would reject the example config (readiness). Instead: a documented minimum, a start-up warning, and a runtime guard on the observed `Commit.Size()` (`archive.go:69`).
  - Update the comment at `cache.go:44-46`.

### M9. The content-hash store: easy to store, hard to account for
* **Lenses:** all seven.
* **Evidence:**
  - `Commit.text` is `map[string]string` (`archive.go:45-50,142`).
  - The archive exposes no blob SHA; the pax header is skipped (`archive.go:111`).
  - The cache charges a fixed cost per item (`cache.go:161,185-202`).
  - The "too large" refusal is permanent and must stay on logical size (`cache.go:94-97`, `archive.go:155`).
  - Repository isolation: `cache.go:13-20`, ADR-0030:89-91.
* **Recommendation:**
  - Defer. Measure with `kept_bytes` and a new `cache_evicted` log line first.
  - Cheap form: at unpack, reuse an identical string at the same path from a neighbouring commit of the same repository, or use `unique.Make` (Go 1.26, `go.mod:3`), and keep full charging.
  - Shared, reference-counted accounting, or the catalyst's cascade eviction, only if E10 shows a need.

### M10. Copied workflows are not fixed, and the rollout needs explaining
* **Lenses:** comprehension, design, catalyst, codebase, readiness, structural.
* **Evidence:**
  - Installs pin `adamtait/reviewer@v<ver>` together with `version:` and `checksum:` (`review.yml.tmpl:42-45`).
  - Workflows copied from `self-review.yml:49` pass `--base` explicitly, so a new binary does not fix them.
  - The `base` input description is "Defaults to the pull request's base" (`action.yml:19`).
  - With `resolveStaleThreads` (`internal/config/config.go:121-123`), old duplicate threads are resolved on the next clean run.
* **Recommendation:**
  - Release note: the symptom; "bump `uses:` and `version:` together"; "custom workflows: drop `--base origin/${{ github.base_ref }}`"; what happens to existing threads.
  - Add a CLI check when `pr.Stack` is set and `--base` names `origin/<stack.base.ref>`. Warn only (codebase, readiness, comprehension), or override and warn (catalyst, design). Needs `ghclient.PullRequest.Stack` (`client.go:40-60`).
  - Update `docs/architecture.md:95-99`.
  - No ADR. The fix restores ADR-0007's rule (`0007:28-30`) (structural, codebase).

### M11. Test 7 overstates the impact in one respect
* **Lenses:** readiness.
* **Evidence:** `CreateReviewComment` failures are logged as warnings (`internal/reporters/github.go:97-101`). GitHub probably refuses inline comments on lines outside the PR's diff (unverified for stacks).
* **Recommendation:** measure the real effects in E2: lower-layer findings in the summary; analyzer and model cost; a lower-layer secret tripping the gate on every layer above; runs of 422 warnings.

### M12. `reviewer watch` diffs the local `HEAD`, not the PR
* **Lenses:** codebase, readiness, catalyst, design.
* **Evidence:**
  - `watch` copies options and sets `--pr` (`cmd/reviewer/watch.go:33-39`), and never fetches or checks out anything.
  - `diff.Changed` diffs base against local `HEAD` (`diff.go:63-72`).
  - The patch fallback still runs analyzers over the local tree (`run.go:74-76`).
  - `--dry-run` only prints "would review" (`internal/poller/poller.go:96-101`).
  - Cascades bump `updated_at` on every upper PR (`poller.go:89-90`).
* **Recommendation:**
  - File the local-`HEAD` defect separately; it affects every PR.
  - Prediction from code (not run): on stack 16, `watch` would post #14's `config.ts` findings on #15.
  - Options: skip stacked PRs whose `mergeable_state` is `dirty` (design); store a hash of each PR's own diff in poller state and skip when unchanged (catalyst).
  - Run E3.

### M13. `ci.yml` uses of `base_ref` are not one fix
* **Lenses:** structural, design, codebase, readiness, catalyst.
* **Evidence:**
  - `ci.yml:96` is an eslint timing run with `|| true`.
  - `ci.yml:121` runs `checkadrs`, a gate.
  - `ci.yml:144` runs `audithistory`, a provenance gate.
  - Both tools accept a SHA (`tools/checkadrs/main.go:46,255`, `tools/audithistory/main.go:181,246`).
* **Recommendation:**
  - Split consumers into those that post (the Action, `self-review`) and those that gate.
  - Keep the gates on the trunk deliberately, with a comment (structural, design). Catalyst would change them; codebase calls it low priority.

### M14. The test fake cannot model stacks
* **Lenses:** readiness, structural.
* **Evidence:** `testgithub` compare answers only a PR's own pair and returns only `merge_base` (`internal/workspace/testgithub/testgithub.go:326-344`).
* **Recommendation:** plan it as its own task:
  - `SetCompare(base, head, {status, ahead, behind, mergeBase, files, patchMissing})`;
  - `/stacks/{n}` and `?pull_request=`;
  - `stack` in the PR JSON;
  - a contents or GraphQL route.

### M15. "Stack changed, reload" costs the reader work
* **Lenses:** comprehension, catalyst, design.
* **Evidence:**
  - A reload loses the conversation (`web/src/nav/conversation.ts:12-14`), the open lookup panel, scroll and selection.
  - The `#file=&line=` hash then points at different code.
  - Cascades take several steps: the retarget and the force-push arrived 3 s apart (sandbox §3).
* **Recommendation:**
  - Show a banner that does not block, naming what changed per layer ("#103 rebased, own change identical"; "#102 has 2 new commits"; "#101 merged").
  - Offer "load new snapshot", which carries the conversation over as text.
  - Grade it: at or below this layer versus above it (design).
  - Debounce, and mark a stack mid-cascade as "in transition".
  - Optionally offer compare(old head, new head) for each layer that moved (catalyst).

### M16. "Comment on GitHub" targets GitHub's current head
* **Lenses:** comprehension.
* **Evidence:** `commentUrl` builds `/files#diff-<hash>R<line>` (`web/src/github/links.ts:27-30`). From the combined view there is still no target PR (`web/src/views/PullView.tsx:180-187`).
* **Recommendation:** warn when the snapshot head is not GitHub's current head. Keep round-1 B4 for the combined view only.

### M17. Terms collide
* **Lenses:** comprehension, structural.
* **Evidence:**
  - "Session" means sign-in (`docs/workspace.md:160-168`).
  - "Base" has five meanings: the base branch (`PullView.tsx:407-408`), `base.sha`, the merge base, the event's `base_ref`, and `stack.base`. "Labelled as part of the base" adds a sixth.
  - "Introduced by", "added by" and "changed in" are used for the same idea. Blame gives "last changed in".
* **Recommendation:** use the glossary in the brief. Never use bare "base". Say "stacked on #101" in the header.

### M18. The trust rule assumes one commit on screen
* **Lenses:** structural, design.
* **Evidence:** "accepted only … for the commit on screen" (`review-workspace.md:355`, `navigation/load.go:6-12`). The trust table has five rows (`review-workspace.md:257-265`).
* **Recommendation:**
  - Reword to "the commit the result is located in".
  - Add stack membership as a row, from GitHub and used for labels only.
  - State that tip-index answers are controlled by whoever pushed the tip.

### M19. Observability
* **Lenses:** codebase, readiness, design.
* **Evidence:**
  - Eviction writes no log (`cache.go:191-199`).
  - Log fields hold one pull and one SHA (`obs/log.go:62-65`, `api/pulls.go:101,107`).
  - `navigation_reported_wrong` has no layer and no answering commit (`api/nav.go:151-179`).
* **Recommendation:**
  - Log `stack_loaded` with layers, merged, stale, calls, unknown labels and tip index state.
  - Log `cache_evicted`, a `stack_changed` counter and `labels_imprecise`.
  - Add a `stack` field on `pull_view`.
  - Record the answering commit on tip results and on reports of wrong results.

### M20. Compare file lists: completeness and decode cost
* **Lenses:** design, structural, readiness.
* **Evidence:**
  - `ghclient` notes "up to 300 files" for compare (`stream.go:128-129`). Pagination beyond that is unverified, and sandbox §5 saw only `rel="first"`.
  - Decoding 49 compares with patches is outside the sizing rule.
* **Recommendation:**
  - Add `CompareRange` returning status, ahead/behind, merge base and paginated files. Leave `Compare` cheap for `catalog.View`.
  - Mark a truncated list "unknown".
  - Bound concurrent decodes, and drop patches unless method (c) needs them.

## Nits

* **N1.** `stack.base.sha` follows the trunk. Never include it in the snapshot's identity. (Readiness, design.)
* **N2.** `defaultBase = "main"` makes an explicit `--base main` count as unset in `--pr` mode (`flags.go:16`, `run.go:217`). Use `flag.Visit`, or document it in the same release note. (Catalyst, zoom-out, structural, comprehension.)
* **N3.** `run.Base = o.base` is the raw flag, not the resolved base (`run.go:43`). After the fix, the report header and `laneb/assemble.go:99` will show a SHA. Pass the resolved base and shorten it for display. (Codebase.)
* **N4.** `workflow_dispatch`, `push` and `merge_group` have no `pull_request`, so `PR_NUMBER` is empty (`action.yml:89`). This predates the fix. Give a clear error instead of the fetch-depth message. (Design, codebase.)
* **N5.** `IndexStatus` covers only `headSha` (`PullView.tsx:452`). Show the tip's index status next to "added above" results. (Comprehension.)
* **N6.** Archive bytes may differ from git blobs (`export-subst`, `eol`; unverified for GitHub). A computed blob mismatch must mean "differs", never "same". (Catalyst.)
* **N7.** Show stale and merged as states of a file chip, not as "unknown". (Catalyst.)
* **N8.** Write "the tip's index covers every open layer", not "indexes know about all changes". (Zoom-out.)
* **N9.** Polling per layer page (layer-head index, tip index, stack) is about 720 calls an hour at 15 s, within budget. State it. (Structural.)
* **N10.** Merged-layer chips cost one `ListFiles` per merged layer. (Readiness.)
* **N11.** A strip entry for a merged layer reads "#14 · merged into stacks/trunk (squash)", avoiding "base". (Comprehension.)
* **N12.** Make the new TypeScript fields optional, and add a stacked `pull_view` example under `internal/workspace/api/testdata/examples`, so the web keeps working after a server rollback. (Readiness.)

## Disagreements between lenses

* **D1. Is a snapshot object needed?**
  - Zoom-out: no. Every page is already pinned to SHAs, so stack data is a field of `pull_view`, and "reload" can wait until after v1.
  - All other lenses: yes, held in the browser.
* **D2. How is the snapshot checked?**
  - Design and readiness: the server re-reads `/stacks/{n}`, remembered for 10-15 s, and answers 409 `stack_changed`.
  - Structural: a client claim, where a forgery only mislabels the forger's own screen, and detection happens in the browser.
* **D3. Line-label method.**
  - Design and comprehension: (b) in the browser.
  - Readiness: (c), with a bake-off.
  - Zoom-out: file-level now; (a) or (c) later.
  - Catalyst: the touch-count split.
  - Structural: adapters behind a fixed contract.
* **D4. The assistant's commit.**
  - Option A (zoom-out, catalyst, readiness): `Subject` is the tip; selections are restamped when the file is identical or untouched above, otherwise refused.
  - Option B (design, codebase, structural): `Segment.SHA` and `Call.File(sha, path)`, amending ADR-0031.
* **D5. The Action's base source.**
  - Drop `--base` and use the REST `base.sha` (zoom-out, structural, catalyst).
  - The event's `base.sha`, because the REST value can be newer than the checkout (design, readiness).
  - Gate on stack membership, so unstacked PRs are unchanged (readiness).
* **D6. The CLI check for a trunk `--base`:** override and warn (catalyst, design), or warn only (codebase, readiness, comprehension).
* **D7. The layer base commit:** GitHub's merge base (comprehension, readiness, design, zoom-out, catalyst), or merge-base(snapshot lower head, head) (structural, codebase).
* **D8. ADR-0030:** amend it in place (codebase, readiness), or write a new ADR while 0030 is on trial (structural).

## Experiments

| # | Experiment | Pass when | Raised by |
| --- | --- | --- | --- |
| E1 | In `pr-probe`, print `HEAD^1 HEAD^2`, the event's `base.sha`, `toJSON(github.event.pull_request.stack)`, and `git diff --name-only --merge-base $BASE_SHA HEAD`. Run after moving `stacks/trunk`, on bottom, middle and top layers and an unstacked control | Each layer's list equals `/pulls/N/files`; control unchanged | all seven |
| E2 | Fix regression: script test of the base choice; real reviewer on a new 3-layer open stack | Upper layer: own files only. Control output identical | readiness, zoom-out |
| E3 | `reviewer --pr 15` on stack 16, in a full clone at #15's head and in a clone without its `base.sha` | Records whether `config.ts` is reviewed | readiness, codebase |
| E4 | Line-label bake-off (a, b, c, touch-count) on stacks 29, 38 and a synthetic 50-layer stack, against a git oracle | 0 wrong labels; ≤2% unknown; ≤3 calls per file at p50; cost of 500 references reported | readiness, structural |
| E5 | SCIP symbol stability across layer heads, including a version bump | Join hit rate reported | structural, codebase, catalyst, design |
| E6 | Tarball for a pre-restack SHA | Served, or mapped to "stack changed" | readiness, catalyst |
| E7 | Token with exactly the four workspace permissions | `/stacks` readable | carried over from the sandbox |
| E8 | Adjacent compare over 300 files | Truncation detectable | structural, readiness, design |
| E9 | Single-file read limits; `export-ignore`; `If-None-Match` 304 on `/stacks` | Recorded | readiness |
| E10 | Heap against `Commit.Size()`, four archives of a ~50 MB repo, with and without string reuse | Decides on the content-hash store | readiness |
| E11 | `base.sha` after the upper layer is pushed without a restack | Confirms the refresh rule | carried over from the sandbox |

## Round-1 claims now obsolete or wrong

* **Obsolete after measurement:**
  - B1 (version pin, per-request header, unknown permission, unstacked 404) — sandbox §1.
  - "`base.sha` may follow the lower head" — sandbox §2.
  - M3 (no index after a cascade) — sandbox §3.
  - B5's "12,000 calls an hour" and the "60 calls or fewer" target — sandbox §4.
  - Unknown compare limits — sandbox §5.
  - The CLI squash re-report dispute, `LayerBase`, and superseding ADR-0007 — sandbox §6-7.
  - Spikes 1-6 and S0, S1's version header, S6.
  - "Every statement about GitHub is unverified".
* **Wrong:** "`--pr` already scopes each layer" (round-1 brief Agreement 6 and C4). The Action overrides it (sandbox §7, `run.go:214-219`).
* **Overruled by the author:**
  - The session at the tip with two archives (the round-1 Ideal Final Result).
  - Navigation and the assistant always at the tip.
  - The link-out option.
  - File labels as the end state.
* **Reopened:** "multi-commit assistant `Subject`, rejected" (D4). Per-file reads (method b) against ADR-0030.
* **Narrowed:**
  - Round-1 B4 ("Comment on GitHub" has no target) now applies to the combined view only.
  - Ref chaining is a fallback only.
  - The staleness formula needs the merged-neighbour rule.
  - `stackmap.Compose` from compare patches survives only as option (c), on adjacent compares.
* **Still valid:**
  - The combined file list comes from archives.
  - A failed stack read must not reach `s.fail` (`api/pulls.go:45-74`).
  - 429 handling.
  - Additive API and poller fields.
  - Never extend the comment marker.
  - The conversation is lost on a layer switch.
  - The misleading "outside this PR" badge.
  - "Unknown, never unchanged".

## Strongest ideas by lens

* **Design:**
  - A per-tab snapshot that the server checks; facts for SHA pairs cached; a graded banner.
  - Method (b) in the browser, with references joined by symbol in two lists.
  - The Action fix with the `head.sha` checkout, two variables and a narrow CLI check.
* **Codebase:**
  - Symbol-keyed tip references.
  - A snapshot in `sessionStorage`, plus a snapshot-pinned `PullView`.
  - Labels from `@codemirror/merge` `Chunk.build`, where `precise:false` means unknown.
  - The split history check, and `watch` treated as its own fix.
* **Zoom-out:**
  - No snapshot object.
  - Cross-stack answers from the assistant at the tip plus `stack_outline`, with no line mapping.
  - Fix the Action by deleting `--base`, after one probe.
* **Comprehension:**
  - Every location carries its commit and role.
  - An old snapshot keeps working, behind a banner that does not block.
  - Method (b) labels with four visible states.
  - Failed-cascade files collapsed as "already merged (#14)".
* **Readiness:**
  - A stack-gated Action base.
  - Labels from adjacent compares fetched once per snapshot.
  - Rollout safety: absent config means off, and a runtime memory guard.
* **Structural:**
  - A layer page is today's page with SHAs from the snapshot, which the browser carries.
  - A symbol join across indexes.
  - A standard-library-only `stackmap` with a fixed label contract and swappable adapters.
* **Catalyst:**
  - Symbol-keyed references, checked against the tip text.
  - The touch-count split for labels.
  - A snapshot built only from SHA-keyed calls, plus string reuse with a neighbouring commit.
  - The CLI stops overriding the PR's own base.
