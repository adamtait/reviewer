// SPDX-License-Identifier: MIT

package pluginhost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/adamtait/reviewer/pkg/finding"
	"github.com/adamtait/reviewer/pkg/plugin"
)

// conn is one live plugin process and the state of its conversation.
//
// Reads are performed in a goroutine and selected against a deadline, because a
// blocking read on a pipe does not return when a context is cancelled. On
// timeout the process group is killed, which closes the pipe and lets the
// goroutine finish — the channel is buffered so it never blocks on a receiver
// that has already given up.
type conn struct {
	id      string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	in      *plugin.Reader
	out     *plugin.Writer
	timeout time.Duration

	name    string
	version string

	descriptors []plugin.Descriptor

	dead   bool
	exited chan struct{}
	wg     sync.WaitGroup
}

// drain waits for the stderr pump to finish, so the run log is complete before
// the caller prints a summary.
func (c *conn) drain() { c.wg.Wait() }

func (c *conn) say(f plugin.Frame) error {
	return c.out.Write(f)
}

type readResult struct {
	frame plugin.Frame
	err   error
}

// hear reads one frame, giving up after d. A timeout is terminal for this plugin:
// the stream position is no longer known, so the caller kills it.
func (c *conn) hear(ctx context.Context, d time.Duration) (plugin.Frame, error) {
	ch := make(chan readResult, 1)
	go func() {
		f, err := c.in.Read()
		ch <- readResult{f, err}
	}()

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case r := <-ch:
		if errors.Is(r.err, plugin.ErrClosed) {
			return plugin.Frame{}, errStreamClosed
		}
		return r.frame, r.err
	case <-timer.C:
		_ = killGroup(c.cmd)
		return plugin.Frame{}, fmt.Errorf("no response within %s", d)
	case <-ctx.Done():
		_ = killGroup(c.cmd)
		return plugin.Frame{}, ctx.Err()
	}
}

// handshake exchanges hello and collects the analyzer descriptors.
func (c *conn) handshake(ctx context.Context, hostID string, deadline time.Duration) error {
	if err := c.say(plugin.Frame{
		Type:     plugin.TypeHello,
		Protocol: plugin.Protocol,
		Host:     hostID,
	}); err != nil {
		return fmt.Errorf("sending hello: %w", err)
	}

	f, err := c.hear(ctx, deadline)
	if err != nil {
		return fmt.Errorf("waiting for hello: %w", err)
	}
	switch {
	case f.Type == plugin.TypeError:
		return fmt.Errorf("refused the handshake: %s", f.Message)
	case f.Type != plugin.TypeHello:
		return fmt.Errorf("answered hello with a %s frame", f.Type)
	case f.Protocol != plugin.Protocol:
		return fmt.Errorf("speaks protocol %d, this host speaks %d", f.Protocol, plugin.Protocol)
	}
	c.name, c.version = f.Plugin, f.Version

	if err := c.say(plugin.Frame{Type: plugin.TypeDescribe}); err != nil {
		return fmt.Errorf("sending describe: %w", err)
	}
	f, err = c.hear(ctx, deadline)
	if err != nil {
		return fmt.Errorf("waiting for describe: %w", err)
	}
	if f.Type != plugin.TypeDescribe {
		return fmt.Errorf("answered describe with a %s frame", f.Type)
	}

	seen := map[string]bool{}
	for _, d := range f.Analyzers {
		switch {
		case d.ID == "":
			return errors.New("described an analyzer with no id")
		case seen[d.ID]:
			return fmt.Errorf("described analyzer %q twice", d.ID)
		case d.Lane != finding.LaneDeterministic && d.Lane != finding.LaneLLM:
			// An unrecognised lane cannot be gated, so it is refused outright
			// rather than guessed at (ADR-0005).
			return fmt.Errorf("analyzer %q declares lane %q, which is neither %q nor %q",
				d.ID, d.Lane, finding.LaneDeterministic, finding.LaneLLM)
		}
		seen[d.ID] = true
	}
	c.descriptors = f.Analyzers
	return nil
}

// analyze runs one analyzer. Frames for a different analyzer, or types this host
// does not know, are skipped rather than treated as failures: a plugin may
// legitimately send something a newer host understands.
func (c *conn) analyze(ctx context.Context, id string, req plugin.AnalyzeRequest) ([]finding.Finding, []string, error) {
	if err := c.say(plugin.Frame{
		Type:     plugin.TypeAnalyze,
		Analyzer: id,
		Request:  &req,
	}); err != nil {
		return nil, nil, fmt.Errorf("sending analyze: %w", err)
	}

	deadline := time.Now().Add(c.timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			_ = killGroup(c.cmd)
			return nil, nil, fmt.Errorf("no response within %s", c.timeout)
		}
		f, err := c.hear(ctx, remaining)
		if err != nil {
			return nil, nil, err
		}

		switch f.Type {
		case plugin.TypeFindings:
			if f.Analyzer != "" && f.Analyzer != id {
				continue
			}
			valid, warnings := validate(id, f.Findings, f.Warnings)
			return valid, warnings, nil

		case plugin.TypeError:
			if f.Analyzer != "" && f.Analyzer != id {
				continue
			}
			return nil, nil, errors.New(f.Message)

		default:
			continue
		}
	}
}

// validate drops findings a plugin should not have sent. One malformed finding
// must not discard the rest: the analyzer is usually right about the others.
func validate(analyzerID string, in []finding.Finding, warnings []string) ([]finding.Finding, []string) {
	out := make([]finding.Finding, 0, len(in))
	for _, f := range in {
		// The fingerprint is the core's to compute (ADR-0015); a plugin that sets
		// one is ignored rather than trusted.
		f.Fingerprint = ""
		checkable := f
		checkable.Fingerprint = "checked-later"
		if err := checkable.Validate(); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: dropped an invalid finding (%s): %v", analyzerID, f.RuleID, err))
			continue
		}
		out = append(out, f)
	}
	return out, warnings
}
