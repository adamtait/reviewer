// SPDX-License-Identifier: MIT

// Package ghclient is the GitHub API surface this tool needs, and nothing else.
//
// It is hand-rolled over net/http rather than built on a client library. The
// surface is six calls, the responses are small, and a narrow hand-written client
// makes the token scopes this tool requires readable in one file — which matters
// when the thing being asked for is write access to pull requests. It also keeps
// the dependency inventory at two modules, which is a property worth protecting
// (ADR-0028).
package ghclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// APIVersion pins the REST API's dated contract, so a server-side change cannot
// alter response shapes under us.
const APIVersion = "2022-11-28"

// Repo identifies a repository.
type Repo struct {
	Owner string
	Name  string
}

func (r Repo) String() string { return r.Owner + "/" + r.Name }

// PullRequest is the subset of a pull request this tool reads.
type PullRequest struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Draft  bool   `json:"draft"`
	Head   Ref    `json:"head"`
	Base   Ref    `json:"base"`
	// UpdatedAt drives the poller's watermark.
	UpdatedAt time.Time `json:"updated_at"`
	// Body, User, CreatedAt and HTMLURL are what the review workspace shows at
	// the top of a pull request: its description, who opened it and when, and
	// where it lives on GitHub.
	Body      string    `json:"body"`
	User      User      `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	HTMLURL   string    `json:"html_url"`
	// ChangedFiles is GitHub's count of the files the pull request changes. It
	// is present when one pull request is read and absent from a list. The
	// files call stops at 3,000, so this is how a caller knows it was cut short.
	ChangedFiles int `json:"changed_files"`
	// MergedAt is zero for a pull request that was closed without merging, or
	// is still open. Outcome measurement orders merged pull requests by it.
	MergedAt time.Time `json:"merged_at"`
	// MergeCommitSHA is the commit a merge put on the base branch: the squash
	// commit, the last rebased commit, or the merge commit. Outcome
	// measurement looks for it in a later blame.
	MergeCommitSHA string `json:"merge_commit_sha"`
	// Labels are the pull request's GitHub labels.
	Labels []Label `json:"labels"`
}

// Label is a GitHub label, by name.
type Label struct {
	Name string `json:"name"`
}

// RepoInfo is what must be known about a repository before anything of it is
// shown: that this token can see it at all, and that it is not public.
type RepoInfo struct {
	FullName string `json:"full_name"`
	// Private is false for a public repository, which every GitHub token can
	// read. The workspace refuses to serve one for that reason.
	Private    bool   `json:"private"`
	Visibility string `json:"visibility"`
}

// Ref is one end of a pull request.
type Ref struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
	// Repo is the repository this end lives in, which for the head of a pull
	// request from a fork is the fork. GitHub sends null once a fork is
	// deleted.
	Repo *RefRepo `json:"repo"`
}

// RefRepo is the repository one end of a pull request lives in.
type RefRepo struct {
	FullName string `json:"full_name"`
}

// ReviewComment is an existing inline comment. Only the fields dedupe and
// acceptance measurement need.
type ReviewComment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	Path string `json:"path"`
	// Line is nil when GitHub has marked the comment outdated, which is itself
	// the signal acceptance measurement uses (ADR-0024).
	Line     *int      `json:"line"`
	User     User      `json:"user"`
	NodeID   string    `json:"node_id"`
	CreateAt time.Time `json:"created_at"`
	// PullRequestReviewID is the review the comment was submitted with, which
	// is how outcome measurement finds what one review commented on.
	PullRequestReviewID int64 `json:"pull_request_review_id"`
}

// Outdated reports whether GitHub has detached this comment from the diff, which
// happens when the code it pointed at changed.
func (c ReviewComment) Outdated() bool { return c.Line == nil }

// IssueComment is a comment on the pull request conversation rather than on a line.
type IssueComment struct {
	ID     int64  `json:"id"`
	Body   string `json:"body"`
	User   User   `json:"user"`
	NodeID string `json:"node_id"`
}

// Review is one review of a pull request.
type Review struct {
	ID   int64 `json:"id"`
	User User  `json:"user"`
	// State is APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED or PENDING. A
	// dismissed approval reads DISMISSED, not APPROVED.
	State string `json:"state"`
	// CommitID is the head the reviewer was looking at when they submitted,
	// which is not necessarily the head that merged.
	CommitID string `json:"commit_id"`
	// SubmittedAt is zero for a PENDING review, which nobody else can see yet.
	SubmittedAt time.Time `json:"submitted_at"`
}

// User is a comment's author. Login is what distinguishes this tool's own
// comments from a human's when the token's identity is known.
type User struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

// ChangedFile is a file in a pull request, used when reviewing by number rather
// than from a local checkout.
type ChangedFile struct {
	Filename string `json:"filename"`
	Status   string `json:"status"`
	Patch    string `json:"patch"`
	// PreviousFilename is set for a renamed file: where its old version is to
	// be read from.
	PreviousFilename string `json:"previous_filename"`
	// Deletions is how many lines the change removed. GitHub sends it when it
	// leaves Patch out of a diff too large to include, so it is how a caller
	// tells a removal it cannot see from no removal at all.
	Deletions int `json:"deletions"`
	// Changes is how many lines the change added and removed together. The
	// risk record reads the history of the most-changed files first.
	Changes int `json:"changes"`
}

// Client is the read surface. Writes are added in PR-18 as a separate interface,
// so that a reader of this file can see that reviewing a pull request needs no
// write access at all.
type Client interface {
	// Viewer is the identity the token authenticates as.
	Viewer(ctx context.Context) (User, error)
	GetPullRequest(ctx context.Context, repo Repo, number int) (PullRequest, error)
	ListPullRequests(ctx context.Context, repo Repo, state string) ([]PullRequest, error)
	ListFiles(ctx context.Context, repo Repo, number int) ([]ChangedFile, error)
	ListReviewComments(ctx context.Context, repo Repo, number int) ([]ReviewComment, error)
	ListIssueComments(ctx context.Context, repo Repo, number int) ([]IssueComment, error)
	ListReviews(ctx context.Context, repo Repo, number int) ([]Review, error)
}

// ErrNotFound distinguishes "this pull request does not exist" from "the token
// cannot see it" — which GitHub does not, returning 404 for both.
var ErrNotFound = errors.New("not found, or not visible to this token")

// ErrUnauthorized means the token is missing or rejected.
var ErrUnauthorized = errors.New("the GitHub token was rejected")

// ErrForbidden means GitHub understood the token and refused the request: the
// token lacks a permission, is still waiting for an organisation owner's
// approval, or is blocked by the organisation's policy. A rate limit is not
// this; it is reported in its own words.
var ErrForbidden = errors.New("GitHub refused the request")

// statusError reports a failed request in the same words as before ErrForbidden
// existed, while letting a caller ask errors.Is which kind it was.
type statusError struct {
	message string
	kind    error
}

func (e *statusError) Error() string        { return e.message }
func (e *statusError) Is(target error) bool { return target == e.kind }

// REST is the HTTP implementation.
type REST struct {
	// BaseURL is the API root. Supplied by the caller rather than compiled in, so
	// that no host appears in this repository (ADR-0004) and GitHub Enterprise
	// works without a code change.
	BaseURL string
	Token   string
	HTTP    *http.Client
	// UserAgent identifies this tool in GitHub's logs, which is a courtesy that
	// costs nothing and helps when debugging rate limits.
	UserAgent string
	// GraphQLURL is where the GraphQL endpoint lives. It is not derivable from
	// BaseURL: github.com serves REST at api.github.com and GraphQL at
	// api.github.com/graphql, while GitHub Enterprise serves them at
	// <host>/api/v3 and <host>/api/graphql — sibling paths, not nested ones.
	GraphQLURL string
}

// New returns a client. baseURL must be supplied; there is no default.
func New(baseURL, token, userAgent string) (*REST, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("no GitHub API base URL configured")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("%w: no token supplied", ErrUnauthorized)
	}
	base := strings.TrimRight(baseURL, "/")
	return &REST{
		BaseURL:    base,
		GraphQLURL: defaultGraphQLURL(base),
		Token:      token,
		HTTP:       &http.Client{Timeout: 30 * time.Second},
		UserAgent:  userAgent,
	}, nil
}

// defaultGraphQLURL guesses the GraphQL endpoint from the REST one. It is only a
// guess: a deployment that does not match either shape sets github.graphqlUrl.
func defaultGraphQLURL(base string) string {
	// GitHub Enterprise: .../api/v3 for REST, .../api/graphql for GraphQL.
	if strings.HasSuffix(base, "/api/v3") {
		return strings.TrimSuffix(base, "/v3") + "/graphql"
	}
	return base + "/graphql"
}

func (c *REST) Viewer(ctx context.Context) (User, error) {
	var u User
	err := c.get(ctx, "/user", &u)
	return u, err
}

// GetRepo reads one repository as this token sees it. It is a method on REST and
// not part of Client: reviewing a pull request never needs it, and only the
// review workspace asks.
func (c *REST) GetRepo(ctx context.Context, repo Repo) (RepoInfo, error) {
	var info RepoInfo
	err := c.get(ctx, "/repos/"+repo.String(), &info)
	return info, err
}

func (c *REST) GetPullRequest(ctx context.Context, repo Repo, number int) (PullRequest, error) {
	var pr PullRequest
	err := c.get(ctx, fmt.Sprintf("/repos/%s/pulls/%d", repo, number), &pr)
	return pr, err
}

func (c *REST) ListPullRequests(ctx context.Context, repo Repo, state string) ([]PullRequest, error) {
	if state == "" {
		state = "open"
	}
	return paginate[PullRequest](ctx, c, fmt.Sprintf("/repos/%s/pulls?state=%s&sort=updated&direction=desc",
		repo, url.QueryEscape(state)))
}

// ListOpenPullRequests returns up to max open pull requests, most recently
// updated first, and whether there were more.
//
// It is bounded where ListPullRequests is not because it is called while a
// person waits: a repository with thousands of open pull requests would
// otherwise cost one round trip per hundred before anything was shown.
func (c *REST) ListOpenPullRequests(ctx context.Context, repo Repo, max int) (pulls []PullRequest, more bool, err error) {
	pulls, err = paginateUntil[PullRequest](ctx, c,
		fmt.Sprintf("/repos/%s/pulls?state=open&sort=updated&direction=desc", repo),
		func(PullRequest) bool {
			if max <= 0 {
				more = true
				return true
			}
			max--
			return false
		})
	if err != nil {
		return nil, false, err
	}
	return pulls, more, nil
}

// ListPullRequestsUpdatedSince returns the pull requests touched in a window,
// stopping as soon as the pages run past it.
//
// The list is sorted by update time descending, so the first pull request older
// than the cutoff means every one after it is too. Without the early stop the cost
// of a measurement scales with the size of the repository rather than with the
// size of the window — fifty round trips on a five-thousand-pull-request
// repository to look at seven, which is the wrong axis and makes a secondary rate
// limit far more likely.
func (c *REST) ListPullRequestsUpdatedSince(ctx context.Context, repo Repo, since time.Time) ([]PullRequest, error) {
	return paginateUntil[PullRequest](ctx, c,
		fmt.Sprintf("/repos/%s/pulls?state=all&sort=updated&direction=desc", repo),
		func(pr PullRequest) bool { return pr.UpdatedAt.Before(since) })
}

func (c *REST) ListFiles(ctx context.Context, repo Repo, number int) ([]ChangedFile, error) {
	return paginate[ChangedFile](ctx, c, fmt.Sprintf("/repos/%s/pulls/%d/files", repo, number))
}

func (c *REST) ListReviewComments(ctx context.Context, repo Repo, number int) ([]ReviewComment, error) {
	return paginate[ReviewComment](ctx, c, fmt.Sprintf("/repos/%s/pulls/%d/comments", repo, number))
}

func (c *REST) ListIssueComments(ctx context.Context, repo Repo, number int) ([]IssueComment, error) {
	return paginate[IssueComment](ctx, c, fmt.Sprintf("/repos/%s/issues/%d/comments", repo, number))
}

func (c *REST) ListReviews(ctx context.Context, repo Repo, number int) ([]Review, error) {
	return paginate[Review](ctx, c, fmt.Sprintf("/repos/%s/pulls/%d/reviews", repo, number))
}

// TimelineEvent is one entry of a pull request's timeline, reduced to what it
// was and when.
type TimelineEvent struct {
	// Event is GitHub's name for the entry: "ready_for_review",
	// "convert_to_draft", "committed", and so on.
	Event string `json:"event"`
	// CreatedAt is zero for the kinds of entry that carry their time under
	// another name, such as a commit's.
	CreatedAt time.Time `json:"created_at"`
}

// ListTimelineEvents reads a pull request's timeline. It is a method on REST and
// not part of Client: only outcome measurement asks, for the moment a draft was
// marked ready for review, which the pull request itself does not record.
func (c *REST) ListTimelineEvents(ctx context.Context, repo Repo, number int) ([]TimelineEvent, error) {
	return paginate[TimelineEvent](ctx, c, fmt.Sprintf("/repos/%s/issues/%d/timeline", repo, number))
}

// PullRequestsForCommit returns the numbers of the pull requests GitHub
// associates with a commit: for a commit on the default branch, the merged pull
// request that introduced it, whether as a squash, a rebased commit or a commit
// of a merged branch. It is a method on REST and not part of Client: only
// outcome measurement asks, to learn whose a line blame credits to a commit is.
//
// The response is decoded in the shape GitHub documents, a list of pull
// requests, and has not been checked against a live response.
func (c *REST) PullRequestsForCommit(ctx context.Context, repo Repo, sha string) ([]int, error) {
	pulls, err := paginate[PullRequest](ctx, c, fmt.Sprintf("/repos/%s/commits/%s/pulls", repo, url.PathEscape(sha)))
	if err != nil {
		return nil, err
	}
	numbers := make([]int, 0, len(pulls))
	for _, pr := range pulls {
		numbers = append(numbers, pr.Number)
	}
	return numbers, nil
}

// MaxCompareFiles is the most files GitHub lists in one comparison. A
// comparison that returns this many may have been cut short, so a caller
// looking for a file in it cannot conclude from its absence that it is absent.
const MaxCompareFiles = 300

// CompareFiles returns the files changed between two commits, with their
// patches, as GitHub's three-dot comparison reports them. It is a method on
// REST and not part of Client, for the same reason as ListTimelineEvents.
//
// Not Compare, which wants the merge base alone and asks GitHub to leave the
// files out. No paging parameter here: GitHub sends the file list with the
// first page only.
func (c *REST) CompareFiles(ctx context.Context, repo Repo, base, head string) ([]ChangedFile, error) {
	var out struct {
		Files []ChangedFile `json:"files"`
	}
	path := fmt.Sprintf("/repos/%s/compare/%s...%s", repo, url.PathEscape(base), url.PathEscape(head))
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return out.Files, nil
}

// GetContentsAt reads one file as it is at a commit. The bool is false, with no
// error, when the file is not there: GitHub answers 404 for that, and also for
// a repository this token cannot see, which a caller has usually ruled out by
// then. It is a method on REST and not part of Client: only the risk record
// reads files, and only at the merge base, so that a pull request cannot change
// the configuration its own record is computed from.
//
// sha should be a full commit hash, for the reason Archive gives.
func (c *REST) GetContentsAt(ctx context.Context, repo Repo, path, sha string) ([]byte, bool, error) {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	target := fmt.Sprintf("/repos/%s/contents/%s?ref=%s", repo, strings.Join(segments, "/"), url.QueryEscape(sha))

	var raw json.RawMessage
	_, err := c.do(ctx, http.MethodGet, target, nil, &raw)
	switch {
	case errors.Is(err, ErrNotFound):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}
	// A directory answers with a list of its entries, not an object.
	if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && trimmed[0] == '[' {
		return nil, false, fmt.Errorf("GET %s: %s is a directory, not a file", redact(c.BaseURL+target), path)
	}
	var file struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, false, fmt.Errorf("decoding %s response: %w", redact(c.BaseURL+target), err)
	}
	switch {
	case file.Type != "" && file.Type != "file":
		return nil, false, fmt.Errorf("GET %s: %s is a %s, not a file", redact(c.BaseURL+target), path, file.Type)
	case file.Encoding != "base64":
		// GitHub leaves the content out, with encoding "none", for a file over
		// one megabyte. Nothing this tool reads that way should be that large.
		return nil, false, fmt.Errorf("GET %s: %s is too large to read through the contents API", redact(c.BaseURL+target), path)
	}
	// The content is wrapped at 60 characters, which StdEncoding refuses.
	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
	if err != nil {
		return nil, false, fmt.Errorf("decoding %s content: %w", redact(c.BaseURL+target), err)
	}
	return content, true, nil
}

// Commit is one commit, as far as the risk record reads it.
type Commit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
	} `json:"commit"`
}

// Subject is the first line of the commit message.
func (c Commit) Subject() string {
	subject, _, _ := strings.Cut(c.Commit.Message, "\n")
	return subject
}

// ListCommits returns the commits reachable from ref that touch path and were
// made after since, newest first. It is a method on REST and not part of
// Client: only the risk record asks, for recent reverts of a changed file.
//
// ref is named explicitly rather than left to GitHub's default, which is the
// repository's default branch: a pull request may target another branch, and
// its history is the one that matters.
func (c *REST) ListCommits(ctx context.Context, repo Repo, ref, path string, since time.Time) ([]Commit, error) {
	return paginate[Commit](ctx, c, fmt.Sprintf("/repos/%s/commits?sha=%s&path=%s&since=%s",
		repo, url.QueryEscape(ref), url.QueryEscape(path), url.QueryEscape(since.UTC().Format(time.RFC3339))))
}

// paginate follows GitHub's Link headers to the end.
//
// Not optional: a pull request with more than thirty existing comments would
// otherwise have its later ones invisible to dedupe, and the tool would repost
// them — the exact failure fingerprinting exists to prevent.
func paginate[T any](ctx context.Context, c *REST, path string) ([]T, error) {
	const perPage = 100
	next := addQuery(path, "per_page", strconv.Itoa(perPage))

	var all []T
	for next != "" {
		var page []T
		link, err := c.do(ctx, http.MethodGet, next, nil, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		next = nextLink(link)
	}
	return all, nil
}

// paginateUntil is paginate with a stopping condition, for an endpoint whose order
// makes the rest of the pages irrelevant.
func paginateUntil[T any](ctx context.Context, c *REST, path string, stop func(T) bool) ([]T, error) {
	const perPage = 100
	next := addQuery(path, "per_page", strconv.Itoa(perPage))

	var all []T
	for next != "" {
		var page []T
		link, err := c.do(ctx, http.MethodGet, next, nil, &page)
		if err != nil {
			return all, err
		}
		for _, item := range page {
			if stop(item) {
				return all, nil
			}
			all = append(all, item)
		}
		next = nextLink(link)
	}
	return all, nil
}

func addQuery(path, key, value string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + key + "=" + value
}

// nextLink reads the rel="next" URL out of a Link header.
func nextLink(header string) string {
	for _, part := range strings.Split(header, ",") {
		segments := strings.Split(strings.TrimSpace(part), ";")
		if len(segments) < 2 {
			continue
		}
		target := strings.Trim(strings.TrimSpace(segments[0]), "<>")
		for _, attr := range segments[1:] {
			if strings.TrimSpace(attr) == `rel="next"` {
				return target
			}
		}
	}
	return ""
}

func (c *REST) get(ctx context.Context, path string, into any) error {
	_, err := c.do(ctx, http.MethodGet, path, nil, into)
	return err
}

// do performs one request and returns its Link header.
func (c *REST) do(ctx context.Context, method, pathOrURL string, body any, into any) (string, error) {
	target := pathOrURL
	if strings.HasPrefix(target, "/") {
		target = c.BaseURL + target
	}

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return "", fmt.Errorf("encoding request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", APIVersion)
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", method, redact(target), err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return "", fmt.Errorf("%w (%s %s)", ErrUnauthorized, method, redact(target))
	case resp.StatusCode == http.StatusNotFound:
		return "", fmt.Errorf("%w (%s %s)", ErrNotFound, method, redact(target))
	case resp.StatusCode == http.StatusForbidden && strings.Contains(string(raw), "rate limit"):
		return "", fmt.Errorf("GitHub rate limit reached; reset at %s", resp.Header.Get("X-RateLimit-Reset"))
	case resp.StatusCode == http.StatusForbidden:
		return "", &statusError{
			message: fmt.Sprintf("%s %s: %s: %s", method, redact(target), resp.Status, apiMessage(raw)),
			kind:    ErrForbidden,
		}
	case resp.StatusCode >= 300:
		// The message, not the whole body: a GitHub error body can be long and is
		// not worth pasting into a run log.
		return "", fmt.Errorf("%s %s: %s: %s", method, redact(target), resp.Status, apiMessage(raw))
	}

	if into != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, into); err != nil {
			return "", fmt.Errorf("decoding %s response: %w", redact(target), err)
		}
	}
	return resp.Header.Get("Link"), nil
}

// apiMessage pulls GitHub's own error message out of a response body.
func apiMessage(raw []byte) string {
	var e struct {
		Message string `json:"message"`
		Errors  []struct {
			Resource string `json:"resource"`
			Field    string `json:"field"`
			Code     string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &e); err != nil || e.Message == "" {
		return strings.TrimSpace(truncate(string(raw), 200))
	}
	if len(e.Errors) > 0 {
		first := e.Errors[0]
		return fmt.Sprintf("%s (%s.%s: %s)", e.Message, first.Resource, first.Field, first.Code)
	}
	return e.Message
}

// redact removes any query string before a URL reaches a log. GitHub does not
// accept tokens in the query, but a future caller might add one, and a URL in an
// error message is exactly the thing that ends up in a CI log.
func redact(target string) string {
	if i := strings.IndexByte(target, '?'); i >= 0 {
		return target[:i] + "?…"
	}
	return target
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
