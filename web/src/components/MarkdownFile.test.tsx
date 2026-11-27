// SPDX-License-Identifier: MIT

import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { MarkdownFile, isMarkdown } from './MarkdownFile';

afterEach(cleanup);

describe('a Markdown file', () => {
  it('is known by its name, as GitHub knows it', () => {
    for (const path of ['README.md', 'docs/guide.markdown', 'NOTES.MD', 'a.mdown', 'a.mkdn', 'a.mkd', 'a.mdwn']) {
      expect(isMarkdown(path), path).toBe(true);
    }
    for (const path of ['src/cart.ts', 'page.mdx', 'md', 'README', 'notes.md.txt']) {
      expect(isMarkdown(path), path).toBe(false);
    }
  });

  it('is drawn as Markdown, with line breaks in a paragraph joined as GitHub joins them in a file', () => {
    const { getByTestId } = render(<MarkdownFile text={'# Alpha\n\nA\nshop.\n\n- one\n- two\n'} />);
    const file = getByTestId('markdown-file');
    expect(file.querySelector('h3')?.textContent).toBe('Alpha'); // below the page's own headings
    expect(file.querySelectorAll('li')).toHaveLength(2);
    expect(file.querySelector('p br')).toBeNull();
  });

  it('never turns what it contains into something that runs, loads or leads away', () => {
    const hostile = ['<script>window.pwned = true</script>', '![tracking](https://elsewhere.example/pixel.png)', '[phish](https://elsewhere.example/)'].join(
      '\n\n',
    );
    const { container } = render(<MarkdownFile text={hostile} github="https://github.example/example/alpha/pull/12" />);
    expect(container.querySelector('script, img, a, iframe')).toBeNull();
    expect((window as unknown as { pwned?: boolean }).pwned).toBeUndefined();
  });
});
