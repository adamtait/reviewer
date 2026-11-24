// SPDX-License-Identifier: MIT

// A pull request's description is written by its author, who is not trusted.
// It is shown as text: no HTML, no images, no links to click. Markdown drawing
// arrives with the assistant, with raw HTML off.

export function Description({ body }: { body: string }) {
  if (body.trim() === '') {
    return <p className="description description-empty">No description.</p>;
  }
  return (
    <div className="description" data-testid="description">
      {body}
    </div>
  );
}
