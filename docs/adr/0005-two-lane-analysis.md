<!-- SPDX-License-Identifier: MIT -->
# ADR-0005 — Declare each analyzer's lane in its plugin descriptor

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-04
- **Supersedes:** —
- **Superseded by:** —

## Context

The system has two kinds of analysis with different safety properties. Deterministic analysis needs no
model and makes no network egress. Model-backed analysis sends a diff to a provider, and must be
blocked outright when a credential is found in that diff (ADR-0012) — otherwise the system turns a
leak into a leak plus exfiltration.

That gate is only sound if the core knows, before running anything, which analyzers would reach a
model. The core cannot determine this by inspection: an analyzer is a subprocess in an arbitrary
language, and any analyzer could open a socket.

The obvious implementation is a list in the core of which analyzer IDs are model-backed. That works
exactly as long as every analyzer is first-party. The moment a third-party plugin exists, an analyzer
absent from the list is treated as safe by default, and the gate has a hole that no test in this
repository would catch.

## Decision

Every analyzer declares its lane — `deterministic` or `llm` — in the descriptor it returns from
`describe`, before any analysis runs. The core groups by the declared lane and refuses to send an
`analyze` frame to any `llm`-lane analyzer once the secrets gate has tripped. The lane also travels on
each emitted `Finding`, so a reporter can distinguish facts from judgements without consulting the
registry.

## Consequences

- **Positive:** the gate is enforced against declared intent, with no list in the core to fall out of
  date. A plugin author declaring `llm` opts into being blocked, which is the incentive alignment we
  want.
- **Negative:** the declaration is a promise, not a sandbox. A plugin that declares `deterministic`
  and then calls a model is not prevented, only misreported. Closing that would take process
  isolation with no network namespace, which is disproportionate for a tool a team installs
  deliberately — and is the honest limit of this decision.
- **Neutral:** `lane` becomes part of both the protocol and the findings schema, so it cannot be
  changed without a protocol bump.

## Alternatives rejected

- **A core-side allowlist of model-backed analyzer IDs** — correct until the first third-party plugin,
  then silently wrong in the unsafe direction.
- **Inferring the lane from whether an analyzer's findings carry evidence** — evidence arrives after
  the analyzer has already run, which is far too late for a gate.
- **Running every plugin with no network access unless it declares otherwise** — the right answer if
  this were a hosted service. For a CLI the user installs, the sandbox is theirs to impose.

## Revisit when

A plugin that is not first-party and not audited by the person installing it becomes a normal thing to
run, at which point the declaration needs to become an enforced network restriction.
