// SPDX-License-Identifier: MIT

import MarkdownIt from 'markdown-it';
import { type DefaultTreeAdapterTypes as T, defaultTreeAdapter as tree, html as namespaces, parse } from 'parse5';
import { type ReactNode, createElement, useMemo } from 'react';

// Markdown from people and models that are not trusted: a pull request's
// description, and an assistant's answer, which can repeat whatever the model
// read. It is drawn the way GitHub draws it, under GitHub's own limits.
//
// The text is turned into HTML by markdown-it, and that HTML is parsed by
// parse5, which follows the HTML standard's parsing rules and builds a tree of
// plain objects. The browser never parses it: nothing in it can run, load, or
// be reported against the content security policy. The tree is then copied
// into React elements, one node at a time, and only the tags and attributes
// listed below are copied. No HTML string ever reaches the page, so there is
// nothing to inject into.
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
    const clean = (html: string) => html.replace(filtered, '&lt;');
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

/** A block of a Markdown text: its lines, counted from 0, `end` not included. */
export interface Block {
  start: number;
  end: number;
  /** Whether the block is one item of a list. */
  item: boolean;
  /** For an item of an ordered list, the number it is drawn with. */
  number?: number;
}

/**
 * The blocks a Markdown text is drawn as, in order, found by the same parser
 * that draws it: each paragraph, heading, quote, table, code block or rule,
 * and each item of a list rather than the whole list, so that a change to one
 * item of a long list marks that item. Lines that belong to no block, such as
 * blank lines and link reference definitions, are left out.
 */
export function markdownBlocks(text: string): Block[] {
  const tokens = parsers.text.parse(text, {});
  const found: Block[] = [];
  for (let i = 0; i < tokens.length; i++) {
    const token = tokens[i];
    if (!token?.map || token.level !== 0) continue;
    if (token.type !== 'bullet_list_open' && token.type !== 'ordered_list_open') {
      found.push({ start: token.map[0], end: token.map[1], item: false });
      continue;
    }
    const close = token.type.replace('_open', '_close');
    let number = Number(token.attrGet('start') ?? 1);
    for (i++; i < tokens.length; i++) {
      const inner = tokens[i];
      if (!inner || (inner.level === 0 && inner.type === close)) break;
      if (inner.level !== 1 || inner.type !== 'list_item_open' || !inner.map) continue;
      const item: Block = { start: inner.map[0], end: inner.map[1], item: true };
      if (token.type === 'ordered_list_open') item.number = number++;
      found.push(item);
    }
  }
  return found;
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
  'noscript',
  'noembed',
  'noframes',
  'title',
  'textarea',
  'xmp',
  'plaintext',
  'applet',
]);

/** Containers in which text between elements is only layout, and is not drawn: React refuses text in a table's rows. */
const structural = new Set(['ul', 'ol', 'dl', 'table', 'thead', 'tbody', 'tfoot', 'tr']);

/** Blocks: whitespace with a block, or nothing, on each side is only layout, and is not drawn. */
const blocks = new Set([
  'p',
  'div',
  'h1',
  'h2',
  'h3',
  'h4',
  'h5',
  'h6',
  'ul',
  'ol',
  'li',
  'dl',
  'dt',
  'dd',
  'table',
  'blockquote',
  'pre',
  'hr',
  'details',
  'summary',
  'figure',
  'figcaption',
]);

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
    const body = bodyOf(parse(html, { scriptingEnabled: false }));
    if (!body) return [];
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

type Node = T.ChildNode | T.ParentNode;
type Element = T.Element;
type Text = T.TextNode;

interface Marks {
  /** List items that began with `[ ]` or `[x]`, which now hold a checkbox. */
  tasks: Set<Element>;
  /** Block quotes that begin with an alert marker, and the alert's kind. */
  alerts: Map<Element, string>;
}

/** The `body` of a parsed document. The parser always makes one. */
function bodyOf(document: T.Document): Element | undefined {
  const root = document.childNodes.find(isElement);
  return root?.childNodes.find((node): node is Element => isElement(node) && node.tagName === 'body');
}

function isElement(node: Node): node is Element {
  return 'tagName' in node;
}

function isText(node: Node | null | undefined): node is Text {
  return node?.nodeName === '#text';
}

/** An attribute's value, or null when the element does not have it. The parser lower-cases names. */
function attribute(node: Element, name: string): string | null {
  return node.attrs.find((a) => a.name === name)?.value ?? null;
}

/** Every element under `node`, in document order. */
function* elements(node: Node): Generator<Element> {
  for (const child of 'childNodes' in node ? node.childNodes : []) {
    if (!isElement(child)) continue;
    yield child;
    yield* elements(child);
  }
}

function textOf(node: Node): string {
  if (isText(node)) return node.value;
  return 'childNodes' in node ? node.childNodes.map(textOf).join('') : '';
}

function siblings(node: T.ChildNode): T.ChildNode[] {
  return node.parentNode?.childNodes ?? [];
}

function next(node: T.ChildNode): T.ChildNode | undefined {
  const all = siblings(node);
  return all[all.indexOf(node) + 1];
}

function previous(node: T.ChildNode): T.ChildNode | undefined {
  const all = siblings(node);
  return all[all.indexOf(node) - 1];
}

function isTag(node: T.ChildNode | undefined, name: string): boolean {
  return node !== undefined && isElement(node) && node.tagName === name;
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
function firstContent(node: Element): T.ChildNode | undefined {
  return node.childNodes.find((child) => !(isText(child) && child.value.trim() === ''));
}

/** Where a list item's or a block quote's own text begins: inside its first paragraph, if it has one. */
function leadingText(node: Element): Text | undefined {
  let first = firstContent(node);
  if (first && isTag(first, 'p')) first = (first as Element).childNodes[0];
  return isText(first) ? first : undefined;
}

/** Finds task-list items, puts a checkbox in place of each one's `[ ]` or `[x]`, and marks the item. */
function markTasks(body: Element, marks: Marks): void {
  for (const item of Array.from(elements(body)).filter((e) => e.tagName === 'li')) {
    const text = leadingText(item);
    const match = text?.value.match(/^\[([ xX])\](?=\s|$)/);
    if (!text?.parentNode || !match) continue;
    text.value = text.value.slice(match[0].length);
    const attrs = [{ name: 'type', value: 'checkbox' }, ...(match[1] === ' ' ? [] : [{ name: 'checked', value: '' }])];
    tree.insertBefore(text.parentNode, tree.createElement('input', namespaces.NS.HTML, attrs), text);
    marks.tasks.add(item);
  }
}

/** Finds block quotes that open with `[!NOTE]` and the like, removes the marker, and marks the quote. */
function markAlerts(body: Element, marks: Marks): void {
  for (const quote of Array.from(elements(body)).filter((e) => e.tagName === 'blockquote')) {
    const text = leadingText(quote);
    const match = text?.value.match(/^\[!(note|tip|important|warning|caution)\][ \t]*(?:\n|$)/i);
    if (!text || !match?.[1]) continue;
    // The marker must be alone on its line. When it ends its text rather
    // than its line, what follows must be a break or nothing.
    const after = next(text);
    if (!match[0].endsWith('\n') && after !== undefined && !isTag(after, 'br')) continue;
    text.value = text.value.slice(match[0].length);
    const paragraph = text.parentNode;
    if (text.value === '' && after && isTag(after, 'br')) {
      // With line breaks drawn, the marker's line ends in a break, and the
      // next line starts with the newline that followed it.
      tree.detachNode(after);
      const following = next(text);
      if (isText(following)) following.value = following.value.replace(/^\s+/, '');
    }
    if (text.value === '') tree.detachNode(text);
    if (paragraph && paragraph !== quote && isElement(paragraph) && firstContent(paragraph) === undefined) tree.detachNode(paragraph);
    marks.alerts.set(quote, match[1].toLowerCase());
  }
}

/** Whether whitespace-only text is only layout: inside a list or table, or between two blocks. */
function layout(node: Text): boolean {
  const parent = node.parentNode;
  if (parent && isElement(parent) && structural.has(parent.tagName)) return true;
  const isBlock = (n: T.ChildNode | undefined) => n === undefined || (isElement(n) && blocks.has(n.tagName));
  return isBlock(previous(node)) && isBlock(next(node));
}

function children(node: Element, context: Context, key: string): ReactNode[] {
  return node.childNodes.flatMap((child, i) => draw(child, context, `${key}.${i}`));
}

function draw(node: T.ChildNode, context: Context, key: string): ReactNode[] {
  if (isText(node)) {
    const text = node.value;
    if (text.trim() === '' && layout(node)) return [];
    return context.plain || !context.repository ? [text] : references(text, context.repository, context, key);
  }
  // Comments, and anything else that is not an element, are not shown.
  if (!isElement(node)) return [];
  // Drawings and formulas are not copied: their tags are not HTML's.
  if (node.namespaceURI !== namespaces.NS.HTML) return [];

  const name = node.tagName;
  if (dropped.has(name)) return [];
  if (name === 'img') return [image(node, key)];
  if (name === 'a') return [anchor(node, context, key)];
  if (name === 'input') {
    // Only a checkbox, and never one the reader can change.
    if (attribute(node, 'type')?.toLowerCase() !== 'checkbox') return [];
    return [<input key={key} type="checkbox" disabled checked={attribute(node, 'checked') !== null} readOnly />];
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
    const style = attribute(node, 'style')?.match(/^\s*text-align:\s*(left|right|center)\s*;?\s*$/i)?.[1];
    const align = (attribute(node, 'align') ?? style)?.toLowerCase();
    if (align === 'left' || align === 'right' || align === 'center' || align === 'justify') props.align = align;
  }
  const colspan = attribute(node, 'colspan');
  const rowspan = attribute(node, 'rowspan');
  if ((name === 'td' || name === 'th') && colspan !== null) props.colSpan = span(colspan);
  if ((name === 'td' || name === 'th') && rowspan !== null) props.rowSpan = span(rowspan);
  const start = Number.parseInt(attribute(node, 'start') ?? '', 10);
  if (name === 'ol' && Number.isFinite(start)) props.start = start;
  if (name === 'details' && attribute(node, 'open') !== null) props.open = true;
  const datetime = attribute(node, 'datetime');
  if (name === 'time' && datetime !== null) props.dateTime = datetime;
  if (name === 'li' && context.marks.tasks.has(node)) props.className = 'task-list-item';
  if (name === 'pre') {
    // Whatever the language named, the block is shown as text. A diagram's
    // source is its source; nothing draws it.
    const code = node.childNodes.find((child): child is Element => isTag(child, 'code'));
    const language = (code && attribute(code, 'class'))?.match(/(?:^|\s)language-(\S+)/)?.[1];
    if (language) props['data-language'] = language;
  }
  return props;
}

function span(value: string): number {
  const n = Number.parseInt(value, 10);
  return Number.isFinite(n) && n >= 1 && n <= 1000 ? n : 1;
}

function anchor(node: Element, context: Context, key: string): ReactNode {
  const inside = children(node, { ...context, plain: true }, key);
  const address = attribute(node, 'href');
  if (address === null) return <span key={key}>{inside}</span>;
  // A bare address that markdown-it linked has decoded text and an encoded address.
  const text = textOf(node).trim();
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
  const alt = attribute(node, 'alt')?.trim() ?? '';
  const source = attribute(node, 'src') ?? '';
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
