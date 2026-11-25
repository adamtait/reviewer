// SPDX-License-Identifier: MIT

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

import { ApiFailure, api, isNotFound } from '../api/client';
import type { ChangedFile, LookupKind, PullView as Pull } from '../api/types';
import { ActionSheet, type SheetAction } from '../components/ActionSheet';
import { AssistantPanel } from '../components/AssistantPanel';
import type { LineRef, PaneHandle, SelectedLines } from '../components/CodePane';
import { Description } from '../components/Description';
import { FileDiff, sources } from '../components/DiffView';
import { IndexStatus } from '../components/IndexStatus';
import { Link } from '../components/Link';
import { InlineMarkdown } from '../components/Markdown';
import { type LookupResult, LookupResults } from '../components/References';
import { SearchPanel } from '../components/SearchPanel';
import { ShortcutHelp } from '../components/ShortcutHelp';
import { commentUrl, openOnGitHub } from '../github/links';
import type { Action } from '../keys/keymap';
import { useHotkeys } from '../keys/useHotkeys';
import { type Quoted, useConversation } from '../nav/conversation';
import {
  type Context,
  type Origin,
  definitionOutcome,
  findDefinition,
  findReferences,
  goTo,
  lookupCommit,
} from '../nav/navigation';
import { href, navigate } from '../router';
import { useNarrow } from '../useNarrow';
import { NotFound } from './NotFound';

type State = { phase: 'loading' } | { phase: 'ready'; pull: Pull } | { phase: 'notfound' } | { phase: 'failed'; message: string };

export interface PullViewProps {
  owner: string;
  repo: string;
  number: number;
  /** From the address: a file to scroll to, and a line in it, counted from 1. */
  file?: string | undefined;
  line?: number | undefined;
  /** Whether this repository has an assistant configured. */
  assistant?: boolean;
}

const always: readonly Action[] = [
  'nextChange', 'prevChange', 'nextFile', 'prevFile', 'definition', 'references', 'search', 'toggleDiff', 'comment',
  'switchPull', 'back', 'help',
];

/** A lookup whose answer is being shown and not followed. */
interface Shown {
  context: Context;
  origin: Origin;
  lookup: LookupResult;
}

export function PullView({ owner, repo, number, file, line, assistant = false }: PullViewProps) {
  const [state, setState] = useState<State>({ phase: 'loading' });
  const [mode, setMode] = useState<'diff' | 'new'>('diff');
  const [panel, setPanel] = useState<'none' | 'search' | 'help' | 'lookup' | 'assistant'>('none');
  const conversation = useConversation(owner, repo, number);
  const [quoted, setQuoted] = useState<Quoted | null>(null);
  const available = useMemo(() => new Set<Action>(assistant ? [...always, 'ask'] : always), [assistant]);
  const [sheet, setSheet] = useState<{ file: ChangedFile; at: LineRef } | null>(null);
  const [shown, setShown] = useState<Shown | null>(null);
  const [notice, setNotice] = useState('');
  const [active, setActive] = useState(0);
  const narrow = useNarrow();
  const panes = useRef(new Map<string, PaneHandle>());

  useEffect(() => {
    const controller = new AbortController();
    setState({ phase: 'loading' });
    api
      .pull(owner, repo, number, controller.signal)
      .then((pull) => {
        setState({ phase: 'ready', pull });
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        if (isNotFound(error)) setState({ phase: 'notfound' });
        else setState({ phase: 'failed', message: error instanceof ApiFailure ? error.message : 'The pull request could not be read.' });
      });
    return () => {
      controller.abort();
    };
  }, [owner, repo, number]);

  const pull = state.phase === 'ready' ? state.pull : null;
  const files = useMemo(() => pull?.files ?? [], [pull]);

  const scrollTo = useCallback((path: string) => {
    document.querySelector(`[data-path="${CSS.escape(path)}"]`)?.scrollIntoView({ block: 'start' });
  }, []);

  // The address can name a file. Go to it once the list is known.
  useEffect(() => {
    if (!file || files.length === 0) return;
    const index = files.findIndex((f) => f.path === file);
    if (index < 0) return;
    setActive(index);
    scrollTo(file);
  }, [file, files, scrollTo]);

  const goToFile = useCallback(
    (index: number) => {
      const target = files[index];
      if (!target) return;
      setActive(index);
      navigate(href({ view: 'pull', owner, repo, number, file: target.path }), { replace: true });
      scrollTo(target.path);
      panes.current.get(target.path)?.focus();
    },
    [files, owner, repo, number, scrollTo],
  );

  const moveChange = useCallback(
    (direction: 1 | -1) => {
      const here = files[active];
      if (here && panes.current.get(here.path)?.moveChange(direction, 'cursor')) return;
      // No further change in this file: go on to the next file that has one,
      // or failing that, to the next file.
      for (let i = active + direction; i >= 0 && i < files.length; i += direction) {
        const next = files[i];
        if (!next) break;
        if (panes.current.get(next.path)?.moveChange(direction, 'edge')) {
          setActive(i);
          return;
        }
      }
      goToFile(active + direction);
    },
    [files, active, goToFile],
  );

  const comment = useCallback(
    (target: ChangedFile, at: LineRef) => {
      if (!pull) return;
      const path = at.side === 'old' ? target.previousPath || target.path : target.path;
      void commentUrl(pull.webUrl, path, at.line, at.side).then(openOnGitHub);
    },
    [pull],
  );

  // One lookup: where is this defined, or where is it used.
  const look = useCallback(
    (kind: LookupKind, target: ChangedFile, at: LineRef) => {
      if (!pull || at.character === undefined) return;
      // A removed line is looked up in the old version of its file, at the
      // merge base; anything else at the head commit.
      const context: Context = { owner, repo, sha: lookupCommit(pull, at.side), pull: number };
      const origin: Origin = {
        side: at.side,
        path: at.side === 'old' ? target.previousPath || target.path : target.path,
        line: at.line,
        character: at.character,
      };
      setNotice(kind === 'definition' ? 'Looking for the definition…' : 'Looking for references…');
      const found =
        kind === 'definition'
          ? findDefinition(context, origin).then((result): LookupResult => ({ kind, result }))
          : findReferences(context, origin).then((result): LookupResult => ({ kind, result }));
      found
        .then((lookup) => {
          setNotice('');
          if (lookup.kind === 'definition') {
            const outcome = definitionOutcome(lookup.result);
            if (outcome.action === 'jump') {
              // Leave this place in the history first, so that going back
              // returns to this file and line of the diff.
              navigate(href({ view: 'pull', owner, repo, number, file: target.path, line: at.line + 1 }), { replace: true });
              goTo(context, outcome.target, {
                label: lookup.result.label,
                kind,
                origin: { sha: context.sha, path: origin.path, line: origin.line, character: origin.character },
              });
              return;
            }
          }
          setShown({ context, origin, lookup });
          setPanel('lookup');
        })
        .catch((error: unknown) => {
          setNotice(error instanceof ApiFailure ? error.message : 'The lookup failed.');
        });
    },
    [pull, owner, repo, number],
  );

  // Asks about lines of a changed file. Lines the pull request removes are in
  // the old version of the file, so a question about them is asked at the
  // merge base; any other at the head commit.
  const askAbout = useCallback(
    (target: ChangedFile | null, chosen: SelectedLines | null) => {
      if (!pull) return;
      if (target && chosen) {
        setQuoted({
          sha: lookupCommit(pull, chosen.side),
          path: chosen.side === 'old' ? target.previousPath || target.path : target.path,
          startLine: chosen.startLine,
          endLine: chosen.endLine,
          text: chosen.text,
        });
      }
      setPanel('assistant');
    },
    [pull],
  );

  const lookAtCursor = useCallback(
    (kind: LookupKind) => {
      const target = files[active];
      const at = target ? panes.current.get(target.path)?.cursor() : null;
      if (target && at) look(kind, target, at);
    },
    [files, active, look],
  );

  useHotkeys(
    pull
      ? {
          ...(assistant
            ? {
                ask: () => {
                  const target = files[active];
                  askAbout(target ?? null, target ? (panes.current.get(target.path)?.selection() ?? null) : null);
                },
              }
            : {}),
          definition: () => {
            lookAtCursor('definition');
          },
          references: () => {
            lookAtCursor('references');
          },
          nextChange: () => {
            moveChange(1);
          },
          prevChange: () => {
            moveChange(-1);
          },
          nextFile: () => {
            goToFile(Math.min(active + 1, files.length - 1));
          },
          prevFile: () => {
            goToFile(Math.max(active - 1, 0));
          },
          search: () => {
            setPanel('search');
          },
          toggleDiff: () => {
            setMode((m) => (m === 'diff' ? 'new' : 'diff'));
          },
          comment: () => {
            const target = files[active];
            const at = target ? panes.current.get(target.path)?.cursor() : null;
            if (target && at) comment(target, at);
          },
          switchPull: () => {
            navigate('/');
          },
          back: () => {
            setPanel('none');
            setSheet(null);
            setNotice('');
          },
          help: () => {
            setPanel('help');
          },
        }
      : {},
  );

  if (state.phase === 'loading') return <main className="pad muted">Reading the pull request from GitHub…</main>;
  if (state.phase === 'notfound') return <NotFound />;
  if (state.phase === 'failed') return <main className="pad error">{state.message}</main>;

  const current = state.pull;
  const targetLine = line === undefined ? undefined : line - 1;

  const sheetActions = (target: ChangedFile, at: LineRef): SheetAction[] => {
    const { before, after } = sources(current, target);
    const source = at.side === 'old' ? before : after;
    const actions: SheetAction[] = [];
    // A position in the code, and not a whole line, can be looked up.
    if (at.character !== undefined) {
      actions.push(
        {
          label: 'Definition',
          hint: 'g d',
          run: () => {
            look('definition', target, at);
          },
        },
        {
          label: 'References',
          hint: 'g r',
          run: () => {
            look('references', target, at);
          },
        },
      );
    }
    if (assistant) {
      const text = panes.current.get(target.path)?.lines(at.side, at.line, at.line + 1);
      if (text !== null && text !== undefined) {
        actions.push({
          label: 'Ask',
          hint: 'a',
          run: () => {
            askAbout(target, { side: at.side, startLine: at.line, endLine: at.line + 1, text });
          },
        });
      }
    }
    actions.push({
      label: 'Comment on GitHub',
      hint: 'c',
      run: () => {
        comment(target, at);
      },
    });
    if (source) {
      actions.push({
        label: 'Open this file',
        run: () => {
          navigate(href({ view: 'code', owner, repo, sha: source.sha, path: source.path, line: at.line + 1, pull: number }));
        },
      });
    }
    return actions;
  };

  return (
    <div className={`pull-view${panel === 'search' || panel === 'lookup' || panel === 'assistant' ? ' with-panel' : ''}`}>
      <main>
        <header className="pull-header">
          <h1>
            <InlineMarkdown text={current.title} /> <span className="muted">#{current.number}</span>
          </h1>
          <p className="muted">
            {current.draft ? <span className="badge">Draft</span> : null}
            {current.state !== 'open' ? <span className="badge">Closed</span> : null} {current.author} wants to merge into{' '}
            <code>{current.baseRef}</code>.{' '}
            <a href={current.webUrl} target="_blank" rel="noopener noreferrer">
              Open on GitHub
            </a>
          </p>
          <div className="toolbar">
            <button
              type="button"
              aria-pressed={mode === 'new'}
              onClick={() => {
                setMode((m) => (m === 'diff' ? 'new' : 'diff'));
              }}
            >
              {mode === 'diff' ? 'Show new code only' : 'Show the diff'}
            </button>
            <button
              type="button"
              onClick={() => {
                setPanel('search');
              }}
            >
              Search code
            </button>
            {assistant ? (
              <button
                type="button"
                onClick={() => {
                  setPanel('assistant');
                }}
              >
                Ask
              </button>
            ) : null}
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

        <IndexStatus owner={owner} repo={repo} sha={current.headSha} />
        {notice ? (
          <p className="notice" role="status">
            {notice}
          </p>
        ) : null}

        <Description body={current.body} github={current.webUrl} />

        <nav className="file-list" aria-label="Changed files">
          <h2>
            {files.length} changed {files.length === 1 ? 'file' : 'files'}
          </h2>
          {current.filesTruncated ? (
            <p className="muted">
              This pull request changes more files than GitHub lists. The rest are on{' '}
              <a href={`${current.webUrl}/files`} target="_blank" rel="noopener noreferrer">
                GitHub
              </a>
              .
            </p>
          ) : null}
          <ol>
            {files.map((f, i) => (
              <li key={f.path} className={i === active ? 'active' : undefined}>
                <a
                  href={href({ view: 'pull', owner, repo, number, file: f.path })}
                  onClick={(event) => {
                    event.preventDefault();
                    goToFile(i);
                  }}
                >
                  <span className={`status status-${f.status}`}>{f.status}</span> {f.path}
                </a>
              </li>
            ))}
          </ol>
        </nav>

        <div className="diffs">
          {files.map((f, i) => (
            <FileDiff
              key={f.path}
              owner={owner}
              repo={repo}
              pull={current}
              file={f}
              mode={mode}
              layout={narrow ? 'unified' : 'split'}
              line={file === f.path ? targetLine : undefined}
              eager={i < 3 || file === f.path}
              onLineAction={(target, at) => {
                setActive(i);
                setSheet({ file: target, at });
              }}
              tapMenu={narrow}
              onActivate={() => {
                setActive(i);
              }}
              onSymbolAction={(target, at, intent) => {
                setActive(i);
                if (intent === 'definition') look('definition', target, at);
                else setSheet({ file: target, at });
              }}
              ref={(handle) => {
                if (handle) panes.current.set(f.path, handle);
                else panes.current.delete(f.path);
              }}
            />
          ))}
        </div>

        <nav className="bottom-bar" aria-label="Move through the changes">
          <button
            type="button"
            onClick={() => {
              moveChange(-1);
            }}
          >
            Previous change
          </button>
          <button
            type="button"
            onClick={() => {
              moveChange(1);
            }}
          >
            Next change
          </button>
          <Link to="/">Pull requests</Link>
        </nav>
      </main>

      {panel === 'search' ? (
        <aside className="side-panel">
          <SearchPanel
            owner={owner}
            repo={repo}
            sha={current.headSha}
            pull={number}
            onClose={() => {
              setPanel('none');
            }}
          />
        </aside>
      ) : null}
      {panel === 'assistant' ? (
        <aside className="side-panel side-panel-assistant">
          <AssistantPanel
            owner={owner}
            repo={repo}
            sha={current.headSha}
            pull={number}
            github={current.webUrl}
            conversation={conversation}
            selection={quoted}
            onClearSelection={() => {
              setQuoted(null);
            }}
            onClose={() => {
              setPanel('none');
            }}
          />
        </aside>
      ) : null}
      {panel === 'lookup' && shown ? (
        <aside className="side-panel">
          <LookupResults
            context={shown.context}
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
          title={`${sheet.at.side === 'old' ? sheet.file.previousPath || sheet.file.path : sheet.file.path}, line ${sheet.at.line + 1}${sheet.at.side === 'old' ? ' (before)' : ''}`}
          actions={sheetActions(sheet.file, sheet.at)}
          onClose={() => {
            setSheet(null);
          }}
        />
      ) : null}
    </div>
  );
}
