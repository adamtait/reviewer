// SPDX-License-Identifier: MIT

package ghclient

import (
	"context"
	"fmt"
	"net/http"
)

// Writer is everything this tool can change on a pull request. Kept separate from
// Client so that the read path demonstrably needs no write access.
//
// The whole list is three verbs: post an inline comment, post or update one
// conversation comment, and resolve a thread this tool itself created. Nothing
// here can merge, approve, close, or push.
type Writer interface {
	CreateReviewComment(ctx context.Context, repo Repo, number int, c NewReviewComment) (ReviewComment, error)
	CreateIssueComment(ctx context.Context, repo Repo, number int, body string) (IssueComment, error)
	UpdateIssueComment(ctx context.Context, repo Repo, id int64, body string) (IssueComment, error)
	ResolveReviewThread(ctx context.Context, threadID string) error
	ReviewThreads(ctx context.Context, repo Repo, number int) ([]ReviewThread, error)
}

// NewReviewComment is an inline comment to post.
type NewReviewComment struct {
	Body string `json:"body"`
	// CommitID must be the pull request's head at the time of posting; GitHub
	// rejects a stale one rather than silently attaching to the wrong revision.
	CommitID string `json:"commit_id"`
	Path     string `json:"path"`
	// Line is the last line of the span, which is what GitHub anchors to.
	Line int `json:"line"`
	// StartLine is set only for a multi-line comment, and must be omitted rather
	// than zero for a single-line one.
	StartLine int    `json:"start_line,omitempty"`
	Side      string `json:"side"`
	StartSide string `json:"start_side,omitempty"`
}

// ReviewThread is an inline conversation, with the resolution state acceptance
// measurement reads (ADR-0024) and the node id needed to resolve it.
type ReviewThread struct {
	ID         string
	IsResolved bool
	IsOutdated bool
	// Comments are the thread's comments in order; the first carries this tool's
	// fingerprint marker when the thread is one of ours.
	Comments []ReviewComment
}

func (c *REST) CreateReviewComment(ctx context.Context, repo Repo, number int, comment NewReviewComment) (ReviewComment, error) {
	if comment.Side == "" {
		// RIGHT is the post-change side. Commenting on the LEFT side would put the
		// comment on code the pull request deleted.
		comment.Side = "RIGHT"
	}
	if comment.StartLine != 0 && comment.StartSide == "" {
		comment.StartSide = comment.Side
	}
	if comment.StartLine >= comment.Line {
		// GitHub requires start_line < line; a one-line span must omit it entirely.
		comment.StartLine = 0
		comment.StartSide = ""
	}

	var created ReviewComment
	_, err := c.do(ctx, http.MethodPost,
		fmt.Sprintf("/repos/%s/pulls/%d/comments", repo, number), comment, &created)
	return created, err
}

func (c *REST) CreateIssueComment(ctx context.Context, repo Repo, number int, body string) (IssueComment, error) {
	var created IssueComment
	_, err := c.do(ctx, http.MethodPost,
		fmt.Sprintf("/repos/%s/issues/%d/comments", repo, number),
		map[string]string{"body": body}, &created)
	return created, err
}

func (c *REST) UpdateIssueComment(ctx context.Context, repo Repo, id int64, body string) (IssueComment, error) {
	var updated IssueComment
	_, err := c.do(ctx, http.MethodPatch,
		fmt.Sprintf("/repos/%s/issues/comments/%d", repo, id),
		map[string]string{"body": body}, &updated)
	return updated, err
}

// graphQL is the second protocol this client speaks, and only because thread
// resolution has no REST equivalent.
func (c *REST) graphQL(ctx context.Context, query string, variables map[string]any, into any) error {
	body := map[string]any{"query": query, "variables": variables}
	_, err := c.do(ctx, http.MethodPost, c.GraphQLURL, body, into)
	return err
}

// ReviewThreads lists inline threads with their resolution state. REST exposes
// comments but not whether their thread is resolved, which is what acceptance
// measurement needs, so this one read goes through GraphQL.
func (c *REST) ReviewThreads(ctx context.Context, repo Repo, number int) ([]ReviewThread, error) {
	const query = `
query($owner:String!, $name:String!, $number:Int!, $cursor:String) {
  repository(owner:$owner, name:$name) {
    pullRequest(number:$number) {
      reviewThreads(first:100, after:$cursor) {
        pageInfo { hasNextPage endCursor }
        nodes {
          id
          isResolved
          isOutdated
          comments(first:1) { nodes { id databaseId body path author { login } } }
        }
      }
    }
  }
}`

	var out []ReviewThread
	var cursor *string
	for {
		var resp struct {
			Data struct {
				Repository struct {
					PullRequest struct {
						ReviewThreads struct {
							PageInfo struct {
								HasNextPage bool   `json:"hasNextPage"`
								EndCursor   string `json:"endCursor"`
							} `json:"pageInfo"`
							Nodes []struct {
								ID         string `json:"id"`
								IsResolved bool   `json:"isResolved"`
								IsOutdated bool   `json:"isOutdated"`
								Comments   struct {
									Nodes []struct {
										ID         string `json:"id"`
										DatabaseID int64  `json:"databaseId"`
										Body       string `json:"body"`
										Path       string `json:"path"`
										Author     struct {
											Login string `json:"login"`
										} `json:"author"`
									} `json:"nodes"`
								} `json:"comments"`
							} `json:"nodes"`
						} `json:"reviewThreads"`
					} `json:"pullRequest"`
				} `json:"repository"`
			} `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}

		vars := map[string]any{"owner": repo.Owner, "name": repo.Name, "number": number}
		if cursor != nil {
			vars["cursor"] = *cursor
		}
		if err := c.graphQL(ctx, query, vars, &resp); err != nil {
			return nil, err
		}
		// GraphQL reports errors with a 200, so the body has to be checked.
		if len(resp.Errors) > 0 {
			return nil, fmt.Errorf("listing review threads: %s", resp.Errors[0].Message)
		}

		threads := resp.Data.Repository.PullRequest.ReviewThreads
		for _, node := range threads.Nodes {
			thread := ReviewThread{ID: node.ID, IsResolved: node.IsResolved, IsOutdated: node.IsOutdated}
			for _, comment := range node.Comments.Nodes {
				thread.Comments = append(thread.Comments, ReviewComment{
					ID:     comment.DatabaseID,
					NodeID: comment.ID,
					Body:   comment.Body,
					Path:   comment.Path,
					User:   User{Login: comment.Author.Login},
				})
			}
			out = append(out, thread)
		}
		if !threads.PageInfo.HasNextPage {
			return out, nil
		}
		next := threads.PageInfo.EndCursor
		cursor = &next
	}
}

// ResolveReviewThread marks a thread resolved. Only ever called on a thread this
// tool started, which is checked by the caller (ADR-0018's sibling concern: this
// is the one action the tool takes on a conversation).
func (c *REST) ResolveReviewThread(ctx context.Context, threadID string) error {
	const mutation = `
mutation($threadId:ID!) {
  resolveReviewThread(input:{threadId:$threadId}) { thread { id isResolved } }
}`
	var resp struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.graphQL(ctx, mutation, map[string]any{"threadId": threadID}, &resp); err != nil {
		return err
	}
	if len(resp.Errors) > 0 {
		return fmt.Errorf("resolving thread: %s", resp.Errors[0].Message)
	}
	return nil
}
