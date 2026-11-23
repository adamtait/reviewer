# Goldfish Review Report: Stage 9, Implementation Readiness Test (round 2)

**Target:** `docs/designs/review-workspace.md` (DetailedRange, lines 573–954), as revised after the first review round.

**Standard:** `implementation-readiness-standard.md` for the readiness and codebase reviewers. The style reviewer applied its own audit vectors.

**Verdict:** the plan does not meet the standard. Three findings block work as written.

## Reviewer Status

| Reviewer | Status | Findings |
| --- | --- | --- |
| `egm-implementation-readiness` | completed | 3 Blocker, 7 Major |
| `egm-codebase-details` | completed | 1 Blocker, 11 Major |
| `egm-style` | completed | 0 Blocker, 7 Major, 1 Nit |

The three reviewers overlap, so their 30 raw findings are merged into 15 below. Each entry names the raw findings it came from: `R-n` for readiness, `K-n` for codebase and `P-n` for style.

All three could research on the web, and two ran experiments in scratch directories outside the repository: resolving the Go module graphs, installing the listed web packages, and running the pinned secrets scanner. None changed a file in the repository.

**What the reviewers found correct**

* Every file marked Modify exists, and nothing marked Create already exists.
* The ADR file names, metadata-only supersession edits and index updates match `tools/checkadrs`.
* The existing test fakes for the GitHub client keep compiling under the plan.
* The five GitHub token prefixes to be added match GitHub's documentation, and `ghp_` is already covered.
* An explicit scanner rule file takes precedence over one in the scanned content.

## Prioritized Findings (Blockers -> Major -> Nits)

### IR-1. The two Go dependencies cannot be added as written - Severity: Blocker

* **Reviewer Lens:** `egm-implementation-readiness` (R-3), `egm-codebase-details` (K-1, K-2), `egm-style` (P-1)
* **Affected Scope:** lines 575, 751, 755–756, 784, 787, 857
* **Discrepancy Description**:
  * **The Postgres driver.** Every release of `pgx` from v5.9.0 to the latest, v5.13.0, declares Go 1.25. The last release for Go 1.24 is v5.8.0, from December 2025. This repository's `go.mod` says 1.24 and CI tests 1.24 and 1.25 ([module file](https://proxy.golang.org/github.com/jackc/pgx/v5/@v/v5.13.0.mod)). With toolchain switching on, adding the dependency rewrites `go.mod` to 1.25, and the CI leg labelled 1.24 would silently run 1.25. Even v5.8.0 adds 15 modules that each need an inventory row.
  * **The index reader.** The SCIP Go bindings moved to `github.com/scip-code/scip/bindings/go/scip`, a separate module that requires Go 1.25. Its module graph includes `pgregory.net/rapid`, which is under the MPL-2.0 licence ([module file](https://proxy.golang.org/github.com/scip-code/scip/bindings/go/scip/@v/v0.10.0.mod)). `tools/checklicenses` inventories the whole module graph and refuses any licence containing "MPL". The last release under the old path puts 292 modules in that graph, two of them MPL-2.0. Either way `make licenses` fails in PR-51.
* **Rationale & Recommendation**: The plan says a refused dependency "is replaced" and names no replacement, and it defers the choice to entry condition E2, which only covers running the indexer. Pin both versions and the Go floor, list the CI matrix change, and decide how the index is read: a change to the licence tool with a new ADR, or a decoder that needs no bindings.

### IR-2. Database columns and every request and response shape are left to the implementer - Severity: Blocker

* **Reviewer Lens:** `egm-implementation-readiness` (R-1), `egm-codebase-details` (K-11)
* **Affected Scope:** lines 600, 680–698, 700–728, 801, 902
* **Discrepancy Description**: The plan says "everything else is decided in the pull request that builds it" and makes unwritten migrations the source of truth. The standard names choosing a schema and designing an API as choices that fail a plan. The occurrence table is keyed on "position" and names no symbol column, so the lookup behind every navigation call has no defined shape. A `history` table is created that the shared list omits. The 21 routes have a purpose column only: no bodies, status codes, error format or stream framing. Who may read another user's conversation is not stated.
* **Rationale & Recommendation**: The claim that both sides are validated against one schema file has no mechanism: Go's standard library has no JSON Schema package, the plan commits to the standard library, and the web list has no validator. Put column lists, literal value sets and one request and response per route in the plan, and name the validator. This conflicts with the zoom-out reviewers, who want the plan cut back until the entry conditions are recorded.

### IR-3. Nothing says where stage work runs or how a dead stage is released - Severity: Blocker

* **Reviewer Lens:** `egm-implementation-readiness` (R-2)
* **Affected Scope:** lines 686, 709, 718, 810–811, 850, 890
* **Discrepancy Description**: The plan fixes running states, says opening a pull request "starts the fetch if needed", and says a second request "returns the running state". It does not say whether fetch, index load, brief and review run inside the request or after the response, or who owns a running state.
* **Rationale & Recommendation**: On Cloud Run this decides whether the feature works. Under default billing CPU is allocated only during request processing, a request is cut at 300 seconds by default, and a stopping instance gets 10 seconds ([billing settings](https://docs.cloud.google.com/run/docs/configuring/billing-settings), [request timeout](https://docs.cloud.google.com/run/docs/configuring/request-timeout)). If the request that set "generating" dies, nothing clears it. No route or interval is defined for the browser to learn progress, and no test covers two simultaneous opens or recovery. State the execution model, an ownership and expiry rule, how the client polls, and tests for both.

### IR-4. Scanned content can switch the secrets scan off - Severity: Major

* **Reviewer Lens:** `egm-implementation-readiness` (R-4), `egm-codebase-details` (K-5), `egm-style` (P-3)
* **Affected Scope:** lines 617, 692, 864–865, 868, 937
* **Discrepancy Description**: One reviewer ran the pinned scanner, gitleaks 8.28.0, the way the plan describes. The fixture credential was reported. The same line with a `gitleaks:allow` comment appended was not reported. A `.gitleaksignore` file at the root of the scanned directory suppressed both findings, even with the flag that disables the inline comment.
* **Rationale & Recommendation**: A pull request author can mark a credential, and the gateway then records "clean" and sends it. The plan's only bypass test is a rule file inside the content. The scanner's default rules also exempt paths such as lockfiles and `node_modules/`, so results depend on the file name the content is written under, which the plan does not state. Scan results are keyed by content hash while the operator's allowlist is path-based, so the same bytes at an allowed and a disallowed path share one cached verdict. Add the flag, fix a layout where no scanned file can sit at the scan root, and add both cases to the tests.

### IR-5. The current diagram library brings a licence the gate refuses - Severity: Major

* **Reviewer Lens:** `egm-implementation-readiness` (R-5), `egm-style` (P-1)
* **Affected Scope:** lines 751, 760, 840, 880
* **Discrepancy Description**: Installing `mermaid` today resolves version 12.3.0, which depends directly on `elkjs`, licensed "EPL-2.0 OR GPL-3.0-or-later". Both alternatives are on the licence tool's denylist, so the dual-licence rule the plan adds does not help. The 11.x line has no such dependency ([registry](https://registry.npmjs.org/mermaid)).
* **Rationale & Recommendation**: The plan pins no version and defers the check to PR-53, after two feedback weeks. The library also accounts for more than 100 of the roughly 150 production packages that would need hand-written inventory rows, and ADR-0028 rejected a table of that size as unreadable. Decide now: pin the 11.x line, choose another renderer, or drop diagrams.

### IR-6. Reusing the review code gives the author's description authority, and the gateway contract has holes - Severity: Major

* **Reviewer Lens:** `egm-implementation-readiness` (R-6), `egm-codebase-details` (K-4, K-9)
* **Affected Scope:** lines 663–676, 803, 866–867, 887–888
* **Discrepancy Description**: The plan passes the description and intent record to the CLI's review code as guidance, which that code prints under a heading saying guidance documents "win". The review code's `Gather` reads a working tree, so the service must build its input by hand, and the plan does not say what goes in each field. A review is two provider calls; if the second fails, every unchecked finding is kept, and the plan has no state for that. An unparseable reply becomes a warning plus zero findings, which would display as "the model raised nothing".
* **Rationale & Recommendation**: The gateway's request type has only a token limit, while the review code sets a temperature and asks for JSON output, so the wrapper must drop these or invent fields. The plan says the caller "reads the outcome from the gateway" but defines no call, and says withheld paths are returned "beside the answer" but the event list has no variant for them. Both providers answer rate limits and exhausted quota with the same status code, and no rule separates them. Pass the description under its own heading, which means changing the review code, and define these items.

### IR-7. The planned GitHub calls cannot produce the index states, and lookup rules are incomplete - Severity: Major

* **Reviewer Lens:** `egm-implementation-readiness` (R-7), `egm-codebase-details` (K-8, K-12)
* **Affected Scope:** lines 613, 632–637, 686, 728, 846, 849–852
* **Discrepancy Description**: The artifact list filters by name only, and every commit's artifact has the same fixed name, so finding one commit's index means paging the whole list. The service cannot tell "waiting for CI" from "not produced" or "failed" without the workflow-runs call, which the plan does not add ([workflow runs API](https://docs.github.com/en/rest/actions/workflow-runs)). The existing client's request helper cannot serve a zip download: it asks for JSON and decodes the body.
* **Rationale & Recommendation**: The fixed artifact name is never stated, though the template and the service's configuration must match. SCIP local symbols do not encode a name, so the name check is undefined for local variables. SCIP columns follow a per-document encoding, and the plan says only "zero-based". The committed fixture index would carry the generating machine's absolute path, and the history audit skips binary files, so neither publishing gate would catch it. Fix a neutral build path for the fixture or build it at test time.

### IR-8. The file lists and CI wiring are incomplete, and two changes break existing tests or callers - Severity: Major

* **Reviewer Lens:** `egm-implementation-readiness` (R-8), `egm-codebase-details` (K-3, K-6), `egm-style` (P-5)
* **Affected Scope:** lines 702, 767, 779–782, 787–788, 819, 839, 846–848, 852, 867, 912
* **Discrepancy Description**:
  * **PR-51 breaks an unlisted test.** `cmd/reviewer/main_test.go` asserts "7 files to create" and "7 files created" on a TypeScript fixture, and an eighth installed file fails both.
  * **The tool installer change collides with its callers.** The script's one argument is already the install directory, passed by both workflows. "Accept tool names as arguments" does not say how the two are told apart.
  * **CI gaps.** The new `workspace` job inherits the variable that makes a missing tool fail, and from PR-52 its tests need the scanner, which the job is not said to install. The release job runs only `make check`, which excludes the database tests, so an image would be published from a tag whose store tests never ran. The image job's place in the job order and its publishing permission are unstated.
* **Rationale & Recommendation**: Handler tests are promised for every route, but the test file is listed in only two pull requests. The Dockerfile row names no base images. The trigger for writing the index workflow differs from the existing TypeScript condition, and no test keeps the hand-copied example equal to the rendered template. Four statements about today's code are also inaccurate: the installer's arguments, what the existing diff functions compare, "correct the count" where the count is right today, and a schema file said to be modified by pull requests that precede its creation.

### IR-9. Source import: one file can fail a whole commit, and three mechanisms are unnamed - Severity: Major

* **Reviewer Lens:** `egm-implementation-readiness` (R-9), `egm-codebase-details` (K-10), `egm-style` (P-2)
* **Affected Scope:** lines 688–689, 810–814, 936
* **Discrepancy Description**: The plan treats any valid UTF-8 as text. Valid UTF-8 can contain a NUL byte, which a Postgres text column cannot store ([Postgres docs](https://www.postgresql.org/docs/current/datatype-character.html)). Import is one transaction, so one such file means the pull request cannot be opened. The existing diff package's `git` helper inherits the whole process environment, so a new function built on it hands the database address and keys to `git`, outside the packages where the "empty environment" test is placed.
* **Rationale & Recommendation**: The plan asks for "an empty environment" and a token "through the environment" without naming the mechanism. One reviewer checked that configuration passed through `GIT_CONFIG_COUNT` variables works with no helper program, where a credential helper would be a third program. Git has no fetch size limit, so the size cap needs a stated method. The plan's status value "removed" also differs from the existing package's "deleted".

### IR-10. The web tooling list cannot run the plan's own lint and test commands - Severity: Major

* **Reviewer Lens:** `egm-implementation-readiness` (R-10), `egm-codebase-details` (K-11)
* **Affected Scope:** lines 757–761, 827, 833–836, 879, 922–923
* **Discrepancy Description**: One reviewer installed exactly the listed packages. Linting a `.tsx` file fails with a parsing error, because no TypeScript parser is listed. Type-checking fails for `react`, because no React types are listed. The test runner has no document object, and the two component tests need one.
* **Rationale & Recommendation**: Each missing package is an unplanned dependency and inventory row. Two build details are also undecided: emptying the build output directory deletes the tracked placeholder file, and the embed directive compiles with only the placeholder present in one of its two forms and not the other.

### IR-11. Running `npm ci` inside the licence tool also runs it inside a unit test on every `make test` - Severity: Major

* **Reviewer Lens:** `egm-codebase-details` (K-7)
* **Affected Scope:** lines 751, 840
* **Discrepancy Description**: The plan's reason is that the CI job that calls the tool has no Node step. An existing test calls the same function, so a network install would run in every matrix leg, in any contributor's `make test`, and twice in `make check`.
* **Rationale & Recommendation**: If "the tree it reads" includes the plugin's directory, the install deletes that `node_modules` while other packages' tests are driving the real plugin. The headings for the two new inventory sections and the new key are also not given, and the parser matches exact heading text.

### IR-12. The gates and hand checks have no pass values, and their measures arrive too late - Severity: Major

* **Reviewer Lens:** `egm-style` (P-4)
* **Affected Scope:** lines 594, 596, 899, 947–948
* **Discrepancy Description**: Work continues "only if the thresholds the author sets are met", with no numbers and no date by which they are fixed. The counts depend on usage events whose recording arrives in PR-55, after all three feedback weeks. The outcome measure has no before and after windows and no size of change that counts.
* **Rationale & Recommendation**: The restore check opens a pull request view "before the first repository is connected", when there is nothing to open. State the numbers or the date they are fixed, name each measure's source at each feedback point, and give the hand checks a workable order.

### IR-13. The "A reviewer can then" column overstates three rows - Severity: Major

* **Reviewer Lens:** `egm-style` (P-6)
* **Affected Scope:** lines 582, 585, 590, 592
* **Discrepancy Description**: "Sign in" is claimed after PR-48, but the sign-in page arrives in PR-50. "Navigate code" is TypeScript only, in repositories where the workflow file was copied in by hand, for commits whose run produced an index. "First version complete" is a judgement, not something a reviewer can do.
* **Rationale & Recommendation**: The operator deploys after every pull request, but the operating guide, the statement of what the service stores and the retention sweep all arrive in the last one, while the team uses the service from PR-50.

### IR-14. Two guarantees are worded more strongly than the check behind them - Severity: Major

* **Reviewer Lens:** `egm-style` (P-7)
* **Affected Scope:** lines 577, 702, 785, 829, 834, 838
* **Discrepancy Description**: "Each leaves `make check` passing" holds while the database tests are kept out of `make check`, where they skip, and the web tests are not added to it. `docs/architecture.md` records a skipped test that looked green for the project's whole history.
* **Rationale & Recommendation**: "Cannot drift apart unnoticed" is claimed for the service and the web application, but the plan validates only the fake API's responses, the web types are written by hand, and nothing runs the built application against the service. State the checks each pull request must pass, and say which drift is caught.

### IR-15. Wording - Severity: Nit

* **Reviewer Lens:** `egm-style` (P-8)
* **Affected Scope:** lines 586, 628, 676, 684, 728, 902, 925, 935, 939, 952–954
* **Discrepancy Description**: "No value is chosen on the operator's behalf" holds only for four settings, while three other limits are fixed in the plan. `org`, `owner` and "organisation" name one thing. "Usage ledger" means the model call table, not the usage event table. "After the cache period" should read "within".
* **Rationale & Recommendation**: Pick one term per concept and correct the four statements.
