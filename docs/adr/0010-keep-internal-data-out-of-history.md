<!-- SPDX-License-Identifier: MIT -->
# ADR-0010 — Keep secrets and internal data out of this repository's history

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-08
- **Supersedes:** —
- **Superseded by:** —

## Context

ADR-0004 keeps repository-specific values out of the *code*. That is a build-time guarantee about the
working tree. It says nothing about history, and history is where this kind of mistake becomes
permanent: once a credential or an internal hostname is in a commit, removing it needs a rewrite that
changes every subsequent hash and invalidates every clone and every open pull request.

This repository is intended to become public. A published repository is published with its history.

## Decision

Nothing internal or secret enters a commit in the first place. Two mechanisms:

- `make hooks` installs a pre-commit hook that runs `gitleaks protect --staged` and refuses the
  commit. It is opt-in, because a hook that appears without being asked for is a hook people delete,
  and it degrades to a warning when gitleaks is not installed rather than blocking work.
- CI scans the entire history on every pull request with `fetch-depth: 0`. This is the guarantee; the
  hook is the convenience that stops you finding out at the end.

The configuration adds one rule to the gitleaks defaults: internal-looking hostnames, which are the
ADR-0004 failure mode that no credential scanner catches on its own. `testdata/` is allowlisted, and
the fixture credential it exists for is fabricated but in a format gitleaks genuinely matches —
verified rather than assumed, since a fixture secret that no scanner detects makes the secrets
analyzer's own tests vacuous.

## Consequences

- **Positive:** publishing needs no history audit beyond running the scan that has already run on
  every pull request.
- **Negative:** a false positive blocks a commit until it is allowlisted, and every allowlist entry
  widens the hole slightly. The entries are therefore path-scoped rather than pattern-scoped wherever
  possible.
- **Neutral:** the hook is opt-in, so a contributor who never runs `make hooks` is protected only by
  CI. That is the correct order — the guarantee belongs in the place nobody can skip.

## Alternatives rejected

- **Scrub before publishing** — a history rewrite is not reliably reversible, and it announces to
  anyone watching that there was something to scrub.
- **A mandatory hook installed by a build step** — hooks that install themselves get deleted, and a
  developer who disables one disables it for every repository.
- **Trusting review to catch credentials** — a reviewer reading a diff for logic does not reliably
  notice a forty-character string.

## Revisit when

A legitimate value repeatedly trips the scan, which means the rule is shaped wrongly rather than that
the value is fine.
