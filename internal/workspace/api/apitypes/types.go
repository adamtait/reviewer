// SPDX-License-Identifier: MIT

// Package apitypes defines every body the workspace's HTTP API sends or
// accepts.
//
// The web application's types in web/src/api/types.ts mirror these by hand, the
// way the TypeScript plugin mirrors the plugin protocol. What keeps the two
// honest is the set of example responses under ../testdata/examples: the handler
// tests must produce them, and the web application's tests are served them.
package apitypes

// Error is the body of every response that is not a success.
type Error struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody says what went wrong, for a program (Code) and for a person
// (Message).
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error codes. A client switches on these, never on Message.
const (
	CodeBadRequest    = "bad_request"
	CodeSignedOut     = "signed_out"
	CodeNotFound      = "not_found"
	CodeTooLarge      = "too_large"
	CodeGitHubFailed  = "github_failed"
	CodeGitHubTimeout = "github_timeout"
	CodeInternal      = "internal"
)

// Health is the body of the health address.
type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}
