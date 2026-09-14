// SPDX-License-Identifier: MIT

/**
 * The plugin protocol, version 1, as the TypeScript side sees it.
 *
 * This mirrors `pkg/plugin` in the Go core and `schema/plugin-protocol.schema.json`.
 * Keeping a hand-written mirror rather than generating it is deliberate: the
 * protocol is eleven fields, and a generator plus its drift check is more moving
 * parts than the drift it would prevent. The schema is the contract; this file
 * and the Go structs are two readings of it, and the conformance test in the Go
 * repository drives this plugin through the real codec on every CI run.
 */

/** Which half of the system an analyzer belongs to. Declared, never inferred. */
export type Lane = "deterministic" | "llm";

export type Confidence = "high" | "medium" | "low";
export type Severity = "error" | "warning" | "info";

/** One reported observation. Mirrors `finding.Finding`. */
export interface Finding {
  /** Filled in by the core; a plugin leaves it empty. */
  fingerprint: string;
  ruleId: string;
  lane: Lane;
  confidence: Confidence;
  severity: Severity;
  /** Relative to the repository root, forward slashes. */
  file: string;
  /** 1-indexed. */
  line: number;
  /** 1-indexed, inclusive. Omitted for a single-line finding. */
  endLine?: number;
  message: string;
  suggestion?: string;
  /** Required when lane is "llm". */
  evidence?: string;
}

/** What a plugin says about one analyzer it provides. */
export interface Descriptor {
  id: string;
  lane: Lane;
  /** Lower runs earlier. The core leaves gaps so plugins can interleave. */
  order: number;
  /** False when the analyzer cannot run in this repository. */
  available: boolean;
  /** One line explaining `available: false`. */
  unavailable?: string;
}

/** A changed file and the line ranges the diff touched, as inclusive pairs. */
export interface ChangedFile {
  path: string;
  /** added, modified or renamed. */
  status?: string;
  ranges: [number, number][];
}

export interface AnalyzeRequest {
  /** Absolute path of the repository under review. */
  root: string;
  changed: ChangedFile[];
  /** Monorepo workspaces in scope. Empty means all. */
  projects?: string[];
  /** The ref the diff was taken against; absent for a staged diff. */
  base?: string;
  /** What the deterministic lane already found. Set only for model-lane analyzers. */
  prior?: Finding[];
  /** The change is the index rather than a branch; previous contents are HEAD's. */
  staged?: boolean;
  contextLines?: number;
  /** This analyzer's config block, passed through untouched. */
  settings?: unknown;
}

export type FrameType = "hello" | "describe" | "analyze" | "findings" | "error" | "bye";

/** One line on the wire. A flat envelope, as in the Go implementation. */
export interface Frame {
  type: FrameType;
  protocol?: number;
  host?: string;
  plugin?: string;
  version?: string;
  analyzers?: Descriptor[];
  analyzer?: string;
  request?: AnalyzeRequest;
  findings?: Finding[];
  warnings?: string[];
  message?: string;
}

/** The wire version this module implements. */
export const PROTOCOL = 1;

/** Narrows an unknown parsed value to a Frame, or explains why it is not one. */
export function parseFrame(line: string): { frame: Frame } | { error: string } {
  let value: unknown;
  try {
    value = JSON.parse(line);
  } catch (err) {
    return { error: `line is not a protocol frame: ${(err as Error).message}` };
  }
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return { error: "frame is not a JSON object" };
  }
  const type = (value as { type?: unknown }).type;
  if (typeof type !== "string" || type === "") {
    return { error: "frame has no type" };
  }
  return { frame: value as Frame };
}
