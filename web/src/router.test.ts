// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';

import { type Place, href, parse, safeNext } from './router';

const at = (address: string) => parse(new URL(address, 'https://workspace.example'));
const sha = 'a'.repeat(40);

describe('addresses', () => {
  it('read the four views', () => {
    expect(at('/')).toEqual({ view: 'list' });
    expect(at('/signin')).toEqual({ view: 'signin', next: '/' });
    expect(at('/example/alpha/pull/12')).toEqual({ view: 'pull', owner: 'example', repo: 'alpha', number: 12 });
    expect(at(`/example/alpha/blob/${sha}/src/deep/file.ts`)).toEqual({
      view: 'code', owner: 'example', repo: 'alpha', sha, path: 'src/deep/file.ts',
    });
  });

  it('read a place in the diff and a line of a file', () => {
    expect(at('/example/alpha/pull/12#file=src%2Fsum.ts&line=3')).toEqual({
      view: 'pull', owner: 'example', repo: 'alpha', number: 12, file: 'src/sum.ts', line: 3,
    });
    expect(at(`/example/alpha/blob/${sha}/src/sum.ts?pull=12#L7`)).toEqual({
      view: 'code', owner: 'example', repo: 'alpha', sha, path: 'src/sum.ts', line: 7, pull: 12,
    });
  });

  it('survive a round trip', () => {
    const places: Place[] = [
      { view: 'list' },
      { view: 'signin', next: '/example/alpha/pull/12' },
      { view: 'pull', owner: 'example', repo: 'alpha', number: 12 },
      { view: 'pull', owner: 'example', repo: 'alpha', number: 12, file: 'src/a b&c=d.ts', line: 40 },
      { view: 'code', owner: 'example', repo: 'alpha', sha, path: 'src/a file#1?.ts', line: 2, pull: 12 },
      { view: 'code', owner: 'example', repo: 'alpha', sha, path: 'README.md' },
    ];
    for (const place of places) {
      expect(at(href(place)), href(place)).toEqual(place);
    }
  });

  it('treat anything else as not found', () => {
    for (const address of [
      '/example',
      '/example/alpha',
      '/example/alpha/pull/abc',
      '/example/alpha/pull/0',
      '/example/alpha/pull/12/extra',
      '/example/alpha/blob/main/src/a.ts', // a branch name is not a commit
      `/example/alpha/blob/${sha}`, // no path
      '/example/alpha/tree/main',
      '/%E0%A4%A',
    ]) {
      expect(at(address), address).toEqual({ view: 'notfound' });
    }
  });

  it('ignore a line that is not a number', () => {
    expect(at('/example/alpha/pull/12#file=a.ts&line=abc')).toEqual({
      view: 'pull', owner: 'example', repo: 'alpha', number: 12, file: 'a.ts',
    });
    expect(at(`/example/alpha/blob/${sha}/a.ts#Lx`)).toEqual({ view: 'code', owner: 'example', repo: 'alpha', sha, path: 'a.ts' });
  });
});

describe('where sign-in goes next', () => {
  it('is only ever a path on this site', () => {
    expect(safeNext('/example/alpha/pull/12#file=a.ts')).toBe('/example/alpha/pull/12#file=a.ts');
    for (const hostile of [
      null,
      '',
      'https://elsewhere.example/',
      '//elsewhere.example/',
      '/\\elsewhere.example/',
      'javascript:alert(1)',
      '/signin',
      '/signin?next=/signin',
    ]) {
      expect(safeNext(hostile), String(hostile)).toBe('/');
    }
  });

  it('is read from the sign-in address through the same check', () => {
    expect(at('/signin?next=%2Fexample%2Falpha%2Fpull%2F12')).toEqual({ view: 'signin', next: '/example/alpha/pull/12' });
    expect(at('/signin?next=https%3A%2F%2Felsewhere.example')).toEqual({ view: 'signin', next: '/' });
  });
});
