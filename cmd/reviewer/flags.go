// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// options is the parsed command line.
type options struct {
	subcommand string
	statePath  string
	root       string
	base       string
	staged     bool
	pr         int
	config     string
	reporter   string
	only       []string
	skip       []string
	dryRun     bool
	version    bool
}

const usage = `reviewer — advisory code review over a pull request or a local diff

usage:
  reviewer [flags]

flags:
  --root DIR        repository to review (default: the working directory)
  --base REF        review the diff against this ref's merge base (default: main)
  --staged          review the staged changes instead of a branch diff
  --pr N            review pull request N
  --config PATH     configuration file (default: .review/config.yaml under --root)
  --reporter NAME   text, rdjson or github (default: text)
  --dry-run         with --reporter github, print what would be posted and post nothing
  --only IDs        run only these analyzers, comma-separated
  --skip IDs        run everything except these analyzers, comma-separated
  --version         print the version and exit

subcommands:
  watch             review every open pull request that changed since the last
                    poll, then exit. Run it from launchd, systemd or cron —
                    scheduling is not this tool's job.

reviewer is advisory and never fails a build: it exits 0 whatever it finds.
A usage error exits 2.
`

// parse reads the command line. It returns an error only for genuine misuse,
// which is the sole path to a non-zero exit (ADR-0009).
// subcommands are recognised before flag parsing, because Go's flag package stops
// at the first positional argument — so `reviewer watch --dry-run` would leave
// --dry-run unparsed and report it as a stray argument.
var subcommands = map[string]bool{"watch": true, "version": true}

func parse(args []string, stderr io.Writer) (options, error) {
	var o options
	var only, skip string

	if len(args) > 0 && subcommands[args[0]] {
		o.subcommand = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("reviewer", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }

	fs.StringVar(&o.root, "root", ".", "")
	fs.StringVar(&o.base, "base", "main", "")
	fs.BoolVar(&o.staged, "staged", false, "")
	fs.IntVar(&o.pr, "pr", 0, "")
	fs.StringVar(&o.config, "config", "", "")
	fs.StringVar(&o.reporter, "reporter", "text", "")
	fs.StringVar(&only, "only", "", "")
	fs.StringVar(&skip, "skip", "", "")
	fs.StringVar(&o.statePath, "state", ".review/watch-state.json", "")
	fs.BoolVar(&o.dryRun, "dry-run", false, "")
	fs.BoolVar(&o.version, "version", false, "")

	if err := fs.Parse(args); err != nil {
		return options{}, errUsage{err}
	}
	if rest := fs.Args(); len(rest) > 0 {
		fmt.Fprint(stderr, usage)
		return options{}, errUsage{fmt.Errorf("unexpected argument %q", rest[0])}
	}
	// `reviewer version` and `--version` are both common; accept either.
	if o.subcommand == "version" {
		o.version = true
	}

	o.only = splitList(only)
	o.skip = splitList(skip)

	switch {
	case o.staged && o.pr != 0:
		return options{}, errUsage{fmt.Errorf("--staged and --pr describe different things to review")}
	case len(o.only) > 0 && len(o.skip) > 0:
		return options{}, errUsage{fmt.Errorf("pass --only or --skip, not both")}
	case o.pr < 0:
		return options{}, errUsage{fmt.Errorf("--pr must be a positive pull request number")}
	}
	return o, nil
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// errUsage marks the one error class that exits non-zero.
type errUsage struct{ error }

func (e errUsage) Unwrap() error { return e.error }
