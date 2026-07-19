// Package webassets contains the small shared browser assets used by dsmctl's
// local web surfaces.
package webassets

import (
	_ "embed"
	"io"
	"net/http"
)

const (
	ThemeColor         = "#0d263f"
	FaviconContentType = "image/svg+xml"
)

//go:embed favicon.svg
var faviconSVG string

// FaviconSVG returns the canonical dsmctl favicon source.
func FaviconSVG() string {
	return faviconSVG
}

// ServeFavicon returns the canonical favicon with browser-safe response
// headers. The source is compiled into the binary, so it has no runtime file
// or network dependency.
func ServeFavicon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Content-Type", FaviconContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(w, faviconSVG)
}
