// SPDX-License-Identifier: MIT

import { repositoryUrl } from '../github/links';
import { Markdown } from './Markdown';

// A Markdown file in the repository, drawn for reading. Its author is no more
// trusted than a pull request's, so it goes through the same rules in
// Markdown.tsx: HTML only through GitHub's allowlist, no images loaded, and no
// link that can be followed unless it leads to this workspace or to the GitHub
// it reads from. As GitHub draws a file, a line break in a paragraph is not a
// break.

// The extensions GitHub draws as Markdown. MDX is left out: it is JSX, and
// most of what is in it would be drawn as text.
const extensions = /\.(md|markdown|mdown|mkdn|mkd|mdwn)$/i;

/** Whether a file at this path is Markdown, judged as GitHub judges it: by its name. */
export function isMarkdown(path: string): boolean {
  return extensions.test(path);
}

export function MarkdownFile({ text, github }: { text: string; github?: string | undefined }) {
  return (
    <article className="markdown-file" data-testid="markdown-file">
      <Markdown text={text} trusted={github ? [github] : []} repository={github ? repositoryUrl(github) : undefined} />
    </article>
  );
}
