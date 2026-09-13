// SPDX-License-Identifier: MIT

package poller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/ghclient"
)

type fakeClient struct{ prs []ghclient.PullRequest }

func (f *fakeClient) Viewer(context.Context) (ghclient.User, error) { return ghclient.User{}, nil }
func (f *fakeClient) GetPullRequest(context.Context, ghclient.Repo, int) (ghclient.PullRequest, error) {
	return ghclient.PullRequest{}, nil
}
func (f *fakeClient) ListPullRequests(context.Context, ghclient.Repo, string) ([]ghclient.PullRequest, error) {
	return f.prs, nil
}
func (f *fakeClient) ListFiles(context.Context, ghclient.Repo, int) ([]ghclient.ChangedFile, error) {
	return nil, nil
}
func (f *fakeClient) ListReviewComments(context.Context, ghclient.Repo, int) ([]ghclient.ReviewComment, error) {
	return nil, nil
}
func (f *fakeClient) ListIssueComments(context.Context, ghclient.Repo, int) ([]ghclient.IssueComment, error) {
	return nil, nil
}

func at(minute int) time.Time {
	return time.Date(2026, 9, 13, 10, minute, 0, 0, time.UTC)
}

func pr(number, minute int) ghclient.PullRequest {
	return ghclient.PullRequest{Number: number, UpdatedAt: at(minute)}
}

func poll(t *testing.T, client *fakeClient, statePath string, opts func(*Options)) (reviewed []int, log string) {
	t.Helper()
	var b strings.Builder
	o := Options{Repo: ghclient.Repo{Owner: "o", Name: "r"}, StatePath: statePath, Log: &b}
	if opts != nil {
		opts(&o)
	}
	err := Poll(context.Background(), client, func(_ context.Context, n int) error {
		reviewed = append(reviewed, n)
		return nil
	}, o)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	return reviewed, b.String()
}

func TestFirstPollReviewsEveryOpenPullRequest(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	client := &fakeClient{prs: []ghclient.PullRequest{pr(1, 0), pr(2, 5)}}

	reviewed, _ := poll(t, client, statePath, nil)
	if fmt.Sprint(reviewed) != "[1 2]" {
		t.Fatalf("want both reviewed, got %v", reviewed)
	}
}

// The watermark is the whole point: a second poll with nothing changed must do
// nothing.
func TestUnchangedPullRequestsAreSkipped(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	client := &fakeClient{prs: []ghclient.PullRequest{pr(1, 0), pr(2, 5)}}

	poll(t, client, statePath, nil)
	reviewed, log := poll(t, client, statePath, nil)

	if len(reviewed) != 0 {
		t.Fatalf("want nothing re-reviewed, got %v", reviewed)
	}
	if !strings.Contains(log, "0 reviewed, 2 unchanged") {
		t.Fatalf("want the skip reported, got %q", log)
	}
}

// Keyed by timestamp rather than by a boolean, so a new push means a new review.
func TestANewPushIsReviewedAgain(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	client := &fakeClient{prs: []ghclient.PullRequest{pr(1, 0)}}
	poll(t, client, statePath, nil)

	client.prs = []ghclient.PullRequest{pr(1, 30)}
	reviewed, _ := poll(t, client, statePath, nil)
	if fmt.Sprint(reviewed) != "[1]" {
		t.Fatalf("want the updated pull request reviewed again, got %v", reviewed)
	}
}

// Commenting on a draft is interrupting someone who has not asked for an opinion.
func TestDraftsAreSkipped(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	draft := pr(1, 0)
	draft.Draft = true
	client := &fakeClient{prs: []ghclient.PullRequest{draft, pr(2, 1)}}

	reviewed, log := poll(t, client, statePath, nil)
	if fmt.Sprint(reviewed) != "[2]" {
		t.Fatalf("want only the ready pull request reviewed, got %v", reviewed)
	}
	if !strings.Contains(log, "#1: draft") {
		t.Fatalf("want the skip explained, got %q", log)
	}
}

// A failure must not advance the watermark, or the pull request is never retried.
func TestAFailedReviewIsRetriedNextPoll(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	client := &fakeClient{prs: []ghclient.PullRequest{pr(1, 0), pr(2, 1)}}

	var attempts []int
	failFirst := func(_ context.Context, n int) error {
		attempts = append(attempts, n)
		if n == 1 && len(attempts) == 1 {
			return errors.New("the API was unhappy")
		}
		return nil
	}
	opts := Options{Repo: ghclient.Repo{Owner: "o", Name: "r"}, StatePath: statePath, Log: io.Discard}
	if err := Poll(context.Background(), client, failFirst, opts); err != nil {
		t.Fatal(err)
	}
	if err := Poll(context.Background(), client, failFirst, opts); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(attempts) != "[1 2 1]" {
		t.Fatalf("want the failed pull request retried and the succeeded one not, got %v", attempts)
	}
}

func TestDryRunReviewsNothingAndWritesNoState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	client := &fakeClient{prs: []ghclient.PullRequest{pr(1, 0)}}

	reviewed, log := poll(t, client, statePath, func(o *Options) { o.DryRun = true })
	if len(reviewed) != 0 {
		t.Fatalf("a dry run reviews nothing, got %v", reviewed)
	}
	if !strings.Contains(log, "#1: would review") {
		t.Fatalf("want the intent logged, got %q", log)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatal("a dry run must not advance the watermark")
	}
}

// A corrupt watermark means "review everything once", not "stop": the cost is
// duplicate API calls, which the per-finding dedupe absorbs.
func TestACorruptStateFileDoesNotStopThePoller(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{prs: []ghclient.PullRequest{pr(1, 0)}}

	reviewed, log := poll(t, client, statePath, nil)
	if fmt.Sprint(reviewed) != "[1]" {
		t.Fatalf("want the pull request reviewed, got %v", reviewed)
	}
	if !strings.Contains(log, "treating every open pull request as new") {
		t.Fatalf("want the recovery explained, got %q", log)
	}
}

// A poller killed mid-write must not leave a truncated watermark behind.
func TestStateIsWrittenAtomically(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	client := &fakeClient{prs: []ghclient.PullRequest{pr(1, 0)}}
	poll(t, client, statePath, nil)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("a temporary file was left behind: %s", e.Name())
		}
	}
}

// The watermark is written after each review, not only at the end: a poll
// interrupted part-way through keeps the progress it made. Ordering alone would
// not have achieved that.
func TestProgressIsPersistedAsItHappens(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	client := &fakeClient{prs: []ghclient.PullRequest{pr(1, 0), pr(2, 5), pr(3, 10)}}

	// Fail on the third, as an interrupted poll would.
	stopAfterTwo := func(_ context.Context, n int) error {
		if n == 3 {
			return errors.New("interrupted")
		}
		return nil
	}
	opts := Options{Repo: ghclient.Repo{Owner: "o", Name: "r"}, StatePath: statePath, Log: io.Discard}
	if err := Poll(context.Background(), client, stopAfterTwo, opts); err != nil {
		t.Fatal(err)
	}

	// The next poll must review only the one that did not complete.
	var second []int
	if err := Poll(context.Background(), client, func(_ context.Context, n int) error {
		second = append(second, n)
		return nil
	}, opts); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(second) != "[3]" {
		t.Fatalf("want only the interrupted pull request retried, got %v", second)
	}
}
