// SPDX-License-Identifier: MIT

package model

import (
	"fmt"

	"github.com/adamtait/reviewer/internal/config"
)

// New builds the provider the destination repository configured.
//
// Every way of not being ready returns ErrDisabled with a reason, and none of them
// is an error the run should fail on: a review with no model lane is the product
// working as designed (ADR-0009). The reason is worth carrying because "lane B did
// nothing" has five causes and they need different fixes.
func New(cfg config.Config, secrets config.Secrets) (Provider, error) {
	if !cfg.LaneB.Enabled {
		return nil, Unavailable("laneB.enabled is false in the configuration")
	}
	if cfg.LaneB.Provider == "" {
		return nil, Unavailable("no laneB.provider is configured")
	}
	if cfg.LaneB.Model == "" {
		// No default. A model name compiled in here goes stale faster than a
		// release, and choosing one on someone's behalf spends their money.
		return nil, Unavailable("no model is named; set %s in the environment or laneB.model",
			config.EnvModelName)
	}

	switch cfg.LaneB.Provider {
	case "openai-compatible", "openai", "gemini", "anthropic":
		return newHTTP(cfg, secrets)
	case "claude-code", "codex":
		return newCLI(cfg)
	default:
		// Unreachable through Resolve, which validates the name, but a provider
		// added to config and not here would otherwise be silently ignored.
		return nil, Unavailable("no adapter for provider %q", cfg.LaneB.Provider)
	}
}

// The adapters land next: the HTTP paths in PR-37 to PR-39, the subscription CLIs
// in PR-40. Until then every path reports itself unavailable with a reason, which
// is the same behaviour as a path nobody configured — so nothing above this
// package has to know the difference.
func newHTTP(cfg config.Config, secrets config.Secrets) (Provider, error) {
	return nil, Unavailable("no adapter for %q is built yet", cfg.LaneB.Provider)
}

func newCLI(cfg config.Config) (Provider, error) {
	return nil, Unavailable("no adapter for %q is built yet", cfg.LaneB.Provider)
}

// Describe says why the lane is off, for a run's warnings. Kept separate from the
// error so a caller can report the reason without deciding whether to.
func Describe(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("the model lane did not run: %v", err)
}
