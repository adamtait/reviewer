// SPDX-License-Identifier: MIT

package api

import (
	"encoding/json"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/adamtait/reviewer/internal/workspace/api/apitypes"
	"github.com/adamtait/reviewer/internal/workspace/obs"
	"github.com/adamtait/reviewer/internal/workspace/source"
)

const (
	// maxSearchBody bounds a search request.
	maxSearchBody = 8 << 10
	// minQuery is the shortest search text, in characters. One character
	// matches most lines of most files, and the answer would only ever be the
	// first 200 of them.
	minQuery = 2
	// maxQuery and maxPath bound the search text and a path, in bytes.
	maxQuery = 256
	maxPath  = 4096
)

// commit returns the files of the commit a request names, reading them from
// GitHub as the signed-in user if they are not in memory. It writes the
// response itself when it cannot.
func (s *Server) commit(w http.ResponseWriter, r *http.Request) (*source.Commit, bool) {
	sha := r.PathValue("sha")
	if !fullHash(sha) {
		// A branch name or a short hash is refused, not resolved: what is held
		// in memory under a name must be the commit that name means for good.
		WriteError(w, http.StatusBadRequest, apitypes.CodeBadRequest,
			"a commit is named by its full hash: 40 characters, 0-9 and a-f")
		return nil, false
	}
	repo, client, ok := s.asUser(w, r)
	if !ok {
		return nil, false
	}
	obs.From(r.Context()).SetSHA(sha)
	commit, err := s.Source.Commit(r.Context(), client, repo.Owner, repo.Name, sha)
	if err != nil {
		s.fail(w, r, err)
		return nil, false
	}
	return commit, true
}

// fullHash reports whether s is a full commit hash as GitHub writes one.
func fullHash(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// tree returns every path in a commit.
func (s *Server) tree(w http.ResponseWriter, r *http.Request) {
	commit, ok := s.commit(w, r)
	if !ok {
		return
	}
	entries := commit.Entries()
	out := apitypes.Tree{Entries: make([]apitypes.TreeEntry, 0, len(entries))}
	for _, e := range entries {
		out.Entries = append(out.Entries, apitypes.TreeEntry{Path: e.Path, Kind: string(e.Kind)})
	}
	WriteJSON(w, http.StatusOK, out)
}

// file returns one file's text, or why its text is not held.
func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" || len(path) > maxPath {
		WriteError(w, http.StatusBadRequest, apitypes.CodeBadRequest, "name the file with ?path=")
		return
	}
	commit, ok := s.commit(w, r)
	if !ok {
		return
	}
	kind, found := commit.Kind(path)
	if !found {
		notFound(w, r)
		return
	}
	text, _ := commit.Text(path)
	WriteJSON(w, http.StatusOK, apitypes.File{Path: path, Kind: string(kind), Text: text})
}

// search returns the lines of a commit's text that contain the query.
//
// The query is in the body. Nothing here puts it in the log.
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	var body apitypes.Search
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSearchBody))
	if err != nil || json.Unmarshal(raw, &body) != nil {
		WriteError(w, http.StatusBadRequest, apitypes.CodeBadRequest, "the request body must be {\"query\": \"...\"}")
		return
	}
	if utf8.RuneCountInString(body.Query) < minQuery || len(body.Query) > maxQuery || len(body.PathPrefix) > maxPath {
		WriteError(w, http.StatusBadRequest, apitypes.CodeBadRequest, "search for at least 2 characters")
		return
	}
	commit, ok := s.commit(w, r)
	if !ok {
		return
	}
	matches, truncated := commit.Search(source.Query{
		Text: body.Query, PathPrefix: body.PathPrefix, CaseSensitive: body.CaseSensitive,
	})
	out := apitypes.SearchResult{Matches: make([]apitypes.Match, 0, len(matches)), Truncated: truncated}
	for _, m := range matches {
		out.Matches = append(out.Matches, apitypes.Match{Path: m.Path, Line: m.Line, Text: m.Text})
	}
	WriteJSON(w, http.StatusOK, out)
}
