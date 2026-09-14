// SPDX-License-Identifier: MIT

package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/internal/config"
	"github.com/adamtait/reviewer/pkg/finding"
	"github.com/adamtait/reviewer/pkg/plugin"
)

// The single most important structural property in the system. The model lane is
// behind the secrets gate because it is declared in the `llm` lane and the
// sequencer runs lanes in order with the gate between them (ADR-0012). Nothing
// inside the model lane checks for a credential; it simply is not reached.
//
// If this descriptor ever said `deterministic`, every guarantee in ADR-0012 would
// be gone and every test of the gate would still pass, because they all test the
// sequencer rather than what it is given.
func TestTheModelLaneIsDeclaredInTheModelLane(t *testing.T) {
	h := New(config.Defaults(), config.Secrets{}, "test")

	var found bool
	for _, d := range h.Describe() {
		if d.ID != ModelLaneID {
			if d.Lane != finding.LaneDeterministic {
				t.Errorf("%s is declared in the %q lane; only the model lane belongs there", d.ID, d.Lane)
			}
			continue
		}
		found = true
		if d.Lane != finding.LaneLLM {
			t.Fatalf("%s is declared in the %q lane, which puts it in front of the secrets gate", d.ID, d.Lane)
		}
	}
	if !found {
		t.Fatalf("%s is not offered at all", ModelLaneID)
	}
}

// It runs last. It is the slowest analyzer here and the only one that sends
// anything anywhere (ADR-0013).
func TestTheModelLaneRunsLast(t *testing.T) {
	h := New(config.Defaults(), config.Secrets{}, "test")

	for _, d := range h.Describe() {
		if d.ID != ModelLaneID && d.Order >= ModelLaneOrder {
			t.Errorf("%s has order %d, at or after the model lane's %d", d.ID, d.Order, ModelLaneOrder)
		}
	}
}

// Every way of not being ready is a skip with a reason, reported once at the top of
// the run rather than per invocation.
func TestTheModelLaneDeclinesWithAReasonWhenItIsNotConfigured(t *testing.T) {
	h := New(config.Defaults(), config.Secrets{}, "test")

	for _, d := range h.Describe() {
		if d.ID != ModelLaneID {
			continue
		}
		if d.Available {
			t.Fatal("the model lane is off by default and must say so")
		}
		if d.Unavailable == "" {
			t.Error("want a reason, got silence")
		}
	}
}

// A pull request whose base commit is not in the checkout has no diff that
// corresponds to what the secrets gate scanned. The first version fell back to
// `git diff --cached`, which sent the developer's local index to a model provider
// while the gate had scanned the pull request's files — two different sets of
// content, and only one of them checked (ADR-0012).
func TestTheModelLaneRefusesToInventADiff(t *testing.T) {
	cfg := config.Defaults()
	cfg.Root = t.TempDir()
	cfg.LaneB.Enabled = true
	cfg.LaneB.Provider = "openai-compatible"
	cfg.LaneB.Model = "a-model"
	cfg.LaneB.BaseURL = "http://127.0.0.1:1"
	h := New(cfg, config.Secrets{ModelAPIKey: "sk-test-0123456789"}, "test")

	findings, warnings, err := h.Analyze(context.Background(), ModelLaneID, plugin.AnalyzeRequest{
		Root:    cfg.Root,
		Changed: []plugin.ChangedFile{{Path: "src/a.ts", Status: "modified", Ranges: [][2]int{{1, 1}}}},
		// Neither a base nor staged: the API supplied the file list.
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("want nothing, got %+v", findings)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "fetch-depth") {
		t.Errorf("want the fix named, got %v", warnings)
	}
}
