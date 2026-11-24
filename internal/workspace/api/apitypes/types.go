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

	// The three ways a sign-in is refused. Each has a different fix, so each
	// has its own code and the sign-in page explains each in its own words.
	CodeNotFineGrained = "not_fine_grained"
	CodeRejected       = "rejected"
	CodeNoRepository   = "no_repository"
)

// TokenRequest is the body of a sign-in.
type TokenRequest struct {
	Token string `json:"token"`
}

// RepositoryRef names a repository.
type RepositoryRef struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

// Me is the signed-in user and the connected repositories they can read. A
// repository the user cannot read is absent, not marked: the list must not
// reveal what is connected.
type Me struct {
	Login        string          `json:"login"`
	Repositories []RepositoryRef `json:"repositories"`
}

// Health is the body of the health address.
type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// PullSummary is one row of the list of open pull requests.
type PullSummary struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Author string `json:"author"`
	Draft  bool   `json:"draft"`
	// HeadSHA is the full hash of the commit the pull request proposes.
	HeadSHA string `json:"headSha"`
	// UpdatedAt is RFC 3339, in UTC.
	UpdatedAt string `json:"updatedAt"`
}

// PullList is the open pull requests of one repository, most recently updated
// first. Truncated says the repository has more than were listed.
type PullList struct {
	Pulls     []PullSummary `json:"pulls"`
	Truncated bool          `json:"truncated"`
}

// The ways a pull request changes a file.
const (
	StatusAdded    = "added"
	StatusModified = "modified"
	StatusDeleted  = "deleted"
	StatusRenamed  = "renamed"
)

// ChangedFile is one file a pull request changes.
type ChangedFile struct {
	Path string `json:"path"`
	// PreviousPath is set for a renamed file: where its old version is read
	// from, at the merge base. Empty otherwise.
	PreviousPath string `json:"previousPath"`
	Status       string `json:"status"`
}

// PullView is what the top of a pull request shows, and what the diff is drawn
// between: the file texts at MergeBaseSHA and at HeadSHA.
type PullView struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	// Body is the description as its author wrote it. It is shown as plain
	// text and never rendered.
	Body   string `json:"body"`
	Author string `json:"author"`
	// State is "open" or "closed". A merged pull request is closed.
	State        string        `json:"state"`
	Draft        bool          `json:"draft"`
	HeadSHA      string        `json:"headSha"`
	BaseRef      string        `json:"baseRef"`
	MergeBaseSHA string        `json:"mergeBaseSha"`
	Files        []ChangedFile `json:"files"`
	// FilesTruncated says the pull request changes more files than are listed.
	FilesTruncated bool `json:"filesTruncated"`
	// WebURL is the pull request on GitHub.
	WebURL string `json:"webUrl"`
}

// The kinds of entry in a commit.
const (
	KindText     = "text"
	KindBinary   = "binary"
	KindTooLarge = "too_large"
	KindOther    = "other"
)

// TreeEntry is one path in a commit.
type TreeEntry struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

// Tree is every path in a commit, sorted.
type Tree struct {
	Entries []TreeEntry `json:"entries"`
}

// File is one file at a commit. Text is empty unless Kind is "text".
type File struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// Match is one line that contains a search's text. Line counts from 0.
type Match struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Search is the body of a search request. It travels in the body and not the
// address so that what a user searched for is not in the platform's request log.
type Search struct {
	// Query is matched literally. At least 2 characters.
	Query         string `json:"query"`
	PathPrefix    string `json:"pathPrefix"`
	CaseSensitive bool   `json:"caseSensitive"`
}

// SearchResult is the matching lines, in path order. Truncated says the search
// stopped at its limit with more to find.
type SearchResult struct {
	Matches   []Match `json:"matches"`
	Truncated bool    `json:"truncated"`
}
