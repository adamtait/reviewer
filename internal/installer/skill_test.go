// SPDX-License-Identifier: MIT

package installer

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/internal/testfixture"
)

// The reviewer's question for this PR: does SKILL.md tell an agent *when* to
// invoke, not only how? A skill that only documents a command is a manual page,
// and an agent with a manual page runs it at the wrong times or not at all.
func TestTheSkillSaysWhenToRunAndHowToReadTheResult(t *testing.T) {
	root := testfixture.Destination(t, "tiny-monorepo")
	install(t, root)
	skill := read(t, root, ".agent/skills/code-review/SKILL.md")

	// The frontmatter is how an agent decides to load this at all, so the
	// description has to describe the occasion rather than the tool.
	if !strings.HasPrefix(skill, "---\n") || !strings.Contains(skill, "\ndescription:") {
		t.Fatalf("want frontmatter with a description:\n%s", skill)
	}
	for _, occasion := range []string{"before pushing", "before opening a pull request", "review comments"} {
		if !strings.Contains(strings.ToLower(skill), occasion) {
			t.Errorf("the skill does not say to run it %s", occasion)
		}
	}
	// And when not to: an agent that runs this on every file save has understood
	// the command and not the cost.
	if !strings.Contains(skill, "Not on every file save") {
		t.Error("the skill does not say when not to run it")
	}

	// The distinction that decides what an agent does with each finding.
	for _, want := range []string{"Facts", "Suggestions", "Confidence is the marker"} {
		if !strings.Contains(skill, want) {
			t.Errorf("the skill does not explain %q", want)
		}
	}
	// The failure mode worth naming explicitly, because it is the one an agent
	// under time pressure reaches for.
	if !strings.Contains(skill, "Do not fix a finding by suppressing it") {
		t.Error("the skill does not warn against suppressing a finding")
	}
	if !strings.Contains(skill, "not treat an empty result as permission") {
		t.Error("the skill lets an empty result read as approval")
	}
}

// The exit criterion: the wrapper adds no flags the CLI does not document. An agent
// reading SKILL.md would otherwise learn a vocabulary that only works here.
func TestTheWrapperAddsNoFlagsOfItsOwn(t *testing.T) {
	root := testfixture.Destination(t, "tiny-monorepo")
	install(t, root)
	script := read(t, root, ".agent/skills/code-review/scripts/review.sh")

	// Everything the script passes to reviewer, other than --root and the
	// caller's own arguments.
	invented := regexp.MustCompile(`--[a-z-]+`).FindAllString(script, -1)
	for _, flag := range invented {
		switch flag {
		case "--root", "--help":
		default:
			t.Errorf("the wrapper invents %s; an agent would learn a flag the CLI does not document", flag)
		}
	}
	if !strings.Contains(script, `"$@"`) {
		t.Error("the wrapper does not pass its arguments through")
	}
	// A missing binary has to say what to do about it, on stderr, with a non-zero
	// status — an agent that reads "not found" on stdout may parse it as a report.
	for _, want := range []string{"go install", "exit 127", ">&2"} {
		if !strings.Contains(script, want) {
			t.Errorf("the wrapper does not %q when reviewer is absent", want)
		}
	}
}

// A skill whose one command is not executable fails on its first use, with an error
// about permissions rather than about the review.
func TestTheWrapperIsExecutable(t *testing.T) {
	root := testfixture.Destination(t, "tiny-monorepo")
	install(t, root)

	info, err := os.Stat(filepath.Join(root, ".agent/skills/code-review/scripts/review.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("mode is %v, want it executable", info.Mode().Perm())
	}
}

// Handing a repository whose trunk is `master` a command that says `main` produces
// a command that fails.
func TestTheSkillNamesThisRepositorysTrunk(t *testing.T) {
	root := testfixture.Destination(t, "tiny-monorepo")
	testfixture.Git(t, root)("branch", "-m", "trunk")
	install(t, root)

	if skill := read(t, root, ".agent/skills/code-review/SKILL.md"); !strings.Contains(skill, "--base trunk") {
		t.Errorf("want the detected branch:\n%s", skill)
	}
}

// A section explaining a lane nobody configured is a section that teaches the
// reader to skim.
func TestTheModelLaneNoteAppearsOnlyWhenOneIsConfigured(t *testing.T) {
	withProvider := testfixture.Destination(t, "tiny-monorepo")
	anthropic, err := ProviderByID("anthropic")
	if err != nil {
		t.Fatal(err)
	}
	plan := BuildPlan(mustDetect(t, withProvider), releaseV, Options{Provider: anthropic})
	if _, err := Install(plan, releaseV); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, withProvider, ".agent/skills/code-review/SKILL.md"), "model lane") {
		t.Error("want the note when a provider is configured")
	}

	without := testfixture.Destination(t, "tiny-monorepo")
	install(t, without)
	if strings.Contains(read(t, without, ".agent/skills/code-review/SKILL.md"), "model lane") {
		t.Error("want no note when no provider is configured")
	}
}
