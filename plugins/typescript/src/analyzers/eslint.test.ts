// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import type { AnalyzeRequest } from "../protocol.js";
import { confidenceFor, eslintAnalyzer, ruleIDFor } from "./eslint.js";

function repo(files: Record<string, string>): string {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "reviewer-eslint-"));
  for (const [name, body] of Object.entries(files)) {
    const target = path.join(root, name);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, body);
  }
  return root;
}

function request(root: string, ...changed: string[]): AnalyzeRequest {
  return { root, changed: changed.map((p) => ({ path: p, ranges: [[1, 500]] as [number, number][] })) };
}

/** A flat config with the type-aware rule this analyzer exists for. */
const flatConfig = `
import tseslint from ${JSON.stringify(path.join(process.cwd(), "node_modules", "typescript-eslint", "dist", "index.js"))};

export default tseslint.config({
  files: ["**/*.ts"],
  languageOptions: {
    parserOptions: { projectService: false, project: "./tsconfig.json" },
  },
  extends: [...tseslint.configs.recommendedTypeChecked],
  rules: { "@typescript-eslint/no-floating-promises": "error" },
});
`;

const tsconfig = JSON.stringify({
  compilerOptions: { target: "ES2022", module: "ESNext", moduleResolution: "bundler", strict: true, noEmit: true },
  include: ["src"],
});

test("ruleIDFor separates type-aware rules from stylistic ones", () => {
  assert.equal(ruleIDFor("@typescript-eslint/no-floating-promises"), "logic/no-floating-promises");
  assert.equal(ruleIDFor("no-console"), "lint/no-console");
});

test("confidence is high only for type-aware rules", () => {
  // Type-aware rules are computed from the type graph, not a heuristic, so they
  // are allowed inline. Stylistic rules are capped at medium (ADR-0017).
  assert.equal(confidenceFor("@typescript-eslint/no-floating-promises"), "high");
  assert.equal(confidenceFor("semi"), "medium");
});

test("no eslint in the repository is reported, not fatal", async () => {
  const root = repo({ "src/a.ts": "export const a = 1;\n" });
  const reason = await eslintAnalyzer.unavailable?.(root);
  // The plugin's own eslint is resolvable, so the missing piece is the config.
  assert.match(reason ?? "", /flat config|not installed/);
});

test("no flat config means the analyzer declines rather than inventing rules", async () => {
  const root = repo({ "tsconfig.json": tsconfig, "src/a.ts": "export const a = 1;\n" });
  const reason = await eslintAnalyzer.unavailable?.(root);
  assert.match(reason ?? "", /flat config/);

  const { findings, warnings } = await eslintAnalyzer.run(request(root, "src/a.ts"));
  assert.deepEqual(findings, []);
  assert.match(warnings?.join("\n") ?? "", /flat config/);
});

test("finds a floating promise in a changed file", async () => {
  const root = repo({
    "tsconfig.json": tsconfig,
    "eslint.config.mjs": flatConfig,
    "src/http.ts": `
export async function send(): Promise<void> {}

export function fireAndForget(): void {
  send();
}
`,
  });

  const { findings } = await eslintAnalyzer.run(request(root, "src/http.ts"));
  const floating = findings.filter((f) => f.ruleId === "logic/no-floating-promises");
  assert.equal(floating.length, 1, `want one floating-promise finding, got ${JSON.stringify(findings)}`);
  assert.equal(floating[0]?.confidence, "high");
  assert.equal(floating[0]?.file, "src/http.ts");
});

test("only changed files are linted", async () => {
  const root = repo({
    "tsconfig.json": tsconfig,
    "eslint.config.mjs": flatConfig,
    "src/touched.ts": "export const ok = 1;\n",
    "src/untouched.ts": `
export async function send(): Promise<void> {}
export function bad(): void { send(); }
`,
  });

  const { findings } = await eslintAnalyzer.run(request(root, "src/touched.ts"));
  assert.deepEqual(
    findings.filter((f) => f.file === "src/untouched.ts"),
    [],
    "an untouched file must not be linted (ADR-0007)",
  );
});

test("a config that throws is a warning, not a failed run", async () => {
  const root = repo({
    "tsconfig.json": tsconfig,
    "eslint.config.mjs": "throw new Error('this config is broken');\n",
    "src/a.ts": "export const a = 1;\n",
  });

  const { findings, warnings } = await eslintAnalyzer.run(request(root, "src/a.ts"));
  assert.deepEqual(findings, []);
  assert.ok((warnings?.length ?? 0) > 0, "the failure must be reported");
});

test("non-lintable changed files are skipped without invoking eslint", async () => {
  const root = repo({ "tsconfig.json": tsconfig, "eslint.config.mjs": flatConfig, "README.md": "# hi\n" });
  const { findings } = await eslintAnalyzer.run(request(root, "README.md"));
  assert.deepEqual(findings, []);
});
