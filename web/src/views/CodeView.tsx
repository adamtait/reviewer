// SPDX-License-Identifier: MIT

import { useCallback, useEffect, useRef, useState } from 'react';

import { ApiFailure, api, isNotFound } from '../api/client';
import type { File, LookupKind, PullView } from '../api/types';
import { ActionSheet, type SheetAction } from '../components/ActionSheet';
import { CodePane, type LineRef, type PaneHandle } from '../components/CodePane';
import { IndexStatus } from '../components/IndexStatus';
import { Link } from '../components/Link';
import { type LookupResult, LookupResults, ReportWrong } from '../components/References';
import { SearchPanel } from '../components/SearchPanel';
import { ShortcutHelp } from '../components/ShortcutHelp';
import { blobUrl, openOnGitHub } from '../github/links';
import type { Action } from '../keys/keymap';
import { useHotkeys } from '../keys/useHotkeys';
import {
  type Context,
  type Origin,
  arrival,
  definitionOutcome,
  findDefinition,
  findReferences,
  goTo,
  labelText,
} from '../nav/navigation';
import { href, navigate } from '../router';
import { useNarrow } from '../useNarrow';
import { NotFound } from './NotFound';

type State = { phase: 'loading' } | { phase: 'ready'; file: File } | { phase: 'notfound' } | { phase: 'failed'; message: string };

export interface CodeViewProps {
  owner: string;
  repo: string;
  sha: string;
  path: string;
  /** A line to mark, counted from 1, as in the address. */
  line?: number | undefined;
  /** The pull request this file was opened from, if any. */
  pull?: number | undefined;
}

const available: ReadonlySet<Action> = new Set<Action>(['definition', 'references', 'search', 'switchPull', 'back', 'help']);

const whyNoText: Record<Exclude<File['kind'], 'text'>, string> = {
  binary: 'This file is not text, so it is not shown.',
  too_large: 'This file is larger than the workspace is set to read.',
  other: 'This entry is a link or a submodule, not a file.',
};

/** One file at one commit: where a search result or a "go to" lands, often outside the pull request's changes. */
export function CodeView({ owner, repo, sha, path, line, pull }: CodeViewProps) {
  const [state, setState] = useState<State>({ phase: 'loading' });
  const [from, setFrom] = useState<PullView | null>(null);
  const [panel, setPanel] = useState<'none' | 'search' | 'help' | 'lookup'>('none');
  const [sheet, setSheet] = useState<LineRef | null>(null);
  const [shown, setShown] = useState<{ origin: Origin; lookup: LookupResult } | null>(null);
  const [notice, setNotice] = useState('');
  const narrow = useNarrow();
  const pane = useRef<PaneHandle>(null);
  // How this page was reached, if a lookup led here. It is in the history
  // entry, so it is still known after going forward again or reloading.
  const how = arrival(window.history.state);
  const context: Context = { owner, repo, sha, pull };

  useEffect(() => {
    const controller = new AbortController();
    setState({ phase: 'loading' });
    api
      .file(owner, repo, sha, path, controller.signal)
      .then((file) => {
        setState({ phase: 'ready', file });
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        if (isNotFound(error)) setState({ phase: 'notfound' });
        else setState({ phase: 'failed', message: error instanceof ApiFailure ? error.message : 'The file could not be read.' });
      });
    return () => {
      controller.abort();
    };
  }, [owner, repo, sha, path]);

  // The pull request the reader came from says whether this file is part of
  // the change, and where GitHub is.
  useEffect(() => {
    if (!pull) return;
    const controller = new AbortController();
    api
      .pull(owner, repo, pull, controller.signal)
      .then(setFrom)
      .catch(() => {
        // The file is still worth showing without its pull request.
      });
    return () => {
      controller.abort();
    };
  }, [owner, repo, pull]);

  const back = pull ? href({ view: 'pull', owner, repo, number: pull }) : null;

  const look = useCallback(
    (kind: LookupKind, at: LineRef) => {
      if (at.character === undefined) return;
      const here: Context = { owner, repo, sha, pull };
      const origin: Origin = { side: 'new', path, line: at.line, character: at.character };
      setNotice(kind === 'definition' ? 'Looking for the definition…' : 'Looking for references…');
      const found =
        kind === 'definition'
          ? findDefinition(here, origin).then((result): LookupResult => ({ kind, result }))
          : findReferences(here, origin).then((result): LookupResult => ({ kind, result }));
      found
        .then((lookup) => {
          setNotice('');
          if (lookup.kind === 'definition') {
            const outcome = definitionOutcome(lookup.result);
            if (outcome.action === 'jump') {
              goTo(here, outcome.target, {
                label: lookup.result.label,
                kind,
                origin: { sha, path, line: origin.line, character: origin.character },
              });
              return;
            }
          }
          setShown({ origin, lookup });
          setPanel('lookup');
        })
        .catch((error: unknown) => {
          setNotice(error instanceof ApiFailure ? error.message : 'The lookup failed.');
        });
    },
    [owner, repo, sha, pull, path],
  );

  useHotkeys({
    definition: () => {
      const at = pane.current?.cursor();
      if (at) look('definition', at);
    },
    references: () => {
      const at = pane.current?.cursor();
      if (at) look('references', at);
    },
    search: () => {
      setPanel('search');
    },
    switchPull: () => {
      navigate('/');
    },
    back: () => {
      if (panel !== 'none' || sheet) {
        setPanel('none');
        setSheet(null);
      } else if (how) {
        // Reached by a lookup: go back to exactly where it was made.
        window.history.back();
      } else if (back) {
        navigate(back);
      }
    },
    help: () => {
      setPanel('help');
    },
  });

  if (state.phase === 'notfound') return <NotFound />;

  const inChange = from?.files.some((f) => (sha === from.headSha ? f.path === path : (f.previousPath || f.path) === path)) ?? null;

  const actions = (at: LineRef): SheetAction[] => {
    const list: SheetAction[] = [];
    if (at.character !== undefined) {
      list.push(
        {
          label: 'Definition',
          hint: 'g d',
          run: () => {
            look('definition', at);
          },
        },
        {
          label: 'References',
          hint: 'g r',
          run: () => {
            look('references', at);
          },
        },
      );
    }
    if (from) {
      list.push({
        label: 'Open on GitHub',
        run: () => {
          openOnGitHub(blobUrl(from.webUrl, sha, path, at.line));
        },
      });
    }
    return list;
  };

  return (
    <div className={`code-view${panel === 'search' || panel === 'lookup' ? ' with-panel' : ''}`}>
      <main>
        <header className="code-header">
          {how ? (
            // One action back to the same place: the history entry the lookup left.
            <a
              href={back ?? '/'}
              className="back"
              onClick={(event) => {
                event.preventDefault();
                window.history.back();
              }}
            >
              ← Back to where you were
            </a>
          ) : back ? (
            <Link to={back} className="back">
              ← Back to diff
            </Link>
          ) : null}
          <h1 className="file-path">{path}</h1>
          <p className="muted">
            At commit <code>{sha.slice(0, 7)}</code>.
            {inChange === false ? <span className="badge outside"> Outside this pull request’s changes</span> : null}
            {from && sha === from.mergeBaseSha ? <span className="badge"> Before the change</span> : null}
          </p>
          {how ? (
            <div className="arrival" data-testid="arrival">
              <span className={`label label-${how.label}`}>{labelText[how.label]}</span>
              <span className="muted">
                {' '}
                The index says this is the {how.kind === 'definition' ? 'definition' : 'use'} you asked about. It is not
                checked.
              </span>
              <ReportWrong context={context} how={how} />
            </div>
          ) : null}
          <IndexStatus owner={owner} repo={repo} sha={sha} />
          {notice ? (
            <p className="notice" role="status">
              {notice}
            </p>
          ) : null}
          <div className="toolbar">
            <button
              type="button"
              onClick={() => {
                setPanel('search');
              }}
            >
              Search code
            </button>
            <button
              type="button"
              onClick={() => {
                setPanel('help');
              }}
            >
              Shortcuts
            </button>
          </div>
        </header>

        {state.phase === 'loading' ? <p className="muted pad">Reading from GitHub…</p> : null}
        {state.phase === 'failed' ? <p className="error pad">{state.message}</p> : null}
        {state.phase === 'ready' && state.file.kind !== 'text' ? <p className="muted pad">{whyNoText[state.file.kind]}</p> : null}
        {state.phase === 'ready' && state.file.kind === 'text' ? (
          <CodePane
            ref={pane}
            path={path}
            text={state.file.text}
            line={line === undefined ? undefined : line - 1}
            onLineAction={(at) => {
              if (from) setSheet(at);
            }}
            tapMenu={narrow}
            onSymbolAction={(at, intent) => {
              if (intent === 'definition') look('definition', at);
              else setSheet(at);
            }}
          />
        ) : null}
      </main>

      {panel === 'search' ? (
        <aside className="side-panel">
          <SearchPanel
            owner={owner}
            repo={repo}
            sha={sha}
            pull={pull}
            onClose={() => {
              setPanel('none');
            }}
          />
        </aside>
      ) : null}
      {panel === 'lookup' && shown ? (
        <aside className="side-panel">
          <LookupResults
            context={context}
            origin={shown.origin}
            lookup={shown.lookup}
            onClose={() => {
              setPanel('none');
            }}
          />
        </aside>
      ) : null}
      {panel === 'help' ? (
        <ShortcutHelp
          available={available}
          onClose={() => {
            setPanel('none');
          }}
        />
      ) : null}
      {sheet ? (
        <ActionSheet
          title={`${path}, line ${sheet.line + 1}`}
          actions={actions(sheet)}
          onClose={() => {
            setSheet(null);
          }}
        />
      ) : null}
    </div>
  );
}
