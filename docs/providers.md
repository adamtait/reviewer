<!-- SPDX-License-Identifier: MIT -->
# Model access paths

The deterministic lane needs no model. Everything in this document concerns lane B — the
model-backed lane — which is **off in every generated config** and is turned on by editing
`laneB.enabled` in the destination repository's `.review/config.yaml`.

`reviewer init` asks which access path to configure and writes the choice to `laneB.provider`.
It never writes a credential: every variable is named in `.review/.env.example` and read from
the environment at run time. That file lives under `.review/` rather than at the repository root
so that the `.gitignore` `init` writes can cover the `.env` you copy it to.

## The six paths

| `laneB.provider` | What it is | Needs | Works on a hosted runner |
|---|---|---|---|
| `anthropic` | Claude API, billed per token | `REVIEW_MODEL_API_KEY`, `REVIEW_MODEL_NAME` | yes |
| `openai` | OpenAI API, billed per token | `REVIEW_MODEL_API_KEY`, `REVIEW_MODEL_NAME` | yes |
| `gemini` | Gemini API, billed per token | `REVIEW_MODEL_API_KEY`, `REVIEW_MODEL_NAME` | yes |
| `openai-compatible` | any OpenAI-shaped endpoint | `REVIEW_MODEL_API_KEY`, `REVIEW_MODEL_BASE_URL`, `REVIEW_MODEL_NAME` | yes |
| `claude-code` | the Claude Code CLI on a subscription you are signed in to | the `claude` binary on `PATH` | no |
| `codex` | the Codex CLI on a subscription you are signed in to | the `codex` binary on `PATH` | no |

## Choosing

```
reviewer init                          # asks, and lists the six
reviewer init --provider gemini --yes  # no prompt
reviewer init --yes                    # no prompt, no provider: deterministic lane only
```

An install with no provider is complete. The deterministic lane is the one that pays for itself;
the model lane is the one that needs a budget and a decision.

## Where the endpoint comes from

No endpoint is compiled into the binary (ADR-0004). `reviewer init` writes the chosen path's endpoint
into the destination's `.review/config.yaml` as `laneB.baseUrl`, exactly as it writes
`github.apiBaseUrl`, and `REVIEW_MODEL_BASE_URL` overrides it for one machine without editing a
tracked file.

So a gateway, a proxy or a self-hosted server is reached by changing one line of config, on any of the
four API paths — not just on `openai-compatible`. What makes that path different is that it has no
endpoint to write at install, because the endpoint is the thing you are supplying.

With no `laneB.baseUrl` and no `REVIEW_MODEL_BASE_URL`, the model lane reports itself unavailable and
makes no call at all. A provider that silently posted to a compiled-in default would put your diff
somewhere you never named.

## Why `openai-compatible` exists separately

It is the only path that takes a base URL. Everything else about it is the OpenAI request shape,
so a gateway, a proxy, a self-hosted server or an internal endpoint is reachable without naming
that endpoint anywhere in this repository (ADR-0004). If your endpoint speaks the OpenAI protocol,
this is the path, and `REVIEW_MODEL_BASE_URL` is where it goes.

## Why the two subscription paths cannot run in CI

They drive a CLI that is signed in to a subscription held by a person. A hosted runner has no such
session, and putting one there would mean storing that person's credentials in a shared secret.
So on those paths the model lane runs from a machine where someone is logged in — via
`reviewer watch`, or `reviewer` by hand before opening a pull request — and is skipped in the
workflow. `init` says so in the generated workflow, at the place where someone would otherwise
wonder why the lane is quiet.

The deterministic lane runs in CI on every path.

## In CI: one secret, the rest as variables

The API key is a credential and goes in a repository **secret**. The model name and, for
`openai-compatible`, the base URL are configuration, and the generated workflow reads them as
repository **variables** (`vars.REVIEW_MODEL_NAME`, `vars.REVIEW_MODEL_BASE_URL`). Both are read
only from the environment — nothing is written into `.review/config.yaml` — so a workflow carrying
the key alone would give the model lane a credential and nothing to call.

Turning the lane on in CI is therefore: set the secret, set the variables, flip
`laneB.enabled: true`. The workflow itself needs no edit.

## The subscription paths, in detail

`claude-code` and `codex` spawn the CLI you already have, non-interactively, and read its JSON output.
There is no key and no endpoint to configure — that is the whole point of offering them.

Two properties of that boundary are worth knowing, because they are the things that would otherwise
go wrong quietly:

**The prompt goes on stdin, never on the command line.** An argument list is world-readable: `ps`
shows it to every other user on the machine, and it reaches process accounting, audit logs and crash
reports. The prompt contains your diff. A test asserts that neither the system prompt nor the diff
appears in the spawned process's argv.

**A hung CLI is killed, and so is everything it started.** These tools can sit waiting for input if
they decide the session needs re-authenticating, so each invocation is bounded and runs in its own
process group — a timeout kills the group rather than leaving orphans behind holding your terminal.

Pin the executable with `tools.claude-code.path` or `tools.codex.path` in `.review/config.yaml` if it
is not on `PATH`.

## What is never written

- No credential, on any path, by any command. `init` writes `.review/.env.example`, which names
  variables and assigns nothing, and a `.gitignore` covering the `.env` you copy it to.
- No endpoint, into this repository's source. `REVIEW_MODEL_BASE_URL` carries yours.
- No model name, into this repository's source. Model names go stale faster than releases;
  `REVIEW_MODEL_NAME` carries yours, so changing it is not a commit.

## The agent skill

`reviewer init` also writes `.agent/skills/code-review/`, so an agent working in the
destination repository reviews a change before pushing it rather than after somebody else has read it.

The skill's value is in what it says about *when* to run and how to read the result, not in the
command. It draws the one distinction that decides what to do with a finding: high-confidence findings
are **facts** from a compiler or a pattern match, and everything else is a **suggestion** from a model
reading the diff. A suggestion you disagree with is not a finding you have to argue against.

It also names the three failure modes worth naming: suppressing a finding instead of fixing it, acting
on a finding about code the change did not touch, and reading an empty result as approval.

## Forks

A fork's pull request gets no secrets (ADR-0018). The model lane is therefore skipped on fork
pull requests and the deterministic lane still runs and still comments. That is the intended
degradation, not a limitation to work around.
