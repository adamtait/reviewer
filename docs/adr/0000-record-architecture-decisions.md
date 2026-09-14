<!-- SPDX-License-Identifier: MIT -->
# ADR-0000 — Record architecture decisions in an append-only log

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-00
- **Supersedes:** —
- **Superseded by:** —

## Context

This project makes a long run of decisions that are cheap to make and expensive to revisit: a wire
protocol, a findings schema, a persistence strategy, a host language. Six months on, the code shows
*what* was chosen and never *what else was considered*, so every re-examination restarts from zero
and tends to relitigate settled ground.

A decision log only works if it is trusted, and it is only trusted if it cannot be quietly rewritten.
A log where a wrong decision can be edited into a right one after the fact records nothing.

## Decision

Every architecturally significant decision gets one file in `docs/adr/`, named `NNNN-kebab-slug.md`,
following `docs/adr/template.md`. An ADR lands in the same pull request as the code that first
implements its decision, so a reviewer judges the rationale and the implementation together;
decisions antecedent to any code land in PR-00. Once an ADR is `Accepted` its body is immutable — a
decision that turns out wrong is **superseded** by a new, higher-numbered ADR, never edited.
`tools/checkadrs` enforces this in CI.

## Consequences

- **Positive:** the rejected alternative and its cost survive next to the decision. Supersession
  chains make the history of a decision readable in order.
- **Negative:** correcting a typo in an Accepted ADR requires a status-block-only edit or a new ADR.
  Accepted friction: the rule is only worth having if it has no convenient exception.
- **Neutral:** ADR prose is excluded from a PR's diff budget, so recording a decision never pressures
  an author to skip it.

## Alternatives rejected

- **A single `DECISIONS.md` changelog** — loses per-decision provenance; an edit to one entry is
  invisible in review among edits to twelve others.
- **Decisions in commit messages only** — unsearchable in practice, and a squash merge destroys them.
- **A wiki or an issue tracker** — not versioned with the code, not reviewable in a diff, and not
  present in the tarball someone downloads five years from now.

## Revisit when

Fewer than three ADRs are written in six months of active development — the log has become
ceremony rather than record, and the honest move is to delete it rather than pretend.
