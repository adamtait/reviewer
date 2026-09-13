# Implementation notes — M4

Working log. Deleted by this milestone's final commit; content moves to the PR description.

## PR-27 — the Opengrep analyzer

**Opengrep is not installable in this environment**, so the JSON shape and flag names are Semgrep's,
which Opengrep forked, and are unverified against the real binary. Where that risk lands is worth
being precise about, because the same class of assumption broke `gitleaks dir` in M1:

- *A wrong flag* makes the command fail, which surfaces as a warning carrying the tool's own
  complaint. Diagnosable, and the reason stderr's first line is kept in the error.
- *A different JSON shape* makes the report unparseable, which is reported as unavailable rather
  than as clean.
- *A silently-ignored target list* — the exact `gitleaks dir` failure — would make a rule report on
  files the pull request never touched. This is the one that would be quiet, so the analyzer filters
  results back to the changed set itself rather than relying on the core's diff filter to do it.

**Neither the exit code nor stderr decides anything.** This class of tool exits non-zero when it
finds something, so only a parseable report distinguishes a finding from a failure. Learned in M1
against a real binary; applied here on principle.

**A repository with no rules is told to write one, not told to install Opengrep.** The probe checks
for rule files before it checks for the binary. Sending someone to install a tool they do not yet
need is a worse first experience than telling them the truth.

**An unrecognised severity becomes a warning, not an error.** A rule set this tool does not fully
understand should not be able to raise the loudest thing it can say.

## PR-28 — the rule pack and `rules test`

**Duplicate ids are checked here rather than left to Opengrep**, which refuses the whole run on one:
a single collision would hide every other rule's result, and the message does not name the two files
that collided. Here the first declaration keeps working and both files are named.

**Both halves of a test are required**, which is this project's rule and not Opengrep's. A rule with
only a `ruleid:` case is one nobody has checked for false positives, and a single false positive is
enough for a team to start ignoring an entire pack.

**`rules test` fails when Opengrep is absent.** Reporting success while never executing a pattern
would make the command worse than not having it. That means CI here cannot run it against
`examples/rules`, so a Go test asserts the half that is checkable — the pack loads, no duplicate ids,
every rule fully cased, every message long enough to carry a reason.

**A third exit status.** A review exits 0 whatever it finds (ADR-0009) and misuse exits 2. `rules
test` is neither: it is a check with a right answer, meant to run in a script, so a pack that is not
ready exits 1.

**Deviation: seven cases, not six.** The plan's proof says "3 rules, 6 cases". The examples ship
seven because `no-console-in-lib` has two distinct non-matching cases worth showing — a logger and a
throw. The command reports what is there.

## PR-30 — the osv-scanner analyzer

**Taken out of plan order** (before PR-29, PR-31 and PR-32, which are all in the TypeScript plugin).
The plan states the four are additive and their relative order is free; grouping it with the two Go
analyzers beside it kept one area of the code open at a time. It also happened to be the only one of
the four whose real binary is installable here, which is how the next note exists.

**The defect the real binary revealed.** `osv-scanner` prints `{"results": []}` and writes the reason
only to stderr when the vulnerability database is unreachable. Stdout alone therefore says "no
vulnerabilities" for a scan that never happened. Verified against osv-scanner 2.5.1 with
`api.osv.dev` blocked by this environment's egress policy: stdout was an empty report and the exit
status was **127**, which is the only signal that distinguishes the two. Without that check every run
behind a restricted network would report every dependency clean — silently, and in the one analyzer
where "clean" is a security claim.

A test asserts it against the real binary, and passes here precisely *because* the network is
blocked: the analyzer reports unavailable rather than clean.

**"Introduced by this pull request" is computed by scanning twice.** The lockfile as it is, and as it
was — the previous contents read with `git show <base>:<path>`. The alternative, inferring
introduction from the lockfile's own diff ranges, is format-specific and wrong for a version bump,
where only the version line is added and the package name is not.

**This needed one additive protocol field.** `AnalyzeRequest.Base` carries the ref the diff was taken
against, empty for a staged review (where the previous contents are HEAD's). Old plugins ignore it;
most analyzers never look at it, because working from the changed lines alone is enough for them.

**The previous contents are written under the original base name.** osv-scanner picks its parser from
the file name, so handing it `tmp1234` produces "could not determine extractor suitable to this file".

**A clean head scan skips the base scan**, because that would be a second network round trip for a
question nobody asked. And a pull request that changes no manifest never spawns the scanner at all.

**When the base scan fails but the head scan worked, nothing is reported** and the run says so.
"Introduced by this pull request" cannot be established, and reporting everything found would be
exactly the noise this analyzer exists to avoid.

**The finding lands at line 1 of the lockfile.** The fix is a version bump in the manifest, not an
edit inside the lockfile, and pointing at a lockfile's internals invites someone to hand-edit one.

## PR-29 — the Knip analyzer

**npm is reachable in this environment even though the binary releases are not**, so Knip, Vitest and
osv-scanner were all verified against the real thing. Opengrep is the one analyzer in this milestone
that could not be.

**Knip is spawned, not imported.** Its programmatic API is explicitly internal; the JSON reporter is
the documented surface. A spawned process also cannot take the plugin down with it.

**`require.resolve("knip/package.json")` fails on a perfectly present installation** with
ERR_PACKAGE_PATH_NOT_EXPORTED, because a package's `exports` map decides what may be resolved by
subpath and Knip's does not list its own manifest. The node_modules chain is walked instead, which is
also the more honest question: is Knip installed *in this repository*.

**It declines without a Knip config**, the way the ESLint analyzer declines without a flat config. A
wrong guess at entry points makes every export in the codebase look unused, which is a worse first
impression than no findings at all.

**Unused exports are medium confidence, undeclared dependencies are high.** Knip cannot see a dynamic
import, a re-export consumed from a barrel, or a symbol a published package exposes on purpose. An
undeclared dependency is not a judgement: the import is there and the declaration is not.

**An unused dependency is reported on the line that declares it.** Knip reports these against
package.json with no line, and a finding at line 1 would be dropped by the core's diff filter on
every real pull request — leaving an analyzer that appeared to work and reported nothing.

## PR-31 — the type-coverage ratchet

**Measured over the shared program**, not by spawning `type-coverage`, which would type-check the
repository a second time — the single most expensive thing this tool does (ADR-0014).

**The defect found by running the binary.** The regression was first reported against
`.review/type-coverage-baseline.json`, which reads naturally and is invisible: no real pull request
touches that file, so the core's diff filter drops the finding on every run. The analyzer reported
"1 finding" and the report said "no findings". It now lands on the changed file carrying the most
`any` identifiers on changed lines, which is also the most useful place for it.

This is the third instance of the same class in this project — an analyzer whose output is correct and
unreachable. It is worth naming as a rule: **a finding's location is part of its correctness, and the
only way to check it is to run the thing.**

**`error` is excluded from the `any` count.** The compiler's `any` flag covers both a real `any` and
the `error` type an unresolved import produces; counting the second would move the ratchet whenever a
dependency failed to install.

**A tenth of a percentage point of tolerance.** Adding a well-typed file changes the denominator, so
an entirely innocent change moves the fourth decimal place. Zero tolerance would make the ratchet a
random-noise generator.

**No baseline means record, not report.** Inventing one from the current run would silently bless
whatever state the codebase is in today. An unreadable baseline is treated as absent rather than as
zero, which would report a catastrophic regression on every run.

**`baseline --write` reaches the analyzer through its settings block.** The measurement belongs to the
component that can perform it — the core has no TypeScript compiler and no business having one
(ADR-0002) — and the protocol already passes an analyzer's settings through untouched. A dedicated
frame would be a protocol change for one command.

## PR-32 — the changed-tests analyzer

**Neither runner's selection logic is reimplemented.** `vitest --changed` and `jest --changedSince`
already know what a change affects, and a second opinion about it would be a second thing to keep
right.

**Designed around the location trap rather than discovering it again.** A test that breaks because of
a change to the code it covers lives in a file the pull request never opened. The assertion's own
location is the right answer and the wrong place, so the comment lands on a line the change touched
and the assertion's real location is carried in the message — where it is just as actionable.

**The assertion's line comes from the stack trace, not from the runner's `location` field**, which
points at the `test(...)` declaration. The first stack frame inside the repository is the assertion;
everything after it is the runner's own machinery.

**A file that failed to load is reported too.** It is the most common real failure — a syntax error,
or an import of something the change removed — and it has no assertions to report against.

**Exit code cannot decide.** A failing test and a runner that would not start are both non-zero, so
only the presence of a report distinguishes them.

**Nothing changed means the runner is never spawned.** Both runners interpret an empty selection as
"run everything", which is minutes of a reviewer's time for a question nobody asked.

**The test fixture has to be committed clean and then edited.** Both runners select by comparing
against git, so a fixture where everything is committed has nothing changed and selects no tests —
which is how the first version of these tests passed while measuring nothing.
