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
