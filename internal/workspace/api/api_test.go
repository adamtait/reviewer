// SPDX-License-Identifier: MIT

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/internal/workspace/api/apitypes"
	"github.com/adamtait/reviewer/internal/workspace/obs"
)

// newTestServer returns a Server writing its log to the returned buffer.
func newTestServer() (*Server, *bytes.Buffer) {
	var log bytes.Buffer
	return New(obs.New(&log, obs.NewRedactor()), "test-version"), &log
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
	s, log := newTestServer()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body apitypes.Health
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ok" || body.Version != "test-version" {
		t.Errorf("body = %+v", body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("API responses must not be kept by a shared cache")
	}

	lines := logLines(t, log)
	if len(lines) != 1 {
		t.Fatalf("want exactly one log line per request, got %d", len(lines))
	}
	if lines[0]["route"] != "health" || lines[0]["status"] != float64(200) {
		t.Errorf("log line = %v", lines[0])
	}
	if id := rec.Header().Get(HeaderRequestID); id == "" || lines[0]["request_id"] != id {
		t.Errorf("the response's request id %q should be the log line's %v", id, lines[0]["request_id"])
	}
}

// An address the table does not name gets the same body as a repository the
// user cannot read, so the two cannot be told apart.
func TestAnUnknownAddressIsNotFound(t *testing.T) {
	s, log := newTestServer()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nothing/here", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body apitypes.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != apitypes.CodeNotFound {
		t.Errorf("code = %q, want %q", body.Error.Code, apitypes.CodeNotFound)
	}
	if lines := logLines(t, log); len(lines) != 1 || lines[0]["route"] != "unmatched" {
		t.Errorf("want one line with route \"unmatched\", got %v", lines)
	}
}

// A panic becomes a 500 with nothing of the panic in the body, and the request
// still gets its log line.
func TestAPanicBecomesAnInternalError(t *testing.T) {
	s, log := newTestServer()
	s.Add(Route{Method: http.MethodGet, Pattern: "/boom", Name: "boom", Handler: func(http.ResponseWriter, *http.Request) {
		panic("held a secret: do not show this")
	}})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("the panic's text reached the caller: %s", rec.Body.String())
	}
	lines := logLines(t, log)
	if len(lines) != 2 {
		t.Fatalf("want a panic line and a request line, got %d", len(lines))
	}
	if lines[0]["msg"] != "panic" || lines[1]["msg"] != "request" || lines[1]["status"] != float64(500) {
		t.Errorf("log lines = %v", lines)
	}
}

// What a handler records about its request is on the request's one line.
func TestAHandlersFieldsReachTheLogLine(t *testing.T) {
	s, log := newTestServer()
	s.Add(Route{Method: http.MethodGet, Pattern: "/who/{owner}/{repo}", Name: "who", Repo: true,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			fields := obs.From(r.Context())
			fields.SetUser("octocat")
			fields.SetRepo(r.PathValue("owner"), r.PathValue("repo"))
			w.WriteHeader(http.StatusNoContent)
		}})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/who/example-owner/example-repo", nil))

	lines := logLines(t, log)
	if len(lines) != 1 {
		t.Fatalf("want one line, got %d", len(lines))
	}
	line := lines[0]
	if line["user"] != "octocat" || line["owner"] != "example-owner" || line["repo"] != "example-repo" || line["status"] != float64(204) {
		t.Errorf("log line = %v", line)
	}
	// The route's name, never the address: the measures are counts by name.
	if line["route"] != "who" {
		t.Errorf("route = %v, want the route's name", line["route"])
	}
}

func TestTheRouteTableListsWhatIsServed(t *testing.T) {
	s, _ := newTestServer()
	routes := s.Routes()
	if len(routes) != 1 || routes[0].Name != "health" || routes[0].Repo {
		t.Errorf("want the health route alone, and not marked as carrying repository data, got %+v", routes)
	}
}
