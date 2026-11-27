<!-- SPDX-License-Identifier: MIT -->
# Comprehension report: risk-gated merge

**Target:** `docs/designs/risk-gated-merge.md` (FeatureRange 1-757, DetailedRange 758-1093)
**Reviewer:** `egm-comprehension` (Stage 6). Read the document alone; no other repository files; no
document standard.

## What the feature spec says (paraphrase)

**How the existing system works, according to this document.** Coding agents write more than 90%
of the team's pull requests, a human reviews each before merge, and `main` deploys straight to
production; recovery is a revert and redeploy. `reviewer` is never defined in one sentence, but the
diagram and later text show it is an automated code-review tool that runs as a "review" job in a
GitHub Actions workflow on each pull request, posts inline comments and a summary carrying hidden
`rv:` markers, and always exits 0 so it never blocks (ADR-0009). Its permissions are set by
ADR-0018. Each repository configures it in `.review/config.yaml`, read by a strict decoder that
rejects unknown keys. It has other subcommands (`metrics`, `baseline`, `rules test`, `init`, which
installs the workflow), ships as a composite action that installs a checksummed binary, and a
separate `review-index.yml` workflow uploads a TypeScript navigation index artifact on every push.
Its GitHub write client cannot merge, approve or push.

**What this proposes.** v1 needs no organisation-level change and has three independent parts:

1. `reviewer risk`, an advisory command run as its own job after review. It posts one sticky
   comment and labels from a fixed `risk/` set (auth, data, ci, deps, infra, wide, reach, history,
   findings, review-config), computed from changed paths, a built-in risk map extended by
   `.review/risk.yaml`, CODEOWNERS groups, recent reverts, TypeScript reach and review findings.
   Labels only point to reasons to look; none says "safe". It always exits 0, and because it reads
   inputs the pull request controls, it flags pull requests that change that configuration.
2. A docs-only merge class from GitHub settings alone: CODEOWNERS owns everything except a
   documentation allowlist, and the ruleset requires code-owner review with 0 approvals, so pull
   requests touching only allowlisted directories merge on green CI. A new CI check, `reviewer
   owners check`, fails when a path has no owner and is not allowlisted, and a setup guide is
   added. It ships only if sandbox tests (Q1, Q2) confirm GitHub behaves as assumed.
3. `reviewer outcomes`, read-only, for a baseline: time to first approval, how many past pull
   requests would have been docs-only, outcome labels (reverted, hotfixed, clean) and first-review
   labels.

The later stages need an org owner to confirm a pinned required workflow. They add a separate
fail-closed program, `merge-gate`, run as that required workflow, which passes when (a) a
non-author human with write access approved the exact head SHA, or (b) the pull request fits an
enabled class in `.review/merge-policy.yaml` read from the base branch, with no compiled-in
never-qualify rule applying. Native auto-merge merges. A pass is bound to head, base and policy
SHAs. There is a kill switch (repository variable) with a sweep, break glass via a ruleset bypass,
an informational commit status, decision records, a scheduled post-merge audit, and a four-step
rollout that never weakens review. Alternatives explain rejecting risk scores, learned models,
Apps, machine users, merging from code, `pull_request_target`-style producers and a merge queue.

## What the implementation plan says (paraphrase)

First a person runs a one-off `gh pr list` query to see whether docs-only pull requests are a
meaningful share; if not, Part 2 can be dropped. Then twelve stacked pull requests continue the
existing plan's numbering.

**v1 (PR-55 to PR-60):** PR-55 `outcomes` with new API reads and heuristic tests; PR-56
`AddLabels`/`RemoveLabel` and ADR-0035, waiting on Q3; PR-57 a CODEOWNERS parser and the pure
`internal/risk` library with a test banning "safe/low/ok" labels; PR-58 the `reviewer risk`
command, its reporter, a `mode` action input and a commented-out `risk` job in the installer
template; PR-59 `reviewer owners check` (exits 1), ADR-0036 and `docs/merge-classes.md`; PR-60 a
trials file with the baseline, a 30-item hand audit and sandbox results.

**Later (PR-61 to PR-66, only if Q4 is yes):** PR-61 gate-only REST reads and a one-method
`StatusWriter`; PR-62 policy decoder and predicates; PR-63 `merge-gate` (exit 0 pass, 2 waiting,
1 error), the human-rule truth table, import allowlist, decision records, and ADR-0037 to ADR-0040
superseding ADR-0026, ADR-0009 and ADR-0018; PR-64 gate action, release artifact and workflow
template; PR-65 audit and sweep; PR-66 runbooks.

Verification: build, unit tests (exit-policy walks, CODEOWNERS table, label test, truth table),
`make check`, coverage rules, sandbox experiments, a dry-run of `reviewer risk` on 20 real pull
requests, and a 4-week comparison of time to approval against the baseline.

## Findings

No Blockers: every section could be paraphrased.

### 1. `reviewer` is never defined - Severity: Major
* **Affected Scope:** lines 17-38; diagram 295-316
* **Description:** The system being changed is never described in one sentence; a newcomer pieces
  it together from the diagram, ADR-0009 references and Part 1.
* **Recommendation:** Add two or three sentences near the top on what `reviewer` does today, how it
  runs, and its "never block" contract.

### 2. ADRs cited by number only - Severity: Major
* **Affected Scope:** lines 23, 24, 140-143, 155, 177, 533, 593, 614, 630, 688, 740, 1034-1043
* **Description:** Load-bearing ADRs are never summarised (ADR-0009 at line 23; the shape ADR-0009
  "describes" at line 630; ADR-0022/0024 for "model-lane category"; ADR-0026 only in the plan;
  ADR-0004 and ADR-0023 listed but never cited). The feature spec mentions only the ADR-0018
  supersession (line 465); ADR-0009 and ADR-0026 supersessions appear only at lines 1034-1043.
* **Recommendation:** Add a "Relevant ADRs" table (number, one-line decision, effect of this
  design), and list every ADR the later stages supersede in Part 4.

### 3. Sandbox experiments cited by number only - Severity: Major
* **Affected Scope:** lines 110, 196, 204, 449, 1081, 1085
* **Description:** Experiments 4-12, 14 and 16-19 are never described, yet line 1085 makes
  experiments 1 to 19 a precondition for requiring the gate.
* **Recommendation:** Add an appendix with one line per experiment: what it tests, the expected
  result, and which question or goal it unblocks.

### 4. Undefined and colliding decision identifiers - Severity: Major
* **Affected Scope:** lines 204-212 vs 519-700; line 1022
* **Description:** Q4-Q7 cite D-A, D-B, D-E, D5, D6, defined nowhere, in two naming schemes. "D5"
  is also an Alternative's label (line 659). Line 1022 "implements the D-E rule", which exists only
  as a starting point at line 475.
* **Recommendation:** Define each decision ID inline or drop the IDs, and give Alternatives a
  distinct prefix.

### 5. Whether deletions count as docs-only - Severity: Major
* **Affected Scope:** lines 91-95 (G4), 385-393 (Part 2), 801-802 (PR-55)
* **Description:** G4 and the measurement count only added or modified files, but unowned
  CODEOWNERS paths cannot tell a deletion from an edit, so v1 would let deletions merge unreviewed
  and the measured count would be stricter than what GitHub allows.
* **Recommendation:** State what native settings let through (including deletions and renames
  inside the allowlist), accept or prevent it in G4, and make the measurement predicate match.

### 6. G1's check depends on a later-stage tool - Severity: Major
* **Affected Scope:** lines 79-82, 503-507, 787
* **Description:** The only post-merge audit described is `merge-gate audit` (PR-65). No v1 audit
  is specified.
* **Recommendation:** Define a v1 check (for example a query for merged pull requests lacking
  `<!-- rv:risk -->`) and which PR delivers it, or reword G1.

### 7. No path from the v1 docs-only setup to the gate setup - Severity: Major
* **Affected Scope:** lines 383-406, 509-517; CUJs 3, 5, 6
* **Description:** It is not said whether code-owner review stays on under the gate, whether
  rollout step 2 means going back from 0 to 1 required approval, or where the allowlist lives once
  both CODEOWNERS and `merge-policy.yaml` express it.
* **Recommendation:** Add a "From v1 to the gate" paragraph with the ruleset state at each rollout
  step, starting from Part 2's configuration.

### 8. `risk/findings` trigger undefined - Severity: Major
* **Affected Scope:** line 371 vs line 341
* **Description:** It fires on "a high-confidence finding", but markers are counted by rule id and
  never said to carry confidence.
* **Recommendation:** Define "high-confidence": a marker field, a rule list or a threshold.

### 9. "Label" has two meanings; "review workspace" undefined - Severity: Major
* **Affected Scope:** lines 155-156, 429-431, 690-692; 741
* **Description:** GitHub labels and data labels (outcome, first-review) share a word, making the
  non-goal on labelling in the workspace and E3 ambiguous.
* **Recommendation:** Use "GitHub label" and "outcome label" consistently; define the review
  workspace in one sentence or link its design.

### 10. Project terms undefined - Severity: Nit
* **Affected Scope:** lines 61, 138, 177, 443, 553
* **Description:** "Model lane", "model-lane category", "review lanes", "plugin hosting".
* **Recommendation:** Add a Terms entry for "lane" or describe the model lane in one line.

### 11. Brief citations and "the requester" - Severity: Nit
* **Affected Scope:** lines 53, 193, 333, 389, 405, 455, 483, 509, 737-738
* **Description:** "brief, Blocker N" points to an external document; "the requester" is not
  identified.
* **Recommendation:** Keep the inline restatements and say once that the brief is provenance only.

### 12. "Lens" unexplained - Severity: Nit
* **Affected Scope:** lines 521-523, 587, 597, 616, 624, 664, 682, 690
* **Recommendation:** Say once that lenses are reviewer perspectives from earlier review rounds, or
  drop the names.

### 13. "Shadow log" and "agreed period" undefined - Severity: Nit
* **Affected Scope:** line 115 (G8)
* **Recommendation:** Name where the log lives and who sets the period.

### 14. Root or `docs/` CODEOWNERS not flagged - Severity: Nit
* **Affected Scope:** lines 84-86, 229-236, 344, 372
* **Description:** A pull request editing a CODEOWNERS file outside `.github/` can change its own
  `risk/wide` result without `risk/review-config`.
* **Recommendation:** Include every CODEOWNERS location in `risk/review-config`, or accept the gap.

### 15. Stale NFR details - Severity: Nit
* **Affected Scope:** lines 716-724
* **Description:** Counts an API call for a local CODEOWNERS read; omits the merge-base lookup;
  says "adds under 30 seconds to the review job" though risk is a separate job; rollback says
  "turn the action input off" though `mode` is not an on/off switch.
* **Recommendation:** Fix these lines.

### 16. Overview count - Severity: Nit
* **Affected Scope:** line 17 vs 19-37
* **Description:** "Adds two things... in two phases" precedes three v1 items plus later stages.
* **Recommendation:** Reword.

### 17. Gate ruleset configuration scattered - Severity: Nit
* **Affected Scope:** lines 496-498, 277-279
* **Description:** "Require branches to be up to date" and a separate gate ruleset are relied on
  but not listed in Part 4's ruleset description or rollout steps.
* **Recommendation:** List the full gate-stage ruleset configuration in one place.

### 18. PR number used before numbering is introduced - Severity: Nit
* **Affected Scope:** line 465
* **Recommendation:** Refer to "the Detailed Implementation Plan, PR-63" or drop it.

### 19. Stack table ordering and placeholders - Severity: Nit
* **Affected Scope:** lines 775-788
* **Description:** PR-56 waits on Q3 while PR-60 records Q3 after PR-55; "none" and "—" are mixed;
  PR-59 shows no gate though Part 2 depends on Q1 and Q2.
* **Recommendation:** Say sandbox runs happen before PR-56 and PR-60 only records them; use one
  placeholder; note PR-59 can merge before Q1 but must not be used until Q1 and Q2 pass.

### 20. `.review/risk.yaml` format not shown - Severity: Nit
* **Affected Scope:** lines 233-235, 888-890
* **Recommendation:** Add a short example of the file and of a rejected entry.
