// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const inventoryDoc = `# Third-party licenses

## Go modules

| Module | Version | License | Why |
|---|---|---|---|
| ` + "`gopkg.in/yaml.v3`" + ` | v3.0.1 | MIT | config parsing |

## npm packages

| Package | Version | License | Why |
|---|---|---|---|
| ` + "`typescript`" + ` | 5.9.3 | Apache-2.0 | the analyzer engine |

## External binaries

| Binary | License | Boundary |
|---|---|---|
| ` + "`opengrep`" + ` | LGPL-2.1 | process boundary only |
`

func TestParseInventoryReadsSectionsAndBothTableShapes(t *testing.T) {
	inv := parseInventory(inventoryDoc)

	if got := inv.namesIn(sectionGo)["gopkg.in/yaml.v3"]; got != "MIT" {
		t.Fatalf("want MIT from the name|version|license table, got %q", got)
	}
	// The binaries table is name|license|boundary, so the license is column two.
	if got := inv.namesIn(sectionBinaries)["opengrep"]; got != "LGPL-2.1" {
		t.Fatalf("want LGPL-2.1 from the name|license table, got %q", got)
	}

	// Section membership is the fix for a bug that only appeared once the npm
	// section had entries: reconciling the whole inventory against one ecosystem
	// reports every Go module as missing from the npm tree.
	if got := inv.namesIn(sectionGo); len(got) != 1 || got["gopkg.in/yaml.v3"] == "" {
		t.Fatalf("want only the Go module in the Go section, got %v", got)
	}
	if got := inv.namesIn(sectionNPM); len(got) != 1 || got["typescript"] == "" {
		t.Fatalf("want only the npm package in the npm section, got %v", got)
	}
	if got := inv.namesIn(sectionBinaries); len(got) != 1 || got["opengrep"] == "" {
		t.Fatalf("want only the binary in the binaries section, got %v", got)
	}
}

func TestReconcileFailsInBothDirections(t *testing.T) {
	listed := map[string]string{"a": "MIT"}

	undocumented := reconcile("go module", map[string]string{"a": "v1", "b": "v1"}, listed)
	if len(undocumented) != 1 || !strings.Contains(undocumented[0], "b is not listed") {
		t.Fatalf("want an undocumented-dependency violation, got %v", undocumented)
	}

	stale := reconcile("go module", map[string]string{}, listed)
	if len(stale) != 1 || !strings.Contains(stale[0], "no longer a dependency") {
		t.Fatalf("want a stale-entry violation, got %v", stale)
	}
}

// Reconciling per section is what stops a Go module being reported as a missing
// npm package. Without this the check fails the moment the npm section is filled.
func TestSectionsAreReconciledSeparately(t *testing.T) {
	inv := parseInventory(inventoryDoc)

	goViolations := reconcile("go module", map[string]string{"gopkg.in/yaml.v3": "v3.0.1"}, inv.namesIn(sectionGo))
	if len(goViolations) != 0 {
		t.Fatalf("the Go section must not be judged against npm packages, got %v", goViolations)
	}
	npmViolations := reconcile("npm package", map[string]string{"typescript": "5.9.3"}, inv.namesIn(sectionNPM))
	if len(npmViolations) != 0 {
		t.Fatalf("the npm section must not be judged against Go modules, got %v", npmViolations)
	}
}

func TestCopyleftIsRefusedOnlyWhenLinked(t *testing.T) {
	linkedDoc := "## Go modules\n\n| M | V | L |\n|---|---|---|\n| `example.com/gpl-thing` | v1 | GPL-3.0 |\n"
	linked := checkCopyleft(parseInventory(linkedDoc),
		map[section]map[string]string{sectionGo: {"example.com/gpl-thing": "v1.0.0"}})
	if len(linked) != 1 || !strings.Contains(linked[0], "must not link copyleft") {
		t.Fatalf("want a copyleft violation, got %v", linked)
	}

	// Spawned: not a violation, and exempt by section rather than by a name list.
	// This is ADR-0011's whole point, so it needs a test of its own.
	spawned := checkCopyleft(parseInventory(inventoryDoc), map[section]map[string]string{
		sectionGo: {"gopkg.in/yaml.v3": "v3.0.1"}, sectionNPM: {"typescript": "5.9.3"},
	})
	if len(spawned) != 0 {
		t.Fatalf("a spawned LGPL binary is not a linked dependency, got %v", spawned)
	}
}

func TestBoundaryViolationIsCaughtInTheManifest(t *testing.T) {
	root := t.TempDir()
	// A dependency that never resolved still states intent, so the manifest text
	// is checked as well as the resolved graph.
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module x\n\nrequire github.com/opengrep/opengrep-go v0.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := checkBoundary(root, map[string]string{}, map[string]string{})
	if len(got) == 0 {
		t.Fatal("want a boundary violation for an opengrep entry in go.mod")
	}
	if !strings.Contains(got[0], "ADR-0011") {
		t.Fatalf("want the decision cited so the reader knows why, got %q", got[0])
	}
}

// The real inventory must pass; this is the check CI runs.
func TestTheRealInventoryIsInSync(t *testing.T) {
	violations, summary, err := check("../..")
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("THIRD_PARTY_LICENSES.md is out of sync:\n%s", strings.Join(violations, "\n"))
	}
	if !strings.Contains(summary, "0 copyleft") {
		t.Fatalf("unexpected summary: %s", summary)
	}
}

const webDoc = `## npm development dependencies

| Package | Version | License | Why |
|---|---|---|---|
| ` + "`typescript`" + ` | 5.9.3 | Apache-2.0 | compiles the plugin |

## Web application packages

| Package | Version | License | Why |
|---|---|---|---|
| ` + "`react`" + ` | 19.3.1 | MIT | the page |
| ` + "`left-pad`" + ` | 1.0.0 | MPL-2.0 | an example of a refused license |

## Web application development dependencies

| Package | Version | License | Why |
|---|---|---|---|
| ` + "`typescript`" + ` | 6.0.3 | Apache-2.0 | type-checks the web application |
`

// `typescript` is a development dependency of the plugin and of the web
// application. Keyed by name alone, one row would overwrite the other, and
// whichever section lost its row would be reported as undocumented.
func TestOneNameInTwoSectionsIsTwoRows(t *testing.T) {
	inv := parseInventory(webDoc)
	if got := inv.namesIn(sectionNPMDev); len(got) != 1 || got["typescript"] != "Apache-2.0" {
		t.Errorf("the plugin's development section: %v", got)
	}
	if got := inv.namesIn(sectionWebDev); len(got) != 1 || got["typescript"] != "Apache-2.0" {
		t.Errorf("the web application's development section: %v", got)
	}
	// Each reconciles against its own source without the other in the way.
	if v := reconcile("npm development dependency", map[string]string{"typescript": "5.9.3"}, inv.namesIn(sectionNPMDev)); len(v) != 0 {
		t.Errorf("plugin section: %v", v)
	}
	if v := reconcile("web application development dependency", map[string]string{"typescript": "6.0.3"}, inv.namesIn(sectionWebDev)); len(v) != 0 {
		t.Errorf("web section: %v", v)
	}
}

func TestAWebPackageUnderARefusedLicenseFails(t *testing.T) {
	inv := parseInventory(webDoc)
	got := checkCopyleft(inv, map[section]map[string]string{
		sectionWeb: {"react": "19.3.1", "left-pad": "1.0.0"},
	})
	if len(got) != 1 || !strings.Contains(got[0], "left-pad") || !strings.Contains(got[0], "MPL-2.0") {
		t.Fatalf("want one violation naming left-pad and its license, got %v", got)
	}
	// The same row is no violation once the package is no longer a dependency:
	// that is a stale row, and reconcile is what reports it.
	if got := checkCopyleft(inv, map[section]map[string]string{sectionWeb: {"react": "19.3.1"}}); len(got) != 0 {
		t.Fatalf("a row for a package that is gone: %v", got)
	}
}

const lockfile = `{
  "lockfileVersion": 3,
  "packages": {
    "": {
      "dependencies": {"react": "^19.0.0"},
      "devDependencies": {"vite": "^8.0.0", "typescript": "^6.0.0"}
    },
    "node_modules/react": {"version": "19.3.1", "license": "MIT"},
    "node_modules/scheduler": {"version": "0.28.0", "license": "MIT"},
    "node_modules/@codemirror/state": {"version": "6.7.6", "license": "MIT"},
    "node_modules/vite": {"version": "8.3.8", "license": "MIT", "dev": true},
    "node_modules/typescript": {"version": "6.0.3", "license": "Apache-2.0", "dev": true},
    "node_modules/lightningcss": {"version": "1.30.0", "license": "MPL-2.0", "dev": true},
    "node_modules/vite/node_modules/nested-dev": {"version": "1.0.0", "license": "MIT", "dev": true},
    "node_modules/react/node_modules/nested-prod": {"version": "2.0.0", "license": "MIT"},
    "node_modules/sometimes": {"version": "1.0.0", "license": "MIT", "devOptional": true}
  }
}`

func writeWeb(t *testing.T, lock string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "web", "package-lock.json"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestTheWebLockfileIsReadWithoutAnInstall(t *testing.T) {
	web, err := webPackages(writeWeb(t, lockfile))
	if err != nil {
		t.Fatal(err)
	}
	if !web.checked {
		t.Fatal("the lockfile was there and was not read")
	}

	// Distributed: everything not marked development-only, at any depth, under
	// its own name and not its path.
	wantDistributed := map[string]string{
		"react": "19.3.1", "scheduler": "0.28.0", "@codemirror/state": "6.7.6",
		"nested-prod": "2.0.0", "sometimes": "1.0.0",
	}
	if len(web.distributed) != len(wantDistributed) {
		t.Errorf("distributed = %v, want %v", web.distributed, wantDistributed)
	}
	for name, version := range wantDistributed {
		if web.distributed[name] != version {
			t.Errorf("distributed[%s] = %q, want %q", name, web.distributed[name], version)
		}
	}
	// A development-only entry is left out, whatever its license: it is not
	// built into the page.
	for _, devOnly := range []string{"vite", "typescript", "lightningcss", "nested-dev"} {
		if _, in := web.distributed[devOnly]; in {
			t.Errorf("%s is development-only and was counted as distributed", devOnly)
		}
	}

	// Direct development dependencies: the ones package.json names, and not
	// what they bring.
	if len(web.directDev) != 2 || web.directDev["vite"] != "8.3.8" || web.directDev["typescript"] != "6.0.3" {
		t.Errorf("directDev = %v, want vite and typescript", web.directDev)
	}
	if got := checkDeclared(web); len(got) != 0 {
		t.Errorf("a development-only package under MPL was refused: %v", got)
	}
}

func TestADistributedWebPackageThatDeclaresCopyleftFails(t *testing.T) {
	web, err := webPackages(writeWeb(t, strings.Replace(lockfile,
		`"node_modules/scheduler": {"version": "0.28.0", "license": "MIT"}`,
		`"node_modules/scheduler": {"version": "0.28.0", "license": "(MIT OR GPL-3.0-only)"}`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	got := checkDeclared(web)
	if len(got) != 1 || !strings.Contains(got[0], "scheduler") {
		t.Fatalf("want one violation naming scheduler, got %v", got)
	}
}

func TestNoWebApplicationIsNotAnError(t *testing.T) {
	web, err := webPackages(t.TempDir())
	if err != nil || web.checked {
		t.Fatalf("checked = %v, err = %v; want false and nil", web.checked, err)
	}
	if _, err := webPackages(writeWeb(t, `{"lockfileVersion": 1, "dependencies": {}}`)); err == nil {
		t.Error("an old-format lockfile, which does not list every package, was accepted")
	}
	if _, err := webPackages(writeWeb(t, `not json`)); err == nil {
		t.Error("an unreadable lockfile was accepted")
	}
}

func TestTheBoundaryCheckReadsTheWebManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "web", "package.json"),
		[]byte(`{"dependencies": {"semgrep-wasm": "1.0.0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := checkBoundary(root, map[string]string{}, map[string]string{})
	if len(got) != 1 || !strings.Contains(got[0], filepath.Join("web", "package.json")) {
		t.Fatalf("want a boundary violation for web/package.json, got %v", got)
	}
}
