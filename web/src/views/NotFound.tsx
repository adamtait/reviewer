// SPDX-License-Identifier: MIT

import { Link } from '../components/Link';

// One message for an address that does not exist and for a repository this
// user cannot read. The service does not say which, and neither does this.
export function NotFound() {
  return (
    <main className="notfound">
      <h1>Not found</h1>
      <p>There is nothing at this address, or you do not have access to it on GitHub.</p>
      <p>
        <Link to="/">Open pull requests</Link>
      </p>
    </main>
  );
}
