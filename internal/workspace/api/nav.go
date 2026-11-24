// SPDX-License-Identifier: MIT

package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/adamtait/reviewer/internal/workspace/api/apitypes"
	"github.com/adamtait/reviewer/internal/workspace/navigation"
	"github.com/adamtait/reviewer/internal/workspace/obs"
	"github.com/adamtait/reviewer/internal/workspace/source"
)

// maxReportBody bounds a "this was wrong" report.
const maxReportBody = 8 << 10

// maxPosition bounds a line or character number. No file the service holds
// comes near it.
const maxPosition = 1 << 30

// indexFor returns the commit's files and, if it has one, its index, with the
// index's status.
func (s *Server) indexFor(w http.ResponseWriter, r *http.Request) (*source.Commit, *navigation.Index, navigation.Status, bool) {
	commit, ok := s.commit(w, r)
	if !ok {
		return nil, nil, navigation.Status{}, false
	}
	repo, client, ok := s.asUser(w, r)
	if !ok {
		return nil, nil, navigation.Status{}, false
	}
	index, status, err := s.Navigation.Lookup(r.Context(), client, repo.Owner, repo.Name, r.PathValue("sha"))
	if err != nil {
		s.fail(w, r, err)
		return nil, nil, navigation.Status{}, false
	}
	return commit, index, status, true
}

// indexState says whether the commit has a navigation index, loading it if it
// has and it is not in memory.
func (s *Server) indexState(w http.ResponseWriter, r *http.Request) {
	_, _, status, ok := s.indexFor(w, r)
	if !ok {
		return
	}
	out := apitypes.IndexState{
		State: status.State, Reason: status.Reason, ToolVersion: status.ToolVersion, Documents: status.Documents,
	}
	if status.RunID != 0 {
		if connected, found := s.Access.Lookup(r.PathValue("owner"), r.PathValue("repo")); found {
			// Built from configuration and the run's number, never passed on
			// from GitHub's response: this is a link the browser will open.
			out.RunURL = fmt.Sprintf("%s/%s/%s/actions/runs/%d", strings.TrimRight(s.WebBaseURL, "/"),
				url.PathEscape(connected.Owner), url.PathEscape(connected.Name), status.RunID)
		}
	}
	obs.From(r.Context()).SetOutcome("index_" + status.State)
	WriteJSON(w, http.StatusOK, out)
}

// position reads the path, line and character a lookup asks about.
func position(w http.ResponseWriter, r *http.Request) (path string, line, character int, ok bool) {
	query := r.URL.Query()
	path = query.Get("path")
	line, lineErr := strconv.Atoi(query.Get("line"))
	character, characterErr := strconv.Atoi(query.Get("character"))
	if path == "" || len(path) > maxPath || lineErr != nil || characterErr != nil ||
		line < 0 || character < 0 || line > maxPosition || character > maxPosition {
		WriteError(w, http.StatusBadRequest, apitypes.CodeBadRequest,
			"a lookup needs ?path=, ?line= and ?character=, with both numbers counted from 0")
		return "", 0, 0, false
	}
	return path, line, character, true
}

// definition answers "where is this defined".
func (s *Server) definition(w http.ResponseWriter, r *http.Request) {
	path, line, character, ok := position(w, r)
	if !ok {
		return
	}
	commit, index, _, ok := s.indexFor(w, r)
	if !ok {
		return
	}
	found := navigation.FindDefinition(index, commit, path, line, character)
	// The label goes in the log line: the feedback week counts lookups by it.
	obs.From(r.Context()).SetLabel(found.Label)
	WriteJSON(w, http.StatusOK, apitypes.Definition{
		Label:     found.Label,
		Targets:   locations(found.Targets),
		Package:   found.Package,
		Version:   found.Version,
		Matches:   matches(found.Matches),
		Truncated: found.Truncated,
	})
}

// references answers "where is this used".
func (s *Server) references(w http.ResponseWriter, r *http.Request) {
	path, line, character, ok := position(w, r)
	if !ok {
		return
	}
	commit, index, _, ok := s.indexFor(w, r)
	if !ok {
		return
	}
	found := navigation.FindReferences(index, commit, path, line, character)
	obs.From(r.Context()).SetLabel(found.Label)
	WriteJSON(w, http.StatusOK, apitypes.References{
		Label:            found.Label,
		References:       locations(found.References),
		Truncated:        found.Truncated,
		Matches:          matches(found.Matches),
		MatchesTruncated: found.MatchesTruncated,
	})
}

func locations(in []navigation.Location) []apitypes.Location {
	out := make([]apitypes.Location, 0, len(in))
	for _, at := range in {
		out = append(out, apitypes.Location{
			Path: at.Path, Line: at.Line, Character: at.Character, EndLine: at.EndLine, EndCharacter: at.EndCharacter,
		})
	}
	return out
}

func matches(in []source.Match) []apitypes.Match {
	out := make([]apitypes.Match, 0, len(in))
	for _, m := range in {
		out = append(out, apitypes.Match{Path: m.Path, Line: m.Line, Text: m.Text})
	}
	return out
}

// report records that a user found a navigation result wrong.
//
// It is a log line and nothing else: the service has nowhere to keep it. The
// line names the lookup, so whoever reads the log can repeat it. It carries no
// text from the user and none from the file.
func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	var body apitypes.Report
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxReportBody))
	if err != nil || json.Unmarshal(raw, &body) != nil {
		WriteError(w, http.StatusBadRequest, apitypes.CodeBadRequest, "the request body must be a report")
		return
	}
	kinds := map[string]bool{apitypes.KindDefinition: true, apitypes.KindReferences: true}
	labels := map[string]bool{
		apitypes.LabelIndex: true, apitypes.LabelTextSearch: true, apitypes.LabelExternal: true, apitypes.LabelNone: true,
	}
	if !fullHash(body.SHA) || body.Path == "" || len(body.Path) > maxPath || !kinds[body.Kind] || !labels[body.Label] ||
		body.Line < 0 || body.Character < 0 || body.Line > maxPosition || body.Character > maxPosition {
		WriteError(w, http.StatusBadRequest, apitypes.CodeBadRequest,
			"a report names a commit by its full hash, a path, a line, a character, a kind and a label")
		return
	}
	fields := obs.From(r.Context())
	fields.SetSHA(body.SHA)
	fields.SetLabel(body.Label)
	fields.SetOutcome("reported_wrong")
	// A line of its own beside the request's, so the count of reports is a
	// count of one message and needs no knowledge of routes.
	s.log.Event(r.Context(), slog.LevelWarn, "navigation_reported_wrong",
		slog.String("owner", r.PathValue("owner")), slog.String("repo", r.PathValue("repo")),
		slog.String("sha", body.SHA), slog.String("path", body.Path),
		slog.Int("line", body.Line), slog.Int("character", body.Character),
		slog.String("kind", body.Kind), slog.String("label", body.Label))
	w.WriteHeader(http.StatusNoContent)
}
