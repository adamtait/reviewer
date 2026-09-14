// SPDX-License-Identifier: MIT

/**
 * Dead code: exports nothing imports, and dependencies nothing uses.
 *
 * The value here is entirely in the filtering. Every codebase of any age has dead
 * code, and a tool that reported all of it on the first pull request that touched
 * the file would be uninstalled that afternoon. So a finding survives only if the
 * pull request introduced or touched the symbol itself — pre-existing dead code
 * two lines away is not this change's problem (ADR-0007).
 *
 * Knip is spawned rather than imported. Its programmatic API is explicitly
 * internal, and the JSON reporter is the documented surface; a spawned process
 * also cannot take the plugin down with it.
 */

import { spawn } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import type { Analyzer } from "../serve.js";
import type { AnalyzeRequest, Finding } from "../protocol.js";

export const ID = "knip";
export const ORDER = 50;

/**
 * Config files Knip recognises. Without one, Knip guesses this repository's entry
 * points, and a wrong guess makes every export in the codebase look unused.
 *
 * This analyzer therefore declines rather than guessing, in the same way the
 * ESLint analyzer declines without a flat config: the repository's own
 * configuration is the rules, and inventing rules produces findings nobody asked
 * for and nobody trusts.
 */
const CONFIGS = [
  "knip.json",
  "knip.jsonc",
  ".knip.json",
  ".knip.jsonc",
  "knip.config.ts",
  "knip.config.js",
  "knip.config.mjs",
  "knip.config.cjs",
];

/** The shape Knip's JSON reporter emits. Verified against knip 5.x. */
interface KnipReport {
  issues?: KnipFileIssues[];
}

interface KnipFileIssues {
  file: string;
  exports?: KnipSymbol[];
  types?: KnipSymbol[];
  dependencies?: KnipNamed[];
  devDependencies?: KnipNamed[];
  unlisted?: KnipNamed[];
}

interface KnipSymbol {
  name: string;
  line?: number;
  col?: number;
}

/**
 * Knip reports a named issue with the line it sits on — for a dependency, the line
 * in package.json that declares it; for an unlisted import, the import statement.
 *
 * Verified against knip 6.35.1. An earlier version of this analyzer assumed the
 * line was absent and re-derived it by searching the file for the package name,
 * which finds the first occurrence anywhere — an `overrides` or `resolutions`
 * block above `dependencies` wins, and the finding lands on a line the change did
 * not touch, where the core's diff filter drops it.
 */
interface KnipNamed {
  name: string;
  line?: number;
  col?: number;
}

export const knipAnalyzer: Analyzer = {
  id: ID,
  order: ORDER,
  lane: "deterministic",

  async unavailable(root: string): Promise<string | undefined> {
    if (findConfig(root) === undefined) {
      return `no Knip config in ${root}; add one so entry points are declared rather than guessed`;
    }
    if (resolveKnip(root) === undefined) {
      return "knip is not a dependency of this repository";
    }
    return undefined;
  },

  async run(req: AnalyzeRequest) {
    const bin = resolveKnip(req.root);
    if (bin === undefined) {
      return { findings: [], warnings: [`${ID}: knip is not a dependency of this repository`] };
    }

    const result = await runKnip(bin, req.root);
    if ("error" in result) {
      return { findings: [], warnings: [`${ID}: ${result.error}`] };
    }

    const touched = changedLines(req);
    const findings: Finding[] = [];

    for (const issues of result.report.issues ?? []) {
      const file = toSlash(issues.file);

      for (const symbol of [...(issues.exports ?? []), ...(issues.types ?? [])]) {
        const line = symbol.line ?? 0;
        if (!touched(file, line)) continue;
        findings.push({
          // Assigned by the core, never by an analyzer (ADR-0015).
          fingerprint: "",
          ruleId: "dead/unused-export",
          lane: "deterministic",
          // Knip cannot see a dynamic import, a re-export consumed by name from a
          // barrel, or a symbol a published package exposes on purpose. It is
          // right far more often than not, which is exactly the case for the
          // collapsed summary rather than an inline comment (ADR-0017).
          confidence: "medium",
          severity: "warning",
          file,
          line,
          message:
            `\`${symbol.name}\` is exported here and nothing imports it. Delete it, or, if it is ` +
            "part of this package's public surface, tell Knip so in its config.",
        });
      }

      for (const [kind, deps] of [
        ["dependencies", issues.dependencies ?? []],
        ["devDependencies", issues.devDependencies ?? []],
      ] as [string, KnipNamed[]][]) {
        for (const dep of deps) {
          const at = dep.line ?? 0;
          if (!touched(file, at)) continue;
          findings.push({
            fingerprint: "",
            ruleId: "deps/unused-dependency",
            lane: "deterministic",
            confidence: "medium",
            severity: "warning",
            file,
            line: at,
            message:
              `\`${dep.name}\` is declared as a ${kind === "devDependencies" ? "dev " : ""}` +
              "dependency here and nothing imports it.",
          });
        }
      }

      for (const dep of issues.unlisted ?? []) {
        const at = dep.line ?? 0;
        if (!touched(file, at)) continue;
        findings.push({
          fingerprint: "",
          ruleId: "deps/undeclared-dependency",
          lane: "deterministic",
          // Not a judgement: the import is there and the declaration is not.
          confidence: "high",
          severity: "error",
          file,
          line: at,
          message:
            `\`${dep.name}\` is imported here but is not a declared dependency. It works today ` +
            "because something else installed it, and it will stop working when that changes.",
        });
      }
    }

    return { findings, warnings: result.warnings };
  },
};

/**
 * Runs Knip and reads its report.
 *
 * `--no-exit-code` because Knip exits non-zero when it finds something, and this
 * tool has no use for a distinction the report already carries. Output that will
 * not parse means the run did not happen, whatever the status said.
 */
async function runKnip(
  bin: string,
  root: string,
): Promise<{ report: KnipReport; warnings: string[] } | { error: string }> {
  const { code, stdout, stderr } = await execute(bin, ["--reporter", "json", "--no-exit-code"], root);

  let report: KnipReport;
  try {
    const parsed: unknown = JSON.parse(stdout);
    // Knip has emitted both shapes across versions: an object with `issues`, and
    // a bare array of the same per-file objects.
    report = Array.isArray(parsed) ? { issues: parsed as KnipFileIssues[] } : (parsed as KnipReport);
  } catch {
    return { error: `knip exited ${code} without a report: ${firstLine(stderr) || "no output"}` };
  }

  const warnings: string[] = [];
  const note = firstLine(stderr);
  if (note !== "") warnings.push(`${ID}: ${note}`);
  return { report, warnings };
}

function execute(
  bin: string,
  args: string[],
  cwd: string,
): Promise<{ code: number; stdout: string; stderr: string }> {
  return new Promise((resolve) => {
    // Spawned through this process's own Node rather than relying on the shebang,
    // so it runs on the same runtime the plugin was started with.
    const child = spawn(process.execPath, [bin, ...args], { cwd, stdio: ["ignore", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (chunk: Buffer) => (stdout += chunk.toString()));
    child.stderr.on("data", (chunk: Buffer) => (stderr += chunk.toString()));
    child.on("error", (err) => resolve({ code: -1, stdout, stderr: String(err) }));
    child.on("close", (code) => resolve({ code: code ?? -1, stdout, stderr }));
  });
}

/**
 * Resolves Knip's executable from the repository under review, never from here.
 *
 * The node_modules chain is walked rather than asking Node to resolve
 * `knip/package.json`: a package's `exports` map decides what may be imported by
 * subpath, and Knip's does not list its own manifest — so the obvious
 * `require.resolve("knip/package.json")` fails with ERR_PACKAGE_PATH_NOT_EXPORTED
 * on an installation that is perfectly present. Walking up is also the more
 * honest question: is Knip installed *in this repository*.
 */
function resolveKnip(root: string): string | undefined {
  for (let dir = path.resolve(root); ; dir = path.dirname(dir)) {
    const manifest = path.join(dir, "node_modules", "knip", "package.json");
    if (fs.existsSync(manifest)) {
      const bin = binFrom(manifest);
      if (bin !== undefined) return bin;
    }
    if (path.dirname(dir) === dir) return undefined;
  }
}

function binFrom(manifest: string): string | undefined {
  try {
    const pkg = JSON.parse(fs.readFileSync(manifest, "utf8")) as { bin?: string | Record<string, string> };
    const bin = typeof pkg.bin === "string" ? pkg.bin : pkg.bin?.["knip"];
    if (bin === undefined) return undefined;
    const full = path.join(path.dirname(manifest), bin);
    return fs.existsSync(full) ? full : undefined;
  } catch {
    return undefined;
  }
}

function findConfig(root: string): string | undefined {
  for (const name of CONFIGS) {
    if (fs.existsSync(path.join(root, name))) return name;
  }
  try {
    const pkg = JSON.parse(fs.readFileSync(path.join(root, "package.json"), "utf8")) as Record<string, unknown>;
    if (pkg["knip"] !== undefined) return "package.json";
  } catch {
    // No package.json, or an unreadable one. Handled by the caller.
  }
  return undefined;
}

/**
 * Whether a line is one this pull request touched.
 *
 * The whole analyzer rests on this. Without it, adding Knip to a mature codebase
 * opens a comment on every dead export in every file anyone touches.
 */
function changedLines(req: AnalyzeRequest): (file: string, line: number) => boolean {
  const ranges = new Map<string, [number, number][]>();
  for (const c of req.changed) ranges.set(toSlash(c.path), c.ranges);
  return (file, line) => {
    const spans = ranges.get(file);
    if (spans === undefined || line < 1) return false;
    return spans.some(([start, end]) => line >= start && line <= end);
  };
}

function toSlash(p: string): string {
  return p.split(path.sep).join("/");
}

function firstLine(s: string): string {
  const trimmed = s.trim();
  const i = trimmed.indexOf("\n");
  return (i >= 0 ? trimmed.slice(0, i) : trimmed).trim();
}
