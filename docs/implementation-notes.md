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
