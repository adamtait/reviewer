// SPDX-License-Identifier: MIT

import { afterEach, describe, expect, it, vi } from 'vitest';

import { readClosed, writeClosed } from './closed';

afterEach(() => {
  sessionStorage.clear();
  vi.restoreAllMocks();
});

describe('the files closed on a pull request', () => {
  it('are read back as they were kept', () => {
    writeClosed('example', 'alpha', 7, new Set(['src/cart.ts', 'docs/old.md']));
    expect(readClosed('example', 'alpha', 7)).toEqual(new Set(['src/cart.ts', 'docs/old.md']));
  });

  it('are kept apart for each pull request', () => {
    writeClosed('example', 'alpha', 7, new Set(['src/cart.ts']));
    expect(readClosed('example', 'alpha', 8)).toEqual(new Set());
    expect(readClosed('example', 'beta', 7)).toEqual(new Set());
  });

  it('leave no entry once every file is open again', () => {
    writeClosed('example', 'alpha', 7, new Set(['src/cart.ts']));
    writeClosed('example', 'alpha', 7, new Set());
    expect(sessionStorage.length).toBe(0);
  });

  it('are none when the entry is not a list of paths', () => {
    sessionStorage.setItem('review-workspace:closed:example/alpha#7', '{"src/cart.ts":true}');
    expect(readClosed('example', 'alpha', 7)).toEqual(new Set());
    sessionStorage.setItem('review-workspace:closed:example/alpha#7', 'not json');
    expect(readClosed('example', 'alpha', 7)).toEqual(new Set());
    sessionStorage.setItem('review-workspace:closed:example/alpha#7', '["src/cart.ts", 3]');
    expect(readClosed('example', 'alpha', 7)).toEqual(new Set(['src/cart.ts']));
  });

  it('are none, and nothing fails, when storage is turned off', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('denied', 'SecurityError');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('full', 'QuotaExceededError');
    });
    expect(() => {
      writeClosed('example', 'alpha', 7, new Set(['src/cart.ts']));
    }).not.toThrow();
    expect(readClosed('example', 'alpha', 7)).toEqual(new Set());
  });
});
