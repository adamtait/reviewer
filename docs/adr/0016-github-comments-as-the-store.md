<!-- SPDX-License-Identifier: MIT -->
# ADR-0016 — Use GitHub's own comments as the only persistent store

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-18
- **Supersedes:** —
- **Superseded by:** —

## Context

The system needs to remember things across runs. At minimum: which findings it has already reported, so
a force-push does not produce a second copy of every comment (ADR-0015). Later: whether a reported
finding was acted on, which is what drives the decision to keep or delete a rule (ADR-0024).

The reflex is a database. For a tool that is otherwise a stateless binary (ADR-0008), that means a host,
a connection string, a backup story, a schema migration path, and a second thing that can be down when
someone opens a pull request.

## Decision

GitHub is the store. Every comment this tool posts carries an invisible
`<!-- rv:a3f9c2 -->` marker, and a run begins by reading the pull request's existing comments and
collecting those markers. Both inline review comments and the conversation comment are read, so a
finding that moved between the collapsed summary and an inline comment is still recognised as already
said.

Acceptance measurement reuses the same data rather than adding any: GitHub already marks a comment
outdated when the code it pointed at changed, and a thread resolved when someone acted on it, so a query
grouped by rule id yields acceptance per rule with no instrumentation of our own.

## Consequences

- **Positive:** nothing to deploy, nothing to back up, nothing that can be down independently of GitHub
  itself. The state is visible: a human can read exactly what the tool believes by looking at the pull
  request. Deleting a comment genuinely resets the tool's memory of it, which is the behaviour a user
  would expect anyway.
- **Negative:** every run pays a read of the pull request's comments, paginated, before it can post
  anything — two or three API calls against the rate limit. Cross-repository or historical analysis needs
  a query per pull request rather than one query, which makes the weekly acceptance report an N-call
  operation rather than a single one.
- **Negative:** the store's schema is a regex over comment bodies. A future change to the marker format
  orphans every existing comment, so the format is effectively frozen.
- **Neutral:** markers are invisible in rendered markdown but present in the raw body, so anyone reading
  the API or quoting a comment sees them. That is honest rather than a leak.

## Alternatives rejected

- **A database** — everything above, to store what a comment body already carries.
- **A file committed to the repository** — turns every review into a commit, and conflicts on every
  concurrent pull request.
- **GitHub's check-run annotations instead of comments** — they expire with the check run, which is
  exactly the persistence this needs, and they cannot be replied to or resolved.
- **Deriving identity from GitHub's comment position tracking** — it tells us a comment is outdated, not
  whether we have already said something. Useful for measuring acceptance, useless for dedupe.

## Revisit when

The weekly acceptance query across several repositories becomes slow enough to matter, which is the one
thing a real datastore would genuinely fix. A read-only cache is then the answer, not a move of the
source of truth.
