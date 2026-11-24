// SPDX-License-Identifier: MIT

import { cleanup, render, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiFailure } from '../api/client';
import type { ChangedFile, File, PullView } from '../api/types';
import pullView from '../../../internal/workspace/api/testdata/examples/pull_view.json';

// The code editor needs a real browser's layout, which the journeys in e2e/
// give it. Here it is replaced by components that show what they were handed,
// so these tests are about which text is read from where.
vi.mock('./CodePane', () => ({
  CodePane: (props: { path: string; text: string; side?: string }) => (
    <pre data-testid="code" data-side={props.side ?? 'new'}>
      {props.text}
    </pre>
  ),
  MergePane: (props: { oldText: string; newText: string; layout: string }) => (
    <div data-testid="merge" data-layout={props.layout}>
      <pre data-testid="old">{props.oldText}</pre>
      <pre data-testid="new">{props.newText}</pre>
    </div>
  ),
}));

const file = vi.fn<(owner: string, repo: string, sha: string, path: string) => Promise<File>>();
vi.mock('../api/client', async (original) => {
  const actual = await original<typeof import('../api/client')>();
  return { ...actual, api: { ...actual.api, file: (...args: [string, string, string, string]) => file(...args) } };
});

const { FileDiff, sources } = await import('./DiffView');

const pull = pullView as PullView;
const head = pull.headSha;
const mergeBase = pull.mergeBaseSha;

const changed = (path: string): ChangedFile => {
  const found = pull.files.find((f) => f.path === path);
  if (!found) throw new Error(`the example pull request does not change ${path}`);
  return found;
};

/** Answers a file request with text that says where it was read from. */
function serve(overrides: Record<string, File | Error> = {}) {
  file.mockImplementation((_owner, _repo, sha, path) => {
    const answer = overrides[`${sha}:${path}`];
    if (answer instanceof Error) return Promise.reject(answer);
    return Promise.resolve(answer ?? { path, kind: 'text', text: `${path} at ${sha === head ? 'head' : sha === mergeBase ? 'merge base' : sha}` });
  });
}

afterEach(() => {
  cleanup();
  file.mockReset();
});

const show = (path: string, mode: 'diff' | 'new' = 'diff', layout: 'split' | 'unified' = 'split') =>
  render(<FileDiff owner="example" repo="alpha" pull={pull} file={changed(path)} mode={mode} layout={layout} eager />);

describe('where each side of a diff is read from', () => {
  it('is the merge base and the head commit, never the base branch', () => {
    expect(mergeBase).not.toBe(head);
    expect(sources(pull, changed('src/cart.ts'))).toEqual({
      before: { sha: mergeBase, path: 'src/cart.ts' },
      after: { sha: head, path: 'src/cart.ts' },
    });
  });

  it("is a renamed file's old path on the merge base", () => {
    expect(sources(pull, changed('src/sum.ts'))).toEqual({
      before: { sha: mergeBase, path: 'src/total.ts' },
      after: { sha: head, path: 'src/sum.ts' },
    });
  });

  it('is one side only for a file that was added or deleted', () => {
    expect(sources(pull, changed('src/tax.ts'))).toEqual({ before: null, after: { sha: head, path: 'src/tax.ts' } });
    expect(sources(pull, changed('docs/old.md'))).toEqual({ before: { sha: mergeBase, path: 'docs/old.md' }, after: null });
  });
});

describe('a changed file', () => {
  it('draws removed lines from the merge base and added lines from the head', async () => {
    serve();
    const { findByTestId } = show('src/cart.ts');
    expect((await findByTestId('old')).textContent).toBe('src/cart.ts at merge base');
    expect((await findByTestId('new')).textContent).toBe('src/cart.ts at head');
    expect(file).toHaveBeenCalledWith('example', 'alpha', mergeBase, 'src/cart.ts', expect.anything());
    expect(file).toHaveBeenCalledWith('example', 'alpha', head, 'src/cart.ts', expect.anything());
    expect(file).toHaveBeenCalledTimes(2);
  });

  it('reads a renamed file at its old path for the old side', async () => {
    serve();
    const { findByTestId, getByText } = show('src/sum.ts');
    expect((await findByTestId('old')).textContent).toBe('src/total.ts at merge base');
    expect((await findByTestId('new')).textContent).toBe('src/sum.ts at head');
    expect(getByText(/src\/total\.ts →/)).toBeTruthy();
  });

  it('shows an added file as its new text, with one request', async () => {
    serve();
    const { findByTestId, queryByTestId } = show('src/tax.ts');
    const code = await findByTestId('code');
    expect(code.textContent).toBe('src/tax.ts at head');
    expect(code.dataset.side).toBe('new');
    expect(queryByTestId('merge')).toBeNull();
    expect(file).toHaveBeenCalledTimes(1);
  });

  it('shows a deleted file as its old text, marked as the old side', async () => {
    serve();
    const { findByTestId } = show('docs/old.md');
    const code = await findByTestId('code');
    expect(code.textContent).toBe('docs/old.md at merge base');
    expect(code.dataset.side).toBe('old');
    expect(file).toHaveBeenCalledWith('example', 'alpha', mergeBase, 'docs/old.md', expect.anything());
  });

  it('shows only the new version when the diff is toggled off', async () => {
    serve();
    const modified = show('src/cart.ts', 'new');
    expect((await modified.findByTestId('code')).textContent).toBe('src/cart.ts at head');
    expect(modified.queryByTestId('merge')).toBeNull();
    cleanup();

    const deleted = show('docs/old.md', 'new');
    expect(await deleted.findByText('This pull request deletes this file.')).toBeTruthy();
  });

  it('passes the layout on: two columns, or one on a narrow screen', async () => {
    serve();
    const { findByTestId } = show('src/cart.ts', 'diff', 'unified');
    expect((await findByTestId('merge')).dataset.layout).toBe('unified');
  });

  it('says why a file that is not text is not shown, with a link to GitHub', async () => {
    serve({ [`${head}:src/cart.ts`]: { path: 'src/cart.ts', kind: 'binary', text: '' } });
    const { findByText, getByRole, queryByTestId } = show('src/cart.ts');
    expect(await findByText(/not text/)).toBeTruthy();
    expect(queryByTestId('merge')).toBeNull();
    const link = getByRole('link', { name: 'Open it on GitHub' });
    expect(link.getAttribute('href')).toBe(`https://github.example/example/alpha/blob/${head}/src/cart.ts`);
    expect(link.getAttribute('rel')).toContain('noopener');
  });

  it('says so when the archive does not hold a file GitHub lists as changed', async () => {
    serve({ [`${head}:src/cart.ts`]: new ApiFailure(404, 'not_found', 'not found') });
    const { findByText } = show('src/cart.ts');
    expect(await findByText(/not in the archive GitHub provides/)).toBeTruthy();
  });

  it('offers another try when a read fails, and reads again', async () => {
    serve({ [`${head}:src/cart.ts`]: new ApiFailure(504, 'github_timeout', 'GitHub did not answer in time; try again') });
    const { findByText, findByTestId, getByRole } = show('src/cart.ts');
    expect(await findByText(/GitHub did not answer in time/)).toBeTruthy();

    serve();
    getByRole('button', { name: 'Try again' }).click();
    expect((await findByTestId('new')).textContent).toBe('src/cart.ts at head');
  });

  it('waits to read a file until it is near the screen', async () => {
    const observed: Element[] = [];
    let reveal: () => void = () => undefined;
    vi.stubGlobal(
      'IntersectionObserver',
      class {
        constructor(private readonly callback: (entries: { isIntersecting: boolean }[]) => void) {
          reveal = () => {
            this.callback([{ isIntersecting: true }]);
          };
        }
        observe(element: Element) {
          observed.push(element);
        }
        disconnect() {
          return undefined;
        }
      },
    );
    try {
      serve();
      const { findByTestId } = render(
        <FileDiff owner="example" repo="alpha" pull={pull} file={changed('src/cart.ts')} mode="diff" layout="split" />,
      );
      expect(observed).toHaveLength(1);
      expect(file).not.toHaveBeenCalled();

      reveal();
      expect((await findByTestId('new')).textContent).toBe('src/cart.ts at head');
      await waitFor(() => {
        expect(file).toHaveBeenCalledTimes(2);
      });
    } finally {
      vi.unstubAllGlobals();
    }
  });
});
