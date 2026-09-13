// SPDX-License-Identifier: MIT

/**
 * A ratchet on type coverage: the proportion of identifiers whose type is not
 * `any`.
 *
 * This is the one analyzer that reports a *trend* rather than a fact about the
 * diff, and that shape is deliberate. Nobody can fix a codebase's `any` count in
 * one pull request, and a comment saying "coverage is 94%" on every change would
 * be noise within a week. What a reviewer can act on is "this change made it
 * worse", so only a decrease is reported and an increase never is.
 *
 * It measures over the program the other analyzers already built (ADR-0014). A
 * separate `type-coverage` process would type-check the repository a second time,
 * which is the single most expensive thing this tool does.
 */

import fs from "node:fs";
import path from "node:path";
import type ts from "typescript";
import { getProgram } from "../program.js";
import type { Analyzer } from "../serve.js";
import type { AnalyzeRequest, Finding } from "../protocol.js";

export const ID = "type-coverage";
export const ORDER = 60;

/** Where the ratchet's previous mark lives, in the repository under review. */
export const BASELINE_PATH = ".review/type-coverage-baseline.json";

/**
 * How much of a drop is worth a comment.
 *
 * Not zero. Adding a well-typed file changes the denominator, so an entirely
 * innocent change moves the number in the fourth decimal place; reporting that
 * would make the ratchet a random-noise generator. A tenth of a percentage point
 * is well below what one careless `any` costs in a repository of any size.
 */
const TOLERANCE = 0.001;

/** The baseline file's contents. */
export interface Baseline {
  coverage: number;
  identifiers: number;
  recordedAt: string;
}

/** What this analyzer accepts in its config block. */
interface Settings {
  /**
   * Set by `reviewer baseline --write`, never by a config file. The protocol
   * passes an analyzer's settings through untouched, which is how a command can
   * reach one analyzer without a protocol change.
   */
  writeBaseline?: boolean;
}

export const typecovAnalyzer: Analyzer = {
  id: ID,
  order: ORDER,
  lane: "deterministic",

  async unavailable(): Promise<string | undefined> {
    return undefined; // decided per request, against that repository's root
  },

  async run(req: AnalyzeRequest) {
    let measured: Measurement;
    try {
      measured = measure(req.root);
    } catch (err) {
      return { findings: [], warnings: [`${ID}: ${err instanceof Error ? err.message : String(err)}`] };
    }
    if (measured.identifiers === 0) {
      return { findings: [], warnings: [`${ID}: no identifiers to measure`] };
    }

    const settings = (req.settings ?? {}) as Settings;
    if (settings.writeBaseline === true) {
      const baseline = writeBaseline(req.root, measured);
      return {
        findings: [],
        warnings: [
          `${ID}: recorded ${percent(baseline.coverage)} over ${baseline.identifiers} identifiers in ${BASELINE_PATH}`,
        ],
      };
    }

    const previous = readBaseline(req.root);
    if (previous === undefined) {
      // Record, do not report. A repository that has never recorded a baseline
      // has nothing to have regressed from, and inventing one from this run would
      // silently bless whatever state the codebase is in today.
      return {
        findings: [],
        warnings: [
          `${ID}: ${percent(measured.coverage)} typed, and no baseline to compare against. ` +
            `Run \`reviewer baseline --write\` to start the ratchet.`,
        ],
      };
    }

    const drop = previous.coverage - measured.coverage;
    if (drop <= TOLERANCE) {
      return { findings: [], warnings: [] };
    }

    const at = attribute(req, measured);
    const findings: Finding[] = [
      {
        fingerprint: "",
        ruleId: "types/coverage-regression",
        lane: "deterministic",
        // The measurement is arithmetic; which `any` mattered is a judgement. That
        // belongs in the summary, not inline (ADR-0017).
        confidence: "medium",
        severity: "warning",
        file: at.file,
        line: at.line,
        message:
          `Type coverage fell from ${percent(previous.coverage)} to ${percent(measured.coverage)}. ` +
          (at.anys > 0
            ? `${at.anys === 1 ? "One identifier" : `${at.anys} identifiers`} this change touched here ` +
              `${at.anys === 1 ? "is" : "are"} typed \`any\`. `
            : "Something in this change is typed `any` where it was not before. ") +
          `If the drop is intended, re-record the baseline with \`reviewer baseline --write\`.`,
      },
    ];
    return { findings, warnings: [] };
  },
};

/** One measurement of a program. */
export interface Measurement {
  identifiers: number;
  anys: number;
  coverage: number;
  /**
   * Where the `any`s are, by repository-relative path, and on which lines.
   *
   * Carried because the regression has to be reported somewhere a reviewer can
   * see it. A finding against the baseline file is dropped by the core's diff
   * filter on every real pull request, since nothing in the change touches that
   * file — the analyzer would produce a finding on every run and the report would
   * show none.
   */
  anysByFile: Map<string, number[]>;
}

/**
 * Counts identifiers and the ones typed `any`.
 *
 * Declaration files are skipped: they are frequently third-party, always outside
 * this repository's control when they come from `node_modules`, and would swamp
 * the measurement. Files under node_modules are skipped for the same reason.
 */
export function measure(root: string): Measurement {
  const built = getProgram(root);
  const tsc = built.compiler.ts;
  const checker = built.program.getTypeChecker();

  let identifiers = 0;
  let anys = 0;
  const anysByFile = new Map<string, number[]>();

  for (const fileName of built.fileNames) {
    if (fileName.endsWith(".d.ts") || fileName.includes("/node_modules/")) continue;
    const source = built.program.getSourceFile(fileName);
    if (source === undefined) continue;
    const relative = toSlash(path.relative(root, fileName));

    const visit = (node: ts.Node): void => {
      if (tsc.isIdentifier(node)) {
        identifiers++;
        const type = checker.getTypeAtLocation(node);
        // The `any` flag covers both a declared `any` and one the compiler fell
        // back to. `intrinsicName` separates a real `any` from `error`, which is
        // what an unresolved import produces: counting those would make the
        // ratchet move when a dependency failed to install.
        if ((type.flags & tsc.TypeFlags.Any) !== 0 && intrinsicName(type) === "any") {
          anys++;
          const line = source.getLineAndCharacterOfPosition(node.getStart(source)).line + 1;
          const lines = anysByFile.get(relative);
          if (lines === undefined) anysByFile.set(relative, [line]);
          else lines.push(line);
        }
      }
      tsc.forEachChild(node, visit);
    };
    visit(source);
  }

  return {
    identifiers,
    anys,
    coverage: identifiers === 0 ? 0 : (identifiers - anys) / identifiers,
    anysByFile,
  };
}

/**
 * Decides where the regression is reported.
 *
 * A regression is a fact about a whole change, but a finding has to live on a
 * line that the change touched — anything else is dropped by the core's diff
 * filter (ADR-0007), and the analyzer would produce a finding on every run while
 * the report showed none. Verified: attributing this to the baseline file made it
 * invisible on every pull request that did not happen to touch that file.
 *
 * So it lands on the changed file carrying the most `any` identifiers on changed
 * lines. That is also the most useful place for it: the file the reviewer has to
 * look at anyway.
 */
function attribute(req: AnalyzeRequest, measured: Measurement): { file: string; line: number; anys: number } {
  let best: { file: string; line: number; anys: number } | undefined;

  for (const changed of req.changed) {
    const file = toSlash(changed.path);
    const lines = measured.anysByFile.get(file);
    if (lines === undefined) continue;
    const onChangedLines = lines.filter((line) =>
      changed.ranges.some(([start, end]) => line >= start && line <= end),
    );
    if (onChangedLines.length === 0) continue;
    if (best === undefined || onChangedLines.length > best.anys) {
      best = { file, line: Math.min(...onChangedLines), anys: onChangedLines.length };
    }
  }
  if (best !== undefined) return best;

  // No `any` on a changed line: the drop came from somewhere else — a deletion of
  // well-typed code, or a dependency whose types changed. Report it on the first
  // changed file so it is still visible, at its first changed line.
  const first = req.changed[0];
  if (first !== undefined && first.ranges[0] !== undefined) {
    return { file: toSlash(first.path), line: first.ranges[0][0], anys: 0 };
  }
  return { file: BASELINE_PATH, line: 1, anys: 0 };
}

function toSlash(p: string): string {
  return p.split(path.sep).join("/");
}

function intrinsicName(type: ts.Type): string {
  return (type as unknown as { intrinsicName?: string }).intrinsicName ?? "";
}

export function readBaseline(root: string): Baseline | undefined {
  try {
    const parsed = JSON.parse(fs.readFileSync(path.join(root, BASELINE_PATH), "utf8")) as Partial<Baseline>;
    if (typeof parsed.coverage !== "number" || !Number.isFinite(parsed.coverage)) return undefined;
    return {
      coverage: parsed.coverage,
      identifiers: parsed.identifiers ?? 0,
      recordedAt: parsed.recordedAt ?? "",
    };
  } catch {
    return undefined;
  }
}

export function writeBaseline(root: string, measured: Measurement): Baseline {
  const baseline: Baseline = {
    coverage: measured.coverage,
    identifiers: measured.identifiers,
    recordedAt: new Date().toISOString(),
  };
  const full = path.join(root, BASELINE_PATH);
  fs.mkdirSync(path.dirname(full), { recursive: true });
  fs.writeFileSync(full, JSON.stringify(baseline, null, 2) + "\n");
  return baseline;
}

function percent(ratio: number): string {
  return `${(ratio * 100).toFixed(1)}%`;
}
