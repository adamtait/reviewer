// SPDX-License-Identifier: MIT

// The files a reader has closed on a pull request's page.
//
// They are kept in the browser tab's session storage, one entry for each pull
// request, so that going to a definition and coming back, or reloading, does
// not open them all again. A new tab starts with every file open. Nothing is
// sent to the service.

const prefix = 'review-workspace:closed:';

function key(owner: string, repo: string, number: number): string {
  return `${prefix}${owner}/${repo}#${String(number)}`;
}

/** The paths closed on this pull request's page, or none if nothing was kept. */
export function readClosed(owner: string, repo: string, number: number): Set<string> {
  try {
    const kept = sessionStorage.getItem(key(owner, repo, number));
    if (kept === null) return new Set();
    const parsed: unknown = JSON.parse(kept);
    if (!Array.isArray(parsed)) return new Set();
    return new Set(parsed.filter((path): path is string => typeof path === 'string'));
  } catch {
    // Storage turned off, or an entry that is not ours: every file is open.
    return new Set();
  }
}

/** Keeps the paths closed on this pull request's page, dropping the entry when none are. */
export function writeClosed(owner: string, repo: string, number: number, closed: ReadonlySet<string>): void {
  try {
    if (closed.size === 0) sessionStorage.removeItem(key(owner, repo, number));
    else sessionStorage.setItem(key(owner, repo, number), JSON.stringify([...closed]));
  } catch {
    // Storage full or turned off: the files stay closed while the page is open.
  }
}
