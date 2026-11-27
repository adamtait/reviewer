// SPDX-License-Identifier: MIT

package codeowners

import (
	"reflect"
	"strings"
	"testing"
)

func parse(t *testing.T, text string) File {
	t.Helper()
	f, err := Parse(strings.NewReader(text))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return f
}

// Each case is one of the examples in GitHub's documentation of CODEOWNERS
// ("About code owners", "Example of a CODEOWNERS file"), on its own after a
// global default, with the paths the documentation's comment says it owns and
// does not.
func TestGitHubsDocumentedExamples(t *testing.T) {
	const global = "* @global-owner1 @global-owner2\n"
	for _, tc := range []struct {
		name  string
		rules string
		owned map[string][]string
	}{
		{
			name:  "a global default owns everything",
			rules: "",
			owned: map[string][]string{
				"README.md":          {"@global-owner1", "@global-owner2"},
				"src/deep/nested.go": {"@global-owner1", "@global-owner2"},
			},
		},
		{
			name:  "*.js: every JS file, at any depth, and only JS files",
			rules: "*.js    @js-owner #This is an inline comment.\n",
			owned: map[string][]string{
				"app.js":         {"@js-owner"},
				"src/web/app.js": {"@js-owner"},
				"app.ts":         {"@global-owner1", "@global-owner2"},
			},
		},
		{
			name:  "an email address as an owner",
			rules: "*.go docs@example.com\n",
			owned: map[string][]string{"cmd/main.go": {"docs@example.com"}},
		},
		{
			name:  "a team as an owner",
			rules: "*.txt @octo-org/octocats\n",
			owned: map[string][]string{"notes/a.txt": {"@octo-org/octocats"}},
		},
		{
			name:  "/build/logs/: the directory at the root and its subdirectories",
			rules: "/build/logs/ @doctocat\n",
			owned: map[string][]string{
				"build/logs/a.log":       {"@doctocat"},
				"build/logs/2026/b.log":  {"@doctocat"},
				"src/build/logs/a.log":   {"@global-owner1", "@global-owner2"},
				"build/logs-old/a.log":   {"@global-owner1", "@global-owner2"},
				"build/other/logs/a.log": {"@global-owner1", "@global-owner2"},
			},
		},
		{
			name:  "docs/*: files in docs, not further nested files",
			rules: "docs/* docs@example.com\n",
			owned: map[string][]string{
				"docs/getting-started.md":           {"docs@example.com"},
				"docs/build-app/troubleshooting.md": {"@global-owner1", "@global-owner2"},
			},
		},
		{
			name:  "apps/: an apps directory anywhere",
			rules: "apps/ @octocat\n",
			owned: map[string][]string{
				"apps/a.go":            {"@octocat"},
				"src/apps/a/b.go":      {"@octocat"},
				"deeply/nested/apps/x": {"@octocat"},
				"apps.go":              {"@global-owner1", "@global-owner2"},
			},
		},
		{
			name:  "/docs/: the docs directory at the root and its subdirectories",
			rules: "/docs/ @doctocat\n",
			owned: map[string][]string{
				"docs/a.md":     {"@doctocat"},
				"docs/x/y.md":   {"@doctocat"},
				"src/docs/a.md": {"@global-owner1", "@global-owner2"},
			},
		},
		{
			name:  "/scripts/: two owners",
			rules: "/scripts/ @doctocat @octocat\n",
			owned: map[string][]string{"scripts/build.sh": {"@doctocat", "@octocat"}},
		},
		{
			name:  "**/logs: any file in a logs directory",
			rules: "**/logs @octocat\n",
			owned: map[string][]string{
				"build/logs/a":         {"@octocat"},
				"scripts/logs/b":       {"@octocat"},
				"deeply/nested/logs/c": {"@octocat"},
				"logs/d":               {"@octocat"},
				"build/logsx/e":        {"@global-owner1", "@global-owner2"},
			},
		},
		{
			// The later rule has no owners, so it overrides the earlier one
			// and leaves apps/github unowned.
			name:  "/apps/ owned, then /apps/github with no owners",
			rules: "/apps/ @octocat\n/apps/github\n",
			owned: map[string][]string{
				"apps/a.go":          {"@octocat"},
				"apps/github/a.go":   nil,
				"apps/github/x/y.go": nil,
			},
		},
		{
			name:  "/apps/ owned, then /apps/github with its own owner",
			rules: "/apps/ @octocat\n/apps/github @doctocat\n",
			owned: map[string][]string{
				"apps/a.go":        {"@octocat"},
				"apps/github/a.go": {"@doctocat"},
			},
		},
	} {
		f := parse(t, global+tc.rules)
		for path, want := range tc.owned {
			got, matched := f.Owners(path)
			if !matched {
				t.Errorf("%s: %q matched no rule; the global default should match everything", tc.name, path)
			}
			if !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
				t.Errorf("%s: %q is owned by %v, want %v", tc.name, path, got, want)
			}
		}
	}
}

func TestTheLastMatchingRuleWins(t *testing.T) {
	f := parse(t, "*.go @go\n/internal/ @internal\n*.go @go-again\n")
	if got, _ := f.Owners("internal/a.go"); !reflect.DeepEqual(got, []string{"@go-again"}) {
		t.Errorf("internal/a.go: %v, want the last rule's owner", got)
	}
	if got, _ := f.Owners("internal/a.md"); !reflect.DeepEqual(got, []string{"@internal"}) {
		t.Errorf("internal/a.md: %v", got)
	}
}

// matched separates "no rule" from "a rule with no owners", which a lint needs
// and the owner count does not.
func TestNoRuleIsNotTheSameAsAnUnownedRule(t *testing.T) {
	f := parse(t, "/src/ @team\n/src/vendor/\n")
	if owners, matched := f.Owners("README.md"); matched || owners != nil {
		t.Errorf("README.md: %v, %v; want no match", owners, matched)
	}
	if owners, matched := f.Owners("src/vendor/x.go"); !matched || len(owners) != 0 {
		t.Errorf("src/vendor/x.go: %v, %v; want matched with no owners", owners, matched)
	}
	if rule, ok := f.Rule("src/vendor/x.go"); !ok || rule.Line != 2 || rule.Pattern != "/src/vendor/" {
		t.Errorf("Rule: %+v, %v; want line 2", rule, ok)
	}
}

func TestRulesAreKeptInOrderWithTheirLines(t *testing.T) {
	f := parse(t, "# header\n\n*       @a\n   # indented comment\n/docs/  @b @c   # trailing\n/vendor/\n")
	var got []string
	for _, r := range f.Rules {
		got = append(got, strings.TrimSpace(strings.Join(append([]string{r.Pattern}, r.Owners...), " ")))
	}
	want := []string{"* @a", "/docs/ @b @c", "/vendor/"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rules %q, want %q", got, want)
	}
	for i, line := range []int{3, 5, 6} {
		if f.Rules[i].Line != line {
			t.Errorf("rule %d is on line %d, want %d", i, f.Rules[i].Line, line)
		}
	}
	if f.Rules[2].Owners != nil {
		t.Errorf("a rule with no owners has %v", f.Rules[2].Owners)
	}
}

func TestPathsAreCaseSensitive(t *testing.T) {
	f := parse(t, "/Docs/ @a\n")
	if _, matched := f.Owners("docs/x.md"); matched {
		t.Error("/Docs/ matched docs/x.md")
	}
}

func TestAnEmptyFileOwnsNothing(t *testing.T) {
	f := parse(t, "# only a comment\n\n")
	if len(f.Rules) != 0 {
		t.Fatalf("rules: %+v", f.Rules)
	}
	if _, matched := f.Owners("a.go"); matched {
		t.Error("an empty file matched a path")
	}
}

func TestParseRefusesWhatCODEOWNERSDoesNotSupport(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
	}{
		{"negation", "* @a\n!docs/ @b\n", "line 2"},
		{"a character range", "* @a\n\n/src/[ab]/ @b\n", "line 3"},
		{"a closing bracket alone", "src/a] @b\n", "line 1"},
		{"an escaped #", "* @a\n\\#notes @b\n", "line 2"},
		{"a question mark", "src/?.go @b\n", "line 1"},
		{"a slash alone", "/ @b\n", "line 1"},
		{"** inside a segment", "src/a**/ @b\n", "line 1"},
	} {
		_, err := Parse(strings.NewReader(tc.text))
		if err == nil {
			t.Errorf("%s: parsed; want an error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v does not say %q", tc.name, err, tc.want)
		}
	}
}
