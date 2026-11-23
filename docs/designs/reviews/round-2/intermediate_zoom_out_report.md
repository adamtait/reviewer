# Goldfish Review Report: Stage 8, Intermediate Zoom Out (round 2)

**Target:** `docs/designs/review-workspace.md` (full file, lines 1–954)

**Standard:** `zoom-out-standard.md`

**What this pass did.** The document was not edited after Stage 5. This reviewer read the three earlier reports of this round and judged the fixes they ask for as a set, against the standard's preference for the fewest components. It could research on the web and changed no file.

## Reviewer Status

| Reviewer | Status | Findings |
| --- | --- | --- |
| `egm-zoom-out` | completed | 1 Blocker, 8 Major, 1 Nit |

**Verdict.** Context sufficiency passes. The design does not meet the standard, and accepting all the requested mitigations would move it further from the simplicity requirement. The three reports together ask for about twenty new mechanisms or states. For most of them, the problem goes away if one sentence, input or pull request is removed from the first version instead.

## Prioritized Findings (Blockers -> Major -> Nits)

### Z2-1. Settle five things first; about half the earlier findings then need no fix - Severity: Blocker

* **Affected Scope:** lines 26–32, 100–105, 109, 330, 440–441, 575
* **Discrepancy Description**: The gate holds all work until E1 to E4 are recorded, and E4's stop rule would reduce the design to the change brief. Any mitigation written for a part E4 later deletes is wasted.
* **Rationale & Recommendation**: Settle in this order.
  1. **E4, with pass criteria.** One new outside fact: VS Code for the Web has offered full-project TypeScript "Go to Definition" and "Find All References" since version 1.93, in Chrome, Edge and Firefox but not Safari ([release notes](https://code.visualstudio.com/updates/v1_93)). GitHub's web editor opens a pull request when `.` is pressed on its page ([GitHub docs](https://docs.github.com/en/codespaces/the-githubdev-web-based-editor)). The reviewer could not confirm the feature is on in GitHub's own build of that editor, and it probably lacks types generated at install and iPhone support. A ten-minute check on one real pull request settles it. E4 must also say whether the one-provider rule rules Copilot out; if it does, "ask" is distinctive for that reason alone.
     * If "ask" is covered, PR-52 and PR-53 leave the first version, and I-2, D-4, C-2, D-7, I-8, Z-6 and Z-9 need no fix now. Questions are 30 of the 40 million estimated weekly tokens.
     * If navigation is covered, PR-51 leaves, and with it I-4, I-5, I-10, D-6, D-8 and C-6.
     * If both are covered, there is no service.
  2. **The trial the design declined.** It decides whether the service calls the CLI's review code at all (Z2-5).
  3. **Storage, together with the rule that a running stage binds other readers** (Z2-3, Z2-4). Settle these before D-2, I-1, C-3, C-4, C-5, I-6, D-9 and Y-7.
  4. **E1, put to the owner with both routes at once** (Z-5). Settle it before D-3 and I-7.
  5. **End the document at line 571 until the above are recorded** (Z-7). About a dozen findings point mainly at lines below 573.

### Z2-2. The smallest version is two template changes and one CLI change, then at most five pull requests - Severity: Major

* **Affected Scope:** lines 36, 38, 93, 435, 440, 579–596, 639–652, 781–782, 831, 834, 846, 891, 912–913
* **Discrepancy Description**: After the public documentation, two things look distinctive. One is navigation resolved by the compiler from a diff line into unchanged code at the pull request's commit, including generated types. The other is the intent record. A diff reader and "ask" are not distinctive.
* **Rationale & Recommendation**:
  * **Part A needs no service.** The agent skill change with the intent record format: GitHub shows the description on the pull request's first tab, so "the reason before any line is read" is tested on GitHub's own page. The index workflow template: this is also entry condition E2, and the plan already says the file is copied by hand. One CLI change not in the plan: an author-supplied section in the review code's input, after which the declined trial can run on one repository.
  * **Part B applies only if E4 leaves a navigation gap:** PR-47 to PR-51, with the first feedback week after PR-51. The week after PR-50 tests only reading a diff. Trim the scanner from the image (it is first used in PR-52), and the touch sheet, command palette, shortcut help and phone journeys unless touch navigation is itself under test.
  * **Not needed to test anything:** PR-52 to PR-56. The image release and its ADR serve other operators, which the design lists as not planned.
  * **Smaller still, for a desktop-only trial:** the same binary run by each engineer with their own token tests navigation without sign-in, hosting or a shared store. The design rejects that shape because of phones and sharing, which the first feedback question does not need.

### Z2-3. Leases, takeover and a detached worker repair a rule that can be deleted - Severity: Major

* **Affected Scope:** lines 282–293, 384, 501, 567, 686–688, 694
* **Discrepancy Description**: I-1 and D-2 ask for an owner, a lease, a heartbeat, takeover, an "abandoned" state and a detached job. The stuck row exists for one reason: line 291 makes a running state binding on other readers, and the state is stored.
* **Rationale & Recommendation**: The rule avoids duplicate work when two people open the same commit at once. At 10 users and about 80 commits opened a week that is rare, and a duplicate costs one fetch or one brief. Delete the sentence at 291 and store results only, not running states. A request that finds no result does the work itself. The keys are by content or commit, so a duplicate write is harmless, and a crashed attempt leaves nothing behind, so the next open retries. "No background work" stays true. This also removes the conflict between I-1, which wants work that outlives the request, and I-7, which wants no token outside the request. Still needed from D-2, with no new mechanism: state the platform's request timeout against the time E3 measures.

### Z2-4. Decide storage before fixing fetch, memory or retention; the repository already has a diff path without `git` - Severity: Major

* **Affected Scope:** lines 192, 264, 277, 297–304, 384–386, 456–457, 520, 525, 687–689, 810–817, 911
* **Discrepancy Description**: The reports pull three ways: keep Cloud Run and add disk mounts, move to `git` on a disk, or drop `git`. A file table keyed by git hash with a path list per commit is git's own model rebuilt in Postgres, and fetching trees and then only missing files rebuilds git's fetch negotiation.
* **Rationale & Recommendation**: No earlier report raised this reuse point: `ghclient.ListFiles` and `diff.FromPatches` are already used together in `cmd/reviewer/run.go` to turn GitHub's per-file patches into changed files and line ranges with no `git`. The plan instead adds a new diff function, a compare call and the fetch. If files outside the diff are read on demand with the viewer's token, the navigation slice stores no source. That removes the fetch into memory, three tables, the commit sweep, the growth risk and the "empty environment" contradiction, and GitHub then enforces access on every source read. The cost is text search at a commit, which needs the whole tree. E3's timing should decide between one archive per commit searched in memory and a bare repository on a disk.

### Z2-5. Repairing the reuse of the CLI review code costs more than deferring that half of the review - Severity: Major

* **Affected Scope:** lines 75, 289, 347, 361, 365–370, 668, 674, 866, 887
* **Discrepancy Description**: Z-4, I-3, D-5 and Y-4 are right about the code; this reviewer confirmed that guidance "wins", that the diff is cut at 120 KB, and that a failed second pass keeps every finding. The full repair is an author-context input, typed outcomes from two calls, a truncation flag, lockfile exclusion and two or three new view states.
* **Rationale & Recommendation**: That repair buys a second copy of findings the CLI already posts on GitHub whenever its model lane is on, where reviewers still comment. Take "findings on changed lines" out of the workspace's first version, and remove the provider wrapper, the call into the review code and the Review stage. Keep "notes on the plan", which does not use that code. Make the author-context input in the CLI now, because the trial needs it. Goal 6 is a stated requirement, so meeting its second half through the CLI lane needs the author's agreement.

### Z2-6. The coverage audit defends a claim the first version need not make; take the trigger change but not the run lookups - Severity: Major

* **Affected Scope:** lines 57, 241–248, 287, 310, 314, 318, 359, 361–363, 886
* **Discrepancy Description**: I-4 and D-6 ask for coverage per commit, "not indexed" marks, text matches merged into reference lists and a third source label. All of it defends one claim, "what unchanged code depends on it", drawn from the unverifiable index and stored in the brief for every reader.
* **Rationale & Recommendation**: Remove that input from the brief. The brief then uses trusted inputs only, the brief and index stages stop depending on each other, and the sentence that a false index cannot change what readers see becomes true as written. For find-references, take only the wording fix: label results "from the CI-built index", never show an empty list as "no references", and offer the text search that already exists. From I-5 and D-8, take the trigger change: indexing every branch push means one trigger, a default checkout that is already the pushed commit, and binding by head commit alone. It also makes moot whether a pull request run reports the head or the temporary merge commit. Do not take the run lookups and time limit; two index states are enough, "ready" and "no index for this commit", with a link to the repository's Actions page.

### Z2-7. Remove the operator allowlist and switch the scanner's exemptions off; do not build exemption logic - Severity: Major

* **Affected Scope:** lines 332–341, 495, 524, 566, 617, 670, 692, 865
* **Discrepancy Description**: I-2 and D-4 found the same hole and ask for opposite amounts of machinery. I-2 wants the gateway to apply exemptions itself. D-4 wants the scanner's own exemptions switched off and the limit stated.
* **Rationale & Recommendation**: The standard favours D-4, and the switches exist ([gitleaks README](https://raw.githubusercontent.com/gitleaks/gitleaks/master/README.md)). I-2's second hole, a clean verdict cached for content first seen at an allowlisted path, exists only because of the operator allowlist. Remove that allowlist, and the cache key is then correct with no path logic. The cost is one the design already accepts: a fixture credential blocks AI for that commit until the rate is measured. D-4's test that only the gateway imports provider adapters conflicts with a single shared adapter stack; enforce "one door" by which package is handed the keys.

### Z2-8. A login allowlist is a second access table; refuse public repositories and settle E1 first - Severity: Major

* **Affected Scope:** lines 62, 102, 322–326, 448–449, 523, 618–625, 802
* **Discrepancy Description**: D-3's exposure exists only when a connected repository is public, because every token reads public repositories. An allowlist is a hand-kept table beside GitHub's, a shape the design rejected elsewhere because "a wrong row grants the wrong access".
* **Rationale & Recommendation**: Make a public repository a configuration error in the first version, using the repository lookup the plan already adds, and refuse a pasted token without the fine-grained prefix. "Mirror GitHub access" is then the whole rule again. Put both identity routes to the owner in E1 first: under an App route the pasted-token problems go together.

### Z2-9. Budget shares, required caching, earlier telemetry and an extra feedback week all come before any measurement - Severity: Major

* **Affected Scope:** lines 288, 345, 388–394, 398, 518, 594, 625, 670, 853, 899–905
* **Discrepancy Description**: A reserved budget share partitions a spend that is only an estimate. Requiring prompt caching optimises before a bottleneck is shown. Moving telemetry ahead of the feedback weeks adds a subsystem to measure three counts.
* **Rationale & Recommendation**: With the assistant deferred there is one consumer and nothing to partition; the real gap is that "budget blocked" is an outcome but not one of the brief's states. Keep D-7's two checks that need nothing built: a monthly figure in money, and the account's spend cap checked before the gateway is built. Three of the five feedback measures are counts over request logs that already carry user, repository and pull request, and `cmd/reviewer/metrics.go` already walks pull request history for the baseline. The telemetry pull request can wait; the goal it serves is a stated want, so deferring it is the author's call.

### Z2-10. Drop the diagram library instead of sandboxing it, and cut the plan before adding text - Severity: Nit

* **Affected Scope:** lines 59, 380, 760, 840, 879–880; 96–113, 516–528, 573–917
* **Discrepancy Description**: D-10 keeps the diagram library inside a sandboxed frame; Z-9 shows diagram text. The registry confirms the library depends directly on a package under two licences this repository's checker refuses. Several findings ask for more risks, questions and definitions, while Z-7 asks to cut the 380 lines below 571.
* **Rationale & Recommendation**: The standard favours showing diagram text. Dropping the library may also drop the planned dual-licence change to the checker and the diagram test. Sending the content security policy as a response header costs nothing and should be taken. Cut the plan first, because several of the requested additions describe parts Z2-1 may remove.

## Where the reports conflict

| Reports | Conflict | The standard favours |
| --- | --- | --- |
| I-1, D-2 against I-7 | Work that outlives the request, against no token outside the request | Neither; remove the shared running state (Z2-3) |
| I-6, Y-7 against Z-3 against D-9 | Disk mounts on Cloud Run, `git` on a disk, or no `git` | Least owned storage; E3 decides (Z2-4) |
| I-2 against D-4 | Exemption logic in the gateway, against exemptions off | D-4 (Z2-7) |
| D-4 against Z-6 | An import test, against one shared adapter stack | Key possession (Z2-7) |
| Z-6 against Z-8 and ADR-0021 | Vendor SDKs, against dependency cost | Extend the existing adapters, if built (Z2-5) |
| I-4, D-6 against C-6, Y-8 | Measure coverage, against relabel | Relabel and remove the claim (Z2-6) |
| C-1 against Z-2, Z-10 | Telemetry before feedback, against logs and the existing command | Z-2 and Z-10 (Z2-9) |
| D-7 against Z-2, Y-5 | Require caching now, against measure first | Measure first (Z2-9) |
| D-10 against Z-9 | Sandbox the diagram library, against drop it | Z-9 (Z2-10) |
| S-3, S-4, Y-9, C-8 to C-12 against Z-7 | Add text, against cut the plan | Cut first (Z2-10) |

## Outside facts this reviewer relied on

* VS Code for the Web, full-project TypeScript navigation, not in Safari: [release notes](https://code.visualstudio.com/updates/v1_93). The same product's web page still says "rich single file completions": [VS Code for the Web](https://code.visualstudio.com/docs/setup/vscode-web).
* GitHub's web editor opens a pull request with `.`, has no compute and runs a subset of extensions: [GitHub docs](https://docs.github.com/en/codespaces/the-githubdev-web-based-editor).
* The pull request files call returns at most 3,000 files with a patch for each: [pulls API](https://docs.github.com/en/rest/pulls/pulls).
* Package dependencies and licences: [mermaid](https://registry.npmjs.org/mermaid/latest), [elkjs](https://registry.npmjs.org/elkjs/latest), [markdown-it](https://registry.npmjs.org/markdown-it/latest).

## Not verified

* Whether GitHub's current "Files changed" page still offers definition and reference lookups.
* Whether GitHub's build of the web editor has full-project TypeScript navigation on a pull request, and how it behaves on an iPhone.
* Whether a pull request's workflow run reports the head commit or the temporary merge commit. The push trigger in Z2-6 makes this moot.
* Which token permission the pull request files call needs.
* Whether the team holds Copilot licences.
