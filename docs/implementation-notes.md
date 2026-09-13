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
