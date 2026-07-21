package certificate

// Local, offline certificate validation. These helpers parse PEM material with
// crypto/x509 and enforce the pre-apply invariants the work item requires BEFORE
// the NAS is ever touched: the private key mathematically matches the leaf, the
// intermediate chain links to the leaf, the leaf is not expired, and (for a
// DSM-service binding) the leaf covers the connection host. A failure here is a
// planning/apply-time error rather than a silent apply that bricks admin TLS.
//
// The key-matching helper is the ONE place key bytes are parsed. Its caller in
// the application layer resolves the credential reference to bytes only at apply
// time, runs this check, streams the key, and zeroizes it — the bytes never
// leave that scope.

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"strings"
	"time"
)

// ParseLeaf decodes the first CERTIFICATE block from pemBytes.
func ParseLeaf(pemBytes []byte) (*x509.Certificate, error) {
	block, rest := decodeCertBlock(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no CERTIFICATE PEM block found in leaf material")
	}
	_ = rest
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse leaf certificate: %w", err)
	}
	return leaf, nil
}

// ParseIntermediates decodes every CERTIFICATE block from pemBytes. An empty
// input yields no certificates and no error (the intermediate chain is optional).
func ParseIntermediates(pemBytes []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	rest := pemBytes
	for {
		var block *pem.Block
		block, rest = decodeCertBlock(rest)
		if block == nil {
			break
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse intermediate certificate: %w", err)
		}
		certs = append(certs, cert)
	}
	return certs, nil
}

// decodeCertBlock returns the next CERTIFICATE PEM block and the remaining bytes,
// skipping non-certificate blocks (for example a stray key or params block in the
// public bundle would be ignored here rather than mistaken for a cert).
func decodeCertBlock(data []byte) (*pem.Block, []byte) {
	for {
		block, rest := pem.Decode(data)
		if block == nil {
			return nil, rest
		}
		if block.Type == "CERTIFICATE" {
			return block, rest
		}
		data = rest
	}
}

// LeafFingerprint returns the lowercase hex SHA-256 of a certificate's DER.
func LeafFingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// DesiredFromLeaf builds the plan-time public fingerprint of the imported leaf.
// refName is the NAME portion of the key credential reference (never the value).
func DesiredFromLeaf(leaf *x509.Certificate, refName string, hasIntermediate bool) DesiredCertificate {
	return DesiredCertificate{
		Subject:              nameFromPKIX(leaf.Subject.CommonName, orgOf(leaf.Subject.Organization), countryOf(leaf.Subject.Country)),
		SubjectAltNames:      sansFromLeaf(leaf),
		Issuer:               nameFromPKIX(leaf.Issuer.CommonName, orgOf(leaf.Issuer.Organization), countryOf(leaf.Issuer.Country)),
		Serial:               leaf.SerialNumber.Text(16),
		NotBeforeUnix:        leaf.NotBefore.Unix(),
		NotAfterUnix:         leaf.NotAfter.Unix(),
		SHA256:               LeafFingerprint(leaf),
		KeyCredentialRefName: refName,
		HasIntermediate:      hasIntermediate,
	}
}

// sansFromLeaf collects the leaf's Subject Alternative Names as DSM's CRT.list
// reports them: DNS names verbatim plus IP addresses in string form (e.g.
// "192.0.2.235"). Omitting the IP SANs made an IP-covering import's
// postcondition re-read mismatch the observed set and falsely report the cert
// "not found in the store after apply" even though the import succeeded.
func sansFromLeaf(leaf *x509.Certificate) []string {
	sans := append([]string(nil), leaf.DNSNames...)
	for _, ip := range leaf.IPAddresses {
		sans = append(sans, ip.String())
	}
	return sans
}

func nameFromPKIX(cn, org, country string) Name {
	return Name{CommonName: cn, Organization: org, Country: country}
}

func orgOf(values []string) string {
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

func countryOf(values []string) string {
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

// ValidateNotExpired reports an error when the leaf's validity window does not
// include now. It rejects both an expired leaf and a not-yet-valid one.
func ValidateNotExpired(leaf *x509.Certificate, now time.Time) error {
	if now.After(leaf.NotAfter) {
		return fmt.Errorf("leaf certificate expired at %s", leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	if now.Before(leaf.NotBefore) {
		return fmt.Errorf("leaf certificate is not valid until %s", leaf.NotBefore.UTC().Format(time.RFC3339))
	}
	return nil
}

// ValidateChain verifies the intermediate(s) chain to the leaf: the leaf's
// issuer must resolve through the supplied intermediates, checked by signature.
// An empty intermediate set is accepted (a leaf issued directly by a trusted or
// self root needs none); a non-empty set that does not link to the leaf is an
// error. Chaining to a public trust anchor is intentionally not required — a
// private CA leaf is a valid bring-your-own case.
func ValidateChain(leaf *x509.Certificate, intermediates []*x509.Certificate) error {
	if len(intermediates) == 0 {
		return nil
	}
	// Find the immediate issuer of the leaf among the intermediates and verify
	// the signature, then walk upward. Every supplied intermediate must be part
	// of a chain rooted at the leaf, so a stray unrelated cert is rejected.
	pool := append([]*x509.Certificate(nil), intermediates...)
	current := leaf
	used := make(map[int]bool, len(pool))
	for step := 0; step < len(pool); step++ {
		idx := issuerIndex(current, pool, used)
		if idx < 0 {
			return fmt.Errorf("intermediate chain does not link to certificate %q (missing issuer %q)", current.Subject.CommonName, current.Issuer.CommonName)
		}
		if err := current.CheckSignatureFrom(pool[idx]); err != nil {
			return fmt.Errorf("intermediate %q does not sign %q: %w", pool[idx].Subject.CommonName, current.Subject.CommonName, err)
		}
		used[idx] = true
		current = pool[idx]
	}
	// Every supplied intermediate must have been consumed by the walk; a leftover
	// means an unrelated certificate was bundled in.
	for i := range pool {
		if !used[i] {
			return fmt.Errorf("intermediate %q is not part of the leaf's chain", pool[i].Subject.CommonName)
		}
	}
	return nil
}

func issuerIndex(child *x509.Certificate, pool []*x509.Certificate, used map[int]bool) int {
	for i, candidate := range pool {
		if used[i] {
			continue
		}
		if candidate.Subject.String() == child.Issuer.String() {
			return i
		}
	}
	return -1
}

// ValidateKeyMatchesLeaf parses the private key PEM and reports an error unless
// its public half equals the leaf's public key. This is the mathematical
// key/cert match; it is the only function that handles key bytes, and its caller
// zeroizes them immediately after. It supports PKCS#1, PKCS#8, and SEC1 keys.
func ValidateKeyMatchesLeaf(keyPEM, leafPEM []byte) error {
	leaf, err := ParseLeaf(leafPEM)
	if err != nil {
		return err
	}
	priv, err := parsePrivateKey(keyPEM)
	if err != nil {
		return err
	}
	if !publicKeysEqual(priv, leaf.PublicKey) {
		return fmt.Errorf("private key does not match the leaf certificate public key")
	}
	return nil
}

func parsePrivateKey(keyPEM []byte) (any, error) {
	var block *pem.Block
	rest := keyPEM
	for {
		block, rest = pem.Decode(rest)
		if block == nil {
			return nil, fmt.Errorf("no PRIVATE KEY PEM block found in key material")
		}
		if strings.Contains(block.Type, "PRIVATE KEY") {
			break
		}
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("unsupported or malformed private key")
}

// publicKeysEqual compares the public half of a parsed private key with a
// certificate's public key across the supported algorithms.
func publicKeysEqual(priv any, certPub any) bool {
	switch key := priv.(type) {
	case *rsa.PrivateKey:
		pub, ok := certPub.(*rsa.PublicKey)
		return ok && key.PublicKey.Equal(pub)
	case *ecdsa.PrivateKey:
		pub, ok := certPub.(*ecdsa.PublicKey)
		return ok && key.PublicKey.Equal(pub)
	case ed25519.PrivateKey:
		pub, ok := certPub.(ed25519.PublicKey)
		return ok && key.Public().(ed25519.PublicKey).Equal(pub)
	default:
		return false
	}
}

// ValidateSANCoversHost reports an error unless the leaf's SAN (or CN fallback)
// covers host, honoring a single leading wildcard label. It is applied only when
// the import/bind targets the DSM service, so a certificate that would not serve
// the connection host is rejected before it can break admin TLS.
func ValidateSANCoversHost(leaf *x509.Certificate, host string) error {
	return ValidateNamesCoverHost(leaf.DNSNames, leaf.Subject.CommonName, host)
}

// ValidateNamesCoverHost is the SAN-coverage check over already-extracted names,
// used when only the DSM-reported SAN list and common name are available (an
// installed certificate identified by id, not a freshly parsed PEM). An empty
// host is a no-op.
func ValidateNamesCoverHost(sans []string, commonName, host string) error {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return nil
	}
	names := sans
	if len(names) == 0 && commonName != "" {
		names = []string{commonName}
	}
	for _, name := range names {
		if hostMatches(host, strings.ToLower(strings.TrimSpace(name))) {
			return nil
		}
	}
	return fmt.Errorf("certificate does not cover connection host %q (names: %s)", host, strings.Join(names, ", "))
}

// hostMatches implements RFC 6125 style matching with a single leftmost wildcard.
func hostMatches(host, pattern string) bool {
	if pattern == host {
		return true
	}
	if !strings.HasPrefix(pattern, "*.") {
		return false
	}
	suffix := pattern[1:] // ".example.com"
	// A wildcard matches exactly one left label: host must end with suffix and
	// have no extra dot in the wildcard-covered label.
	if !strings.HasSuffix(host, suffix) {
		return false
	}
	left := host[:len(host)-len(suffix)]
	return left != "" && !strings.Contains(left, ".")
}
