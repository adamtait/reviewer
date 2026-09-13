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

## What is never written

- No credential, on any path, by any command. `init` writes `.review/.env.example`, which names
  variables and assigns nothing, and a `.gitignore` covering the `.env` you copy it to.
- No endpoint, into this repository's source. `REVIEW_MODEL_BASE_URL` carries yours.
- No model name, into this repository's source. Model names go stale faster than releases;
  `REVIEW_MODEL_NAME` carries yours, so changing it is not a commit.

## Forks

A fork's pull request gets no secrets (ADR-0018). The model lane is therefore skipped on fork
pull requests and the deterministic lane still runs and still comments. That is the intended
degradation, not a limitation to work around.
