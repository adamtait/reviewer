<!-- SPDX-License-Identifier: MIT -->
# Third-party licenses

Everything this project depends on, and under what terms. `tools/checklicenses`
reconciles this file against the real dependency graph on every CI run, so it
cannot drift: an undocumented dependency and a documented one that no longer
exists both fail the build.

This project is MIT (ADR-0001). No dependency here may be copyleft — that is
enforced, not merely observed, by the denylist in `tools/checklicenses`.

## Go modules

| Module | Version | License | Why |
|---|---|---|---|
| `gopkg.in/yaml.v3` | v3.0.1 | MIT and Apache-2.0 | Parsing `.review/config.yaml`. The Go standard library has no YAML decoder, and the configuration format is YAML because the Opengrep rule files beside it are. |
| `gopkg.in/check.v1` | (indirect) | BSD-2-Clause | Test dependency of `yaml.v3`. Not linked into any binary this project ships. |

## npm packages

The TypeScript plugin's dependency tree, added in PR-13. Until then this section
is intentionally empty rather than absent, so its absence is never mistaken for
"not checked".

| Package | Version | License | Why |
|---|---|---|---|

## External binaries

These are **spawned as separate processes**, never linked, vendored, or imported.
They are not dependencies of this project in any distribution sense: the project
ships without them and degrades to a warning when one is absent.

| Binary | License | Boundary |
|---|---|---|
| `opengrep` | LGPL-2.1 | Process boundary only (ADR-0011). Invoked as `opengrep scan --json`. No Go module, no npm package, no cgo. `tools/checklicenses` fails the build if `opengrep` ever appears in `go.mod` or in the plugin's `package.json`. |
| `gitleaks` | MIT | Process boundary. Invoked as `gitleaks detect`/`protect`. |
| `osv-scanner` | Apache-2.0 | Process boundary. Invoked as `osv-scanner --format json`. |

## Development tools

Installed by `make tools`, pinned to the same versions CI installs. They run
against the source and are not part of any released artifact.

| Tool | License |
|---|---|
| `honnef.co/go/tools` (staticcheck) | MIT |
| `github.com/google/addlicense` | Apache-2.0 |
