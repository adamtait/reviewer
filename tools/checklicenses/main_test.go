// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const inventory = "" +
	"| `gopkg.in/yaml.v3` | v3.0.1 | MIT | config parsing |\n" +
	"| `opengrep` | LGPL-2.1 | process boundary only |\n"

func TestParseInventoryReadsBothTableShapes(t *testing.T) {
	got := parseInventory(inventory)
	if got["gopkg.in/yaml.v3"] != "MIT" {
		t.Fatalf("want MIT from the name|version|license table, got %q", got["gopkg.in/yaml.v3"])
	}
	// The binaries table is name|license|boundary, so the license is column two.
	if got["opengrep"] != "LGPL-2.1" {
		t.Fatalf("want LGPL-2.1 from the name|license table, got %q", got["opengrep"])
	}
}

func TestReconcileFailsInBothDirections(t *testing.T) {
	inv := map[string]string{"a": "MIT"}

	undocumented := reconcile("go module", map[string]string{"a": "v1", "b": "v1"}, inv)
	if len(undocumented) != 1 || !strings.Contains(undocumented[0], "b is not listed") {
		t.Fatalf("want an undocumented-dependency violation, got %v", undocumented)
	}

	stale := reconcile("go module", map[string]string{}, inv)
	if len(stale) != 1 || !strings.Contains(stale[0], "no longer a dependency") {
		t.Fatalf("want a stale-entry violation, got %v", stale)
	}
}

func TestCopyleftIsRefusedOnlyWhenLinked(t *testing.T) {
	// Linked: a violation.
	linked := checkCopyleft(
		map[string]string{"example.com/gpl-thing": "GPL-3.0"},
		map[string]string{"example.com/gpl-thing": "v1.0.0"},
		nil)
	if len(linked) != 1 || !strings.Contains(linked[0], "must not link copyleft") {
		t.Fatalf("want a copyleft violation, got %v", linked)
	}

	// Spawned: not a violation. This is ADR-0011's whole point, so it needs a
	// test of its own rather than being implied by the inventory passing.
	spawned := checkCopyleft(
		map[string]string{"opengrep": "LGPL-2.1"},
		map[string]string{}, map[string]string{})
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
