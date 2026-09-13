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
