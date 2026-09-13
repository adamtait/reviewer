// SPDX-License-Identifier: MIT

/**
 * Type errors, from the repository's own compiler over the shared program.
 *
 * The repository's CI almost certainly runs `tsc` already, so this analyzer's
 * value is not catching type errors nobody would find — it is attributing them to
 * the lines in the diff, so a reviewer sees them in context instead of in a build
 * log. That is also why every diagnostic is high confidence: the compiler is not
 * guessing.
 */

import path from "node:path";
import type ts from "typescript";
import { formatDiagnostic, getDiagnostics } from "../program.js";
import type { Analyzer } from "../serve.js";
import type { AnalyzeRequest, Finding, Severity } from "../protocol.js";

export const ID = "tsc";
export const ORDER = 20;

export const tscAnalyzer: Analyzer = {
  id: ID,
  order: ORDER,
  lane: "deterministic",

  async unavailable(): Promise<string | undefined> {
    return undefined; // decided per request, against that repository's root
  },

  async run(req: AnalyzeRequest) {
    const warnings: string[] = [];
    let diagnostics: readonly ts.Diagnostic[];
    let compiler: typeof ts;
    let compilerFrom: string;
    let fallback: boolean;

    try {
      const result = getDiagnostics(req.root);
      diagnostics = result.diagnostics;
      compiler = result.built.compiler.ts;
      compilerFrom = result.built.compiler.from;
      fallback = result.built.compiler.fallback;
    } catch (err) {
      // No tsconfig, or an unreadable one. Not a failure of the run.
      return { findings: [], warnings: [`${ID}: ${err instanceof Error ? err.message : String(err)}`] };
    }

    if (fallback) {
      warnings.push(
        `${ID}: analysed with the plugin's own typescript (${compilerFrom}) because the repository's could not be resolved; ` +
          "findings may differ from your local build",
      );
    }

    const changed = new Set(req.changed.map((c) => c.path));
    const findings: Finding[] = [];

    for (const d of diagnostics) {
      if (d.file === undefined || d.start === undefined) continue;

      const file = path.relative(req.root, d.file.fileName).split(path.sep).join("/");
      // Scope here as well as in the core: a type error in an untouched file is
      // real but is not this pull request's doing (ADR-0007), and skipping it
      // early avoids formatting thousands of diagnostics on a large repository.
      if (!changed.has(file)) continue;

      const { line } = d.file.getLineAndCharacterOfPosition(d.start);
      const endPos = d.length === undefined ? undefined : d.file.getLineAndCharacterOfPosition(d.start + d.length);

      findings.push({
        fingerprint: "",
        // Namespaced by us, suffixed with the compiler's own code so a reader can
        // search for it and acceptance stays stable across compiler versions.
        ruleId: `types/TS${d.code}`,
        lane: "deterministic",
        confidence: "high",
        severity: severityOf(compiler, d),
        file,
        line: line + 1,
        ...(endPos !== undefined && endPos.line !== line ? { endLine: endPos.line + 1 } : {}),
        message: formatDiagnostic(compiler, d),
      });
    }

    return { findings, warnings };
  },
};

/** Maps the compiler's own category rather than hardcoding its numeric values. */
function severityOf(compiler: typeof ts, d: ts.Diagnostic): Severity {
  switch (d.category) {
    case compiler.DiagnosticCategory.Error:
      return "error";
    case compiler.DiagnosticCategory.Warning:
      return "warning";
    default:
      return "info";
  }
}
