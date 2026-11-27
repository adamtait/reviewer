# Goldfish Review Report

**Stage:** 8, Intermediate Zoom Out Review

**Target:** `docs/designs/risk-gated-merge.md` (`lines 1-1093`; not edited since Stage 5)

**Inputs the reviewer read:** the target; the Stage 5 zoom-out report, the Stage 6 comprehension report and the Stage 7 critic report; and repository files to check claims. It did not read `docs/designs/briefs/`. It had no web access, so every claim about GitHub behaviour in this report is unverified.

**Repository checks (the synthesizer re-checked the first three):**
- `internal/laneb/gather.go:39-47` reads every `guidance:` file from the working tree into the model-lane input.
- `internal/installer/templates/review.yml.tmpl:10` has a bare `pull_request:` trigger, with no `types:` and no draft filter.
- `docs/designs/review-workspace.md:615` lists "a merge base that was never the tip of a push" as a commit that gets no index.
- `internal/config/config.go:29` defines `guidance:`.
- `docs/adr/0009-never-block-a-pull-request.md:43-45, 56-58` holds the Neutral and Revisit-when text quoted in finding 11.

## Reviewer Status

| Reviewer | Status |
| --- | --- |
| `egm-zoom-out` | completed |

**Severity counts:** 1 Blocker, 10 Major, 4 Nit.

**Main conclusion:** About 40% of the Stage 6 and Stage 7 findings sit in Part 4 or exist because of it. Fixing those in place would make the document longer but no sounder. The findings that matter for v1 come down to a few structural cuts, because several of them share a root cause.

## Prioritized Findings (Blockers -> Major -> Nits)

### 1. The allowlist admits files that models and agents read - Severity: Blocker
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** Lines 25-28, 91-95 (G4), 213-214 (Q8), 383-393
* **Relates to:** the Critic Blocker on model-read docs; the Stage 5 Major on the allowlist; Comprehension 5.
* **Discrepancy Description**: The code confirms the hole. Guidance documents named in `.review/config.yaml` are read from the working tree into the model-lane prompt (`gather.go:39-47`), and the spec's own example allowlist, `docs/**`, would contain them. This is the only v1 path that removes human review, so it stays a Blocker.
* **Rationale & Recommendation**: Adopt the Critic's fix in reduced form.
  - The feature spec states the rule: only people read an allowlisted directory. That excludes anything named in `guidance:`, anything build or runtime code reads, and any published site (Q8).
  - Add one mechanical guard: `owners check` fails if any `guidance:` entry falls under an allowlisted directory. The config is already parsed, so this is cheap.
  - Reject the Critic's second guard, which detects files "referenced from agent instruction files". The reference graph has no clear edge, so the check could only be a heuristic. Make it a checklist item in `docs/merge-classes.md` instead, and limit the class to directories a maintainer confirms are read only by people.

### 2. Repairing Part 4 in place would bloat a stage that cannot yet be validated - Severity: Major
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** Lines 32-37, 107-127, 256-288, 438-517, 562-700 (alternative groups B, C, D1, D3, D4, E1, E2), 721-724, 783-788, 989-1066
* **Relates to:** the Stage 5 Blocker and its Majors on Part 4 depth, the granting gate, the kill switch and side effects; the Critic findings on G8, up-to-date churn, audit as "proof", CUJ 8 and the ADR supersessions; Comprehension 3, 4, 7, 13, 17 and 18.
* **Discrepancy Description**: The later stages raised about nine Majors that sit entirely in Part 4. Fixing them in the document would add:
  - a redefined G8 with a sample size;
  - estimates of branch-update churn;
  - audit retention and a matching key;
  - a sweep duration;
  - a table of rulesets from v1 to the gate;
  - an appendix of 19 experiments.

  All of this would be rewritten once Q4, Q5 and Q7 are answered.
* **Rationale & Recommendation**: Do not apply these fixes here. Carry them forward as a requirements list, one line per item, in the Part 4 sketch (see the Part 4 verdict below).

### 3. Docs-only goes live in v1 with no failure policy - Severity: Major
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** Lines 125-127 (G12), 208-210 (Q6), 401-402, 697-700, 727
* **Relates to:** the Critic Major on docs-only precision; the Stage 5 Major on measurement.
* **Discrepancy Description**: The docs-only class ships in v1. The audit and the response to a failure (G12) live with the gate in Part 4. The Critic's proposed detection, through Part 3's `reverted` and `hotfixed` labels, would rarely catch a bad docs change, as the Critic itself notes.
* **Rationale & Recommendation**: Adopt in reduced form. Move G12's substance into v1 for docs-only as one short paragraph:
  - What counts as a docs escape: a docs-only merge later reverted or corrected for something a reviewer would have caught, or one that changed model, agent or site behaviour.
  - Who owns the audit sample, its size and how often it runs.
  - The response to the first escape: set required approvals back to 1.

  For detection, a query that lists merged pull requests with no approval is enough. Reject detection through outcome labels. That also supports taking those labels out of v1 (finding 9).

### 4. Owners check: use a structural CODEOWNERS lint, not path matching - Severity: Major
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** Lines 96-98 (G5), 248-254 (CUJ 4), 387-400, 873-881, 951-964
* **Relates to:** the Stage 5 Major on the owners check; the Critic Major on the ownership check; Comprehension 14.
* **Discrepancy Description**: Stage 5 and the Critic agree that CUJ 4 cannot happen as written. The Critic's fix needs three new things:
  - a separate machine-readable allowlist file;
  - a comparison of effective owners for every tracked path;
  - a sandbox experiment to show that the tool's matcher agrees with GitHub's.

  The risk the Critic is guarding against, a home-built matcher that disagrees with GitHub, goes away if the check does not match paths at all.
* **Rationale & Recommendation**: Adopt the lint that Stage 5 proposed. It checks that:
  - the first rule is `*` with an owner;
  - CODEOWNERS owns itself;
  - every rule with no owner equals an allowlist entry;
  - allowlisted directories hold only the allowed file types (a prefix check that does not need CODEOWNERS semantics);
  - no `guidance:` entry falls under an allowlisted directory (finding 1).

  The allowlist comes from the `--allowlist` flags in the CI step. One sentence in Part 2 answers the Critic's concern that the check is circular, and no new file is needed. State the required rule order in the template, and make the check a required check. Drop `internal/codeowners`. Its only other user is the owner-group count in `risk/wide`, which rarely fires under this template, so `risk/wide` becomes "more than 40 files" only. Reject the matcher-agreement experiment, because no matcher is left to compare.

### 5. G4 cannot be enforced as worded: restate it instead of building a check - Severity: Major
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** Lines 91-95, 238-246
* **Relates to:** the Critic Major on native settings; Comprehension 5; the Stage 5 Major on the allowlist.
* **Discrepancy Description**: G4 promises more than native settings can enforce. Deletions and renames inside the allowlist are not covered. The Critic's option, "a CI check that fails on those statuses for PRs with no review", would need to know review state at CI time. That means re-running on review events, which is a small version of the Part 4 gate.
* **Rationale & Recommendation**: Restate the class as "every changed path is unowned". Accept deletions and renames inside the allowlist as a stated risk, and add rename, delete and symlink cases to the Q1 sandbox steps. Reject the review-aware CI check.

### 6. `risk/findings` is the shared root of three problems: drop it - Severity: Major
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** Lines 79-82 (G1), 300-305, 322-323, 341-343, 371
* **Relates to:** the Stage 5 Major on `needs: review` and its Nit on confidence; the Critic Major on G1 and its Nit on findings; Comprehension 6 and 8.
* **Discrepancy Description**: This is a new judgement. `risk/findings` is the only reason the risk job has `needs: review`, and that one dependency causes three problems:
  - no record is written when the review job fails, which breaks G1;
  - the "high-confidence" trigger is undefined, because the markers carry only a fingerprint and a rule id;
  - every signal waits for the review job, even signals that do not need it.

  The label also repeats what the inline comments on the pull request already say.
* **Rationale & Recommendation**: Drop the label and run the risk job in parallel, with no `needs:`. The three findings then close without new text. Give G1 a v1 check: a query for merged pull requests that lack `<!-- rv:risk -->`, excluding forks. The trigger has no `types:` (`review.yml.tmpl:10`), so if G1 keeps "non-draft", name `ready_for_review`. Which trigger types GitHub uses by default is unverified. Reject expanding `reviewer outcomes` to count records.

### 7. The case for cutting reach is stronger than at Stage 5 - Severity: Major
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** Lines 322-324, 348-356, 369, 716-718, 911-914
* **Relates to:** the Stage 5 Major on reach; the Critic Nit on ADR-0034 still being Proposed.
* **Discrepancy Description**: There is new evidence. The sibling design's own Risks table lists "a merge base that was never the tip of a push" as a commit that gets no index (`review-workspace.md:615`). Reach is looked up by the merge-base SHA, so by the repository's own account "reach not available" will be common.
* **Rationale & Recommendation**: Cut reach from v1 and keep it in Future Improvements. The cut also removes `actions: read`, the artifact lookup, the new dependency from the CLI on the workspace package, and most of the corrections to API cost and latency that the Critic and Comprehension asked for.

### 8. A pull request can suppress its own labels, and a missing label reads as safe - Severity: Major
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** Lines 84-87, 223-236 (CUJ 1, CUJ 2), 329-344, 374-375
* **Relates to:** the Critic Major on suppression; the Stage 5 Major on reading config from the head; the Critic Major on CUJ 2; Comprehension 14.
* **Discrepancy Description**: The risk job reads `.review/risk.yaml` from the pull request's head, so the pull request can edit the rules that label it. CUJ 1 also teaches readers that "no `risk/` label" means "a glance is enough".
* **Rationale & Recommendation**: Adopt in reduced form.
  - Read `.review/risk.yaml` from the base (Stage 5). CUJ 2's unresolved "if" then disappears, because the pull request's edits take effect only after it merges. Once CODEOWNERS is no longer parsed (finding 4), Comprehension 14 goes away too.
  - The comment names the head SHA it was computed for and links to its run, so a missing or stale comment shows that the record is missing.
  - Reword CUJ 1 so it no longer teaches that no label means "glance only".
  - The remaining way in is editing the workflow under `pull_request`. Record it as a risk, controlled by native code-owner review of `.github/`.

  Reject a "record missing" state drawn by the tool, because the tool cannot draw anything when it did not run.

### 9. Part 3 trim, re-checked against the later findings - Severity: Major
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** Lines 99-105 (G6), 408-436, 792-847
* **Relates to:** the Stage 5 Major on measurement; the Critic Majors on label usefulness and outcome heuristics; Comprehension 9.
* **Discrepancy Description**: The Critic's label-usefulness metric, precision per label against "changed after review", would keep the heaviest heuristic in Part 3 (a fetch of review comments plus a compare for every pull request) just to judge an advisory label.
* **Rationale & Recommendation**: Stage 5's trim still holds. Keep time to first approval, the candidate count and a list of merges with no review. Adopt the Critic's finding in reduced form:
  - report how often each label fires in the 20-PR dry run and the first weeks;
  - remove a default label, by a code change, when it fires on most pull requests or reviewers report it as noise.

  With the outcome labels gone, the two meanings of "label" (Comprehension 9) mostly go too.

### 10. Add a v1 Risks and Limits section - Severity: Major
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** No such section exists; it belongs between line 700 and line 704.
* **Relates to:** the Critic Blocker on the missing section. It is rated Major here because each gap that matters already has its own finding.
* **Discrepancy Description**: The document has no single place that lists the v1 risks it accepts.
* **Rationale & Recommendation**: Add a table limited to v1, with about seven rows:
  - CODEOWNERS misconfiguration;
  - docs that models and agents read;
  - published docs (Q8);
  - deletions and renames under the allowlist;
  - label suppression and workflow edits;
  - label fatigue;
  - agents turning on auto-merge everywhere.

  Point to Q1-Q3 for unverified GitHub behaviour rather than repeating it. The kill-switch window, audit retention and branch-update churn belong to the gate follow-up. The table also keeps the document short: several fixes above become one row each, not new CUJs or subsections.

### 11. Give a fresh reader the context they need - Severity: Major
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** Lines 10-40, 21-24, 740
* **Relates to:** Comprehension 1, 2 and 10; the Critic Major on ADRs.
* **Discrepancy Description**: The document assumes the reader already knows the existing CLI and the ADRs it changes.
* **Rationale & Recommendation**: Adopt, reduced to two pieces.
  - A short "Background: the existing CLI" section, following the pattern at `review-workspace.md:91-101`. It covers what `reviewer` is, its lanes, ADR-0009's exit-0 contract and ADR-0018's permissions for the review job.
  - A two-row "Changes to recorded decisions" table for v1: ADR-0035 sits beside ADR-0018 for the risk job, and ADR-0036 narrows ADR-0009 for `owners check`.

  Reject a full "Relevant ADRs" table covering all 13 cited ADRs. Move ADR-0009, ADR-0026 and the gate's reconciliation with ADR-0018 to the follow-up. ADR-0009:56-58 ("that team's own CI step ... not a flag here") is a strong argument for reopening D1 there.

### 12. Brief references, experiments and decision IDs - Severity: Nit
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** Lines 193-212, 333, 389, 455, 1085
* **Relates to:** Comprehension 3, 4 and 11; the Critic Major on brief references.
* **Discrepancy Description**: The document points to experiment numbers and decision IDs defined in briefs the reader does not have.
* **Rationale & Recommendation**: Once Part 4 goes, only experiments 13 and 15 and the Q2 and Q3 checks remain. Describe them inline, as Q1 mostly does already. Delete the IDs D-A, D-B, D-E, D5 and D6, and move Q4, Q5 and Q7 to the sketch. Say once that brief citations record where an idea came from and are not needed to follow the design. Reject the appendix of 19 experiments.

### 13. Other merge classes, "rejected by author" and the no-App premise - Severity: Nit
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** Lines 141-142, 151-153, 521-544, 571-632
* **Relates to:** the Critic Majors on Non-Goals, rejections given only as "by author", and the no-App Open Question.
* **Discrepancy Description**: The other merge classes are ruled out without reasons. Several alternatives say only "Rejected by author". The premise that there is no GitHub App is never stated as a precondition.
* **Rationale & Recommendation**:
  - For the six classes the Non-Goals rule out, add a short list with one clause each, or extend the Future Improvements bullet. Reject a full pros-and-cons entry for each class.
  - Replace "Rejected by author" with a concrete reason, but only in the alternatives that remain: A1-A4, B4, B5, D2, D5, E3 and E4. A2 and D2 need real reasons. The other weak entries (B2, C2, D1) leave with Part 4.
  - Add the no-App premise as one line in the sketch's preconditions.

### 14. Additions to reject outright - Severity: Nit
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** Whole document
* **Relates to:** Stage 6 and Stage 7 suggestions.
* **Discrepancy Description**: Several proposed additions would push the spec to the wrong level of detail or make it longer without making it sounder.
* **Rationale & Recommendation**: Do not add:
  - a CUJ for Part 3, which is a one-off measurement a maintainer runs;
  - a `.review/risk.yaml` example in the feature spec, which belongs in the PR-58 docs;
  - a separate allowlist file or a matcher-agreement experiment (finding 4);
  - a CI check that knows review state (finding 5);
  - a "record missing" state drawn by the tool (finding 8);
  - tooling for precision per label (finding 9);
  - any of the wording Nits in Part 4 ("proves", "doorbell", "now", "writer", "shadow log"), since that text is being cut.

### 15. Editorial sweep of the text that survives - Severity: Nit
* **Reviewer Lens:** `egm-zoom-out`
* **Affected Scope:** Overview, NFRs, Q3, Terms, closing sections, diagram
* **Relates to:** the Stage 5 Nits; Comprehension 15, 16 and 19; the Critic Majors on the record definition, the "two things" count, "no production risk", unsourced figures and inconsistent "unverified" markers; the Critic Nits.
* **Discrepancy Description**: Small accuracy and consistency gaps remain in text that will survive the cuts.
* **Rationale & Recommendation**: Fix these in one pass, only in text that survives the cuts:
  - Correct the count in the Overview, and qualify "no organisation-level change" for Q2.
  - Define the record as one comment plus zero or more `risk/` labels.
  - Write "little production risk" in place of "no production risk".
  - Label figures as estimates.
  - Mark every untested GitHub claim "(unverified)".
  - Correct the NFRs: latency is the risk job's own wall-clock time, rollback is "remove the risk job", and add a note on pagination.
  - Give Q3 a fallback: a record that is a comment only.
  - Make Terms and the closing sections H2 headings.
  - Add the owners-check node to the diagram.

## Verdict on the Stage 5 Recommendation to Cut Part 4

The recommendation still holds, and the later stages strengthen it. Most later findings that depend on Part 4 are contradictions inside it:
- G8 cannot pass.
- The up-to-date rule voids approvals.
- Side effects can fail a run.
- CUJ 8 contradicts the Revocation text.
- The audit is not proof.
- There is no path from v1 to the gate.

Fixing these would add text to a design that is not ready to be fixed. Cut Part 4 to a sketch of under a page, in four parts:

1. **Preconditions:** Q4 (a pinned required workflow is available), and a new question on whether organisation policy forbids a GitHub App.
2. **Invariants, stated as principles:**
   - Nothing but the pinned workflow can satisfy the rule (G7).
   - A pass is bound to the head, the base and the policy (G9).
   - The gate fails closed and has a break-glass path (G10).
   - The gate never passes where GitHub's native rule would not (G8, made one-way).
3. **Independence:** a statement that v1 does not depend on the gate.
4. **Requirements for the follow-up design, one line each:**
   - Reconcile approval of an exact SHA with up-to-date branches.
   - Consider a gate that can only block.
   - Make the kill switch an enable switch, or use the ruleset change itself.
   - Separate the decision from its side effects.
   - Define the audit's matching key and retention.
   - Reconcile ADR-0009 and ADR-0026, and reopen D1.
   - Specify the ruleset at each step from Part 2 to the gate.

**What moves, what stays and what goes:**
- G12 moves into v1 for docs-only (finding 3).
- Terms used only by the gate are cut.
- Alternatives B1-B3, C1-C2, D1, D3, D4, E1 and E2 move to the follow-up, with B1 kept as a one-line pointer.
- CUJs 5-8 go.
- These go: the PR-61 to PR-66 rows, plan entries 20-26, the reserved numbers ADR-0037 to ADR-0040, and the gate rows in the NFRs.
- The forgery argument in the Problem section (lines 58-67) stays, marked unverified, because it explains why docs-only uses native settings.

## Recommended Minimal Change Set (in order of importance)

1. **Close the allowlist hole.** Allow only directories that people alone read, and make `owners check` fail on any `guidance:` path under the allowlist. Add Q8 (published docs) and agent-read docs to the checklist. Restate G4 as "every changed path is unowned", accept deletions and renames inside the allowlist as a stated risk, and add rename, delete and symlink cases to the Q1 sandbox steps.
2. **Cut Part 4** to the sketch and requirements list above.
3. **Give docs-only a v1 failure policy:** what counts as an escape, who owns the audit sample and how often it runs, approvals back to 1 after the first escape, and a query for merges with no review to detect it.
4. **Replace the path-matching owners check** with the structural CODEOWNERS lint. Take the allowlist from the CI flags, make the check required, rewrite G5 and CUJ 4, and drop `internal/codeowners`.
5. **Simplify `reviewer risk`:**
   - drop `risk/findings`, `risk/reach` and `needs: review`;
   - read `.review/risk.yaml` from the base;
   - put the head SHA and a run link in the comment;
   - give G1 a v1 query that excludes forks.
6. **Add the missing context:** a Background section, a v1 Risks and Limits table of about seven rows, and a two-row "Changes to recorded decisions" table.
7. **Trim Part 3** to time to first approval, the candidate count and a list of merges with no review. Add how often each label fires, and a rule for removing noisy default labels.
8. **Do one editorial pass** on the surviving text (finding 15).
