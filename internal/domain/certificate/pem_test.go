package certificate

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

// --- test certificate/key generators ---

func rsaKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	return key
}

func keyPEM(t *testing.T, key any) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func certPEM(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// makeLeaf issues a leaf signed by (issuerCert, issuerKey). When issuerCert is
// nil the leaf is self-signed with leafKey.
func makeLeaf(t *testing.T, cn string, sans []string, notBefore, notAfter time.Time, leafKey *rsa.PrivateKey, issuerCert *x509.Certificate, issuerKey *rsa.PrivateKey) ([]byte, *x509.Certificate) {
	t.Helper()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn, Organization: []string{"Acme"}, Country: []string{"US"}},
		DNSNames:     sans,
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	parent := template
	signer := leafKey
	if issuerCert != nil {
		parent = issuerCert
		signer = issuerKey
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent, &leafKey.PublicKey, signer)
	if err != nil {
		t.Fatalf("create leaf: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	return certPEM(der), cert
}

func makeCA(t *testing.T, cn string, key *rsa.PrivateKey) ([]byte, *x509.Certificate) {
	t.Helper()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano() ^ 0x5555),
		Subject:               pkix.Name{CommonName: cn, Organization: []string{"Acme CA"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA: %v", err)
	}
	return certPEM(der), cert
}

// --- key/cert match ---

func TestValidateKeyMatchesLeaf(t *testing.T) {
	key := rsaKey(t)
	future := time.Now().Add(365 * 24 * time.Hour)
	leaf, _ := makeLeaf(t, "nas.example.com", []string{"nas.example.com"}, time.Now().Add(-time.Hour), future, key, nil, nil)

	if err := ValidateKeyMatchesLeaf(keyPEM(t, key), leaf); err != nil {
		t.Fatalf("matching key rejected: %v", err)
	}

	other := rsaKey(t)
	if err := ValidateKeyMatchesLeaf(keyPEM(t, other), leaf); err == nil {
		t.Fatal("mismatched key accepted")
	}
}

func TestValidateKeyMatchesLeafECDSA(t *testing.T) {
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: "ec.example.com"},
		DNSNames:     []string{"ec.example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &ec.PublicKey, ec)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateKeyMatchesLeaf(keyPEM(t, ec), certPEM(der)); err != nil {
		t.Fatalf("matching ECDSA key rejected: %v", err)
	}
}

// --- chain ---

func TestValidateChain(t *testing.T) {
	caKey := rsaKey(t)
	_, caCert := makeCA(t, "Acme Intermediate CA", caKey)
	leafKey := rsaKey(t)
	_, leaf := makeLeaf(t, "nas.example.com", []string{"nas.example.com"}, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour), leafKey, caCert, caKey)

	if err := ValidateChain(leaf, []*x509.Certificate{caCert}); err != nil {
		t.Fatalf("valid chain rejected: %v", err)
	}
	// Empty chain is accepted (a leaf needing no intermediate).
	if err := ValidateChain(leaf, nil); err != nil {
		t.Fatalf("empty chain rejected: %v", err)
	}
	// An unrelated CA does not link to the leaf.
	otherKey := rsaKey(t)
	_, otherCA := makeCA(t, "Unrelated CA", otherKey)
	if err := ValidateChain(leaf, []*x509.Certificate{otherCA}); err == nil {
		t.Fatal("unrelated intermediate accepted")
	}
}

// --- expiry ---

func TestValidateNotExpired(t *testing.T) {
	key := rsaKey(t)
	now := time.Now()
	_, valid := makeLeaf(t, "a", []string{"a"}, now.Add(-time.Hour), now.Add(time.Hour), key, nil, nil)
	if err := ValidateNotExpired(valid, now); err != nil {
		t.Fatalf("valid cert rejected: %v", err)
	}
	_, expired := makeLeaf(t, "a", []string{"a"}, now.Add(-2*time.Hour), now.Add(-time.Hour), key, nil, nil)
	if err := ValidateNotExpired(expired, now); err == nil {
		t.Fatal("expired cert accepted")
	}
	_, future := makeLeaf(t, "a", []string{"a"}, now.Add(time.Hour), now.Add(2*time.Hour), key, nil, nil)
	if err := ValidateNotExpired(future, now); err == nil {
		t.Fatal("not-yet-valid cert accepted")
	}
}

// --- SAN coverage ---

func TestValidateSANCoversHost(t *testing.T) {
	key := rsaKey(t)
	_, exact := makeLeaf(t, "nas.example.com", []string{"nas.example.com", "alt.example.com"}, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), key, nil, nil)
	if err := ValidateSANCoversHost(exact, "nas.example.com"); err != nil {
		t.Fatalf("exact SAN rejected: %v", err)
	}
	if err := ValidateSANCoversHost(exact, "alt.example.com"); err != nil {
		t.Fatalf("alt SAN rejected: %v", err)
	}
	if err := ValidateSANCoversHost(exact, "other.example.com"); err == nil {
		t.Fatal("uncovered host accepted")
	}
	// Empty host is a no-op.
	if err := ValidateSANCoversHost(exact, ""); err != nil {
		t.Fatalf("empty host errored: %v", err)
	}

	_, wild := makeLeaf(t, "*.example.com", []string{"*.example.com"}, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), key, nil, nil)
	if err := ValidateSANCoversHost(wild, "nas.example.com"); err != nil {
		t.Fatalf("wildcard did not cover single label: %v", err)
	}
	if err := ValidateSANCoversHost(wild, "a.b.example.com"); err == nil {
		t.Fatal("wildcard matched two labels")
	}
	if err := ValidateSANCoversHost(wild, "example.com"); err == nil {
		t.Fatal("wildcard matched bare apex")
	}
}

func TestDesiredFromLeafCarriesOnlyPublicFields(t *testing.T) {
	key := rsaKey(t)
	leafBytes, leaf := makeLeaf(t, "nas.example.com", []string{"nas.example.com"}, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), key, nil, nil)
	desired := DesiredFromLeaf(leaf, "TLS_KEY", true)
	if desired.Subject.CommonName != "nas.example.com" || desired.KeyCredentialRefName != "TLS_KEY" || !desired.HasIntermediate {
		t.Fatalf("desired = %#v", desired)
	}
	if len(desired.SHA256) != 64 {
		t.Fatalf("fingerprint = %q", desired.SHA256)
	}
	// The fingerprint must equal the SHA-256 of the parsed DER.
	if want := LeafFingerprint(leaf); desired.SHA256 != want {
		t.Fatalf("fingerprint %q != %q", desired.SHA256, want)
	}
	// A private key must never be inferable from the leaf material or Desired.
	if strings.Contains(string(leafBytes), "PRIVATE") {
		t.Fatal("leaf PEM unexpectedly contains private-key material")
	}
}

func TestDesiredFromLeafIncludesIPSANs(t *testing.T) {
	// DSM's CRT.list reports IP SANs as bare strings; the desired set must carry
	// them too, or an IP-covering import's postcondition re-read mismatches the
	// observed set and falsely reports the cert "not found after apply".
	key := rsaKey(t)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: "192.0.2.235"},
		DNSNames:     []string{"nas.example.com"},
		IPAddresses:  []net.IP{net.ParseIP("192.0.2.235")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create leaf: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	desired := DesiredFromLeaf(leaf, "TLS_KEY", false)
	var hasDNS, hasIP bool
	for _, san := range desired.SubjectAltNames {
		switch san {
		case "nas.example.com":
			hasDNS = true
		case "192.0.2.235":
			hasIP = true
		}
	}
	if !hasDNS || !hasIP {
		t.Fatalf("SubjectAltNames = %v; want both nas.example.com and 192.0.2.235", desired.SubjectAltNames)
	}
}
