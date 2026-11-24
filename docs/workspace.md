<!-- SPDX-License-Identifier: MIT -->
# Operating the review workspace

The review workspace is a web application used beside GitHub's pull request page. It adds code
navigation and an AI assistant to a pull request, and leaves commenting, approving and merging to
GitHub. Its design is [`designs/review-workspace.md`](designs/review-workspace.md).

This guide is for the person who deploys and configures it. It grows with each pull request of the
design's plan. **So far a user can sign in, see the open pull requests of the repositories they can
read, read a pull request's description and diff, open any file at its commit, search the whole
repository at that commit, go to a definition and find references in TypeScript, and jump to GitHub
to comment. The assistant is not built yet.**

## What it is

One Go binary, `cmd/workspace`, in one container, with no database. It stores nothing: no source,
no tokens, no conversations. What it knows it holds in memory, and reads again from GitHub after a
restart.

It is a separate program from the `reviewer` command in this repository. Unlike `reviewer`, which
exits 0 whatever happens, it refuses to start with a configuration it cannot serve from.

## Run it locally

```
npm --prefix web ci && make web     # build the web application into the service
go build -o workspace ./cmd/workspace

WORKSPACE_COOKIE_KEY="$(head -c 32 /dev/urandom | base64)" \
WORKSPACE_MODEL_KEY_EXAMPLE=your-provider-key \
  ./workspace serve --config examples/workspace.config.yaml

curl http://localhost:8080/healthz
```

`./workspace version` prints the version. Then open `http://localhost:8080/`.

Without the first line the service still builds and its API works; the page says the web
application is not in the build.

To work on the web application, run the service as above and `npm --prefix web run dev` beside
it. The development server serves the page with live reload and passes `/api` and `/auth` on to the
service at `http://localhost:8080`, or wherever `WORKSPACE_SERVICE` says.

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

The sign-in page lists these steps and explains each refusal. The same sign-in can be made
without a browser:

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

### Calling the API directly

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
| 502 `github_failed` | GitHub could not be asked, or refused. A refusal usually means the token lacks one of the four read permissions; the log line's `outcome` is then `github_forbidden`. A token without the Actions permission fails this way on the index and on every lookup. |
| 504 `github_timeout` | A fetch passed `limits.fetchSeconds`. |

## Navigation

Go-to-definition and find-references use an index that the repository's own CI builds: the
`review-index.yml` workflow that `reviewer init` writes, which runs `scip-typescript` on every push
and saves the result as an artifact named `review-index`. A repository without that workflow has no
index, and every lookup in it is a text search.

### The four states

When a commit is opened, the service asks GitHub, as the user, for the index workflow's runs for
that commit. The first rule that applies decides:

| GitHub reports | The page says | What happens |
| --- | --- | --- |
| A run that succeeded | Navigation uses the index CI built for this commit | The newest such run's artifact is downloaded, checked and held in memory. |
| Otherwise, a run that has not finished | CI is still building the index | The page asks again every 15 seconds. Lookups use text search until then. |
| Otherwise, a finished run | The index job failed, with a link to the run | Lookups use text search. |
| No run | No index for this commit | Lookups use text search. |

A run counts only if GitHub reports it as a push run of the index workflow file for that exact
commit. A run a pull request started, a run of another workflow and a run for another commit are
ignored.

A successful run can still give no index. The page then says "No index for this commit" with the
reason: the job saved no artifact, the artifact has expired (GitHub keeps one for 90 days unless the
workflow says otherwise), it is larger than `limits.indexBytes`, it is not a zip holding exactly
one file named `index.scip`, it cannot be read, or it describes no file of the commit.

### What the index is trusted with

Nothing it cannot be checked for. A pull request's author controls the job that builds the index,
so the service cannot know an index is honest. It bounds what a false one can do:

- A file the index describes must be a text file of the commit, and every position must lie inside
  that file's text. What does not is dropped.
- A definition is followed only if the text at the target is the same word that was asked about.
  An index cannot send a reader from one name to unrelated code.
- Every result is labelled with where it came from, and the label never says the result is right.

| Label | Meaning |
| --- | --- |
| From the CI-built index | The index gave this place, and the word there matches. Not otherwise checked. |
| From text search | Lines of the commit that contain the word. Not known to be its definition, or all of its uses. |
| Defined outside the repository | The index places it in a named package and version that is not part of the repository. Nothing to open. |
| Not found | Neither the index nor a search found anything. |

A false or partial index can mislead navigation for that commit. It cannot change the diff, the
source shown, or what text search finds. Find-references always shows the text matches beside the
index's list, because an index can cover only part of a repository.

`limits.indexBytes` bounds the artifact, the file inside it and the index built from it. Indexes
share `limits.cacheBytes` with the commits' files.

### Reports that a result was wrong

A result reached from the index carries a "This was wrong" button. Pressing it writes one log line
and nothing else: the service has nowhere to keep it. The line names the lookup, so that whoever
reads the log can repeat it. It carries no text from the user.

```json
{"level":"WARN","msg":"navigation_reported_wrong","owner":"…","repo":"…","sha":"…",
 "path":"src/cart.ts","line":14,"character":12,"kind":"definition","label":"index"}
```

To look into one, open `/{owner}/{repo}/blob/{sha}/{path}#L{line + 1}` and repeat the lookup.

### The navigation week's measures

The design's first feedback week is measured from the log. These queries are for Cloud Logging's
Log Analytics, where the service's lines are rows of a view and each line's fields are under
`json_payload`. Replace `VIEW` with your log view and set the week. **They have not been run
against a deployment: there is none yet. Check each against a known day before relying on it.**

Engineers who opened at least one pull request, and how many different pull requests were opened:

```sql
SELECT
  COUNT(DISTINCT JSON_VALUE(json_payload.user)) AS engineers,
  COUNT(DISTINCT CONCAT(JSON_VALUE(json_payload.owner), '/', JSON_VALUE(json_payload.repo), '#',
                        JSON_VALUE(json_payload.pull))) AS pull_requests
FROM `VIEW`
WHERE timestamp BETWEEN TIMESTAMP('2027-01-11') AND TIMESTAMP('2027-01-16')
  AND JSON_VALUE(json_payload.msg) = 'request'
  AND JSON_VALUE(json_payload.route) = 'pull_view'
  AND JSON_VALUE(json_payload.status) = '200'
```

Lookups for each pull request view, as a median. A lookup's line carries the commit and not the
pull request, so the two are joined on the commit. Lookups from removed lines are made at the merge
base and are not counted by this:

```sql
WITH lines AS (
  SELECT JSON_VALUE(json_payload.route) AS route, JSON_VALUE(json_payload.user) AS user,
         JSON_VALUE(json_payload.sha) AS sha
  FROM `VIEW`
  WHERE timestamp BETWEEN TIMESTAMP('2027-01-11') AND TIMESTAMP('2027-01-16')
    AND JSON_VALUE(json_payload.msg) = 'request' AND JSON_VALUE(json_payload.status) = '200'
),
views AS (SELECT user, sha, COUNT(*) AS views FROM lines WHERE route = 'pull_view' GROUP BY user, sha),
lookups AS (SELECT user, sha, COUNT(*) AS lookups FROM lines
            WHERE route IN ('definition', 'references') GROUP BY user, sha)
SELECT APPROX_QUANTILES(IFNULL(lookups, 0) / views, 2)[OFFSET(1)] AS median_lookups_per_view
FROM views LEFT JOIN lookups USING (user, sha)
```

Reports for every hundred lookups answered from the index:

```sql
SELECT
  COUNTIF(JSON_VALUE(json_payload.msg) = 'navigation_reported_wrong') AS reports,
  COUNTIF(JSON_VALUE(json_payload.msg) = 'request'
          AND JSON_VALUE(json_payload.route) IN ('definition', 'references')
          AND JSON_VALUE(json_payload.label) = 'index') AS lookups_from_the_index
FROM `VIEW`
WHERE timestamp BETWEEN TIMESTAMP('2027-01-11') AND TIMESTAMP('2027-01-16')
```

How lookups were answered, by label, which shows how often the index had nothing to say:

```sql
SELECT JSON_VALUE(json_payload.label) AS label, COUNT(*) AS lookups
FROM `VIEW`
WHERE timestamp BETWEEN TIMESTAMP('2027-01-11') AND TIMESTAMP('2027-01-16')
  AND JSON_VALUE(json_payload.msg) = 'request'
  AND JSON_VALUE(json_payload.route) IN ('definition', 'references')
GROUP BY label ORDER BY lookups DESC
```

The fifth measure is a question the design owner asks the team, and is not in the log.

## The web application

One page, served by the service at every address that is not under `/api/`, `/auth/` or
`/healthz`. The page holds nothing of any repository: what it shows, it asks the API for, with the
session cookie. So the page is the same for everyone, signed in or not.

| Address | What it shows |
| --- | --- |
| `/` | The open pull requests of the connected repositories the user can read. |
| `/signin` | Sign-in. Any address shows it when there is no session, and signing in continues to that address. |
| `/{owner}/{repo}/pull/{number}` | A pull request: title, description, changed files, diff. `#file={path}&line={n}` selects a place. |
| `/{owner}/{repo}/blob/{sha}/{path}` | One file at a commit. `#L{n}` selects a line. |

The two repository addresses mirror GitHub's own, so a GitHub address becomes a workspace address
by changing the host.

What a user should know:

- **The description is shown as plain text.** It is its author's text and is not trusted, so
  nothing in it is drawn as a link, an image or formatting.
- **The diff is drawn between the pull request's head and its merge base**, in two columns on a
  wide screen and one on a narrow one. Each file is read when it nears the screen.
- **Commenting happens on GitHub.** Clicking or tapping a line number offers "Comment on GitHub",
  which opens GitHub's "Files changed" page at that line. On a narrow screen a removed line has no
  line number of its own, so this is offered for lines of the new file only.
- **Search covers the whole repository at the pull request's head commit**, not only the changed
  files, and matches text exactly.
- **Go to definition is `g` then `d`, find references `g` then `r`,** with the cursor on a name.
  A click on a name with Alt, Ctrl or Cmd held goes to its definition. On a narrow screen, a tap on
  a name lists both. A lookup from a removed line is made at the merge base, and any other at the
  head commit.
- **`?` lists the keyboard shortcuts**, each with the way to do the same by touch.

### The content security policy

The page is sent with this header, and the browser enforces it:

```text
default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:;
font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'
```

Scripts run only if they came from the service itself, and the page can send requests only to the
service. A script that reached the page inside a description would not run, and nothing on the page
can send what it shows elsewhere. If you put a proxy in front of the service, it must not remove
or loosen this header. The browser tests run under it.

## Deploying on Cloud Run

The service is designed for one container on Cloud Run.

Build the image from a checkout with `docker build -t workspace .` and push it to a registry your
project can read. No image is published by this repository. The build needs nothing installed but
Docker: it builds the web application in a stage of its own, from the lockfile, and none of that
stage but the built page reaches the image.

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
- **The log.** It records who opened which pull request and which commit, and that they searched or
  looked something up. It does not record what they searched for, which file they read, or where
  in a file a lookup was made. A "this was wrong" report is the exception: its line names the file
  and position, so that the result can be looked into.
- **Memory.** While a commit is held, its text is in the process's memory. Anyone who can read the
  process's memory can read it.

The service stores nothing else.

## What the image contains

The image is built on a base with no shell and no package manager, and holds one program, the
service. CI checks this on every pull request with
[`.github/scripts/check-image.sh`](../.github/scripts/check-image.sh), because the service reads
pull requests whose authors it does not trust and must never run their code.
