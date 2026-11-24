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
