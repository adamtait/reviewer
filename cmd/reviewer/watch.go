// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"io"

	"github.com/adamtait/reviewer/internal/config"
	"github.com/adamtait/reviewer/internal/ghclient"
	"github.com/adamtait/reviewer/internal/poller"
)

// watch reviews every open pull request that has changed since the last poll.
//
// One pass, not a loop: scheduling belongs to launchd, systemd or cron, which
// already handle restarts, logging and machine sleep. A daemon here would
// reimplement all three worse (ADR-0008).
func watch(ctx context.Context, o options, stdout, stderr io.Writer, getenv func(string) string) error {
	cfg, secrets, err := config.Resolve(o.root, o.config, getenv)
	if err != nil {
		return err
	}
	client, err := ghclient.New(cfg.GitHub.APIBaseURL, secrets.GitHubToken, "reviewer/"+version)
	if err != nil {
		return errUsage{err}
	}
	repo, err := parseRepo(cfg.GitHub.Repo)
	if err != nil {
		return errUsage{err}
	}

	return poller.Poll(ctx, client, func(ctx context.Context, number int) error {
		// Each pull request is reviewed through exactly the path the Action uses,
		// so the two triggers cannot drift in what they report.
		prOpts := o
		prOpts.pr = number
		prOpts.reporter = "github"
		return review(ctx, prOpts, stdout, stderr, getenv)
	}, poller.Options{
		Repo:      repo,
		StatePath: cfg.Abs(o.statePath),
		DryRun:    o.dryRun,
		Log:       stderr,
	})
}
