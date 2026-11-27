// SPDX-License-Identifier: MIT

import { useMemo } from 'react';

import { repositoryUrl } from '../github/links';
import { Markdown } from './Markdown';
import { diffMarkdown } from './markdownDiff';

// Two versions of a Markdown file, drawn, with what the pull request changes
// marked block by block (see markdownDiff.ts). Each block is drawn under the
// rules in Markdown.tsx, as a description is.

const said = { added: 'Added: ', removed: 'Removed: ' } as const;

export function RenderedDiff({ oldText, newText, github }: { oldText: string; newText: string; github?: string | undefined }) {
  const { parts, added, removed } = useMemo(() => diffMarkdown(oldText, newText), [oldText, newText]);
  const trusted = github ? [github] : [];
  const repository = github ? repositoryUrl(github) : undefined;
  return (
    <div className="rendered-diff" data-testid="rendered-diff">
      <p className="rendered-diff-summary muted">
        {added === 0 && removed === 0
          ? 'Drawn, the file looks the same: the change is in its text. Turn “Rendered” off to see it.'
          : `${count(removed, 'block')} removed, ${count(added, 'block')} added.`}
      </p>
      {parts.map((part, i) => (
        <div key={i} className={`md-block md-${part.kind}${part.item ? ' md-item' : ''}`} data-change={part.kind}>
          {part.kind === 'same' ? null : <span className="visually-hidden">{said[part.kind]}</span>}
          <Markdown text={part.text} trusted={trusted} repository={repository} />
        </div>
      ))}
    </div>
  );
}

function count(n: number, noun: string): string {
  return `${String(n)} ${noun}${n === 1 ? '' : 's'}`;
}
