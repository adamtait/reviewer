// SPDX-License-Identifier: MIT

import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { RenderedDiff } from './RenderedDiff';

afterEach(cleanup);

describe('a rendered diff', () => {
  it('draws both versions, marks each changed block, and says what changed to a screen reader', () => {
    const { getByTestId } = render(<RenderedDiff oldText={'# Title\n\nOld **text**.\n'} newText={'# Title\n\nNew **text**.\n\n- added\n'} />);
    const diff = getByTestId('rendered-diff');
    const marked = [...diff.querySelectorAll<HTMLElement>('[data-change]')].map((el) => `${el.dataset.change ?? ''}: ${el.textContent}`);
    expect(marked).toEqual(['same: Title', 'removed: Removed: Old text.', 'added: Added: New text.', 'added: Added: added']);
    expect(diff.querySelector('[data-change="added"] strong')?.textContent).toBe('text');
    expect(diff.textContent).toContain('1 block removed, 2 blocks added.');
  });

  it('says so when the change is not in what is drawn', () => {
    const { getByTestId } = render(<RenderedDiff oldText={'Text.\n'} newText={'Text.   \n'} />);
    expect(getByTestId('rendered-diff').textContent).toContain('the change is in its text');
  });

  it('never turns what it contains into something that runs, loads or leads away', () => {
    const { container } = render(
      <RenderedDiff oldText="" newText={'<script>window.pwned = true</script>\n\n![x](https://elsewhere.example/p.png)\n\n[phish](https://elsewhere.example/)\n'} />,
    );
    expect(container.querySelector('script, img, a, iframe')).toBeNull();
    expect((window as unknown as { pwned?: boolean }).pwned).toBeUndefined();
  });
});
