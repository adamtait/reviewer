<!-- SPDX-License-Identifier: MIT -->
# Contributing

## The development loop

```
make tools     # the linters CI pins, once
make check     # everything CI runs
```

`make check` is the whole gate: build, the plugin build, `go vet`, `gofmt`, tests,
`staticcheck`, licence headers, the ADR check, and the third-party inventory. If it
passes locally it passes in CI, because CI runs the same target.

The TypeScript plugin has its own:

```
npm --prefix plugins/typescript ci
npm --prefix plugins/typescript test
```

Optional, and worth doing once:

```
make hooks     # a pre-commit secret scan
make secrets   # scan the whole history, as CI does
```

## What a change needs

**Green at every commit.** Not green at the end of the branch — at every commit, so
any of them can be reverted alone.

**A test that would have failed before.** Several defects in this project's history
were found by a test written at the same time as the code. Several more were not,
and needed the binary run the way a user runs it. If your change is about what a
spawned tool does, or where a finding lands, run it rather than trusting a unit
test: three analyzers here shipped findings that were correct and invisible.

**A comment that says why, not what.** The code says what. The interesting thing is
the alternative you rejected and the failure you are preventing — especially when
it is not obvious, which is most of the time.

## Architecture decisions

Significant decisions are recorded in [docs/adr/](docs/adr/) and the log is
**append-only**: an `Accepted` ADR is never edited, only superseded by a new,
higher-numbered one. `tools/checkadrs` enforces that, along with numbering, index
sync, and two-way supersession links.

An ADR lands in the same change as the code that first implements its decision. If
you find yourself explaining a decision in a commit message at length, it wants an
ADR instead.

## Writing a rule

Convention rules live in the repository being reviewed, never here. See
[docs/writing-rules.md](docs/writing-rules.md).

The examples in `examples/rules/` are deliberately generic. A rule encoding one
codebase's architecture belongs in that codebase.

## Writing an analyzer

Two ways, and the first is usually right:

**A plugin**, in any language, speaking the protocol over stdio. Nothing needs to
change here. See [docs/plugin-protocol.md](docs/plugin-protocol.md).

**A built-in**, in `internal/analyzers/`, for something that wraps an external
binary. It is still reached over the protocol — the built-ins are a plugin like any
other (ADR-0006) — and gets the same descriptor validation, lane gating and fault
containment as a third-party one.

Whichever you write, three rules apply and each has cost this project a defect:

1. **A finding must land on a changed line.** Findings outside the diff are dropped
   by the core, silently from the analyzer's point of view. Check where yours lands
   by running it, not by reading it.
2. **Never report "clean" for a scan that did not happen.** Exit codes lie: tools in
   this class exit non-zero when they find something, and at least one prints an
   empty report and writes the reason only to stderr. Only a parseable report means
   a scan happened.
3. **Decline with a reason.** An analyzer that cannot run says why, once, at the top
   of the run. "Not installed" and "no config" are different problems with different
   fixes.

## Dependencies

This project has one non-test Go dependency. Adding another needs a reason in the
change's description, an entry in `THIRD_PARTY_LICENSES.md`, and a licence
`tools/checklicenses` accepts — no copyleft, enforced rather than observed.

An external binary spawned as a process is not a dependency in that sense, which is
how Opengrep (LGPL-2.1) is usable at all. That boundary is asserted by a test.

## Contribution terms

By contributing you agree that your contribution is licensed under the MIT licence,
the same as the rest of the project. Every source file carries an SPDX header;
`make check` fails without one.
