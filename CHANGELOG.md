<!-- SPDX-License-Identifier: MIT -->
# Changelog

Notable changes, newest first. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## Versioning

Two version numbers, deliberately independent (ADR-0026):

- **The product version** — what a tag says, what `reviewer --version` prints,
  and what `@adamtait/reviewer-plugin-typescript` is published as. The binary and
  the plugin always share it: `reviewer init` pins the plugin devDependency to the
  binary's own version, so they are installed as a pair, and the release refuses a
  tag that disagrees with the plugin's `package.json`.
- **The plugin protocol version** — what a plugin author needs to know, printed
  by `reviewer --version` alongside the product version. Adding a frame type, an
  optional field, or a `Finding` field does not bump it. Removing or renaming
  anything, or changing what a field means, does.

The product version is semver over the command line, the configuration file, and
the finding schema. A new analyzer, a new rule, or a new finding from an existing
rule is a minor change, not a breaking one — a review that says more than it did
last week is the tool working.

Prereleases (`v0.1.0-rc.1`) are published like any other release. A snapshot
build is not: it carries a deliberately non-semver version, so `init` from one
prints that it cannot pin the plugin rather than pinning to something unpublished.

## [Unreleased]

Everything so far. The first tagged release is `v0.1.0` (PR-46).

### Added

- Deterministic lane: secrets (gitleaks), convention patterns (opengrep),
  vulnerable dependencies this change introduces (osv-scanner), and the
  TypeScript analyzers — types, lint, layering, dead code, type coverage, and the
  tests covering the files you changed.
- Model lane, off by default, behind a structural secrets gate: six model access
  paths chosen at install, every finding put back to be disproved before it is
  emitted, and taste categories capped in code rather than by asking nicely.
- A plugin protocol, spoken by every analyzer including the first-party ones, with
  a Go SDK and a worked example in POSIX shell.
- `reviewer init`, which inspects a repository and writes what it detected,
  printing what it could not work out and what each gap costs.
- Two CI surfaces: a GitHub Action and a local poller.
- `reviewer metrics`, reporting acceptance per rule, so a rule nobody acts on can
  be deleted on evidence.
