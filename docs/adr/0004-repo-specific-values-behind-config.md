<!-- SPDX-License-Identifier: MIT -->
# ADR-0004 — Route every repository-specific value through Config

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-03
- **Supersedes:** —
- **Superseded by:** —

## Context

This repository is intended to be public from the first commit. The repositories it reviews are not:
they carry internal service names, architecture conventions encoded as rules, endpoint URLs, and
guidance documents that describe how a particular team works.

The failure mode is gradual and hard to reverse. A default endpoint here, a rule pattern committed as
an example there, and publishing stops being "make the repository public" and becomes "audit and
rewrite four years of history". Once such a value is in git history, removing it requires a rewrite
that invalidates every clone.

## Decision

Repository-specific values reach the core through exactly one channel: `config.Resolve`, which
returns a `Config` built from defaults, the destination repository's `.review/config.yaml`, and a
three-variable environment overlay. Nothing in the core names a host, an organisation, a rule pattern
or a guidance document. Credentials are separated into a `Secrets` value whose `String` method prints
whether each field is set and never what it contains.

`TestNoEndpointsInTheSource` walks every non-test `.go` file and fails on a `http://` or `https://`
literal, so the seam is a build failure rather than a convention.

## Consequences

- **Positive:** publishing is `chmod +x` on the repository's visibility, not a sanitisation project.
  The configuration surface doubles as the documentation of what the tool needs to know about a
  repository.
- **Negative:** convenient defaults that would encode a common layout are unavailable — the tool
  cannot guess that rules live in `.review/rules` for *your* repository without the installer writing
  it down. That cost is paid once, at install (ADR-0020).
- **Neutral:** the no-URL test will eventually flag a legitimate literal, such as a schema `$id`.
  When it does, the right response is to move that value into config, not to weaken the test.

## Alternatives rejected

- **An internal configuration package in-tree, excluded from the published tarball** — the seam is
  then a build tag rather than a repository boundary, and a single import from the core to that
  package silently breaks it.
- **Convention-over-configuration discovery** — every convention is a guess about someone's layout,
  and guesses that work are indistinguishable from defaults that encode one team's architecture.

## Revisit when

The no-URL test starts failing for legitimate reasons more than once a quarter, which would mean the
core has grown a genuine need to name network resources and the rule needs a narrower shape.
