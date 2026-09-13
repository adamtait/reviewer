// SPDX-License-Identifier: MIT

package model

// The remaining shapes land in PR-38 (anthropic), PR-39 (gemini) and PR-40 (the
// subscription CLIs). Until then each reports itself unavailable with a reason,
// which is what a path nobody configured does — so nothing above this package has
// to know the difference.

import "github.com/adamtait/reviewer/internal/config"

func newCLI(cfg config.Config) (Provider, error) {
	return nil, Unavailable("no adapter for %q is built yet", cfg.LaneB.Provider)
}
