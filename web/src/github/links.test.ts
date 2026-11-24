// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';

import { blobUrl, commentUrl, repositoryUrl } from './links';

const webUrl = 'https://github.example/example/alpha/pull/12';

describe('links to GitHub', () => {
  it('point a comment at a line of the new file with R and at a removed line with L', async () => {
    const added = await commentUrl(webUrl, 'src/sum.ts', 0, 'new');
    const removed = await commentUrl(webUrl, 'src/sum.ts', 41, 'old');

    expect(added).toMatch(/^https:\/\/github\.example\/example\/alpha\/pull\/12\/files#diff-[0-9a-f]{64}R1$/);
    expect(removed).toMatch(/^https:\/\/github\.example\/example\/alpha\/pull\/12\/files#diff-[0-9a-f]{64}L42$/);
    // Both name the same file.
    expect(added.slice(0, -2)).toBe(removed.slice(0, -3));
  });

  it("use the SHA-256 of the file's path as the anchor", async () => {
    // "abc" has a digest everyone can check against a published test vector.
    const url = await commentUrl(webUrl, 'abc', 9, 'new');
    expect(url).toBe(`${webUrl}/files#diff-ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015adR10`);
  });

  it('count lines from 1, where the application counts from 0', async () => {
    expect(await commentUrl(webUrl, 'a', 0, 'new')).toMatch(/R1$/);
    expect(await commentUrl(webUrl, 'a', 99, 'old')).toMatch(/L100$/);
  });

  it('find the repository from a pull request address', () => {
    expect(repositoryUrl(webUrl)).toBe('https://github.example/example/alpha');
  });

  it('address a file at a commit, with each part of its path escaped', () => {
    const sha = '1'.repeat(40);
    expect(blobUrl(webUrl, sha, 'src/a file#1.ts')).toBe(`https://github.example/example/alpha/blob/${sha}/src/a%20file%231.ts`);
    expect(blobUrl(webUrl, sha, 'src/sum.ts', 2)).toBe(`https://github.example/example/alpha/blob/${sha}/src/sum.ts#L3`);
  });
});
