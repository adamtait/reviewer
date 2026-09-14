// SPDX-License-Identifier: MIT

package installer

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/adamtait/reviewer/internal/config"
)

// Provider describes one model access path: what it needs to work, and where it
// can work. The set is config.Providers(); this table adds what the installer has
// to know to generate the right files for each.
//
// No endpoint and no model name appears here. An endpoint is config's business
// (ADR-0004) and a model name goes stale faster than a release; both are named as
// variables and left for the destination to fill.
type Provider struct {
	// ID is the value written to laneB.provider.
	ID string
	// Summary is what the chooser reads.
	Summary string
	// Env are the variables this path needs, in the order a person fills them.
	Env []string
	// Binary is the CLI this path drives, for the two subscription paths. Empty
	// for the API paths.
	Binary string
	// WorksInCI is false for a path that needs a signed-in CLI on the machine.
	// A hosted runner has no subscription, so the model lane on those paths runs
	// only where someone is logged in.
	WorksInCI bool
}

// NeedsAPIKey reports whether this path reads a credential from the environment.
// The two subscription paths do not: they borrow a session a person already
// established, which is the reason to offer them at all.
func (p Provider) NeedsAPIKey() bool {
	for _, name := range p.Env {
		if name == config.EnvModelAPIKey {
			return true
		}
	}
	return false
}

// providers is keyed by nothing: order is the order offered, and it runs from the
// most portable path to the least.
func providers() []Provider {
	return []Provider{
		{
			ID:        "anthropic",
			Summary:   "Claude API, billed per token",
			Env:       []string{config.EnvModelAPIKey, config.EnvModelName},
			WorksInCI: true,
		},
		{
			ID:        "openai",
			Summary:   "OpenAI API, billed per token",
			Env:       []string{config.EnvModelAPIKey, config.EnvModelName},
			WorksInCI: true,
		},
		{
			ID:        "gemini",
			Summary:   "Gemini API, billed per token",
			Env:       []string{config.EnvModelAPIKey, config.EnvModelName},
			WorksInCI: true,
		},
		{
			ID:      "openai-compatible",
			Summary: "any OpenAI-shaped endpoint: a gateway, a proxy, or a local server",
			// The only path that needs a base URL, which is exactly why it exists:
			// an endpoint this repository must never name can be supplied here.
			Env:       []string{config.EnvModelAPIKey, config.EnvModelBaseURL, config.EnvModelName},
			WorksInCI: true,
		},
		{
			ID:        "claude-code",
			Summary:   "the Claude Code CLI, using a subscription you are already signed in to",
			Binary:    "claude",
			WorksInCI: false,
		},
		{
			ID:        "codex",
			Summary:   "the Codex CLI, using a subscription you are already signed in to",
			Binary:    "codex",
			WorksInCI: false,
		},
	}
}

// ProviderByID returns the named path. An unknown name is an error rather than a
// fallback: silently installing a different provider than the one asked for is
// worse than refusing.
func ProviderByID(id string) (Provider, error) {
	for _, p := range providers() {
		if p.ID == id {
			return p, nil
		}
	}
	return Provider{}, fmt.Errorf("unknown provider %q; choose one of %s",
		id, strings.Join(config.Providers(), ", "))
}

// ErrNoProviderChosen means the chooser got no answer. Not a failure: an install
// with no provider is a complete, working deterministic-lane install, and the
// model lane is off in either case.
var ErrNoProviderChosen = errors.New("no provider chosen")

// ChooseProvider settles which model access path the destination will use.
//
// requested wins when it is set. assumeYes suppresses the prompt. Otherwise the
// list is printed and one line is read; an empty line, or a stream with nothing in
// it, means no provider — which is why this does not need to know whether it is
// attached to a terminal.
func ChooseProvider(in io.Reader, out io.Writer, requested string, assumeYes bool) (Provider, error) {
	if requested != "" {
		return ProviderByID(requested)
	}
	if assumeYes {
		return Provider{}, ErrNoProviderChosen
	}

	all := providers()
	fmt.Fprintln(out, "\nWhich model access path should the model lane use?")
	fmt.Fprintln(out, "The lane stays switched off either way; this only decides what to configure.")
	for i, p := range all {
		suffix := ""
		if !p.WorksInCI {
			suffix = " — local runs only, no subscription on a hosted runner"
		}
		fmt.Fprintf(out, "  %d. %-18s %s%s\n", i+1, p.ID, p.Summary, suffix)
	}
	fmt.Fprint(out, "Number or name, or blank for none: ")

	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		// EOF with nothing read: not interactive, and not an error.
		return Provider{}, ErrNoProviderChosen
	}
	answer := strings.TrimSpace(line)
	if answer == "" {
		return Provider{}, ErrNoProviderChosen
	}
	if n, convErr := strconv.Atoi(answer); convErr == nil {
		if n < 1 || n > len(all) {
			return Provider{}, fmt.Errorf("%q is not one of the %d choices", answer, len(all))
		}
		return all[n-1], nil
	}
	return ProviderByID(answer)
}
