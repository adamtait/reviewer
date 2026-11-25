// SPDX-License-Identifier: MIT

import MarkdownIt, { type Token } from 'markdown-it';
import { type ReactNode, createElement, useMemo } from 'react';

// Markdown from people and models that are not trusted: a pull request's
// description, and an assistant's answer, which can repeat whatever the model
// read.
//
// The text is parsed into tokens and the tokens are drawn as React elements
// from a fixed list. No HTML string is ever built, so there is nothing to
// inject into. Raw HTML in the text is shown as the characters it is. Images
// are not loaded: an image address is a request to another server, which
// would tell that server who is reading. A link is shown as its text and its
// full address, and can be clicked only if it leads to this workspace or to
// the GitHub it reads from.

const parser = new MarkdownIt({ html: false, linkify: false, typographer: false });
// With the rule off, `![alt](address)` stays as the text the author typed.
parser.disable(['image']);

/** Block and inline tags the tokens may become. Anything else is drawn as its text. */
const tags: Record<string, string> = {
  p: 'p',
  ul: 'ul',
  ol: 'ol',
  li: 'li',
  blockquote: 'blockquote',
  table: 'table',
  thead: 'thead',
  tbody: 'tbody',
  tr: 'tr',
  th: 'th',
  td: 'td',
  em: 'em',
  strong: 'strong',
  s: 's',
  // Headings are kept below the page's own, so that a description cannot
  // restructure the page's outline.
  h1: 'h3',
  h2: 'h4',
  h3: 'h5',
  h4: 'h6',
  h5: 'h6',
  h6: 'h6',
};

export interface MarkdownProps {
  text: string;
  /**
   * Origins a link may lead to and still be clickable: this workspace, and the
   * GitHub it reads from. A link anywhere else is shown and cannot be followed.
   */
  trusted?: readonly string[];
}

export function Markdown({ text, trusted = [] }: MarkdownProps) {
  const tokens = useMemo(() => parser.parse(text, {}), [text]);
  const origins = useMemo(() => new Set([window.location.origin, ...trusted.map(originOf).filter((o) => o !== '')]), [trusted]);
  return <div className="markdown">{draw(tokens, origins, 'm')}</div>;
}

function originOf(address: string): string {
  try {
    return new URL(address).origin;
  } catch {
    return '';
  }
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

function draw(tokens: readonly Token[], origins: ReadonlySet<string>, prefix: string): ReactNode[] {
  const out: ReactNode[] = [];
  for (let i = 0; i < tokens.length; i++) {
    const token = tokens[i];
    if (!token) continue;
    const key = `${prefix}.${i}`;

    if (token.nesting === 1) {
      // Find the token that closes this one, and draw what lies between.
      let depth = 1;
      let end = i + 1;
      while (end < tokens.length && depth > 0) {
        depth += tokens[end]?.nesting ?? 0;
        end++;
      }
      const inside = draw(tokens.slice(i + 1, end - 1), origins, key);
      out.push(open(token, inside, origins, key));
      i = end - 1;
      continue;
    }
    if (token.nesting === -1) continue; // a close with no open: nothing to draw

    switch (token.type) {
      case 'inline':
        out.push(...draw(token.children ?? [], origins, key));
        break;
      case 'text':
        out.push(token.content);
        break;
      case 'softbreak':
        out.push('\n');
        break;
      case 'hardbreak':
        out.push(<br key={key} />);
        break;
      case 'code_inline':
        out.push(<code key={key}>{token.content}</code>);
        break;
      case 'fence':
      case 'code_block':
        // Whatever the language named, the block is shown as text. A diagram's
        // source is its source; nothing draws it.
        out.push(
          <pre key={key} data-language={token.info.trim() || undefined}>
            <code>{token.content}</code>
          </pre>,
        );
        break;
      case 'hr':
        out.push(<hr key={key} />);
        break;
      default:
        // A kind of token this list does not know. Its text is shown, as text.
        if (token.content) out.push(token.content);
    }
  }
  return out;
}

function open(token: Token, inside: ReactNode[], origins: ReadonlySet<string>, key: string): ReactNode {
  if (token.type === 'link_open') {
    const address = String(token.attrGet('href') ?? '');
    const shown = (
      <>
        {inside}
        <span className="link-address"> ({address})</span>
      </>
    );
    if (followable(address, origins)) {
      return (
        <a key={key} href={address} target="_blank" rel="noopener noreferrer">
          {shown}
        </a>
      );
    }
    return (
      <span key={key} className="link-text">
        {shown}
      </span>
    );
  }
  const tag = tags[token.tag];
  if (!tag) return <span key={key}>{inside}</span>;
  return createElement(tag, { key }, ...inside);
}
