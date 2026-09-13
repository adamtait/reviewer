// SPDX-License-Identifier: MIT

// Package poller reviews pull requests on a schedule instead of on a push.
//
// It exists because the Action is the better trigger and might not be available:
// an organisation can disable Actions, restrict them to verified publishers, or
// withhold `pull-requests: write`. A system whose only trigger needs someone
// else's permission is a system that can be switched off by a policy change
// (ADR-0019).
package poller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/adamtait/reviewer/internal/ghclient"
)

// Reviewer reviews one pull request. The poller decides *which*; it knows nothing
// about how a review works.
type Reviewer func(ctx context.Context, number int) error

// Options configure one poll.
type Options struct {
	Repo ghclient.Repo
	// StatePath holds the watermark between runs. A poller with no memory
	// re-reviews every open pull request on every tick, which at ten to fifty
	// pull requests a week is both slow and — because dedupe is per finding, not
	// per run — a lot of wasted API calls.
	StatePath string
	// DryRun lists what would be reviewed and reviews nothing.
	DryRun bool
	// Log receives one line per decision, so a launchd job leaves a readable trail.
	Log io.Writer
}

// state is what survives between polls.
type state struct {
	// Seen maps pull request number to the updatedAt we last reviewed. Keyed by
	// timestamp rather than by a boolean so a pull request that gets a new push is
	// reviewed again, which is the whole point.
	Seen map[int]time.Time `json:"seen"`
}

// Poll reviews every open pull request that has changed since the last poll.
func Poll(ctx context.Context, client ghclient.Client, review Reviewer, opts Options) error {
	log := opts.Log
	if log == nil {
		log = io.Discard
	}

	previous, err := loadState(opts.StatePath)
	if err != nil {
		// A corrupt or unreadable watermark means "review everything once" rather
		// than "stop", because the cost is duplicate API calls and the dedupe
		// layer absorbs the rest.
		fmt.Fprintf(log, "could not read %s (%v); treating every open pull request as new\n", opts.StatePath, err)
		previous = state{Seen: map[int]time.Time{}}
	}

	open, err := client.ListPullRequests(ctx, opts.Repo, "open")
	if err != nil {
		return fmt.Errorf("listing pull requests: %w", err)
	}

	next := state{Seen: map[int]time.Time{}}
	var reviewed, skipped int

	// Oldest first, so a crash part-way through leaves the watermark advanced for
	// the ones that did complete.
	sort.Slice(open, func(i, j int) bool { return open[i].UpdatedAt.Before(open[j].UpdatedAt) })

	for _, pr := range open {
		// Draft pull requests are work in progress; commenting on one is
		// interrupting someone who has not asked for an opinion yet.
		if pr.Draft {
			fmt.Fprintf(log, "#%d: draft, skipping\n", pr.Number)
			continue
		}

		last, seen := previous.Seen[pr.Number]
		if seen && !pr.UpdatedAt.After(last) {
			next.Seen[pr.Number] = last
			skipped++
			continue
		}

		if opts.DryRun {
			fmt.Fprintf(log, "#%d: would review (updated %s)\n", pr.Number, pr.UpdatedAt.Format(time.RFC3339))
			next.Seen[pr.Number] = pr.UpdatedAt
			reviewed++
			continue
		}

		fmt.Fprintf(log, "#%d: reviewing (updated %s)\n", pr.Number, pr.UpdatedAt.Format(time.RFC3339))
		if err := review(ctx, pr.Number); err != nil {
			// One pull request failing must not stop the rest, and its watermark
			// is deliberately not advanced so the next poll retries it.
			fmt.Fprintf(log, "#%d: %v\n", pr.Number, err)
			if last, ok := previous.Seen[pr.Number]; ok {
				next.Seen[pr.Number] = last
			}
			continue
		}
		next.Seen[pr.Number] = pr.UpdatedAt
		reviewed++
	}

	fmt.Fprintf(log, "%d reviewed, %d unchanged\n", reviewed, skipped)

	if opts.DryRun {
		return nil
	}
	return saveState(opts.StatePath, next)
}

func loadState(path string) (state, error) {
	if path == "" {
		return state{Seen: map[int]time.Time{}}, nil
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return state{Seen: map[int]time.Time{}}, nil
	}
	if err != nil {
		return state{}, err
	}
	var s state
	if err := json.Unmarshal(raw, &s); err != nil {
		return state{}, err
	}
	if s.Seen == nil {
		s.Seen = map[int]time.Time{}
	}
	return s, nil
}

// saveState writes the watermark atomically. A poller killed mid-write would
// otherwise leave a truncated file, and the next run would re-review everything.
func saveState(path string, s state) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
