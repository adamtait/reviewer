// SPDX-License-Identifier: MIT

/** Entry point. Registers the analyzers this plugin provides and serves them. */

import { tscAnalyzer } from "./analyzers/tsc.js";
import { protectStdout, serve, type Analyzer } from "./serve.js";

const analyzers: Analyzer[] = [
  tscAnalyzer,
  // typescript-eslint lands in PR-14, dependency-cruiser in PR-15, Knip in
  // PR-29, type-coverage in PR-31 and the changed-tests runner in PR-32. Each
  // reuses the program tscAnalyzer already built.
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
