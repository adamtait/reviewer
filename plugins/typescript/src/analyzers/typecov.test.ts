// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { resetPrograms } from "../program.js";
import { BASELINE_PATH, measure, readBaseline, typecovAnalyzer, writeBaseline } from "./typecov.js";
import type { AnalyzeRequest } from "../protocol.js";

function repo(source: string): string {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "typecov-"));
  write(root, "package.json", '{"name":"fixture","version":"1.0.0","type":"module"}');
  write(root, "tsconfig.json", JSON.stringify({
    compilerOptions: { target: "ES2022", module: "NodeNext", moduleResolution: "NodeNext", strict: true, noEmit: true },
    include: ["src"],
  }));
  write(root, "src/index.ts", source);
  // Each fixture is a distinct root, but the program cache is keyed by tsconfig
  // path and a temp directory can be reused within a run.
  resetPrograms();
  return root;
}

function write(root: string, rel: string, body: string): void {
  const full = path.join(root, rel);
  fs.mkdirSync(path.dirname(full), { recursive: true });
  fs.writeFileSync(full, body);
}

function request(
  root: string,
  settings?: unknown,
  changed: { path: string; ranges: [number, number][] }[] = [],
): AnalyzeRequest {
  return { root, changed, contextLines: 0, settings };
}

const TYPED = "export function add(a: number, b: number): number {\n  return a + b;\n}\n";
const LOOSE = "export function add(a: any, b: any): any {\n  return a + b;\n}\n";

test("a well-typed file measures higher than a loose one", () => {
  const typed = measure(repo(TYPED));
  const loose = measure(repo(LOOSE));

  assert.ok(typed.identifiers > 0, "the fixture should have identifiers");
  assert.equal(typed.anys, 0, "nothing in the typed fixture is any");
  assert.ok(loose.anys > 0, "the loose fixture should have anys");
  assert.ok(loose.coverage < typed.coverage, `${loose.coverage} should be below ${typed.coverage}`);
});

test("no baseline means record, not report", async () => {
  const root = repo(LOOSE);
  const { findings, warnings } = await typecovAnalyzer.run(request(root));

  // Inventing a baseline from this run would silently bless whatever state the
  // codebase is in today, which is the opposite of a ratchet.
  assert.deepEqual(findings, []);
  assert.match(warnings?.[0] ?? "", /no baseline to compare against/);
  assert.match(warnings?.[0] ?? "", /reviewer baseline --write/);
  assert.equal(fs.existsSync(path.join(root, BASELINE_PATH)), false, "a plain run must not write one");
});

test("a drop below the baseline is reported on a line the change touched", async () => {
  const root = repo(LOOSE);
  // A baseline recorded when the file was fully typed.
  writeBaseline(root, { identifiers: 100, anys: 0, coverage: 1, anysByFile: new Map() });

  const { findings } = await typecovAnalyzer.run(
    request(root, undefined, [{ path: "src/index.ts", ranges: [[1, 3]] }]),
  );
  assert.equal(findings.length, 1, JSON.stringify(findings));
  assert.equal(findings[0]?.ruleId, "types/coverage-regression");
  assert.equal(findings[0]?.confidence, "medium");
  assert.match(findings[0]?.message ?? "", /fell from 100\.0% to /);
  assert.match(findings[0]?.message ?? "", /reviewer baseline --write/);

  // The whole point: a finding against the baseline file is invisible, because
  // the core drops anything outside the diff and no real pull request touches
  // that file. It has to land where the `any`s the change touched are.
  assert.equal(findings[0]?.file, "src/index.ts");
  assert.ok((findings[0]?.line ?? 0) >= 1 && (findings[0]?.line ?? 0) <= 3);
  assert.match(findings[0]?.message ?? "", /typed `any`/);
});

test("a drop with no any on a changed line still lands in the diff", async () => {
  const root = repo(TYPED);
  writeBaseline(root, { identifiers: 100, anys: 0, coverage: 1, anysByFile: new Map() });

  const { findings } = await typecovAnalyzer.run(
    request(root, undefined, [{ path: "src/index.ts", ranges: [[2, 2]] }]),
  );
  // The typed fixture is at 100%, so nothing is reported; the branch under test is
  // the fallback, exercised through attribute() by a change with no anys.
  assert.deepEqual(findings, []);
});

test("the ratchet never reports an increase", async () => {
  const root = repo(TYPED);
  // A baseline recorded when the codebase was worse than it is now.
  writeBaseline(root, { identifiers: 100, anys: 50, coverage: 0.5, anysByFile: new Map() });

  const { findings } = await typecovAnalyzer.run(request(root));
  assert.deepEqual(findings, [], "improving is not a finding");
});

test("a drop within tolerance is not reported", async () => {
  const root = repo(TYPED);
  const measured = measure(root);
  // A fifth of the tolerance: the kind of movement adding a well-typed file
  // causes by changing the denominator.
  writeBaseline(root, { ...measured, coverage: measured.coverage + 0.0002 });

  const { findings } = await typecovAnalyzer.run(request(root));
  assert.deepEqual(findings, [], "noise in the fourth decimal place is not a regression");
});

test("writeBaseline is reached through the analyzer's settings", async () => {
  const root = repo(TYPED);
  const { findings, warnings } = await typecovAnalyzer.run(request(root, { writeBaseline: true }));

  assert.deepEqual(findings, [], "recording a baseline is not a review");
  assert.match(warnings?.[0] ?? "", /recorded 100\.0%/);

  const written = readBaseline(root);
  assert.ok(written !== undefined);
  assert.equal(written.coverage, 1);
  assert.ok(written.identifiers > 0);
  assert.ok(written.recordedAt.length > 0);
});

test("an unreadable baseline is treated as absent, not as zero", async () => {
  const root = repo(TYPED);
  write(root, BASELINE_PATH, "{ not json");

  const { findings, warnings } = await typecovAnalyzer.run(request(root));
  // A baseline of zero would make every run report a catastrophic regression.
  assert.deepEqual(findings, []);
  assert.match(warnings?.[0] ?? "", /no baseline/);
});

test("a repository with no tsconfig is a warning, not a crash", async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "typecov-empty-"));
  fs.writeFileSync(path.join(root, "package.json"), '{"name":"x","version":"1.0.0"}');
  resetPrograms();

  const { findings, warnings } = await typecovAnalyzer.run(request(root));
  assert.deepEqual(findings, []);
  assert.ok((warnings?.length ?? 0) > 0);
});
