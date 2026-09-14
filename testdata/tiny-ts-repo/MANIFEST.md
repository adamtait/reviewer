# tiny-ts-repo

Built into a temporary git repository by `internal/testfixture`. `base/` is
committed first, then `head/` is copied over it and committed, so the diff
between the two commits is the change under review.

`head/` deliberately introduces, in one commit:

| File | Problem | Analyzer that should find it |
|---|---|---|
| `src/domain/order.ts` | imports `../infra/http.js`, crossing a layer boundary | dependency-cruiser |
| `src/infra/http.ts` | calls `send()` without awaiting the promise | typescript-eslint |
| `src/config.ts` | a credential-shaped string literal | gitleaks |

The key in `src/config.ts` is fabricated, but in a format gitleaks genuinely
matches — verified, because a fixture secret that no scanner detects gives the
secrets analyzer nothing to find and makes its tests vacuous. Do not replace it
with an obviously-fake placeholder.

`testdata/` is allowlisted in this repository's `.gitleaks.toml`, so scanning
this repository's own history stays clean while scanning the fixture as a
destination repository still fires.
