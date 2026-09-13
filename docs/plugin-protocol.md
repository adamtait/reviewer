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

## Versioning

The protocol version is bumped independently of the product version (ADR-0026):
the question a plugin author asks is "which protocol", not "which release".

Compatible changes — adding a frame type, adding an optional field to an existing
frame, adding a `Finding` field — do not bump it. Removing or renaming anything,
or changing a field's meaning, does.
