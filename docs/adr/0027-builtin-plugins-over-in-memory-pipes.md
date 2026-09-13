<!-- SPDX-License-Identifier: MIT -->
# ADR-0027 — Serve built-in analyzers over in-memory pipes

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-10
- **Supersedes:** —
- **Superseded by:** —

## Context

ADR-0006 makes the plugin protocol the only path to an analyzer, first-party ones included, so that
the protocol is exercised on every run rather than only by strangers. Three analyzers — gitleaks,
Opengrep and osv-scanner — wrap an external binary and need no language runtime of their own, so they
are compiled into the `reviewer` binary.

That leaves a question ADR-0006 did not answer: how does code inside the binary reach the core over a
protocol designed for a child process? Two options both satisfy the letter of ADR-0006. The binary
could re-exec itself as its own plugin, which is what a subprocess-shaped reading implies. Or the
handler could run in a goroutine with the two sides joined by pipes.

## Decision

Built-in analyzers are served by one plugin, `builtin`, that implements `plugin.Handler` and is
registered with `Manager.AddLocal`. The handler runs in a goroutine; host and plugin are joined by two
`io.Pipe`s. Every frame is encoded and decoded by the same codec an external plugin uses, and the
descriptor validation, the lane gate and the per-analyzer timeout are the same code paths.

What differs is only the transport and what "abort" means: a child process has its process group
killed, while a built-in handler has its input closed, because a goroutine cannot be interrupted
mid-call.

## Consequences

- **Positive:** no re-exec, no second process, no argv contortion to make a binary act as its own
  plugin. The protocol stays on the critical path of every run, which is ADR-0006's actual purpose.
  A built-in analyzer is written against the same `Handler` interface a third party implements, so the
  SDK is dogfooded rather than merely published.
- **Negative:** a built-in analyzer is not isolated. It shares the host's address space, so a panic in
  one takes the run down where a child process would only have been dropped with a warning, and a
  hang cannot be forcibly stopped — only abandoned. This is the real cost, and it is the reason
  built-in analyzers are limited to thin wrappers around external binaries: the risky work happens in
  the child process the wrapper spawns, not in the wrapper.
- **Neutral:** frames are still serialised, so the protocol overhead ADR-0006 accepted is paid here
  too. At these volumes it is not measurable.

## Alternatives rejected

- **Re-exec the binary as its own plugin** (`reviewer __plugin builtin`) — full isolation, and a
  hidden subcommand that exists only to talk to itself, plus a process spawn per run for work that is
  already in memory. Worth revisiting only if a built-in analyzer ever does something that can panic.
- **A privileged in-process analyzer interface** — rejected by ADR-0006, and it would mean the
  protocol was only ever exercised by third parties.

## Revisit when

A built-in analyzer does real work in-process rather than spawning a binary, at which point isolation
starts to matter and re-exec becomes the better trade.
