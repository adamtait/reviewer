// SPDX-License-Identifier: MIT

// Command checklicenses reconciles THIRD_PARTY_LICENSES.md against the real
// dependency graph, and refuses any copyleft dependency.
//
// The inventory is only worth having if it cannot drift, so this fails in both
// directions: a dependency that is not documented, and a documented dependency
// that no longer exists. It also enforces the Opengrep boundary (ADR-0011) by
// failing if that project ever appears as a linked dependency rather than a
// spawned binary.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// copyleft names the licenses that would place obligations on an MIT project
// when linked. Matching is substring-based on the SPDX-ish text in the
// inventory, which is deliberately blunt: a false positive here is a
// conversation, a false negative is a licensing problem.
var copyleft = []string{"GPL", "AGPL", "LGPL", "MPL", "EPL", "CDDL", "SSPL"}

// boundaryOnly are projects that may be spawned but never linked. They are
// listed in the inventory's external-binaries section and must appear in no
// dependency manifest.
var boundaryOnly = []string{"opengrep", "semgrep"}

var (
	rowRe     = regexp.MustCompile(`(?m)^\|\s*` + "`" + `([^` + "`" + `]+)` + "`" + `\s*\|\s*([^|]*?)\s*\|\s*([^|]*?)\s*\|`)
	sectionRe = regexp.MustCompile(`(?m)^## (.+)$`)
)

// section names the four parts of the inventory. Each is reconciled against its
// own dependency source: mixing them would report every Go module as missing
// from the npm tree, which is a real bug this shape prevents.
type section int

const (
	sectionUnknown section = iota
	sectionGo
	// sectionNPM holds packages that are distributed inside the published npm
	// package. Reconciled against the full transitive production tree.
	sectionNPM
	// sectionNPMDev holds direct development dependencies only. They are not
	// distributed, so their transitive trees are not inventoried (ADR-0028).
	sectionNPMDev
	sectionBinaries
	sectionTools
)

func sectionOf(heading string) section {
	switch strings.ToLower(strings.TrimSpace(heading)) {
	case "go modules":
		return sectionGo
	case "npm packages":
		return sectionNPM
	case "npm development dependencies":
		return sectionNPMDev
	case "external binaries":
		return sectionBinaries
	case "development tools":
		return sectionTools
	}
	return sectionUnknown
}

// inventory is the parsed THIRD_PARTY_LICENSES.md: a license per name, per section.
type inventory struct {
	licenses map[string]string
	sections map[string]section
}

func (inv inventory) namesIn(s section) map[string]string {
	out := map[string]string{}
	for name, sec := range inv.sections {
		if sec == s {
			out[name] = inv.licenses[name]
		}
	}
	return out
}

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()

	violations, summary, err := check(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "checklicenses: %v\n", err)
		os.Exit(2)
	}
	for _, v := range violations {
		fmt.Fprintln(os.Stderr, v)
	}
	if len(violations) > 0 {
		fmt.Fprintf(os.Stderr, "\n%d violation(s)\n", len(violations))
		os.Exit(1)
	}
	fmt.Println(summary)
}

func check(root string) (violations []string, summary string, err error) {
	inventoryPath := filepath.Join(root, "THIRD_PARTY_LICENSES.md")
	raw, err := os.ReadFile(inventoryPath)
	if err != nil {
		return nil, "", fmt.Errorf("reading the inventory: %w", err)
	}
	inv := parseInventory(string(raw))

	goMods, err := goModules(root)
	if err != nil {
		return nil, "", err
	}
	violations = append(violations, reconcile("go module", goMods, inv.namesIn(sectionGo))...)

	// Two npm reads, for two different questions. The distributed tree is what
	// ships to a user and is inventoried in full; development dependencies are
	// not distributed, so only the direct ones are listed (ADR-0028).
	npmProd, npmChecked, err := npmPackages(root, "--all", "--omit=dev")
	if err != nil {
		return nil, "", err
	}
	npmDev, _, err := npmPackages(root, "--depth=0")
	if err != nil {
		return nil, "", err
	}
	if npmChecked {
		violations = append(violations, reconcile("distributed npm package", npmProd, inv.namesIn(sectionNPM))...)
		violations = append(violations, reconcile("npm development dependency", npmDev, inv.namesIn(sectionNPMDev))...)
	} else if listed := len(inv.namesIn(sectionNPM)) + len(inv.namesIn(sectionNPMDev)); listed > 0 {
		violations = append(violations, fmt.Sprintf(
			"THIRD_PARTY_LICENSES.md lists %d npm packages but the plugin's dependency tree could not be read", listed))
	}

	npmPkgs := merge(npmProd, npmDev)
	violations = append(violations, checkCopyleft(inv, goMods, npmPkgs)...)
	violations = append(violations, checkBoundary(root, goMods, npmPkgs)...)

	sort.Strings(violations)

	npmNote := "npm: not yet present"
	if npmChecked {
		npmNote = fmt.Sprintf("npm: %d distributed, %d direct dev", len(npmProd), len(npmDev))
	}
	return violations, fmt.Sprintf("Go: %d modules, %s, 0 copyleft, inventory in sync", len(goMods), npmNote), nil
}

// parseInventory reads the document section by section, so each name is
// reconciled against the dependency source it actually belongs to.
func parseInventory(md string) inventory {
	inv := inventory{licenses: map[string]string{}, sections: map[string]section{}}

	// Split on headings, keeping each heading with the body that follows it.
	headings := sectionRe.FindAllStringSubmatchIndex(md, -1)
	for i, h := range headings {
		current := sectionOf(md[h[2]:h[3]])
		end := len(md)
		if i+1 < len(headings) {
			end = headings[i+1][0]
		}
		for _, m := range rowRe.FindAllStringSubmatch(md[h[1]:end], -1) {
			name, second, third := m[1], strings.TrimSpace(m[2]), strings.TrimSpace(m[3])
			// Module and package tables are name | version | license; the binary
			// and tool tables are name | license | …, so take whichever column
			// looks like a license.
			license := third
			if looksLikeLicense(second) {
				license = second
			}
			inv.licenses[name] = license
			inv.sections[name] = current
		}
	}
	return inv
}

func looksLikeLicense(s string) bool {
	for _, token := range []string{"MIT", "BSD", "Apache", "ISC", "GPL", "MPL", "Unlicense", "CC0"} {
		if strings.Contains(s, token) {
			return true
		}
	}
	return false
}

func goModules(root string) (map[string]string, error) {
	cmd := exec.Command("go", "list", "-m", "-json", "all")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("listing go modules: %w", err)
	}

	mods := map[string]string{}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var m struct {
			Path    string `json:"Path"`
			Version string `json:"Version"`
			Main    bool   `json:"Main"`
		}
		if err := dec.Decode(&m); err != nil {
			return nil, fmt.Errorf("reading go module list: %w", err)
		}
		if m.Main {
			continue
		}
		mods[m.Path] = m.Version
	}
	return mods, nil
}

// npmPackages reads the TypeScript plugin's dependency tree. It reports
// checked=false when the plugin does not exist yet, so the absence of an npm
// section is never mistaken for an npm section that passed.
func npmPackages(root string, args ...string) (pkgs map[string]string, checked bool, err error) {
	dir := filepath.Join(root, "plugins", "typescript")
	if _, statErr := os.Stat(filepath.Join(dir, "package.json")); statErr != nil {
		return map[string]string{}, false, nil
	}
	cmd := exec.Command("npm", append([]string{"ls", "--json"}, args...)...)
	cmd.Dir = dir
	out, _ := cmd.Output() // npm exits non-zero on peer-dep warnings; the JSON is still good
	if len(out) == 0 {
		return nil, false, fmt.Errorf("npm ls produced no output in %s", dir)
	}

	var tree struct {
		Dependencies map[string]json.RawMessage `json:"dependencies"`
	}
	if err := json.Unmarshal(out, &tree); err != nil {
		return nil, false, fmt.Errorf("reading npm ls output: %w", err)
	}
	pkgs = map[string]string{}
	var walk func(map[string]json.RawMessage)
	walk = func(deps map[string]json.RawMessage) {
		for name, raw := range deps {
			var node struct {
				Version      string                     `json:"version"`
				Dependencies map[string]json.RawMessage `json:"dependencies"`
			}
			_ = json.Unmarshal(raw, &node)
			pkgs[name] = node.Version
			walk(node.Dependencies)
		}
	}
	walk(tree.Dependencies)
	return pkgs, true, nil
}

// reconcile compares one section of the inventory against one dependency source,
// failing in both directions: an undocumented dependency, and a documented one
// that no longer exists.
func reconcile(kind string, actual, listed map[string]string) []string {
	var violations []string
	for name := range actual {
		if _, ok := listed[name]; !ok {
			violations = append(violations,
				fmt.Sprintf("%s %s is not listed in THIRD_PARTY_LICENSES.md", kind, name))
		}
	}
	for name := range listed {
		if _, ok := actual[name]; !ok {
			violations = append(violations,
				fmt.Sprintf("THIRD_PARTY_LICENSES.md lists %s as a %s, which is no longer a dependency", name, kind))
		}
	}
	return violations
}

// checkCopyleft refuses a copyleft license on anything this project links.
// Spawned binaries and development tools are exempt by section, not by a hardcoded
// name list: that is the distinction ADR-0011 turns on.
func checkCopyleft(inv inventory, goMods, npmPkgs map[string]string) []string {
	var violations []string
	for name, license := range inv.licenses {
		switch inv.sections[name] {
		case sectionBinaries, sectionTools:
			continue // spawned or dev-only, not linked; see ADR-0011
		}
		_, isDep := goMods[name]
		if !isDep {
			_, isDep = npmPkgs[name]
		}
		if !isDep {
			continue
		}
		for _, bad := range copyleft {
			if strings.Contains(strings.ToUpper(license), bad) {
				violations = append(violations, fmt.Sprintf(
					"%s is a linked dependency licensed %s; this project is MIT and must not link copyleft code",
					name, license))
			}
		}
	}
	return violations
}

// checkBoundary is ADR-0011's enforcement: Opengrep may be spawned, never linked.
func checkBoundary(root string, goMods, npmPkgs map[string]string) []string {
	var violations []string
	for _, banned := range boundaryOnly {
		for name := range goMods {
			if strings.Contains(strings.ToLower(name), banned) {
				violations = append(violations, fmt.Sprintf(
					"go module %s links %s, which may only be spawned as a separate process (ADR-0011)", name, banned))
			}
		}
		for name := range npmPkgs {
			if strings.Contains(strings.ToLower(name), banned) {
				violations = append(violations, fmt.Sprintf(
					"npm package %s links %s, which may only be spawned as a separate process (ADR-0011)", name, banned))
			}
		}
		// Belt and braces: a manifest entry that never resolved still states intent.
		for _, manifest := range []string{"go.mod", filepath.Join("plugins", "typescript", "package.json")} {
			body, err := os.ReadFile(filepath.Join(root, manifest))
			if err != nil {
				continue
			}
			if strings.Contains(strings.ToLower(string(body)), banned) {
				violations = append(violations, fmt.Sprintf(
					"%s mentions %s; it may only be spawned as a separate process (ADR-0011)", manifest, banned))
			}
		}
	}
	return violations
}

// merge combines two dependency sets for the copyleft check, which cares about
// every license present rather than about which tree it came from.
func merge(sets ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, set := range sets {
		for name, version := range set {
			out[name] = version
		}
	}
	return out
}
