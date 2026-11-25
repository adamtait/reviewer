// SPDX-License-Identifier: MIT

// Command workspace serves the review workspace: a web application used beside
// GitHub's pull request page, described in docs/designs/review-workspace.md.
//
// Unlike the reviewer command beside it, this one is a server, and it does
// fail: a configuration it cannot serve from is an error and a non-zero exit,
// because a service that starts with half a configuration is worse than one
// that does not start (ADR-0029). ADR-0009's "never block" is about a review,
// not about this.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// version is overwritten at build time by the linker.
var version = "dev"

const (
	exitFailed = 1
	exitUsage  = 2
)

// errUsage is a mistyped command line.
type errUsage struct{ error }

const usage = `usage:
  workspace serve --config FILE    serve the API and the web application
  workspace version                print the version`

func main() {
	// Cloud Run stops a container with SIGTERM and gives it ten seconds.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr, os.Getenv); err != nil {
		fmt.Fprintf(os.Stderr, "workspace: %v\n", err)
		var usageErr errUsage
		if errors.As(err, &usageErr) {
			os.Exit(exitUsage)
		}
		os.Exit(exitFailed)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	if len(args) == 0 {
		return errUsage{errors.New("no command given\n" + usage)}
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version)
		return nil
	case "serve":
		flags := flag.NewFlagSet("serve", flag.ContinueOnError)
		flags.SetOutput(stderr)
		configPath := flags.String("config", "", "path to the configuration file")
		if err := flags.Parse(args[1:]); err != nil {
			return errUsage{err}
		}
		if *configPath == "" {
			return errUsage{errors.New("serve needs --config FILE")}
		}
		return serve(ctx, *configPath, stdout, getenv)
	default:
		return errUsage{fmt.Errorf("unknown command %q\n%s", args[0], usage)}
	}
}
