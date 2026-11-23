# Goldfish Review Report: Stage 7, Critic Review (round 3)

**Target:** `docs/designs/review-workspace.md` (FeatureRange, lines 1–691), as revised after the second review round.

**Standard:** `feature-spec-standard.md` for the design and structural reviewers. The style reviewer applied its own audit vectors.

**Verdict:** the document has every section the standard names, with a clean heading hierarchy and well-formed tables. It does not yet pass the standard's rules on risks and alternatives. One gap in access control lets the central security goal pass its tests while serving another repository's source.

## Reviewer Status

| Reviewer | Status | Findings |
| --- | --- | --- |
| `egm-design` | completed | 1 Blocker, 7 Major, 2 Nit |
| `egm-structural` | completed | 0 Blocker, 5 Major, 5 Nit |
| `egm-style` | completed | 0 Blocker, 7 Major, 3 Nit |

All three reviewers could research on the web, and each outside fact below carries the page it was checked against. None read an earlier review report. None changed a file in the repository.

Findings are prefixed `D-` for design, `S-` for structural and `Y-` for style.

## Prioritized Findings (Blockers -> Major -> Nits)

### D-1. Cached source is not bound to the repository the access check covers - Severity: Blocker

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 99, 337, 347, 356, 360, 370
* **Discrepancy Description**: Archives, indexes and in-flight fetches are shared between users and described as held per commit. The access check is made on the owner and repository in the address, while the commit hash arrives separately from the browser. Nothing says a cached entry is bound to the repository it was fetched for. With entries keyed by hash alone, a user who can read connected repository A but not B is served B's source or index through A's routes. Goal 6's three tests all pass with that flaw in place.
* **Rationale & Recommendation**: Not every engineer can read every repository, and the wording "that commit's archive" invites the unsafe key. State that every cache entry and shared fetch is keyed by owner, repository and hash, and that a conversation is looked up by id, repository and user. Add a Goal 6 test in which a user with access to one connected repository requests another's cached commit, index and conversation through both repositories' routes.

### D-2. Instance behaviour on Cloud Run undermines the shared fetch and the memory bound - Severity: Major

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 153, 337, 347–351, 356, 364, 414, 577, 621–622, 655–660
* **Discrepancy Description**: The diff needs two requests per changed file, so a pull request of about 40 files can reach Cloud Run's default of 80 concurrent requests per instance. Cloud Run then starts a second instance with empty memory, and requests sent there fetch both archives again ([concurrency](https://docs.cloud.google.com/run/docs/about-concurrency)). Idle instances go within 15 minutes, so most opens after a pause are cold. The design says a duplicate fetch "costs seconds" while calling the time unmeasured and accepting 60 seconds in E3. The byte limits cover the cache but not fetches in flight, garbage-collector headroom, or the scanner's scratch files, which Cloud Run holds in memory. Over the limit the instance is terminated and every request on it fails. There is no target for the time from opening a pull request to seeing the diff, and no instance settings or hosting cost.
* **Rationale & Recommendation**: Fix the instance settings in the design (minimum and maximum instances, concurrency) with their monthly cost, because the shared-fetch reasoning depends on them. Draw the diff from per-file reads of the changed files and load the archives behind it. Bound the fetches in flight and count them in the sizing rule. Add a time-to-diff target and measure it cold in E3.

### D-3. Two alternatives are rejected on a Go version that is out of support - Severity: Major

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 254, 561, 571
* **Discrepancy Description**: Two alternatives are rejected because they need Go 1.25 and "this repository declares 1.24", and the plan builds the service image on Go 1.24. Go 1.26 shipped on 2026-02-10 and 1.27 on 2026-08-19, so 1.24 and 1.25 no longer receive fixes ([release history](https://go.dev/doc/devel/release)). This repository's CI matrix is pinned to those two versions.
* **Rationale & Recommendation**: The service faces the internet, carries users' GitHub tokens, and parses tar, zip and protobuf input written by pull request authors with the standard library. Make raising the Go version and CI matrix to supported releases a precondition of PR-48, with a rule for keeping it current. Then argue the two rejections again on the reasons that remain.

### D-4. The outcome rule fits neither provider, and nothing meets a real provider before users do - Severity: Major

* **Reviewer Lens:** `egm-design`; the same point on the outcome rule is Y-6 from `egm-style`
* **Affected Scope:** lines 102, 425–439, 441, 608, 619–620
* **Discrepancy Description**: The rule "rate limited with no retry time means quota exhausted" fits neither provider. Anthropic says not every 429 carries a retry time and that a self-set spend limit returns HTTP 400 ([rate limits](https://platform.claude.com/docs/en/api/rate-limits)). Google's error reference lists three 429 codes with no retry delay, plus HTTP 402 for depleted credit ([API errors](https://aistudio.google.com/docs/api-errors)). A provider 4xx such as a bad key fits none of the nine outcomes. The conversation interface has no place for the reasoning blocks providers expect back unchanged in a tool loop, and reasoning is billed as output, so the 2,000-output-token assumption behind the spend estimate is unsupported. The SDK alternative is dismissed by citing ADR-0021, which declined SDKs "for a single JSON POST". The style reviewer adds that "two lowest account tiers" omits a tier below them, and that the price range is a subset of the page.
* **Rationale & Recommendation**: Map outcomes from each provider's explicit error code, treat an unrecognised 429 as rate limited, and add an outcome for provider request errors. Add an opaque provider part to the turn model. Add an entry condition before PR-53: one scripted live conversation per provider that streams, makes two tool calls and reads token counts; use its output figure to redo the estimate. Weigh the SDKs again against that measured work.

### D-5. The new skill section turns review-thread text into instructions - Severity: Major

* **Reviewer Lens:** `egm-design`; the claim strength is Y-4 from `egm-style`, and the missing risk row is part of S-3
* **Affected Scope:** lines 105–106, 222–226, 310–315, 617–640, 667
* **Discrepancy Description**: The section has an agent read review threads and then push and reply with the pull request author's credentials. Open threads include the CLI's own comments and, where the model lane is on, model-written comments derived from pull request content. The skill does not tell the agent to separate these or to treat thread text as data. Agent replies posted under a person's account carry no marker, so they will mix into the "human reviews" that phase 2 learns from. Journey 10 states agent behaviour as an outcome ("it does not resolve threads"), but the mechanism is prose in a skill file that an agent may not follow. Goal 13's check asserts only that words are present in a template, and journey 10 is never run. Goal 12's check says every existing file is written as before, while the skill file changes.
* **Rationale & Recommendation**: This section ships in PR-47, before anything else. Add risk rows, have the skill skip or list separately bot threads, and require a visible marker on agent replies. Reword journey 10 as what the skill instructs. Make a hand-run of journey 10 with both agents on one repository an exit check for PR-47, and correct Goal 12's check.

### D-6. The "defined outside the repository" label would fire for the repository's own code - Severity: Major

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 95, 152, 184, 371, 374, 625
* **Discrepancy Description**: The rule fires when the index has no definition and the symbol's identifier names a package. `scip-typescript` 0.6.2 gives every symbol the name and version of the nearest `package.json`, including the repository's own code. So the condition holds for almost every in-repository symbol. A definition is missing whenever the index is partial, or its document was dropped because the path is not in the archive, which covers files generated during the CI install.
* **Rationale & Recommendation**: Those lookups would be labelled as defined outside the repository, naming the repository's own package. Goal 2 promises each label says where a result came from. Call a symbol external only when its package name and version match no `package.json` in the commit's archive; otherwise say the index has no definition and show text matches. Add a test for a dropped document, and have E2 count the documents the service would drop.

### D-7. The web editor is rejected on reasons the cited sources do not support - Severity: Major

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 61, 154, 516–519, 592
* **Discrepancy Description**: Two of the four reasons for rejecting GitHub's web editor do not hold against the sources. The cited release notes describe project-wide navigation with typings for packages listed in `package.json`, and VS Code for the Web runs extensions that can call an HTTP service ([web extensions](https://code.visualstudio.com/api/extension-guides/web-extensions)), which bears on "the team cannot extend it". E4 is closed without the trial the concept brief asked for. Reviewing in a desktop IDE with a pull request extension and the team's own permitted agents is not weighed. Three alternative rows and E4 rest on what the design owner reports or requires, without the link to the record that the standard asks for.
* **Rationale & Recommendation**: Navigation is the half of the design that needs a CI job per repository, a hand-written decoder and an unverifiable index. Open one real pull request in the web editor on a desktop browser and on a phone, record what fails, and keep only the reasons that survive. Add the option "navigation from the web editor or IDE, build only the assistant". Link each design-owner decision to its entry in the concept brief.

### D-8. Per-file withholding departs from ADR-0012 without saying so, and the rule file is undefined - Severity: Major

* **Reviewer Lens:** `egm-design`; the limits of Goal 8's check are Y-5 from `egm-style`
* **Affected Scope:** lines 81, 101, 399–423, 501, 599–600, 635–636
* **Discrepancy Description**: ADR-0012 stops the whole model lane on any detection and says the rule is never to be weakened. The service withholds one file and carries on, yet the new ADR is listed as building on ADR-0012 with no argument for the change. A value flagged in one file is still sent when it appears in another file in a form no rule matches. "The service's own rule file" is undefined: the scanner's default rules carry a global allowlist and per-rule allowlists keyed on content, including a long list of stopwords for the generic-key rule, which would contradict "there is no allowlist" and give content a fifth way to silence a rule. One search result can mean scanning up to 200 files, and that time is unmeasured. Goal 8's check claims "each way content could try to switch the scanner off" where four attempts were tested, and names no test for "does not send what it cannot scan" or "only the configured provider".
* **Rationale & Recommendation**: Say in the new ADR that the service departs from the whole-run stop and why. When the scanner reports a credential, block that literal value in every outgoing part of the conversation. Define the rule file and which built-in allowlists it keeps, and add a planted credential containing a stopword to the tests. Measure scan time for a 200-match search. Narrow the goal's wording and add the two missing checks.

### S-1. The decisions table, the ADR table and the alternatives groups disagree - Severity: Major

* **Reviewer Lens:** `egm-structural`
* **Affected Scope:** lines 244–257, 496–506, 540–550, 581–586, 602–608
* **Discrepancy Description**: Decision 6 (the tool loop) is said to be recorded in ADR-0034, which the ADR table defines as the conversation interface. That interface has no row in the decisions table, and its alternatives sit under "Decisions 9 and 10". Three rows under Decision 2 concern index building and change detection. ADR-0031 is marked "builds on ADR-0018", while the text explains that relation through the index workflow, which is ADR-0035's subject.
* **Rationale & Recommendation**: Add a decisions row for the conversation interface with its own alternatives group, and say which ADR records the tool-loop choice. Move the misfiled rows, and attach the ADR-0018 relation to the ADR that covers the index workflow.

### S-2. E1 has no outcome for two of its three failure cases - Severity: Major

* **Reviewer Lens:** `egm-structural`; the same point is Y-7 from `egm-style`
* **Affected Scope:** lines 151, 533, 630, 632
* **Discrepancy Description**: E1 passes only if tokens are approved, no user is an outside collaborator, and there is one organisation. "Otherwise" covers only which method is approved. It gives no outcome when tokens are approved but a user is an outside collaborator, or the repositories span two organisations. Two facts are stated too strongly: by default a repository admin can install an App that asks only for repository permissions ([GitHub docs](https://docs.github.com/en/organizations/managing-programmatic-access-to-your-organization/limiting-oauth-app-and-github-app-access-requests-and-installations)), and the 366-day token lifetime is a default policy an organisation owner can change.
* **Rationale & Recommendation**: State the outcome for each failed condition and mirror it in the risks table. Qualify both facts with "by default".

### S-3. The risks table omits three risks the text raises - Severity: Major

* **Reviewer Lens:** `egm-structural`; a fourth is Y-1 from `egm-style`
* **Affected Scope:** lines 99, 315, 377, 447, 463, 617–640, 667
* **Discrepancy Description**: Missing from the table: agents pushing and replying with the pull request author's credentials; a merge base that was never the tip of a push having no index; and a model statement with no excerpt behind it being shown like any other. The style reviewer adds a fourth. "Only that user can read it" holds only through the application's routes: the operator runs the database and its backups, which hold conversations with code, and holds the cookie key that opens every sealed token.
* **Rationale & Recommendation**: Add the rows. Scope each "only" sentence to "through the workspace's API", and state what the operator can read whatever their GitHub access.

### S-4. Four checks the goals promise have no step in the plan - Severity: Major

* **Reviewer Lens:** `egm-structural`
* **Affected Scope:** lines 92, 106, 107, 161, 658
* **Discrepancy Description**: The document says each goal names its check and the Verification Plan maps each to a test. Goal 14 names no check. Goal 13 names journey 10, which appears in no test list and no hand check. The lookup time is "measured in PR-52", whose plan has no such measurement. The phone trial of journey 2 is not among the feedback-week measures.
* **Rationale & Recommendation**: Reword the claim to match what exists, and either add these checks to the plan or change the four statements.

### S-5. Open Questions is not the complete list it is said to be - Severity: Major

* **Reviewer Lens:** `egm-structural`
* **Affected Scope:** lines 51, 95, 154, 389, 629, 657–660
* **Discrepancy Description**: Four open points sit outside the section: the target for the first words of an answer is "not yet set"; two "(proposed)" targets have no decider or date; whether a token's permissions can be read back is unconfirmed in one place and stated as "cannot" in another; and Goal 2 says E2 measures a share of lookups where E2 counts 50 hand-picked symbols. E4 is closed yet still listed.
* **Rationale & Recommendation**: Add these to Open Questions with a decider and a date, or soften the claim of completeness. Move E4 to the Record of decisions.

### Y-2. Goal 7's check passes while the binary links a second way to start a program - Severity: Major

* **Reviewer Lens:** `egm-style`; the same point is D-10 from `egm-design`
* **Affected Scope:** lines 100, 646
* **Discrepancy Description**: Goal 7 says "the service starts one program", but its check fails only when a service package imports the process-starting package directly or calls the `git` helpers. Three CLI packages the service reuses import it themselves, including the model package's code that starts two CLI programs.
* **Rationale & Recommendation**: Test the dependency closure of the service binary: the packages importing the process-starting package must equal a named list, and only the scan package may call one. Add an image check.

### Y-3. The claim that both agents read the skill file is not supported - Severity: Major

* **Reviewer Lens:** `egm-style`
* **Affected Scope:** line 84; relied on by 98, 106, 140, 310–315
* **Discrepancy Description**: "The team's two agents, Antigravity and Claude Code, both read it" is not supported for Claude Code. Its documentation lists project skills at `.claude/skills/<name>/SKILL.md` and does not list `.agent/skills/` ([skills](https://code.claude.com/docs/en/skills)), and the installer writes only `.agent/skills/code-review/`. Antigravity's documentation gives `.agents/skills` as the default and keeps `.agent/skills` for backward compatibility ([skills](https://antigravity.google/docs/skills)).
* **Rationale & Recommendation**: Test whether a Claude Code session loads the installed skill and record the result. If it does not, write the skill to `.claude/skills/` as well, or limit the claim, the intent record and Goal 13 to Antigravity.

### Nits

| ID | Lens | Affected scope | Finding |
| --- | --- | --- | --- |
| D-9 | `egm-design` | 172, 425–437, 451, 463–464, 628 | An answer can carry a link whose address contains repository text, and one click sends it to another host. The rendering rules cover model output only, not the agent-written title, description and intent record. The content security policy is described for scripts, connections and frames only, while the risks table claims nothing can load from another host. Reaching the round or token limit has no outcome. |
| D-10 | `egm-design` | 103, 210–214, 468–470 | Goal 10's check, a narrow viewport against a fake API, cannot show that code stays visible above a phone keyboard. The design says logs never contain source text, but search terms travel in the request address, which Cloud Run's own request log records. |
| S-6 | `egm-structural` | 11, 17–22, 65–69, 106, 310–315 | The Overview says the workspace "adds two things" and designs "navigation and asking". The design also ships two skill sections and Goal 13, which the Overview never mentions and no gap in The Problem motivates. |
| S-7 | `egm-structural` | 95, 470 | "This was wrong" reports are said to be read from logs. They are stored in the database. |
| S-8 | `egm-structural` | 35, 151, 533, 630; 98, 172, 312; 356, 470, 622, 636 | "Owner" is reserved for the account a repository belongs to, and used three times for the organisation owner. "Operating guide" and "intent record" are used before they are identified and are not in the Terms table. The same points are in Y-9. |
| S-9 | `egm-structural` | 510, 533, 557, 571, 592 | Each alternatives entry is said to give the trade-off that decided it. The LSIF row says "not evaluated", the App row says "not rejected", and the chosen Postgres driver is stated only inside a rejected row. |
| S-10 | `egm-structural` | 82, 441, 503 | "Two of the six are HTTP adapters". The repository has four HTTP paths and two CLI paths, and the design does not say what happens to the two OpenAI-shaped adapters. The same point is in Y-8. |
| Y-8 | `egm-style` | 87, 524 | The review code is said to drop failed checks "without telling the caller"; it returns a warning on every failure path, and only the diff cut goes unreported. "CI has one job, `required`" reads as a single job; there are seven. |
| Y-9 | `egm-style` | 95, 99, 373, 625 | Goal 6 uses "answers" for AI answers and then for GitHub's access reply. One label has three names: "the first label", "the index label" and "the source label". |
| Y-10 | `egm-style` | 19, 51, 92, 577, 619, 629 | Four absolutes are contradicted elsewhere: "for each pushed commit" against the tip of each push only; "each goal names the check" against Goal 14; "the complete list" against an unset target; "nothing is stored at rest" against stored conversations. Two quantities have no basis: a duplicate fetch that "costs seconds", and "500 questions a week" beside dozens of pull requests. |

## Checked and found to hold

* **Structure.** No heading level is skipped, all 26 tables in scope are well formed, and prose counts match their lists. Relative links resolve.
* **ADR citations.** Citations of the existing ADRs match their texts. The ADR tool permits metadata-only supersession, as the design says.
* **The indexer.** `scip-typescript` 0.6.2 is the latest release, depends on TypeScript 6, needs Node 22 and declares UTF-16 positions, so the UTF-16 rule will not drop its documents.
* **GitHub.** The four read permissions in E1 are the ones GitHub lists for the calls the design makes. Fine-grained tokens need owner approval by default, have one resource owner and are not usable by outside collaborators. Pull request workflows do not run on a merge conflict.
* **Provider figures.** The tier caps, the price range and the estimate's arithmetic hold.
* **Dependencies.** The Go version claims for `pgx`, `lib/pq` and the SCIP library hold, as does the Mermaid licence claim.
* **Other platforms.** The VS Code release notes, the web editor's lack of compute, Monaco's mobile statement, and Cloud Run's timeout and file-locking limit are as cited.

## Not verified

* The four-row scanner table: none of the three ran the scanner.
* E3's calls, including whether the archive call accepts a full hash and how `export-ignore` behaves.
* What happens when reasoning blocks are left out of a tool loop: an error, or only lost quality.
* Whether a browser opens 80 or more file requests at once; this depends on web code that does not exist yet.
* Whether one index per push, kept 90 days, exceeds the plan's included artifact storage.
* That GitHub's web editor, as distinct from vscode.dev, offers the navigation the 1.93 notes describe.
* The design owner's reports, the cited study, and the claims about other vendors' tools.
