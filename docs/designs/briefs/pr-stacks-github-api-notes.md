<!-- SPDX-License-Identifier: MIT -->
# GitHub stacked pull requests: API notes

Notes taken on 2026-11-27 from GitHub's documentation, for reviewers of the PR stacks brief who
cannot open URLs. These are excerpts and paraphrases, not a full copy.

Sources:
- https://docs.github.com/en/pull-requests/get-started/about-stacked-prs
- https://docs.github.com/en/pull-requests/reference/stacked-pull-requests-apis-and-webhooks
- https://docs.github.com/en/rest/pulls/stacks
- https://github.com/github/gh-stack

## What a stack is

Two or more pull requests in the same repository. The bottom one targets the stack's trunk
(usually the default branch, but it can be any branch). Each later one targets the branch of the
pull request below it. Each pull request on GitHub shows only its own layer's diff. Cross-fork
stacks are not supported. Created with the `gh stack` CLI extension (`gh extension install
github/gh-stack`; `gh stack link` builds a stack from existing branches or PR numbers), the web UI,
or the REST API.

Rebasing: GitHub can run a server-side cascading rebase, or `gh stack` runs one locally. When the
bottom pull request merges, the remaining branches are rebased and the next one re-targets the
trunk.

Merging: bottom up only. Merging a mid-stack PR merges everything below it; those above stay open
and re-target the trunk. Merge commit, squash and rebase are all supported; history is the same as
merging each PR in order. API merges of stacked PRs must use a new asynchronous merge endpoint.

CI: "CI checks triggered by pull requests on your default branch run for all pull requests in the
stack, not just the bottom one." Branch protection is enforced on every PR in the stack.

## REST

All endpoints use `X-GitHub-Api-Version: 2026-03-10`.

- A pull request resource that belongs to a stack carries a `stack` object: the stack's number and
  size, and the pull request's position and base.
- `GET /repos/{owner}/{repo}/stacks` lists stacks. Query `pull_request=<n>` filters to the stack
  containing that PR. Paginated, max 100 per page. Each entry ("Pull Request Stack Minimal"):
  `id`, `number`, `node_id`, `url`, `base.ref` (the trunk), `open`, `created_at`, and
  `pull_requests[]` ordered bottom to top, each with `number`, `state` (open|closed), `draft`,
  `merged_at`, `head.ref`, `head.sha`.
- `GET /repos/{owner}/{repo}/stacks/{stack_number}` returns the full stack: same top-level fields,
  and each pull request carries `number`, `title`, `state`, `merged_at`, `draft`, `html_url`,
  `user`, `head {ref, sha, repo}`, `base {ref, sha, repo}`.
- `POST .../stacks` (create), `POST .../stacks/{n}/add`, `POST .../stacks/{n}/unstack` modify
  stacks. The workspace makes no write calls and would not use these.

Not found in the documentation read: which fine-grained token permission the read endpoints need,
and whether GitHub Enterprise Server supports stacks (the user reports it does not today).

## GraphQL

Read-only `stack` and `stackEntry` fields on `PullRequest`. No stack mutations.

## Webhooks

`pull_request` payloads carry a `stack` object while the PR is in a stack, including the stack's
target branch. There is a `stacked` action.
