// SPDX-License-Identifier: MIT

import { useEffect, useRef } from 'react';

import { type Action, KeyMatcher, isTextField } from './keymap';

export type Handlers = Partial<Record<Action, () => void>>;

/**
 * Runs a handler when its action's keys are pressed anywhere on the page.
 *
 * Single keys are off while a text field has focus, with one exception: Esc,
 * which leaves the field. A key with no handler is left alone, so the browser
 * and the code editor still see it.
 */
export function useHotkeys(handlers: Handlers): void {
  const latest = useRef(handlers);
  useEffect(() => {
    latest.current = handlers;
  });

  useEffect(() => {
    const matcher = new KeyMatcher();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented) return;
      const typing = isTextField(event.target);
      if (typing && event.key !== 'Escape') return;

      const result = matcher.feed(event);
      if (result === null) return;
      if (result === 'pending') return;
      const handler = latest.current[result];
      if (!handler) return;
      event.preventDefault();
      handler();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => {
      window.removeEventListener('keydown', onKeyDown);
    };
  }, []);
}
