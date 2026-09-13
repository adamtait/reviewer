// SPDX-License-Identifier: MIT

// Command audithistory looks for things that must never have been in this
// repository: detail belonging to somewhere else, and files whose licence is
// incompatible with publishing under MIT.
//
// It reads git objects rather than the working tree. A secret deleted in the next
// commit is still in the history, and publishing a repository publishes its
// history — so "the working tree is clean" answers a question nobody asked.
//
// # What it looks for, and why by shape
//
// The patterns describe shapes — a private hostname, a home directory, a chat or
// tracker URL — rather than names. Two reasons, and the second is the one that
// matters:
//
//   - A name list has to be written down, and writing an organisation's internal
//     hostnames into a public repository to prove the repository contains no
//     internal hostnames is self-defeating (ADR-0004).
//   - A name list only finds what someone thought of. Shapes keep working for the
//     organisation nobody had in mind when this was written, which is every
//     organisation that ever forks it.
//
// Credentials are deliberately not covered: gitleaks scans the full history in
// its own CI job, against rules maintained by people who do that for a living.
// A second, worse implementation here would mostly produce disagreement.
//
// # Cost
//
// Every distinct blob is scanned once, not once per commit that contains it. A
// file untouched for two hundred commits is one scan, which is what makes running
// this on every pull request affordable.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// maxBlobBytes skips anything too large to be prose or source. A lockfile or a
// vendored bundle is megabytes of URLs and hashes, and scanning it produces
// nothing but noise.
const maxBlobBytes = 1 << 20

// pattern is one thing worth refusing, with the reason a reader needs to decide
// whether a hit is real.
type pattern struct {
	name string
	re   *regexp.Regexp
	// why says what the match would mean if it is genuine. Printed with every
	// finding, because an audit nobody can act on gets switched off.
	why string
}

// patterns are shapes, never names. See the package comment.
//
// Built from fragments so that this file does not itself contain a literal
// example of what it refuses — a guard whose source trips the guard is a guard
// people learn to ignore.
var patterns = []pattern{
	{
		name: "internal-host",
		// A hostname under a private or site-local suffix, in a position where a
		// hostname can actually appear: after a scheme, an @, or an opening quote
		// or delimiter, and followed by a port, a path, or the end of the token.
		//
		// The obvious pattern — any word ending in one of these suffixes — is
		// unusable. `.test` is a special-use TLD and also how every test file in
		// this repository is named, and `SOURCE.test(path)` is a method call; a
		// first draft of this reported 45 findings and every one was a filename.
		// An audit whose output is all noise is an audit that gets skipped.
		//
		// `test` is dropped from the suffix list entirely: it collides with
		// filenames and method calls in every language here, and a leaked
		// `something.test` hostname is not a real risk.
		// `.local` is the same problem one step down: it is a real suffix (mDNS),
		// and it is also how half the properties in any codebase are named —
		// `config.local`, `state.local`. It is kept only where it carries a port or
		// a path, which a property access never does.
		re: regexp.MustCompile(`(?i)(?:^|//|@|[\s"'` + "`" + `=<(\[,])` +
			`[a-z0-9][a-z0-9-]*(?:\.[a-z0-9-]+)*\.` +
			`(?:(?:internal|intranet|corp|lan|localdomain)(?:[:/][^\s"'` + "`" + `]*)?` +
			`|local[:/][^\s"'` + "`" + `]*)` +
			`(?:$|[\s"'` + "`" + `>)\],;])`),
		why: "a hostname that only resolves inside one network. It says where this " +
			"was written and is useless to everyone else.",
	},
	{
		name: "private-address",
		// RFC 1918 and carrier-grade NAT, with a port — a bare 10.0.0.1 is too
		// often a version string or an example.
		re:  regexp.MustCompile(`\b(?:10\.\d{1,3}|192\.168|172\.(?:1[6-9]|2\d|3[01])|100\.(?:6[4-9]|[7-9]\d|1[01]\d|12[0-7]))\.\d{1,3}(?:\.\d{1,3})?:\d{2,5}\b`),
		why: "a private network address and port, which names a machine nobody outside can reach.",
	},
	{
		name: "home-directory",
		// An absolute path under someone's home directory.
		re:  regexp.MustCompile(`(?:/Users/|/home/|C:\\Users\\)[a-zA-Z][a-zA-Z0-9._-]{1,31}[/\\]`),
		why: "an absolute path inside somebody's home directory, which names a person and a machine.",
	},
	{
		name: "internal-tooling",
		// Chat, tracker and wiki hosts. Assembled rather than written out so this
		// line is not itself a URL (see the package comment).
		re: regexp.MustCompile(`(?i)\b[a-z0-9-]+\.(?:` +
			strings.Join([]string{"slack", "atlassian", "jira", "confluence", "notion", "linear"}, "|") +
			`)\.(?:com|net|app|so)\b`),
		why: "a link into a private workspace. Anyone reading this cannot open it, and it " +
			"names the organisation that wrote the line.",
	},
	{
		name: "placeholder-scope",
		// The npm scope the original plan used before this became a personal
		// project. Kept because it is the one name this repository is known to have
		// carried, and it is generic enough to name safely.
		re:  regexp.MustCompile(`@team/`),
		why: "the placeholder organisation scope from the original plan, which this project dropped.",
	},
	{
		name: "copyleft-header",
		// A licence that would place obligations on an MIT distribution. Matched on
		// the identifier a file states about itself.
		re: regexp.MustCompile(`(?i)(SPDX-License-Identifier:\s*(?:A?GPL|LGPL|MPL|EPL|CDDL|SSPL)` +
			`|GNU (?:GENERAL|LESSER GENERAL|AFFERO GENERAL) PUBLIC LICENSE)`),
		why: "a file under a licence this project cannot redistribute under MIT (ADR-0001, ADR-0011).",
	},
}

// skipPath excludes paths whose matches are structural rather than real.
//
// Deliberately short. Every entry here is a place the audit does not look, so a
// long list is how an audit becomes decorative.
func skipPath(path string) bool {
	switch {
	case strings.Contains(path, "node_modules/"):
		// Third-party JavaScript. Its licences are reconciled by checklicenses
		// against the dependency graph, which is a better question than grep.
		return true
	case strings.HasSuffix(path, "package-lock.json"):
		// Registry URLs and integrity hashes, none of it written by anyone here.
		return true
	case path == "tools/audithistory/main.go":
		// This file describes the shapes it refuses.
		return true
	case path == "docs/implementation-plan.md":
		// The plan records the decision to drop the placeholder scope, and says so
		// using it. Excluding the record of a removal is not the same as excluding
		// the thing removed.
		return true
	}
	return false
}

type finding struct {
	blob    string
	path    string
	line    int
	pattern string
	why     string
	text    string
}

func main() {
	all := flag.Bool("all", false, "scan every object in the repository's history")
	base := flag.String("base", "", "scan only objects introduced since this ref")
	flag.Parse()

	if !*all && *base == "" {
		fmt.Fprintln(os.Stderr, "audithistory: pass --all to scan the whole history, "+
			"or --base REF to scan what a branch adds")
		os.Exit(2)
	}

	blobs, err := listBlobs(*all, *base)
	if err != nil {
		fmt.Fprintf(os.Stderr, "audithistory: %v\n", err)
		os.Exit(2)
	}

	var findings []finding
	scanned := 0
	for _, b := range blobs {
		if skipPath(b.path) {
			continue
		}
		body, err := blobContents(b.hash)
		if err != nil {
			fmt.Fprintf(os.Stderr, "audithistory: reading %s (%s): %v\n", b.path, b.hash, err)
			os.Exit(2)
		}
		scanned++
		findings = append(findings, scan(b, body)...)
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].path != findings[j].path {
			return findings[i].path < findings[j].path
		}
		return findings[i].line < findings[j].line
	})

	for _, f := range findings {
		fmt.Printf("%s:%d (%s, blob %s)\n  %s\n  %s\n",
			f.path, f.line, f.pattern, f.blob[:8], f.text, f.why)
	}

	fmt.Printf("%d blobs scanned, %s\n", scanned, plural(len(findings), "finding"))
	if len(findings) > 0 {
		// A finding is not automatically a leak — it is something a person has to
		// look at before this repository is published or a release is tagged.
		os.Exit(1)
	}
}

type blob struct {
	hash string
	path string
}

// listBlobs enumerates every distinct blob, with a path it was seen at.
//
// Distinct by hash: a file present in two hundred commits is one blob and one
// scan. `git rev-list --objects` prints the path alongside, which is what makes
// a finding say "docs/notes.md" rather than an object id nobody can look up.
func listBlobs(all bool, base string) ([]blob, error) {
	args := []string{"rev-list", "--objects"}
	if all {
		args = append(args, "--all")
	} else {
		args = append(args, "HEAD", "--not", base)
	}

	out, err := run(args...)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var blobs []blob
	for _, line := range strings.Split(out, "\n") {
		hash, path, ok := strings.Cut(line, " ")
		if !ok || path == "" || seen[hash] {
			// No path means a commit or a tree, not a blob.
			continue
		}
		seen[hash] = true
		blobs = append(blobs, blob{hash: hash, path: path})
	}
	return blobs, sizeFilter(&blobs)
}

// sizeFilter drops blobs too large to be worth reading, and anything that is not
// a blob at all.
func sizeFilter(blobs *[]blob) error {
	if len(*blobs) == 0 {
		return nil
	}
	var in strings.Builder
	for _, b := range *blobs {
		in.WriteString(b.hash)
		in.WriteByte('\n')
	}

	cmd := exec.Command("git", "cat-file", "--batch-check")
	cmd.Stdin = strings.NewReader(in.String())
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git cat-file --batch-check: %w", err)
	}

	keep := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 3 || fields[1] != "blob" {
			continue
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil || size > maxBlobBytes {
			continue
		}
		keep[fields[0]] = true
	}

	filtered := (*blobs)[:0]
	for _, b := range *blobs {
		if keep[b.hash] {
			filtered = append(filtered, b)
		}
	}
	*blobs = filtered
	return nil
}

func blobContents(hash string) (string, error) {
	return run("cat-file", "blob", hash)
}

// scan reports every pattern that matches, line by line.
//
// Binary content is skipped by looking for a NUL: a compiled binary matches the
// home-directory pattern constantly, through embedded build paths, and none of
// those are things anybody wrote.
func scan(b blob, body string) []finding {
	if strings.IndexByte(body, 0) >= 0 {
		return nil
	}
	var out []finding
	for i, line := range strings.Split(body, "\n") {
		for _, p := range patterns {
			if match := p.re.FindString(line); match != "" {
				out = append(out, finding{
					blob:    b.hash,
					path:    b.path,
					line:    i + 1,
					pattern: p.name,
					why:     p.why,
					text:    strings.TrimSpace(truncate(line, 160)),
				})
			}
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
