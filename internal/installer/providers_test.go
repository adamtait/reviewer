// SPDX-License-Identifier: MIT

package installer

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/internal/config"
	"github.com/adamtait/reviewer/internal/testfixture"
)

// The installer's set and the engine's set are the same set. A provider the
// installer can write but the engine rejects is a config that fails validation on
// the first review.
func TestEveryProviderOfferedIsOneTheEngineAccepts(t *testing.T) {
	offered := map[string]bool{}
	for _, p := range providers() {
		offered[p.ID] = true
		cfg := config.Defaults()
		cfg.LaneB.Enabled = true
		cfg.LaneB.Provider = p.ID
		if err := cfg.Validate(); err != nil {
			t.Errorf("the engine rejects %q: %v", p.ID, err)
		}
	}
	for _, id := range config.Providers() {
		if !offered[id] {
			t.Errorf("the engine accepts %q but the installer never offers it", id)
		}
	}
	if len(offered) != len(config.Providers()) {
		t.Errorf("the installer offers %d paths, the engine accepts %d", len(offered), len(config.Providers()))
	}
}

// No provider may carry an endpoint or a model name. Both are the destination's,
// and a value here would be stale in this repository rather than wrong in theirs.
func TestNoProviderNamesAnEndpointOrAModel(t *testing.T) {
	host := regexp.MustCompile(`https?://|\.com|\.ai\b`)
	for _, p := range providers() {
		for _, field := range append([]string{p.ID, p.Summary, p.Binary}, p.Env...) {
			if host.MatchString(field) {
				t.Errorf("provider %q names a host in %q", p.ID, field)
			}
		}
	}
}

func TestChooseProviderByFlag(t *testing.T) {
	p, err := ChooseProvider(strings.NewReader(""), &strings.Builder{}, "gemini", true)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "gemini" {
		t.Errorf("want gemini, got %q", p.ID)
	}
}

func TestChooseProviderRejectsAnUnknownName(t *testing.T) {
	_, err := ChooseProvider(strings.NewReader(""), &strings.Builder{}, "geminii", true)
	if err == nil {
		t.Fatal("want an error rather than a near-miss fallback")
	}
	if !strings.Contains(err.Error(), "geminii") {
		t.Errorf("want the name echoed, got %v", err)
	}
}

func TestChooseProviderPrompts(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		wantErr           bool
	}{
		{name: "by number", input: "3\n", want: "gemini"},
		{name: "by name", input: "codex\n", want: "codex"},
		{name: "with whitespace", input: "  anthropic  \n", want: "anthropic"},
		{name: "blank means none", input: "\n", want: ""},
		{name: "no input at all", input: "", want: ""},
		{name: "no trailing newline", input: "openai", want: "openai"},
		{name: "number out of range", input: "9\n", wantErr: true},
		{name: "nonsense", input: "chatgpt\n", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := &strings.Builder{}
			p, err := ChooseProvider(strings.NewReader(tc.input), out, "", false)
			switch {
			case tc.wantErr:
				if err == nil || errors.Is(err, ErrNoProviderChosen) {
					t.Fatalf("want a refusal, got %q %v", p.ID, err)
				}
				return
			case tc.want == "":
				if !errors.Is(err, ErrNoProviderChosen) {
					t.Fatalf("want ErrNoProviderChosen, got %q %v", p.ID, err)
				}
			default:
				if err != nil || p.ID != tc.want {
					t.Fatalf("want %q, got %q %v", tc.want, p.ID, err)
				}
			}
			// Every path is offered, so nobody has to know the names in advance.
			for _, id := range config.Providers() {
				if !strings.Contains(out.String(), id) {
					t.Errorf("the prompt omits %q:\n%s", id, out.String())
				}
			}
		})
	}
}

func TestAssumeYesDoesNotPrompt(t *testing.T) {
	out := &strings.Builder{}
	_, err := ChooseProvider(strings.NewReader("3\n"), out, "", true)
	if !errors.Is(err, ErrNoProviderChosen) {
		t.Fatalf("want no provider, got %v", err)
	}
	if out.String() != "" {
		t.Errorf("--yes must ask nothing, got:\n%s", out.String())
	}
}

// The proof for this PR: choosing a provider writes the provider block and names
// the variables, and nothing anywhere holds a value.
func TestInstallWithAProviderWritesTheBlockAndNamesTheVariables(t *testing.T) {
	root := testfixture.Destination(t, "tiny-monorepo")
	gemini, err := ProviderByID("gemini")
	if err != nil {
		t.Fatal(err)
	}
	plan := BuildPlan(mustDetect(t, root), releaseV, Options{Provider: gemini})
	if _, err := Install(plan, releaseV); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := config.Resolve(root, "", func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LaneB.Provider != "gemini" {
		t.Errorf("want provider gemini in the config, got %q", cfg.LaneB.Provider)
	}
	// The exit criterion: off regardless of provider, flipped by hand.
	if cfg.LaneB.Enabled {
		t.Error("the model lane must be off in every generated config")
	}
	if cfg.LaneB.Model != "" {
		t.Errorf("no model name may be written; got %q", cfg.LaneB.Model)
	}

	env := read(t, root, ".review/.env.example")
	for _, name := range gemini.Env {
		if !strings.Contains(env, name+"=") {
			t.Errorf(".env.example does not name %s:\n%s", name, env)
		}
	}
	if regexp.MustCompile(`(?m)^[A-Z_]+=\S`).MatchString(env) {
		t.Errorf(".env.example assigns a value:\n%s", env)
	}
	// The API paths need the secret in CI, so the workflow must carry it: turning
	// the lane on should be one edit, not two.
	if wf := read(t, root, ".github/workflows/review.yml"); !strings.Contains(wf, "secrets.REVIEW_MODEL_API_KEY") {
		t.Errorf("want the model secret wired into the workflow:\n%s", wf)
	}
}

// A subscription path needs no secret and cannot work on a hosted runner, and the
// generated workflow has to say both.
func TestInstallWithASubscriptionPath(t *testing.T) {
	root := testfixture.Destination(t, "tiny-monorepo")
	claudeCode, err := ProviderByID("claude-code")
	if err != nil {
		t.Fatal(err)
	}
	plan := BuildPlan(mustDetect(t, root), releaseV, Options{Provider: claudeCode})
	if !mentions(plan.Notes, "hosted runner") {
		t.Errorf("want the CI limitation in the plan, got %v", plan.Notes)
	}
	if _, err := Install(plan, releaseV); err != nil {
		t.Fatal(err)
	}

	wf := read(t, root, ".github/workflows/review.yml")
	if strings.Contains(wf, "secrets.REVIEW_MODEL_API_KEY") {
		t.Errorf("a subscription path needs no API key secret:\n%s", wf)
	}
	if !strings.Contains(wf, "no such session") {
		t.Errorf("want the workflow to explain why the lane is quiet:\n%s", wf)
	}
	env := read(t, root, ".review/.env.example")
	if strings.Contains(env, "REVIEW_MODEL_API_KEY") {
		t.Errorf("a subscription path needs no key; .env.example should not name one:\n%s", env)
	}
}

func TestInstallWithNoProviderSaysSo(t *testing.T) {
	root := testfixture.Destination(t, "tiny-monorepo")
	plan := BuildPlan(mustDetect(t, root), releaseV, Options{})
	if !mentions(plan.Notes, "no model access path chosen") {
		t.Errorf("want the absence stated, got %v", plan.Notes)
	}
	if _, err := Install(plan, releaseV); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Resolve(root, "", func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LaneB.Provider != "" {
		t.Errorf("want no provider, got %q", cfg.LaneB.Provider)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("a config with no provider must still be valid: %v", err)
	}
}
