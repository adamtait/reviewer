// SPDX-License-Identifier: MIT

package gate

import (
	"strings"
	"testing"

	"github.com/adamtait/reviewer/pkg/finding"
)

func f(ruleID, file string) finding.Finding {
	return finding.Finding{RuleID: ruleID, File: file}
}

func TestDecide(t *testing.T) {
	tests := []struct {
		name        string
		findings    []finding.Finding
		scanned     bool
		wantBlocked bool
		wantReason  string
	}{
		{
			name:    "clean scan does not block",
			scanned: true,
		},
		{
			name:        "a credential blocks",
			findings:    []finding.Finding{f("secrets/github-pat", "src/config.ts")},
			scanned:     true,
			wantBlocked: true,
			wantReason:  "src/config.ts",
		},
		{
			// Failing closed is the whole point: an unscanned diff and a dirty
			// diff are indistinguishable from here.
			name:        "an unperformed scan blocks",
			scanned:     false,
			wantBlocked: true,
			wantReason:  "could not run",
		},
		{
			name:        "other findings do not block",
			findings:    []finding.Finding{f("arch/no-domain-to-infra", "a.ts"), f("logic/x", "b.ts")},
			scanned:     true,
			wantBlocked: false,
		},
		{
			// Matched on the rule namespace, not an analyzer id, so a second
			// secrets scanner closes the gate without the gate knowing about it.
			name:        "any secrets/ rule blocks, whatever produced it",
			findings:    []finding.Finding{f("secrets/some-future-scanner-rule", "x.env")},
			scanned:     true,
			wantBlocked: true,
			wantReason:  "x.env",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide(tc.findings, tc.scanned)
			if got.Blocked != tc.wantBlocked {
				t.Fatalf("want blocked=%v, got %v (%s)", tc.wantBlocked, got.Blocked, got.Reason)
			}
			if tc.wantReason != "" && !strings.Contains(got.Reason, tc.wantReason) {
				t.Fatalf("want a reason containing %q, got %q", tc.wantReason, got.Reason)
			}
			if got.Blocked && got.Reason == "" {
				t.Fatal("a blocked gate must say why")
			}
		})
	}
}

// The reason is shown to a developer who has just committed a credential, so it
// has to say what happened to their diff, not only that something was skipped.
func TestTheReasonSaysTheDiffWasNotSent(t *testing.T) {
	for _, s := range []State{
		Decide([]finding.Finding{f("secrets/x", "a.ts")}, true),
		Decide(nil, false),
	} {
		if !strings.Contains(s.Reason, "not sent") && !strings.Contains(s.Reason, "not sent anywhere") {
			t.Fatalf("want the reason to say the diff was not sent, got %q", s.Reason)
		}
	}
}

func TestDescribeFiles(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{[]string{"a.ts"}, "a.ts"},
		{[]string{"a.ts", "b.ts"}, "a.ts and b.ts"},
		{[]string{"a.ts", "b.ts", "c.ts"}, "a.ts and 2 other files"},
		// Duplicates collapse: one file with three credentials is still one file.
		{[]string{"a.ts", "a.ts", "a.ts"}, "a.ts"},
	}
	for _, tc := range tests {
		if got := describeFiles(tc.in); got != tc.want {
			t.Fatalf("describeFiles(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
