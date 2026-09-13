<!-- SPDX-License-Identifier: MIT -->
# ADR-0014 — Host every TypeScript analyzer in one warm plugin sharing one program

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-13a
- **Supersedes:** —
- **Superseded by:** —

## Context

Five of the deterministic analyzers are TypeScript compiler consumers: `tsc`, typescript-eslint's
type-aware rules, type-coverage, and to a lesser degree Knip and dependency-cruiser. Three of them need
a `ts.Program` built over the same `tsconfig`, and building that program — plus computing
`getPreEmitDiagnostics` over it — is the dominant cost of a run on any repository large enough to care.

This is the cost that decided the host language. The weighted evaluation in the plan's Appendix A
scored Go at 63 against TypeScript's 95, and the gap was almost entirely this one line: a design where
each analyzer is a short-lived subprocess builds the program three times per run. That assumption, not
Go, produced the low score.

## Decision

One plugin process serves every TypeScript analyzer and stays alive for the whole run. `program.ts`
builds at most one `ts.Program` per resolved `tsconfig` path per process, caches it along with its
diagnostics, and hands the same object to every analyzer that asks.

The compiler itself is resolved from the **repository under review**, not from the plugin, using
`createRequire` against the repository root. A plugin installed as a devDependency of that repository
sits inside its `node_modules`, so this gets the version the repository's own build and editor use.
When it cannot be resolved the plugin's own copy is used and every analyzer says so in a warning,
because analysing with a different compiler than the author runs produces findings they cannot
reproduce.

The cache is keyed on the config path and is deliberately **not** shared across runs: the working tree
has changed by then, and a stale program reports findings about code that no longer exists.

## Consequences

- **Positive:** one compiler pass per run instead of three. This is what makes a Go core affordable,
  and it is measurable — `buildCount()` exists so a test can assert that three analyzers cost one
  build rather than trusting the claim.
- **Negative:** the analyzers share a process, so they share its failure modes. A crash in one takes
  the others with it, where a process per analyzer would have lost only the one. Contained by the host
  rather than by the plugin: a dead plugin is dropped with a warning and the run continues (ADR-0013).
  Memory is also shared, and a program over a very large repository is not small.
- **Neutral:** the plugin has no runtime dependencies of its own and resolves everything from the
  repository under review, which keeps the license inventory small but makes the plugin's behaviour
  depend on a tree it does not control.

## Alternatives rejected

- **A process per analyzer** — the option that scored Go at 63. Triples the dominant cost and directly
  worsens the type-aware lint latency risk the plan already flags as most likely to kill a feature.
- **Caching the program across runs on disk** — the incremental-build problem, with a stale-cache
  failure mode that reports findings about deleted code. TypeScript's own `--incremental` is the right
  tool if this ever matters, and it belongs to the repository, not to this plugin.
- **Shipping the plugin's own typescript and always using it** — reproducible for us, unreproducible
  for the author, whose editor disagrees with the review.

## Revisit when

`--only eslint` exceeds eight minutes cold with this cache in place. At that point the fix is a faster
analyzer, not a faster host — see the plan's kill criteria.
