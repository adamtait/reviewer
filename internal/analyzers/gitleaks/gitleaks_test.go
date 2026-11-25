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

// `gitleaks dir` takes at most one positional path and silently ignores the
// rest, falling back to scanning the working directory. Passing the whole
// changed-file list therefore scanned the entire repository — surfacing
// pre-existing credentials that would close the secrets gate permanently on any
// repository with one committed in an old file. The stub records its arguments so
// the one-path-per-invocation contract is asserted rather than assumed.
func TestEachFileIsScannedSeparately(t *testing.T) {
	log := filepath.Join(t.TempDir(), "args.log")
	// The scanned path is the last argument; everything before it is a flag or a
	// flag's value.
	script := `for a in "$@"; do target="$a"; done
while [ $# -gt 0 ]; do
  case "$1" in --report-path) shift; out="$1";; esac
  shift
done
echo "$target" >> "` + log + `"
echo "[]" > "$out"
exit 0
`
	req := req(t, "a.ts", "b.ts", "c.ts")
	if _, _, err := Analyze(context.Background(), req, stubBinary(t, script)); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the scanner was never invoked: %v", err)
	}
	lines := strings.Fields(string(body))
	if len(lines) != 3 {
		t.Fatalf("want one invocation per changed file, got %d targets: %v", len(lines), lines)
	}
	for _, want := range []string{"a.ts", "b.ts", "c.ts"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("want %s scanned, got %q", want, body)
		}
	}
}

// One unscannable file means the diff was not fully checked, so the gate must
// fail closed rather than report the files that did scan as clean.
func TestOneUnscannableFileFailsTheWholeScan(t *testing.T) {
	script := `for a in "$@"; do target="$a"; done
while [ $# -gt 0 ]; do
  case "$1" in --report-path) shift; out="$1";; esac
  shift
done
case "$target" in *poison.ts*) echo "cannot read" >&2; exit 2;; esac
echo "[]" > "$out"
exit 0
`
	_, _, err := Analyze(context.Background(), req(t, "fine.ts", "poison.ts"), stubBinary(t, script))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable so the gate fails closed, got %v", err)
	}
}

// gitleaks logs an INF line to stderr on every successful scan. Turning that into
// a warning made every clean review look degraded.
func TestACleanScanProducesNoWarnings(t *testing.T) {
	script := `while [ $# -gt 0 ]; do
  case "$1" in --report-path) shift; out="$1";; esac
  shift
done
echo "INF scanned ~1019 bytes in 23ms" >&2
echo "[]" > "$out"
exit 0
`
	findings, warnings, err := Analyze(context.Background(), req(t, "a.ts"), stubBinary(t, script))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 || len(warnings) != 0 {
		t.Fatalf("a clean scan must be silent, got %+v / %v", findings, warnings)
	}
}

// The run's deadline has to reach the scanner's own process, because a built-in
// analyzer shares the host's address space and nothing else can stop it.
func TestTheContextStopsTheScanner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := Analyze(ctx, req(t, "a.ts"), stubBinary(t, "sleep 30\n"))
	if err == nil {
		t.Fatal("want a cancelled scan to return an error")
	}
}

// recording is a stub scanner that writes down how it was called: its
// arguments, its working directory and its environment.
const recording = `
log="$RECORD_TO"
[ -n "$log" ] || log="$(dirname "$0")/called"
{ pwd; printf '%s\n' "$#"; for a in "$@"; do printf '%s\n' "$a"; done; echo "--env"; env | sort; } > "$log"
while [ $# -gt 0 ]; do
  case "$1" in --report-path) shift; out="$1";; esac
  shift
done
echo '[]' > "$out"
exit 0
`

func called(t *testing.T, binary string) (dir string, args []string, env []string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(binary), "called"))
	if err != nil {
		t.Fatalf("the scanner was not run: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	dir = lines[0]
	at := 2
	for ; at < len(lines) && lines[at] != "--env"; at++ {
		args = append(args, lines[at])
	}
	if at+1 < len(lines) {
		env = lines[at+1:]
	}
	return dir, args, env
}

// The analyzer's command is the same command it has always run: the repository
// root as working directory, the relative file as target, and nothing added.
// Scan was exported for a second caller, and that must not have changed this one.
func TestTheAnalyzersCommandIsUnchanged(t *testing.T) {
	binary := stubBinary(t, recording)
	r := req(t, "config.ts")
	if _, _, err := Analyze(context.Background(), r, binary); err != nil {
		t.Fatal(err)
	}
	dir, args, env := called(t, binary)

	wantDir, _ := filepath.EvalSymlinks(r.Root)
	gotDir, _ := filepath.EvalSymlinks(dir)
	if gotDir != wantDir {
		t.Errorf("working directory %q, want the repository root %q", gotDir, wantDir)
	}
	if len(args) != 9 {
		t.Fatalf("arguments = %q", args)
	}
	reportPath := args[5]
	want := []string{"dir", "--no-banner", "--report-format", "json", "--report-path", reportPath, "--exit-code", "1", "config.ts"}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("argument %d = %q, want %q (all: %q)", i, args[i], want[i], args)
		}
	}
	// It inherits the environment, as before.
	if len(env) == 0 {
		t.Error("the analyzer's scan ran with an empty environment; it has always inherited its caller's")
	}
}

func TestScanPassesEachOptionToTheScanner(t *testing.T) {
	binary := stubBinary(t, recording)
	work := t.TempDir()
	t.Setenv("GITLEAKS_CONFIG", "/somewhere/else.toml")
	reports, err := Scan(context.Background(), binary, Options{
		Dir: work, Target: "/scan/here", Config: "/rules/rules.toml",
		IgnoreAllowComments: true, MaxDecodeDepth: 2, EmptyEnvironment: true,
	})
	if err != nil || len(reports) != 0 {
		t.Fatalf("reports %v, err %v", reports, err)
	}
	dir, args, env := called(t, binary)
	wantDir, _ := filepath.EvalSymlinks(work)
	gotDir, _ := filepath.EvalSymlinks(dir)
	if gotDir != wantDir {
		t.Errorf("working directory %q, want %q", gotDir, wantDir)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--config /rules/rules.toml", "--ignore-gitleaks-allow", "--max-decode-depth 2"} {
		if !strings.Contains(joined, want) {
			t.Errorf("arguments %q lack %q", joined, want)
		}
	}
	if args[len(args)-1] != "/scan/here" {
		t.Errorf("the target is %q, want it last", args[len(args)-1])
	}
	// Nothing of the caller's environment reaches the scanner. A shell sets a
	// few variables of its own, such as PWD; what matters is that none of ours
	// is there, least of all one that configures the scanner.
	for _, line := range env {
		if strings.HasPrefix(line, "GITLEAKS_") || strings.HasPrefix(line, "HOME=") || strings.HasPrefix(line, "PATH=/") && strings.Contains(line, "go") {
			t.Errorf("the scanner's environment holds %q", line)
		}
	}
}

func TestScanFailsClosedLikeTheAnalyzer(t *testing.T) {
	if _, err := Scan(context.Background(), stubBinary(t, "exit 0\n"), Options{Dir: t.TempDir(), Target: "."}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("a scanner that wrote no report: %v, want ErrUnavailable", err)
	}
	if _, err := Scan(context.Background(), filepath.Join(t.TempDir(), "absent"), Options{Dir: t.TempDir(), Target: "."}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("a scanner that is not there: %v, want ErrUnavailable", err)
	}
}
