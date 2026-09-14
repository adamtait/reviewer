<!-- SPDX-License-Identifier: MIT -->
# ADR-0011 — Invoke Opengrep only as a separate process

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-09 (enforcement), honoured by PR-27 (the analyzer)
- **Supersedes:** —
- **Superseded by:** —

## Context

Opengrep is how team conventions become executable rules rather than prompt text — the highest-value
part of the deterministic lane over time. It is licensed LGPL-2.1.

This project is MIT (ADR-0001). The LGPL's obligations attach on linking, and the boundary between
"linked" and "merely invoked" is the thing that decides whether an MIT project stays MIT. That
boundary is easy to hold deliberately and easy to cross by accident: one convenient binding package,
added by someone who has not read this file, and the licensing position changes silently.

The same reasoning applies to Semgrep, from which Opengrep is derived.

## Decision

Opengrep is spawned as a separate process — `opengrep scan --json` — and nothing else. No Go module,
no npm package, no cgo, no vendored source. The analyzer that uses it parses its JSON output like any
other external tool's.

This is enforced rather than documented. `tools/checklicenses` fails the build if `opengrep` or
`semgrep` appears in the resolved Go module graph, in the plugin's npm tree, or as text in `go.mod` or
`package.json` — the last because a dependency that never resolved still states intent. Separately,
no linked dependency may carry a copyleft license at all.

The binary is not shipped with this project and is not required by it: when it is absent the analyzer
reports itself unavailable and the run continues (ADR-0013).

## Consequences

- **Positive:** the licensing position is a build failure away from changing, not a code review away.
  Opengrep can be upgraded independently of this project, and a destination repository pins whichever
  version it wants.
- **Negative:** process startup per run, JSON parsing instead of a typed API, and no access to
  Opengrep's internals for anything richer than its documented output. Rule authors must install the
  binary themselves.
- **Neutral:** the same boundary applies to gitleaks and osv-scanner, which are permissively licensed
  and would not have needed it. Treating all three identically means the analyzer code has one shape.

## Alternatives rejected

- **A Go binding** — convenient, and it makes this project's licensing a question that needs a
  lawyer rather than a test.
- **Vendoring the rule engine** — the same problem with a worse upgrade story.
- **Writing our own pattern matcher** — Opengrep's value is that rules read like the code they match,
  which is a large amount of work to reproduce and the reason rule-writing takes minutes.

## Revisit when

Opengrep changes license, or its JSON output stops carrying enough to build a finding from.
