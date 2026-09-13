// SPDX-License-Identifier: MIT

// Command reviewer runs an advisory code review over a pull request or a local
// diff. It never blocks: every exit status is 0 except a usage error (ADR-0009).
package main

import (
	"fmt"
	"io"
	"os"
)

// version is overwritten at release time by the linker.
var version = "dev"

// exitUsage is the one non-zero status this program may return (ADR-0009).
const exitUsage = 2

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "reviewer: %v\n", err)
		os.Exit(exitUsage)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 && (args[0] == "--version" || args[0] == "version") {
		fmt.Fprintf(stdout, "reviewer %s\n", version)
		return nil
	}
	fmt.Fprintln(stderr, "reviewer: no analysis wired up yet")
	return nil
}
