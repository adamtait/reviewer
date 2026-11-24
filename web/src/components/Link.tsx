// SPDX-License-Identifier: MIT

import type { ReactNode } from 'react';

import { isPlainClick, navigate } from '../router';

/**
 * A link to another place in the application. It is a real link: it can be
 * opened in a new tab or copied. `label` names it for a screen reader when its
 * text alone, such as a line number, does not say where it leads.
 */
export function Link({ to, children, className, label }: { to: string; children: ReactNode; className?: string; label?: string }) {
  return (
    <a
      href={to}
      className={className}
      aria-label={label}
      onClick={(event) => {
        if (!isPlainClick(event)) return;
        event.preventDefault();
        navigate(to);
      }}
    >
      {children}
    </a>
  );
}
