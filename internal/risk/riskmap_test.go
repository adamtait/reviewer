// SPDX-License-Identifier: MIT

package risk

import (
	"reflect"
	"strings"
	"testing"
)

// The defaults are the design's table, character for character. Changing one
// is a change to what every repository's labels mean, and should be made in
// the design first.
func TestTheDefaultsAreTheDesignsTable(t *testing.T) {
	want := map[Label][]string{
		LabelAuth:  {"**/auth/**", "**/*auth*.*", "**/permissions/**", "**/crypto/**", "**/payments/**", "**/billing/**"},
		LabelData:  {"**/migrations/**", "**/*.sql", "**/schema.*", "**/prisma/**"},
		LabelCI:    {".github/**", ".review/**", "AGENTS.md", "CLAUDE.md", "GEMINI.md", "**/SKILL.md", ".claude/**", ".cursorrules"},
		LabelDeps:  {"**/package.json", "**/package-lock.json", "**/pnpm-lock.yaml", "**/yarn.lock", "**/go.mod", "**/go.sum", "**/Cargo.toml", "**/Cargo.lock", "**/requirements*.txt", "**/pyproject.toml", "**/poetry.lock"},
		LabelInfra: {"**/*.tf", "**/k8s/**", "**/helm/**", "**/Dockerfile*", "**/docker-compose*.yml"},
	}
	if !reflect.DeepEqual(defaultPatterns, want) {
		t.Errorf("defaults:\n%v\nwant:\n%v", defaultPatterns, want)
	}
}

func TestTheDefaultsMatch(t *testing.T) {
	m := Defaults()
	for _, tc := range []struct {
		path string
		want []Label
	}{
		{"src/auth/session.go", []Label{LabelAuth}},
		{"lib/oauth.ts", []Label{LabelAuth}},
		{"internal/billing/invoice.go", []Label{LabelAuth}},
		{"db/migrations/0001_init.sql", []Label{LabelData}},
		{"prisma/schema.prisma", []Label{LabelData}},
		{".github/workflows/ci.yml", []Label{LabelCI}},
		{".review/config.yaml", []Label{LabelCI}},
		{"AGENTS.md", []Label{LabelCI}},
		{"skills/lint/SKILL.md", []Label{LabelCI}},
		{".claude/settings.json", []Label{LabelCI}},
		{"web/package.json", []Label{LabelDeps}},
		{"go.sum", []Label{LabelDeps}},
		{"requirements-dev.txt", []Label{LabelDeps}},
		{"infra/main.tf", []Label{LabelInfra}},
		{"Dockerfile", []Label{LabelInfra}},
		{"deploy/docker-compose.prod.yml", []Label{LabelInfra}},
		// AGENTS.md is a root file in the table; only SKILL.md is matched at
		// any depth.
		{"docs/AGENTS.md", nil},
		{"README.md", nil},
		{"src/main.go", nil},
	} {
		var got []Label
		for _, l := range PathLabels {
			if m.Match(l, tc.path) {
				got = append(got, l)
			}
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestParseAddsToTheDefaults(t *testing.T) {
	m, warnings, err := Parse([]byte(`version: 1
add:
  risk/auth: ["src/session/**", "src/tokens/**"]
  risk/data: ["db/**"]
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings: %q", warnings)
	}
	for _, tc := range []struct {
		label Label
		path  string
		want  bool
	}{
		{LabelAuth, "src/session/store.go", true},
		{LabelAuth, "src/tokens/jwt.go", true},
		{LabelAuth, "src/auth/x.go", true}, // a default, still there
		{LabelData, "db/seed.rb", true},
		{LabelData, "src/session/store.go", false},
		{LabelCI, "src/session/store.go", false},
	} {
		if got := m.Match(tc.label, tc.path); got != tc.want {
			t.Errorf("%s %s: %v, want %v", tc.label, tc.path, got, tc.want)
		}
	}
	if got := m.Added(LabelAuth); !reflect.DeepEqual(got, []string{"src/session/**", "src/tokens/**"}) {
		t.Errorf("Added: %q", got)
	}
}

// A `!` in risk.yaml narrows the file's own patterns. It cannot remove a
// default: the design says the file can add and not remove.
func TestAnExclusionCannotRemoveADefault(t *testing.T) {
	m, _, err := Parse([]byte(`version: 1
add:
  risk/auth: ["src/**", "!src/auth/**", "!src/generated/**"]
`))
	if err != nil {
		t.Fatal(err)
	}
	if !m.Match(LabelAuth, "src/auth/login.go") {
		t.Error("an exclusion in risk.yaml removed a default match")
	}
	if m.Match(LabelAuth, "src/generated/types.go") {
		t.Error("an exclusion in risk.yaml did not narrow the file's own pattern")
	}
	if !m.Match(LabelAuth, "src/handler.go") {
		t.Error("the file's own pattern did not match")
	}
}

func TestParseWarnsAboutEntriesWithNoEffect(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, want string
	}{
		{"a label with no patterns", "version: 1\nadd:\n  risk/auth: []\n", "lists no patterns"},
		{"a pattern that is already a default", "version: 1\nadd:\n  risk/data: [\"**/*.sql\"]\n", "already a pattern"},
		{"a pattern listed twice", "version: 1\nadd:\n  risk/data: [\"db/**\", \"db/**\"]\n", "already a pattern"},
		{"no add at all", "version: 1\n", "adds no patterns"},
	} {
		_, warnings, err := Parse([]byte(tc.yaml))
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], tc.want) {
			t.Errorf("%s: warnings %q, want one containing %q", tc.name, warnings, tc.want)
		}
	}
}

func TestAnInvalidFileLeavesTheDefaultsAlone(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, want string
	}{
		{"empty", "", "version: 1"},
		{"comments only", "# nothing yet\n", "version: 1"},
		{"no version", "add:\n  risk/auth: [\"x/**\"]\n", "version"},
		{"another version", "version: 2\nadd:\n  risk/auth: [\"x/**\"]\n", "version 2"},
		{"a remove key", "version: 1\nadd:\n  risk/auth: [\"x/**\"]\nremove:\n  risk/auth: [\"**/auth/**\"]\n", "remove"},
		{"a misspelt key", "version: 1\nadds:\n  risk/auth: [\"x/**\"]\n", "adds"},
		{"an unknown label", "version: 1\nadd:\n  risk/auth: [\"x/**\"]\n  risk/secrets: [\"x/**\"]\n", "risk/secrets"},
		{"a label that is not a path label", "version: 1\nadd:\n  risk/wide: [\"x/**\"]\n", "risk/wide"},
		{"a label without its prefix", "version: 1\nadd:\n  auth: [\"x/**\"]\n", `"auth"`},
		{"an invalid pattern", "version: 1\nadd:\n  risk/auth: [\"x/**\", \"src/[a-z]/**\"]\n", "not supported"},
		{"patterns as a string", "version: 1\nadd:\n  risk/auth: \"x/**\"\n", "cannot unmarshal"},
		{"a label twice", "version: 1\nadd:\n  risk/auth: [\"x/**\"]\n  risk/auth: [\"y/**\"]\n", "already defined"},
		{"two documents", "version: 1\nadd:\n  risk/auth: [\"x/**\"]\n---\nversion: 1\n", "more than one"},
		{"not YAML", "version: [1\n", "yaml"},
	} {
		m, warnings, err := Parse([]byte(tc.yaml))
		if err == nil {
			t.Errorf("%s: parsed; want an error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v does not mention %q", tc.name, err, tc.want)
		}
		if warnings != nil {
			t.Errorf("%s: warnings %q alongside an error", tc.name, warnings)
		}
		if m.Match(LabelAuth, "x/a.go") || len(m.Added(LabelAuth)) != 0 {
			t.Errorf("%s: an addition from a rejected file is in the map", tc.name)
		}
		if !m.Match(LabelAuth, "src/auth/a.go") {
			t.Errorf("%s: the defaults are not in the map", tc.name)
		}
	}
}
