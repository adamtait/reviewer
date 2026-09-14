// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { resetPrograms } from "../program.js";
import type { AnalyzeRequest } from "../protocol.js";
import { tscAnalyzer } from "./tsc.js";

function repo(files: Record<string, string>): string {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "reviewer-tsc-"));
  for (const [name, body] of Object.entries(files)) {
    const target = path.join(root, name);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, body);
  }
  return root;
}

const tsconfig = JSON.stringify({
  compilerOptions: { target: "ES2022", module: "ESNext", moduleResolution: "bundler", strict: true, noEmit: true },
  include: ["src"],
});

function request(root: string, ...changed: string[]): AnalyzeRequest {
  return { root, changed: changed.map((p) => ({ path: p, ranges: [[1, 200]] as [number, number][] })) };
}

test("reports a type error on a changed line", async () => {
  resetPrograms();
  const root = repo({
    "tsconfig.json": tsconfig,
    "src/order.ts": 'export const total: number = "twelve";\n',
  });

  const { findings } = await tscAnalyzer.run(request(root, "src/order.ts"));
  assert.equal(findings.length, 1);
  const [f] = findings;
  assert.equal(f?.file, "src/order.ts");
  assert.equal(f?.line, 1);
  assert.equal(f?.severity, "error");
  assert.equal(f?.confidence, "high", "the compiler is not guessing");
  assert.match(f?.ruleId ?? "", /^types\/TS\d+$/);
  assert.match(f?.message ?? "", /not assignable/);
});

test("a type error in an untouched file is not this pull request's doing", async () => {
  resetPrograms();
  const root = repo({
    "tsconfig.json": tsconfig,
    "src/touched.ts": "export const ok = 1;\n",
    "src/untouched.ts": 'export const bad: number = "no";\n',
  });

  const { findings } = await tscAnalyzer.run(request(root, "src/touched.ts"));
  assert.deepEqual(findings, [], "only changed files may be reported (ADR-0007)");
});

test("a repository with no tsconfig warns rather than failing", async () => {
  resetPrograms();
  const root = repo({ "src/a.ts": "export const a = 1;\n" });

  const { findings, warnings } = await tscAnalyzer.run(request(root, "src/a.ts"));
  assert.deepEqual(findings, []);
  assert.match(warnings?.join("\n") ?? "", /no tsconfig\.json found/);
});

test("using the plugin's own compiler is reported", async () => {
  resetPrograms();
  const root = repo({ "tsconfig.json": tsconfig, "src/a.ts": "export const a = 1;\n" });

  const { warnings } = await tscAnalyzer.run(request(root, "src/a.ts"));
  assert.match(warnings?.join("\n") ?? "", /plugin's own typescript/);
});

test("a clean repository reports nothing", async () => {
  resetPrograms();
  const root = repo({ "tsconfig.json": tsconfig, "src/a.ts": "export const a: number = 1;\n" });

  const { findings } = await tscAnalyzer.run(request(root, "src/a.ts"));
  assert.deepEqual(findings, []);
});
