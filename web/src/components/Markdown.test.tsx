// SPDX-License-Identifier: MIT

import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { Markdown } from './Markdown';

afterEach(cleanup);

const github = 'https://github.example';
const show = (text: string) => render(<Markdown text={text} trusted={[`${github}/example/alpha/pull/12`]} />);

describe('Markdown from untrusted text', () => {
  it('draws the ordinary things', () => {
    const { container } = show(
      ['# A title', '', 'Some **bold** and *emphasis* and `code`.', '', '- one', '- two', '', '> quoted', '', '| a | b |', '|---|---|', '| 1 | 2 |'].join(
        '\n',
      ),
    );
    expect(container.querySelector('strong')?.textContent).toBe('bold');
    expect(container.querySelector('em')?.textContent).toBe('emphasis');
    expect(container.querySelector('p code')?.textContent).toBe('code');
    expect(container.querySelectorAll('li')).toHaveLength(2);
    expect(container.querySelector('blockquote')?.textContent).toContain('quoted');
    expect(container.querySelectorAll('td')).toHaveLength(2);
    // A top heading in the text is drawn below the page's own headings.
    expect(container.querySelector('h1, h2')).toBeNull();
    expect(container.querySelector('h3')?.textContent).toBe('A title');
  });

  it('shows a script tag as the characters it is', () => {
    const hostile = 'Before <script>window.pwned = true</script> after\n\n<script>window.pwned = true</script>\n\n<img src=x onerror="window.pwned = true">';
    const { container } = show(hostile);
    expect(container.querySelector('script, img, iframe, object, embed')).toBeNull();
    expect(container.textContent).toContain('<script>window.pwned = true</script>');
    expect(container.textContent).toContain('onerror=');
    expect((window as unknown as { pwned?: boolean }).pwned).toBeUndefined();
  });

  it('does not load a remote image', () => {
    const { container } = show('Look: ![a tracking pixel](https://elsewhere.example/pixel.png?reader=you)');
    expect(container.querySelector('img')).toBeNull();
    // What the author typed is still there to read.
    expect(container.textContent).toContain('https://elsewhere.example/pixel.png?reader=you');
  });

  it('shows a link to another host with its address, and does not make it clickable', () => {
    const { container } = show('See [the release notes](https://elsewhere.example/notes) for more.');
    expect(container.querySelector('a')).toBeNull();
    expect(container.textContent).toBe('See the release notes (https://elsewhere.example/notes) for more.');
  });

  it('does not make a link clickable because its text looks like somewhere safe', () => {
    const { container } = show(`[${github}/example/alpha](https://elsewhere.example/phish)`);
    expect(container.querySelector('a')).toBeNull();
    expect(container.textContent).toContain('(https://elsewhere.example/phish)');
  });

  it('makes a link clickable only when it leads to the workspace or to GitHub', () => {
    const { container } = show(
      [
        `[on GitHub](${github}/example/alpha/issues/3)`,
        '[in the workspace](/example/alpha/pull/12)',
        `[a lookalike](${github}.elsewhere.example/x)`,
        '[another scheme](ftp://github.example/x)',
      ].join('\n\n'),
    );
    const links = Array.from(container.querySelectorAll('a'));
    expect(links.map((a) => a.getAttribute('href'))).toEqual([`${github}/example/alpha/issues/3`, '/example/alpha/pull/12']);
    for (const link of links) {
      expect(link.getAttribute('rel')).toBe('noopener noreferrer');
      // The address is shown even when the link can be followed.
      expect(link.textContent).toContain(`(${link.getAttribute('href') ?? ''})`);
    }
  });

  it('never makes a javascript link', () => {
    const { container } = show('[click me](javascript:alert(1)) and [this](JaVaScRiPt:alert(1)) and [data](data:text/html,<script>1</script>)');
    expect(container.querySelector('a')).toBeNull();
  });

  it('shows a diagram block as its source', () => {
    const { container } = show('```mermaid\ngraph TD\n  A-->B\n```');
    const block = container.querySelector('pre');
    expect(block?.textContent).toBe('graph TD\n  A-->B\n');
    expect(block?.getAttribute('data-language')).toBe('mermaid');
    expect(container.querySelector('svg, canvas')).toBeNull();
  });

  it('shows code blocks exactly, including what looks like markup', () => {
    const { container } = show('```ts\nconst html = "<b>not bold</b>";\n```');
    expect(container.querySelector('pre code')?.textContent).toBe('const html = "<b>not bold</b>";\n');
    expect(container.querySelector('b')).toBeNull();
  });
});
