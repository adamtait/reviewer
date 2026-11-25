// SPDX-License-Identifier: MIT

package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

func built() http.Handler {
	return New(fstest.MapFS{
		"index.html":              {Data: []byte(`<!doctype html><script type="module" src="/assets/index-abc123.js"></script>`)},
		"assets/index-abc123.js":  {Data: []byte(`console.log("app")`)},
		"assets/index-def456.css": {Data: []byte(`body{}`)},
		"placeholder.txt":         {Data: []byte("not for serving")},
	})
}

func get(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestThePageIsServedForEveryAddressOfTheApplication(t *testing.T) {
	h := built()
	for _, address := range []string{
		"/",
		"/signin",
		"/example/alpha/pull/12",
		"/example/alpha/blob/" + strings.Repeat("a", 40) + "/src/deep/file.ts",
		"/something/that/is/not/a/place",
		"/index.html",
		"/placeholder.txt",
	} {
		rec := get(h, http.MethodGet, address)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "/assets/index-abc123.js") {
			t.Errorf("%s: status %d, body %q; want the page", address, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
			t.Errorf("%s: Content-Type %q", address, got)
		}
		// A new build must be picked up without anyone clearing a cache.
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("%s: Cache-Control %q, want no-cache", address, got)
		}
	}
}

func TestThePolicyIsExactlyTheDesigns(t *testing.T) {
	const want = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
		"font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
	h := built()
	for _, address := range []string{"/", "/example/alpha/pull/12", "/assets/index-abc123.js", "/assets/missing.js"} {
		rec := get(h, http.MethodGet, address)
		if got := rec.Header().Get("Content-Security-Policy"); got != want {
			t.Errorf("%s: policy is\n%s\nwant\n%s", address, got, want)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("X-Frame-Options") != "DENY" ||
			rec.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s: headers %v", address, rec.Header())
		}
	}
	// The two properties everything else rests on, said outright so that a
	// well-meant edit to the policy fails here with a reason.
	for _, forbidden := range []string{"script-src 'self' 'unsafe-inline'", "'unsafe-eval'", "connect-src *", "default-src *", "https:"} {
		if strings.Contains(ContentSecurityPolicy, forbidden) {
			t.Errorf("the policy allows %q", forbidden)
		}
	}
}

func TestAssetsAreServedAsFilesAndKept(t *testing.T) {
	h := built()
	rec := get(h, http.MethodGet, "/assets/index-abc123.js")
	if rec.Code != http.StatusOK || rec.Body.String() != `console.log("app")` {
		t.Fatalf("status %d, body %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "javascript") {
		t.Errorf("Content-Type %q: a browser refuses a script served as anything else under nosniff", got)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("Cache-Control %q, want immutable", got)
	}
	if got := get(h, http.MethodGet, "/assets/index-def456.css").Header().Get("Content-Type"); !strings.Contains(got, "css") {
		t.Errorf("stylesheet Content-Type %q", got)
	}
}

func TestAMissingAssetIsNotThePage(t *testing.T) {
	h := built()
	for _, address := range []string{
		"/assets/gone.js",
		"/assets/",
		"/assets/../index.html",
		"/assets/..%2Findex.html",
		"/assets/../placeholder.txt",
	} {
		rec := get(h, http.MethodGet, address)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", address, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "doctype") || strings.Contains(rec.Body.String(), "not for serving") {
			t.Errorf("%s: served %q", address, rec.Body.String())
		}
	}
}

func TestOnlyReadingIsAllowed(t *testing.T) {
	h := built()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		if rec := get(h, method, "/"); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /: status %d, want 405", method, rec.Code)
		}
	}
	if rec := get(h, http.MethodHead, "/"); rec.Code != http.StatusOK {
		t.Errorf("HEAD /: status %d", rec.Code)
	}
}

func TestACheckoutWithoutTheBuiltApplicationSaysSo(t *testing.T) {
	h := New(fstest.MapFS{"placeholder.txt": {Data: []byte("x")}})
	rec := get(h, http.MethodGet, "/example/alpha/pull/12")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "make web") {
		t.Errorf("status %d, body %q; want 503 and how to build it", rec.Code, rec.Body.String())
	}
}

// The embedded directory compiles and serves on any checkout: the built page
// where `make web` has run, and the "not built" notice where it has not.
func TestTheEmbeddedApplicationAnswers(t *testing.T) {
	rec := get(Handler(), http.MethodGet, "/")
	if rec.Code != http.StatusOK && rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d", rec.Code)
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("no policy on the embedded application's response")
	}
}

// The journeys in web/e2e run against a preview server that sends the policy
// too, so that they fail if the built page needs anything the policy forbids.
// That only means something while the preview server's policy is this one.
func TestTheJourneysRunUnderTheSamePolicy(t *testing.T) {
	config, err := os.ReadFile("../../../web/vite.config.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), `"`+ContentSecurityPolicy+`"`) {
		t.Errorf("web/vite.config.ts does not hold the service's policy word for word:\n%s", ContentSecurityPolicy)
	}
}
