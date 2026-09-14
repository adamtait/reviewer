// SPDX-License-Identifier: MIT

// Package gate holds the one safety property in this system.
//
// If a developer commits a credential, the worst available response is a
// pipeline that immediately ships that diff to a model provider: it turns a leak
// into a leak plus exfiltration. So a credential in the diff stops the model
// lane outright, and so does a secrets scan that could not run — an unscanned
// diff and a dirty diff are indistinguishable from here (ADR-0012).
package gate

import (
	"fmt"
	"strings"

	"github.com/adamtait/reviewer/pkg/finding"
)

// SecretsRulePrefix is the namespace the secrets analyzer emits under. Findings
// are matched on this rather than on an analyzer id, so a second secrets scanner
// closes the gate too without the gate being taught about it.
const SecretsRulePrefix = "secrets/"

// State is the gate's decision for one run.
type State struct {
	// Blocked is true when the model lane must not run.
	Blocked bool
	// Reason is one line explaining why, for the run log and the report.
	Reason string
}

// Decide inspects the findings so far and whether the scan was actually
// performed.
//
// scanned is false when no secrets analyzer ran at all — absent binary, crash,
// unreadable report. That fails closed: the alternative is sending a diff nobody
// has checked to a third party.
func Decide(findings []finding.Finding, scanned bool) State {
	var files []string
	for _, f := range findings {
		if strings.HasPrefix(f.RuleID, SecretsRulePrefix) {
			files = append(files, f.File)
		}
	}
	if len(files) > 0 {
		return State{
			Blocked: true,
			Reason: fmt.Sprintf(
				"a credential was found in %s, so the model lane did not run — the diff was not sent anywhere",
				describeFiles(files)),
		}
	}
	if !scanned {
		return State{
			Blocked: true,
			Reason:  "the secrets scan could not run, so the model lane did not run — an unchecked diff is not sent to a model",
		}
	}
	return State{}
}

func describeFiles(files []string) string {
	seen := map[string]bool{}
	var unique []string
	for _, f := range files {
		if !seen[f] {
			seen[f] = true
			unique = append(unique, f)
		}
	}
	switch len(unique) {
	case 1:
		return unique[0]
	case 2:
		return unique[0] + " and " + unique[1]
	default:
		return fmt.Sprintf("%s and %d other files", unique[0], len(unique)-1)
	}
}
