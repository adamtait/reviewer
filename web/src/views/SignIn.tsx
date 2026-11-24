// SPDX-License-Identifier: MIT

import { type SyntheticEvent, useState } from 'react';

import { ApiFailure, api } from '../api/client';

// Each refusal has a different fix, so each is explained in its own words.
const refusals: Record<string, { title: string; fix: string }> = {
  not_fine_grained: {
    title: 'That is not a fine-grained token.',
    fix: 'Create a fine-grained personal access token; its name starts with github_pat_. Classic tokens are not accepted, because one that reads private code can also write to every repository you can reach.',
  },
  rejected: {
    title: 'GitHub did not accept that token.',
    fix: 'It may have expired, been revoked, or been copied incompletely. Create a new one and paste it again.',
  },
  no_repository: {
    title: 'That token cannot read any repository connected to this workspace.',
    fix: 'Check that the token names the connected repositories and has the four permissions below. In an organisation, an owner may need to approve the token first; until then it reads nothing private.',
  },
  github_failed: {
    title: 'GitHub could not be asked.',
    fix: 'This is not a problem with your token. Try again in a moment.',
  },
};

const permissions: { name: string; why: string }[] = [
  { name: 'Contents', why: 'to read the code at the pull request’s commit' },
  { name: 'Pull requests', why: 'to list pull requests and read their descriptions and changed files' },
  { name: 'Metadata', why: 'to confirm you can read the repository; GitHub selects it for you' },
  { name: 'Actions', why: 'to fetch the navigation index that CI builds' },
];

export function SignIn({ onSignedIn }: { onSignedIn: () => void }) {
  const [token, setToken] = useState('');
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<{ title: string; fix: string } | null>(null);

  const submit = (event: SyntheticEvent) => {
    event.preventDefault();
    if (token.trim() === '' || busy) return;
    setBusy(true);
    setRefusal(null);
    api
      .signIn(token.trim())
      .then(() => {
        setToken('');
        onSignedIn();
      })
      .catch((error: unknown) => {
        const code = error instanceof ApiFailure ? error.code : '';
        setRefusal(
          refusals[code] ?? {
            title: 'Sign-in failed.',
            fix: error instanceof ApiFailure ? error.message : 'The workspace could not be reached.',
          },
        );
      })
      .finally(() => {
        setBusy(false);
      });
  };

  return (
    <main className="signin">
      <h1>Sign in</h1>
      <p>
        The workspace reads GitHub as you, with a token you create. It shows you a repository only if GitHub lets you
        read it, and it never writes to GitHub.
      </p>

      <form onSubmit={submit}>
        <label>
          <span className="label">Fine-grained personal access token</span>
          <input
            type="password"
            name="token"
            value={token}
            autoComplete="off"
            spellCheck={false}
            placeholder="github_pat_…"
            onChange={(event) => {
              setToken(event.target.value);
            }}
          />
        </label>
        <button type="submit" disabled={busy || token.trim() === ''}>
          {busy ? 'Checking with GitHub…' : 'Sign in'}
        </button>
      </form>

      {refusal ? (
        <div className="refusal" role="alert">
          <strong>{refusal.title}</strong>
          <p>{refusal.fix}</p>
        </div>
      ) : null}

      <h2>Creating a token</h2>
      <ol>
        <li>On GitHub, open Settings, Developer settings, Personal access tokens, Fine-grained tokens.</li>
        <li>Set the resource owner to the owner of the repositories you review here.</li>
        <li>Choose “Only select repositories” and pick them.</li>
        <li>
          Under repository permissions, set these four to <strong>Read-only</strong> and leave the rest at “No access”:
          <ul>
            {permissions.map((permission) => (
              <li key={permission.name}>
                <strong>{permission.name}</strong>, {permission.why}.
              </li>
            ))}
          </ul>
        </li>
      </ol>
      <p className="muted">
        The token is sealed into a cookie in this browser and is not stored by the workspace. Signing out clears the
        cookie here; revoking the token on GitHub ends every session that uses it.
      </p>
    </main>
  );
}
