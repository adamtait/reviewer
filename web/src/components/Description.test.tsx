// SPDX-License-Identifier: MIT

import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { Description } from './Description';

afterEach(cleanup);

describe('the description', () => {
  it('is shown as text, whatever it contains', () => {
    const hostile = [
      '<script>window.pwned = true</script>',
      '<img src=x onerror="window.pwned = true">',
      '[click me](javascript:alert(1))',
      '<a href="https://elsewhere.example">a link</a>',
      '![tracking](https://elsewhere.example/pixel.png)',
    ].join('\n');
    const { getByTestId, container } = render(<Description body={hostile} />);

    // Every character arrives, as characters.
    expect(getByTestId('description').textContent).toBe(hostile);
    // And none of it became an element.
    expect(container.querySelector('script, img, a, iframe')).toBeNull();
    expect((window as unknown as { pwned?: boolean }).pwned).toBeUndefined();
  });

  it('keeps line breaks, which the stylesheet shows', () => {
    const { getByTestId } = render(<Description body={'one\n\ntwo'} />);
    expect(getByTestId('description').textContent).toBe('one\n\ntwo');
  });

  it('says so when there is none', () => {
    const { getByText } = render(<Description body={'  \n'} />);
    expect(getByText('No description.')).toBeTruthy();
  });
});
