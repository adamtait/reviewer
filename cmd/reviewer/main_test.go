// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/internal/testfixture"
)

func noEnv(string) string { return "" }

func TestParse(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		check func(*testing.T, options, error)
	}{
		{"defaults", nil, func(t *testing.T, o options, err error) {
			if err != nil {
				t.Fatal(err)
			}
			if o.base != "main" || o.reporter != "text" || o.root != "." {
				t.Fatalf("unexpected defaults: %+v", o)
			}
		}},
		{"version flag", []string{"--version"}, func(t *testing.T, o options, err error) {
			if err != nil || !o.version {
				t.Fatalf("want the version flag set, got %+v %v", o, err)
			}
		}},
		{"version subcommand", []string{"version"}, func(t *testing.T, o options, err error) {
			if err != nil || !o.version {
				t.Fatalf("want the version subcommand accepted, got %+v %v", o, err)
			}
		}},
		{"comma lists", []string{"--only", "tsc, eslint ,"}, func(t *testing.T, o options, err error) {
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(o.only, "|") != "tsc|eslint" {
				t.Fatalf("want whitespace and empties trimmed, got %v", o.only)
			}
		}},
		{"only and skip together", []string{"--only", "a", "--skip", "b"}, wantUsageError("not both")},
		{"staged and pr together", []string{"--staged", "--pr", "7"}, wantUsageError("different things")},
		{"negative pr", []string{"--pr", "-1"}, wantUsageError("positive")},
		{"stray argument", []string{"lint"}, wantUsageError(`unexpected argument "lint"`)},
		{"unknown flag", []string{"--nope"}, wantUsageError("")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o, err := parse(tc.args, new(bytes.Buffer))
			tc.check(t, o, err)
		})
	}
}

func wantUsageError(substr string) func(*testing.T, options, error) {
	return func(t *testing.T, _ options, err error) {
		t.Helper()
		var u errUsage
		if !errors.As(err, &u) {
			t.Fatalf("want a usage error, got %v", err)
		}
		if substr != "" && !strings.Contains(err.Error(), substr) {
			t.Fatalf("want an error containing %q, got %v", substr, err)
		}
	}
}

func TestVersionPrintsTheProtocol(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), []string{"--version"}, &stdout, &stderr, noEnv); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "plugin protocol") {
		t.Fatalf("want the protocol version reported, got %q", stdout.String())
	}
}

// A repository with no configuration at all still gets the built-in analyzers,
// which is the difference between a tool that needs setting up and one that is
// useful the moment it is installed.
func TestReviewWithNoConfigurationStillRunsTheBuiltins(t *testing.T) {
	if _, err := exec.LookPath("gitleaks"); err != nil {
		t.Skip("gitleaks is not installed; the built-in analyzer reports itself unavailable")
	}
	repo := testfixture.Build(t, "tiny-ts-repo")

	var stdout, stderr bytes.Buffer
	err := run(context.Background(),
		[]string{"--root", repo.Root, "--base", repo.Base, "--reporter", "text"},
		&stdout, &stderr, noEnv)
	if err != nil {
		t.Fatalf("a repository with no configuration must review cleanly, got %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "secrets/") {
		t.Fatalf("want the fixture credential found with no configuration at all, got %q", out)
	}
	if strings.Contains(out, "no analyzers are configured") {
		t.Fatalf("the built-in plugin should have registered analyzers, got %q", out)
	}
}

// With gitleaks absent the analyzer must report itself unavailable rather than
// failing the run, and the reason must reach the report.
func TestAMissingScannerIsReportedNotFatal(t *testing.T) {
	repo := testfixture.Build(t, "tiny-ts-repo")
	writeConfig(t, repo.Root, "tools:\n  gitleaks:\n    path: definitely-not-installed-xyz\n")

	var stdout, stderr bytes.Buffer
	if err := run(context.Background(),
		[]string{"--root", repo.Root, "--base", repo.Base}, &stdout, &stderr, noEnv); err != nil {
		t.Fatalf("a missing scanner must not fail the review, got %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "gitleaks") || !strings.Contains(out, "not installed") {
		t.Fatalf("want the missing scanner explained, got %q", out)
	}
}

// End to end through a real plugin process: the shell example reports a finding
// on README.md, which the diff filter then drops because the fixture's change
// does not touch that file. Both halves matter.
func TestReviewEndToEndThroughAPlugin(t *testing.T) {
	repo := testfixture.Build(t, "tiny-ts-repo")
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "..", "..", "examples", "plugins", "shell-hello", "plugin.sh")

	writeConfig(t, repo.Root, "plugins:\n  - id: shell-hello\n    command: "+script+"\n")

	var stdout, stderr bytes.Buffer
	if err := run(context.Background(),
		[]string{"--root", repo.Root, "--base", repo.Base, "--reporter", "text"},
		&stdout, &stderr, noEnv); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	if strings.Contains(out, "no analyzers are configured") {
		t.Fatalf("the plugin should have registered an analyzer:\n%s\nstderr:\n%s", out, stderr.String())
	}
	// README.md is unchanged in this fixture, so the finding is correctly dropped.
	if !strings.Contains(out, "outside the diff") {
		t.Fatalf("want the out-of-diff finding accounted for, got:\n%s", out)
	}
	if !strings.Contains(stderr.String(), "shell-hello: started") {
		t.Fatalf("want the plugin's diagnostics in the run log, got %q", stderr.String())
	}
}

func TestReviewReportsAMissingPluginWithoutFailing(t *testing.T) {
	repo := testfixture.Build(t, "tiny-ts-repo")
	writeConfig(t, repo.Root, "plugins:\n  - id: ghost\n    command: definitely-not-installed-xyz\n")

	var stdout, stderr bytes.Buffer
	if err := run(context.Background(),
		[]string{"--root", repo.Root, "--base", repo.Base}, &stdout, &stderr, noEnv); err != nil {
		t.Fatalf("a missing plugin must not fail the review, got %v", err)
	}
	if !strings.Contains(stdout.String(), "warning: plugin ghost") {
		t.Fatalf("want the plugin failure surfaced, got %q", stdout.String())
	}
}

func TestRDJSONReporterProducesParseableOutput(t *testing.T) {
	repo := testfixture.Build(t, "tiny-ts-repo")

	var stdout, stderr bytes.Buffer
	if err := run(context.Background(),
		[]string{"--root", repo.Root, "--base", repo.Base, "--reporter", "rdjson"},
		&stdout, &stderr, noEnv); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("rdjson output is not JSON: %v\n%s", err, stdout.String())
	}
	if doc["source"] == nil {
		t.Fatalf("want a source block, got %v", doc)
	}
}

func TestUnknownReporterIsAUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"--reporter", "yaml"}, &stdout, &stderr, noEnv)
	var u errUsage
	if !errors.As(err, &u) {
		t.Fatalf("want a usage error, got %v", err)
	}
}

// --pr is parsed but not yet wired to the GitHub client. It must say so rather
// than silently reviewing the working tree instead.
func TestPRFlagSaysItIsNotWiredUpYet(t *testing.T) {
	repo := testfixture.Build(t, "tiny-ts-repo")
	var stdout, stderr bytes.Buffer
	if err := run(context.Background(),
		[]string{"--root", repo.Root, "--pr", "7"}, &stdout, &stderr, noEnv); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "not wired up yet") {
		t.Fatalf("want an explicit not-yet message, got %q", stdout.String())
	}
}

func writeConfig(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".review")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// analyzers.skip in the config file must actually take effect. Decoding and
// validating a setting that is then ignored is worse than not supporting it.
func TestConfigSkipIsApplied(t *testing.T) {
	repo := testfixture.Build(t, "tiny-ts-repo")
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "..", "..", "examples", "plugins", "shell-hello", "plugin.sh")
	writeConfig(t, repo.Root,
		"plugins:\n  - id: shell-hello\n    command: "+script+"\nanalyzers:\n  skip: [shell-hello, gitleaks]\n")

	var stdout, stderr bytes.Buffer
	if err := run(context.Background(),
		[]string{"--root", repo.Root, "--base", repo.Base}, &stdout, &stderr, noEnv); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	// Both analyzers skipped, so nothing ran and the report says so.
	if !strings.Contains(out, "no analyzers are configured") {
		t.Fatalf("want the configured skip to take effect, got:\n%s", out)
	}
	if strings.Contains(out, "outside the diff") || strings.Contains(out, "secrets/") {
		t.Fatalf("a skipped analyzer still produced findings:\n%s", out)
	}
}

// A flag replaces the file's setting rather than combining with it.
func TestFlagOverridesConfigSkip(t *testing.T) {
	repo := testfixture.Build(t, "tiny-ts-repo")
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "..", "..", "examples", "plugins", "shell-hello", "plugin.sh")
	writeConfig(t, repo.Root,
		"plugins:\n  - id: shell-hello\n    command: "+script+"\nanalyzers:\n  skip: [shell-hello]\n")

	var stdout, stderr bytes.Buffer
	if err := run(context.Background(),
		[]string{"--root", repo.Root, "--base", repo.Base, "--only", "shell-hello"},
		&stdout, &stderr, noEnv); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "no analyzers are configured") {
		t.Fatalf("--only must override the file's skip, got:\n%s", stdout.String())
	}
}
