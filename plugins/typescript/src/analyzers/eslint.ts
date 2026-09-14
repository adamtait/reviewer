// SPDX-License-Identifier: MIT

/**
 * Type-aware lint, using the repository's own ESLint and its own flat config.
 *
 * The rule set is not ours. Shipping our own rules would mean reporting things the
 * repository has deliberately turned off, which is the fastest way to be muted.
 * What this analyzer adds is the *type-aware* subset — `no-floating-promises` and
 * friends — surfaced on the diff, because those are the rules a repository most
 * often has configured but does not run in CI for cost reasons.
 */

import path from "node:path";
import { createRequire } from "node:module";
import type { Analyzer } from "../serve.js";
import type { AnalyzeRequest, Confidence, Finding, Severity } from "../protocol.js";

export const ID = "eslint";
export const ORDER = 30;

/**
 * The rules this analyzer reports. Restricting the set is deliberate: a full lint
 * run duplicates what the repository's own CI already does, and the value here is
 * the type-aware rules that catch a class of real Node incidents.
 *
 * A repository that has configured additional rules still gets them — this is a
 * floor on what is reported, not a ceiling, because the config comes from the
 * repository.
 */
export const TYPE_AWARE_RULES = [
  "@typescript-eslint/no-floating-promises",
  "@typescript-eslint/no-misused-promises",
  "@typescript-eslint/await-thenable",
  "@typescript-eslint/no-unsafe-argument",
  "@typescript-eslint/no-unsafe-assignment",
  "@typescript-eslint/no-unsafe-call",
  "@typescript-eslint/no-unsafe-member-access",
  "@typescript-eslint/no-unsafe-return",
] as const;

/** The subset of ESLint's API this analyzer uses. */
interface ESLintModule {
  ESLint: new (options: {
    cwd: string;
    errorOnUnmatchedPattern: boolean;
  }) => {
    lintFiles(patterns: string[]): Promise<LintResult[]>;
    // Present from ESLint 9; used only to report the version in a warning.
    readonly version?: string;
  };
}

interface LintResult {
  filePath: string;
  messages: LintMessage[];
}

interface LintMessage {
  ruleId: string | null;
  severity: 0 | 1 | 2;
  message: string;
  line?: number;
  endLine?: number;
}

/** Loads the repository's ESLint, or the plugin's own as a reported fallback. */
function loadESLint(root: string): { mod: ESLintModule; from: string; fallback: boolean } | undefined {
  const fromRepo = createRequire(path.join(root, "package.json"));
  try {
    const resolved = fromRepo.resolve("eslint");
    return { mod: fromRepo(resolved) as ESLintModule, from: resolved, fallback: false };
  } catch {
    try {
      const fromHere = createRequire(import.meta.url);
      const resolved = fromHere.resolve("eslint");
      return { mod: fromHere(resolved) as ESLintModule, from: resolved, fallback: true };
    } catch {
      return undefined;
    }
  }
}

/** Flat config file names ESLint 9 recognises. */
const FLAT_CONFIGS = ["eslint.config.js", "eslint.config.mjs", "eslint.config.cjs", "eslint.config.ts"];

async function hasFlatConfig(root: string): Promise<boolean> {
  const { access } = await import("node:fs/promises");
  for (const name of FLAT_CONFIGS) {
    try {
      await access(path.join(root, name));
      return true;
    } catch {
      // try the next one
    }
  }
  return false;
}

export const eslintAnalyzer: Analyzer = {
  id: ID,
  order: ORDER,
  lane: "deterministic",

  async unavailable(root: string): Promise<string | undefined> {
    if (loadESLint(root) === undefined) return "eslint is not installed in this repository";
    if (!(await hasFlatConfig(root))) {
      return "no eslint flat config (eslint.config.js) found; this analyzer uses the repository's own rules";
    }
    return undefined;
  },

  async run(req: AnalyzeRequest) {
    const warnings: string[] = [];

    const loaded = loadESLint(req.root);
    if (loaded === undefined) {
      return { findings: [], warnings: [`${ID}: eslint is not installed in this repository`] };
    }
    if (loaded.fallback) {
      warnings.push(
        `${ID}: linted with the plugin's own eslint (${loaded.from}); findings may differ from your local run`,
      );
    }
    if (!(await hasFlatConfig(req.root))) {
      return { findings: [], warnings: [`${ID}: no eslint flat config found`] };
    }

    // Only changed files, and only those ESLint can lint. Passing the whole
    // repository would be both slow and, after diff filtering, pointless.
    const targets = req.changed.map((c) => c.path).filter((p) => /\.(?:ts|tsx|mts|cts|js|jsx|mjs|cjs)$/.test(p));
    if (targets.length === 0) return { findings: [], warnings };

    const eslint = new loaded.mod.ESLint({ cwd: req.root, errorOnUnmatchedPattern: false });

    let results: LintResult[];
    try {
      results = await eslint.lintFiles(targets);
    } catch (err) {
      // A config that throws is the repository's problem to fix, and is not worth
      // failing the whole review over.
      return {
        findings: [],
        warnings: [...warnings, `${ID}: ${err instanceof Error ? err.message : String(err)}`],
      };
    }

    const findings: Finding[] = [];
    for (const result of results) {
      const file = path.relative(req.root, result.filePath).split(path.sep).join("/");
      for (const m of result.messages) {
        if (m.ruleId === null || m.line === undefined) continue;

        findings.push({
          fingerprint: "",
          ruleId: ruleIDFor(m.ruleId),
          lane: "deterministic",
          confidence: confidenceFor(m.ruleId),
          severity: severityFor(m.severity),
          file,
          line: m.line,
          ...(m.endLine !== undefined && m.endLine !== m.line ? { endLine: m.endLine } : {}),
          message: `${m.message} (${m.ruleId})`,
          // Deliberately no `suggestion`. ESLint's `fix` is a replacement for a
          // character range that is usually a fragment of the line, while a
          // Finding's suggestion replaces the whole reported line span. Carrying
          // one across as the other produces a GitHub suggested change that
          // deletes the rest of the line — `var x = 1` becomes `let`. Emitting
          // them correctly means reading the source and splicing the range, which
          // belongs with the suggested-changes work in M2, not here.
        });
      }
    }
    return { findings, warnings };
  },
};

/**
 * Namespaces the rule. Type-aware rules get `logic/` because they catch real
 * defects; everything else gets `lint/`, which keeps the two distinguishable in
 * acceptance measurement without needing a second analyzer.
 */
export function ruleIDFor(eslintRule: string): string {
  const typeAware = (TYPE_AWARE_RULES as readonly string[]).includes(eslintRule);
  return `${typeAware ? "logic" : "lint"}/${eslintRule.replace("@typescript-eslint/", "")}`;
}

/**
 * Type-aware rules are high confidence: they are computed from the type graph,
 * not from a heuristic. Stylistic rules are capped at medium so they land in the
 * collapsed summary rather than inline (ADR-0017) — a reviewer does not need an
 * inline comment about quote style.
 */
export function confidenceFor(eslintRule: string): Confidence {
  return (TYPE_AWARE_RULES as readonly string[]).includes(eslintRule) ? "high" : "medium";
}

function severityFor(s: 0 | 1 | 2): Severity {
  switch (s) {
    case 2:
      return "error";
    case 1:
      return "warning";
    default:
      return "info";
  }
}
