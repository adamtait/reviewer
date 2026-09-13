// SPDX-License-Identifier: MIT

/** Entry point. Registers the analyzers this plugin provides and serves them. */

import { protectStdout, serve, type Analyzer } from "./serve.js";

const analyzers: Analyzer[] = [
  // Analyzers land in PR-13a (tsc), PR-14 (typescript-eslint), PR-15
  // (dependency-cruiser), PR-29 (knip), PR-31 (type-coverage) and PR-32
  // (changed tests). The scaffold ships with none so the protocol can be proven
  // across the language boundary before six analyzers depend on it.
];

protectStdout();

serve({
  name: "typescript",
  version: process.env["REVIEWER_PLUGIN_VERSION"] ?? "0.1.0",
  analyzers,
}).catch((err: unknown) => {
  process.stderr.write(`typescript: ${err instanceof Error ? err.stack : String(err)}\n`);
  process.exit(1);
});
