<!-- SPDX-License-Identifier: MIT -->
# Architecture decision records

One file per architecturally significant decision. Format: `template.md`. Process: ADR-0000.

**Append-only.** An `Accepted` ADR is never edited; a decision that turns out wrong is superseded by
a new, higher-numbered ADR. `tools/checkadrs` enforces this, along with numbering, index sync and
two-way supersession links. Run it with `go run ./tools/checkadrs`.

An ADR lands in the same pull request as the code that first implements its decision. Decisions that
precede any code land in PR-00.

## Index

| ADR | Title | Status | Implemented by |
|---|---|---|---|
| [0000](0000-record-architecture-decisions.md) | Record architecture decisions in an append-only log | Accepted | PR-00 |
| [0001](0001-license-mit-with-spdx-headers.md) | License the project MIT with per-file SPDX headers | Accepted | PR-00 |
| [0002](0002-go-core-typescript-plugin.md) | Implement the core in Go and reach TypeScript tooling through a plugin | Accepted | PR-00 |
| [0003](0003-one-findings-schema.md) | Normalise every analyzer's output to one findings schema | Accepted | PR-02 |
| [0004](0004-repo-specific-values-behind-config.md) | Route every repository-specific value through Config | Accepted | PR-03 |
| [0005](0005-two-lane-analysis.md) | Declare each analyzer's lane in its plugin descriptor | Accepted | PR-04 |
| [0006](0006-all-analyzers-are-plugins.md) | Reach every analyzer through the plugin protocol, first-party included | Accepted | PR-04 |
| [0007](0007-scope-analysis-to-changed-lines.md) | Scope every finding to the changed lines, with no opt-out | Accepted | PR-06 |
| [0008](0008-cli-not-service.md) | Ship a CLI binary, not a service | Accepted | PR-07 |
| [0009](0009-never-block-a-pull-request.md) | Never block a pull request | Accepted | PR-07 |
| [0010](0010-keep-internal-data-out-of-history.md) | Keep secrets and internal data out of this repository's history | Accepted | PR-08 |
| [0011](0011-opengrep-as-separate-process.md) | Invoke Opengrep only as a separate process | Accepted | PR-09 |
| [0012](0012-abort-llm-lane-on-secret-detection.md) | Abort the model lane on any secret detection, and when the scan could not run | Accepted | PR-11 |
| [0013](0013-fixed-analyzer-order-fail-open.md) | Run analyzers cheap-first in a fixed order, and fail open | Accepted | PR-12 |
| [0014](0014-one-warm-plugin-one-ts-program.md) | Host every TypeScript analyzer in one warm plugin sharing one program | Accepted | PR-13a |
| [0015](0015-fingerprint-by-normalized-snippet.md) | Identify a finding by its normalised snippet, never by line number | Accepted | PR-16 |
| [0016](0016-github-comments-as-the-store.md) | Use GitHub's own comments as the only persistent store | Accepted | PR-18 |
| [0017](0017-presentation-gated-on-confidence.md) | Gate presentation on confidence, not severity | Accepted | PR-18 |
| [0018](0018-pull-request-trigger-not-target.md) | Trigger on `pull_request`, never `pull_request_target` | Accepted | PR-21 |
| [0025](0025-ndjson-over-stdio.md) | Speak newline-delimited JSON over stdio | Accepted | PR-04 |
| [0026](0026-two-release-artifacts.md) | Release two artifacts from one tag | Accepted | PR-01 |
| [0027](0027-builtin-plugins-over-in-memory-pipes.md) | Serve built-in analyzers over in-memory pipes | Accepted | PR-10 |
| [0028](0028-inventory-distributed-dependencies-fully.md) | Inventory distributed dependencies in full, development dependencies at depth one | Accepted | PR-14 |
