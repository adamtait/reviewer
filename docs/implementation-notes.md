# Implementation notes — M1

Working log for this milestone. Deleted by the milestone's final commit; content moves to the PR
description (see the plan's assumptions).

## PR-10 — gitleaks analyzer

**Addition not in the plan: a local plugin transport (ADR-0027).** The plan lists gitleaks, Opengrep
and osv-scanner as "native Go analyzers" while ADR-0006 says every analyzer reaches the core as a
plugin. It never said how code inside the binary speaks a protocol designed for a child process.
Resolved with `Manager.AddLocal`: the handler runs in a goroutine joined to the host by `io.Pipe`s, so
the codec, the descriptor validation and the lane gate are all the same code as for an external
plugin — only the transport differs. Folded into PR-10 rather than given its own PR because the
analyzer cannot be reviewed without a way to reach the core. The cost is stated in ADR-0027: a
built-in analyzer is not isolated, which is why they stay thin wrappers around a spawned binary.

**An empty report file now means "did not scan", not "clean".** Caught by the test for a scanner that
exits 0 without writing a report. Real gitleaks always writes at least `[]` — verified — so an empty
file means the report was never written: a killed process, a full disk, or a future gitleaks that
renames `--report-path`. Treating that as clean would make the secrets gate pass every diff after a
silent flag rename, which is precisely the failure this analyzer exists to prevent. It is now
`ErrUnavailable`, which the gate will treat as though a secret were found.

**The finding never carries the credential.** `Secret` and `Match` are present in gitleaks' report and
deliberately unused; only the rule description is echoed. Posting the value to a pull request would
publish it to everyone with read access and to every notification email. There is a test asserting the
secret string appears in no field of the finding.

**Rule ids are namespaced as `secrets/<gitleaks rule>`.** Acceptance is measured per `ruleId`
(ADR-0024), so the id has to survive a gitleaks upgrade renaming its own rules.

## PR-11 — the secrets gate

**The gate is structural, not a condition.** The plan described it as setting a `laneBBlocked` flag
that skips lane-B analyzers. Implemented instead as two passes over the analyzer list with the gate
between them: the model lane's analyzers are separated before anything runs, so there is no code path
from a finding to a model call. A boolean checked inside one loop is one refactor away from being
checked in the wrong place; a missing call site cannot be moved.

**It matches the `secrets/` rule namespace, not the gitleaks analyzer id.** A second or replacement
secrets scanner then closes the gate without the gate being taught about it.

**The test is a spy plugin, not a mock.** A shell plugin declaring an `llm`-lane analyzer appends to a
file every time it receives an `analyze` frame. The assertion is on what actually crossed the process
boundary — zero invocations with the fixture's credential present, exactly one after it is removed and
committed. A flag-level assertion would have passed even if the gate were checked after the call.

## PR-12 — the sequencer

**It owns the policy, and `run.go` owns the plumbing.** Ordering, `--only`/`--skip`, the two-pass gate
split, timings and the fail-open rule moved out of the CLI into `internal/sequencer`, behind a
two-method `Host` interface. The policy is now testable without starting a process, which is why the
gate has a table test covering "clean", "credential found" and "scan failed" rather than only the
end-to-end spy.

**Skipping the secrets analyzer closes the gate rather than opening it.** `--skip gitleaks` means
nothing checked the diff, which is exactly the fail-closed case. This was not obvious from the plan and
has its own test, because the intuitive reading — skip the analyzer, skip its consequences — is the
unsafe one.

**Timings are reported by default, not behind `--verbose`.** The plan put per-analyzer timings in the
text output; keeping them unconditional is a small decision with a reason worth writing down: the
type-aware lint latency kill criterion is measured in minutes of wall clock, and a number nobody knows
to ask for is a number nobody measures.
