// SPDX-License-Identifier: MIT

import { type Page, expect, test } from '@playwright/test';

import { fakeApi, pullView } from './fake-api';

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

test.describe('journey 4: the diff and search need no index', () => {
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
