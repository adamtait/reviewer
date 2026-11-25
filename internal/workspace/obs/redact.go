// SPDX-License-Identifier: MIT

package obs

import (
	"regexp"
	"strings"
)

// githubToken matches every credential shape GitHub issues.
//
// Fine-grained tokens are the reason for the underscore in the character class:
// `github_pat_` is followed by two runs of letters and digits joined by an
// underscore, so a class without it would redact up to that underscore and leave
// the rest of the token in the log.
var githubToken = regexp.MustCompile(`\b(?:github_pat_[A-Za-z0-9_]{20,}|gh[pousr]_[A-Za-z0-9_]{20,})`)

// Redactor removes credentials from text that is about to be logged.
//
// It knows two things: the shapes of GitHub's tokens, which arrive from users
// and so cannot be listed in advance, and the exact values of the secrets the
// service was started with, which can.
type Redactor struct {
	exact []string
}

// NewRedactor returns a Redactor that also removes each of the given values.
func NewRedactor(secrets ...string) *Redactor {
	r := &Redactor{}
	for _, s := range secrets {
		// Too short to be a credential, and too likely to be a common substring:
		// replacing it would mangle unrelated text.
		if len(s) >= 8 {
			r.exact = append(r.exact, s)
		}
	}
	return r
}

// Redact returns s with every credential it recognises replaced.
func (r *Redactor) Redact(s string) string {
	if r != nil {
		for _, secret := range r.exact {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	return githubToken.ReplaceAllString(s, "[redacted]")
}
