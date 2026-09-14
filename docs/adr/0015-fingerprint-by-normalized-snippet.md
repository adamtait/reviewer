<!-- SPDX-License-Identifier: MIT -->
# ADR-0015 — Identify a finding by its normalised snippet, never by line number

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-16
- **Supersedes:** —
- **Superseded by:** —

## Context

A reviewer's branch is rebased, amended and force-pushed several times before it merges. Each of those
rewrites every line number in the diff. The system re-runs on every push, so it needs to answer "have I
already said this?" — and the obvious key, file plus line, changes on a rebase that altered nothing about
the code.

Getting this wrong is not a small bug. At ten to fifty pull requests a week, a reviewer who force-pushes
three times gets each comment four times. The tool becomes intolerable inside a week, and the damage is
to its reputation rather than to its output.

## Decision

A finding's identity is `sha256(ruleId ‖ filePath ‖ normalisedSnippet)`, truncated to six hex
characters, embedded in each posted comment as an invisible `<!-- rv:a3f9c2 -->` marker.

Three deliberate exclusions:

- **The line number**, so a shift does not change identity.
- **The message**, so rewording a rule's text does not orphan every comment it has ever posted.
- **Horizontal whitespace**, removed entirely rather than collapsed — a formatter adds space where
  there was none, so any normalisation preserving a single space still changes the digest.

And three deliberate inclusions: the rule, because two rules firing on one line are two findings; the
path, because the same line in two files is two findings; and the line *structure* of a multi-line span,
because a two-line finding is not a one-line finding.

Quotes, identifiers and operators are **not** normalised. Those are changes to the code, and a comment
about the old code should be replaced rather than carried forward.

Fingerprints are computed by the core, not by analyzers. A plugin has no reason to know how dedupe
works, and one that guessed would break it; the host discards any fingerprint a plugin sets.

## Consequences

- **Positive:** a rebase, a reindent or a rename of the rule's message text all leave existing comments
  in place. The marker also makes the tool's own comments distinguishable from a human's, which is what
  lets it resolve its own threads without touching anyone else's (ADR-0020's sibling concern).
- **Negative:** two identical lines in the same file under the same rule share one identity, so only the
  first is reported. Accepted: the alternative reintroduces position into the key, and a duplicated
  finding on duplicated code is a reasonable thing to report once.
- **Negative:** six hex characters is 16.7M values. Collision is possible in principle and irrelevant in
  practice at the scale of comments a repository accumulates; the marker is read only within one
  repository's pull requests.
- **Neutral:** an unreadable file yields an identity based on rule and path alone. Weaker, but stable —
  a finding that cannot be deduped beats a finding that cannot be posted.

## Alternatives rejected

- **File plus line** — breaks on every rebase, which is the problem.
- **Hashing the whole file** — one unrelated edit invalidates every comment in it.
- **A database mapping identities to comments** — a host, a backup story and a schema, to store what a
  comment body can carry itself (ADR-0016).
- **GitHub's own comment position tracking** — real, and it marks comments outdated rather than telling
  us whether we have already said something. Useful for measuring acceptance (ADR-0024), useless for
  dedupe.

## Revisit when

Duplicate comments are observed in practice despite this, which would mean the normalisation is missing
a transformation a real formatter performs.
