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
	"strings"
	"time"

	"github.com/adamtait/reviewer/internal/workspace/api"
	"github.com/adamtait/reviewer/internal/workspace/obs"
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

	// The one place the secrets leave their type. From here the cookie key goes
	// to sign-in and the provider keys to the model gateway, as those arrive;
	// today their only use is to be kept out of the log.
	keys := secrets.Reveal()
	known := []string{base64.StdEncoding.EncodeToString(keys.Cookie)}
	for _, key := range keys.Provider {
		known = append(known, key)
	}
	log := obs.New(logTo, obs.NewRedactor(known...))

	server := &http.Server{
		Handler: api.New(log, version).Handler(),
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
