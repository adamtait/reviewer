// SPDX-License-Identifier: MIT

// The shortcut table. It follows the j/k and g-prefix conventions GitHub and
// Gerrit use. Every action also has a touch route, listed beside its keys in
// the help.

export type Action =
  | 'nextChange'
  | 'prevChange'
  | 'nextFile'
  | 'prevFile'
  | 'definition'
  | 'references'
  | 'ask'
  | 'search'
  | 'toggleDiff'
  | 'toggleFile'
  | 'toggleRendered'
  | 'comment'
  | 'switchPull'
  | 'back'
  | 'help';

export interface Binding {
  action: Action;
  /** One key, or a prefix and a key pressed one after the other. */
  keys: readonly [string] | readonly [string, string];
  label: string;
  /** How to do the same by touch. */
  touch: string;
}

export const keymap: readonly Binding[] = [
  { action: 'nextChange', keys: ['j'], label: 'Next change', touch: 'Buttons in the bottom bar' },
  { action: 'prevChange', keys: ['k'], label: 'Previous change', touch: 'Buttons in the bottom bar' },
  { action: 'nextFile', keys: [']'], label: 'Next file', touch: 'File list' },
  { action: 'prevFile', keys: ['['], label: 'Previous file', touch: 'File list' },
  { action: 'definition', keys: ['g', 'd'], label: 'Go to definition', touch: 'Tap the symbol, then "Definition"' },
  { action: 'references', keys: ['g', 'r'], label: 'Find references', touch: 'Tap the symbol, then "References"' },
  { action: 'ask', keys: ['a'], label: 'Ask about the selection or line', touch: 'Tap the line number, then "Ask"' },
  { action: 'search', keys: ['/'], label: 'Search code', touch: 'Search button' },
  { action: 'toggleDiff', keys: ['d'], label: 'Toggle diff and new code', touch: 'Toggle in the header' },
  { action: 'toggleFile', keys: ['o'], label: 'Close or open this file', touch: "Tap the file's name" },
  { action: 'toggleRendered', keys: ['m'], label: 'Draw this Markdown file, or show its text', touch: '"Rendered" on the file\'s bar, or "Show rendered Markdown"' },
  { action: 'comment', keys: ['c'], label: 'Comment on GitHub at this line', touch: 'Tap the line number, then "Comment on GitHub"' },
  { action: 'switchPull', keys: ['g', 'p'], label: 'Switch pull request', touch: 'Pull request list' },
  { action: 'back', keys: ['Escape'], label: 'Return to the diff', touch: '"Back to diff"' },
  { action: 'help', keys: ['?'], label: 'Shortcut help', touch: 'Menu' },
];

/** How a key is written in the help. */
export function keyLabel(key: string): string {
  return key === 'Escape' ? 'Esc' : key;
}

/** What a key press carries that the table cares about. */
export interface KeyPress {
  key: string;
  ctrlKey: boolean;
  metaKey: boolean;
  altKey: boolean;
}

const reservedPlain = new Set(['F5', 'F11', 'F12']);
const reservedWithModifier = new Set(['t', 'w', 'n', 'r', 'l', 'f', 'p', 's']);

/**
 * Keys the browser owns. No shortcut may use one, and the application never
 * stops one reaching the browser.
 */
export function isReserved(press: KeyPress): boolean {
  if (reservedPlain.has(press.key)) return true;
  return (press.ctrlKey || press.metaKey) && reservedWithModifier.has(press.key.toLowerCase());
}

/** How long a prefix key waits for its second key. */
export const prefixWindowMs = 1500;

/**
 * Turns key presses into actions, remembering a prefix key between presses.
 *
 * `feed` returns the action a press completes, "pending" when the press is a
 * prefix waiting for its second key, and null when the press means nothing.
 */
export class KeyMatcher {
  private prefix: string | null = null;
  private prefixAt = 0;

  constructor(
    private readonly bindings: readonly Binding[] = keymap,
    private readonly now: () => number = () => Date.now(),
  ) {}

  feed(press: KeyPress): Action | 'pending' | null {
    // A press with Ctrl, Cmd or Alt is the browser's or the system's, never ours.
    if (press.ctrlKey || press.metaKey || press.altKey || isReserved(press)) {
      this.prefix = null;
      return null;
    }

    const prefix = this.prefix !== null && this.now() - this.prefixAt <= prefixWindowMs ? this.prefix : null;
    this.prefix = null;

    if (prefix !== null) {
      const completed = this.bindings.find((b) => b.keys.length === 2 && b.keys[0] === prefix && b.keys[1] === press.key);
      if (completed) return completed.action;
      // Not a second key for this prefix: treat it as a fresh press.
    }

    const single = this.bindings.find((b) => b.keys.length === 1 && b.keys[0] === press.key);
    if (single) return single.action;

    if (this.bindings.some((b) => b.keys.length === 2 && b.keys[0] === press.key)) {
      this.prefix = press.key;
      this.prefixAt = this.now();
      return 'pending';
    }
    return null;
  }
}

/** Whether typing is going into a field, where single keys are text and not shortcuts. */
export function isTextField(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement) {
    return true;
  }
  return target.isContentEditable || target.getAttribute('contenteditable') === 'true';
}
