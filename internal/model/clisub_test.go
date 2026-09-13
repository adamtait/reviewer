// SPDX-License-Identifier: MIT

package model

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/config"
)

// stub writes a fake CLI and returns a config pointing at it.
func stub(t *testing.T, provider, body string) (config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, provider)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	cfg.LaneB.Enabled = true
	cfg.LaneB.Provider = provider
	cfg.LaneB.Model = "a-model"
	cfg.Tools = map[string]config.Tool{provider: {Path: path}}
	return cfg, dir
}

// The exit criterion, and the reason this adapter writes to a pipe rather than
// passing a flag: an argument list is world-readable. `ps` shows it to every user
// on the machine, and it reaches process accounting, audit logs and crash reports.
// The prompt contains the diff.
func TestThePromptArrivesOnStdinAndNeverOnArgv(t *testing.T) {
	for _, provider := range []string{"claude-code", "codex"} {
		t.Run(provider, func(t *testing.T) {
			cfg, dir := stub(t, provider, "#!/bin/sh\n"+
				"printf '%s' \"$*\" > "+filepath.Join("$(dirname \"$0\")", "argv")+"\n"+
				"cat > "+filepath.Join("$(dirname \"$0\")", "stdin")+"\n"+
				replyFor(provider, "ok")+"\n")

			p, err := New(cfg, config.Secrets{})
			if err != nil {
				t.Fatal(err)
			}
			const secret = "+  const token = leakThisIfArgvIsUsed;"
			if _, err := p.Complete(context.Background(), []Message{
				{Role: RoleSystem, Content: "be careful"},
				{Role: RoleUser, Content: "the diff:\n" + secret},
			}, Options{Model: "a-model"}); err != nil {
				t.Fatal(err)
			}

			argv := read(t, filepath.Join(dir, "argv"))
			if strings.Contains(argv, secret) || strings.Contains(argv, "be careful") {
				t.Fatalf("prompt content reached argv, where ps can read it: %s", argv)
			}
			stdin := read(t, filepath.Join(dir, "stdin"))
			for _, want := range []string{"be careful", secret} {
				if !strings.Contains(stdin, want) {
					t.Errorf("stdin does not carry %q:\n%s", want, stdin)
				}
			}
			// The model still has to be selected, and that is not prompt content.
			if !strings.Contains(argv, "a-model") {
				t.Errorf("the model was not passed: %s", argv)
			}
		})
	}
}

func TestClaudeCodeReadsItsEnvelope(t *testing.T) {
	cfg, _ := stub(t, "claude-code", "#!/bin/sh\ncat > /dev/null\n"+
		`printf '%s' '{"type":"result","subtype":"success","is_error":false,"result":"the answer"}'`+"\n")

	p, err := New(cfg, config.Secrets{})
	if err != nil {
		t.Fatal(err)
	}
	text, err := p.Complete(context.Background(), nil, Options{Model: "a-model"})
	if err != nil || text != "the answer" {
		t.Fatalf("got %q %v", text, err)
	}
}

// These CLIs exit non-zero for a refusal as well as for a failure, so the exit code
// is the tiebreaker rather than the decision.
func TestClaudeCodeReportsARefusalAsOne(t *testing.T) {
	cfg, _ := stub(t, "claude-code", "#!/bin/sh\ncat > /dev/null\n"+
		`printf '%s' '{"type":"result","subtype":"error_max_turns","is_error":true,"result":"I cannot help with that."}'`+"\n"+
		"exit 1\n")

	p, _ := New(cfg, config.Secrets{})
	_, err := p.Complete(context.Background(), nil, Options{Model: "a-model"})
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("want ErrRefused, got %v", err)
	}
	if !strings.Contains(err.Error(), "I cannot help") {
		t.Errorf("want the reason carried through, got %v", err)
	}
}

// A stream of events, not one object. Progress lines that are not JSON must not
// make the whole reply unreadable: these CLIs print banners to stdout in some
// configurations, and refusing over one would make the adapter unusable.
func TestCodexKeepsTheLastMessageAndSkipsNoise(t *testing.T) {
	cfg, _ := stub(t, "codex", "#!/bin/sh\ncat > /dev/null\n"+
		"echo 'Reading prompt from stdin...'\n"+
		`echo '{"type":"item.started","msg":{"type":"agent_message","text":"thinking"}}'`+"\n"+
		"echo 'not json at all'\n"+
		`echo '{"type":"item.completed","msg":{"type":"agent_message","text":"the final answer"}}'`+"\n")

	p, err := New(cfg, config.Secrets{})
	if err != nil {
		t.Fatal(err)
	}
	text, err := p.Complete(context.Background(), nil, Options{Model: "a-model"})
	if err != nil || text != "the final answer" {
		t.Fatalf("got %q %v", text, err)
	}
}

// A signed-in CLI can sit waiting for input forever if it decides the session
// needs re-authenticating, and a review that never returns is worse than one that
// fails.
func TestAHangingCLIIsKilledAtTheTimeout(t *testing.T) {
	cfg, _ := stub(t, "codex", "#!/bin/sh\ncat > /dev/null\nsleep 60\n")

	p, err := New(cfg, config.Secrets{})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = p.Complete(context.Background(), nil, Options{Model: "a-model", Timeout: 200 * time.Millisecond})

	if err == nil || !strings.Contains(err.Error(), "gave up") {
		t.Fatalf("err = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("the call was not bounded: %v", elapsed)
	}
}

// A CLI that spawns children must not leave them behind holding the terminal.
func TestATimeoutKillsTheWholeProcessGroup(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "child-survived")
	cfg, _ := stub(t, "codex", "#!/bin/sh\ncat > /dev/null\n"+
		"( sleep 3; touch "+marker+" ) &\n"+
		"sleep 60\n")

	p, err := New(cfg, config.Secrets{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Complete(context.Background(), nil,
		Options{Model: "a-model", Timeout: 200 * time.Millisecond}); err == nil {
		t.Fatal("want a timeout")
	}

	time.Sleep(4 * time.Second)
	if _, err := os.Stat(marker); err == nil {
		t.Error("a child outlived the killed CLI")
	}
}

func TestAnAbsentCLIIsADisabledLaneWithAReason(t *testing.T) {
	cfg := config.Defaults()
	cfg.LaneB.Enabled = true
	cfg.LaneB.Provider = "claude-code"
	cfg.LaneB.Model = "a-model"
	cfg.Tools = map[string]config.Tool{"claude-code": {Path: "/nonexistent/claude"}}

	_, err := New(cfg, config.Secrets{})
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("want ErrDisabled, got %v", err)
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("want the reason stated, got %v", err)
	}
}

// No key, no endpoint. That is the entire point of these two paths: a person who
// pays for a subscription and not for tokens has nothing to configure.
func TestTheSubscriptionPathsNeedNoCredential(t *testing.T) {
	cfg, _ := stub(t, "claude-code", "#!/bin/sh\ncat >/dev/null\n"+
		`printf '%s' '{"result":"ok"}'`+"\n")

	if _, err := New(cfg, config.Secrets{}); err != nil {
		t.Fatalf("a subscription path must work with no key and no endpoint: %v", err)
	}
}

func TestTheCLIProviderSatisfiesTheInterface(t *testing.T) {
	var _ Provider = (*cliProvider)(nil)
}

func replyFor(provider, text string) string {
	if provider == "claude-code" {
		return `printf '%s' '{"type":"result","is_error":false,"result":"` + text + `"}'`
	}
	return `echo '{"type":"item.completed","msg":{"type":"agent_message","text":"` + text + `"}}'`
}

func read(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// These CLIs hold real credentials and print them in their diagnostics. Their
// output becomes a run warning, and a run warning becomes a comment on a public
// pull request.
func TestCLIOutputIsRedacted(t *testing.T) {
	const leaked = "sk-live-ABCDEFGH01234567890"

	t.Run("stderr on a failure", func(t *testing.T) {
		cfg, _ := stub(t, "codex", "#!/bin/sh\ncat >/dev/null\n"+
			"echo 'auth failed for token "+leaked+"' >&2\nexit 1\n")
		p, err := New(cfg, config.Secrets{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.Complete(context.Background(), nil, Options{Model: "a-model"})
		if err == nil || strings.Contains(err.Error(), leaked) {
			t.Fatalf("the key reached the error: %v", err)
		}
	})

	t.Run("the result text on a refusal", func(t *testing.T) {
		cfg, _ := stub(t, "claude-code", "#!/bin/sh\ncat >/dev/null\n"+
			`printf '%s' '{"is_error":true,"result":"credentials rejected: `+leaked+`"}'`+"\nexit 1\n")
		p, err := New(cfg, config.Secrets{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.Complete(context.Background(), nil, Options{Model: "a-model"})
		if !errors.Is(err, ErrRefused) {
			t.Fatalf("want the refusal preserved through redaction, got %v", err)
		}
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("the key reached the error: %v", err)
		}
	})
}
