// SPDX-License-Identifier: MIT

import { useEffect, useRef } from 'react';

export interface SheetAction {
  label: string;
  /** Shown beside the label: the keys that do the same. */
  hint?: string;
  run: () => void;
}

/**
 * Every action available at one place, in one list. On a phone this is how a
 * line's actions are reached; on a desktop it is the same list, by clicking a
 * line number.
 */
export function ActionSheet({ title, actions, onClose }: { title: string; actions: SheetAction[]; onClose: () => void }) {
  const first = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    first.current?.focus();
  }, []);

  return (
    <div
      className="sheet-backdrop"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div className="sheet" role="dialog" aria-modal="true" aria-label={title}>
        <h2 className="sheet-title">{title}</h2>
        <ul className="sheet-actions">
          {actions.map((action, i) => (
            <li key={action.label}>
              <button
                type="button"
                ref={i === 0 ? first : undefined}
                onClick={() => {
                  onClose();
                  action.run();
                }}
              >
                <span>{action.label}</span>
                {action.hint ? <kbd>{action.hint}</kbd> : null}
              </button>
            </li>
          ))}
          <li>
            <button type="button" className="sheet-cancel" onClick={onClose}>
              Cancel
            </button>
          </li>
        </ul>
      </div>
    </div>
  );
}
