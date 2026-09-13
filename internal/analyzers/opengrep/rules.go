// SPDX-License-Identifier: MIT

package opengrep

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Pack is a repository's rule directory, loaded and checked.
//
// The loading is this tool's job; deciding whether a pattern matches is Opengrep's
// (ADR-0011). The split matters for what can be verified without the binary
// installed: a malformed pack, a duplicate id and a rule with no test case are all
// findable here, and all three are mistakes someone makes while writing their
// third rule.
type Pack struct {
	// Dir is the directory the pack was loaded from.
	Dir string
	// Rules are every rule in the pack, in file then declaration order.
	Rules []Rule
	// Problems are reasons the pack is not fit to run. A pack with problems is
	// reported rather than silently half-loaded: a rule file that failed to parse
	// means a convention nobody is checking, and a run that quietly skipped it
	// would look identical to one where the rule passed.
	Problems []string
}

// Rule is one rule as this tool needs to see it.
type Rule struct {
	// ID is the rule's own id, as written.
	ID string
	// ReportedID is what a finding from it carries: the declared
	// `metadata.reviewer-rule-id`, or ID under this tool's namespace.
	ReportedID string
	// File is the rule file's name, relative to the pack directory.
	File string
	// Severity as written, uppercased.
	Severity string
	// Message as written.
	Message string
	// TestFile is the Opengrep test case file beside the rule, if there is one.
	TestFile string
	// Positive and Negative count the annotated cases naming this rule: lines
	// marked `ruleid:` that must match, and `ok:` that must not.
	Positive, Negative int
}

// ruleFile is the subset of a rule file's YAML this tool reads. Everything else —
// patterns, languages, path filters — is Opengrep's to interpret, and is
// deliberately not modelled here: a struct that listed every pattern form would
// have to be revised every time Opengrep gained one.
type ruleFile struct {
	Rules []struct {
		ID       string `yaml:"id"`
		Severity string `yaml:"severity"`
		Message  string `yaml:"message"`
		Metadata struct {
			ReviewerRuleID string `yaml:"reviewer-rule-id"`
		} `yaml:"metadata"`
	} `yaml:"rules"`
}

// LoadPack reads every rule file in a directory, along with the test cases beside
// them. A missing directory yields an empty pack and no problem: a repository that
// has written no rules is where every repository starts.
func LoadPack(root, rulesDir string) (Pack, error) {
	dir := rulesDir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, filepath.FromSlash(rulesDir))
	}
	pack := Pack{Dir: dir}

	names, err := RuleFiles(root, rulesDir)
	if err != nil {
		return pack, err
	}

	seen := map[string]string{}
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			pack.Problems = append(pack.Problems, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		var parsed ruleFile
		if err := yaml.Unmarshal(raw, &parsed); err != nil {
			pack.Problems = append(pack.Problems, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		if len(parsed.Rules) == 0 {
			pack.Problems = append(pack.Problems,
				fmt.Sprintf("%s: no rules in this file", name))
			continue
		}

		for i, r := range parsed.Rules {
			id := strings.TrimSpace(r.ID)
			if id == "" {
				pack.Problems = append(pack.Problems,
					fmt.Sprintf("%s: rules[%d] has no id", name, i))
				continue
			}
			if where, duplicate := seen[id]; duplicate {
				// Opengrep refuses a duplicate id too, but it refuses the whole
				// run — so the same mistake would take every other rule down with
				// it, and the message would not say which two files collided.
				pack.Problems = append(pack.Problems,
					fmt.Sprintf("%s: rule id %q is already declared in %s", name, id, where))
				continue
			}
			seen[id] = name

			rule := Rule{
				ID:         id,
				ReportedID: reportedID(id, r.Metadata.ReviewerRuleID),
				File:       name,
				Severity:   strings.ToUpper(strings.TrimSpace(r.Severity)),
				Message:    strings.TrimSpace(r.Message),
			}
			rule.TestFile, rule.Positive, rule.Negative = testCases(dir, name, id)
			pack.Rules = append(pack.Rules, rule)
		}
	}
	return pack, nil
}

// reportedID is what a finding carries, mirroring ruleID's logic for a rule read
// from disk rather than from a report.
func reportedID(id, declared string) string {
	if declared = strings.TrimSpace(declared); declared != "" {
		return declared
	}
	if strings.Contains(id, "/") {
		return id
	}
	return Namespace + id
}

// testCases finds the Opengrep test file for a rule file and counts the cases that
// name this rule.
//
// Opengrep's convention is a file beside the rule with the same base name and the
// target language's extension, annotated on the line before each match: `ruleid:
// <id>` for a line that must match, `ok: <id>` for one that must not. Counting
// them here is what lets `rules test` report coverage even where the patterns
// themselves cannot be executed.
func testCases(dir, ruleFileName, id string) (testFile string, positive, negative int) {
	base := strings.TrimSuffix(ruleFileName, filepath.Ext(ruleFileName))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", 0, 0
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext == ".yaml" || ext == ".yml" {
			continue
		}
		if strings.TrimSuffix(name, filepath.Ext(name)) != base {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		positive, negative = countAnnotations(string(raw), id)
		return name, positive, negative
	}
	return "", 0, 0
}

// countAnnotations counts `ruleid:` and `ok:` annotations naming one rule. An
// annotation may list several ids separated by commas.
func countAnnotations(body, id string) (positive, negative int) {
	for _, line := range strings.Split(body, "\n") {
		kind, ids, ok := annotation(line)
		if !ok || !names(ids, id) {
			continue
		}
		if kind == "ruleid" {
			positive++
		} else {
			negative++
		}
	}
	return positive, negative
}

// annotation reads one comment line. Only the two markers Opengrep defines are
// recognised; `todoruleid` and the rest are deliberately ignored, because a case
// marked as not yet working is not coverage.
func annotation(line string) (kind, ids string, ok bool) {
	trimmed := strings.TrimSpace(line)
	for _, prefix := range []string{"//", "#", "/*", "*", "--"} {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
	}
	// A block comment closes on the same line more often than not, and the closer
	// would otherwise become part of the last rule id.
	trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "*/"))
	for _, marker := range []string{"ruleid:", "ok:"} {
		if rest, found := strings.CutPrefix(trimmed, marker); found {
			return strings.TrimSuffix(marker, ":"), rest, true
		}
	}
	return "", "", false
}

func names(list, id string) bool {
	for _, candidate := range strings.Split(list, ",") {
		if strings.TrimSpace(candidate) == id {
			return true
		}
	}
	return false
}

// Untested lists the rules with no positive case, no negative case, or no test
// file at all — sorted, so the report is stable.
//
// Both halves are required. A rule with only a positive case is a rule nobody has
// checked for false positives, and a false positive is how a whole rule pack loses
// its audience.
func (p Pack) Untested() []string {
	var out []string
	for _, rule := range p.Rules {
		switch {
		case rule.TestFile == "":
			out = append(out, fmt.Sprintf("%s (%s): no test file beside the rule", rule.ID, rule.File))
		case rule.Positive == 0 && rule.Negative == 0:
			out = append(out, fmt.Sprintf("%s (%s): the test file names no case for it", rule.ID, rule.TestFile))
		case rule.Positive == 0:
			out = append(out, fmt.Sprintf("%s (%s): no `ruleid:` case, so nothing proves it matches", rule.ID, rule.TestFile))
		case rule.Negative == 0:
			out = append(out, fmt.Sprintf("%s (%s): no `ok:` case, so nothing proves it does not over-match", rule.ID, rule.TestFile))
		}
	}
	sort.Strings(out)
	return out
}

// Cases is the total number of annotated cases in the pack.
func (p Pack) Cases() int {
	total := 0
	for _, rule := range p.Rules {
		total += rule.Positive + rule.Negative
	}
	return total
}
