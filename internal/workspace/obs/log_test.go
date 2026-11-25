// SPDX-License-Identifier: MIT

package obs

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// credential builds a token-shaped string at run time. Written out as a literal
// it would be refused by this repository's own secrets scan — which is the scan
// working, and not a reason to add an exemption for test files.
func credential(prefix string) string {
	return prefix + "_" + strings.Repeat("R2xKq7mN", 5)[:36]
}

// fineGrained builds the shape of a fine-grained personal access token: two
// runs of letters and digits joined by an underscore.
func fineGrained() string {
	return "github" + "_pat_" + strings.Repeat("A1b2C3d4", 3)[:22] + "_" + strings.Repeat("e5F6g7H8", 8)[:59]
}

func TestRedactRemovesEveryGitHubTokenShape(t *testing.T) {
	const providerKey = "a-provider-value-0123456789"
	r := NewRedactor(providerKey)
	for _, token := range []string{
		// A fine-grained token has an underscore inside it. A pattern that stops
		// at that underscore leaves the second half in the log.
		fineGrained(),
		credential("ghp"), credential("gho"), credential("ghu"), credential("ghs"), credential("ghr"),
		providerKey,
	} {
		got := r.Redact("GET failed with " + token + " in the body")
		if got != "GET failed with [redacted] in the body" {
			t.Errorf("want %q… replaced whole and nothing else touched, got %q", token[:10], got)
		}
	}
	// Ordinary text is left alone, and a nil Redactor still knows the shapes.
	if got := r.Redact("the ghost of a path/like_this"); got != "the ghost of a path/like_this" {
		t.Errorf("ordinary text was changed: %q", got)
	}
	var none *Redactor
	if got := none.Redact(credential("ghp")); got != "[redacted]" {
		t.Errorf("a nil Redactor should still remove token shapes, got %q", got)
	}
}

func decode(t *testing.T, line string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("log line is not JSON: %v: %s", err, line)
	}
	return m
}

func TestARequestWritesOneLineWithItsFields(t *testing.T) {
	var out bytes.Buffer
	log := New(&out, NewRedactor())

	ctx, fields := WithFields(context.Background())
	fields.SetUser("octocat")
	fields.SetRepo("example-owner", "example-repo")
	fields.SetPull(12)
	fields.SetSHA("0123456789abcdef0123456789abcdef01234567")
	fields.SetLabel("index")
	log.Request(ctx, "abcd", "definition", "GET", 200, 42*time.Millisecond)

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("want exactly one line, got %d", len(lines))
	}
	got := decode(t, lines[0])
	for key, want := range map[string]any{
		"msg": "request", "request_id": "abcd", "route": "definition", "method": "GET",
		"status": float64(200), "duration_ms": float64(42), "user": "octocat",
		"owner": "example-owner", "repo": "example-repo", "pull": float64(12), "label": "index",
	} {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
	// A field nobody set is left out, not written empty: the feedback queries
	// count lines that have a label.
	if _, present := got["outcome"]; present {
		t.Error("an unset field should be absent from the line")
	}
}

// The guarantee is the logger's, not its callers': whatever string reaches it,
// in any attribute, is redacted.
func TestEveryStringTheLoggerWritesIsRedacted(t *testing.T) {
	token := fineGrained()
	const key = "a-provider-value-0123456789"
	var out bytes.Buffer
	log := New(&out, NewRedactor(key))

	ctx, fields := WithFields(context.Background())
	fields.SetOutcome("failed: " + token)
	log.Request(ctx, "abcd", "route with "+key, "GET", 500, time.Millisecond)
	log.Event(ctx, slog.LevelError, "panic: "+token, slog.String("detail", "key "+key))

	if strings.Contains(out.String(), token) || strings.Contains(out.String(), key) {
		t.Errorf("a credential reached the log:\n%s", out.String())
	}
}

// Outside a request, From hands back a scratch value so handlers under test can
// call the setters freely.
func TestFromWithoutARequest(t *testing.T) {
	From(context.Background()).SetUser("nobody")
}
