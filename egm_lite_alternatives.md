# Rendering GitHub markup in the workspace

> **BLUF:** Recommend Option A: keep rendering in the browser with
> `markdown-it`, turn raw HTML on, parse the output into a detached
> document, and copy it into React elements through a GitHub-derived
> allowlist. It gets the Dependabot-style `<details>` bodies right, keeps
> the "nothing is ever injected as HTML" guarantee, and needs no service or
> API change. Option B (GitHub's own `body_html`) is more faithful for
> GitHub-only features, but it splits rendering into two paths and changes
> the Go API and its recorded examples.

## Alternatives Considered

### A. Client-side GFM with an allowlist copy to React (recommended)

- `markdown-it` with `html: true`, `linkify: true`, and `breaks: true` for
  GitHub text. A small core rule escapes GFM's nine "tagfilter" tags
  (`script`, `style`, `iframe`, `title`, `textarea`, `xmp`, `noembed`,
  `noframes`, `plaintext`), so they show as text, as they do on GitHub.
- Parse the HTML string with `DOMParser`, which makes an inert document: no
  scripts run and nothing loads. Walk that document and create React
  elements only for allowlisted tags and attributes. Unknown tags are
  unwrapped, so their text stays. Comments are dropped. Links go through
  the existing `followable()` check, and images become a placeholder.
- **Pros:** one renderer for descriptions, titles, and answers; no Go
  change; the existing link and image policy carries over unchanged.
- **Cons:** GitHub-only features such as `#123`, `@user`, alerts, and task
  lists each need a small local rule. Emoji shortcodes, footnotes, and math
  stay unrendered unless we add plugins.

### B. Ask GitHub for `body_html` and copy it through the same allowlist

- Request the pull request with `Accept: application/vnd.github.full+json`
  and send `bodyHtml` to the page beside `body`.
- **Pros:** exact GitHub output: references, mentions, emoji, alerts,
  footnotes, and task lists, all already sanitized by GitHub.
- **Cons:** assistant answers still need Option A, so there are two
  renderers. It changes `ghclient`, `catalog`, `apitypes`, `types.ts`, and
  the recorded examples. The output depends on GitHub's CSS class names
  (`markdown-alert`, `task-list-item`). Its images point at
  `camo.githubusercontent.com`, which the CSP blocks anyway.

### C. DOMPurify plus `dangerouslySetInnerHTML` (rejected)

- Adds a dependency and breaks the design rule in
  [Markdown.tsx](web/src/components/Markdown.tsx#L10-L11) that no HTML
  string is ever put into the page. Option A gives the same result without
  doing that.

## Sources checked (2026-11-25)

- GitHub's allowlist: `html-pipeline` `SanitizationFilter`
  (`gjtorikian/html-pipeline`, `lib/html_pipeline/sanitization_filter.rb`).
- GFM spec section 6.11, "Disallowed Raw HTML (extension)":
  <https://github.github.com/gfm/>.
- GitHub Docs, "Basic writing and formatting syntax" > "Line breaks": in
  issues, pull requests, and discussions, a newline renders as a line break.
