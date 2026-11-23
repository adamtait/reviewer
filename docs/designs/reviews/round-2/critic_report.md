# Goldfish Review Report: Stage 7, Critic Review (round 2)

**Target:** `docs/designs/review-workspace.md` (FeatureRange, lines 1–572), as revised after the first review round.

**Standard:** `feature-spec-standard.md` for the design and structural reviewers. The style reviewer applied its own audit vectors.

**Verdict:** the structural reviewer finds that the layout now conforms to the standard. The design and style reviewers do not pass the document as written.

## Reviewer Status

| Reviewer | Status | Findings |
| --- | --- | --- |
| `egm-design` | completed | 2 Blocker, 7 Major, 1 Nit |
| `egm-structural` | completed | 1 Blocker, 3 Major, 6 Nit |
| `egm-style` | completed | 0 Blocker, 9 Major, 1 Nit |

All three reviewers could research on the web, and each external fact below carries the page it was checked against. None changed a file in the repository.

## Prioritized Findings (Blockers -> Major -> Nits)

### D-1. The strongest alternative is unevaluated, so the Alternatives Rule is not met - Severity: Blocker

* **Reviewer Lens:** `egm-design`; the same point is S-1 from `egm-structural`
* **Affected Scope:** lines 26, 30–32, 98–105, 440–441, and the gate at 575
* **Discrepancy Description**: The standard fails a spec that has not shown viable alternatives were considered and rejected for documented reasons. "GitHub's own features, or a browser-hosted VS Code" is recorded as "Not yet evaluated". "Try the CLI's model lane with wider inputs" is "Not run" because it "delays the build", which is a schedule preference. The design says E4 could reduce everything to the change brief, and all four entry conditions are unanswered. The reviewer's own check suggests E4 will come back at least partly positive:
  * **Asking about code.** GitHub documents "Ask about this diff" for a file or selected lines in Files changed, and "Summarize this pull request", for anyone with a Copilot licence ([GitHub docs](https://docs.github.com/en/copilot/tutorials/using-copilot-to-explore-pull-requests)). This contradicts "There is no way to ask", subject to licence.
  * **Navigation.** GitHub documents jump-to-definition and find-all-references for TypeScript in the repository file view. It works by name, within one repository, and is not described for the diff view ([GitHub docs](https://docs.github.com/en/repositories/working-with-files/using-files/navigating-code-on-github)).
  * **What survives.** On the documentation alone, only compiler-resolved navigation inside a pull request is clearly missing.
* **Rationale & Recommendation**: Run E4 and the wider-inputs trial now. Write each result into Alternatives as a rejection with reasons, or cut the gateway and assistant. Set the feedback thresholds. The check has to be run; rewording cannot close the gap.

### D-2. The first-open pipeline has no owner once the request ends, and a dead stage never recovers - Severity: Blocker

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 192, 257, 275–293, 359, 384, 519–520, 567
* **Discrepancy Description**: The service "has no background work of its own", so fetch, import, index load, brief and review all run inside some reviewer's HTTP request. A second reviewer "sees its state and does not start it again". No lease, heartbeat or expiry is described. Cloud Run allocates CPU only during request processing under request-based billing, ends a request at its timeout (5 minutes by default), terminates an instance over its memory limit, and can shut an idle one down at any time ([billing settings](https://docs.cloud.google.com/run/docs/configuring/billing-settings), [request timeout](https://docs.cloud.google.com/run/docs/configuring/request-timeout), [container contract](https://docs.cloud.google.com/run/docs/container-contract)).
* **Rationale & Recommendation**: A timeout, a deploy or an out-of-memory kill during the fetch ends the worker without cleanup. The stage row stays at "fetching" or "generating", and because commits never change, nothing retries it. A phone makes this likely, and the brief is shared, so one reader's lost connection can strand it for everyone. Add an owner and a lease expiry to each stage, let any later request take over an expired stage, say where the work runs, state the request timeout, and add an "abandoned" state with a retry for source and index.

### D-3. "Can read the repository on GitHub" is not "is on the team", and E1 misses three token limits - Severity: Major

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 62, 102, 189, 322–326, 447–449, 523
* **Discrepancy Description**: Sign-in accepts any valid GitHub token, and no login allowlist, organisation check, network restriction or rate limit is stated. GitHub says tokens always include read access to all public repositories. If any connected repository is public, as this one is meant to be, every GitHub account gets its code view, its telemetry including per-reviewer events, and AI questions charged to its budget. Fine-grained tokens also have limits E1 does not ask about: each covers a single user or organisation; outside collaborators can only use classic tokens; and a token pending approval reads only public resources, which looks the same as "no access" ([managing tokens](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens)).
* **Rationale & Recommendation**: A classic token with the `repo` scope is not refused, although it is the write-everything credential used to reject OAuth Apps. Add a login allowlist or organisation-membership check at sign-in, refuse public repositories unless allowlisted, accept only fine-grained tokens, and add the three questions to E1.

### D-4. The content being scanned can switch the secrets scanner off - Severity: Major

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 64, 334–341, 495, 524, 536
* **Discrepancy Description**: The repository's own allowlist is rejected because a pull request could clear its own secret. The same hole exists inline. The scanner ignores any line carrying a `gitleaks:allow` comment unless run with `--ignore-gitleaks-allow`, reads a `.gitleaksignore` file from its working directory by default, and does no decoding by default, so a base64-wrapped key passes ([gitleaks README](https://github.com/gitleaks/gitleaks)). The existing invocation sets none of these, and the planned test covers only `.gitleaks.toml`.
* **Rationale & Recommendation**: An agent told to make CI green can add the comment, and Goal 9 then sends the credential. Separately, "one gateway" holds by convention only: provider keys are environment variables visible to the whole process, and the CLI's own adapters stay in the binary. Pass the three flags, add a planted-credential test for each, add a test that only the gateway imports provider adapters or reads key variables, and state that the gate catches known token formats only.

### D-5. Reusing the CLI review code breaks two claims - Severity: Major

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 361, 367–370, 403, 522
* **Discrepancy Description**: Guidance is read from the merge base so a change cannot weaken its own rules. The plan then passes the description and intent record to the review code as guidance documents, which that code introduces as documents that "win" where they disagree with general practice. The author's own text lands in the one slot the model is told to obey. Separately, the review code cuts the diff at 120 KB and notes this only inside the prompt. Git lists files in path order, so a lockfile comes before `src/`, and a dependency bump can use the whole budget.
* **Rationale & Recommendation**: The view would still say "the model raised nothing". Give the review code a separate author-supplied input rendered in its untrusted section, return truncation to the caller, add a "partial" state, and exclude lockfiles and generated files.

### D-6. Index-derived statements sit in the trusted half of the brief - Severity: Major

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 57, 210, 312, 318, 361–363, 521–522
* **Discrepancy Description**: The index is "treated as written by the pull request's author". Yet "what unchanged code depends on it" goes under "The code shows", and that split is the stated mitigation for hidden instructions. Honest incompleteness is the likelier failure: a job that indexes one `tsconfig` in a repository with several yields a valid partial index, and an empty reference list then reads as "nothing depends on this".
* **Rationale & Recommendation**: Measure coverage at load and show it with every reference list and in the brief. Label these statements as a third source, "the CI-built index says". Say when references were capped, and back every empty result with a text search for the name.

### D-7. Cost is given in tokens only, and the estimate collides with provider spend caps - Severity: Major

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 345, 477, 501, 518, 544
* **Discrepancy Description**: 40 million input tokens a week is about 173 million a month. At one provider's published input prices of $2 to $8 per million, depending on the model, that is roughly $350 to $1,400 a month before any output tokens ([pricing](https://platform.claude.com/docs/en/about-claude/pricing)). That provider's monthly spend caps are $500 and $1,000 on its two lowest tiers, and at the cap usage pauses until the first day of the next month ([rate limits](https://platform.claude.com/docs/en/api/rate-limits)). CUJ 6 could therefore last weeks.
* **Rationale & Recommendation**: "Quota exhausted" also has several shapes: a rate-limit response with a retry time, a spend-cap response without one, and others. The estimate omits output tokens, the always-on database charge, a second dependency install per push, and prompt caching. Give a monthly money figure per model class with output included, check the account's tier cap before PR-52, require prompt caching, and map each quota shape to an outcome.

### D-8. GitHub Actions behaviour leaves holes in when an index exists - Severity: Major

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 135–139, 241–248, 287, 310, 316, 478
* **Discrepancy Description**: Workflows do not run on `pull_request` activity while the pull request has a merge conflict, so such a pull request sits at "waiting for CI" indefinitely. Commits pushed with a workflow's own token do not create workflow runs, so an agent that runs inside Actions never gets an index ([events that trigger workflows](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows)). The artifact listing returns the run's head commit but no event type, so "a pull request run" needs a second call. The reviewer could not confirm that the head commit reported for a pull request run is the head and not the temporary merge commit.
* **Rationale & Recommendation**: Artifacts last 90 days by default, so a swept commit cannot always be re-indexed. Look runs up by head commit and use their status to separate "waiting" from "not produced", with a time limit. Consider triggering on every branch push, which conflicts do not block.

### D-9. One rejection rests on false premises, and a pinned fact is a year stale - Severity: Major

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 103, 457, 527
* **Discrepancy Description**: "Reading a whole tree takes one call per file" is wrong: one call returns a whole commit as an archive. "Search would be lost" does not follow either, because the chosen design searches its own Postgres copy. What `git` buys is the diff, file hashes and integrity checking. The design also says the latest `scip-typescript` release is 0.4.0 from October 2025; the registry shows five later releases, the newest 0.6.2 on 10 November 2026, which bundles TypeScript 6 and takes its SCIP bindings from a different organisation ([npm registry](https://registry.npmjs.org/@sourcegraph/scip-typescript)).
* **Rationale & Recommendation**: Correct both entries. Dropping `git` would remove one of the two programs in Goal 8 and the in-memory scratch repository, so the table should weigh that honestly. Have E2 choose a version deliberately and record which TypeScript major it uses.

### S-2. "Empty environment" is stated as a rule in four places and contradicted in one of them - Severity: Major

* **Reviewer Lens:** `egm-structural`; the same point is Y-2 from `egm-style`
* **Affected Scope:** lines 63, 304, 523, 534
* **Discrepancy Description**: Goal 8's check, the Risks row and the security bullet say `git` starts with an empty environment. Line 304 says the same, then says the viewer's token is passed to `git` through the environment.
* **Rationale & Recommendation**: As worded, Goal 8's test cannot pass against the mechanism described, and the Risks row offers the empty environment as a mitigation for stored tokens while the token itself travels that way. Use one wording everywhere, such as "no inherited environment; `git` receives only the viewer's token". The style reviewer adds that "none of them appears in logs" names only pattern matching as its mechanism, and an encryption key has no recognisable pattern.

### S-3. The Risks table is not the full list of risks the document names - Severity: Major

* **Reviewer Lens:** `egm-structural`
* **Affected Scope:** lines 516–528; risks stated elsewhere at 100–104, 326, 440, 479
* **Discrepancy Description**: Missing from the table: the "accepted risk" that wider context is first tested when the brief ships; entry condition E1, refusal of token approval; entry condition E3 failing, which has no stated fallback anywhere; the encryption key being in the process's memory; and access removed on GitHub lasting for the cache period.
* **Rationale & Recommendation**: A reader who uses the table to confirm the risks misses these five. Add them. The design reviewer adds a sixth: per-user file-viewing time visible to all, which the concepts brief flags and the table omits.

### S-4. The Open Questions section is not the full list of open points - Severity: Major

* **Reviewer Lens:** `egm-structural`
* **Affected Scope:** lines 22, 96–113
* **Discrepancy Description**: The Overview sends the reader to another file for the points that still need the author. That file lists six. Two have no entry in Open Questions: telemetry scope, and the author's confirmation of the changes the drafter made beyond the author's four rulings.
* **Rationale & Recommendation**: As a result, the telemetry goal and the rejections of the GitHub App route, the bucket, the key service and the job queue all read as settled, when the record says the author can still overrule them. Add the two points to Open Questions.

### Y-1. The problem statement asserts facts about GitHub that GitHub's documentation contradicts - Severity: Major

* **Reviewer Lens:** `egm-style`
* **Affected Scope:** lines 11, 26, 30, 32, 105, 441
* **Discrepancy Description**: The bold leads "The diff is plain text." and "There is no way to ask." are unattributed absolutes, while line 26 says they are the requester's untested account. This supports D-1 with the same sources. Line 11 also says "the team reports missing", where the brief attributes the report to one requester.
* **Rationale & Recommendation**: Read literally, E4's stop rule is met by GitHub's documentation alone. Attribute the leads to the requester, name the specific gap left after GitHub's documented features, and give E4 the capabilities to compare.

### Y-4. "An empty review means the model raised nothing" does not match the reused review code - Severity: Major

* **Reviewer Lens:** `egm-style`
* **Affected Scope:** line 370, with 368 and 47
* **Discrepancy Description**: A disproved finding is dropped silently (ADR-0023), a finding with an unknown rule is discarded (ADR-0022), a finding off changed lines is dropped (ADR-0007), and the diff is clipped at 120 KB. An empty list can mean the model raised findings that were then removed, or that it did not read everything.
* **Rationale & Recommendation**: `docs/providers.md` names "reading an empty result as approval" as a failure mode, and the view's wording invites that reading. Say "no finding survived the checks", and state when the diff was truncated.

### Y-5. The gates are called decisive but have no pass or fail criteria - Severity: Major

* **Reviewer Lens:** `egm-style`
* **Affected Scope:** lines 38, 66, 100, 103–105, 109, 110, 467, 527
* **Discrepancy Description**: The Overview says the plan names "what result would stop or reshape the rest", while the open questions say the author still sets the numbers. E2 says "record whether it works" with no definition of working. E3 records a time with no limit. E4 says "a fixed, short time" with no duration. The phone test names no screen width and no definition of failing.
* **Rationale & Recommendation**: Give each gate a number or an observable condition, or say plainly that the criterion is still open.

### Y-6. "Before any line is read" is not what the described flow delivers - Severity: Major

* **Reviewer Lens:** `egm-style`
* **Affected Scope:** lines 36, 60, 117–121, 279–280, 293
* **Discrepancy Description**: The Problem section promises that the reason for a change is on screen before any line is read. The flow shows the diff first and then generates the brief, and says the first reviewer waits for it.
* **Rationale & Recommendation**: The first reader of every new commit sees the diff before the brief. State the claim for later readers of a commit, and describe the first reader's case as it is.

### Y-7. Several alternatives are rejected on reasons that are overstated or unmeasured - Severity: Major

* **Reviewer Lens:** `egm-style`
* **Affected Scope:** lines 435, 456, 457, 458, 468, 501
* **Discrepancy Description**: Beyond the two false premises in D-9: "a persistent disk, which means one machine" overlooks that Cloud Run documents network file system mounts, whose real drawback is that they do not support file locking ([volume mounts](https://docs.cloud.google.com/run/docs/configuring/services/nfs-volume-mounts)). "Most intermediate commits are never opened" has not been measured. "Which leaves generated types missing" was never tested. "Its TypeScript indexer" is ambiguous, because LSIF has two and only one recommends moving.
* **Rationale & Recommendation**: Calibrate each reason. The decisions may still stand on the reasons that hold.

### Y-8. Reader-facing labels name a source the service cannot confirm - Severity: Major

* **Reviewer Lens:** `egm-style`
* **Affected Scope:** lines 57, 121, 210, 343, 363, 522
* **Discrepancy Description**: A result is called "compiler-labelled", but the service knows only that it came from an artifact. Half the brief is titled "The code shows", but its content is model output. "Each statement links to its source" is a property of model output with no stated enforcement.
* **Rationale & Recommendation**: Use labels such as "from the CI-built index" and "the model's reading of the code", and state what happens to a statement with no link.

### Y-9. Several goal checks and the performance requirements do not measure what they name - Severity: Major

* **Reviewer Lens:** `egm-style`; the structural reviewer raises the performance point as a nit
* **Affected Scope:** lines 54, 57, 59–61, 69, 519, 526, 540–544
* **Discrepancy Description**: Goal 4 (asking, diagrams, alternatives) is checked by whether answers link to excerpts. Goal 6 is checked by whether a commit and model name are shown. Goal 5's check does not test "before". Goal 2's check has no pass condition. The Performance section describes mechanisms and holds no number; the one target, "under one second", sits in the Risks table with no query or repository size.
* **Rationale & Recommendation**: Make each check test the goal's own claim, and put numeric targets, or "not yet set", in the requirements section.

### Nits

| ID | Lens | Affected scope | Finding |
| --- | --- | --- | --- |
| D-10 | `egm-design` | 380, 522; 384–386 | The content security policy is to be set in the page, where not every control applies; send it as a response header, pin the diagram library and draw diagrams in a sandboxed frame. The retention rule cannot be computed: no pull request state is stored and the sweep has no credential, so it is really age-only. |
| S-5 | `egm-structural` | 199; 429–510 | Alternatives are said to be grouped by decision number, and several are not. A single provider, the key in the environment and live reads have rejected alternatives but no row in the decisions table. |
| S-6 | `egm-structural` | 187–197, 412–421 | The nine decisions and eight new ADRs are not mapped to each other. Decisions 7 and 9 have no ADR, and ADR 0036 matches no decision. |
| S-7 | `egm-structural` | 20, 60, 203, 230, 282, 288, 404 | "Three sources" above a four-row table. Goal 5 names four stand-in states and the table has five. "Stage" has three meanings. The later stages are said to be listed once and appear twice. |
| S-8 | `egm-structural` | 22, 38, 62, 100, 427, 537 | "Five minutes in the example configuration" has no such example. "The implementation plan" could mean an existing file. Pull request numbers are used before they are introduced. |
| S-10 | `egm-structural` | 103, 447, 468, 509 | The LSIF claim and two claims resting on vendor documentation carry no link. |
| Y-10 | `egm-style` | throughout | "The author" means two people, and four words name the signed-in person. Unquantified superlatives ("least code", "fewest parts"). Present tense for things not built. Revision history narrated in the text. ADR-0004 cited for a rule that is in ADR-0021. "It needs no secret" holds only if no install needs a registry token. |

## Checked and found to hold

* **Structure.** All five required areas are present. No heading level is skipped, all 16 tables are well formed, and all six relative links resolve.
* **The linked decision record.** Both review files exist and record what the design says they record.
* **External facts.** The default checkout on a pull request is the merge commit. The artifacts API filters by name and returns the run's head commit. A fine-grained token with read access to Actions lists and downloads artifacts. The `repo` scope is the only OAuth scope for private code and includes write. The Monaco editor's README answers "No" on mobile browser support. Cloud Run's file system is in memory. The cited study supports the sentence it is cited for.
* **Arithmetic.** The token and row estimates follow from their stated assumptions.

## Not verified

* Whether a fine-grained token's permissions can be read back.
* Whether the team holds Copilot licences.
* Whether the head commit reported for a pull request's workflow run is the head or the temporary merge commit.
