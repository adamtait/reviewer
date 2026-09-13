<!-- SPDX-License-Identifier: MIT -->
# Writing convention rules

A convention is a pattern, not a prompt. If a reviewer can say "we always do X here" and point at
two examples, the rule can be written down and checked, and it should be — a pattern match is free,
deterministic, and gives the same answer on the same code every time. Only the things that genuinely
need judgement belong in the model lane.

Rules live in the repository being reviewed, at `.review/rules/*.yaml`. They are never in this
repository: a rule encodes the architecture of the codebase it guards, which makes it that
repository's property (ADR-0004, ADR-0010).

## How rules are run

`reviewer` spawns **Opengrep** as a separate process — `opengrep scan --json` — and reads its
report. Nothing is linked, vendored or imported: Opengrep is LGPL-2.1 and this project is MIT, so
the process boundary is what makes the combination lawful (ADR-0011). A test asserts the boundary
rather than trusting it.

Practical consequences:

- **Opengrep is optional.** Without it, the analyzer reports itself unavailable and the run
  continues with a note. Nothing fails (ADR-0013).
- **No rules is the normal starting state.** An empty or absent `.review/rules/` is not an error,
  and the analyzer says "write one to enable convention checks" rather than "opengrep is not
  installed" — the second would send you to install a tool you do not yet need.
- **Only changed files are scanned**, and findings are filtered back to the changed set here as well
  as by the core's diff filter. A rule you add today does not open a hundred comments on code nobody
  touched (ADR-0007).

## A rule

```yaml
rules:
  - id: no-raw-fetch-in-domain
    languages: [typescript]
    severity: ERROR
    message: >
      The domain layer must not call fetch directly. Inject a client so the
      behaviour can be substituted in a test.
    patterns:
      - pattern: fetch(...)
      - pattern-inside: |
          $BODY
    paths:
      include:
        - src/domain/
```

Three things this tool cares about:

**`id`** becomes the finding's rule id, namespaced: `no-raw-fetch-in-domain` is reported as
`conventions/no-raw-fetch-in-domain`. The namespace is this tool's, not Opengrep's, so a rule keeps
its identity across Opengrep upgrades.

**`severity`** maps to the finding's severity: `ERROR` → error, `INFO` → info, anything else →
warning. A level this tool does not recognise becomes a warning rather than an error: an unfamiliar
rule set should not be able to raise the loudest thing the tool can say.

**`message`** is what a reviewer reads on their pull request. Write it as a sentence to a colleague,
with the reason in it. "Don't use fetch" is a rule; "inject a client so the behaviour can be
substituted in a test" is a rule someone will follow.

## Keeping a rule's identity when you rename it

Acceptance is measured per rule id (ADR-0024), and a rule whose id changes starts that measurement
from zero. Opengrep derives its `check_id` partly from the file the rule lives in, so moving a rule
between files would silently reset its history. To pin it:

```yaml
    metadata:
      reviewer-rule-id: arch/no-raw-fetch-in-domain
```

Declared ids are used verbatim, namespace included, so this is also how a rule joins a namespace
other than `conventions/`.

## Choosing what to write

Write the rule for the comment you have left three times. The measurement in
`reviewer metrics` will tell you within a few weeks whether it was worth writing — a rule whose
findings are routinely dismissed is costing attention and should be deleted, which is the point of
measuring per rule rather than in aggregate.

Do not write a rule for something a type would catch. `tsc` already runs, it is faster, and its
message is better.
