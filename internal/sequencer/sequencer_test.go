// SPDX-License-Identifier: MIT

package sequencer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/pluginhost"
	"github.com/adamtait/reviewer/pkg/finding"
	"github.com/adamtait/reviewer/pkg/plugin"
)

// fakeHost records the order analyzers were asked to run in, so the policy can be
// tested without starting a process.
type fakeHost struct {
	registered []pluginhost.Registered
	calls      []string
	results    map[string][]finding.Finding
	fail       map[string]error
	delay      map[string]time.Duration
}

func (h *fakeHost) Analyzers() []pluginhost.Registered { return h.registered }

func (h *fakeHost) Analyze(_ context.Context, _, id string, _ plugin.AnalyzeRequest) ([]finding.Finding, []string, error) {
	h.calls = append(h.calls, id)
	if d, ok := h.delay[id]; ok {
		time.Sleep(d)
	}
	if err, ok := h.fail[id]; ok {
		return nil, nil, err
	}
	return h.results[id], nil, nil
}

func reg(id string, order int, lane finding.Lane) pluginhost.Registered {
	return pluginhost.Registered{
		Descriptor: plugin.Descriptor{ID: id, Order: order, Lane: lane, Available: true},
		PluginID:   "p",
	}
}

func secretFinding() finding.Finding {
	return finding.Finding{
		RuleID: "secrets/github-pat", Lane: finding.LaneDeterministic,
		Confidence: finding.ConfidenceHigh, Severity: finding.SeverityError,
		File: "src/config.ts", Line: 5, Message: "a credential",
	}
}

func TestOrderIsDeclaredNotRegistrationOrder(t *testing.T) {
	h := &fakeHost{registered: []pluginhost.Registered{
		reg("late", 90, finding.LaneDeterministic),
		reg("early", 10, finding.LaneDeterministic),
		reg("middle", 50, finding.LaneDeterministic),
		// Equal order: ties break on id so a run is reproducible whatever order
		// the plugins happened to start in.
		reg("b-tie", 50, finding.LaneDeterministic),
	}}
	Run(context.Background(), h, plugin.AnalyzeRequest{}, Options{})
	if got := strings.Join(h.calls, ","); got != "early,b-tie,middle,late" {
		t.Fatalf("want cheap-first declared order with id tie-break, got %s", got)
	}
}

func TestOnlyAndSkip(t *testing.T) {
	registered := []pluginhost.Registered{
		reg("a", 10, finding.LaneDeterministic),
		reg("b", 20, finding.LaneDeterministic),
		reg("c", 30, finding.LaneDeterministic),
	}
	tests := []struct {
		name string
		opts Options
		want string
	}{
		{"no filter", Options{}, "a,b,c"},
		{"only", Options{Only: []string{"c", "a"}}, "a,c"},
		{"skip", Options{Skip: []string{"b"}}, "a,c"},
		{"only wins over skip", Options{Only: []string{"b"}, Skip: []string{"b"}}, "b"},
		{"only an unknown id runs nothing", Options{Only: []string{"nope"}}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := &fakeHost{registered: registered}
			Run(context.Background(), h, plugin.AnalyzeRequest{}, tc.opts)
			if got := strings.Join(h.calls, ","); got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

// One analyzer failing costs its own findings and nothing else (ADR-0013).
func TestFailureIsOpen(t *testing.T) {
	h := &fakeHost{
		registered: []pluginhost.Registered{
			reg("first", 10, finding.LaneDeterministic),
			reg("broken", 20, finding.LaneDeterministic),
			reg("last", 30, finding.LaneDeterministic),
		},
		fail:    map[string]error{"broken": errors.New("no configuration found")},
		results: map[string][]finding.Finding{"last": {secretFinding()}},
	}
	res := Run(context.Background(), h, plugin.AnalyzeRequest{}, Options{})

	if got := strings.Join(h.calls, ","); got != "first,broken,last" {
		t.Fatalf("a failure must not stop the run, got %q", got)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("want the surviving analyzer's finding, got %+v", res.Findings)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "no configuration found") {
		t.Fatalf("want the failure reported as a warning, got %v", res.Warnings)
	}
	// The timing row must mark it failed rather than silently reporting 0 findings.
	var found bool
	for _, timing := range res.Timings {
		if timing.Analyzer == "broken" {
			found = timing.Failed
		}
	}
	if !found {
		t.Fatalf("want the failed analyzer marked in the timings, got %+v", res.Timings)
	}
}

func TestTheGateSeparatesTheLanes(t *testing.T) {
	tests := []struct {
		name        string
		results     map[string][]finding.Finding
		fail        map[string]error
		wantCalls   string
		wantBlocked bool
	}{
		{
			name:      "clean scan lets the model lane run",
			wantCalls: "gitleaks,judge",
		},
		{
			name:        "a credential blocks the model lane",
			results:     map[string][]finding.Finding{"gitleaks": {secretFinding()}},
			wantCalls:   "gitleaks",
			wantBlocked: true,
		},
		{
			// The scan did not complete, so nothing checked the diff.
			name:        "a failed scan blocks the model lane",
			fail:        map[string]error{"gitleaks": errors.New("not installed")},
			wantCalls:   "gitleaks",
			wantBlocked: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := &fakeHost{
				registered: []pluginhost.Registered{
					reg(SecretsAnalyzer, 10, finding.LaneDeterministic),
					reg("judge", 500, finding.LaneLLM),
				},
				results: tc.results,
				fail:    tc.fail,
			}
			res := Run(context.Background(), h, plugin.AnalyzeRequest{}, Options{})
			if got := strings.Join(h.calls, ","); got != tc.wantCalls {
				t.Fatalf("want calls %q, got %q", tc.wantCalls, got)
			}
			if res.Gate.Blocked != tc.wantBlocked {
				t.Fatalf("want blocked=%v, got %v", tc.wantBlocked, res.Gate.Blocked)
			}
			if tc.wantBlocked {
				joined := strings.Join(res.Skipped, "\n")
				if !strings.Contains(joined, "judge") {
					t.Fatalf("want the skipped model-lane analyzer named, got %q", joined)
				}
			}
		})
	}
}

// A blocked gate with nothing in the model lane must not add noise to the report.
func TestABlockedGateWithNoModelLaneIsSilent(t *testing.T) {
	h := &fakeHost{
		registered: []pluginhost.Registered{reg(SecretsAnalyzer, 10, finding.LaneDeterministic)},
		results:    map[string][]finding.Finding{SecretsAnalyzer: {secretFinding()}},
	}
	res := Run(context.Background(), h, plugin.AnalyzeRequest{}, Options{})
	if !res.Gate.Blocked {
		t.Fatal("want the gate blocked")
	}
	if len(res.Skipped) != 0 {
		t.Fatalf("nothing was skipped, so nothing should be reported: %v", res.Skipped)
	}
}

// Skipping the secrets analyzer must close the gate, not open it: the diff is
// then unchecked, which is exactly the case the gate fails closed on.
func TestSkippingTheSecretsAnalyzerBlocksTheModelLane(t *testing.T) {
	h := &fakeHost{registered: []pluginhost.Registered{
		reg(SecretsAnalyzer, 10, finding.LaneDeterministic),
		reg("judge", 500, finding.LaneLLM),
	}}
	res := Run(context.Background(), h, plugin.AnalyzeRequest{}, Options{Skip: []string{SecretsAnalyzer}})
	if strings.Join(h.calls, ",") != "" && strings.Contains(strings.Join(h.calls, ","), "judge") {
		t.Fatalf("the model lane ran with the secrets scan skipped: %v", h.calls)
	}
	if !res.Gate.Blocked {
		t.Fatal("skipping the secrets scan must close the gate")
	}
}

func TestTimingsAreRecordedForEveryAnalyzer(t *testing.T) {
	h := &fakeHost{
		registered: []pluginhost.Registered{
			reg("quick", 10, finding.LaneDeterministic),
			reg("slow", 20, finding.LaneDeterministic),
		},
		delay: map[string]time.Duration{"slow": 20 * time.Millisecond},
	}
	res := Run(context.Background(), h, plugin.AnalyzeRequest{}, Options{})
	if len(res.Timings) != 2 {
		t.Fatalf("want a timing per analyzer, got %+v", res.Timings)
	}
	var slow time.Duration
	for _, timing := range res.Timings {
		if timing.Analyzer == "slow" {
			slow = timing.Elapsed
		}
	}
	if slow < 15*time.Millisecond {
		t.Fatalf("want the slow analyzer's elapsed time recorded, got %s", slow)
	}
}
