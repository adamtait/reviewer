// SPDX-License-Identifier: MIT

import { type Action, keyLabel, keymap } from '../keys/keymap';

/** The shortcut table, with the touch route for each action. */
export function ShortcutHelp({ available, onClose }: { available: ReadonlySet<Action>; onClose: () => void }) {
  return (
    <div
      className="sheet-backdrop"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div className="sheet help" role="dialog" aria-modal="true" aria-label="Shortcuts">
        <h2 className="sheet-title">Shortcuts</h2>
        <table>
          <thead>
            <tr>
              <th scope="col">Action</th>
              <th scope="col">Keyboard</th>
              <th scope="col">Touch</th>
            </tr>
          </thead>
          <tbody>
            {keymap.map((binding) => (
              <tr key={binding.action} className={available.has(binding.action) ? undefined : 'unavailable'}>
                <td>
                  {binding.label}
                  {available.has(binding.action) ? null : <span className="note"> (not on this page)</span>}
                </td>
                <td>
                  {binding.keys.map((key, i) => (
                    <span key={key}>
                      {i > 0 ? ' then ' : null}
                      <kbd>{keyLabel(key)}</kbd>
                    </span>
                  ))}
                </td>
                <td>{binding.touch}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <button type="button" className="sheet-cancel" onClick={onClose}>
          Close
        </button>
      </div>
    </div>
  );
}
