// SPDX-License-Identifier: MIT

import { type Block, markdownBlocks } from './Markdown';

// A diff of two versions of a Markdown file, by the blocks they are drawn as.
//
// Each version is split into blocks by the parser that draws it (see `markdownBlocks`
// in Markdown.tsx), and the two lists are lined up by each block's source
// text. A block in both is unchanged; one only in the old version was removed,
// and one only in the new version was added. A block whose text changed is
// therefore shown twice: as it was, removed, and as it is, added.
//
// Each block is then drawn on its own. A link that names a reference defined
// elsewhere in the file (`[text][label]`) still resolves, because the lines
// that belong to no block, among them every reference definition, are drawn
// with each block of the same version, where they show nothing.

export type ChangeKind = 'same' | 'added' | 'removed';

export interface Part {
  kind: ChangeKind;
  /** The block's Markdown, ready to draw on its own. */
  text: string;
  /** Whether the block is one item of a list. */
  item: boolean;
}

export interface MarkdownDiff {
  parts: Part[];
  added: number;
  removed: number;
}

/** Past this many pairs of blocks to compare, the changed middle is shown as removed, then added, without lining it up. */
const most = 1_000_000;

interface Version {
  lines: string[];
  blocks: Block[];
  keys: string[];
  /** The lines in no block that are not blank: reference definitions, mostly. */
  rest: string;
}

function read(text: string): Version {
  const lines = text.split('\n');
  const found = markdownBlocks(text);
  const covered = new Uint8Array(lines.length);
  for (const block of found) covered.fill(1, block.start, block.end);
  const rest = lines.filter((line, i) => !covered[i] && line.trim() !== '').join('\n');
  const keys = found.map((block) => source(lines, block));
  return { lines, blocks: found, keys, rest };
}

function source(lines: string[], block: Block): string {
  return lines
    .slice(block.start, block.end)
    .map((line) => line.trimEnd())
    .join('\n')
    .trimEnd();
}

/** The block as Markdown to draw on its own, numbered as it is in its list. */
function drawable(version: Version, index: number): string {
  const block = version.blocks[index];
  let text = version.keys[index] ?? '';
  if (block?.number !== undefined) text = text.replace(/^(\s*)\d{1,9}([.)])/, `$1${String(block.number)}$2`);
  return version.rest === '' ? text : `${text}\n\n${version.rest}`;
}

/** Pairs of indexes, one in each list, of the longest run of keys the two have in common, in order. */
function common(a: string[], b: string[]): [number, number][] {
  let head = 0;
  while (head < a.length && head < b.length && a[head] === b[head]) head++;
  let tail = 0;
  while (tail < a.length - head && tail < b.length - head && a[a.length - 1 - tail] === b[b.length - 1 - tail]) tail++;

  const pairs: [number, number][] = [];
  for (let i = 0; i < head; i++) pairs.push([i, i]);

  const n = a.length - head - tail;
  const m = b.length - head - tail;
  if (n > 0 && m > 0 && n * m <= most) {
    // lengths[i][j]: the longest common run of a[head+i..] and b[head+j..].
    const width = m + 1;
    const lengths = new Uint32Array((n + 1) * width);
    for (let i = n - 1; i >= 0; i--) {
      for (let j = m - 1; j >= 0; j--) {
        lengths[i * width + j] =
          a[head + i] === b[head + j]
            ? (lengths[(i + 1) * width + j + 1] ?? 0) + 1
            : Math.max(lengths[(i + 1) * width + j] ?? 0, lengths[i * width + j + 1] ?? 0);
      }
    }
    let i = 0;
    let j = 0;
    while (i < n && j < m) {
      if (a[head + i] === b[head + j]) {
        pairs.push([head + i, head + j]);
        i++;
        j++;
      } else if ((lengths[(i + 1) * width + j] ?? 0) >= (lengths[i * width + j + 1] ?? 0)) {
        i++;
      } else {
        j++;
      }
    }
  }

  for (let k = tail; k > 0; k--) pairs.push([a.length - k, b.length - k]);
  return pairs;
}

export function diffMarkdown(oldText: string, newText: string): MarkdownDiff {
  const before = read(oldText);
  const after = read(newText);
  const parts: Part[] = [];
  let added = 0;
  let removed = 0;
  const removedUpTo = (end: number, from: number) => {
    for (let i = from; i < end; i++) {
      parts.push({ kind: 'removed', text: drawable(before, i), item: before.blocks[i]?.item ?? false });
      removed++;
    }
  };
  const addedUpTo = (end: number, from: number) => {
    for (let j = from; j < end; j++) {
      parts.push({ kind: 'added', text: drawable(after, j), item: after.blocks[j]?.item ?? false });
      added++;
    }
  };

  let i = 0;
  let j = 0;
  for (const [a, b] of [...common(before.keys, after.keys), [before.keys.length, after.keys.length] as [number, number]]) {
    // Between two blocks in common, what was there before, then what is there now.
    removedUpTo(a, i);
    addedUpTo(b, j);
    if (a < before.keys.length && b < after.keys.length) {
      parts.push({ kind: 'same', text: drawable(after, b), item: after.blocks[b]?.item ?? false });
    }
    i = a + 1;
    j = b + 1;
  }
  return { parts, added, removed };
}
