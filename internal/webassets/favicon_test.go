package webassets

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFaviconDesignContract(t *testing.T) {
	svg := FaviconSVG()
	for _, want := range []string{
		`viewBox="0 0 64 64"`,
		`rx="16"`,
		`#4da5f4`,
		`#2588df`,
		`#146fbd`,
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("favicon is missing design marker %q", want)
		}
	}
	if got := strings.Count(svg, `class="tile"`); got != 4 {
		t.Errorf("favicon tile count = %d, want 4", got)
	}
	for _, banned := range []string{"<script", "<image", "<foreignObject", "href=", "url(http", "@import"} {
		if strings.Contains(svg, banned) {
			t.Errorf("favicon contains unsafe or external marker %q", banned)
		}
	}
}

func TestServeFavicon(t *testing.T) {
	recorder := httptest.NewRecorder()
	ServeFavicon(recorder, httptest.NewRequest(http.MethodGet, "/favicon.svg", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != FaviconContentType {
		t.Errorf("Content-Type = %q", got)
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if got := recorder.Body.String(); got != FaviconSVG() {
		t.Fatal("served favicon differs from the embedded source")
	}
}

func TestServeFaviconMethodContract(t *testing.T) {
	head := httptest.NewRecorder()
	ServeFavicon(head, httptest.NewRequest(http.MethodHead, "/favicon.svg", nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD response = %d, body length = %d", head.Code, head.Body.Len())
	}

	post := httptest.NewRecorder()
	ServeFavicon(post, httptest.NewRequest(http.MethodPost, "/favicon.svg", nil))
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST response = %d, Allow = %q", post.Code, post.Header().Get("Allow"))
	}
}
