// SPDX-License-Identifier: MIT

import { type Ref, useEffect, useId, useRef, useState } from 'react';

import { ApiFailure, api, isNotFound } from '../api/client';
import type { ChangedFile, File, PullView } from '../api/types';
import { blobUrl } from '../github/links';
import { CodePane, type LineRef, MergePane, type PaneHandle, type SymbolIntent } from './CodePane';
import { MarkdownFile, isMarkdown } from './MarkdownFile';
import { RenderedDiff } from './RenderedDiff';

/** Where one side of a changed file is read from. */
export interface Source {
  sha: string;
  path: string;
}

/**
 * The two versions a changed file's diff is drawn between.
 *
 * The old version is read at the merge base, not at the tip of the base
 * branch: the diff shows what this pull request changes, and the base branch
 * has usually moved on since. A renamed file's old version is at its old path.
 */
export function sources(
  pull: Pick<PullView, 'headSha' | 'mergeBaseSha'>,
  file: ChangedFile,
): { before: Source | null; after: Source | null } {
  const after = { sha: pull.headSha, path: file.path };
  switch (file.status) {
    case 'added':
      return { before: null, after };
    case 'deleted':
      return { before: { sha: pull.mergeBaseSha, path: file.path }, after: null };
    case 'renamed':
      return { before: { sha: pull.mergeBaseSha, path: file.previousPath || file.path }, after };
    case 'modified':
      return { before: { sha: pull.mergeBaseSha, path: file.path }, after };
  }
}

/**
 * Whether a changed file can be drawn in place of its text: a Markdown file,
 * one version of it or both with the changes marked, unless it is deleted and
 * only the new code is shown, which leaves nothing to draw.
 */
export function renderable(file: ChangedFile, mode: 'diff' | 'new'): boolean {
  if (file.status === 'deleted' && mode === 'new') return false;
  return isMarkdown(file.path) || isMarkdown(file.previousPath || file.path);
}

/** One side once read: the file, or the fact that the commit's archive does not hold it. */
type Loaded = { file: File } | { absent: true } | null;

type State =
  | { phase: 'waiting' }
  | { phase: 'loading' }
  | { phase: 'ready'; before: Loaded; after: Loaded }
  | { phase: 'failed'; message: string };

async function read(owner: string, repo: string, source: Source | null, signal: AbortSignal): Promise<Loaded> {
  if (!source) return null;
  try {
    return { file: await api.file(owner, repo, source.sha, source.path, signal) };
  } catch (error) {
    // GitHub's list of changed files can name a path its archive leaves out.
    if (isNotFound(error)) return { absent: true };
    throw error;
  }
}

const whyNoText: Record<Exclude<File['kind'], 'text'>, string> = {
  binary: 'This file is not text, so it is not shown.',
  too_large: 'This file is larger than the workspace is set to read.',
  other: 'This entry is a link or a submodule, not a file.',
};

export interface FileDiffProps {
  owner: string;
  repo: string;
  pull: PullView;
  file: ChangedFile;
  /** The diff, or only the new version of the file. */
  mode: 'diff' | 'new';
  layout: 'split' | 'unified';
  /** A line of the new file to mark and scroll to, counted from 0. */
  line?: number | undefined;
  /** Read the file now, without waiting for it to scroll into view. */
  eager?: boolean;
  onLineAction?: (file: ChangedFile, ref: LineRef) => void;
  /** A click on a word in the code: a lookup, or on a narrow screen the list of actions. */
  onSymbolAction?: (file: ChangedFile, ref: LineRef, intent: SymbolIntent) => void;
  /** Whether a plain tap on a word opens the list of actions. */
  tapMenu?: boolean;
  /** The reader clicked or tabbed into this file: it is now the one the keyboard acts on. */
  onActivate?: () => void;
  /**
   * Whether the file's code is shown. A closed file's code is taken off the
   * page, so it has no pane, and moving through the changes passes it by.
   */
  open?: boolean;
  /** The reader clicked or tapped the file's name, to close it or open it again. */
  onToggle?: () => void;
  /**
   * Whether a Markdown file is drawn in place of its text, where it can be
   * (see `renderable`). Drawn, it has no pane, so moving through the changes
   * passes it by.
   */
  rendered?: boolean;
  /** The reader pressed the file's "Rendered" switch. */
  onRenderedToggle?: () => void;
  ref?: Ref<PaneHandle>;
}

/** One changed file: its two versions read from the service and drawn as a diff. */
export function FileDiff({
  owner,
  repo,
  pull,
  file,
  mode,
  layout,
  line,
  eager,
  onLineAction,
  onSymbolAction,
  tapMenu,
  onActivate,
  open = true,
  onToggle,
  rendered: drawn = false,
  onRenderedToggle,
  ref,
}: FileDiffProps) {
  const [state, setState] = useState<State>({ phase: 'waiting' });
  const [attempt, setAttempt] = useState(0);
  const [visible, setVisible] = useState(eager === true || typeof IntersectionObserver === 'undefined');
  const bodyId = useId();
  // The line the address names is shown once. Opening the file again shows
  // its top, where the reader just tapped, and does not jump back to that line.
  const [lineShown, setLineShown] = useState<number | undefined>(undefined);
  const paneLine = line === lineShown ? undefined : line;
  const section = useRef<HTMLElement>(null);
  const canRender = renderable(file, mode);
  const rendered = canRender && drawn;

  // A large pull request has hundreds of files. Each is read when it comes
  // near the screen, not all at once.
  useEffect(() => {
    if (visible || !section.current) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) setVisible(true);
      },
      { rootMargin: '600px 0px' },
    );
    observer.observe(section.current);
    return () => {
      observer.disconnect();
    };
  }, [visible]);

  const { headSha, mergeBaseSha } = pull;
  const { path, previousPath, status } = file;
  useEffect(() => {
    if (!visible) return;
    const controller = new AbortController();
    const { before, after } = sources({ headSha, mergeBaseSha }, { path, previousPath, status });
    setState({ phase: 'loading' });
    Promise.all([read(owner, repo, before, controller.signal), read(owner, repo, after, controller.signal)])
      .then(([b, a]) => {
        setState({ phase: 'ready', before: b, after: a });
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        setState({ phase: 'failed', message: error instanceof ApiFailure ? error.message : 'The file could not be read.' });
      });
    return () => {
      controller.abort();
    };
  }, [visible, owner, repo, headSha, mergeBaseSha, path, previousPath, status, attempt]);

  const act = onLineAction
    ? (lineRef: LineRef) => {
        onLineAction(file, lineRef);
      }
    : undefined;
  const symbol = onSymbolAction
    ? (lineRef: LineRef, intent: SymbolIntent) => {
        onSymbolAction(file, lineRef, intent);
      }
    : undefined;

  return (
    <section
      className={`file-diff${open ? '' : ' closed'}`}
      ref={section}
      data-path={file.path}
      aria-label={file.path}
      onFocus={onActivate}
    >
      <h3 className="file-header">
        <button
          type="button"
          className="file-toggle"
          aria-expanded={open}
          aria-controls={bodyId}
          onClick={() => {
            // Not every browser focuses a button it clicks, so this file is
            // made the active one here as well as on focus.
            onActivate?.();
            if (open) setLineShown(line);
            onToggle?.();
          }}
        >
          <span className="file-chevron" aria-hidden="true">
            {open ? '▾' : '▸'}
          </span>
          <span className={`status status-${file.status}`}>{file.status}</span>
          <span className="file-path">
            {file.status === 'renamed' && file.previousPath ? (
              <>
                <span className="muted">{file.previousPath} → </span>
                {file.path}
              </>
            ) : (
              file.path
            )}
          </span>
        </button>
        {canRender && open ? (
          <button
            type="button"
            className="file-view"
            aria-pressed={rendered}
            title={rendered ? 'Show this file’s Markdown as text' : 'Draw this file’s Markdown'}
            onClick={() => {
              onActivate?.();
              onRenderedToggle?.();
            }}
          >
            Rendered
          </button>
        ) : null}
      </h3>
      <div className="file-body" id={bodyId} hidden={!open}>
        {open ? body() : null}
      </div>
    </section>
  );

  function body() {
    if (state.phase === 'waiting' || state.phase === 'loading') {
      return <p className="muted pad">Reading from GitHub…</p>;
    }
    if (state.phase === 'failed') {
      return (
        <p className="error pad">
          {state.message}{' '}
          <button
            type="button"
            onClick={() => {
              setAttempt((n) => n + 1);
            }}
          >
            Try again
          </button>
        </p>
      );
    }

    const { before, after } = state;
    const problem = unreadable(after, pull.headSha, file.path) ?? unreadable(before, pull.mergeBaseSha, file.previousPath || file.path);
    if (problem) return problem;

    const oldText = before && 'file' in before ? before.file.text : null;
    const newText = after && 'file' in after ? after.file.text : null;

    if (rendered) {
      if (mode === 'diff' && oldText !== null && newText !== null) {
        return <RenderedDiff oldText={oldText} newText={newText} github={pull.webUrl} />;
      }
      const text = newText ?? oldText;
      if (text !== null) return <MarkdownFile text={text} github={pull.webUrl} />;
    }

    if (newText === null && oldText !== null) {
      if (mode === 'new') return <p className="muted pad">This pull request deletes this file.</p>;
      return (
        <CodePane
          path={file.previousPath || file.path}
          text={oldText}
          side="old"
          onLineAction={act}
          onSymbolAction={symbol}
          tapMenu={tapMenu}
          {...(ref ? { ref } : {})}
        />
      );
    }
    if (newText === null) return <p className="muted pad">Nothing to show.</p>;
    if (oldText === null || mode === 'new') {
      return (
        <CodePane
          path={file.path}
          text={newText}
          side="new"
          line={paneLine}
          onLineAction={act}
          onSymbolAction={symbol}
          tapMenu={tapMenu}
          {...(ref ? { ref } : {})}
        />
      );
    }
    return (
      <MergePane
        path={file.path}
        oldText={oldText}
        newText={newText}
        layout={layout}
        line={paneLine}
        onLineAction={act}
        onSymbolAction={symbol}
        tapMenu={tapMenu}
        {...(ref ? { ref } : {})}
      />
    );
  }

  function unreadable(side: Loaded, sha: string, sidePath: string) {
    if (!side) return null;
    const onGitHub = (
      <a href={blobUrl(pull.webUrl, sha, sidePath)} target="_blank" rel="noopener noreferrer">
        Open it on GitHub
      </a>
    );
    if ('absent' in side) {
      return (
        <p className="muted pad">
          This file is not in the archive GitHub provides for this commit. {onGitHub}.
        </p>
      );
    }
    if (side.file.kind !== 'text') {
      return (
        <p className="muted pad">
          {whyNoText[side.file.kind]} {onGitHub}.
        </p>
      );
    }
    return null;
  }
}
