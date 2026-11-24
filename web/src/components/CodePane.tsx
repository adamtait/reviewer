// SPDX-License-Identifier: MIT

import { javascript } from '@codemirror/lang-javascript';
import { defaultHighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { MergeView, getChunks, unifiedMergeView } from '@codemirror/merge';
import { EditorSelection, EditorState, type Extension } from '@codemirror/state';
import { Decoration, EditorView, highlightSpecialChars, lineNumbers } from '@codemirror/view';
import { type Ref, useEffect, useImperativeHandle, useRef } from 'react';

import type { Side } from '../github/links';

// The code and diff display. Everything CodeMirror is in this file, so the
// rest of the application deals in text, paths and line numbers.
//
// The editors are read-only and not editable: nothing here changes a file, and
// a phone must not raise its keyboard when code is tapped.

/** A line in one side of a file. `line` counts from 0. */
export interface LineRef {
  side: Side;
  line: number;
}

/** What the page can ask of a pane. */
export interface PaneHandle {
  /**
   * Moves to the next (1) or previous (-1) change, from the cursor or from the
   * pane's edge. Returns false when there is no change in that direction, so
   * the caller can move on to the next file.
   */
  moveChange(direction: 1 | -1, from: 'cursor' | 'edge'): boolean;
  /** The line the cursor is on. */
  cursor(): LineRef | null;
  focus(): void;
}

const theme = EditorView.theme({
  '&': { fontSize: '13px', backgroundColor: 'var(--code-bg)', color: 'var(--text)' },
  '.cm-content': { fontFamily: 'var(--mono)', padding: '4px 0' },
  '.cm-gutters': {
    backgroundColor: 'var(--code-bg)',
    color: 'var(--muted)',
    border: 'none',
    borderRight: '1px solid var(--border)',
  },
  '.cm-lineNumbers .cm-gutterElement': { cursor: 'pointer', minWidth: '3ch', padding: '0 8px' },
  '.cm-lineNumbers .cm-gutterElement:hover': { color: 'var(--text)', textDecoration: 'underline' },
  '&.cm-focused': { outline: '2px solid var(--focus)', outlineOffset: '-2px' },
  '.cm-target-line': { backgroundColor: 'var(--target)' },
});

/** Colouring is for the languages the navigation index covers. Other files are shown plain. */
function language(path: string): Extension[] {
  const extension = /\.([a-z]+)$/i.exec(path)?.[1]?.toLowerCase() ?? '';
  switch (extension) {
    case 'ts':
    case 'mts':
    case 'cts':
      return [javascript({ typescript: true })];
    case 'tsx':
      return [javascript({ typescript: true, jsx: true })];
    case 'js':
    case 'mjs':
    case 'cjs':
      return [javascript()];
    case 'jsx':
      return [javascript({ jsx: true })];
    default:
      return [];
  }
}

function base(path: string, label: string, onLine: (line: number) => void): Extension[] {
  return [
    EditorState.readOnly.of(true),
    EditorView.editable.of(false),
    lineNumbers({
      domEventHandlers: {
        // Tapping or clicking a line number is how a line's actions are reached.
        click(view, block) {
          onLine(view.state.doc.lineAt(block.from).number - 1);
          return true;
        },
      },
    }),
    highlightSpecialChars(),
    syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
    ...language(path),
    // Focusable, so the keyboard can move a cursor through read-only code.
    EditorView.contentAttributes.of({ tabindex: '0', 'aria-label': label }),
    theme,
  ];
}

const targetLine = Decoration.line({ class: 'cm-target-line' });

/** Marks one line and scrolls it into view. Out-of-range lines are ignored. */
function showLine(view: EditorView, line: number | undefined): Extension[] {
  if (line === undefined || line < 0 || line >= view.state.doc.lines) return [];
  const at = view.state.doc.line(line + 1);
  view.dispatch({
    selection: EditorSelection.cursor(at.from),
    effects: EditorView.scrollIntoView(at.from, { y: 'center' }),
  });
  return [];
}

function cursorLine(view: EditorView): number {
  return view.state.doc.lineAt(view.state.selection.main.head).number - 1;
}

/**
 * Moves the cursor to a neighbouring change. The library's own command wraps
 * round at the last change; this one says there is none, so the page can go on
 * to the next file.
 */
function moveChange(view: EditorView, direction: 1 | -1, from: 'cursor' | 'edge'): boolean {
  const chunks = getChunks(view.state)?.chunks ?? [];
  if (chunks.length === 0) return false;
  const head = view.state.selection.main.head;
  const target =
    direction === 1
      ? chunks.find((chunk) => from === 'edge' || chunk.fromB > head)
      : [...chunks].reverse().find((chunk) => from === 'edge' || chunk.fromB < head);
  if (!target) return false;
  const position = Math.min(target.fromB, view.state.doc.length);
  view.dispatch({
    selection: EditorSelection.cursor(position),
    effects: EditorView.scrollIntoView(position, { y: 'center' }),
  });
  view.focus();
  return true;
}

export interface CodePaneProps {
  path: string;
  text: string;
  /** Which side of the pull request this text is. Reported with line actions. */
  side?: Side;
  /** A line to mark and scroll to, counted from 0. */
  line?: number | undefined;
  onLineAction?: ((ref: LineRef) => void) | undefined;
  ref?: Ref<PaneHandle>;
}

/** One file's text, read-only. */
export function CodePane({ path, text, side = 'new', line, onLineAction, ref }: CodePaneProps) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  const onLine = useRef(onLineAction);
  useEffect(() => {
    onLine.current = onLineAction;
  });

  useEffect(() => {
    if (!host.current) return;
    const marked =
      line !== undefined && line >= 0
        ? [
            EditorView.decorations.of((v) =>
              line < v.state.doc.lines ? Decoration.set([targetLine.range(v.state.doc.line(line + 1).from)]) : Decoration.none,
            ),
          ]
        : [];
    const editor = new EditorView({
      parent: host.current,
      state: EditorState.create({
        doc: text,
        extensions: [...base(path, path, (n) => onLine.current?.({ side, line: n })), ...marked],
      }),
    });
    view.current = editor;
    showLine(editor, line);
    return () => {
      editor.destroy();
      view.current = null;
    };
  }, [path, text, side, line]);

  useImperativeHandle(ref, () => ({
    moveChange: () => false,
    cursor: () => (view.current ? { side, line: cursorLine(view.current) } : null),
    focus: () => view.current?.focus(),
  }));

  return <div className="code-pane" data-testid="code-pane" ref={host} />;
}

export interface MergePaneProps {
  path: string;
  /** The file at the merge base. */
  oldText: string;
  /** The file at the head commit. */
  newText: string;
  /** Two columns, or one with removed lines shown above what replaced them. */
  layout: 'split' | 'unified';
  line?: number | undefined;
  onLineAction?: ((ref: LineRef) => void) | undefined;
  ref?: Ref<PaneHandle>;
}

// Unchanged stretches are folded, leaving three lines of context round each change.
const collapse = { margin: 3, minSize: 6 };

/** Two versions of one file, with what changed between them marked. */
export function MergePane({ path, oldText, newText, layout, line, onLineAction, ref }: MergePaneProps) {
  const host = useRef<HTMLDivElement>(null);
  const newSide = useRef<EditorView | null>(null);
  const oldSide = useRef<EditorView | null>(null);
  const onLine = useRef(onLineAction);
  useEffect(() => {
    onLine.current = onLineAction;
  });

  useEffect(() => {
    if (!host.current) return;
    const act = (side: Side) => (n: number) => onLine.current?.({ side, line: n });

    if (layout === 'unified') {
      const editor = new EditorView({
        parent: host.current,
        state: EditorState.create({
          doc: newText,
          extensions: [
            ...base(path, path, act('new')),
            unifiedMergeView({ original: oldText, mergeControls: false, gutter: true, collapseUnchanged: collapse }),
          ],
        }),
      });
      newSide.current = editor;
      showLine(editor, line);
      return () => {
        editor.destroy();
        newSide.current = null;
      };
    }

    const merge = new MergeView({
      parent: host.current,
      a: { doc: oldText, extensions: base(path, `${path}, before`, act('old')) },
      b: { doc: newText, extensions: base(path, `${path}, after`, act('new')) },
      gutter: true,
      collapseUnchanged: collapse,
    });
    newSide.current = merge.b;
    oldSide.current = merge.a;
    showLine(merge.b, line);
    return () => {
      merge.destroy();
      newSide.current = null;
      oldSide.current = null;
    };
  }, [path, oldText, newText, layout, line]);

  useImperativeHandle(ref, () => ({
    moveChange: (direction, from) => (newSide.current ? moveChange(newSide.current, direction, from) : false),
    cursor: () => {
      // The side that has focus is the side the user means.
      if (oldSide.current?.hasFocus) return { side: 'old', line: cursorLine(oldSide.current) };
      return newSide.current ? { side: 'new', line: cursorLine(newSide.current) } : null;
    },
    focus: () => newSide.current?.focus(),
  }));

  return <div className={`merge-pane merge-${layout}`} data-testid="merge-pane" ref={host} />;
}
