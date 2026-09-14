// SPDX-License-Identifier: MIT

/**
 * One TypeScript program per `tsconfig`, per run.
 *
 * This module is the reason the reviewer core can be written in Go (ADR-0002,
 * ADR-0014). `tsc`, typescript-eslint and type-coverage all need a program built
 * over the same `tsconfig`, and building it is the dominant cost of a run on a
 * large repository. A process-per-analyzer design builds it three times; this
 * plugin stays alive for the whole run and builds it once.
 *
 * The program is cached for the process lifetime, which is exactly one run. It is
 * deliberately not cached across runs: the working tree has changed by then, and
 * a stale program reports findings about code that no longer exists.
 */

import { createRequire } from "node:module";
import path from "node:path";
import type ts from "typescript";

/** A loaded TypeScript compiler and where it came from. */
export interface Compiler {
  ts: typeof ts;
  /** Resolved path, for the run log — which typescript actually analysed the code. */
  from: string;
  /** True when the repository's own typescript could not be resolved. */
  fallback: boolean;
}

/** A built program and the config it came from. */
export interface BuiltProgram {
  program: ts.Program;
  compiler: Compiler;
  configPath: string;
  /** Absolute file names the program includes. */
  fileNames: readonly string[];
}

/**
 * Resolves the *repository's* typescript, not the plugin's.
 *
 * A plugin installed as a devDependency of the repository under review sits inside
 * that repository's `node_modules`, so resolving from the repository root gets the
 * version the repository's own build and editor use. Analysing with a different
 * compiler than the author runs produces findings they cannot reproduce, which is
 * the fastest way to lose their trust.
 */
export function loadCompiler(root: string): Compiler {
  const requireFromRepo = createRequire(path.join(root, "package.json"));
  try {
    const resolved = requireFromRepo.resolve("typescript");
    return { ts: requireFromRepo(resolved) as typeof ts, from: resolved, fallback: false };
  } catch {
    // The plugin's own copy. Reported as a warning by every analyzer that uses
    // it, because the version mismatch explains findings the author may not see.
    const requireFromHere = createRequire(import.meta.url);
    const resolved = requireFromHere.resolve("typescript");
    return { ts: requireFromHere(resolved) as typeof ts, from: resolved, fallback: true };
  }
}

/** Where a repository's tsconfig lives, or undefined when it has none. */
export function findConfig(root: string, compiler: Compiler): string | undefined {
  const found = compiler.ts.findConfigFile(root, compiler.ts.sys.fileExists, "tsconfig.json");
  return found ?? undefined;
}

interface CacheEntry {
  built: BuiltProgram;
  diagnostics: readonly ts.Diagnostic[];
}

/**
 * Caches per resolved config path. Keyed on the path rather than on file contents:
 * within one run the working tree does not change, and hashing every input file to
 * prove it would cost more than the program build this cache exists to avoid.
 */
const cache = new Map<string, CacheEntry>();

/** Counts builds, so a test can prove the program is shared rather than rebuilt. */
let builds = 0;

/** How many programs have been built in this process. For tests and tracing. */
export function buildCount(): number {
  return builds;
}

/** Discards the cache. For tests only; a real run is one process. */
export function resetPrograms(): void {
  cache.clear();
  builds = 0;
}

/**
 * Returns the program for a repository, building it at most once per process.
 *
 * Throws when the repository has no `tsconfig.json`: every analyzer that calls
 * this reports itself unavailable in that case rather than failing at analysis
 * time.
 */
export function getProgram(root: string): BuiltProgram {
  const compiler = loadCompiler(root);
  const configPath = findConfig(root, compiler);
  if (configPath === undefined) {
    throw new Error("no tsconfig.json found");
  }

  const cached = cache.get(configPath);
  if (cached) return cached.built;

  const { ts: tsc } = compiler;
  const read = tsc.readConfigFile(configPath, tsc.sys.readFile);
  if (read.error) {
    throw new Error(`tsconfig.json could not be read: ${formatDiagnostic(tsc, read.error)}`);
  }

  const parsed = tsc.parseJsonConfigFileContent(
    read.config as unknown,
    tsc.sys,
    path.dirname(configPath),
    // noEmit: nothing is written. This analyzer type-checks, it does not build.
    { noEmit: true },
    configPath,
  );
  if (parsed.errors.length > 0) {
    const first = parsed.errors[0];
    if (first !== undefined && first.category === tsc.DiagnosticCategory.Error) {
      throw new Error(`tsconfig.json is invalid: ${formatDiagnostic(tsc, first)}`);
    }
  }

  const program = tsc.createProgram({
    rootNames: parsed.fileNames,
    options: parsed.options,
    ...(parsed.projectReferences === undefined ? {} : { projectReferences: parsed.projectReferences }),
  });
  builds += 1;

  const built: BuiltProgram = {
    program,
    compiler,
    configPath,
    fileNames: program.getSourceFiles().map((f) => f.fileName),
  };
  // Diagnostics are computed once too: getPreEmitDiagnostics is the expensive
  // half of a type-check, and three analyzers want the same answer.
  cache.set(configPath, { built, diagnostics: tsc.getPreEmitDiagnostics(program) });
  return built;
}

/** The program's diagnostics, computed once per program. */
export function getDiagnostics(root: string): { built: BuiltProgram; diagnostics: readonly ts.Diagnostic[] } {
  const built = getProgram(root);
  const entry = cache.get(built.configPath);
  return { built, diagnostics: entry?.diagnostics ?? [] };
}

export function formatDiagnostic(tsc: typeof ts, d: ts.Diagnostic): string {
  return tsc.flattenDiagnosticMessageText(d.messageText, " ");
}
