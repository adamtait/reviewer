// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/internal/config"
	"github.com/adamtait/reviewer/internal/ghclient"
	"github.com/adamtait/reviewer/internal/testfixture"
)

// A pull request third in a stack: its own base is the branch below it, and the
// stack's trunk is main. The pull_request event names main as its base_ref.
func stackedPR() ghclient.PullRequest {
	return ghclient.PullRequest{
		Number: 12,
		Head:   ghclient.Ref{Ref: "a3", SHA: "head12"},
		Base:   ghclient.Ref{Ref: "a2", SHA: "base12"},
		Stack:  &ghclient.Stack{Number: 13, Position: 3, Size: 3, Base: ghclient.Ref{Ref: "main", SHA: "trunk"}},
	}
}

func plainPR() ghclient.PullRequest {
	return ghclient.PullRequest{
		Number: 24,
		Head:   ghclient.Ref{Ref: "control", SHA: "head24"},
		Base:   ghclient.Ref{Ref: "main", SHA: "base24"},
	}
}

func TestReviewBase(t *testing.T) {
	testMerge12 := []string{"firstparent", "head12"}
	testMerge24 := []string{"firstparent", "head24"}
	cases := []struct {
		name      string
		flag      string
		pr        ghclient.PullRequest
		parents   []string
		want      string
		wantNotes int
	}{
		// The case this exists for: the Action passes --base origin/<base_ref>,
		// and for a stacked pull request base_ref is the trunk.
		{"stacked, trunk named, test merge checkout", "origin/main", stackedPR(), testMerge12, "firstparent", 1},
		{"stacked, own base named, test merge checkout", "origin/a2", stackedPR(), testMerge12, "firstparent", 1},
		{"stacked, no --base, test merge checkout", defaultBase, stackedPR(), testMerge12, "firstparent", 0},
		// Outside a stack the first parent is the base tip: the same files as
		// before, and nothing worth telling anyone.
		{"unstacked, base named, test merge checkout", "origin/main", plainPR(), testMerge24, "firstparent", 0},
		{"unstacked, refs spelling", "refs/remotes/origin/main", plainPR(), testMerge24, "firstparent", 0},
		// A checkout of the head itself, as reviewer watch or a person has.
		{"stacked, trunk named, head checkout", "origin/main", stackedPR(), []string{"parent"}, "origin/main", 1},
		{"stacked, own base named, head checkout", "origin/a2", stackedPR(), []string{"parent"}, "origin/a2", 0},
		{"no --base, head checkout", defaultBase, plainPR(), []string{"parent"}, "base24", 0},
		{"no --base, empty flag", "", plainPR(), nil, "base24", 0},
		// Explicit and unrelated: it wins, whatever the checkout.
		{"unrelated --base, test merge checkout", "origin/release", stackedPR(), testMerge12, "origin/release", 0},
		// A merge whose second parent is not this pull request's head is some
		// other merge, not GitHub's test merge for it.
		{"another merge", "origin/main", plainPR(), []string{"x", "y"}, "origin/main", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, notes := reviewBase(tc.flag, tc.pr, tc.parents)
			if got != tc.want {
				t.Errorf("base = %q, want %q", got, tc.want)
			}
			if len(notes) != tc.wantNotes {
				t.Errorf("notes = %q, want %d", notes, tc.wantNotes)
			}
		})
	}
}

// End to end through the pull request path, on a repository built the way GitHub
// builds test merge commits for a stack: each layer's merge commit has the merge
// commit of the layer below as its first parent. Measured on GitHub in
// docs/designs/briefs/pr-stacks-sandbox-results.md §7a.
func TestAStackedPullRequestIsReviewedAloneOnItsTestMergeCommit(t *testing.T) {
	root := t.TempDir()
	git := testfixture.Git(t, root)
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(msg string) string {
		t.Helper()
		git("add", "-A")
		git("commit", "-q", "-m", msg)
		return strings.TrimSpace(git("rev-parse", "HEAD"))
	}

	git("init", "-q", "-b", "main")
	write("base.ts", "export const base = 1;\n")
	fork := commit("trunk")

	git("checkout", "-q", "-b", "a1")
	write("a1.ts", "export const a1 = 1;\n")
	head1 := commit("layer 1")
	git("checkout", "-q", "-b", "a2")
	write("a2.ts", "export const a2 = 1;\n")
	head2 := commit("layer 2")

	// The trunk moves on after the stack branched, and an unstacked pull
	// request branches from it.
	git("checkout", "-q", "main")
	write("later.ts", "export const later = 1;\n")
	trunk := commit("trunk moves")
	git("checkout", "-q", "-b", "control")
	write("control.ts", "export const control = 1;\n")
	headC := commit("unstacked")

	// GitHub's test merge commits.
	git("checkout", "-q", "--detach", trunk)
	git("merge", "-q", "--no-ff", "-m", "merge layer 1", head1)
	merge1 := strings.TrimSpace(git("rev-parse", "HEAD"))
	git("merge", "-q", "--no-ff", "-m", "merge layer 2", head2)
	merge2 := strings.TrimSpace(git("rev-parse", "HEAD"))
	git("checkout", "-q", "--detach", trunk)
	git("merge", "-q", "--no-ff", "-m", "merge control", headC)
	mergeC := strings.TrimSpace(git("rev-parse", "HEAD"))
	git("update-ref", "refs/remotes/origin/main", trunk)
	git("update-ref", "refs/remotes/origin/a1", head1)

	pulls := map[string]ghclient.PullRequest{
		"2": {Number: 2, Head: ghclient.Ref{Ref: "a2", SHA: head2},
			// base.sha lags: GitHub had not refreshed it, as measured.
			Base:  ghclient.Ref{Ref: "a1", SHA: fork},
			Stack: &ghclient.Stack{Number: 1, Position: 2, Size: 2, Base: ghclient.Ref{Ref: "main", SHA: fork}}},
		"3": {Number: 3, Head: ghclient.Ref{Ref: "control", SHA: headC},
			Base: ghclient.Ref{Ref: "main", SHA: trunk}},
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		pr, ok := pulls[n]
		if !ok || !strings.HasPrefix(r.URL.Path, "/repos/o/r/pulls/") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(pr)
	}))
	t.Cleanup(api.Close)

	cfg := config.Config{Root: root}
	cfg.GitHub.APIBaseURL = api.URL
	cfg.GitHub.Repo = "o/r"
	secrets := config.Secrets{GitHubToken: "t"}

	review := func(checkout string, pr int, base string) ([]string, string, []string) {
		t.Helper()
		git("checkout", "-q", "--detach", checkout)
		files, used, notes, err := changedFiles(context.Background(),
			options{root: root, pr: pr, base: base}, cfg, secrets)
		if err != nil {
			t.Fatal(err)
		}
		var paths []string
		for _, f := range files {
			paths = append(paths, f.Path)
		}
		sort.Strings(paths)
		return paths, used, notes
	}

	// What the Action passes today for a stacked pull request.
	paths, used, notes := review(merge2, 2, "origin/main")
	if !reflect.DeepEqual(paths, []string{"a2.ts"}) {
		t.Errorf("stacked, --base origin/main: files = %v, want only the layer's own a2.ts", paths)
	}
	if used != merge1 {
		t.Errorf("stacked: base = %s, want the test merge commit's first parent %s", used, merge1)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "stacked on a1") {
		t.Errorf("stacked: notes = %q, want one saying what was done and why", notes)
	}

	// No --base: base.sha lags, and the first parent is still the right base.
	if paths, _, _ := review(merge2, 2, defaultBase); !reflect.DeepEqual(paths, []string{"a2.ts"}) {
		t.Errorf("stacked, no --base: files = %v, want [a2.ts]", paths)
	}

	// An unstacked pull request covers what it always did, without a note.
	paths, _, notes = review(mergeC, 3, "origin/main")
	if !reflect.DeepEqual(paths, []string{"control.ts"}) || len(notes) != 0 {
		t.Errorf("unstacked: files = %v, notes = %q; want [control.ts] and no notes", paths, notes)
	}

	// On the head itself there is no test merge to measure from. The pull
	// request's own branch below gives its own change; the trunk does not, and
	// the run says so.
	if paths, _, _ := review(head2, 2, "origin/a1"); !reflect.DeepEqual(paths, []string{"a2.ts"}) {
		t.Errorf("head checkout, --base origin/a1: files = %v, want [a2.ts]", paths)
	}
	paths, _, notes = review(head2, 2, "origin/main")
	if !reflect.DeepEqual(paths, []string{"a1.ts", "a2.ts"}) || len(notes) != 1 {
		t.Errorf("head checkout, --base origin/main: files = %v, notes = %q; want both layers and a warning", paths, notes)
	}
}
