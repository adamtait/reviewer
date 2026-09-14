// SPDX-License-Identifier: MIT

package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/adamtait/reviewer/internal/config"
)

// cliProvider drives a CLI that is already signed in to somebody's subscription.
//
// These two paths exist because the person most likely to try this tool on a
// personal project is the person who pays for a subscription and not for API
// tokens. Under an HTTP-shaped interface they would have been "maybe later"
// forever, which is the reason ADR-0021's interface is as narrow as it is.
//
// There is no key *here* — which removes a whole class of failure and adds two
// others. This spawns a program that can do anything the person running it can do,
// and that program holds its own credentials and prints them in its diagnostics.
// Its output ends up in a warning, and a warning ends up in a pull request comment,
// so everything it says goes through Redact on the way out.
type cliProvider struct {
	name string
	// binary is the executable, from config or the path's default name. Never an
	// absolute path compiled in (ADR-0004).
	binary string
	// args are everything except the prompt.
	args []string
	// text pulls the reply out of whatever the CLI prints.
	text func(stdout string) (string, error)
}

// cliTimeout bounds one invocation when the caller sets none. A signed-in CLI can
// sit waiting for input forever if it decides the session needs re-authenticating,
// and a review that never returns is worse than one that fails.
const cliTimeout = 5 * time.Minute

func newCLI(cfg config.Config) (Provider, error) {
	binary := cfg.LaneB.Provider
	if tool, ok := cfg.Tools[cfg.LaneB.Provider]; ok && tool.Path != "" {
		binary = tool.Path
	}

	var p *cliProvider
	switch cfg.LaneB.Provider {
	case "claude-code":
		p = &cliProvider{
			name:   "claude-code",
			binary: binary,
			// --print is non-interactive; the JSON format carries the result in a
			// field rather than mixed into whatever the CLI wants to show a human.
			args: []string{"--print", "--output-format", "json", "--model", cfg.LaneB.Model},
			text: claudeCodeText,
		}
	case "codex":
		p = &cliProvider{
			name:   "codex",
			binary: binary,
			args:   []string{"exec", "--json", "--model", cfg.LaneB.Model},
			text:   codexText,
		}
	default:
		return nil, Unavailable("no CLI adapter for %q", cfg.LaneB.Provider)
	}

	if _, err := exec.LookPath(p.binary); err != nil {
		return nil, Unavailable("%s is not installed or not on PATH", p.binary)
	}
	return p, nil
}

func (p *cliProvider) Name() string { return p.name }

func (p *cliProvider) Complete(ctx context.Context, messages []Message, opts Options) (string, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = cliTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, p.binary, p.args...)

	// The prompt goes on stdin, never on argv.
	//
	// An argument list is world-readable: `ps` shows it to every user on the
	// machine, and it reaches process accounting, audit logs and crash reports. The
	// prompt contains the diff. This is the whole reason this adapter writes to a
	// pipe rather than passing a flag.
	cmd.Stdin = strings.NewReader(prompt(messages))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Its own process group, so a timeout kills whatever it spawned rather than
	// leaving orphans behind holding the terminal.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	runErr := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("%s: gave up after %s", p.name, timeout)
	}

	text, err := p.text(stdout.String())
	switch {
	case err == nil:
		return text, nil
	case errors.Is(err, ErrRefused):
		// A refusal the CLI stated in its own output, which both of these report
		// with a non-zero exit as well. The exit code is the tiebreaker, not the
		// decision: reading it first would turn every refusal into "exit status 1"
		// and lose the reason the CLI gave. The sentinel is preserved with %w; the
		// text it carries was redacted where it was built.
		return "", fmt.Errorf("%s: %w", p.name, err)
	case runErr != nil:
		// An unreadable reply and a non-zero exit: the CLI failed, and its own
		// diagnostics on stderr are more useful than our parse error — redacted,
		// because "auth failed for token sk-…" is a thing these tools print.
		return "", fmt.Errorf("%s: %v: %s", p.name, runErr, firstLine(Redact(stderr.String())))
	default:
		return "", fmt.Errorf("%s: %s", p.name, Redact(err.Error()))
	}
}

// prompt flattens the turns. A CLI has one input, so the system prompt and the
// material arrive as one document with the instructions first.
func prompt(messages []Message) string {
	var b strings.Builder
	for i, m := range messages {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(m.Content)
	}
	return b.String()
}

// claudeCodeText reads `--output-format json`, which wraps the reply in an
// envelope carrying the result and whether the session errored.
func claudeCodeText(stdout string) (string, error) {
	var reply struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &reply); err != nil {
		return "", fmt.Errorf("the reply was not the expected JSON: %w", err)
	}
	if reply.IsError {
		// Redacted here rather than at the caller, so the sentinel survives the
		// wrapping: these CLIs hold real credentials and print them in diagnostics,
		// and this text reaches a pull request comment.
		return "", fmt.Errorf("%w: %s", ErrRefused, Redact(orDefault(reply.Result, reply.Subtype)))
	}
	if strings.TrimSpace(reply.Result) == "" {
		return "", fmt.Errorf("the reply was empty")
	}
	return reply.Result, nil
}

// codexText reads newline-delimited JSON events and keeps the last message.
//
// A stream rather than one object, so the reply is whatever the final assistant
// message says. Anything that is not parseable JSON is skipped rather than fatal:
// these CLIs print progress to stdout in some configurations, and refusing the
// whole reply over one banner line would make the adapter unusable.
func codexText(stdout string) (string, error) {
	var last string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var event struct {
			Type string `json:"type"`
			Msg  struct {
				Type    string `json:"type"`
				Message string `json:"message"`
				Text    string `json:"text"`
			} `json:"msg"`
			Message string `json:"message"`
			Text    string `json:"text"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		for _, candidate := range []string{event.Msg.Message, event.Msg.Text, event.Message, event.Text} {
			if strings.TrimSpace(candidate) != "" {
				last = candidate
			}
		}
	}
	if strings.TrimSpace(last) == "" {
		return "", fmt.Errorf("the reply carried no message")
	}
	return last, nil
}

func orDefault(s, fallback string) string {
	if strings.TrimSpace(s) != "" {
		return s
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return "no reason given"
}
