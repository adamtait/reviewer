// SPDX-License-Identifier: MIT

// Package codeowners reads a CODEOWNERS file and answers who owns a path.
//
// Patterns are matched with internal/glob, adapted to the rules CODEOWNERS
// takes from gitignore:
//
//   - a leading `/` anchors a pattern to the repository root, and so does a
//     `/` in the middle (`docs/*`, `**/logs`);
//   - a pattern with no `/` but a trailing one matches at any depth (`*.js`,
//     `apps/`);
//   - a pattern that names a directory owns everything under it (`/apps/github`
//     owns `apps/github/x`), unless its last segment holds a `*`: GitHub
//     documents that `docs/*` matches `docs/getting-started.md` and not
//     `docs/build-app/troubleshooting.md`;
//   - the last matching rule wins, and a rule with no owners leaves the paths
//     it matches unowned.
//
// Syntax GitHub documents as not working in CODEOWNERS (`!` negation, `[ ]`
// ranges, `\#` escapes) is refused with its line number, as is `?`, which the
// shared grammar does not have. A file that half-parses would give owners that
// GitHub would not, so a refusal is the safer answer: the caller says owners
// are not available.
package codeowners

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/adamtait/reviewer/internal/glob"
)

// Locations are where GitHub looks for a CODEOWNERS file, in the order it
// looks. The first one found is the only one used.
var Locations = []string{".github/CODEOWNERS", "CODEOWNERS", "docs/CODEOWNERS"}

// Rule is one line of the file that is not blank or a comment.
type Rule struct {
	// Line is the 1-based line number in the file.
	Line int
	// Pattern is the pattern as written.
	Pattern string
	// Owners are the owners as written, in order. Empty means the rule leaves
	// what it matches unowned.
	Owners []string

	// globs select a path if any of them matches it.
	globs []glob.Pattern
}

// Match reports whether the rule's pattern matches a path.
func (r Rule) Match(path string) bool {
	for _, g := range r.globs {
		if g.Match(path) {
			return true
		}
	}
	return false
}

// File is a parsed CODEOWNERS file.
type File struct {
	// Rules are in file order. The last one that matches a path decides.
	Rules []Rule
}

// Parse reads a CODEOWNERS file, refusing the whole file at the first line it
// cannot read.
func Parse(r io.Reader) (File, error) {
	f, problems, err := ParseAll(r)
	switch {
	case err != nil:
		return File{}, err
	case len(problems) > 0:
		return File{}, problems[0]
	}
	return f, nil
}

// ParseAll reads every line it can, and returns one problem for each line it
// cannot, in file order. err is set only when the file itself cannot be read.
//
// Parse is for answering who owns a path, where a half-read file is worse than
// none. ParseAll is for the lint, which has to name every bad line in one run
// and checks the rest of the file as GitHub applies it: GitHub documents that
// it skips a line with invalid syntax and uses the others.
func ParseAll(r io.Reader) (f File, problems []error, err error) {
	sc := bufio.NewScanner(r)
	// GitHub ignores a CODEOWNERS file over 3 MB; a line longer than the
	// scanner's default is not one worth reading.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for line := 1; sc.Scan(); line++ {
		fields := strings.Fields(sc.Text())
		// A field that starts with # starts a comment, whether the line does
		// or the comment follows the owners.
		for i, field := range fields {
			if strings.HasPrefix(field, "#") {
				fields = fields[:i]
				break
			}
		}
		if len(fields) == 0 {
			continue
		}
		rule, err := parseRule(line, fields[0], fields[1:])
		if err != nil {
			problems = append(problems, err)
			continue
		}
		f.Rules = append(f.Rules, rule)
	}
	if err := sc.Err(); err != nil {
		return File{}, nil, fmt.Errorf("reading CODEOWNERS: %w", err)
	}
	return f, problems, nil
}

func parseRule(line int, pattern string, owners []string) (Rule, error) {
	fail := func(why string) (Rule, error) {
		return Rule{}, fmt.Errorf("CODEOWNERS line %d: %q: %s", line, pattern, why)
	}
	switch {
	case strings.HasPrefix(pattern, "!"):
		return fail("negation with ! does not work in CODEOWNERS")
	case strings.ContainsAny(pattern, "[]"):
		return fail("character ranges with [ ] do not work in CODEOWNERS")
	case strings.HasPrefix(pattern, `\#`):
		return fail(`escaping a leading # with \ does not work in CODEOWNERS`)
	case strings.Contains(pattern, "?"):
		return fail("? is not supported by this tool's pattern syntax")
	}

	body := pattern
	anchored := strings.HasPrefix(body, "/")
	body = strings.TrimPrefix(body, "/")
	dir := strings.HasSuffix(body, "/")
	body = strings.TrimSuffix(body, "/")
	if body == "" {
		return fail("the pattern names no path")
	}
	if strings.Contains(body, "/") {
		anchored = true
	}
	if !anchored {
		body = "**/" + body
	}

	sources := []string{body + "/"}
	if !dir {
		sources = []string{body}
		last := body[strings.LastIndex(body, "/")+1:]
		if !strings.Contains(last, "*") {
			sources = append(sources, body+"/")
		}
	}
	if len(owners) == 0 {
		owners = nil
	}
	rule := Rule{Line: line, Pattern: pattern, Owners: owners}
	for _, s := range sources {
		g, err := glob.Compile(s)
		if err != nil {
			return fail(err.Error())
		}
		rule.globs = append(rule.globs, g)
	}
	return rule, nil
}

// Rule returns the rule that decides a path's owners: the last one that
// matches it. ok is false when no rule matches.
func (f File) Rule(path string) (rule Rule, ok bool) {
	for i := len(f.Rules) - 1; i >= 0; i-- {
		if f.Rules[i].Match(path) {
			return f.Rules[i], true
		}
	}
	return Rule{}, false
}

// Owners returns a path's owners, and whether any rule matched it. A path a
// rule matched can still have no owners: a rule with none leaves it unowned.
func (f File) Owners(path string) (owners []string, matched bool) {
	rule, ok := f.Rule(path)
	if !ok {
		return nil, false
	}
	return rule.Owners, true
}
