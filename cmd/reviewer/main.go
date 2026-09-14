// SPDX-License-Identifier: MIT

// Command reviewer runs an advisory code review over a pull request or a local
// diff.
//
// It never blocks a build: whatever it finds, and whatever fails while it looks,
// it exits 0 (ADR-0009). The single exception is a usage error, which exits 2 —
// a mistyped flag should not be silently treated as a clean review.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/adamtait/reviewer/pkg/plugin"
)

// protocolVersion is reported by --version: the compatibility question a plugin
// author asks is which protocol, not which release (ADR-0026).
const protocolVersion = plugin.Protocol

// version is overwritten at release time by the linker.
var version = "dev"

// exitUsage is the one non-zero status this program may return.
const exitUsage = 2

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, os.Getenv); err != nil {
		fmt.Fprintf(os.Stderr, "reviewer: %v\n", err)
		var usageErr errUsage
		if errors.As(err, &usageErr) {
			os.Exit(exitUsage)
		}
		// Everything else is a failure to review, not a review that failed.
		os.Exit(0)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	o, err := parse(args, stderr)
	if err != nil {
		return err
	}
	if o.version {
		_, err := fmt.Fprintf(stdout, "reviewer %s (plugin protocol %d)\n", version, protocolVersion)
		return err
	}
	return review(ctx, o, stdout, stderr, getenv)
}
