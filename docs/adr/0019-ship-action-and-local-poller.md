<!-- SPDX-License-Identifier: MIT -->
# ADR-0019 — Ship both a GitHub Action and a local poller

- **Status:** Accepted
- **Date:** 2026-09-13
- **Implemented by:** PR-22
- **Supersedes:** —
- **Superseded by:** —

## Context

The Action is the better trigger: it fires on the push, needs no machine to be awake, and runs where the
code already is. The plan treated a local poller as a contingency to build only if Actions turned out to
be unavailable.

The trouble with a contingency is that it is built under pressure, by someone who has just discovered
they need it, against a design that assumed they would not. And the reasons Actions might be unavailable
are not hypothetical: an organisation can disable them, restrict them to verified publishers, withhold
`pull-requests: write`, or require approval for a workflow's first run by an outside contributor. Every
one of those is a policy decision made by someone other than the person who installed this tool.

A system whose only trigger needs somebody else's permission can be switched off by a policy change.

## Decision

Both ship, from the start. `reviewer watch` polls open pull requests, reviews those whose `updatedAt` has
moved since the last poll, and exits. The Action and the poller call the same review path with the same
reporter, so the two triggers cannot drift in what they report.

`watch` performs **one pass** rather than looping. Scheduling belongs to launchd, systemd or cron, which
already handle restarts, log rotation and machine sleep; a daemon here would reimplement all three worse
and add a process that can be wedged or forgotten (ADR-0008).

Three behaviours the watermark makes possible, each with a test: an unchanged pull request is skipped; a
new push is reviewed again, because the watermark stores a timestamp rather than a flag; and a failed
review does *not* advance the watermark, so the next poll retries it.

Drafts are skipped. Commenting on one is interrupting someone who has not asked for an opinion yet.

## Consequences

- **Positive:** the tool cannot be disabled by an organisation policy it does not control. The poller is
  also the honest development surface — reviewing a real pull request from a laptop needs no workflow.
- **Negative:** latency is the poll interval rather than seconds, so the poller is a worse experience and
  should not be anyone's first choice. It also needs a machine that is awake, and a token stored somewhere
  on it — the plist reads the token from a file rather than embedding it, because a plist is
  world-readable and gets copied around.
- **Negative:** two triggers is two things to keep working. Mitigated by both calling one review path: the
  surfaces differ only in how they decide *which* pull request.
- **Neutral:** the watermark is a JSON file, written atomically. A poller killed mid-write would otherwise
  leave a truncated file and re-review everything on the next tick.

## Alternatives rejected

- **Action only** — one policy change away from having no trigger at all.
- **Poller only** — worse latency, needs a machine, and forgoes the surface that already has credentials
  and compute where the code lives.
- **A long-running daemon** — reimplements scheduling, restart and log rotation, and can be wedged
  without anyone noticing.
- **A webhook receiver** — needs a host and a public endpoint, which is the service ADR-0008 rejected.

## Revisit when

Poll latency becomes the thing people complain about, which would mean the poller has become the primary
trigger and the reason Actions are unavailable is worth solving directly.
