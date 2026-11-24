<!-- SPDX-License-Identifier: MIT -->
# ADR-0032 — Sign users in with their own GitHub token, sealed in the session cookie

- **Status:** Proposed
- **Date:** 2026-11-24
- **Implemented by:** PR-49
- **Supersedes:** —
- **Superseded by:** —

## Context

The review workspace (`docs/designs/review-workspace.md`) shows a user the code of private
repositories. It has to know who the user is and which repositories they may see, and its rule is
that it mirrors GitHub: a user sees a repository in the workspace if and only if GitHub lets them
read it.

There are three usual ways to learn that. An OAuth App cannot ask for narrow permissions: the only
scope that reads private repositories also grants write access to every repository the user can
reach. A GitHub App needs someone to register and install it, which the team expects not to be
allowed. A service account with one token that reads everything needs a hand-kept table mapping
users to what they may see, and a wrong row grants the wrong access.

The service also keeps no database (ADR-0029), so it has nowhere to keep a session.

This record is `Proposed` while the workspace's first version is on trial.

## Decision

Identity sits behind one interface with three operations: sign a user in, say whether they can read
a repository, and return a GitHub client that acts as them. Everything else in the service is handed
a client and a login and does not know how the user signed in.

The first method behind it is a fine-grained personal access token the user pastes. It is accepted
only if it has the fine-grained shape, GitHub says whose it is, and it can read at least one
connected repository. The token, the login and an expiry time are sealed with AES-256-GCM into a
cookie that scripts cannot read, and nothing about the session is kept on the server. Every read of
GitHub for a user is made with that user's token. "Can this user read this repository" is GitHub's
answer to that token, believed for a configured period of at most an hour. A repository GitHub
reports as public is refused for every user.

## Consequences

- **Positive:** GitHub's access rules are the service's access rules, with no second table. Access
  removed on GitHub ends in the workspace within the configured period.
- **Positive:** no store of tokens to protect, back up or leak. A breach of the server's disk finds
  none.
- **Positive:** a user who can read one connected repository cannot reach another's data, because
  every read is made as them.
- **Negative:** signing out clears the cookie in that browser only. A copy of the cookie made
  beforehand works until it expires or the token is revoked on GitHub.
- **Negative:** the key that seals cookies is in the process's memory, and whoever holds it can open
  any cookie presented while it is in use. Changing it signs everyone out.
- **Negative:** users create and paste a token by hand, and paste a new one when it expires. In an
  organisation each token needs an owner's approval by default, one token covers one organisation,
  and an outside collaborator cannot use one at all.
- **Negative:** the service cannot check which permissions a pasted token carries. It asks for
  read-only ones and makes no write call whatever the token allows.
- **Neutral:** a public repository cannot be connected. Every GitHub token can read one, so
  connecting one would let any GitHub account sign in.

## Alternatives rejected

- **OAuth App sign-in** — the only scope that reads private code also grants write access to every
  repository the user can reach.
- **A GitHub App** — not rejected for good. It needs an installation the team expects not to be
  allowed, and the interface leaves room for it.
- **Google sign-in with one read-only service token** — one token that reads every connected
  repository, and a hand-kept table of who may see what.
- **Keep each user's token on the server, encrypted** — a store of credentials, and a database the
  service otherwise does not have.
- **A list of permitted logins** — a second access table beside GitHub's. Refusing public
  repositories closes the same gap with no table.
- **Accept classic tokens** — a classic token that reads private code can also write to it.

## Revisit when

An organisation refuses fine-grained tokens and allows an App, or users need a session to end on
the server the moment they sign out.
