// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { knipAnalyzer } from "./knip.js";
import type { AnalyzeRequest } from "../protocol.js";

/**
 * A repository with one live export, one dead export, one dead type and one
 * unused dependency — enough for every finding this analyzer can produce.
 *
 * `main` is deliberately absent from package.json: with it, src/index.ts is an
 * entry point and its exports can never be unused, which is a thing worth knowing
 * about Knip before trusting its answer.
 */
function fixture(): string {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "knip-"));
  write(root, "package.json", JSON.stringify({
    name: "fixture",
    version: "1.0.0",
    type: "module",
    dependencies: { "left-pad": "^1.3.0" },
  }, null, 2));
  write(root, "knip.json", JSON.stringify({ entry: ["src/entry.ts"], project: ["src/**/*.ts"] }));
  write(root, "tsconfig.json", JSON.stringify({
    compilerOptions: { target: "ES2022", module: "NodeNext", moduleResolution: "NodeNext", strict: true, noEmit: true },
    include: ["src"],
  }));
  write(root, "src/entry.ts", 'import { used } from "./index.js";\nexport const main = used();\n');
  write(root, "src/index.ts", [
    "export function used(): number {",
    "  return 1;",
    "}",
    "export function neverUsed(): string {",
    '  return "dead";',
    "}",
    "export type AlsoDead = { a: number };",
    "",
  ].join("\n"));
  // Knip resolves its own installation from the repository under review, so the
  // fixture needs one. Linked rather than installed: this is the plugin's own
  // copy, and a real destination would have its own.
  fs.mkdirSync(path.join(root, "node_modules"), { recursive: true });
  fs.symlinkSync(
    path.resolve(import.meta.dirname, "../../node_modules/knip"),
    path.join(root, "node_modules", "knip"),
  );
  return root;
}

function write(root: string, rel: string, body: string): void {
  const full = path.join(root, rel);
  fs.mkdirSync(path.dirname(full), { recursive: true });
  fs.writeFileSync(full, body);
}

function request(root: string, changed: { path: string; ranges: [number, number][] }[]): AnalyzeRequest {
  return { root, changed, contextLines: 0 };
}

const knipInstalled = fs.existsSync(path.resolve(import.meta.dirname, "../../node_modules/knip"));

test("reports a dead export the change introduced", { skip: !knipInstalled }, async () => {
  const root = fixture();
  // Lines 4-7: neverUsed and AlsoDead.
  const { findings, warnings } = await knipAnalyzer.run(
    request(root, [{ path: "src/index.ts", ranges: [[4, 7]] }]),
  );

  assert.equal(warnings?.length ?? 0, 0, `unexpected warnings: ${JSON.stringify(warnings)}`);
  const names = findings.map((f) => `${f.ruleId} ${f.file}:${f.line}`).sort();
  assert.deepEqual(names, ["dead/unused-export src/index.ts:4", "dead/unused-export src/index.ts:7"]);
  assert.equal(findings[0]?.confidence, "medium");
  assert.match(findings[0]?.message ?? "", /nothing imports it/);
});

test("pre-existing dead code in a touched file is not reported", { skip: !knipInstalled }, async () => {
  const root = fixture();
  // The change touched only the live function, two lines above the dead one.
  const { findings } = await knipAnalyzer.run(
    request(root, [{ path: "src/index.ts", ranges: [[1, 3]] }]),
  );

  assert.deepEqual(findings, [], "dead code the change did not touch is not this change's problem");
});

test("the unchanged fixture yields nothing at all", { skip: !knipInstalled }, async () => {
  const root = fixture();
  const { findings } = await knipAnalyzer.run(request(root, []));
  assert.deepEqual(findings, []);
});

test("an unused dependency is reported on the line that declares it", { skip: !knipInstalled }, async () => {
  const root = fixture();
  const body = fs.readFileSync(path.join(root, "package.json"), "utf8").split("\n");
  const line = body.findIndex((l) => l.includes('"left-pad"')) + 1;
  assert.ok(line > 1, "the fixture should declare left-pad below line 1");

  const { findings } = await knipAnalyzer.run(
    request(root, [{ path: "package.json", ranges: [[line, line]] }]),
  );

  // Reported at the declaration, not at line 1. At line 1 the core's diff filter
  // would drop every one of these, and the analyzer would look like it worked.
  const unused = findings.filter((f) => f.ruleId === "deps/unused-dependency");
  assert.equal(unused.length, 1, JSON.stringify(findings));
  assert.equal(unused[0]?.line, line);
  assert.match(unused[0]?.message ?? "", /left-pad/);
});

test("declines without a Knip config rather than guessing entry points", async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "knip-noconfig-"));
  write(root, "package.json", '{"name":"x","version":"1.0.0"}');

  const reason = await knipAnalyzer.unavailable?.(root);
  assert.match(reason ?? "", /no Knip config/);
});

test("declines when knip is not a dependency of the repository", async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "knip-nodep-"));
  write(root, "package.json", '{"name":"x","version":"1.0.0"}');
  write(root, "knip.json", "{}");

  const reason = await knipAnalyzer.unavailable?.(root);
  assert.match(reason ?? "", /not a dependency/);
});

test("output that is not a report is a warning, never silence", async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "knip-broken-"));
  write(root, "package.json", '{"name":"x","version":"1.0.0"}');
  write(root, "knip.json", "{}");
  // A "knip" that is installed but produces nothing usable.
  write(root, "node_modules/knip/package.json", '{"name":"knip","version":"0.0.0","bin":{"knip":"bin.js"}}');
  write(root, "node_modules/knip/bin.js", 'process.stderr.write("boom\\n");process.exit(3);\n');

  const { findings, warnings } = await knipAnalyzer.run(request(root, []));
  assert.deepEqual(findings, []);
  assert.match(warnings?.[0] ?? "", /without a report/);
});

test("an undeclared import is reported at the import, not at line 1", { skip: !knipInstalled }, async () => {
  const root = fixture();
  write(root, "src/entry.ts", [
    'import { used } from "./index.js";',
    'import leftPad from "left-pad";',
    "export const main = leftPad(String(used()), 3);",
    "",
  ].join("\n"));
  // package.json declares left-pad, so make it undeclared.
  write(root, "package.json", JSON.stringify({ name: "fixture", version: "1.0.0", type: "module" }, null, 2));

  const { findings } = await knipAnalyzer.run(
    request(root, [{ path: "src/entry.ts", ranges: [[2, 2]] }]),
  );

  const unlisted = findings.filter((f) => f.ruleId === "deps/undeclared-dependency");
  assert.equal(unlisted.length, 1, JSON.stringify(findings));
  // At line 1 the core's diff filter drops it on every real pull request, and the
  // analyzer looks like it works while reporting nothing.
  assert.equal(unlisted[0]?.line, 2);
  assert.equal(unlisted[0]?.file, "src/entry.ts");
  assert.equal(unlisted[0]?.confidence, "high");
});

test("an unused dependency uses Knip's own line, not the first name match", { skip: !knipInstalled }, async () => {
  const root = fixture();
  // `left-pad` appears in overrides first. Searching the file for the name finds
  // line 6; the declaration the change touched is further down.
  write(root, "package.json", [
    "{",
    '  "name": "fixture",',
    '  "version": "1.0.0",',
    '  "type": "module",',
    '  "overrides": {',
    '    "left-pad": "1.3.0"',
    "  },",
    '  "dependencies": {',
    '    "left-pad": "^1.3.0"',
    "  }",
    "}",
    "",
  ].join("\n"));

  const { findings } = await knipAnalyzer.run(
    request(root, [{ path: "package.json", ranges: [[9, 9]] }]),
  );

  const unused = findings.filter((f) => f.ruleId === "deps/unused-dependency");
  assert.equal(unused.length, 1, JSON.stringify(findings));
  assert.equal(unused[0]?.line, 9, "the declaration, not the overrides entry above it");
});
