// SPDX-License-Identifier: MIT

import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { type Handlers, useHotkeys } from './useHotkeys';

function Page({ handlers }: { handlers: Handlers }) {
  useHotkeys(handlers);
  return (
    <div>
      <input aria-label="field" />
      <button type="button">a button</button>
    </div>
  );
}

const key = (target: Element | Window, k: string, init: KeyboardEventInit = {}) => {
  const event = new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true, ...init });
  target.dispatchEvent(event);
  return event;
};

afterEach(cleanup);

describe('useHotkeys', () => {
  it('runs the handler for a key, and for a prefix then a key', () => {
    const nextChange = vi.fn();
    const switchPull = vi.fn();
    render(<Page handlers={{ nextChange, switchPull }} />);

    expect(key(window, 'j').defaultPrevented).toBe(true);
    expect(nextChange).toHaveBeenCalledTimes(1);

    key(window, 'g');
    key(window, 'p');
    expect(switchPull).toHaveBeenCalledTimes(1);
  });

  it('does not fire single keys while a text field has focus', () => {
    const nextChange = vi.fn();
    const search = vi.fn();
    const { getByLabelText } = render(<Page handlers={{ nextChange, search }} />);
    const field = getByLabelText('field');

    expect(key(field, 'j').defaultPrevented).toBe(false);
    key(field, '/');
    expect(nextChange).not.toHaveBeenCalled();
    expect(search).not.toHaveBeenCalled();
  });

  it('lets Esc through from a text field', () => {
    const back = vi.fn();
    const { getByLabelText } = render(<Page handlers={{ back }} />);
    key(getByLabelText('field'), 'Escape');
    expect(back).toHaveBeenCalledTimes(1);
  });

  it('leaves a key alone when the page has no handler for it', () => {
    render(<Page handlers={{}} />);
    expect(key(window, 'j').defaultPrevented).toBe(false);
  });

  it('never takes a browser shortcut', () => {
    const search = vi.fn();
    const toggleDiff = vi.fn();
    render(<Page handlers={{ search, toggleDiff }} />);
    expect(key(window, 'f', { ctrlKey: true }).defaultPrevented).toBe(false);
    expect(key(window, 'd', { metaKey: true }).defaultPrevented).toBe(false);
    expect(key(window, 'F5').defaultPrevented).toBe(false);
    expect(search).not.toHaveBeenCalled();
    expect(toggleDiff).not.toHaveBeenCalled();
  });

  it('stops listening when the page goes away', () => {
    const nextChange = vi.fn();
    const { unmount } = render(<Page handlers={{ nextChange }} />);
    unmount();
    key(window, 'j');
    expect(nextChange).not.toHaveBeenCalled();
  });
});
