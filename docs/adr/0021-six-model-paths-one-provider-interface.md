<!-- SPDX-License-Identifier: MIT -->
# ADR-0021 — Six model access paths behind one Provider interface, chosen at install

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-33
- **Supersedes:** —
- **Superseded by:** —

## Context

PLAN §1 said model access would be "injected via env, OpenAI-compatible" — one client, one wire format,
one kind of credential. That is the smallest thing that could work, and it excludes half of what people
actually have.

Two of the six paths this project supports are not HTTP APIs at all. `claude-code` and `codex` drive a
CLI that is already signed in to somebody's subscription: there is no key, no endpoint, and no request
body. A person who pays for a subscription and not for API tokens is exactly the person most likely to
try this tool on a personal project, and an OpenAI-compatible client has nothing to offer them.

The other four differ in ways that look large from inside an SDK and small from outside: a header name,
a JSON envelope, where the system prompt goes. All four answer the same question — here is some text,
give me some text back.

So the design question is where to put the seam, and how much to put through it.

## Decision

One interface, one method:

```go
type Provider interface {
    Name() string
    Complete(ctx context.Context, messages []Message, opts Options) (string, error)
}
```

The narrowness is the point. A subscription CLI cannot stream tokens, report usage, accept a tool
schema, or return log-probabilities. An interface asking for any of those would admit four paths and
quietly exclude two — and the exclusion would show up as a `nil` return or an `ErrNotSupported` in
four places, which is an interface that does not mean what it says.

This tool needs none of them. It asks one question and reads one answer.

Three further consequences follow from where the seam sits:

**Nothing above this package knows which path is in use.** The review pass and the invalidation pass
take a `Provider`. They cannot behave differently for one provider, because they cannot tell.

**Not being ready is a value, not a failure.** `ErrDisabled` covers every way the lane can be off —
switched off, no provider, no model, no key, no binary — each with its own reason. Every caller treats
it as a skip, because a review that stops because a model is unreachable has failed at the job it was
built to do (ADR-0009).

**No endpoint, model name, or organisation appears in this package.** They come from config and the
environment (ADR-0004). `laneB.model` has no default: a model name compiled in here goes stale faster
than a release, and choosing one on someone's behalf spends their money.

The path is chosen at install (ADR-0020), written to `laneB.provider`, and the credential — where
there is one — comes from the environment and is never written to disk.

## Consequences

- **Positive:** a subscription path costs the same amount of core code as an API path, which is the
  only reason the two CLI adapters exist at all. Under an HTTP-shaped interface they would have been
  "maybe later" forever.
- **Positive:** the fake provider is three lines of interface, so every test above this package asserts
  against an exact reply rather than a model's mood. Lane B's logic is fully testable with no network.
- **Positive:** adding a seventh path is one file and one `case`.
- **Negative:** no streaming, so a long review is a long wait with nothing on screen. Accepted: this is
  a batch tool that posts a comment, not a chat.
- **Negative:** no token accounting from the interface, so cost reporting would have to come from each
  adapter separately. Not needed yet; noted as the most likely reason to revisit.
- **Negative:** structured output is requested, not guaranteed. Two paths can enforce a JSON schema and
  the CLIs cannot, so the caller validates every response regardless — which it would have to do
  anyway, since an enforced schema still admits a semantically wrong answer.
- **Neutral:** `Redact` lives here and runs on every provider error. A 401 body frequently echoes the
  key that failed, and an error message is the least guarded thing in the system: it reaches stderr,
  the run log, and through a warning a public pull request comment.

## Alternatives rejected

- **One OpenAI-compatible client**, as PLAN §1 said. Excludes Gemini's wire format and both subscription
  CLIs — that is, the two paths that cost nothing to use.
- **An interface per capability** (`Streamer`, `ToolUser`, `Completer`), with type assertions at each
  call site. Every call site then carries a branch for a capability this tool does not use.
- **A vendor SDK per provider, behind the interface.** Each brings a transitive dependency tree that
  `THIRD_PARTY_LICENSES.md` must inventory and `tools/checklicenses` must reconcile, for a single JSON
  POST. The project has one non-test Go dependency and that is worth keeping.
- **Passing the provider name down**, so callers can adapt. That is the leak this interface exists to
  prevent: within a week something would behave differently for one vendor and nobody would know why.

## Revisit when

Cost becomes something someone needs to see per run, or a review grows long enough that streaming
output is the difference between a tool people wait for and one they cancel.
