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

	"github.com/adamtait/reviewer/internal/config"
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

// --pr needs GitHub configured. Without it the run must say which piece is
// missing rather than silently reviewing the working tree instead.
func TestPRFlagNeedsGitHubConfigured(t *testing.T) {
	repo := testfixture.Build(t, "tiny-ts-repo")
	var stdout, stderr bytes.Buffer
	if err := run(context.Background(),
		[]string{"--root", repo.Root, "--pr", "7"}, &stdout, &stderr, noEnv); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "GitHub API base URL") {
		t.Fatalf("want the missing configuration named, got %q", stdout.String())
	}
}

// watch sets --pr per pull request, so --staged alongside it would post the local
// index's findings onto every open pull request.
func TestWatchRefusesStaged(t *testing.T) {
	_, err := parse([]string{"watch", "--staged"}, new(bytes.Buffer))
	var usage errUsage
	if !errors.As(err, &usage) {
		t.Fatalf("want a usage error, got %v", err)
	}
	if !strings.Contains(err.Error(), "index") {
		t.Fatalf("want the conflict explained, got %v", err)
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
	// Both analyzers skipped, so nothing ran and the report names the skip as the
	// reason. "No analyzers are configured" would be the wrong explanation here:
	// one is, and it was filtered out.
	if !strings.Contains(out, "removed by only/skip") {
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
	if strings.Contains(stdout.String(), "no analyzers") {
		t.Fatalf("--only must override the file's skip, got:\n%s", stdout.String())
	}
}

// PR-11's proof, and the most important test in the project: an analyzer in the
// model lane must receive no analyze frame at all when the diff contains a
// credential. The spy records every invocation to a file, so the assertion is
// about what actually crossed the process boundary rather than about a flag.
func TestTheModelLaneIsNotInvokedWhenTheDiffCarriesACredential(t *testing.T) {
	if _, err := exec.LookPath("gitleaks"); err != nil {
		t.Skip("gitleaks is not installed; the gate would fail closed for a different reason")
	}

	spyPlugin := func(t *testing.T, log string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "spy.sh")
		body := `#!/bin/sh
while IFS= read -r frame; do
  case "$frame" in
    *'"type":"hello"'*)    printf '{"type":"hello","protocol":1,"plugin":"spy","version":"1.0.0"}\n' ;;
    *'"type":"describe"'*) printf '{"type":"describe","analyzers":[{"id":"spy-llm","lane":"llm","order":500,"available":true}]}\n' ;;
    *'"type":"analyze"'*)
      echo "invoked" >> "` + log + `"
      printf '{"type":"findings","analyzer":"spy-llm","findings":[]}\n' ;;
    *'"type":"bye"'*)      exit 0 ;;
  esac
done
`
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}

	invocations := func(log string) int {
		body, err := os.ReadFile(log)
		if err != nil {
			return 0
		}
		return strings.Count(string(body), "invoked")
	}

	t.Run("credential present: the model lane never runs", func(t *testing.T) {
		repo := testfixture.Build(t, "tiny-ts-repo")
		log := filepath.Join(t.TempDir(), "spy.log")
		writeConfig(t, repo.Root, "plugins:\n  - id: spy\n    command: "+spyPlugin(t, log)+"\n")

		var stdout, stderr bytes.Buffer
		if err := run(context.Background(),
			[]string{"--root", repo.Root, "--base", repo.Base}, &stdout, &stderr, noEnv); err != nil {
			t.Fatal(err)
		}
		if n := invocations(log); n != 0 {
			t.Fatalf("the model lane was invoked %d times with a credential in the diff", n)
		}
		out := stdout.String()
		if !strings.Contains(out, "secrets/") {
			t.Fatalf("want the credential reported, got:\n%s", out)
		}
		// The developer has to be told what happened to their diff.
		if !strings.Contains(out, "not sent anywhere") {
			t.Fatalf("want the gate's reason in the report, got:\n%s", out)
		}
	})

	t.Run("credential removed: the model lane runs", func(t *testing.T) {
		repo := testfixture.Build(t, "tiny-ts-repo")
		if err := os.Remove(filepath.Join(repo.Root, "src", "config.ts")); err != nil {
			t.Fatal(err)
		}
		commit(t, repo.Root, "remove the credential")

		log := filepath.Join(t.TempDir(), "spy.log")
		writeConfig(t, repo.Root, "plugins:\n  - id: spy\n    command: "+spyPlugin(t, log)+"\n")

		var stdout, stderr bytes.Buffer
		if err := run(context.Background(),
			[]string{"--root", repo.Root, "--base", repo.Base}, &stdout, &stderr, noEnv); err != nil {
			t.Fatal(err)
		}
		if n := invocations(log); n != 1 {
			t.Fatalf("want the model lane invoked once on a clean diff, got %d\n%s\n%s",
				n, stdout.String(), stderr.String())
		}
	})
}

// With no secrets scanner available the gate must fail closed: an unchecked diff
// is not sent to a third party just because nothing checked it.
func TestTheModelLaneIsNotInvokedWhenTheScanCouldNotRun(t *testing.T) {
	repo := testfixture.Build(t, "tiny-ts-repo")
	log := filepath.Join(t.TempDir(), "spy.log")
	spy := filepath.Join(t.TempDir(), "spy.sh")
	body := `#!/bin/sh
while IFS= read -r frame; do
  case "$frame" in
    *'"type":"hello"'*)    printf '{"type":"hello","protocol":1,"plugin":"spy","version":"1.0.0"}\n' ;;
    *'"type":"describe"'*) printf '{"type":"describe","analyzers":[{"id":"spy-llm","lane":"llm","order":500,"available":true}]}\n' ;;
    *'"type":"analyze"'*)  echo "invoked" >> "` + log + `"; printf '{"type":"findings","analyzer":"spy-llm","findings":[]}\n' ;;
    *'"type":"bye"'*)      exit 0 ;;
  esac
done
`
	if err := os.WriteFile(spy, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, repo.Root,
		"plugins:\n  - id: spy\n    command: "+spy+"\ntools:\n  gitleaks:\n    path: definitely-not-installed-xyz\n")

	var stdout, stderr bytes.Buffer
	if err := run(context.Background(),
		[]string{"--root", repo.Root, "--base", repo.Base}, &stdout, &stderr, noEnv); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(log); err == nil && strings.Contains(string(body), "invoked") {
		t.Fatalf("the model lane ran with no secrets scan performed:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "could not run") {
		t.Fatalf("want the gate to explain that the scan did not happen, got:\n%s", stdout.String())
	}
}

func commit(t *testing.T, root, message string) {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", message}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestGitHubReporterRequiresAPullRequestNumber(t *testing.T) {
	repo := testfixture.Build(t, "tiny-ts-repo")
	var stdout, stderr bytes.Buffer
	err := run(context.Background(),
		[]string{"--root", repo.Root, "--base", repo.Base, "--reporter", "github"},
		&stdout, &stderr, noEnv)

	var usage errUsage
	if !errors.As(err, &usage) {
		t.Fatalf("want a usage error, got %v", err)
	}
	// Being asked to comment on a pull request and silently not doing it is worse
	// than saying why.
	if !strings.Contains(err.Error(), "--pr") {
		t.Fatalf("want the missing flag named, got %v", err)
	}
}

func TestGitHubReporterRequiresAToken(t *testing.T) {
	repo := testfixture.Build(t, "tiny-ts-repo")
	writeConfig(t, repo.Root, "github:\n  apiBaseUrl: http://127.0.0.1:1\n  repo: o/r\n")

	var stdout, stderr bytes.Buffer
	err := run(context.Background(),
		[]string{"--root", repo.Root, "--base", repo.Base, "--reporter", "github", "--pr", "1"},
		&stdout, &stderr, noEnv)
	if err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("want a clear message about the missing token, got %v", err)
	}
}

func TestParseRepo(t *testing.T) {
	if _, err := parseRepo("owner/name"); err != nil {
		t.Fatalf("owner/name must parse, got %v", err)
	}
	for _, bad := range []string{"", "owner", "/name", "owner/", "  "} {
		if _, err := parseRepo(bad); err == nil {
			t.Fatalf("%q must not parse as a repository", bad)
		}
	}
}

// Go's flag package stops at the first positional argument, so a subcommand has to
// be recognised before parsing or every flag after it is reported as a stray
// argument. `reviewer watch --dry-run` is the case that caught this.
func TestSubcommandsAcceptFlagsAfterThem(t *testing.T) {
	o, err := parse([]string{"watch", "--dry-run", "--root", "/tmp"}, new(bytes.Buffer))
	if err != nil {
		t.Fatalf("flags after a subcommand must parse, got %v", err)
	}
	if o.subcommand != "watch" || !o.dryRun || o.root != "/tmp" {
		t.Fatalf("unexpected options: %+v", o)
	}
}

func TestVersionSubcommandAndFlagAgree(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}} {
		o, err := parse(args, new(bytes.Buffer))
		if err != nil || !o.version {
			t.Fatalf("%v should request the version, got %+v %v", args, o, err)
		}
	}
}

// The installer's exit criterion: a dry run leaves the destination repository
// untouched. Asserted with git status rather than by counting files, because the
// failure this guards against is a writer that lands without a --dry-run check —
// which would show up as a modified tracked file, not only as a new one.
func TestInitDryRunWritesNothing(t *testing.T) {
	root := testfixture.Destination(t, "tiny-monorepo")
	git := testfixture.Git(t, root)

	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), []string{"init", "--root", root, "--dry-run"}, &stdout, &stderr, noEnv); err != nil {
		t.Fatal(err)
	}

	if status := git("status", "--porcelain"); status != "" {
		t.Errorf("--dry-run changed the repository:\n%s", status)
	}
	if !strings.Contains(stdout.String(), "5 files to create, 1 devDependency to add, 0 overwrites") {
		t.Errorf("want the plan summary on stdout, got:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "nothing written") {
		t.Errorf("want the dry run said so on stderr, got:\n%s", stderr.String())
	}
}

func TestInitRejectsForceOnOtherSubcommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"watch", "--force"}, &stdout, &stderr, noEnv)
	var usageErr errUsage
	if !errors.As(err, &usageErr) {
		t.Fatalf("want a usage error, got %v", err)
	}
}

// The whole of M3's claim in one test: a repository this tool has never seen goes
// from clean to reviewing in one command.
func TestInitThenReviewOnAFreshRepository(t *testing.T) {
	root := testfixture.Destination(t, "tiny-monorepo")

	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), []string{"init", "--root", root}, &stdout, &stderr, noEnv); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "5 files created") {
		t.Fatalf("want the file set written, got:\n%s", stdout.String())
	}

	// The review must find the config the installer just wrote, and the config
	// must load. Nothing can run — the plugin is not installed here — but "no
	// analyzers ran" is a different failure from "the config is unreadable", and
	// only the second is this test's concern.
	stdout.Reset()
	if err := run(context.Background(), []string{"--root", root, "--staged"}, &stdout, &stderr, noEnv); err != nil {
		t.Fatalf("reviewing with the generated config: %v", err)
	}
	if strings.Contains(stdout.String(), "no analyzers are configured") {
		t.Errorf("the generated config configures a plugin; got:\n%s", stdout.String())
	}
	// The plugin cannot start here — it is not installed in the fixture — and that
	// has to be reported against the plugin by name. Whether anything else ran
	// depends on which binaries this machine has, so that is not asserted.
	if !strings.Contains(stdout.String(), "plugin typescript") {
		t.Errorf("want the plugin's failure reported by name; got:\n%s", stdout.String())
	}
}

func TestInitProviderFlagWritesTheProviderBlock(t *testing.T) {
	root := testfixture.Destination(t, "tiny-monorepo")

	var stdout, stderr bytes.Buffer
	if err := run(context.Background(),
		[]string{"init", "--root", root, "--provider", "gemini", "--yes"},
		&stdout, &stderr, noEnv); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Resolve(root, "", noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LaneB.Provider != "gemini" || cfg.LaneB.Enabled {
		t.Errorf("want gemini configured and the lane off, got %+v", cfg.LaneB)
	}
	// --yes must have asked nothing.
	if strings.Contains(stdout.String(), "Which model access path") {
		t.Errorf("--yes prompted:\n%s", stdout.String())
	}
}

func TestInitRejectsAnUnknownProvider(t *testing.T) {
	root := testfixture.Destination(t, "tiny-monorepo")
	git := testfixture.Git(t, root)

	var stdout, stderr bytes.Buffer
	err := run(context.Background(),
		[]string{"init", "--root", root, "--provider", "nope", "--yes"},
		&stdout, &stderr, noEnv)
	var usageErr errUsage
	if !errors.As(err, &usageErr) {
		t.Fatalf("want a usage error, got %v", err)
	}
	// Refused before anything was written: an install that half-happened and then
	// complained would be worse than one that did not start.
	if status := git("status", "--porcelain"); status != "" {
		t.Errorf("a refused provider still wrote files:\n%s", status)
	}
}
