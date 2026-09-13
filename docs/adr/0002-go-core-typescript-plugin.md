<!-- SPDX-License-Identifier: MIT -->
# ADR-0002 — Implement the core in Go and reach TypeScript tooling through a plugin

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-00 (decision), PR-01 (module), PR-04/PR-04a (protocol), PR-13 (plugin)
- **Supersedes:** —
- **Superseded by:** —

## Context

The review targets are TypeScript repositories, and the highest-value deterministic analyzers —
`tsc`, typescript-eslint, dependency-cruiser, Knip, type-coverage — are Node libraries. Three of
them construct a TypeScript program over the same `tsconfig`, which is the dominant cost of a run.

A weighted evaluation of candidate host languages (`docs/implementation-plan.md`, Appendix A) scored
TypeScript on Node at 95 and Go at 63. That Go score assumed each TypeScript analyzer would run as
its own short-lived subprocess, so the TypeScript program would be built three times per run.

That assumption is wrong if the analyzers share one long-lived process. Rescored with a warm plugin
hosting all six analyzers over one shared program, Go reaches 84. The residual gap is velocity and
the core's inability to review its own TypeScript, not architecture.

Weighed against that: the project is intended to support target languages beyond TypeScript. In a
TypeScript host, a Go or Python analyzer is a bolted-on subprocess escape hatch; in a Go host with a
first-class protocol, every analyzer including the first-party ones is a plugin, so the extension
path is the only path and cannot rot.

## Decision

The core is Go: CLI, config, findings schema, plugin host, sequencer, diff scoping, fingerprinting,
the secrets gate, reporters, the GitHub client, the model providers, the installer and the poller.
All analysis reaches the core over the plugin protocol (ADR-0006, ADR-0025). TypeScript analysis
lives in `plugins/typescript`, a Node plugin that stays warm for a whole run and serves every
TypeScript analyzer from one shared TypeScript program (ADR-0014).

## Consequences

- **Positive:** other target languages are additive by construction. The protocol is a real public
  API with a schema, an SDK and an example plugin, exercised by first-party code on every run.
  A small dependency tree keeps `THIRD_PARTY_LICENSES.md` short and the license check meaningful.
- **Negative:** the Go core gets no dogfooding from the TypeScript analyzers. Mitigated, not solved,
  by pointing self-review at `plugins/typescript/src/` (PR-21). Go analyzers for the core's own code
  are deliberately out of scope. A wire protocol must be designed, versioned and kept stable —
  roughly 740 lines a single-language build would not need — and releases now ship two artifacts
  (ADR-0026).
- **Neutral:** contributors to the core write Go; contributors to TypeScript rules and analyzers
  write TypeScript. The split follows the existing skill boundary rather than cutting across it.

## Alternatives rejected

- **TypeScript on Node** — scored highest, and would keep core self-review and higher solo velocity,
  but makes non-TypeScript analysis a second-class escape hatch.
- **Rust** — the only host that could one day embed `oxc` in-process, but too slow to build solo and
  with no official SDK for one of the six required model providers.
- **Go with a subprocess per analyzer** — the option that scored 63; triples the dominant cost of a
  run and directly worsens the type-aware lint latency risk.

## Revisit when

The plugin protocol, rather than the analysis it carries, becomes the bottleneck — total plugin frame
time exceeding 10% of a run's wall clock, or more than two protocol-breaking changes in the first
three months.
