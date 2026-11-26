// SPDX-License-Identifier: MIT

import { useEffect, useState } from 'react';

import { ApiFailure, api } from '../api/client';
import type { Me, PullList as Pulls, RepositoryRef } from '../api/types';
import { Link } from '../components/Link';
import { InlineMarkdown } from '../components/Markdown';
import { href } from '../router';

type RepoState = { phase: 'loading' } | { phase: 'ready'; list: Pulls } | { phase: 'failed'; message: string };

const day = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' });

function RepositoryPulls({ repository }: { repository: RepositoryRef }) {
  const [state, setState] = useState<RepoState>({ phase: 'loading' });
  const { owner, name } = repository;

  useEffect(() => {
    const controller = new AbortController();
    api
      .pulls(owner, name, controller.signal)
      .then((list) => {
        setState({ phase: 'ready', list });
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        setState({ phase: 'failed', message: error instanceof ApiFailure ? error.message : 'The list could not be read.' });
      });
    return () => {
      controller.abort();
    };
  }, [owner, name]);

  return (
    <section className="repository" aria-label={`${owner}/${name}`}>
      <h2>
        {owner}/{name}
      </h2>
      {state.phase === 'loading' ? <p className="muted">Reading from GitHub…</p> : null}
      {state.phase === 'failed' ? <p className="error">{state.message}</p> : null}
      {state.phase === 'ready' && state.list.pulls.length === 0 ? <p className="muted">No open pull requests.</p> : null}
      {state.phase === 'ready' && state.list.pulls.length > 0 ? (
        <ul className="pull-list">
          {state.list.pulls.map((pull) => (
            <li key={pull.number}>
              <Link to={href({ view: 'pull', owner, repo: name, number: pull.number })}>
                <span className="pull-title">
                  <InlineMarkdown text={pull.title} /> <span className="muted">#{pull.number}</span>
                </span>
                <span className="pull-meta muted">
                  {pull.draft ? <span className="badge">Draft</span> : null} {pull.author}, updated{' '}
                  {day.format(new Date(pull.updatedAt))}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      ) : null}
      {state.phase === 'ready' && state.list.truncated ? (
        <p className="muted">Only the {state.list.pulls.length} most recently updated are listed.</p>
      ) : null}
    </section>
  );
}

/** The open pull requests of every connected repository this user can read. */
export function PullList({ me }: { me: Me }) {
  return (
    <main className="pull-lists">
      <h1>Open pull requests</h1>
      {me.repositories.length === 0 ? (
        <p className="muted">Your token cannot read any repository connected to this workspace.</p>
      ) : (
        me.repositories.map((repository) => (
          <RepositoryPulls key={`${repository.owner}/${repository.name}`} repository={repository} />
        ))
      )}
    </main>
  );
}
