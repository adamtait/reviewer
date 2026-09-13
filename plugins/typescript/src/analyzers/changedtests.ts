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

import { spawn } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import type { Analyzer } from "../serve.js";
import type { AnalyzeRequest, Finding } from "../protocol.js";

export const ID = "changed-tests";
export const ORDER = 70;

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

    const outputFile = path.join(fs.mkdtempSync(path.join(os.tmpdir(), "reviewer-tests-")), "report.json");
    try {
      const result = await execute(runner.bin, argsFor(runner.name, req.base, outputFile), req.root);

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

      return { findings: toFindings(req, report), warnings: [] };
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
function argsFor(runner: Runner, base: string | undefined, outputFile: string): string[] {
  const ref = (base ?? "").trim();
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
    const first = req.changed[0];
    const firstRange = first?.ranges[0];
    if (first !== undefined && firstRange !== undefined) {
      file = toSlash(first.path);
      line = firstRange[0];
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
