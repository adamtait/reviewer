<!-- SPDX-License-Identifier: MIT -->
# ADR-0025 — Speak newline-delimited JSON over stdio

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-04
- **Supersedes:** —
- **Superseded by:** —

## Context

ADR-0006 makes the plugin protocol the only path to an analyzer, so its shape decides who can write
one. The realistic candidates were gRPC by way of `hashicorp/go-plugin`, which is the well-trodden Go
answer, and a line-oriented text protocol over the pipes every operating system already provides.

The protocol also has to carry a findings payload that can reach hundreds of kilobytes on a large
repository, and it has to keep one process alive across many analyzer invocations so shared state —
a TypeScript compiler program — survives between them (ADR-0014).

## Decision

One JSON object per line over stdin and stdout, with a versioned `hello` handshake. Stdout carries
frames exclusively; diagnostics go to stderr. Frame types a receiver does not recognise are ignored
rather than rejected, which makes additions compatible. The protocol version is an integer bumped
independently of the product version.

The reader is built on a growable `bufio.Reader` rather than `bufio.Scanner`, because Scanner's
default 64KB token limit would silently truncate a large findings frame.

## Consequences

- **Positive:** a plugin needs no SDK, no code generation and no JSON library — the 40-line POSIX
  shell example in `examples/plugins/shell-hello` is a real plugin, driven by a Go conformance test on
  every CI run. The whole conversation is greppable in a terminal, which makes debugging a plugin a
  matter of reading it.
- **Negative:** no schema enforcement at the transport layer, no streaming within a frame, and no
  typed errors — the core validates each frame itself and treats a malformed line as a warning against
  that plugin. Large payloads are buffered whole rather than streamed.
- **Neutral:** `schema/plugin-protocol.schema.json` documents the frames for non-Go authors, and is
  the contract rather than generated code.

## Alternatives rejected

- **gRPC via `hashicorp/go-plugin`** — a protobuf toolchain and a code-generation step for every
  plugin author, in exchange for typing the core already does by hand. It would make the shell plugin
  impossible, and that plugin is the conformance test that keeps the protocol honest.
- **A one-shot subprocess per analyzer with JSON on stdout** — simpler, and what the original Go
  evaluation assumed, but it forecloses shared state between analyzers, which is the entire reason a
  Go core is affordable here (ADR-0014).
- **A Unix socket** — the same protocol with a harder setup story and no benefit at this scale.

## Revisit when

Frame time exceeds 10% of a run's wall clock, or the protocol takes more than two breaking changes in
its first three months. The first response is batching `analyze` frames inside the host; adding gRPC
would be treating a design problem as a transport problem.
