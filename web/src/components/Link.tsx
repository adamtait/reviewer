// SPDX-License-Identifier: MIT

import type { ReactNode } from 'react';

import { isPlainClick, navigate } from '../router';

/** A link to another place in the application. It is a real link: it can be opened in a new tab or copied. */
export function Link({ to, children, className }: { to: string; children: ReactNode; className?: string }) {
  return (
    <a
      href={to}
      className={className}
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
