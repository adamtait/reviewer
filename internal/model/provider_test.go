// SPDX-License-Identifier: MIT

package model

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/internal/config"
)

// Every way of not being ready is a skip with a reason, never a failed run. A
// review that stops because a model is unreachable has failed at the job it was
// built to do (ADR-0009).
func TestNewIsDisabledRatherThanBroken(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*config.Config)
		want   string
	}{
		{name: "lane off", mutate: func(c *config.Config) {}, want: "laneB.enabled is false"},
		{
			name:   "no provider",
			mutate: func(c *config.Config) { c.LaneB.Enabled = true },
			want:   "no laneB.provider",
		},
		{
			name: "no model",
			mutate: func(c *config.Config) {
				c.LaneB.Enabled = true
				c.LaneB.Provider = "anthropic"
			},
			want: "no model is named",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Defaults()
			tc.mutate(&cfg)

			provider, err := New(cfg, config.Secrets{})
			if !errors.Is(err, ErrDisabled) {
				t.Fatalf("want ErrDisabled, got %v", err)
			}
			if provider != nil {
				t.Error("want no provider constructed")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want the reason to mention %q, got %v", tc.want, err)
			}
		})
	}
}

// The interface has to admit both shapes without either leaking into the core.
// The adapters assert their own half as they land.
func TestTheFakeSatisfiesTheInterface(t *testing.T) {
	var _ Provider = (*Fake)(nil)
}

// A provider name config accepts but this package has no adapter for must be a
// stated reason, not a silent nothing.
func TestEveryConfiguredProviderHasAnAnswer(t *testing.T) {
	for _, name := range config.Providers() {
		cfg := config.Defaults()
		cfg.LaneB.Enabled = true
		cfg.LaneB.Provider = name
		cfg.LaneB.Model = "some-model"

		_, err := New(cfg, config.Secrets{})
		if err == nil {
			continue // an adapter exists and was constructed
		}
		if !errors.Is(err, ErrDisabled) {
			t.Errorf("%s: want a stated reason, got %v", name, err)
		}
		if strings.Contains(err.Error(), "no adapter for") && !strings.Contains(err.Error(), name) {
			t.Errorf("%s: the reason does not name the provider: %v", name, err)
		}
	}
}

func TestFakeAnswersInOrderAndRepeatsTheLast(t *testing.T) {
	f := NewFake("first", "second")

	for _, want := range []string{"first", "second", "second"} {
		got, err := f.Complete(context.Background(), []Message{{Role: RoleUser, Content: "q"}}, Options{})
		if err != nil || got != want {
			t.Fatalf("got %q %v, want %q", got, err, want)
		}
	}
	if len(f.Calls()) != 3 {
		t.Errorf("want three calls recorded, got %d", len(f.Calls()))
	}
}

// A caller that forgot to thread cancellation through would otherwise pass every
// test and hang in production.
func TestFakeHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := NewFake("x").Complete(ctx, nil, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("want the cancellation honoured, got %v", err)
	}
}

// The most important function in this package: an error message is the least
// guarded thing in the system — stderr, the run log, and through a warning a
// public pull request comment.
func TestRedact(t *testing.T) {
	for _, tc := range []struct {
		name, in, key string
		wantGone      []string
	}{
		{
			name:     "the configured key, verbatim",
			in:       `401 Unauthorized: key "abcd1234efgh5678" is not valid`,
			key:      "abcd1234efgh5678",
			wantGone: []string{"abcd1234efgh5678"},
		},
		{
			name:     "a key shape we did not configure",
			in:       `{"error":"Incorrect API key provided: sk-proj-AAAABBBBCCCCDDDD"}`,
			wantGone: []string{"sk-proj-AAAABBBBCCCCDDDD"},
		},
		{
			name:     "a Google key in a proxied body",
			in:       "API key not valid: AIzaSyA1B2C3D4E5F6G7H8",
			wantGone: []string{"AIzaSyA1B2C3D4E5F6G7H8"},
		},
		{
			name:     "an echoed authorization header",
			in:       "request failed: Authorization: Bearer abcdefghijklmnop0123",
			wantGone: []string{"abcdefghijklmnop0123"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Redact(tc.in, tc.key)
			for _, secret := range tc.wantGone {
				if strings.Contains(got, secret) {
					t.Errorf("Redact left %q in %q", secret, got)
				}
			}
			if !strings.Contains(got, "[redacted]") {
				t.Errorf("want the replacement visible, got %q", got)
			}
		})
	}
}

// A short "secret" is not one, and replacing it would mangle unrelated text.
func TestRedactIgnoresSomethingTooShortToBeACredential(t *testing.T) {
	got := Redact("the model returned an error for the file src/on.ts", "on")
	if got != "the model returned an error for the file src/on.ts" {
		t.Errorf("Redact mangled the message: %q", got)
	}
}
