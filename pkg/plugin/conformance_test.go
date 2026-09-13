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
	// A real diff, not an empty one. An analyze request with no changed files
	// cannot distinguish a plugin that anchors its findings from one that reports
	// at a fixed location — and the fixed-location plugin is the one whose
	// findings the core silently drops (ADR-0007).
	req := &plugin.AnalyzeRequest{
		Root: ".",
		Changed: []plugin.ChangedFile{
			{Path: "src/cart.ts", Status: "added", Ranges: [][2]int{{4, 9}, {20, 21}}},
			{Path: "src/order.ts", Status: "modified", Ranges: [][2]int{{2, 2}}},
		},
	}

	for _, f := range []plugin.Frame{
		{Type: plugin.TypeHello, Protocol: plugin.Protocol, Host: "conformance-test"},
		{Type: plugin.TypeDescribe},
		{Type: plugin.TypeAnalyze, Analyzer: "shell-hello", Request: req},
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

	// Valid is not the same as visible. This is the assertion the documentation
	// tells every plugin author to write, so the example a stranger copies had
	// better pass it: before it did, the example reported at README.md:1 and the
	// core dropped its finding on every real run.
	if !onAChangedLine(req.Changed, f.File, f.Line) {
		t.Errorf("the example plugin reported %s at %s:%d, which is outside the diff "+
			"and would be dropped by the core", f.RuleID, f.File, f.Line)
	}

	// Diagnostics belong on stderr; stdout is the protocol channel.
	if !strings.Contains(stderr.String(), "shell-hello: started") {
		t.Fatalf("want the plugin's own logging on stderr, got %q", stderr.String())
	}
}

// onAChangedLine is the check the core applies before anything reaches a reader
// (ADR-0007). Restated here rather than imported so that this file stays a
// description of what a plugin author has to satisfy, independent of where the
// core happens to implement it.
func onAChangedLine(changed []plugin.ChangedFile, file string, line int) bool {
	for _, c := range changed {
		if c.Path != file {
			continue
		}
		for _, r := range c.Ranges {
			if line >= r[0] && line <= r[1] {
				return true
			}
		}
	}
	return false
}
