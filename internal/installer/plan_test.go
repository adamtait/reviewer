// SPDX-License-Identifier: MIT

package installer

import (
	"strings"
	"testing"

	"github.com/adamtait/reviewer/internal/testfixture"
)

func TestPlanForTheFixtureRepository(t *testing.T) {
	root := testfixture.Destination(t, "tiny-monorepo")
	p := BuildPlan(mustDetect(t, root), "v0.1.0", Options{})

	if got, want := p.Summary(), "5 files to create, 1 devDependency to add, 0 files to overwrite"; got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
	if len(p.DevDependencies) != 1 {
		t.Fatalf("want the TypeScript plugin, got %+v", p.DevDependencies)
	}
	if d := p.DevDependencies[0]; d.Name != PluginPackage || d.Version != "0.1.0" {
		t.Errorf("want the plugin pinned to the binary's version, got %s@%s", d.Name, d.Version)
	}

	out := &strings.Builder{}
	if err := p.Write(out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"npm (from package-lock.json)",
		"2 (packages/pkg-a, packages/pkg-b) from packages/*",
		"vitest",
		".review/config.yaml",
		".github/workflows/review.yml",
		"no dependency-cruiser config",
		"5 files to create",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("plan does not mention %q:\n%s", want, text)
		}
	}
}

// An existing file is left alone, and --force is what turns it into an overwrite.
// The distinction is the plan's, not the writer's: --dry-run --force has to be
// able to show what --force would destroy.
func TestPlanLeavesExistingFilesAloneUnlessForced(t *testing.T) {
	root := testfixture.Destination(t, "tiny-monorepo")
	write(t, root, ".review/config.yaml", "analyzers: []\n")
	d := mustDetect(t, root)

	plain := BuildPlan(d, "v0.1.0", Options{})
	if got, want := plain.Summary(), "4 files to create, 1 devDependency to add, 0 files to overwrite"; got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
	if plain.Files[0].Action != Unchanged {
		t.Errorf("want the existing config left alone, got %q", plain.Files[0].Action)
	}

	forced := BuildPlan(d, "v0.1.0", Options{Force: true})
	if got, want := forced.Summary(), "4 files to create, 1 devDependency to add, 1 file to overwrite"; got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
	if forced.Files[0].Action != Overwrite {
		t.Errorf("want --force to plan an overwrite, got %q", forced.Files[0].Action)
	}
}

func TestPlanAddsNoPluginToARepositoryWithoutTypeScript(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", `{"name":"plain"}`)

	p := BuildPlan(mustDetect(t, root), "v0.1.0", Options{})
	if len(p.DevDependencies) != 0 {
		t.Errorf("nothing for the plugin to analyze; got %+v", p.DevDependencies)
	}
	if got, want := p.Summary(), "5 files to create, 0 devDependencies to add, 0 files to overwrite"; got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
	if !mentions(p.Notes, "no tsconfig.json") {
		t.Errorf("want the reason stated, got %v", p.Notes)
	}
}

func TestPlanNamesWhatItCouldNotFind(t *testing.T) {
	root := t.TempDir()

	p := BuildPlan(mustDetect(t, root), "v0.1.0", Options{})
	if !mentions(p.Notes, "not a Node project") {
		t.Errorf("want the absence of package.json explained, got %v", p.Notes)
	}
	// One Node-shaped note, not seven: once there is no package.json, every other
	// such absence follows from it and repeating them buries the one that matters.
	if got := len(nodeGaps(mustDetect(t, root))); got != 1 {
		t.Errorf("want a single explanation for the Node absences, got %d", got)
	}
	// The absences that are not Node-shaped are still reported, though. ADR-0020
	// promises every absence with what it costs, and the GitHub remote is one the
	// reporter cannot work without.
	if !mentions(p.Notes, "no GitHub origin remote") {
		t.Errorf("want the missing remote reported, got %v", p.Notes)
	}
	// A non-Node repository still gets a full install: the binary analyzers work.
	if got, want := p.Summary(), "5 files to create, 0 devDependencies to add, 0 files to overwrite"; got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
}

func TestPluginVersionFollowsTheBinary(t *testing.T) {
	for _, tc := range []struct {
		binary, want string
		noted        bool
	}{
		{binary: "v0.1.0", want: "0.1.0"},
		{binary: "1.2.3", want: "1.2.3"},
		{binary: "v0.2.0-rc.1", want: "0.2.0-rc.1"},
		{binary: "dev", want: "latest", noted: true},
		{binary: "", want: "latest", noted: true},
		{binary: "v0.1", want: "latest", noted: true},
	} {
		got, note := pluginVersionFor(tc.binary)
		if got != tc.want {
			t.Errorf("pluginVersionFor(%q) = %q, want %q", tc.binary, got, tc.want)
		}
		if (note != "") != tc.noted {
			t.Errorf("pluginVersionFor(%q) note = %q, want noted=%v", tc.binary, note, tc.noted)
		}
	}
}

func mentions(notes []string, substr string) bool {
	for _, note := range notes {
		if strings.Contains(note, substr) {
			return true
		}
	}
	return false
}
