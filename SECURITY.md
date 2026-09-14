<!-- SPDX-License-Identifier: MIT -->
# Security

## Reporting a vulnerability

Open a [private security advisory](https://github.com/adamtait/reviewer/security/advisories/new).
Please do not open a public issue for anything exploitable.

This is a personal project, so there is no response-time commitment. There is a
commitment to fix or to say plainly that it will not be fixed.

## What this tool does with your code

Worth reading before you turn on the model lane, because these are the properties
that decide whether that is safe.

**The deterministic lane makes no network request.** Every analyzer in it runs
locally or spawns a local binary. The one exception is `osv-scanner`, which queries
a vulnerability database, and it is the last analyzer to run for that reason.

**The model lane sends the diff to whatever provider you configured**, along with
the guidance documents your config names and the deterministic lane's findings.
Nothing else. It is off until you enable it, and what goes into the prompt is built
by one pure function so that "what is sent" is answerable by reading it.

**A credential in the diff stops the model lane.** Not by a check inside it — the
secrets scan runs first and the model lane is on the far side of it, so it is never
reached. A scan that could not run counts as a scan that found something.

**Nothing writes a credential to disk.** `reviewer init` writes a `.env.example`
naming variables and assigning nothing, and a `.gitignore` covering the `.env` you
copy it to.

**A credential never reaches an error message**, a log, a URL, or a process
argument list. Provider errors pass through a redactor that strips the configured
key, the configured endpoint, and anything key-shaped; the subscription CLI adapters
put the prompt on stdin because an argument list is world-readable.

## Trust boundaries

**A fork's pull request is untrusted.** The generated workflow triggers on
`pull_request`, never `pull_request_target`, so a fork's pull request gets no
secrets — the model lane is skipped and the deterministic lane still comments. That
is the intended degradation.

**A repository's own files are untrusted input to the model lane.** The diff, the
guidance documents and the analyzer output are all written by whoever opened the
pull request. The prompt says so, the diff is fenced so its content cannot close
the fence, and a prompt override the change itself introduces is ignored — a system
prompt supplied by the branch under review is a branch reviewing itself.

**Paths from config cannot escape the repository.** Guidance and prompt paths are
resolved through symlinks before the containment check, refused if they are not
regular files, and capped in size.

**Spawned tools receive an end-of-options separator.** A repository can track a file
whose name begins with a dash, and without the separator that filename becomes a
flag on a scanner's command line.

## Supply chain

Releases are built from a tag by a workflow, with checksums published alongside.
The GitHub Action pins a version *and* a checksum: a pinned version without a
checksum trusts whoever can move a tag.
