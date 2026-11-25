// SPDX-License-Identifier: MIT

package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/adamtait/reviewer/internal/ghclient"
	"github.com/adamtait/reviewer/internal/workspace/api/apitypes"
	"github.com/adamtait/reviewer/internal/workspace/catalog"
	"github.com/adamtait/reviewer/internal/workspace/obs"
	"github.com/adamtait/reviewer/internal/workspace/source"
)

// asUser returns the repository a request is about and a GitHub client that
// acts as the signed-in user. It is for handlers on Repo routes, where the
// middleware has already checked that this user may read this repository.
//
// The repository is returned under the spelling in the configuration, whatever
// the address used, so the request GitHub sees is the one the operator wrote
// down.
func (s *Server) asUser(w http.ResponseWriter, r *http.Request) (ghclient.Repo, *ghclient.REST, bool) {
	session, signedIn := SessionFrom(r.Context())
	connected, found := s.Access.Lookup(r.PathValue("owner"), r.PathValue("repo"))
	if !signedIn || !found {
		// Not reachable through the middleware. Answered as "not found" all
		// the same, so a handler wired to the wrong kind of route shows nothing.
		notFound(w, r)
		return ghclient.Repo{}, nil, false
	}
	client, err := s.Identity.Client(session)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, apitypes.CodeInternal, "internal error")
		return ghclient.Repo{}, nil, false
	}
	return ghclient.Repo{Owner: connected.Owner, Name: connected.Name}, client, true
}

// fail answers a request whose read of GitHub, or of a commit's files, did not
// succeed. Every handler that reads either ends its error path here, so one
// kind of failure has one answer.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	fields := obs.From(r.Context())
	switch {
	case errors.Is(err, ghclient.ErrUnauthorized):
		// The token stopped working between the access check and this call.
		s.signedOut(w)
	case errors.Is(err, ghclient.ErrNotFound):
		notFound(w, r)
	case errors.Is(err, source.ErrTooLarge):
		fields.SetOutcome(apitypes.CodeTooLarge)
		WriteError(w, http.StatusRequestEntityTooLarge, apitypes.CodeTooLarge,
			"This commit holds more text than the workspace is configured to read.")
	case errors.Is(err, context.DeadlineExceeded):
		fields.SetOutcome(apitypes.CodeGitHubTimeout)
		WriteError(w, http.StatusGatewayTimeout, apitypes.CodeGitHubTimeout,
			"GitHub did not answer in time; try again")
	case errors.Is(err, context.Canceled):
		// The browser went away. Nobody reads this response; the outcome is
		// for the log, so an abandoned request is not counted as a failure.
		fields.SetOutcome("abandoned")
		WriteError(w, http.StatusBadGateway, apitypes.CodeGitHubFailed, "the request was abandoned")
	case errors.Is(err, ghclient.ErrForbidden):
		fields.SetOutcome("github_forbidden")
		WriteError(w, http.StatusBadGateway, apitypes.CodeGitHubFailed,
			"GitHub refused this request. The token may lack one of the read permissions the sign-in page lists.")
	default:
		fields.SetOutcome(apitypes.CodeGitHubFailed)
		WriteError(w, http.StatusBadGateway, apitypes.CodeGitHubFailed, "GitHub could not be asked; try again")
	}
}

// listPulls returns the repository's open pull requests.
func (s *Server) listPulls(w http.ResponseWriter, r *http.Request) {
	repo, client, ok := s.asUser(w, r)
	if !ok {
		return
	}
	list, err := catalog.List(r.Context(), client, repo)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, list)
}

// viewPull returns one pull request.
func (s *Server) viewPull(w http.ResponseWriter, r *http.Request) {
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || number <= 0 || strconv.Itoa(number) != r.PathValue("number") {
		WriteError(w, http.StatusBadRequest, apitypes.CodeBadRequest, "a pull request is named by its number")
		return
	}
	repo, client, ok := s.asUser(w, r)
	if !ok {
		return
	}
	obs.From(r.Context()).SetPull(number)
	view, err := catalog.View(r.Context(), client, repo, number, s.WebBaseURL)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	obs.From(r.Context()).SetSHA(view.HeadSHA)
	WriteJSON(w, http.StatusOK, view)
}
