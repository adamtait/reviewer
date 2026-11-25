// SPDX-License-Identifier: MIT

package ghclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// The downloads below are the review workspace's: a commit's archive, and a
// workflow artifact. They differ from every other call in this package in three
// ways, and each difference is a way to get it wrong.
//
// They are large and slow. The client every other call uses has a 30-second
// timeout that also bounds reading the body, so a download through it is cut
// off at 30 seconds however long the caller allowed. These go through a second
// client with no timeout of its own; the caller's context is the only deadline.
//
// They are not JSON. The body is returned as a stream for the caller to bound
// and decode.
//
// And GitHub answers them with a redirect to a signed address on another host.
// The token must not follow it there, and the signed address must not reach an
// error message: it is a credential for the thing it points at.

// streamClient follows no redirect itself, so that download can.
var streamClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// download GETs an API path and returns the body it leads to.
func (c *REST) download(ctx context.Context, path, accept string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", APIVersion)
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", redact(c.BaseURL+path), stripURL(err))
	}

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		location := resp.Header.Get("Location")
		_ = resp.Body.Close()
		if location == "" {
			return nil, fmt.Errorf("GET %s: redirected with no address", redact(c.BaseURL+path))
		}
		// A new request, built from nothing: no Authorization header, and no
		// header copied from the first. The signed address is its own credential.
		follow, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
		if err != nil {
			return nil, fmt.Errorf("GET %s: the redirect address is not a URL", redact(c.BaseURL+path))
		}
		if c.UserAgent != "" {
			follow.Header.Set("User-Agent", c.UserAgent)
		}
		resp, err = streamClient.Do(follow)
		if err != nil {
			// Named by the path that was asked for. The address that failed is
			// the signed one, and must not be printed.
			return nil, fmt.Errorf("GET %s: following the redirect: %w", redact(c.BaseURL+path), stripURL(err))
		}
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		return resp.Body, nil
	case resp.StatusCode == http.StatusUnauthorized:
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w (GET %s)", ErrUnauthorized, redact(c.BaseURL+path))
	case resp.StatusCode == http.StatusNotFound:
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w (GET %s)", ErrNotFound, redact(c.BaseURL+path))
	case resp.StatusCode == http.StatusForbidden:
		_ = resp.Body.Close()
		return nil, &statusError{
			message: fmt.Sprintf("GET %s: %s", redact(c.BaseURL+path), resp.Status),
			kind:    ErrForbidden,
		}
	default:
		_ = resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", redact(c.BaseURL+path), resp.Status)
	}
}

// stripURL removes the address from a transport error. net/http wraps every
// failure in a *url.Error whose text begins with the method and the full URL,
// query string and all.
func stripURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

// Archive returns a commit's files as a gzip-compressed tar stream. Every path
// in it sits under one top-level folder, which GitHub names after the owner,
// the repository and a short hash.
//
// sha must be a full commit hash: a branch name here would return whatever the
// branch points at by the time GitHub reads it, and the caller would hold it as
// the commit it asked for.
func (c *REST) Archive(ctx context.Context, repo Repo, sha string) (io.ReadCloser, error) {
	return c.download(ctx, fmt.Sprintf("/repos/%s/tarball/%s", repo, url.PathEscape(sha)), "application/vnd.github+json")
}

// Compare returns the merge base of two commits: the commit a pull request's
// diff is measured from.
func (c *REST) Compare(ctx context.Context, repo Repo, base, head string) (string, error) {
	var out struct {
		MergeBase struct {
			SHA string `json:"sha"`
		} `json:"merge_base_commit"`
	}
	// per_page=1: only the merge base is wanted, and without it GitHub sends
	// up to 300 files and their patches along.
	path := fmt.Sprintf("/repos/%s/compare/%s...%s?per_page=1", repo, url.PathEscape(base), url.PathEscape(head))
	if err := c.get(ctx, path, &out); err != nil {
		return "", err
	}
	if out.MergeBase.SHA == "" {
		return "", fmt.Errorf("GET %s: no merge base in the response", redact(c.BaseURL+path))
	}
	return out.MergeBase.SHA, nil
}

// WorkflowRun is one run of a workflow, as far as the review workspace reads it.
type WorkflowRun struct {
	ID      int64  `json:"id"`
	HeadSHA string `json:"head_sha"`
	// Event is what started the run: "push", "pull_request", and so on.
	Event string `json:"event"`
	// Path is the workflow file the run is of, from the repository root.
	Path string `json:"path"`
	// Status is "completed" once the run has ended, and otherwise one of
	// GitHub's words for a run that has not.
	Status string `json:"status"`
	// Conclusion is set once Status is "completed": "success", "failure",
	// "cancelled", and so on.
	Conclusion string    `json:"conclusion"`
	CreatedAt  time.Time `json:"created_at"`
}

// Artifact is a file a workflow run saved.
type Artifact struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	SizeInBytes int64  `json:"size_in_bytes"`
	// Expired means GitHub has deleted the content and kept the record.
	Expired bool `json:"expired"`
}

// maxWrappedPages bounds the pages read from a list GitHub wraps in an object.
// These lists are filtered to one commit or one run, so they are short; the
// bound is against a response that is not.
const maxWrappedPages = 3

// ListWorkflowRuns returns the runs of one workflow file for one commit that
// one kind of event started, newest first.
//
// GitHub applies the three filters. The caller should still check each run's
// HeadSHA, Event and Path itself before trusting it: the filters are a request,
// and the fields are the answer.
func (c *REST) ListWorkflowRuns(ctx context.Context, repo Repo, workflowFile, headSHA, event string) ([]WorkflowRun, error) {
	path := fmt.Sprintf("/repos/%s/actions/workflows/%s/runs?head_sha=%s&event=%s&per_page=100",
		repo, url.PathEscape(workflowFile), url.QueryEscape(headSHA), url.QueryEscape(event))
	var all []WorkflowRun
	err := c.wrapped(ctx, path, func(raw []byte) error {
		var page struct {
			Runs []WorkflowRun `json:"workflow_runs"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return err
		}
		all = append(all, page.Runs...)
		return nil
	})
	return all, err
}

// ListRunArtifacts returns the artifacts of one run that have a given name.
func (c *REST) ListRunArtifacts(ctx context.Context, repo Repo, runID int64, name string) ([]Artifact, error) {
	path := fmt.Sprintf("/repos/%s/actions/runs/%d/artifacts?name=%s&per_page=100", repo, runID, url.QueryEscape(name))
	var all []Artifact
	err := c.wrapped(ctx, path, func(raw []byte) error {
		var page struct {
			Artifacts []Artifact `json:"artifacts"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return err
		}
		all = append(all, page.Artifacts...)
		return nil
	})
	return all, err
}

// wrapped reads a list that GitHub wraps in an object, page by page. Most of
// GitHub's lists are bare arrays, which paginate reads; the Actions lists are
// not, so each page is handed to the caller to take its own field from.
func (c *REST) wrapped(ctx context.Context, path string, page func(raw []byte) error) error {
	next := path
	for pages := 0; next != "" && pages < maxWrappedPages; pages++ {
		var raw json.RawMessage
		link, err := c.do(ctx, http.MethodGet, next, nil, &raw)
		if err != nil {
			return err
		}
		if err := page(raw); err != nil {
			return fmt.Errorf("GET %s: reading the response: %w", redact(c.BaseURL+path), err)
		}
		next = nextLink(link)
	}
	return nil
}

// DownloadArtifact returns an artifact's content: a zip archive, as a stream.
// GitHub answers with a redirect to a signed address, which is followed
// without the token.
func (c *REST) DownloadArtifact(ctx context.Context, repo Repo, artifactID int64) (io.ReadCloser, error) {
	return c.download(ctx, fmt.Sprintf("/repos/%s/actions/artifacts/%d/zip", repo, artifactID), "application/vnd.github+json")
}
