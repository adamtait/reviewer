// SPDX-License-Identifier: MIT

// Command checkadrs enforces the ADR conventions described in ADR-0000: numbering,
// required metadata, index synchronisation, two-way supersession links, and the
// append-only rule that an Accepted ADR's body is never edited in place.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	fileRe     = regexp.MustCompile(`^(\d{4})-[a-z0-9]+(?:-[a-z0-9]+)*\.md$`)
	titleRe    = regexp.MustCompile(`(?m)^# ADR-(\d{4}) — (.+)$`)
	fieldRe    = regexp.MustCompile(`(?m)^- \*\*([A-Za-z ]+):\*\* (.*)$`)
	indexRowRe = regexp.MustCompile(`(?m)^\| \[(\d{4})\]\(([^)]+)\) \| (.+?) \| (.+?) \| (.+?) \|$`)
	adrRefRe   = regexp.MustCompile(`ADR-(\d{4})`)

	validStatus = map[string]bool{
		"Accepted": true, "Proposed": true, "Rejected": true, "Superseded": true,
	}
)

// adr is one parsed decision record.
type adr struct {
	num           int
	file          string
	title         string
	status        string
	implementedBy string
	supersedes    []int
	supersededBy  []int
	body          string // everything from the first "## " heading onward
}

func main() {
	dir := flag.String("dir", "docs/adr", "directory holding the ADR log")
	base := flag.String("base", "origin/main", "git ref to enforce the append-only rule against")
	flag.Parse()

	violations, count, err := check(*dir, *base)
	if err != nil {
		fmt.Fprintf(os.Stderr, "checkadrs: %v\n", err)
		os.Exit(2)
	}
	for _, v := range violations {
		fmt.Fprintln(os.Stderr, v)
	}
	if len(violations) > 0 {
		fmt.Fprintf(os.Stderr, "\n%d ADRs, %s\n", count, plural(len(violations), "violation"))
		os.Exit(1)
	}
	fmt.Printf("%d ADRs, index in sync, 0 violations\n", count)
}

func check(dir, base string) (violations []string, count int, err error) {
	adrs, violations, err := loadADRs(dir)
	if err != nil {
		return nil, 0, err
	}
	violations = append(violations, checkNumbering(adrs)...)
	violations = append(violations, checkSupersession(adrs)...)

	index, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		return nil, 0, fmt.Errorf("reading ADR index: %w", err)
	}
	violations = append(violations, checkIndex(adrs, string(index))...)
	violations = append(violations, checkAppendOnly(adrs, dir, base)...)

	sort.Strings(violations)
	return violations, len(adrs), nil
}

func loadADRs(dir string) ([]adr, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	var (
		adrs       []adr
		violations []string
	)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || name == "README.md" || name == "template.md" {
			continue
		}
		if !fileRe.MatchString(name) {
			violations = append(violations, fmt.Sprintf("%s: filename does not match NNNN-kebab-slug.md", name))
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, nil, err
		}
		a, errs := parseADR(name, string(content))
		violations = append(violations, errs...)
		adrs = append(adrs, a)
	}
	sort.Slice(adrs, func(i, j int) bool { return adrs[i].num < adrs[j].num })
	return adrs, violations, nil
}

// parseADR reads one record. It reports every problem it finds rather than
// stopping at the first, so a single run surfaces the whole fix list.
func parseADR(file, content string) (adr, []string) {
	a := adr{file: file, body: bodyOf(content)}
	var violations []string

	numFromName, _ := strconv.Atoi(file[:4])
	a.num = numFromName

	m := titleRe.FindStringSubmatch(content)
	switch {
	case m == nil:
		violations = append(violations, fmt.Sprintf("%s: missing or malformed '# ADR-NNNN — Title' heading", file))
	default:
		a.title = strings.TrimSpace(m[2])
		if headingNum, _ := strconv.Atoi(m[1]); headingNum != numFromName {
			violations = append(violations, fmt.Sprintf("%s: heading says ADR-%s but the filename says %04d", file, m[1], numFromName))
		}
	}

	fields := map[string]string{}
	for _, f := range fieldRe.FindAllStringSubmatch(content, -1) {
		fields[strings.TrimSpace(f[1])] = strings.TrimSpace(f[2])
	}
	a.status = fields["Status"]
	a.implementedBy = fields["Implemented by"]
	a.supersedes = refsIn(fields["Supersedes"])
	a.supersededBy = refsIn(fields["Superseded by"])

	if !validStatus[a.status] {
		violations = append(violations, fmt.Sprintf("%s: Status is %q, want one of Accepted, Proposed, Rejected, Superseded", file, a.status))
	}
	if a.implementedBy == "" {
		violations = append(violations, fmt.Sprintf("%s: missing 'Implemented by' field", file))
	}
	if a.status == "Superseded" && len(a.supersededBy) == 0 {
		violations = append(violations, fmt.Sprintf("%s: status is Superseded but no 'Superseded by' ADR is named", file))
	}
	return a, violations
}

// bodyOf returns the immutable part of a record: everything from the first
// section heading onward. The metadata block above it may change, which is how
// an Accepted ADR is marked Superseded without violating the append-only rule.
func bodyOf(content string) string {
	if i := strings.Index(content, "\n## "); i >= 0 {
		return content[i:]
	}
	return ""
}

func refsIn(field string) []int {
	var out []int
	for _, m := range adrRefRe.FindAllStringSubmatch(field, -1) {
		n, _ := strconv.Atoi(m[1])
		out = append(out, n)
	}
	return out
}

// checkNumbering requires numbers to be unique, not dense. The implementation
// plan reserves an ADR number per decision up front but lands each record with
// the PR that implements it, so the log legitimately has gaps at any moment: a
// number is an identifier, not a position.
func checkNumbering(adrs []adr) []string {
	var violations []string
	seen := map[int]string{}
	for _, a := range adrs {
		if prev, dup := seen[a.num]; dup {
			violations = append(violations, fmt.Sprintf("%s: duplicate ADR number %04d, already used by %s", a.file, a.num, prev))
			continue
		}
		seen[a.num] = a.file
	}
	return violations
}

func checkSupersession(adrs []adr) []string {
	byNum := map[int]adr{}
	for _, a := range adrs {
		byNum[a.num] = a
	}
	var violations []string
	for _, a := range adrs {
		for _, target := range a.supersededBy {
			other, ok := byNum[target]
			if !ok {
				violations = append(violations, fmt.Sprintf("%s: 'Superseded by' names ADR-%04d, which does not exist", a.file, target))
				continue
			}
			if !contains(other.supersedes, a.num) {
				violations = append(violations, fmt.Sprintf("%s: one-way supersession, ADR-%04d does not list this record under 'Supersedes'", a.file, target))
			}
		}
		for _, target := range a.supersedes {
			other, ok := byNum[target]
			if !ok {
				violations = append(violations, fmt.Sprintf("%s: 'Supersedes' names ADR-%04d, which does not exist", a.file, target))
				continue
			}
			if !contains(other.supersededBy, a.num) {
				violations = append(violations, fmt.Sprintf("%s: one-way supersession, ADR-%04d does not list this record under 'Superseded by'", a.file, target))
			}
		}
	}
	return violations
}

func checkIndex(adrs []adr, index string) []string {
	rows := map[int]struct{ file, title, status string }{}
	for _, m := range indexRowRe.FindAllStringSubmatch(index, -1) {
		n, _ := strconv.Atoi(m[1])
		rows[n] = struct{ file, title, status string }{m[2], strings.TrimSpace(m[3]), strings.TrimSpace(m[4])}
	}
	var violations []string
	for _, a := range adrs {
		row, ok := rows[a.num]
		if !ok {
			violations = append(violations, fmt.Sprintf("%s: not listed in docs/adr/README.md", a.file))
			continue
		}
		if row.title != a.title {
			violations = append(violations, fmt.Sprintf("%s: index title %q disagrees with the heading %q", a.file, row.title, a.title))
		}
		if row.status != a.status {
			violations = append(violations, fmt.Sprintf("%s: index status %q disagrees with the record's %q", a.file, row.status, a.status))
		}
		if row.file != a.file {
			violations = append(violations, fmt.Sprintf("%s: index links to %q", a.file, row.file))
		}
		delete(rows, a.num)
	}
	for num := range rows {
		violations = append(violations, fmt.Sprintf("docs/adr/README.md: index lists ADR-%04d, which has no file", num))
	}
	return violations
}

// checkAppendOnly is the rule ADR-0000 exists for: once Accepted, a record's body
// is immutable. It compares each record against the merge base rather than the tip
// so that a long-lived branch is judged against what it actually diverged from.
func checkAppendOnly(adrs []adr, dir, base string) []string {
	out, err := git("merge-base", "HEAD", base)
	mergeBase := strings.TrimSpace(out)
	if err != nil || mergeBase == "" {
		// No base ref: a fresh clone, a detached CI checkout, or the very first
		// branch. There is nothing to have edited, so this is not a violation.
		fmt.Fprintf(os.Stderr, "checkadrs: skipping the append-only check (no merge base with %s)\n", base)
		return nil
	}
	var violations []string
	for _, a := range adrs {
		path := filepath.Join(dir, a.file)
		previous, err := git("show", mergeBase+":"+path)
		if err != nil {
			continue // new file on this branch; nothing to compare against
		}
		before, _ := parseADR(a.file, previous)
		if before.status != "Accepted" {
			continue
		}
		if strings.TrimRight(before.body, "\n") != strings.TrimRight(a.body, "\n") {
			violations = append(violations, fmt.Sprintf("%s: accepted ADR modified outside its status block — supersede it with a new ADR instead", a.file))
		}
	}
	return violations
}

func git(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	return string(out), err
}

func contains(xs []int, want int) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
