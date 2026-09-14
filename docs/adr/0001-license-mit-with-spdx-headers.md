<!-- SPDX-License-Identifier: MIT -->
# ADR-0001 — License the project MIT with per-file SPDX headers

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-00 (decision), PR-01 (`LICENSE` and CI enforcement)
- **Supersedes:** —
- **Superseded by:** —

## Context

This is a personal project, authored outside any employer's scope, intended to be public from the
first commit rather than opened later. The repository must therefore be publishable at any moment:
license present, provenance clean, no file of uncertain origin.

The project also invokes third-party tools with varied licenses, one of which — Opengrep — is
LGPL-2.1. Whatever license this project takes has to coexist with spawning that binary without
inheriting its obligations (see ADR-0011).

## Decision

The project is MIT, copyright the author. `LICENSE` sits at the repository root from PR-01. Every
`.go` and `.ts` source file carries `SPDX-License-Identifier: MIT` as its first line, enforced by
`addlicense -check` in CI, so a file copied out of this repository carries its license with it.
No `NOTICE` file: MIT does not require attribution beyond the license text, and an unmaintained
`NOTICE` is worse than none.

## Consequences

- **Positive:** no license review gates any release. Contributors need no CLA; the inbound=outbound
  convention in `CONTRIBUTING.md` is sufficient for MIT.
- **Negative:** no patent grant. For a code-review tool with no novel algorithm this costs nothing,
  but it is a real difference from Apache-2.0 and is not recoverable without relicensing.
- **Neutral:** SPDX headers add one line per file and one CI check.

## Alternatives rejected

- **Apache-2.0** — the patent grant and `NOTICE` upkeep buy nothing for a personal project with no
  patent exposure, and the longer header is friction on every new file.
- **Deferring the license to a publication milestone** — retrofitting headers across a hundred files
  later is exactly the rewrite this repository's structure exists to avoid.

## Revisit when

The project acquires a corporate owner or a contributor whose employer requires an explicit patent
grant. Relicensing then needs every contributor's agreement, which is why the decision is recorded
now rather than assumed.
