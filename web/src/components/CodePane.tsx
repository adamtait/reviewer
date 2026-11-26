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
  /**
   * A character in the line, counted from 0 in UTF-16 code units, when the
   * place is a position in the code and not a whole line. It is the unit the
   * navigation index counts in, so it is sent to the service as it is.
   */
  character?: number;
}

/** What a click on a word in the code asks for. */
export type SymbolIntent = 'definition' | 'menu';

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
  /**
   * The lines the reader has selected, or the cursor's line when nothing is
   * selected, with their text. `startLine` counts from 0 and `endLine` is
   * exclusive.
   */
  selection(): SelectedLines | null;
  /** The text of whole lines on one side, or null if that side has no such lines. */
  lines(side: Side, startLine: number, endLine: number): string | null;
  focus(): void;
}

/** A run of whole lines in one side of a file. */
export interface SelectedLines {
  side: Side;
  startLine: number;
  endLine: number;
  text: string;
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

/** Reports a click on a word, with what kind of click it was. */
type OnSymbol = (line: number, character: number, intent: SymbolIntent) => void;

/**
 * Turns a click on a word into a lookup request.
 *
 * With Ctrl, Cmd or Alt held, the click asks for the definition, as in an
 * editor. A plain click asks for the list of actions only where `tapMenu` is
 * set, which is on narrow screens: there a tap is the only pointer there is,
 * while on a desktop a plain click has to stay free for selecting text.
 */
function symbolClicks(onSymbol: OnSymbol, tapMenu: () => boolean): Extension {
  return EditorView.domEventHandlers({
    click(event, view) {
      const wanted = event.metaKey || event.ctrlKey || event.altKey;
      if (!wanted && !tapMenu()) return false;
      const position = view.posAtCoords({ x: event.clientX, y: event.clientY });
      if (position === null) return false;
      const word = view.state.wordAt(position);
      if (!word) return false;
      const line = view.state.doc.lineAt(position);
      view.dispatch({ selection: EditorSelection.cursor(position) });
      onSymbol(line.number - 1, position - line.from, wanted ? 'definition' : 'menu');
      return true;
    },
  });
}

function base(path: string, label: string, onLine: (line: number) => void, clicks: Extension): Extension[] {
  return [
    clicks,
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

/** Where the cursor is: its line, and its offset in that line in UTF-16 code units. */
function cursorPlace(view: EditorView): { line: number; character: number } {
  const head = view.state.selection.main.head;
  const line = view.state.doc.lineAt(head);
  return { line: line.number - 1, character: head - line.from };
}

/** The whole lines the selection touches. A selection that ends at the start of a line does not take that line. */
function selectedLines(view: EditorView, side: Side): SelectedLines {
  const { from, to } = view.state.selection.main;
  const first = view.state.doc.lineAt(from);
  let last = view.state.doc.lineAt(to);
  if (to > from && to === last.from && last.number > first.number) last = view.state.doc.line(last.number - 1);
  return {
    side,
    startLine: first.number - 1,
    endLine: last.number,
    text: view.state.doc.sliceString(first.from, last.to),
  };
}

/** The text of lines startLine up to endLine, counted from 0 with the end excluded. */
function textOfLines(view: EditorView, startLine: number, endLine: number): string | null {
  const total = view.state.doc.lines;
  if (startLine < 0 || endLine <= startLine || endLine > total) return null;
  return view.state.doc.sliceString(view.state.doc.line(startLine + 1).from, view.state.doc.line(endLine).to);
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
  /** A click on a word. See symbolClicks for which clicks count. */
  onSymbolAction?: ((ref: LineRef, intent: SymbolIntent) => void) | undefined;
  /** Whether a plain tap on a word opens the list of actions. */
  tapMenu?: boolean | undefined;
  ref?: Ref<PaneHandle>;
}

/** One file's text, read-only. */
export function CodePane({ path, text, side = 'new', line, onLineAction, onSymbolAction, tapMenu, ref }: CodePaneProps) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  // Asked for focus before the editor was made: it takes focus once it is.
  const focusWhenMade = useRef(false);
  const onLine = useRef(onLineAction);
  const onSymbol = useRef(onSymbolAction);
  const menu = useRef(tapMenu);
  useEffect(() => {
    onLine.current = onLineAction;
    onSymbol.current = onSymbolAction;
    menu.current = tapMenu;
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
        extensions: [
          ...base(
            path,
            path,
            (n) => onLine.current?.({ side, line: n }),
            symbolClicks(
              (n, character, intent) => onSymbol.current?.({ side, line: n, character }, intent),
              () => menu.current === true,
            ),
          ),
          ...marked,
        ],
      }),
    });
    view.current = editor;
    showLine(editor, line);
    if (focusWhenMade.current) {
      focusWhenMade.current = false;
      editor.focus();
    }
    return () => {
      editor.destroy();
      view.current = null;
    };
  }, [path, text, side, line]);

  useImperativeHandle(ref, () => ({
    moveChange: () => false,
    cursor: () => (view.current ? { side, ...cursorPlace(view.current) } : null),
    selection: () => (view.current ? selectedLines(view.current, side) : null),
    lines: (which, startLine, endLine) => (view.current && which === side ? textOfLines(view.current, startLine, endLine) : null),
    focus: () => {
      if (view.current) view.current.focus();
      else focusWhenMade.current = true;
    },
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
  onSymbolAction?: ((ref: LineRef, intent: SymbolIntent) => void) | undefined;
  tapMenu?: boolean | undefined;
  ref?: Ref<PaneHandle>;
}

// Unchanged stretches are folded, leaving three lines of context round each change.
const collapse = { margin: 3, minSize: 6 };

/** Two versions of one file, with what changed between them marked. */
export function MergePane({ path, oldText, newText, layout, line, onLineAction, onSymbolAction, tapMenu, ref }: MergePaneProps) {
  const host = useRef<HTMLDivElement>(null);
  const newSide = useRef<EditorView | null>(null);
  const oldSide = useRef<EditorView | null>(null);
  // Asked for focus before the editors were made: the new side takes it once they are.
  const focusWhenMade = useRef(false);
  const onLine = useRef(onLineAction);
  const onSymbol = useRef(onSymbolAction);
  const menu = useRef(tapMenu);
  useEffect(() => {
    onLine.current = onLineAction;
    onSymbol.current = onSymbolAction;
    menu.current = tapMenu;
  });

  useEffect(() => {
    if (!host.current) return;
    const takeFocus = (editor: EditorView) => {
      if (!focusWhenMade.current) return;
      focusWhenMade.current = false;
      editor.focus();
    };
    const act = (side: Side) => (n: number) => onLine.current?.({ side, line: n });
    const clicks = (side: Side) =>
      symbolClicks(
        (n, character, intent) => onSymbol.current?.({ side, line: n, character }, intent),
        () => menu.current === true,
      );

    if (layout === 'unified') {
      const editor = new EditorView({
        parent: host.current,
        state: EditorState.create({
          doc: newText,
          extensions: [
            ...base(path, path, act('new'), clicks('new')),
            unifiedMergeView({ original: oldText, mergeControls: false, gutter: true, collapseUnchanged: collapse }),
          ],
        }),
      });
      newSide.current = editor;
      showLine(editor, line);
      takeFocus(editor);
      return () => {
        editor.destroy();
        newSide.current = null;
      };
    }

    const merge = new MergeView({
      parent: host.current,
      a: { doc: oldText, extensions: base(path, `${path}, before`, act('old'), clicks('old')) },
      b: { doc: newText, extensions: base(path, `${path}, after`, act('new'), clicks('new')) },
      gutter: true,
      collapseUnchanged: collapse,
    });
    newSide.current = merge.b;
    oldSide.current = merge.a;
    showLine(merge.b, line);
    takeFocus(merge.b);
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
      if (oldSide.current?.hasFocus) return { side: 'old', ...cursorPlace(oldSide.current) };
      return newSide.current ? { side: 'new', ...cursorPlace(newSide.current) } : null;
    },
    selection: () => {
      if (oldSide.current?.hasFocus) return selectedLines(oldSide.current, 'old');
      return newSide.current ? selectedLines(newSide.current, 'new') : null;
    },
    lines: (which, startLine, endLine) => {
      const view = which === 'old' ? oldSide.current : newSide.current;
      return view ? textOfLines(view, startLine, endLine) : null;
    },
    focus: () => {
      if (newSide.current) newSide.current.focus();
      else focusWhenMade.current = true;
    },
  }));

  return <div className={`merge-pane merge-${layout}`} data-testid="merge-pane" ref={host} />;
}
