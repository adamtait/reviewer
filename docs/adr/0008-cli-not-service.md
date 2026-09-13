<!-- SPDX-License-Identifier: MIT -->
# ADR-0008 — Ship a CLI binary, not a service

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-07
- **Supersedes:** —
- **Superseded by:** —

## Context

The system needs to run on three surfaces: a CI job on a pull request, an agent that reviews before
opening one, and an engineer's machine. The conventional shape for the first of those is a service
behind a webhook.

A service brings a host, a deployment, a TLS certificate, an authentication story, a secret store, an
upgrade path, and someone to page when it stops. All of that is infrastructure this project would own
forever in exchange for the difference between receiving a webhook and being invoked by a workflow
that already runs on every pull request.

It also fails the other two surfaces. An agent cannot call an internal service from wherever it
happens to run, and an engineer reviewing a local branch before pushing has nothing to send.

## Decision

The system is a single command-line binary. CI invokes it from a workflow step, the agent shells out
to it, and an engineer runs it against a working tree. It holds no state between runs: what has to
persist lives in GitHub (ADR-0016), and what has to be configured lives in the repository under
review (ADR-0004).

## Consequences

- **Positive:** nothing to deploy, nothing to authenticate to, nothing to page anyone about. The same
  code path serves all three surfaces, so a bug found on one is fixed for all. Running it is how you
  debug it.
- **Negative:** no cross-run state, no scheduling of its own, and no way to react to an event that is
  not a process invocation. Anything needing liveness must be driven by something that already runs —
  the Action, or the local poller (ADR-0019).
- **Neutral:** startup cost is paid per run. Against a fifteen-minute latency budget this is noise.

## Alternatives rejected

- **A webhook service or GitHub App** — the infrastructure above, plus an approval to deploy it, in
  exchange for a trigger mechanism the Action already provides.
- **A long-running local daemon** — state and liveness without a deployment, but it is a process that
  can be stale, wedged or forgotten, and the poller gets the same result from a scheduled invocation.

## Revisit when

Something the tool must react to is not expressible as "a process ran on a repository" — for example
review latency that requires reacting to a push within seconds rather than a workflow's start-up time.
