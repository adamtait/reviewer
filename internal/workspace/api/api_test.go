// SPDX-License-Identifier: MIT

package api

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/workspace/access"
	"github.com/adamtait/reviewer/internal/workspace/api/apitypes"
	"github.com/adamtait/reviewer/internal/workspace/identity"
	"github.com/adamtait/reviewer/internal/workspace/navigation"
	"github.com/adamtait/reviewer/internal/workspace/obs"
	"github.com/adamtait/reviewer/internal/workspace/source"
	"github.com/adamtait/reviewer/internal/workspace/testgithub"
	"github.com/adamtait/reviewer/internal/workspace/wsconfig"
)

var update = flag.Bool("update", false, "rewrite the example responses under testdata/examples")

// world is a server over a stand-in GitHub with three connected repositories:
//
//	example/alpha   private, readable by alice
//	example/beta    private, readable by alice and carol
//	example/open    public
//
// and a fourth, example/hidden, that exists on GitHub and is not connected.
type world struct {
	t      *testing.T
	gh     *testgithub.Server
	server *Server
	cache  *source.Cache
	log    *bytes.Buffer
	http   http.Handler
}

// roomy is limits no test fixture reaches.
var roomy = source.Limits{ArchiveBytes: 1 << 20, FileBytes: 1 << 16}

func newWorld(t *testing.T) *world {
	t.Helper()
	return newWorldWith(t, roomy, time.Minute)
}

// newWorldWith is newWorld with chosen limits on a commit's size and on how
// long a fetch from GitHub may take.
func newWorldWith(t *testing.T, limits source.Limits, fetchTimeout time.Duration) *world {
	t.Helper()
	gh := testgithub.New(t)
	gh.SetRepo("example", "alpha", testgithub.Repo{Private: true, Readers: []string{"alice"}})
	gh.SetRepo("example", "beta", testgithub.Repo{Private: true, Readers: []string{"alice", "carol"}})
	gh.SetRepo("example", "open", testgithub.Repo{Private: false})
	gh.SetRepo("example", "hidden", testgithub.Repo{Private: true, Readers: []string{"alice", "carol"}})

	repos := []wsconfig.Repository{
		{Owner: "example", Name: "alpha"}, {Owner: "example", Name: "beta"}, {Owner: "example", Name: "open"},
	}
	checker := access.New(repos, time.Minute, nil)
	cookies, err := identity.NewCookies(bytes.Repeat([]byte{7}, 32), true)
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	cache := source.NewCache(1<<22, fetchTimeout)
	store := source.NewStore(cache, limits)
	server := New(Deps{
		Log:     obs.New(&log, obs.NewRedactor()),
		Version: "test-version",
		Identity: &identity.TokenMethod{
			NewClient: gh.Client, Access: checker, SessionLength: time.Hour,
		},
		Cookies: cookies,
		Access:  checker,
		Source:  store,
		Navigation: navigation.NewService(navigation.Config{
			WorkflowFile: "review-index.yml", ArtifactName: "review-index", IndexBytes: 1 << 20,
		}, cache, store),
		WebBaseURL: "https://github.example",
	})
	return &world{t: t, gh: gh, server: server, cache: cache, log: &log}
}

// do sends one request. A non-GET carries the workspace's header unless bare.
func (w *world) do(method, path string, body any, cookie *http.Cookie, bare bool) *httptest.ResponseRecorder {
	w.t.Helper()
	if w.http == nil {
		w.http = w.server.Handler()
	}
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			w.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if method != http.MethodGet && !bare {
		req.Header.Set(HeaderRequest, "1")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	w.http.ServeHTTP(rec, req)
	return rec
}

// signIn registers a user on the stand-in and returns their session cookie.
func (w *world) signIn(login string) *http.Cookie {
	w.t.Helper()
	rec := w.do(http.MethodPost, "/auth/token", apitypes.TokenRequest{Token: w.gh.AddUser(login)}, nil, false)
	if rec.Code != http.StatusNoContent {
		w.t.Fatalf("sign-in as %s: status %d: %s", login, rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == identity.CookieName {
			return c
		}
	}
	w.t.Fatalf("sign-in as %s set no session cookie", login)
	return nil
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body apitypes.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("not an error body: %v: %s", err, rec.Body.String())
	}
	return body.Error.Code
}

// example compares a response with its recorded example, or rewrites the
// example under -update. The web application's tests are served these same
// files, so a field that changes here fails a test on whichever side was not
// updated.
func example(t *testing.T, name string, rec *httptest.ResponseRecorder) {
	t.Helper()
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, rec.Body.Bytes(), "", "  "); err != nil {
		t.Fatalf("response is not JSON: %v: %s", err, rec.Body.String())
	}
	got := strings.TrimSpace(pretty.String()) + "\n"
	path := filepath.Join("testdata", "examples", name+".json")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no recorded example for %s; run the tests with -update to record it: %v", name, err)
	}
	if got != string(want) {
		t.Errorf("%s does not match its recorded example.\n--- got ---\n%s--- want ---\n%s", name, got, want)
	}
}

func logLines(t *testing.T, log *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(log.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line is not JSON: %v: %s", err, line)
		}
		out = append(out, m)
	}
	return out
}

func TestHealthAnswersWithoutNeedingAnythingElse(t *testing.T) {
	w := newWorld(t)
	rec := w.do(http.MethodGet, "/healthz", nil, nil, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	example(t, "health", rec)
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("API responses must not be kept by a shared cache")
	}
	lines := logLines(t, w.log)
	if len(lines) != 1 || lines[0]["route"] != "health" || lines[0]["status"] != float64(200) {
		t.Fatalf("want exactly one log line for the request, got %v", lines)
	}
	if id := rec.Header().Get(HeaderRequestID); id == "" || lines[0]["request_id"] != id {
		t.Errorf("the response's request id %q should be the log line's %v", id, lines[0]["request_id"])
	}
	if len(w.gh.Calls()) != 0 {
		t.Error("the health address must not depend on GitHub")
	}
}

func TestAnUnknownAddressIsNotFound(t *testing.T) {
	w := newWorld(t)
	rec := w.do(http.MethodGet, "/api/v1/nothing/here", nil, nil, false)
	if rec.Code != http.StatusNotFound || errorCode(t, rec) != apitypes.CodeNotFound {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	example(t, "error_not_found", rec)
	if lines := logLines(t, w.log); len(lines) != 1 || lines[0]["route"] != "unmatched" {
		t.Errorf("want one line with route \"unmatched\", got %v", lines)
	}
}

func TestAPanicBecomesAnInternalError(t *testing.T) {
	w := newWorld(t)
	w.server.Add(Route{Method: http.MethodGet, Pattern: "/boom", Name: "boom", Handler: func(http.ResponseWriter, *http.Request) {
		panic("held a secret: do not show this")
	}})
	rec := w.do(http.MethodGet, "/boom", nil, nil, false)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("the panic's text reached the caller: %s", rec.Body.String())
	}
	lines := logLines(t, w.log)
	if len(lines) != 2 || lines[0]["msg"] != "panic" || lines[1]["status"] != float64(500) {
		t.Errorf("want a panic line and a request line, got %v", lines)
	}
}

func TestSignInSetsASessionAndMeListsOnlyWhatTheUserCanRead(t *testing.T) {
	w := newWorld(t)

	alice := w.signIn("alice")
	if !alice.HttpOnly || !alice.Secure || alice.SameSite != http.SameSiteLaxMode {
		t.Errorf("session cookie attributes: %+v", alice)
	}
	rec := w.do(http.MethodGet, "/api/v1/me", nil, alice, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: status %d: %s", rec.Code, rec.Body.String())
	}
	// alpha and beta. Not the public repository, and not the one that is on
	// GitHub but was never connected.
	example(t, "me", rec)

	// carol reads beta alone, and her list says nothing of alpha.
	var me apitypes.Me
	rec = w.do(http.MethodGet, "/api/v1/me", nil, w.signIn("carol"), false)
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.Login != "carol" || len(me.Repositories) != 1 || me.Repositories[0].Name != "beta" {
		t.Errorf("carol's list should hold beta alone, got %+v", me)
	}

	// The sign-in's log line names the user and never the token.
	if strings.Contains(w.log.String(), testgithub.Token("alice")) {
		t.Error("a token reached the log")
	}
}

func TestEachSignInRefusalHasItsOwnCode(t *testing.T) {
	w := newWorld(t)
	w.gh.AddUser("bob") // a real user who can read nothing connected and private
	for name, tc := range map[string]struct {
		token string
		code  string
	}{
		"a classic token":                  {"gh" + "p_classic", apitypes.CodeNotFineGrained},
		"a token GitHub does not know":     {testgithub.Token("nobody"), apitypes.CodeRejected},
		"a valid token that reads nothing": {testgithub.Token("bob"), apitypes.CodeNoRepository},
	} {
		rec := w.do(http.MethodPost, "/auth/token", apitypes.TokenRequest{Token: tc.token}, nil, false)
		if rec.Code != http.StatusUnauthorized || errorCode(t, rec) != tc.code {
			t.Errorf("%s: status %d, body %s; want 401 %s", name, rec.Code, rec.Body.String(), tc.code)
		}
		if len(rec.Result().Cookies()) != 0 {
			t.Errorf("%s: a refused sign-in set a cookie", name)
		}
	}
	rec := w.do(http.MethodPost, "/auth/token", "not an object", nil, false)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a malformed body: status %d, want 400", rec.Code)
	}
}

// When GitHub cannot be asked, the answer is "try again", not "bad token".
func TestSignInWhenGitHubIsDown(t *testing.T) {
	w := newWorld(t)
	token := w.gh.AddUser("alice")
	w.gh.Close()
	rec := w.do(http.MethodPost, "/auth/token", apitypes.TokenRequest{Token: token}, nil, false)
	if rec.Code != http.StatusBadGateway || errorCode(t, rec) != apitypes.CodeGitHubFailed {
		t.Errorf("status %d, body %s; want 502 github_failed", rec.Code, rec.Body.String())
	}
}

func TestARequestWithoutASessionIsSignedOut(t *testing.T) {
	w := newWorld(t)
	rec := w.do(http.MethodGet, "/api/v1/me", nil, nil, false)
	if rec.Code != http.StatusUnauthorized || errorCode(t, rec) != apitypes.CodeSignedOut {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	example(t, "error_signed_out", rec)

	forged := &http.Cookie{Name: identity.CookieName, Value: "bm90LWEtcmVhbC1jb29raWU"}
	if rec := w.do(http.MethodGet, "/api/v1/me", nil, forged, false); rec.Code != http.StatusUnauthorized {
		t.Errorf("a forged cookie: status %d, want 401", rec.Code)
	}
}

// A token revoked on GitHub ends the session at the next question GitHub is
// asked, and the cookie is removed so the page stops presenting it.
func TestARevokedTokenSignsTheUserOut(t *testing.T) {
	w := newWorld(t)
	w.server.Add(probe())
	alice := w.signIn("alice")
	// A repository alice has not been checked against yet, so GitHub is asked.
	w.gh.RevokeUser("alice")
	checker := w.server.Access
	checker.SetClock(func() time.Time { return time.Now().Add(time.Hour) }) // past every cached yes
	rec := w.do(http.MethodGet, "/api/v1/repos/example/alpha/probe", nil, alice, false)
	if rec.Code != http.StatusUnauthorized || errorCode(t, rec) != apitypes.CodeSignedOut {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == identity.CookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("want the session cookie removed")
	}
}

func TestSignOutRemovesTheCookie(t *testing.T) {
	w := newWorld(t)
	rec := w.do(http.MethodPost, "/auth/signout", nil, w.signIn("alice"), false)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
	if c := rec.Result().Cookies(); len(c) != 1 || c[0].MaxAge >= 0 {
		t.Errorf("want the cookie expired, got %+v", c)
	}
}

// Another site's page can make a browser send a POST with the user's cookie. It
// cannot add this header.
func TestANonGetWithoutTheWorkspaceHeaderIsRefused(t *testing.T) {
	w := newWorld(t)
	token := w.gh.AddUser("alice")
	rec := w.do(http.MethodPost, "/auth/token", apitypes.TokenRequest{Token: token}, nil, true)
	if rec.Code != http.StatusBadRequest || errorCode(t, rec) != apitypes.CodeBadRequest {
		t.Errorf("status %d: %s; want 400", rec.Code, rec.Body.String())
	}
	if len(w.gh.Calls()) != 0 {
		t.Error("a request refused for its missing header still reached GitHub")
	}
}

// probe is a stand-in for a route that carries a repository's data.
func probe() Route {
	return Route{Method: http.MethodGet, Pattern: "/api/v1/repos/{owner}/{repo}/probe", Name: "probe", Repo: true,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			session, _ := SessionFrom(r.Context())
			WriteJSON(w, http.StatusOK, map[string]string{"shown_to": session.Login})
		}}
}

var pathValue = regexp.MustCompile(`\{[a-z]+\}`)

// fill turns a route's pattern into an address for one repository.
func fill(pattern, owner, repo string) string {
	return pathValue.ReplaceAllStringFunc(pattern, func(name string) string {
		switch name {
		case "{owner}":
			return owner
		case "{repo}":
			return repo
		case "{sha}":
			return strings.Repeat("a", 40)
		default:
			return "1"
		}
	})
}

// The access test. It reads the route table, so a route added later is covered
// without anyone remembering to add it here.
//
// carol can read example/beta and nothing else. For every route that carries a
// repository's data she must get, for a repository she cannot read, exactly the
// response she would get for one that is not connected, and for an address that
// does not exist: there must be no way to tell which is which.
func TestNoRouteShowsARepositoryToAUserWithoutAccess(t *testing.T) {
	w := newWorld(t)
	w.server.Add(probe())
	carol := w.signIn("carol")
	absent := w.do(http.MethodGet, "/api/v1/no/such/address", nil, carol, false)

	walked := 0
	for _, route := range w.server.Routes() {
		if !route.Repo {
			continue
		}
		walked++
		for name, repo := range map[string]string{
			"a private repository she cannot read": "alpha",
			"a public repository":                  "open",
			"a repository that is not connected":   "hidden",
			"a repository that does not exist":     "nonexistent",
		} {
			rec := w.do(route.Method, fill(route.Pattern, "example", repo), nil, carol, false)
			if rec.Code != http.StatusNotFound {
				t.Errorf("route %s, %s: status %d, want 404", route.Name, name, rec.Code)
			}
			if rec.Body.String() != absent.Body.String() {
				t.Errorf("route %s, %s: the body differs from an address that does not exist:\n%s", route.Name, name, rec.Body.String())
			}
		}
		// With no session at all, the answer is "sign in", and still nothing of
		// the repository.
		if rec := w.do(route.Method, fill(route.Pattern, "example", "beta"), nil, nil, false); rec.Code != http.StatusUnauthorized {
			t.Errorf("route %s with no session: status %d, want 401", route.Name, rec.Code)
		}
	}
	if walked == 0 {
		t.Fatal("no route carrying repository data was walked, so the test proved nothing")
	}

	// And the one she can read is shown, so the test is not passing by refusing everyone.
	rec := w.do(http.MethodGet, "/api/v1/repos/example/beta/probe", nil, carol, false)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "carol") {
		t.Errorf("carol should be shown the repository she can read: status %d: %s", rec.Code, rec.Body.String())
	}
	// GitHub's spelling does not matter: the check is not defeated by case.
	if rec := w.do(http.MethodGet, "/api/v1/repos/EXAMPLE/ALPHA/probe", nil, carol, false); rec.Code != http.StatusNotFound {
		t.Errorf("a differently-cased address: status %d, want 404", rec.Code)
	}
}

// The log line for a request on a repository route says who asked about what.
func TestARepositoryRequestIsLoggedWithItsUserAndRepository(t *testing.T) {
	w := newWorld(t)
	w.server.Add(probe())
	alice := w.signIn("alice")
	w.log.Reset()
	w.do(http.MethodGet, "/api/v1/repos/example/alpha/probe", nil, alice, false)
	lines := logLines(t, w.log)
	if len(lines) != 1 {
		t.Fatalf("want one line, got %d", len(lines))
	}
	line := lines[0]
	if line["route"] != "probe" || line["user"] != "alice" || line["owner"] != "example" || line["repo"] != "alpha" {
		t.Errorf("log line = %v", line)
	}
}

func TestTheRouteTableMarksWhatNeedsASession(t *testing.T) {
	w := newWorld(t)
	want := map[string]bool{
		"health": false, "sign_in": false, "sign_out": false, "me": true,
		"pull_list": true, "pull_view": true, "tree": true, "file": true, "search": true,
		"index_state": true, "definition": true, "references": true, "navigation_report": true,
	}
	for _, route := range w.server.Routes() {
		needs, known := want[route.Name]
		if !known {
			t.Errorf("route %q is not in this test's table: say whether it needs a session", route.Name)
			continue
		}
		if (route.Auth || route.Repo) != needs {
			t.Errorf("route %q: needs a session = %v, want %v", route.Name, route.Auth || route.Repo, needs)
		}
	}
}

// With a web application, every address outside the API's own prefixes gets
// the application's page, signed in or not. The page holds nothing of any
// repository, so serving it to anyone gives nothing away; what it shows it has
// to ask the API for.
func TestAddressesOutsideTheAPIGetTheWebApplication(t *testing.T) {
	w := newWorld(t)
	w.server.Web = http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = rw.Write([]byte("<!doctype html><title>the page</title>"))
	})

	for _, address := range []string{"/", "/signin", "/example/alpha/pull/12", "/example/hidden/pull/1", "/apiary", "/authors"} {
		rec := w.do(http.MethodGet, address, nil, nil, false)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "the page") {
			t.Errorf("%s: status %d, body %q; want the page", address, rec.Code, rec.Body.String())
		}
	}
	// The API's prefixes keep their own "not found", in the API's own format.
	for _, address := range []string{"/api/v1/nothing/here", "/api/", "/api", "/auth/nothing", "/healthz/extra"} {
		rec := w.do(http.MethodGet, address, nil, nil, false)
		if rec.Code != http.StatusNotFound || errorCode(t, rec) != apitypes.CodeNotFound {
			t.Errorf("%s: status %d, body %q; want the API's 404", address, rec.Code, rec.Body.String())
		}
	}
	// And the routes themselves are untouched.
	if rec := w.do(http.MethodGet, "/healthz", nil, nil, false); rec.Code != http.StatusOK {
		t.Errorf("/healthz: status %d", rec.Code)
	}
	if rec := w.do(http.MethodGet, "/api/v1/me", nil, nil, false); rec.Code != http.StatusUnauthorized {
		t.Errorf("/api/v1/me with no session: status %d, want 401", rec.Code)
	}

	lines := logLines(t, w.log)
	if lines[0]["route"] != "web" {
		t.Errorf("a page request is logged as %v, want route \"web\"", lines[0]["route"])
	}
	for _, line := range lines {
		if _, has := line["path"]; has {
			t.Error("a request's path reached the log: it can name a repository, a file and a line")
		}
	}
}

func TestEveryResponseForbidsContentSniffing(t *testing.T) {
	w := newWorld(t)
	for _, address := range []string{"/healthz", "/api/v1/me", "/api/v1/nothing"} {
		if got := w.do(http.MethodGet, address, nil, nil, false).Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options %q", address, got)
		}
	}
}
