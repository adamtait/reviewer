// SPDX-License-Identifier: MIT

// Package config is the seam between this repository and the repositories it
// reviews (ADR-0004). Every repo-specific value — plugin commands, tool paths,
// rule directories, model endpoints, guidance documents — reaches the core
// through Config and nowhere else, which is what keeps this repository free of
// anything that could not be published.
package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config is the resolved configuration for one run.
type Config struct {
	// Root is the absolute path of the repository under review. Not settable from
	// the file: it is where the config was found.
	Root string `yaml:"-"`

	Plugins   []Plugin          `yaml:"plugins"`
	Analyzers Analyzers         `yaml:"analyzers"`
	Tools     map[string]Tool   `yaml:"tools"`
	Rules     Rules             `yaml:"rules"`
	Guidance  []string          `yaml:"guidance"`
	Projects  []string          `yaml:"projects"`
	Gate      Gate              `yaml:"gate"`
	LaneB     LaneB             `yaml:"laneB"`
	GitHub    GitHub            `yaml:"github"`
	Baselines map[string]string `yaml:"baselines"`
}

// Plugin describes how to start one analyzer plugin. Command is resolved relative
// to Root when it is not absolute and not on PATH, so a repository can ship a
// plugin without installing it globally.
type Plugin struct {
	ID      string            `yaml:"id"`
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args"`
	Env     map[string]string `yaml:"env"`
	// Timeout bounds the whole plugin process. Zero means Analyzers.Timeout.
	Timeout time.Duration `yaml:"timeout"`
}

// Analyzers holds run-wide analysis policy.
type Analyzers struct {
	// Timeout bounds a single analyzer. Exceeding it kills the analyzer's process
	// group and the run continues without it (ADR-0013).
	Timeout time.Duration `yaml:"timeout"`
	// Only and Skip filter by analyzer ID. Only wins when both are set.
	Only []string `yaml:"only"`
	Skip []string `yaml:"skip"`
	// ContextLines is how much surrounding source a finding carries into lane B.
	ContextLines int `yaml:"contextLines"`
}

// Tool is an external binary an analyzer spawns. Version is advisory: it is
// reported when the binary on PATH disagrees, never enforced, because a run must
// degrade to a warning rather than fail (ADR-0013).
type Tool struct {
	Path    string `yaml:"path"`
	Version string `yaml:"version"`
}

// Rules points at the Opengrep rule directory for this repository. The rules
// themselves live in the repository under review, never here.
type Rules struct {
	Dir string `yaml:"dir"`
}

// LaneB configures the model-backed lane. Disabled by default: enabling it is a
// deliberate edit in the destination repository, never an installer default.
type LaneB struct {
	Enabled bool `yaml:"enabled"`
	// Provider is one of the six supported access paths. The endpoint and
	// credentials for it come from the environment, never from this file.
	Provider   string `yaml:"provider"`
	Model      string `yaml:"model"`
	PromptDir  string `yaml:"promptDir"`
	Invalidate bool   `yaml:"invalidate"`
}

// Gate configures the secrets gate.
type Gate struct {
	// SecretsAnalyzers names the analyzers whose successful completion means the
	// diff was actually checked. It is a list rather than a constant because
	// gate.Decide matches findings by the "secrets/" rule namespace precisely so a
	// replacement scanner works — and a hardcoded id here would undo that by
	// leaving the model lane permanently blocked with "the scan could not run".
	SecretsAnalyzers []string `yaml:"secretsAnalyzers"`
}

// GitHub holds the behaviour of the GitHub reporter.
type GitHub struct {
	// APIBaseURL is where the API lives. No default is compiled in (ADR-0004);
	// the installer writes github.com's, and GitHub Enterprise works by changing
	// this line rather than the code.
	APIBaseURL string `yaml:"apiBaseUrl"`
	// Repo is "owner/name". Detected from the remote when unset.
	Repo string `yaml:"repo"`
	// GraphQLURL overrides the endpoint derived from APIBaseURL. Needed only for a
	// deployment whose REST and GraphQL paths are not github.com's or GitHub
	// Enterprise's shapes.
	GraphQLURL string `yaml:"graphqlUrl"`
	// SelfLogin is the account this tool's token posts as. Used to tell our own
	// threads from a human's when resolving stale ones. GitHub's /user endpoint
	// answers this for a personal token but returns 403 for the installation token
	// an Action runs with, so it can be stated instead.
	SelfLogin string `yaml:"selfLogin"`

	// ResolveStaleThreads is opt-in: resolving a thread is the only action this
	// tool takes on a human's conversation.
	ResolveStaleThreads bool `yaml:"resolveStaleThreads"`
}

// Defaults returns the configuration of a repository with no config file at all.
// Every field here is a policy decision, so this function is the one place to
// look for "what happens if you configure nothing".
func Defaults() Config {
	return Config{
		Analyzers: Analyzers{
			Timeout:      2 * time.Minute,
			ContextLines: 20,
		},
		Rules: Rules{Dir: ".review/rules"},
		Gate:  Gate{SecretsAnalyzers: []string{"gitleaks"}},
		LaneB: LaneB{
			Enabled:    false,
			PromptDir:  ".review/prompts",
			Invalidate: true,
		},
		Tools:     map[string]Tool{},
		Baselines: map[string]string{},
	}
}

// Validate reports every problem at once. A configuration error is a human's
// typo, and listing all of them beats a fix-one-rerun loop.
func (c Config) Validate() error {
	var problems []error

	seen := map[string]bool{}
	for i, p := range c.Plugins {
		switch {
		case p.ID == "":
			problems = append(problems, fmt.Errorf("plugins[%d]: id is required", i))
		case seen[p.ID]:
			problems = append(problems, fmt.Errorf("plugins[%d]: duplicate plugin id %q", i, p.ID))
		default:
			seen[p.ID] = true
		}
		if p.Command == "" {
			problems = append(problems, fmt.Errorf("plugins[%d] (%s): command is required", i, p.ID))
		}
		if p.Timeout < 0 {
			problems = append(problems, fmt.Errorf("plugins[%d] (%s): timeout must not be negative", i, p.ID))
		}
	}

	if len(c.Gate.SecretsAnalyzers) == 0 {
		// An empty list means nothing can ever mark the diff as checked, which
		// would block the model lane forever. Refuse it rather than fail closed
		// in a way nobody could diagnose.
		problems = append(problems, errors.New("gate.secretsAnalyzers: must name at least one analyzer"))
	}
	if c.Analyzers.Timeout <= 0 {
		problems = append(problems, errors.New("analyzers.timeout: must be greater than zero"))
	}
	if c.Analyzers.ContextLines < 0 {
		problems = append(problems, errors.New("analyzers.contextLines: must not be negative"))
	}
	if len(c.Analyzers.Only) > 0 && len(c.Analyzers.Skip) > 0 {
		problems = append(problems, errors.New("analyzers: set only or skip, not both"))
	}

	if c.LaneB.Enabled && c.LaneB.Provider == "" {
		problems = append(problems, errors.New("laneB.provider: required when laneB.enabled is true"))
	}
	if p := c.LaneB.Provider; p != "" && !validProvider(p) {
		problems = append(problems, fmt.Errorf("laneB.provider: %q is not one of %s", p, strings.Join(Providers(), ", ")))
	}

	for _, path := range append(append([]string{}, c.Guidance...), c.Rules.Dir) {
		if filepath.IsAbs(path) {
			problems = append(problems, fmt.Errorf("%q: paths must be relative to the repository root", path))
		}
	}

	return errors.Join(problems...)
}

// Providers lists the supported model access paths (ADR-0021). The endpoint for
// each is supplied by the environment; none is named in this repository.
func Providers() []string {
	return []string{"openai-compatible", "openai", "gemini", "anthropic", "claude-code", "codex"}
}

func validProvider(name string) bool {
	for _, p := range Providers() {
		if p == name {
			return true
		}
	}
	return false
}

// Environment variables read by the overlay. Names only — no values, no defaults,
// and in particular no endpoint ever appears in this repository.
const (
	EnvGitHubToken  = "GITHUB_TOKEN"
	EnvLaneB        = "REVIEW_LLM"
	EnvModelBaseURL = "REVIEW_MODEL_BASE_URL"
	EnvModelAPIKey  = "REVIEW_MODEL_API_KEY"
	EnvModelName    = "REVIEW_MODEL_NAME"
)

// Secrets carries credentials read from the environment. It is deliberately
// separate from Config so that Config can be logged and Secrets cannot.
type Secrets struct {
	ModelBaseURL string
	ModelAPIKey  string
	GitHubToken  string
}

// String hides the key, because the most common way to leak a credential is to
// print the struct that holds it.
func (s Secrets) String() string {
	key := "unset"
	if s.ModelAPIKey != "" {
		key = "set"
	}
	base := "unset"
	if s.ModelBaseURL != "" {
		base = "set"
	}
	token := "unset"
	if s.GitHubToken != "" {
		token = "set"
	}
	return fmt.Sprintf("Secrets{ModelBaseURL:%s ModelAPIKey:%s GitHubToken:%s}", base, key, token)
}

// applyEnv overlays environment settings onto a parsed config. Only three things
// are environment-settable: whether lane B runs at all, which model it names, and
// the credentials — everything else belongs in the file, under review.
func applyEnv(c *Config, s *Secrets, getenv func(string) string) error {
	if v := getenv(EnvLaneB); v != "" {
		switch strings.ToLower(v) {
		case "off", "0", "false", "no":
			c.LaneB.Enabled = false
		case "on", "1", "true", "yes":
			c.LaneB.Enabled = true
		default:
			enabled, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("%s: %q is not on or off", EnvLaneB, v)
			}
			c.LaneB.Enabled = enabled
		}
	}
	if v := getenv(EnvModelName); v != "" {
		c.LaneB.Model = v
	}
	s.ModelBaseURL = getenv(EnvModelBaseURL)
	s.ModelAPIKey = getenv(EnvModelAPIKey)
	s.GitHubToken = getenv(EnvGitHubToken)
	return nil
}

// Abs resolves a repository-relative path against the root.
func (c Config) Abs(rel string) string {
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(c.Root, rel)
}
