// SPDX-License-Identifier: MIT

package laneb

import (
	"context"
	"fmt"

	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/adamtait/reviewer/internal/config"
	"github.com/adamtait/reviewer/internal/diff"
)

// Gather reads everything Assemble needs from the repository.
//
// Separated from Assemble so that the assembler is a pure function of stated
// inputs and the reading is somewhere a reader can check what is read. Every path
// here is either in the diff or named by config: nothing walks the repository
// looking for something interesting to send.
func Gather(ctx context.Context, cfg config.Config, base string, files []diff.File) (Input, []string) {
	var warnings []string

	in := Input{Base: base, Changed: files}

	text, err := diff.Unified(ctx, cfg.Root, base, cfg.Analyzers.ContextLines)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("the model lane has no diff to send: %v", err))
		return in, warnings
	}
	in.Diff = text

	for _, rel := range cfg.Guidance {
		body, err := readUnder(cfg.Root, rel)
		if err != nil {
			// A guidance file the repository names and does not have is worth
			// saying: the review is quietly weaker than its configuration claims.
			warnings = append(warnings, fmt.Sprintf("guidance file %s: %v", rel, err))
			continue
		}
		in.Guidance = append(in.Guidance, Document{Path: filepath.ToSlash(rel), Body: body})
	}

	in.Docs = staleDocs(cfg, files)
	return in, warnings
}

// readUnder reads a repository-relative file, refusing anything that resolves
// outside the root.
//
// Guidance paths come from a configuration file that a pull request can change, so
// `../../../etc/passwd` as a guidance path is a way to have this tool read a file
// and send it to a model provider. Config validation already rejects absolute
// paths; this closes the relative route.
func readUnder(root, rel string) (string, error) {
	full := filepath.Join(root, filepath.FromSlash(rel))
	resolved, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	within, err := filepath.Rel(root, resolved)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("resolves outside the repository")
	}
	body, err := os.ReadFile(resolved)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// docReference matches a path inside a markdown link or inline code span. Narrow
// on purpose: a prose mention of a filename is not a reference, and treating one
// as such would make every document look affected by every change.
var docReference = regexp.MustCompile("(?:\\]\\(([^)\\s]+)\\)|`([^`\\n]+)`)")

// staleDocs finds documentation that describes source this change touches.
//
// Only the repository's own guidance documents and markdown files inside the
// changed files' own directories are examined — not every markdown file in the
// repository, which on a large codebase would be a full tree walk producing a list
// nobody reads.
func staleDocs(cfg config.Config, files []diff.File) []StaleDoc {
	changed := map[string]bool{}
	for _, f := range files {
		if f.Status == diff.StatusDeleted {
			continue
		}
		changed[filepath.ToSlash(f.Path)] = true
	}
	if len(changed) == 0 {
		return nil
	}

	var docs []StaleDoc
	for _, candidate := range docCandidates(cfg, files) {
		if changed[candidate] {
			// A document the change itself edits is being kept up to date, which is
			// the opposite of the problem.
			continue
		}
		body, err := readUnder(cfg.Root, candidate)
		if err != nil {
			continue
		}
		var refs []string
		seen := map[string]bool{}
		for _, m := range docReference.FindAllStringSubmatch(body, -1) {
			for _, ref := range refCandidates(candidate, firstNonEmpty(m[1], m[2])) {
				if !changed[ref] || seen[ref] {
					continue
				}
				seen[ref] = true
				refs = append(refs, ref)
				break
			}
		}
		if len(refs) > 0 {
			sort.Strings(refs)
			docs = append(docs, StaleDoc{Path: candidate, References: refs})
		}
	}
	return docs
}

// docCandidates are the markdown files worth examining: the ones config names as
// guidance, plus any beside a changed file.
func docCandidates(cfg config.Config, files []diff.File) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = filepath.ToSlash(p)
		if strings.ToLower(filepath.Ext(p)) != ".md" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}

	for _, g := range cfg.Guidance {
		add(g)
	}
	for _, f := range files {
		dir := filepath.Dir(filepath.FromSlash(f.Path))
		entries, err := os.ReadDir(filepath.Join(cfg.Root, dir))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			add(filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(out)
	return out
}

// refCandidates turns a reference as written into the repository-relative paths it
// could mean, best guess first.
//
// Two, because the two ways of writing one differ. A markdown link is relative to
// the document — `[order](../src/domain/order.ts)` — while a path in backticks is
// almost always written from the repository root, because that is how people talk
// about files. Resolving a backticked path against the document's directory turns
// `src/infra/http.ts` into `docs/src/infra/http.ts`, which matches nothing.
//
// The caller keeps whichever candidate is a file this change touched, so a guess
// that is wrong simply does not match rather than producing a false reference.
//
// Anything that is not a path into this repository — a URL, an anchor, a symbol
// name, a shell command — yields nothing.
func refCandidates(from, ref string) []string {
	ref = strings.TrimSpace(ref)
	switch {
	case ref == "",
		strings.Contains(ref, "://"),
		strings.HasPrefix(ref, "#"),
		strings.HasPrefix(ref, "mailto:"),
		strings.ContainsAny(ref, " \t"):
		return nil
	}
	if i := strings.IndexAny(ref, "#?"); i >= 0 {
		ref = ref[:i]
	}
	if ref == "" {
		return nil
	}
	if !strings.Contains(ref, "/") && filepath.Ext(ref) == "" {
		// A bare word in backticks is a symbol, not a file.
		return nil
	}
	if strings.HasPrefix(ref, "/") {
		return []string{strings.TrimPrefix(path.Clean(ref), "/")}
	}

	var out []string
	if fromDoc := path.Clean(path.Join(path.Dir(filepath.ToSlash(from)), ref)); !strings.HasPrefix(fromDoc, "..") {
		out = append(out, fromDoc)
	}
	if fromRoot := path.Clean(ref); !strings.HasPrefix(fromRoot, "..") && (len(out) == 0 || fromRoot != out[0]) {
		out = append(out, fromRoot)
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
