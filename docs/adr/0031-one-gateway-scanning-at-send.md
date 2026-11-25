<!-- SPDX-License-Identifier: MIT -->
# ADR-0031 — One gateway for the workspace's model calls, scanning at the moment of sending

- **Status:** Proposed
- **Date:** 2026-11-24
- **Implemented by:** PR-53
- **Supersedes:** —
- **Superseded by:** —

## Context

The review workspace's assistant sends repository content to a model provider: the reader's
question, the code they selected, and whatever the model asks its tools to read. A repository
holds credentials more often than its owners know, and what is sent to a provider cannot be
unsent.

ADR-0012 answers this for the command-line tool: any detection by the secrets scanner, and any
failure to scan, stops the whole model lane. That fits one review of one diff. A conversation is a
different shape. It reads many files, chosen by the model as it goes, and a fixture credential in
one of them would end every answer that touched it. In the design's tests the planned scan flagged
1.6% of this repository's files and 0.2% of a large source tree's, nearly all of them tests and
fixtures.

Two more facts shape the decision. Content can try to switch a scanner off: an exemption comment on
the credential's line, an ignore file beside it, base64 around it, a file name the rules skip. And
each connected repository belongs to someone who chose one provider for it.

This record is `Proposed` while the workspace's first version is on trial. ADR-0012 stays in force
for the command-line tool.

## Decision

One package, the gateway, is the only code in the service that is given provider keys and the only
code that calls a provider. Tests hold the source to that. Each connected repository names one
provider and one model, and a question about a repository reaches that provider and no other.

Everything is scanned at the moment of sending, by the same scanner the command-line tool uses, and
the whole file is scanned behind any lines quoted from it. What happens on a detection depends on
whose content it is:

- **In what the reader sent** — the question, an earlier question or answer, the selected code or
  the file it comes from — nothing is sent, and the reader is told what held it.
- **In what a tool returned** — a file, lines from files, the pull request's description — that
  item is withheld. The model gets a note in its place and the reader is told which item. The
  answer goes on without it.
- **If the scanner cannot run,** nothing is sent.

The scanner is run one fixed way. Each item is written under a generated name in its own numbered
directory, never at the scan root. The scanner runs from an empty directory with no environment,
with its default rules and no allowlist, with the inline exemption comment disabled and encoded
text decoded two layers deep. Each of the four ways content could hide a credential was tried
against the pinned scanner, and each is a test.

A provider's own reasoning data is not scanned. It is opaque, it came from that provider, and it
goes back only to that provider within the same question.

A question ends in exactly one of eight outcomes, and the gateway never returns an error. A wait the
provider names within a configured ceiling is sat out once; heavy demand is tried again once.
Every model call writes one log line that holds no content.

## Consequences

- **Positive:** a credential in a test fixture costs the model that one file, not the answer.
- **Positive:** one place to read to know what leaves the service, and tests that fail if a second
  place appears.
- **Positive:** nothing in a repository can exempt its own content from the scan.
- **Negative:** this is weaker than ADR-0012 for content a tool returns. A file the scanner does
  not flag is sent, and the scanner's default rules have exemptions of their own, keyed on
  content, such as placeholder words for its generic rule. The rules for named credential formats
  have none.
- **Negative:** withholding is by file. One credential in a long file withholds all of it, and the
  model answers without it.
- **Negative:** every question runs the scanner at least once, and each tool round once more. A
  result is remembered by content, scanner version and rules, so a file is scanned once.
- **Negative:** the image holds a second program, the scanner. It is the only one the service
  starts.
- **Neutral:** the scan is of text. An image, a binary or anything the tools do not return is never
  sent, and so is never scanned.

## Alternatives rejected

- **ADR-0012's rule, unchanged** — stop the whole question on any detection. A fixture credential
  anywhere in the files the model reads would end every such answer.
- **Scan a repository once, when it is opened** — scanning at the moment of sending covers what is
  actually sent, including the reader's own typed text, and needs no stored verdicts.
- **Withhold only the flagged lines** — a credential can straddle the lines quoted, and the scanner
  judges a file, not a range.
- **An allowlist the repository can extend** — a pull request could then clear its own secret.
- **Let each package call its provider** — as many places to scan, or to forget to.

## Revisit when

A credential is found to have been sent, the withheld share of files makes answers poor, or a second
provider needs a different rule.
