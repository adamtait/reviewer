// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// blockingReader is a stdin that is open and silent: no data, and no EOF. It is
// what a pipe with no writer looks like, which is what several CI harnesses and
// agent runners attach.
//
// A reader that returns EOF would not reproduce anything — that case was already
// handled. This one is the hang.
type blockingReader struct{ release chan struct{} }

func (b blockingReader) Read([]byte) (int, error) {
	<-b.release
	return 0, io.EOF
}

// TestInitDoesNotBlockOnAStdinNobodyIsTyping is the regression test for a hang.
//
// Before the fix, `init --dry-run` printed the provider question and waited for
// an answer that could not arrive. There was no output explaining it, and it is
// the first command the README tells a stranger to run.
//
// Asserted with a timeout rather than by inspecting output, because a hang is the
// defect: a test that checked only the printed plan would pass while blocking the
// suite forever.
func TestInitDoesNotBlockOnAStdinNobodyIsTyping(t *testing.T) {
	root := repoWithNothingInteresting(t)
	stdin := blockingReader{release: make(chan struct{})}
	// Released at the end whatever happens, so a regression fails this test
	// rather than wedging every test after it.
	defer close(stdin.release)

	done := make(chan error, 1)
	var out, errOut strings.Builder
	go func() {
		done <- initialize(options{subcommand: "init", root: root, dryRun: true}, stdin, &out, &errOut)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("init --dry-run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("init --dry-run blocked reading stdin; nobody is there to answer")
	}

	if strings.Contains(out.String(), "Which model access path") {
		t.Error("a dry run asked a question whose answer it discards")
	}
	if !strings.Contains(errOut.String(), "nothing written") {
		t.Errorf("stderr did not say nothing was written: %q", errOut.String())
	}
}

// TestInitDryRunHonoursAnExplicitProvider keeps the fix from going too far. The
// question is what is conditional, not the choice: --provider must still reach
// the plan, which is the whole point of previewing one.
func TestInitDryRunHonoursAnExplicitProvider(t *testing.T) {
	root := repoWithNothingInteresting(t)
	var out, errOut strings.Builder
	opts := options{subcommand: "init", root: root, dryRun: true, provider: "gemini"}
	if err := initialize(opts, blockingReader{release: make(chan struct{})}, &out, &errOut); err != nil {
		t.Fatalf("init --dry-run --provider gemini: %v", err)
	}
	if !strings.Contains(out.String(), "gemini") {
		t.Errorf("the plan does not mention the requested provider:\n%s", out.String())
	}
}

// TestInitRejectsAnUnknownProviderEvenWhenNotAsking guards the other half: going
// quiet must not also mean going permissive. A misspelled --provider is misuse
// whether or not anyone is at a terminal.
func TestInitRejectsAnUnknownProviderEvenWhenNotAsking(t *testing.T) {
	root := repoWithNothingInteresting(t)
	var out, errOut strings.Builder
	opts := options{subcommand: "init", root: root, dryRun: true, provider: "gemnii"}
	err := initialize(opts, blockingReader{release: make(chan struct{})}, &out, &errOut)
	if err == nil {
		t.Fatal("a misspelled --provider was accepted")
	}
	var usage errUsage
	if !errors.As(err, &usage) {
		t.Errorf("error is %T, want errUsage so the exit status says misuse", err)
	}
}

// TestIsTerminalSaysNoForAPipe covers the predicate directly, including the case
// a file-backed stdin presents: a regular file is not a character device, so a
// `reviewer init < answers.txt` gets no prompt and is told to use --provider.
func TestIsTerminalSaysNoForAPipe(t *testing.T) {
	if isTerminal(strings.NewReader("3\n")) {
		t.Error("a strings.Reader is not a terminal")
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if isTerminal(r) {
		t.Error("a pipe is not a terminal")
	}

	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// /dev/null *is* a character device, which is the one false positive this
	// predicate has. It costs a prompt that immediately reads EOF and returns no
	// provider, which is the same answer the non-interactive path gives.
	_ = isTerminal(f)
}

// repoWithNothingInteresting is a git repository the installer can plan against
// without detecting anything that would change the plan's shape.
func repoWithNothingInteresting(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(root+"/.git", 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}
