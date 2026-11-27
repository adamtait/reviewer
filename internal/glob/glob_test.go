// SPDX-License-Identifier: MIT

package glob

import (
	"errors"
	"testing"
)

func TestMatch(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		yes     []string
		no      []string
	}{
		// A plain pattern is the whole path, from the root.
		{
			pattern: "AGENTS.md",
			yes:     []string{"AGENTS.md"},
			no:      []string{"docs/AGENTS.md", "AGENTS.md.bak", "agents.md", "XAGENTS.md"},
		},
		{
			pattern: "src/main.go",
			yes:     []string{"src/main.go"},
			no:      []string{"main.go", "a/src/main.go", "src/main.go/x", "src/main_go"},
		},
		// `*` stays within one segment.
		{
			pattern: "*.go",
			yes:     []string{"main.go", ".go", "x.y.go"},
			no:      []string{"src/main.go", "main.go.txt", "main.goo"},
		},
		{
			pattern: "src/*.go",
			yes:     []string{"src/a.go", "src/.hidden.go"},
			no:      []string{"src/sub/a.go", "a.go", "src/a.ts"},
		},
		{
			pattern: "src/*",
			yes:     []string{"src/a", "src/a.go", "src/.env"},
			no:      []string{"src", "src/a/b", "lib/a"},
		},
		{
			pattern: "*auth*.*",
			yes:     []string{"auth.go", "oauth.ts", "authority.md", "x-auth-y.z"},
			no:      []string{"auth", "lib/auth.go", "aut.go"},
		},
		{
			// Several stars, where the first guess at what a star swallows is
			// wrong and the match has to go back.
			pattern: "*a*b*c",
			yes:     []string{"abc", "xaybzc", "aabbcc", "abcabc", "ab_c"},
			no:      []string{"acb", "ab", "abcx"},
		},
		// `**` crosses segments: none at the start or in the middle.
		{
			pattern: "**/auth/**",
			yes:     []string{"auth/x.go", "src/auth/x.go", "a/b/c/auth/d/e.go"},
			no:      []string{"auth", "src/auth", "src/oauth/x.go", "src/auth.go"},
		},
		{
			pattern: "**/*.sql",
			yes:     []string{"a.sql", "db/a.sql", "db/m/2026/a.sql"},
			no:      []string{"a.sql.bak", "db/sql", "db/a.SQL"},
		},
		{
			pattern: "**/SKILL.md",
			yes:     []string{"SKILL.md", "skills/x/SKILL.md", "a/b/c/d/SKILL.md"},
			no:      []string{"SKILL.mdx", "skills/x/skill.md"},
		},
		{
			pattern: "a/**/b",
			yes:     []string{"a/b", "a/x/b", "a/x/y/z/b"},
			no:      []string{"b", "a", "a/b/c", "x/a/b", "a/xb"},
		},
		{
			pattern: "**/migrations/**/*.sql",
			yes:     []string{"migrations/1.sql", "db/migrations/2026/1.sql"},
			no:      []string{"db/migrations/1.go", "db/migrations"},
		},
		// A trailing `**` is what is under a directory: one or more segments.
		{
			pattern: ".github/**",
			yes:     []string{".github/CODEOWNERS", ".github/workflows/ci.yml"},
			no:      []string{".github", "x/.github/ci.yml", ".githubx/a"},
		},
		{
			pattern: "**",
			yes:     []string{"a", "a/b", "a/b/c"},
			no:      []string{""},
		},
		// A trailing `/` is the same as a trailing `/**`.
		{
			pattern: "docs/",
			yes:     []string{"docs/a.md", "docs/a/b/c.md"},
			no:      []string{"docs", "a/docs/b.md", "docsx/a.md"},
		},
		{
			pattern: "**/k8s/",
			yes:     []string{"k8s/a.yaml", "deploy/k8s/a/b.yaml"},
			no:      []string{"k8s", "deploy/k8s"},
		},
		{
			pattern: "src/*/",
			yes:     []string{"src/a/b", "src/a/b/c"},
			no:      []string{"src/a", "src"},
		},
		// Match leaves `!` aside; MatchList gives it a meaning.
		{
			pattern: "!docs/**",
			yes:     []string{"docs/a.md"},
			no:      []string{"src/a.go"},
		},
		// A `!` anywhere but the start is a character like any other.
		{
			pattern: "a!b",
			yes:     []string{"a!b"},
			no:      []string{"ab"},
		},
	} {
		p, err := Compile(tc.pattern)
		if err != nil {
			t.Errorf("Compile(%q): %v", tc.pattern, err)
			continue
		}
		for _, path := range tc.yes {
			if !p.Match(path) {
				t.Errorf("%q does not match %q, and should", tc.pattern, path)
			}
		}
		for _, path := range tc.no {
			if p.Match(path) {
				t.Errorf("%q matches %q, and should not", tc.pattern, path)
			}
		}
	}
}

func TestMatchList(t *testing.T) {
	compile := func(patterns ...string) []Pattern {
		var out []Pattern
		for _, s := range patterns {
			out = append(out, MustCompile(s))
		}
		return out
	}
	for _, tc := range []struct {
		name     string
		patterns []Pattern
		path     string
		want     bool
	}{
		{"no patterns", nil, "a.go", false},
		{"no pattern matches", compile("*.ts"), "a.go", false},
		{"one matches", compile("*.ts", "*.go"), "a.go", true},
		{"an exclusion after a match", compile("src/**", "!src/gen/**"), "src/gen/a.go", false},
		{"an exclusion that does not apply", compile("src/**", "!src/gen/**"), "src/a.go", true},
		// The last match wins, in both directions.
		{"a match after an exclusion", compile("src/**", "!src/gen/**", "src/gen/keep.go"), "src/gen/keep.go", true},
		{"an exclusion before any match is overridden", compile("!src/gen/**", "src/**"), "src/gen/a.go", true},
		{"an exclusion alone selects nothing", compile("!src/**"), "src/a.go", false},
		{"an exclusion alone does not select the rest", compile("!src/**"), "lib/a.go", false},
	} {
		if got := MatchList(tc.patterns, tc.path); got != tc.want {
			t.Errorf("%s: MatchList(%v, %q) = %v, want %v", tc.name, tc.patterns, tc.path, got, tc.want)
		}
	}
}

func TestCompileRefusesWhatItCannotMean(t *testing.T) {
	for _, pattern := range []string{
		"",
		"!",
		"!!a",
		"/abs/path",
		"a//b",
		"a/b//",
		"//",
		"a**",
		"**b/c",
		"a/***/b",
		"src/?.go",
		"src/[a-z]*.go",
		"src/a]",
		`\#comment`,
		"a/../b",
		"./a",
	} {
		_, err := Compile(pattern)
		if err == nil {
			t.Errorf("Compile(%q) succeeded; want an error", pattern)
			continue
		}
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("Compile(%q): %v does not wrap ErrInvalid", pattern, err)
		}
	}
}

func TestStringAndNegated(t *testing.T) {
	p := MustCompile("!src/gen/")
	if p.String() != "!src/gen/" || !p.Negated() {
		t.Errorf("String %q, Negated %v", p.String(), p.Negated())
	}
	if MustCompile("src/").Negated() {
		t.Error("a pattern without ! is negated")
	}
}

func TestMustCompilePanicsOnAnInvalidPattern(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("no panic")
		}
	}()
	MustCompile("a**")
}
