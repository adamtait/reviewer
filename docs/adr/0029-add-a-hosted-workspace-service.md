<!-- SPDX-License-Identifier: MIT -->
# ADR-0029 — Add a hosted, stateless workspace service beside the CLI

- **Status:** Proposed
- **Date:** 2026-11-24
- **Implemented by:** PR-48
- **Supersedes:** —
- **Superseded by:** —

## Context

ADR-0008 made this project a command-line tool and not a service, and that decision stands for
reviewing a pull request: a review is a batch job that reads a diff and posts comments.

The review workspace (`docs/designs/review-workspace.md`) is a different kind of thing. It is a
view a person keeps open beside GitHub's pull request page: they follow a symbol into code the
pull request does not touch, search the repository at the pull request's commit, and ask a model
about what they are reading. None of that fits a program that runs once in CI and exits. It needs
something that answers a browser.

Three constraints shape what that something is. It reads pull requests whose authors it does not
trust, so it must never run their code. It is run by one person for about ten users. And its first
version is a test, with two feedback weeks that can stop it.

This record is `Proposed` while that first version is on trial. If the trial passes, a later change
marks it `Accepted` and records that it supersedes ADR-0008, restating that the CLI stays a
command-line tool. Until then ADR-0008 is unchanged and names no successor.

## Decision

The workspace is a second Go binary in this repository, `cmd/workspace`, with its packages under
`internal/workspace/`. It is one stateless container, limited to one instance, with no database: it
stores no source, no tokens and no conversations, holds what it reads from GitHub in memory, and
does slow work inside the request that needs it. Its own packages never start a program, and its
image holds nothing that could start one beyond the programs it is meant to run. Unlike the CLI,
it refuses to start with a configuration it cannot serve from.

## Consequences

- **Positive:** nothing to back up, migrate or restore, and nothing at rest to breach. A restart
  loses only what can be read from GitHub again.
- **Positive:** "never executes pull request code" is held by construction and checked twice: a
  test that no service package imports the package for starting programs, and a CI check of the
  built image's contents.
- **Positive:** the service reuses the CLI's packages for GitHub, the secrets scanner and model
  access, and ships under the same licence and the same checks.
- **Negative:** one instance is a single point of slowness and of failure. A restart empties its
  memory, so the first request after one reads from GitHub again.
- **Negative:** a conversation with the assistant lives in the browser page and is gone on reload,
  because there is nowhere on the server to keep it.
- **Negative:** this repository now holds a server as well as a tool, with a container image and a
  second place where configuration and secrets are read.
- **Neutral:** the service fails loudly where the CLI never does. ADR-0009's "never block a pull
  request" is about a review and does not govern a server's start-up.

## Alternatives rejected

- **Keep to the CLI and post a summary as a comment** — a comment cannot offer navigation or a
  conversation.
- **A new repository for the service** — it reuses the CLI's packages, and two repositories would
  need their releases kept in step.
- **Managed Postgres for conversations and records** — a driver, migrations, a database in CI, a
  monthly charge and a retention question, for needs the first version does not state.
- **Several instances** — each has its own memory, so a second would fetch every archive again.
- **Each engineer runs the binary locally** — no use from a phone, which the design requires.
- **Write the service in TypeScript** — it could not reuse the CLI's Go packages.

## Revisit when

A feedback week shows users need conversations that survive a reload, or one instance's memory or
cold start is a complaint.
