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

The key in `src/config.ts` is a fabricated value in a real-looking format. It is
allowlisted in `.gitleaks.toml` so the scan of this repository's own history
stays clean while the scan of the fixture still fires.
