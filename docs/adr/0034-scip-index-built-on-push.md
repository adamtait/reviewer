<!-- SPDX-License-Identifier: MIT -->
# ADR-0034 — Build a SCIP navigation index in CI on every push

- **Status:** Proposed
- **Date:** 2026-11-23
- **Implemented by:** PR-47
- **Supersedes:** —
- **Superseded by:** —

## Context

The review workspace (`docs/designs/review-workspace.md`) offers go-to-definition and
find-references on a pull request, including into files the pull request does not touch. That needs
the answers a compiler gives, for the exact commit on screen.

Getting those answers means installing the repository's dependencies and building its program,
which is running code a pull request author controls. The workspace service must never do that. A
CI job on the repository already does, on every pull request, with the repository's own permissions
and nothing of the service's.

Two facts about GitHub Actions shape the trigger. A pull request with a merge conflict gets no
`pull_request` run at all. And a `pull_request` run checks out a temporary merge of the pull request
into its base, which is not the commit a reviewer is shown.

This record is `Proposed` while the workspace's first version is on trial. It supersedes nothing.

## Decision

`reviewer init` writes a second workflow, `.github/workflows/review-index.yml`, into a repository
that has a `package.json` and a root `tsconfig.json`. On every branch push it checks out the pushed
commit, installs dependencies the way the review workflow does, runs `scip-typescript` at a pinned
version, and saves the result, `index.scip`, as a workflow artifact named `review-index`. The job
holds read access to contents and no secret. The workspace finds a commit's index by this workflow's
file name and the run's head commit, and treats the index as written by whoever pushed the commit.

## Consequences

- **Positive:** the service never installs or executes a repository's code. The index describes the
  same commit the workspace shows, for pull request heads and the default branch alike, through one
  trigger.
- **Positive:** nothing is uploaded to the service, so it needs no upload address, and the job
  needs no credential for it.
- **Positive:** SCIP is a published format with indexers for other languages, so a later language
  is a second workflow step and not a second reader.
- **Negative:** a second dependency install on every push to every branch. It took 57 seconds on
  this repository.
- **Negative:** no index for a pull request from a fork, whose pushes happen in the fork, or for a
  commit pushed by a workflow with its own token, for which GitHub starts no run.
- **Negative:** the index cannot be verified. A commit's own workflow file builds it, so the
  workspace bounds what it accepts and labels every result with its source.
- **Neutral:** the workflow's file name, the artifact's name and the indexer's version are a
  contract between an installed repository and the service. Changing one means changing both.

## Alternatives rejected

- **Trigger on `pull_request`** — no run during a merge conflict, and the default checkout is a
  temporary merge.
- **Build the index inside the existing review job** — that job runs on `pull_request`, with both
  problems above.
- **Have the job upload the index to the service** — an upload address, a signed-token check, and a
  workflow permission granted to a job that runs pull request code.
- **Index on the service, in a sandbox** — the service would have to operate a sandbox and run
  installs in it.
- **Export an index from this project's TypeScript plugin** — the most code to write and own, and a
  separate exporter for each later language.

## Revisit when

The index job's cost per push is a complaint, or a team's repositories need an index for commits
the push trigger cannot reach.
