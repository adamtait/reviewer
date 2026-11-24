<!-- SPDX-License-Identifier: MIT -->
# Operating the review workspace

The review workspace is a web application used beside GitHub's pull request page. It adds code
navigation and an AI assistant to a pull request, and leaves commenting, approving and merging to
GitHub. Its design is [`designs/review-workspace.md`](designs/review-workspace.md).

This guide is for the person who deploys and configures it. It grows with each pull request of the
design's plan. **So far the service starts, logs, and answers its health address. It has no
sign-in and shows nothing of a repository yet.**

## What it is

One Go binary, `cmd/workspace`, in one container, with no database. It stores nothing: no source,
no tokens, no conversations. What it knows it holds in memory, and reads again from GitHub after a
restart.

It is a separate program from the `reviewer` command in this repository. Unlike `reviewer`, which
exits 0 whatever happens, it refuses to start with a configuration it cannot serve from.

## Run it locally

```
go build -o workspace ./cmd/workspace

WORKSPACE_COOKIE_KEY="$(head -c 32 /dev/urandom | base64)" \
WORKSPACE_MODEL_KEY_EXAMPLE=your-provider-key \
  ./workspace serve --config examples/workspace.config.yaml

curl http://localhost:8080/healthz
```

`./workspace version` prints the version.

If the configuration has problems, the service prints all of them at once and exits with status 1.

## Configuration

A YAML file named by `--config`. [`examples/workspace.config.yaml`](../examples/workspace.config.yaml)
is a complete one with every setting explained. An unknown key is an error, so a misspelt setting
is reported and not silently ignored.

Nothing that costs money or bounds memory has a default. Leaving a limit out stops the service from
starting, so the service never chooses such a value for you.

| Setting | What it is |
| --- | --- |
| `listen` | The address to serve on. |
| `publicUrl` | Where users reach the service. It must be `https`, because requests carry users' GitHub tokens inside the session cookie. Plain `http` is accepted for `localhost` only. |
| `github.apiBaseUrl`, `github.webBaseUrl` | Where GitHub's API and web pages are. |
| `indexWorkflowFile`, `indexArtifactName` | The file name of the index workflow and the name of its artifact. They must match what `reviewer init` writes into a repository: `review-index.yml` and `review-index`. |
| `sessionDays` | How long a sign-in lasts, from 1 to 366. |
| `accessCacheSeconds` | How long "this user can read this repository" is believed before GitHub is asked again, from 1 to 3600. Access removed on GitHub takes effect within this period. |
| `limits` | Ten bounds on memory, time and model use. Each is described in the example file. |
| `repositories` | The connected repositories. Each has an `owner` and a `name`. |

### Repositories

A connected repository must be private. GitHub lets any token read a public repository, so
connecting one would let any GitHub account sign in. The service will refuse a public repository
once sign-in exists.

A repository gets an assistant when all four of `provider`, `model`, `baseUrl` and `apiKeyEnv` are
set. With none of them set it has navigation and search, which need no model. Setting only some is
an error. The only `provider` this version can call is `gemini`.

`apiKeyEnv` is the name of an environment variable, never the key itself.

### Secrets

Secrets come from the environment and are never written to a file.

| Variable | What it holds |
| --- | --- |
| `WORKSPACE_COOKIE_KEY` | 32 random bytes, base64-encoded. It seals session cookies. Changing it signs everyone out. Generate one with `head -c 32 /dev/urandom \| base64`. |
| The variable each repository's `apiKeyEnv` names | That repository's model provider key. |

## Deploying on Cloud Run

The service is designed for one container on Cloud Run.

Build the image from a checkout with `docker build -t workspace .` and push it to a registry your
project can read. No image is published by this repository.

| Cloud Run setting | Value | Why |
| --- | --- | --- |
| Maximum instances | 1 | Everything the service knows is in memory. A second instance would have its own, empty memory, and would fetch every archive again. |
| Maximum concurrent requests per instance | 250 | The default of 80 is reached by one large pull request, whose diff asks for two files per changed file. With one instance at most, requests over the limit would queue. |
| Request timeout | 900 seconds | An answer is streamed for as long as the model takes, over several model calls. |
| Minimum instances | 0, or 1 | With 0 the container stops after about 15 minutes idle and the next request reads from GitHub again. 1 avoids that at a monthly cost. |
| Memory | See the sizing rule below | |
| Authentication | Allow unauthenticated invocations | The service does its own sign-in. |
| Configuration | Mount the YAML file at `/etc/workspace/config.yaml` | The image's default command reads it there. |
| Secrets | Supply each variable above from Secret Manager | |

### Sizing memory

The container needs at least:

```
limits.cacheBytes            everything held between requests
+ 2 × limits.archiveBytes    two archives being unpacked at once: a head commit and its merge base
+ limits.indexBytes          one index being decoded
+ 256 MiB                    the Go runtime, and the secrets scanner's scratch files
```

With the example file's limits that is 1 GiB + 512 MiB + 128 MiB + 256 MiB, so 2 GiB. If the
container passes its memory limit Cloud Run stops it, and every request in flight fails.

## The log

The service writes one JSON line per request to standard output, which Cloud Run sends to Cloud
Logging. Each line has a request identifier, the route's name, the status and the duration, and,
once sign-in exists, the user's login and the repository, pull request and commit.

The service's own lines never contain source code, a pull request description, conversation text,
search text, a token or a key. Every string it logs passes through a filter that removes GitHub
tokens and the secrets the service was started with.

A response carries its request identifier in the `X-Request-Id` header, so someone reporting a
problem can quote the value that finds its line.

## What the operator can read

Be plain with the team about this:

- **The cookie key.** It opens every session cookie, and so every GitHub token presented while the
  key is in use.
- **The log.** It records who opened which pull request and which file.

The service stores nothing else.

## What the image contains

The image is built on a base with no shell and no package manager, and holds one program, the
service. CI checks this on every pull request with
[`.github/scripts/check-image.sh`](../.github/scripts/check-image.sh), because the service reads
pull requests whose authors it does not trust and must never run their code.
