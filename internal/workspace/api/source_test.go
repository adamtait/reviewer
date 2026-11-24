// SPDX-License-Identifier: MIT

package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/workspace/api/apitypes"
	"github.com/adamtait/reviewer/internal/workspace/source"
	"github.com/adamtait/reviewer/internal/workspace/testgithub"
)

// The commits of the pull request the tests open. tipSHA is the base branch's
// tip, which is not where the diff is measured from; baseSHA is the merge base.
const (
	headSHA = "1111111111111111111111111111111111111111"
	tipSHA  = "2222222222222222222222222222222222222222"
	baseSHA = "3333333333333333333333333333333333333333"
)

const alpha = "/api/v1/repos/example/alpha"

// withPull adds to example/alpha a pull request that renames a function, with
// the files of its head commit and of its merge base.
//
// The fixture is small on purpose and is also the web application's: the
// recorded responses these tests check are the ones its tests are served.
func (w *world) withPull() *world {
	w.t.Helper()
	w.gh.AddPull("example", "alpha", testgithub.Pull{
		Number: 12,
		Title:  "Rename total to sum",
		Body:   "`total` returned a sum of line prices, not a count.\n\nCallers are updated.",
		Author: "alice", HeadSHA: headSHA, HeadRef: "rename-total",
		BaseSHA: tipSHA, BaseRef: "main", MergeBase: baseSHA,
		Updated: time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC),
		Files: []testgithub.File{
			{Filename: "docs/old.md", Status: "removed"},
			{Filename: "src/cart.ts", Status: "modified"},
			{Filename: "src/sum.ts", Status: "renamed", PreviousFilename: "src/total.ts"},
			{Filename: "src/tax.ts", Status: "added"},
		},
	})
	w.gh.AddPull("example", "alpha", testgithub.Pull{
		Number: 9, Title: "Try a faster parser", Author: "bob", Draft: true,
		HeadSHA: tipSHA, HeadRef: "parser", BaseSHA: tipSHA, BaseRef: "main", MergeBase: baseSHA,
		Updated: time.Date(2026, 11, 18, 14, 0, 0, 0, time.UTC),
	})
	w.gh.SetArchive("example", "alpha", headSHA, testgithub.Archive(w.t, map[string]string{
		"README.md":       "# Alpha\n\nA shop.\n",
		"logo.png":        "\x89PNG\r\n\x1a\n\x00\x00",
		"src/cart.ts":     "import { sum } from './sum';\n\nexport function cartTotal(prices: number[]): number {\n  return sum(prices);\n}\n",
		"src/sum.ts":      "export function sum(prices: number[]): number {\n  return prices.reduce((a, b) => a + b, 0);\n}\n",
		"src/tax.ts":      "export const rate = 0.2;\n",
		"src/receipt.ts":  "import { sum } from './sum';\n\nexport const receipt = (prices: number[]) => `Total: ${sum(prices)}`;\n",
		"vendor/big.json": strings.Repeat("0123456789abcdef", 5000),
	}))
	w.gh.SetArchive("example", "alpha", baseSHA, testgithub.Archive(w.t, map[string]string{
		"README.md":      "# Alpha\n\nA shop.\n",
		"docs/old.md":    "Notes nobody reads.\n",
		"src/cart.ts":    "import { total } from './total';\n\nexport function cartTotal(prices: number[]): number {\n  return total(prices);\n}\n",
		"src/total.ts":   "export function total(prices: number[]): number {\n  return prices.reduce((a, b) => a + b, 0);\n}\n",
		"src/receipt.ts": "import { total } from './total';\n\nexport const receipt = (prices: number[]) => `Total: ${total(prices)}`;\n",
	}))
	return w
}

func decode[T any](t *testing.T, body []byte) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decoding %s: %v", body, err)
	}
	return out
}

func TestThePullRequestListAndView(t *testing.T) {
	w := newWorld(t).withPull()
	alice := w.signIn("alice")

	rec := w.do(http.MethodGet, alpha+"/pulls", nil, alice, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status %d: %s", rec.Code, rec.Body.String())
	}
	example(t, "pull_list", rec)

	w.log.Reset()
	rec = w.do(http.MethodGet, alpha+"/pulls/12", nil, alice, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("view: status %d: %s", rec.Code, rec.Body.String())
	}
	example(t, "pull_view", rec)

	view := decode[apitypes.PullView](t, rec.Body.Bytes())
	if view.MergeBaseSHA != baseSHA {
		t.Errorf("mergeBaseSha = %s, want the merge base and not the base branch's tip", view.MergeBaseSHA)
	}
	if view.WebURL != "https://github.example/example/alpha/pull/12" {
		t.Errorf("webUrl = %q", view.WebURL)
	}
	if strings.Contains(rec.Body.String(), "elsewhere.invalid") {
		t.Error("an address from GitHub's response was passed on to the browser")
	}

	// The log line names the pull request and its commit, and nothing it says.
	lines := logLines(t, w.log)
	if len(lines) != 1 || lines[0]["route"] != "pull_view" || lines[0]["pull"] != float64(12) || lines[0]["sha"] != headSHA {
		t.Errorf("log line = %v", lines)
	}
	if strings.Contains(w.log.String(), "Rename total") || strings.Contains(w.log.String(), "line prices") {
		t.Error("a title or a description reached the log")
	}

	// The address may spell the repository any way GitHub would accept.
	if rec := w.do(http.MethodGet, "/api/v1/repos/Example/ALPHA/pulls/12", nil, alice, false); rec.Code != http.StatusOK {
		t.Errorf("a differently-cased address: status %d", rec.Code)
	}
}

func TestAPullRequestIsNamedByItsNumber(t *testing.T) {
	w := newWorld(t).withPull()
	alice := w.signIn("alice")
	for _, bad := range []string{"abc", "0", "-3", "12abc", "012", "1e3", "99999999999999999999"} {
		rec := w.do(http.MethodGet, alpha+"/pulls/"+bad, nil, alice, false)
		if rec.Code != http.StatusBadRequest || errorCode(t, rec) != apitypes.CodeBadRequest {
			t.Errorf("pull %q: status %d, want 400", bad, rec.Code)
		}
	}
	rec := w.do(http.MethodGet, alpha+"/pulls/404", nil, alice, false)
	if rec.Code != http.StatusNotFound || errorCode(t, rec) != apitypes.CodeNotFound {
		t.Errorf("a pull request that does not exist: status %d, want 404", rec.Code)
	}
}

func TestTreeFileAndSearch(t *testing.T) {
	w := newWorld(t).withPull()
	alice := w.signIn("alice")
	head := alpha + "/commits/" + headSHA

	rec := w.do(http.MethodGet, head+"/tree", nil, alice, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("tree: status %d: %s", rec.Code, rec.Body.String())
	}
	example(t, "tree", rec)
	kinds := map[string]string{}
	for _, e := range decode[apitypes.Tree](t, rec.Body.Bytes()).Entries {
		kinds[e.Path] = e.Kind
	}
	if kinds["src/sum.ts"] != "text" || kinds["logo.png"] != "binary" || kinds["vendor/big.json"] != "too_large" {
		t.Errorf("kinds = %v", kinds)
	}

	rec = w.do(http.MethodGet, head+"/file?path="+url.QueryEscape("src/sum.ts"), nil, alice, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("file: status %d: %s", rec.Code, rec.Body.String())
	}
	example(t, "file", rec)

	// A file whose text is not held says why, and sends none.
	for path, kind := range map[string]string{"logo.png": "binary", "vendor/big.json": "too_large"} {
		rec := w.do(http.MethodGet, head+"/file?path="+url.QueryEscape(path), nil, alice, false)
		file := decode[apitypes.File](t, rec.Body.Bytes())
		if rec.Code != http.StatusOK || file.Kind != kind || file.Text != "" {
			t.Errorf("%s: status %d, kind %q, %d bytes of text; want 200, %q and none", path, rec.Code, file.Kind, len(file.Text), kind)
		}
	}

	// The renamed file's old version is read at its old path on the merge base.
	rec = w.do(http.MethodGet, alpha+"/commits/"+baseSHA+"/file?path=src/total.ts", nil, alice, false)
	if file := decode[apitypes.File](t, rec.Body.Bytes()); !strings.Contains(file.Text, "function total") {
		t.Errorf("the merge base's src/total.ts: status %d, text %q", rec.Code, file.Text)
	}

	// Search finds the caller the pull request did not change.
	rec = w.do(http.MethodPost, head+"/search", apitypes.Search{Query: "SUM(", PathPrefix: "src/"}, alice, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("search: status %d: %s", rec.Code, rec.Body.String())
	}
	example(t, "search_result", rec)
	found := false
	for _, m := range decode[apitypes.SearchResult](t, rec.Body.Bytes()).Matches {
		if m.Path == "src/receipt.ts" {
			found = true
		}
	}
	if !found {
		t.Error("src/receipt.ts calls sum and is not in the pull request; search must find it")
	}

	rec = w.do(http.MethodPost, head+"/search", apitypes.Search{Query: "SUM(", CaseSensitive: true}, alice, false)
	result := decode[apitypes.SearchResult](t, rec.Body.Bytes())
	if result.Matches == nil || len(result.Matches) != 0 || result.Truncated {
		t.Errorf("an exact-case search for SUM( = %s; want an empty list", rec.Body.String())
	}

	// Each commit was read from GitHub once, however many requests asked.
	if n := w.gh.CallsTo("/repos/example/alpha/tarball/"); n != 2 {
		t.Errorf("%d archive requests for two commits", n)
	}
	if w.gh.TokenLeaks() != 0 {
		t.Error("the user's token was sent to the host GitHub redirected the download to")
	}
}

func TestBadRequestsToTheSourceRoutes(t *testing.T) {
	w := newWorld(t).withPull()
	alice := w.signIn("alice")
	head := alpha + "/commits/" + headSHA

	for name, sha := range map[string]string{
		"a branch name":    "main",
		"a short hash":     headSHA[:7],
		"upper-case hex":   strings.ToUpper(strings.Repeat("ab", 20)),
		"41 characters":    headSHA + "1",
		"not hexadecimal":  strings.Repeat("g", 40),
		"a hash with a ..": "..%2F" + headSHA[:35],
	} {
		rec := w.do(http.MethodGet, alpha+"/commits/"+sha+"/tree", nil, alice, false)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
		}
	}
	if w.gh.CallsTo("/repos/example/alpha/tarball/") != 0 {
		t.Error("a malformed commit name reached GitHub")
	}

	if rec := w.do(http.MethodGet, head+"/file", nil, alice, false); rec.Code != http.StatusBadRequest {
		t.Errorf("a file request with no path: status %d, want 400", rec.Code)
	}
	for _, missing := range []string{"src/absent.ts", "src", "../etc/passwd", "/src/sum.ts"} {
		rec := w.do(http.MethodGet, head+"/file?path="+url.QueryEscape(missing), nil, alice, false)
		if rec.Code != http.StatusNotFound {
			t.Errorf("file %q: status %d, want 404", missing, rec.Code)
		}
	}

	for name, body := range map[string]any{
		"one character":       apitypes.Search{Query: "a"},
		"nothing":             apitypes.Search{},
		"a very long query":   apitypes.Search{Query: strings.Repeat("a", 300)},
		"not an object":       "sum",
		"a very long request": apitypes.Search{Query: "ab", PathPrefix: strings.Repeat("p", 10000)},
	} {
		rec := w.do(http.MethodPost, head+"/search", body, alice, false)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("search with %s: status %d, want 400", name, rec.Code)
		}
	}
	// Two characters are enough, counted as characters and not bytes.
	if rec := w.do(http.MethodPost, head+"/search", apitypes.Search{Query: "é"}, alice, false); rec.Code != http.StatusBadRequest {
		t.Errorf("one two-byte character: status %d, want 400", rec.Code)
	}
	if rec := w.do(http.MethodPost, head+"/search", apitypes.Search{Query: "éé"}, alice, false); rec.Code != http.StatusOK {
		t.Errorf("two characters: status %d, want 200", rec.Code)
	}
	// A search without the workspace's header is refused before anything is read.
	if rec := w.do(http.MethodPost, head+"/search", apitypes.Search{Query: "sum"}, alice, true); rec.Code != http.StatusBadRequest {
		t.Errorf("a search without the header: status %d, want 400", rec.Code)
	}

	// A commit GitHub does not have in this repository.
	rec := w.do(http.MethodGet, alpha+"/commits/"+strings.Repeat("9", 40)+"/tree", nil, alice, false)
	if rec.Code != http.StatusNotFound || errorCode(t, rec) != apitypes.CodeNotFound {
		t.Errorf("an unknown commit: status %d, want 404", rec.Code)
	}
}

func TestWhatWasSearchedForIsNotLogged(t *testing.T) {
	w := newWorld(t).withPull()
	alice := w.signIn("alice")
	w.log.Reset()
	rec := w.do(http.MethodPost, alpha+"/commits/"+headSHA+"/search",
		apitypes.Search{Query: "reduce((a, b)", PathPrefix: "src/"}, alice, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	log := w.log.String()
	for _, private := range []string{"reduce", "src/", "prices", "sum.ts"} {
		if strings.Contains(log, private) {
			t.Errorf("the log holds %q: search text, a path or source reached it\n%s", private, log)
		}
	}
	lines := logLines(t, w.log)
	if len(lines) != 1 || lines[0]["route"] != "search" || lines[0]["sha"] != headSHA || lines[0]["user"] != "alice" {
		t.Errorf("log line = %v", lines)
	}
}

// The test for the gap a commit-only cache key would open. alice's request puts
// a commit of example/alpha in memory. carol, who can read example/beta and not
// example/alpha, must not be able to reach it: not at alpha's address, and not
// by asking for the same hash at the address of the repository she can read.
func TestMemoryNeverAnswersForAnotherRepository(t *testing.T) {
	w := newWorld(t).withPull()
	alice, carol := w.signIn("alice"), w.signIn("carol")

	if rec := w.do(http.MethodGet, alpha+"/commits/"+headSHA+"/tree", nil, alice, false); rec.Code != http.StatusOK {
		t.Fatalf("alice's own request: status %d", rec.Code)
	}
	if w.cache.Stats().Entries != 1 {
		t.Fatalf("the commit is not in memory, so the test would prove nothing")
	}
	absent := w.do(http.MethodGet, "/api/v1/no/such/address", nil, carol, false)

	for name, address := range map[string]string{
		"the tree at the repository she cannot read": alpha + "/commits/" + headSHA + "/tree",
		"a file at the repository she cannot read":   alpha + "/commits/" + headSHA + "/file?path=src/sum.ts",
		"the same hash at the repository she can":    "/api/v1/repos/example/beta/commits/" + headSHA + "/tree",
		"a file by that hash at the one she can":     "/api/v1/repos/example/beta/commits/" + headSHA + "/file?path=src/sum.ts",
	} {
		rec := w.do(http.MethodGet, address, nil, carol, false)
		if rec.Code != http.StatusNotFound || rec.Body.String() != absent.Body.String() {
			t.Errorf("%s: status %d, body %s; want exactly the answer for an address that does not exist",
				name, rec.Code, rec.Body.String())
		}
	}
	for _, address := range []string{alpha, "/api/v1/repos/example/beta"} {
		rec := w.do(http.MethodPost, address+"/commits/"+headSHA+"/search", apitypes.Search{Query: "sum"}, carol, false)
		if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "sum.ts") {
			t.Errorf("search at %s: status %d: %s", address, rec.Code, rec.Body.String())
		}
	}
	// For beta, GitHub was asked, as carol, and said it has no such commit.
	if n := w.gh.CallsTo("/repos/example/beta/tarball/"); n == 0 {
		t.Error("the request for example/beta at that hash was answered without asking GitHub")
	}
	if w.cache.Stats().Entries != 1 {
		t.Errorf("%d entries in memory; carol's requests should have added none", w.cache.Stats().Entries)
	}

	// And when beta really does have a commit with that hash, each repository
	// is served its own.
	w.gh.SetArchive("example", "beta", headSHA, testgithub.Archive(t, map[string]string{"beta.txt": "beta's file\n"}))
	rec := w.do(http.MethodGet, "/api/v1/repos/example/beta/commits/"+headSHA+"/tree", nil, carol, false)
	tree := decode[apitypes.Tree](t, rec.Body.Bytes())
	if rec.Code != http.StatusOK || len(tree.Entries) != 1 || tree.Entries[0].Path != "beta.txt" {
		t.Errorf("beta's own commit: status %d: %s", rec.Code, rec.Body.String())
	}
}

func TestManyRequestsForOneCommitMakeOneFetch(t *testing.T) {
	w := newWorld(t).withPull()
	alice := w.signIn("alice")
	w.http = w.server.Handler()
	release := w.gh.Hold("/signed/")

	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = w.do(http.MethodGet, alpha+"/commits/"+headSHA+"/tree", nil, alice, false).Code
		}(i)
	}
	// Let them all arrive and find the first one's fetch running.
	time.Sleep(100 * time.Millisecond)
	release()
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("request %d: status %d", i, code)
		}
	}
	if n := w.gh.CallsTo("/repos/example/alpha/tarball/"); n != 1 {
		t.Errorf("%d archive requests for one commit, want 1", n)
	}
}

func TestACommitOverTheLimitIsRefusedAsTooLarge(t *testing.T) {
	w := newWorldWith(t, source.Limits{ArchiveBytes: 200, FileBytes: 200}, time.Minute).withPull()
	alice := w.signIn("alice")
	w.log.Reset()
	for i := 0; i < 2; i++ {
		rec := w.do(http.MethodGet, alpha+"/commits/"+headSHA+"/tree", nil, alice, false)
		if rec.Code != http.StatusRequestEntityTooLarge || errorCode(t, rec) != apitypes.CodeTooLarge {
			t.Fatalf("status %d: %s; want 413 too_large", rec.Code, rec.Body.String())
		}
	}
	if n := w.gh.CallsTo("/repos/example/alpha/tarball/"); n != 1 {
		t.Errorf("a commit known to be too large was fetched %d times", n)
	}
	if lines := logLines(t, w.log); lines[0]["outcome"] != "too_large" {
		t.Errorf("log line = %v", lines[0])
	}
}

func TestGitHubFailuresEachHaveTheirOwnAnswer(t *testing.T) {
	t.Run("GitHub is slow", func(t *testing.T) {
		w := newWorldWith(t, roomy, 50*time.Millisecond).withPull()
		alice := w.signIn("alice")
		release := w.gh.Hold("/signed/")
		defer release()
		rec := w.do(http.MethodGet, alpha+"/commits/"+headSHA+"/tree", nil, alice, false)
		if rec.Code != http.StatusGatewayTimeout || errorCode(t, rec) != apitypes.CodeGitHubTimeout {
			t.Errorf("status %d: %s; want 504 github_timeout", rec.Code, rec.Body.String())
		}
	})

	t.Run("GitHub fails", func(t *testing.T) {
		w := newWorld(t).withPull()
		alice := w.signIn("alice")
		w.gh.Fail("/repos/example/alpha/tarball/", http.StatusBadGateway)
		rec := w.do(http.MethodGet, alpha+"/commits/"+headSHA+"/tree", nil, alice, false)
		if rec.Code != http.StatusBadGateway || errorCode(t, rec) != apitypes.CodeGitHubFailed {
			t.Errorf("status %d: %s; want 502 github_failed", rec.Code, rec.Body.String())
		}
		// Not remembered: once GitHub answers, so does the workspace.
		w.gh.Fail("/repos/example/alpha/tarball/", 0)
		if rec := w.do(http.MethodGet, alpha+"/commits/"+headSHA+"/tree", nil, alice, false); rec.Code != http.StatusOK {
			t.Errorf("after GitHub recovered: status %d", rec.Code)
		}
	})

	t.Run("the token lacks a permission", func(t *testing.T) {
		w := newWorld(t).withPull()
		alice := w.signIn("alice")
		w.gh.Fail("/repos/example/alpha/pulls", http.StatusForbidden)
		w.log.Reset()
		rec := w.do(http.MethodGet, alpha+"/pulls", nil, alice, false)
		if rec.Code != http.StatusBadGateway || errorCode(t, rec) != apitypes.CodeGitHubFailed {
			t.Errorf("status %d: %s; want 502 github_failed", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "permission") {
			t.Errorf("the message should say what to fix: %s", rec.Body.String())
		}
		if lines := logLines(t, w.log); lines[0]["outcome"] != "github_forbidden" {
			t.Errorf("log line = %v", lines[0])
		}
	})

	t.Run("the token is revoked after the access check", func(t *testing.T) {
		w := newWorld(t).withPull()
		alice := w.signIn("alice")
		// The access check's "yes" is still remembered, so the first call to
		// find out is the handler's own.
		w.gh.RevokeUser("alice")
		for _, address := range []string{alpha + "/pulls", alpha + "/pulls/12", alpha + "/commits/" + headSHA + "/tree"} {
			rec := w.do(http.MethodGet, address, nil, alice, false)
			if rec.Code != http.StatusUnauthorized || errorCode(t, rec) != apitypes.CodeSignedOut {
				t.Errorf("%s: status %d: %s; want 401 signed_out", address, rec.Code, rec.Body.String())
			}
		}
	})
}

// What is in memory is served to a user whose access GitHub has since removed
// only until the access check's remembered "yes" runs out.
func TestRemovedAccessStopsMemoryToo(t *testing.T) {
	w := newWorld(t).withPull()
	alice := w.signIn("alice")
	address := alpha + "/commits/" + headSHA + "/tree"
	if rec := w.do(http.MethodGet, address, nil, alice, false); rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}

	w.gh.SetRepo("example", "alpha", testgithub.Repo{Private: true, Readers: []string{}})
	w.server.Access.SetClock(func() time.Time { return time.Now().Add(2 * time.Minute) })
	rec := w.do(http.MethodGet, address, nil, alice, false)
	if rec.Code != http.StatusNotFound {
		t.Errorf("after access was removed: status %d, want 404 although the commit is still in memory", rec.Code)
	}
}
