// SPDX-License-Identifier: MIT

package installer

import (
	"strings"
	"testing"

	"github.com/adamtait/reviewer/internal/config"
	"github.com/adamtait/reviewer/internal/testfixture"
)

func TestProjectsFromWorkspaces(t *testing.T) {
	root := testfixture.Destination(t, "tiny-monorepo")

	if got := strings.Join(Projects(mustDetect(t, root)), ","); got != "packages/pkg-a,packages/pkg-b" {
		t.Errorf("Projects() = %q", got)
	}
}

func TestProjectsFromNx(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", `{"name":"root"}`)
	write(t, root, "nx.json", `{}`)
	write(t, root, "apps/web/project.json", `{"name":"web"}`)
	write(t, root, "libs/core/project.json", `{"name":"core"}`)
	// Nx repositories routinely have projects the package manager never declares,
	// which is why the two sources are unioned rather than either one trusted.
	write(t, root, "package.json", `{"name":"root","workspaces":["apps/*"]}`)
	write(t, root, "apps/web/package.json", `{"name":"web"}`)
	// Must not appear: a dependency's own project.json.
	write(t, root, "node_modules/@scope/dep/project.json", `{"name":"dep"}`)
	// Must not appear: build output.
	write(t, root, "dist/apps/web/project.json", `{"name":"stale"}`)

	if got := strings.Join(Projects(mustDetect(t, root)), ","); got != "apps/web,libs/core" {
		t.Errorf("Projects() = %q, want the two real projects and neither of the traps", got)
	}
}

func TestProjectsIsEmptyForASinglePackageRepository(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", `{"name":"one"}`)

	if got := Projects(mustDetect(t, root)); len(got) != 0 {
		t.Errorf("want no projects, got %v", got)
	}
}

// The exit criterion: a single-package repository's generated config is what it
// was before monorepo support existed, with no projects key at all.
func TestASinglePackageConfigCarriesNoProjectsKey(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", `{"name":"one","devDependencies":{"typescript":"^5"}}`)
	write(t, root, "tsconfig.json", `{}`)

	install(t, root)
	body := read(t, root, ".review/config.yaml")
	if strings.Contains(body, "projects") {
		t.Errorf("a single-package config must not mention projects:\n%s", body)
	}

	cfg, _, err := config.Resolve(root, "", func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Projects) != 0 {
		t.Errorf("want no projects, got %v", cfg.Projects)
	}
}

func TestAMonorepoConfigCarriesItsProjects(t *testing.T) {
	root := testfixture.Destination(t, "tiny-monorepo")
	install(t, root)

	cfg, _, err := config.Resolve(root, "", func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.Projects, ","); got != "packages/pkg-a,packages/pkg-b" {
		t.Errorf("want both workspaces in the generated config, got %q", got)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("the generated config does not validate: %v", err)
	}
}

// A repository that declares workspaces whose globs resolve to nothing is
// correct-but-slow, and silence would leave nobody to notice.
func TestAMonorepoWithNoResolvableProjectsSaysSo(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", `{"name":"root","workspaces":["packages/*"]}`)

	plan := BuildPlan(mustDetect(t, root), releaseV, Options{})
	if !mentions(plan.Notes, "no project directory could be resolved") {
		t.Errorf("want the gap reported, got %v", plan.Notes)
	}
}
