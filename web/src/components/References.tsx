// SPDX-License-Identifier: MIT

import { useState } from 'react';

import type { Definition, Label, LookupKind, Match, References } from '../api/types';
import { type Arrival, type Context, type Origin, goTo, labelText, sendReport, targetHref } from '../nav/navigation';
import { isPlainClick } from '../router';

export type LookupResult = { kind: 'definition'; result: Definition } | { kind: 'references'; result: References };

export interface LookupResultsProps {
  context: Context;
  origin: Origin;
  lookup: LookupResult;
  onClose: () => void;
}

/**
 * What a lookup found, when it is not followed straight away: several
 * definitions, a list of references, text matches, a package outside the
 * repository, or nothing.
 *
 * Each list is headed by where it came from. A list from text search is lines
 * that contain the word, and is never presented as definitions or as the
 * complete set of uses.
 */
export function LookupResults({ context, origin, lookup, onClose }: LookupResultsProps) {
  const how = (label: Label): Arrival => ({
    label,
    kind: lookup.kind,
    origin: { sha: context.sha, path: origin.path, line: origin.line, character: origin.character },
  });

  const places = (label: Label, list: readonly { path: string; line: number; text?: string }[]) => (
    <ol className="lookup-list">
      {list.map((at) => (
        <li key={`${at.path}:${at.line}:${at.text ?? ''}`}>
          <a
            href={targetHref(context, at)}
            onClick={(event) => {
              if (!isPlainClick(event)) return;
              event.preventDefault();
              goTo(context, at, how(label));
            }}
          >
            <span className="match-place">
              {at.path}:{at.line + 1}
            </span>
            {at.text !== undefined ? <code className="match-text">{at.text}</code> : null}
          </a>
        </li>
      ))}
    </ol>
  );

  const textMatches = (matches: Match[], truncated: boolean, note: string) => (
    <section aria-label={labelText.text_search}>
      <h3 className="label label-text_search">{labelText.text_search}</h3>
      <p className="muted">{note}</p>
      {matches.length === 0 ? <p className="muted">No line contains the word.</p> : places('text_search', matches)}
      {truncated ? <p className="muted">The search stopped at its limit; there are more.</p> : null}
    </section>
  );

  return (
    <section className="lookup-results" aria-label={lookup.kind === 'definition' ? 'Definition' : 'References'}>
      <header className="row">
        <h2>{lookup.kind === 'definition' ? 'Definition' : 'References'}</h2>
        <button type="button" onClick={onClose}>
          Close
        </button>
      </header>
      <p className="muted">
        For {origin.path}:{origin.line + 1}
        {origin.side === 'old' ? ', before the change' : ''}.
      </p>
      {lookup.kind === 'definition' ? definition(lookup.result) : references(lookup.result)}
      <ReportWrong context={context} how={how(lookup.result.label)} />
    </section>
  );

  function definition(result: Definition) {
    switch (result.label) {
      case 'index':
        return (
          <section aria-label={labelText.index}>
            <h3 className="label label-index">{labelText.index}</h3>
            <p className="muted">The index gives {result.targets.length} places.</p>
            {places('index', result.targets)}
          </section>
        );
      case 'text_search':
        return textMatches(
          result.matches,
          result.truncated,
          'The index has no definition for this. These lines contain the word; they are not known to define it.',
        );
      case 'external':
        return (
          <section aria-label={labelText.external}>
            <h3 className="label label-external">{labelText.external}</h3>
            <p>
              The index places this in the package <code>{result.package}</code>
              {result.version ? (
                <>
                  , version <code>{result.version}</code>
                </>
              ) : null}
              . That code is not part of the repository, so there is nothing to open.
            </p>
          </section>
        );
      case 'none':
        return (
          <section aria-label={labelText.none}>
            <h3 className="label label-none">{labelText.none}</h3>
            <p>Nothing was found for this, in the index or by searching the code.</p>
          </section>
        );
    }
  }

  function references(result: References) {
    return (
      <div className="lookup-columns">
        <section aria-label={labelText.index}>
          <h3 className="label label-index">{labelText.index}</h3>
          {result.references.length === 0 ? (
            <p className="muted">
              The index lists none here. An index can cover only part of a repository, so this does not mean there are
              none.
            </p>
          ) : (
            <>
              <p className="muted">
                {result.references.length} {result.references.length === 1 ? 'place' : 'places'}.
                {result.truncated ? ' The list stopped at its limit; there are more.' : ''}
              </p>
              {places('index', result.references)}
            </>
          )}
        </section>
        {textMatches(result.matches, result.matchesTruncated, 'Lines that contain the word, whatever the index says.')}
      </div>
    );
  }
}

/** The "this was wrong" report: one press, sent as a count for whoever runs the workspace. */
export function ReportWrong({ context, how }: { context: Pick<Context, 'owner' | 'repo'>; how: Arrival }) {
  const [state, setState] = useState<'idle' | 'sending' | 'sent' | 'failed'>('idle');
  if (state === 'sent') {
    return <p className="muted report">Reported. It names this lookup and nothing else.</p>;
  }
  return (
    <p className="report">
      <button
        type="button"
        disabled={state === 'sending'}
        onClick={() => {
          setState('sending');
          sendReport(context, how)
            .then(() => {
              setState('sent');
            })
            .catch(() => {
              setState('failed');
            });
        }}
      >
        This was wrong
      </button>
      {state === 'failed' ? <span className="error"> The report could not be sent.</span> : null}
    </p>
  );
}

export type { LookupKind };
