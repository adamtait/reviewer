// SPDX-License-Identifier: MIT

package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/adamtait/reviewer/internal/workspace/access"
	"github.com/adamtait/reviewer/internal/workspace/api/apitypes"
	"github.com/adamtait/reviewer/internal/workspace/identity"
	"github.com/adamtait/reviewer/internal/workspace/obs"
)

// maxSignInBody bounds the sign-in request. A token is under a hundred bytes.
const maxSignInBody = 4 << 10

// signIn exchanges a pasted token for a session cookie.
func (s *Server) signIn(w http.ResponseWriter, r *http.Request) {
	fields := obs.From(r.Context())
	var body apitypes.TokenRequest
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSignInBody))
	if err != nil || json.Unmarshal(raw, &body) != nil {
		WriteError(w, http.StatusBadRequest, apitypes.CodeBadRequest, "the request body must be {\"token\": \"...\"}")
		return
	}

	session, err := s.Identity.SignIn(r.Context(), body.Token)
	switch {
	case errors.Is(err, identity.ErrNotFineGrained):
		fields.SetOutcome(apitypes.CodeNotFineGrained)
		WriteError(w, http.StatusUnauthorized, apitypes.CodeNotFineGrained,
			"Paste a fine-grained personal access token. Classic tokens are not accepted, because they can carry write access to every repository you can reach.")
		return
	case errors.Is(err, identity.ErrRejected):
		fields.SetOutcome(apitypes.CodeRejected)
		WriteError(w, http.StatusUnauthorized, apitypes.CodeRejected,
			"GitHub did not accept this token. It may have expired or been revoked.")
		return
	case errors.Is(err, identity.ErrNotListed):
		fields.SetOutcome(apitypes.CodeNotListed)
		WriteError(w, http.StatusUnauthorized, apitypes.CodeNotListed,
			"This workspace admits only the GitHub accounts its operator has listed, and yours is not one of them.")
		return
	case errors.Is(err, identity.ErrNoRepository):
		fields.SetOutcome(apitypes.CodeNoRepository)
		WriteError(w, http.StatusUnauthorized, apitypes.CodeNoRepository,
			"This token cannot read any repository connected to the workspace. If it is waiting for an organisation owner's approval, it reads nothing private until it is approved.")
		return
	case err != nil:
		fields.SetOutcome("github_failed")
		WriteError(w, http.StatusBadGateway, apitypes.CodeGitHubFailed, "GitHub could not be asked; try again")
		return
	}

	if err := s.Cookies.Set(w, session); err != nil {
		WriteError(w, http.StatusInternalServerError, apitypes.CodeInternal, "internal error")
		return
	}
	fields.SetUser(session.Login)
	fields.SetOutcome("signed_in")
	w.WriteHeader(http.StatusNoContent)
}

// signOut removes the cookie from this browser. There is no session on the
// server to end: a copy of the cookie made beforehand stays valid until it
// expires or the token is revoked on GitHub, and the operating guide says so.
func (s *Server) signOut(w http.ResponseWriter, _ *http.Request) {
	s.Cookies.Clear(w)
	w.WriteHeader(http.StatusNoContent)
}

// me returns the signed-in user and the connected repositories they can read.
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	session, _ := SessionFrom(r.Context())
	out := apitypes.Me{Login: session.Login, Repositories: []apitypes.RepositoryRef{}}
	for _, repo := range s.Access.Connected() {
		ok, err := s.Identity.CanRead(r.Context(), session, repo.Owner, repo.Name)
		switch {
		case errors.Is(err, access.ErrTokenRejected):
			s.signedOut(w)
			return
		case err != nil:
			// Not a partial list: leaving a repository out because GitHub could
			// not be asked would look exactly like having no access to it.
			obs.From(r.Context()).SetOutcome("github_failed")
			WriteError(w, http.StatusBadGateway, apitypes.CodeGitHubFailed, "GitHub could not be asked; try again")
			return
		case ok:
			_, assistant := s.model(repo.Owner, repo.Name)
			out.Repositories = append(out.Repositories, apitypes.RepositoryRef{Owner: repo.Owner, Name: repo.Name, Assistant: assistant})
		}
	}
	WriteJSON(w, http.StatusOK, out)
}
