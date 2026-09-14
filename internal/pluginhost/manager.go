// SPDX-License-Identifier: MIT

// Package pluginhost starts analyzer plugins, speaks the protocol to them, and
// contains their failures.
//
// The contract the rest of the core relies on: a plugin that never handshakes,
// speaks the wrong protocol version, dies mid-frame, writes garbage to stdout or
// hangs forever produces zero findings, one warning, and no surviving process.
// Nothing a plugin does may fail the run (ADR-0013).
package pluginhost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/adamtait/reviewer/internal/config"
	"github.com/adamtait/reviewer/pkg/finding"
	"github.com/adamtait/reviewer/pkg/plugin"
)

// maxHandshake caps how long hello and describe may take. The actual deadline is
// the plugin's own timeout, capped here: a plugin that cannot introduce itself
// within its analysis budget is not going to analyze anything either, and 30s is
// long enough for a Node process to boot on a cold CI runner.
const maxHandshake = 30 * time.Second

func handshakeDeadline(pluginTimeout time.Duration) time.Duration {
	if pluginTimeout > 0 && pluginTimeout < maxHandshake {
		return pluginTimeout
	}
	return maxHandshake
}

// shutdownGrace is how long a plugin gets to exit after bye before its process
// group is killed. A variable rather than a constant so tests that exercise the
// kill path do not have to wait out the production value.
var shutdownGrace = 5 * time.Second

// Registered is one analyzer, and which plugin provides it.
type Registered struct {
	plugin.Descriptor
	PluginID string
}

// Manager owns every plugin process for one run.
type Manager struct {
	hostID string
	log    io.Writer

	mu    sync.Mutex
	conns []*conn
	warns []string
}

// New returns a Manager that writes plugin diagnostics to log.
func New(hostID string, log io.Writer) *Manager {
	if log == nil {
		log = io.Discard
	}
	return &Manager{hostID: hostID, log: log}
}

// Warnings returns everything that went wrong without failing the run. Callers
// surface these in the run log; they are the only trace a broken plugin leaves.
func (m *Manager) Warnings() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.warns...)
}

func (m *Manager) warnf(format string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.warns = append(m.warns, fmt.Sprintf(format, args...))
}

// Start spawns every configured plugin and completes its handshake. A plugin that
// fails any step is dropped with a warning; Start itself only errors if the
// configuration is unusable.
func (m *Manager) Start(ctx context.Context, cfg config.Config) error {
	for _, spec := range cfg.Plugins {
		timeout := spec.Timeout
		if timeout <= 0 {
			timeout = cfg.Analyzers.Timeout
		}
		c, err := m.dial(ctx, cfg.Root, spec, timeout)
		if err != nil {
			m.warnf("plugin %s: %v", spec.ID, err)
			continue
		}
		m.mu.Lock()
		m.conns = append(m.conns, c)
		m.mu.Unlock()
	}
	return nil
}

// Analyzers returns every analyzer that is available, across all live plugins.
// Unavailable analyzers are reported once as a warning and then forgotten: a
// repository with no ESLint config should not be told about it per run step.
func (m *Manager) Analyzers() []Registered {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []Registered
	for _, c := range m.conns {
		if c.dead {
			continue
		}
		for _, d := range c.descriptors {
			if !d.Available {
				continue
			}
			out = append(out, Registered{Descriptor: d, PluginID: c.id})
		}
	}
	return out
}

// Analyze runs one analyzer and returns its findings. An error here means this
// analyzer produced nothing; the caller continues with the rest of the run.
func (m *Manager) Analyze(ctx context.Context, pluginID, analyzerID string, req plugin.AnalyzeRequest) ([]finding.Finding, []string, error) {
	m.mu.Lock()
	var c *conn
	for _, candidate := range m.conns {
		if candidate.id == pluginID {
			c = candidate
			break
		}
	}
	m.mu.Unlock()

	switch {
	case c == nil:
		return nil, nil, fmt.Errorf("no plugin registered as %q", pluginID)
	case c.dead:
		return nil, nil, fmt.Errorf("plugin %s is no longer running", pluginID)
	}

	findings, warnings, err := c.analyze(ctx, analyzerID, req)
	if err != nil {
		var reported analyzerError
		if errors.As(err, &reported) {
			// The plugin reported the failure properly and the conversation is
			// intact, so its other analyzers still run (ADR-0013).
			return nil, nil, err
		}
		// Anything else means the stream position is no longer known — a timeout,
		// an unparseable line, a dead process — and nothing further from this
		// plugin can be trusted.
		m.kill(c, fmt.Sprintf("analyzer %s: %v", analyzerID, err))
		return nil, nil, err
	}
	return findings, warnings, nil
}

// Close says bye to every plugin, waits briefly, then kills what is left. It is
// safe to call more than once and must be called even when Start failed.
func (m *Manager) Close() {
	m.mu.Lock()
	conns := append([]*conn(nil), m.conns...)
	m.conns = nil
	m.mu.Unlock()

	for _, c := range conns {
		if c.dead {
			continue
		}
		if err := c.say(plugin.Frame{Type: plugin.TypeBye}); err != nil {
			m.warnf("plugin %s: saying bye: %v", c.id, err)
		}
		_ = c.stdin.Close()

		if c.isLocal() {
			// No process to signal; closing the pipe is the whole shutdown.
			<-c.exited
			c.drain()
			c.dead = true
			continue
		}

		select {
		case <-c.exited:
		case <-time.After(shutdownGrace):
			m.warnf("plugin %s: did not exit within %s, killing its process group", c.id, shutdownGrace)
			_ = killGroup(c.cmd)
			<-c.exited
		}
		c.drain()
		c.dead = true
	}
	// Plugins dropped earlier in the run already exited; release their pipes too.
	for _, c := range conns {
		c.closeFiles()
	}
}

func (m *Manager) kill(c *conn, reason string) {
	if c.dead {
		return
	}
	c.dead = true
	m.warnf("plugin %s: dropped for the rest of the run (%s)", c.id, reason)
	if c.isLocal() {
		_ = c.stdin.Close()
		<-c.exited
		c.drain()
		return
	}
	_ = killGroup(c.cmd)
	<-c.exited
	c.drain()
}

// dial spawns one plugin and completes hello and describe.
func (m *Manager) dial(ctx context.Context, root string, spec config.Plugin, timeout time.Duration) (*conn, error) {
	command := spec.Command
	// A command with a path separator is resolved against the repository, so a
	// repository can ship a plugin without installing it globally.
	if strings.ContainsRune(command, filepath.Separator) && !filepath.IsAbs(command) {
		command = filepath.Join(root, command)
	}

	cmd := exec.Command(command, spec.Args...)
	cmd.Dir = root
	cmd.Env = pluginEnv(spec.Env)
	isolate(cmd)

	// Deliberately not cmd.StdoutPipe/StderrPipe: those are closed by cmd.Wait,
	// and this package calls Wait in a goroutine so that an exit can be observed
	// while a read is outstanding. With the pipes owned here instead, a plugin
	// that dies mid-frame still hands us the bytes it managed to write, which is
	// the difference between "not a protocol frame" and an unhelpful
	// "file already closed".
	// stdin runs the other way: the parent writes, the child reads.
	childIn, stdin, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("opening stdin: %w", err)
	}
	stdout, childOut, err := os.Pipe()
	if err != nil {
		closeAll(childIn, stdin)
		return nil, fmt.Errorf("opening stdout: %w", err)
	}
	stderr, childErr, err := os.Pipe()
	if err != nil {
		closeAll(childIn, stdin, stdout, childOut)
		return nil, fmt.Errorf("opening stderr: %w", err)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = childIn, childOut, childErr

	if err := cmd.Start(); err != nil {
		closeAll(stdin, childIn, stdout, childOut, stderr, childErr)
		return nil, fmt.Errorf("starting %s: %w", command, err)
	}
	// The child holds its own descriptors now. Releasing the parent's copies is
	// what lets a read see EOF when the child exits.
	closeAll(childIn, childOut, childErr)

	c := &conn{
		id:      spec.ID,
		cmd:     cmd,
		stdin:   stdin,
		in:      plugin.NewReader(stdout),
		out:     plugin.NewWriter(stdin),
		timeout: timeout,
		exited:  make(chan struct{}),
		files:   []*os.File{stdin, stdout, stderr},
	}

	// Plugin diagnostics belong in the run log, prefixed so a multi-plugin run
	// stays readable.
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		copyPrefixed(m.log, stderr, spec.ID+": ")
	}()

	go func() {
		_ = cmd.Wait()
		close(c.exited)
	}()

	if err := c.handshake(ctx, m.hostID, handshakeDeadline(timeout)); err != nil {
		_ = killGroup(cmd)
		<-c.exited
		c.drain()
		c.dead = true
		return nil, err
	}
	return c, nil
}

// pluginEnv builds the child's environment. The parent's is inherited so a plugin
// can find node, git and the repository's tooling; per-plugin entries override it.
func pluginEnv(extra map[string]string) []string {
	if len(extra) == 0 {
		return nil // nil means inherit
	}
	env := os.Environ()
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

func copyPrefixed(dst io.Writer, src io.Reader, prefix string) {
	buf := make([]byte, 4096)
	var partial []byte
	for {
		n, err := src.Read(buf)
		if n > 0 {
			partial = append(partial, buf[:n]...)
			for {
				i := indexByte(partial, '\n')
				if i < 0 {
					break
				}
				fmt.Fprintf(dst, "%s%s\n", prefix, partial[:i])
				partial = partial[i+1:]
			}
		}
		if err != nil {
			if len(partial) > 0 {
				fmt.Fprintf(dst, "%s%s\n", prefix, partial)
			}
			return
		}
	}
}

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}

var errStreamClosed = errors.New("plugin closed its stream")

func closeAll(files ...*os.File) {
	for _, f := range files {
		if f != nil {
			_ = f.Close()
		}
	}
}

// Unavailable returns the analyzers that declined to run, so the run can say why
// once rather than silently doing less than the user expects.
func (m *Manager) Unavailable() []Registered {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []Registered
	for _, c := range m.conns {
		if c.dead {
			continue
		}
		for _, d := range c.descriptors {
			if d.Available {
				continue
			}
			out = append(out, Registered{Descriptor: d, PluginID: c.id})
		}
	}
	return out
}
