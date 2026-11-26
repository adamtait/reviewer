// SPDX-License-Identifier: MIT

import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import * as parse5 from 'parse5';

import { InlineMarkdown, Markdown } from './Markdown';
import dependabot from './testdata/dependabot.md?raw';

// parse5 as it is, with its calls counted.
vi.mock('parse5', { spy: true });

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

  it('shows a script tag as the characters it is, as GitHub does', () => {
    const hostile = 'Before <script>window.pwned = true</script> after\n\n<script>window.pwned = true</script>\n\n<img src=x onerror="window.pwned = true">';
    const { container } = show(hostile);
    expect(container.querySelector('script, img, iframe, object, embed')).toBeNull();
    expect(container.textContent).toContain('<script>window.pwned = true</script>');
    // The image is described, not loaded, and its handler is gone.
    expect(container.querySelector('.image-placeholder')?.textContent).toBe('[image] (x)');
    expect(container.textContent).not.toContain('onerror');
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
      // The address is shown when the pointer rests on a link that can be followed.
      expect(link.getAttribute('title')).toBe(link.getAttribute('href'));
      expect(link.textContent).not.toContain('(');
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

describe('GitHub markup', () => {
  const repository = `${github}/example/alpha`;
  const gh = (text: string, breaks = false) =>
    render(<Markdown text={text} trusted={[`${repository}/pull/12`]} repository={repository} breaks={breaks} />);

  it('draws a Dependabot description the way GitHub does', () => {
    const errors = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const { container } = render(<Markdown text={dependabot} trusted={['https://github.com/example/alpha/pull/7']} breaks />);
    // React complained about nothing, such as text where a table or list cannot hold it.
    expect(errors).not.toHaveBeenCalled();
    errors.mockRestore();

    const sections = Array.from(container.querySelectorAll('details'));
    expect(sections.map((d) => d.querySelector(':scope > summary')?.textContent)).toEqual([
      'Release notes',
      'Changelog',
      'Commits',
      'Maintainer changes',
    ]);
    // The raw tags are drawn, not shown.
    expect(container.textContent).not.toContain('<details>');
    expect(container.textContent).not.toContain('<summary>');
    expect(container.textContent).not.toContain('raw HTML omitted');
    // Headings inside stay below the page's own.
    expect(container.querySelector('h1, h2')).toBeNull();
    expect(container.querySelector('blockquote h4')?.textContent).toBe('v11.0.7');
    expect(container.querySelector('blockquote h3')?.textContent).toBe('change log');
    expect(container.querySelector('blockquote code')?.textContent).toBe('mergeRows');
    expect(container.querySelector('p em')?.textContent).toBe("Sourced from minimatch's releases.");

    // Links to GitHub can be followed; the npm link cannot, and shows where it leads.
    const commit = container.querySelector('a[href$="e89926755f3f79cf79f1087bd5730ed4d7eba60e"]');
    expect(commit?.textContent).toBe('e899267');
    expect(commit?.querySelector('code')).not.toBeNull();
    const npm = sections[3]?.querySelector('.link-text');
    expect(npm?.textContent).toBe('GitHub Actions (https://www.npmjs.com/~GitHub%20Actions)');
    expect(sections[3]?.querySelector('a')).toBeNull();

    // A bare address in the text becomes a link, and is not repeated beside itself.
    const bare = container.querySelector('a[href="https://github.com/isaacs/minimatch"]');
    expect(bare?.textContent).toBe('https://github.com/isaacs/minimatch');
    expect(container.querySelector('img')).toBeNull();
  });

  it('shows the tags GitHub filters as the characters they are', () => {
    const { container } = gh('<style>p { display: none }</style>\n\n<iframe src="https://elsewhere.example"></iframe>\n\na <textarea>b</textarea>');
    expect(container.querySelector('style, iframe, textarea')).toBeNull();
    expect(container.textContent).toContain('<style>p { display: none }</style>');
    expect(container.textContent).toContain('<iframe src="https://elsewhere.example"></iframe>');
    expect(container.textContent).toContain('<textarea>b</textarea>');
  });

  it('keeps only the attributes on the list, and no raw link that runs code', () => {
    const { container } = gh(
      '<p id="top" class="x" style="color: red" onclick="window.pwned = true" align="center">centred</p>\n\n<a href="javascript:alert(1)">run</a> <a href="https://elsewhere.example/x" onmouseover="window.pwned = true">away</a>',
    );
    const p = container.querySelector('p');
    expect(p?.getAttribute('align')).toBe('center');
    for (const name of ['id', 'class', 'style', 'onclick']) expect(p?.hasAttribute(name)).toBe(false);
    expect(container.querySelector('a')).toBeNull();
    expect(container.textContent).toContain('run (javascript:alert(1))');
    expect(container.textContent).toContain('away (https://elsewhere.example/x)');
  });

  it('keeps the text of a tag it does not know, and drops what is not text', () => {
    const { container } = gh('<custom-tag>kept</custom-tag> <svg><text>drawing</text></svg> <template>hidden</template> <font color=red>plain</font>');
    expect(container.querySelector('custom-tag, svg, template, font')).toBeNull();
    expect(container.textContent).toContain('kept');
    expect(container.textContent).toContain('plain');
    expect(container.textContent).not.toContain('drawing');
    expect(container.textContent).not.toContain('hidden');
  });

  it('draws the GitHub tags a description uses, such as keys, superscripts and definitions', () => {
    const { container } = gh('Press <kbd>Ctrl</kbd>, x<sup>2</sup>, <del>old</del> <ins>new</ins>\n\n<dl><dt>term</dt><dd>meaning</dd></dl>');
    expect(container.querySelector('kbd')?.textContent).toBe('Ctrl');
    expect(container.querySelector('sup')?.textContent).toBe('2');
    expect(container.querySelector('del')?.textContent).toBe('old');
    expect(container.querySelector('ins')?.textContent).toBe('new');
    expect(container.querySelector('dl dt')?.textContent).toBe('term');
  });

  it('keeps a table column aligned', () => {
    const { container } = gh('| a | b |\n|:-:|--:|\n| 1 | 2 |');
    const cells = Array.from(container.querySelectorAll('td'));
    expect(cells.map((c) => c.getAttribute('align'))).toEqual(['center', 'right']);
    expect(cells.some((c) => c.hasAttribute('style'))).toBe(false);
  });

  it('draws a line break as a break only for GitHub text', () => {
    expect(gh('one\ntwo', true).container.querySelector('p br')).not.toBeNull();
    cleanup();
    expect(gh('one\ntwo', false).container.querySelector('br')).toBeNull();
  });

  it('draws a task list with boxes that cannot be changed', () => {
    const { container } = gh('- [ ] to do\n- [x] done\n- [link](/x) not a task');
    const boxes = Array.from(container.querySelectorAll('input'));
    expect(boxes.map((b) => [b.type, b.disabled, b.checked])).toEqual([
      ['checkbox', true, false],
      ['checkbox', true, true],
    ]);
    const items = Array.from(container.querySelectorAll('li'));
    expect(items.map((li) => li.className)).toEqual(['task-list-item', 'task-list-item', '']);
    expect(items[0]?.textContent).toBe(' to do');
    expect(container.textContent).not.toContain('[x]');
  });

  it('does not let the text make a box other than a checkbox', () => {
    const { container } = gh('<input type="text" value="typed"> <input type="checkbox" checked onclick="window.pwned = true">');
    const boxes = Array.from(container.querySelectorAll('input'));
    expect(boxes).toHaveLength(1);
    expect(boxes[0]?.type).toBe('checkbox');
    expect(boxes[0]?.disabled).toBe(true);
    expect(boxes[0]?.hasAttribute('onclick')).toBe(false);
  });

  it.each([
    ['NOTE', 'note', 'Note'],
    ['TIP', 'tip', 'Tip'],
    ['IMPORTANT', 'important', 'Important'],
    ['WARNING', 'warning', 'Warning'],
    ['CAUTION', 'caution', 'Caution'],
  ])('draws a [!%s] block quote as an alert', (marker, kind, title) => {
    for (const breaks of [false, true]) {
      const { container } = gh(`> [!${marker}]\n> Read this first.`, breaks);
      const alert = container.querySelector(`.markdown-alert.markdown-alert-${kind}`);
      expect(alert?.querySelector('.markdown-alert-title')?.textContent).toBe(title);
      expect(alert?.textContent).toBe(`${title}Read this first.`);
      expect(container.querySelector('blockquote, br')).toBeNull();
      cleanup();
    }
  });

  it('leaves a block quote alone when its marker is not one of the five, or not alone on its line', () => {
    const { container } = gh('> [!FOO]\n> text\n\n> [!NOTE] on the same line\n\n> [!NOTE] **bold on the same line**\n\n> [!TIP] `code`');
    expect(container.querySelector('.markdown-alert')).toBeNull();
    expect(container.querySelectorAll('blockquote')).toHaveLength(4);
    expect(container.textContent).toContain('[!FOO]');
  });

  it('links issues, pull requests and people to GitHub', () => {
    const { container } = gh('Fixes #12, see other/repo#3, thanks @octo-cat.');
    expect(Array.from(container.querySelectorAll('a'), (a) => [a.textContent, a.getAttribute('href')])).toEqual([
      ['#12', `${repository}/issues/12`],
      ['other/repo#3', `${github}/other/repo/issues/3`],
      ['@octo-cat', `${github}/octo-cat`],
    ]);
    expect(container.textContent).toBe('Fixes #12, see other/repo#3, thanks @octo-cat.');
  });

  it('does not link a reference in code, in a link, in an email address or inside a word', () => {
    const { container } = gh('`#12` and [see #4](/x) and a@b.example and x#1 and @org/team\n\n```\n#7 @someone\n```');
    expect(Array.from(container.querySelectorAll('a'), (a) => a.getAttribute('href'))).toEqual(['/x']);
    // An email address is shown once, not beside its own address.
    expect(container.textContent).toContain('and a@b.example and');
  });

  it('does not link what only looks like a reference', () => {
    const { container } = gh('see example.com/foo#12 and a.b/c#1 and x-#2 and owner/..#3 and owner/.#4 and @foo- and @-foo');
    expect(container.querySelector('a')).toBeNull();
    cleanup();
    // A login of one character, and one with a hyphen inside, are logins.
    const linked = gh('@a and @a-b');
    expect(Array.from(linked.container.querySelectorAll('a'), (a) => a.getAttribute('href'))).toEqual([`${github}/a`, `${github}/a-b`]);
  });

  it('does not link a reference without a repository to link it to', () => {
    const { container } = show('Fixes #12, thanks @octo-cat.');
    expect(container.querySelector('a')).toBeNull();
  });

  it('draws a title the way GitHub does: code in backticks, and nothing else', () => {
    const { container } = render(
      <h1>
        <InlineMarkdown text="Rename `total` to **sum** <b>now</b> [x](https://elsewhere.example)" />
      </h1>,
    );
    expect(container.querySelector('code')?.textContent).toBe('total');
    expect(container.querySelector('strong, b, a')).toBeNull();
    expect(container.textContent).toBe('Rename total to **sum** <b>now</b> [x](https://elsewhere.example)');
  });
});

describe('the renderer itself', () => {
  it('draws a tag named after a property every object has as its text', () => {
    const { container } = show('Hello <constructor>x</constructor> <toString>y</toString> <valueOf>z</valueOf> <hasOwnProperty>w</hasOwnProperty>');
    expect(container.textContent).toBe('Hello x y z w');
  });

  it('does not parse an unchanged text again when the page draws it again', () => {
    const parse = vi.mocked(parse5.parse);
    parse.mockClear();
    const view = (n: number) => <Markdown key="one" text={`unchanged ${'x'.repeat(n)}`} trusted={[`${github}/example/alpha/pull/12`]} />;
    const { rerender } = render(view(1));
    rerender(view(1));
    rerender(view(1));
    expect(parse).toHaveBeenCalledTimes(1);
    rerender(view(2));
    expect(parse).toHaveBeenCalledTimes(2);
  });

  it('never has the browser parse the text, so no handler in it is even read', () => {
    // Inputs a hand-written scanner read differently from the browser.
    const hostile = [
      '<div>\n<?x <!-- ?><b onclick="alert(1)">bogus</b>-->\n</div>',
      '<div>\n</x <!-- ><i onclick="alert(1)">endtag</i>-->\n</div>',
      '<div>\n<!x <!-- ><i onclick="alert(1)">decl</i>-->\n</div>',
      "<div title='\n\n' onclick=alert(1) x='\n\n'>shown",
      '<svg><![CDATA[<!--]]><b onclick="alert(1)">cdata</b>',
    ];
    const parsed = vi.spyOn(DOMParser.prototype, 'parseFromString');
    for (const text of hostile) {
      const { container } = show(text);
      expect(container.querySelectorAll('[onclick]')).toHaveLength(0);
      cleanup();
    }
    expect(parsed).not.toHaveBeenCalled();
    parsed.mockRestore();
  });

  it('draws a long run of comments quickly', () => {
    const text = '<div>\n' + '<!-- -->'.repeat(8192);
    const began = performance.now();
    show(text);
    expect(performance.now() - began).toBeLessThan(1000);
  });

  it('keeps the space between words inside a block quote or a folded section', () => {
    const { container } = show('<blockquote><b>Breaking</b> <i>change</i></blockquote>\n\n<details><summary>S</summary><code>a</code> <code>b</code></details>');
    expect(container.querySelector('blockquote')?.textContent).toBe('Breaking change');
    expect(container.querySelector('details')?.textContent).toBe('Sa b');
  });

  it('keeps the attributes on the list, within their limits', () => {
    const { container } = show(
      '<table><tr><td colspan=2 rowspan=0>x</td><td colspan=5000>y</td></tr></table>\n\n<ol start=3><li>a</li></ol>\n\n<time datetime=2026-01-01>t</time> <details open><summary>s</summary>body</details>',
    );
    const [first, second] = Array.from(container.querySelectorAll('td'));
    expect(first?.colSpan).toBe(2);
    expect(first?.rowSpan).toBe(1);
    expect(second?.colSpan).toBe(1);
    expect(container.querySelector('ol')?.getAttribute('start')).toBe('3');
    expect(container.querySelector('time')?.getAttribute('datetime')).toBe('2026-01-01');
    expect(container.querySelector('details')?.open).toBe(true);
  });

  it('shows a bare address once, even when its text and its address are written differently', () => {
    const { container } = show('See https://elsewhere.example/a%20b and https://elsewhere.example/café.');
    expect(container.textContent).toBe('See https://elsewhere.example/a b and https://elsewhere.example/café.');
    expect(container.querySelectorAll('.link-text')).toHaveLength(2);
  });
});
