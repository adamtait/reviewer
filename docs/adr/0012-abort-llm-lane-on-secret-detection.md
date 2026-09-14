<!-- SPDX-License-Identifier: MIT -->
# ADR-0012 — Abort the model lane on any secret detection, and when the scan could not run

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-11
- **Supersedes:** —
- **Superseded by:** —

## Context

The model lane sends a diff to a provider. Developers commit credentials by accident — it is common
enough that an entire category of tooling exists for it, and this system runs one of those tools.

The two facts combine badly. A pipeline that reviews every pull request with a model, and which
happens to run on the pull request where someone pasted a live key into a config file, takes a leak
that was confined to one private repository and sends it to a third party. The developer's mistake was
recoverable by rotating the credential; the pipeline's response makes it a disclosure.

There is a second, quieter version. If the secrets scanner is not installed, or crashes, or its output
cannot be parsed, the system knows nothing about whether the diff is clean. Treating "no findings
reported" as "no secrets present" means the first silently broken scanner turns every review into the
scenario above.

## Decision

A finding under the `secrets/` rule namespace blocks every analyzer in the model lane for that run.
So does a secrets scan that did not complete. The deterministic lane runs in full either way, and the
credential is still reported inline — that is the finding that matters.

Two implementation choices carry the weight:

- The gate matches the **rule namespace**, not an analyzer id. A second or replacement secrets
  scanner closes the gate without the gate being taught about it.
- The run loop is **two passes with the gate between them**, rather than a check inside a single loop.
  The model lane's analyzers are separated from the deterministic ones before anything runs, so there
  is no code path from a finding to a model call — the property is structural rather than a condition
  someone could later move or invert.

The report says the diff was not sent anywhere, because the person reading it has just committed a
credential and needs to know its blast radius.

## Consequences

- **Positive:** a leak stays a leak. The failure mode is losing model-lane findings on exactly the
  pull requests where a human is about to be paying close attention anyway.
- **Negative:** fails closed on a broken scanner, so a repository without gitleaks installed gets no
  model lane at all and is told why. That is a real loss of function in exchange for the guarantee,
  and it is the right direction to fail in.
- **Neutral:** the gate cannot stop a plugin that declares `deterministic` and calls a model anyway
  (ADR-0005's stated limit). It enforces the declared shape of the system, not a sandbox.

## Alternatives rejected

- **Redact the secret and continue** — requires trusting redaction to be complete on a diff containing
  an unknown credential format, and a near miss still sends the surrounding context.
- **Warn and continue** — a warning nobody reads in time is indistinguishable from no gate.
- **Block only on high-confidence secrets** — the confidence of a secrets scanner's own verdict is not
  a number worth betting a disclosure on.

## Revisit when

Never, in the direction of weakening it. A future secrets scanner with a genuinely trustworthy
"this is a test fixture" signal could narrow what counts as a hit — but that is a change to what the
scanner reports, not to what the gate does with a report.
