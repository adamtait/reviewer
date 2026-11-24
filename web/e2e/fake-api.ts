// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import type { Page, Route } from '@playwright/test';

import type { ApiError, File, Me, PullList, PullView, Search, SearchResult, Tree } from '../src/api/types';

// A stand-in for the service, inside the browser. It answers with the
// responses recorded under internal/workspace/api/testdata/examples, which the
// service's own tests must produce exactly. A field the service renames
// therefore breaks these journeys until the application is brought into line.

const examples = join(dirname(fileURLToPath(import.meta.url)), '..', '..', 'internal', 'workspace', 'api', 'testdata', 'examples');

function example(name: string): unknown {
  return JSON.parse(readFileSync(join(examples, `${name}.json`), 'utf8'));
}

// Read as the types the application uses. src/api/examples.test.ts is what
// checks that each recorded response really has its type's shape.
export const me = example('me') as Me;
export const pullList = example('pull_list') as PullList;
export const pullView = example('pull_view') as PullView;
export const tree = example('tree') as Tree;
export const searchResult = example('search_result') as SearchResult;
const notFound = example('error_not_found') as ApiError;
const signedOut = example('error_signed_out') as ApiError;
const recordedHead = example('file') as File;
const recordedBase = example('file_merge_base') as File;

const text = (path: string, body: string): File => ({ path, kind: 'text', text: body });

// The two commits of the example pull request. Two files are recorded
// responses; the rest are the same fixture the service's tests use.
const head: Record<string, File> = {
  [recordedHead.path]: recordedHead,
  'README.md': text('README.md', '# Alpha\n\nA shop.\n'),
  'logo.png': { path: 'logo.png', kind: 'binary', text: '' },
  'src/cart.ts': text(
    'src/cart.ts',
    "import { sum } from './sum';\n\nexport function cartTotal(prices: number[]): number {\n  return sum(prices);\n}\n",
  ),
  'src/tax.ts': text('src/tax.ts', 'export const rate = 0.2;\n'),
  'src/receipt.ts': text(
    'src/receipt.ts',
    "import { sum } from './sum';\n\nexport const receipt = (prices: number[]) => `Total: ${sum(prices)}`;\n",
  ),
};

const mergeBase: Record<string, File> = {
  [recordedBase.path]: recordedBase,
  'README.md': text('README.md', '# Alpha\n\nA shop.\n'),
  'docs/old.md': text('docs/old.md', 'Notes nobody reads.\n'),
  'src/cart.ts': text(
    'src/cart.ts',
    "import { total } from './total';\n\nexport function cartTotal(prices: number[]): number {\n  return total(prices);\n}\n",
  ),
  'src/receipt.ts': text(
    'src/receipt.ts',
    "import { total } from './total';\n\nexport const receipt = (prices: number[]) => `Total: ${total(prices)}`;\n",
  ),
};

export interface World {
  /** Whether the browser holds a session. Sign-in sets it. */
  signedIn: boolean;
  /** Repositories this user can read, as "owner/name". Everything else is "not found". */
  readable: Set<string>;
  /** Every request the page made, as "METHOD path". */
  requests: string[];
  /** The body of every search, to check what was asked. */
  searches: Search[];
}

const json = (route: Route, status: number, body: unknown) =>
  route.fulfill({ status, contentType: 'application/json; charset=utf-8', body: JSON.stringify(body) });

/** Installs the stand-in on a page and returns its state, which a test may change. */
export async function fakeApi(page: Page, initial: Partial<World> = {}): Promise<World> {
  const world: World = {
    signedIn: true,
    readable: new Set(me.repositories.map((r) => `${r.owner}/${r.name}`)),
    requests: [],
    searches: [],
    ...initial,
  };

  await page.route(/\/(api|auth)\//, async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const method = request.method();
    world.requests.push(`${method} ${url.pathname}`);

    // As the service does: a non-GET without the header is refused.
    if (method !== 'GET' && request.headers()['x-workspace-request'] !== '1') {
      return json(route, 400, { error: { code: 'bad_request', message: 'missing header' } });
    }

    if (url.pathname === '/auth/token') {
      const { token } = request.postDataJSON() as { token: string };
      if (!token.startsWith('github_pat_')) {
        return json(route, 401, { error: { code: 'not_fine_grained', message: 'Paste a fine-grained personal access token.' } });
      }
      world.signedIn = true;
      return route.fulfill({ status: 204 });
    }
    if (url.pathname === '/auth/signout') {
      world.signedIn = false;
      return route.fulfill({ status: 204 });
    }

    if (!world.signedIn) return json(route, 401, signedOut);
    if (url.pathname === '/api/v1/me') return json(route, 200, me);

    const match = /^\/api\/v1\/repos\/([^/]+)\/([^/]+)\/(.*)$/.exec(url.pathname);
    if (!match) return json(route, 404, notFound);
    const [, owner, name, rest] = match;
    // One answer for a repository that is not there and one that is not this
    // user's to see, whatever is asked of it.
    if (!world.readable.has(`${owner}/${name}`)) return json(route, 404, notFound);
    if (`${owner}/${name}` !== 'example/alpha') {
      return rest === 'pulls' ? json(route, 200, { pulls: [], truncated: false }) : json(route, 404, notFound);
    }

    if (rest === 'pulls') return json(route, 200, pullList);
    if (rest === `pulls/${pullView.number}`) return json(route, 200, pullView);

    const commit = /^commits\/([0-9a-f]{40})\/(tree|file|search)$/.exec(rest ?? '');
    if (!commit) return json(route, 404, notFound);
    const files = commit[1] === pullView.headSha ? head : commit[1] === pullView.mergeBaseSha ? mergeBase : null;
    if (!files) return json(route, 404, notFound);

    switch (commit[2]) {
      case 'tree':
        return json(route, 200, commit[1] === pullView.headSha ? tree : { entries: Object.values(files).map((f) => ({ path: f.path, kind: f.kind })) });
      case 'file': {
        const found = files[url.searchParams.get('path') ?? ''];
        return found ? json(route, 200, found) : json(route, 404, notFound);
      }
      default: {
        const search = request.postDataJSON() as Search;
        world.searches.push(search);
        return json(route, 200, search.query.toLowerCase().startsWith('sum') ? searchResult : { matches: [], truncated: false });
      }
    }
  });

  return world;
}
