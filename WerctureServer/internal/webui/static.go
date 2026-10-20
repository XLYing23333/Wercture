// Package webui serves the embedded management pages.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// MarkdownPage is the HTML shell for a public Markdown link.
func MarkdownPage() ([]byte, error) {
	return dist.ReadFile("dist/preview.html")
}

// Font is the Chinese face used to draw Markdown previews.
func Font() ([]byte, error) {
	sub, err := fs.Sub(dist, "dist/assets")
	if err != nil {
		return nil, err
	}
	matches, err := fs.Glob(sub, "ComicNeueCN-Regular-*.otf")
	if err != nil || len(matches) == 0 {
		return nil, fs.ErrNotExist
	}
	return fs.ReadFile(sub, matches[0])
}

// Serve writes a built file, or the application shell for client-side routes.
func Serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-eval'; worker-src 'self' blob:; connect-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	if name == "favicon.ico" {
		name = "logo.png"
		r.URL.Path = "/logo.png"
	}
	if name == "index.html" || !strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "no-store")
	}
	if file, err := sub.Open(name); err == nil {
		_ = file.Close()
		http.FileServer(http.FS(sub)).ServeHTTP(w, r)
		return
	}
	r.URL.Path = "/"
	http.FileServer(http.FS(sub)).ServeHTTP(w, r)
}
