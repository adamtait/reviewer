// SPDX-License-Identifier: MIT

import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { Description } from './Description';

afterEach(cleanup);

describe('the description', () => {
  it('never turns what it contains into something that runs, loads or leads away', () => {
    const hostile = [
      '<script>window.pwned = true</script>',
      '<img src=x onerror="window.pwned = true">',
      '[click me](javascript:alert(1))',
      '<a href="https://elsewhere.example">a link</a>',
      '![tracking](https://elsewhere.example/pixel.png)',
      '[a real-looking link](https://elsewhere.example/phish)',
    ].join('\n\n');
    const { getByTestId, container } = render(<Description body={hostile} github="https://github.example/example/alpha/pull/12" />);

    // Nothing in it became an element that can run, load or be followed.
    expect(container.querySelector('script, img, a, iframe')).toBeNull();
    expect((window as unknown as { pwned?: boolean }).pwned).toBeUndefined();
    // And the reader can still see what the author wrote.
    const shown = getByTestId('description').textContent;
    expect(shown).toContain('<script>window.pwned = true</script>');
    expect(shown).toContain('https://elsewhere.example/pixel.png');
    expect(shown).toContain('(https://elsewhere.example/phish)');
  });

  it('draws ordinary Markdown', () => {
    const { container } = render(<Description body={'Why: the old name **lied**.\n\n- one\n- two'} />);
    expect(container.querySelector('strong')?.textContent).toBe('lied');
    expect(container.querySelectorAll('li')).toHaveLength(2);
  });

  it('lets a link to GitHub be followed, with its address shown', () => {
    const { container } = render(
      <Description body="Closes [the issue](https://github.example/example/alpha/issues/3)." github="https://github.example/example/alpha/pull/12" />,
    );
    const link = container.querySelector('a');
    expect(link?.getAttribute('href')).toBe('https://github.example/example/alpha/issues/3');
    expect(link?.textContent).toBe('the issue (https://github.example/example/alpha/issues/3)');
  });

  it('says so when there is none', () => {
    const { getByText } = render(<Description body={'  \n'} />);
    expect(getByText('No description.')).toBeTruthy();
  });
});
