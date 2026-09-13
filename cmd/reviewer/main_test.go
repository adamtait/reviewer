// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--version"}, &stdout, &stderr); err != nil {
		t.Fatalf("--version returned an error: %v", err)
	}
	if !strings.HasPrefix(stdout.String(), "reviewer ") {
		t.Fatalf("want a version line on stdout, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("want nothing on stderr, got %q", stderr.String())
	}
}
