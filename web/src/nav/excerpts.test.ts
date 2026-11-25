// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';

import type { Excerpt } from '../api/types';
import { brief, byFile, spoken } from './excerpts';

const sha = 'a'.repeat(40);
const at = (path: string, startLine: number, endLine: number, commit = sha): Excerpt => ({ sha: commit, path, startLine, endLine });

describe('byFile', () => {
  it('puts the lines of one file together and joins runs that touch or overlap', () => {
    const files = byFile([
      at('src/a.ts', 38, 39), // a search hit
      at('src/a.ts', 0, 113), // then the whole file was read
      at('src/a.ts', 42, 43),
      at('src/b.ts', 4, 5),
      at('src/b.ts', 5, 6), // touches the run before it
      at('src/b.ts', 9, 10),
    ]);
    expect(files).toEqual([
      { sha, path: 'src/a.ts', runs: [{ startLine: 0, endLine: 113 }], lines: 113 },
      { sha, path: 'src/b.ts', runs: [{ startLine: 4, endLine: 6 }, { startLine: 9, endLine: 10 }], lines: 3 },
    ]);
  });

  it('lists a file that was read before files a search touched, and keeps arrival order between equals', () => {
    const files = byFile([at('one.ts', 0, 1), at('two.ts', 0, 1), at('read.ts', 0, 40), at('three.ts', 7, 8)]);
    expect(files.map((file) => file.path)).toEqual(['read.ts', 'one.ts', 'two.ts', 'three.ts']);
  });

  it('counts a short read above many scattered search hits', () => {
    const hits = [0, 10, 20, 30, 40].map((line) => at('searched.ts', line, line + 1));
    const files = byFile([...hits, at('read.ts', 0, 3), at('once.ts', 5, 6)]);
    // Three lines in a row from one file, five single lines from another, one from a third.
    expect(files.map((file) => file.path)).toEqual(['read.ts', 'searched.ts', 'once.ts']);
  });

  it('lists the same lines once, however often they were given', () => {
    expect(byFile([at('a.ts', 3, 6), at('a.ts', 3, 6), at('a.ts', 4, 5)])).toEqual([
      { sha, path: 'a.ts', runs: [{ startLine: 3, endLine: 6 }], lines: 3 },
    ]);
  });

  it('keeps one path at two commits apart', () => {
    const other = 'b'.repeat(40);
    const files = byFile([at('a.ts', 0, 2), at('a.ts', 1, 3, other)]);
    expect(files.map((file) => [file.sha, file.runs])).toEqual([
      [sha, [{ startLine: 0, endLine: 2 }]],
      [other, [{ startLine: 1, endLine: 3 }]],
    ]);
  });

  it('does not change the list it was given, and drops a run with no lines', () => {
    const given = [at('a.ts', 5, 6), at('a.ts', 0, 1), at('a.ts', 2, 2)];
    const copy = structuredClone(given);
    expect(byFile(given)[0]?.runs).toEqual([{ startLine: 0, endLine: 1 }, { startLine: 5, endLine: 6 }]);
    expect(given).toEqual(copy);
  });
});

describe('how a run is written', () => {
  it('counts lines from 1, with the end included', () => {
    expect(spoken({ startLine: 3, endLine: 4 })).toBe('line 4');
    expect(spoken({ startLine: 0, endLine: 3 })).toBe('lines 1 to 3');
    expect(brief({ startLine: 3, endLine: 4 })).toBe('4');
    expect(brief({ startLine: 0, endLine: 3 })).toBe('1–3');
  });
});
