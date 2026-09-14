<!-- SPDX-License-Identifier: MIT -->
# Plugin protocol, version 1

Every analyzer reaches the core as a plugin, including the first-party ones
(ADR-0006). There is no privileged in-process path, so the protocol is exercised
on every run rather than only by strangers.

Machine-readable frame reference: [`schema/plugin-protocol.schema.json`](../schema/plugin-protocol.schema.json).
Findings shape: [`schema/finding.schema.json`](../schema/finding.schema.json).

## Transport

Newline-delimited JSON over stdio. One JSON object per line, no length prefix, no
framing header.

**Stdout carries frames and nothing else.** A log line, a progress bar or a stack
trace on stdout corrupts the stream. Diagnostics go to stderr, which the host
captures into the run log. This is the single most common way to break a plugin.

The process lives for the whole run. That is deliberate: it is what lets one
plugin serve many analyzers from shared state — the TypeScript plugin builds a
compiler program once and reuses it for every analyzer that needs one (ADR-0014).

## Conversation

```
host   → hello       protocol version and host identity
plugin → hello       protocol version, plugin name and version
host   → describe
plugin → describe    the analyzers this plugin provides, here, now
host   → analyze     one analyzer at a time, in the order the core decides
plugin → findings    or error
   …                 repeated per analyzer
host   → bye
plugin               exits
```

Rules a plugin must follow:

- Answer `hello` before anything else. A frame that arrives before the handshake
  is an error.
- Refuse a protocol version you do not implement. Reply with an `error` frame
  naming both versions and exit — do not guess at an unknown dialect.
- Ignore frame types you do not recognise. A newer host may send frames your
  plugin predates; ignoring them is what keeps the protocol additively
  extensible.
- Never exit on an analyzer failure. Reply with an `error` frame naming the
  analyzer; the host drops that analyzer's results and continues (ADR-0013).

## Descriptors

`describe` is where a plugin decides what it can actually do in *this*
repository. Probing for an ESLint config or a test runner here costs one
filesystem call and saves a pointless round trip:

```json
{"type":"describe","analyzers":[
  {"id":"tsc","lane":"deterministic","order":20,"available":true},
  {"id":"knip","lane":"deterministic","order":60,"available":false,"unavailable":"no knip configuration found"}
]}
```

`lane` is declared by the plugin, not inferred by the core. The secrets gate
blocks every `llm`-lane analyzer outright when a credential is found in the diff
(ADR-0012), and it can only do that if the lane is on the descriptor — a
core-side allowlist would let a third-party plugin walk past it (ADR-0005).

`order` places the analyzer in the run sequence, lower first. The core leaves
gaps between its own analyzers so a plugin can interleave.

## Reporting a finding

A findings frame answers `analyze`:

```json
{"type":"findings","findings":[
  {"ruleId":"arch/no-domain-to-infra","lane":"deterministic","confidence":"high",
   "severity":"error","file":"src/domain/order.ts","line":12,
   "message":"domain imports infrastructure","evidence":"import { db } from '../infra/db'"}
],"warnings":["two files were skipped: unparseable"]}
```

`fingerprint` is filled in by the core and ignored on the way in. Everything else
is yours. `file` is relative to the repository root with forward slashes, `line`
is 1-indexed, and `endLine` is inclusive — zero means one line.

### The line must be one the diff changed

This is the rule that costs people the most, so it comes before the rest.

**A finding whose file and line fall outside the changed ranges is dropped by the
core, silently, and nothing tells you** (ADR-0007). It is not a warning and not a
configurable filter; there is no opt-out. A review that opened comments on code
the author did not touch would be worthless, and that guarantee is worth more
than any individual finding.

The failure mode is the problem. Your analyzer reports; the run reports nothing;
every unit test passes, because a unit test hands your analyzer a fixture and
checks what it returns — which is correct. Five analyzers in this repository
shipped exactly that way. A vulnerability scanner reported every advisory at
line 1 of the lockfile. A dead-code analyzer did the same. A coverage analyzer
reported against a baseline file that no real change ever touches. Each was a
correct finding that no human could ever see.

So when what you have found is not *on* a changed line, you have to do the work
of anchoring it to one:

- A dependency problem belongs on the changed line that names the package — look
  it up in the manifest diff rather than defaulting to line 1.
- A whole-file problem belongs on the first changed line in that file.
- A problem in a file the change did not touch at all is either not this change's
  business, or belongs on the changed line that caused it. Decide which; there is
  no third answer that reaches a reader.

And test it against a real diff. `AnalyzeRequest.Changed` is right there in the
request, so a test can assert that every finding lands inside it:

```go
for _, f := range got {
    if !onChangedLine(req.Changed, f.File, f.Line) {
        t.Errorf("%s at %s:%d is outside the diff and will be dropped",
            f.RuleID, f.File, f.Line)
    }
}
```

That assertion, in every analyzer's test, would have caught all five.

### Rule ids

Namespaced by category — `arch/no-domain-to-infra`, `deps/known-vulnerability`.
Two constraints:

- **Stable across releases.** Acceptance is measured per rule id (ADR-0024), and
  renaming one discards its history.
- **No whitespace and no `>`.** The id travels inside an HTML comment marker,
  which is how a posted comment is recognised again on the next run (ADR-0015).
  An id that breaks the marker means the comment is reposted on every push and
  its thread is never resolved. The core rejects such an id rather than posting
  it.

### Confidence is not severity

`severity` is how bad the problem is. `confidence` is how sure you are, and it
decides *presentation only* (ADR-0017): `high` earns an inline comment on the
line, and everything else goes into one collapsed summary comment. A `low`
confidence finding is not a mild finding — it is one you would not want to
interrupt someone for.

Report `medium` when you would defend the finding but not the exact line.

### Say you could not run, rather than reporting clean

An analyzer that cannot do its job costs a warning, never a failed review
(ADR-0013). But the two ways of saying so are not interchangeable:

- The tool is absent, or this repository has no configuration for it: say so in
  `describe`, with `available: false` and a one-line reason. The core skips it
  and reports it once.
- The run started and failed: reply with an `error` frame, or with a `findings`
  frame carrying a `warnings` entry if some of the work succeeded.

What you must never do is return an empty findings list for work that did not
happen. It is indistinguishable from a clean result, which is the one thing a
reviewer will act on. A vulnerability scanner in this repository prints a report
with an empty results array when it cannot reach its database, writing the reason
only to stderr — so reading its stdout alone reports "no vulnerabilities" for a
scan that never ran. Check the exit status, and treat "I do not know" as its own
answer.

## Writing a plugin in Go

The SDK is one interface and one call:

```go
package main

import (
    "github.com/adamtait/reviewer/pkg/finding"
    "github.com/adamtait/reviewer/pkg/plugin"
)

type handler struct{}

func (handler) Name() string    { return "example" }
func (handler) Version() string { return "0.1.0" }

func (handler) Describe() []plugin.Descriptor {
    return []plugin.Descriptor{{
        ID: "example", Lane: finding.LaneDeterministic, Order: 500, Available: true,
    }}
}

func (handler) Analyze(id string, req plugin.AnalyzeRequest) ([]finding.Finding, []string, error) {
    // req.Changed holds the files and line ranges in the diff.
    return nil, nil, nil
}

func main() { _ = plugin.Serve(handler{}) }
```

`Analyze` is never called concurrently and only for IDs `Describe` reported, so a
handler may hold expensive state between calls.

## Writing a plugin in anything else

There is no SDK requirement. [`examples/plugins/shell-hello`](../examples/plugins/shell-hello)
is a complete plugin in 40 lines of POSIX shell with no JSON library, and a Go
conformance test drives it through the real codec on every CI run. If that test
ever needs a dependency, the protocol has grown a requirement it should not have.

## Registering and running yours

A plugin is a command, so the core needs nothing installed — only a way to start
it. Add it to the destination repository's `.review/config.yaml`:

```yaml
plugins:
  - id: typescript
    command: node
    args: ["node_modules/@adamtait/reviewer-plugin-typescript/dist/main.js"]
  - id: mine
    command: ./tools/my-plugin
```

Point `args` at the **executable**, not at a package's `main` field. A library
entry point starts, exits 0 without answering the handshake, and produces a
review that reports "no analyzers ran" with nothing else visibly wrong.

Then run just yours, against a real change:

```
reviewer --base main --only mine
```

`--only` takes analyzer ids, not plugin ids, so it names what `describe`
reported. Two things to look at in the output: whether your analyzer appears at
all (if not, the handshake failed — check stderr and check that nothing else
reached stdout), and whether the findings you expected are in the report rather
than only in your own logs (if not, read the diff-filter section above).

## Versioning

The protocol version is bumped independently of the product version (ADR-0026):
the question a plugin author asks is "which protocol", not "which release".

Compatible changes — adding a frame type, adding an optional field to an existing
frame, adding a `Finding` field — do not bump it. Removing or renaming anything,
or changing a field's meaning, does.
