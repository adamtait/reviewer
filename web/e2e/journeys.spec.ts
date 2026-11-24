// SPDX-License-Identifier: MIT

import { type Page, expect, test } from '@playwright/test';

import { fakeApi, indexReady, pullView } from './fake-api';

// The journeys the design names for the first version of the web application,
// each run at a desktop size and at 390 points wide (see playwright.config.ts).

const pull = `/example/alpha/pull/${pullView.number}`;
const narrow = (page: Page) => (page.viewportSize()?.width ?? 1280) < 900;

// The page is served under the service's content security policy. Anything
// the policy stops is reported by the browser, and fails the journey it
// happened in: a page that only works with the policy off does not work.
const refused = new WeakMap<Page, string[]>();
const policies = new WeakMap<Page, string[]>();

test.beforeEach(({ page }) => {
  const seen: string[] = [];
  const sent: string[] = [];
  refused.set(page, seen);
  policies.set(page, sent);
  page.on('console', (message) => {
    if (message.type() === 'error' && /content security policy/i.test(message.text())) seen.push(message.text());
  });
  page.on('pageerror', (error) => {
    seen.push(error.message);
  });
  page.on('response', (response) => {
    const request = response.request();
    if (request.isNavigationRequest() && request.frame() === page.mainFrame()) {
      sent.push(response.headers()['content-security-policy'] ?? '');
    }
  });
});

test.afterEach(({ page }, info) => {
  // A journey skipped for the wrong screen size loaded nothing.
  if (info.status === 'skipped') return;
  const sent = policies.get(page) ?? [];
  expect(sent.length, 'the journey loaded the page at least once').toBeGreaterThan(0);
  for (const policy of sent) {
    expect(policy, 'every load of the page carries the policy').toContain("default-src 'none'");
  }
  expect(refused.get(page) ?? [], 'errors and policy refusals in the browser').toEqual([]);
});

test.describe('journey 1: read the description, then the diff', () => {
  test('the title and description come first, then the files, then the diff', async ({ page }) => {
    await fakeApi(page);
    await page.goto('/');

    // From the list of open pull requests.
    await page.getByRole('link', { name: /Rename total to sum/ }).click();
    await expect(page).toHaveURL(pull);

    await expect(page.getByRole('heading', { level: 1 })).toContainText('Rename total to sum');
    await expect(page.getByTestId('description')).toContainText('returned a sum of line prices');

    const files = page.getByRole('navigation', { name: 'Changed files' });
    await expect(files.getByRole('link')).toHaveCount(pullView.files.length);
    await expect(files).toContainText('src/sum.ts');

    // The order on the page is the order the journey gives.
    const order = await page.evaluate(() => {
      const top = (selector: string) => document.querySelector(selector)?.getBoundingClientRect().top ?? -1;
      return [top('h1'), top('[data-testid=description]'), top('.file-list'), top('.diffs')];
    });
    expect([...order].sort((a, b) => a - b)).toEqual(order);

    // The diff: the old name is on the removed side and the new name on the added.
    const cart = page.locator('[data-path="src/cart.ts"]');
    await expect(cart.getByTestId('merge-pane')).toBeVisible();
    await expect(cart).toContainText('return sum(prices);');
    await expect(cart).toContainText('return total(prices);');
  });

  test('the description is shown as text', async ({ page }) => {
    await fakeApi(page);
    await page.route(`**/api/v1/repos/example/alpha/pulls/${pullView.number}`, (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ...pullView, body: '<img src=x onerror="document.title=\'pwned\'"><b>bold</b>' }),
      }),
    );
    await page.goto(pull);
    await expect(page.getByTestId('description')).toHaveText('<img src=x onerror="document.title=\'pwned\'"><b>bold</b>');
    await expect(page.getByTestId('description').locator('img, b')).toHaveCount(0);
    await expect(page).toHaveTitle('Review workspace');
  });

  test('a renamed file is compared with its old path, and added and deleted files are shown whole', async ({ page }) => {
    await fakeApi(page);
    await page.goto(pull);

    const renamed = page.locator('[data-path="src/sum.ts"]');
    await expect(renamed).toContainText('src/total.ts →');
    await expect(renamed).toContainText('export function sum(');
    await expect(renamed).toContainText('export function total(');

    await expect(page.locator('[data-path="src/tax.ts"]')).toContainText('export const rate = 0.2;');
    await expect(page.locator('[data-path="docs/old.md"]')).toContainText('Notes nobody reads.');
  });

  test('every place has an address, and back returns to the diff', async ({ page }) => {
    await fakeApi(page);
    await page.goto(`${pull}#file=src%2Ftax.ts`);
    await expect(page.getByRole('navigation', { name: 'Changed files' }).locator('li.active')).toContainText('src/tax.ts');

    await page.goto(`/example/alpha/blob/${pullView.headSha}/src/receipt.ts?pull=${pullView.number}#L3`);
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('src/receipt.ts');
    await expect(page.getByText('Outside this pull request’s changes')).toBeVisible();
    await expect(page.locator('.cm-target-line')).toContainText('receipt');

    await page.getByRole('link', { name: /Back to diff/ }).click();
    await expect(page).toHaveURL(pull);
    await page.goBack();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('src/receipt.ts');
  });
});

/**
 * Clicks or taps a word in the code: the nth occurrence of `word` on a line of
 * one editor. The point is found from the text itself, so it does not depend
 * on how the editor splits a line into coloured spans.
 */
async function pressWord(
  page: Page,
  editor: string,
  line: number,
  word: string,
  how: 'click' | 'alt-click' | 'tap' = 'click',
): Promise<void> {
  const lineText = page.locator(`${editor} .cm-content .cm-line`).filter({ hasText: word });
  await lineText.first().waitFor();
  const point = await page.evaluate(
    ([selector, lineNumber, text]) => {
      // Lines the editor has drawn, matched to their numbers by the gutter.
      const root = document.querySelector(selector);
      if (!root) return null;
      // The gutter's first element is a hidden one the editor sizes itself by.
      const numbers = Array.from(root.querySelectorAll('.cm-lineNumbers .cm-gutterElement'))
        .filter((e) => getComputedStyle(e).visibility !== 'hidden')
        .map((e) => e.textContent)
        .filter((n) => n !== '' && !Number.isNaN(Number(n)));
      const lines = Array.from(root.querySelectorAll('.cm-content .cm-line'));
      const wanted = numbers.indexOf(String(lineNumber));
      // The numbered line and no other: a word can be on several lines.
      const target = lines.find((l, i) => i === wanted && l.textContent.includes(text));
      if (!target) return null;
      // To the middle of the screen, clear of the bar fixed to its bottom edge.
      target.scrollIntoView({ block: 'center' });
      const walker = document.createTreeWalker(target, NodeFilter.SHOW_TEXT);
      let offset = target.textContent.indexOf(text);
      for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        const length = node.textContent?.length ?? 0;
        if (offset < length) {
          const range = document.createRange();
          range.setStart(node, offset);
          range.setEnd(node, Math.min(length, offset + 2));
          const box = range.getBoundingClientRect();
          return { x: box.left + box.width / 2, y: box.top + box.height / 2 };
        }
        offset -= length;
      }
      return null;
    },
    [editor, line, word] as const,
  );
  if (!point) throw new Error(`"${word}" is not on line ${line} of ${editor}`);
  if (how === 'tap') {
    await page.touchscreen.tap(point.x, point.y);
  } else if (how === 'alt-click') {
    await page.keyboard.down('Alt');
    await page.mouse.click(point.x, point.y);
    await page.keyboard.up('Alt');
  } else {
    await page.mouse.click(point.x, point.y);
  }
}

const round = `/example/alpha/blob/${pullView.headSha}/src/round.ts?pull=${pullView.number}#L1`;
const taxEditor = '[data-path="src/tax.ts"]';

test.describe('journey 2: follow a symbol into unchanged code and return', () => {
  test('go to definition lands outside the change, says where the result came from, and one action returns', async ({ page }) => {
    const world = await fakeApi(page);
    await page.goto(pull);
    await expect(page.getByTestId('index-status')).toContainText('Navigation uses the index CI built for this commit');
    await page.locator(taxEditor).scrollIntoViewIfNeeded();

    // `round(...)` on line 4 of the added file src/tax.ts.
    if (narrow(page)) {
      await pressWord(page, taxEditor, 4, 'round(cents', 'tap');
      const sheet = page.getByRole('dialog', { name: 'src/tax.ts, line 4' });
      await sheet.getByRole('button', { name: /Definition/ }).tap();
    } else {
      await pressWord(page, taxEditor, 4, 'round(cents');
      await page.keyboard.press('g');
      await page.keyboard.press('d');
    }

    await expect(page).toHaveURL(round);
    // The lookup was made where the reader pointed: on `round`, line 4, at the head commit.
    expect(world.lookups).toEqual([
      expect.objectContaining({ kind: 'definition', sha: pullView.headSha, path: 'src/tax.ts', line: 3, word: 'round' }),
    ]);
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('src/round.ts');
    await expect(page.getByText('Outside this pull request’s changes')).toBeVisible();
    const arrival = page.getByTestId('arrival');
    await expect(arrival).toContainText('From the CI-built index');
    await expect(arrival).toContainText('It is not checked.');
    await expect(page.locator('.cm-target-line')).toContainText('export function round');

    // One action, back to the same file and line of the diff.
    await page.getByRole('link', { name: /Back to where you were/ }).click();
    await expect(page).toHaveURL(`${pull}#file=src%2Ftax.ts&line=4`);
    await expect(page.getByRole('navigation', { name: 'Changed files' }).locator('li.active')).toContainText('src/tax.ts');
    await expect(page.locator(`${taxEditor} .cm-target-line`)).toContainText('withTax');

    // And forward again: the page still knows how it was reached.
    await page.goForward();
    await expect(page.getByTestId('arrival')).toContainText('From the CI-built index');
  });

  test('a click with Alt held goes straight to the definition', async ({ page }) => {
    test.skip(narrow(page), 'a mouse');
    await fakeApi(page);
    await page.goto(pull);
    await page.locator(taxEditor).scrollIntoViewIfNeeded();
    await pressWord(page, taxEditor, 4, 'round(cents', 'alt-click');
    await expect(page).toHaveURL(round);
    await page.keyboard.press('Escape');
    await expect(page).toHaveURL(`${pull}#file=src%2Ftax.ts&line=4`);
  });

  test('a result can be reported as wrong, and the report names the lookup and nothing else', async ({ page }) => {
    test.skip(narrow(page), 'a keyboard');
    const world = await fakeApi(page);
    await page.goto(pull);
    await page.locator(taxEditor).scrollIntoViewIfNeeded();
    await pressWord(page, taxEditor, 4, 'round(cents', 'alt-click');
    await expect(page).toHaveURL(round);

    await page.getByRole('button', { name: 'This was wrong' }).click();
    await expect(page.getByText('Reported.')).toBeVisible();
    expect(world.reports).toHaveLength(1);
    const [report] = world.reports;
    expect(report).toMatchObject({ sha: pullView.headSha, path: 'src/tax.ts', line: 3, kind: 'definition', label: 'index' });
    expect(Object.keys(report ?? {}).sort()).toEqual(['character', 'kind', 'label', 'line', 'path', 'sha']);
  });

  test('references are listed from the index beside the text matches', async ({ page }) => {
    test.skip(narrow(page), 'a keyboard');
    const world = await fakeApi(page);
    await page.goto(pull);
    await page.locator(taxEditor).scrollIntoViewIfNeeded();
    await pressWord(page, taxEditor, 4, 'round(cents');
    await page.keyboard.press('g');
    await page.keyboard.press('r');

    const panel = page.getByRole('region', { name: 'References' });
    const fromIndex = panel.getByRole('region', { name: 'From the CI-built index' });
    const fromSearch = panel.getByRole('region', { name: 'From text search' });
    await expect(panel).toBeVisible();
    expect(world.lookups).toEqual([expect.objectContaining({ kind: 'references', path: 'src/tax.ts', line: 3, word: 'round' })]);
    await expect(fromIndex.getByRole('listitem')).toHaveCount(3);
    await expect(fromSearch.getByRole('listitem')).not.toHaveCount(0);
    await expect(fromSearch).toContainText('whatever the index says');
    // The address has not changed: a list is shown, not followed.
    await expect(page).toHaveURL(new RegExp(`${pull}($|#)`));

    await fromIndex.getByRole('link', { name: /src\/round\.ts:1/ }).click();
    await expect(page).toHaveURL(round);
    await expect(page.getByTestId('arrival')).toContainText('From the CI-built index');
  });
});

test.describe('journey 3: a lookup the index could not resolve', () => {
  test.beforeEach(({ page }) => {
    test.skip(narrow(page), 'a keyboard');
  });

  const lookUp = async (page: Page, editor: string, line: number, word: string) => {
    await pressWord(page, editor, line, word);
    await page.keyboard.press('g');
    await page.keyboard.press('d');
  };

  test('a symbol from a dependency is named as outside the repository, with nothing to jump to', async ({ page }) => {
    const world = await fakeApi(page);
    const address = `/example/alpha/blob/${pullView.headSha}/src/round.ts`;
    await page.goto(address);
    await lookUp(page, '.code-view', 2, 'Math');
    await expect(page.getByRole('region', { name: 'Definition' })).toBeVisible();
    expect(world.lookups).toEqual([expect.objectContaining({ path: 'src/round.ts', line: 1, word: 'Math' })]);

    const panel = page.getByRole('region', { name: 'Definition' });
    await expect(panel.getByRole('region', { name: 'Defined outside the repository' })).toContainText('typescript');
    await expect(panel).toContainText('there is nothing to open');
    await expect(panel.getByRole('link')).toHaveCount(0);
    await expect(page).toHaveURL(address);
  });

  test('text search results are labelled as such and never as a definition', async ({ page }) => {
    const world = await fakeApi(page);
    await page.goto(pull);
    await page.locator(taxEditor).scrollIntoViewIfNeeded();
    await lookUp(page, taxEditor, 3, 'rate');
    await expect(page.getByRole('region', { name: 'Definition' })).toBeVisible();
    expect(world.lookups).toEqual([expect.objectContaining({ kind: 'definition', path: 'src/tax.ts', line: 2, word: 'rate' })]);

    const panel = page.getByRole('region', { name: 'Definition' });
    const search = panel.getByRole('region', { name: 'From text search' });
    await expect(search).toContainText('they are not known to define it');
    await expect(search.getByRole('listitem')).not.toHaveCount(0);
    await expect(panel.getByText('From the CI-built index')).toHaveCount(0);
    // Nothing was followed.
    await expect(page).toHaveURL(new RegExp(`${pull}($|#)`));

    // Choosing a line goes there under the text-search label.
    await search.getByRole('link').first().click();
    await expect(page.getByTestId('arrival')).toContainText('From text search');
  });

  test('nothing found says so', async ({ page }) => {
    await fakeApi(page);
    await page.goto(`/example/alpha/blob/${pullView.headSha}/src/round.ts`);
    await lookUp(page, '.code-view', 1, 'number');
    const panel = page.getByRole('region', { name: 'Definition' });
    await expect(panel.getByRole('region', { name: 'Not found' })).toContainText('Nothing was found');
    await expect(panel.getByRole('link')).toHaveCount(0);
  });

  test('a lookup from a removed line is made at the merge base', async ({ page }) => {
    const world = await fakeApi(page);
    await page.goto(pull);
    const cart = '[data-path="src/cart.ts"]';
    await expect(page.locator(`${cart} .cm-mergeView .cm-editor`)).toHaveCount(2);
    // `total` on the old side, which the pull request removes.
    await lookUp(page, `${cart} .cm-mergeView .cm-editor:first-child`, 4, 'total(prices)');
    await expect(page.getByRole('region', { name: 'Definition' })).toBeVisible();
    // Asked at the merge base, at the old file's path, on the word that was removed.
    expect(world.lookups).toEqual([
      expect.objectContaining({ kind: 'definition', sha: pullView.mergeBaseSha, path: 'src/cart.ts', line: 3, word: 'total' }),
    ]);
  });
});

test.describe('journey 4: the diff and search need no index', () => {
  test('the page says CI is still building the index, and picks it up without a reload', async ({ page }) => {
    const running = { state: 'running', reason: '', runUrl: 'https://github.example/example/alpha/actions/runs/7001', toolVersion: '', documents: 0 } as const;
    const world = await fakeApi(page, { index: running });
    await page.clock.install();
    await page.goto(pull);

    const status = page.getByTestId('index-status');
    await expect(status).toContainText('CI is still building the index for this commit');
    await expect(status.getByRole('link', { name: 'the run on GitHub' })).toHaveAttribute('href', running.runUrl);
    // The diff is there all the same: it comes from the commit's source.
    await expect(page.locator('[data-path="src/cart.ts"]')).toContainText('return sum(prices);');

    // Nothing is answered from an index that is not there: a lookup is a text search.
    if (!narrow(page)) {
      await page.locator(taxEditor).scrollIntoViewIfNeeded();
      await pressWord(page, taxEditor, 4, 'round(cents');
      await page.keyboard.press('g');
      await page.keyboard.press('d');
      const panel = page.getByRole('region', { name: 'Definition' });
      await expect(panel.getByRole('region', { name: 'From text search' })).toBeVisible();
      await expect(panel.getByText('From the CI-built index')).toHaveCount(0);
    }

    // The job finishes. The page asks again within 15 seconds.
    world.index = indexReady;
    await page.clock.fastForward(16_000);
    await expect(status).toContainText('Navigation uses the index CI built for this commit');
    expect(world.requests.filter((r) => r.endsWith(`/commits/${pullView.headSha}/index`)).length).toBeGreaterThanOrEqual(2);
  });

  test('a failed index job and a missing index are each said plainly', async ({ page }) => {
    const world = await fakeApi(page, {
      index: { state: 'failed', reason: '', runUrl: 'https://github.example/example/alpha/actions/runs/7002', toolVersion: '', documents: 0 },
    });
    await page.goto(pull);
    const status = page.getByTestId('index-status');
    await expect(status).toContainText('The index job failed for this commit');
    await expect(status.getByRole('link')).toHaveAttribute('href', /actions\/runs\/7002$/);

    world.index = { state: 'none', reason: 'the index has expired on GitHub', runUrl: '', toolVersion: '', documents: 0 };
    await page.reload();
    await expect(status).toContainText('No index for this commit. Lookups use text search. The index has expired on GitHub.');
  });

  test('search finds a caller in a file the pull request does not change', async ({ page }) => {
    const world = await fakeApi(page);
    await page.goto(pull);
    await expect(page.locator('[data-path="src/cart.ts"]').getByTestId('merge-pane')).toBeVisible();

    await page.getByRole('button', { name: 'Search code' }).click();
    const search = page.getByRole('region', { name: 'Search code' });
    await search.getByRole('searchbox').fill('sum(');
    await search.getByRole('button', { name: 'Search', exact: true }).click();

    await expect(search).toContainText('3 matching lines, from text search.');
    const outside = search.getByRole('link', { name: /src\/receipt\.ts:3/ });
    await expect(outside).toBeVisible();

    // The query went in the request body, to the head commit, and not in an address.
    expect(world.searches).toEqual([{ query: 'sum(', pathPrefix: '', caseSensitive: false }]);
    expect(world.requests).toContain(`POST /api/v1/repos/example/alpha/commits/${pullView.headSha}/search`);
    expect(world.requests.some((r) => r.includes('sum'))).toBe(false);

    await outside.click();
    await expect(page).toHaveURL(new RegExp(`/example/alpha/blob/${pullView.headSha}/src/receipt\\.ts\\?pull=${pullView.number}#L3$`));
    await expect(page.getByText('Outside this pull request’s changes')).toBeVisible();
    await expect(page.locator('.cm-target-line')).toContainText('sum(prices)');
  });

  test('a search under two characters is not sent', async ({ page }) => {
    const world = await fakeApi(page);
    await page.goto(pull);
    await page.getByRole('button', { name: 'Search code' }).click();
    const search = page.getByRole('region', { name: 'Search code' });
    await search.getByRole('searchbox').fill('s');
    await expect(search.getByRole('button', { name: 'Search', exact: true })).toBeDisabled();
    expect(world.searches).toHaveLength(0);
  });
});

test.describe('journey 8: read on a phone', () => {
  test('the description and a single-column diff fit the screen', async ({ page }) => {
    test.skip(!narrow(page), 'the narrow layout');
    await fakeApi(page);
    await page.goto(pull);

    await expect(page.getByTestId('description')).toBeVisible();
    const cart = page.locator('[data-path="src/cart.ts"]');
    await expect(cart.locator('.merge-unified')).toBeVisible();
    await expect(cart.locator('.cm-mergeView')).toHaveCount(0);
    // Removed and added lines are both there, one above the other.
    await expect(cart).toContainText('return total(prices);');
    await expect(cart).toContainText('return sum(prices);');

    // The page itself does not scroll sideways.
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow).toBeLessThanOrEqual(0);

    // The touch route for moving through changes.
    await expect(page.getByRole('navigation', { name: 'Move through the changes' })).toBeVisible();
    await page.getByRole('button', { name: 'Next change' }).tap();
  });

  test('tapping a line number lists what can be done there', async ({ page }) => {
    test.skip(!narrow(page), 'the narrow layout');
    await fakeApi(page);
    await page.goto(pull);

    const tax = page.locator('[data-path="src/tax.ts"]');
    await tax.locator('.cm-lineNumbers .cm-gutterElement', { hasText: /^1$/ }).tap();
    const sheet = page.getByRole('dialog', { name: 'src/tax.ts, line 1' });
    await expect(sheet.getByRole('button', { name: /Comment on GitHub/ })).toBeVisible();
    await expect(sheet.getByRole('button', { name: 'Open this file' })).toBeVisible();
    await sheet.getByRole('button', { name: 'Cancel' }).tap();
    await expect(sheet).toHaveCount(0);
  });

  test('the wide layout has two columns', async ({ page }) => {
    test.skip(narrow(page), 'the wide layout');
    await fakeApi(page);
    await page.goto(pull);
    const cart = page.locator('[data-path="src/cart.ts"]');
    await expect(cart.locator('.cm-mergeView .cm-editor')).toHaveCount(2);
    await expect(page.getByRole('navigation', { name: 'Move through the changes' })).toBeHidden();
  });
});

test.describe('journey 9: a user without access', () => {
  test('sees the same page whether or not the pull request exists', async ({ page }) => {
    // This user can read example/beta and not example/alpha.
    await fakeApi(page, { readable: new Set(['example/beta']) });

    const seen: string[] = [];
    for (const address of [
      pull, // exists, in a repository this user cannot read
      '/example/alpha/pull/99999', // does not exist, in that repository
      '/example/nowhere/pull/1', // a repository that is not connected
      `/example/alpha/blob/${pullView.headSha}/src/sum.ts`, // a file in the one they cannot read
    ]) {
      await page.goto(address);
      await expect(page.getByRole('heading', { name: 'Not found' })).toBeVisible();
      seen.push(await page.locator('main').innerText());
    }
    expect(new Set(seen).size).toBe(1);

    // Nothing of the repository reached the page.
    await page.goto(pull);
    await expect(page.locator('body')).not.toContainText('Rename total to sum');
    await expect(page.locator('body')).not.toContainText('src/sum.ts');
  });

  test('with no session, sign-in is shown and then the link is honoured', async ({ page }) => {
    const world = await fakeApi(page, { signedIn: false });
    await page.goto(pull);
    await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
    await expect(page.locator('body')).not.toContainText('Rename total to sum');
    for (const permission of ['Contents', 'Pull requests', 'Metadata', 'Actions']) {
      await expect(page.getByRole('listitem').filter({ hasText: new RegExp(`^${permission},`) })).toBeVisible();
    }

    // A classic token is refused, with the reason and the fix.
    await page.getByLabel('Fine-grained personal access token').fill('ghp_classic');
    await page.getByRole('button', { name: 'Sign in' }).click();
    await expect(page.getByRole('alert')).toContainText('not a fine-grained token');

    await page.getByLabel('Fine-grained personal access token').fill('github_pat_example');
    await page.getByRole('button', { name: 'Sign in' }).click();
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Rename total to sum');
    await expect(page).toHaveURL(pull);
    expect(world.requests).toContain('POST /auth/token');
  });

  test('a session that ends mid-visit returns to sign-in', async ({ page }) => {
    const world = await fakeApi(page);
    await page.goto('/');
    await expect(page.getByRole('link', { name: /Rename total to sum/ })).toBeVisible();
    world.signedIn = false;
    await page.getByRole('link', { name: /Rename total to sum/ }).click();
    await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
  });
});

test.describe('journey 10: leave a comment', () => {
  test("GitHub's page opens at that file and line", async ({ page, context }) => {
    await fakeApi(page);
    await context.route('https://github.example/**', (route) => route.fulfill({ status: 200, contentType: 'text/html', body: 'GitHub' }));
    await page.goto(pull);

    const tax = page.locator('[data-path="src/tax.ts"]');
    await tax.locator('.cm-lineNumbers .cm-gutterElement', { hasText: /^1$/ }).click();
    const [popup] = await Promise.all([
      page.waitForEvent('popup'),
      page.getByRole('dialog').getByRole('button', { name: /Comment on GitHub/ }).click(),
    ]);
    // The anchor is the SHA-256 of the path, then R and the line, counted from 1.
    const anchor = await page.evaluate(async (path) => {
      const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(path));
      return Array.from(new Uint8Array(digest), (b) => b.toString(16).padStart(2, '0')).join('');
    }, 'src/tax.ts');
    expect(popup.url()).toBe(`${pullView.webUrl}/files#diff-${anchor}R1`);
    // The page that opened it cannot be reached from the one opened.
    expect(await popup.evaluate(() => window.opener === null)).toBe(true);
  });

  test('a removed line is addressed on the old side, at its old path', async ({ page, context }) => {
    test.skip(narrow(page), 'removed lines have their own column in the wide layout');
    await fakeApi(page);
    await context.route('https://github.example/**', (route) => route.fulfill({ status: 200, contentType: 'text/html', body: 'GitHub' }));
    await page.goto(pull);

    const renamed = page.locator('[data-path="src/sum.ts"]');
    await renamed.locator('.cm-mergeView .cm-editor').first().locator('.cm-lineNumbers .cm-gutterElement', { hasText: /^1$/ }).click();
    const sheet = page.getByRole('dialog', { name: 'src/total.ts, line 1 (before)' });
    const [popup] = await Promise.all([page.waitForEvent('popup'), sheet.getByRole('button', { name: /Comment on GitHub/ }).click()]);
    expect(popup.url()).toMatch(/\/files#diff-[0-9a-f]{64}L1$/);
  });
});

test.describe('shortcuts', () => {
  test('the keyboard moves through the page', async ({ page }) => {
    test.skip(narrow(page), 'a keyboard');
    await fakeApi(page);
    await page.goto(pull);
    await expect(page.locator('[data-path="src/cart.ts"]').getByTestId('merge-pane')).toBeVisible();

    await page.keyboard.press('?');
    const help = page.getByRole('dialog', { name: 'Shortcuts' });
    await expect(help).toContainText('Next change');
    await page.keyboard.press('Escape');
    await expect(help).toHaveCount(0);

    // ] and [ move between files, and the address follows.
    await page.keyboard.press(']');
    await expect(page).toHaveURL(new RegExp('#file=src%2Fcart\\.ts$'));
    await page.keyboard.press('[');
    await expect(page).toHaveURL(new RegExp('#file=docs%2Fold\\.md$'));

    // d shows only the new code, and d again brings the diff back.
    await page.keyboard.press('d');
    await expect(page.locator('[data-path="src/cart.ts"]')).not.toContainText('return total(prices);');
    await page.keyboard.press('d');
    await expect(page.locator('[data-path="src/cart.ts"]')).toContainText('return total(prices);');

    // / opens search with the field ready, and typing there is text, not shortcuts.
    await page.keyboard.press('/');
    const box = page.getByRole('searchbox');
    await expect(box).toBeFocused();
    await page.keyboard.type('jd');
    await expect(box).toHaveValue('jd');
    await page.keyboard.press('Escape');
    await expect(page.getByRole('region', { name: 'Search code' })).toHaveCount(0);

    // g then p goes to the list of pull requests.
    await page.locator('body').click({ position: { x: 5, y: 5 } });
    await page.keyboard.press('g');
    await page.keyboard.press('p');
    await expect(page).toHaveURL('/');
  });

  test('j moves to the next change', async ({ page }) => {
    test.skip(narrow(page), 'a keyboard');
    await fakeApi(page);
    await page.goto(pull);
    await expect(page.locator('[data-path="src/cart.ts"]').getByTestId('merge-pane')).toBeVisible();

    await page.keyboard.press('j');
    // The first file with a change to step through is src/cart.ts.
    await expect(page.getByRole('navigation', { name: 'Changed files' }).locator('li.active')).toContainText('src/cart.ts');
    await expect(page.locator('[data-path="src/cart.ts"] .cm-editor.cm-focused')).toHaveCount(1);
  });
});
