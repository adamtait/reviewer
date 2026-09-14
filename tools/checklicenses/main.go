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

var rowRe = regexp.MustCompile(`(?m)^\|\s*` + "`" + `([^` + "`" + `]+)` + "`" + `\s*\|\s*([^|]*?)\s*\|\s*([^|]*?)\s*\|`)

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
	inventory := parseInventory(string(raw))

	goMods, err := goModules(root)
	if err != nil {
		return nil, "", err
	}
	violations = append(violations, reconcile("go module", goMods, inventory)...)

	npmPkgs, npmChecked, err := npmPackages(root)
	if err != nil {
		return nil, "", err
	}
	if npmChecked {
		violations = append(violations, reconcile("npm package", npmPkgs, inventory)...)
	}

	violations = append(violations, checkCopyleft(inventory, goMods, npmPkgs)...)
	violations = append(violations, checkBoundary(root, goMods, npmPkgs)...)

	sort.Strings(violations)

	npmNote := "npm: not yet present"
	if npmChecked {
		npmNote = fmt.Sprintf("npm: %d packages", len(npmPkgs))
	}
	return violations, fmt.Sprintf("Go: %d modules, %s, 0 copyleft, inventory in sync", len(goMods), npmNote), nil
}

// parseInventory maps every backticked name in a table row to the license column
// beside it.
func parseInventory(md string) map[string]string {
	out := map[string]string{}
	for _, m := range rowRe.FindAllStringSubmatch(md, -1) {
		name, second, third := m[1], strings.TrimSpace(m[2]), strings.TrimSpace(m[3])
		// Module and package tables are name | version | license; the binary and
		// tool tables are name | license | …, so take whichever column looks like
		// a license.
		license := third
		if looksLikeLicense(second) {
			license = second
		}
		out[name] = license
	}
	return out
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
func npmPackages(root string) (pkgs map[string]string, checked bool, err error) {
	dir := filepath.Join(root, "plugins", "typescript")
	if _, statErr := os.Stat(filepath.Join(dir, "package.json")); statErr != nil {
		return map[string]string{}, false, nil
	}
	cmd := exec.Command("npm", "ls", "--all", "--json")
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

func reconcile(kind string, actual map[string]string, inventory map[string]string) []string {
	var violations []string
	for name := range actual {
		if _, ok := inventory[name]; !ok {
			violations = append(violations,
				fmt.Sprintf("%s %s is not listed in THIRD_PARTY_LICENSES.md", kind, name))
		}
	}
	for name := range inventory {
		if isBinaryOrTool(name) {
			continue
		}
		if _, ok := actual[name]; !ok {
			violations = append(violations,
				fmt.Sprintf("THIRD_PARTY_LICENSES.md lists %s, which is no longer a dependency", name))
		}
	}
	return violations
}

// isBinaryOrTool recognises inventory rows that describe spawned binaries and
// development tools rather than linked dependencies. Those legitimately have no
// entry in any dependency manifest — that is the whole point of ADR-0011.
func isBinaryOrTool(name string) bool {
	switch name {
	case "opengrep", "gitleaks", "osv-scanner",
		"honnef.co/go/tools", "github.com/google/addlicense":
		return true
	}
	return false
}

func checkCopyleft(inventory, goMods, npmPkgs map[string]string) []string {
	var violations []string
	for name, license := range inventory {
		if isBinaryOrTool(name) {
			continue // spawned, not linked; see ADR-0011
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
