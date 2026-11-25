// SPDX-License-Identifier: MIT

import { type SyntheticEvent, useEffect, useRef, useState } from 'react';

import { ApiFailure, api } from '../api/client';
import type { SearchResult } from '../api/types';
import { href } from '../router';
import { Link } from './Link';

export interface SearchPanelProps {
  owner: string;
  repo: string;
  /** The commit searched: the whole repository at this commit, not only the changed files. */
  sha: string;
  /** The pull request the search was opened from, so a result can lead back to it. */
  pull?: number | undefined;
  onClose: () => void;
}

type State =
  | { phase: 'idle' }
  | { phase: 'searching' }
  | { phase: 'done'; query: string; result: SearchResult }
  | { phase: 'failed'; message: string };

/** Counts characters the way the service does: by code point, not by UTF-16 unit. */
function characters(text: string): number {
  return Array.from(text).length;
}

/** Literal text search over one commit. */
export function SearchPanel({ owner, repo, sha, pull, onClose }: SearchPanelProps) {
  const [query, setQuery] = useState('');
  const [pathPrefix, setPathPrefix] = useState('');
  const [caseSensitive, setCaseSensitive] = useState(false);
  const [state, setState] = useState<State>({ phase: 'idle' });
  const input = useRef<HTMLInputElement>(null);
  const running = useRef<AbortController | null>(null);

  useEffect(() => {
    input.current?.focus();
    return () => running.current?.abort();
  }, []);

  const tooShort = characters(query) < 2;

  const submit = (event: SyntheticEvent) => {
    event.preventDefault();
    if (tooShort) return;
    running.current?.abort();
    const controller = new AbortController();
    running.current = controller;
    setState({ phase: 'searching' });
    api
      .search(owner, repo, sha, { query, pathPrefix, caseSensitive }, controller.signal)
      .then((result) => {
        setState({ phase: 'done', query, result });
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        setState({ phase: 'failed', message: error instanceof ApiFailure ? error.message : 'The search failed.' });
      });
  };

  return (
    <section className="search-panel" aria-label="Search code">
      <form onSubmit={submit} role="search">
        <label>
          <span className="label">Search this commit</span>
          <input
            ref={input}
            type="search"
            value={query}
            placeholder="Text to find, matched exactly"
            autoComplete="off"
            spellCheck={false}
            onChange={(event) => {
              setQuery(event.target.value);
            }}
          />
        </label>
        <label>
          <span className="label">Under path</span>
          <input
            type="text"
            value={pathPrefix}
            placeholder="src/"
            autoComplete="off"
            spellCheck={false}
            onChange={(event) => {
              setPathPrefix(event.target.value);
            }}
          />
        </label>
        <label className="check">
          <input
            type="checkbox"
            checked={caseSensitive}
            onChange={(event) => {
              setCaseSensitive(event.target.checked);
            }}
          />
          Match case
        </label>
        <div className="row">
          <button type="submit" disabled={tooShort}>
            Search
          </button>
          <button type="button" onClick={onClose}>
            Close
          </button>
        </div>
      </form>

      <div className="search-results" aria-live="polite">
        {state.phase === 'idle' && tooShort ? <p className="muted">Type at least 2 characters.</p> : null}
        {state.phase === 'searching' ? <p className="muted">Searching…</p> : null}
        {state.phase === 'failed' ? <p className="error">{state.message}</p> : null}
        {state.phase === 'done' ? (
          <>
            <p className="muted">
              {state.result.matches.length === 0
                ? `Nothing contains “${state.query}”.`
                : `${state.result.matches.length} matching ${state.result.matches.length === 1 ? 'line' : 'lines'}, from text search.`}
              {state.result.truncated ? ' The search stopped at its limit; there are more.' : ''}
            </p>
            <ol>
              {state.result.matches.map((match) => (
                <li key={`${match.path}:${match.line}`}>
                  <Link
                    to={href({ view: 'code', owner, repo, sha, path: match.path, line: match.line + 1, ...(pull ? { pull } : {}) })}
                  >
                    <span className="match-place">
                      {match.path}:{match.line + 1}
                    </span>
                    <code className="match-text">{match.text}</code>
                  </Link>
                </li>
              ))}
            </ol>
          </>
        ) : null}
      </div>
    </section>
  );
}
