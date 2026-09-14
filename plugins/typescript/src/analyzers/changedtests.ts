// SPDX-License-Identifier: MIT

/**
 * The repository's own tests, for the files this change touched.
 *
 * Both runners this supports already know how to select tests affected by a
 * change — `vitest --changed`, `jest --changedSince` — so this analyzer does not
 * decide what to run. Its job is to turn a failure into something a reviewer can
 * see on the pull request: the assertion, the expected and actual, and a location.
 *
 * A failing test is the only finding here, and it is always high confidence.
 * Nothing is being judged: the repository's own test said no.
 */

import { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import type { Analyzer } from "../serve.js";
import type { AnalyzeRequest, Finding } from "../protocol.js";

export const ID = "changed-tests";
export const ORDER = 70;

/** Prefix for the temporary directory each run writes its report into. */
const TEMP_PREFIX = "reviewer-tests-";

/**
 * How long a report directory may survive before it is treated as abandoned.
 *
 * The `finally` that removes one does not run when the plugin's process group is
 * killed on an analyzer timeout, so a wedged run leaks a directory. Reaping older
 * ones at the start of each run is the only cleanup a killed process can get.
 */
const STALE_AFTER_MS = 60 * 60 * 1000;

/** The runners this analyzer knows how to drive. */
type Runner = "vitest" | "jest";

/**
 * Jest's JSON report shape, which Vitest also emits. The two runners agreeing on
 * this format is the reason one parser serves both.
 */
interface TestReport {
  testResults?: FileResult[];
}

interface FileResult {
  name?: string;
  status?: string;
  message?: string;
  assertionResults?: AssertionResult[];
}

interface AssertionResult {
  title?: string;
  fullName?: string;
  status?: string;
  location?: { line?: number };
  failureMessages?: string[];
}

export const changedTestsAnalyzer: Analyzer = {
  id: ID,
  order: ORDER,
  lane: "deterministic",

  async unavailable(root: string): Promise<string | undefined> {
    if (detectRunner(root) === undefined) {
      return "neither vitest nor jest is installed in this repository";
    }
    return undefined;
  },

  async run(req: AnalyzeRequest) {
    const runner = detectRunner(req.root);
    if (runner === undefined) {
      return { findings: [], warnings: [`${ID}: neither vitest nor jest is installed in this repository`] };
    }
    if (req.changed.length === 0) {
      // Nothing changed, so there is nothing for either runner to select. Spawning
      // it would run the whole suite.
      return { findings: [], warnings: [] };
    }

    const base = (req.base ?? "").trim();
    if (base !== "" && !resolves(req.root, base)) {
      // Both runners accept a ref they cannot resolve, select nothing, exit 0 and
      // write a report saying every test passed. Reporting that as a clean run is
      // the fail-open-to-silence outcome ADR-0013 exists to prevent, so the ref is
      // checked before the runner is given it.
      return {
        findings: [],
        warnings: [`${ID}: ${runner.name} cannot select tests: this checkout has no ref \`${base}\``],
      };
    }

    reapStaleReports();
    const outputFile = path.join(fs.mkdtempSync(path.join(os.tmpdir(), TEMP_PREFIX)), "report.json");
    try {
      const result = await execute(runner.bin, argsFor(runner.name, base, outputFile), req.root);

      let report: TestReport;
      try {
        report = JSON.parse(fs.readFileSync(outputFile, "utf8")) as TestReport;
      } catch {
        // No report at all. The exit code cannot tell a failing test from a
        // runner that would not start — both are non-zero — so only the report
        // distinguishes them, and its absence means the second.
        return {
          findings: [],
          warnings: [
            `${ID}: ${runner.name} exited ${result.code} without a report: ` +
              `${firstLine(result.stderr) || firstLine(result.stdout) || "no output"}`,
          ],
        };
      }

      const findings = toFindings(req, report);
      if (findings.length === 0 && (report.testResults ?? []).length === 0) {
        // The runner ran and selected nothing. With a change in hand that is worth
        // saying: it means the selection disagreed with the diff, and silence
        // would read as "the tests pass".
        return {
          findings,
          warnings: [`${ID}: ${runner.name} selected no tests for this change`],
        };
      }
      return { findings, warnings: [] };
    } finally {
      fs.rmSync(path.dirname(outputFile), { recursive: true, force: true });
    }
  },
};

/**
 * Each runner's own way of saying "only what this change affects".
 *
 * With a base ref both runners take one; without, Vitest's bare `--changed` means
 * uncommitted work, which is exactly what a staged review is looking at. Jest has
 * `--onlyChanged` for the same case.
 */
function argsFor(runner: Runner, base: string, outputFile: string): string[] {
  const ref = base.trim();
  if (runner === "vitest") {
    return [
      "run",
      ...(ref === "" ? ["--changed"] : ["--changed", ref]),
      "--reporter=json",
      `--outputFile=${outputFile}`,
    ];
  }
  return [
    ...(ref === "" ? ["--onlyChanged"] : ["--changedSince", ref]),
    "--json",
    `--outputFile=${outputFile}`,
  ];
}

function toFindings(req: AnalyzeRequest, report: TestReport): Finding[] {
  const findings: Finding[] = [];

  for (const file of report.testResults ?? []) {
    const testFile = relative(req.root, file.name ?? "");

    for (const assertion of file.assertionResults ?? []) {
      if (assertion.status !== "failed") continue;
      const message = (assertion.failureMessages ?? []).join("\n");
      const at = locate(req.root, message) ?? { file: testFile, line: assertion.location?.line ?? 1 };
      findings.push(place(req, at, describe(assertion, message)));
    }

    // A file that failed to load at all has no assertions to report against, and
    // it is the most common real failure: a syntax error, or an import of
    // something this change removed.
    if (file.status === "failed" && (file.assertionResults ?? []).every((a) => a.status !== "failed")) {
      const message = file.message ?? "the test file failed to run";
      const at = locate(req.root, message) ?? { file: testFile, line: 1 };
      findings.push(place(req, at, `${testFile} failed to run. ${firstLine(message)}`));
    }
  }

  return findings;
}

/**
 * Decides where a failure is reported.
 *
 * The assertion's own location is the right answer and usually the wrong place: a
 * test that broke because of a change to the code it covers lives in a file the
 * pull request never touched, and the core drops every finding outside the diff
 * (ADR-0007). Reported there, the analyzer would produce findings on every run
 * and the report would show none — which is exactly what the type-coverage ratchet
 * did until it was run against a real repository.
 *
 * So the comment lands on a line the change touched, and the assertion's real
 * location is carried in the message, where it is just as actionable.
 */
function place(req: AnalyzeRequest, at: { file: string; line: number }, message: string): Finding {
  const inDiff = req.changed.some(
    (c) => toSlash(c.path) === at.file && c.ranges.some(([start, end]) => at.line >= start && at.line <= end),
  );

  let file = at.file;
  let line = at.line;
  let prefix = "";
  if (!inDiff) {
    const host = relocate(req, at.file);
    if (host !== undefined) {
      file = host.file;
      line = host.line;
      prefix = `${at.file}:${at.line} — `;
    }
  }

  return {
    fingerprint: "",
    ruleId: "tests/failing",
    lane: "deterministic",
    // Not a judgement: the repository's own test said no.
    confidence: "high",
    severity: "error",
    file,
    line,
    message: prefix + message,
  };
}

/**
 * Picks a changed file to carry a failure whose assertion is outside the diff.
 *
 * Order matters: `req.changed` is in git's path order, so the first entry is
 * routinely a lockfile, a changelog or a config file — a test failure reported on
 * line 1 of a YAML file is not something a reviewer can act on. Preference goes to
 * the source file the test is named after, then to any changed source file, and
 * only then to whatever is there.
 */
function relocate(req: AnalyzeRequest, testFile: string): { file: string; line: number } | undefined {
  const stem = path.basename(testFile).replace(/\.(test|spec)\.[cm]?[jt]sx?$/i, "");

  const at = (c: AnalyzeRequest["changed"][number]): { file: string; line: number } | undefined => {
    const range = c.ranges[0];
    return range === undefined ? undefined : { file: toSlash(c.path), line: range[0] };
  };

  const sources = req.changed.filter((c) => SOURCE.test(c.path));
  const named = sources.find((c) => path.basename(c.path).replace(/\.[cm]?[jt]sx?$/i, "") === stem);
  return at(named ?? sources[0] ?? req.changed[0] ?? ({} as never));
}

/** What counts as source rather than configuration or a manifest. */
const SOURCE = /\.[cm]?[jt]sx?$/i;

/** Removes report directories a killed run left behind. */
function reapStaleReports(): void {
  const tmp = os.tmpdir();
  let entries: string[];
  try {
    entries = fs.readdirSync(tmp);
  } catch {
    return;
  }
  const cutoff = Date.now() - STALE_AFTER_MS;
  for (const name of entries) {
    if (!name.startsWith(TEMP_PREFIX)) continue;
    const full = path.join(tmp, name);
    try {
      if (fs.statSync(full).mtimeMs < cutoff) fs.rmSync(full, { recursive: true, force: true });
    } catch {
      // Someone else's, or already gone. Not this run's problem.
    }
  }
}

/** Whether this checkout can resolve a ref to a commit. */
function resolves(root: string, ref: string): boolean {
  const result = spawnSync("git", ["rev-parse", "--verify", "--quiet", `${ref}^{commit}`], {
    cwd: root,
    stdio: ["ignore", "ignore", "ignore"],
  });
  return result.status === 0;
}

function describe(assertion: AssertionResult, message: string): string {
  const name = assertion.fullName ?? assertion.title ?? "a test";
  const detail = firstLine(message);
  return detail === "" ? `\`${name}\` fails.` : `\`${name}\` fails: ${detail}`;
}

/**
 * Finds the assertion's own location in a stack trace.
 *
 * The first frame that points inside the repository is the assertion; everything
 * after it is the runner's own machinery, which is of no use to a reviewer.
 */
function locate(root: string, message: string): { file: string; line: number } | undefined {
  const pattern = /(?:at |\()?((?:\/|[A-Za-z]:\\)[^\s():]+):(\d+):(\d+)/g;
  for (const match of message.matchAll(pattern)) {
    const absolute = match[1];
    const line = Number(match[2]);
    if (absolute === undefined || !Number.isFinite(line)) continue;
    if (absolute.includes("/node_modules/") || absolute.includes("\\node_modules\\")) continue;
    const rel = path.relative(root, absolute);
    if (rel.startsWith("..") || path.isAbsolute(rel)) continue;
    return { file: toSlash(rel), line };
  }
  return undefined;
}

/**
 * Finds the runner, from what is installed rather than from what a config file
 * suggests: a repository routinely keeps a vitest.config.ts through a migration to
 * something else.
 */
function detectRunner(root: string): { name: Runner; bin: string } | undefined {
  for (const [name, entry] of [
    ["vitest", path.join("vitest", "vitest.mjs")],
    ["jest", path.join("jest", "bin", "jest.js")],
  ] as [Runner, string][]) {
    const bin = findInNodeModules(root, entry);
    if (bin !== undefined) return { name, bin };
  }
  return undefined;
}

/**
 * Walks the node_modules chain. Not `require.resolve`: a package's `exports` map
 * decides what may be resolved by subpath, and neither runner lists its own
 * executable — so the obvious call fails on an installation that is present.
 */
function findInNodeModules(root: string, entry: string): string | undefined {
  for (let dir = path.resolve(root); ; dir = path.dirname(dir)) {
    const candidate = path.join(dir, "node_modules", entry);
    if (fs.existsSync(candidate)) return candidate;
    if (path.dirname(dir) === dir) return undefined;
  }
}

function execute(bin: string, args: string[], cwd: string): Promise<{ code: number; stdout: string; stderr: string }> {
  return new Promise((resolve) => {
    const child = spawn(process.execPath, [bin, ...args], { cwd, stdio: ["ignore", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (chunk: Buffer) => (stdout += chunk.toString()));
    child.stderr.on("data", (chunk: Buffer) => (stderr += chunk.toString()));
    child.on("error", (err) => resolve({ code: -1, stdout, stderr: String(err) }));
    child.on("close", (code) => resolve({ code: code ?? -1, stdout, stderr }));
  });
}

function relative(root: string, absolute: string): string {
  if (absolute === "") return "";
  const rel = path.relative(root, absolute);
  return rel.startsWith("..") ? toSlash(absolute) : toSlash(rel);
}

function toSlash(p: string): string {
  return p.split(path.sep).join("/");
}

function firstLine(s: string): string {
  const trimmed = s.trim();
  const i = trimmed.indexOf("\n");
  return (i >= 0 ? trimmed.slice(0, i) : trimmed).trim();
}
