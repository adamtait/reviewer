<!-- SPDX-License-Identifier: MIT -->
# ADR-0035 — Attach an advisory risk record that only adds attention

- **Status:** Accepted
- **Date:** 2026-11-27
- **Implemented by:** PR-56
- **Supersedes:** —
- **Superseded by:** —

## Context

Human review time is spent evenly on uneven risk: a typo fix in `docs/` and a change to the auth
middleware wait in the same queue, and nothing but the diff tells a reviewer which needs a careful
read (`docs/designs/risk-gated-merge.md`). A signal that says where to look is useful now. A signal
that says where *not* to look is a different thing: it removes review, and the evidence that would
justify removing it (tests, the pull request's own configuration, the model lane) is produced or
written by the party being judged.

Three constraints shape any such signal. ADR-0009: nothing `reviewer` does may block a pull request,
so it exits 0 whatever it finds or fails at. ADR-0018: the review job holds `contents: read` and
`pull-requests: write`, and nothing in the tool can merge, approve, close or push. ADR-0016: GitHub's
own comments are the only store, so whatever the tool needs to remember between runs lives in a
comment it wrote.

Labels are the one place on a pull request that shows in GitHub's pull request list, which is where
a reviewer chooses what to read first. Writing them needs two verbs `ghclient.Writer` did not have.

## Decision

`reviewer risk` attaches a risk record to each non-draft pull request: one sticky issue comment,
marked `<!-- rv:risk -->` and updated in place, and GitHub labels from a closed set of nine
(`risk/auth`, `risk/data`, `risk/ci`, `risk/deps`, `risk/infra`, `risk/wide`, `risk/reach`,
`risk/history`, `risk/review-config`), fixed in `internal/risk` and guarded by a test. Every label
names a reason to look; none says a change is safe, and the comment says "No label means no rule
matched. It does not mean the change is safe." Everything that decides which labels apply (the risk
map in `.review/risk.yaml`, CODEOWNERS, the CI-built index) is read at the merge base through the
API, never from the pull request's head, so a pull request cannot change its own record; one that
changes `.review/`, `.github/` or a CODEOWNERS file is labelled `risk/review-config` for it. The
comment ends with a hidden list of the labels the tool set (`<!-- rv:risk-labels a,b,c -->`); a
later run removes only labels on that list, and trusts the list only in a comment authored by the
token's own login, so a label a person set is never removed. `ghclient.Writer` gains `AddLabels` and
`RemoveLabel`, and nothing else. `reviewer risk` runs as its own job, `risk`, beside the review job,
holding `contents: read`, `pull-requests: write` and `actions: read` (the last to download the index
artifact); it is advisory, decides nothing, and exits 0 after its first API call whatever fails,
with the failure stated in the comment. This supersedes nothing: ADR-0018's permission sentence is
about the review job, whose permissions do not change.

## Consequences

- **Positive:** reviewers get a reason to look, in the pull request list, for changes that touch
  auth, data, CI, dependencies, infrastructure, many owners, widely used symbols, recently reverted
  files, or the review configuration itself.
- **Positive:** the record only ever adds attention. It cannot be used to skip a review, and a pull
  request cannot argue its own labels away by editing the risk map in the same change.
- **Negative:** the `Writer` surface grows from three verbs to four. Labels are visible to everyone
  and can be set by anyone with triage access, so a label's presence proves nothing about who set
  it; the hidden list in the tool's own comment is what the tool trusts, not the label.
- **Negative:** the risk job holds `actions: read`, one more read permission, in a job whose
  definition the pull request's own workflow file controls.
- **Negative:** that adding a label which does not exist creates it (colour `ededed`), that a name
  with `/` is addressed by escaping it, and that removing a label not on the pull request answers
  404, were checked against github.com with a personal token. Whether a workflow `GITHUB_TOKEN`
  holding only `pull-requests: write` may do the same is unverified (Q3 in the design); if it may
  not, the comment is still posted and the label failures are stated in it.
- **Neutral:** "no label" carries no meaning beyond "no rule matched", and the comment has to say so
  every time, because a reader will otherwise take it as "safe".
- **Neutral:** fork pull requests get no record, because their token cannot post (ADR-0018).

## Alternatives rejected

- **A risk score with thresholds** — a number invites reading a low one as "safe", and nothing yet
  shows its recall on known-bad changes.
- **`reviewer risk` as a reporter inside the review run** — it would need `actions: read` in the
  review job, widening the permissions ADR-0018 states, and tie the record's latency to the slowest
  analyzer.
- **New keys in `.review/config.yaml` for the risk map** — the config decoder rejects unknown keys,
  so every older pinned binary would refuse the whole file and skip review.
- **Trusting the labels currently on the pull request as the tool's own** — a person may set a
  `risk/` label on purpose, and the tool would remove it on the next push.
- **Check runs as the store of the record** — they attach to a commit, not to the pull request, and
  need `checks: write`; a comment is already the store (ADR-0016).

## Revisit when

`reviewer outcomes` shows a label firing on more than half of pull requests for 4 weeks (it is
reviewed for deletion, as ADR-0024 does for rules), or the record starts to inform a merge decision,
at which point it is no longer advisory and the gate's own ADRs govern it.
