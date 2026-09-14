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

	if inv.licenses["gopkg.in/yaml.v3"] != "MIT" {
		t.Fatalf("want MIT from the name|version|license table, got %q", inv.licenses["gopkg.in/yaml.v3"])
	}
	// The binaries table is name|license|boundary, so the license is column two.
	if inv.licenses["opengrep"] != "LGPL-2.1" {
		t.Fatalf("want LGPL-2.1 from the name|license table, got %q", inv.licenses["opengrep"])
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
		map[string]string{"example.com/gpl-thing": "v1.0.0"}, nil)
	if len(linked) != 1 || !strings.Contains(linked[0], "must not link copyleft") {
		t.Fatalf("want a copyleft violation, got %v", linked)
	}

	// Spawned: not a violation, and exempt by section rather than by a name list.
	// This is ADR-0011's whole point, so it needs a test of its own.
	spawned := checkCopyleft(parseInventory(inventoryDoc), map[string]string{}, map[string]string{})
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
