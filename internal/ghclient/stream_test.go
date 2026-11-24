// SPDX-License-Identifier: MIT

package ghclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// GitHub answers an archive request with a redirect to a signed address on
// another host. The token is for the API, and must not be sent there.
func TestArchiveFollowsTheRedirectWithoutTheToken(t *testing.T) {
	var sawAuthorization, sawQuery string
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuthorization = r.Header.Get("Authorization")
		sawQuery = r.URL.RawQuery
		fmt.Fprint(w, "the archive")
	}))
	defer storage.Close()

	var askedPath string
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		askedPath = r.URL.Path
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("the API request should carry the token")
		}
		http.Redirect(w, r, storage.URL+"/signed?signature=secret-signature", http.StatusFound)
	})

	sha := strings.Repeat("a", 40)
	body, err := c.Archive(context.Background(), Repo{"o", "r"}, sha)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	got, _ := io.ReadAll(body)
	if string(got) != "the archive" {
		t.Errorf("body = %q", got)
	}
	if askedPath != "/repos/o/r/tarball/"+sha {
		t.Errorf("asked for %q", askedPath)
	}
	if sawAuthorization != "" {
		t.Errorf("the token followed the redirect to another host: %q", sawAuthorization)
	}
	if sawQuery != "signature=secret-signature" {
		t.Errorf("the signed address was not followed as given: %q", sawQuery)
	}
}

// The client every other call uses gives up after 30 seconds, body included. A
// download must be bounded by its caller and by nothing else.
func TestADownloadIsNotCutOffByTheClientTimeout(t *testing.T) {
	if streamClient.Timeout != 0 {
		t.Fatalf("the download client has a timeout of %v; the caller's context must be the only deadline", streamClient.Timeout)
	}
	_, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(150 * time.Millisecond)
		fmt.Fprint(w, "slow body")
	})
	// A client timeout far shorter than the body takes. Were the download to go
	// through it, the read would fail.
	c.HTTP = &http.Client{Timeout: 20 * time.Millisecond}
	body, err := c.Archive(context.Background(), Repo{"o", "r"}, strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	if got, err := io.ReadAll(body); err != nil || string(got) != "slow body" {
		t.Errorf("read %q, %v", got, err)
	}

	// And the caller's own deadline does cut it off.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	body, err = c.Archive(ctx, Repo{"o", "r"}, strings.Repeat("a", 40))
	if err == nil {
		_, err = io.ReadAll(body)
		body.Close()
	}
	if err == nil {
		t.Error("want the caller's deadline to end a slow download")
	}
}

// A signed address is a credential for what it points at. When following it
// fails, the error names the path that was asked for and nothing of the address.
func TestAFailedRedirectDoesNotPrintTheSignedAddress(t *testing.T) {
	storage := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := storage.URL + "/signed?signature=secret-signature"
	storage.Close() // nothing is listening: following the redirect fails

	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, address, http.StatusFound)
	})
	_, err := c.Archive(context.Background(), Repo{"o", "r"}, strings.Repeat("a", 40))
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), "secret-signature") || strings.Contains(err.Error(), "/signed") {
		t.Errorf("the signed address reached the error: %v", err)
	}
	if !strings.Contains(err.Error(), "/repos/o/r/tarball/") {
		t.Errorf("the error should name the path that was asked for: %v", err)
	}
}

func TestDownloadErrorsKeepTheirKinds(t *testing.T) {
	for status, want := range map[int]error{
		http.StatusUnauthorized: ErrUnauthorized,
		http.StatusNotFound:     ErrNotFound,
		http.StatusForbidden:    ErrForbidden,
	} {
		_, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
		_, err := c.Archive(context.Background(), Repo{"o", "r"}, strings.Repeat("a", 40))
		if !errors.Is(err, want) {
			t.Errorf("status %d: want %v, got %v", status, want, err)
		}
	}
}

func TestCompareReturnsTheMergeBase(t *testing.T) {
	var asked string
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path + "?" + r.URL.RawQuery
		fmt.Fprint(w, `{"merge_base_commit":{"sha":"abc123"},"files":[]}`)
	})
	got, err := c.Compare(context.Background(), Repo{"o", "r"}, "base", "head")
	if err != nil {
		t.Fatal(err)
	}
	if got != "abc123" {
		t.Errorf("merge base = %q", got)
	}
	if asked != "/repos/o/r/compare/base...head?per_page=1" {
		t.Errorf("asked for %q", asked)
	}

	_, c = newServer(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{}`) })
	if _, err := c.Compare(context.Background(), Repo{"o", "r"}, "base", "head"); err == nil {
		t.Error("want a response with no merge base to be an error, not an empty hash")
	}
}

func TestChangedFileCarriesARenamesOldPath(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"filename":"new.ts","status":"renamed","previous_filename":"old.ts"}]`)
	})
	files, err := c.ListFiles(context.Background(), Repo{"o", "r"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].PreviousFilename != "old.ts" {
		t.Errorf("files = %+v", files)
	}
}

func TestActionsListsAreReadFromTheirWrappers(t *testing.T) {
	var asked []string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path+"?"+r.URL.RawQuery)
		switch {
		case strings.HasSuffix(r.URL.Path, "/runs") && r.URL.Query().Get("page") == "":
			w.Header().Set("Link", fmt.Sprintf(`<%s%s?page=2>; rel="next"`, srv.URL, r.URL.Path))
			_, _ = w.Write([]byte(`{"total_count":2,"workflow_runs":[
				{"id":20,"head_sha":"abc","event":"push","path":".github/workflows/review-index.yml",
				 "status":"completed","conclusion":"success","created_at":"2026-11-20T10:00:00Z"}]}`))
		case strings.HasSuffix(r.URL.Path, "/runs"):
			_, _ = w.Write([]byte(`{"total_count":2,"workflow_runs":[
				{"id":10,"head_sha":"abc","event":"push","path":".github/workflows/review-index.yml",
				 "status":"in_progress","conclusion":null,"created_at":"2026-11-20T09:00:00Z"}]}`))
		case strings.HasSuffix(r.URL.Path, "/artifacts"):
			_, _ = w.Write([]byte(`{"total_count":1,"artifacts":[
				{"id":7,"name":"review-index","size_in_bytes":1234,"expired":false,"digest":"sha256:00"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c, err := New(srv.URL, "test-token", "")
	if err != nil {
		t.Fatal(err)
	}
	repo := Repo{Owner: "o", Name: "r"}

	runs, err := c.ListWorkflowRuns(context.Background(), repo, "review-index.yml", "abc", "push")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].ID != 20 || runs[0].Conclusion != "success" || runs[1].Status != "in_progress" || runs[1].Conclusion != "" {
		t.Errorf("runs = %+v", runs)
	}
	if runs[0].Path != ".github/workflows/review-index.yml" || runs[0].Event != "push" || runs[0].HeadSHA != "abc" {
		t.Errorf("the fields a caller checks a run by: %+v", runs[0])
	}
	if !strings.Contains(asked[0], "/repos/o/r/actions/workflows/review-index.yml/runs?") ||
		!strings.Contains(asked[0], "head_sha=abc") || !strings.Contains(asked[0], "event=push") {
		t.Errorf("first request: %s", asked[0])
	}

	artifacts, err := c.ListRunArtifacts(context.Background(), repo, 20, "review-index")
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 || artifacts[0].ID != 7 || artifacts[0].SizeInBytes != 1234 || artifacts[0].Expired {
		t.Errorf("artifacts = %+v", artifacts)
	}
	if last := asked[len(asked)-1]; !strings.Contains(last, "/actions/runs/20/artifacts?") || !strings.Contains(last, "name=review-index") {
		t.Errorf("artifact request: %s", last)
	}
}

func TestAWrappedListStopsAfterAFewPages(t *testing.T) {
	pages := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		// A response that always says there is more.
		w.Header().Set("Link", fmt.Sprintf(`<%s%s?page=%d>; rel="next"`, srv.URL, r.URL.Path, pages+1))
		_, _ = w.Write([]byte(`{"workflow_runs":[{"id":1}]}`))
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "test-token", "")
	runs, err := c.ListWorkflowRuns(context.Background(), Repo{Owner: "o", Name: "r"}, "w.yml", "abc", "push")
	if err != nil {
		t.Fatal(err)
	}
	if pages != maxWrappedPages || len(runs) != maxWrappedPages {
		t.Errorf("%d pages read and %d runs; the bound is %d", pages, len(runs), maxWrappedPages)
	}
}

func TestAnArtifactDownloadFollowsItsRedirectWithoutTheToken(t *testing.T) {
	var tokenAtStorage string
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenAtStorage = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("PK-zip-bytes"))
	}))
	defer storage.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/actions/artifacts/7/zip" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, storage.URL+"/blob?sig=secret", http.StatusFound)
	}))
	defer api.Close()
	c, _ := New(api.URL, "test-token", "")
	body, err := c.DownloadArtifact(context.Background(), Repo{Owner: "o", Name: "r"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = body.Close() }()
	raw, _ := io.ReadAll(body)
	if string(raw) != "PK-zip-bytes" || tokenAtStorage != "" {
		t.Errorf("body %q, token sent to storage %q", raw, tokenAtStorage)
	}
}
