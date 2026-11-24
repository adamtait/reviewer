// SPDX-License-Identifier: MIT

// Package api is the workspace's HTTP surface: the route table, the middleware
// every request passes through, and the handlers.
package api

import (
	"net/http"

	"github.com/adamtait/reviewer/internal/workspace/api/apitypes"
	"github.com/adamtait/reviewer/internal/workspace/obs"
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
	// Repo marks a route that carries a repository's data.
	Repo    bool
	Handler http.HandlerFunc
}

// Server holds the route table.
type Server struct {
	log     *obs.Logger
	version string
	routes  []Route
}

// New returns a Server with the routes that need nothing but the process: so
// far, the health address.
func New(log *obs.Logger, version string) *Server {
	s := &Server{log: log, version: version}
	s.Add(Route{Method: http.MethodGet, Pattern: "/healthz", Name: "health", Handler: s.health})
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
		mux.Handle(r.Method+" "+r.Pattern, s.wrap(r.Name, r.Handler))
	}
	// Everything the table does not name. The same body as "a repository you
	// cannot read", deliberately: an address that does not exist and an address
	// that is not yours to see must not look different.
	mux.Handle("/", s.wrap("unmatched", func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotFound, apitypes.CodeNotFound, "not found")
	}))
	return mux
}

// health reports that the process is up. It says nothing about GitHub or a model
// provider: diff, navigation and search must keep working when a provider is
// down, so a health check that failed with one would take them down with it.
func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, apitypes.Health{Status: "ok", Version: s.version})
}
