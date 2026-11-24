// SPDX-License-Identifier: MIT

// Package api is the workspace's HTTP surface: the route table, the middleware
// every request passes through, and the handlers.
package api

import (
	"net/http"
	"strings"

	"github.com/adamtait/reviewer/internal/workspace/access"
	"github.com/adamtait/reviewer/internal/workspace/api/apitypes"
	"github.com/adamtait/reviewer/internal/workspace/identity"
	"github.com/adamtait/reviewer/internal/workspace/obs"
	"github.com/adamtait/reviewer/internal/workspace/source"
)

// Route is one entry in the route table.
//
// The table is data, not a series of mux.Handle calls, because one test has to
// walk it: every route that carries a repository's data must refuse a user who
// cannot read that repository, and a test that lists the routes itself goes
// stale the day someone adds one.
type Route struct {
	// Method and Pattern are what http.ServeMux matches, for example "GET" and
	// "/api/v1/repos/{owner}/{repo}/pulls".
	Method  string
	Pattern string
	// Name identifies the route in the log. The feedback measures are counts
	// by this name, so it is stable and never contains a path value.
	Name string
	// Auth marks a route that needs a signed-in user.
	Auth bool
	// Repo marks a route that carries a repository's data. Its pattern has
	// {owner} and {repo}, and the middleware refuses it unless GitHub says the
	// signed-in user can read that repository. Repo implies Auth.
	Repo    bool
	Handler http.HandlerFunc
}

// Deps is what the server is built from.
type Deps struct {
	Log     *obs.Logger
	Version string
	// Identity and Cookies are sign-in. Everything else in the service is
	// handed a session and does not know how it came about.
	Identity identity.Provider
	Cookies  *identity.Cookies
	// Access lists the connected repositories.
	Access *access.Checker
	// Source holds commits' files in memory.
	Source *source.Store
	// WebBaseURL is GitHub's web address, which links to GitHub are built on.
	WebBaseURL string
	// Web serves the web application: its page for every address that is not
	// the API's, and its assets. Nil means the service has no page.
	Web http.Handler
}

// Server holds the route table.
type Server struct {
	Deps
	log    *obs.Logger
	routes []Route
}

// New returns a Server with the routes that exist so far.
func New(deps Deps) *Server {
	s := &Server{Deps: deps, log: deps.Log}
	s.Add(Route{Method: http.MethodGet, Pattern: "/healthz", Name: "health", Handler: s.health})
	s.Add(Route{Method: http.MethodPost, Pattern: "/auth/token", Name: "sign_in", Handler: s.signIn})
	s.Add(Route{Method: http.MethodPost, Pattern: "/auth/signout", Name: "sign_out", Handler: s.signOut})
	s.Add(Route{Method: http.MethodGet, Pattern: "/api/v1/me", Name: "me", Auth: true, Handler: s.me})

	const repo = "/api/v1/repos/{owner}/{repo}"
	s.Add(Route{Method: http.MethodGet, Pattern: repo + "/pulls", Name: "pull_list", Repo: true, Handler: s.listPulls})
	s.Add(Route{Method: http.MethodGet, Pattern: repo + "/pulls/{number}", Name: "pull_view", Repo: true, Handler: s.viewPull})
	s.Add(Route{Method: http.MethodGet, Pattern: repo + "/commits/{sha}/tree", Name: "tree", Repo: true, Handler: s.tree})
	s.Add(Route{Method: http.MethodGet, Pattern: repo + "/commits/{sha}/file", Name: "file", Repo: true, Handler: s.file})
	s.Add(Route{Method: http.MethodPost, Pattern: repo + "/commits/{sha}/search", Name: "search", Repo: true, Handler: s.search})
	return s
}

// Add appends a route to the table.
func (s *Server) Add(r Route) { s.routes = append(s.routes, r) }

// Routes returns the table, for the tests that walk it.
func (s *Server) Routes() []Route { return append([]Route(nil), s.routes...) }

// Handler builds the handler that serves every route in the table.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, r := range s.routes {
		mux.Handle(r.Method+" "+r.Pattern, s.wrap(r))
	}
	// Everything the table does not name. Under the API's own prefixes that is
	// "not found", with the same body as "a repository you cannot read",
	// deliberately: an address that does not exist and an address that is not
	// yours to see must not look different.
	unmatched := s.wrap(Route{Name: "unmatched", Handler: notFound})
	if s.Web == nil {
		mux.Handle("/", unmatched)
		return mux
	}
	// Anywhere else is the web application's: it does its own routing, so the
	// service answers every such address with the application's page. The page
	// holds nothing of any repository; what it shows it asks the API for.
	page := s.wrap(Route{Name: "web", Handler: s.Web.ServeHTTP})
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) {
			unmatched.ServeHTTP(w, r)
			return
		}
		page.ServeHTTP(w, r)
	}))
	return mux
}

// isAPIPath reports whether a path belongs to the service and not to the web
// application.
func isAPIPath(path string) bool {
	for _, prefix := range []string{"/api/", "/auth/", "/healthz"} {
		if strings.HasPrefix(path, prefix) || path == strings.TrimSuffix(prefix, "/") {
			return true
		}
	}
	return false
}

// health reports that the process is up. It says nothing about GitHub or a model
// provider: diff, navigation and search must keep working when a provider is
// down, so a health check that failed with one would take them down with it.
func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, apitypes.Health{Status: "ok", Version: s.Version})
}

// notFound is the one "not found" there is. A route the table does not have, a
// repository that is not connected, and a repository this user cannot read all
// answer with it, byte for byte.
func notFound(w http.ResponseWriter, _ *http.Request) {
	WriteError(w, http.StatusNotFound, apitypes.CodeNotFound, "not found")
}
