package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/config"
)

func TestPrepareWebLoginTLSConfirmsAndPersistsObservedCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("TLS preflight must not send an HTTP request")
	}))
	defer server.Close()
	store := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	cfg := config.New()
	cfg.DefaultNAS = "lab"
	cfg.NAS["lab"] = config.Profile{URL: server.URL, TimeoutSeconds: 30}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	var prompt bytes.Buffer
	updated, err := prepareWebLoginTLS(context.Background(), strings.NewReader("yes\n"), &prompt, store, cfg, "lab", cfg.NAS["lab"])
	if err != nil {
		t.Fatal(err)
	}
	if updated.TLSMode != "pinned_fingerprint" || len(updated.CertificateFingerprint) != 64 || updated.InsecureSkipTLSVerify {
		t.Fatalf("updated profile = %#v", updated)
	}
	if text := prompt.String(); !strings.Contains(text, "Verification warnings:") || !strings.Contains(text, "system CA store") || !strings.Contains(text, "SHA-256:") || !strings.Contains(text, "Pinned the observed certificate") {
		t.Fatalf("prompt = %q", text)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.NAS["lab"].CertificateFingerprint != updated.CertificateFingerprint || loaded.NAS["lab"].TLSMode != "pinned_fingerprint" {
		t.Fatalf("persisted profile = %#v", loaded.NAS["lab"])
	}
	if _, err := prepareWebLoginTLS(context.Background(), strings.NewReader(""), &bytes.Buffer{}, store, loaded, "lab", loaded.NAS["lab"]); err != nil {
		t.Fatalf("matching stored pin prompted again: %v", err)
	}
}

func TestPrepareWebLoginTLSDeclineDoesNotChangeProfile(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("TLS preflight must not send an HTTP request")
	}))
	defer server.Close()
	store := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	cfg := config.New()
	cfg.NAS["lab"] = config.Profile{URL: server.URL, TimeoutSeconds: 30}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareWebLoginTLS(context.Background(), strings.NewReader("no\n"), &bytes.Buffer{}, store, cfg, "lab", cfg.NAS["lab"]); err == nil || !strings.Contains(err.Error(), "web login did not start") {
		t.Fatalf("decline error = %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.NAS["lab"].TLSMode == "pinned_fingerprint" || loaded.NAS["lab"].CertificateFingerprint != "" {
		t.Fatalf("declined certificate was persisted: %#v", loaded.NAS["lab"])
	}
}

func TestPrepareWebLoginTLSReplacesChangedPinOnlyAfterConfirmation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	store := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	cfg := config.New()
	cfg.NAS["lab"] = config.Profile{URL: server.URL, TLSMode: "pinned_fingerprint", CertificateFingerprint: strings.Repeat("ab", 32)}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	var prompt bytes.Buffer
	updated, err := prepareWebLoginTLS(context.Background(), strings.NewReader("y\n"), &prompt, store, cfg, "lab", cfg.NAS["lab"])
	if err != nil {
		t.Fatal(err)
	}
	if updated.CertificateFingerprint == strings.Repeat("ab", 32) || !strings.Contains(prompt.String(), "Previously pinned") {
		t.Fatalf("pin replacement = %#v, prompt=%q", updated, prompt.String())
	}
}

func TestTerminalSafeCertificateText(t *testing.T) {
	if got := terminalSafe("NAS\x1b[31m\nissuer"); got != "NAS [31m issuer" {
		t.Fatalf("terminal-safe text = %q", got)
	}
}
