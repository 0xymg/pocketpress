package http

import (
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/pocketpress/pocketpress/admin"
)

// newSPAHandler builds a handler for the embedded admin SPA: it serves
// static files (with content types derived from their extension) directly
// out of the embedded filesystem, and falls back to index.html for any GET
// request that doesn't match a real file, so client-side routes such as
// /posts/new resolve to the SPA shell instead of a 404.
func newSPAHandler() (http.Handler, error) {
	distFS, err := admin.FS()
	if err != nil {
		return nil, err
	}

	fileServer := http.FileServerFS(distFS)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if fileExists(distFS, assetPath(r.URL.Path)) {
			setSPACacheControl(w.Header(), r.URL.Path)
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: rewrite to "/" (-> index.html) so client-side
		// routes resolve, without issuing a redirect. The shell is always
		// revalidated (see setSPACacheControl) so a browser never runs a stale
		// index.html — whose inline bootstrap and its startup-computed CSP hash
		// would then mismatch a freshly built binary and dead-lock the SPA.
		setSPACacheControl(w.Header(), "/")
		fallback := new(http.Request)
		*fallback = *r
		fallback.URL = new(url.URL)
		*fallback.URL = *r.URL
		fallback.URL.Path = "/"
		fileServer.ServeHTTP(w, fallback)
	}), nil
}

// immutableAssetPrefix is SvelteKit's content-hashed build-output directory.
// Every file under it carries a hash in its name, so it can be cached forever.
const immutableAssetPrefix = "/_app/immutable/"

// setSPACacheControl picks a Cache-Control policy for an admin SPA response:
// content-hashed build assets under /_app/immutable/ are immutable and cached
// for a year; the index.html shell and every other non-hashed file are served
// no-cache so the browser revalidates them on each load (they change in place
// on every rebuild). This keeps the app shell in lockstep with the binary's
// startup-hashed CSP.
func setSPACacheControl(h http.Header, urlPath string) {
	if strings.HasPrefix(urlPath, immutableAssetPrefix) {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	h.Set("Cache-Control", "no-cache")
}

// assetPath converts a request URL path into the name FileServerFS/fs.Stat
// expect: no leading slash, "" and "/" normalized to "index.html".
func assetPath(urlPath string) string {
	clean := path.Clean(urlPath)
	clean = strings.TrimPrefix(clean, "/")
	if clean == "" || clean == "." {
		return "index.html"
	}
	return clean
}

// fileExists reports whether name identifies a regular file (not a
// directory) in fsys.
func fileExists(fsys fs.FS, name string) bool {
	info, err := fs.Stat(fsys, name)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
