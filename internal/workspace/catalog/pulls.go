// SPDX-License-Identifier: MIT

// Package catalog lists and describes pull requests, as the signed-in user.
//
// Nothing here is kept. Every call asks GitHub with the user's own token, so
// what a user is shown is what GitHub would show them.
package catalog

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/adamtait/reviewer/internal/ghclient"
	"github.com/adamtait/reviewer/internal/workspace/api/apitypes"
)

// MaxListed is the most open pull requests one list shows.
const MaxListed = 300

// GitHub is the calls the catalog makes.
type GitHub interface {
	ListOpenPullRequests(ctx context.Context, repo ghclient.Repo, max int) ([]ghclient.PullRequest, bool, error)
	GetPullRequest(ctx context.Context, repo ghclient.Repo, number int) (ghclient.PullRequest, error)
	ListFiles(ctx context.Context, repo ghclient.Repo, number int) ([]ghclient.ChangedFile, error)
	Compare(ctx context.Context, repo ghclient.Repo, base, head string) (string, error)
}

// List returns a repository's open pull requests, most recently updated first.
func List(ctx context.Context, gh GitHub, repo ghclient.Repo) (apitypes.PullList, error) {
	pulls, more, err := gh.ListOpenPullRequests(ctx, repo, MaxListed)
	if err != nil {
		return apitypes.PullList{}, err
	}
	out := apitypes.PullList{Pulls: make([]apitypes.PullSummary, 0, len(pulls)), Truncated: more}
	for _, pr := range pulls {
		out.Pulls = append(out.Pulls, apitypes.PullSummary{
			Number:    pr.Number,
			Title:     pr.Title,
			Author:    pr.User.Login,
			Draft:     pr.Draft,
			HeadSHA:   pr.Head.SHA,
			UpdatedAt: pr.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

// View returns one pull request: what it says, the two commits its diff is
// drawn between, and the files it changes.
//
// webBase is GitHub's web address, from configuration. The link to the pull
// request is built from it and not taken from GitHub's response, so that an
// address the browser will open is never one that arrived as data.
func View(ctx context.Context, gh GitHub, repo ghclient.Repo, number int, webBase string) (apitypes.PullView, error) {
	pr, err := gh.GetPullRequest(ctx, repo, number)
	if err != nil {
		return apitypes.PullView{}, err
	}

	// The merge base and the file list do not depend on each other, and each
	// is a round trip a person is waiting on.
	var (
		wg        sync.WaitGroup
		mergeBase string
		baseErr   error
		files     []ghclient.ChangedFile
		filesErr  error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		// The base branch's tip is not what the diff is measured from: the
		// branch has usually moved on since the pull request left it. The
		// merge base is the commit both sides share.
		mergeBase, baseErr = gh.Compare(ctx, repo, pr.Base.SHA, pr.Head.SHA)
	}()
	go func() {
		defer wg.Done()
		files, filesErr = gh.ListFiles(ctx, repo, number)
	}()
	wg.Wait()
	if baseErr != nil {
		return apitypes.PullView{}, fmt.Errorf("finding the merge base: %w", baseErr)
	}
	if filesErr != nil {
		return apitypes.PullView{}, fmt.Errorf("listing the changed files: %w", filesErr)
	}

	view := apitypes.PullView{
		Number:       pr.Number,
		Title:        pr.Title,
		Body:         pr.Body,
		Author:       pr.User.Login,
		State:        pr.State,
		Draft:        pr.Draft,
		HeadSHA:      pr.Head.SHA,
		BaseRef:      pr.Base.Ref,
		MergeBaseSHA: mergeBase,
		Files:        make([]apitypes.ChangedFile, 0, len(files)),
		// GitHub's file list stops at 3,000. Its count of changed files does not.
		FilesTruncated: pr.ChangedFiles > len(files),
		WebURL:         WebURL(webBase, repo, number),
	}
	for _, f := range files {
		view.Files = append(view.Files, changedFile(f))
	}
	return view, nil
}

// changedFile maps GitHub's seven statuses onto the four the diff view draws.
func changedFile(f ghclient.ChangedFile) apitypes.ChangedFile {
	out := apitypes.ChangedFile{Path: f.Filename}
	switch f.Status {
	case "added", "copied":
		// A copy is a new file. Where its content came from is not something
		// the diff view shows.
		out.Status = apitypes.StatusAdded
	case "removed":
		out.Status = apitypes.StatusDeleted
	case "renamed":
		out.Status = apitypes.StatusRenamed
		out.PreviousPath = f.PreviousFilename
	default:
		// "modified", "changed", "unchanged", and anything GitHub adds later:
		// the file exists on both sides, and comparing the two shows what
		// happened.
		out.Status = apitypes.StatusModified
	}
	return out
}

// WebURL is a pull request's address on GitHub.
func WebURL(webBase string, repo ghclient.Repo, number int) string {
	return fmt.Sprintf("%s/%s/%s/pull/%d", strings.TrimRight(webBase, "/"),
		url.PathEscape(repo.Owner), url.PathEscape(repo.Name), number)
}
