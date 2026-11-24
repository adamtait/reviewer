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

Packages distributed inside the published `@adamtait/reviewer-plugin-typescript`.
Reconciled against the full transitive production tree.

**This section is empty, and that is the design.** The plugin publishes compiled
JavaScript with no runtime dependencies: every analyzer engine — `typescript`,
`eslint`, `dependency-cruiser` — is resolved from the repository under review
(ADR-0014, ADR-0020), so the plugin analyses your code with your versions.

| Package | Version | License | Why |
|---|---|---|---|

## npm development dependencies

Direct development dependencies of the plugin. Not distributed, so their
transitive trees are not inventoried — see ADR-0028 for why that line is drawn
here and what it costs.

| Package | Version | License | Why |
|---|---|---|---|
| `typescript` | 5.9.3 | Apache-2.0 | Compiles the plugin, and is the analyzer engine the plugin falls back to when the repository under review has none. |
| `@types/node` | 22.20.2 | MIT | Node type definitions. |
| `eslint` | 9.39.5 | MIT | The engine for the typescript-eslint analyzer, and the fallback copy for tests. |
| `typescript-eslint` | 8.70.0 | MIT | Type-aware lint rules. |
| `dependency-cruiser` | 16.10.4 | MIT | Architectural boundary analysis. |
| `vitest` | 5.0.0 | MIT | The test runner the changed-tests analyzer drives, and the copy its own tests run against. Resolved from the repository under review at run time. |
| `knip` | 6.35.1 | ISC | Dead-code analysis, and the copy the plugin's own tests run against. Resolved from the repository under review at run time, like every other engine (ADR-0014). |

## Web application packages

Packages built into the review workspace's web application (`web/`), and so
served to everyone who uses it. Every package in `web/package-lock.json` that is
not development-only, read from the lockfile itself. `tools/checklicenses` also
refuses any of them whose own declared license is copyleft, whatever this table
says.

| Package | Version | License | Why |
|---|---|---|---|
| `react` | 19.3.1 | MIT | The web application's views. |
| `react-dom` | 19.3.1 | MIT | Draws them in the browser. |
| `scheduler` | 0.28.0 | MIT | Brought by `react-dom`. |
| `@codemirror/state` | 6.7.6 | MIT | The code and diff display: its document model. |
| `@codemirror/view` | 6.43.15 | MIT | The code and diff display: drawing, the line-number gutter, touch input. |
| `@codemirror/merge` | 6.12.3 | MIT | The diff: two versions of a file side by side, or in one column. |
| `@codemirror/language` | 6.13.1 | MIT | Syntax colouring. |
| `@codemirror/lang-javascript` | 6.2.5 | MIT | The grammar for TypeScript and JavaScript, the languages the navigation index covers. |
| `@codemirror/autocomplete` | 6.20.4 | MIT | Brought by `@codemirror/lang-javascript`. The editors are read-only, so it is never shown. |
| `@codemirror/lint` | 6.9.8 | MIT | Brought by `@codemirror/lang-javascript`. Not used. |
| `@codemirror/streamparser` | 6.0.0 | MIT | Brought by `@codemirror/language`. |
| `@lezer/common` | 1.5.6 | MIT | The parser the colouring runs on. |
| `@lezer/highlight` | 1.2.5 | MIT | Maps what the parser finds to colours. |
| `@lezer/javascript` | 1.5.6 | MIT | The TypeScript and JavaScript grammar itself. |
| `@lezer/lr` | 1.4.12 | MIT | The parser's run time. |
| `@marijn/find-cluster-break` | 1.0.4 | MIT | Brought by `@codemirror/state`: where a character ends. |
| `crelt` | 1.0.7 | MIT | Brought by `@codemirror/view`. |
| `style-mod` | 4.1.5 | MIT | Brought by `@codemirror/view`: the editor's styles, injected at run time. |
| `w3c-keyname` | 2.2.8 | MIT | Brought by `@codemirror/view`: key names. |
| `markdown-it` | 15.0.3 | MIT | Parses the Markdown of a description or an answer into tokens. The application draws the tokens itself and never uses the HTML this package can write. |
| `linkify-it` | 6.1.0 | MIT | Brought by `markdown-it`. Turning bare addresses into links is switched off. |
| `mdurl` | 2.1.0 | MIT | Brought by `markdown-it`: reading link addresses. |
| `uc.micro` | 3.0.0 | MIT | Brought by `markdown-it` and `linkify-it`: character classes. |
| `entities` | 8.1.0 | BSD-2-Clause | Brought by `markdown-it`: decoding character references. |
| `punycode.js` | 2.3.1 | MIT | Brought by `markdown-it`: internationalised host names. |
| `argparse` | 3.0.4 | PSF-2.0 | Brought by `markdown-it` for its command-line program, which the application does not use. It is in the lockfile as a non-development package, so it is listed. |

## Web application development dependencies

Direct development dependencies of the web application. They build and test it
and are not in what is served, so their transitive trees are not inventoried, as
for the plugin (ADR-0028). One of those trees holds `lightningcss`, which the
build tool uses and which is under MPL-2.0; it is not distributed.

| Package | Version | License | Why |
|---|---|---|---|
| `typescript` | 6.0.3 | Apache-2.0 | Type-checks the web application. |
| `vite` | 8.3.8 | MIT | Builds it. |
| `@vitejs/plugin-react` | 6.3.0 | MIT | The build tool's support for React. |
| `vitest` | 5.1.1 | MIT | Runs the unit tests. |
| `jsdom` | 31.0.1 | MIT | A document for the unit tests to run in. |
| `@testing-library/react` | 16.3.3 | MIT | Renders components in the unit tests. |
| `eslint` | 10.15.0 | MIT | Lint, including the rule that no string is ever set as HTML. |
| `typescript-eslint` | 8.74.0 | MIT | Type-aware lint rules. |
| `@types/react` | 19.3.0 | MIT | Type definitions. |
| `@types/react-dom` | 19.3.0 | MIT | Type definitions. |
| `@types/node` | 22.20.5 | MIT | Type definitions for the build and test configuration, which runs in Node. |
| `@playwright/test` | 1.65.1 | Apache-2.0 | Runs the journeys in a real browser. |

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
