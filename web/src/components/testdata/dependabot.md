Bumps minimatch (https://github.com/isaacs/minimatch) to 11.0.9 and updates ancestor dependency typescript-eslint (https://github.com/typescript-eslint/typescript-eslint/tree/HEAD/packages/typescript-eslint). These dependencies need to be updated together.

Updates `minimatch` from 10.2.6 to 11.0.9
<details>
<summary>Release notes</summary>
<p><em>Sourced from <a href="https://github.com/isaacs/minimatch/releases">minimatch's releases</a>.</em></p>
<blockquote>
<h2>v11.0.7</h2>
<ul>
<li>Quadratic compile time for long wildcard patterns (regression in 11.0.0 <code>mergeRows</code>)</li>
</ul>
<p>Fixes GHSA-cf2h-pxjq-68xw: <a href="https://github.com/isaacs/minimatch/security/advisories/GHSA-cf2h-pxjq-68xw">https://github.com/isaacs/minimatch/security/advisories/GHSA-cf2h-pxjq-68xw</a></p>
</blockquote>
</details>
<details>
<summary>Changelog</summary>
<p><em>Sourced from <a href="https://github.com/isaacs/minimatch/blob/main/changelog.md">minimatch's changelog</a>.</em></p>
<blockquote>
<h1>change log</h1>
<h2>11.0</h2>
<ul>
<li>Remove <code>makeRe()</code>, <code>Minimatch#regexp</code>, and the <code>MMRegExp</code> type.</li>
<li>Remove the <code>AST</code> class.</li>
</ul>
<!-- raw HTML omitted -->
</blockquote>
<p>... (truncated)</p>
</details>
<details>
<summary>Commits</summary>
<ul>
<li><a href="https://github.com/isaacs/minimatch/commit/e89926755f3f79cf79f1087bd5730ed4d7eba60e"><code>e899267</code></a> 11.0.9</li>
<li><a href="https://github.com/isaacs/minimatch/commit/4a0e24d3920ee03c67b1127e579069db0f48c0b0"><code>4a0e24d</code></a> expand numeric ranges inside !(), *() and +() extglobs</li>
<li><a href="https://github.com/isaacs/minimatch/commit/8a26917fa43e2b865dd6017f7acf6ac2fd5190cd"><code>8a26917</code></a> Quadratic compile time for long wildcard patterns (regression in 11.0.0 `merg...</li>
<li>Additional commits viewable in <a href="https://github.com/isaacs/minimatch/compare/v10.2.6...v11.0.9">compare view</a></li>
</ul>
</details>
<details>
<summary>Maintainer changes</summary>
<p>This version was pushed to npm by <a href="https://www.npmjs.com/~GitHub%20Actions">GitHub Actions</a>, a new releaser for minimatch since your current version.</p>
</details>
<br />
