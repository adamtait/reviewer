// SPDX-License-Identifier: MIT

// Package workspace is the root of the review workspace service: a web
// application used beside GitHub's pull request page, described in
// docs/designs/review-workspace.md.
//
// The service is a second binary, cmd/workspace. Its packages live under this
// directory so that one rule can be stated, and tested, about all of them: none
// of them starts a program (see rules_test.go). The CLI's packages do — that is
// what an analyzer is — and the service must never run code from a pull request.
package workspace
