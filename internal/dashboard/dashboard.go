// Package dashboard serves the read-only Factory Floor observation dashboard.
package dashboard

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
)

//go:embed assets/*
var assets embed.FS

// Handler returns the dashboard with an unavailable observation source.
func Handler() http.Handler { return WithJournal(nil, "", "") }

// WithJournal mounts a trusted, read-only journal handler on fixed same-origin paths.
// repo and project identify this installation; they never select filesystem paths.
func WithJournal(api http.Handler, repo, project string) http.Handler {
	root, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	} // The embedded directory is fixed at compile time.
	files := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		switch r.URL.Path {
		case "/identity":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			if r.Method != http.MethodHead {
				_ = json.NewEncoder(w).Encode(map[string]string{"repo": repo, "project": project})
			}
		case "/snapshot", "/events":
			if api == nil {
				http.Error(w, "observation source unavailable", http.StatusServiceUnavailable)
				return
			}
			api.ServeHTTP(w, r)
		case "/", "/styles.css", "/app.js", "/model.js":
			files.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}
