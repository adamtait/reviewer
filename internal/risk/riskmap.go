// SPDX-License-Identifier: MIT

package risk

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/adamtait/reviewer/internal/glob"
)

// MapPath is where a repository keeps its additions to the risk map. It is a
// file of its own, not a key in .review/config.yaml: the config decoder
// rejects unknown keys, so a new key there would make every older pinned
// binary reject the whole config and skip review.
const MapPath = ".review/risk.yaml"

// defaultPatterns are the built-in patterns for each path label, exactly the
// table in the design (docs/designs/risk-gated-merge.md, Part 1).
var defaultPatterns = map[Label][]string{
	LabelAuth:  {"**/auth/**", "**/*auth*.*", "**/permissions/**", "**/crypto/**", "**/payments/**", "**/billing/**"},
	LabelData:  {"**/migrations/**", "**/*.sql", "**/schema.*", "**/prisma/**"},
	LabelCI:    {".github/**", ".review/**", "AGENTS.md", "CLAUDE.md", "GEMINI.md", "**/SKILL.md", ".claude/**", ".cursorrules"},
	LabelDeps:  {"**/package.json", "**/package-lock.json", "**/pnpm-lock.yaml", "**/yarn.lock", "**/go.mod", "**/go.sum", "**/Cargo.toml", "**/Cargo.lock", "**/requirements*.txt", "**/pyproject.toml", "**/poetry.lock"},
	LabelInfra: {"**/*.tf", "**/k8s/**", "**/helm/**", "**/Dockerfile*", "**/docker-compose*.yml"},
}

// defaults is defaultPatterns compiled, once. A default that does not compile
// is a bug in this file, and panics at start-up rather than matching nothing.
var defaults = func() map[Label][]glob.Pattern {
	out := map[Label][]glob.Pattern{}
	for label, patterns := range defaultPatterns {
		for _, p := range patterns {
			out[label] = append(out[label], glob.MustCompile(p))
		}
	}
	return out
}()

// Map is the risk map: the built-in patterns for each path label, plus what a
// repository's risk.yaml adds.
//
// The defaults are not held in the Map, so they cannot be removed: the zero
// Map is the defaults alone, and a repository's patterns are matched as a list
// of their own and ORed with the defaults. A `!` in risk.yaml therefore
// narrows only that file's own patterns, never a default.
type Map struct {
	added map[Label][]glob.Pattern
}

// Defaults is the risk map with no repository additions.
func Defaults() Map { return Map{} }

// Match reports whether a path matches a label's patterns. It is false for a
// label that is not a path label.
func (m Map) Match(label Label, path string) bool {
	return glob.MatchList(defaults[label], path) || glob.MatchList(m.added[label], path)
}

// Added returns the patterns a repository added to a label, as written.
func (m Map) Added(label Label) []string {
	var out []string
	for _, p := range m.added[label] {
		out = append(out, p.String())
	}
	return out
}

// mapFile is the shape of risk.yaml.
type mapFile struct {
	Version *int                `yaml:"version"`
	Add     map[string][]string `yaml:"add"`
}

// Parse reads a risk.yaml. It returns the map, warnings about entries that
// have no effect, and an error if the file cannot be used. On an error the
// Map is the defaults alone, which is what the record then uses, and the
// error is the reason the comment gives.
//
// A file with a key other than `version` and `add`, a `version` other than 1
// or none, a label under `add` that is not a path label, or a pattern that
// does not compile, is an error. Rejecting the whole file is deliberate: a
// partly applied file is a record that differs from what its author wrote, in
// a way nobody is told about.
func Parse(raw []byte) (Map, []string, error) {
	var file mapFile
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	switch err := dec.Decode(&file); {
	case errors.Is(err, io.EOF):
		return Defaults(), nil, errors.New("the file is empty; it needs `version: 1`")
	case err != nil:
		return Defaults(), nil, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Defaults(), nil, errors.New("the file holds more than one YAML document")
	}

	switch {
	case file.Version == nil:
		return Defaults(), nil, errors.New("`version` is missing; it must be 1")
	case *file.Version != 1:
		return Defaults(), nil, fmt.Errorf("version %d is not supported; it must be 1", *file.Version)
	}

	var names []string
	for name := range file.Add {
		names = append(names, name)
	}
	sort.Strings(names)

	m := Map{added: map[Label][]glob.Pattern{}}
	var warnings []string
	for _, name := range names {
		if !isPathLabel(name) {
			return Defaults(), nil, fmt.Errorf("add: %q is not a label patterns can be added to; the labels are %s", name, pathLabelList())
		}
		label := Label(name)
		patterns := file.Add[name]
		if len(patterns) == 0 {
			warnings = append(warnings, fmt.Sprintf("add: %s lists no patterns", label))
		}
		seen := map[string]bool{}
		for _, d := range defaultPatterns[label] {
			seen[d] = true
		}
		for _, raw := range patterns {
			p, err := glob.Compile(raw)
			if err != nil {
				return Defaults(), nil, fmt.Errorf("add: %s: %w", label, err)
			}
			if seen[raw] {
				warnings = append(warnings, fmt.Sprintf("add: %s: %q is already a pattern for this label", label, raw))
				continue
			}
			seen[raw] = true
			m.added[label] = append(m.added[label], p)
		}
	}
	if len(file.Add) == 0 {
		warnings = append(warnings, "the file adds no patterns")
	}
	return m, warnings, nil
}

func pathLabelList() string {
	var names []string
	for _, l := range PathLabels {
		names = append(names, string(l))
	}
	return strings.Join(names, ", ")
}
