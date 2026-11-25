// SPDX-License-Identifier: MIT

import { afterEach, describe, expect, it } from 'vitest';

import definition from '../../../internal/workspace/api/testdata/examples/definition.json';
import type { Definition, Location } from '../api/types';
import { type Arrival, arrival, definitionOutcome, goTo, labelText, lookupCommit, reportFor, targetHref } from './navigation';

const head = '1'.repeat(40);
const mergeBase = '3'.repeat(40);
const at = (path: string, line: number): Location => ({ path, line, character: 0, endLine: line, endCharacter: 3 });

afterEach(() => {
  window.history.replaceState(null, '', '/');
});

describe('which commit a lookup uses', () => {
  it('is the merge base for a removed line and the head for any other', () => {
    const pull = { headSha: head, mergeBaseSha: mergeBase };
    expect(lookupCommit(pull, 'old')).toBe(mergeBase);
    expect(lookupCommit(pull, 'new')).toBe(head);
  });
});

describe('what a definition lookup leads to', () => {
  const answer = (over: Partial<Definition>): Definition => ({
    label: 'none', targets: [], package: '', version: '', matches: [], truncated: false, ...over,
  });

  it('follows one target from the index', () => {
    const recorded = definition as Definition;
    expect(definitionOutcome(recorded)).toEqual({ action: 'jump', target: recorded.targets[0] });
  });

  it('shows several targets and lets the reader choose', () => {
    const result = answer({ label: 'index', targets: [at('a.ts', 1), at('a.ts', 6)] });
    expect(definitionOutcome(result)).toEqual({ action: 'show', result });
  });

  it('never follows a text match as if it were a definition', () => {
    const result = answer({ label: 'text_search', matches: [{ path: 'a.ts', line: 1, text: 'sum()' }] });
    expect(definitionOutcome(result)).toEqual({ action: 'show', result });
    // Even if a response wrongly carried a target under that label.
    const odd = answer({ label: 'text_search', targets: [at('a.ts', 1)] });
    expect(definitionOutcome(odd).action).toBe('show');
  });

  it('shows a package outside the repository, and "not found", with nothing to jump to', () => {
    expect(definitionOutcome(answer({ label: 'external', package: 'rxjs', version: '7.8.2' })).action).toBe('show');
    expect(definitionOutcome(answer({ label: 'none' })).action).toBe('show');
  });
});

describe('going to a result', () => {
  const context = { owner: 'example', repo: 'alpha', sha: head, pull: 12 };
  const how: Arrival = { label: 'index', kind: 'definition', origin: { sha: head, path: 'src/cart.ts', line: 3, character: 9 } };

  it('addresses the target at the commit the lookup was made at, keeping the pull request', () => {
    expect(targetHref(context, at('src/money.ts', 12))).toBe(`/example/alpha/blob/${head}/src/money.ts?pull=12#L13`);
    expect(targetHref({ ...context, pull: undefined }, at('a.ts', 0))).toBe(`/example/alpha/blob/${head}/a.ts#L1`);
  });

  it('adds a history entry, so back returns to where the reader was', () => {
    window.history.replaceState(null, '', '/example/alpha/pull/12#file=src%2Fcart.ts&line=4');
    const before = window.history.length;
    goTo(context, at('src/money.ts', 12), how);
    expect(window.location.pathname).toBe(`/example/alpha/blob/${head}/src/money.ts`);
    expect(window.history.length).toBe(before + 1);
  });

  it('carries how the page was reached in the history entry, not the address', () => {
    goTo(context, at('src/money.ts', 12), how);
    expect(arrival(window.history.state)).toEqual(how);
    expect(window.location.href).not.toContain('index');
    expect(window.location.href).not.toContain('cart');
  });

  it('reads nothing from a history entry that was not a lookup', () => {
    for (const state of [null, undefined, 'text', 7, {}, { arrival: null }, { arrival: { label: 'index' } }]) {
      expect(arrival(state)).toBeNull();
    }
  });
});

describe('a "this was wrong" report', () => {
  it('names the lookup that gave the result, and its label', () => {
    const how: Arrival = { label: 'index', kind: 'definition', origin: { sha: head, path: 'src/cart.ts', line: 3, character: 9 } };
    expect(reportFor(how)).toEqual({ sha: head, path: 'src/cart.ts', line: 3, character: 9, kind: 'definition', label: 'index' });
  });
});

describe('labels', () => {
  it('name a source, and none of them claims to be right', () => {
    expect(labelText.index).toBe('From the CI-built index');
    for (const text of Object.values(labelText)) {
      expect(text.toLowerCase()).not.toMatch(/correct|verified|accurate|exact/);
    }
  });

  it('exist for every label the service sends', () => {
    expect(Object.keys(labelText).sort()).toEqual(['external', 'index', 'none', 'text_search']);
  });
});
