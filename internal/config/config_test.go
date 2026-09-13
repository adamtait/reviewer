// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func noEnv(string) string { return "" }

func envFrom(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".review")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestResolveWithNoFileYieldsDefaults(t *testing.T) {
	c, _, err := Resolve(t.TempDir(), "", noEnv)
	if err != nil {
		t.Fatalf("a repository with no config file must resolve: %v", err)
	}
	if c.Analyzers.Timeout != 2*time.Minute {
		t.Fatalf("want the default timeout, got %v", c.Analyzers.Timeout)
	}
	if c.LaneB.Enabled {
		t.Fatal("lane B must be off unless explicitly enabled")
	}
	if c.Root == "" || !filepath.IsAbs(c.Root) {
		t.Fatalf("want an absolute root, got %q", c.Root)
	}
}

func TestResolveReadsTheFile(t *testing.T) {
	root := writeConfig(t, `
plugins:
  - id: typescript
    command: node
    args: ["node_modules/@adamtait/reviewer-plugin-typescript/dist/serve.js"]
analyzers:
  timeout: 90s
  contextLines: 12
rules:
  dir: .review/rules
laneB:
  enabled: false
  provider: gemini
`)
	c, _, err := Resolve(root, "", noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Plugins) != 1 || c.Plugins[0].ID != "typescript" {
		t.Fatalf("want one plugin named typescript, got %+v", c.Plugins)
	}
	if c.Analyzers.Timeout != 90*time.Second {
		t.Fatalf("want 90s, got %v", c.Analyzers.Timeout)
	}
	if c.LaneB.Provider != "gemini" {
		t.Fatalf("want the gemini provider, got %q", c.LaneB.Provider)
	}
	// Defaults survive where the file is silent.
	if c.LaneB.PromptDir != ".review/prompts" {
		t.Fatalf("want the default prompt dir, got %q", c.LaneB.PromptDir)
	}
}

func TestUnknownKeyIsAnError(t *testing.T) {
	root := writeConfig(t, "analyzers:\n  timeuot: 30s\n")
	_, _, err := Resolve(root, "", noEnv)
	if err == nil {
		t.Fatal("a misspelled key must not be silently ignored")
	}
	if !strings.Contains(err.Error(), "timeuot") {
		t.Fatalf("want the offending key named in the error, got %v", err)
	}
	if !strings.Contains(err.Error(), "config.yaml") {
		t.Fatalf("want the file named in the error, got %v", err)
	}
}

func TestEnvOverlay(t *testing.T) {
	root := writeConfig(t, "laneB:\n  enabled: false\n  provider: anthropic\n")

	c, s, err := Resolve(root, "", envFrom(map[string]string{
		EnvLaneB:        "on",
		EnvModelName:    "some-model",
		EnvModelAPIKey:  "secret-value",
		EnvModelBaseURL: "https://example.invalid",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !c.LaneB.Enabled {
		t.Fatal("REVIEW_LLM=on must enable lane B")
	}
	if c.LaneB.Model != "some-model" {
		t.Fatalf("want the model from the environment, got %q", c.LaneB.Model)
	}
	if s.ModelAPIKey != "secret-value" {
		t.Fatal("want the key carried in Secrets")
	}
	if strings.Contains(s.String(), "secret-value") {
		t.Fatalf("Secrets.String must not reveal the key, got %q", s.String())
	}

	off, _, err := Resolve(root, "", envFrom(map[string]string{EnvLaneB: "off"}))
	if err != nil {
		t.Fatal(err)
	}
	if off.LaneB.Enabled {
		t.Fatal("REVIEW_LLM=off must disable lane B")
	}

	if _, _, err := Resolve(root, "", envFrom(map[string]string{EnvLaneB: "maybe"})); err == nil {
		t.Fatal("an unparseable REVIEW_LLM must be an error, not a silent default")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Config)
		want string
	}{
		{"defaults are valid", func(*Config) {}, ""},
		{"plugin without id", func(c *Config) {
			c.Plugins = []Plugin{{Command: "node"}}
		}, "id is required"},
		{"plugin without command", func(c *Config) {
			c.Plugins = []Plugin{{ID: "ts"}}
		}, "command is required"},
		{"duplicate plugin id", func(c *Config) {
			c.Plugins = []Plugin{{ID: "ts", Command: "node"}, {ID: "ts", Command: "node"}}
		}, "duplicate plugin id"},
		{"zero timeout", func(c *Config) { c.Analyzers.Timeout = 0 }, "analyzers.timeout"},
		{"only and skip together", func(c *Config) {
			c.Analyzers.Only, c.Analyzers.Skip = []string{"a"}, []string{"b"}
		}, "only or skip, not both"},
		{"lane B on without a provider", func(c *Config) { c.LaneB.Enabled = true }, "laneB.provider"},
		{"unknown provider", func(c *Config) { c.LaneB.Provider = "telepathy" }, "is not one of"},
		{"absolute guidance path", func(c *Config) { c.Guidance = []string{"/etc/AGENTS.md"} }, "must be relative"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := Defaults()
			tc.mut(&c)
			err := c.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestRootIsNotSettableFromTheFile(t *testing.T) {
	// `root` is yaml:"-", so naming it is an unknown key rather than an override.
	root := writeConfig(t, "root: /somewhere/else\n")
	_, _, err := Resolve(root, "", noEnv)
	if err == nil || !strings.Contains(err.Error(), "root") {
		t.Fatalf("want the file to be rejected for naming root, got %v", err)
	}
}

// TestNoEndpointsInTheSource is the seam guard (ADR-0004). Nothing in the core may
// name a host: every endpoint arrives through config or the environment.
func TestNoEndpointsInTheSource(t *testing.T) {
	url := regexp.MustCompile(`https?://[a-zA-Z0-9]`)
	err := filepath.WalkDir("..", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(body), "\n") {
			if url.MatchString(line) {
				t.Errorf("%s:%d names a host, which must come from config instead: %s", path, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
