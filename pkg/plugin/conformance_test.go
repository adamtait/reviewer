// SPDX-License-Identifier: MIT

package plugin_test

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/pkg/plugin"
)

// TestShellPluginConformance drives the POSIX-shell example plugin through the
// real codec. It is the guard on ADR-0025's claim that the protocol needs no
// language runtime, no SDK and no JSON library: if this test starts needing a
// dependency, the protocol has grown a requirement it should not have.
func TestShellPluginConformance(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the example plugin is a POSIX shell script")
	}
	script := filepath.Join("..", "..", "examples", "plugins", "shell-hello", "plugin.sh")

	var in bytes.Buffer
	w := plugin.NewWriter(&in)
	for _, f := range []plugin.Frame{
		{Type: plugin.TypeHello, Protocol: plugin.Protocol, Host: "conformance-test"},
		{Type: plugin.TypeDescribe},
		{Type: plugin.TypeAnalyze, Analyzer: "shell-hello", Request: &plugin.AnalyzeRequest{Root: "."}},
		{Type: plugin.TypeBye},
	} {
		if err := w.Write(f); err != nil {
			t.Fatal(err)
		}
	}

	cmd := exec.Command("sh", script)
	cmd.Stdin = &in
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("plugin exited with %v\nstderr:\n%s", err, stderr.String())
	}

	r := plugin.NewReader(bytes.NewReader(stdout.Bytes()))
	var frames []plugin.Frame
	for {
		f, err := r.Read()
		if errors.Is(err, plugin.ErrClosed) {
			break
		}
		if err != nil {
			t.Fatalf("plugin wrote a line that is not a frame: %v\nstdout:\n%s", err, stdout.String())
		}
		frames = append(frames, f)
	}

	if len(frames) != 3 {
		t.Fatalf("want hello, describe and findings; got %d frames:\n%s", len(frames), stdout.String())
	}
	if frames[0].Type != plugin.TypeHello || frames[0].Protocol != plugin.Protocol {
		t.Fatalf("bad handshake: %+v", frames[0])
	}
	if frames[0].Plugin != "shell-hello" {
		t.Fatalf("want the plugin to identify itself, got %q", frames[0].Plugin)
	}

	if len(frames[1].Analyzers) != 1 {
		t.Fatalf("want one descriptor, got %+v", frames[1].Analyzers)
	}
	d := frames[1].Analyzers[0]
	if d.ID != "shell-hello" || !d.Available || d.Lane != "deterministic" {
		t.Fatalf("bad descriptor: %+v", d)
	}

	if len(frames[2].Findings) != 1 {
		t.Fatalf("want one finding, got %+v", frames[2].Findings)
	}
	// The finding must satisfy the schema every other analyzer satisfies, apart
	// from the fingerprint, which the core fills in.
	f := frames[2].Findings[0]
	f.Fingerprint = "filled-in-by-the-core"
	if err := f.Validate(); err != nil {
		t.Fatalf("the example plugin emitted an invalid finding: %v", err)
	}

	// Diagnostics belong on stderr; stdout is the protocol channel.
	if !strings.Contains(stderr.String(), "shell-hello: started") {
		t.Fatalf("want the plugin's own logging on stderr, got %q", stderr.String())
	}
}
