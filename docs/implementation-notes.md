# Implementation notes — M2

Working log. Deleted by this milestone's final commit; content moves to the PR description.

## PR-16 — fingerprinting

**Whitespace is removed, not collapsed.** My first implementation collapsed runs of whitespace to a
single space, and the reformatting test caught that this is not enough: a formatter adds space where
there was none, so `call(x,y)` and `call(x, y)` still hashed differently. Removing horizontal whitespace
entirely is the only normalisation that makes those equal. The normalised snippet is unreadable, which
does not matter — it is only ever hashed.

**The message is excluded from identity.** Not obvious, and worth stating: including it would orphan
every comment a rule has ever posted the first time someone improves its wording.

**Fields are null-separated in the digest,** so a rule id ending in a separator cannot collide with a
different rule-and-path pair that concatenates to the same bytes. There is a test for it.

## PR-17 — the GitHub client

**Deviation: hand-rolled over `net/http`, not `go-github`.** The plan specified `go-github`. The surface
this tool needs is six read calls and three writes, the responses are small, and a narrow hand-written
client makes the token scopes readable in one file — which matters when what you are asking a team for is
write access to their pull requests. It also keeps the dependency inventory at two Go modules, a property
I have been claiming as a reason to prefer Go and should therefore not spend casually. About 250 lines
against a dependency with its own transitive tree.

**Pagination is not optional, and it has its own test.** A pull request with more than one page of
existing comments would have its later ones invisible to dedupe, and the tool would repost them — the
exact failure fingerprinting exists to prevent. The default page size is set to 100 to keep request
counts down.

**Query strings are redacted from error messages.** GitHub does not accept tokens in the query today, but
a URL in an error message is precisely what ends up in a CI log, and the cost of the guard is four lines.

**Reads and writes are separate interfaces.** The read interface is `Client`; writes arrive in PR-18 as
their own type. A reader of this package can see that reviewing a pull request needs no write access at
all, and there is a test that fails if someone widens the read interface.

## PR-18 — the GitHub reporter

**The comment reads like a review comment, and there is a test enforcing that.** No emoji, no "Automated
review" banner, no heading. It competes for attention with human comments on the same pull request and
should not announce itself as different; the rule id in small text at the bottom is what someone searches
for when they want to argue with the rule. `TestBodyShape` fails on `🤖`, "Automated" and "AI-generated".

**Dedupe reads the summary comment as well as the inline ones.** A finding can move between the collapsed
summary and an inline comment — that is the whole point of promoting a category once its acceptance rate
justifies it — and reading only inline comments would post it again as if new. The summary carries a
marker per finding it lists, and `ParseAllMarkers` collects them.

**Dedupe also applies within a single run.** Two findings that normalise to one identity post once. Not
hypothetical: a rule firing twice on a duplicated block is exactly what the snippet-based identity in
ADR-0015 collapses.

**`Side: "RIGHT"` is set explicitly by the reporter rather than defaulted by the client.** Commenting on
the LEFT side attaches to code the pull request deleted, which is a silent wrong answer, and the call
site is where that choice should be visible. The client keeps the default as a safety net.

**A failed comment does not lose the others.** GitHub rejects a comment whose line it does not consider
part of the diff, which happens when the diff we computed and the diff GitHub computed disagree at an
edge. Each failure is a warning; the rest still post.

## PR-19 — the collapsed summary comment

**The gate's reason goes above the fold, outside the `<details>`.** Someone who has just committed a
credential needs to know the diff was not sent anywhere, and must not have to expand anything to find
out. This is the one thing in the summary that is not collapsible.

**An unchanged summary is not rewritten.** Rewriting an identical body bumps the comment's timestamp and
re-notifies every subscriber for nothing — the sort of thing that makes a bot feel noisy even when it is
saying the same thing.

**An empty summary clears the comment rather than deleting it.** A comment that vanishes leaves a reader
wondering whether the tool ran at all.

**Messages are flattened to one line.** A multi-line analyzer message would otherwise break out of its
markdown list item and mangle the block; there is a test for it.

## PR-20 — stale thread resolution

**Three conditions, each guarding a different mistake.** A thread is resolved only if it carries one of
our fingerprint markers, *and* its first comment was written by the token's own identity, *and* it is not
already resolved. The second condition is the non-obvious one: a human who quotes the bot's comment
inherits its marker, and resolving their thread would be the tool silently closing someone else's
conversation. Each condition has its own test case.

**It fails loudly if it cannot identify itself.** Without knowing which account the token belongs to,
there is no way to tell our threads from a human's, and resolving the wrong one is not something the tool
can undo. That path returns an error rather than guessing.

**A test-data bug worth recording:** my first version of these tests used `gone111` as a fingerprint, and
they failed. Fingerprints are hex, and `g`, `o` and `n` are not — the marker regex correctly refused it.
The code was right and the fixture was wrong, which is the better way round.

## PR-21 — the composite action and self-review

**The action requires a checksum, not just a version.** A pinned version without one trusts whoever can
move a tag; an input with `required: true` makes the omission a configuration error rather than a silent
weakening. Both inputs are documented with *why* they are required, in the action's own metadata, because
that is what someone reads before adding it to their workflow.

**It checks `fetch-depth: 0` rather than assuming it.** Without full history there is no merge base, and
the diff would silently cover the wrong commits — a wrong answer rather than an error. The action fails
with the fix in the message.

**No `continue-on-error`.** `reviewer` exits 0 whatever it finds (ADR-0009), so if the step fails the tool
itself is broken and that should be visible. Adding `continue-on-error` would hide exactly the failures
worth knowing about.

**Self-review builds the binary from the pull request rather than downloading a release.** A regression
should be caught on the pull request that introduces it, not on the one after the release.

**This repository now has its own `.review/config.yaml`,** which is the one legitimate instance of that
file living here: this repository is also a repository under review. It names `api.github.com`, which the
seam test permits because the test walks Go source — configuration is precisely the channel ADR-0004
requires such a value to arrive through, and a comment in the file says so.
