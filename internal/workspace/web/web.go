// SPDX-License-Identifier: MIT

// Package web serves the workspace's web application: one page and its assets,
// built from the web/ directory at the repository root and embedded here.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// The built application. `make web` fills this directory; on a checkout where
// it has not been run the directory holds only a placeholder.
//
//go:embed all:dist
var embedded embed.FS

// ContentSecurityPolicy is sent with the page.
//
// Nothing is allowed that is not listed. Scripts come from this origin only,
// so a script that reached the page inside a pull request's description or a
// model's answer would not run. Requests go to this origin only, so nothing on
// the page can send what it shows anywhere else. The page cannot be framed.
//
// Styles alone are allowed inline: the code editor injects its styles at run
// time.
const ContentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

const notBuilt = `The review workspace's web application is not in this build.

Build it with "make web" and build the service again. The API is unaffected.
`

// Handler serves the embedded application.
func Handler() http.Handler {
	built, err := fs.Sub(embedded, "dist")
	if err != nil {
		// The directory is named in the embed directive above, so this cannot
		// happen in a binary that compiled.
		panic(err)
	}
	return New(built)
}

// New serves an application from files: index.html at the root and everything
// else under assets/.
//
// The page is returned for every address that is not an asset, because the
// application does its own routing: /example/repo/pull/12 is a place in the
// application, not a file.
func New(files fs.FS) http.Handler {
	assets := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		secure(w.Header())

		if strings.HasPrefix(r.URL.Path, "/assets/") {
			name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
			if info, err := fs.Stat(files, name); err != nil || info.IsDir() || !strings.HasPrefix(name, "assets/") {
				// A plain "not found", not the page: a missing script must
				// fail as a missing script.
				http.NotFound(w, r)
				return
			}
			// The build names each asset after its content, so a name never
			// means two things and a browser may keep it for good.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			assets.ServeHTTP(w, r)
			return
		}

		page, err := fs.ReadFile(files, "index.html")
		if err != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(notBuilt))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Always asked for again, so a new build is picked up at once. The
		// page is small; what is large is in the assets it names.
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(page)
	})
}

// secure sets the headers every response from this package carries.
func secure(h http.Header) {
	h.Set("Content-Security-Policy", ContentSecurityPolicy)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	// Said twice, for browsers that read only the older header.
	h.Set("X-Frame-Options", "DENY")
}
