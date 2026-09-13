// SPDX-License-Identifier: MIT

package gitleaks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/pkg/plugin"
)

// stubBinary writes a fake gitleaks onto PATH. The analyzer's contract with the
// real tool — report file, exit 1 on findings, nothing useful on stdout — is
// awkward enough that the interesting tests are about honouring it, not about
// gitleaks' own detection.
func stubBinary(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gitleaks")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func req(t *testing.T, files ...string) plugin.AnalyzeRequest {
	t.Helper()
	root := t.TempDir()
	var changed []plugin.ChangedFile
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		changed = append(changed, plugin.ChangedFile{Path: f, Ranges: [][2]int{{1, 1}}})
	}
	return plugin.AnalyzeRequest{Root: root, Changed: changed}
}

// The report is written to the path given in --report-path; this stub emits one
// finding and exits 1, exactly as the real tool does.
const oneFinding = `
while [ $# -gt 0 ]; do
  case "$1" in --report-path) shift; out="$1";; esac
  shift
done
cat > "$out" <<'JSON'
[{"RuleID":"github-pat","Description":"Uncovered a GitHub Personal Access Token.","StartLine":5,"EndLine":5,"File":"src/config.ts","Secret":"ghp_SHOULD_NEVER_APPEAR","Match":"ghp_SHOULD_NEVER_APPEAR"}]
JSON
exit 1
`

func TestAnalyzeMapsTheReport(t *testing.T) {
	findings, warnings, err := Analyze(context.Background(), req(t, "a.ts"), stubBinary(t, oneFinding))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("want no warnings, got %v", warnings)
	}
	if len(findings) != 1 {
		t.Fatalf("want one finding, got %+v", findings)
	}
	f := findings[0]
	// The rule id namespace is ours so acceptance stays stable across gitleaks
	// upgrades.
	if f.RuleID != "secrets/github-pat" {
		t.Fatalf("want a namespaced rule id, got %q", f.RuleID)
	}
	if f.File != "src/config.ts" || f.Line != 5 {
		t.Fatalf("want src/config.ts:5, got %s:%d", f.File, f.Line)
	}
	if f.Severity != "error" || f.Confidence != "high" {
		t.Fatalf("a committed credential is a high-confidence error, got %s/%s", f.Severity, f.Confidence)
	}
	f.Fingerprint = "set-by-the-core"
	if err := f.Validate(); err != nil {
		t.Fatalf("invalid finding: %v", err)
	}
}

// The whole point of the message: it must describe the credential without
// reproducing it. Posting a secret to a pull request publishes it to everyone
// with read access and to every notification email.
func TestTheSecretIsNeverInTheFinding(t *testing.T) {
	findings, _, err := Analyze(context.Background(), req(t, "a.ts"), stubBinary(t, oneFinding))
	if err != nil {
		t.Fatal(err)
	}
	whole := findings[0].Message + findings[0].Suggestion + findings[0].Evidence
	if strings.Contains(whole, "ghp_SHOULD_NEVER_APPEAR") {
		t.Fatalf("the finding carries the credential: %q", whole)
	}
}

// The secrets gate depends on telling "clean" apart from "did not run". These
// four cases are the ones that would otherwise be silently reported as clean.
func TestUnavailableIsDistinctFromClean(t *testing.T) {
	tests := []struct {
		name   string
		binary string
		script string
		want   error
	}{
		{name: "not installed", binary: "definitely-not-installed-xyz", want: ErrUnavailable},
		{name: "writes no report", script: "exit 0\n", want: ErrUnavailable},
		{
			name: "unreadable report",
			script: `while [ $# -gt 0 ]; do case "$1" in --report-path) shift; out="$1";; esac; shift; done
echo "not json" > "$out"
exit 1
`,
			want: ErrUnavailable,
		},
		{
			name: "fails with an unexpected status and no findings",
			script: `while [ $# -gt 0 ]; do case "$1" in --report-path) shift; out="$1";; esac; shift; done
echo "[]" > "$out"
echo "config is invalid" >&2
exit 2
`,
			want: ErrUnavailable,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			binary := tc.binary
			if binary == "" {
				binary = stubBinary(t, tc.script)
			}
			_, _, err := Analyze(context.Background(), req(t, "a.ts"), binary)
			if !errors.Is(err, tc.want) {
				t.Fatalf("want ErrUnavailable so the gate fails closed, got %v", err)
			}
		})
	}
}

// Exit 0 with an empty report is the genuinely clean case and must not be
// confused with a failure.
func TestCleanScanIsNotAnError(t *testing.T) {
	script := `while [ $# -gt 0 ]; do case "$1" in --report-path) shift; out="$1";; esac; shift; done
echo "[]" > "$out"
exit 0
`
	findings, warnings, err := Analyze(context.Background(), req(t, "a.ts"), stubBinary(t, script))
	if err != nil {
		t.Fatalf("a clean scan is not an error, got %v", err)
	}
	if len(findings) != 0 || len(warnings) != 0 {
		t.Fatalf("want nothing reported, got %+v / %v", findings, warnings)
	}
}

func TestNoChangedFilesSkipsTheScanEntirely(t *testing.T) {
	// The stub would fail if invoked, which is how this asserts it was not.
	findings, _, err := Analyze(context.Background(), plugin.AnalyzeRequest{Root: t.TempDir()},
		stubBinary(t, "exit 3\n"))
	if err != nil {
		t.Fatalf("an empty diff is not an error, got %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want no findings, got %+v", findings)
	}
}

func TestProbeDistinguishesAbsentFromBroken(t *testing.T) {
	if _, _, err := Probe("definitely-not-installed-xyz"); err == nil ||
		!strings.Contains(err.Error(), "not installed") {
		t.Fatalf("want an absent-binary message, got %v", err)
	}
	if _, _, err := Probe(stubBinary(t, "exit 1\n")); err == nil ||
		!strings.Contains(err.Error(), "would not run") {
		t.Fatalf("want a broken-binary message, got %v", err)
	}
	if _, version, err := Probe(stubBinary(t, "echo 8.28.0\n")); err != nil || version != "8.28.0" {
		t.Fatalf("want the version reported, got %q %v", version, err)
	}
}
