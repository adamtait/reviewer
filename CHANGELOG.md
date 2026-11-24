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

### Changed

- **The minimum Go version is now 1.26.** Go 1.24 and 1.25 no longer receive
  security fixes, and CI now tests the two supported releases, 1.26 and 1.27.
  Building `reviewer` from source needs Go 1.26 or newer; the released binaries
  are unaffected.
  The pinned `staticcheck` moves to 2026.2.2, which supports both.

### Added

- `cmd/workspace`, the skeleton of the review workspace service described in
  `docs/designs/review-workspace.md`: configuration, logging and a health
  address, with a container image. It does nothing for a user yet. See
  `docs/workspace.md` (ADR-0029).
- Sign-in for the workspace service with a fine-grained personal access token,
  sealed into the session cookie and not stored, and an access check that asks
  GitHub as the user and refuses public repositories (ADR-0032).
- The workspace service reads a repository as the signed-in user: its open pull
  requests, one pull request with its merge base and changed files, the files
  of a commit, and literal text search at a commit. A commit's files come from
  GitHub as one archive and are held in memory, bounded by configured limits
  and keyed by repository and commit together (ADR-0030).
- The workspace's web application, in `web/` and embedded in the service: sign-in,
  the list of open pull requests, a pull request's description and diff in two
  columns or one, a code view for any file at a commit, text search, keyboard
  shortcuts with a touch route for each, and links to GitHub for commenting. It
  is served under a content security policy that allows scripts and requests
  from the service alone. `THIRD_PARTY_LICENSES.md` gains its packages, and
  `tools/checklicenses` reads them from the web application's lockfile.

- `reviewer init` writes a second workflow, `.github/workflows/review-index.yml`,
  into a repository with a root `tsconfig.json`. On every push it builds a
  navigation index of the TypeScript code and saves it as a workflow artifact,
  for the review workspace described in `docs/designs/review-workspace.md`
  (ADR-0034). An existing install gains this one file on its next `init`; every
  other file is left as it is. Deleting the workflow turns that navigation off
  and changes nothing else.

## [0.1.0] - 2026-09-16

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
- `tools/audithistory`, which scans every object in the history — not the working
  tree — for detail belonging to somewhere else and for files under a licence
  incompatible with MIT. Run on every pull request and on `main`.
