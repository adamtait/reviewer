// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { changedTestsAnalyzer } from "./changedtests.js";
import type { AnalyzeRequest } from "../protocol.js";

const PLUGIN_MODULES = path.resolve(import.meta.dirname, "../../node_modules");
const vitestInstalled = fs.existsSync(path.join(PLUGIN_MODULES, "vitest"));

/**
 * A git repository whose committed state is green, plus one uncommitted edit that
 * breaks it.
 *
 * Both runners select tests by comparing against git, so the fixture has to be
 * committed clean and then edited: a repository where everything is committed has
 * nothing changed, and neither runner would select a single test.
 */
function fixture(breaks: "the test" | "the source" | "nothing"): string {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "changedtests-"));
  write(root, "package.json", '{"name":"fixture","version":"1.0.0","type":"module"}');
  write(root, "vitest.config.ts", 'import { defineConfig } from "vitest/config";\nexport default defineConfig({ test: { includeTaskLocation: true } });\n');
  write(root, "src/order.ts", "export function total(amounts: number[]): number {\n  return amounts.reduce((a, b) => a + b, 0);\n}\n");
  write(root, "src/order.test.ts", TEST_FILE.join("\n"));
  fs.symlinkSync(PLUGIN_MODULES, path.join(root, "node_modules"));

  // `--changed` needs a git repository to compare against.
  const git = (...args: string[]): void => {
    execFileSync("git", args, {
      cwd: root,
      env: {
        ...process.env,
        GIT_AUTHOR_NAME: "fixture", GIT_AUTHOR_EMAIL: "f@example.invalid",
        GIT_COMMITTER_NAME: "fixture", GIT_COMMITTER_EMAIL: "f@example.invalid",
        GIT_CONFIG_GLOBAL: "/dev/null", GIT_CONFIG_SYSTEM: "/dev/null",
      },
      stdio: "ignore",
    });
  };
  write(root, ".gitignore", "node_modules\n");
  git("init", "-q", "-b", "main");
  git("add", "-A");
  git("commit", "-q", "-m", "base");

  // The uncommitted change under review.
  if (breaks === "nothing") {
    // Committed and clean: nothing for either runner to select.
  } else if (breaks === "the test") {
    write(root, "src/order.test.ts", TEST_FILE.map((l) => l.replace("toBe(3)", "toBe(99)")).join("\n"));
  } else {
    write(root, "src/order.ts", "export function total(amounts: number[]): number {\n  return amounts.length;\n}\n");
  }
  return root;
}

/** Green as committed. Line 9 is the assertion the tests below expect to fail. */
const TEST_FILE = [
  'import { expect, test } from "vitest";',
  'import { total } from "./order.js";',
  "",
  'test("adds up", () => {',
  "  expect(total([1, 2])).toBe(3);",
  "});",
  "",
  'test("is wrong on purpose", () => {',
  "  expect(total([1, 2])).toBe(3);",
  "});",
  "",
];

function write(root: string, rel: string, body: string): void {
  const full = path.join(root, rel);
  fs.mkdirSync(path.dirname(full), { recursive: true });
  fs.writeFileSync(full, body);
}

function request(root: string, changed: { path: string; ranges: [number, number][] }[]): AnalyzeRequest {
  return { root, changed, contextLines: 0 };
}

test("a failing assertion is reported at its own line", { skip: !vitestInstalled }, async () => {
  const root = fixture("the test");
  // The change touched the test file, so the comment can land on the assertion.
  const { findings, warnings } = await changedTestsAnalyzer.run(
    request(root, [{ path: "src/order.test.ts", ranges: [[1, 11]] }]),
  );

  assert.equal(warnings?.length ?? 0, 0, JSON.stringify(warnings));
  assert.ok(findings.length >= 1, JSON.stringify(findings, null, 1));
  const f = findings.find((x) => x.line === 9) ?? findings[0];
  assert.equal(f?.ruleId, "tests/failing");
  assert.equal(f?.confidence, "high");
  assert.equal(f?.severity, "error");
  assert.equal(f?.file, "src/order.test.ts");
  // Line 9 is the assertion, not line 8 where the test is declared. The stack
  // frame carries it; the runner's `location` field points at the declaration.
  assert.equal(f?.line, 9);
  assert.match(f?.message ?? "", /fails/);
  assert.match(f?.message ?? "", /expected 3 to be 99/);
});

test("a failure in an untouched test file still lands in the diff", { skip: !vitestInstalled }, async () => {
  const root = fixture("the source");
  // The realistic case: the change is in the source, the failure is in a test
  // file the pull request never opened. Reported at the assertion, the core's
  // diff filter would drop it and the analyzer would appear to find nothing.
  const { findings } = await changedTestsAnalyzer.run(
    request(root, [{ path: "src/order.ts", ranges: [[2, 2]] }]),
  );

  assert.ok(findings.length >= 1, JSON.stringify(findings, null, 1));
  for (const f of findings) {
    assert.equal(f.file, "src/order.ts", "reported on a file the change touched");
    assert.equal(f.line, 2);
    // The assertion's real location is not lost, only moved into the message.
    assert.match(f.message, /^src\/order\.test\.ts:\d+ — /);
  }
});

test("nothing changed means the runner is not spawned", { skip: !vitestInstalled }, async () => {
  const root = fixture("the test");
  const { findings, warnings } = await changedTestsAnalyzer.run(request(root, []));
  // Spawning with nothing selected runs the entire suite, which is minutes of a
  // reviewer's time for a question nobody asked.
  assert.deepEqual(findings, []);
  assert.deepEqual(warnings, []);
});

test("declines when neither runner is installed", async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "changedtests-none-"));
  write(root, "package.json", '{"name":"x","version":"1.0.0"}');

  const reason = await changedTestsAnalyzer.unavailable?.(root);
  assert.match(reason ?? "", /neither vitest nor jest/);
});

test("a runner that produces no report is a warning, never silence", async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "changedtests-broken-"));
  write(root, "package.json", '{"name":"x","version":"1.0.0"}');
  write(root, "node_modules/vitest/vitest.mjs", 'process.stderr.write("boom\\n");process.exit(1);\n');

  const { findings, warnings } = await changedTestsAnalyzer.run(
    request(root, [{ path: "src/a.ts", ranges: [[1, 1]] }]),
  );
  // A failing test and a runner that would not start are both non-zero exits, so
  // only the report tells them apart.
  assert.deepEqual(findings, []);
  assert.match(warnings?.[0] ?? "", /without a report/);
  assert.match(warnings?.[0] ?? "", /boom/);
});

test("a base ref this checkout cannot resolve is a warning, never a clean bill", { skip: !vitestInstalled }, async () => {
  const root = fixture("the test");

  const { findings, warnings } = await changedTestsAnalyzer.run({
    root,
    changed: [{ path: "src/order.test.ts", ranges: [[1, 11]] }],
    contextLines: 0,
    base: "origin/does-not-exist",
  });

  // Both runners accept an unresolvable ref, select nothing, exit 0 and report
  // that every test passed. Reported as clean, that is a silent all-green for a
  // run that executed nothing.
  assert.deepEqual(findings, []);
  assert.match(warnings?.[0] ?? "", /no ref `origin\/does-not-exist`/);
});

test("a selection that picks nothing is stated rather than read as passing", { skip: !vitestInstalled }, async () => {
  const root = fixture("nothing");
  // A changed file the runner will not associate with any test.
  write(root, "docs/notes.md", "nothing to run here\n");

  const { findings, warnings } = await changedTestsAnalyzer.run({
    root,
    changed: [{ path: "docs/notes.md", ranges: [[1, 1]] }],
    contextLines: 0,
    base: "HEAD",
  });

  assert.deepEqual(findings, []);
  assert.match(warnings?.[0] ?? "", /selected no tests/);
});

test("a relocated failure prefers a source file over a manifest", { skip: !vitestInstalled }, async () => {
  const root = fixture("the source");

  const { findings } = await changedTestsAnalyzer.run({
    root,
    // Git's path order puts the config first, which is exactly the trap.
    changed: [
      { path: ".review/config.yaml", ranges: [[1, 1]] },
      { path: "src/order.ts", ranges: [[2, 2]] },
    ],
    contextLines: 0,
  });

  assert.ok(findings.length >= 1, JSON.stringify(findings, null, 1));
  for (const f of findings) {
    assert.equal(f.file, "src/order.ts", "a test failure on line 1 of a YAML file helps nobody");
    assert.equal(f.line, 2);
  }
});
