// SPDX-License-Identifier: MIT

package laneb

import (
	"context"
	"fmt"
	"io"

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
func Gather(ctx context.Context, cfg config.Config, base string, staged bool, files []diff.File) (Input, []string) {
	var warnings []string

	in := Input{Base: base, Staged: staged, Changed: files}

	text, err := diff.Unified(ctx, cfg.Root, base, staged, cfg.Analyzers.ContextLines)
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

// maxDocumentBytes bounds a single file this package will read.
//
// Applied while reading, not after. The prompt's own budget clips the text
// afterwards, but by then the whole file is in memory — and a 2 GB markdown file,
// or a symlink to /dev/zero, kills the process with an out-of-memory error rather
// than the warning ADR-0013 promises.
const maxDocumentBytes = 1 << 20

// readUnder reads a repository-relative file, refusing anything that is not a
// regular file inside the repository.
//
// Everything this function reads ends up in a prompt sent to a third party, and
// every path it is given is repository-controlled: guidance paths come from a
// configuration file a pull request can change, and prompt overrides are files in
// the tree. So "inside the repository" has to be true of the *file*, not of the
// path as written.
//
// Symlinks are resolved before the containment check, which the first version of
// this function did not do. A branch adding `docs/leak.md -> /proc/self/environ`
// and one line of config could otherwise put every credential in this process's
// environment — GITHUB_TOKEN and the model API key included — into the prompt, and
// the same trick on `.review/prompts/review.md` could make any file on the machine
// the system prompt.
func readUnder(root, rel string) (string, error) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	full := filepath.Join(realRoot, filepath.FromSlash(rel))

	// Lexical first, so an obvious `../..` is refused without touching the disk.
	if err := within(realRoot, full); err != nil {
		return "", err
	}
	// Then the real path. A symlink anywhere along the way is followed here and
	// checked again, which is the part that matters.
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	if err := within(realRoot, resolved); err != nil {
		return "", err
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		// A device, a fifo or a socket. /dev/zero is a regular-looking guidance
		// file that never ends.
		return "", fmt.Errorf("is not a regular file")
	}
	if info.Size() > maxDocumentBytes {
		return "", fmt.Errorf("is %d bytes, over the %d-byte limit for a document",
			info.Size(), maxDocumentBytes)
	}

	f, err := os.Open(resolved)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// Bounded even so: the size checked above and the size read can differ if the
	// file grows between the two.
	body, err := io.ReadAll(io.LimitReader(f, maxDocumentBytes))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func within(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("resolves outside the repository")
	}
	return nil
}

// docReference matches a path inside a markdown link or inline code span. Narrow
// on purpose: a prose mention of a filename is not a reference, and treating one
// as such would make every document look affected by every change.
var docReference = regexp.MustCompile("(?:\\]\\(([^)\\s]+)\\)|`([^`\\n]+)`)")

// maxDocCandidates bounds how many documents are examined for references. A
// directory with two hundred markdown files in it is not a repository whose
// documentation this section can usefully summarise.
const maxDocCandidates = 32

// staleDocs finds documentation that describes source this change touches.
//
// The documents examined are the repository's own guidance files and the markdown
// files sitting in the same directories as the changed files — not every markdown
// file in the repository, which on a large codebase would be a full tree walk
// producing a list nobody reads.
//
// That second set is read without being named by config, which is worth being
// explicit about: it is bounded in count, each file is read through readUnder (so
// it is a regular file inside the repository and under a size cap), and only the
// paths it *references* are reported — never its contents.
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
	//nolint:gocritic // the bound is applied at the end, once the set is known
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
	if len(out) > maxDocCandidates {
		out = out[:maxDocCandidates]
	}
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
