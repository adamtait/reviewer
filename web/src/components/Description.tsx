// SPDX-License-Identifier: MIT

import { repositoryUrl } from '../github/links';
import { Markdown } from './Markdown';

// A pull request's description is written by its author, who is not trusted.
// It is drawn as GitHub draws it, under the rules in Markdown.tsx: HTML only
// through GitHub's allowlist, no images loaded, and no link that can be
// followed unless it leads to this workspace or to the GitHub it reads from.
// As on GitHub, a line break in a paragraph is drawn as a break.

export function Description({ body, github }: { body: string; github?: string | undefined }) {
  if (body.trim() === '') {
    return <p className="description description-empty">No description.</p>;
  }
  return (
    <div className="description" data-testid="description">
      <Markdown text={body} trusted={github ? [github] : []} breaks repository={github ? repositoryUrl(github) : undefined} />
    </div>
  );
}
