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

Nine jobs, of which one matters to branch protection.

- **test** — the full suite, over the two most recent Go releases × ubuntu and
  macos. It installs gitleaks, opengrep and osv-scanner via
  [`.github/scripts/install-tools.sh`](../.github/scripts/install-tools.sh), by
  pinned version and SHA-256, and a mismatch fails the job rather than falling
  back to an unverified binary. `make tools` runs the same script, so a
  contributor and a runner get the same versions.
- **lint**, **adrs**, **plugin**, **secrets** — staticcheck and licence headers;
  the append-only ADR check and the dependency inventory; the plugin's own Node
  test suite; a full-history secret scan.
- **workspace-image** — builds the review workspace's container image, checks that it holds
  the service and nothing else that can run, and starts it. See "The review workspace" below.
- **web** — the review workspace's web application: lint, type-check, unit tests, and the
  user journeys in a real browser at a desktop size and at a phone's width, under the same
  content security policy the service sends.
- **provenance** — `tools/audithistory` over what a branch adds, and over the whole
  history on `main`. Publishing a repository publishes its history, so this reads
  git objects rather than the working tree.
- **required** — a single job that fails unless every other job succeeded. It
  exists because the matrix's job names contain their parameters, so naming them
  individually in branch protection means editing it every time a Go release
  lands, and a required check matching no job blocks every pull request.

`REVIEWER_REQUIRE_TOOLS` is set for the suite. Three tests skip when a
third-party binary is absent, and a skipped test is indistinguishable from a
passing one in a green job — which is how the test asserting that no diff reaches
a model provider after a credential is found sat here having never once run on a
runner. With the variable set, a missing binary fails instead.

## The review workspace

A second binary, `cmd/workspace`, lives beside the CLI: a web application used beside GitHub's
pull request page, with code navigation and an AI assistant. It is designed in
[`designs/review-workspace.md`](designs/review-workspace.md), operated per
[`workspace.md`](workspace.md), and on trial: its ADRs (0029 onward) are `Proposed`.

Its packages live under `internal/workspace/`, apart from the CLI's, so that one rule can be stated
about all of them: none starts a program. The CLI's analyzers exist to start programs; the service
must never run code from a pull request. `internal/workspace/rules_test.go` holds the service to
that in source, and the **workspace-image** job holds the image to it.

The service is not part of a run of `reviewer`, shares no state with it, and stores nothing. A
commit's files are read from GitHub as one archive, with the token of the user who asked, and held
in memory under a byte limit (ADR-0030).

| Package | What it does |
| --- | --- |
| `wsconfig` | Reads and checks the configuration and the secrets. |
| `obs` | Writes the log, one line per request, with credentials removed. |
| `identity`, `access` | Sign-in, and "can this user read this repository", which is GitHub's answer. |
| `source` | Unpacks a commit's archive into memory; the cache; text search. |
| `catalog` | Lists and describes pull requests. |
| `navigation` | Finds a commit's CI-built index, decodes it, checks it against the commit's files, and answers definition and reference lookups by the design's five rules. The decoder (`internal/scip`) and the run-selection and unzip rules (`internal/scipfetch`) sit outside `internal/workspace` because the CLI reads the same index; `rules_test.go` holds them to the service's rules too. |
| `api`, `api/apitypes` | The route table, the middleware every request passes through, the handlers, and the request and response bodies. |
| `scan` | Runs the secrets scanner over what is about to be sent to a model provider, in one fixed way that content cannot change. |
| `gateway` | The one way out to a model provider: scans, calls the repository's provider, runs the tools the model asks for, applies the limits. |
| `assistant` | Builds a question for the gateway, with the six read-only tools bound to the commit. |
| `web` | Embeds the built web application and serves it under a content security policy. |
| `testgithub` | A stand-in for GitHub's API, for the tests. |

The web application is in `web/`, outside the Go module's packages: React, with CodeMirror for
code and diffs. It holds no data of its own. Everything it shows it asks the service's API for, so
the page itself can be served to anyone. Its types mirror `api/apitypes` by hand, and the recorded
responses under `internal/workspace/api/testdata/examples/` are checked by both sides' tests.
