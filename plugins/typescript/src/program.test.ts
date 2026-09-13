// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { buildCount, findConfig, getDiagnostics, getProgram, loadCompiler, resetPrograms } from "./program.js";

/** A throwaway repository with a tsconfig and the given files. */
function repo(files: Record<string, string>): string {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "reviewer-program-"));
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

test("the program is built once and shared", () => {
  resetPrograms();
  const root = repo({
    "tsconfig.json": tsconfig,
    "src/a.ts": "export const a: number = 1;\n",
  });

  const before = buildCount();
  const first = getProgram(root);
  assert.equal(buildCount(), before + 1, "the first call must build");

  // This is the claim the whole Go-core decision rests on: three analyzers
  // asking for the program must cost one build, not three.
  const second = getProgram(root);
  const third = getProgram(root);
  assert.equal(buildCount(), before + 1, "later calls must reuse the program");
  assert.equal(second.program, first.program);
  assert.equal(third.program, first.program);
});

test("diagnostics are computed once too", () => {
  resetPrograms();
  const root = repo({
    "tsconfig.json": tsconfig,
    "src/bad.ts": 'export const n: number = "not a number";\n',
  });

  const first = getDiagnostics(root);
  const builds = buildCount();
  const second = getDiagnostics(root);

  assert.equal(buildCount(), builds, "a second diagnostics request must not rebuild");
  assert.equal(first.diagnostics, second.diagnostics, "the same diagnostics array must be returned");
  assert.ok(first.diagnostics.length > 0, "the fixture has a type error");
});

test("a repository with no tsconfig is a clear error, not a crash", () => {
  resetPrograms();
  const root = repo({ "src/a.ts": "export const a = 1;\n" });
  assert.throws(() => getProgram(root), /no tsconfig\.json found/);
});

test("an invalid tsconfig is reported with the compiler's own message", () => {
  resetPrograms();
  const root = repo({ "tsconfig.json": "{ this is not json }", "src/a.ts": "export const a = 1;\n" });
  assert.throws(() => getProgram(root), /tsconfig\.json could not be read|tsconfig\.json is invalid/);
});

test("findConfig locates a tsconfig above the given directory", () => {
  const root = repo({ "tsconfig.json": tsconfig, "src/a.ts": "export const a = 1;\n" });
  const compiler = loadCompiler(root);
  const found = findConfig(root, compiler);
  assert.ok(found !== undefined);
  assert.equal(path.basename(found), "tsconfig.json");
});

test("the compiler is reported as a fallback when the repository has none", () => {
  const root = repo({ "tsconfig.json": tsconfig, "src/a.ts": "export const a = 1;\n" });
  const compiler = loadCompiler(root);
  // The throwaway repository has no node_modules, so the plugin's own copy is
  // used — and says so, because analysing with a different compiler than the
  // author runs produces findings they cannot reproduce.
  assert.equal(compiler.fallback, true);
  assert.match(compiler.from, /typescript/);
});
