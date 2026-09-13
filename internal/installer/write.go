// SPDX-License-Identifier: MIT

package installer

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/adamtait/reviewer/internal/config"
)

// templates are embedded rather than read from disk: `reviewer init` runs from a
// downloaded binary with no source tree beside it.
//
// Named individually rather than globbed: templates/ also holds launchd.plist.tmpl,
// which is documentation for the poller and has no place in the install path. A
// glob would embed it as a payload nothing can render.
//
//go:embed templates/config.yaml.tmpl templates/review.yml.tmpl
//go:embed templates/review.gitignore.tmpl templates/env.example.tmpl
var templates embed.FS

// checksumPlaceholder is what the generated workflow carries where the release
// archive's SHA-256 belongs.
//
// The installer cannot know it. The checksum covers the archive that contains the
// running binary, so the binary would have to know its own digest — and resolving
// it over the network at install time would mean trusting whoever can move a tag,
// which is the thing the checksum exists to prevent. So it is left obviously
// unfilled, and `init` says so as its last word.
const checksumPlaceholder = "PASTE_THE_SHA256_FROM_THE_RELEASE_CHECKSUMS_FILE"

// versionPlaceholder is what the generated workflow carries when the binary that
// generated it is not a release. `adamtait/reviewer@vdev` is not a ref, and a
// workflow that fails at `uses:` fails before it can say why.
const versionPlaceholder = "0.0.0-REPLACE-WITH-A-RELEASE-VERSION"

// nodeVersion is what the generated workflow installs. The plugin declares
// node >= 22; pinning the major here rather than reading .nvmrc keeps the
// generated workflow honest about what was actually verified.
const nodeVersion = "22"

// Rendered is one file's full content, ready to write.
type Rendered struct {
	Path    string
	Action  Action
	Content []byte
}

// Report is what Install actually did, as distinct from what the plan said it
// would. The two are compared in tests: a writer that quietly does more than its
// plan makes --dry-run a lie.
type Report struct {
	Created     []string
	Overwritten []string
	Unchanged   []string
	// Manual is the steps a person still has to take. An installer that finishes
	// silently while leaving the workflow unable to run has not installed
	// anything.
	Manual []string
}

// funcs are the only way a value reaches a generated file.
//
// Everything interpolated into the generated YAML is either repository content or
// derived from it: a workspace directory name, a remote URL, a version string. A
// bare interpolation of any of those produces YAML that means something other than
// the string — a directory called `a: b` becomes a mapping, a remote containing
// `#` truncates at a comment — or, in the workflow, an extra step.
var funcs = template.FuncMap{"yaml": yamlScalar}

// yamlScalar renders a string as a quoted YAML scalar. JSON's string encoding is a
// valid YAML 1.2 double-quoted scalar, so the standard library does the escaping
// rather than a hand-rolled quoter that would be wrong for some character nobody
// thought of.
func yamlScalar(value string) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// Render turns a plan into file contents. It renders every file the plan would
// write and nothing else, so Install has no opportunity to invent a path.
func Render(p Plan, pluginVersion string) ([]Rendered, error) {
	data := templateData(p, pluginVersion)
	var out []Rendered
	for _, f := range p.Files {
		if f.Action != Create && f.Action != Overwrite {
			continue
		}
		content, err := renderOne(f.Path, data)
		if err != nil {
			return nil, err
		}
		if content == nil {
			continue
		}
		out = append(out, Rendered{Path: f.Path, Action: f.Action, Content: content})
	}
	return out, nil
}

// renderOne maps a destination path to its template. A path with no template
// returns nil content rather than an error, so a plan entry can outlive the
// template that fills it.
func renderOne(path string, data any) ([]byte, error) {
	var name string
	switch path {
	case ".review/config.yaml":
		name = "config.yaml.tmpl"
	case ".review/.gitignore":
		name = "review.gitignore.tmpl"
	case ".github/workflows/review.yml":
		name = "review.yml.tmpl"
	case ".review/.env.example":
		name = "env.example.tmpl"
	case ".review/rules/.gitkeep":
		// Deliberately empty: git tracks the directory, nothing more.
		return []byte{}, nil
	default:
		return nil, nil
	}

	tmpl, err := template.New(name).Funcs(funcs).ParseFS(templates, "templates/"+name)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", name, err)
	}
	body := &strings.Builder{}
	if err := tmpl.Execute(body, data); err != nil {
		return nil, fmt.Errorf("rendering %s: %w", name, err)
	}
	return []byte(body.String()), nil
}

// Install renders the plan and writes it. It writes only what the plan said, and
// returns what it did.
func Install(p Plan, pluginVersion string) (Report, error) {
	if len(p.Problems) > 0 {
		// Refused whole rather than in part: a problem means a write would land
		// somewhere other than where the plan said, and there is no half of that
		// worth performing.
		return Report{}, fmt.Errorf("%s: %s",
			plural(len(p.Problems), "problem", "problems"), strings.Join(p.Problems, "; "))
	}
	files, err := Render(p, pluginVersion)
	if err != nil {
		return Report{}, err
	}

	var report Report
	for _, f := range p.Files {
		if f.Action == Unchanged {
			report.Unchanged = append(report.Unchanged, f.Path)
		}
	}

	for _, f := range files {
		full := filepath.Join(p.Detected.Root, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return report, fmt.Errorf("creating %s: %w", filepath.Dir(f.Path), err)
		}
		if err := os.WriteFile(full, f.Content, 0o644); err != nil {
			return report, fmt.Errorf("writing %s: %w", f.Path, err)
		}
		if f.Action == Overwrite {
			report.Overwritten = append(report.Overwritten, f.Path)
		} else {
			report.Created = append(report.Created, f.Path)
		}
	}

	// Taken from the plan, not recomputed, so that --dry-run and a real install
	// cannot report different outstanding work.
	report.Manual = p.Manual
	return report, nil
}

// manualSteps are the things the installer will not do for you, each because
// doing it would be worse than naming it.
func manualSteps(p Plan, pluginVersion string) []string {
	var steps []string
	for _, d := range p.DevDependencies {
		// The devDependency is not written into package.json.
		//
		// Editing package.json without updating the lockfile leaves the
		// repository in a state where `npm ci` — which the generated workflow
		// runs — fails. And editing the lockfile correctly means resolving the
		// dependency tree, which is the package manager's entire job. Running the
		// package manager here would need the network during an install that is
		// otherwise offline and reversible.
		//
		// So the exact command is printed instead. The package manager then edits
		// package.json and the lockfile together, correctly.
		steps = append(steps, fmt.Sprintf("%s  # adds the TypeScript analyzers",
			installCommand(p.Detected.PackageManager, d)))
	}
	if p.Detected.PackageManager == "bun" {
		steps = append(steps, "add a step installing bun to .github/workflows/review.yml: "+
			"it is not on a hosted runner and actions/setup-node cannot install it")
	}
	for _, f := range p.Files {
		if f.Path != ".github/workflows/review.yml" || f.Action == Unchanged {
			continue
		}
		steps = append(steps, "in .github/workflows/review.yml, replace "+
			checksumPlaceholder+" with the release archive's SHA-256, from the "+
			"checksums file published with the release")
		if workflowVersion(pluginVersion) == versionPlaceholder {
			steps = append(steps, "in .github/workflows/review.yml, replace "+
				versionPlaceholder+" with a released version: this binary is not one, "+
				"so it could not name the release the workflow should download")
		}
	}
	return steps
}

// installCommand is the destination's own package manager saying what it would
// say, because that is the line a person will paste.
func installCommand(manager string, d Dependency) string {
	spec := d.Name + "@" + d.Version
	switch manager {
	case "pnpm":
		return "pnpm add -Dw " + spec
	case "yarn":
		return "yarn add -D " + spec
	case "bun":
		return "bun add -d " + spec
	default:
		return "npm install --save-dev " + spec
	}
}

// Write prints what happened, in the same shape as the plan, so the two can
// be read against each other.
func (r Report) Write(w io.Writer) error {
	b := &strings.Builder{}
	fmt.Fprintf(b, "%s created, %s overwritten, %s unchanged\n",
		plural(len(r.Created), "file", "files"),
		plural(len(r.Overwritten), "file", "files"),
		plural(len(r.Unchanged), "file", "files"))
	for _, path := range r.Created {
		fmt.Fprintf(b, "  created      %s\n", path)
	}
	for _, path := range r.Overwritten {
		fmt.Fprintf(b, "  overwritten  %s\n", path)
	}
	for _, path := range r.Unchanged {
		fmt.Fprintf(b, "  unchanged    %s\n", path)
	}
	// Manual is deliberately not printed here. It belongs to the plan, which is
	// printed immediately above this in the same invocation, and repeating three
	// paragraphs makes the report harder to read rather than more complete.
	_, err := io.WriteString(w, b.String())
	return err
}

// data is what the templates see. It is a flat struct rather than the Plan so
// that adding a field to Plan cannot silently change generated output.
type data struct {
	Plugins          []config.Plugin
	Timeout          string
	ContextLines     int
	RulesDir         string
	SecretsAnalyzers []string
	Provider         string
	Repo             string

	NeedsNode      bool
	NodeVersion    string
	PackageManager string
	InstallCommand string
	// CacheKey is what actions/setup-node is told to cache, or empty when it has
	// no support for the detected manager.
	CacheKey string
	// NeedsCorepack puts `corepack enable` before setup-node. pnpm and yarn are not
	// on a hosted runner, and setup-node's own cache resolution shells out to the
	// manager — so without this the cache step fails before the install runs.
	NeedsCorepack    bool
	ReviewerVersion  string
	ReviewerChecksum string

	// NeedsModelSecret puts the model credential into the generated workflow's
	// env. It follows the provider, not laneB.enabled, so that turning the model
	// lane on is the one edit the exit criterion promises rather than two.
	NeedsModelSecret bool
	// ProviderWorksInCI is false for the subscription paths, and the generated
	// workflow says so where someone would otherwise wonder why the lane is quiet.
	ProviderWorksInCI bool
	// ProviderVars are the provider's non-secret variables. They go into the
	// workflow as Actions variables: the model name and base URL are configuration,
	// not credentials, and both are read only from the environment — so without
	// them the lane has a key and nothing to call.
	ProviderVars []string
	// ProviderEnv are the variables .env.example names.
	ProviderEnv []string

	// Projects is written only for a repository that has them, so a single-package
	// repository's generated config is byte-for-byte what it was before monorepo
	// support existed.
	Projects []string
}

// templateData derives the generated files from the detection and from
// config.Defaults(), so a default the engine changes cannot drift from the value
// the installer writes.
func templateData(p Plan, pluginVersion string) data {
	defaults := config.Defaults()
	d := p.Detected

	out := data{
		Timeout:          durationString(defaults.Analyzers.Timeout),
		ContextLines:     defaults.Analyzers.ContextLines,
		RulesDir:         defaults.Rules.Dir,
		SecretsAnalyzers: defaults.Gate.SecretsAnalyzers,
		Repo:             d.Remote,

		NeedsNode:        len(p.DevDependencies) > 0,
		NodeVersion:      nodeVersion,
		PackageManager:   packageManagerOrNpm(d.PackageManager),
		ReviewerVersion:  workflowVersion(pluginVersion),
		ReviewerChecksum: checksumPlaceholder,

		Provider:          p.Options.Provider.ID,
		NeedsModelSecret:  p.Options.Provider.NeedsAPIKey(),
		ProviderWorksInCI: p.Options.Provider.ID == "" || p.Options.Provider.WorksInCI,
		ProviderEnv:       p.Options.Provider.Env,
		ProviderVars:      nonSecret(p.Options.Provider.Env),
		Projects:          p.Projects,
	}
	out.InstallCommand = ciInstallCommand(out.PackageManager)
	out.CacheKey = setupNodeCache(out.PackageManager)
	out.NeedsCorepack = out.PackageManager == "pnpm" || out.PackageManager == "yarn"

	if len(p.DevDependencies) > 0 {
		out.Plugins = []config.Plugin{{
			ID:      "typescript",
			Command: "node",
			// dist/main.js, not the package's "main" field: that names dist/serve.js,
			// which is the library the plugin is built from. Pointing the config at
			// it produces a process that exits 0 without ever answering the
			// handshake, and a review that reports "no analyzers ran".
			Args: []string{"node_modules/" + PluginPackage + "/dist/main.js"},
		}}
	}
	return out
}

// workflowVersion is the release the generated workflow pins. The Action
// downloads a release archive, so a version that is not a release has nothing to
// download and is left obviously unfilled rather than written as a ref that does
// not resolve.
func workflowVersion(binaryVersion string) string {
	if version, note := pluginVersionFor(binaryVersion); note == "" {
		return version
	}
	return versionPlaceholder
}

// nonSecret splits the credential out of a provider's variables. The rest are
// configuration and belong in Actions variables rather than secrets.
func nonSecret(env []string) []string {
	var out []string
	for _, name := range env {
		if name != config.EnvModelAPIKey {
			out = append(out, name)
		}
	}
	return out
}

func packageManagerOrNpm(manager string) string {
	if manager == "" {
		return "npm"
	}
	return manager
}

// setupNodeCache is the manager actions/setup-node knows how to cache. It accepts
// npm, yarn and pnpm and nothing else: `cache: bun` fails the step outright, so
// bun gets no cache rather than a broken one.
func setupNodeCache(manager string) string {
	switch manager {
	case "npm", "yarn", "pnpm":
		return manager
	default:
		return ""
	}
}

// ciInstallCommand is the reproducible-install form, which is not what a person
// runs by hand: CI must install the lockfile exactly, or the workflow reviews a
// dependency tree nobody committed.
func ciInstallCommand(manager string) string {
	switch manager {
	case "pnpm":
		return "pnpm install --frozen-lockfile"
	case "yarn":
		return "yarn install --immutable"
	case "bun":
		return "bun install --frozen-lockfile"
	default:
		return "npm ci"
	}
}

// durationString writes a duration the way a person would in YAML: "2m", not
// "2m0s". Only a trailing whole-zero component is dropped, so "1m30s" survives —
// trimming the literal "0s" would turn it into "1m3".
func durationString(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}
