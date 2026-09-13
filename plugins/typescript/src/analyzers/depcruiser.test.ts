// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import type { AnalyzeRequest } from "../protocol.js";
import { depCruiserAnalyzer } from "./depcruiser.js";

function repo(files: Record<string, string>): string {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "reviewer-depcruise-"));
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

/** The canonical rule: the domain layer may not reach into infrastructure. */
const layerRules = JSON.stringify({
  forbidden: [
    {
      name: "no-domain-to-infra",
      severity: "error",
      comment: "domain must not depend on infra",
      from: { path: "^src/domain" },
      to: { path: "^src/infra" },
    },
  ],
  options: { doNotFollow: { path: "node_modules" } },
});

test("reports a boundary violation on the import that creates it", async () => {
  const root = repo({
    ".dependency-cruiser.json": layerRules,
    "src/domain/order.ts": `// a comment first, so line 1 is not the answer by accident
import { send } from "../infra/http.js";

export function submit(): void {
  send();
}
`,
    "src/infra/http.ts": "export function send(): void {}\n",
  });

  const { findings } = await depCruiserAnalyzer.run(request(root, "src/domain/order.ts"));
  assert.equal(findings.length, 1, JSON.stringify(findings));
  const [f] = findings;
  assert.equal(f?.ruleId, "arch/no-domain-to-infra");
  assert.equal(f?.file, "src/domain/order.ts");
  // The whole point: the comment lands on the import, not at the top of the file.
  assert.equal(f?.line, 2);
  assert.equal(f?.severity, "error");
  assert.equal(f?.confidence, "high");
  assert.match(f?.message ?? "", /must not depend on/);
});

test("a repository that respects its own rules reports nothing", async () => {
  const root = repo({
    ".dependency-cruiser.json": layerRules,
    "src/domain/order.ts": "export const total = 1;\n",
    "src/infra/http.ts": "export function send(): void {}\n",
  });
  const { findings } = await depCruiserAnalyzer.run(request(root, "src/domain/order.ts"));
  assert.deepEqual(findings, []);
});

test("no config means the analyzer declines rather than inventing an architecture", async () => {
  const root = repo({ "src/a.ts": "export const a = 1;\n" });
  const reason = await depCruiserAnalyzer.unavailable?.(root);
  assert.match(reason ?? "", /no \.dependency-cruiser config/);

  const { findings, warnings } = await depCruiserAnalyzer.run(request(root, "src/a.ts"));
  assert.deepEqual(findings, []);
  assert.match(warnings?.join("\n") ?? "", /no \.dependency-cruiser config/);
});

test("an unreadable config is a warning, not a failed run", async () => {
  const root = repo({ ".dependency-cruiser.json": "{ not json", "src/a.ts": "export const a = 1;\n" });
  const { findings, warnings } = await depCruiserAnalyzer.run(request(root, "src/a.ts"));
  assert.deepEqual(findings, []);
  assert.match(warnings?.join("\n") ?? "", /could not read/);
});

test("an empty diff does not invoke the cruiser", async () => {
  const root = repo({ ".dependency-cruiser.json": layerRules });
  const { findings } = await depCruiserAnalyzer.run({ root, changed: [] });
  assert.deepEqual(findings, []);
});

test("a cycle is described as a path, not as a bare rule name", async () => {
  const root = repo({
    ".dependency-cruiser.json": JSON.stringify({
      forbidden: [{ name: "no-cycles", severity: "error", from: {}, to: { circular: true } }],
      options: { doNotFollow: { path: "node_modules" } },
    }),
    "src/a.ts": 'import { b } from "./b.js";\nexport const a = b;\n',
    "src/b.ts": 'import { a } from "./a.js";\nexport const b = a;\n',
  });

  const { findings } = await depCruiserAnalyzer.run(request(root, "src/a.ts"));
  assert.ok(findings.length > 0, "the cycle must be reported");
  assert.match(findings[0]?.message ?? "", /cycle/i);
  assert.match(findings[0]?.message ?? "", /→/, "a cycle is only actionable if the path is shown");
});
