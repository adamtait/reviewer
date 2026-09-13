# Implementation notes — M6

Working log. Deleted by this milestone's final commit; content moves to the PR description.

## PR-41 — the agent skill

**The skill's value is what it says about *when*, not how.** A skill that documents a command is a
manual page, and an agent with a manual page runs it at the wrong times or not at all. So the
frontmatter describes the occasion — before pushing, before opening a pull request, after changing
something in response to review comments — and the body says when *not* to: not on every file save,
because it spawns compilers and the thing it protects is somebody else's patience.

**Facts versus suggestions is the distinction that decides behaviour.** High confidence came from a
compiler or a pattern match, so the code has the problem. Everything else came from a model reading the
diff, and a suggestion you disagree with is not a finding you have to argue against. Confidence is the
marker, which is the same rule ADR-0017 uses for presentation — an agent and a human are told the same
thing.

**Three failure modes are named explicitly** because they are the ones an agent under time pressure
reaches for: fixing a finding by suppressing it, acting on a finding about code the change did not
touch, and reading an empty result as approval rather than as absence of evidence.

**The wrapper adds no flags of its own**, asserted by a test that greps it for anything but `--root`.
An agent reading SKILL.md must not learn a vocabulary that works only in one repository.

**The default branch is detected, not assumed.** `origin/HEAD` first, then `init.defaultBranch`, then
whatever is checked out. Handing a repository whose trunk is `master` a command that says `main`
produces a command that fails on first use.

**`review.sh` is written 0755.** The writer used 0644 for everything, so the skill's one command would
have failed on its first use with an error about permissions rather than about the review.

**A silent no-op in my own tooling.** Two fields — the default branch and the model-lane note — were
added to the template data struct but never assigned, because the edit that was supposed to add them to
the struct literal did not match after gofmt realigned it. The template rendered `--base ` with nothing
after it and dropped the lane B section, and the build was clean. Found by reading the generated file,
not by any test; every scripted edit now asserts it changed something.

## PR-42 — the metrics command

**The rule id now travels in the comment marker**, not only in the comment's visible text. The visible
text is markdown a human can edit, and parsing a `<sub>` tag out of it would be a scrape rather than a
measurement. `<!-- rv:a3f9c2 conventions/no-console -->`. The id is optional in the pattern, so a
comment written before it was added still parses and still dedupes — it is counted under a visible
`(rule not recorded)` row rather than dropped, which keeps the totals adding up.

**Acceptance is resolved ÷ (resolved + open), with outdated excluded.** That is the definition the
reviewer's question asks for, and the exclusion is the load-bearing part: an outdated thread means
GitHub detached the comment because the code moved, so nobody decided anything about it. Counting it as
a rejection would punish rules that fire on fast-moving branches.

**`n/a`, never 0%, for a rule with nothing decided.** Reporting zero is how a rule that has not had the
chance yet gets deleted for being new — and the whole point of the table is to delete the right ones.
A rule with nothing decided also sorts *last*, because putting it at the top of a worst-first table
reads as an indictment.

**CSV is unrounded and markdown is not.** The table is for reading; the CSV is for a spreadsheet, where
a pre-rounded number cannot be re-aggregated. A rule with nothing decided gets an empty field rather
than a number, so nothing averages "n/a" into a zero.

**Only threads this tool started are counted**, read from the first comment. A human quoting a marker
in a reply does not make their conversation ours to measure.

**The honest limitation, recorded in the ADR rather than in a comment nobody reads:** "resolved" means
somebody clicked resolve, not that the finding was right. A team that resolves threads to clear the
sidebar produces flattering numbers. What survives that is the *ordering* — the habit is uniform across
rules — which is what the table is sorted by.
