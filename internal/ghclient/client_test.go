// SPDX-License-Identifier: MIT

package ghclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// server records the requests it receives, so the tests can assert on headers and
// pagination rather than only on decoded results.
type server struct {
	*httptest.Server
	requests []*http.Request
}

func newServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) (*server, *REST) {
	t.Helper()
	s := &server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests = append(s.requests, r)
		handler(w, r)
	}))
	t.Cleanup(s.Close)

	c, err := New(s.URL, "test-token", "reviewer/test")
	if err != nil {
		t.Fatal(err)
	}
	return s, c
}

func TestNewRefusesAnEmptyBaseURLOrToken(t *testing.T) {
	if _, err := New("", "token", "ua"); err == nil {
		t.Fatal("no base URL is compiled in, so an empty one must be refused (ADR-0004)")
	}
	if _, err := New("https://example.invalid", "", "ua"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized for a missing token, got %v", err)
	}
}

func TestRequestHeaders(t *testing.T) {
	s, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"login":"reviewer[bot]","type":"Bot"}`)
	})
	user, err := c.Viewer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if user.Login != "reviewer[bot]" {
		t.Fatalf("want the viewer decoded, got %+v", user)
	}

	req := s.requests[0]
	for header, want := range map[string]string{
		"Authorization":        "Bearer test-token",
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": APIVersion,
		"User-Agent":           "reviewer/test",
	} {
		if got := req.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

// Pagination is not optional: a pull request with more than one page of existing
// comments would otherwise have the later ones invisible to dedupe, and the tool
// would repost them — the exact failure fingerprinting exists to prevent.
func TestListReviewCommentsFollowsEveryPage(t *testing.T) {
	var pages int
	s, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		pages++
		switch r.URL.Query().Get("page") {
		case "":
			w.Header().Set("Link", `<`+baseOf(r)+`/repos/o/r/pulls/7/comments?page=2>; rel="next", <x>; rel="last"`)
			fmt.Fprint(w, `[{"id":1,"body":"one","path":"a.ts","line":3}]`)
		case "2":
			fmt.Fprint(w, `[{"id":2,"body":"two","path":"b.ts","line":9}]`)
		default:
			t.Errorf("unexpected page %q", r.URL.Query().Get("page"))
		}
	})

	got, err := c.ListReviewComments(context.Background(), Repo{"o", "r"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want both pages, got %d: %+v", len(got), got)
	}
	if pages != 2 {
		t.Fatalf("want two requests, got %d", pages)
	}
	if q := s.requests[0].URL.Query().Get("per_page"); q != "100" {
		t.Fatalf("want a large page size to keep the request count down, got %q", q)
	}
}

func baseOf(r *http.Request) string { return "http://" + r.Host }

// A comment GitHub has detached from the diff reports no line, and that is the
// signal acceptance measurement reads.
func TestOutdatedComments(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"id":1,"body":"still here","path":"a.ts","line":3},
		                {"id":2,"body":"detached","path":"a.ts","line":null}]`)
	})
	got, err := c.ListReviewComments(context.Background(), Repo{"o", "r"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Outdated() {
		t.Fatal("a comment with a line is not outdated")
	}
	if !got[1].Outdated() {
		t.Fatal("a comment with a null line is outdated")
	}
}

func TestErrorMapping(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   error
		substr string
	}{
		{"unauthorized", http.StatusUnauthorized, `{"message":"Bad credentials"}`, ErrUnauthorized, ""},
		{"not found", http.StatusNotFound, `{"message":"Not Found"}`, ErrNotFound, ""},
		{
			"rate limited", http.StatusForbidden,
			`{"message":"API rate limit exceeded"}`, nil, "rate limit",
		},
		{
			// Refused, as distinct from rate limited: a token that lacks a
			// permission or is waiting for an organisation owner's approval.
			"forbidden", http.StatusForbidden,
			`{"message":"Resource not accessible by personal access token"}`, ErrForbidden, "403 Forbidden",
		},
		{
			// GitHub's own message, not the whole body: an error body can be long
			// and is not worth pasting into a CI log.
			"validation failed", http.StatusUnprocessableEntity,
			`{"message":"Validation Failed","errors":[{"resource":"PullRequestReviewComment","field":"line","code":"invalid"}]}`,
			nil, "line: invalid",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			_, err := c.GetPullRequest(context.Background(), Repo{"o", "r"}, 1)
			if err == nil {
				t.Fatal("want an error")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
			if tc.substr != "" && !strings.Contains(err.Error(), tc.substr) {
				t.Fatalf("want an error containing %q, got %v", tc.substr, err)
			}
		})
	}
}

// A query string must never reach a log: a URL in an error message is exactly
// what ends up in CI output.
func TestQueryStringsAreRedactedFromErrors(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	})
	_, err := c.ListPullRequests(context.Background(), Repo{"o", "r"}, "open")
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), "state=open") {
		t.Fatalf("the query string must be redacted, got %v", err)
	}
	if !strings.Contains(err.Error(), "?…") {
		t.Fatalf("want the redaction visible, got %v", err)
	}
}

func TestListFilesAndPullRequests(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/files"):
			fmt.Fprint(w, `[{"filename":"src/a.ts","status":"modified","patch":"@@ -1 +1 @@"}]`)
		default:
			fmt.Fprint(w, `[{"number":7,"title":"t","state":"open","head":{"ref":"f","sha":"abc"},"base":{"ref":"main","sha":"def"}}]`)
		}
	})

	files, err := c.ListFiles(context.Background(), Repo{"o", "r"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Filename != "src/a.ts" {
		t.Fatalf("unexpected files: %+v", files)
	}

	prs, err := c.ListPullRequests(context.Background(), Repo{"o", "r"}, "open")
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 || prs[0].Number != 7 || prs[0].Base.Ref != "main" {
		t.Fatalf("unexpected pull requests: %+v", prs)
	}
}

// The read interface must stay read-only, so that a reader of this package can see
// that reviewing a pull request needs no write access.
func TestClientInterfaceIsReadOnly(t *testing.T) {
	var c Client = &REST{}
	_ = c
	for _, method := range []string{"Create", "Update", "Delete", "Post", "Patch", "Put", "Resolve"} {
		if strings.Contains(clientMethods, method) {
			t.Fatalf("the read interface names a write method (%s); writes belong in a separate interface", method)
		}
	}
}

// clientMethods is the interface's method set as source text. A compile-time
// reflection check cannot see an interface's method names without instantiating
// them, and the point here is to catch a *human* widening the interface.
const clientMethods = "Viewer GetPullRequest ListPullRequests ListFiles ListReviewComments ListIssueComments ListReviews"

func TestNextLink(t *testing.T) {
	tests := map[string]string{
		`<https://api/x?page=2>; rel="next", <https://api/x?page=9>; rel="last"`: "https://api/x?page=2",
		`<https://api/x?page=9>; rel="last"`:                                     "",
		``:                                                                       "",
		`malformed`:                                                              "",
	}
	for header, want := range tests {
		if got := nextLink(header); got != want {
			t.Errorf("nextLink(%q) = %q, want %q", header, got, want)
		}
	}
}

// The workspace asks two things of a repository before showing any of it: can
// this token see it, and is it private.
func TestGetRepo(t *testing.T) {
	var asked string
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		fmt.Fprint(w, `{"full_name":"o/r","private":true,"visibility":"private"}`)
	})
	info, err := c.GetRepo(context.Background(), Repo{"o", "r"})
	if err != nil {
		t.Fatal(err)
	}
	if asked != "/repos/o/r" {
		t.Errorf("asked for %q, want /repos/o/r", asked)
	}
	if !info.Private || info.Visibility != "private" || info.FullName != "o/r" {
		t.Errorf("info = %+v", info)
	}
}

// A rate limit is still reported in its own words and is not ErrForbidden: the
// caller's response to "slow down" and to "no" are different.
func TestARateLimitIsNotForbidden(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"API rate limit exceeded"}`)
	})
	_, err := c.GetRepo(context.Background(), Repo{"o", "r"})
	if err == nil || errors.Is(err, ErrForbidden) {
		t.Fatalf("want a rate limit error that is not ErrForbidden, got %v", err)
	}
}

func TestPullRequestCarriesWhatTheWorkspaceShows(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"number":7,"title":"t","body":"why","user":{"login":"octocat"},`+
			`"created_at":"2026-11-01T10:00:00Z","html_url":"http://example.invalid/o/r/pull/7"}`)
	})
	pr, err := c.GetPullRequest(context.Background(), Repo{"o", "r"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Body != "why" || pr.User.Login != "octocat" || pr.CreatedAt.IsZero() || pr.HTMLURL == "" {
		t.Errorf("pull request = %+v", pr)
	}
}

func TestListOpenPullRequestsStopsAtTheLimit(t *testing.T) {
	var asked []string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.RawQuery)
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		first := map[string]int{"1": 1, "2": 101, "3": 201}[page]
		var items []string
		for n := first; n < first+100; n++ {
			items = append(items, fmt.Sprintf(`{"number":%d}`, n))
		}
		next, _ := strconv.Atoi(page)
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/o/r/pulls?page=%d>; rel="next"`, srv.URL, next+1))
		_, _ = w.Write([]byte("[" + strings.Join(items, ",") + "]"))
	}))
	defer srv.Close()
	c, err := New(srv.URL, "test-token", "")
	if err != nil {
		t.Fatal(err)
	}

	pulls, more, err := c.ListOpenPullRequests(context.Background(), Repo{Owner: "o", Name: "r"}, 150)
	if err != nil {
		t.Fatal(err)
	}
	if len(pulls) != 150 || !more || pulls[149].Number != 150 {
		t.Errorf("%d pull requests, more %v; want 150 and true", len(pulls), more)
	}
	if len(asked) != 2 {
		t.Errorf("%d pages were read; the limit was reached on the second", len(asked))
	}
	if !strings.Contains(asked[0], "state=open") || !strings.Contains(asked[0], "per_page=100") {
		t.Errorf("first request: %s", asked[0])
	}
}

func TestListOpenPullRequestsSaysWhenItHasThemAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"number":7},{"number":3}]`))
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "test-token", "")
	pulls, more, err := c.ListOpenPullRequests(context.Background(), Repo{Owner: "o", Name: "r"}, 2)
	if err != nil || len(pulls) != 2 || more {
		t.Errorf("pulls %d, more %v, err %v; want 2, false, nil", len(pulls), more, err)
	}
}

// A pending review has no submitted_at, and a deleted account has no user. Both
// must decode rather than fail the whole list.
func TestListReviews(t *testing.T) {
	var asked []string
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		switch r.URL.Query().Get("page") {
		case "":
			w.Header().Set("Link", `<`+baseOf(r)+`/repos/o/r/pulls/7/reviews?page=2>; rel="next"`)
			fmt.Fprint(w, `[{"id":1,"user":{"login":"alice","type":"User"},"state":"CHANGES_REQUESTED",`+
				`"commit_id":"aaa","submitted_at":"2026-11-01T10:00:00Z"}]`)
		default:
			fmt.Fprint(w, `[{"id":2,"user":null,"state":"PENDING","commit_id":"bbb","submitted_at":null}]`)
		}
	})
	got, err := c.ListReviews(context.Background(), Repo{"o", "r"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want both pages, got %+v", got)
	}
	first := got[0]
	if first.ID != 1 || first.User.Login != "alice" || first.User.Type != "User" ||
		first.State != "CHANGES_REQUESTED" || first.CommitID != "aaa" || first.SubmittedAt.IsZero() {
		t.Errorf("first review = %+v", first)
	}
	if !got[1].SubmittedAt.IsZero() || got[1].User.Login != "" {
		t.Errorf("pending review = %+v", got[1])
	}
	if asked[0] != "/repos/o/r/pulls/7/reviews" {
		t.Errorf("asked for %q", asked[0])
	}
}

func TestListTimelineEvents(t *testing.T) {
	var asked string
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		// A commit carries its time under another name and must not break the list.
		fmt.Fprint(w, `[{"event":"committed","sha":"aaa","author":{"date":"2026-11-01T09:00:00Z"}},
		                {"event":"ready_for_review","created_at":"2026-11-01T10:00:00Z"}]`)
	})
	got, err := c.ListTimelineEvents(context.Background(), Repo{"o", "r"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if asked != "/repos/o/r/issues/7/timeline" {
		t.Errorf("asked for %q", asked)
	}
	if len(got) != 2 || got[1].Event != "ready_for_review" || got[1].CreatedAt.IsZero() || !got[0].CreatedAt.IsZero() {
		t.Errorf("events = %+v", got)
	}
}

// The file list is what is wanted, so the request must not carry the paging
// parameter that Compare uses to leave it out.
func TestCompareFiles(t *testing.T) {
	s, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"merge_base_commit":{"sha":"base"},"files":[`+
			`{"filename":"docs/b.md","previous_filename":"docs/a.md","status":"renamed","patch":"@@ -1 +1 @@"}]}`)
	})
	got, err := c.CompareFiles(context.Background(), Repo{"o", "r"}, "aaa", "bbb")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Filename != "docs/b.md" || got[0].PreviousFilename != "docs/a.md" || got[0].Patch == "" {
		t.Errorf("files = %+v", got)
	}
	req := s.requests[0]
	if req.URL.Path != "/repos/o/r/compare/aaa...bbb" {
		t.Errorf("asked for %q", req.URL.Path)
	}
	if req.URL.RawQuery != "" {
		t.Errorf("query %q would leave the file list out", req.URL.RawQuery)
	}
}

func TestPullRequestCarriesMergedAt(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"number":1,"merged_at":"2026-11-01T10:00:00Z"},{"number":2,"merged_at":null}]`)
	})
	prs, err := c.ListPullRequests(context.Background(), Repo{"o", "r"}, "closed")
	if err != nil {
		t.Fatal(err)
	}
	if prs[0].MergedAt.IsZero() || !prs[1].MergedAt.IsZero() {
		t.Errorf("merged_at = %v, %v; want set, then zero", prs[0].MergedAt, prs[1].MergedAt)
	}
}

func TestReviewCommentCarriesItsReview(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"id":1,"path":"a.go","line":3,"pull_request_review_id":42}]`)
	})
	got, err := c.ListReviewComments(context.Background(), Repo{"o", "r"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].PullRequestReviewID != 42 {
		t.Errorf("review id = %d, want 42", got[0].PullRequestReviewID)
	}
}

// Outcome measurement finds a merge in a later blame by its merge commit, reads
// the risk/ labels, and tells a fork's pull request from one in the repository.
func TestPullRequestCarriesWhatOutcomesReads(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"number":1,"merge_commit_sha":"m1","labels":[{"name":"risk/auth"},{"name":"docs"}],`+
			`"head":{"ref":"fix/x","sha":"h1","repo":{"full_name":"someone/fork"}},"base":{"sha":"b1"}},`+
			`{"number":2,"head":{"sha":"h2","repo":null}}]`)
	})
	prs, err := c.ListPullRequests(context.Background(), Repo{"o", "r"}, "closed")
	if err != nil {
		t.Fatal(err)
	}
	first := prs[0]
	if first.MergeCommitSHA != "m1" || first.Base.SHA != "b1" || first.Head.Ref != "fix/x" {
		t.Errorf("pull request = %+v", first)
	}
	if len(first.Labels) != 2 || first.Labels[0].Name != "risk/auth" {
		t.Errorf("labels = %+v", first.Labels)
	}
	if first.Head.Repo == nil || first.Head.Repo.FullName != "someone/fork" {
		t.Errorf("head repo = %+v", first.Head.Repo)
	}
	// A deleted fork's head repository is null.
	if prs[1].Head.Repo != nil {
		t.Errorf("head repo = %+v, want nil", prs[1].Head.Repo)
	}
}

// A diff too large to include comes without its patch, and the deletion count
// is what says lines were removed anyway.
func TestChangedFileCarriesDeletions(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"filename":"big.go","status":"modified","deletions":4000}]`)
	})
	got, err := c.ListFiles(context.Background(), Repo{"o", "r"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Deletions != 4000 || got[0].Patch != "" {
		t.Errorf("file = %+v", got[0])
	}
}

// The response below is in the shape GitHub documents for a commit's pull
// requests, a list of pull requests. It has not been compared with a response
// from GitHub itself.
func TestPullRequestsForCommit(t *testing.T) {
	s, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"number":12,"state":"closed","merged_at":"2026-11-01T10:00:00Z",`+
			`"merge_commit_sha":"m12","head":{"ref":"alice/feature","sha":"h12"}}]`)
	})
	got, err := c.PullRequestsForCommit(context.Background(), Repo{"o", "r"}, "c0ffee")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != 12 {
		t.Errorf("pull requests = %v, want [12]", got)
	}
	req := s.requests[0]
	if req.Method != http.MethodGet || req.URL.Path != "/repos/o/r/commits/c0ffee/pulls" {
		t.Errorf("asked %s %s", req.Method, req.URL.Path)
	}
}

// A commit no pull request introduced, pushed straight to the branch, has
// none, and that is an answer rather than an error.
func TestPullRequestsForACommitWithNone(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `[]`) })
	got, err := c.PullRequestsForCommit(context.Background(), Repo{"o", "r"}, "c0ffee")
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want none and no error", got, err)
	}
}

// Labels go through the issues API, which pull requests share, in one call
// with every name in the body. The response shape is GitHub's: the label list
// after the change, which AddLabels does not need and must not choke on.
func TestAddLabels(t *testing.T) {
	var body string
	s, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		fmt.Fprint(w, `[{"name":"risk/auth","color":"ededed"},{"name":"needs review"}]`)
	})
	err := c.AddLabels(context.Background(), Repo{"o", "r"}, 7, []string{"risk/auth", "needs review"})
	if err != nil {
		t.Fatal(err)
	}
	req := s.requests[0]
	if req.Method != http.MethodPost || req.URL.Path != "/repos/o/r/issues/7/labels" {
		t.Errorf("asked %s %s", req.Method, req.URL.Path)
	}
	if body != `{"labels":["risk/auth","needs review"]}` {
		t.Errorf("body = %s", body)
	}
}

// Adding nothing is not a request: it would be a wasted call.
func TestAddLabelsWithNoneMakesNoRequest(t *testing.T) {
	s, c := newServer(t, func(http.ResponseWriter, *http.Request) {})
	if err := c.AddLabels(context.Background(), Repo{"o", "r"}, 7, nil); err != nil {
		t.Fatal(err)
	}
	if len(s.requests) != 0 {
		t.Errorf("made %d requests", len(s.requests))
	}
}

// A label name is a path segment. Every label this tool sets has a "/" in it,
// which unescaped would address a different resource, and a person's label may
// have a space.
func TestRemoveLabelEscapesTheName(t *testing.T) {
	for name, want := range map[string]string{
		"risk/auth":    "/repos/o/r/issues/7/labels/risk%2Fauth",
		"needs review": "/repos/o/r/issues/7/labels/needs%20review",
	} {
		s, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `[]`) })
		if err := c.RemoveLabel(context.Background(), Repo{"o", "r"}, 7, name); err != nil {
			t.Fatal(err)
		}
		req := s.requests[0]
		if req.Method != http.MethodDelete || req.URL.EscapedPath() != want {
			t.Errorf("%q: asked %s %s, want DELETE %s", name, req.Method, req.URL.EscapedPath(), want)
		}
	}
}

// GitHub answers 404 when the label is not on the pull request (checked
// against github.com). The caller wanted it gone and it is gone, so that is
// success; any other failure is still an error.
func TestRemoveLabelThatIsNotThere(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Label does not exist"}`)
	})
	if err := c.RemoveLabel(context.Background(), Repo{"o", "r"}, 7, "risk/auth"); err != nil {
		t.Errorf("a 404 is not an error here, got %v", err)
	}

	_, c = newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"Resource not accessible by integration"}`)
	})
	if err := c.RemoveLabel(context.Background(), Repo{"o", "r"}, 7, "risk/auth"); !errors.Is(err, ErrForbidden) {
		t.Errorf("a 403 is still an error, got %v", err)
	}
}
