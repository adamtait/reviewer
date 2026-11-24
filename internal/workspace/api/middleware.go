// SPDX-License-Identifier: MIT

package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/adamtait/reviewer/internal/workspace/api/apitypes"
	"github.com/adamtait/reviewer/internal/workspace/obs"
)

// HeaderRequestID carries the request's identifier back to the caller, so a
// person reporting a problem can quote the one value that finds its log line.
const HeaderRequestID = "X-Request-Id"

// wrap puts a handler behind the three things every request gets: an
// identifier, one log line, and a panic that becomes a 500 rather than a dropped
// connection.
func (s *Server) wrap(name string, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		id := newRequestID()
		ctx, _ := obs.WithFields(r.Context())
		r = r.WithContext(ctx)

		rec := &recorder{ResponseWriter: w}
		rec.Header().Set(HeaderRequestID, id)

		defer func() {
			if recovered := recover(); recovered != nil {
				// The panic's value is logged, redacted like everything else.
				// The caller is told only that something broke: a panic message
				// can quote whatever the handler was holding.
				s.log.Event(ctx, slog.LevelError, "panic",
					slog.String("request_id", id), slog.String("route", name),
					slog.String("panic", fmt.Sprint(recovered)))
				if !rec.wrote {
					WriteError(rec, http.StatusInternalServerError, apitypes.CodeInternal, "internal error")
				}
			}
			s.log.Request(ctx, id, name, r.Method, rec.status(), time.Since(started))
		}()

		next(rec, r)
	})
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
