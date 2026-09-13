<!-- SPDX-License-Identifier: MIT -->
# ADR-0018 — Trigger on `pull_request`, never `pull_request_target`

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-21
- **Supersedes:** —
- **Superseded by:** —

## Context

GitHub offers two events for running a workflow on a pull request. `pull_request` runs the workflow from
the *pull request's* commit with no access to repository secrets for a fork. `pull_request_target` runs
the workflow from the *base branch* with full access to secrets, including for forks.

`pull_request_target` is the one that makes the model lane work on a fork, and it is a known privilege
escalation: the workflow has the repository's secrets, and anything it executes from the pull request's
checkout — a build script, a lint config, a test helper, a dependency's install hook — runs with them.
For a tool whose entire job is to execute the repository's own analyzers against contributed code, that
is not a theoretical concern; it is the mechanism.

## Decision

The composite action and the generated workflow use `pull_request`. A fork's pull request therefore gets
no `REVIEW_MODEL_API_KEY`, the model lane is skipped, and the deterministic lane runs and comments as
usual.

That is the correct degradation rather than a limitation to work around: the deterministic lane is the
half worth trusting, and it needs no secrets. The binary is installed by pinned version and verified
against a checksum, so the workflow's own supply chain does not widen what a fork can reach.

Permissions are the minimum the job needs: `contents: read` and `pull-requests: write`. Nothing in this
tool can merge, approve, close or push.

## Consequences

- **Positive:** a fork's pull request cannot reach the repository's secrets through this workflow, whatever
  the contributor puts in their branch. Reviews still happen on forks, with the half of the system that
  does not need credentials.
- **Negative:** no model-lane review on forks. For a repository that takes most of its contributions from
  forks, that is most of the value of the model lane gone. The answer is a second, manually triggered
  workflow that a maintainer runs after reading the diff — not a change to this one.
- **Neutral:** `GITHUB_TOKEN` on a fork pull request is read-only, so posting comments on a fork requires
  either a `pull_request_target` workflow or a maintainer-triggered one. The tool reports that it could not
  comment rather than failing.

## Alternatives rejected

- **`pull_request_target`** — hands repository secrets to fork-authored code, in a tool that exists to
  execute that code's own tooling.
- **`pull_request_target` with a hardcoded base-branch checkout** — the documented mitigation, and it
  still runs the repository's analyzers against the fork's content with secrets present, which is the
  same exposure with more steps.
- **A separate token with narrower scopes** — narrows the blast radius without closing the hole, and adds
  a credential to manage.

## Revisit when

Never, in the direction of `pull_request_target`. If fork reviews with a model become important, the shape
is a maintainer-triggered workflow on a diff a human has already looked at.
