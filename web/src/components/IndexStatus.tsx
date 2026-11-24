// SPDX-License-Identifier: MIT

import { useEffect, useState } from 'react';

import { ApiFailure, api } from '../api/client';
import type { IndexState } from '../api/types';

/** How often the page asks again while CI is still building the index. */
export const recheckMs = 15_000;

type State = { phase: 'asking' } | { phase: 'known'; index: IndexState } | { phase: 'failed'; message: string };

/**
 * Says whether the commit has a navigation index, in one of four states.
 *
 * While CI is still building it the page asks again every 15 seconds, so
 * navigation becomes available without a reload. Whatever the state, the diff
 * and text search work: they come from the commit's source, not from CI.
 */
export function IndexStatus({ owner, repo, sha }: { owner: string; repo: string; sha: string }) {
  const [state, setState] = useState<State>({ phase: 'asking' });

  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;

    const ask = () => {
      api
        .indexState(owner, repo, sha, controller.signal)
        .then((index) => {
          setState({ phase: 'known', index });
          if (index.state === 'running') timer = setTimeout(ask, recheckMs);
        })
        .catch((error: unknown) => {
          if (controller.signal.aborted) return;
          setState({ phase: 'failed', message: error instanceof ApiFailure ? error.message : 'The index could not be checked.' });
        });
    };
    setState({ phase: 'asking' });
    ask();
    return () => {
      controller.abort();
      if (timer !== undefined) clearTimeout(timer);
    };
  }, [owner, repo, sha]);

  return (
    <p className="index-status muted" data-testid="index-status" aria-live="polite">
      {text()}
    </p>
  );

  function text() {
    if (state.phase === 'asking') return 'Checking for a navigation index…';
    if (state.phase === 'failed') return `Navigation index: ${state.message} Lookups use text search.`;

    const { index } = state;
    const run = index.runUrl ? (
      <a href={index.runUrl} target="_blank" rel="noopener noreferrer">
        the run on GitHub
      </a>
    ) : null;
    switch (index.state) {
      case 'ready':
        return (
          <>
            Navigation uses the index CI built for this commit ({index.documents} {index.documents === 1 ? 'file' : 'files'}
            {index.toolVersion ? `, scip-typescript ${index.toolVersion}` : ''}). Results from it are labelled, and are not
            checked.
          </>
        );
      case 'running':
        return <>CI is still building the index for this commit. Lookups use text search until it finishes. See {run}.</>;
      case 'failed':
        return <>The index job failed for this commit. Lookups use text search. See {run}.</>;
      case 'none':
        return (
          <>
            No index for this commit. Lookups use text search.
            {index.reason ? ` ${capitalise(index.reason)}.` : ''}
            {index.reason && run ? <> See {run}.</> : null}
          </>
        );
    }
  }
}

function capitalise(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}
