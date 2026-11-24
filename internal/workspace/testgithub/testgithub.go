// SPDX-License-Identifier: MIT

// Package testgithub is a small stand-in for GitHub's API, for the workspace's
// tests.
//
// It answers as GitHub does where the workspace's safety depends on it: an
// unknown token is 401, and a private repository a user cannot read is 404,
// exactly as if it did not exist. It is used only from tests.
package testgithub

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/ghclient"
)

// Repo is one repository the stand-in knows.
type Repo struct {
	Private bool
	// Readers are the logins that can read it. A public repository is readable
	// by every valid token, whatever this says.
	Readers []string
}

// Pull is one pull request the stand-in knows.
type Pull struct {
	Number  int
	Title   string
	Body    string
	Author  string
	Closed  bool
	Draft   bool
	HeadSHA string
	HeadRef string
	// BaseSHA is the tip of the base branch, which is not where the diff is
	// measured from. MergeBase is.
	BaseSHA   string
	BaseRef   string
	MergeBase string
	Updated   time.Time
	Files     []File
	// ChangedFiles is the count GitHub reports. Zero means len(Files); set it
	// higher to stand for a pull request whose file list GitHub cut short.
	ChangedFiles int
}

// File is one file a pull request changes, in GitHub's words.
type File struct {
	Filename string `json:"filename"`
	// Status is one of GitHub's: added, removed, modified, renamed, copied,
	// changed, unchanged.
	Status           string `json:"status"`
	PreviousFilename string `json:"previous_filename,omitempty"`
}

// Server is the stand-in.
type Server struct {
	*httptest.Server
	// downloads is a second host, the one archive requests are redirected to,
	// as GitHub redirects them to a host that is not its API.
	downloads *httptest.Server

	mu       sync.Mutex
	tokens   map[string]string        // token -> login
	repos    map[string]Repo          // "owner/name", lower-cased
	pulls    map[string]map[int]Pull  // by repository
	archives map[string][]byte        // "owner/name@sha" -> gzip tar
	failing  map[string]int           // path prefix -> status
	slow     map[string]chan struct{} // path prefix -> closed to let it answer
	calls    []string
	leaked   int
}

// New starts a stand-in that is closed when the test ends.
func New(t *testing.T) *Server {
	t.Helper()
	s := &Server{
		tokens: map[string]string{}, repos: map[string]Repo{},
		pulls: map[string]map[int]Pull{}, archives: map[string][]byte{},
		failing: map[string]int{}, slow: map[string]chan struct{}{},
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	s.downloads = httptest.NewServer(http.HandlerFunc(s.serveDownload))
	t.Cleanup(s.Close)
	t.Cleanup(s.downloads.Close)
	return s
}

// AddPull adds or replaces a pull request.
func (s *Server) AddPull(owner, name string, p Pull) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.ToLower(owner + "/" + name)
	if s.pulls[key] == nil {
		s.pulls[key] = map[int]Pull{}
	}
	s.pulls[key][p.Number] = p
}

// SetArchive sets what the archive call returns for a commit.
func (s *Server) SetArchive(owner, name, sha string, gzipTar []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.archives[strings.ToLower(owner+"/"+name)+"@"+sha] = gzipTar
}

// Fail makes every request whose path begins with prefix answer with status,
// whoever asks. A status of 0 removes the rule.
func (s *Server) Fail(prefix string, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status == 0 {
		delete(s.failing, prefix)
		return
	}
	s.failing[prefix] = status
}

// Hold makes every request whose path begins with prefix wait until the
// returned function is called.
func (s *Server) Hold(prefix string) (release func()) {
	gate := make(chan struct{})
	s.mu.Lock()
	s.slow[prefix] = gate
	s.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { close(gate) }) }
}

// TokenLeaks is how many requests to the download host carried an
// Authorization header. It must stay zero: the address a download is
// redirected to is on another host, and the user's token must not follow.
func (s *Server) TokenLeaks() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.leaked
}

// CallsTo counts the requests served whose path begins with prefix.
func (s *Server) CallsTo(prefix string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, call := range s.calls {
		if _, path, _ := strings.Cut(call, " "); strings.HasPrefix(path, prefix) {
			n++
		}
	}
	return n
}

// Token returns a credential in the shape of a fine-grained token for a made-up
// user. Built here rather than written out in a test, so no test file holds a
// string that looks like a credential.
func Token(name string) string { return "github" + "_pat_" + name }

// AddUser registers a login and returns the token that acts as them.
func (s *Server) AddUser(login string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	token := Token(login)
	s.tokens[token] = login
	return token
}

// RevokeUser makes a user's token stop working.
func (s *Server) RevokeUser(login string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, Token(login))
}

// SetRepo adds or replaces a repository.
func (s *Server) SetRepo(owner, name string, repo Repo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repos[strings.ToLower(owner+"/"+name)] = repo
}

// Calls returns every request path served so far.
func (s *Server) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

// Client returns a GitHub client that talks to the stand-in with a token.
func (s *Server) Client(token string) (*ghclient.REST, error) {
	return ghclient.New(s.URL, token, "workspace-test")
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.calls = append(s.calls, r.Method+" "+r.URL.Path)
	login, known := s.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	s.mu.Unlock()

	if !known {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	if gate, status := s.rules(r.URL.Path); gate != nil || status != 0 {
		if gate != nil {
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		if status != 0 {
			writeJSON(w, status, map[string]string{"message": http.StatusText(status)})
			return
		}
	}

	notFound := func() {
		// GitHub does not say which: a repository you cannot see and one that
		// does not exist are the same answer. So is anything inside either.
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if r.URL.Path == "/user" {
		writeJSON(w, http.StatusOK, map[string]string{"login": login, "type": "User"})
		return
	}
	if len(parts) < 3 || parts[0] != "repos" {
		notFound()
		return
	}
	key := strings.ToLower(parts[1] + "/" + parts[2])
	s.mu.Lock()
	repo, exists := s.repos[key]
	s.mu.Unlock()
	if !exists || (repo.Private && !contains(repo.Readers, login)) {
		notFound()
		return
	}

	rest := parts[3:]
	switch {
	case len(rest) == 0:
		visibility := "public"
		if repo.Private {
			visibility = "private"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"full_name": parts[1] + "/" + parts[2], "private": repo.Private, "visibility": visibility,
		})

	case len(rest) == 1 && rest[0] == "pulls":
		writeJSON(w, http.StatusOK, s.listPulls(key))

	case len(rest) >= 2 && rest[0] == "pulls":
		number, _ := strconv.Atoi(rest[1])
		s.mu.Lock()
		pull, found := s.pulls[key][number]
		s.mu.Unlock()
		switch {
		case !found:
			notFound()
		case len(rest) == 2:
			writeJSON(w, http.StatusOK, pullJSON(pull, true))
		case len(rest) == 3 && rest[2] == "files":
			files := pull.Files
			if files == nil {
				files = []File{}
			}
			writeJSON(w, http.StatusOK, files)
		default:
			notFound()
		}

	case len(rest) == 2 && rest[0] == "compare":
		base, head, ok := strings.Cut(rest[1], "...")
		if !ok {
			notFound()
			return
		}
		s.mu.Lock()
		mergeBase := ""
		for _, pull := range s.pulls[key] {
			if pull.BaseSHA == base && pull.HeadSHA == head {
				mergeBase = pull.MergeBase
			}
		}
		s.mu.Unlock()
		if mergeBase == "" {
			notFound()
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"merge_base_commit": map[string]string{"sha": mergeBase}})

	case len(rest) == 2 && rest[0] == "tarball":
		s.mu.Lock()
		_, found := s.archives[key+"@"+rest[1]]
		s.mu.Unlock()
		if !found {
			notFound()
			return
		}
		// As GitHub does: a redirect to another host, to an address that is
		// its own credential.
		w.Header().Set("Location", fmt.Sprintf("%s/signed/%s@%s?signature=do-not-print-this", s.downloads.URL, key, rest[1]))
		w.WriteHeader(http.StatusFound)

	default:
		notFound()
	}
}

// rules returns the gate and the forced status for a path, if any.
func (s *Server) rules(path string) (gate chan struct{}, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for prefix, g := range s.slow {
		if strings.HasPrefix(path, prefix) {
			gate = g
		}
	}
	for prefix, code := range s.failing {
		if strings.HasPrefix(path, prefix) {
			status = code
		}
	}
	return gate, status
}

// listPulls returns the open pull requests, most recently updated first.
func (s *Server) listPulls(key string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var open []Pull
	for _, pull := range s.pulls[key] {
		if !pull.Closed {
			open = append(open, pull)
		}
	}
	sort.Slice(open, func(i, j int) bool { return open[i].Updated.After(open[j].Updated) })
	out := make([]map[string]any, 0, len(open))
	for _, pull := range open {
		out = append(out, pullJSON(pull, false))
	}
	return out
}

// NotTheLink is what the stand-in sends as a pull request's web address. A
// response from the workspace that contains it has passed on an address that
// arrived as data.
const NotTheLink = "//elsewhere.invalid/not-the-link-to-show"

// pullJSON is a pull request as GitHub writes one. The count of changed files
// is present only when one pull request is read, as on GitHub.
func pullJSON(p Pull, single bool) map[string]any {
	state := "open"
	if p.Closed {
		state = "closed"
	}
	out := map[string]any{
		"number": p.Number, "title": p.Title, "body": p.Body, "state": state, "draft": p.Draft,
		"user":       map[string]string{"login": p.Author, "type": "User"},
		"head":       map[string]string{"sha": p.HeadSHA, "ref": p.HeadRef},
		"base":       map[string]string{"sha": p.BaseSHA, "ref": p.BaseRef},
		"updated_at": p.Updated.UTC().Format(time.RFC3339),
		"created_at": p.Updated.UTC().Format(time.RFC3339),
		// Deliberately not an address on GitHub. The workspace builds its own
		// link and must not pass this one on.
		"html_url": NotTheLink,
	}
	if single {
		count := p.ChangedFiles
		if count == 0 {
			count = len(p.Files)
		}
		out["changed_files"] = count
	}
	return out
}

// serveDownload is the second host. It knows nothing of tokens: the address is
// the credential.
func (s *Server) serveDownload(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.calls = append(s.calls, "DOWNLOAD "+r.URL.Path)
	if r.Header.Get("Authorization") != "" {
		s.leaked++
	}
	body, found := s.archives[strings.TrimPrefix(r.URL.Path, "/signed/")]
	s.mu.Unlock()

	if gate, _ := s.rules(r.URL.Path); gate != nil {
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
	}
	if !found || r.URL.Query().Get("signature") == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/x-gzip")
	_, _ = w.Write(body)
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Archive builds a commit's archive the way GitHub lays one out: gzip over tar,
// a global header first, and every file under one top-level folder. Files are
// written in path order, so the same files always give the same bytes.
func Archive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	const top = "example-repository-0123abc/"
	check(tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header",
		PAXRecords: map[string]string{"comment": strings.Repeat("0", 40)},
	}))
	check(tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: top, Mode: 0o755}))
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		body := files[path]
		check(tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: top + path, Size: int64(len(body)), Mode: 0o644}))
		_, err := tw.Write([]byte(body))
		check(err)
	}
	check(tw.Close())
	check(zw.Close())
	return buf.Bytes()
}
