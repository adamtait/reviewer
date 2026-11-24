// SPDX-License-Identifier: MIT

import { useCallback, useEffect, useState } from 'react';

import { api, isSignedOut, setSignedOutListener } from './api/client';
import type { Me } from './api/types';
import { Link } from './components/Link';
import { navigate, parse, safeNext, useAddress } from './router';
import { CodeView } from './views/CodeView';
import { NotFound } from './views/NotFound';
import { PullList } from './views/PullList';
import { PullView } from './views/PullView';
import { SignIn } from './views/SignIn';

type Session = { phase: 'loading' } | { phase: 'in'; me: Me } | { phase: 'out' } | { phase: 'failed' };

export function App() {
  const address = useAddress();
  const url = new URL(address);
  const place = parse(url);
  const [session, setSession] = useState<Session>({ phase: 'loading' });

  const load = useCallback(() => {
    api
      .me()
      .then((me) => {
        setSession({ phase: 'in', me });
      })
      .catch((error: unknown) => {
        setSession(isSignedOut(error) ? { phase: 'out' } : { phase: 'failed' });
      });
  }, []);

  useEffect(() => {
    // Whichever call finds the session gone, the whole page learns of it here.
    setSignedOutListener(() => {
      setSession({ phase: 'out' });
    });
    load();
  }, [load]);

  if (session.phase === 'loading') return <p className="pad muted">Loading…</p>;

  if (session.phase === 'failed') {
    return (
      <main className="pad">
        <p className="error">The workspace could not be reached.</p>
        <button
          type="button"
          onClick={() => {
            setSession({ phase: 'loading' });
            load();
          }}
        >
          Try again
        </button>
      </main>
    );
  }

  if (session.phase === 'out') {
    // The address stays as it is, so signing in lands where the link pointed.
    const next = place.view === 'signin' ? place.next : safeNext(url.pathname + url.search + url.hash);
    return (
      <SignIn
        onSignedIn={() => {
          setSession({ phase: 'loading' });
          load();
          if (place.view === 'signin') navigate(next, { replace: true });
        }}
      />
    );
  }

  if (place.view === 'signin') {
    // Already signed in: there is nothing to do on this page.
    queueMicrotask(() => {
      navigate(place.next, { replace: true });
    });
    return null;
  }

  // Whether the repository on screen has an assistant: the Ask controls are
  // shown only where a question can be answered.
  const repository = place.view === 'pull' || place.view === 'code' ? place : null;
  const assistant =
    repository !== null &&
    session.me.repositories.some(
      (r) => r.assistant && r.owner.toLowerCase() === repository.owner.toLowerCase() && r.name.toLowerCase() === repository.repo.toLowerCase(),
    );

  return (
    <>
      <header className="app-header">
        <Link to="/" className="app-name">
          Review workspace
        </Link>
        <span className="muted who">{session.me.login}</span>
        <button
          type="button"
          onClick={() => {
            void api.signOut().finally(() => {
              setSession({ phase: 'out' });
              navigate('/', { replace: true });
            });
          }}
        >
          Sign out
        </button>
      </header>
      {place.view === 'list' ? <PullList me={session.me} /> : null}
      {place.view === 'pull' ? (
        <PullView
          key={`${place.owner}/${place.repo}#${place.number}`}
          owner={place.owner}
          repo={place.repo}
          number={place.number}
          file={place.file}
          line={place.line}
          assistant={assistant}
        />
      ) : null}
      {place.view === 'code' ? (
        <CodeView
          owner={place.owner}
          repo={place.repo}
          sha={place.sha}
          path={place.path}
          line={place.line}
          pull={place.pull}
          assistant={assistant}
        />
      ) : null}
      {place.view === 'notfound' ? <NotFound /> : null}
    </>
  );
}
