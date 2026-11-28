<!-- SPDX-License-Identifier: MIT -->
# PR stacks: sandbox test results

Run on 2026-11-27 and 2026-11-28 in two parts.

**Read-only, against `adamtait/reviewer`.** The repository already holds three GitHub-native
stacks:

| Stack | Layers | State | Pull requests, bottom to top |
| --- | --- | --- | --- |
| 38 | 4 | open | #31, #36, #37, #42 |
| 29 | 10 | open, all drafts | #26, #28, #30, #32, #33, #34, #35, #39, #40, #41 |
| 3 | 9 | all merged by squash, 2026-09-14 | #1, #2, #4, #5, #6, #8, #9, #10, #11 |

**With writes, against `adamtait/reviewer-sandbox`.** Another session was using the sandbox's
`main` at the same time, so these tests use their own trunk, `stacks/trunk`, and branches under
`stacks/`. The trunk holds a small TypeScript project and two workflows:

- `review-index.yml`: the workspace's index workflow, triggered by `push` to any branch.
- `pr-probe.yml`: the same trigger as reviewer's `self-review.yml` (`pull_request` with the default
  activity types). It prints what `reviewer --base "origin/${{ github.base_ref }}"` would review,
  without running reviewer: `base_ref`, `base_sha`, the commits and the changed files.

| Stack | Layers | Pull requests | Used for |
| --- | --- | --- | --- |
| 13 | 3 | #10 `a1-order` → #11 `a2-pricing` → #12 `a3-api` | tests 2, 3, 7 |
| 16 | 2 | #14 `b1-config` → #15 `b2-feature` | test 6 |

Both stacks were built and merged with `gh stack` 0.3.0 (`init`, `add`, `submit`, `rebase`,
`push`, `merge --squash`).

The token was the session's fine-grained `GH_TOKEN`, which holds more permissions than the
workspace's four. Where the permission matters, the result relies on GitHub's
`X-Accepted-GitHub-Permissions` response header, which names what the endpoint requires.

Test numbers 1 to 6 match "Sandbox experiments" in
[`pr-stacks-concepts_brief.md`](pr-stacks-concepts_brief.md). Test 7 was not planned; it came
out of test 3's probe logs.

## Summary

| # | Test | Result |
| --- | --- | --- |
| 1 | Discovery and permission | **Answered.** Works under the client's pinned API version and needs only `pull_requests=read`. An unstacked PR costs no extra call. |
| 2 | `base.sha` semantics | **Answered.** `base.sha` does not follow the base branch, and no refresh rule could be pinned down. An ancestry check through `Compare` catches a stale layer straight away. |
| 3 | Index runs after a cascade | **Answered: yes.** GitHub's cascade force-pushes fire `push` events, and the rebased heads got successful index runs within 3 seconds. |
| 4 | Rate cost | **Answered.** About 2.4 calls per layer to open a stack. Not a constraint if only the tip's index is polled. |
| 5 | Compare limits | **Answered, and worse than assumed for combined ranges.** No adjacent-layer compare lost a patch. The combined compare of a 9-layer stack lost about 100 of 232. |
| 6 | CLI after a squash merge | **Answered.** A successful cascade is reviewed correctly. A failed cascade leaves the PR retargeted, unrebased and conflicted, and GitHub runs no `pull_request` workflow until the conflict is resolved. |
| 7 | `github.base_ref` on stacked PRs | **New, and a defect in reviewer today.** For a PR in a stack, the event's `base_ref` is the stack's trunk, not the parent branch. reviewer's Action passes it as `--base`, so every upper layer is reviewed together with every layer below it. Of three candidate fixes, only diffing against the test merge commit's first parent (`HEAD^1`) was correct (7a). |

## 1. Discovery and permission

- `GET /repos/{o}/{r}/stacks`, `GET /stacks?pull_request=N` and `GET /stacks/{n}` all return 200
  under both `X-GitHub-Api-Version: 2022-11-28` (the version `internal/ghclient` pins) and
  `2026-03-10`. Responses are identical under both.
- Every stacks response carries `X-Accepted-GitHub-Permissions: pull_requests=read`. That is
  within the four read permissions the workspace's sign-in asks for (Contents, Pull requests,
  Metadata, Actions).
- `GET /pulls/N` for a stacked PR carries a `stack` object under **both** versions:

  ```json
  "stack": {"base": {"ref": "main", "sha": "d1fa551d…"}, "id": 4230424, "number": 38,
            "position": 2, "size": 4}
  ```

  For an unstacked PR (#27) the `stack` key is absent, and `GET /stacks?pull_request=27` returns
  200 with `[]`, not 404.
- GraphQL `PullRequest.stack` and `PullRequest.stackEntry` resolve.
- A merged layer stays in the stack. After #10 merged, `GET /stacks/13` still listed it, with
  `state: closed` and `merged_at` set, and #11 still reported `position: 2`.

**What this changes in the brief.** The objection "stack discovery cannot run, because the client
pins 2022-11-28" is wrong: no per-request version header is needed. The workspace already calls
`GetPullRequest` on every open, so discovery is one field on a response it already reads. An
unstacked PR costs nothing extra, and a stacked PR costs one `GET /stacks/{n}`. The fallback to the
single-PR view still matters for errors, but it is a smaller job than staged in S1. Merged layers
come back from the API in order, so "show merged layers, labelled as base" needs no extra lookup.

Not tested: a token holding exactly the four workspace permissions. The header is GitHub's own
statement of the requirement; a direct test needs the user to make such a token.

## 2. `base.sha` semantics

From the existing stacks:

- The bottom layer's `base.sha` is a snapshot of the trunk taken when the PR last synced, not the
  trunk's current tip. Stack 29's bottom PR #26 reports 143d98c while `main` is at d1fa551.
- PR #26's head is **not** built on its own `base.sha`: compare 143d98c…c2739e8 is `diverged`
  (merge base 286856a).
- After squash merges (stack 3), every adjacent pair of recorded heads compares as `diverged`,
  with 57–110 files: the merged layers' recorded heads are their last pre-merge heads.

From the sandbox (stack 13):

| Step | #11's `base.sha` | Lower head (#10) | `Compare` lower…#11 |
| --- | --- | --- | --- |
| Built with `gh stack submit` | 5002070 | 5002070 | `ahead` 1, `behind` 0 |
| Pushed a commit to #10 with plain `git push`, no restack | 5002070, unchanged after 80 s and again after 3 min | 1f99949 | **`diverged`** 1/1 |
| `gh stack rebase` and `gh stack push` | 018f960 | 018f960 | `ahead` 1, `behind` 0 |

GitHub's own diff for #11 stayed correct throughout (`src/pricing.ts` only), because it uses the
merge base.

**What this changes in the brief.**

- `base.sha` did not follow the base branch. It moved on a restack (`gh stack push` force-pushes
  every layer) and did **not** move on a plain push to the upper layer: after #11 gained 06217a6
  and #12 was pushed normally (606c41a), #12's `base.sha` stayed at c2e4950 for over an hour.
  Neither GitHub's documentation nor these runs give a rule for when it refreshes, so the brief
  should not rely on one.
- In every case run, the equality check (`base.sha` ≠ lower head) and the ancestry check agreed.
  The ancestry check is still the one to use: it asks the question directly ("does this layer
  contain the layer below?") and does not depend on when GitHub refreshes a field.
- The ancestry check (`Compare` lower head…upper head is `ahead` with `behind_by` 0) caught the
  stale layer at once and does not depend on when GitHub refreshes anything. Use it.
- An open layer whose lower neighbour is merged must be measured from the trunk's merge base, not
  from the merged layer's recorded head. Compare 018f960 (#10's recorded head)…c2e4950 (#11 after
  the cascade) is `diverged` 2/2.

## 3. Index runs after a cascading rebase

In stack 13, squash-merging #10 at 00:06:09 led to:

| Time | Event |
| --- | --- |
| 00:06:11 | #11 `automatic_base_change_succeeded` (retargeted to `stacks/trunk`) |
| 00:06:14 | #11 and #12 `head_ref_force_pushed` (GitHub's cascade rebase), new heads c2e4950 and fa320e6 |
| 00:06:14 | `review-index` `push` runs start on both new heads; both succeeded |
| 00:06:16 | `pr-probe` `pull_request` (`synchronize`) runs start on both new heads |

A local `gh stack rebase` + `gh stack push` also gave every new head one index run and one probe
run.

**What this changes in the brief.** The risk "the tip may have no index after a server-side
cascade" does not happen in the normal case. The tip has no index only for the minute or so while
CI builds it, the same as after any push, and the workspace already shows "CI is still building the
index" for that.

## 4. Rate cost

Reading stacks 29 and 38 (14 layers) took 34 calls: one stack read, one bottom-PR read, one
`Compare` and one index-run lookup per layer. That is about 2.4 calls per layer, so about 120
calls to open a 50-layer stack. A user's token allows 5,000 an hour.

The "12,000 calls an hour" risk in the brief comes from polling the index status of all 50 layers
every 15 seconds. Under the tip-only design only the tip's index is used, so only the tip needs
polling, which is the same one poll a single PR costs today.

## 5. Compare limits

- Adjacent-layer compares: of 21 compares across the three stacks in `adamtait/reviewer` (up to
  110 files and 13,405 changed lines each), none lost a patch. The only patch-less entries were
  four pure renames (status `renamed`, 0 changes), which never carry a patch.
- Combined compare: stack 3's trunk merge base to its tip (14 commits, 232 files) returned
  **about 100 files with no patch and `changes: 0`**, all with status `added`. GitHub stopped
  sending patches part-way through. The response has no field saying so, and no pagination
  (`Link` holds only `rel="first"`).
- Stack 29's combined range (71 files) lost no patches.

**What this changes in the brief.** Any attribution or rendering built on GitHub's patches must
treat "no patch, 0 changes, status not `renamed`" as **unknown**, never as "unchanged". A combined
diff should be drawn from file text at two commits, as the workspace's `DiffView` already does for
a single PR, and not from a combined compare's patches. Adjacent compares were reliable in this
sample, but 21 compares is a sample, not a guarantee.

## 6. CLI after a squash merge

**When the cascade succeeds** (stack 13, and stack 3 in `adamtait/reviewer`): the retarget and the
force-push arrive about 3 seconds apart. Only the rebased head triggers a `pull_request` run: a
retarget alone sends `edited`, which the default trigger ignores. The rebased head no longer holds
the merged layer's commits, so the merged layer's lines are not in the review. (Test 7 shows a
separate problem for every layer that is not at the bottom.)

**When the cascade fails** (stack 16): `stacks/trunk` was given a commit adding a `src/feature.ts`
that conflicts with #15's, then #14 was squash-merged.

| Time | Event |
| --- | --- |
| 00:04:41 | #15 `automatic_base_change_succeeded`: retargeted to `stacks/trunk` |
| — | No force-push. #15's head stayed at 8f01017, `mergeable: false`, `mergeable_state: dirty` |
| — | No `pull_request` run |
| 00:05:02 | A new commit pushed to #15 without rebasing (288f203). `review-index` ran on it. **No `pr-probe` run**, still none 5 minutes later |

GitHub's own file list for #15 then showed `src/config.ts` as well as `src/feature.ts`, so the
merged layer's file appears as part of #15's change.

**What this changes in the brief.**

- The Action cannot report a merged layer's lines again while a failed cascade is unresolved,
  because GitHub runs no `pull_request` workflow on a conflicted PR. When the author resolves it
  with `gh stack rebase`, the rebased head no longer holds the merged layer's commits.
- `reviewer watch` does not depend on workflow triggers. It polls by `updated_at` and would review
  #15 in this state, measured from #15's REST `base.sha` (`cmd/reviewer/run.go:216-218`). Whether
  that includes `config.ts` depends on whether that base contains the squash commit. Not tested.
- The workspace's single-PR view of #15 in this state would show `config.ts` as changed, because it
  uses GitHub's file list and the merge base. A stack view should label such a file as already in
  the base (merged in #14).

## 7. `github.base_ref` on stacked pull requests

Every probe run, with the event's own values:

| PR (real base branch) | Action | `base_ref` in the event | `base_sha` in the event | Files a `--base origin/<base_ref>` review would cover |
| --- | --- | --- | --- | --- |
| #11 (`a1-order`) | `opened`, before it joined the stack | `stacks/a1-order` | 5002070 (#10's head) | `pricing.ts` |
| #11 (`a1-order`) | `synchronize` after restack | **`stacks/trunk`** | 018f960 (#10's head) | `order.ts`, `pricing.ts` |
| #12 (`a2-pricing`) | `opened` | **`stacks/trunk`** | 0dc156e (#11's head) | `order.ts`, `pricing.ts`, `api.ts` |
| #12 (`a2-pricing`) | `synchronize` after restack | **`stacks/trunk`** | f227cef (#11's head) | `order.ts`, `pricing.ts`, `api.ts` |
| #12 (`a2-pricing`) | `synchronize` after cascade | **`stacks/trunk`** | c2e4950 (#11's head) | `pricing.ts`, `api.ts` |
| #15 (`b1-config`) | `opened` | **`stacks/trunk`** | f192db2 (#14's head) | `config.ts`, `feature.ts` |

Meanwhile `GET /pulls/12` reports `base.ref: stacks/a2-pricing`. For a PR in a stack, the
`pull_request` event's `base_ref` names the stack's trunk, while `base_sha` stays the head of the
layer below. This is presumably how GitHub makes CI that targets the default branch run on every
layer of a stack.

reviewer passes that value straight through:

- `action.yml:51,82`: `BASE_REF: ${{ inputs.base || github.base_ref }}`, then
  `--base "origin/${BASE_REF}"`.
- `.github/workflows/self-review.yml:49`: `--base "origin/${{ github.base_ref }}"`.
- `cmd/reviewer/run.go:214-218`: an explicit `--base` wins over the PR's own `base.sha`.
- `.github/workflows/ci.yml:96,121,144` do the same for the eslint, ADR and history checks.

**Effect today.** On a GitHub-native stack, reviewer's Action reviews layer N against the trunk,
so it covers layers 1 to N. Every finding in a lower layer is posted again on every PR above it.
Under a 50-layer stack, a finding in layer 1 is posted 50 times. The diff filter (ADR-0007) does
not help, because the lower layers' lines are inside the range it is given.

**Fix.** Review against the test merge commit's first parent (`HEAD^1`), as measured in 7a below,
not against `github.base_ref` or `base.sha`. This is a CLI and Action change that needs no stack
model, and it can land before any workspace work.

### 7a. What the Action actually diffs: the test merge commit

The probe in test 7 diffed against the PR's head SHA. The Action does not: `actions/checkout` on a
`pull_request` event checks out GitHub's test merge commit (`refs/pull/N/merge`), and
`internal/diff/diff.go:67-68` runs `git diff --merge-base <base> HEAD` against that commit.

For stacked PRs, GitHub builds each layer's test merge commit **on top of the test merge commit
of the layer below**, not on the trunk:

| Ref | Commit | First parent | Second parent |
| --- | --- | --- | --- |
| `refs/pull/11/merge` | 62b7335 | 539c8ae (`stacks/trunk` tip) | c2e4950 (#11 head) |
| `refs/pull/12/merge` | 7b1c5a2 | **62b7335 (#11's test merge commit)** | fa320e6 (#12 head) |

`git diff --name-only 7b1c5a2^1 7b1c5a2` lists only `src/api.ts`, which is #12's own change.

GitHub did not rebuild either test merge commit when the trunk moved (d7afff8, adding
`src/trunk-only.ts`). Each was rebuilt only when that PR's head was pushed. After pushing #11 and
then #12, the refs were:

| Ref | Commit | First parent | Second parent |
| --- | --- | --- | --- |
| `refs/pull/11/merge` | 23a8f94 | d7afff8 (new trunk tip) | 06217a6 (#11 head) |
| `refs/pull/12/merge` | 3445b82 | 23a8f94 (#11's test merge commit) | 606c41a (#12 head) |

Three ways to choose the base, run against `refs/pull/12/merge` (3445b82). #12's own change is
`src/api.ts`:

| Base | Files reviewed | Verdict |
| --- | --- | --- |
| `--merge-base origin/stacks/trunk` (what the Action does today) | `api.ts`, `pricing.ts` | Wrong: includes #11's layer |
| `--merge-base <base.sha>` (c2e4950, the event's and the REST value) | `api.ts`, `trunk-only.ts` | Wrong: includes a trunk change that is in no layer |
| `HEAD^1` (#11's test merge commit) | `api.ts` | Right |

The same three bases on a stack's bottom layer and on an unstacked control PR (#24, opened against
`stacks/trunk`), each after the trunk had moved past the PR's fork point:

| PR | Own change (`/pulls/N/files`) | Today | `base.sha` | `HEAD^1` |
| --- | --- | --- | --- | --- |
| #11, bottom of stack 13 | `pricing.ts` | `pricing.ts` | `pricing.ts`, `trunk-only.ts` | `pricing.ts` |
| #24, unstacked | `control.ts` | `control.ts` | `control.ts`, `trunk-later.ts` | `control.ts` |

`HEAD^1` gives today's result wherever today's result is right, and fixes the upper layers.
`base.sha` lagged on both, unstacked PR included, so it is wrong as a review base for any PR.

`HEAD^1` only means "the base" when `HEAD` is GitHub's test merge commit, which is what
`actions/checkout` gives on a `pull_request` event. A workflow that checks out the head SHA, or a
run from `reviewer watch` or a terminal, must not use it.

**What this changes in the fix.** Inside the Action's checkout, `HEAD^1` is "the trunk plus every
layer below, merged". So `--base HEAD^1` scopes a review to the layer's own change, and for an
unstacked PR `HEAD^1` is the base branch's tip, which matches what `--merge-base origin/<base>`
gives today. Swapping in `github.event.pull_request.base.sha` is wrong, as the table shows: when `base.sha` is
behind, the review picks up trunk changes. It is also mechanically broken:
`action.yml:45` and `:82` prefix `BASE_REF` with `origin/`, so a SHA there breaks both the history
check and `--base`. The checkout also needs the parent commits (`fetch-depth` of at least 2 for
`HEAD^1`).

## Still open

- Whether `reviewer watch` would review a lower layer's lines on a PR left retargeted by a failed
  cascade (test 6, last point).
- A token holding exactly the workspace's four permissions (test 1).
- The fix inside a real Action run. It has been run with the real binary, outside Actions, on the
  sandbox's own test merge commits, with the Action's arguments (`--pr N --base
  origin/stacks/trunk`). A recorder plugin captured what each binary handed to analyzers:

  | PR | Today's binary | Fixed binary (branch `fix/stacked-pr-base`) |
  | --- | --- | --- |
  | #12, top of stack 13 | `api.ts`, `pricing.ts`, base `origin/stacks/trunk` | `api.ts`, base 23a8f94 (`HEAD^1`), with a note saying why |
  | #24, unstacked | `control.ts`, base `origin/stacks/trunk` | `control.ts`, base 2ca3013 (`HEAD^1`) |

  A run inside Actions needs a release, because the Action downloads a released binary.

## Sandbox state

Left in place for inspection in `adamtait/reviewer-sandbox`: stack 13 (#10 merged, #11 and #12
open) and stack 16 (#14 merged, #15 open and conflicted), with branches `stacks/trunk`,
`stacks/a1-order`, `stacks/a2-pricing`, `stacks/a3-api`, `stacks/b1-config`, `stacks/b2-feature`.
The other session's `main` and `q1*`/`e2e-risk` branches were not touched.
