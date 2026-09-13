<!-- SPDX-License-Identifier: MIT -->
# Architecture

How the pieces fit, and which of the arrangements are load-bearing rather than
incidental. The decisions themselves, with their alternatives, are in
[`docs/adr/`](adr/); this is the map.

## One run

```
  git diff ──▶ changed files and line ranges
                      │
                      ▼
        ┌──────── pass 1: the deterministic lane ────────┐
        │  gitleaks  opengrep  osv  tsc  eslint  knip …  │
        └────────────────────┬───────────────────────────┘
                             │  findings
                             ▼
                     ╔═══════════════╗
                     ║ secrets gate  ║   a credential in the diff, or a scan
                     ╚═══════╤═══════╝   that could not run, stops here
                             │
                             ▼
        ┌──────── pass 2: the model lane ────────────────┐
        │  assemble prompt → provider → disprove → cap   │
        └────────────────────┬───────────────────────────┘
                             │
                      diff filter (ADR-0007)
                             │
                   fingerprint, then report
```

Two passes with the gate between them, sequenced by `internal/sequencer`.

## The gate is structural

The model lane does not check whether a credential was found. It is not reached.

`internal/sequencer` runs the deterministic lane, hands the findings to
`gate.Decide`, and only then runs the `llm`-lane analyzers (ADR-0012). "No diff
leaves the machine when a credential was found" is therefore a property of where
the analyzer sits in the sequence, not of a condition inside it — there is no flag
to get wrong, and no third-party plugin can opt out of it, because the lane is
declared on the descriptor and the core groups by lane (ADR-0005).

The gate also fails closed. A secrets scan that could not run is not a scan that
found nothing: the model lane is skipped and the run says why (ADR-0013 buys
fail-open for everything *except* this).

## Every analyzer is a plugin

There is no privileged in-process path. The first-party analyzers speak the same
newline-delimited JSON over stdio that a stranger's plugin does (ADR-0006,
ADR-0025), so the protocol is exercised on every run rather than only by
outsiders. See [`plugin-protocol.md`](plugin-protocol.md).

The Go core owns everything that must be correct regardless of which analyzer
produced it: diff scoping, the gate, fingerprints, presentation, and reporting.
The TypeScript plugin owns the analyzers that need a compiler program, and builds
it once for all of them (ADR-0014).

## The diff filter

A finding whose file and line fall outside the changed ranges is dropped by the
core, with no opt-out (ADR-0007).

This is the constraint that shapes analyzer design more than any other, and the
one that has cost this project the most: a vulnerability scanner reporting every
advisory at line 1 of a lockfile, a dead-code analyzer doing the same, a coverage
analyzer reporting against a baseline file no real change touches, an example
plugin reporting at a fixed path. Each produced correct findings that no human
could ever see, and each passed its unit tests. An analyzer's job includes
anchoring what it found to a line the change touched.

## Presentation is decided by confidence, severity by the finding

Only `high` confidence earns an inline comment; everything else goes into one
collapsed summary (ADR-0017). Confidence is how sure the analyzer is, severity is
how bad the problem is, and conflating them produces a review that interrupts
people about things it is guessing at.

Model-lane findings are additionally capped per category in code rather than by
asking the model to be modest (ADR-0022), and each is put back to the model to be
disproved before it is emitted (ADR-0023).

## Findings survive a force-push

A finding is identified by `sha256(ruleId ‖ filePath ‖ normalisedSnippet)`,
shortened, and carried in an invisible HTML comment on the posted comment
(ADR-0015). Line number and message are deliberately excluded: a rebase moves
every line, and a reworded message is the same problem. This is what lets the
reporter recognise its own comment on the next run instead of posting it again,
and what lets acceptance be measured per rule (ADR-0024).

## Two CI surfaces

A GitHub Action and a local poller, both built unconditionally (ADR-0019). The
Action runs on `pull_request`, never `pull_request_target`, so a fork's code is
never run with a token that can write (ADR-0018).

## This repository's own CI

Six jobs, of which one matters to branch protection.

- **test** — the full suite, over the two most recent Go releases × ubuntu and
  macos. It installs gitleaks, opengrep and osv-scanner via
  [`.github/scripts/install-tools.sh`](../.github/scripts/install-tools.sh), by
  pinned version and SHA-256, and a mismatch fails the job rather than falling
  back to an unverified binary. `make tools` runs the same script, so a
  contributor and a runner get the same versions.
- **lint**, **adrs**, **plugin**, **secrets** — staticcheck and licence headers;
  the append-only ADR check and the dependency inventory; the plugin's own Node
  test suite; a full-history secret scan.
- **required** — a single job that fails unless every other job succeeded. It
  exists because the matrix's job names contain their parameters, so naming them
  individually in branch protection means editing it every time a Go release
  lands, and a required check matching no job blocks every pull request.

`REVIEWER_REQUIRE_TOOLS` is set for the suite. Three tests skip when a
third-party binary is absent, and a skipped test is indistinguishable from a
passing one in a green job — which is how the test asserting that no diff reaches
a model provider after a credential is found sat here having never once run on a
runner. With the variable set, a missing binary fails instead.
