package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/config"
)

func TestHTTPClientPinnedFingerprint(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer server.Close()
	fingerprint := sha256.Sum256(server.Certificate().Raw)
	profile := config.Profile{URL: server.URL, TLSMode: "pinned_fingerprint", CertificateFingerprint: hex.EncodeToString(fingerprint[:])}
	response, err := HTTPClient(profile).Get(server.URL)
	if err != nil {
		t.Fatalf("pinned request failed: %v", err)
	}
	_ = response.Body.Close()

	profile.CertificateFingerprint = strings.Repeat("0", 64)
	if _, err := HTTPClient(profile).Get(server.URL); err == nil || !strings.Contains(err.Error(), "pinned") {
		t.Fatalf("mismatched pin error = %v", err)
	}

	profile.CertificateFingerprint = hex.EncodeToString(fingerprint[:])
	profile.URL = strings.Replace(server.URL, "127.0.0.1", "localhost", 1)
	response, err = HTTPClient(profile).Get(profile.URL)
	if err != nil {
		t.Fatalf("matching pin should allow an explicitly confirmed hostname mismatch: %v", err)
	}
	_ = response.Body.Close()
}

func TestHTTPClientSystemCARejectsUntrustedCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	if _, err := HTTPClient(config.Profile{URL: server.URL, TLSMode: "system_ca"}).Get(server.URL); err == nil {
		t.Fatal("system CA mode accepted an untrusted certificate")
	}
}
