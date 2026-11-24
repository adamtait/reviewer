// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';

import errorNotFound from '../../../internal/workspace/api/testdata/examples/error_not_found.json';
import errorSignedOut from '../../../internal/workspace/api/testdata/examples/error_signed_out.json';
import definition from '../../../internal/workspace/api/testdata/examples/definition.json';
import file from '../../../internal/workspace/api/testdata/examples/file.json';
import fileMergeBase from '../../../internal/workspace/api/testdata/examples/file_merge_base.json';
import health from '../../../internal/workspace/api/testdata/examples/health.json';
import indexState from '../../../internal/workspace/api/testdata/examples/index_state.json';
import me from '../../../internal/workspace/api/testdata/examples/me.json';
import pullList from '../../../internal/workspace/api/testdata/examples/pull_list.json';
import pullView from '../../../internal/workspace/api/testdata/examples/pull_view.json';
import references from '../../../internal/workspace/api/testdata/examples/references.json';
import searchResult from '../../../internal/workspace/api/testdata/examples/search_result.json';
import tree from '../../../internal/workspace/api/testdata/examples/tree.json';
import type { ApiError, Definition, File, Health, IndexState, Me, PullList, PullView, References, SearchResult, Tree } from './types';

// The service's handler tests must produce these files exactly. Here each is
// checked against the type this application reads it as, in both directions:
// the type-checker refuses a field the type has and the file lacks, and
// `exactly` refuses a field the file has and the type does not know about.
//
// So a field renamed, added or removed on the Go side fails here until
// types.ts is brought into line, and the other way round fails the Go tests.

/** Every key in a recorded response, with array elements merged. */
function keys(value: unknown, prefix = ''): string[] {
  if (Array.isArray(value)) return [...new Set(value.flatMap((item) => keys(item, `${prefix}[]`)))];
  if (value !== null && typeof value === 'object') {
    return Object.entries(value).flatMap(([key, inner]) => {
      const path = prefix ? `${prefix}.${key}` : key;
      return [path, ...keys(inner, path)];
    });
  }
  return [];
}

function exactly(recorded: unknown, known: string[]) {
  expect(keys(recorded).sort()).toEqual([...known].sort());
}

describe('the recorded responses match the types', () => {
  it('me', () => {
    const typed: Me = me;
    exactly(typed, ['login', 'repositories', 'repositories[].owner', 'repositories[].name']);
  });

  it('health', () => {
    const typed: Health = health;
    exactly(typed, ['status', 'version']);
  });

  it('errors', () => {
    const notFound: ApiError = errorNotFound;
    const signedOut: ApiError = errorSignedOut;
    exactly(notFound, ['error', 'error.code', 'error.message']);
    exactly(signedOut, ['error', 'error.code', 'error.message']);
    expect(notFound.error.code).toBe('not_found');
    expect(signedOut.error.code).toBe('signed_out');
  });

  it('the pull request list', () => {
    const typed: PullList = pullList;
    exactly(typed, [
      'pulls', 'pulls[].number', 'pulls[].title', 'pulls[].author', 'pulls[].draft', 'pulls[].headSha', 'pulls[].updatedAt',
      'truncated',
    ]);
  });

  it('one pull request', () => {
    const typed = pullView as PullView;
    exactly(typed, [
      'number', 'title', 'body', 'author', 'state', 'draft', 'headSha', 'baseRef', 'mergeBaseSha',
      'files', 'files[].path', 'files[].previousPath', 'files[].status', 'filesTruncated', 'webUrl',
    ]);
    for (const changed of typed.files) {
      expect(['added', 'modified', 'deleted', 'renamed']).toContain(changed.status);
    }
  });

  it('the tree', () => {
    const typed = tree as Tree;
    exactly(typed, ['entries', 'entries[].path', 'entries[].kind']);
    for (const entry of typed.entries) {
      expect(['text', 'binary', 'too_large', 'other']).toContain(entry.kind);
    }
  });

  it('a file', () => {
    const typed = file as File;
    exactly(typed, ['path', 'kind', 'text']);
    exactly(fileMergeBase, ['path', 'kind', 'text']);
  });

  it('the index state', () => {
    const typed = indexState as IndexState;
    exactly(typed, ['state', 'reason', 'runUrl', 'toolVersion', 'documents']);
    expect(['none', 'running', 'failed', 'ready']).toContain(typed.state);
  });

  const location = (prefix: string) => ['path', 'line', 'character', 'endLine', 'endCharacter'].map((field) => `${prefix}[].${field}`);

  it('a definition', () => {
    const typed = definition as Definition;
    // The recorded answer has targets and no matches, so only the targets'
    // fields are present to be checked; a match has the search result's shape.
    exactly(typed, ['label', 'targets', ...location('targets'), 'package', 'version', 'matches', 'truncated']);
    expect(['index', 'text_search', 'external', 'none']).toContain(typed.label);
  });

  it('references', () => {
    const typed = references as References;
    exactly(typed, [
      'label', 'references', ...location('references'), 'truncated',
      'matches', 'matches[].path', 'matches[].line', 'matches[].text', 'matchesTruncated',
    ]);
  });

  it('a search result', () => {
    const typed: SearchResult = searchResult;
    exactly(typed, ['matches', 'matches[].path', 'matches[].line', 'matches[].text', 'truncated']);
  });
});
