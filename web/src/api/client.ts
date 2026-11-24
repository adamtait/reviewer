// SPDX-License-Identifier: MIT

import type { ApiError, File, Me, PullList, PullView, Search, SearchResult, Tree } from './types';

/** A response that was not a success. */
export class ApiFailure extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'ApiFailure';
    this.status = status;
    this.code = code;
  }
}

/** Whether a failure means the user has no session. */
export function isSignedOut(e: unknown): boolean {
  return e instanceof ApiFailure && e.code === 'signed_out';
}

/** Whether a failure means "not there, or not yours to see". The two are never told apart. */
export function isNotFound(e: unknown): boolean {
  return e instanceof ApiFailure && e.code === 'not_found';
}

type SignedOutListener = () => void;
let onSignedOut: SignedOutListener = () => undefined;

/** Registers the one place that reacts to a lost session, whichever call found it. */
export function setSignedOutListener(listener: SignedOutListener): void {
  onSignedOut = listener;
}

async function call<T>(method: 'GET' | 'POST', path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (method !== 'GET') {
    // The service refuses a non-GET without this header. Another site's page
    // cannot add it, which is the point.
    headers['X-Workspace-Request'] = '1';
  }
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
  }
  const init: RequestInit = { method, headers, credentials: 'same-origin' };
  if (body !== undefined) init.body = JSON.stringify(body);
  if (signal) init.signal = signal;

  const response = await fetch(path, init);
  if (response.status === 204) {
    return undefined as T;
  }
  let parsed: unknown;
  try {
    parsed = await response.json();
  } catch {
    throw new ApiFailure(response.status, 'internal', 'The workspace sent a response that could not be read.');
  }
  if (!response.ok) {
    const failure = parsed as Partial<ApiError>;
    const error = new ApiFailure(
      response.status,
      failure.error?.code ?? 'internal',
      failure.error?.message ?? 'The request failed.',
    );
    if (error.code === 'signed_out') onSignedOut();
    throw error;
  }
  return parsed as T;
}

const repo = (owner: string, name: string) =>
  `/api/v1/repos/${encodeURIComponent(owner)}/${encodeURIComponent(name)}`;

export const api = {
  me: (signal?: AbortSignal) => call<Me>('GET', '/api/v1/me', undefined, signal),

  signIn: (token: string) => call<undefined>('POST', '/auth/token', { token }),

  signOut: () => call<undefined>('POST', '/auth/signout'),

  pulls: (owner: string, name: string, signal?: AbortSignal) =>
    call<PullList>('GET', `${repo(owner, name)}/pulls`, undefined, signal),

  pull: (owner: string, name: string, number: number, signal?: AbortSignal) =>
    call<PullView>('GET', `${repo(owner, name)}/pulls/${number}`, undefined, signal),

  tree: (owner: string, name: string, sha: string, signal?: AbortSignal) =>
    call<Tree>('GET', `${repo(owner, name)}/commits/${sha}/tree`, undefined, signal),

  file: (owner: string, name: string, sha: string, path: string, signal?: AbortSignal) =>
    call<File>('GET', `${repo(owner, name)}/commits/${sha}/file?path=${encodeURIComponent(path)}`, undefined, signal),

  // The query travels in the body, so what was searched for is not in an address.
  search: (owner: string, name: string, sha: string, query: Search, signal?: AbortSignal) =>
    call<SearchResult>('POST', `${repo(owner, name)}/commits/${sha}/search`, query, signal),
};

export type Api = typeof api;
