// SPDX-License-Identifier: MIT

package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/internal/config"
)

// examplePath is the documented configuration a stranger copies from.
const examplePath = "../../examples/config.minimal.yaml"

// TestTheExampleConfigLoads is the only thing that keeps documentation honest.
//
// The loader rejects unknown keys, so a renamed or removed field fails here
// rather than in the reader's repository. Proofreading does not catch that: the
// example stays plausible long after the code it documents has moved.
func TestTheExampleConfigLoads(t *testing.T) {
	root := t.TempDir()
	raw, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("reading the example: %v", err)
	}
	path := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := config.Resolve(root, path, func(string) string { return "" })
	if err != nil {
		t.Fatalf("the documented example does not load: %v", err)
	}

	// Spot-check the values a reader would actually rely on, rather than only
	// that it parsed: a file can decode cleanly and still document nothing.
	if len(cfg.Plugins) != 1 {
		t.Fatalf("plugins = %d, want the one TypeScript entry", len(cfg.Plugins))
	}
	if cfg.LaneB.Enabled {
		t.Error("the example enables the model lane; it must be opt-in")
	}
}

// TestTheExampleNamesTheExecutable guards the mistake the installer already made
// once: pointing the plugin command at the package's "main" field, which names
// the library rather than the executable. The result is a process that exits 0
// without answering the handshake and a review that reports "no analyzers ran"
// with nothing else visibly wrong — so it is worth a test in both places.
func TestTheExampleNamesTheExecutable(t *testing.T) {
	cfg := loadExample(t)
	args := strings.Join(cfg.Plugins[0].Args, " ")
	if !strings.HasSuffix(args, "/dist/main.js") {
		t.Errorf("plugin args = %q, want the dist/main.js executable", args)
	}
}

// TestTheExampleIsPublishable restates PR-43's exit criterion as a test. An
// endpoint or an organisation name reaching this file would ship in the one
// document a stranger copies verbatim.
func TestTheExampleIsPublishable(t *testing.T) {
	raw, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"http://", "https://"} {
		for _, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			// A commented URL explaining what a reader must fill in is the point
			// of the field; a configured one is the leak (ADR-0004).
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			if strings.Contains(line, bad) {
				t.Errorf("the example configures a URL: %s", trimmed)
			}
		}
	}
}

func loadExample(t *testing.T) config.Config {
	t.Helper()
	root := t.TempDir()
	raw, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Resolve(root, path, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
