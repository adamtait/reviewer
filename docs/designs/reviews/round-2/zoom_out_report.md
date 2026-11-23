# Goldfish Review Report: Stage 5, Initial Zoom Out (round 2)

**Target:** `docs/designs/review-workspace.md` (full file, lines 1–954), as revised after the first review round.

**Standard:** `zoom-out-standard.md`

**Verdict from the zoom-out reviewer:** does not meet the standard yet. Context sufficiency passes. The failures are in using what already exists, in building ahead of need, and in level of detail.

## Reviewer Status

| Reviewer | Status | Findings |
| --- | --- | --- |
| `egm-zoom-out` | completed | 1 Blocker, 8 Major, 1 Nit |
| `egm-inventive-catalyst` | completed | 5 Major, 5 Nit |

Both reviewers read the target in full and neither read the first round's reports. Unlike the first round, both could research on the web, and each external fact below carries the page it was checked against. Neither created or changed a file in the repository.

## Prioritized Findings (Blockers -> Major -> Nits)

### Z-1. The problem is not established, and the check that would establish it is deferred and too narrow - Severity: Blocker

* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** lines 26–32, 100–105, 441, 575
* **Discrepancy Description**: Entry condition E4 could reduce the design to the change brief, yet about 380 lines of file-level plan assume it passes. A short pass over public documentation already answers part of E4:
  * **Navigation in pull requests exists.** GitHub announced go-to-definition and find-references in the Files changed tab in December 2021. For TypeScript it is based on name search, not on the compiler ([GitHub blog](https://github.blog/news-insights/product-news/precise-code-navigation-python-code-navigation-pull-requests/), [GitHub docs](https://docs.github.com/en/repositories/working-with-files/using-files/navigating-code-on-github)). The reviewer could not confirm it survives in GitHub's current pull request page.
  * **Asking about a diff exists.** Copilot Chat on github.com explains selected diff lines and summarises a pull request with repository context, for Copilot licence holders ([GitHub docs](https://docs.github.com/en/copilot/tutorials/explore-pull-requests)).
  * **Third-party tools have the same shape.** Devin Review opens any pull request by swapping the host in the address, groups and explains the diff, and has codebase-aware chat ([Devin docs](https://docs.devin.ai/work-with-devin/devin-review)). Sourcegraph's browser extension adds definition and reference lookups on pull request diffs.
  * **The team's own agent tool** runs cloud sessions on a repository from a browser or phone.
* **Rationale & Recommendation**: Lines 30 and 32 are inaccurate as written. What looks distinctive is compiler-resolved navigation, the intent record, the one-provider rule and phone use, and none of these is used as a comparison criterion. Answer E4 first, widen it to third-party tools, and restate the Problem as the remaining gap.

### Z-2. Justified as a test, sized as a product, and ordered so the test comes late - Severity: Major

* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** lines 38, 109, 440, 579–596, 899
* **Discrepancy Description**: The first feedback week follows PR-50 and offers reading a diff and text search, which GitHub already does, so it tests none of the hypotheses. The change brief addresses the first stated problem and is what E4 says would survive, yet it ships eighth with no feedback point after it. The stop rule depends on thresholds that are still an open question. The feedback measures are computed by telemetry that arrives in PR-55, after all three feedback weeks.
* **Rationale & Recommendation**: Run the declined trial of the brief's premise now, set thresholds before PR-47, and take early measures from request logs.

### Z-3. The largest block of new code exists to avoid a disk, for data the design calls recoverable - Severity: Major

* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** lines 192, 257, 273–304, 384–386, 456–457, 476–478, 520, 525–526, 686–691
* **Discrepancy Description**: To stay stateless, the design adds a file store keyed by hash, per-commit path lists, stored diffs, a trigram index, index de-duplication and a reference-counting sweep. Line 456 concedes that a bare git repository is "least code", and line 478 says source and index can be fetched again, so a disk would hold only a cache. Search speed is unmeasured, and at the design's own sizing of 5,000 files one commit's text can be scanned directly.
* **Rationale & Recommendation**: The platform does not fit the work. Cloud Run gives CPU only while a request is in flight by default, can stop idle instances at any time, and caps requests at 60 minutes ([container contract](https://docs.cloud.google.com/run/docs/container-contract), [request timeout](https://docs.cloud.google.com/run/docs/configuring/request-timeout)). First open runs a fetch, an import, an index load and several model calls, and no owner is named for a stage whose instance has gone. Line 457 also rests on a wrong fact: GitHub lists a whole tree in one call and serves it as one archive. The real limit is that compare lists at most 300 changed files ([commits API](https://docs.github.com/en/rest/commits/commits)). The design also promises `git` an empty environment while passing it the token through the environment. Simpler options: one small machine with git as the store and a database only for authoritative records, or one archive per commit scanned in memory.

### Z-4. Passing the author's description as "guidance" undoes the design's own rule - Severity: Major

* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** lines 361, 368, 674, 887–888
* **Discrepancy Description**: Guidance is read from the merge base so a change cannot weaken its own rules. The plan then passes the description and intent record to the CLI's review code as guidance. Guidance is the one input the model is told to defer to: `internal/laneb/assemble.go` says guidance documents "win", and the review prompt says "the guidance wins". Text written by the agent that opened the pull request would arrive with that authority. Guidance also shares a 24 KB budget consumed in order, so a long description can crowd out real guidance.
* **Rationale & Recommendation**: Reading the outcome from a side channel is the same avoidance. `internal/laneb/invalidate.go` keeps every finding when the disprove pass cannot run, so quota running out between the two passes would show unchecked findings, and the view has no state for that. The fix is small: add an author-context field to the review code's input, and return typed outcomes from the review and disprove calls.

### Z-5. The identity choice rests on a comparison GitHub's documentation does not support - Severity: Major

* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** lines 84, 102, 189, 322–326, 447–449, 523
* **Discrepancy Description**: The App route is rejected because it needs an organisation owner. By default an owner must also approve each fine-grained token, and such tokens expire within 366 days ([token policy](https://docs.github.com/en/organizations/managing-programmatic-access-to-your-organization/setting-a-personal-access-token-policy-for-your-organization)). The token route needs that approval per user and per renewal. Outside collaborators cannot use fine-grained tokens for organisation repositories, and one token cannot span organisations ([managing tokens](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens)).
* **Rationale & Recommendation**: The stored-token risk is specific to pasted tokens. An App's user token is limited to permissions both the App and the user hold, and expires after eight hours ([App user tokens](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-user-access-token-for-a-github-app)). E1 should put both routes to the owner at once, and ask about outside collaborators and how many organisations are involved.

### Z-6. A second provider stack is built beside the first - Severity: Major

* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** lines 343, 419, 668, 866–867
* **Discrepancy Description**: `internal/model` already holds Anthropic and Gemini adapters with shared retry and redaction. The design adds a second pair in the gateway, plus a wrapper to make the new pair look like the old interface. ADR-0021 names per-run cost and streaming as its revisit triggers. Both have fired, yet the new ADR only "builds on" it. The adapters are tested against hand-written fixtures, so the tests check the author's reading of the provider documentation.
* **Rationale & Recommendation**: Reopen the decision: either one adapter per provider with the single call as the simple case, or the vendors' own SDKs.

### Z-7. File-level detail is stacked on four open conditions, and one stated fact is already stale - Severity: Major

* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** lines 100–105, 527, 560, 575, 598–917, 756
* **Discrepancy Description**: More than a third of the document fixes function signatures, event names, numeric caps and a key table, all conditional on the entry conditions.
  * The design names `scip-typescript` 0.4.0 as the latest release. The npm registry shows five later releases, the newest 0.6.2 on 2026-11-10, which bundles TypeScript 6 and needs Node 22 ([npm registry](https://registry.npmjs.org/@sourcegraph%2Fscip-typescript)).
  * Its README lists workspace flags for yarn, pnpm and npm, and none for `bun`.
  * The SCIP Go bindings are now `github.com/scip-code/scip/bindings/go/scip`. That module requires Go 1.25 and pulls in more than protobuf.
  * The `scip` command line tool has an experimental command that converts an index to SQLite, which the design does not weigh.
* **Rationale & Recommendation**: Stop the document at line 571 until E1 to E4 are recorded.

### Z-8. Reuse of the CLI is thin, and sharing its repository is costly - Severity: Major

* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** lines 196, 257–271, 410–421, 436, 604, 628, 751–763, 773, 784, 840, 857, 912
* **Discrepancy Description**: Five of eleven parts reuse nothing, and the reused packages each need new exported entry points. The repository's rules are expensive for a service: three ADRs are superseded, the licence checker is rewritten, and one private deployment must be configured as a generic product with no defaults. `pgx` and the SCIP bindings both declare Go 1.25, while `go.mod` declares 1.24 and CI tests 1.24.
* **Rationale & Recommendation**: The author chose this placement, but the design should state its cost. A nested Go module under the same root might isolate dependencies; the reviewer did not check this.

### Z-9. The diagram library fails the repository's own licence rule - Severity: Major

* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** lines 59, 380, 751, 760, 840, 879–880
* **Discrepancy Description**: `mermaid` depends directly on `elkjs`, licensed "EPL-2.0 OR GPL-3.0-or-later" ([mermaid](https://registry.npmjs.org/mermaid/latest), [elkjs](https://registry.npmjs.org/elkjs/latest)). `tools/checklicenses/main.go` refuses both. The planned dual-licence change rescues `dompurify` but not `elkjs`, where both alternatives are refused.
* **Rationale & Recommendation**: By the design's own rule the dependency must then be replaced, and this would surface at PR-53. Show diagram source as text in the first version, or settle the licence question now.

### I-1. A stage shared by all readers is owned by one reader's request, and nothing can take it over - Severity: Major

* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** lines 257, 273–293, 384, 520, 709, 718, 890
* **Discrepancy Description**: Fetch, index load, brief and review are long tasks. Their state is written to Postgres and a second reader "does not start it again", but the work itself lives inside the first reader's HTTP request. Cloud Run ends a request at its timeout, gives CPU only while a request is in progress under default billing, and terminates an instance that exceeds its memory ([request timeout](https://docs.cloud.google.com/run/docs/configuring/request-timeout), [billing settings](https://docs.cloud.google.com/run/docs/configuring/billing-settings)).
* **Rationale & Recommendation**: If the instance dies mid-stage, nothing writes "failed". The row stays "fetching" and every later reader is told to wait. The brief and review also depend on the first reader holding a connection open, which is unlikely on a phone. Give each stage row an owner and a lease the worker renews, and let any request that finds an expired lease take the stage over. The brief and review need no GitHub credential, so they can run detached in a job, as the sweep already does.

### I-2. The secrets gate can be cleared by the content it scans, while harmless fixtures block AI features - Severity: Major

* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** lines 332–341, 495, 524, 566, 617, 692, 864–865
* **Discrepancy Description**: The design refuses any allowlist a pull request could edit. The scanner still offers three: it skips any line carrying a `gitleaks:allow` comment unless started with `--ignore-gitleaks-allow`, it looks for a `.gitleaksignore` file in its working directory, and its default rules exempt paths such as lock files and `node_modules/` ([gitleaks README](https://raw.githubusercontent.com/gitleaks/gitleaks/master/README.md)). The existing invocation passes none of the flags that turn these off. The design tests only that a `.gitleaks.toml` in the content has no effect.
* **Rationale & Recommendation**: An author, or an agent trying to get its brief generated, can clear a secret with one comment. Separately, results are cached by content hash, scanner version and rule file, but the verdict also depends on the file's path through the operator's allowlist, so content first seen at an allowlisted path is cached as clean and then sent from any other path. Run the scanner with every in-content and path exemption off, and apply exemptions in the gateway, honouring a marker or path only where the line or file is unchanged from the merge base.

### I-3. Reusing the CLI's review code unchanged hides partial results - Severity: Major

* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** lines 361–370, 404, 674, 887–888
* **Discrepancy Description**: This repeats Z-4 on author text as guidance and adds two points. The review makes two calls, and if quota runs out between them the disprove pass returns every finding as kept, with no state for "not checked". The review code also cuts the diff at 120 KB, so the view would say "the model raised nothing" about a diff it only partly read.
* **Rationale & Recommendation**: Have the service call the pure parts of the review code (prompt assembly, parsing, caps, verdict application) and own the model calls and their errors. Add states for "findings not checked" and "diff partly read".

### I-4. The index is bounded against wrong answers but not against missing ones - Severity: Major

* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** lines 57, 308–318, 361–363, 521, 886
* **Discrepancy Description**: Every bound checks what the index contains. None checks what it leaves out. Find-references results carry the compiler label, and the brief's "what unchanged code depends on it" is drawn from them. An index covering part of the repository therefore yields "nothing else depends on this" under the product's strongest label, stored for every reader. This needs no bad intent: `scip-typescript` indexes the one `tsconfig.json` project it is run in unless workspace flags are passed.
* **Rationale & Recommendation**: Use the trusted store to audit the untrusted one. Compute coverage per commit, mark files with no index document as "not indexed", and append text matches from those files to reference results under the text-search label.

### I-5. The index job waits on an event that needs a mergeable pull request - Severity: Major

* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** lines 241–248, 287, 310, 359
* **Discrepancy Description**: GitHub does not run `pull_request` workflows while a pull request has a merge conflict ([events that trigger workflows](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows)). Such a pull request gets no run and no artifact, so "waiting for CI" never ends and the brief loses its dependent-code input.
* **Rationale & Recommendation**: Trigger the index workflow on a push to any branch. The default checkout is then the pushed commit, and the service binds on the run's head commit alone. The service can also list runs by head commit to tell "no run started" from "running". Cost: runs for branches nobody reviews, and no index for pull requests from forks.

### Z-10. A one-off baseline is built as a service feature - Severity: Nit

* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** lines 388–394, 589, 596, 724–725, 899–905
* **Discrepancy Description**: `cmd/reviewer/metrics.go` already walks pull request history over a window and prints CSV. The baseline is described as one-off, yet gets an endpoint, a table, a new GitHub call and a screen.
* **Rationale & Recommendation**: A subcommand run once would do. These are wants the requester stated, hence a nit.

### I-6. Every first open transfers a whole commit into memory - Severity: Nit

* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** lines 277, 297, 304, 456–457, 519–520, 810
* **Discrepancy Description**: A single-commit fetch downloads every file of that commit, for head and merge base alike, and Cloud Run's file system is memory. Only the storing is incremental.
* **Rationale & Recommendation**: First, mount a Cloud Run ephemeral disk for scratch, which is a deployment setting ([ephemeral disk](https://docs.cloud.google.com/run/docs/configuring/services/ephemeral-disk)). Then, if E3 shows slow fetches, fetch trees only and fetch just the files not already stored.

### I-7. User tokens are stored on the server, although no flow uses one outside that user's own request - Severity: Nit

* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** lines 102, 324, 479, 523, 696, 797
* **Discrepancy Description**: Nothing is prepared in advance, there is no background work, and the sweep does not call GitHub. A token is only ever needed while its owner's browser is making a request.
* **Rationale & Recommendation**: Seal the token into the session cookie with the server key and keep only the session hash in Postgres. A copy of the database plus the key then yields no token. Cost: sign-out cannot revoke a stolen cookie, and it rules out moving the source fetch to a job, which pulls against I-1.

### I-8. One budget covers the shared brief and private questions - Severity: Nit

* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** lines 288, 345, 359, 501, 518, 625, 670
* **Discrepancy Description**: By the design's own estimate, questions use about three times the tokens of briefs. When the monthly budget is spent, the one artefact that serves every reader stops along with private questions. The brief's states have no entry for a budget stop.
* **Rationale & Recommendation**: A reserved share for brief and review, and a limit on regenerations per commit.

### I-9. The premise behind the brief is tested last and has no feedback week - Severity: Nit

* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** lines 105, 440, 584–588, 594
* **Discrepancy Description**: The cheap trial was dropped because it "delays the build", but it is elapsed time in CI on one repository and can run beside the build. The brief ships with no feedback week.
* **Rationale & Recommendation**: Run the trial during PR-47 to PR-50, and add a feedback point after PR-54.

### I-10. The indexer facts in E2 are out of date, and the stated fallback is weaker than written - Severity: Nit

* **Reviewer Lens:** `egm-inventive-catalyst`
* **Affected Scope:** lines 103, 467, 527, 560, 756, 846
* **Discrepancy Description**: This confirms Z-7 on the indexer's version. It adds that TypeScript 7.0, released in July 2026, ships with no programmatic interface, with a new one expected in 7.1 ([TypeScript blog](https://devblogs.microsoft.com/typescript/announcing-typescript-7-0/)). The fallback "the plugin exports SCIP" relies on the plugin loading the compiler from the repository under review, which has no interface to call in a repository on TypeScript 7.0.
* **Rationale & Recommendation**: In E2, record the TypeScript major version each repository uses, and state the fallback only for repositories whose compiler still exposes the interface.

## Checked and found consistent

* The compare call returns the merge base ([commits API](https://docs.github.com/en/rest/commits/commits)).
* The artifacts list filters by name and reports the run's head commit ([artifacts API](https://docs.github.com/en/rest/actions/artifacts)).
* A fine-grained token with read access to Actions, Contents and Metadata covers the artifact, compare and repository calls.
* The design's descriptions of `ghclient`, `diff`, `gitleaks`, `laneb`, `model`, the installer templates and the CI workflows match the repository.

One correction: "an index rebuilt by re-running CI" holds only for 30 days after the original run, and artifacts last 90 days by default.

## Not verified by either reviewer

* Fetching a single commit by hash with a fine-grained token. Entry condition E3 covers it.
* The claim that an OAuth App's only scope for private repositories also grants write access, and the claim about the Monaco editor on mobile browsers.
* Whether the team holds Copilot licences.
