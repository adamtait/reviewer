// SPDX-License-Identifier: MIT

import { Markdown } from './Markdown';

// A pull request's description is written by its author, who is not trusted.
// It is drawn as Markdown under the rules in Markdown.tsx: no HTML, no images,
// and no link that can be followed unless it leads to this workspace or to
// the GitHub it reads from.

export function Description({ body, github }: { body: string; github?: string | undefined }) {
  if (body.trim() === '') {
    return <p className="description description-empty">No description.</p>;
  }
  return (
    <div className="description" data-testid="description">
      <Markdown text={body} trusted={github ? [github] : []} />
    </div>
  );
}
