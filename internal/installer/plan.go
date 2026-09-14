// SPDX-License-Identifier: MIT

package installer

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/adamtait/reviewer/internal/config"
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
	// Add is a dependency the destination's own package manager must install.
	// The installer prints the command rather than running it or editing
	// package.json; see manualSteps in write.go for why.
	Add Action = "add"
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

// Options are the choices a person makes, as distinct from the facts Detect
// gathers. Kept separate because every field here is something the plan cannot
// work out for itself.
type Options struct {
	// Force replaces files that already exist.
	Force bool
	// Provider is the chosen model access path. The zero value means none was
	// chosen, which is a complete install: the model lane is off regardless.
	Provider Provider
}

// Plan is every decision init makes, gathered before any of it happens, so that
// --dry-run can print the whole thing and writing is a separate step over a value
// that has already been inspected.
type Plan struct {
	Detected        Detected
	Files           []File
	DevDependencies []Dependency
	// Projects are the directories the repository divides into, written into the
	// generated config so a change confined to one is analyzed as one.
	Projects []string
	// Problems make the install unsafe to perform. Install refuses while any
	// remains, because every one of them means a write would land somewhere other
	// than where the plan says.
	Problems []string
	// Manual is the steps a person has to take that this tool will not take for
	// them. Part of the plan rather than of the report, so that --dry-run is a
	// faithful preview: two of these decide whether the generated workflow runs
	// at all.
	Manual []string
	// Options are the choices this plan was built with, carried so that rendering
	// and reporting cannot disagree with what was planned.
	Options Options
	// Notes are the things the installer could not determine, each with what it
	// means for the resulting review. A plan that silently omits what it did not
	// find is how an install looks successful and reviews nothing.
	Notes []string
}

// BuildPlan decides what init would do to the detected repository. Options change
// the plan's shape rather than the writer's behaviour, so `--dry-run --force`
// shows what `--force` would do and `--dry-run --provider gemini` shows what
// choosing gemini would write.
func BuildPlan(d Detected, pluginVersion string, opts Options) Plan {
	p := Plan{Detected: d, Options: opts}
	force := opts.Force

	for _, f := range []struct{ path, purpose string }{
		{".review/config.yaml", "what runs, against what, and how it is reported"},
		{".review/rules/.gitkeep", "where this repository's own rule packs go"},
		{".review/.gitignore", "keeps local state and your .env out of version control"},
		{".github/workflows/review.yml", "runs the review on every pull request"},
		{".review/.env.example", "names the variables the model lane needs, never their values"},
	} {
		p.addProblem(unsafePath(d.Root, f.path))
		action, problem := actionFor(d.Root, f.path, force)
		p.addProblem(problem)
		p.Files = append(p.Files, File{Path: f.path, Action: action, Purpose: f.purpose})
	}

	if d.HasPackageJSON && d.TSConfig != "" {
		version, note := pluginVersionFor(pluginVersion)
		p.DevDependencies = append(p.DevDependencies, Dependency{
			Name:    PluginPackage,
			Version: version,
			Action:  Add,
			Purpose: "the TypeScript analyzers: tsc, eslint, dependency-cruiser",
		})
		if note != "" {
			p.Notes = append(p.Notes, note)
		}
	}

	p.Projects = Projects(d)
	p.Manual = manualSteps(p, pluginVersion)
	p.Notes = append(p.Notes, gaps(d)...)
	p.Notes = append(p.Notes, providerNotes(opts.Provider)...)
	if projectsUnreadable(d, p.Projects) {
		p.Notes = append(p.Notes,
			"this looks like a monorepo but no project directory could be resolved: the "+
				"review will run against the whole repository, which is correct and slower")
	}
	return p
}

// actionFor decides between creating, overwriting and leaving alone.
//
// Lstat, not Stat: a symlink is a thing that is there, and a dangling one would
// otherwise read as absent and be "created" by writing through to its target.
//
// A path that cannot be examined for any reason other than absence is reported as
// a problem rather than quietly treated as present. `.review` existing as a
// regular file makes every path under it fail with ENOTDIR, and calling that
// "unchanged" turns an install that wrote no config into one that reports success.
func actionFor(root, rel string, force bool) (Action, string) {
	_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel)))
	switch {
	case err == nil:
		if force {
			return Overwrite, ""
		}
		return Unchanged, ""
	case errors.Is(err, fs.ErrNotExist):
		return Create, ""
	default:
		// The cause, not the path that hit it: `.review` present as a regular file
		// blocks four paths for one reason, and naming each of them four times
		// buries how few distinct things are actually wrong.
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			return Unchanged, fmt.Sprintf("cannot write under %s: %v",
				filepath.ToSlash(filepath.Dir(rel)), pathErr.Err)
		}
		return Unchanged, fmt.Sprintf("cannot examine %s: %v", rel, err)
	}
}

// unsafePath refuses to write through a symlink.
//
// Every path this installer writes is repository-relative, and a repository can
// commit a symlink at any component of one: `.review -> ../../elsewhere`, or
// `.review/config.yaml -> ~/.bashrc`. Following it would write outside the root
// while the report named an in-repo path, which is both an escape and a lie about
// where the bytes went. Dangling symlinks are the dangerous case, since they look
// absent to a plain Stat.
func unsafePath(root, rel string) string {
	current := root
	for _, segment := range strings.Split(rel, "/") {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			// Nothing exists from here down, so nothing can be followed.
			return ""
		}
		if err != nil {
			// Reported by actionFor, which examines the same path.
			return ""
		}
		if info.Mode()&os.ModeSymlink != 0 {
			relative, relErr := filepath.Rel(root, current)
			if relErr != nil {
				relative = current
			}
			return fmt.Sprintf(
				"%s is a symlink: writing through it would land outside this repository",
				filepath.ToSlash(relative))
		}
	}
	return ""
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

// providerNotes says what the chosen path implies. Both notes are consequences a
// person would otherwise discover after the install rather than before it.
func providerNotes(p Provider) []string {
	if p.ID == "" {
		return []string{
			"no model access path chosen: the deterministic lane is fully installed and the " +
				"model lane has nothing to call. Re-run with --provider to add one",
		}
	}
	var notes []string
	if !p.WorksInCI {
		notes = append(notes, fmt.Sprintf(
			"%s drives the %s CLI and a signed-in subscription, neither of which exists on a "+
				"hosted runner: the model lane will run locally and be skipped in the workflow",
			p.ID, p.Binary))
	}
	if p.NeedsAPIKey() {
		notes = append(notes, fmt.Sprintf(
			"%s needs %s in the environment. .review/.env.example names it; no value is written anywhere",
			p.ID, config.EnvModelAPIKey))
	}
	return notes
}

// gaps reports what the installer did not find and what each absence costs. Every
// one of these is a working install that reviews less than the reader expects.
func gaps(d Detected) []string {
	notes := nodeGaps(d)
	if d.Remote == "" {
		notes = append(notes,
			"no GitHub origin remote: set github.repo in .review/config.yaml by hand, or the "+
				"GitHub reporter has nowhere to post")
	}
	if d.Nx {
		notes = append(notes,
			"nx.json is present: projects are read from the project.json files found under it")
	}
	return notes
}

// nodeGaps are the absences that only mean something in a Node project. Once
// there is no package.json they all follow from that one fact, and printing seven
// notes buries it — but the absences outside this set are still reported, because
// ADR-0020 promises every absence with what it costs.
func nodeGaps(d Detected) []string {
	var notes []string
	if !d.HasPackageJSON {
		return []string{
			"no package.json: this is not a Node project, so the TypeScript plugin will " +
				"decline every analyzer; the binary analyzers still run",
		}
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
	return notes
}

// addProblem records a reason not to install, once. One symlinked `.review`
// blocks four paths for the same reason, and saying so four times buries how few
// distinct things are actually wrong.
func (p *Plan) addProblem(problem string) {
	if problem == "" {
		return
	}
	for _, existing := range p.Problems {
		if existing == problem {
			return
		}
	}
	p.Problems = append(p.Problems, problem)
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
	return fmt.Sprintf("%s to create, %s to add, %s to overwrite",
		plural(p.count(Create), "file", "files"),
		plural(len(p.DevDependencies), "devDependency", "devDependencies"),
		plural(p.count(Overwrite), "file", "files"))
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

	if len(p.Manual) > 0 {
		fmt.Fprintf(b, "\n%s you will have to take:\n", plural(len(p.Manual), "step", "steps"))
		for _, step := range p.Manual {
			fmt.Fprintf(b, "  %s\n", step)
		}
	}

	if len(p.Problems) > 0 {
		fmt.Fprintf(b, "\n%s stopping this install:\n", plural(len(p.Problems), "problem", "problems"))
		for _, problem := range p.Problems {
			fmt.Fprintf(b, "  %s\n", problem)
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
		{"projects", projectsRow(p.Projects)},
		{"model access path", orNone(p.Options.Provider.ID)},
	}
}

func projectsRow(projects []string) string {
	if len(projects) == 0 {
		return "none: the whole repository is one unit"
	}
	if len(projects) <= 6 {
		return fmt.Sprintf("%d (%s)", len(projects), strings.Join(projects, ", "))
	}
	return fmt.Sprintf("%d (%s, …)", len(projects), strings.Join(projects[:6], ", "))
}

func orNone(s string) string {
	if s == "" {
		return "none chosen"
	}
	return s
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
