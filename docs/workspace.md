<!-- SPDX-License-Identifier: MIT -->
# Operating the review workspace

The review workspace is a web application used beside GitHub's pull request page. It adds code
navigation and an AI assistant to a pull request, and leaves commenting, approving and merging to
GitHub. Its design is [`designs/review-workspace.md`](designs/review-workspace.md).

This guide is for the person who deploys and configures it. It grows with each pull request of the
design's plan. **So far the service signs users in and answers, over its API, with a repository's
open pull requests, one pull request's description and changed files, the files of a commit, and
text search at a commit. It has no web page yet: every step below is an API call until the web
application arrives.**

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
connecting one would let any GitHub account sign in. The service refuses a public repository for
every user; see "Who can see what" below.

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

## Signing in

A user signs in with a fine-grained personal access token of their own. The service keeps no
credential of its own for GitHub: everything it reads, it reads as the user who asked.

### Creating a token

On GitHub, under Settings, Developer settings, Personal access tokens, Fine-grained tokens:

1. Set the resource owner to the owner of the connected repositories.
2. Choose "Only select repositories" and pick the connected ones.
3. Under repository permissions, set these four to **Read-only** and leave the rest at "No access":
   Contents, Pull requests, Metadata, Actions.

In an organisation, an owner must by default approve the token before it reads anything private.
Until then sign-in is refused with the third message below.

Two limits come from GitHub. A token covers one owner, so one deployment serves one organisation's
repositories. And an outside collaborator cannot use a fine-grained token on an organisation's
repositories, so cannot sign in.

### What sign-in checks

A token is accepted only if all three hold:

| Check | Refusal code | What the user is told |
| --- | --- | --- |
| It is a fine-grained token | `not_fine_grained` | Classic tokens are not accepted, because they can carry write access to every repository the user can reach. |
| GitHub accepts it | `rejected` | It may have expired or been revoked. |
| It can read at least one connected repository | `no_repository` | It may be waiting for an organisation owner's approval. |

The service cannot check which permissions a token carries. It makes no write call to GitHub,
whatever the token allows.

Until the web application exists, sign in with:

```
curl -i -c cookies.txt -H 'X-Workspace-Request: 1' \
  -d '{"token":"<the token>"}' http://localhost:8080/auth/token
curl -b cookies.txt http://localhost:8080/api/v1/me
```

### The session

The token, the login and an expiry time are sealed into a cookie with `WORKSPACE_COOKIE_KEY`. The
cookie cannot be read by scripts and is not sent with requests another site makes. Nothing about
the session is kept on the server, which has three consequences:

- Signing out clears the cookie in that browser only.
- A copy of the cookie works until it expires, after `sessionDays`, or until the token is revoked
  on GitHub. Revoking the token is how to end a session for certain.
- Changing `WORKSPACE_COOKIE_KEY` signs everyone out.

### Who can see what

Before anything of a repository is shown, the service asks GitHub, with the user's own token,
whether that user can read it. A yes is believed for `accessCacheSeconds`; a no is asked again every
time, so access granted on GitHub works at once and access removed stops within that period.

A repository the user cannot read, one that is not connected, and an address that does not exist
all get the same "not found" response.

If a connected repository turns out to be public, the service refuses it for every user and logs
`a connected repository is public and is refused for every user`. Remove it from the configuration,
or make it private and restart.

## Reading source

The service holds no copy of a repository. The first time a user opens a commit, the service asks
GitHub for that commit's archive, with that user's token, and unpacks it in memory. Later requests
for the same commit are answered from memory until the entry is dropped to make room, or the
container restarts.

| What is kept | Rule |
| --- | --- |
| A file's text | The file is valid UTF-8, holds no NUL byte, and is at most `limits.fileBytes`. |
| A path with a mark | Every other entry: `binary`, `too_large`, or `other` for a link or a submodule. |
| Nothing | A commit whose kept text and paths pass `limits.archiveBytes`. The request is refused with `too_large`, and the refusal is remembered so the archive is not fetched again. |

Four limits govern this:

| Limit | What it bounds |
| --- | --- |
| `limits.fileBytes` | One file's text. Larger files are listed and not kept, so they cannot be read or searched. |
| `limits.archiveBytes` | The kept text and paths of one commit. |
| `limits.cacheBytes` | Everything held in memory together. When it is full, the entry used longest ago is dropped. |
| `limits.fetchSeconds` | One fetch from GitHub, counted from the first request that needed it. After it, waiting requests get `github_timeout`. |

Requests for the same commit of the same repository share one fetch, and at most two fetches run at
once. A third waits.

Files that GitHub leaves out of an archive are absent. A repository can mark paths `export-ignore`
in `.gitattributes`, and those are left out.

### What a first open costs

Through the service's own code, this repository's archive at one commit took 0.97 seconds to fetch
and unpack: 301 files and 2.2 MB of kept text. A request for the same commit afterwards took 13
microseconds. That is the only measurement so far, and it is of a small repository.

For your own repositories, read it from the log. Every fetch writes one line:

```json
{"level":"INFO","msg":"archive_fetched","owner":"…","repo":"…","sha":"…","took_ms":972,
 "kept_bytes":2227815,"entries":301,"cache_bytes":2227815,"cache_entries":1}
```

`took_ms` is the transfer and the unpacking together. `cache_bytes` against `limits.cacheBytes`
shows how full memory is. A fetch that fails writes `archive_fetch_failed` with the reason.

### Trying it without the web application

```
BASE=http://localhost:8080/api/v1/repos/<owner>/<repo>
curl -b cookies.txt $BASE/pulls
curl -b cookies.txt $BASE/pulls/<number>
curl -b cookies.txt $BASE/commits/<full hash>/tree
curl -b cookies.txt "$BASE/commits/<full hash>/file?path=README.md"
curl -b cookies.txt -H 'X-Workspace-Request: 1' \
  -d '{"query":"TODO","pathPrefix":"src/"}' $BASE/commits/<full hash>/search
```

A commit is named by its full 40-character hash. A branch name or a short hash is refused. A pull
request's response gives the two hashes its diff is drawn between: `headSha` and `mergeBaseSha`.

Search matches the query as literal text, ignoring case unless `caseSensitive` is true, and returns
at most 200 lines. The list of pull requests shows at most 300, most recently updated first.

### Errors

| Status and code | Meaning |
| --- | --- |
| 400 `bad_request` | A malformed request: a commit not named by its full hash, a search under 2 characters, a non-GET without the `X-Workspace-Request: 1` header. |
| 401 `signed_out` | No session, an expired one, or a token GitHub no longer accepts. |
| 404 `not_found` | An address, repository, pull request, commit or file that does not exist, or that this user cannot read. The response does not say which. |
| 413 `too_large` | The commit passes `limits.archiveBytes`. |
| 502 `github_failed` | GitHub could not be asked, or refused. A refusal usually means the token lacks one of the four read permissions; the log line's `outcome` is then `github_forbidden`. |
| 504 `github_timeout` | A fetch passed `limits.fetchSeconds`. |

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

The service unpacks at most two archives at once, which is what makes the second line a bound and
not a guess. Its count of bytes is a low estimate: it counts text and paths, not the structures
that hold them. Leave headroom rather than sizing to the exact sum.

## The log

The service writes one JSON line per request to standard output, which Cloud Run sends to Cloud
Logging. Each line has a request identifier, the route's name, the status and the duration, and,
for a signed-in user, their login and the repository, pull request and commit.

The service's own lines never contain source code, a pull request description, conversation text,
search text, a token or a key. Every string it logs passes through a filter that removes GitHub
tokens and the secrets the service was started with.

A response carries its request identifier in the `X-Request-Id` header, so someone reporting a
problem can quote the value that finds its line.

## What the operator can read

Be plain with the team about this:

- **The cookie key.** It opens every session cookie, and so every GitHub token presented while the
  key is in use.
- **The log.** It records who opened which pull request and which commit, and that they searched.
  It does not record what they searched for, or which file they read.
- **Memory.** While a commit is held, its text is in the process's memory. Anyone who can read the
  process's memory can read it.

The service stores nothing else.

## What the image contains

The image is built on a base with no shell and no package manager, and holds one program, the
service. CI checks this on every pull request with
[`.github/scripts/check-image.sh`](../.github/scripts/check-image.sh), because the service reads
pull requests whose authors it does not trust and must never run their code.
