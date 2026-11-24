// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/adamtait/reviewer/internal/ghclient"
	"github.com/adamtait/reviewer/internal/workspace/access"
	"github.com/adamtait/reviewer/internal/workspace/api"
	"github.com/adamtait/reviewer/internal/workspace/identity"
	"github.com/adamtait/reviewer/internal/workspace/navigation"
	"github.com/adamtait/reviewer/internal/workspace/obs"
	"github.com/adamtait/reviewer/internal/workspace/source"
	"github.com/adamtait/reviewer/internal/workspace/web"
	"github.com/adamtait/reviewer/internal/workspace/wsconfig"
)

// shutdownGrace is how long in-flight requests get once the process is told to
// stop. Cloud Run allows ten seconds between SIGTERM and SIGKILL.
const shutdownGrace = 8 * time.Second

// onListen is called with the address once the server is accepting
// connections. Tests set it to learn which port ":0" became.
var onListen func(addr string)

// serve loads the configuration and serves until ctx is cancelled.
func serve(ctx context.Context, configPath string, logTo io.Writer, getenv func(string) string) error {
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		return err
	}
	secrets, secretProblems := wsconfig.LoadSecrets(cfg, getenv)
	if problems := append(cfg.Problems(), secretProblems...); len(problems) > 0 {
		// Every problem at once. The operator fixes the file in one pass
		// instead of one restart per mistake.
		return fmt.Errorf("the service cannot start with this configuration (%d problems):\n  - %s",
			len(problems), strings.Join(problems, "\n  - "))
	}

	// The one place the secrets leave their type. The cookie key goes to
	// sign-in. The provider keys will go to the model gateway when it arrives;
	// until then their only use is to be kept out of the log.
	keys := secrets.Reveal()
	known := []string{base64.StdEncoding.EncodeToString(keys.Cookie)}
	for _, key := range keys.Provider {
		known = append(known, key)
	}
	log := obs.New(logTo, obs.NewRedactor(known...))

	handler, err := newHandler(ctx, cfg, keys.Cookie, log)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler: handler,
		// A client that never finishes its headers must not hold a connection
		// open. There is deliberately no write timeout: an answer is streamed
		// for as long as the model takes, and each handler sets its own deadline.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", cfg.Listen, err)
	}
	log.Event(ctx, slog.LevelInfo, "listening",
		slog.String("address", listener.Addr().String()),
		slog.String("version", version),
		slog.Int("repositories", len(cfg.Repositories)),
		slog.String("secrets", secrets.String()))
	if onListen != nil {
		onListen(listener.Addr().String())
	}

	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()

	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := server.Shutdown(stopCtx); err != nil {
		return fmt.Errorf("stopping: %w", err)
	}
	if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Event(context.Background(), slog.LevelInfo, "stopped")
	return nil
}

// userAgent identifies the service in GitHub's logs.
func userAgent() string { return "review-workspace/" + version }

// newHandler builds the service from its configuration.
func newHandler(ctx context.Context, cfg wsconfig.Config, cookieKey []byte, log *obs.Logger) (http.Handler, error) {
	checker := access.New(cfg.Repositories, time.Duration(cfg.AccessCacheSeconds)*time.Second,
		func(owner, name string) {
			// A configuration mistake, found only once a user's token can be
			// asked about the repository: the service has no credential of its
			// own to check with at start.
			log.Event(ctx, slog.LevelError, "a connected repository is public and is refused for every user",
				slog.String("owner", owner), slog.String("repo", name))
		})

	public, err := url.Parse(cfg.PublicURL)
	if err != nil {
		return nil, fmt.Errorf("publicUrl: %w", err)
	}
	cookies, err := identity.NewCookies(cookieKey, public.Scheme == "https")
	if err != nil {
		return nil, err
	}

	// One cache for everything held in memory, so one number bounds it all.
	// The navigation index will share it.
	limits := cfg.Limits
	cache := source.NewCache(limits.CacheBytes, time.Duration(limits.FetchSeconds)*time.Second)
	store := source.NewStore(cache, source.Limits{ArchiveBytes: limits.ArchiveBytes, FileBytes: limits.FileBytes})
	store.Observe = func(f source.Fetch) {
		// The only record of what a first open costs. It runs outside any
		// request, so the line names the commit and not a user.
		attrs := []slog.Attr{
			slog.String("owner", f.Owner), slog.String("repo", f.Repo), slog.String("sha", f.SHA),
			slog.Int64("took_ms", f.Took.Milliseconds()),
		}
		if f.Err != nil {
			log.Event(ctx, slog.LevelWarn, "archive_fetch_failed", append(attrs, slog.String("error", f.Err.Error()))...)
			return
		}
		held := cache.Stats()
		log.Event(ctx, slog.LevelInfo, "archive_fetched", append(attrs,
			slog.Int64("kept_bytes", f.Bytes), slog.Int("entries", f.Entries),
			slog.Int64("cache_bytes", held.Bytes), slog.Int("cache_entries", held.Entries))...)
	}

	return api.New(api.Deps{
		Log:     log,
		Version: version,
		Identity: &identity.TokenMethod{
			NewClient: func(token string) (*ghclient.REST, error) {
				return ghclient.New(cfg.GitHub.APIBaseURL, token, userAgent())
			},
			Access:        checker,
			SessionLength: time.Duration(cfg.SessionDays) * 24 * time.Hour,
		},
		Cookies: cookies,
		Access:  checker,
		Source:  store,
		Navigation: navigation.NewService(navigation.Config{
			WorkflowFile: cfg.IndexWorkflowFile,
			ArtifactName: cfg.IndexArtifactName,
			IndexBytes:   limits.IndexBytes,
		}, cache, store),
		WebBaseURL: cfg.GitHub.WebBaseURL,
		Web:        web.Handler(),
	}).Handler(), nil
}
