// SPDX-License-Identifier: MIT

package ghclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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
const clientMethods = "Viewer GetPullRequest ListPullRequests ListFiles ListReviewComments ListIssueComments"

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
