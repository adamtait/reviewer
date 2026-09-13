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
