// SPDX-License-Identifier: MIT

package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/adamtait/reviewer/internal/workspace/access"
	"github.com/adamtait/reviewer/internal/workspace/api/apitypes"
	"github.com/adamtait/reviewer/internal/workspace/identity"
	"github.com/adamtait/reviewer/internal/workspace/obs"
)

// HeaderRequestID carries the request's identifier back to the caller, so a
// person reporting a problem can quote the one value that finds its log line.
const HeaderRequestID = "X-Request-Id"

// HeaderRequest must be present, with the value "1", on every request that is
// not a GET. A browser will not let another site's page add a header to a
// request without this service's permission, which it never gives, so the
// header shows the request came from the workspace's own page. The cookie's
// SameSite attribute says the same thing a second way.
const HeaderRequest = "X-Workspace-Request"

type sessionKey struct{}

// SessionFrom returns the signed-in user for a request on an Auth or Repo route.
func SessionFrom(ctx context.Context) (identity.Session, bool) {
	s, ok := ctx.Value(sessionKey{}).(identity.Session)
	return s, ok
}

// wrap puts a route's handler behind everything a request gets: an identifier,
// one log line, a panic that becomes a 500 rather than a dropped connection,
// and, where the route asks for them, a signed-in user and the access check.
func (s *Server) wrap(route Route) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		id := newRequestID()
		ctx, fields := obs.WithFields(r.Context())

		rec := &recorder{ResponseWriter: w}
		rec.Header().Set(HeaderRequestID, id)
		// No response is to be read as anything but what it says it is. JSON
		// that holds a file's text must never be taken for a page.
		rec.Header().Set("X-Content-Type-Options", "nosniff")

		defer func() {
			if recovered := recover(); recovered != nil {
				// The panic's value is logged, redacted like everything else.
				// The caller is told only that something broke: a panic message
				// can quote whatever the handler was holding.
				s.log.Event(ctx, slog.LevelError, "panic",
					slog.String("request_id", id), slog.String("route", route.Name),
					slog.String("panic", fmt.Sprint(recovered)))
				if !rec.wrote {
					WriteError(rec, http.StatusInternalServerError, apitypes.CodeInternal, "internal error")
				}
			}
			s.log.Request(ctx, id, route.Name, r.Method, rec.status(), time.Since(started))
		}()

		if route.Method != "" && r.Method != http.MethodGet && r.Method != http.MethodHead &&
			r.Header.Get(HeaderRequest) != "1" {
			WriteError(rec, http.StatusBadRequest, apitypes.CodeBadRequest,
				"this request must carry the header "+HeaderRequest+": 1")
			return
		}

		if route.Auth || route.Repo {
			session, err := s.Cookies.Read(r)
			if err != nil {
				s.signedOut(rec)
				return
			}
			fields.SetUser(session.Login)
			ctx = context.WithValue(ctx, sessionKey{}, session)

			if route.Repo {
				owner, repo := r.PathValue("owner"), r.PathValue("repo")
				fields.SetRepo(owner, repo)
				ok, err := s.Identity.CanRead(ctx, session, owner, repo)
				switch {
				case errors.Is(err, access.ErrTokenRejected):
					s.signedOut(rec)
					return
				case err != nil:
					fields.SetOutcome("github_failed")
					WriteError(rec, http.StatusBadGateway, apitypes.CodeGitHubFailed, "GitHub could not be asked; try again")
					return
				case !ok:
					notFound(rec, r)
					return
				}
			}
		}

		route.Handler(rec, r.WithContext(ctx))
	})
}

// signedOut tells the browser its session is gone and removes the cookie, so
// the page stops presenting it.
func (s *Server) signedOut(w http.ResponseWriter) {
	s.Cookies.Clear(w)
	WriteError(w, http.StatusUnauthorized, apitypes.CodeSignedOut, "sign in again")
}

// newRequestID returns 16 random hex characters.
func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on any platform this runs on, and an
		// identifier is not worth refusing a request over.
		return "0000000000000000"
	}
	return hex.EncodeToString(b[:])
}

// recorder remembers the status a handler wrote, for the log line.
type recorder struct {
	http.ResponseWriter
	code  int
	wrote bool
}

func (r *recorder) WriteHeader(code int) {
	if !r.wrote {
		r.code, r.wrote = code, true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.code, r.wrote = http.StatusOK, true
	}
	return r.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the real writer, which a streamed
// answer needs in order to flush.
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *recorder) status() int {
	if !r.wrote {
		return http.StatusOK
	}
	return r.code
}

// WriteJSON writes v as the response body.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Nothing the API returns may be kept by a shared cache: every response is
	// one user's view of a repository.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes an apitypes.Error.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, apitypes.Error{Error: apitypes.ErrorBody{Code: code, Message: message}})
}
