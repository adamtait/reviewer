<!-- SPDX-License-Identifier: MIT -->
# ADR-0026 — Release two artifacts from one tag

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-01 (module path and package name), PR-45 (automation)
- **Supersedes:** —
- **Superseded by:** —

## Context

ADR-0002 puts the core in Go and TypeScript analysis in a Node plugin. Those are two different
package ecosystems with two different distribution channels, and they must agree on a version at run
time: a plugin speaking protocol v2 against a binary that only knows v1 is a broken install.

The consuming repositories are TypeScript projects whose CI already runs `setup-node`. They do not
necessarily have a Go toolchain, so `go install` cannot be the only way in.

## Decision

One tag produces two artifacts. GoReleaser cross-compiles the `reviewer` binary for linux and darwin
on amd64 and arm64, publishes them with checksums to GitHub Releases, and `go install` keeps working
for Go users. The same tag publishes `@adamtait/reviewer-plugin-typescript` to npm at the same
version. The release fails if the two versions would diverge. The protocol version is bumped
independently of the product version, because the compatibility question a plugin author asks is
"which protocol", not "which release".

## Consequences

- **Positive:** consumers install the binary by pinned version and checksum in the Action, and the
  plugin through their existing package manager, where it resolves their own `typescript` and
  `eslint`. Neither ecosystem is made to pretend to be the other.
- **Negative:** two publishing credentials, two registries, and a release that is only atomic by
  construction rather than by the registries' guarantees — a half-published release is possible and
  has to be detected and re-run rather than rolled back.
- **Neutral:** the repository holds a Go module and an npm package side by side, so a protocol change
  touches both in one commit. That weakens per-PR revertability for protocol changes specifically,
  which is the correct trade: the two halves must never be released apart.

## Alternatives rejected

- **A single npm package that downloads the binary on `postinstall`** — hides the Go core behind npm,
  makes the install fail closed on any npm or network policy, and blocks `go install` entirely. Kept
  as a documented fallback if a destination repo forbids binary downloads in CI.
- **Two repositories** — forces a cross-repository release dance for every protocol change, which is
  the one change that must stay atomic.

## Revisit when

A destination repository forbids downloading release binaries in CI, at which point the npm wrapper
moves from fallback to primary for that consumer.
