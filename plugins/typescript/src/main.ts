// SPDX-License-Identifier: MIT

/** Entry point. Registers the analyzers this plugin provides and serves them. */

import { changedTestsAnalyzer } from "./analyzers/changedtests.js";
import { depCruiserAnalyzer } from "./analyzers/depcruiser.js";
import { eslintAnalyzer } from "./analyzers/eslint.js";
import { knipAnalyzer } from "./analyzers/knip.js";
import { tscAnalyzer } from "./analyzers/tsc.js";
import { typecovAnalyzer } from "./analyzers/typecov.js";
import { protectStdout, serve, type Analyzer } from "./serve.js";

const analyzers: Analyzer[] = [
  tscAnalyzer,
  eslintAnalyzer,
  depCruiserAnalyzer,
  knipAnalyzer,
  typecovAnalyzer,
  changedTestsAnalyzer,
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
