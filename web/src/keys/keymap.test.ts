// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';

import { type KeyPress, KeyMatcher, isReserved, isTextField, keymap, prefixWindowMs } from './keymap';

const press = (key: string, modifiers: Partial<KeyPress> = {}): KeyPress => ({
  key,
  ctrlKey: false,
  metaKey: false,
  altKey: false,
  ...modifiers,
});

describe('the shortcut table', () => {
  it("triggers each action with that action's keys", () => {
    for (const binding of keymap) {
      const matcher = new KeyMatcher();
      const results = binding.keys.map((key) => matcher.feed(press(key)));
      expect(results.at(-1), binding.label).toBe(binding.action);
      if (binding.keys.length === 2) {
        expect(results[0], `${binding.label}: the prefix waits`).toBe('pending');
      }
    }
  });

  it('gives every action a different key sequence', () => {
    const sequences = keymap.map((b) => b.keys.join(' '));
    expect(new Set(sequences).size).toBe(sequences.length);
  });

  it('never makes a prefix key an action of its own', () => {
    const prefixes = new Set(keymap.filter((b) => b.keys.length === 2).map((b) => b.keys[0]));
    for (const binding of keymap) {
      if (binding.keys.length === 1) expect(prefixes.has(binding.keys[0])).toBe(false);
    }
  });

  it('uses no key the browser owns', () => {
    for (const binding of keymap) {
      for (const key of binding.keys) {
        expect(isReserved(press(key)), key).toBe(false);
      }
    }
  });

  it('has a touch route for every action', () => {
    for (const binding of keymap) {
      expect(binding.touch.length, binding.label).toBeGreaterThan(0);
    }
  });
});

describe('reserved keys', () => {
  it('are the function keys and the browser shortcuts the design lists', () => {
    for (const key of ['F5', 'F11', 'F12']) expect(isReserved(press(key))).toBe(true);
    for (const key of ['t', 'w', 'n', 'r', 'l', 'f', 'p', 's', 'T', 'R']) {
      expect(isReserved(press(key, { ctrlKey: true })), `Ctrl+${key}`).toBe(true);
      expect(isReserved(press(key, { metaKey: true })), `Cmd+${key}`).toBe(true);
    }
    expect(isReserved(press('j'))).toBe(false);
    expect(isReserved(press('r'))).toBe(false);
  });

  it('are never matched, with or without a prefix before them', () => {
    const matcher = new KeyMatcher();
    expect(matcher.feed(press('r', { ctrlKey: true }))).toBeNull();
    expect(matcher.feed(press('g'))).toBe('pending');
    // Ctrl+R after g is a reload, not "find references".
    expect(matcher.feed(press('r', { ctrlKey: true }))).toBeNull();
    // And the prefix is forgotten.
    expect(matcher.feed(press('r'))).toBeNull();
  });

  it('leave any key with Ctrl, Cmd or Alt to the browser', () => {
    const matcher = new KeyMatcher();
    expect(matcher.feed(press('j', { ctrlKey: true }))).toBeNull();
    expect(matcher.feed(press('d', { metaKey: true }))).toBeNull();
    expect(matcher.feed(press('/', { altKey: true }))).toBeNull();
  });
});

describe('prefix keys', () => {
  it('expire', () => {
    let now = 0;
    const matcher = new KeyMatcher(keymap, () => now);
    expect(matcher.feed(press('g'))).toBe('pending');
    now += prefixWindowMs + 1;
    // Too late to be "g then d": this d is the single key d.
    expect(matcher.feed(press('d'))).toBe('toggleDiff');
  });

  it('fall back to the single key when the second key completes nothing', () => {
    const matcher = new KeyMatcher();
    expect(matcher.feed(press('g'))).toBe('pending');
    expect(matcher.feed(press('j'))).toBe('nextChange');
  });

  it('ignore keys that mean nothing', () => {
    const matcher = new KeyMatcher();
    expect(matcher.feed(press('x'))).toBeNull();
    expect(matcher.feed(press('Shift'))).toBeNull();
  });
});

describe('text fields', () => {
  it('are where single keys are text, not shortcuts', () => {
    const input = document.createElement('input');
    const area = document.createElement('textarea');
    const editable = document.createElement('div');
    editable.setAttribute('contenteditable', 'true');
    const code = document.createElement('div');
    code.setAttribute('contenteditable', 'false');
    const button = document.createElement('button');

    expect(isTextField(input)).toBe(true);
    expect(isTextField(area)).toBe(true);
    expect(isTextField(editable)).toBe(true);
    // Read-only code is not a text field: shortcuts work while it has focus.
    expect(isTextField(code)).toBe(false);
    expect(isTextField(button)).toBe(false);
    expect(isTextField(null)).toBe(false);
  });
});
