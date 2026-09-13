# Implementation notes

Running log of decisions, deviations and tradeoffs encountered while implementing
`docs/implementation-plan.md`. Anything here was *not* settled by the plan, or contradicts it.

Newest phase last. Each entry says what the plan assumed, what was actually done, and why.

---

## Environment constraints discovered at the start

| Plan assumed | Reality | What I did |
|---|---|---|
| `go 1.25` toolchain directive | Go 1.24.7 is what is installed | `go.mod` says `go 1.24`. Nothing in the code needs 1.25; bump the directive when the build host moves. **Revisit before PR-45** — the release matrix in the plan says "two most recent Go releases". |
| `gh` CLI with stack support for PRs | `gh` is not available in this environment at all | Stacked PRs are created through the GitHub MCP API with each PR's base set to the previous branch in the stack. Functionally identical to a `gh` stack; the branches are `claude/impl-m0`, `claude/impl-m1`, … each based on its predecessor. |
| gitleaks, opengrep, osv-scanner, staticcheck, addlicense, goreleaser available | None installed | `staticcheck` and `addlicense` installed via `go install` (both Go tools, and the module proxy works). The three scanners are *analyzer dependencies*, not build dependencies — the analyzers that spawn them already degrade to a warning when the binary is missing (plan PR-10 exit criterion), so their absence does not block M0/M1. CI installs them by pinned version at PR-44. |

## Phase M0 — Foundation & OSS seam

### Deviation: PR-00 carries a minimal `go.mod`

**Plan:** PR-00 delivers `tools/checkadrs/main.go` and its proof is `go run ./tools/checkadrs`;
PR-01 delivers `go.mod`.

**Problem:** a Go program cannot run without a module. PR-00's stated proof is unrunnable at PR-00,
which breaks the plan's own "green at every commit" constraint.

**Done:** PR-00 carries a two-line `go.mod` (module path + `go` directive) as part of delivering a
runnable Go tool. PR-01 keeps everything else it was scoped for — `LICENSE`, CI, SPDX enforcement,
the `cmd/reviewer` skeleton, ADR-0026. The ordering the plan actually cared about — decisions
recorded before the code that implements them — is preserved.

**Alternative rejected:** writing `checkadrs` as a shell script so PR-00 needs no module. It would
have to be rewritten in Go by PR-09 anyway, when `checklicenses` needs the same git plumbing.

### Deviation: ADR numbering is unique, not sequential

**Plan:** §B.3 rule 1 — `checkadrs` fails on "a duplicate or non-sequential ADR number".

**Problem:** the plan's own register (§B.1) reserves ADR numbers by topic but lands each record with
the PR that implements it. ADR-0026 lands in PR-01 while ADR-0003 lands in PR-02, so the log has
gaps at every point in its life. Enforcing density would have made the first two commits fail.

**Done:** the check requires uniqueness only. A number is an identifier, not a position; the
register is the reservation. Plan §B.3 corrected to match, with the reasoning inline.

### Deviation: the findings JSON Schema is hand-written, not generated

**Plan:** PR-02's proof reads `go run ./tools/genschema && git diff --exit-code schema/`.

**Problem:** `tools/genschema` appears in that proof but in neither PR-02's file list nor the
repository layout — an inconsistency in the plan. Building a Go-struct-to-JSON-Schema generator to
serve one eleven-field struct is more machinery than the problem deserves, and the generator would
need its own drift test anyway.

**Done:** `schema/finding.schema.json` is authored by hand. `TestSchemaMatchesTheStruct` uses
reflection to assert that the schema's `properties` exactly match the struct's JSON field names, and
that the schema's `required` list exactly matches the fields without `omitempty`. Drift in either
direction fails the build, which is what the generator would have bought.

**Cost:** the schema's descriptions, enum values and the conditional `evidence` rule are not derived
from Go and can drift in *content* while field names stay in sync. Judged acceptable: those are the
parts a human reads, and a plugin author reading a stale description is a smaller failure than a
plugin author missing a field entirely.

### Addition: a Makefile as the single source of the check commands

**Plan:** CI commands listed inline in `.github/workflows/ci.yml`; a `Makefile` appears only at PR-08
for the git hooks.

**Why changed:** the `addlicense` invocation needs a non-trivial ignore list (prose, YAML, JSON,
testdata, and the example config a user copies into their own repo — none of which should carry an
SPDX header). Having that list live only in the workflow guarantees that "it passed locally" and "it
passed in CI" eventually diverge, and the first symptom is a red build on a green branch.

**Done:** `Makefile` introduced at PR-04 with `make check` as the whole gate; CI calls the same
targets. The `tools` target pins the same staticcheck and addlicense versions CI installs, with a
comment saying the two must match — a real duplication that is cheaper than a bootstrap script.

### Discovery: `cmd.StdoutPipe` cannot be used with a concurrent `cmd.Wait`

**Plan:** PR-04a specifies spawn, handshake, graceful bye then process-group kill, with four
fault-injection cases including "dies mid-frame".

**Problem found by the mid-frame test:** the host calls `cmd.Wait()` in a goroutine so it can observe
an exit while a read is outstanding. `exec.Cmd.StdoutPipe` documents that this is incorrect — `Wait`
closes those pipes as soon as the process exits, so a plugin that writes half a frame and dies hands
the host `read |0: file already closed` instead of the half-frame. The distinction matters: one says
"the plugin is broken and here is what it wrote", the other says nothing useful at all.

**Done:** the host creates its own `os.Pipe` for each stream, assigns the child's ends to
`cmd.Stdin/Stdout/Stderr`, and closes the parent's copies of the child ends after `Start` so EOF still
propagates. The parent keeps the read ends, so buffered bytes survive the child's death.

### Deviation: the handshake deadline is derived, not fixed at 30s

**Plan:** implied a fixed handshake timeout.

**Problem:** a fixed 30s made the fault-injection suite take 30 seconds, because "never handshakes" is
a test that can only end by timing out. A slow test suite is a suite that stops being run.

**Done:** the handshake deadline is the plugin's own timeout, capped at 30 seconds. A plugin that
cannot introduce itself within its analysis budget will not analyze anything either, and the cap keeps
a cold Node boot on a CI runner from being called a failure. The suite now runs in under a second.

### Correction: the fixture's fake credential was undetectable

**Plan:** PR-10's exit criterion is that the fixture yields
`secrets/generic-api-key src/config.ts:4`.

**Problem:** the credential I first wrote into the fixture (`sk-live-…`, with hyphens) matches no
gitleaks rule. Verified by scanning it directly: "no leaks found". The fixture would have given the
secrets analyzer nothing to find, PR-10's tests would have passed vacuously, and the secrets gate —
the one safety property in the whole system — would have been built on a test that never fired.

**Done:** replaced with a fabricated value in a format gitleaks genuinely matches, verified by
scanning before committing. `testdata/MANIFEST.md` now says not to replace it with an
obviously-fake placeholder, and says why.

**Worth knowing:** my first probe of the hook used AWS's canonical documentation key
(`AKIAIOSFODNN7EXAMPLE`) and gitleaks correctly ignored it — those are in its default allowlist. The
gate does work; testing it needs a plausible fake, not a famous one.

### Review round 1 — seven defects found and fixed

`/review` at high effort on the M0 diff found seven behavioural bugs the suite did not cover. All
fixed, each with a regression test. Three were reproduced with throwaway tests by the reviewer before
being reported.

| Where | Defect | Why it mattered |
|---|---|---|
| `pluginhost/manager.go` | A protocol-conformant `error` frame for one analyzer killed the whole plugin, so its remaining analyzers never ran | Directly contradicted ADR-0013, `docs/plugin-protocol.md`, and `protocol.go`'s own "never fatal to the run" comment. The TypeScript plugin serves six analyzers; one misconfigured ESLint would have cost the other five. Now only a *desynchronised* stream — timeout, unparseable line, dead process — drops a plugin, which is the real distinction. |
| `config/load.go` | An empty or comments-only `.review/config.yaml` failed with a bare `EOF` and aborted the review, while *no* file correctly yielded defaults | This is exactly what a hand-created placeholder leaves behind. yaml's `io.EOF` now means "empty document". |
| `diff/parse.go` | Hunk *body* lines were matched against the header cases, because the parser never tracked whether it was inside a hunk | An added line beginning `++ ` rewrote the file's path, and a removed `-- ` line — an ordinary SQL, Lua or Haskell comment — corrupted the status. Findings for that file were then silently dropped as "outside the diff". The nastiest of the seven: it fails quietly, on real-world content, in the component everything else depends on. |
| `cmd/reviewer/run.go` | `analyzers.only` / `analyzers.skip` from the config file were decoded and validated but never applied | A configured `skip: [knip]` did nothing. Decoding a setting and then ignoring it is worse than not supporting it. |
| `cmd/reviewer/run.go` | `host.Warnings()` was read before the deferred `host.Close()` | Every shutdown warning — "did not exit within 5s, killing its process group" — was generated after the report was written and was unreachable. `Close` is now called explicitly before reporting; the deferred call remains as an idempotent safety net. |
| `pluginhost/manager.go` | The parent ends of the three pipes were never closed for a dropped plugin | Two to three descriptors leaked per failed plugin. |
| `reporters/rdjson.go` | Warnings and skips were discarded | A run where every plugin failed to start serialised identically to a clean review. They now go to a side channel (stderr) rather than being smuggled into the reviewdog document as fake diagnostics. |

**Worth noting about the process:** five of the seven are failure-path bugs — what happens when a
plugin misbehaves, a file is empty, a diff contains awkward content. The happy paths were all
correct. That is the shape of defect this project's own design is meant to catch in other people's
code, which is a reasonable argument that the tool is worth building.
