// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/adamtait/reviewer/internal/codeowners"
	"github.com/adamtait/reviewer/internal/config"
	"github.com/adamtait/reviewer/internal/glob"
)

// neverAllowlist is the part of the never-allowlist compiled into the binary:
// files a coding agent reads as instructions, and the directories that decide
// how the repository is built, reviewed and merged (docs/merge-classes.md). A
// path here must stay owned whatever the repository's configuration says, so it
// is not configurable; the configuration can only add to it.
//
// `**/SKILL.md` also matches a SKILL.md at the root, so the root one needs no
// entry of its own. AGENTS.md, CLAUDE.md and GEMINI.md are the root files only,
// as the risk map has them.
var neverAllowlist = []string{
	"AGENTS.md", "CLAUDE.md", "GEMINI.md", "**/SKILL.md", ".cursorrules",
	".claude/", ".github/", ".review/", "docs/adr/",
}

// neverEntry is one path on the never-allowlist: a path no CODEOWNERS rule may
// leave unowned, because a model, an agent or the build reads it.
type neverEntry struct {
	// path is repository-relative, with a trailing `/` for a directory. A
	// compiled-in entry may be a pattern; one from configuration or a
	// //go:embed directive is a literal path.
	path string
	// source says where the entry came from, for the message.
	source string
}

// ownersViolation is one reason the lint fails. line is the CODEOWNERS line it
// is about, or 0 when no one line is.
type ownersViolation struct {
	line int
	text string
}

// ownersCheck is `reviewer owners check`, the structural CODEOWNERS lint the
// docs-only merge class depends on (docs/merge-classes.md).
//
// With 0 required approvals and code-owner review on, a path is reviewed only if
// CODEOWNERS gives it an owner, so a wrong CODEOWNERS file removes review with
// no visible sign. This command is what makes that visible: it fails unless the
// first rule owns `*`, the file owns itself, every rule with no owner is a
// directory, and nothing on the never-allowlist is unowned.
//
// It exits 1 on a failure, like `rules test`: it is a check with a right answer,
// required by the ruleset, not a review (ADR-0036; ADR-0009 governs reviews).
func ownersCheck(o options, stdout io.Writer, getenv func(string) string) error {
	root, err := filepath.Abs(o.root)
	if err != nil {
		return err
	}
	// A missing configuration file is fine: Resolve returns the defaults, and
	// only the compiled-in list and the default prompt directory apply. One that
	// cannot be read is misuse, as it is for `rules test`; either way the
	// required check does not pass.
	cfg, _, err := config.Resolve(o.root, o.config, getenv)
	if err != nil {
		return errUsage{err}
	}

	rel, raw, err := readCodeowners(root)
	if err != nil {
		return errCheckFailed{err}
	}

	entries := configEntries(cfg, root)
	embeds, embedErr := goEmbedEntries(root)
	entries = dedupeEntries(append(entries, embeds...))

	file, problems, err := codeowners.ParseAll(bytes.NewReader(raw))
	if err != nil {
		return errCheckFailed{fmt.Errorf("%s: %w", rel, err)}
	}
	violations := lintOwners(rel, file, problems, entries)
	if embedErr != nil {
		// Never report clean for a scan that did not happen: a //go:embed target
		// that was not looked for is one that was not checked.
		violations = append(violations, ownersViolation{
			text: fmt.Sprintf("could not finish looking for //go:embed targets: %v", embedErr)})
	}

	fmt.Fprintf(stdout, "%s, %s, %s\n", rel,
		count(len(file.Rules), "rule", "rules"),
		count(len(entries), "never-allowlist path", "never-allowlist paths"))
	if len(violations) == 0 {
		fmt.Fprintln(stdout, "  passed   `*` is owned first, the file owns itself, every unowned rule is a"+
			" directory, and no never-allowlist path is unowned")
		fmt.Fprintln(stdout, "ok")
		return nil
	}
	for _, v := range violations {
		where := "  -        "
		if v.line > 0 {
			where = fmt.Sprintf("  line %-4d", v.line)
		}
		fmt.Fprintf(stdout, "%s %s\n", where, v.text)
	}
	return errCheckFailed{fmt.Errorf("%s is not safe for the docs-only merge class: %s",
		rel, count(len(violations), "violation", "violations"))}
}

// readCodeowners reads the CODEOWNERS file GitHub would use: the first that
// exists of codeowners.Locations.
func readCodeowners(root string) (rel string, raw []byte, err error) {
	for _, loc := range codeowners.Locations {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(loc)))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return "", nil, err
		}
		return loc, raw, nil
	}
	// With no CODEOWNERS file, code-owner review requires nothing, so at 0
	// approvals every pull request merges unreviewed.
	return "", nil, fmt.Errorf("no CODEOWNERS file at %s; without one, nothing needs a code owner's review",
		strings.Join(codeowners.Locations, ", "))
}

// lintOwners applies every rule of the lint and returns every violation, so one
// run gives the whole fix list.
func lintOwners(rel string, file codeowners.File, problems []error, entries []neverEntry) []ownersViolation {
	var out []ownersViolation

	// GitHub skips a line it cannot read and applies the rest, so the rules
	// below are checked against the lines that parsed.
	for _, p := range problems {
		out = append(out, ownersViolation{text: p.Error()})
	}

	switch {
	case len(file.Rules) == 0:
		out = append(out, ownersViolation{
			text: "the file has no rules; the first must be `*` with an owner, so every path is owned"})
	case file.Rules[0].Pattern != "*" || len(file.Rules[0].Owners) == 0:
		first := file.Rules[0]
		out = append(out, ownersViolation{line: first.Line, text: fmt.Sprintf(
			"the first rule is %q; it must be `*` with an owner, so a path no later rule names is still owned",
			ruleText(first))})
	}

	// The file has to own itself, or a pull request could change who owns
	// everything without an owner's review. A CODEOWNERS file added where
	// GitHub looks first would replace this one, so those places count too.
	for _, loc := range codeowners.Locations {
		if owned, why := ownedBy(file, loc); !owned {
			text := fmt.Sprintf("CODEOWNERS does not own itself: %s %s", loc, why.text)
			if loc != rel {
				text = fmt.Sprintf("CODEOWNERS does not own %s, where a new CODEOWNERS file would replace this one: %s",
					loc, why.text)
			}
			out = append(out, ownersViolation{line: why.line, text: text})
		}
		if loc == rel {
			break
		}
	}

	// A rule with no owners is the allowlist. It has to name a directory, so the
	// allowlist is a list of places people read, not a pattern such as `*.md`
	// that also matches an agent's instruction file wherever one is added.
	for _, r := range file.Rules {
		if len(r.Owners) == 0 && !isDirectoryPattern(r.Pattern) {
			out = append(out, ownersViolation{line: r.Line, text: fmt.Sprintf(
				"%q has no owners and is not a directory; an unowned rule must end in /", r.Pattern)})
		}
	}

	for _, e := range entries {
		if probe, why, exposed := exposedPath(file, e); exposed {
			text := fmt.Sprintf("%s (%s) is on the never-allowlist, and %s %s", e.path, e.source, probe, why.text)
			out = append(out, ownersViolation{line: why.line, text: text})
		}
	}
	return out
}

// ownedBy reports whether a path has an owner and, when it has none, why: the
// rule that leaves it unowned, or that no rule matches it.
func ownedBy(file codeowners.File, p string) (bool, ownersViolation) {
	rule, ok := file.Rule(p)
	switch {
	case !ok:
		return false, ownersViolation{text: "is matched by no rule"}
	case len(rule.Owners) == 0:
		return false, ownersViolation{line: rule.Line, text: fmt.Sprintf("is left unowned by %q", rule.Pattern)}
	}
	return true, ownersViolation{}
}

// exposedPath looks for a path on the never-allowlist entry that CODEOWNERS
// leaves unowned, and returns the first it finds.
//
// This is a probe, not a proof. It does not decide whether some unowned rule
// could match some path the entry covers; it tries concrete paths and asks
// CODEOWNERS about each:
//
//   - the entry itself, with `**` dropped and each `*` replaced by "x", or for
//     a directory a file under it (`.claude/` tries `.claude/x`);
//   - for each rule with no owners, a path inside what that rule names, and the
//     entry's own probe paths inside it (`/docs/guides/` against `**/SKILL.md`
//     tries `docs/guides/SKILL.md`), kept only when the entry covers it;
//   - for an unowned rule that matches at any depth, the same inside each
//     directory entry (`guides/` against `.claude/` tries `.claude/guides/x`).
//
// A rule that exposes a never-allowlist path only at a deeper or
// differently-named path can get past it. The rule that every unowned rule is a
// directory is what keeps the set of unowned paths simple enough for a probe to
// be worth having.
func exposedPath(file codeowners.File, e neverEntry) (string, ownersViolation, bool) {
	dir := strings.HasSuffix(e.path, "/")
	base := instantiate(strings.TrimSuffix(e.path, "/"))
	own := []string{base}
	if dir {
		// Git tracks files, not directories, so a directory is owned when the
		// files under it are. The directory's own path is not probed: it would
		// be a file of that name, and asking about it fails the pattern the
		// setup guide recommends, `/docs/` unowned with `/docs/adr/` owned
		// again after it.
		own = []string{base + "/x"}
	}
	// A literal path from configuration may hold a character the grammar
	// refuses. It is still probed at its own paths; only the paths found
	// through unowned rules need the pattern to say whether they are covered.
	pattern, patternErr := glob.Compile(e.path)

	candidates := append([]string{}, own...)
	for _, r := range file.Rules {
		if len(r.Owners) != 0 {
			continue
		}
		body := strings.TrimSuffix(strings.TrimPrefix(r.Pattern, "/"), "/")
		inside := instantiate(body)
		bases := []string{inside}
		anchored := strings.HasPrefix(r.Pattern, "/") || strings.Contains(body, "/")
		if !anchored && dir {
			bases = append(bases, base+"/"+inside)
		}
		for _, b := range bases {
			candidates = append(candidates, b, b+"/x")
			for _, p := range own {
				candidates = append(candidates, b+"/"+p)
			}
		}
	}

	for i, c := range candidates {
		covered := i < len(own) || (patternErr == nil && pattern.Match(c))
		if !covered {
			continue
		}
		if owned, why := ownedBy(file, c); !owned {
			return c, why, true
		}
	}
	return "", ownersViolation{}, false
}

// instantiate turns a pattern into one concrete path it matches: a leading `/`
// is dropped, a `**` segment matches nothing (or "x" at the end, where it needs
// at least one segment), and each `*` becomes "x".
func instantiate(pattern string) string {
	pattern = strings.TrimPrefix(pattern, "/")
	var out []string
	segments := strings.Split(pattern, "/")
	for i, s := range segments {
		switch {
		case s == "":
			continue
		case s == "**" && i < len(segments)-1:
			continue
		case s == "**":
			out = append(out, "x")
		default:
			out = append(out, strings.ReplaceAll(s, "*", "x"))
		}
	}
	if len(out) == 0 {
		return "x"
	}
	return strings.Join(out, "/")
}

// isDirectoryPattern reports whether a CODEOWNERS pattern names a directory and
// everything under it: a trailing `/`, or a trailing `/**`, which the shared
// grammar treats as the same thing.
func isDirectoryPattern(p string) bool {
	return strings.HasSuffix(p, "/") || strings.HasSuffix(p, "/**")
}

func ruleText(r codeowners.Rule) string {
	return strings.Join(append([]string{r.Pattern}, r.Owners...), " ")
}

// configEntries is the never-allowlist the configuration adds: every guidance
// path, which the model lane reads into its prompt, and the prompt directory.
// The defaults are used when there is no configuration file, so the default
// prompt directory is always on the list.
func configEntries(cfg config.Config, root string) []neverEntry {
	var out []neverEntry
	add := func(p, source string, dir bool) {
		clean := path.Clean(filepath.ToSlash(p))
		// A path outside the repository cannot be in CODEOWNERS, and the model
		// lane refuses to read one, so there is nothing to check.
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
			return
		}
		if dir || strings.HasSuffix(p, "/") || isDir(root, clean) {
			clean += "/"
		}
		out = append(out, neverEntry{path: clean, source: source})
	}
	for _, g := range cfg.Guidance {
		add(g, "guidance", false)
	}
	if cfg.LaneB.PromptDir != "" {
		add(cfg.LaneB.PromptDir, "laneB.promptDir", true)
	}
	return out
}

// goEmbedEntries finds every path a //go:embed directive in the working tree
// names, as entries on the never-allowlist: an embedded file is built into the
// binary, so changing it changes what ships.
//
// Go only. Other build steps that read files (code generators, bundlers, a
// Dockerfile's COPY) are not detected; the setup guide says to keep their
// inputs off the allowlist by hand. Directories the go tool itself ignores
// (testdata, and names starting with . or _) are skipped, as are vendor and
// node_modules, which hold other people's code.
func goEmbedEntries(root string) ([]neverEntry, error) {
	var out []neverEntry
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (name == "vendor" || name == "node_modules" || name == "testdata" ||
				strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		relFile, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		relFile = filepath.ToSlash(relFile)
		directives, err := embedDirectives(p)
		if err != nil {
			return err
		}
		for _, dir := range directives {
			for _, pattern := range dir.patterns {
				source := fmt.Sprintf("//go:embed in %s:%d", relFile, dir.line)
				out = append(out, embedTargets(root, path.Dir(relFile), pattern, source)...)
			}
		}
		return nil
	})
	return out, err
}

// embedDirective is one //go:embed line.
type embedDirective struct {
	line     int
	patterns []string
}

// embedDirectives reads the //go:embed lines of one Go file.
func embedDirectives(file string) ([]embedDirective, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []embedDirective
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		rest, ok := strings.CutPrefix(text, "//go:embed")
		if !ok || rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
			continue
		}
		out = append(out, embedDirective{line: line, patterns: embedPatterns(rest)})
	}
	return out, sc.Err()
}

// embedPatterns splits a //go:embed line's arguments: space-separated, each
// bare, "double-quoted" or `back-quoted`, as the go command reads them. A
// quoted pattern that does not unquote is kept as written, so that it is still
// probed.
func embedPatterns(s string) []string {
	var out []string
	s = strings.TrimSpace(s)
	for s != "" {
		var p string
		switch s[0] {
		case '"', '`':
			end := strings.IndexByte(s[1:], s[0])
			if end < 0 {
				p, s = s, ""
				break
			}
			p, s = s[:end+2], s[end+2:]
			if u, err := strconv.Unquote(p); err == nil {
				p = u
			}
		default:
			end := strings.IndexAny(s, " \t")
			if end < 0 {
				end = len(s)
			}
			p, s = s[:end], s[end:]
		}
		out = append(out, strings.TrimPrefix(p, "all:"))
		s = strings.TrimSpace(s)
	}
	return out
}

// embedTargets turns one //go:embed pattern, relative to its file's directory,
// into entries: the files and directories it matches in the working tree. A
// pattern that matches nothing (a build output not yet built) is probed as
// written, as a directory, since that is the more cautious reading.
func embedTargets(root, dir, pattern, source string) []neverEntry {
	joined := path.Join(dir, pattern)
	if strings.HasPrefix(joined, "../") || joined == ".." {
		return nil
	}
	matches, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(joined)))
	if len(matches) == 0 {
		if strings.ContainsAny(pattern, "*?[") {
			// A pattern that matches nothing does not build, so it embeds
			// nothing either.
			return nil
		}
		return []neverEntry{{path: joined + "/", source: source}}
	}
	var out []neverEntry
	for _, m := range matches {
		rel, err := filepath.Rel(root, m)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if isDir(root, rel) {
			rel += "/"
		}
		out = append(out, neverEntry{path: rel, source: source})
	}
	return out
}

// dedupeEntries keeps the first of entries with the same path, so a guidance
// path that is also compiled in, or a file several directives embed, is
// reported once. The compiled-in list comes first.
func dedupeEntries(extra []neverEntry) []neverEntry {
	var out []neverEntry
	seen := map[string]bool{}
	for _, p := range neverAllowlist {
		seen[p] = true
		out = append(out, neverEntry{path: p, source: "compiled in"})
	}
	var rest []neverEntry
	for _, e := range extra {
		if !seen[e.path] {
			seen[e.path] = true
			rest = append(rest, e)
		}
	}
	sort.SliceStable(rest, func(i, j int) bool { return rest[i].path < rest[j].path })
	return append(out, rest...)
}

func isDir(root, rel string) bool {
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil && info.IsDir()
}
