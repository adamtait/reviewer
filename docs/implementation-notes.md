# Implementation notes — M3

Working log. Deleted by this milestone's final commit; content moves to the PR description.

## PR-23 — installer detection and the install plan

**Workspace globs are expanded during detection, not left for PR-26.** The plan's proof line asks for
"2 workspaces" from a fixture that declares `packages/*`, so `Detected` carries both the declared
patterns (`WorkspaceGlobs`) and the directories they resolve to (`Workspaces`). A directory counts only
if it holds a `package.json`: `packages/*` also matches a scratch directory, and the fixture has one
specifically so a test asserts it is skipped. PR-26 now only has to write `projects:` and narrow the
diff, which is what its reviewer's question is actually about.

**`**` is narrowed to `*`.** Go's `filepath.Glob` has no recursive wildcard. One level covers what npm
and pnpm layouts actually produce; a deeper nesting is reported as zero workspaces and the plan says
"none matched packages/**" rather than guessing. Negated pnpm patterns (`!libs/deprecated`) are honoured.

**`pnpm-workspace.yaml` is still parsed by hand**, and the parser now has to end the package list at the
next top-level key — `catalog:` is common, and without that every later key read as a package glob.

**`--force` changes the plan, not the writer.** `BuildPlan` takes it and produces `overwrite` actions,
so `--dry-run --force` can show what `--force` would replace. The alternative — a writer that decides
at write time — makes the dry run structurally unable to answer the only question worth asking before
a destructive install.

**The plan prints its basis, not only its conclusion.** `npm (from package-lock.json)`, not `npm`. And
every absence is printed with what it costs ("no ESLint flat config: the eslint analyzer declines
rather than inventing rules"), because the failure mode of an installer is an install that looks
successful and reviews less than the reader expects. When there is no `package.json` at all the other
six Node-shaped notes are suppressed: they all follow from that one, and printing seven buries it.

**The plugin devDependency is pinned exactly, not as a range.** Binary and plugin are one release
(ADR-0026) and a version mismatch is a protocol mismatch, so `^` would let a destination drift onto a
plugin this binary cannot talk to. A binary whose version is not a release (`dev`) plans `latest` and
says so in the notes rather than refusing.

**Deviation from the plan's file counts.** PR-23's proof says 5 files; PR-24's says 4 created. The
fifth is `.env.example`, which only has content once a provider is chosen, so PR-24 writes four and
PR-25 adds the fifth. The plan lists all five from PR-23 because the plan describes the finished
install, not the current commit. Fifth file aside, the set is `.review/config.yaml`,
`.review/rules/.gitkeep`, `.review/.gitignore` (the poller's watermark is a local artefact, and PR-22
writes it into `.review/`), and `.github/workflows/review.yml`.

**Bug found by running the binary, not by a test.** `parseRemote` split the whole URL on `/` and took
the last two segments, so `https://github.com/owner/` returned `github.com/owner`. The host has to come
off before the path is split. The table test that caught it was written before the fix, and the other
five URL forms had been passing for the wrong reason.

**`init` without `--dry-run` writes nothing yet** and says so on stderr, exiting 0. It is not a usage
error — the command is right, the writers land in PR-24 — and ADR-0009 reserves non-zero for misuse.

**`testfixture.Destination` is new**, alongside `Build`. `Build` materialises a two-commit diff from
`base/` and `head/`; the installer needs a flat repository with one commit, because its exit criterion
is that `git status --porcelain` is empty after a dry run and an uncommitted tree reports every file as
untracked. The git-invocation helper is now exported as `testfixture.Git` so a test can add a remote.

**Detection runs git, so it can see out of the fixture.** Running `init --dry-run` inside
`testdata/tiny-monorepo` reports `github repo adamtait/reviewer`: the fixture is inside this repository,
and `git remote get-url origin` answers for the enclosing repository. That is correct behaviour for a
subdirectory of a real repository, but it means the tests copy the fixture out to a temp directory
rather than reading it in place, or they would assert against whatever remote the machine has.
