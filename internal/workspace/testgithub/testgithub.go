// SPDX-License-Identifier: MIT

// Package testgithub is a small stand-in for GitHub's API, for the workspace's
// tests.
//
// It answers as GitHub does where the workspace's safety depends on it: an
// unknown token is 401, and a private repository a user cannot read is 404,
// exactly as if it did not exist. It is used only from tests.
package testgithub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/adamtait/reviewer/internal/ghclient"
)

// Repo is one repository the stand-in knows.
type Repo struct {
	Private bool
	// Readers are the logins that can read it. A public repository is readable
	// by every valid token, whatever this says.
	Readers []string
}

// Server is the stand-in.
type Server struct {
	*httptest.Server

	mu     sync.Mutex
	tokens map[string]string // token -> login
	repos  map[string]Repo   // "owner/name", lower-cased
	calls  []string
}

// New starts a stand-in that is closed when the test ends.
func New(t *testing.T) *Server {
	t.Helper()
	s := &Server{tokens: map[string]string{}, repos: map[string]Repo{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// Token returns a credential in the shape of a fine-grained token for a made-up
// user. Built here rather than written out in a test, so no test file holds a
// string that looks like a credential.
func Token(name string) string { return "github" + "_pat_" + name }

// AddUser registers a login and returns the token that acts as them.
func (s *Server) AddUser(login string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	token := Token(login)
	s.tokens[token] = login
	return token
}

// RevokeUser makes a user's token stop working.
func (s *Server) RevokeUser(login string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, Token(login))
}

// SetRepo adds or replaces a repository.
func (s *Server) SetRepo(owner, name string, repo Repo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repos[strings.ToLower(owner+"/"+name)] = repo
}

// Calls returns every request path served so far.
func (s *Server) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

// Client returns a GitHub client that talks to the stand-in with a token.
func (s *Server) Client(token string) (*ghclient.REST, error) {
	return ghclient.New(s.URL, token, "workspace-test")
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.calls = append(s.calls, r.Method+" "+r.URL.Path)
	login, known := s.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	s.mu.Unlock()

	if !known {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case r.URL.Path == "/user":
		writeJSON(w, http.StatusOK, map[string]string{"login": login, "type": "User"})
	case len(parts) == 3 && parts[0] == "repos":
		s.mu.Lock()
		repo, exists := s.repos[strings.ToLower(parts[1]+"/"+parts[2])]
		s.mu.Unlock()
		if !exists || (repo.Private && !contains(repo.Readers, login)) {
			// GitHub does not say which: a repository you cannot see and one
			// that does not exist are the same answer.
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
			return
		}
		visibility := "public"
		if repo.Private {
			visibility = "private"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"full_name": parts[1] + "/" + parts[2], "private": repo.Private, "visibility": visibility,
		})
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
