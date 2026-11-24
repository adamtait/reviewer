<!-- SPDX-License-Identifier: MIT -->
# ADR-0030 — Read source from GitHub as the user and hold it in memory

- **Status:** Proposed
- **Date:** 2026-11-24
- **Implemented by:** PR-50
- **Supersedes:** —
- **Superseded by:** —

## Context

The review workspace (`docs/designs/review-workspace.md`) shows a pull request's diff, lets a
reader open any file at the pull request's commit, and searches the whole repository at that
commit. All three need the repository's files as they were at a commit, not only the lines the pull
request changed.

Four things constrain how the files arrive. The service keeps no database and no disk state
(ADR-0029). It has no GitHub credential of its own: every read is made with the token of the user
who asked (ADR-0032). It must never run code from a pull request, which rules out anything that
starts `git`. And a pull request's author controls that pull request's CI job, so nothing a CI job
produces can be taken as true.

This record is `Proposed` while the workspace's first version is on trial.

## Decision

When a user opens a commit that is not in memory, the service asks GitHub for that commit's archive
with that user's token, and unpacks the stream in memory. It keeps the path of every entry, and the
text of every file that is valid UTF-8, holds no NUL byte and is within a per-file limit. Any other
entry is kept as a path with a mark: binary, too large, or other. Nothing is written to disk, no
link is followed, and nothing in the archive is run. A commit whose kept text passes a per-commit
limit is refused, and the refusal is remembered.

Commits are held in one cache bounded by a configured number of bytes. When it is full, the entry
used longest ago is dropped. Every entry, and every fetch in flight, is keyed by owner, repository
and commit hash together. Requests for the same commit of the same repository share one fetch, and
at most two fetches run at once.

A commit is named by its full 40-character hash. A branch name or a short hash is refused, so what
is held under a name is what that name means for good.

The file list, a file's text and text search are answered from memory. Search is a literal scan of
the kept text, with no index.

The access check runs on every request, before memory is consulted. The cache holds data, never
permission to see it.

The CI-built navigation index (ADR-0034) reaches the service the same way, as the user, and is
treated as untrusted. The rules that bound it arrive with navigation and are recorded there.

## Consequences

- **Positive:** search works across the whole repository at the pull request's commit, including
  files the pull request does not touch, without a search index to build or keep right.
- **Positive:** the service holds no copy of any repository at rest. A restart clears everything it
  knew, and a breach of its disk finds no source.
- **Positive:** a user who can read repository A and not B cannot reach B's source through A's
  address by naming one of B's commits. The request finds nothing in memory under A and asks GitHub
  for that commit in A, as that user.
- **Positive:** unpacking is the standard library's `archive/tar` and `compress/gzip`. No program is
  started and no dependency is added.
- **Negative:** the first open of a commit waits for a transfer from GitHub: about a second for a
  repository of 2 MB of text, and unmeasured for a large one. After a restart or an idle stop,
  every commit is a first open again.
- **Negative:** memory is the limit. A repository whose text passes the per-commit limit cannot be
  opened at all, and the operator must size the container from the limits.
- **Negative:** one instance at most. A second would have its own empty memory.
- **Negative:** a commit in memory is served until the access check's remembered answer runs out,
  up to an hour after GitHub removed a user's access, as with every other read.
- **Neutral:** the archive GitHub sends is not checked against the commit's hash. GitHub is trusted
  to send the commit that was asked for, as it is for every other call.
- **Neutral:** files GitHub leaves out of an archive, such as those marked `export-ignore`, are
  absent. The diff view shows such a file as not available, with a link to it on GitHub.

## Alternatives rejected

- **Fetch with `git` and store files in a database** — git checks every object against its hash,
  but this rebuilds git's storage, needs a database, and has the service start a second program
  holding the user's token.
- **Read each file from GitHub when it is viewed** — nothing in memory, and the diff appears as fast
  as its files, but there is no text search at a commit, which needs the whole tree. It remains the
  fallback if first opens prove too slow.
- **Both: per-file reads for the diff, archives for search** — two read paths that must agree on
  which files are text. Revisit if the first deployment shows a slow first open.
- **Have the CI job push source to the service** — the pull request controls its own CI job, so the
  service would have to verify what it received, and would need a public upload address.
- **Poll GitHub on a timer with a service token** — a standing credential that reads every
  connected repository.
- **Key the cache by commit hash alone** — a hash names the same content everywhere, so this looks
  safe and is not: a user could name a commit of a repository they cannot read at the address of
  one they can, and be served it from memory without GitHub being asked.
- **Build a search index** — a scan of 90 MB of text took 0.13 seconds in the design's tests. An
  index would be a second structure to bound and to keep in step with the first.

## Revisit when

The first deployment's cold open takes longer than 15 seconds, a connected repository's text passes
what one container can hold, or the service needs a second instance.
