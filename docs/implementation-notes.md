# Implementation notes — M7

Working log. Deleted by this milestone's final commit; content moves to the PR description.

## PR-43 — README, CONTRIBUTING, SECURITY, templates, example config, plugin guide

### Decisions not in the spec

**The plugin guide gained a section the spec did not ask for.** `docs/plugin-protocol.md`
described the wire format completely and said nothing about the diff filter — which is the
single most expensive thing a plugin author can not know. Five analyzers in this project
have shipped findings that were correct, validated, logged, and invisible. A protocol
reference that omits that is accurate and useless, so "Reporting a finding" now leads with
it, names the five failures concretely, and gives the assertion that would have caught all
of them.

**The example config is verified by the loader, not by proofreading.** `internal/config`
now loads `examples/config.minimal.yaml` through `config.Resolve` in a test. The loader
rejects unknown keys, so a renamed field fails in CI rather than in a stranger's repository.
Documentation drifts silently; this is the only thing that makes it not.

**PR-43's exit criterion is partly unsatisfiable as literally written.** "No `@team`
anywhere in `docs/`" cannot hold, because `docs/implementation-plan.md` is the record of the
decision to drop `@team` naming, and the criterion's own text contains the string. The
intent — no org-specific naming, no internal URL, in anything published — does hold: the
only scoped name in the published documents is `@adamtait`, the project's own npm scope, and
the only URLs are `api.github.com` inside a comment explaining what a reader must fill in,
and this repository's own GitHub links.

### Defects found by walking the README as a stranger

The spec's reviewer question — can a stranger install, configure, get a true finding, and
write a plugin from these files alone — was answered by doing it, in a fresh repository,
with a built binary. Four defects, none of which any unit test could have found:

1. **`reviewer init --dry-run` hung.** It prompted for a model access path and blocked. The
   reader already treated an immediate EOF as "not interactive", but an open stdin with no
   writer never reaches EOF, so it waited for input that could not arrive — no output, no
   explanation, on the first command the README tells a stranger to run. Worse, a dry run
   writes nothing, so the answer was discarded even when given. Now the question is asked
   only when it can be both answered and used: `--provider` is honoured in every case, and a
   dry run says the real run will ask. Regression test asserts with a timeout, because a
   test that checked only the output would pass while hanging the suite.

2. **The example config named the library, not the executable.** `dist/serve.js` instead of
   `dist/main.js` — the exact mistake the installer was fixed for in M3, still sitting in the
   file a stranger copies. It starts a process that exits 0 without handshaking and produces
   "no analyzers ran" with nothing else visibly wrong.

3. **The invocation I had just written into the plugin guide did not exist.** `reviewer
   analyze --base main --only mine --format text`: there is no `analyze` subcommand and
   `--format` is metrics-only. It exits 2. Found by running it.

4. **The example plugin could never produce a visible finding.** `shell-hello` reported at a
   hardcoded `README.md:1`, and its comment defended this as "a demonstration of diff scoping
   rather than a bug". It is not: a stranger who copies the documented example gets a plugin
   that logs a finding and shows none, which reads as a broken plugin. It now extracts the
   first changed file and line from the request — still in POSIX shell with no JSON library,
   so the no-dependencies claim stays honest — and reports nothing, honestly, when there is
   nothing to anchor to.

### The test that should have caught #4, and did not

`TestShellPluginConformance` sent an analyze request with **no changed files**, so it could
not distinguish a plugin that anchors its findings from one that reports at a fixed
location — and the fixed-location plugin is the one whose findings the core drops. It now
sends a real diff and asserts the finding lands inside it, which is the assertion the new
documentation tells every plugin author to write.

`TestReviewEndToEndThroughAPlugin` was worse: it asserted the example's finding **was**
dropped. The example's bug was load-bearing for a test of the core's diff filter, which is
how it survived. The two claims are now separate — the example must produce a visible
finding, and a plugin written to be wrong on purpose covers the drop path.

### Verification

Built the binary, created a fresh repository with a real type error, ran `init` (no prompt,
7 files, every gap explained), ran the review, and got `types/TS2322` on the changed line —
and nothing on the identically-broken file that the change did not touch. Then copied
`examples/plugins/shell-hello/plugin.sh` to `tools/my-plugin`, registered it per the guide,
and got its finding in the report.

**Not verified:** `go install github.com/adamtait/reviewer/cmd/reviewer@latest` and
`npm install --save-dev @adamtait/reviewer-plugin-typescript`, because neither is published
yet — that is PR-45. The walkthrough substituted a local build and a local plugin path. The
install instructions are the one part of the README a stranger cannot yet follow.

## PR-44 — Harden public CI

### The finding that shaped the rest

`TestTheModelLaneIsNotInvokedWhenTheDiffCarriesACredential` — the test asserting that no
diff reaches a model provider once a credential is found, which is the security property
this design is arranged around — **had never run in CI**. It skips when gitleaks is absent,
CI installed no gitleaks, and the job was green. Two other tests were in the same position.

A skipped test and a passing test are the same colour. So CI now sets
`REVIEWER_REQUIRE_TOOLS`, and `testfixture.RequireTool` turns a missing binary into a
failure there while still skipping for a contributor who has not installed three
third-party binaries. If the install step ever breaks, the suite fails rather than quietly
covering less.

### Pinning

`.github/scripts/install-tools.sh` installs gitleaks 8.28.0, opengrep 1.9.0 and osv-scanner
2.2.3 by URL and SHA-256, per platform, and `make tools` runs the same script — so "green
locally" and "green in CI" mean the same thing. gitleaks publishes a checksums file;
opengrep and osv-scanner publish bare binaries, so those hashes were computed from the
downloaded artifacts and recorded. A mismatch exits 1 and installs nothing: a substituted
secrets scanner is not a degraded review, it is a false clean.

Verified by running it into an empty directory on a PATH with none of the three present,
and by pinning a version whose checksum does not match — which refused, installed nothing,
and exited 1.

### opengrep turned out to be installable, and had never worked

M4's PR description recorded that opengrep could not be installed in this environment and
so the analyzer was unverified against the real binary. That was wrong: it installs fine.
Running it immediately produced the defect the earlier note had been standing in for.

**`opengrep scan` rejects `--metrics=off`.** Opengrep dropped Semgrep's telemetry in the
fork, so the flag does not exist, and the analyzer failed on *every* run against a real
binary — surfacing as a warning, fail-open per ADR-0013, which is why it was survivable
enough to go unnoticed. Every test passed, against a fake binary that accepts any flag.
The same invocation appears a second time in `rules test`, with the same flag and the same
result. Both fixed; `--disable-version-check` covers the network-egress concern that
`--metrics=off` was there for.

Verified end to end: the analyzer now produces `conventions/no-console-in-lib` on the right
line of a real file, using the example rule, through the real binary. That closes the gap
M4 stated.

A second, smaller one: opengrep's `--test` reporter writes `✖`, and its Python layer
encodes stdout using the locale, so on a non-UTF-8 runner it died inside its own reporting
with an encoding traceback instead of naming the failing rule. `PYTHONIOENCODING=utf-8` is
locale-independent, which `LC_ALL` is not — `C.UTF-8` exists on Linux and not on macOS.

### Two tests that only ever tested one branch

`TestRulesTest/an absent engine is reported, never passed over` branched on whether
opengrep happened to be on the machine's PATH. So the assertion that matters — a rule pack
is never reported ready when no pattern ran — was tested only on machines *without*
opengrep, and the engine path only on machines with it, and in CI neither ran. The absent
engine is now configured rather than waited for, and the engine path is its own subtest, so
both run everywhere.

Its rule fixture was `id` and `message` and nothing else: no pattern, no language. The
engine accepts that and runs zero tests against it, so the "installed" branch was asserting
that opengrep exits 0 on a pack that tests nothing — the exact outcome the subtest is named
for. Replaced with a real pattern and cases that exercise it, then confirmed by hand that a
pack whose cases do not hold fails.

### CI shape

Matrix over Go 1.24 (the go.mod floor) and 1.25 × ubuntu and macos, `fail-fast: false`; a
Node leg; caches for Go modules, npm and the pinned binaries, keyed on the install script so
a changed pin cannot be served from a stale cache; `concurrency` cancelling superseded
pull-request runs.

One `required` job aggregates the rest, because the matrix's job names contain their
parameters — naming them individually in branch protection means editing it whenever a Go
release lands, and a required check matching no job blocks every pull request. It checks
each result equals `success` rather than using `!failure()`, which a cancelled or skipped
dependency would satisfy.

### docs/architecture.md did not exist

Four earlier PRs list it in their "touches" and the plan puts it in the repository layout,
but it was never written. Written now: the two passes and why the gate is structural rather
than conditional, the plugin boundary, the diff filter and the five analyzers it has caught,
presentation versus severity, fingerprints, the two CI surfaces, and this repository's own
CI.

### Verification

Simulated a clean runner — a PATH with none of the three binaries — ran the install script,
and ran the full suite with `REVIEWER_REQUIRE_TOOLS=1`: green, with the three previously
skipping tests actually executing. Confirmed the guard fails when a tool is absent and the
variable is set, and still skips when it is not.

**Not verified:** the workflow has not run on GitHub — no macOS leg, no Go 1.25 leg, and no
cache behaviour has been exercised. The install script is verified on linux-x86_64 only;
the darwin and linux-arm64 checksums are recorded from the published artifacts but nothing
has run them.

## PR-45 — Release automation for both artifacts

### The pairing is the whole problem

`reviewer init` pins the plugin devDependency to the binary's own version, so a binary
released at 0.2.0 tells every repository it installs into to ask npm for plugin 0.2.0. If
the tag and `plugins/typescript/package.json` disagree, nothing fails here — it fails later,
in a stranger's repository, as an npm error about a version that was never published.

So `verify` runs before anything is built, and refuses on three grounds: the tag and the
plugin's package.json disagreeing; a tag that is not `vMAJOR.MINOR.PATCH` (the installer
pins only when the version looks like a release and otherwise writes `latest`, which would
silently unpin every repository installed by that release); and no `CHANGELOG.md` section
for the version.

Each gate was extracted and run locally against both accepting and refusing inputs, because
a workflow that cannot be executed here is otherwise entirely unverified. `v0.1.0` and
`v0.0.1-rc.1` pass the shape gate; `main`, `v1.2` and `0.1.0` are refused. The skew gate
accepts a matching tag and refuses `v0.2.0` against a 0.1.0 package.json. The changelog gate
accepts both `## [0.1.0]` and `## 0.3.0` heading styles.

### Defects found by building the release

**The package would have published its own tests.** `files: ["dist", ...]` included
`dist/**/*.test.js`, and those import devDependencies a consumer does not install — a
published test file is a broken import waiting for anyone who globs the package. Excluded,
and the release now refuses to publish if any reappear, because the `files` field is easy to
edit without thinking about what it lets through.

**A snapshot build pinned a version that can never exist.** I had set GoReleaser's snapshot
template to `{{ incpatch .Version }}-snapshot`, which is semver-shaped, so
`pluginVersionFor` treated `0.0.1-snapshot` as a release and pinned the plugin to it. The
regex is right — `-rc.1` is a genuine prerelease and does get published — so the fix belongs
in the template: `snapshot-{{ .ShortCommit }}` is not semver, and `init` from a snapshot now
prints "not a release" and writes `latest`, which is true.

**`go mod tidy` had never been run.** GoReleaser's before-hook ran it and corrected
`gopkg.in/yaml.v3` from `// indirect` to a direct dependency, which it plainly is. Harmless,
but it means the tidy state was never checked; it is now, on every release, and a release
from an untidy tree fails rather than quietly tidying.

### Deliberately not done

No `workflow_dispatch` on the release workflow, and no branch trigger. Every path to a
published artifact goes through a tag, so "what was in v0.2.0" is answerable from the git
history without trusting anyone's memory. The tag also re-runs the full suite with the
pinned third-party binaries before publishing anything: CI gates branches, but a tag can
point at a commit no pull request ever gated.

### Verification

`goreleaser check` validates. `goreleaser release --snapshot --clean` produced archives for
linux and darwin × amd64 and arm64 with a SHA-256 checksums file, each archive carrying the
binary plus LICENSE and README. The stamped binary reports its version, and — the thing that
actually matters — a binary built with `-X main.version=v0.1.0` writes
`@adamtait/reviewer-plugin-typescript@0.1.0` into the plan, while a snapshot build writes
`latest` and says why. `npm pack --dry-run` ships `dist/main.js` and zero test files.

**Not verified:** nothing has been published. The release workflow has never run — no
GitHub Release, no npm publish, no provenance attestation, and `NPM_TOKEN` does not exist
yet. The spec's proof calls for a `v0.0.1-rc.1` tag on a scratch branch; that publishes to a
public registry under a name this project does not own yet, so it is left for whoever holds
the npm account. Every gate that would refuse such a tag has been run by hand.

## PR-46 — Audit provenance

### Shapes, not names

The audit matches the *shape* of detail belonging to somewhere else — a hostname under a
private suffix, an RFC 1918 address with a port, an absolute path inside a home directory,
a link into a private workspace, a copyleft licence header — and never a name.

Two reasons, and the second is the one that matters. Writing an organisation's internal
hostnames into a public repository in order to prove the repository contains no internal
hostnames is self-defeating (ADR-0004). And a name list only ever finds what somebody
thought of; shapes keep working for the organisation nobody had in mind, which is every
organisation that forks this.

Credentials are deliberately out of scope: gitleaks already scans the full history in its
own CI job, against rules maintained by people who do that for a living.

### The first draft reported 45 findings, all noise

`\b[a-z0-9.-]*\.(internal|corp|…|test)\b` matched every `order.test.ts` in the repository
and every `SOURCE.test(path)` call. `.test` is a real special-use TLD and also how every
test file here is named, so it is dropped entirely — a leaked `something.test` hostname is
not a risk worth that false-positive rate.

`.local` is the same problem one step down: a real mDNS suffix, and also how half the
properties in any codebase are named. It is kept only where it carries a port or a path,
which `config.local` never does. The tightened pattern requires a hostname to sit where a
hostname can sit — after a scheme, an `@`, or an opening delimiter.

This matters more than the count suggests. An audit whose output is all noise gets skipped,
and then it is not an audit.

### "0 findings" has to be earned

A scanner whose patterns match nothing reports a clean history in exactly the words a clean
history produces. That failure has happened five times in this project already, so the tool
has a test asserting every pattern catches a genuine example, a second asserting ordinary
code does not, and a third that the skip list stays short — the skip list being where an
audit quietly stops looking.

Verified beyond the unit tests: built a repository with a leak committed and then deleted,
so the working tree is clean and the history is not. The audit finds it and exits 1. That
is the whole reason the tool reads git objects rather than files.

### A gap in the ADR-0004 guard

`TestNoEndpointsInTheSource` says "nothing in the core may name a host" and walked `..`
from `internal/config` — which is `internal/`. `cmd/`, `pkg/` and `tools/` were never
checked by the test that claims to check them. Widened to the module root; the tree was
already clean, so it cost nothing, and a probe file in `cmd/` confirms the widened guard
actually reaches there.

### Cost

Distinct blobs, not commits: a file untouched for two hundred commits is one scan. The full
history is 623 blobs and under two seconds, and a pull request's additions are a few dozen —
so CI audits what a branch adds on every push, and the whole history on `main`.

### Not done, and why

The plan's PR-46 also flips the repository to public and tags `v0.1.0`. Neither is mine to
do: visibility is a GitHub setting on an account I am not administering, and the tag
triggers a real publish to npm under a scope whose ownership I cannot verify — the one
action in this plan that is genuinely irreversible. The audit that was supposed to gate both
is in place, clean, and running on every pull request, which is the part that had to exist
first.
