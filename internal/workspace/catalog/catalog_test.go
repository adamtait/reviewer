// SPDX-License-Identifier: MIT

package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/ghclient"
	"github.com/adamtait/reviewer/internal/workspace/api/apitypes"
	"github.com/adamtait/reviewer/internal/workspace/testgithub"
)

const (
	head  = "1111111111111111111111111111111111111111"
	tip   = "2222222222222222222222222222222222222222"
	base  = "3333333333333333333333333333333333333333"
	other = "4444444444444444444444444444444444444444"
)

var repo = ghclient.Repo{Owner: "example", Name: "alpha"}

func setup(t *testing.T) (*testgithub.Server, *ghclient.REST) {
	t.Helper()
	gh := testgithub.New(t)
	gh.SetRepo("example", "alpha", testgithub.Repo{Private: true, Readers: []string{"alice"}})
	gh.AddPull("example", "alpha", testgithub.Pull{
		Number: 12, Title: "Rename total to sum", Body: "Why: the old name lied.\n\n<script>alert(1)</script>",
		Author: "alice", HeadSHA: head, HeadRef: "rename", BaseSHA: tip, BaseRef: "main", MergeBase: base,
		Updated: time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC),
		Files: []testgithub.File{
			{Filename: "src/sum.ts", Status: "renamed", PreviousFilename: "src/total.ts"},
			{Filename: "src/app.ts", Status: "modified"},
			{Filename: "src/new.ts", Status: "added"},
			{Filename: "docs/old.md", Status: "removed"},
			{Filename: "src/copy.ts", Status: "copied", PreviousFilename: "src/app.ts"},
			{Filename: "bin/tool", Status: "changed"},
		},
	})
	gh.AddPull("example", "alpha", testgithub.Pull{
		Number: 7, Title: "Work in progress", Author: "bob", Draft: true,
		HeadSHA: other, BaseSHA: tip, BaseRef: "main", MergeBase: base,
		Updated: time.Date(2026, 11, 22, 16, 0, 0, 0, time.FixedZone("west", -8*3600)),
	})
	gh.AddPull("example", "alpha", testgithub.Pull{
		Number: 3, Title: "Merged long ago", Author: "alice", Closed: true,
		HeadSHA: other, BaseSHA: tip, BaseRef: "main", MergeBase: base,
		Updated: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	client, err := gh.Client(gh.AddUser("alice"))
	if err != nil {
		t.Fatal(err)
	}
	return gh, client
}

func TestListShowsOpenPullRequestsNewestFirst(t *testing.T) {
	_, client := setup(t)
	list, err := List(context.Background(), client, repo)
	if err != nil {
		t.Fatal(err)
	}
	if list.Truncated || len(list.Pulls) != 2 {
		t.Fatalf("list = %+v, want the two open pull requests", list)
	}
	want := []apitypes.PullSummary{
		// Times are written in UTC whatever zone GitHub sent them in.
		{Number: 7, Title: "Work in progress", Author: "bob", Draft: true, HeadSHA: other, UpdatedAt: "2026-11-23T00:00:00Z"},
		{Number: 12, Title: "Rename total to sum", Author: "alice", HeadSHA: head, UpdatedAt: "2026-11-20T09:30:00Z"},
	}
	for i := range want {
		if list.Pulls[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, list.Pulls[i], want[i])
		}
	}
}

func TestAnEmptyListIsAListNotNull(t *testing.T) {
	gh, client := setup(t)
	gh.SetRepo("example", "quiet", testgithub.Repo{Private: true, Readers: []string{"alice"}})
	list, err := List(context.Background(), client, ghclient.Repo{Owner: "example", Name: "quiet"})
	if err != nil {
		t.Fatal(err)
	}
	if list.Pulls == nil || len(list.Pulls) != 0 {
		t.Errorf("Pulls = %#v, want an empty list the browser can loop over", list.Pulls)
	}
}

func TestViewGivesTheMergeBaseAndTheChangedFiles(t *testing.T) {
	_, client := setup(t)
	view, err := View(context.Background(), client, repo, 12, "https://github.example/")
	if err != nil {
		t.Fatal(err)
	}
	if view.HeadSHA != head {
		t.Errorf("HeadSHA = %s", view.HeadSHA)
	}
	// The merge base, not the tip of the base branch: the diff is measured
	// from the commit both sides share.
	if view.MergeBaseSHA != base {
		t.Errorf("MergeBaseSHA = %s, want the merge base %s (the base branch's tip is %s)", view.MergeBaseSHA, base, tip)
	}
	if view.Title != "Rename total to sum" || view.Author != "alice" || view.State != "open" || view.BaseRef != "main" || view.Draft {
		t.Errorf("view = %+v", view)
	}
	// The description is passed on exactly as written. Showing it as text is
	// the browser's job, and it can only do that if nothing here changed it.
	if !strings.Contains(view.Body, "<script>alert(1)</script>") {
		t.Errorf("Body = %q", view.Body)
	}

	want := []apitypes.ChangedFile{
		{Path: "src/sum.ts", PreviousPath: "src/total.ts", Status: "renamed"},
		{Path: "src/app.ts", Status: "modified"},
		{Path: "src/new.ts", Status: "added"},
		{Path: "docs/old.md", Status: "deleted"},
		// A copy is a new file; its source is not a previous path to diff against.
		{Path: "src/copy.ts", Status: "added"},
		{Path: "bin/tool", Status: "modified"},
	}
	if len(view.Files) != len(want) {
		t.Fatalf("files = %+v", view.Files)
	}
	for i := range want {
		if view.Files[i] != want[i] {
			t.Errorf("file %d = %+v, want %+v", i, view.Files[i], want[i])
		}
	}
	if view.FilesTruncated {
		t.Error("FilesTruncated with every file listed")
	}
}

func TestTheLinkToGitHubIsBuiltNotPassedOn(t *testing.T) {
	_, client := setup(t)
	view, err := View(context.Background(), client, repo, 12, "https://github.example/")
	if err != nil {
		t.Fatal(err)
	}
	if view.WebURL != "https://github.example/example/alpha/pull/12" {
		t.Errorf("WebURL = %q", view.WebURL)
	}
	if strings.Contains(view.WebURL, "elsewhere") {
		t.Error("the address GitHub's response carried was passed on as a link")
	}
}

func TestViewSaysWhenGitHubCutTheFileListShort(t *testing.T) {
	gh, client := setup(t)
	gh.AddPull("example", "alpha", testgithub.Pull{
		Number: 40, Title: "Vendored a framework", Author: "alice",
		HeadSHA: head, BaseSHA: tip, BaseRef: "main", MergeBase: base,
		Files:        []testgithub.File{{Filename: "a.ts", Status: "added"}},
		ChangedFiles: 4200,
	})
	view, err := View(context.Background(), client, repo, 40, "https://github.example")
	if err != nil {
		t.Fatal(err)
	}
	if !view.FilesTruncated || len(view.Files) != 1 {
		t.Errorf("FilesTruncated = %v with %d of 4200 files listed", view.FilesTruncated, len(view.Files))
	}
}

func TestViewOfAClosedPullRequestStillWorks(t *testing.T) {
	_, client := setup(t)
	view, err := View(context.Background(), client, repo, 3, "https://github.example")
	if err != nil {
		t.Fatal(err)
	}
	if view.State != "closed" || view.Files == nil || len(view.Files) != 0 {
		t.Errorf("view = %+v", view)
	}
}

func TestViewPassesOnWhatKindOfFailureItWas(t *testing.T) {
	gh, client := setup(t)

	if _, err := View(context.Background(), client, repo, 999, "https://github.example"); !errors.Is(err, ghclient.ErrNotFound) {
		t.Errorf("an unknown pull request: %v, want not found", err)
	}

	// Each of the two parallel calls failing on its own.
	gh.Fail("/repos/example/alpha/compare/", 403)
	if _, err := View(context.Background(), client, repo, 12, "https://github.example"); !errors.Is(err, ghclient.ErrForbidden) {
		t.Errorf("compare refused: %v, want forbidden", err)
	}
	gh.Fail("/repos/example/alpha/compare/", 0)
	gh.Fail("/repos/example/alpha/pulls/12/files", 500)
	if _, err := View(context.Background(), client, repo, 12, "https://github.example"); err == nil {
		t.Error("the file list failed and View returned no error")
	}

	// A user who cannot read the repository gets GitHub's "not found".
	stranger, err := gh.Client(gh.AddUser("mallory"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := List(context.Background(), stranger, repo); !errors.Is(err, ghclient.ErrNotFound) {
		t.Errorf("a stranger's list: %v, want not found", err)
	}
}
