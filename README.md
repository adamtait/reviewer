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

To measure what happened to the pull requests that merged, without writing
anything to GitHub:

```
reviewer outcomes --since 90d --allowlist docs/                   # a summary in markdown
reviewer outcomes --allowlist docs/ --allowlist guides/ --format csv   # one row per pull request
reviewer outcomes --only-unreviewed                               # the merges nobody reviewed
```

It reports the median and 90th percentile hours from ready for review to the first
human approval; how many pull requests only touched the `--allowlist` directories
(a directory, never a file extension), counted strictly (only added or modified
files) and natively (every changed path, removals and renames included); whether
each was `reverted` (GitHub's revert title and body), `modified-later` (a pull
request merged within 14 days removed or changed a line that GitHub's blame, at
that pull request's merge base, credits to a commit this one introduced, whether it
was squashed, rebased or merged with a merge commit), `hotfixed` (modified later by
a pull request with a word such as fix, bug or revert in its title or branch, or a
phrase such as "fixes #N" naming this one in its description), `not-checked` (the
merge base, the blame or a commit's pull request could not be read, or the run
reached its bound of 200 commit lookups) or `clean`. A line changed again in between counts
against the change in between, and a later pull request branched before this one
merged never saw its lines and does not count. It reports whether the first human
review was followed by changes to the files it commented on
(`changed-after-review`, `unchanged-after-review`, `no-review`). It also lists the
merged pull requests with no `<!-- rv:risk -->` comment from this tool, and, per
`risk/` label, how often it fired and the median hours to first approval with and
without it. It reads the repository from config, or from `--repo OWNER/NAME`.

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

## Docs-only merges

A repository can let pull requests that change only documentation merge without
an approval, using GitHub's own settings: CODEOWNERS owns every path except a list
of documentation directories, and the ruleset requires code-owner review with 0
approvals. A wrong CODEOWNERS file then removes review with no visible sign, so
the setup makes this lint a required check:

```
reviewer owners check
```

It fails, and exits 1, unless the first rule owns `*`, CODEOWNERS owns itself,
every rule with no owner is a directory, and nothing a model, an agent or the build
reads is left unowned: agent instruction files, `.github/`, `.review/`, the
model lane's guidance and prompts, and files a `//go:embed` directive names. It is
a check, not a review, which is why it can fail (ADR-0036). Read
[docs/merge-classes.md](docs/merge-classes.md) before changing a ruleset: the
GitHub behaviour it depends on is unverified until its sandbox steps have run.

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

## The review workspace

A second program in this repository, `cmd/workspace`, is a small web service for
the human half of a review. You sign in with a read-only GitHub token, open a pull
request, and read it with the whole repository at that commit to hand: the
description and the diff, any file, text search, go-to-definition and references
for TypeScript from an index your own CI builds, and an assistant you can ask about
the code on screen. It reads everything from GitHub as you, stores nothing, and
never runs the code it shows. Comments stay on GitHub.

It has not yet been used by a team. To deploy one, see
[docs/workspace.md](docs/workspace.md).

## Documentation

- [How it fits together](docs/architecture.md)
- [Running the review workspace](docs/workspace.md)
- [Writing convention rules](docs/writing-rules.md)
- [Docs-only merges](docs/merge-classes.md)
- [Model access paths](docs/providers.md)
- [The plugin protocol](docs/plugin-protocol.md)
- [Architecture decisions](docs/adr/) — every significant decision, why, and what
  was rejected
- [Contributing](CONTRIBUTING.md)

## Licence

MIT. See [LICENSE](LICENSE) and [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md).
