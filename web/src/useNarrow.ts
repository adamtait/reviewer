// SPDX-License-Identifier: MIT

import { useSyncExternalStore } from 'react';

// Below this width the diff is one column and line actions open as a sheet.
const query = '(max-width: 899px)';

function subscribe(listener: () => void): () => void {
  const media = window.matchMedia(query);
  media.addEventListener('change', listener);
  return () => {
    media.removeEventListener('change', listener);
  };
}

/** Whether the screen is narrow: a phone, or a small window. */
export function useNarrow(): boolean {
  return useSyncExternalStore(subscribe, () => window.matchMedia(query).matches);
}
