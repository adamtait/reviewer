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
