<!-- SPDX-License-Identifier: MIT -->
# ADR-0003 — Normalise every analyzer's output to one findings schema

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-02
- **Supersedes:** —
- **Superseded by:** —

## Context

The system runs nine deterministic analyzers and two model passes, in two languages, across three
reporters and a dedupe layer. Each underlying tool has its own output shape: ESLint messages,
dependency-cruiser violations, SARIF from gitleaks, Opengrep's semgrep-compatible JSON, free-form
JSON from a model.

Everything downstream — diff scoping, fingerprinting, confidence gating, the GitHub reporter, the
acceptance-rate query — has to work uniformly over all of it. If each analyzer kept its own shape,
every one of those would need an adapter per analyzer, and adding a tenth analyzer would mean
touching six places.

## Decision

Both lanes normalise to one `Finding` struct, defined in `pkg/finding` and mirrored by
`schema/finding.schema.json` for plugin authors who are not writing Go. `Evidence` is required when
`Lane` is `llm`, enforced in `Validate()` and in the schema, because a model-produced finding without
its reasoning is indistinguishable from a guess. `Confidence` is a separate axis from `Severity`
precisely so presentation can be gated on the former (ADR-0017).

The schema is authored by hand and kept honest by a reflection test that compares its properties and
`required` list against the struct's fields and `omitempty` tags.

## Consequences

- **Positive:** every reporter, the fingerprinter and the dedupe layer are written once. A new
  analyzer in any language is additive and needs no core change.
- **Negative:** tool-specific richness is lost in translation — ESLint's fix objects, SARIF's code
  flows, dependency-cruiser's full cycle path. Analyzers must flatten to a span and a message, and
  anything that does not fit goes in `Message` as prose.
- **Neutral:** the struct is part of the plugin API. Adding an optional field is compatible;
  removing or renaming one is a protocol break (ADR-0025).

## Alternatives rejected

- **Per-analyzer output shapes with adapters at the reporter** — N×M adapters, and the dedupe layer
  would need to understand every shape to compute a fingerprint.
- **SARIF as the internal format** — solves interchange but is far larger than this system needs, and
  it has no place for `confidence` as a first-class presentation axis, which is the one field the
  whole acceptance-rate strategy turns on.
- **Generating the schema from the struct** — a generator plus its own drift test is more moving
  parts than a hand-written schema plus one reflection test, for a struct of eleven fields.

## Revisit when

A third-party plugin needs to carry analyzer-specific structured data through the core to a reporter.
The answer then is a typed extension field, not a second schema.
