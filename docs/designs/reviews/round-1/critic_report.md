# Goldfish Review Report: Stage 7, Critic Review

**Target:** `docs/designs/review-workspace.md` (FeatureRange, lines 1–689)

**Standard:** `feature-spec-standard.md` for the design and structural reviewers. The style reviewer applied its own audit vectors.

**Verdict:** none of the three reviewers passes the document as written.

## Reviewer Status

| Reviewer | Status | Findings |
| --- | --- | --- |
| `egm-design` | completed | 2 Blocker, 8 Major |
| `egm-structural` | completed | 1 Blocker, 6 Major, 3 Nit |
| `egm-style` | completed | 1 Blocker, 7 Major, 2 Nit |

The design reviewer checked claims against repository code and notes that its statements about GitHub Actions and Cloud Run behaviour are from its own knowledge. The style reviewer could not verify claims about GitHub's features or the two cited studies. The design reviewer judged the trust rule to be the right organising idea, and says most of its findings are places where the rule is not carried through.

## Prioritized Findings (Blockers -> Major -> Nits)

### D-1. The commit a diff is measured against is chosen by CI and never confirmed with GitHub - Severity: Blocker

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 189–195, 232–234, 278, 364, 537
* **Discrepancy Description**: The trust table names GitHub as the source for a pull request's head commit, title, description and state. It says nothing about the base. The base commit arrives in the CI upload, and when the service does not hold it, the same pull request's CI run uploads it. At view time the service asks GitHub only for the head. Recomputing hashes proves a pack's content matches the commit it claims to be. It does not prove that commit is in the repository's history, or that it is this pull request's merge base.
* **Rationale & Recommendation**: A pull request that edits its workflow can name any commit as its base. The workspace diff can then leave out part of the change, so the claims that CI cannot change the diff do not hold. Guidance is read from the base commit, so the pull request can also supply the guidance its own AI review is held to. Add the base to the trust table, fetch the merge base from GitHub with the viewer's credential at view time, store the base each brief and review was built from, and read guidance only from commits that arrived from a default-branch push run.

### D-2. CUJ 4 cannot happen, because the service has no source for a commit until its CI job finishes - Severity: Blocker

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 46, 48, 118–122, 225–234, 272–280, 665
* **Discrepancy Description**: CUJ 4 and line 48 say the diff and text search work while CI is still running. The only route for source into the service is `reviewer publish`, which runs after the existing checks and the index step and sends pack and index in one upload. After every push there is no diff for the full job length. Some pull requests never deliver at all: as the reviewer understands it, GitHub does not start `pull_request` workflows while a pull request has a merge conflict; a failed earlier step skips later steps; and a failed publish is only a warning.
* **Rationale & Recommendation**: The goal on line 46 depends on a complete, successful CI run, which the design never states. Either split the upload, sending the snapshot as the first step after checkout and the index later, both set to run when earlier steps fail; or add a fallback that reads the diff and files from GitHub with the viewer's credential when no snapshot is held. State the expected delay and add a visible "CI did not deliver" state.

### S-1. Rejections are attributed to "the author" with no linked record - Severity: Blocker

* **Reviewer Lens:** `egm-structural`
* **Affected Scope:** lines 445, 453–631, and the "Decided by" column at 170–181
* **Discrepancy Description**: The standard says a rejection must state a concrete trade-off and must not be attributed to a maintainer without a link to their issue, discussion or pull request. 22 of 29 entries open the Reason field with "Rejected by author", and five more rest on the author's approval of the Technical Plan. No link in scope points to a record. Ten entries have a Reason field with no trade-off at all, only the attribution and the winning option (511, 517, 525, 531, 545, 551, 565, 571, 585, 591).
* **Rationale & Recommendation**: The linked brief does not record decisions 1–5, and for identity it records a different order. Alternatives AA, AB and AC reverse choices the brief records. Either link a real record per entry, or drop the attribution and move the trade-off into the Reason field. Most Cons bullets already hold it. No link should be invented.

### Y-1. "Send nothing unscanned to a model" promises more than the gateway section delivers - Severity: Blocker

* **Reviewer Lens:** `egm-style`
* **Affected Scope:** line 54; also 266–268, 326, 328–334, 353, 363, 604, 652
* **Discrepancy Description**: This is the document's main data-protection guarantee and its own text contradicts it. "Every model call passes one outbound point" is untrue where the CLI's model lane stays on in CI, which line 268 allows. "Scans the payload" describes a lookup of stored results for file excerpts only. Typed text gets a six-prefix pattern list whose own comment says it cannot be complete and which does not match `github_pat_`, the token form users are asked to paste. Line 334 does not say what happens on a match. The pull request title, description and intent record are sent to the model with no check named.
* **Rationale & Recommendation**: The goal's check, a count of model calls with no recorded scan decision, can be zero in every case above. Scope the goal to calls the service makes. State, for each kind of payload part, which check applies and what a match does, and make the check measure that.

### D-3. The index is built on a different set of files from the snapshot it is served against - Severity: Major

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 47, 122, 193, 227, 363, 503; `internal/installer/templates/review.yml.tmpl` lines 20–24
* **Discrepancy Description**: The generated workflow checks out with no `ref`. On a `pull_request` run that is GitHub's temporary merge of the pull request into its base branch, not the head commit. The index therefore describes the merge result while the snapshot shown is the head commit. Whenever the base branch has moved, positions shift. The name check catches part of this for definitions only; references and the dependents list fed to the brief have no such check. Separately, the snapshot is the commit's git tree, so dependency type declarations under `node_modules` and types generated at install time are outside it, and go-to-definition on them cannot show a target.
* **Rationale & Recommendation**: This breaks the navigation rule on the normal path, with no attacker, and the service cannot detect it. State which commit CI indexes and check out the head commit for the index step. Have the upload carry the commit the index was built from so the service can refuse a mismatch. Say what a reviewer sees for a symbol defined outside the tree.

### D-4. An upload is tied to a repository, not to the pull request or branch it claims to be for - Severity: Major

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 93, 193, 225, 236, 274
* **Discrepancy Description**: Ingest accepts a token that GitHub signed, that is issued for this service, and that names an accepted repository. Nothing ties the pull request number, the commit or the kind of upload to the run that sent it. Any workflow run in the repository can upload an index for another pull request's head commit or for a default-branch commit, send another pull request's title and description, or send "closed" for another pull request, which schedules its data for removal. The document also does not say what happens when a second upload arrives for the same commit.
* **Rationale & Recommendation**: This contradicts the claim that a false index misleads only its own pull request's readers. Use the token's own claims: take the pull request number from its `ref`, require a push event on the default branch for default-branch uploads, and use the numeric repository identifier. Record which run uploaded each index, and define the repeated-upload rule.

### D-5. The "send nothing unscanned" guarantee has four gaps - Severity: Major

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 54, 136–140, 276, 298, 328–334, 353, 363, 402
* **Discrepancy Description**: Typed text is checked by one pattern with six prefixes that was written to scrub provider error messages. Descriptions and intent records are sent to the model with no named check. Scan results are stored with the scanner version and no rescan job exists, so after a rule update either stale results are accepted or every unchanged file counts as unscanned. A hit in any changed file switches AI off for the whole commit with no way to clear a false positive.
* **Rationale & Recommendation**: Run the real scanner over typed text, descriptions and intent records. Add a rescan job and say which scanner versions the gateway accepts. Say where allow rules live (server side or the base commit, never the pull request head) and who may add one.

### D-6. `id-token: write` is granted to the job that runs pull request code, and treated as free - Severity: Major

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 223, 227, 231, 243, 434; ADR-0018
* **Discrepancy Description**: Once a job has this permission, every process in it can ask GitHub for a signed token naming any audience. The review job runs the pull request's install scripts and lint configuration. Line 243 argues a redirect "gains nothing" because the author can already read the code. That covers a human author. The stated threat is a poisoned dependency or an injected instruction, and for that actor the permission is a new capability: a GitHub-signed identity for this repository, usable against the workspace and against anything else that trusts these repositories' tokens.
* **Rationale & Recommendation**: "No secret is added" is true and is not the whole risk. ADR-0018 fixes the job's permissions, so changing them is a supersession and not an extension. Put publish in its own job that holds the permission and runs only the pinned, checksum-verified binary, taking the index as an artifact from the job that installs. Take the service address from the workflow or a repository variable. Check what else trusts these repositories' tokens before rollout.

### D-7. The prompt-injection analysis covers only the private assistant answer - Severity: Major

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 104, 195, 357, 361–366, 653
* **Discrepancy Description**: Line 357 concludes the worst case is "a wrong answer for the user who asked". Two cases fall outside that. The brief and AI review are generated once per commit from author-controlled text and shown first to every reviewer, so text in a pull request can steer what everyone reads. And the browser can reach the network: a model answer containing an image or link whose address embeds text the model read sends that text to another host when drawn.
* **Rationale & Recommendation**: State both cases. Block remote resource loading in rendered model output, including diagrams. In the brief, visibly separate "the author says" from "the code shows". State that an empty AI review is not evidence of a clean change.

### D-8. Model spend is triggered by every push with no estimate, and competes with the interactive features under test - Severity: Major

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 236, 277, 338, 361–366, 398–402, 660
* **Discrepancy Description**: Every upload queues a brief and a review, and every description edit regenerates the brief. Agents push in bursts. Nothing drops queued work for a commit that is no longer the head. Background jobs and interactive questions share one budget and one rate limit. The document contains no token or cost figure.
* **Rationale & Recommendation**: A burst of pushes can leave reviewers facing a quota message on the features the first version exists to test. Give a weekly token estimate with its assumptions, skip jobs for superseded commits, consider generating on first open, and reserve a share for interactive use.

### D-9. The Alternatives Rule is not met as written - Severity: Major

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 445–639
* **Discrepancy Description**: Ten alternatives give "Rejected by author at decision N" as the entire reason. Several entries reverse the concepts brief with no pointer to where that was decided. B, D and AC state a preference or restate the outcome. Alternative P has strong advantages and one weak drawback, while the chosen tool carries two open viability questions. Z is "not rejected, and not evaluated", although it is the main alternative to building a viewer.
* **Rationale & Recommendation**: Put the technical reason in each rejection line and link the decision record. Evaluate Z, even briefly. Give AC a reason, or run the trial.

### D-10. Scalability limits and several failure modes are not stated - Severity: Major

* **Reviewer Lens:** `egm-design`
* **Affected Scope:** lines 44–59, 87–96, 276, 284–289, 326, 402, 643–668
* **Discrepancy Description**: No limit carries a number. Ingest does all its work inside one upload request with no size or time bound, and a request that dies midway leaves a part-processed commit with no job to resume it. Each commit stores one path row and one index row per file, and default-branch and merge-base commits have no retention rule. The trigram index spans every stored version of every file, so search cost grows with history. One process accepts public uploads and can also decrypt every user's token and read provider keys.
* **Rationale & Recommendation**: Add a Risks and Limits section with measured or estimated sizes, the platform limits they must fit, a retention rule for every record kind, a resume path for failed ingest, and a fallback for each open question that could invalidate a decision.

### S-2. There is no Risks & Mitigations section - Severity: Major

* **Reviewer Lens:** `egm-structural`
* **Affected Scope:** whole scope
* **Discrepancy Description**: The standard requires failure modes, security implications, scalability limits and their mitigations to be called out. Risk content exists but is spread over at least eight places. Nothing in lines 1–689 states a scalability or capacity limit, and no passage names complexity or operating burden as a risk.
* **Rationale & Recommendation**: Gather the risk content into one section, with values for the size limits and a bound on the work done inside the upload request.

### S-3. The decision table and the alternatives do not line up - Severity: Major

* **Reviewer Lens:** `egm-structural`
* **Affected Scope:** lines 170–183, 445, 505–611
* **Discrepancy Description**: The table numbers ten decisions, but line 445 speaks of "the five numbered decisions". Rows 6 (Go, second binary) and 7 (web stack) have no alternative entry. Line 183 promises reasons "where each appears below", and row 7 gets one sentence covering CodeMirror only. Alternatives Y, N and W are filed under headings for other decisions.
* **Rationale & Recommendation**: Give every table row its alternatives under a heading that carries its number, or say why a row has none.

### S-4. Alternative Z sits in "Alternatives Considered" but was not considered - Severity: Major

* **Reviewer Lens:** `egm-structural`
* **Affected Scope:** lines 633–639
* **Discrepancy Description**: Its Reason field reads "Not rejected, and not evaluated", and its pros and cons are "from memory". The brief asked the design to weigh this option. It is not carried in Open Questions, so the unresolved item has no tracked home. It is also out of letter order.
* **Rationale & Recommendation**: Evaluate it, or move it to Open Questions.

### S-5. The component diagram disagrees with the text - Severity: Major

* **Reviewer Lens:** `egm-structural`
* **Affected Scope:** lines 201–219 against 249–262, 276–277, 402
* **Discrepancy Description**: The diagram assigns "scan, index, brief, review" to `jobs`. The flow says scan and index run inside the upload request, and only brief and review are jobs. The diagram omits the `scan` and `brief` parts from the component table and adds "notices", which has no table row. As drawn, the bucket has its own connector but Postgres does not.
* **Rationale & Recommendation**: Redraw the diagram from the component table.

### S-6. Later work is classified four ways that disagree - Severity: Major

* **Reviewer Lens:** `egm-structural`
* **Affected Scope:** lines 20, 67–85, 419–424, 680–686
* **Discrepancy Description**: The Overview says three later stages are "named under Non-Goals". Non-Goals lists six unnumbered bullets, and stage numbers appear only under Future Improvements. The branch view belongs to no stage. "Not planned" contains navigation for other languages, which is also a Future Improvement, and a shared service, which is also listed as a later-stage constraint. The unread-merges stage has no entry under the constraints.
* **Rationale & Recommendation**: Use one list of later stages and refer to it from the other three places.

### S-7. The Goals section breaks its own rule - Severity: Major

* **Reviewer Lens:** `egm-structural`
* **Affected Scope:** lines 44, 46, 56–59, 657
* **Discrepancy Description**: Line 44 says each goal names how it will be checked. Four of 14 goals have no "Checked by": phone, hotkeys, telemetry and existing installs. Line 44 also sends unmeasured thresholds to Open Questions, but the "stated delay" and the one-second lookup target are not listed there.
* **Rationale & Recommendation**: Add the four checks and the two open questions.

### Y-2. Navigation claims treat an unverifiable index as verified - Severity: Major

* **Reviewer Lens:** `egm-style`
* **Affected Scope:** lines 47, 114–116, 193, 195; compare 274, 350, 363
* **Discrepancy Description**: Line 47 promises navigation "without wrong answers" and a definition shown "only when the compiler resolved it". Line 193 says the index "cannot be verified". What the service can check is that the path exists, the position is in the file and the name matches, and two symbols with the same name pass that check. "Zero confirmed wrong definitions" counts user reports, not wrong answers.
* **Rationale & Recommendation**: State the goal as "every result is labelled with how it was obtained". Say that a compiler-labelled result is as correct as the CI-built index, and add the index to the brief's listed inputs.

### Y-3. Absolute claims about GitHub carry no evidence - Severity: Major

* **Reviewer Lens:** `egm-style`
* **Affected Scope:** lines 11, 30, 32, 38, 453
* **Discrepancy Description**: The document says GitHub "does not have" code navigation or AI help, that there is "no way to ask", and that the workspace adds "what GitHub cannot do". The concepts brief records these as problems the requester reported, not as checked facts. The reviewer believes GitHub sells Copilot features that answer questions on pull request pages, but could not verify this, and the document does not say whether the team has them.
* **Rationale & Recommendation**: Say what was checked, on which GitHub plan and when, or attribute the statements to the requester.

### Y-4. The prompt-injection bound is stated as a maximum the document does not support - Severity: Major

* **Reviewer Lens:** `egm-style`
* **Affected Scope:** line 357
* **Discrepancy Description**: "The most such text can do is produce a wrong answer for the user who asked." The document itself gives injected text three other effects: it can spend tool rounds against the shared budget, shape Markdown and Mermaid drawn in the asker's browser, and reach the brief and AI review that every reader sees.
* **Rationale & Recommendation**: Keep the first clause about what the tools cannot do, and list the remaining effects.

### Y-5. "Keep pull request code off the server" is false as written - Severity: Major

* **Reviewer Lens:** `egm-style`
* **Affected Scope:** lines 53, 503; compare 286–287, 298, 647
* **Discrepancy Description**: The service stores every file's text in Postgres. What it avoids is executing the code. Line 503 adds "entirely", which makes the statement wrong. The goal's check is satisfied trivially because the service never clones, and it names no test.
* **Rationale & Recommendation**: Use "never executes pull request code" in both places and name a test.

### Y-6. "Each goal names how it will be checked" is not true of the goals - Severity: Major

* **Reviewer Lens:** `egm-style`
* **Affected Scope:** lines 44–59
* **Discrepancy Description**: Four goals have no check, and "a stated delay" is stated nowhere. Tap-to-navigate is conditional on "if the touch design holds" with no criterion, owner or date. Two checks do not match the mechanism: line 49 says answers link to code they "relied on", while line 355 records what the model "was given"; and line 50 says every view shows a brief or lists missing inputs, while three other states exist (preparing, AI off after a secret, waiting on quota).
* **Rationale & Recommendation**: Add the missing checks or correct line 44. Move the tap-to-navigate condition to Open Questions with a named test.

### Y-7. Many "Reason for Rejection" fields give no reason, and caveats on the chosen options sit elsewhere - Severity: Major

* **Reviewer Lens:** `egm-style`
* **Affected Scope:** lines 445–631
* **Discrepancy Description**: Ten entries say only "Rejected by author at decision N, in favour of" the chosen option. Three caveats sit away from the choice they bear on: `scip-typescript`'s unverified upkeep and `bun` support belong at decision 3, the unapproved credential routes at decision 1, and the untested premise behind the brief and AI review beside goals 50–51. Several pros and cons are unquantified ("the largest saving", "fewest parts and lowest cost", "the most code", "almost no cost"). Line 476 drops the statistic's condition of zero observed misses, and lines 502–503 state the generated-types effect as fact when the brief calls it untested.
* **Rationale & Recommendation**: State the deciding drawback in each Reason field, or rename the field and say it once. Move the three caveats to where the reader weighs the choice.

### Y-8. Access, timing and rollback statements are firmer than their mechanisms - Severity: Major

* **Reviewer Lens:** `egm-style`
* **Affected Scope:** lines 20, 52, 254, 276–277, 322, 342, 402, 421, 651, 659–660, 666
* **Discrepancy Description**: Access is stated as absolute ("on every read") but the answer is cached "for a few minutes". Line 276 says everything is ready when the CI step returns, as fact, though no ingest time has been measured. "Within a minute" holds only with no backlog. The streaming requirement states nothing a stream does not already do. Two limits are missing from Open Questions: the closed pull request retention period and the assistant's round and token limit. "None of them forces a rewrite" predicts stages not yet designed. Rollback omits three things the design adds to a repository: the `id-token: write` permission, the agent-skill instructions and the config block.
* **Rationale & Recommendation**: Give one cache figure in the goal, add ingest time to the open question on build time, and list everything rollback must remove.

### S-8. Goals and journeys are not symmetric - Severity: Nit

* **Reviewer Lens:** `egm-structural`
* **Affected Scope:** lines 42–59, 98–164
* **Discrepancy Description**: Four goals have no journey: AI review, hotkeys, telemetry and existing installs. Two journeys have no goal: CUJ 9 appears only inside a Non-Goal, and CUJ 11 is backed only by the skill change.
* **Rationale & Recommendation**: Add a journey or a goal for each, or say why none is needed.

### S-9. Heading nesting and pseudo-headings - Severity: Nit

* **Reviewer Lens:** `egm-structural`
* **Affected Scope:** lines 63, 67, 76, 641, 670, 672, 680
* **Discrepancy Description**: "Additional Resources" and "Future Improvements" are third-level headings, so they nest under "Non-Functional Requirements". Non-Goals uses bold lines as sub-headings where every other section uses headings. "The Problem" also carries a "proposed solution" paragraph.
* **Rationale & Recommendation**: Promote the two sections and use real sub-headings.

### S-10. Link and reference gaps - Severity: Nit

* **Reviewer Lens:** `egm-structural`
* **Affected Scope:** lines 28, 34, 330–441, 631, 665, 677–678
* **Discrepancy Description**: Two citations have no reference entry. Thirteen ADR mentions are plain text. The external sources are unlinked, though three claims rest on them. "PR-57" is used before the plan defines it.
* **Rationale & Recommendation**: Add a reference list and link the ADRs.

### Y-9. Problem-section claims outrun their support - Severity: Nit

* **Reviewer Lens:** `egm-style`
* **Affected Scope:** lines 26, 28, 34, 36
* **Discrepancy Description**: "Slow and shallow" is an unmeasured judgement. Line 28 turns one 2013 study into "research finds", and its "ahead of finding defects" does not match the brief's summary of the same paper. "Makes all three worse" is argued only for the first problem, and the supporting study covers open-source projects. Line 36 says the model lane "reads only the diff", but it also reads guidance files named in config.
* **Rationale & Recommendation**: Calibrate each claim to its evidence.

### Y-10. Terminology drift and unresolved references - Severity: Nit

* **Reviewer Lens:** `egm-style`
* **Affected Scope:** lines 20, 22, 53, 54, 70, 183, 197, 213, 258, 266, 311, 326, 428, 445, 477
* **Discrepancy Description**: One component has three names: "outbound point", "gateway" and "door". "Server" and "service" are used for the same thing. "Seven-lens review" and "shadow mode" are not defined. Line 311 quotes "a local-files form", a phrase found nowhere else. Line 445 says "five numbered decisions" against a ten-row table, and line 20 says "three later stages" against six bullets.
* **Rationale & Recommendation**: Pick one name for each thing and define the two undefined terms.

## Checked and not raised

* **Cross-repository file sharing.** Lines 286 and 298 say files are keyed by hash alone. The implementation plan keys them by repository and hash, which closes the access concern. The feature sections should match it.
* **Token counts for brief and review jobs.** Line 336 reads as if the one-call interface returns no usage. The implementation plan adds usage and a distinct quota error, so this is a wording issue only.
* **Calibrated claims.** The style reviewer checked lines 90, 187, 330, 366, 637–639, 657, 658 and 665 and found each hedged to its evidence or matching the ADR it cites.
* **Structure that conforms.** No heading level is skipped, all eight tables are well formed, all eleven journeys use Given/When/Then, and the four relative links resolve.
