# Goldfish Review Report: Stage 6, Comprehension Test (round 2)

**Target:** `docs/designs/review-workspace.md` (FeatureRange lines 1–572, DetailedRange lines 573–954)

**Standard:** none. The test is whether a reader who knows software but not this project can understand the proposal from this document alone. The reviewer opened no other repository file and used no web lookups.

## Reviewer Status

| Reviewer | Status | Findings |
| --- | --- | --- |
| `egm-comprehension` | completed | 0 Blocker, 7 Major, 5 Nit |

**Verdict:** no Blockers. No section was impossible to paraphrase.

Compare the two paraphrases below with what you intended. A wrong paraphrase is itself a finding.

## What this proposes

A hosted web application, the "review workspace", that a 10-engineer team uses next to GitHub's pull request page; commenting, approving and merging stay on GitHub. It adds four things:

- code navigation and text search at the pull request's commit;
- an AI assistant that answers questions by calling six read-only tools;
- a "change brief" that separates what the author says from what the code shows;
- an AI review, made of notes on the plan plus findings on changed lines.

It has two parts. A per-repository CI job runs `scip-typescript` and saves a navigation index as a workflow artifact. A Go service on Cloud Run with Postgres fetches source from GitHub using each reviewer's own fine-grained token, loads the index, and sends all model calls through one gateway that scans for secrets at the moment of sending. The service never runs pull request code and never writes to GitHub. The work is ten implementation pull requests (PR-47 to PR-56), gated by four entry conditions and three one-week team trials.

## How the existing system works, per this document

The repository holds `reviewer`, a stateless command-line tool. `reviewer init` writes a GitHub Actions workflow, a config file and an agent skill file into a repository; the workflow runs the tool on every pull request and posts comments, which are its only store. It has two lanes:

- a deterministic lane (gitleaks secrets scanner, type checks, lint, convention rules);
- a model lane, off by default, that sends the diff and named guidance documents to a language model and reports only on changed lines.

The model lane does not run if the scanner finds a credential or cannot run. Each model finding is re-challenged before it is shown, and some categories are capped at medium confidence. A separate Node plugin runs the TypeScript analyzers and answers no navigation queries. One tag releases the binary and the plugin's npm package. Repository rules: no address, organisation or model name in Go source; nothing internal in history; exit code always 0.

## Prioritized Findings (Blockers -> Major -> Nits)

### C-1. The feedback points depend on telemetry that arrives after them - Severity: Major

* **Affected Scope:** lines 594, 579–590, 899–905; also 57, 109
* **Discrepancy Description**: The three trial weeks follow PR-50, PR-51 and PR-53 and decide whether work continues. Their measures are usage counts. Everything that records and computes those counts arrives in PR-55: the browser reporting, the `events` route, the `usage_event` table and the code that computes the measures. Only the "this was wrong" report and model call records exist earlier.
* **Rationale**: The document does not say how the numbers are obtained during the trial weeks, or where the two-question poll is run and stored. Goal 2's check, "the share of lookups in each class", has the same dependency.

### C-2. The secrets rule does not cover every kind of content - Severity: Major

* **Affected Scope:** lines 157, 332–341, 524, 656–676, 868
* **Discrepancy Description**: The rule gives two outcomes: a file or tool result is withheld, and the diff or the user's typed text stops the call. Four things are left open.
  * **Description and intent record.** They are scanned, but no step says what happens when the secret is there, and the test list skips it.
  * **How the gateway recognises the diff.** The scan list does not mention the diff, and the part types have no diff kind.
  * **"In the diff" versus "in a changed file".** CUJ 7 says a secret "in the diff" stops the brief. The risks table says a credential "in a changed file" does. Excerpts are scanned as whole files, so these are different cases.
  * **The `AsProvider` path.** The whole review prompt goes as one text part. Nothing in it can be withheld item by item, so "one rule for every file" seems not to hold there.
* **Rationale**: A reader cannot say what the reviewer sees in each case.

### C-3. How stages run and change state is not explained - Severity: Major

* **Affected Scope:** lines 139, 248, 257, 278–293, 359, 384, 709, 718, 890
* **Discrepancy Description**: The service is stateless and "has no background work of its own", yet fetch, index load, brief and review are stages with running states that a second reviewer "does not start again".
* **Rationale**: The document does not say where a stage's work runs, what happens to a stage stuck in "fetching" or "generating" if that request ends or the container is recycled, what later moves an index from "waiting for CI" to "ready", or how the service tells "waiting for CI" from "not produced" or "failed".

### C-4. The retention rule cannot be stated precisely - Severity: Major

* **Affected Scope:** lines 384–386, 525, 682–698, 911
* **Discrepancy Description**: Line 384 removes commits that no open pull request refers to and that nobody viewed within the period. Line 386 says a closed, never-revisited pull request "is removed by the age rule", which reads as age alone being enough. If the service never learns of the close, the commit is still referenced by a pull request it believes is open.
* **Rationale**: The fixed table list has no table for pull requests, their open or closed state, or a last-viewed time. The sweep job has no viewer credential to ask GitHub. The reader cannot see what the rule is evaluated against.

### C-5. "Empty environment" contradicts how the token reaches `git` - Severity: Major

* **Affected Scope:** lines 63, 304, 523, 534, 810, 936
* **Discrepancy Description**: Goal 8 and its test say `git` and the scanner each start "with an empty environment". Lines 304 and 810 say the viewer's token "is passed to it through the environment".
* **Rationale**: The likely meaning is "nothing inherited from the service, plus the one token variable", but as written the goal's check is false for every fetch.

### C-6. The index bounds are described only for definitions - Severity: Major

* **Affected Scope:** lines 57, 210, 308–318, 353, 361–363, 521, 886
* **Discrepancy Description**: The name check and the stated residual risk apply to go-to-definition only. No check is stated for references from the index, so a false index could add or omit them. References feed find-references, two of the assistant's tools, and the brief's "what unchanged code depends on it".
* **Rationale**: Line 318 says a false index "cannot change the diff, the source shown or search results" but does not mention the brief or the assistant's answers, so a reader would conclude its reach is narrower than it is. Goal 2 promises a label on "every navigation result" but defines the classes only for definitions.

### C-7. The second agent-skill instruction connects to nothing else - Severity: Major

* **Affected Scope:** lines 250–253, 549, 891; compare 82, 439
* **Discrepancy Description**: The skill file gains an instruction for the agent to read open review threads, make changes, push and reply. No problem statement, goal, journey or verification row covers it, and the related "send to agent" action is a non-goal for this version.
* **Rationale**: "Nothing else changes there" understates it: agents in every repository would start pushing and replying on GitHub. The Background also never says what the skill file does today or which agents read it.

### C-8. "Connected repository" is never defined - Severity: Nit

* **Affected Scope:** lines 56, 448, 459, 526, 548, 552; configuration at 618–625, 777, 915
* **Discrepancy Description**: Goal 1 and several other passages depend on the term.
* **Rationale**: The reader has to infer from the configuration sample that it means "listed by the operator in the service config with a provider and key". The steps to connect one are deferred to a guide written in the last pull request.

### C-9. The Background omits several existing-system items the plan relies on - Severity: Nit

* **Affected Scope:** lines 69, 239, 269, 368, 419, 577, 668, 674, 782, 787, 887
* **Discrepancy Description**: `internal/laneb` is never tied to "the model lane" or "the CLI's review code". "The published Action" is absent from the Background. ADR-0021 is "built on" with no hint of what it decides. `model.Provider` is not introduced. "The existing plan's PR-46", "all three" tools and the `required` job are not explained.
* **Rationale**: Each is a small gap a sentence in the Background would close.

### C-10. Several words carry two meanings or have several names - Severity: Nit

* **Affected Scope:** lines 22, 26–32, 100, 109, 121, 179, 189, 190, 207, 252, 324, 359, 518, 524, 586, 594, 728
* **Discrepancy Description**: "The requester" is never identified. "The author" is the document's author in some places and the pull request's author in others, and for agent-written pull requests it is unclear whether that is the human or the agent. User, reviewer, viewer and reader all name the same person. "Route" means a sign-in method and an HTTP route. "Usage ledger" is not defined.
* **Rationale**: "PR-47" and similar appear in the feature part before the numbering is explained. In a document about reviewing pull requests they read like reviewed pull requests.

### C-11. Small internal mismatches - Severity: Nit

* **Affected Scope:** lines 62, 203, 361, 628, 682–698, 783, 902
* **Discrepancy Description**: Line 203 says "three sources" and the table has four rows. Goal 7 says "five minutes in the example configuration", but the only configuration shown has a zero, which is declared an error. PR-55 creates a `history` table that is missing from the fixed table list.
* **Rationale**: Nothing says which paths "the repository's documentation and ADR folders" are, or whether `.review/config.yaml` itself is read from the merge base or only the documents it names.

### C-12. Process terms and outside files - Severity: Nit

* **Affected Scope:** lines 22, 427, 556, 952–954
* **Discrepancy Description**: "Five-stage review", "goldfish" and "EGM" are not explained. "Which points still need the author" is said to live in another file.
* **Rationale**: The reader cannot tell from this document whether the Open Questions section is the complete list of those points.
