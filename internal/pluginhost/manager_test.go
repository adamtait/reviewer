// SPDX-License-Identifier: MIT

package pluginhost

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/config"
	"github.com/adamtait/reviewer/pkg/finding"
	"github.com/adamtait/reviewer/pkg/plugin"
)

// script writes an executable shell plugin and returns a config entry for it.
func script(t *testing.T, id, body string) config.Plugin {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fault-injection plugins are POSIX shell scripts")
	}
	path := filepath.Join(t.TempDir(), id+".sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return config.Plugin{ID: id, Command: path}
}

func cfgWith(t *testing.T, timeout time.Duration, plugins ...config.Plugin) config.Config {
	t.Helper()
	c := config.Defaults()
	c.Root = t.TempDir()
	c.Analyzers.Timeout = timeout
	c.Plugins = plugins
	return c
}

// pidsOf captures the live child pids so a test can prove none survive Close.
func pidsOf(m *Manager) []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	var pids []int
	for _, c := range m.conns {
		if c.cmd.Process != nil {
			pids = append(pids, c.cmd.Process.Pid)
		}
	}
	return pids
}

// assertReaped fails if any pid is still alive. Signal 0 is the portable
// "does this process exist" probe; a reaped child answers ESRCH.
func assertReaped(t *testing.T, pids []int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for _, pid := range pids {
		for {
			err := syscall.Kill(pid, 0)
			if err == syscall.ESRCH {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("process %d survived Close", pid)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

const goodPlugin = `
while IFS= read -r frame; do
  case "$frame" in
    *'"type":"hello"'*)    printf '{"type":"hello","protocol":1,"plugin":"good","version":"1.0.0"}\n' ;;
    *'"type":"describe"'*) printf '{"type":"describe","analyzers":[{"id":"demo","lane":"deterministic","order":10,"available":true},{"id":"absent","lane":"deterministic","order":20,"available":false,"unavailable":"nothing to do here"}]}\n' ;;
    *'"type":"analyze"'*)  printf '{"type":"findings","analyzer":"demo","findings":[{"fingerprint":"","ruleId":"demo/found","lane":"deterministic","confidence":"high","severity":"warning","file":"a.ts","line":2,"message":"found it"}]}\n' ;;
    *'"type":"bye"'*)      exit 0 ;;
  esac
done
`

func TestHappyPath(t *testing.T) {
	var log bytes.Buffer
	m := New("test/1.0", &log)
	cfg := cfgWith(t, 5*time.Second, script(t, "good", goodPlugin))

	if err := m.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	pids := pidsOf(m)
	defer func() { m.Close(); assertReaped(t, pids) }()

	got := m.Analyzers()
	if len(got) != 1 {
		t.Fatalf("want only the available analyzer, got %+v", got)
	}
	if got[0].ID != "demo" || got[0].PluginID != "good" {
		t.Fatalf("bad registration: %+v", got[0])
	}

	findings, warnings, err := m.Analyze(context.Background(), "good", "demo", plugin.AnalyzeRequest{Root: cfg.Root})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].RuleID != "demo/found" {
		t.Fatalf("want one finding, got %+v", findings)
	}
	if findings[0].Lane != finding.LaneDeterministic {
		t.Fatalf("want the declared lane preserved, got %q", findings[0].Lane)
	}
	if len(warnings) != 0 {
		t.Fatalf("want no warnings, got %v", warnings)
	}
	if len(m.Warnings()) != 0 {
		t.Fatalf("want no manager warnings, got %v", m.Warnings())
	}
}

// The four containment cases from the plan: no handshake, a version mismatch, a
// death mid-frame, and a hang. Each must yield zero findings, a warning, and no
// surviving process.
func TestFaultContainment(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "never handshakes",
			body: "while IFS= read -r frame; do :; done\n",
			want: "no response within",
		},
		{
			name: "exits without a word",
			body: "exit 0\n",
			want: "plugin closed its stream",
		},
		{
			name: "speaks a different protocol",
			body: `while IFS= read -r f; do printf '{"type":"hello","protocol":99,"plugin":"future"}\n'; done` + "\n",
			want: "speaks protocol 99",
		},
		{
			name: "dies mid-frame",
			body: `while IFS= read -r f; do printf '{"type":"hello","protoc'; exit 1; done` + "\n",
			want: "not a protocol frame",
		},
		{
			name: "writes garbage to stdout",
			body: `while IFS= read -r f; do echo "Debugger listening on port 9229"; done` + "\n",
			want: "not a protocol frame",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var log bytes.Buffer
			m := New("test/1.0", &log)
			// Short timeout: the hang cases must not make the suite slow.
			cfg := cfgWith(t, 300*time.Millisecond, script(t, "faulty", tc.body))

			var pids []int
			done := make(chan struct{})
			go func() {
				defer close(done)
				_ = m.Start(context.Background(), cfg)
				pids = pidsOf(m)
			}()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("Start hung; the handshake deadline did not fire")
			}

			if got := m.Analyzers(); len(got) != 0 {
				t.Fatalf("a faulty plugin must contribute no analyzers, got %+v", got)
			}
			warns := m.Warnings()
			if len(warns) != 1 {
				t.Fatalf("want exactly one warning, got %v", warns)
			}
			if !strings.Contains(warns[0], tc.want) {
				t.Fatalf("want a warning containing %q, got %q", tc.want, warns[0])
			}
			m.Close()
			assertReaped(t, pids)
		})
	}
}

// A plugin that hangs during analysis, rather than during the handshake, is the
// case the per-analyzer timeout exists for.
func TestHangDuringAnalyzeIsBounded(t *testing.T) {
	body := `
while IFS= read -r frame; do
  case "$frame" in
    *'"type":"hello"'*)    printf '{"type":"hello","protocol":1,"plugin":"sleeper","version":"1.0.0"}\n' ;;
    *'"type":"describe"'*) printf '{"type":"describe","analyzers":[{"id":"slow","lane":"deterministic","order":10,"available":true}]}\n' ;;
    *'"type":"analyze"'*)  sleep 30 ;;
    *'"type":"bye"'*)      exit 0 ;;
  esac
done
`
	var log bytes.Buffer
	m := New("test/1.0", &log)
	cfg := cfgWith(t, 300*time.Millisecond, script(t, "sleeper", body))
	if err := m.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	pids := pidsOf(m)

	start := time.Now()
	findings, _, err := m.Analyze(context.Background(), "sleeper", "slow", plugin.AnalyzeRequest{})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("want a timeout error")
	}
	if !strings.Contains(err.Error(), "no response within") {
		t.Fatalf("want a timeout error, got %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want no findings, got %+v", findings)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("the timeout did not bound the wait: took %s", elapsed)
	}
	// The plugin is not trusted afterwards: the stream position is unknown.
	if _, _, err := m.Analyze(context.Background(), "sleeper", "slow", plugin.AnalyzeRequest{}); err == nil {
		t.Fatal("want the plugin to be dropped after a timeout")
	}
	m.Close()
	assertReaped(t, pids)
}

func TestDescribeIsValidated(t *testing.T) {
	tests := []struct{ name, describe, want string }{
		{
			"analyzer with no id",
			`{"type":"describe","analyzers":[{"lane":"deterministic","order":10,"available":true}]}`,
			"no id",
		},
		{
			"duplicate analyzer id",
			`{"type":"describe","analyzers":[{"id":"a","lane":"deterministic","order":1,"available":true},{"id":"a","lane":"deterministic","order":2,"available":true}]}`,
			`described analyzer "a" twice`,
		},
		{
			// An unrecognised lane cannot be gated, so it is refused (ADR-0005).
			"unknown lane",
			`{"type":"describe","analyzers":[{"id":"a","lane":"vibes","order":1,"available":true}]}`,
			`declares lane "vibes"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`
while IFS= read -r frame; do
  case "$frame" in
    *'"type":"hello"'*)    printf '{"type":"hello","protocol":1,"plugin":"odd","version":"1.0.0"}\n' ;;
    *'"type":"describe"'*) printf '%s\n' ;;
    *'"type":"bye"'*)      exit 0 ;;
  esac
done
`, tc.describe)
			var log bytes.Buffer
			m := New("test/1.0", &log)
			cfg := cfgWith(t, time.Second, script(t, "odd", body))
			_ = m.Start(context.Background(), cfg)
			pids := pidsOf(m)

			warns := m.Warnings()
			if len(warns) != 1 || !strings.Contains(warns[0], tc.want) {
				t.Fatalf("want a warning containing %q, got %v", tc.want, warns)
			}
			m.Close()
			assertReaped(t, pids)
		})
	}
}

// One malformed finding must not discard the rest: the analyzer is usually right
// about the others.
func TestInvalidFindingsAreDroppedIndividually(t *testing.T) {
	body := `
while IFS= read -r frame; do
  case "$frame" in
    *'"type":"hello"'*)    printf '{"type":"hello","protocol":1,"plugin":"mixed","version":"1.0.0"}\n' ;;
    *'"type":"describe"'*) printf '{"type":"describe","analyzers":[{"id":"mixed","lane":"deterministic","order":10,"available":true}]}\n' ;;
    *'"type":"analyze"'*)  printf '{"type":"findings","analyzer":"mixed","findings":[{"ruleId":"good/one","lane":"deterministic","confidence":"high","severity":"info","file":"a.ts","line":1,"message":"fine"},{"ruleId":"bad/one","lane":"llm","confidence":"high","severity":"info","file":"a.ts","line":1,"message":"no evidence"}]}\n' ;;
    *'"type":"bye"'*)      exit 0 ;;
  esac
done
`
	var log bytes.Buffer
	m := New("test/1.0", &log)
	cfg := cfgWith(t, 5*time.Second, script(t, "mixed", body))
	if err := m.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	pids := pidsOf(m)
	defer func() { m.Close(); assertReaped(t, pids) }()

	findings, warnings, err := m.Analyze(context.Background(), "mixed", "mixed", plugin.AnalyzeRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].RuleID != "good/one" {
		t.Fatalf("want the valid finding kept, got %+v", findings)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "evidence is required") {
		t.Fatalf("want the drop explained, got %v", warnings)
	}
}

func TestPluginStderrReachesTheRunLog(t *testing.T) {
	body := `
echo "starting up" >&2
while IFS= read -r frame; do
  case "$frame" in
    *'"type":"hello"'*)    printf '{"type":"hello","protocol":1,"plugin":"chatty","version":"1.0.0"}\n' ;;
    *'"type":"describe"'*) echo "describing" >&2; printf '{"type":"describe","analyzers":[]}\n' ;;
    *'"type":"bye"'*)      exit 0 ;;
  esac
done
`
	var log bytes.Buffer
	m := New("test/1.0", &log)
	cfg := cfgWith(t, 5*time.Second, script(t, "chatty", body))
	if err := m.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	pids := pidsOf(m)
	m.Close()
	assertReaped(t, pids)

	for _, want := range []string{"chatty: starting up", "chatty: describing"} {
		if !strings.Contains(log.String(), want) {
			t.Fatalf("want %q in the run log, got:\n%s", want, log.String())
		}
	}
}

func TestMissingCommandIsAWarningNotAFailure(t *testing.T) {
	var log bytes.Buffer
	m := New("test/1.0", &log)
	cfg := cfgWith(t, time.Second, config.Plugin{ID: "ghost", Command: "definitely-not-installed-xyz"})
	if err := m.Start(context.Background(), cfg); err != nil {
		t.Fatalf("a missing plugin binary must not fail Start, got %v", err)
	}
	warns := m.Warnings()
	if len(warns) != 1 || !strings.Contains(warns[0], "ghost") {
		t.Fatalf("want one warning naming the plugin, got %v", warns)
	}
	m.Close()
}

func TestCloseIsIdempotent(t *testing.T) {
	var log bytes.Buffer
	m := New("test/1.0", &log)
	cfg := cfgWith(t, 5*time.Second, script(t, "good", goodPlugin))
	if err := m.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	pids := pidsOf(m)
	m.Close()
	m.Close()
	assertReaped(t, pids)
}
