// SPDX-License-Identifier: MIT

import type { Excerpt } from '../api/types';

// The code an answer rests on, arranged for reading.
//
// The service reports every run of lines the model was given, in the order it
// was given them. A search that finds thirty lines reports thirty runs of one
// line, and a file read after a search reports lines already listed. Shown as
// they arrive, one question's list ran to sixty entries. Here they are put
// together by file, with runs that touch or overlap joined into one.

/** A run of lines. `startLine` counts from 0 and `endLine` is exclusive, as in the API. */
export interface Run {
  startLine: number;
  endLine: number;
}

/** The lines of one file, at one commit, that the model was given. */
export interface FileExcerpts {
  sha: string;
  path: string;
  runs: Run[];
  /** How many lines the runs hold between them. */
  lines: number;
}

/**
 * Groups excerpts by file and joins runs that touch or overlap. A file that
 * was read says more about an answer than one a search brushed past, so files
 * come in order of their longest unbroken run, longest first, and then of how
 * many lines were given in all. Files equal in both keep the order they
 * arrived in.
 */
export function byFile(excerpts: readonly Excerpt[]): FileExcerpts[] {
  const files = new Map<string, { sha: string; path: string; runs: Run[] }>();
  for (const excerpt of excerpts) {
    if (excerpt.endLine <= excerpt.startLine) continue;
    const key = `${excerpt.sha}\n${excerpt.path}`;
    let file = files.get(key);
    if (!file) {
      file = { sha: excerpt.sha, path: excerpt.path, runs: [] };
      files.set(key, file);
    }
    file.runs.push({ startLine: excerpt.startLine, endLine: excerpt.endLine });
  }

  const out: FileExcerpts[] = [];
  for (const file of files.values()) {
    const sorted = [...file.runs].sort((a, b) => a.startLine - b.startLine || a.endLine - b.endLine);
    const joined: Run[] = [];
    for (const run of sorted) {
      const last = joined.at(-1);
      if (last && run.startLine <= last.endLine) last.endLine = Math.max(last.endLine, run.endLine);
      else joined.push({ ...run });
    }
    out.push({ sha: file.sha, path: file.path, runs: joined, lines: joined.reduce((n, run) => n + run.endLine - run.startLine, 0) });
  }
  const longest = (file: FileExcerpts) => Math.max(...file.runs.map((run) => run.endLine - run.startLine));
  // Array.prototype.sort is stable, so equals keep their arrival order.
  return out.sort((a, b) => longest(b) - longest(a) || b.lines - a.lines);
}

/** A run of lines as a person counts them: "line 4", or "lines 1 to 3". */
export function spoken(run: Run): string {
  const first = run.startLine + 1;
  return run.endLine > first ? `lines ${String(first)} to ${String(run.endLine)}` : `line ${String(first)}`;
}

/** The same, short, for a list of runs beside a file's name: "4", or "1–3". */
export function brief(run: Run): string {
  const first = run.startLine + 1;
  return run.endLine > first ? `${String(first)}–${String(run.endLine)}` : String(first);
}
