// SPDX-License-Identifier: MIT

// Package builtin is the plugin that serves the analyzers compiled into the
// reviewer binary: the ones that wrap an external scanner rather than needing a
// language runtime.
//
// It is a plugin like any other (ADR-0006) and is reached over the protocol
// (ADR-0027). Nothing here is privileged: the same descriptor validation, lane
// gating and fault containment apply as to a third-party plugin.
package builtin

import (
	"context"
	"fmt"

	"github.com/adamtait/reviewer/internal/analyzers/gitleaks"
	"github.com/adamtait/reviewer/internal/config"
	"github.com/adamtait/reviewer/pkg/finding"
	"github.com/adamtait/reviewer/pkg/plugin"
)

// Name is the plugin's id, used to address its analyzers and to prefix its
// diagnostics in the run log.
const Name = "builtin"

// Handler serves the built-in analyzers.
type Handler struct {
	cfg     config.Config
	version string
	// Unavailable records why an analyzer cannot run, filled in by Describe so
	// the reason is reported once rather than per analyzer invocation.
	unavailable map[string]string
}

// New returns the built-in plugin for one run.
func New(cfg config.Config, version string) *Handler {
	return &Handler{cfg: cfg, version: version, unavailable: map[string]string{}}
}

func (h *Handler) Name() string    { return Name }
func (h *Handler) Version() string { return h.version }

// Describe probes for each analyzer's external binary. Doing it here means a
// repository without gitleaks installed is told once, at the top of the run,
// rather than discovering it per analyzer.
func (h *Handler) Describe() []plugin.Descriptor {
	return []plugin.Descriptor{
		h.descriptor(gitleaks.ID, gitleaks.Order, finding.LaneDeterministic),
	}
}

func (h *Handler) descriptor(id string, order int, lane finding.Lane) plugin.Descriptor {
	d := plugin.Descriptor{ID: id, Lane: lane, Order: order, Available: true}
	if reason := h.probe(id); reason != "" {
		d.Available = false
		d.Unavailable = reason
		h.unavailable[id] = reason
	}
	return d
}

// probe returns why an analyzer cannot run, or "" when it can.
func (h *Handler) probe(id string) string {
	switch id {
	case gitleaks.ID:
		if _, _, err := gitleaks.Probe(h.binary(id)); err != nil {
			return err.Error()
		}
	}
	return ""
}

// binary returns the executable for an analyzer: whatever the repository pinned,
// or the tool's default name. Never a compiled-in path (ADR-0004).
func (h *Handler) binary(id string) string {
	if tool, ok := h.cfg.Tools[id]; ok && tool.Path != "" {
		return tool.Path
	}
	return ""
}

// Analyze runs one built-in analyzer.
//
// The context is the run's, not a fresh one: a built-in plugin shares the host's
// address space, so nothing can kill it, and the context is the only way an
// analyzer's own subprocess gets stopped when the run's deadline passes
// (ADR-0027).
func (h *Handler) Analyze(ctx context.Context, id string, req plugin.AnalyzeRequest) ([]finding.Finding, []string, error) {
	switch id {
	case gitleaks.ID:
		return gitleaks.Analyze(ctx, req, h.binary(id))
	default:
		return nil, nil, fmt.Errorf("no built-in analyzer named %q", id)
	}
}
