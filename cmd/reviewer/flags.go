// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
)

// defaultBase is what --base means when nobody said. For a pull request review it
// is replaced by the pull request's own base, so the default only matters for a
// local run.
const defaultBase = "main"

// options is the parsed command line.
type options struct {
	subcommand   string
	statePath    string
	root         string
	base         string
	staged       bool
	pr           int
	config       string
	reporter     string
	only         []string
	skip         []string
	dryRun       bool
	force        bool
	provider     string
	rulesDir     string
	write        bool
	noInvalidate bool
	since        string
	repo         string
	format       string
	yes          bool
	version      bool
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
  --dry-run         with --reporter github, print what would be posted and post nothing;
                    with init, print the plan and write nothing
  --force           with init, replace files that already exist
  --rules DIR       with rules test, the rule directory (default: .review/rules)
  --write           with baseline, record the measurement
  --since WINDOW    with metrics, how far back to look: 7d, 2w, 36h (default: 7d)
  --repo OWNER/NAME with metrics, the repository, overriding config
  --format NAME     with metrics, markdown or csv (default: markdown)
  --no-invalidate   skip the model lane's second, disproving pass. For debugging
                    what the first pass produces; the run says its findings are
                    unchecked
  --provider NAME   with init, configure this model access path. One of
                    openai-compatible, openai, gemini, anthropic, claude-code, codex
  --yes             with init, do not prompt; leave the model lane unconfigured
                    unless --provider says otherwise
  --only IDs        run only these analyzers, comma-separated
  --skip IDs        run everything except these analyzers, comma-separated
  --version         print the version and exit

subcommands:
  metrics           acceptance by rule: how many of this tool's findings anybody
                    acted on, so a rule nobody wants can be deleted on evidence

  baseline --write  record the type-coverage ratchet's mark, which future runs
                    compare against

  rules test        check this repository's rule pack: that it loads, that no two
                    rules share an id, that every rule has a matching and a
                    non-matching case, and that the patterns agree with them

  init              inspect this repository and print what installing the
                    reviewer into it would change

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
var subcommands = map[string]bool{
	"init": true, "watch": true, "version": true, "rules": true, "baseline": true,
	"metrics": true,
}

// twoWordSubcommands take a second bare word. Recognised before flag parsing for
// the same reason as the first: Go's flag package stops at the first positional
// argument, so `reviewer rules test --rules x` would otherwise leave both the word
// and the flag unparsed.
var twoWordSubcommands = map[string]map[string]bool{"rules": {"test": true}}

func parse(args []string, stderr io.Writer) (options, error) {
	var o options
	var only, skip string

	if len(args) > 0 && subcommands[args[0]] {
		o.subcommand = args[0]
		args = args[1:]
		if verbs, ok := twoWordSubcommands[o.subcommand]; ok {
			if len(args) == 0 {
				fmt.Fprint(stderr, usage)
				return options{}, errUsage{fmt.Errorf("%s needs a verb: %s", o.subcommand, verbList(verbs))}
			}
			if !verbs[args[0]] {
				fmt.Fprint(stderr, usage)
				return options{}, errUsage{fmt.Errorf("%s %q is not a thing; try %s", o.subcommand, args[0], verbList(verbs))}
			}
			o.subcommand += " " + args[0]
			args = args[1:]
		}
	}

	fs := flag.NewFlagSet("reviewer", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }

	fs.StringVar(&o.root, "root", ".", "")
	fs.StringVar(&o.base, "base", defaultBase, "")
	fs.BoolVar(&o.staged, "staged", false, "")
	fs.IntVar(&o.pr, "pr", 0, "")
	fs.StringVar(&o.config, "config", "", "")
	fs.StringVar(&o.reporter, "reporter", "text", "")
	fs.StringVar(&only, "only", "", "")
	fs.StringVar(&skip, "skip", "", "")
	fs.StringVar(&o.statePath, "state", ".review/watch-state.json", "")
	fs.BoolVar(&o.dryRun, "dry-run", false, "")
	fs.BoolVar(&o.force, "force", false, "")
	fs.StringVar(&o.provider, "provider", "", "")
	fs.StringVar(&o.rulesDir, "rules", "", "")
	fs.BoolVar(&o.write, "write", false, "")
	fs.BoolVar(&o.noInvalidate, "no-invalidate", false, "")
	fs.StringVar(&o.since, "since", "", "")
	fs.StringVar(&o.repo, "repo", "", "")
	fs.StringVar(&o.format, "format", "", "")
	fs.BoolVar(&o.yes, "yes", false, "")
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
	case o.staged && o.subcommand == "watch":
		// watch sets --pr per pull request. With --staged also set, the staged
		// diff's findings would be posted onto every open pull request.
		return options{}, errUsage{fmt.Errorf("watch reviews pull requests; --staged reviews your index")}
	case len(o.only) > 0 && len(o.skip) > 0:
		return options{}, errUsage{fmt.Errorf("pass --only or --skip, not both")}
	case o.pr < 0:
		return options{}, errUsage{fmt.Errorf("--pr must be a positive pull request number")}
	case o.force && o.subcommand != "init":
		return options{}, errUsage{fmt.Errorf("--force applies to init, which decides what to overwrite")}
	case o.provider != "" && o.subcommand != "init":
		return options{}, errUsage{fmt.Errorf("--provider applies to init; a review reads the provider from config")}
	case o.rulesDir != "" && o.subcommand != "rules test":
		return options{}, errUsage{fmt.Errorf("--rules applies to `rules test`; a review reads the rule directory from config")}
	case o.write && o.subcommand != "baseline":
		return options{}, errUsage{fmt.Errorf("--write applies to baseline; a review never writes to the repository")}
	case (o.since != "" || o.format != "") && o.subcommand != "metrics":
		return options{}, errUsage{fmt.Errorf("--since and --format apply to metrics")}
	case o.repo != "" && o.subcommand != "metrics":
		return options{}, errUsage{fmt.Errorf("--repo applies to metrics; a review reads the repository from config")}
	case o.yes && o.subcommand != "init":
		return options{}, errUsage{fmt.Errorf("--yes applies to init, which is the only subcommand that asks anything")}
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

func verbList(verbs map[string]bool) string {
	var names []string
	for verb := range verbs {
		names = append(names, verb)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// errUsage marks misuse of the command line. Exits 2.
type errUsage struct{ error }

func (e errUsage) Unwrap() error { return e.error }

// errCheckFailed marks a check command whose subject failed the check — a rule pack
// that is not ready, not a tool that is broken.
//
// A review exits 0 whatever it finds (ADR-0009), and that is about not blocking a
// pull request on an opinion. `rules test` is not a review: it is a check with a
// right answer, run by someone who wants to know, and it has to be usable in a
// script. So it exits 1, which is neither of the other two things.
type errCheckFailed struct{ error }

func (e errCheckFailed) Unwrap() error { return e.error }
