<!-- SPDX-License-Identifier: MIT -->
# ADR-0006 — Reach every analyzer through the plugin protocol, first-party included

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-04
- **Supersedes:** —
- **Superseded by:** —

## Context

ADR-0002 puts the core in Go while the highest-value analyzers are Node libraries, so some
out-of-process mechanism is required regardless. The question is whether that mechanism is the only
path or an escape hatch beside a privileged in-process one.

The tempting arrangement is: first-party analyzers link into the core, and plugins exist for everyone
else. It is less code on day one. It also means the protocol is exercised only by third parties, which
is the worst possible test distribution — the path the maintainer never runs is the path that breaks,
and it breaks for people who cannot fix it.

## Decision

Every analyzer reaches the core as a plugin over the protocol in `pkg/plugin`. There is no in-process
analyzer interface. The native Go analyzers that spawn pinned binaries — gitleaks, Opengrep,
osv-scanner — are registered through the same descriptor mechanism as an external plugin, and the
TypeScript analyzers arrive over stdio from a Node process.

## Consequences

- **Positive:** the protocol is on the critical path of every run, so a regression in it fails the
  maintainer's own CI before it reaches anyone else. Adding a target language is additive by
  construction. `--only` and `--skip` work uniformly.
- **Negative:** serialisation and a process boundary on work that could have been a function call.
  Measured against a 15-minute latency budget this is noise, but it is not free, and it is what §9's
  protocol-overhead kill criterion watches.
- **Neutral:** the core has no analyzer-specific code at all. Analyzer behaviour is a plugin concern,
  which makes the core smaller and the plugins individually testable.

## Alternatives rejected

- **In-process first-party analyzers with plugins as an escape hatch** — two code paths, and the
  protocol only gets exercised by people who cannot fix it when it breaks.
- **Plugins as shared libraries** — forecloses non-Go plugins, which is most of the point, and makes a
  crashing analyzer a crashing core.

## Revisit when

Protocol frame time exceeds 10% of a run's wall clock. The first response is batching frames inside
the host, not a privileged in-process path.
