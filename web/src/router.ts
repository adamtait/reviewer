// SPDX-License-Identifier: MIT

import { useSyncExternalStore } from 'react';

// Every place in the application has its own address, so the browser's back
// and forward buttons, a reload and a pasted link all work. The two repository
// addresses mirror GitHub's own paths.
//
// Line numbers in an address count from 1, as they are shown beside the code.
// Everywhere else in the application they count from 0.

export type Place =
  | { view: 'list' }
  | { view: 'signin'; next: string }
  | { view: 'pull'; owner: string; repo: string; number: number; file?: string; line?: number }
  | { view: 'code'; owner: string; repo: string; sha: string; path: string; line?: number; pull?: number }
  | { view: 'notfound' };

const fullHash = /^[0-9a-f]{40}$/;
const pullNumber = /^[1-9][0-9]*$/;
const lineNumber = /^[1-9][0-9]{0,8}$/;

function segments(pathname: string): string[] | null {
  try {
    return pathname.split('/').filter((s) => s !== '').map(decodeURIComponent);
  } catch {
    return null;
  }
}

/** Reads an address into a place. Anything it does not recognise is "not found". */
export function parse(url: URL): Place {
  const parts = segments(url.pathname);
  if (parts === null) return { view: 'notfound' };

  if (parts.length === 0) return { view: 'list' };
  if (parts.length === 1 && parts[0] === 'signin') {
    return { view: 'signin', next: safeNext(url.searchParams.get('next')) };
  }

  const [owner, repo, kind, ...rest] = parts;
  if (owner === undefined || repo === undefined) return { view: 'notfound' };

  if (kind === 'pull' && rest.length === 1 && pullNumber.test(rest[0] ?? '')) {
    const place: Place = { view: 'pull', owner, repo, number: Number(rest[0]) };
    const hash = new URLSearchParams(url.hash.replace(/^#/, ''));
    const file = hash.get('file');
    const line = hash.get('line');
    if (file) place.file = file;
    if (line && lineNumber.test(line)) place.line = Number(line);
    return place;
  }

  if (kind === 'blob' && rest.length >= 2 && fullHash.test(rest[0] ?? '')) {
    const place: Place = { view: 'code', owner, repo, sha: rest[0] ?? '', path: rest.slice(1).join('/') };
    const line = /^#L([1-9][0-9]{0,8})$/.exec(url.hash);
    if (line) place.line = Number(line[1]);
    const pull = url.searchParams.get('pull');
    if (pull && pullNumber.test(pull)) place.pull = Number(pull);
    return place;
  }

  return { view: 'notfound' };
}

/**
 * Where to go after signing in. Only a path on this site is accepted: a full
 * address here would let a crafted link send a newly signed-in user elsewhere.
 */
export function safeNext(next: string | null): string {
  if (!next || !next.startsWith('/') || next.startsWith('//') || next.includes('\\')) return '/';
  if (next === '/signin' || next.startsWith('/signin?')) return '/';
  return next;
}

const encodePath = (path: string) => path.split('/').map(encodeURIComponent).join('/');

/** Writes a place as an address. */
export function href(place: Place): string {
  switch (place.view) {
    case 'list':
      return '/';
    case 'signin':
      return place.next === '/' ? '/signin' : `/signin?next=${encodeURIComponent(place.next)}`;
    case 'pull': {
      const base = `/${encodeURIComponent(place.owner)}/${encodeURIComponent(place.repo)}/pull/${place.number}`;
      if (!place.file) return base;
      const hash = new URLSearchParams({ file: place.file });
      if (place.line) hash.set('line', String(place.line));
      return `${base}#${hash.toString()}`;
    }
    case 'code': {
      const base = `/${encodeURIComponent(place.owner)}/${encodeURIComponent(place.repo)}/blob/${place.sha}/${encodePath(place.path)}`;
      const query = place.pull ? `?pull=${place.pull}` : '';
      const hash = place.line ? `#L${place.line}` : '';
      return `${base}${query}${hash}`;
    }
    case 'notfound':
      return '/';
  }
}

const changed = 'workspace:navigate';

/** Goes to an address on this site, as a new entry in the browser's history unless replacing. */
export function navigate(to: string, options: { replace?: boolean } = {}): void {
  if (options.replace) {
    window.history.replaceState(null, '', to);
  } else {
    window.history.pushState(null, '', to);
  }
  window.dispatchEvent(new Event(changed));
}

function subscribe(listener: () => void): () => void {
  window.addEventListener('popstate', listener);
  window.addEventListener('hashchange', listener);
  window.addEventListener(changed, listener);
  return () => {
    window.removeEventListener('popstate', listener);
    window.removeEventListener('hashchange', listener);
    window.removeEventListener(changed, listener);
  };
}

/** The current address. It changes on navigation, back, forward and a change of fragment. */
export function useAddress(): string {
  return useSyncExternalStore(subscribe, () => window.location.href);
}

/** Whether a click on a link should be left to the browser: a new tab, a download, another button. */
export function isPlainClick(event: { button: number; metaKey: boolean; ctrlKey: boolean; shiftKey: boolean; altKey: boolean }): boolean {
  return event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey;
}
