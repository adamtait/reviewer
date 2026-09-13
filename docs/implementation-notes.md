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
