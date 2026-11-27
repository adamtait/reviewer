// SPDX-License-Identifier: MIT

package ghclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// The response below is in the shape GitHub's GraphQL documentation gives for
// Commit.blame. It has not been compared with a response from GitHub itself.
func TestBlame(t *testing.T) {
	var request struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	s, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decoding the request: %v", err)
		}
		fmt.Fprint(w, `{"data":{"repository":{"object":{"blame":{"ranges":[
			{"startingLine":1,"endingLine":9,"commit":{"oid":"aaaa"}},
			{"startingLine":10,"endingLine":12,"commit":{"oid":"bbbb"}}
		]}}}}}`)
	})
	got, err := c.Blame(context.Background(), Repo{Owner: "o", Name: "n"}, "base0", "cmd/a.go")
	if err != nil {
		t.Fatal(err)
	}
	want := []BlameRange{{StartLine: 1, EndLine: 9, Commit: "aaaa"}, {StartLine: 10, EndLine: 12, Commit: "bbbb"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Blame = %+v, want %+v", got, want)
	}

	req := s.requests[0]
	if req.Method != http.MethodPost || req.URL.Path != "/graphql" {
		t.Errorf("asked %s %s, want POST /graphql", req.Method, req.URL.Path)
	}
	for _, want := range []string{"object(oid:$oid)", "blame(path:$path)", "startingLine", "endingLine", "oid"} {
		if !strings.Contains(request.Query, want) {
			t.Errorf("the query has no %q:\n%s", want, request.Query)
		}
	}
	for key, want := range map[string]string{"owner": "o", "name": "n", "oid": "base0", "path": "cmd/a.go"} {
		if request.Variables[key] != want {
			t.Errorf("variable %s = %v, want %q", key, request.Variables[key], want)
		}
	}
}

// GraphQL answers 200 with the error in the body, so a blame of a path that
// does not exist must not read as a file nobody wrote.
func TestBlameErrors(t *testing.T) {
	for name, tt := range map[string]struct {
		body string
		is   error
	}{
		"an error in the body": {
			body: `{"data":{"repository":{"object":null}},"errors":[{"message":"Could not resolve file for path 'x'."}]}`,
		},
		"no such commit": {
			body: `{"data":{"repository":{"object":null}}}`,
			is:   ErrNotFound,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, tt.body) })
			got, err := c.Blame(context.Background(), Repo{Owner: "o", Name: "n"}, "base0", "x")
			if err == nil {
				t.Fatalf("want an error, got ranges %+v", got)
			}
			if tt.is != nil && !errors.Is(err, tt.is) {
				t.Errorf("want %v, got %v", tt.is, err)
			}
		})
	}
}
