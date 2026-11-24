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

	// The ways a sign-in is refused. Each has a different fix, so each
	// has its own code and the sign-in page explains each in its own words.
	CodeNotFineGrained = "not_fine_grained"
	CodeRejected       = "rejected"
	CodeNoRepository   = "no_repository"
	// CodeNotListed: the workspace admits listed accounts only.
	CodeNotListed = "not_listed"
)

// TokenRequest is the body of a sign-in.
type TokenRequest struct {
	Token string `json:"token"`
}

// RepositoryRef names a repository, and says whether questions can be asked
// about it: whether it has a model provider configured.
type RepositoryRef struct {
	Owner     string `json:"owner"`
	Name      string `json:"name"`
	Assistant bool   `json:"assistant"`
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

// The states of a commit's navigation index.
const (
	IndexNone    = "none"
	IndexRunning = "running"
	IndexFailed  = "failed"
	IndexReady   = "ready"
)

// IndexState says whether a commit has a navigation index.
type IndexState struct {
	State string `json:"state"`
	// Reason says why there is no index although the index job succeeded.
	Reason string `json:"reason"`
	// RunURL is the workflow run on GitHub that the state rests on: the one
	// that built the index, is building it, or failed. Empty when there is none.
	RunURL string `json:"runUrl"`
	// ToolVersion and Documents describe a loaded index.
	ToolVersion string `json:"toolVersion"`
	Documents   int    `json:"documents"`
}

// Location is a span in a file at the commit asked about. Lines count from 0,
// the end is exclusive, and characters are UTF-16 code units.
type Location struct {
	Path         string `json:"path"`
	Line         int    `json:"line"`
	Character    int    `json:"character"`
	EndLine      int    `json:"endLine"`
	EndCharacter int    `json:"endCharacter"`
}

// Where a navigation result came from. A label names the source. It does not
// say the result is correct: the index cannot be verified.
const (
	LabelIndex      = "index"
	LabelTextSearch = "text_search"
	LabelExternal   = "external"
	LabelNone       = "none"
)

// Definition answers "where is this defined".
type Definition struct {
	Label string `json:"label"`
	// Targets is filled when Label is "index".
	Targets []Location `json:"targets"`
	// Package and Version are filled when Label is "external".
	Package string `json:"package"`
	Version string `json:"version"`
	// Matches is filled when Label is "text_search".
	Matches []Match `json:"matches"`
	// Truncated says the text search stopped at its limit.
	Truncated bool `json:"truncated"`
}

// References answers "where is this used". The text matches are always given
// beside the index's list, because an index can cover part of a repository.
type References struct {
	Label      string     `json:"label"`
	References []Location `json:"references"`
	// Truncated says the index held more references than are listed.
	Truncated bool    `json:"truncated"`
	Matches   []Match `json:"matches"`
	// MatchesTruncated says the text search stopped at its limit.
	MatchesTruncated bool `json:"matchesTruncated"`
}

// The kinds of lookup a report can be about.
const (
	KindDefinition = "definition"
	KindReferences = "references"
)

// Report is a user saying a navigation result was wrong. It names the lookup
// and carries no text: it is written to the log, and the log holds nothing a
// user typed.
type Report struct {
	SHA       string `json:"sha"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Character int    `json:"character"`
	// Kind is "definition" or "references".
	Kind string `json:"kind"`
	// Label is the label the wrong result carried.
	Label string `json:"label"`
}

// CodeNoAssistant: the repository has no model provider configured.
const CodeNoAssistant = "no_assistant"

// AskMessage is an earlier question or answer. The browser holds the
// conversation and sends it with each question; the service keeps none of it.
type AskMessage struct {
	// Role is "user" or "assistant".
	Role string `json:"role"`
	Text string `json:"text"`
}

// AskSelection is the code the reader selected. StartLine counts from 0 and
// EndLine is exclusive.
type AskSelection struct {
	SHA       string `json:"sha"`
	Path      string `json:"path"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
}

// Ask is one question.
type Ask struct {
	Text string `json:"text"`
	// PullNumber is the pull request the reader has open, or 0.
	PullNumber int           `json:"pullNumber"`
	History    []AskMessage  `json:"history"`
	Selection  *AskSelection `json:"selection"`
}

// The events of an answer's stream. Each is sent as one server-sent event
// whose name is the constant and whose data is the type beside it. Exactly one
// of "done" and "stopped" ends every stream.
const (
	EventDelta    = "delta"
	EventExcerpt  = "excerpt"
	EventWithheld = "withheld"
	EventWaiting  = "waiting"
	EventDone     = "done"
	EventStopped  = "stopped"
)

// Delta is more of the answer.
type Delta struct {
	Text string `json:"text"`
}

// Excerpt is a run of lines the model was given. StartLine counts from 0 and
// EndLine is exclusive.
type Excerpt struct {
	SHA       string `json:"sha"`
	Path      string `json:"path"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
}

// Withheld is content the secrets scan held back from the model.
type Withheld struct {
	What   string `json:"what"`
	Reason string `json:"reason"`
}

// Waiting says the assistant is sitting out a wait the provider named.
type Waiting struct {
	Seconds int    `json:"seconds"`
	Reason  string `json:"reason"`
}

// Done ends an answer that was completed.
type Done struct {
	Model           string `json:"model"`
	InputTokens     int    `json:"inputTokens"`
	OutputTokens    int    `json:"outputTokens"`
	ReasoningTokens int    `json:"reasoningTokens"`
	ModelCalls      int    `json:"modelCalls"`
}

// Stopped ends an answer that was not completed. Outcome is one of
// rate_limited, quota_exhausted, limit_reached, secret_blocked, scan_failed,
// refused and provider_error.
type Stopped struct {
	Outcome string `json:"outcome"`
	Message string `json:"message"`
	// RetryAfterSeconds and ResetAt are set when the provider named a wait.
	RetryAfterSeconds int    `json:"retryAfterSeconds"`
	ResetAt           string `json:"resetAt"`
}
