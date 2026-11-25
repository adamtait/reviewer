# Checklist: draw GitHub markup in the review workspace

Task: draw pull request descriptions, titles, and assistant answers the way
GitHub does. That includes the raw HTML Dependabot writes (`<details>`,
`<summary>`, `<blockquote>`, `<code>`, `<a>`). The current guarantees stay:
nothing runs, nothing loads, and only workspace or GitHub links can be
followed. Approach: Option A in
[egm_lite_alternatives.md](egm_lite_alternatives.md).

Agreed decisions:

- A link that can be followed shows its address on hover (`title`), not
  inline. A link that cannot be followed keeps its address inline.
- Images are not loaded. They are drawn as a placeholder showing the alt
  text and the address. The CSP does not change.
- Scope is core plus extras. Extras: task lists, the five alerts, `#123`,
  `owner/repo#123`, and `@user` references, and inline code in titles.
- Assistant answers get the same HTML allowlist. A single newline becomes a
  line break only in GitHub text (descriptions).

**Review baseline:** starting commit `f70c7708d467a21f49f7446a1d7d9e2b74809f0f`
(includes the alternatives and checklist commits); working tree clean.

## Change targets

### Renderer

- [x] [web/src/components/Markdown.tsx](web/src/components/Markdown.tsx):
  replace the token walker with: render, parse inertly, copy through the
  allowlist.
  - Parser: `markdown-it` with `html: true`, `linkify: true`, and `image`
    still disabled. New prop `breaks?: boolean`, which sets `breaks` on a
    second parser instance so the two flavours never share mutable options.
  - **Tagfilter** (GFM spec 6.11): a core rule escapes the leading `<` of
    `title`, `textarea`, `style`, `xmp`, `iframe`, `noembed`, `noframes`,
    `script`, and `plaintext` in `html_block` and `html_inline` tokens. They
    show as text, as on GitHub.
  - **Inert parse:** `new DOMParser().parseFromString(html, 'text/html')`.
    The parsed document is never attached to the page.
  - **Copy to React from an allowlist.** Tags: GitHub's
    `SanitizationFilter` list, without `img`, `picture`, and `source`.
    Headings stay demoted (`h1` becomes `h3`, and so on, as now).
  - Attributes are limited to:
    - `href` on `a`, through `followable()`
    - `align` on `p`, `div`, headings, `td`, and `th`
    - `open` on `details`
    - `colspan` and `rowspan` on `td` and `th`
    - `start` on `ol`
    - `datetime` on `time`
    
    Everything else is dropped: `style`, `class`, `id`, `name`, and `on*`.
    One exception: `style="text-align:X"` on `th` and `td`, which
    `markdown-it` writes for table alignment, becomes `align="X"`.
  - **Unknown elements** are unwrapped, so their text stays. Comments are
    dropped. These are dropped together with their contents: `script`,
    `style`, `template`, `svg`, `math`, `object`, `embed`, `iframe`, and
    `noscript`.
  - **`input`** is allowed only as `type="checkbox"`, always `disabled`.
    The only attribute it may carry is `checked`.
  - **Links:** a link that can be followed becomes
    `<a href title=address target=_blank rel="noopener noreferrer">`, with
    no inline address. A link that cannot be followed becomes
    `<span class="link-text">` with ` (address)` inline. If the link text
    is the same as the address (a linkified bare URL), the address is not
    repeated.
  - **Images:** `<img>` in raw HTML becomes
    `<span class="image-placeholder">` with the text
    `[image: alt] (address)`. `![alt](address)` stays as the text the
    author typed (`image` rule off), as now.
- [x] Same file, **task lists**: a `- [ ]` or `- [x]` list item gets a
  disabled checkbox in place of its leading marker, as GitHub does. Do this
  in the DOM walk: the `li` whose first text starts with `[ ] `, `[x] `, or
  `[X] `.
- [x] Same file, **alerts**: when a `blockquote`'s first paragraph starts
  with `[!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]`, or `[!CAUTION]`,
  followed by a line end, `<br>`, or nothing, draw it as
  `<div class="markdown-alert markdown-alert-{kind}">`. It gets a title
  paragraph ("Note", "Tip", and so on) and loses the marker. Any other
  `[!X]` stays a plain blockquote.
- [x] Same file, **references**: new prop `repository?: string`, the
  repository's GitHub address from
  [`repositoryUrl()`](web/src/github/links.ts#L33). In text nodes that are
  not inside `a`, `code`, or `pre`:
  - `#123` links to `{repository}/issues/123`. GitHub redirects that to the
    pull request when it is one.
  - `owner/repo#123` links to `{origin}/owner/repo/issues/123`.
  - `@login` links to `{origin}/login`.
  
  Boundaries: no word character, `/`, or `@` before the reference. This
  keeps `a@b.com`, `foo#1`, and URL fragments unlinked. A login is
  `[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})`. These links go through
  `followable()` like any other link.
- [x] Same file, **inline title**: export `InlineMarkdown({ text })`. It
  uses `parseInline` and draws only `code_inline` as `<code>`. Everything
  else is plain text, with no links, emphasis, or HTML, matching GitHub's
  titles.

### Callers

- [x] [web/src/components/Description.tsx](web/src/components/Description.tsx):
  pass `breaks` and `repository={repositoryUrl(github)}` when `github` is
  set. Update the comment to the new rules.
- [x] [web/src/components/AssistantPanel.tsx:124](web/src/components/AssistantPanel.tsx#L124):
  pass `repository` (no `breaks`).
- [x] [web/src/views/PullView.tsx:341](web/src/views/PullView.tsx#L341)
  and [web/src/views/PullList.tsx:48](web/src/views/PullList.tsx#L48):
  draw `title` with `InlineMarkdown`.
- [x] [web/src/styles/app.css](web/src/styles/app.css#L501-L540): styles
  for `details`/`summary`, `.markdown-alert-*` (border colour per kind),
  task-list items (no bullet), `kbd`, `.image-placeholder`, and `dl`.

### Tests

- [x] [web/src/components/testdata/dependabot.md](web/src/components/testdata/dependabot.md)
  (new): one real-shaped Dependabot body from the user's example, with
  Dependabot's real line breaks, cut to one section of each kind (release
  notes, changelog, commits, maintainer changes). It keeps the
  `<!-- raw HTML omitted -->` comment and `<br />`.
- [x] [web/src/components/Markdown.test.tsx](web/src/components/Markdown.test.tsx):
  - **Keep** every current security assertion.
  - **Adjust two:**
    - The `<img onerror>` case still has no `img` element, and now shows a
      placeholder in place of the raw `onerror=` text.
    - A link that can be followed now carries its address in `title`, not
      `textContent`.
  - **Add:**
    - **Dependabot fixture:** four `details`, each with a `summary`; the
      `h2` drawn as `h4`; `blockquote` and `code` present; commit links to
      the GitHub host clickable; the npmjs link not clickable, with its
      address shown; the comment not shown; no `img`.
    - **Tagfilter:** `<style>` and `<iframe>` show as text.
    - **Attributes:** a raw `<a href="javascript:…">` is not clickable;
      `onclick`, `style`, and `id` are removed.
    - **Elements:** an unknown tag is unwrapped and its text kept;
      `<svg>` and `<template>` are dropped with their content.
    - **Tables and URLs:** table alignment survives as `align`; a bare URL
      is linked and its address shown once.
    - **Line breaks:** `breaks` on gives `<br>`; off gives none.
    - **Task lists:** `- [ ]` and `- [x]` give disabled checkboxes with the
      right `checked` state.
    - **Alerts:** each of the five kinds is drawn as an alert, and `[!FOO]`
      stays a blockquote.
    - **References:** `#12`, `other/repo#3`, and `@octo-cat` are linked;
      none are linked inside code or inside an existing link; `a@b.com`
      and `x#1` are not linked.
    - **Titles:** `InlineMarkdown` draws backticks as `code` and leaves
      `**x**` and `<b>` as text.
- [x] [web/src/components/Description.test.tsx](web/src/components/Description.test.tsx):
  - Keep the hostile-content test.
  - Move the followable link's address assertion from `textContent` to
    `title`.
  - Add: a single newline gives `<br>`.
- [x] [web/e2e/journeys.spec.ts:79-92](web/e2e/journeys.spec.ts#L79-L92):
  rename "the description is shown as text" to "the description is drawn
  under the allowlist". With the same hostile body:
  - `<b>` is drawn bold;
  - no `img` element exists;
  - the title stays `Review workspace`;
  - **[⏳ ACK] test:** under the real CSP, no request leaves for a host
    other than the page's own while the description renders, recorded with
    `page.on('request')`.
  
  This replaces the old literal-text assertion. The behaviour change was
  agreed (decision 2). It is not a weakening: the no-run and no-load
  checks get stronger.

### Documentation

- [x] [docs/workspace.md:565-568](docs/workspace.md#L565-L568): describe the
  new rules. GitHub's HTML allowlist, nine tags shown as text, images not
  loaded, a link's address on hover when it can be followed and inline
  when it cannot, task lists, alerts, references, and diagrams still shown
  as source.
- [x] [docs/designs/review-workspace.md](docs/designs/review-workspace.md#L1166):
  add an **As built** bullet under PR-54 recording the change from "raw
  HTML off" and why: Dependabot and Renovate bodies are mostly HTML. The
  original line 469 stays as history.
- [x] [THIRD_PARTY_LICENSES.md:77-78](THIRD_PARTY_LICENSES.md#L77-L78):
  `markdown-it` now writes HTML that the application parses inertly and
  copies through an allowlist. `linkify-it` is now switched on.
- [x] Comment-only edits saying the body is now drawn as GitHub-flavoured
  Markdown through an allowlist:
  [internal/workspace/api/apitypes/types.go:111-112](internal/workspace/api/apitypes/types.go#L111-L112)
  and [web/src/api/types.ts:76](web/src/api/types.ts#L76).

### Tooling

- [x] In `web/`: `npm ci`, `npm run typecheck`, `npm run lint`,
  `npm test`, and `npm run e2e -- --trace off`. Use the pre-installed
  Chromium at `/opt/pw-browsers/chromium` if the pinned Playwright
  version needs it.
- [x] `go build ./... && go vet ./internal/workspace/...`, for the
  comment-only Go edit.

## Execution notes

- **Inline title:** `InlineMarkdown` uses a small backtick pattern, not
  `markdown-it`'s `parseInline`. It behaves the same (code in backticks,
  everything else as typed) and needs no token filtering.
- **Diagram blocks:** `pre` keeps `data-language` from markdown-it's
  `language-X` class, so the existing diagram test still holds.
- **Whitespace:** whitespace-only text inside `ul`, `ol`, `dl`, table parts,
  `details` and `blockquote` is not drawn. React rejects text inside table
  rows, and the fixture test checks that React logs no error.
- **Emails:** `fuzzyEmail: false` still links `a@b.example` as `mailto:`. It
  cannot be followed, and its address is not repeated beside it.
- **Journey 5** ("an answer is drawn as text") is unchanged and passes. It
  failed only on the CSP report, which `withoutHandlers()` fixed.
- **Results:** typecheck, lint and 121 unit tests pass. The e2e suite has
  56 passed and 16 skipped (the skips are tests limited to one screen
  size, as before). `go build ./...` and `go vet ./internal/workspace/...`
  pass, and `gofmt` is clean.

## Deferred and excluded (agreed before review)

- **Not drawn:** emoji shortcodes (`:tada:`), footnotes, math (`$…$`), and
  syntax highlighting in fenced blocks. Each stays as the text the author
  typed.
- **Mermaid and other diagram blocks** stay as source, per the design.
- **Images** are never loaded, and the CSP is unchanged.
- **Commit SHAs** are not linked automatically.
- **Option B** (GitHub's `body_html`) is not pursued.
- **Before any pull request:** remove `egm_lite_alternatives.md` and this
  checklist from the branch, or confirm with the user that they stay.

## Unverified blind-spot risks

- **Verified (tag removed):** a document made by `DOMParser` does not run
  scripts or fetch resources. The e2e journey checks this under the real CSP.
  A Chromium probe without any CSP also found no handler execution and no
  requests, for `DOMParser`, `<template>` content and `createHTMLDocument`
  alike. **Finding:** under the CSP, Chromium reports a violation for every
  inline `on…` attribute it parses, even in a detached document. That
  failed the journeys' no-violations check. Fix (in `Markdown.tsx`):
  `withoutHandlers()` takes `on…` attributes out of raw HTML before parsing,
  reading tags the way the HTML tokenizer does. The allowlist copy is still
  the security boundary.
- **[⏳ ACK]** A link to `/{owner}/{repo}/issues/{n}` redirects to the pull
  request when `n` is one. This is GitHub's documented behaviour. It is not
  tested here, because the tests use a fake GitHub host.

---

### 🔍 Assumption Lifecycle Legend:

- `[⚠️ UNVERIFIED]`: Siloed external contract. Requires indisputable
  evidence to verify.
- `[⏳ ACK]`: Acknowledged assumption. Factual evidence unavailable; risk
  accepted and opaque-box empirical test strictly scheduled.
