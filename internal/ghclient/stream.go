// SPDX-License-Identifier: MIT

package ghclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
