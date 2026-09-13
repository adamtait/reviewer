// SPDX-License-Identifier: MIT

/**
 * The protocol loop.
 *
 * Channel discipline is the one rule that matters and the easiest to break:
 * **stdout carries frames and nothing else**. Anything a human should read goes
 * to stderr, which the host captures into the run log. A stray `console.log`
 * anywhere in this package — or in a library it calls — corrupts the stream, so
 * `console.log` is reassigned to stderr on startup rather than trusted not to be
 * called. ESLint and dependency-cruiser both print to stdout in some
 * configurations, which is not hypothetical.
 */

import { createInterface } from "node:readline";
import type { Readable, Writable } from "node:stream";
import { PROTOCOL, parseFrame, type AnalyzeRequest, type Descriptor, type Finding, type Frame } from "./protocol.js";

/** What an analyzer implementation provides. */
export interface Analyzer {
  readonly id: string;
  readonly order: number;
  readonly lane: "deterministic" | "llm";
  /**
   * Why this analyzer cannot run here, or undefined when it can. Called once,
   * during `describe`, so a repository missing an ESLint config is told at the
   * top of the run rather than per invocation.
   */
  unavailable?(root: string): Promise<string | undefined>;
  run(req: AnalyzeRequest): Promise<{ findings: Finding[]; warnings?: string[] }>;
}

export interface ServeOptions {
  name: string;
  version: string;
  analyzers: Analyzer[];
  input?: Readable;
  output?: Writable;
  log?: Writable;
}

/** Runs the conversation until the host says bye or the stream closes. */
export async function serve(opts: ServeOptions): Promise<void> {
  const input = opts.input ?? process.stdin;
  const output = opts.output ?? process.stdout;
  const log = opts.log ?? process.stderr;

  const write = (frame: Frame): void => {
    output.write(JSON.stringify(frame) + "\n");
  };
  const note = (message: string): void => {
    log.write(`${opts.name}: ${message}\n`);
  };

  const byId = new Map(opts.analyzers.map((a) => [a.id, a]));
  let greeted = false;

  const lines = createInterface({ input, crlfDelay: Infinity });
  for await (const line of lines) {
    if (line.trim() === "") continue;

    const parsed = parseFrame(line);
    if ("error" in parsed) {
      // Keep reading: the host may recover, and exiting would lose every
      // analyzer that could still have run.
      note(parsed.error);
      write({ type: "error", message: parsed.error });
      continue;
    }
    const frame = parsed.frame;

    switch (frame.type) {
      case "hello": {
        if (frame.protocol !== PROTOCOL) {
          const message = `host speaks protocol ${frame.protocol}, this plugin speaks ${PROTOCOL}`;
          write({ type: "error", message });
          note(message);
          return;
        }
        greeted = true;
        write({ type: "hello", protocol: PROTOCOL, plugin: opts.name, version: opts.version });
        break;
      }

      case "describe": {
        if (!greeted) return refuse(write, note, frame.type);
        write({ type: "describe", analyzers: await describe(opts.analyzers) });
        break;
      }

      case "analyze": {
        if (!greeted) return refuse(write, note, frame.type);
        const analyzer = frame.analyzer ? byId.get(frame.analyzer) : undefined;
        if (!analyzer) {
          write({
            type: "error",
            ...(frame.analyzer === undefined ? {} : { analyzer: frame.analyzer }),
            message: `no analyzer named ${String(frame.analyzer)}`,
          });
          break;
        }
        try {
          const { findings, warnings } = await analyzer.run(frame.request ?? { root: process.cwd(), changed: [] });
          write({ type: "findings", analyzer: analyzer.id, findings, ...(warnings?.length ? { warnings } : {}) });
        } catch (err) {
          // One analyzer failing is not fatal to the run (ADR-0013).
          const message = err instanceof Error ? err.message : String(err);
          note(`${analyzer.id}: ${message}`);
          write({ type: "error", analyzer: analyzer.id, message });
        }
        break;
      }

      case "bye":
        return;

      default:
        // A newer host may send frames this plugin predates. Ignoring them is
        // what keeps the protocol additively extensible.
        note(`ignoring unknown frame type ${JSON.stringify(frame.type)}`);
    }
  }
}

function refuse(write: (f: Frame) => void, note: (m: string) => void, got: string): void {
  const message = `received ${got} before hello`;
  write({ type: "error", message });
  note(message);
}

async function describe(analyzers: Analyzer[]): Promise<Descriptor[]> {
  const out: Descriptor[] = [];
  for (const a of analyzers) {
    const reason = await a.unavailable?.(process.cwd());
    out.push({
      id: a.id,
      lane: a.lane,
      order: a.order,
      available: reason === undefined,
      ...(reason === undefined ? {} : { unavailable: reason }),
    });
  }
  return out;
}

/**
 * Redirects console output to stderr. Called before any analyzer runs, because a
 * library that logs to stdout would otherwise be indistinguishable from a
 * protocol frame.
 */
export function protectStdout(): void {
  const toStderr = (...args: unknown[]): void => {
    process.stderr.write(args.map((a) => (typeof a === "string" ? a : JSON.stringify(a))).join(" ") + "\n");
  };
  console.log = toStderr;
  console.info = toStderr;
  console.debug = toStderr;
  console.warn = toStderr;
}
