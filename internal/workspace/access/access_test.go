// SPDX-License-Identifier: MIT

package access

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/ghclient"
	"github.com/adamtait/reviewer/internal/workspace/wsconfig"
)

// fakeGitHub answers GetRepo from a table and counts the calls.
type fakeGitHub struct {
	calls   int
	asked   []string
	answers map[string]func() (ghclient.RepoInfo, error)
}

func (f *fakeGitHub) GetRepo(_ context.Context, repo ghclient.Repo) (ghclient.RepoInfo, error) {
	f.calls++
	f.asked = append(f.asked, repo.String())
	if answer, ok := f.answers[repo.String()]; ok {
		return answer()
	}
	return ghclient.RepoInfo{}, ghclient.ErrNotFound
}

func private() (ghclient.RepoInfo, error) { return ghclient.RepoInfo{Private: true}, nil }

var repos = []wsconfig.Repository{{Owner: "Example", Name: "Alpha"}, {Owner: "Example", Name: "Beta"}}

func TestARepositoryThatIsNotConnectedIsRefusedWithoutAskingGitHub(t *testing.T) {
	gh := &fakeGitHub{}
	ok, err := New(repos, time.Minute, nil).CanRead(context.Background(), "octocat", gh, "example", "gamma")
	if ok || err != nil {
		t.Errorf("want false with no error, got %v, %v", ok, err)
	}
	if gh.calls != 0 {
		t.Errorf("GitHub was asked %d times about a repository that is not connected", gh.calls)
	}
}

func TestAYesIsBelievedForThePeriodAndNoLonger(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	gh := &fakeGitHub{answers: map[string]func() (ghclient.RepoInfo, error){"Example/Alpha": private}}
	c := New(repos, 5*time.Minute, nil)
	c.SetClock(func() time.Time { return now })
	ctx := context.Background()

	// Matched without regard to case, and asked under the configured spelling.
	if ok, err := c.CanRead(ctx, "octocat", gh, "EXAMPLE", "alpha"); !ok || err != nil {
		t.Fatalf("want access, got %v, %v", ok, err)
	}
	if gh.asked[0] != "Example/Alpha" {
		t.Errorf("GitHub was asked for %q, want the configured spelling", gh.asked[0])
	}
	if ok, _ := c.CanRead(ctx, "octocat", gh, "example", "alpha"); !ok || gh.calls != 1 {
		t.Errorf("want the answer reused within the period, got %v after %d calls", ok, gh.calls)
	}
	// The answer is one user's. Another user is asked about separately.
	if ok, _ := c.CanRead(ctx, "hubot", gh, "example", "alpha"); !ok || gh.calls != 2 {
		t.Errorf("want a second user asked about separately, got %v after %d calls", ok, gh.calls)
	}

	// Access is removed on GitHub. Within the period the old answer still holds;
	// after it, GitHub is asked again and the removal takes effect.
	gh.answers["Example/Alpha"] = func() (ghclient.RepoInfo, error) { return ghclient.RepoInfo{}, ghclient.ErrNotFound }
	now = now.Add(4 * time.Minute)
	if ok, _ := c.CanRead(ctx, "octocat", gh, "example", "alpha"); !ok {
		t.Error("within the period the cached answer should still hold")
	}
	now = now.Add(2 * time.Minute)
	if ok, err := c.CanRead(ctx, "octocat", gh, "example", "alpha"); ok || err != nil {
		t.Errorf("after the period, removed access must be refused, got %v, %v", ok, err)
	}
}

// A "no" is not remembered: access granted on GitHub takes effect at once.
func TestANoIsAskedAgain(t *testing.T) {
	gh := &fakeGitHub{answers: map[string]func() (ghclient.RepoInfo, error){}}
	c := New(repos, time.Minute, nil)
	ctx := context.Background()
	if ok, _ := c.CanRead(ctx, "octocat", gh, "example", "alpha"); ok {
		t.Fatal("want no access")
	}
	gh.answers["Example/Alpha"] = private
	if ok, _ := c.CanRead(ctx, "octocat", gh, "example", "alpha"); !ok {
		t.Error("want access as soon as GitHub grants it")
	}
}

// A token that lacks a permission, or is waiting for approval, gets a 403. That
// is "cannot read", not an error.
func TestForbiddenIsNoAccess(t *testing.T) {
	gh := &fakeGitHub{answers: map[string]func() (ghclient.RepoInfo, error){
		"Example/Alpha": func() (ghclient.RepoInfo, error) { return ghclient.RepoInfo{}, ghclient.ErrForbidden },
	}}
	ok, err := New(repos, time.Minute, nil).CanRead(context.Background(), "octocat", gh, "example", "alpha")
	if ok || err != nil {
		t.Errorf("want false with no error, got %v, %v", ok, err)
	}
}

// Every GitHub token can read a public repository. Serving one would let any
// GitHub account in, so it is refused for everyone and reported once.
func TestAPublicRepositoryIsRefusedForEveryone(t *testing.T) {
	var reported []string
	gh := &fakeGitHub{answers: map[string]func() (ghclient.RepoInfo, error){
		"Example/Alpha": func() (ghclient.RepoInfo, error) { return ghclient.RepoInfo{Private: false, Visibility: "public"}, nil },
	}}
	c := New(repos, time.Minute, func(owner, name string) { reported = append(reported, owner+"/"+name) })
	ctx := context.Background()

	for _, login := range []string{"octocat", "hubot", "octocat"} {
		if ok, err := c.CanRead(ctx, login, gh, "example", "alpha"); ok || err != nil {
			t.Errorf("%s: want a public repository refused, got %v, %v", login, ok, err)
		}
	}
	if len(reported) != 1 || reported[0] != "Example/Alpha" {
		t.Errorf("want the configuration error reported once, got %v", reported)
	}
	if gh.calls != 1 {
		t.Errorf("once known to be public, GitHub need not be asked again; asked %d times", gh.calls)
	}
}

func TestARejectedTokenIsNotTheSameAsNoAccess(t *testing.T) {
	gh := &fakeGitHub{answers: map[string]func() (ghclient.RepoInfo, error){
		"Example/Alpha": func() (ghclient.RepoInfo, error) { return ghclient.RepoInfo{}, ghclient.ErrUnauthorized },
	}}
	_, err := New(repos, time.Minute, nil).CanRead(context.Background(), "octocat", gh, "example", "alpha")
	if !errors.Is(err, ErrTokenRejected) {
		t.Errorf("want ErrTokenRejected, got %v", err)
	}
}

// When GitHub cannot be asked, there is no answer, and no answer is not "yes".
func TestAGitHubFailureIsAnErrorAndGrantsNothing(t *testing.T) {
	boom := errors.New("connection reset")
	gh := &fakeGitHub{answers: map[string]func() (ghclient.RepoInfo, error){
		"Example/Alpha": func() (ghclient.RepoInfo, error) { return ghclient.RepoInfo{}, boom },
	}}
	ok, err := New(repos, time.Minute, nil).CanRead(context.Background(), "octocat", gh, "example", "alpha")
	if ok || !errors.Is(err, boom) {
		t.Errorf("want false and the failure, got %v, %v", ok, err)
	}
}

func TestConnectedKeepsConfigurationOrder(t *testing.T) {
	c := New(repos, time.Minute, nil)
	got := c.Connected()
	if len(got) != 2 || got[0].Name != "Alpha" || got[1].Name != "Beta" {
		t.Errorf("Connected() = %+v", got)
	}
	if r, ok := c.Lookup("EXAMPLE", "BETA"); !ok || r.Name != "Beta" {
		t.Errorf("Lookup should match without regard to case, got %+v, %v", r, ok)
	}
}
