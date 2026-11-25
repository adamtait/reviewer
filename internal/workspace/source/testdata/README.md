# Test data

`archive.tar.gz` is a real response from GitHub's archive call:
`GET /repos/adamtait/reviewer/tarball/a255a80d7b5e7382ed052a5242667e2a420a83ca`, this repository's
first commit, fetched on 2026-11-24. It holds one file, `README.md`.

It is here so the tests read GitHub's own layout and not only archives the tests built themselves:
a gzip tar, a global header entry, and every path under one top-level folder named after the owner,
the repository and a short hash.
