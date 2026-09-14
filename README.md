<!-- SPDX-License-Identifier: MIT -->
# reviewer

Automated code review that comments on a pull request the way a careful colleague
would: about the lines you changed, with a reason, and never about anything it
cannot point at.

It runs two lanes. The **deterministic lane** is the product — types, lint,
layering, secrets, dead code, vulnerable dependencies, type coverage, failing
tests, and your own conventions written as patterns. The **model lane** is the
addition, for the things a pattern matcher cannot see, and it is off until you turn
it on.

It never fails your build. Whatever it finds, and whatever fails while it looks, it
exits 0.

## Install

```
go install github.com/adamtait/reviewer/cmd/reviewer@latest
```

Or download a binary from [Releases](https://github.com/adamtait/reviewer/releases)
and verify it against the published checksums.

## Set it up in a repository

From the root of the repository you want reviewed:

```
reviewer init --dry-run     # see exactly what it would write, and why
reviewer init               # write it
```

`init` inspects the repository once and writes plain, tracked files: a
`.review/config.yaml` describing what runs, a GitHub Actions workflow, and an
agent skill. Every value it writes was detected, and a wrong one is a one-line fix
rather than a bug report against a heuristic.

It prints what it could not work out, and what each gap costs — "no ESLint flat
config: the eslint analyzer declines rather than inventing rules" — because the
failure mode of an installer is an install that looks successful and reviews less
than you expect.

Then do the two things it tells you to: install the plugin devDependency it names,
and fill in the release version and checksum in the generated workflow.

## Use it

```
reviewer --staged              # what you are about to commit
reviewer --base main           # a branch, against its merge base
reviewer --pr 42               # a pull request, posting comments
```

Nothing is ever reported outside the lines you changed. A pre-existing problem two
lines away is real and is not this change's to fix, and reporting it is the fastest
way to teach a team to ignore the tool.

## What you get

```
src/domain/order.ts
       4  error    types/TS2322   Type 'number' is not assignable to type 'string'.
      12  warning  dead/unused-export  `formatTotal` is exported here and nothing
                   imports it. (medium confidence)
```

Two things decide how a finding is presented, and they answer different questions:

- **Confidence** decides how loudly. Only high-confidence findings become inline
  comments; everything else goes into one collapsed summary, which is the right
  place for anything arguable.
- **Lane** decides what kind of claim it is. A finding marked `model` came from a
  model reading your diff — frequently right, and a judgement. Everything else came
  from a compiler or a pattern match.

## The analyzers

| Analyzer | Needs | Reports |
|---|---|---|
| `gitleaks` | the `gitleaks` binary | credentials in the diff — and stops the model lane when it finds one |
| `opengrep` | the `opengrep` binary and your rules | your own conventions, written as patterns |
| `osv` | the `osv-scanner` binary | vulnerabilities **this change introduces**, by scanning the lockfile twice |
| `tsc` | a tsconfig | type errors, attributed to the lines you changed |
| `eslint` | a flat config | your lint rules, using your config |
| `dependency-cruiser` | its config | layering violations, on the import that creates them |
| `knip` | a knip config | exports and dependencies nothing uses |
| `type-coverage` | a tsconfig | a drop in typed-ness against a recorded baseline |
| `changed-tests` | vitest or jest | your own tests, for the files you changed |
| `model-review` | a configured provider | the rest — see [docs/providers.md](docs/providers.md) |

Every one of them declines with a reason rather than guessing. Nothing is bundled:
the TypeScript analyzers use *your* typescript, *your* eslint, *your* rules, so a
finding is one you can reproduce.

## Your own conventions

The review comments a team repeats are usually patterns, and a pattern is free to
check. Write them as [Opengrep rules](docs/writing-rules.md) in `.review/rules/`,
with a test case that matches and one that does not, and `reviewer rules test`
checks both.

## The model lane

Off by default. Six access paths — four APIs and two that drive a CLI you are
already signed in to — chosen at install. See [docs/providers.md](docs/providers.md).

Three things about it are worth knowing before you turn it on:

- **It cannot run when a credential is in the diff.** That is structural, not a
  check: the secrets scan runs first and the model lane is on the far side of it.
- **Every finding is disproved before it is emitted.** A second pass tries to show
  each one is wrong, and only findings that survive a *specific* refutation are
  posted.
- **Taste categories cannot claim high confidence.** That ceiling is in code, not
  in the prompt.

## Writing a plugin

Every analyzer reaches the core the same way, first-party ones included: a
subprocess speaking newline-delimited JSON over stdio. There is no SDK requirement
and no build step — the example plugin is forty lines of POSIX shell.

See [docs/plugin-protocol.md](docs/plugin-protocol.md).

## Documentation

- [How it fits together](docs/architecture.md)
- [Writing convention rules](docs/writing-rules.md)
- [Model access paths](docs/providers.md)
- [The plugin protocol](docs/plugin-protocol.md)
- [Architecture decisions](docs/adr/) — every significant decision, why, and what
  was rejected
- [Contributing](CONTRIBUTING.md)

## Licence

MIT. See [LICENSE](LICENSE) and [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md).
