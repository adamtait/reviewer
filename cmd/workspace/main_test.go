// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testCookieKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" // 32 zero bytes

func noEnv(string) string { return "" }

func TestVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), []string{"version"}, &stdout, &stderr, noEnv); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout.String()) != version {
		t.Errorf("printed %q, want %q", stdout.String(), version)
	}
}

func TestAMistypedCommandLineIsAUsageError(t *testing.T) {
	for _, args := range [][]string{{}, {"frobnicate"}, {"serve"}, {"serve", "--no-such-flag"}} {
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), args, &stdout, &stderr, noEnv)
		var usageErr errUsage
		if !errors.As(err, &usageErr) {
			t.Errorf("args %v: want a usage error, got %v", args, err)
		}
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A service that starts with half a configuration is worse than one that does
// not start. And the operator is told everything that is wrong, at once.
func TestServeRefusesABadConfigurationAndSaysEverythingWrong(t *testing.T) {
	path := writeConfig(t, "listen: \":0\"\nrepositories:\n  - owner: o\n    name: r\n")
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"serve", "--config", path}, &stdout, &stderr, noEnv)
	if err == nil {
		t.Fatal("want serve to refuse a configuration with no limits and no cookie key")
	}
	for _, want := range []string{"publicUrl", "limits.cacheBytes", "sessionDays", "WORKSPACE_COOKIE_KEY is not set"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should mention %q:\n%v", want, err)
		}
	}
	var usageErr errUsage
	if errors.As(err, &usageErr) {
		t.Error("a bad configuration is a failure to start, not a usage error")
	}
}

const validConfig = `
listen: "127.0.0.1:0"
publicUrl: "http://localhost:8080"
github:
  apiBaseUrl: "http://127.0.0.1:1"
  webBaseUrl: "http://127.0.0.1:1"
indexWorkflowFile: "review-index.yml"
indexArtifactName: "review-index"
sessionDays: 30
accessCacheSeconds: 300
limits:
  archiveBytes: 1000
  fileBytes: 100
  indexBytes: 1000
  cacheBytes: 10000
  fetchSeconds: 5
  modelCalls: 2
  questionTokens: 1000
  answerTokens: 100
  callSeconds: 5
  waitSeconds: 1
repositories:
  - owner: example-owner
    name: example-repo
    provider: gemini
    model: a-model
    baseUrl: "http://127.0.0.1:1"
    apiKeyEnv: TEST_MODEL_KEY
`

// The whole lifecycle: start, answer the health address, stop when told, and
// never write a secret to the log on the way.
func TestServeAnswersHealthAndStopsWhenTold(t *testing.T) {
	const providerKey = "a-provider-value-that-must-stay-out-of-the-log"
	getenv := func(name string) string {
		switch name {
		case "WORKSPACE_COOKIE_KEY":
			return testCookieKey
		case "TEST_MODEL_KEY":
			return providerKey
		}
		return ""
	}

	listening := make(chan string, 1)
	onListen = func(addr string) { listening <- addr }
	t.Cleanup(func() { onListen = nil })

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	var stdout, stderr bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"serve", "--config", writeConfig(t, validConfig)}, &stdout, &stderr, getenv)
	}()

	var addr string
	select {
	case addr = <-listening:
	case err := <-done:
		t.Fatalf("serve returned before listening: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not start listening")
	}

	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health status = %d, want 200", resp.StatusCode)
	}

	stop()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("want a clean stop, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop when its context was cancelled")
	}

	log := stdout.String()
	if strings.Contains(log, providerKey) || strings.Contains(log, testCookieKey) {
		t.Errorf("a secret reached the log:\n%s", log)
	}
	for _, want := range []string{`"msg":"listening"`, `"route":"health"`, `"msg":"stopped"`} {
		if !strings.Contains(log, want) {
			t.Errorf("the log should contain %s:\n%s", want, log)
		}
	}
}
