package tlstrust

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProbeSystemTrustAndUnknownAuthority(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()

	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	if err := ProbeWithOptions(context.Background(), server.URL, "", Options{RootCAs: pool}); err != nil {
		t.Fatalf("trusted probe failed: %v", err)
	}

	err := Probe(context.Background(), server.URL, "")
	var trustErr *TrustError
	if !errors.As(err, &trustErr) || trustErr.Code != CodeTrustRequired {
		t.Fatalf("unknown authority error = %#v, want trust challenge", err)
	}
	digest := sha256.Sum256(server.Certificate().Raw)
	if trustErr.Certificate.Fingerprint != hex.EncodeToString(digest[:]) {
		t.Fatalf("fingerprint = %q", trustErr.Certificate.Fingerprint)
	}
	if trustErr.Certificate.NotAfter.IsZero() || len(trustErr.Certificate.IPAddresses) == 0 {
		t.Fatalf("certificate details are incomplete: %#v", trustErr.Certificate)
	}
}

func TestProbePinnedCertificateAndMismatch(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	digest := sha256.Sum256(server.Certificate().Raw)
	pin := hex.EncodeToString(digest[:])
	if err := Probe(context.Background(), server.URL, pin); err != nil {
		t.Fatalf("matching pin failed: %v", err)
	}
	err := Probe(context.Background(), server.URL, strings.Repeat("ab", 32))
	var trustErr *TrustError
	if !errors.As(err, &trustErr) || trustErr.Code != CodePinMismatch {
		t.Fatalf("mismatch error = %#v, want pin mismatch", err)
	}
	if trustErr.ExpectedFingerprint != strings.Repeat("ab", 32) || trustErr.Certificate.Fingerprint != pin {
		t.Fatalf("mismatch details = %#v", trustErr)
	}
}

func TestProbeOffersWarningsAndPinForHostnameOrValidityErrors(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name      string
		dnsNames  []string
		addresses []net.IP
		notBefore time.Time
		notAfter  time.Time
		want      string
	}{
		{name: "hostname", dnsNames: []string{"other.example"}, notBefore: now.Add(-time.Hour), notAfter: now.Add(time.Hour), want: "does not match nas address"},
		{name: "expired", addresses: []net.IP{net.ParseIP("127.0.0.1")}, notBefore: now.Add(-2 * time.Hour), notAfter: now.Add(-time.Hour), want: "expired"},
		{name: "not-yet-valid", addresses: []net.IP{net.ParseIP("127.0.0.1")}, notBefore: now.Add(time.Hour), notAfter: now.Add(2 * time.Hour), want: "not valid before"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newCertificateServer(t, test.dnsNames, test.addresses, test.notBefore, test.notAfter)
			defer server.Close()
			err := Probe(context.Background(), server.URL, "")
			var trustErr *TrustError
			if !errors.As(err, &trustErr) || trustErr.Code != CodeTrustRequired {
				t.Fatalf("verification error = %#v, want trust challenge", err)
			}
			if !strings.Contains(strings.ToLower(strings.Join(trustErr.ValidationWarnings, "\n")), test.want) {
				t.Fatalf("warnings = %#v, want %q", trustErr.ValidationWarnings, test.want)
			}
			if err := Probe(context.Background(), server.URL, trustErr.Certificate.Fingerprint); err != nil {
				t.Fatalf("explicitly pinned certificate was rejected: %v", err)
			}
		})
	}
}

func TestProbeHTTPAndFingerprintNormalization(t *testing.T) {
	if err := Probe(context.Background(), "http://127.0.0.1:5000", ""); err != nil {
		t.Fatalf("HTTP profile unexpectedly failed: %v", err)
	}
	value, err := NormalizeFingerprint("AA:bb" + strings.Repeat(":00", 30))
	if err != nil || value != "aabb"+strings.Repeat("00", 30) {
		t.Fatalf("normalized fingerprint = %q, %v", value, err)
	}
}

func newCertificateServer(t *testing.T, dnsNames []string, addresses []net.IP, notBefore, notAfter time.Time) *httptest.Server {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "dsmctl TLS test"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
		IPAddresses:           addresses,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	server.StartTLS()
	return server
}
