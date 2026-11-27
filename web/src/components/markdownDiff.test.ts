// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';

import { markdownBlocks } from './Markdown';
import { diffMarkdown } from './markdownDiff';

const kinds = (oldText: string, newText: string) => diffMarkdown(oldText, newText).parts.map((p) => `${p.kind}: ${p.text}`);

describe('the blocks of a Markdown text', () => {
  it('are each top-level block, and each item of a list', () => {
    const text = '# Title\n\nA paragraph\nover two lines.\n\n- one\n- two\n\n1. first\n1. second\n\n- - -\n';
    expect(markdownBlocks(text)).toEqual([
      { start: 0, end: 1, item: false },
      { start: 2, end: 4, item: false },
      { start: 5, end: 6, item: true },
      { start: 6, end: 8, item: true },
      { start: 8, end: 9, item: true, number: 1 },
      { start: 9, end: 11, item: true, number: 2 },
      { start: 11, end: 12, item: false },
    ]);
  });
});

describe('a diff of two versions of a Markdown file', () => {
  it('marks nothing when nothing drawn changed, not even trailing spaces', () => {
    const diff = diffMarkdown('# Title\n\nText.\n', '# Title  \n\nText.\n\n');
    expect(diff.added).toBe(0);
    expect(diff.removed).toBe(0);
    expect(diff.parts.map((p) => p.kind)).toEqual(['same', 'same']);
  });

  it('shows a changed block as it was, removed, then as it is, added, in its place', () => {
    expect(kinds('# Title\n\nOld text.\n\n## End\n', '# Title\n\nNew text.\n\n## End\n')).toEqual([
      'same: # Title',
      'removed: Old text.',
      'added: New text.',
      'same: ## End',
    ]);
  });

  it('marks only the item of a list that changed', () => {
    const diff = diffMarkdown('- one\n- two\n- three\n', '- one\n- 2\n- three\n- four\n');
    expect(diff.parts.map((p) => `${p.kind}: ${p.text}`)).toEqual(['same: - one', 'removed: - two', 'added: - 2', 'same: - three', 'added: - four']);
    expect(diff.parts.every((p) => p.item)).toBe(true);
  });

  it('draws an item of an ordered list with the number it has in its list', () => {
    const diff = diffMarkdown('1. a\n1. b\n1. c\n', '1. a\n1. b\n1. C\n');
    expect(diff.parts.map((p) => p.text)).toEqual(['1. a', '2. b', '3. c', '3. C']);
  });

  it('draws each block with the reference definitions of its own version', () => {
    const diff = diffMarkdown('See [the docs][d].\n\n[d]: https://old.example/\n', 'See [the guide][d].\n\n[d]: https://new.example/\n');
    expect(diff.parts.map((p) => `${p.kind}: ${p.text}`)).toEqual([
      'removed: See [the docs][d].\n\n[d]: https://old.example/',
      'added: See [the guide][d].\n\n[d]: https://new.example/',
    ]);
  });

  it('shows everything as removed or added when one version is empty', () => {
    expect(diffMarkdown('', '# New\n\nText.\n')).toMatchObject({ added: 2, removed: 0 });
    expect(diffMarkdown('# Old\n', '')).toMatchObject({ added: 0, removed: 1 });
  });

  it('still shows every change in a file too long to line up block by block', () => {
    const many = (word: string) => Array.from({ length: 1100 }, (_, i) => `${word} ${String(i)}.`).join('\n\n');
    const diff = diffMarkdown(`# Same\n\n${many('old')}\n\n# End\n`, `# Same\n\n${many('new')}\n\n# End\n`);
    expect(diff).toMatchObject({ added: 1100, removed: 1100 });
    expect(diff.parts[0]).toMatchObject({ kind: 'same', text: '# Same' });
    expect(diff.parts.at(-1)).toMatchObject({ kind: 'same', text: '# End' });
  });
});
