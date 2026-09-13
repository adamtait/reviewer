# tiny-monorepo

A destination repository for the installer's tests. Two npm workspaces, TypeScript,
vitest, an ESLint flat config and no dependency-cruiser config — the last absence is
deliberate, so a test can assert the plan reports what it did not find.

Every file here was written for this fixture. No third-party code, no generated
lockfile content beyond the shape the detector reads (ADR-0010).
