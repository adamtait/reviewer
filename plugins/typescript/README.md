<!-- SPDX-License-Identifier: MIT -->
# @adamtait/reviewer-plugin-typescript

TypeScript analyzers for [reviewer](https://github.com/adamtait/reviewer), served
over the plugin protocol.

Install it in the repository you want reviewed, not globally:

```sh
npm install --save-dev @adamtait/reviewer-plugin-typescript
```

That is deliberate. The plugin analyses your code with *your* `typescript` and
`eslint` versions, resolved from your `node_modules`, so its findings match what
your own build and editor report. A globally installed copy would analyse your
code with whatever versions it happened to ship with. `reviewer init` adds the
dependency for you.

## Why one plugin for six analyzers

`tsc`, typescript-eslint and type-coverage all build a TypeScript program over the
same `tsconfig`. Building it once and sharing it is the difference between one
compiler pass per run and three, and it is the reason the reviewer core can be
written in Go at all — see ADR-0002 and ADR-0014 in the main repository.

The process therefore stays alive for a whole run and holds that program between
analyzer invocations.

## Channel discipline

Stdout carries protocol frames and nothing else; diagnostics go to stderr. The
plugin reassigns `console.log` to stderr on startup rather than trusting every
library it calls not to print — ESLint and dependency-cruiser both write to stdout
under some configurations, which is not hypothetical.

## Development

```sh
npm ci
npm run build
npm test
```

The Go repository's `make check` builds this package, because a cross-language
conformance test drives the real plugin from the real host. A skipped conformance
test would hide a drift between the two hand-written sides of the protocol, which
is the one failure neither side's own tests can catch.
