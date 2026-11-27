# Goldfish Review Report

**Target:** `docs/designs/risk-gated-merge.md` (`Lines 1-757`, FeatureRange; the Detailed Implementation Plan from line 758 is out of scope)

**Stage:** 7, Critic Review. **GoldfishContext:** none.

**Standards:** `egm-design` and `egm-structural` applied `resources/document_standards/feature-spec-standard.md`. `egm-style` applied the audit vectors in its own persona.

**Evidence rules:** Reviewers read the repository (`cmd/`, `internal/`, `docs/adr/`, `.github/`, `docs/designs/review-workspace.md`). They did not read `docs/designs/briefs/` or `docs/designs/reviews/`. They had no web access, so every claim in this report about GitHub behaviour is unverified.

## Reviewer Status

| Reviewer | Status |
| --- | --- |
| `egm-design` | completed |
| `egm-structural` | completed |
| `egm-style` | completed |

**Severity counts after deduplication:** 2 Blocker, 22 Major, 27 Nit.

**When lenses disagreed on severity,** the higher one is used and the finding says so.

**Repository citations all three reviewers confirmed (no finding):**
- `cmd/reviewer/flags.go:101` (subcommand table) and `:230-239` (`errCheckFailed`, the exit-1 precedent).
- `internal/config/load.go:70-73` (`KnownFields(true)`).
- `internal/ghclient/write.go` (four write methods and one read).
- `ListWorkflowRuns`, `ListRunArtifacts` and `PreviousFilename` exist.
- `docs/designs/review-workspace.md:520` and `:530`.
- ADR-0035 is the next free number.

## Prioritized Findings (Blockers -> Major -> Nits)

### The docs allowlist can include files that models and agents read as instructions - Severity: Blocker
* **Reviewer Lens:** `egm-design` (with `egm-style` on the vague exclusion wording)
* **Affected Scope:** `Lines 25-28`, `Lines 71-72`, `Lines 91-95`, `Lines 213-214` (Q8), `Lines 383-393`
* **Discrepancy Description**: Docs-only is the only v1 path that removes human review. The allowlist excludes "agent instruction files" and "markdown that code reads" (lines 390-391). It does not define either term, and nothing enforces either one. The repository feeds documentation to a model:
  * `.review/config.yaml` accepts a `guidance:` list (`internal/config/config.go:29`).
  * Each listed document is read from the working tree (`internal/laneb/gather.go:39-47`).
  * The text is inserted into the model-lane prompt (`internal/laneb/assemble.go:42-45`, `:103`).
  * The tests use `docs/conventions.md` and `docs/architecture.md` (`internal/laneb/assemble_test.go:163-171`, `:240-252`).

  The spec's own example allowlist, `docs/**` minus `docs/adr/**`, would contain such files. The coding agents that write most pull requests also read repository docs as context. A prompt-injected agent could therefore merge, with no review, text that steers every later model review and agent session. That breaks the rule at lines 71-72 that only inputs the pull request cannot change may grant eligibility.
* **Rationale & Recommendation**:
  * Name the exclusion concretely: "files listed in `guidance:`, files referenced from agent instruction files, and markdown imported, embedded or parsed by build or runtime code".
  * Make `reviewer owners check` fail when an allowlisted path is named in `guidance:` or referenced from an agent instruction file.
  * Add this case to the Risks section (next finding) and give it a CUJ.
  * If agent-read docs cannot be told apart from other docs, narrow the class to directories confirmed to be read only by people.

### Missing Risks and Limits section - Severity: Blocker
* **Reviewer Lens:** `egm-structural` (supported by `egm-design`, whose findings point to several risks that have no home)
* **Affected Scope:** No such section exists; it belongs between `Lines 700-704`. Risks are scattered across `Lines 58-67`, `Lines 213-214`, `Lines 405-406`, `Lines 499-501`, `Lines 706-714`.
* **Discrepancy Description**: The feature-spec standard requires a Risks and Mitigations area covering failure modes, security implications, scalability limits and mitigations. `docs/designs/review-workspace.md`, which line 741 names as this design's template, has an H2 "Risks and Limits" (its line 603). This spec has none:
  * "That window is a stated risk" (line 501) is stated nowhere else.
  * The silent loss of review from a wrong CODEOWNERS file (lines 405-406) appears only as the reason for a scope decision.
  * Q8 (production docs) has no mitigation.
  * The unverified GitHub behaviours that v1 and Part 4 depend on are not gathered as dependency risks.
  * Rate limits and the 20-file history cap are not discussed.
  * The risk job's exposure is not discussed: it runs on pull request content with `pull-requests: write` and `actions: read`.
* **Rationale & Recommendation**: Add an H2 "Risks and Limits" before Non-Functional Requirements. Give each risk one row: failure mode, impact, mitigation, and the goal or question it ties to. Cover at least:
  * CODEOWNERS misconfiguration.
  * Model-read docs (Blocker above).
  * Suppressed or removed risk labels.
  * The kill-switch sweep window.
  * Docs-site deploys from allowlisted paths.
  * Agents enabling auto-merge on every pull request.
  * Label fatigue.
  * Branch-update churn.
  * Retention of audit evidence.
  * API ceilings.
  * Each unverified GitHub behaviour.

### G1 cannot hold as designed, and its check depends on a later-stage component - Severity: Major
* **Reviewer Lens:** `egm-design`, `egm-structural`, `egm-style`
* **Affected Scope:** `Lines 79-82` (G1), `Lines 322-323`, `Lines 341-343`, `Lines 503-507`
* **Discrepancy Description**: G1 is a v1 goal: a record on every pull request. Its check is "the post-merge audit query finds no merged pull request without a record". The only post-merge audit in the spec is the scheduled job in Part 4, which belongs to the later stages, so v1 cannot be verified as written. The goal also fails in these cases:
  * `needs: review` usually skips the dependent job when `review` fails (unverified GitHub behaviour). The review job can fail before the binary runs, at the history check (`action.yml:41-49`) or on a checksum mismatch (`:65-66`). This contradicts line 343.
  * Fork pull requests get a read-only token (ADR-0018:43-45), so the job cannot post a comment or labels.
  * A draft marked ready may get no run if `ready_for_review` is not a trigger type (unverified).
* **Rationale & Recommendation**:
  * Define a v1 audit, for example a `reviewer outcomes` mode that looks for the `<!-- rv:risk -->` marker on merged pull requests, or change G1's check to something v1 provides.
  * Specify `if: always()` or an equivalent on the risk job, and list the trigger types.
  * Exclude fork pull requests from G1 explicitly.

### Docs-only goes live in v1 with no precision target, failure policy or detection - Severity: Major
* **Reviewer Lens:** `egm-design` (with `egm-style` on the undefined term "escape")
* **Affected Scope:** `Lines 125-127` (G12), `Lines 208-210` (Q6), `Lines 401-402`, `Lines 697-700`, `Line 727`
* **Discrepancy Description**: G12 requires a written precision target and failure policy before a merge class goes live. It is listed only under the later stages, yet docs-only goes live in v1. For v1 the spec has:
  * only a proposal of "zero escapes plus a permanent audit sample";
  * no definition of an escape for docs;
  * no owner, size or cadence for the audit sample;
  * no detection after a merge, because the audit job exists only in Part 4;
  * no kill switch other than an admin editing the ruleset.

  Part 3's outcome labels (reverted, hotfixed) will rarely catch a bad docs change, so "zero escapes" cannot be measured.
* **Rationale & Recommendation**:
  * Apply G12 to docs-only in v1.
  * Define a docs escape.
  * Name the owner, size and cadence of the audit sample.
  * Define a v1 detection mechanism, for example `reviewer outcomes` filtered to merges that had no review.

### The ownership check guards a case that cannot happen and misses the cases that matter - Severity: Major
* **Reviewer Lens:** `egm-design`
* **Affected Scope:** `Lines 96-98` (G5), `Lines 248-254` (CUJ 4), `Lines 387-391`, `Lines 397-400`, `Lines 404-406`
* **Discrepancy Description**: Part 2 makes `*` owned the first CODEOWNERS rule (line 387). A new `scripts/` directory is therefore already owned, unless an unowned allowlist rule matches it, and then it is allowlisted by definition. CUJ 4 cannot occur as written, and G5's check (adding an unowned top-level directory) passes trivially. The cases that would silently remove review are not covered:
  * `owners check` matches patterns differently from GitHub's matcher.
  * An allowlist pattern is broader than intended.
  * "`docs/**` minus `docs/adr/**`" depends on rule order, which the spec does not state.
  * GitHub ignores invalid lines or owners without write access (unverified).

  The spec also never says where `owners check` gets the allowlist. If the source is "whatever CODEOWNERS leaves unowned", the check is circular.
* **Rationale & Recommendation**:
  * Define the allowlist in one machine-readable file.
  * Have the check compare the effective owner of every tracked path against that file.
  * Specify the rule order in the CODEOWNERS template.
  * Add a sandbox experiment confirming that GitHub's matcher agrees with `owners check` on a set of fixtures.
  * Rewrite G5 and CUJ 4 around these cases.

### Native settings cannot enforce the docs-only class as G4 defines it - Severity: Major
* **Reviewer Lens:** `egm-design`
* **Affected Scope:** `Lines 91-95` (G4), `Lines 238-246` (CUJ 3), `Lines 482-484`
* **Discrepancy Description**: G4 limits the class to files "added or modified" under the allowlist, and Part 4 treats removals, renames, copies and mode changes as not eligible. CODEOWNERS has no notion of file status. In v1, all of these would merge without review:
  * deleting allowlisted docs;
  * a rename into the allowlist;
  * a mode change;
  * a symlink under `docs/`.

  Renames out of owned paths depend on unverified GitHub behaviour.
* **Rationale & Recommendation**: Restate the v1 class as what GitHub enforces ("every changed path is unowned"). Add the rename, delete and symlink cases to the Q1 sandbox experiments, or have a CI check fail on those statuses for pull requests with no review.

### Relationship to ADR-0009, ADR-0018 and ADR-0026 is not recorded, and the handling is inconsistent - Severity: Major
* **Reviewer Lens:** `egm-design`, `egm-structural`, `egm-style`
* **Affected Scope:** `Lines 21-24`, `Lines 378-381`, `Lines 438-443`, `Lines 463-465`, `Lines 628-632` (D1)
* **Discrepancy Description**: ADRs are append-only (`docs/adr/README.md`), but the spec handles each affected ADR differently, in passing:
  * **ADR-0009.** Its text says a gate should be "someone's choice, in their own workflow, and not a default of this tool" (`docs/adr/0009-never-block-a-pull-request.md:43-45`), and its Revisit-when section says "not a flag here" (`:56-58`). A fail-closed `cmd/merge-gate` in this repository conflicts with that. D1 concedes a separate consumer is "the shape ADR-0009 describes", then rejects it with only "build it in this repository". The spec never says how ADR-0009's status changes. ADR-0029:53-54 is the precedent for scoping ADR-0009 explicitly.
  * **ADR-0018, risk job.** Line 380 has a new ADR-0035 record the risk job's `actions: read` "because ADR-0018 lists what the tool can do". ADR-0018 sets the review job's permissions and lists what the tool cannot do.
  * **ADR-0018, gate.** Line 465 supersedes ADR-0018 for `statuses: write`.
  * **ADR-0026.** It sets two release artifacts from one tag. Line 440 gives the gate "its own release artifact" without discussing this.
* **Rationale & Recommendation**:
  * Add an H3 under Technical Plan, modelled on review-workspace.md's "Changes to recorded decisions". Give each new or affected ADR a row: decision, and relation to existing records (new, supersedes, scopes).
  * State how ADR-0009, ADR-0018 and ADR-0026 are reconciled.
  * Give D1 a concrete reason, for example the shared `ghclient` and `diff` packages and release in lockstep, as ADR-0029:60-61 argues.

### Alternatives are rejected "by author" without concrete trade-offs - Severity: Major
* **Reviewer Lens:** `egm-structural`, `egm-design`
* **Affected Scope:** `Lines 521-523`, `Line 544` (A2), `Lines 571-572`, `Line 578` (B2), `Line 624` (C2), `Line 632` (D1)
* **Discrepancy Description**: The standard requires each rejection to state a concrete technical or scope trade-off, and forbids attributing a rejection to a person without linking a decision record. The convention used here, "Rejected by author ... during the brainstorm", names a person and links nothing. Several Reason fields have no trade-off:
  * D1 restates the choice.
  * B2 ("the route needs neither an App nor a machine user") is circular.
  * A2 says only "with A1".
  * C2 names only the lens that raised it.
* **Rationale & Recommendation**: Put the deciding trade-off in each Reason field, for example "a separate consumer would need X, which costs Y". Remove "by author", or link the discussion where the decision was made.

### Non-Goals point to rationale that does not exist - Severity: Major
* **Reviewer Lens:** `egm-structural`
* **Affected Scope:** `Lines 151-153`; compare `Lines 519-700` and `Lines 746-749`
* **Discrepancy Description**: Lines 151-153 say six merge classes are "each ruled out for now with a reason (see Alternatives Considered)": comment-only, formatting-only, generated code, lockfile bumps, test-only and inert-additive. No Alternatives entry covers any of them. Future Improvements gives one-line prerequisites for four. Formatting-only and lockfile bumps have no rationale anywhere. The most visible scope decision, why docs-only is the only class, therefore has no recorded alternatives.
* **Rationale & Recommendation**: Add an Alternatives group, for example "Which merge classes", with one entry per class giving pros, cons and the deciding reason. Otherwise, repoint the Non-Goal to a section that holds the reasons.

### The no-App premise is unconfirmed but not tracked as an Open Question - Severity: Major
* **Reviewer Lens:** `egm-design` (with `egm-style` on the subjectless hedge)
* **Affected Scope:** `Lines 141-142`, `Lines 191-214`, `Lines 564-572` (B1), `Line 754`
* **Discrepancy Description**: Line 141 says Apps are "understood to be disallowed (not yet confirmed)" and does not say who holds that view. B1 is called the only route that fully answers forgery. Part 4's whole mechanism follows from the premise: the pinned required workflow, the forgery workarounds, and a post-merge audit as proof. No Q-item tracks it.
* **Rationale & Recommendation**: Add an Open Question with an owner: "Does organisation policy forbid a GitHub App?" Make Part 4's choice of mechanism wait on the answer. Rewrite line 141 as "Organisation policy is believed to disallow Apps (unconfirmed; see Qn)".

### A pull request can suppress its own risk labels, and a missing label reads as safe - Severity: Major
* **Reviewer Lens:** `egm-design`
* **Affected Scope:** `Lines 84-87` (G2), `Lines 223-236` (CUJ 1, CUJ 2), `Lines 372-375`
* **Discrepancy Description**: Under the `pull_request` trigger, the workflow runs from the pull request's own commit (ADR-0018:12-13). A pull request that edits the review workflow can therefore drop the risk job or its labels, and G2's promise to label `.github/` changes cannot be enforced. A suppressed record also looks the same as a low-risk one. CUJ 1 even teaches reviewers that "no `risk/` label" means a glance is enough. Agents with write tokens can remove labels, and label removal is driven by the tool's previous comment, which any workflow can edit.
* **Rationale & Recommendation**:
  * Have the comment carry a run reference.
  * Show a "record missing or stale" state instead of an empty label set.
  * State that `risk/review-config` is best-effort for `.github/` changes, and name native code-owner review of `.github/` as the real control.
  * List label removal under Risks.

### G8 cannot pass as defined, and line 509 claims more than G8 guarantees - Severity: Major
* **Reviewer Lens:** `egm-design`, `egm-style`
* **Affected Scope:** `Lines 112-114` (G8), `Lines 475-478`, `Line 509`
* **Discrepancy Description**: G8 requires zero disagreements with GitHub's own approval state "for an agreed period". The gate's human path is deliberately stricter:
  * the approval must be on the current head;
  * the approver must not have pushed to the pull request;
  * write permission is checked at evaluation time.

  Agents push with a person's token (lines 66-67), so many pull requests will disagree by design, and the gate never leaves shadow. "An agreed period" sets no duration or sample size, so "zero" has no denominator. Line 509 ("At no step is review weaker than today") is absolute, but step 3 replaces the native rule with new code, a risk that E2 itself lists.
* **Rationale & Recommendation**:
  * Redefine G8 as "the gate never passes where GitHub's native rule would not". Log disagreements in the stricter direction as an expected class.
  * Set the period: "at least N weeks and M pull requests, fixed before shadow starts".
  * Make line 509 conditional on G8 passing before step 3.

### "Require branches to be up to date" brings back the staleness the design targets, and is missing from the rollout - Severity: Major
* **Reviewer Lens:** `egm-design`, `egm-style`
* **Affected Scope:** `Lines 49-51`, `Lines 496-498`, `Lines 509-517`, `Lines 674-675` (E1)
* **Discrepancy Description**: Revocation and E1 rely on the ruleset requiring branches to be up to date. Every merge to `main` then forces every open pull request to update and re-run CI and the gate. Native auto-merge may not update branches itself (unverified), so agents rebase in a loop. That is the "branch drifts, agent waits" problem from lines 49-51, and the spec states no cost. Line 497 also presents the setting as existing fact, but neither the Part 2 ruleset nor the rollout steps add it.
* **Rationale & Recommendation**:
  * Write "the gate ruleset must require branches to be up to date" and add the setting to the rollout list.
  * Estimate the update churn from G6 data, and say who or what updates branches.
  * Record the throughput limit as a risk, with a trigger for revisiting E1.

### The post-merge audit is called proof, but its evidence can be forged or expire - Severity: Major
* **Reviewer Lens:** `egm-design` (with `egm-style` on the word "proof")
* **Affected Scope:** `Lines 211-212` (Q7), `Lines 503-507`, `Lines 685-692` (E3)
* **Discrepancy Description**:
  * E3 rejects check runs because they expire (ADR-0016:52-53). Job summaries and run logs also have retention limits (unverified), and the spec does not address this.
  * The audit finds the gate run "by workflow file path and run ID". A pull request branch can contain a file at that path, so the run's workflow ref or SHA must also be checked against the pinned ref.
  * Nobody is named to act on a flag, and no response time is set.
* **Rationale & Recommendation**:
  * Specify the matching key: pinned workflow ref and SHA, event, and head SHA.
  * State the retention assumption.
  * Name an owner and response time for flags, and tie both to Q7.
  * Replace "proof" with "authoritative check".

### Nothing measures whether risk labels help, or removes noisy ones - Severity: Major
* **Reviewer Lens:** `egm-design`
* **Affected Scope:** `Lines 79-90` (G1-G3), `Lines 129-130`, `Lines 334-337`
* **Discrepancy Description**: The v1 goals for `reviewer risk` require only that output exists and contains no "safe" label. Defaults cannot be removed (lines 336-337), so noisy labels such as `risk/deps` or `risk/wide` cannot be tuned out, and label fatigue is the likely failure. The project's own pattern is to measure acceptance per rule and delete rules nobody acts on (ADR-0024; ADR-0017:39-42). The spec has no equivalent.
* **Rationale & Recommendation**: Add a v1 metric, for example per-label precision against Part 3's "changed after review" label. Define how a default label is dropped when the evidence supports it.

### References into the unreadable brief, and a colliding decision ID (D5) - Severity: Major
* **Reviewer Lens:** `egm-structural`, `egm-style`
* **Affected Scope:** `Line 91`, `Lines 193-212`, `Line 333`, `Lines 388-389`, `Line 405`, `Line 455`, `Line 465`, `Line 483`, `Line 509`, `Lines 587-690` (lens attributions), `Lines 628-665` (Alternatives D1-D5), `Lines 737-738`
* **Discrepancy Description**: The spec cites many items it never defines:
  * experiments 1-3, 13 and 15;
  * decisions D-A, D-B, D-E, D5 and D6;
  * "brief, Blocker 5/8/9/10" and "brief, Major 24/25/27";
  * PR-63;
  * named review lenses.

  A fresh engineer cannot resolve them, which fails the standard's core principle. "Decision D5" in Q6 (line 209) means the precision target, while Alternative "D5" (line 659) means new config keys. Two ID formats are mixed (D-A and D5).
* **Rationale & Recommendation**:
  * Add a short register, for example under Open Questions, giving each cited experiment and decision a one-line description.
  * Rename one ID scheme so that no ID is reused, for example "BD-5" for brief decisions.
  * Where a brief finding is cited, restate its point in one clause.

### GitHub behaviour is marked "unverified" in some places and stated as fact in others - Severity: Major
* **Reviewer Lens:** `egm-style`, `egm-structural` (proposed as Nit by `egm-structural`; Major used)
* **Affected Scope:** `Lines 36-37`, `Lines 66-67`, `Lines 163-166`, `Lines 173-174`, `Line 312`, `Lines 417-418`, `Lines 452-455`, `Lines 496-498`, `Lines 504-505`, `Lines 557`, `Lines 569-570`, `Lines 612-615`
* **Discrepancy Description**: The same claim changes status depending on where it is read. Line 612 marks name-match forgery "(unverified, experiment 1)", while lines 36-37 and 66-67 state it flatly. These claims are stated without a marker, and no reviewer could confirm them from the repository:
  * required checks are matched by name;
  * checks and statuses attach to a commit SHA;
  * auto-merge merges as the account that enabled it;
  * GitHub's convention for revert titles and bodies;
  * "a job can only end as success or failure", which is the stated reason the commit status exists, although jobs can also be cancelled or skipped;
  * any workflow can post as the same identity;
  * merging needs `contents: write`;
  * repository secrets can be read by workflows on agent branches;
  * `pull_request_target` behaviour on base branches an attacker creates.

  Line 615 hedges with "probably" instead of a marker. Only check-run expiry is supported in the repository (ADR-0016:52).
* **Rationale & Recommendation**: Mark every untested statement of GitHub behaviour "(unverified, experiment N)", or link it to a numbered experiment. Replace "probably" with the marker. Re-check the job-outcome claim at line 454, since it is the reason for a design element.

### Non-Functional Requirements contradict Part 1 - Severity: Major
* **Reviewer Lens:** `egm-structural`, `egm-style`, `egm-design`
* **Affected Scope:** `Lines 716-724`, `Line 726`; compare `Lines 320-327`
* **Discrepancy Description**:
  * **Latency.** "`reviewer risk` adds under 30 seconds to the review job." Risk is a separate `needs: review` job with its own runner start-up, full-history checkout (`action.yml:41-49`), install and artifact download, so the record arrives after the review finishes.
  * **Rollback.** "Turn the action input off." The input `mode` takes `review` or `risk` and has no off value, and Part 1 enables risk by adding a job.
  * **API cost: pagination.** `ListFiles` and `ListIssueComments` page through results (`internal/ghclient/client.go:271-281`), so 1 and 2 calls are understated, and `risk/wide` (more than 40 files) implies multi-page lists.
  * **API cost: CODEOWNERS.** Reading the local CODEOWNERS file is not an API call.
  * **API cost: gate.** The figure of about 6 calls omits per-approver scaling and the status post.
  * **API cost: outcomes.** `reviewer outcomes` likely needs timeline or event calls to find the ready-for-review time (unverified).
* **Rationale & Recommendation**:
  * Restate latency as the risk job's wall-clock target, with a measured basis.
  * Make the rollback "remove the `risk` job".
  * Correct the call budget: "5 plus 1 per approver, plus 1 status post" for the gate, with pagination noted.

### The risk-record definition disagrees with itself - Severity: Major
* **Reviewer Lens:** `egm-style`, `egm-structural` (proposed as Nit by `egm-structural`; Major used)
* **Affected Scope:** `Line 22`, `Lines 79-81`, `Lines 183-184`, `Lines 223-225`
* **Discrepancy Description**: The Overview says a record is "one comment and one or more labels". CUJ 1 has the README fix carrying no `risk/` label. Terms says "one marker comment and its labels".
* **Rationale & Recommendation**: Use "one comment and zero or more `risk/` labels" in the Overview, G1 and Terms.

### Unsourced figures carry key arguments - Severity: Major
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Lines 12-14`, `Lines 53-55`, `Lines 64-65`, `Lines 99-105`, `Lines 531-533`, `Lines 697-698`
* **Discrepancy Description**:
  * "More than 90% of the pull requests" and "dozens of pull requests a week" have no source.
  * Three rejections rest on "dozens a week": line 64, A1, and E4 ("months at this volume").
  * Lines 13-14 rank human review as the most expensive step, yet line 54 says no baseline exists and G6 is there to create one.
* **Rationale & Recommendation**: Give each figure a source and period, or label it an estimate. Write the duration claim as arithmetic ("300 / N per week = M weeks"). Present the cost ranking as the team's view, to be measured by G6.

### "Carry no production risk" contradicts Q8 - Severity: Major
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Lines 49-51`, `Lines 213-214`
* **Discrepancy Description**: The Problem heading says docs changes "carry no production risk". Q8 says rendered docs are production content. "An approval that adds little" is also a judgement nobody has measured.
* **Rationale & Recommendation**: Use "changes with little production risk", and limit the claim to docs outside any deployed docs site.

### Overview says "two things" but lists four, and "no organisation-level change" conflicts with Q2 - Severity: Major
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Lines 17-19`, `Lines 198-199`
* **Discrepancy Description**: "This design adds two things to `reviewer`, in two phases" is followed by three v1 items and one later item. Two of the v1 items, the GitHub settings and the measurement, are not additions to `reviewer`. "Needs no organisation-level change" conflicts with Q2, which may require an org owner.
* **Rationale & Recommendation**: Rewrite as "This design has two phases. v1 (no organisation-level change unless Q2 finds an organisation approval minimum) has three parts: ...".

### CUJ 8 overstates revocation - Severity: Major
* **Reviewer Lens:** `egm-style` (with `egm-structural` on the missing risk entry)
* **Affected Scope:** `Lines 286-287`, `Lines 499-501`
* **Discrepancy Description**: CUJ 8 says passes already posted "are revoked by the sweep". Part 4 says a posted pass can still merge until the sweep finishes. Neither place gives a duration or an action for the operator to take during the window.
* **Rationale & Recommendation**: In CUJ 8, state that passes can still merge until the sweep finishes, and give a target duration. In Revocation, name the step the maintainer takes during the window, for example disabling auto-merge on affected pull requests.

### CUJ 2's outcome depends on an unresolved "if" - Severity: Major
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Lines 232-235`
* **Discrepancy Description**: "`risk/auth` still appears for `src/auth/**` if a default covers it" leaves the result open, so nobody can check the Then clause.
* **Rationale & Recommendation**: State the result ("the built-in auth default covers `src/auth/**`, so `risk/auth` still appears"). Add a separate clause for a path that only the repository file listed.

### Additional Resources and Future Improvements: heading levels and contents - Severity: Nit
* **Reviewer Lens:** `egm-structural`, `egm-style`
* **Affected Scope:** `Lines 733-754`
* **Discrepancy Description**:
  * Both sections are H3 headings after a horizontal rule, so they nest under Non-Functional Requirements. The template uses H2 for both (review-workspace.md:657, 667).
  * Line 740 lists ADR-0004, 0023 and 0026, which the body never cites, and omits the proposed ADR-0035.
  * Paths are code spans, not links.
  * Reviewers could not check the brief paths at 737-739 because they were barred from that directory.
* **Rationale & Recommendation**: Promote both headings to H2. Align the ADR list with the body's citations, and use relative links.

### Terms are defined after use, and some terms are missing or inconsistent - Severity: Nit
* **Reviewer Lens:** `egm-structural`, `egm-style`
* **Affected Scope:** `Lines 25-37`, `Lines 79-127`, `Lines 161-189`, `Lines 61`, `Line 138`, `Line 178`, `Line 210`, `Line 304`, `Line 350`, `Line 485`, `Line 555`, `Line 560`, `Line 700`, `Line 709`
* **Discrepancy Description**:
  * The Overview and Goals use these terms before Terms defines them: "merge class", "pinned required workflow", "deny-by-default", "base-branch policy", "kill switch" and "break-glass".
  * "Exclusion", "never-qualify list" and "the fixed floor" name one concept.
  * "Model lane" and "risk map" are not defined.
  * The index has four names: navigation index, `review-index` artifact, base-branch index, and navigation index artifact.
  * "Org owner" and "organisation owner" are both used.
  * "Escape" is not defined.
* **Rationale & Recommendation**: Move Terms under the Overview, as the template does. Pick one name for each concept, and add the missing terms.

### Horizontal rules used inconsistently - Severity: Nit
* **Reviewer Lens:** `egm-structural`
* **Affected Scope:** `Line 702`, `Line 733`, `Line 756`
* **Discrepancy Description**: Rules surround only the NFR section; no other H2 boundary has one.
* **Rationale & Recommendation**: Keep the rule after the header block and the one before the Detailed Implementation Plan. Drop the rules at 702 and 733.

### Alternatives status labels do not match the legend - Severity: Nit
* **Reviewer Lens:** `egm-structural`, `egm-style`
* **Affected Scope:** `Lines 521-523`, `Line 587`, `Line 597`, `Line 603`, `Line 616`, `Line 624`, `Line 664`, `Line 682`, `Line 690`
* **Discrepancy Description**: The legend defines "Open", but no entry uses it. B5 uses an undeclared fourth label, "Not rejected", for an option the design adopts as a complement. Attributions such as "raised by the ... lens" and "every round-2 lens" are review-process jargon.
* **Rationale & Recommendation**: Drop "Open" or use it. Move B5 out of the rejected alternatives. Replace the lens attributions with "raised in design review", or remove them.

### Architecture diagram is incomplete and misaligned - Severity: Nit
* **Reviewer Lens:** `egm-structural`, `egm-style`
* **Affected Scope:** `Lines 295-316`
* **Discrepancy Description**: The Part 2 `reviewer owners check` job is missing from the diagram. Text in the merge-gate column runs into the risk column (line 302) and is split by an arrow (line 305). Line 304 has trailing whitespace.
* **Rationale & Recommendation**: Add the owners-check node, and give each lane its own aligned column.

### Lines exceed the wrap width - Severity: Nit
* **Reviewer Lens:** `egm-structural`, `egm-style`
* **Affected Scope:** `Line 433`, `Line 640`
* **Discrepancy Description**: Both lines run past the roughly 100-column wrap used elsewhere in the document.
* **Rationale & Recommendation**: Rewrap them.

### No CUJ covers Part 3 - Severity: Nit
* **Reviewer Lens:** `egm-structural`
* **Affected Scope:** `Lines 216-290`, `Lines 408-436`
* **Discrepancy Description**: G6 and `reviewer outcomes` have no user journey, while every other part has at least one.
* **Rationale & Recommendation**: Add a CUJ for producing the baseline.

### Run-on Overview sentence - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Lines 22-24`
* **Discrepancy Description**: The sentence "It never decides or blocks anything, and it exits 0, so ADR-0009 holds, and the review job keeps the permissions ADR-0018 states" chains four clauses.
* **Rationale & Recommendation**: Use "It is advisory and always exits 0 (ADR-0009). The review job's permissions (ADR-0018) do not change."

### Subjective framing - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Line 58`, `Line 65`
* **Discrepancy Description**: "The obvious fix is unsafe in this setting" is subjective framing. "The test suites are not trusted enough" does not say who distrusts them.
* **Rationale & Recommendation**: Use "Scoring pull requests and merging the low scores is unsafe here" and "the team does not yet trust the test suites as a substitute (see Non-Goals)".

### Vague cross-reference - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Line 172`
* **Discrepancy Description**: "(unverified; see Open Questions)" does not name the question.
* **Rationale & Recommendation**: Use "(unverified; Q4)".

### "Writer" is overloaded - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Line 259`, `Line 477`
* **Discrepancy Description**: "Writer" names a person here, but `ghclient.Writer` and `StatusWriter` are interfaces.
* **Rationale & Recommendation**: Use "a user with write access".

### G2 examples read as label names but are not - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Lines 83-84`
* **Discrepancy Description**: "touches auth", "changes CI" and "crosses 3 owner groups" read like literal label names, but they are not in the label set.
* **Rationale & Recommendation**: Use `risk/auth`, `risk/ci` and `risk/wide`.

### Label table rows are not parallel - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Lines 363-372`
* **Discrepancy Description**: Only the first row starts with "a changed path matches".
* **Rationale & Recommendation**: Start every row the same way, or drop the phrase from the first row.

### Overclaim about label removal - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Lines 374-375`
* **Discrepancy Description**: "It never removes a label a person set" fails when a person sets the same label the tool added earlier.
* **Rationale & Recommendation**: Use "it removes only `risk/` labels its previous comment lists". See also the Major finding on label suppression.

### Long chained sentence in Reach - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Lines 348-356`
* **Discrepancy Description**: One sentence chains five clauses.
* **Rationale & Recommendation**: Split it into three sentences: the source artifact, how the run is found, and how it is queried.

### Filler - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Line 395`
* **Discrepancy Description**: "This repository supplies two pieces of support:" is filler.
* **Rationale & Recommendation**: Use "This repository adds:".

### Outcome heuristics: unclear antecedent, no gate, and line drift - Severity: Nit
* **Reviewer Lens:** `egm-style`, `egm-design`
* **Affected Scope:** `Lines 417-433`
* **Discrepancy Description**: "All three heuristics ... before anyone relies on them" has an unclear antecedent and sets no gate. The "hotfixed in the same lines" heuristic compares patch line ranges across pull requests and ignores drift from commits in between.
* **Rationale & Recommendation**: Name the three heuristics, and gate publication of G6 numbers on a hand audit of N pull requests. Attribute lines with blame on the later pull request's base, or state that the hand audit is expected to correct for drift.

### "Proves" overclaims a test - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Lines 441-442`
* **Discrepancy Description**: "An import-allowlist test proves it imports only ..." claims more than a test can show.
* **Rationale & Recommendation**: Use "checks".

### Metaphor - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Line 467`
* **Discrepancy Description**: "Events are a doorbell, not an input." The next sentence says the same thing literally.
* **Rationale & Recommendation**: Delete the metaphor.

### Ambiguous "now" - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Line 476`
* **Discrepancy Description**: "Has write permission now" does not say when "now" is.
* **Rationale & Recommendation**: Use "at evaluation time".

### Loose paraphrase of ADR-0017 and an unsourced count - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Lines 531-533`
* **Discrepancy Description**: ADR-0017 rejected a combined score for deciding presentation, not for merging. "About 20 signals" has no source.
* **Rationale & Recommendation**: State the scope of ADR-0017's rejection accurately. Cite the signal count or drop it.

### Statistic without a confidence level - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Line 697`
* **Discrepancy Description**: "Bounds the rate only below about 3/n" gives no confidence level.
* **Rationale & Recommendation**: Add "at 95% confidence".

### Redundant sentence - Severity: Nit
* **Reviewer Lens:** `egm-style`
* **Affected Scope:** `Lines 39-40`, `Lines 292-293`
* **Discrepancy Description**: Both passages say each part can ship or be dropped on its own.
* **Rationale & Recommendation**: Keep one.

### `risk/findings` needs data the marker does not carry - Severity: Nit
* **Reviewer Lens:** `egm-design`
* **Affected Scope:** `Line 371`, `Lines 341-343`
* **Discrepancy Description**: Markers carry only the fingerprint and rule ID (`internal/fingerprint/fingerprint.go:30-39`). High confidence can only be inferred from "posted inline" (ADR-0017:26-27).
* **Rationale & Recommendation**: State that inference rule. Say whether resolved threads, or threads left outdated by earlier pushes, count.

### B4's ADR-0008 argument is weaker than presented - Severity: Nit
* **Reviewer Lens:** `egm-design`
* **Affected Scope:** `Lines 593-596`
* **Discrepancy Description**: ADR-0029 (Proposed) already adds a hosted service to this repository (`docs/adr/0029-add-a-hosted-workspace-service.md:25-36`).
* **Rationale & Recommendation**: Lead with the vendor-data and App reasons instead.

### Reach depends on a Proposed ADR - Severity: Nit
* **Reviewer Lens:** `egm-design`
* **Affected Scope:** `Lines 348-356`, `Lines 750-751`
* **Discrepancy Description**: Reach relies on ADR-0034, which is:
  * Proposed, and contingent on the workspace trial (`docs/adr/0034-scip-index-built-on-push.md:25`);
  * limited to repositories with a root `tsconfig.json` (`:29-30`);
  * unable to verify the index (`:49-50`).
* **Rationale & Recommendation**: Note the dependency, and that `risk/reach` will often show "not available" in consumer repositories.

### Q3 has no fallback, and Security treats it as settled - Severity: Nit
* **Reviewer Lens:** `egm-design`, `egm-style`
* **Affected Scope:** `Lines 200-202`, `Lines 378-381`, `Lines 706-709`
* **Discrepancy Description**: Line 709 says "Q3 confirms that labels fit `pull-requests: write`", but Q3 is open. Labels may need `issues: write` or rights to create labels (unverified).
* **Rationale & Recommendation**: Write "Q3 must confirm ...", and give a fallback, for example a comment-only record.
