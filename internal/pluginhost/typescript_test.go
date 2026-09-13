// SPDX-License-Identifier: MIT

package pluginhost_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/config"
	"github.com/adamtait/reviewer/internal/pluginhost"
)

// TestTypeScriptPluginHandshake drives the real Node plugin from the real Go host.
// The two sides mirror one protocol by hand, so this is the test that catches a
// drift between them — and it is the whole reason a Go core is affordable here.
func TestTypeScriptPluginHandshake(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "..", "..", "plugins", "typescript", "dist", "main.js")
	if _, err := os.Stat(entry); err != nil {
		t.Skip("the TypeScript plugin is not built; run `npm --prefix plugins/typescript run build`")
	}

	var log bytes.Buffer
	m := pluginhost.New("test/1.0", &log)

	cfg := config.Defaults()
	cfg.Root = t.TempDir()
	cfg.Analyzers.Timeout = 30 * time.Second
	cfg.Plugins = []config.Plugin{{ID: "typescript", Command: node, Args: []string{entry}}}

	if err := m.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	if w := m.Warnings(); len(w) != 0 {
		t.Fatalf("the handshake must be clean, got %v\nplugin log:\n%s", w, log.String())
	}
	// The point is that a descriptor crossed the language boundary intact: the
	// lane, order and availability the TypeScript side declared are what the Go
	// side registered.
	got := m.Analyzers()
	if len(got) == 0 {
		t.Fatalf("want the plugin's analyzers registered, got none\nplugin log:\n%s", log.String())
	}
	byID := map[string]bool{}
	for _, a := range got {
		byID[a.ID] = true
		if a.Lane != "deterministic" && a.Lane != "llm" {
			t.Fatalf("analyzer %s crossed the boundary with lane %q", a.ID, a.Lane)
		}
		if a.PluginID != "typescript" {
			t.Fatalf("want the plugin id attributed, got %q", a.PluginID)
		}
	}
	if !byID["tsc"] {
		t.Fatalf("want the tsc analyzer registered, got %+v", got)
	}
}
