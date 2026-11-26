// SPDX-License-Identifier: MIT

import MarkdownIt from 'markdown-it';
import { type ReactNode, createElement, useMemo } from 'react';

// Markdown from people and models that are not trusted: a pull request's
// description, and an assistant's answer, which can repeat whatever the model
// read. It is drawn the way GitHub draws it, under GitHub's own limits.
//
// The text is turned into HTML by markdown-it, and that HTML is parsed into a
// document that is never attached to this page: a DOMParser document runs no
// script and loads nothing. That document is then copied into React elements,
// one node at a time, and only the tags and attributes listed below are
// copied. No HTML string ever reaches the page, so there is nothing to inject
// into.
//
// What GitHub allows, and what this keeps from it:
//
// - Raw HTML, through the allowlist GitHub's sanitizer uses. A tag not on it
//   is dropped and its text kept. A few tags are dropped with everything in
//   them, because their contents are not text for a reader.
// - GFM's "tagfilter": nine tags that change how the rest of the page is
//   parsed (`<script>`, `<style>`, `<iframe>`...) are shown as the characters
//   they are, as GitHub shows them.
// - Images are not loaded: an image address is a request to another server,
//   which would tell that server who is reading. An image is shown as its
//   description and its address.
// - A link can be clicked only if it leads to this workspace or to the GitHub
//   it reads from, and then its address is shown when the pointer rests on
//   it. A link anywhere else is shown as its text and its full address.
// - Task lists, the five alerts (`> [!NOTE]`...), and references to issues,
//   pull requests and people (`#12`, `owner/repo#12`, `@login`).

/** Tags that GitHub's tagfilter shows as text (GFM spec, section 6.11). */
const filtered = /<(?=\/?(?:title|textarea|style|xmp|iframe|noembed|noframes|script|plaintext)(?:[\s/>]|$))/gi;

function makeParser(breaks: boolean) {
  const parser = new MarkdownIt({ html: true, linkify: true, breaks, typographer: false });
  // With the rule off, `![alt](address)` stays as the text the author typed.
  parser.disable(['image']);
  // GitHub links `https://...` and `www.` addresses in text, not bare host
  // names or email addresses.
  parser.linkify.set({ fuzzyLink: false, fuzzyEmail: false });
  parser.core.ruler.push('tagfilter', (state) => {
    const clean = (html: string) => withoutHandlers(html.replace(filtered, '&lt;'));
    for (const token of state.tokens) {
      if (token.type === 'html_block') token.content = clean(token.content);
      for (const child of token.children ?? []) {
        if (child.type === 'html_inline') child.content = clean(child.content);
      }
    }
  });
  return parser;
}

const parsers = { text: makeParser(false), github: makeParser(true) };

/**
 * Raw HTML with every `on...` attribute taken out of its tags.
 *
 * This is not what keeps a handler from running: the copy into React keeps
 * no handler, and a parsed document runs none. It is here because Chromium
 * checks the content security policy as soon as it parses a handler, even in
 * a document that is never shown, and reports a violation for each one. A
 * description should not fill the console, or a policy report, with those.
 *
 * It reads tags the way the HTML tokenizer does: a name, then attributes
 * separated by spaces or slashes, each value quoted or not. Comments are
 * passed over whole, ended where the tokenizer ends them.
 */
export function withoutHandlers(html: string): string {
  let out = '';
  let i = 0;
  const space = (c: string | undefined) => c === ' ' || c === '\t' || c === '\n' || c === '\f' || c === '\r';
  while (i < html.length) {
    if (html.startsWith('<!--', i)) {
      // As the tokenizer reads a comment: `<!-->` and `<!--->` end at once,
      // and any other ends at the first `-->` or `--!>`.
      let stop: number;
      if (html.startsWith('<!-->', i)) stop = i + 5;
      else if (html.startsWith('<!--->', i)) stop = i + 6;
      else {
        const ends = [html.indexOf('-->', i + 4), html.indexOf('--!>', i + 4)].filter((n) => n !== -1);
        const end = ends.length === 0 ? -1 : Math.min(...ends);
        stop = end === -1 ? html.length : end + (html.startsWith('--!>', end) ? 4 : 3);
      }
      out += html.slice(i, stop);
      i = stop;
      continue;
    }
    if (html[i] !== '<' || !/[A-Za-z]/.test(html[i + 1] ?? '')) {
      out += html.charAt(i);
      i++;
      continue;
    }
    // A start tag: its name, then its attributes up to `>`.
    let j = i + 1;
    while (j < html.length && !space(html[j]) && html[j] !== '/' && html[j] !== '>') j++;
    out += html.slice(i, j);
    while (j < html.length && html[j] !== '>') {
      if (space(html[j]) || html[j] === '/') {
        out += html.charAt(j);
        j++;
        continue;
      }
      const start = j;
      j++; // The first character belongs to the name, even an `=`.
      while (j < html.length && !space(html[j]) && html[j] !== '/' && html[j] !== '>' && html[j] !== '=') j++;
      const name = html.slice(start, j);
      let k = j;
      while (space(html[k])) k++;
      if (html[k] === '=') {
        k++;
        while (space(html[k])) k++;
        const quote = html[k];
        if (quote === '"' || quote === "'") {
          const close = html.indexOf(quote, k + 1);
          k = close === -1 ? html.length : close + 1;
        } else {
          while (k < html.length && !space(html[k]) && html[k] !== '>') k++;
        }
        j = k;
      }
      if (!/^on/i.test(name)) out += html.slice(start, j);
    }
    i = j;
  }
  return out;
}

/**
 * Tags that may be drawn, from GitHub's sanitizer allowlist, and the tag each
 * becomes. `img`, `picture` and `source` are left out: images are not loaded.
 */
const tags: Record<string, string> = {
  // Headings are kept below the page's own, so that a description cannot
  // restructure the page's outline.
  h1: 'h3',
  h2: 'h4',
  h3: 'h5',
  h4: 'h6',
  h5: 'h6',
  h6: 'h6',
  ...Object.fromEntries(
    (
      'br b i strong em a pre code tt div ins del sup sub p ol ul table thead tbody tfoot blockquote dl dt dd kbd q samp var hr ' +
      'ruby rt rp li tr td th s strike summary details caption figure figcaption abbr bdo cite dfn mark small span time wbr'
    )
      .split(' ')
      .map((tag) => [tag, tag]),
  ),
};

/** Tags drawn with no children. */
const empty = new Set(['br', 'hr', 'wbr']);

/** Tags dropped with everything inside them. Any other tag not in `tags` is dropped and its contents kept. */
const dropped = new Set([
  'script',
  'style',
  'template',
  'svg',
  'math',
  'object',
  'embed',
  'iframe',
  'frame',
  'frameset',
  'noscript',
  'noembed',
  'noframes',
  'title',
  'textarea',
  'xmp',
  'plaintext',
  'head',
  'link',
  'meta',
  'base',
  'applet',
]);

/** Containers in which text between elements is only layout, and is not drawn. */
const structural = new Set(['body', 'ul', 'ol', 'dl', 'table', 'thead', 'tbody', 'tfoot', 'tr', 'details', 'blockquote', 'picture']);

const aligned = new Set(['p', 'div', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'td', 'th']);

const alertTitles: Record<string, string> = {
  note: 'Note',
  tip: 'Tip',
  important: 'Important',
  warning: 'Warning',
  caution: 'Caution',
};

export interface MarkdownProps {
  text: string;
  /**
   * Origins a link may lead to and still be clickable: this workspace, and the
   * GitHub it reads from. A link anywhere else is shown and cannot be followed.
   */
  trusted?: readonly string[];
  /**
   * Draw each line break in a paragraph as a break, as GitHub does in issues,
   * pull requests and discussions. Off for text that follows plain Markdown,
   * such as a model's answer.
   */
  breaks?: boolean;
  /**
   * The repository's address on GitHub. With it, `#12`, `owner/repo#12` and
   * `@login` in the text become links to GitHub.
   */
  repository?: string | undefined;
}

export function Markdown({ text, trusted = [], breaks = false, repository }: MarkdownProps) {
  // Callers build `trusted` afresh on each render. Keyed on its contents, an
  // unchanged text is not parsed again, such as every earlier answer while a
  // new one streams in.
  const trustedKey = trusted.join('\n');
  const origins = useMemo(
    () => new Set([window.location.origin, ...trustedKey.split('\n').map(originOf).filter((o) => o !== '')]),
    [trustedKey],
  );
  const drawn = useMemo(() => {
    const html = (breaks ? parsers.github : parsers.text).render(text);
    const body = new DOMParser().parseFromString(html, 'text/html').body;
    const marks: Marks = { tasks: new Set(), alerts: new Map() };
    markTasks(body, marks);
    markAlerts(body, marks);
    const context: Context = { origins, repository: repository ? repositoryOf(repository) : undefined, marks, plain: false };
    return children(body, context, 'm');
  }, [text, breaks, repository, origins]);
  return <div className="markdown">{drawn}</div>;
}

/**
 * A pull request's title, drawn as GitHub draws one: text in backticks is
 * code, and everything else, emphasis, links and HTML included, is the
 * characters the author typed.
 */
export function InlineMarkdown({ text }: { text: string }) {
  const parts: ReactNode[] = [];
  let at = 0;
  for (const match of text.matchAll(/`([^`]+)`/g)) {
    if (match.index > at) parts.push(text.slice(at, match.index));
    parts.push(<code key={match.index}>{match[1]}</code>);
    at = match.index + match[0].length;
  }
  if (at < text.length) parts.push(text.slice(at));
  return <>{parts}</>;
}

interface Marks {
  /** List items that began with `[ ]` or `[x]`, which now hold a checkbox. */
  tasks: Set<Element>;
  /** Block quotes that begin with an alert marker, and the alert's kind. */
  alerts: Map<Element, string>;
}

interface Context {
  origins: ReadonlySet<string>;
  repository: Repository | undefined;
  marks: Marks;
  /** Inside a link or code: text here is never turned into a reference. */
  plain: boolean;
}

interface Repository {
  origin: string;
  url: string;
}

function originOf(address: string): string {
  try {
    return new URL(address).origin;
  } catch {
    return '';
  }
}

function repositoryOf(address: string): Repository | undefined {
  const origin = originOf(address);
  return origin === '' ? undefined : { origin, url: address.replace(/\/+$/, '') };
}

/** Whether a link's address is one a reader may be sent to by a click. */
function followable(address: string, origins: ReadonlySet<string>): boolean {
  try {
    const url = new URL(address, window.location.origin);
    return (url.protocol === 'https:' || url.protocol === 'http:') && origins.has(url.origin);
  } catch {
    return false;
  }
}

/** The first child that is not whitespace. */
function firstContent(node: Node): ChildNode | null {
  let child = node.firstChild;
  while (child && child.nodeType === Node.TEXT_NODE && (child.textContent ?? '').trim() === '') child = child.nextSibling;
  return child;
}

/** Where a list item's or a block quote's own text begins: inside its first paragraph, if it has one. */
function leadingText(node: Element): Text | null {
  let first = firstContent(node);
  if (first instanceof Element && first.localName === 'p') first = first.firstChild;
  return first && first.nodeType === Node.TEXT_NODE ? (first as Text) : null;
}

/** Finds task-list items, puts a checkbox in place of each one's `[ ]` or `[x]`, and marks the item. */
function markTasks(body: HTMLElement, marks: Marks): void {
  for (const item of Array.from(body.querySelectorAll('li'))) {
    const text = leadingText(item);
    const match = text?.data.match(/^\[([ xX])\](?=\s|$)/);
    if (!text || !match) continue;
    text.data = text.data.slice(match[0].length);
    const box = body.ownerDocument.createElement('input');
    box.setAttribute('type', 'checkbox');
    if (match[1] !== ' ') box.setAttribute('checked', '');
    text.before(box);
    marks.tasks.add(item);
  }
}

/** Finds block quotes that open with `[!NOTE]` and the like, removes the marker, and marks the quote. */
function markAlerts(body: HTMLElement, marks: Marks): void {
  for (const quote of Array.from(body.querySelectorAll('blockquote'))) {
    const text = leadingText(quote);
    const match = text?.data.match(/^\[!(note|tip|important|warning|caution)\][ \t]*(?:\n|$)/i);
    if (!text || !match?.[1]) continue;
    // The marker must be alone on its line. When it ends its text rather
    // than its line, what follows must be a break or nothing.
    const after = text.nextSibling;
    if (!match[0].endsWith('\n') && after !== null && !(after instanceof Element && after.localName === 'br')) continue;
    text.data = text.data.slice(match[0].length);
    const paragraph = text.parentElement;
    if (text.data === '' && text.nextSibling instanceof Element && text.nextSibling.localName === 'br') {
      // With line breaks drawn, the marker's line ends in a break, and the
      // next line starts with the newline that followed it.
      text.nextSibling.remove();
      const next: ChildNode | null = text.nextSibling;
      if (next instanceof Text) next.data = next.data.replace(/^\s+/, '');
    }
    if (text.data === '') text.remove();
    if (paragraph && paragraph !== quote && firstContent(paragraph) === null) paragraph.remove();
    marks.alerts.set(quote, match[1].toLowerCase());
  }
}

function children(node: Node, context: Context, key: string): ReactNode[] {
  return Array.from(node.childNodes).flatMap((child, i) => draw(child, context, `${key}.${i}`));
}

function draw(node: Node, context: Context, key: string): ReactNode[] {
  if (node.nodeType === Node.TEXT_NODE) {
    const text = node.textContent ?? '';
    if (text.trim() === '' && structural.has(node.parentElement?.localName ?? '')) return [];
    return context.plain || !context.repository ? [text] : references(text, context.repository, context, key);
  }
  // Comments, and anything else that is not an element, are not shown.
  if (!(node instanceof Element)) return [];

  const name = node.localName;
  if (dropped.has(name)) return [];
  if (name === 'img') return [image(node, key)];
  if (name === 'a') return [anchor(node, context, key)];
  if (name === 'input') {
    // Only a checkbox, and never one the reader can change.
    if (node.getAttribute('type')?.toLowerCase() !== 'checkbox') return [];
    return [<input key={key} type="checkbox" disabled checked={node.hasAttribute('checked')} readOnly />];
  }
  if (name === 'blockquote') {
    const kind = context.marks.alerts.get(node);
    if (kind) {
      return [
        <div key={key} className={`markdown-alert markdown-alert-${kind}`}>
          <p className="markdown-alert-title">{alertTitles[kind]}</p>
          {children(node, context, key)}
        </div>,
      ];
    }
  }

  // Only the table's own entries: a tag named `constructor` must not find Object.
  const tag = Object.hasOwn(tags, name) ? tags[name] : undefined;
  if (!tag) return children(node, context, key);
  const props = attributes(node, name, context, key);
  if (empty.has(tag)) return [createElement(tag, props)];
  const inner = name === 'code' || name === 'pre' ? { ...context, plain: true } : context;
  return [createElement(tag, props, ...children(node, inner, key))];
}

/** The attributes kept on an allowed tag. Everything else, `style`, `class`, `id` and every handler included, is dropped. */
function attributes(node: Element, name: string, context: Context, key: string): Record<string, unknown> {
  const props: Record<string, unknown> = { key };
  if (aligned.has(name)) {
    // markdown-it writes a table column's alignment as a style, which is not kept.
    const style = node.getAttribute('style')?.match(/^\s*text-align:\s*(left|right|center)\s*;?\s*$/i)?.[1];
    const align = (node.getAttribute('align') ?? style)?.toLowerCase();
    if (align === 'left' || align === 'right' || align === 'center' || align === 'justify') props.align = align;
  }
  if ((name === 'td' || name === 'th') && node.hasAttribute('colspan')) props.colSpan = span(node.getAttribute('colspan'));
  if ((name === 'td' || name === 'th') && node.hasAttribute('rowspan')) props.rowSpan = span(node.getAttribute('rowspan'));
  if (name === 'ol' && node.hasAttribute('start')) {
    const start = Number.parseInt(node.getAttribute('start') ?? '', 10);
    if (Number.isFinite(start)) props.start = start;
  }
  if (name === 'details' && node.hasAttribute('open')) props.open = true;
  if (name === 'time' && node.hasAttribute('datetime')) props.dateTime = node.getAttribute('datetime');
  if (name === 'li' && context.marks.tasks.has(node)) props.className = 'task-list-item';
  if (name === 'pre') {
    // Whatever the language named, the block is shown as text. A diagram's
    // source is its source; nothing draws it.
    const language = node.querySelector(':scope > code')?.className.match(/(?:^|\s)language-(\S+)/)?.[1];
    if (language) props['data-language'] = language;
  }
  return props;
}

function span(value: string | null): number {
  const n = Number.parseInt(value ?? '', 10);
  return Number.isFinite(n) && n >= 1 && n <= 1000 ? n : 1;
}

function anchor(node: Element, context: Context, key: string): ReactNode {
  const inside = children(node, { ...context, plain: true }, key);
  const address = node.getAttribute('href');
  if (address === null) return <span key={key}>{inside}</span>;
  // A bare address that markdown-it linked has decoded text and an encoded address.
  const text = node.textContent.trim();
  const same = text === address || parsers.text.normalizeLink(text) === address || `mailto:${text}` === address;
  return link(address, inside, same, context, key);
}

/**
 * A link that leads to the workspace or to GitHub can be followed, with its
 * address shown when the pointer rests on it. Any other is shown as its text
 * and its full address, unless the text already is the address.
 */
function link(address: string, inside: ReactNode[], textIsAddress: boolean, context: Context, key: string): ReactNode {
  if (followable(address, context.origins)) {
    return (
      <a key={key} href={address} title={address} target="_blank" rel="noopener noreferrer">
        {inside}
      </a>
    );
  }
  return (
    <span key={key} className="link-text">
      {inside}
      {textIsAddress ? null : <span className="link-address"> ({address})</span>}
    </span>
  );
}

function image(node: Element, key: string): ReactNode {
  const alt = node.getAttribute('alt')?.trim() ?? '';
  const source = node.getAttribute('src') ?? '';
  return (
    <span key={key} className="image-placeholder">
      {alt ? `[image: ${alt}]` : '[image]'}
      {source ? <span className="link-address"> ({source})</span> : null}
    </span>
  );
}

/**
 * `#12`, `owner/repo#12` and `@login`, each preceded by the start of the text
 * or by a character that cannot be part of a word, a host name, a path or an
 * email address. A login, and so an owner, ends in a letter or a digit.
 */
const referencePattern =
  /(^|[^\w/@#&.-])(?:([A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?)\/([A-Za-z0-9._-]{1,100})#([0-9]+)(?!\w)|#([0-9]+)(?!\w)|@([A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?)(?![\w/-]))/g;

function references(text: string, repository: Repository, context: Context, key: string): ReactNode[] {
  const out: ReactNode[] = [];
  let at = 0;
  for (const match of text.matchAll(referencePattern)) {
    const [whole, before = '', owner, repo, crossNumber, number, login] = match;
    const start = match.index + before.length;
    let address: string;
    if (repo === '.' || repo === '..') continue; // Not a repository: it would lead out of one.
    if (owner && repo && crossNumber) address = `${repository.origin}/${owner}/${repo}/issues/${crossNumber}`;
    else if (number) address = `${repository.url}/issues/${number}`;
    else if (login) address = `${repository.origin}/${login}`;
    else continue;
    if (start > at) out.push(text.slice(at, start));
    const shown = whole.slice(before.length);
    out.push(link(address, [shown], false, context, `${key}.r${start}`));
    at = match.index + whole.length;
  }
  if (at < text.length) out.push(text.slice(at));
  return out;
}
