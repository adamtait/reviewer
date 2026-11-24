// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import type { Page, Route } from '@playwright/test';

import type {
  AnswerEvent,
  ApiError,
  Ask,
  Definition,
  File,
  IndexState,
  Location,
  Match,
  Me,
  PullList,
  PullView,
  References,
  Report,
  Search,
  SearchResult,
  Tree,
} from '../src/api/types';

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
export const indexReady = example('index_state') as IndexState;

/** An answer's stream, exactly as the service's own tests record it. */
export const recordedAnswer = readFileSync(join(examples, 'question.sse'), 'utf8');
/** The stream of a question that was not sent, as recorded. */
export const recordedRefusal = readFileSync(join(examples, 'question_stopped.sse'), 'utf8');

/** Writes events as the service writes them: a name line, a data line, a blank line. */
export function stream(...events: AnswerEvent[]): string {
  return events.map((e) => `event: ${e.event}\ndata: ${JSON.stringify(e.data)}\n\n`).join('');
}

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
  // An added file that calls into a file the pull request does not touch.
  'src/tax.ts': text(
    'src/tax.ts',
    "import { round } from './round';\n\nexport const rate = 0.2;\nexport const withTax = (cents: number) => round(cents * (1 + rate));\n",
  ),
  'src/round.ts': text('src/round.ts', 'export function round(n: number): number {\n  return Math.round(n);\n}\n'),
  'src/receipt.ts': text(
    'src/receipt.ts',
    "import { sum } from './sum';\n\nexport const receipt = (prices: number[]) => `Total: ${sum(prices)}`;\n",
  ),
};

const mergeBase: Record<string, File> = {
  [recordedBase.path]: recordedBase,
  'README.md': text('README.md', '# Alpha\n\nA shop.\n'),
  'docs/old.md': text('docs/old.md', 'Notes nobody reads.\n'),
  'src/round.ts': text('src/round.ts', 'export function round(n: number): number {\n  return Math.round(n);\n}\n'),
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
  /** What the index route answers for the head commit. A test may change it while the page is open. */
  index: IndexState;
  /** Every "this was wrong" report received. */
  reports: Report[];
  /** Every lookup asked for, with the word found at its position. */
  lookups: { kind: string; sha: string; path: string; line: number; character: number; word: string }[];
  /** Whether example/alpha has an assistant. */
  assistant: boolean;
  /** The streams the next questions are answered with, in order. When empty, the recorded answer. */
  answers: string[];
  /** Every question asked, with the commit it was asked about. */
  asked: { sha: string; ask: Ask }[];
}

const span = (path: string, line: number, character: number, length: number): Location => ({
  path,
  line,
  character,
  endLine: line,
  endCharacter: character + length,
});

/** The identifier under a position, as the service finds one when the index has nothing there. */
function wordAt(file: File | undefined, line: number, character: number): string {
  const text = file?.text.split('\n')[line] ?? '';
  let start = character;
  let end = character;
  while (start > 0 && /[\w$]/.test(text.charAt(start - 1))) start--;
  while (end < text.length && /[\w$]/.test(text.charAt(end))) end++;
  return text.slice(start, end);
}

/** Lines of a commit that contain a word on its own: what the service's text search returns. */
function wholeWord(files: Record<string, File>, word: string): Match[] {
  const pattern = new RegExp(`(^|[^\\w$])${word.replace(/[$]/g, '\\$')}($|[^\\w$])`);
  return Object.values(files)
    .sort((a, b) => a.path.localeCompare(b.path))
    .flatMap((file) =>
      file.text
        .split('\n')
        .map((line, n): Match => ({ path: file.path, line: n, text: line }))
        .filter((match) => pattern.test(match.text)),
    );
}

// What the stand-in's index knows. `round` is defined in a file the pull
// request does not touch; `Math` belongs to a package outside the repository;
// `number` is in no index and on too many lines to be worth searching for.
// Any other word falls back to a text search, as it does in the service.
function definitionOf(files: Record<string, File>, path: string, line: number, character: number): Definition {
  const none: Definition = { label: 'none', targets: [], package: '', version: '', matches: [], truncated: false };
  const word = wordAt(files[path], line, character);
  if (word === 'round') return { ...none, label: 'index', targets: [span('src/round.ts', 0, 16, 5)] };
  if (word === 'Math') return { ...none, label: 'external', package: 'typescript', version: '6.0.3' };
  if (word === '' || word === 'number') return none;
  const matches = wholeWord(files, word);
  return matches.length === 0 ? none : { ...none, label: 'text_search', matches };
}

function referencesOf(files: Record<string, File>, path: string, line: number, character: number): References {
  const word = wordAt(files[path], line, character);
  const matches = word === '' ? [] : wholeWord(files, word);
  const references = word === 'round' ? [span('src/round.ts', 0, 16, 5), span('src/tax.ts', 0, 9, 5), span('src/tax.ts', 3, 40, 5)] : [];
  return {
    label: references.length > 0 ? 'index' : matches.length > 0 ? 'text_search' : 'none',
    references,
    truncated: false,
    matches,
    matchesTruncated: false,
  };
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
    index: indexReady,
    reports: [],
    lookups: [],
    assistant: true,
    answers: [],
    asked: [],
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
    if (url.pathname === '/api/v1/me') {
      return json(route, 200, { ...me, repositories: me.repositories.map((r) => ({ ...r, assistant: r.assistant && world.assistant })) });
    }

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
    if (rest === 'navigation/reports') {
      world.reports.push(request.postDataJSON() as Report);
      return route.fulfill({ status: 204 });
    }

    const question = /^commits\/([0-9a-f]{40})\/questions$/.exec(rest ?? '');
    if (question) {
      if (!world.assistant) return json(route, 404, { error: { code: 'no_assistant', message: 'This repository has no assistant configured.' } });
      world.asked.push({ sha: question[1] ?? '', ask: request.postDataJSON() as Ask });
      return route.fulfill({
        status: 200,
        contentType: 'text/event-stream; charset=utf-8',
        headers: { 'Cache-Control': 'no-store' },
        body: world.answers.shift() ?? recordedAnswer,
      });
    }

    const commit = /^commits\/([0-9a-f]{40})\/(tree|file|search|index|definition|references)$/.exec(rest ?? '');
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
      case 'index':
        // Only the head commit has an index. The merge base was never the tip of a push.
        return json(
          route,
          200,
          commit[1] === pullView.headSha ? world.index : { state: 'none', reason: '', runUrl: '', toolVersion: '', documents: 0 },
        );
      case 'definition':
      case 'references': {
        const path = url.searchParams.get('path') ?? '';
        const line = Number(url.searchParams.get('line'));
        const character = Number(url.searchParams.get('character'));
        // With no index, every lookup is a text search.
        const indexed = commit[1] === pullView.headSha && world.index.state === 'ready';
        const word = wordAt(files[path], line, character);
        world.lookups.push({ kind: commit[2], sha: commit[1] ?? '', path, line, character, word });
        if (commit[2] === 'definition') {
          const found = definitionOf(files, path, line, character);
          if (!indexed && (found.label === 'index' || found.label === 'external')) {
            const matches = wholeWord(files, word);
            return json(route, 200, { label: matches.length ? 'text_search' : 'none', targets: [], package: '', version: '', matches, truncated: false });
          }
          return json(route, 200, found);
        }
        const found = referencesOf(files, path, line, character);
        return json(route, 200, indexed ? found : { ...found, references: [], label: found.matches.length ? 'text_search' : 'none' });
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
