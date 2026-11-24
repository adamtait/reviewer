// SPDX-License-Identifier: MIT

// Links out to GitHub. Commenting, approving and merging stay on GitHub; the
// workspace takes the user to the right place and does not write there itself.
//
// Every link is built on the pull request's `webUrl`, which the service builds
// from its configuration. Nothing here uses an address that arrived as data
// from a repository.

/** A line of the new file, or a line the pull request removed. */
export type Side = 'new' | 'old';

async function sha256Hex(text: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(text));
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, '0')).join('');
}

/**
 * The address of a line on GitHub's "Files changed" page, where a comment is
 * written. `line` counts from 0, as everywhere in this application; GitHub's
 * anchor counts from 1.
 *
 * GitHub does not document this anchor: the SHA-256 of the file's path, then R
 * and a line of the new file, or L and a line of the old. If GitHub changes
 * it, the link still opens the pull request's files page.
 */
export async function commentUrl(webUrl: string, path: string, line: number, side: Side): Promise<string> {
  const anchor = await sha256Hex(path);
  return `${webUrl}/files#diff-${anchor}${side === 'new' ? 'R' : 'L'}${line + 1}`;
}

/** The repository's address on GitHub, from a pull request's. */
export function repositoryUrl(webUrl: string): string {
  return webUrl.replace(/\/pull\/[0-9]+$/, '');
}

/** A file at a commit on GitHub. `line`, if given, counts from 0. */
export function blobUrl(webUrl: string, sha: string, path: string, line?: number): string {
  const encoded = path.split('/').map(encodeURIComponent).join('/');
  const anchor = line === undefined ? '' : `#L${line + 1}`;
  return `${repositoryUrl(webUrl)}/blob/${sha}/${encoded}${anchor}`;
}

/** Opens a GitHub address in a new tab that cannot reach back into this page. */
export function openOnGitHub(url: string): void {
  window.open(url, '_blank', 'noopener,noreferrer');
}
