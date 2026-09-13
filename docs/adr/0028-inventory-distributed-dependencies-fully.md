<!-- SPDX-License-Identifier: MIT -->
# ADR-0028 — Inventory distributed dependencies in full, development dependencies at depth one

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-14
- **Supersedes:** —
- **Superseded by:** —

## Context

ADR-0011 put a machine-checked license inventory in place, reconciled against the real dependency graph
so it cannot drift. That worked while the only dependencies were two Go modules.

Adding the TypeScript plugin's analyzer engines — `eslint`, `typescript-eslint`, `dependency-cruiser` —
brought the npm tree to 143 packages. Inventorying all of them transitively means a table nobody reads,
that changes on every `npm update`, and whose churn trains a reviewer to approve inventory diffs without
looking. An inventory that is skimmed is worth less than a smaller one that is read.

But the reason for having an inventory does not go away, and "it is only a dev dependency" is exactly
the argument that lets a copyleft package into a published artifact by accident.

## Decision

The line is drawn at what is **distributed**, not at what is installed.

- **Distributed dependencies are inventoried in full, transitively.** For the plugin that is the
  production tree, `npm ls --all --omit=dev`, which is currently empty by design: the plugin publishes
  compiled JavaScript and resolves every analyzer engine from the repository under review (ADR-0014).
  For the Go binary it is the whole module graph, because static linking distributes all of it.
- **Development dependencies are listed at depth one only.** Direct entries, reconciled against
  `npm ls --depth=0`, so a new one cannot be added without a line saying what it is for.
- **The copyleft denylist applies to every license the inventory names**, in either category. A dev
  dependency with a copyleft license is still a question worth answering before it is added.

## Consequences

- **Positive:** the inventory stays readable and its diffs stay meaningful. The distributed section
  being empty is itself informative — a reviewer seeing a package appear there knows something changed
  about what ships.
- **Negative:** a copyleft package buried in a dev dependency's transitive tree is not caught. That is
  a real hole, mitigated only by those packages not being distributed. If the plugin ever gains a
  production dependency, its whole tree comes into scope and the table grows accordingly — which is the
  correct trigger, because that is the moment it starts mattering.
- **Neutral:** two npm reads instead of one, and one more section in the document.

## Alternatives rejected

- **Inventory all 143 transitively** — an unreadable table, churning on every update, that nobody
  reviews. The check would still pass and would stop meaning anything.
- **Drop dev dependencies from the inventory entirely** — removes the one line of defence at the point
  a dependency is *added*, which is when the decision is actually made.
- **Generate the inventory instead of hand-writing it** — loses the "Why" column, which is the part a
  human gets value from; a generated list of names is `package-lock.json` with extra steps.

## Revisit when

The plugin gains a production dependency, or a licence-scanning tool with a trustworthy transitive
report becomes worth the dependency it would itself add.
