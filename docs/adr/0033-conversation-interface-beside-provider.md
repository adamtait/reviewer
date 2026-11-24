<!-- SPDX-License-Identifier: MIT -->
# ADR-0033 — A conversation interface beside `Provider`

- **Status:** Proposed
- **Date:** 2026-11-24
- **Implemented by:** PR-53
- **Supersedes:** —
- **Superseded by:** —

## Context

ADR-0021 put six model access paths behind one interface, `Provider`: a prompt in and text out. It
is that small on purpose. Four of the paths are HTTP APIs and two drive a command-line program
signed in to someone's subscription, and a prompt and its text are all that both kinds can do. The
record names what would reopen it: streaming and cost reporting.

The review workspace's assistant needs both, and one thing more. The model asks for tools, the
service runs them, and the results go back, several times in one question. The answer is shown as
it is written. Tokens are counted, because a question has a limit on them. And the provider tested
refuses the next step of a tool loop unless its own reasoning data from the last step is sent back,
unread.

None of that can be added to `Provider` without taking it away from the two paths that cannot do
it.

This record is `Proposed` while the workspace's first version is on trial. If the trial passes, a
later change marks it `Accepted` and records how it stands beside ADR-0021. Until then ADR-0021 is
unchanged.

## Decision

`internal/model` gains a second interface, `Converser`, beside `Provider`. A conversation is a list
of turns, and a turn is a list of parts: text, a tool call, a tool result, and an opaque part that
carries a provider's reasoning data through without anything reading it. A call reports events as
they happen: more text, a finished part, the token counts, and how it ended. A failure is one type,
with the provider's status, its own code, and any wait it named.

`Provider`, its six adapters and every caller of them are unchanged. The command-line tool goes on
using `Provider`.

The first `Converser` is for Google's `interactions` method, used statelessly and streamed. It is
new code, written against recordings of the live service, and does not reuse the command-line
tool's Google adapter, which rewrites model names and calls a different method. It sends the
configured model name as it is.

Reasoning data stays inside one question. A finished question is replayed to the model as plain
text, which the provider accepts, so reasoning data never reaches the browser and is never stored.

## Consequences

- **Positive:** the command-line tool's model path is untouched, and its two subscription paths
  keep working.
- **Positive:** the adapter's tests replay what the live service sent, and require the adapter to
  send what the live service accepted.
- **Positive:** a second provider is a second adapter behind the same interface. The opaque part is
  what lets a provider with reasoning data of another shape fit.
- **Negative:** two interfaces to a model in one package, and a reader has to know which is for
  what.
- **Negative:** one provider. The concept named two; the second was not available to test against.
- **Negative:** a follow-up question does not carry the files the model read for the earlier one,
  so the model reads them again.
- **Neutral:** an HTTP path only. A conversation cannot be driven through a subscription's
  command-line program.

## Alternatives rejected

- **Extend `Provider`** — streaming, tools and token counts are exactly what ADR-0021 kept out so
  that six unlike paths could share it.
- **Reuse the existing Google adapter** — it calls a method with no tool loop of this shape, and
  rewrites the model name it is given.
- **Use the provider's stored conversations** — the provider would then hold the conversation, and
  repository content with it. Stateless calls keep nothing there.
- **Read the reasoning data, or drop it** — reading it ties the service to one provider's format.
  Dropping it was tried against the live service: the next step of the tool loop is refused.
- **Send reasoning data to the browser with the conversation** — the live test showed a finished
  question does not need it, so it need never leave the request.

## Revisit when

A second provider is added, the provider's method changes shape, or the command-line tool needs
streaming or tools.
