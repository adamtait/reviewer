// SPDX-License-Identifier: MIT

package installer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"
)

// PluginPackage is the npm name of the TypeScript plugin. The installer adds it
// to the destination's devDependencies rather than bundling it: the plugin runs
// the destination's own TypeScript and ESLint, so it has to be installed
// alongside them (ADR-0014).
const PluginPackage = "@adamtait/reviewer-plugin-typescript"

// Action is what init would do to one file or dependency.
type Action string

const (
	// Create writes something that is not there.
	Create Action = "create"
	// Overwrite replaces something that is, and only happens under --force.
	Overwrite Action = "overwrite"
	// Unchanged leaves a file alone because it already exists.
	Unchanged Action = "unchanged"
)

// File is one entry in the plan. Purpose is printed alongside the path: a list of
// paths tells you what will happen but not whether you want it to.
type File struct {
	Path    string
	Action  Action
	Purpose string
}

// Dependency is a package the destination must install for the plan to work.
type Dependency struct {
	Name    string
	Version string
	Action  Action
	Purpose string
}

// Plan is every decision init makes, gathered before any of it happens, so that
// --dry-run can print the whole thing and writing is a separate step over a value
// that has already been inspected.
type Plan struct {
	Detected        Detected
	Files           []File
	DevDependencies []Dependency
	// Notes are the things the installer could not determine, each with what it
	// means for the resulting review. A plan that silently omits what it did not
	// find is how an install looks successful and reviews nothing.
	Notes []string
}

// BuildPlan decides what init would do to the detected repository. force says
// whether an existing file would be replaced; it changes the plan's shape rather
// than the writer's behaviour, so --dry-run --force shows what --force would do.
func BuildPlan(d Detected, pluginVersion string, force bool) Plan {
	p := Plan{Detected: d}

	for _, f := range []struct{ path, purpose string }{
		{".review/config.yaml", "what runs, against what, and how it is reported"},
		{".review/rules/.gitkeep", "where this repository's own rule packs go"},
		{".review/.gitignore", "keeps the poller's watermark out of version control"},
		{".github/workflows/review.yml", "runs the review on every pull request"},
		{".env.example", "names the variables the model lane needs, never their values"},
	} {
		p.Files = append(p.Files, File{
			Path:    f.path,
			Action:  actionFor(filepath.Join(d.Root, filepath.FromSlash(f.path)), force),
			Purpose: f.purpose,
		})
	}

	if d.HasPackageJSON && d.TSConfig != "" {
		version, note := pluginVersionFor(pluginVersion)
		p.DevDependencies = append(p.DevDependencies, Dependency{
			Name:    PluginPackage,
			Version: version,
			Action:  Create,
			Purpose: "the TypeScript analyzers: tsc, eslint, dependency-cruiser",
		})
		if note != "" {
			p.Notes = append(p.Notes, note)
		}
	}

	p.Notes = append(p.Notes, gaps(d)...)
	return p
}

// actionFor decides between creating, overwriting and leaving alone. A file the
// installer cannot stat for any reason other than absence is treated as present:
// the conservative reading, since the alternative is planning to overwrite
// something that could not be examined.
func actionFor(path string, force bool) Action {
	if _, err := os.Stat(path); err != nil && os.IsNotExist(err) {
		return Create
	}
	if force {
		return Overwrite
	}
	return Unchanged
}

var releaseVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

// pluginVersionFor pins the plugin to the binary's own version. The two are one
// release (ADR-0026) and a mismatch is a protocol mismatch, so a range would
// let the destination drift onto a plugin this binary cannot talk to.
//
// A binary built outside the release process has no version to pin to, which is
// a fact about the plan worth printing rather than a reason to refuse.
func pluginVersionFor(binaryVersion string) (version, note string) {
	if releaseVersion.MatchString(binaryVersion) {
		return strings.TrimPrefix(binaryVersion, "v"), ""
	}
	return "latest", fmt.Sprintf(
		"this binary reports version %q, which is not a release, so the plugin would be "+
			"added as \"latest\" instead of pinned to a matching version", binaryVersion)
}

// gaps reports what the installer did not find and what each absence costs. Every
// one of these is a working install that reviews less than the reader expects.
func gaps(d Detected) []string {
	var notes []string
	if !d.HasPackageJSON {
		notes = append(notes,
			"no package.json: this is not a Node project, so the TypeScript plugin will "+
				"decline every analyzer; the binary analyzers still run")
		return notes
	}
	if d.PackageManager == "" {
		notes = append(notes,
			"no lockfile and no packageManager field: install the plugin devDependency by hand")
	}
	if d.TSConfig == "" {
		notes = append(notes,
			"no tsconfig.json at the root: the tsc analyzer has no program to build, so the "+
				"TypeScript plugin is not part of this plan")
	}
	if d.ESLintConfig == "" {
		notes = append(notes,
			"no ESLint flat config: the eslint analyzer declines rather than inventing rules")
	}
	if d.DepCruiserConfig == "" {
		notes = append(notes,
			"no dependency-cruiser config: the layering analyzer declines; add one to state "+
				"this repository's layering rules")
	}
	if d.TestRunner == "" {
		notes = append(notes,
			"neither vitest nor jest is a dependency: the changed-tests analyzer cannot say "+
				"how to run the tests it asks for")
	}
	if d.Remote == "" {
		notes = append(notes,
			"no GitHub origin remote: set github.repo in .review/config.yaml by hand")
	}
	if d.Nx {
		notes = append(notes,
			"nx.json is present: project-level scoping is read from it, and until then the "+
				"review runs against the whole repository with the diff filter applied")
	}
	return notes
}

// Counts of each action, for the one-line summary.
func (p Plan) count(a Action) int {
	n := 0
	for _, f := range p.Files {
		if f.Action == a {
			n++
		}
	}
	return n
}

// Summary is the line a reader checks before running init for real.
func (p Plan) Summary() string {
	return fmt.Sprintf("%s to create, %s to add, %d overwrites",
		plural(p.count(Create), "file", "files"),
		plural(len(p.DevDependencies), "devDependency", "devDependencies"),
		p.count(Overwrite))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// Write prints the plan: what was detected, what would happen, and what the
// installer could not work out. The detected section is not decoration — every
// wrong line in it is a wrong line in the generated config, and this is the only
// point at which a person can catch it.
func (p Plan) Write(w io.Writer) error {
	b := &strings.Builder{}
	fmt.Fprintf(b, "reviewer init — plan for %s\n\n", p.Detected.Root)

	fmt.Fprintln(b, "detected")
	tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
	for _, row := range p.detectedRows() {
		fmt.Fprintf(tw, "  %s\t%s\n", row[0], row[1])
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	fmt.Fprintln(b, "\nfiles")
	tw = tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
	for _, f := range p.Files {
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", f.Action, f.Path, f.Purpose)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if len(p.DevDependencies) > 0 {
		fmt.Fprintln(b, "\ndevDependencies")
		tw = tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
		for _, d := range p.DevDependencies {
			fmt.Fprintf(tw, "  %s\t%s@%s\t%s\n", d.Action, d.Name, d.Version, d.Purpose)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}

	if len(p.Notes) > 0 {
		fmt.Fprintln(b, "\nnotes")
		for _, note := range p.Notes {
			fmt.Fprintf(b, "  - %s\n", note)
		}
	}

	fmt.Fprintf(b, "\n%s\n", p.Summary())
	_, err := io.WriteString(w, b.String())
	return err
}

func (p Plan) detectedRows() [][2]string {
	d := p.Detected
	manager := "not found"
	if d.PackageManager != "" {
		manager = fmt.Sprintf("%s (from %s)", d.PackageManager, d.LockfileBasis)
	}
	workspaces := "none"
	if len(d.Workspaces) > 0 {
		workspaces = fmt.Sprintf("%d (%s) from %s",
			len(d.Workspaces),
			strings.Join(d.Workspaces, ", "),
			strings.Join(d.WorkspaceGlobs, ", "))
	} else if len(d.WorkspaceGlobs) > 0 {
		workspaces = fmt.Sprintf("none matched %s", strings.Join(d.WorkspaceGlobs, ", "))
	}
	return [][2]string{
		{"package manager", manager},
		{"workspaces", workspaces},
		{"nx", yesNo(d.Nx)},
		{"test runner", orNotFound(d.TestRunner)},
		{"tsconfig", orNotFound(d.TSConfig)},
		{"eslint config", orNotFound(d.ESLintConfig)},
		{"dependency-cruiser", orNotFound(d.DepCruiserConfig)},
		{"github repo", orNotFound(d.Remote)},
	}
}

func orNotFound(s string) string {
	if s == "" {
		return "not found"
	}
	return s
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
