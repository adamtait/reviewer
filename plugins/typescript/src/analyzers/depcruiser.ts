// SPDX-License-Identifier: MIT

/**
 * Architectural boundary violations, from the repository's own
 * `.dependency-cruiser.js` rules.
 *
 * This is the highest-value deterministic check in the system and also the one
 * whose findings are hardest to place: a violation is a fact about an *edge*
 * between two modules, while a review comment has to live on a line. The edge is
 * reported on the import statement that creates it, found by reading the source,
 * because "somewhere in this file" is not a comment a reviewer can act on.
 */

import path from "node:path";
import { createRequire } from "node:module";
import type { Analyzer } from "../serve.js";
import type { AnalyzeRequest, Finding, Severity } from "../protocol.js";

export const ID = "dependency-cruiser";
export const ORDER = 40;

/** Config file names dependency-cruiser recognises, in its own order. */
const CONFIGS = [
  ".dependency-cruiser.js",
  ".dependency-cruiser.cjs",
  ".dependency-cruiser.mjs",
  ".dependency-cruiser.json",
  ".dependency-cruiser.jsonc",
];

/** The subset of dependency-cruiser's API this analyzer uses. */
interface CruiseModule {
  cruise(
    files: string[],
    options?: { ruleSet?: unknown; validate?: boolean; baseDir?: string; tsConfig?: { fileName: string } },
  ): Promise<CruiseResult>;
}

interface CruiseResult {
  output: {
    summary: {
      violations: Violation[];
    };
  };
}

interface Violation {
  from: string;
  to: string;
  rule: { name: string; severity: "error" | "warn" | "info" | "ignore" };
  cycle?: { name: string }[];
}

/**
 * Loads dependency-cruiser.
 *
 * It is ESM-only from v16, so `createRequire` cannot load it — a dynamic `import`
 * is the only way in. That also means the "resolve from the repository root"
 * trick used for `typescript` does not apply, and it does not need to: the plugin
 * is installed as a devDependency *of the repository under review*, so it sits
 * inside that repository's `node_modules` and ordinary resolution from here finds
 * the repository's own hoisted copy. `resolvedFrom` reports which file was
 * actually loaded so the run log can show it rather than claim it.
 */
async function loadCruiser(): Promise<{ mod: CruiseModule; from: string } | undefined> {
  try {
    const from = import.meta.resolve("dependency-cruiser");
    const mod = (await import("dependency-cruiser")) as unknown as CruiseModule;
    return { mod, from };
  } catch {
    return undefined;
  }
}

async function findConfig(root: string): Promise<string | undefined> {
  const { access } = await import("node:fs/promises");
  for (const name of CONFIGS) {
    const candidate = path.join(root, name);
    try {
      await access(candidate);
      return candidate;
    } catch {
      // try the next one
    }
  }
  return undefined;
}

export const depCruiserAnalyzer: Analyzer = {
  id: ID,
  order: ORDER,
  lane: "deterministic",

  async unavailable(root: string): Promise<string | undefined> {
    if ((await loadCruiser()) === undefined) return "dependency-cruiser is not installed in this repository";
    if ((await findConfig(root)) === undefined) {
      return "no .dependency-cruiser config found; this analyzer reports the repository's own rules";
    }
    return undefined;
  },

  async run(req: AnalyzeRequest) {
    const warnings: string[] = [];

    const loaded = await loadCruiser();
    if (loaded === undefined) {
      return { findings: [], warnings: [`${ID}: not installed in this repository`] };
    }

    const configPath = await findConfig(req.root);
    if (configPath === undefined) {
      return { findings: [], warnings: [`${ID}: no .dependency-cruiser config found`] };
    }

    let ruleSet: unknown;
    try {
      ruleSet = await loadRuleSet(req.root, configPath);
    } catch (err) {
      return {
        findings: [],
        warnings: [...warnings, `${ID}: could not read ${path.basename(configPath)}: ${message(err)}`],
      };
    }

    const changed = req.changed.map((c) => c.path);
    if (changed.length === 0) return { findings: [], warnings };

    let result: CruiseResult;
    try {
      // baseDir matters: dependency-cruiser resolves both the file list and every
      // import against the process working directory otherwise, which is the
      // plugin's own directory rather than the repository under review.
      //
      // Cruising only the changed files still follows their dependencies, so a
      // rule about what a changed file may import is evaluated correctly. A rule
      // about what may import *it* is not — noted in the PR description.
      result = await loaded.mod.cruise(changed, { ruleSet, validate: true, baseDir: req.root });
    } catch (err) {
      return { findings: [], warnings: [...warnings, `${ID}: ${message(err)}`] };
    }

    const findings: Finding[] = [];
    for (const v of result.output.summary.violations) {
      if (v.rule.severity === "ignore") continue;

      const file = toRepoPath(v.from);
      const line = await importLine(path.join(req.root, file), v.to);

      findings.push({
        fingerprint: "",
        ruleId: `arch/${v.rule.name}`,
        lane: "deterministic",
        // The rule is the repository's own statement about its architecture, and
        // the edge either exists or it does not. There is nothing to be unsure of.
        confidence: "high",
        severity: severityFor(v.rule.severity),
        file,
        line,
        message: describe(v),
      });
    }
    return { findings, warnings };
  },
};

/** Loads the rule set, whether it is JSON or a module. */
async function loadRuleSet(root: string, configPath: string): Promise<unknown> {
  if (configPath.endsWith(".json") || configPath.endsWith(".jsonc")) {
    const { readFile } = await import("node:fs/promises");
    const raw = await readFile(configPath, "utf8");
    // jsonc: strip line comments, which is all dependency-cruiser's own examples use.
    return JSON.parse(raw.replace(/^\s*\/\/.*$/gm, "")) as unknown;
  }
  const requireFromRepo = createRequire(path.join(root, "package.json"));
  const loaded = requireFromRepo(configPath) as { default?: unknown };
  return loaded.default ?? loaded;
}

/**
 * Finds the line of the import that creates a forbidden edge.
 *
 * A violation names two modules, not a position. Reporting line 1 would put every
 * boundary comment at the top of the file, away from the import that caused it, so
 * the source is scanned for the specifier. Falling back to line 1 when it cannot
 * be found is deliberate: a finding in roughly the right place beats no finding.
 */
async function importLine(absPath: string, to: string): Promise<number> {
  const { readFile } = await import("node:fs/promises");
  let source: string;
  try {
    source = await readFile(absPath, "utf8");
  } catch {
    return 1;
  }

  // Match on the module's basename without extension, which survives the
  // difference between what was written ("../infra/http.js") and what was
  // resolved ("src/infra/http.ts").
  const stem = path.basename(to).replace(/\.[cm]?[jt]sx?$/, "");
  const lines = source.split("\n");
  for (const [i, line] of lines.entries()) {
    if (!/^\s*(?:import|export)\b|require\(/.test(line)) continue;
    if (line.includes(stem)) return i + 1;
  }
  return 1;
}

function describe(v: Violation): string {
  if (v.cycle !== undefined && v.cycle.length > 0) {
    const hops = v.cycle.map((c) => toRepoPath(c.name)).join(" → ");
    return `Dependency cycle (${v.rule.name}): ${toRepoPath(v.from)} → ${hops}`;
  }
  return `${toRepoPath(v.from)} must not depend on ${toRepoPath(v.to)} (${v.rule.name})`;
}

function toRepoPath(p: string): string {
  return p.split(path.sep).join("/");
}

function severityFor(s: Violation["rule"]["severity"]): Severity {
  switch (s) {
    case "error":
      return "error";
    case "warn":
      return "warning";
    default:
      return "info";
  }
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
