<!-- SPDX-License-Identifier: MIT -->
# Goldfish findings: PR stack support in reviewer

**Target:** FeatureConcept "PR stack support in reviewer" (brainstorm), grounded in
the repository at the commit this brief was written from. Paths below are relative to its
root.
**Mode:** BRAINSTORM. **Date:** 2026-11-27. **Synthesized brief:** `pr-stacks-concepts_brief.md`.

Findings below are deduplicated across lenses. Each lists every lens that raised it. Statements
about GitHub behaviour are unverified unless they cite the API notes
(`docs/designs/briefs/pr-stacks-github-api-notes.md`).

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

### B1. Stack discovery cannot run today, and its permission is unknown
* **Lenses:** all seven.
* **Evidence:** `internal/ghclient/client.go:29` (`APIVersion = "2022-11-28"`), applied at
  `client.go:381` and `stream.go:45`, and asserted at `client_test.go:64`. Stacks need `2026-03-10`
  (API notes 35). `PullRequest` has no `stack` field (`client.go:40-60`), and `Ref` has no repo
  (`client.go:73-76`). The permission is unknown (API notes 50-51). The service cannot inspect the
  token's permissions (`docs/workspace.md:127-128,148`). A 404 through `s.fail` becomes a page-wide
  not-found (`internal/workspace/api/pulls.go:51-52,66-69`).
* **Recommendation:** set the version per request, only on stack calls, and keep the global pin.
  Make the stack lookup optional: on 400/403/404, show the single-PR view with a distinct reason and
  never call `s.fail`. Run the sandbox test before any design work. Check whether `GET /pulls/N`
  already carries `stack` under the old version; if so, `catalog.View` (`pulls.go:59`) detects a
  stack at no extra cost. Fallback: chain refs through the open-PR list (`pulls.go:33-49`).

### B2. The session unit is undecided; everything assumes one commit
* **Lenses:** structural, comprehension, design, codebase, readiness.
* **Evidence:** every data route is keyed by one `{sha}` (`internal/workspace/api/router.go:84-91`).
  `Subject` is one commit (`internal/workspace/assistant/assistant.go:84-97`), and the tools are
  bound to it (`tools.go:32`). `PullView` has one `MergeBaseSHA` (`catalog/pulls.go:93-107`). The
  sizing rule assumes "a head commit and its merge base" (`docs/workspace.md:642`). The web
  `Context.sha` is one value (`web/src/nav/navigation.ts:44-51`), and so is `IndexStatus`
  (`web/src/views/PullView.tsx:452`).
* **Recommendation:** decide that a session is an immutable stack snapshot keyed by the ordered
  (PR, head SHA) tuple, and answer "stack changed, reload" on mismatch. Navigation and the assistant
  run at the tip. Every result's "At commit" line (`CodeView.tsx:257`) says "(stack tip)" or
  "(head of #N)".

### B3. Per-layer diffs as specified need two archives per layer
* **Lenses:** design, codebase, readiness, catalyst, comprehension, zoom-out.
* **Evidence:** `DiffView` reads whole texts at two SHAs (`web/src/components/DiffView.tsx:24-36`).
  File text comes only from archives (`internal/workspace/api/source.go:45,81-98`). `maxLoads = 2`
  (`internal/workspace/source/cache.go:41-47`). Sizing: `docs/workspace.md:640-650`; the example
  1 GiB cache holds about 4 worst-case archives.
* **Recommendation:** keep the tip and the trunk merge base as full trees. For layer diffs, choose
  one of: link out to the layer's existing PR view (zoom-out, codebase, readiness); compare patches
  with no context expansion (design); or per-file reads (catalyst), which collide with ADR-0030
  (`docs/adr/0030-read-source-as-the-user.md:80-84`) and need a new ADR. Record the choice in an ADR.
  If layer archives are loaded lazily, keep at most about four resident and state a minimum
  `cacheBytes`.

### B4. "Comment on GitHub" from the combined diff has no target PR
* **Lenses:** comprehension.
* **Evidence:** the comment action uses `pull.webUrl` (`web/src/views/PullView.tsx:180-187`).
* **Recommendation:** target the PR of the layer that introduced the line. If that layer is stale
  and the line is not in its own diff, disable the action and show the reason.

### B5. The rate budget at 50 layers is unknown, and secondary limits are not handled
* **Lenses:** readiness, design, codebase.
* **Evidence:** index-status polling runs every 15 s per commit (`docs/workspace.md:273`); for 50
  layers that is about 12,000 calls an hour. Only a 403 containing "rate limit" is recognised
  (`client.go:405-406`); a 429 and `Retry-After` are ignored. Nothing bounds concurrent JSON calls.
  Each question already runs `catalog.View` (3 calls, `api/assistant.go:86`); done per layer, that
  is up to 150 calls.
* **Recommendation:** poll only the tip and the layer on screen. Add a separate bounded semaphore
  for metadata calls, plus `limits.stackLayers` and `limits.metadataConcurrency` with no defaults
  (`internal/workspace/wsconfig/config.go:7-8`). Handle 429. Spike the cost of opening a stack and
  set a target, for example 60 calls or fewer.

## Major

### M1. No line-to-layer code exists, and the obvious parts to reuse get it wrong
* **Lenses:** codebase, design, catalyst, readiness, zoom-out.
* **Evidence:** `diff.FromPatches` (`internal/diff/diff.go:181-205`) records each hunk's whole
  new-side range, context lines included (`internal/diff/parse.go:46-47,100-111,144`). A missing
  patch becomes Binary with no ranges (`diff.go:184-189`). `diff.File` keeps only new-side ranges
  (`diff.go:40`). `Compare` asks for `per_page=1` and discards files (`ghclient/stream.go:128-137`).
  `internal/diff` runs git (`diff.go:116-117`), and the workspace forbids starting programs
  (`internal/workspace/rules_test.go:52-69`).
* **Recommendation:** add a pure package (`internal/stackmap`) with
  `Compose([]LayerHunks) FileMap` and `MapForward(layer, path, line)`. Write a new two-sided hunk
  parser that counts only `+`/`-` lines and has an explicit "unknown" label. Cache results by SHA
  pair. Alternatives: a blame chain over per-file texts at each layer head that touches the file
  (catalyst); GraphQL blame on the tip with a commit-to-layer map (readiness); per-file labels only
  in v1 (zoom-out). "The service computes no diff" (`docs/designs/review-workspace.md:349`) needs
  a recorded change.

### M2. The proposed staleness detector is weak
* **Lenses:** design, codebase, readiness, catalyst, comprehension, zoom-out (zoom-out accepts it
  as a free warning).
* **Evidence:** the minimal list has no `base.sha` (API notes 39-43); only `GET /stacks/{n}` has it
  (44-46). The catalyst suspects that PR N's `base.sha` follows N-1's current branch head whether or
  not N was rebased (unverified). `Compare` returns the merge base (`stream.go:120-138`), and
  `catalog.View` already computes it (`pulls.go:79`).
* **Recommendation:** layer N≥2 is up to date when merge-base(head N-1, head N) == head N-1.
  The compare's `status`/`behind_by` gives the same signal at no extra cost. The bottom layer is
  never stale because trunk moved on. Also warn when the combined diff's merge base is older than
  the newest merged layer: after a squash merge, merged changes can reappear (zoom-out). Optionally
  show the exact "not yet cascaded" diff, compare(merge-base(K-1, K), head K-1) (catalyst).

### M3. The tip may have no index after a cascading rebase
* **Lenses:** design, codebase, readiness, catalyst, zoom-out.
* **Evidence:** an index is found only through a push run for that exact SHA
  (`internal/workspace/navigation/fetch.go:131,215`) and is checked against that commit's archive
  (`fetch.go:145-148,298`). Pushes made with a workflow's token start no run
  (`docs/adr/0034-scip-index-built-on-push.md:47-48`). Runs take about 57 s (`ADR-0034:45-46`). The
  index runs on every branch push (`internal/installer/templates/review-index.yml.tmpl:14-16`).
  Caps: 500 references from the index (`navigation/query.go:29`), 30 per tool call (`tools.go:26`).
* **Recommendation:** sandbox server-side cascades, local `gh stack` rebases and bottom-layer
  merges. Design the "tip index pending or missing" state up front. Only the tip's index matters
  if navigation runs at the tip.

### M4. The assistant is bound to one commit; cross-stack questions need tools, not a new subject
* **Lenses:** codebase, design, zoom-out, readiness, comprehension.
* **Evidence:** `gateway.Segment.Path` means "at the commit the question is about"
  (`internal/workspace/gateway/gateway.go:29-30`). The secrets scan reads files at that commit
  (`gateway.go:62-65,364-367`). Excerpts are stamped with the request SHA, and selections from
  other commits are refused (`api/assistant.go:69,115`). `listChangedFiles` covers one PR
  (`tools.go:368-385`). Descriptions can be up to 20k characters each (`tools.go:27`). The tool
  count is asserted in `assistant_test.go:111`.
* **Recommendation:** keep `Subject` on the tip. Add `stack_outline`/`list_stack_layers` and a
  layer-scoped `list_changed_files(layer)` / `read_description(layer)`. Add layer labels to
  `find_references`/`find_definition` output (`tools.go:270-357`). The prompt tells the model to
  name the layer for every claim and to flag stale layers. A question asked from a lower layer's
  view must not quietly answer from the tip while the screen shows the lower layer (zoom-out): ask
  cross-stack questions from the whole-stack view.

### M5. Combined-diff file list has no source
* **Lenses:** codebase, design.
* **Evidence:** `ListFiles` works per PR, capped at 3,000 (`pulls.go:104-105`). Merging the
  per-layer lists mishandles renames and files added then deleted.
* **Recommendation:** compute the list in memory from the two archives
  (`internal/workspace/source/archive.go:53-69`). Show `too_large`/`binary` entries as "unknown".

### M6. The trust rule and index binding assume one commit on screen
* **Lenses:** structural, design, catalyst, comprehension.
* **Evidence:** trust table at `docs/designs/review-workspace.md:257-265`. An index is accepted only
  "for the commit on screen" (`review-workspace.md:355`). Provenance labels are in
  `web/src/nav/navigation.ts:16-21`.
* **Recommendation:** stack membership and order become a sixth kind of data, trusted because it
  comes from GitHub. Layer labels come from GitHub data only, never from the index. The layer chip
  is kept separate from the provenance label. Results name which commit's index answered, because
  different people push different layers.

### M7. The CLI re-report claim is disputed; a shared model is premature
* **Lenses:** design, codebase, zoom-out, readiness, structural, catalyst.
* **Evidence:** `--pr` uses `pr.Base.SHA` with `git diff --merge-base` (`cmd/reviewer/run.go:216-222`,
  `internal/diff/diff.go:63-72`). The API-patch fallback is at `run.go:225-237`. The Action passes
  `--base origin/<base_ref>` (`action.yml:82,87`). Dedupe is per PR
  (`internal/reporters/github.go:76,172-183`, `summary.go:97`). Fingerprints exclude line numbers
  (`internal/fingerprint/fingerprint.go:95-107`). `watch` re-reviews on any `updated_at` change
  (`internal/poller/poller.go:89-90`) and never checks out the PR head (`run.go:220-222`). The
  workflow listens to default `pull_request` events only (`review.yml.tmpl:9-10`).
* **Positions:**
  * Catalyst and structural: re-reporting is real when a squash-merged lower layer leaves a
    re-targeted upper PR not yet rebased. Fix: `LayerBase(stack, n)` (layer N-1's last head
    SHA), plus stack-wide dedupe by fingerprint.
  * Codebase: the Action's merge-commit checkout hides it, except on the patch fallback.
  * Zoom-out: a re-target alone starts no run.
  * Design: the real cost is up to 49 `watch` model runs per cascade that post nothing new. Fix:
    store a hash of each PR's own diff in `state.Seen` (`poller.go:48`) and skip when unchanged.
* **Recommendation:** spike the squash-merge case first. Share at most the stack metadata type and
  the ghclient method. Do not share attribution. Do not make the CLI call `/stacks` until it is
  known whether `GITHUB_TOKEN` can read it. Any change to the CLI's base needs a superseding ADR
  for ADR-0007 (`docs/adr/0007-scope-analysis-to-changed-lines.md:28-30`), which is Accepted and
  frozen.

### M8. URL, current layer and moving around 50 layers
* **Lenses:** comprehension, codebase.
* **Evidence:** `Place.pull` holds only number, file and line (`web/src/router.ts:12-17`). Hash
  parsing is at `router.ts:46-50`. Keys are crowded (`web/src/keys/keymap.ts:35-44`), and
  keymap.ts:3-5 requires touch routes. `PullList` prints one row per PR (`PullList.tsx:45`).
* **Recommendation:** the opened PR is the current layer and defaults to its own diff. Switching
  layers navigates to `/pull/<n>`. The combined view uses `#view=stack&file=…&line=…`. No stack
  number in the URL. Add a layer rail showing `#103 title · 4 files · state · stale icon`, a `g s`
  picker, and next/previous layer keys with touch routes. Add per-file layer chips and a
  "touched by #N" filter in the combined diff. Group stacks in the pull list.

### M9. Switching layers loses the assistant conversation
* **Lenses:** comprehension.
* **Evidence:** the conversation lives in the page (`web/src/nav/conversation.ts:9-14`) and is keyed
  by PR (`PullView.tsx:66,77`).
* **Recommendation:** key the conversation and panel by stack while the reader stays in one stack.

### M10. Reference labels, badges and lines changed by two layers
* **Lenses:** comprehension.
* **Evidence:** reference rows show only `path:line` (`web/src/components/References.tsx:46-48`). The
  "Outside this pull request's changes" badge checks one PR's files (`CodeView.tsx:189,258`).
* **Recommendation:** add a layer chip (`#103`, `#103 · stale`, `main`, `main (via #100)`). Group
  references by layer, bottom to top, with "unchanged by the stack" last. Use three badge states:
  changed in this layer, changed in #N, not changed by the stack. In a layer view, mark lines a
  later layer rewrites as "changed again in #104".

### M11. Staleness and failure messages
* **Lenses:** comprehension, zoom-out, structural.
* **Recommendation:** put the stale icon in the rail, a banner on the stale layer, and a chip on
  every reference and excerpt drawn from it. Example text: "#105 is based on an older version of
  #104 (based on def5678; #104 is now at abc1234). This diff may show changes from #104, or miss
  them. Navigation uses the stack tip." Keep one explicit state set owned by the snapshot:
  consistent, out of date, merged, closed without merging, truncated (over the layer cap). Write
  "the tip wins" as a rule of the attribution function, not as display logic.

### M12. Observability
* **Lenses:** codebase, zoom-out.
* **Evidence:** rate-limit headers are read only on a 403 (`client.go:405-406`). The only fetch
  telemetry is `Store.Observe` (`internal/workspace/source/store.go:37-40`). Log fields hold one
  pull and one SHA (`api/pulls.go:101,107`). The wrong-report path is at
  `internal/workspace/api/nav.go:151-179`.
* **Recommendation:** log one `stack_loaded` line with layer count, merged count, stale layers,
  GitHub calls, unattributed lines and files, tip index state and `X-RateLimit-Remaining`. Add
  the layer and the head-SHA tuple to `navigation_reported_wrong`. Log distinct
  `stack_unavailable_forbidden` and `stack_unavailable_notfound` outcomes. Add a `stack` field to
  `pull_view` so review time per stack can be measured from existing queries
  (`docs/workspace.md:326-343`).

### M13. Persisted formats must stay compatible
* **Lenses:** readiness.
* **Evidence:** the marker regex reads one optional token after the fingerprint (`fingerprint.go:53`).
  The poller reviews every PR again if it cannot read its state (`poller.go:44-49,58-65`). API
  types are mirrored by hand in `internal/workspace/api/apitypes/types.go:6-9`,
  `web/src/api/examples.test.ts:75` and `internal/workspace/api/testdata/examples/pull_view.json`.
  `Location`/`References` have no layer (`types.go:201-242`).
* **Recommendation:** never extend the comment marker; put the layer in visible text. Add poller
  state fields only additively. Add API fields only additively; `headSha`/`mergeBaseSha` keep
  their meaning.

### M14. Builds on decisions still on trial; documents and ADRs to change
* **Lenses:** structural.
* **Evidence:** ADRs 0029-0034 are `Proposed` (`docs/adr/README.md:46-51`), to be accepted after
  the second feedback week (`review-workspace.md:510`). `tools/checkadrs/main.go:271` freezes only
  Accepted bodies. The next ADR number is 0035 (README:51).
* **Recommendation:** add new Proposed ADRs: session = stack snapshot; attribution as a pure
  function with the tip as truth; how layer contents are read; the trust rule for stack data;
  and, only if the CLI proceeds, an ADR superseding 0007. Note that old reports reused
  0035/0036 as draft numbers. Decide whether stack work waits for the feedback weeks. Add a Terms
  section to the brief (as the auto-merge brief was asked to do).

### M15. Merged layers per line are expensive
* **Lenses:** design, zoom-out, readiness.
* **Recommendation:** merged code lives in trunk, outside the combined diff. Label merged layers per
  file and per PR in v1 ("also changed by merged #99"), using `merged_at` (API notes 42). Do not
  fetch archives for merged layers.

## Nits

* **N1.** Define the terms: layer, trunk, tip, combined diff, introduced by, merged layer, source of
  truth, stale, snapshot. (Comprehension, structural.)
* **N2.** Index status per layer: show it once for the tip, and say on each layer-head lookup
  whether it fell back to text search. (Comprehension.)
* **N3.** Label inferred stacks (branch-on-branch, GHES) "inferred from branches". (Comprehension.)
* **N4.** The CLI should use the same chip text as the workspace (`#103`, `main (via #100)`).
  (Comprehension.)
* **N5.** `source.Cache` `Key` holds one SHA (`cache.go:21-26`). Put a SHA pair in `What`, or add a
  field. Do not key anything by stack number, because a stack changes. (Design, codebase.)
* **N6.** GHES needs no special path: `BaseURL` is configurable (`client.go:160-163`). It only needs
  the B1 fallback. An unknown API version on GHES must quietly mean "no stack". (Codebase,
  readiness, design.)
* **N7.** Branch-on-branch: derive it from the single `ListOpenPullRequests` call (`pulls.go:34`,
  cap 300) by matching `base.ref` to `head.ref`. Keep the model linear and refuse trees. (Design,
  zoom-out, catalyst.)
* **N8.** `review-workspace.md:15` ("Nothing described here is built yet") is stale. The
  "Branch views" Non-Goal (`review-workspace.md:145`) should be cross-referenced. (Structural,
  zoom-out.)
* **N9.** A merged layer's branch may be deleted; reading it may need `refs/pull/N/head`.
  (Catalyst.)
* **N10.** Cross-fork stacks are unsupported (API notes 17-18), so the provider boundary still
  holds even though more authors' text reaches the model. (Design.)
* **N11.** Ask for evidence of real stack sizes before treating 50 layers as a design driver.
  (Zoom-out.)

## Strongest ideas by lens (for reference)

* **Design:** one tree, one index, a coordinate map per layer. Labels are added as a post-pass.
  The poller skips a PR when its own diff hash is unchanged.
* **Codebase:** compute the combined file list from the archives. The assistant stays on the tip
  with metadata tools. The stack-unavailable path must not call `s.fail`.
* **Zoom-out:** the first slice is a stack strip, a whole-stack view at the tip, per-file labels
  and one assistant tool, with constant memory and no CLI change.
* **Comprehension:** decide which commit every lookup uses. Comments target the introducing
  layer's PR. The conversation is keyed by stack.
* **Readiness:** spikes with written acceptance tables. Version header per request. Poll only
  the tip and the layer on screen.
* **Structural:** session = immutable stack snapshot. Critical path: discovery, snapshot, diffs,
  attribution, navigation, assistant; the CLI is a parallel branch.
* **Catalyst:** the tip is the whole universe and a layer is a range over it. Staleness by
  ancestry. The "not yet cascaded" diff. `LayerBase` as the only CLI change.
