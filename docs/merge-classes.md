<!-- SPDX-License-Identifier: MIT -->
# Merge classes

A merge class is a named, rule-checked description of pull requests allowed to merge without a
human. This guide sets up the first one, **docs-only**: a pull request whose every changed path is in
a documentation allowlist merges once CI is green, with no approval. The design is
`docs/designs/risk-gated-merge.md` (Part 2, goals G4, G5 and G5b).

No code in this repository decides a docs-only merge. GitHub does, from two pieces of repository
configuration: a CODEOWNERS file that owns every path except the allowlist, and a ruleset that
requires code-owner review with 0 general approvals. `reviewer owners check` is the lint that keeps
the CODEOWNERS file honest (ADR-0036).

**Every statement about GitHub's behaviour in this guide is unverified** until the sandbox steps
below have run on your plan. Do not change a real repository's ruleset before they have, and before
the failure policy has an owner.

## Before you start

Three things must be true, in this order:

1. **Enough pull requests would qualify.** Count the merged pull requests of the last 90 days whose
   files are all under the proposed allowlist. If fewer than 5% qualify, stop here: the class saves
   too little to be worth its risk.
2. **Q1 passes in a sandbox repository**: GitHub enforces code-owner review at 0 required approvals.
   If it does not, a CODEOWNERS file cannot hold anything back, and there is no docs-only class.
3. **Q2 passes on the real repository**: no organisation ruleset already sets a minimum approval
   count. If one does, the class needs an organisation owner, and this guide does not apply.

## How it works

With "require review from code owners" on and 0 required approvals:

*   a pull request that changes any owned path waits for an approval from that path's owner;
*   a pull request whose every changed path is unowned needs no approval at all.

So the allowlist is the set of unowned paths, and CODEOWNERS is the whole policy. A wrong CODEOWNERS
file removes review from a path and nothing on the pull request shows it, which is why the lint is a
required check.

Native settings cannot tell a delete, a rename or a symlink from an edit. Any change whose every
path is unowned merges, including one that deletes allowlisted files, renames them within the
allowlist, or adds a symlink there. That is accepted (G4); the weekly audit below is what watches
it. A rename from the allowlist to an owned path touches an owned path, so it waits for review.

## The CODEOWNERS template

GitHub reads the first of `.github/CODEOWNERS`, `CODEOWNERS` and `docs/CODEOWNERS` that exists, and
the last rule that matches a path decides its owners. Write the file in three parts:

```text
# 1. Everything is owned. This must be the first rule.
*                   @your-org/maintainers

# Rules that give paths more specific owners go here. Every one of them
# names an owner.
/src/payments/      @your-org/payments

# 2. The allowlist: directories people alone read, each with no owner and
#    each ending in /. Nothing on the never-allowlist below.
/docs/guides/
/docs/tutorials/

# 3. Owned again wherever it appears, including inside the allowlist,
#    because the last matching rule wins.
SKILL.md            @your-org/maintainers
```

*   The first rule is `*` with an owner, so a path no later rule names is still owned.
*   The file owns itself through `*`. Keep it at `.github/CODEOWNERS`, which is under `.github/` and
    so can never be in the allowlist.
*   Every allowlist rule is a directory and ends in `/` (a trailing `/**` means the same). A pattern
    such as `*.md` would also leave `AGENTS.md`, `CLAUDE.md` and every other instruction file
    unowned.
*   The allowlist comes after `*` and the owned rules, because a later rule overrides an earlier one.
    The one exception is part 3: `SKILL.md` is on the never-allowlist at any depth, so it has to be
    owned again after the allowlist, or a SKILL.md could be added inside an allowlisted directory
    with no review.
*   If an allowlisted directory holds something on the never-allowlist, own that path again after
    the allowlist. For example, allowlisting `/docs/` needs `/docs/adr/ @your-org/maintainers`
    after it.
*   The owning team must have write access to the repository, or GitHub does not count its approval.

## The never-allowlist

Nothing here may be unowned, because a model, an agent or the build reads it, or because it decides
how the repository is reviewed and merged:

*   every path listed under `guidance:` in `.review/config.yaml`, which the model lane reads into its
    prompt;
*   `laneB.promptDir` (`.review/prompts` when unset);
*   agent instruction files: `AGENTS.md`, `CLAUDE.md`, `GEMINI.md` and `.cursorrules` at the root,
    `SKILL.md` anywhere, and everything under `.claude/`;
*   `.github/`, `.review/` and `docs/adr/`;
*   every file a Go `//go:embed` directive names, since it is built into the binary;
*   every file another build step reads: code generators, bundlers, a Dockerfile's `COPY`, test
    fixtures CI depends on;
*   every directory a docs site publishes (see "Published documentation").

`reviewer owners check` enforces the first five. The last two it cannot detect, so check them by
hand when you choose the allowlist and whenever the build changes.

## The lint

`reviewer owners check` reads CODEOWNERS and `.review/config.yaml` from the working tree (a missing
configuration file is fine: the compiled-in list and the default prompt directory still apply) and
exits 1, listing every violation with its CODEOWNERS line, when:

*   there is no CODEOWNERS file, or a line does not parse;
*   the first rule is not `*` with an owner;
*   CODEOWNERS does not own itself, or a place where a new CODEOWNERS file would replace it;
*   a rule with no owners is not a directory;
*   a path on the never-allowlist resolves unowned.

For example, with `docs/guidance/` listed under `guidance:` and added to the allowlist (CUJ 4):

```
$ reviewer owners check
.github/CODEOWNERS, 5 rules, 11 never-allowlist paths
  line 4    docs/guidance/ (guidance) is on the never-allowlist, and docs/guidance/x is left unowned by "/docs/guidance/"
reviewer: .github/CODEOWNERS is not safe for the docs-only merge class: 1 violation
```

The never-allowlist check is a probe, not a proof. It asks CODEOWNERS about concrete paths: each
entry, a file under each directory entry, and paths inside each unowned rule. A rule that exposes a
listed path only at some other path can get past it; keeping every allowlist rule a plain directory
is what makes the probe worth having.

The lint reads the pull request's own CODEOWNERS and configuration. That is the point: it checks the
file being proposed. A pull request that changes either of them, or the workflow that runs the lint,
changes an owned path and waits for a code owner.

### Make it a required check

Add a workflow that runs on every pull request. Do not give it a `paths:` filter: a required check
that never starts leaves every pull request waiting.

```yaml
name: codeowners

on:
  pull_request:

permissions:
  contents: read

jobs:
  owners:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install reviewer
        run: |
          set -euo pipefail
          archive="reviewer_${VERSION}_linux_amd64.tar.gz"
          curl --fail --silent --show-error --location --output "$archive" \
            "https://github.com/adamtait/reviewer/releases/download/v${VERSION}/${archive}"
          echo "${CHECKSUM}  ${archive}" | sha256sum --check --status \
            || { echo "::error::checksum mismatch for ${archive}"; exit 1; }
          tar -xzf "$archive" reviewer
          install -m 0755 reviewer "${RUNNER_TEMP}/reviewer"
        env:
          # A release that has `owners check`, pinned with its checksum from the
          # release's checksums file.
          VERSION: "<version>"
          CHECKSUM: "<sha256>"

      - name: Check CODEOWNERS
        run: '"${RUNNER_TEMP}/reviewer" owners check'
```

Then add the job's check name (`owners`) to the ruleset's required status checks, beside the
existing CI checks.

## Ruleset settings

On the repository ruleset that protects `main`:

| Setting | Change to |
| --- | --- |
| Require a pull request before merging | on (leave it on) |
| Required approvals | 0 (from 1) |
| Require review from Code Owners | on |
| Required status checks | the existing CI checks, plus `owners` |

Leave these as they are:

*   "Dismiss stale pull request approvals when new commits are pushed", and "Require approval of the
    most recent reviewable push". Whether they still apply at 0 approvals is part of Q1.
*   The bypass list. Adding anyone to it to make the class work defeats it.
*   Every existing required status check. Docs-only merges still wait for green CI.
*   Force-push and deletion protection, and anything else the ruleset already sets.

`reviewer init` does not write CODEOWNERS or rulesets: it cannot know the teams, and a wrong
CODEOWNERS file removes review without any visible sign.

## Sandbox steps

Run these in a throwaway repository on the same GitHub plan as the real one, and record the plan,
the date and each result in the trials file (`docs/designs/trials/risk-gated-merge-baseline.md`).
The expected results are what this design assumes. **None of them has been verified.**

### Q1: code-owner review at 0 approvals

Set up the sandbox with the CODEOWNERS template above (`*` owned by a team, `/docs/guides/` unowned),
a ruleset at 0 required approvals with code-owner review on, the lint workflow as a required check,
and native auto-merge allowed. From an account that is not a code owner, open one pull request for
each row and enable auto-merge on it:

| Pull request | Expected (unverified) |
| --- | --- |
| (a) edits a file under `docs/guides/` | merges with no approval |
| (b) deletes a file under `docs/guides/` | merges with no approval |
| (c) renames a file within `docs/guides/` | merges with no approval |
| (d) adds a symlink under `docs/guides/` | merges with no approval |
| (e) edits an owned file, such as `README.md` | waits for a code owner's approval |
| (f) edits `docs/guides/` and an owned file together | waits for a code owner's approval |
| (g) as (e), approved by a code owner, then a new commit pushed | approval dismissed, if "dismiss stale approvals" is on |
| (h) as (a), with a "changes requested" review from a code owner | not known; record whether it can merge |
| (i) adds `/.claude/` as an unowned rule to CODEOWNERS | the lint fails, and the pull request cannot merge even after a code owner approves |

If (e) or (f) can merge without an approval, code-owner review is not enforced at 0 approvals, and
the docs-only class does not ship. v1 is then `reviewer risk` and measurement only.

### Q2: no organisation ruleset sets an approval count

On the real repository:

```
gh api repos/OWNER/REPO/rules/branches/main
```

This lists every rule that applies to `main`, with where it came from. Expected (unverified): no
`pull_request` rule whose `ruleset_source_type` is `Organization` and whose
`required_approving_review_count` is above 0. Also check classic branch protection, which applies
alongside rulesets:

```
gh api repos/OWNER/REPO/branches/main/protection
```

Expected (unverified): no `required_pull_request_reviews`, or one you can change yourself. If an
organisation ruleset sets an approval count, stop: lowering it needs an organisation owner.

## Failure policy

The docs-only class has a failure policy before it goes live (G5b).

**Owner.** The team named on the `*` rule in CODEOWNERS owns the audit and the decisions below.

**An escape** is a docs-only merge with no review that is later:

*   reverted;
*   modified by a fix, as `reviewer outcomes` labels it (`hotfixed`); or
*   flagged in the weekly audit.

**The weekly audit.** Once a week, the owning team lists the merges of the last 7 days that had no
human review:

```
reviewer outcomes --since 7d --only-unreviewed
```

and reads 5 of them, chosen at random (all of them, if there are fewer than 5). For each, it checks
that every changed path was in the allowlist, and asks whether a reviewer would have asked for a
change. Anything a reviewer would have stopped is an escape. Keep the list read and the outcome, so
the audit itself can be checked.

**After the first escape**, set required approvals back to 1 (the rollback below) the same day, and
leave them there until the cause is understood and fixed: a CODEOWNERS rule tightened, a directory
taken off the allowlist, or the class retired. Record the escape and the fix in the trials file.

## Rollback

Set "Required approvals" back to 1 on the ruleset. Every pull request then needs an approval again,
docs-only ones included (unverified: whether pull requests already open are re-evaluated at once).
Leave CODEOWNERS and the lint as they are; they do no harm at 1 approval, and the class can be turned
back on by setting 0 again.

Rehearse the rollback in the sandbox before going live, and time it: it should be one person, one
setting, a few minutes.

## Published documentation

A directory that a docs site builds or deploys from is production content: what merges there is
what readers see. It leaves the allowlist (Q8). Check before allowlisting a directory, and whenever a
docs site is added:

*   GitHub Pages' source folder, in the repository's Pages settings;
*   any workflow that builds or deploys documentation, and the directory its tool reads (for example,
    `docs_dir` in `mkdocs.yml`).

The lint does not detect this, because no file says it in a form the lint can read.
