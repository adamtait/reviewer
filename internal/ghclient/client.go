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
}

// Ref is one end of a pull request.
type Ref struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
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
}

// ErrNotFound distinguishes "this pull request does not exist" from "the token
// cannot see it" — which GitHub does not, returning 404 for both.
var ErrNotFound = errors.New("not found, or not visible to this token")

// ErrUnauthorized means the token is missing or rejected.
var ErrUnauthorized = errors.New("the GitHub token was rejected")

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
