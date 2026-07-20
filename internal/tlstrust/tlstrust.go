// Package tlstrust performs credential-free TLS preflight for interactive NAS
// enrollment. Normal Web PKI verification is attempted first. When TLS can
// present a parseable leaf but CA, hostname, or validity verification fails,
// the package returns the exact observed certificate and human-readable
// warnings so a person may explicitly pin it to that NAS profile.
package tlstrust

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

const (
	CodeTrustRequired  = "certificate_trust_required"
	CodePinMismatch    = "certificate_pin_mismatch"
	defaultDialTimeout = 30 * time.Second
)

// Certificate is the non-secret identity shown to a person before a
// trust-on-first-use decision. Fingerprint is the lowercase SHA-256 digest of
// the exact leaf certificate DER.
type Certificate struct {
	Fingerprint string    `json:"fingerprint"`
	Subject     string    `json:"subject"`
	Issuer      string    `json:"issuer"`
	DNSNames    []string  `json:"dns_names"`
	IPAddresses []string  `json:"ip_addresses"`
	NotBefore   time.Time `json:"not_before"`
	NotAfter    time.Time `json:"not_after"`
	SelfSigned  bool      `json:"self_signed"`
}

// TrustError is returned only when a person may explicitly pin a freshly
// observed leaf certificate. Other TLS failures remain ordinary errors and do
// not offer a pin action.
type TrustError struct {
	Code                string      `json:"code"`
	Certificate         Certificate `json:"certificate"`
	ExpectedFingerprint string      `json:"expected_fingerprint,omitempty"`
	ValidationWarnings  []string    `json:"validation_warnings,omitempty"`
	cause               error
}

func (e *TrustError) Error() string {
	if e == nil {
		return "TLS certificate trust is required"
	}
	if e.Code == CodePinMismatch {
		return "TLS server certificate does not match the pinned SHA-256 fingerprint"
	}
	return "TLS certificate did not pass normal verification"
}

func (e *TrustError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Options is primarily a test seam. Nil RootCAs uses the operating system
// trust store and a zero Now uses time.Now.
type Options struct {
	RootCAs *x509.CertPool
	Now     func() time.Time
}

// Probe authenticates an HTTPS endpoint before an interactive login begins.
// An empty pin means normal Web PKI verification. A non-empty pin verifies the
// exact observed leaf and intentionally replaces CA, hostname, and validity
// policy for that profile. HTTP endpoints have no TLS certificate and return
// success unchanged.
func Probe(ctx context.Context, rawURL, pinnedFingerprint string) error {
	return ProbeWithOptions(ctx, rawURL, pinnedFingerprint, Options{})
}

// ProbeWithOptions is Probe with injectable trust roots and time.
func ProbeWithOptions(ctx context.Context, rawURL, pinnedFingerprint string, opts Options) error {
	endpoint, err := parseEndpoint(rawURL)
	if err != nil {
		return err
	}
	if endpoint.scheme == "http" {
		return nil
	}
	pin, err := NormalizeFingerprint(pinnedFingerprint)
	if err != nil {
		return err
	}
	if pin != "" {
		observed, leaf, observeErr := observe(ctx, endpoint)
		if observeErr != nil {
			return observeErr
		}
		if observed.Fingerprint != pin {
			return &TrustError{
				Code: CodePinMismatch, Certificate: observed, ExpectedFingerprint: pin,
				ValidationWarnings: certificateWarnings(endpoint.host, leaf, opts, nil),
			}
		}
		return nil
	}

	config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: endpoint.host, RootCAs: opts.RootCAs}
	connection, err := dial(ctx, endpoint, config)
	if err == nil {
		return connection.Close()
	}
	if !isCertificateVerificationError(err) {
		return fmt.Errorf("verify TLS certificate for %s: %w", endpoint.host, err)
	}
	observed, leaf, observeErr := observe(ctx, endpoint)
	if observeErr != nil {
		return observeErr
	}
	return &TrustError{
		Code: CodeTrustRequired, Certificate: observed,
		ValidationWarnings: certificateWarnings(endpoint.host, leaf, opts, err), cause: err,
	}
}

// Observe returns the current leaf certificate without authenticating its
// issuer, hostname, or validity period. It never sends an HTTP request or
// credentials; callers must bind any trust decision to the returned exact
// fingerprint.
func Observe(ctx context.Context, rawURL string) (Certificate, error) {
	endpoint, err := parseEndpoint(rawURL)
	if err != nil {
		return Certificate{}, err
	}
	if endpoint.scheme != "https" {
		return Certificate{}, errors.New("certificate observation requires an https URL")
	}
	certificate, _, err := observe(ctx, endpoint)
	return certificate, err
}

func observe(ctx context.Context, endpoint tlsEndpoint) (Certificate, *x509.Certificate, error) {
	// This connection is deliberately unauthenticated only long enough to read
	// the candidate leaf certificate for display. No HTTP request, cookie,
	// password, OTP, or login code is sent on it.
	config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: endpoint.host, InsecureSkipVerify: true} //nolint:gosec
	connection, err := dial(ctx, endpoint, config)
	if err != nil {
		return Certificate{}, nil, fmt.Errorf("observe TLS certificate for %s: %w", endpoint.host, err)
	}
	defer connection.Close()
	peers := connection.ConnectionState().PeerCertificates
	if len(peers) == 0 {
		return Certificate{}, nil, errors.New("TLS peer did not provide a certificate")
	}
	leaf := peers[0]
	digest := sha256.Sum256(leaf.Raw)
	addresses := make([]string, 0, len(leaf.IPAddresses))
	for _, address := range leaf.IPAddresses {
		addresses = append(addresses, address.String())
	}
	selfSigned := bytes.Equal(leaf.RawSubject, leaf.RawIssuer) && leaf.CheckSignatureFrom(leaf) == nil
	return Certificate{
		Fingerprint: hex.EncodeToString(digest[:]),
		Subject:     leaf.Subject.String(),
		Issuer:      leaf.Issuer.String(),
		DNSNames:    append([]string(nil), leaf.DNSNames...),
		IPAddresses: addresses,
		NotBefore:   leaf.NotBefore.UTC(),
		NotAfter:    leaf.NotAfter.UTC(),
		SelfSigned:  selfSigned,
	}, leaf, nil
}

func isCertificateVerificationError(err error) bool {
	var verificationError *tls.CertificateVerificationError
	if errors.As(err, &verificationError) {
		return true
	}
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameError x509.HostnameError
	var invalidError x509.CertificateInvalidError
	return errors.As(err, &unknownAuthority) || errors.As(err, &hostnameError) || errors.As(err, &invalidError)
}

func certificateWarnings(host string, leaf *x509.Certificate, opts Options, verificationErr error) []string {
	warnings := make([]string, 0, 3)
	appendWarning := func(message string) {
		for _, existing := range warnings {
			if existing == message {
				return
			}
		}
		warnings = append(warnings, message)
	}
	var unknownAuthority x509.UnknownAuthorityError
	if errors.As(verificationErr, &unknownAuthority) {
		appendWarning("issuer is not trusted by the system CA store")
	}
	if err := leaf.VerifyHostname(host); err != nil {
		appendWarning(fmt.Sprintf("certificate does not match NAS address %q", host))
	}
	now := time.Now()
	if opts.Now != nil {
		now = opts.Now()
	}
	if now.Before(leaf.NotBefore) {
		appendWarning(fmt.Sprintf("certificate is not valid before %s", leaf.NotBefore.UTC().Format(time.RFC3339)))
	}
	if now.After(leaf.NotAfter) {
		appendWarning(fmt.Sprintf("certificate expired at %s", leaf.NotAfter.UTC().Format(time.RFC3339)))
	}
	if len(warnings) == 0 && verificationErr != nil {
		appendWarning("certificate failed normal Web PKI verification")
	}
	return warnings
}

func dial(ctx context.Context, endpoint tlsEndpoint, config *tls.Config) (*tls.Conn, error) {
	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: defaultDialTimeout}, Config: config}
	connection, err := dialer.DialContext(ctx, "tcp", endpoint.address)
	if err != nil {
		return nil, err
	}
	tlsConnection, ok := connection.(*tls.Conn)
	if !ok {
		_ = connection.Close()
		return nil, errors.New("TLS dial did not return a TLS connection")
	}
	return tlsConnection, nil
}

type tlsEndpoint struct {
	scheme  string
	host    string
	address string
}

func parseEndpoint(rawURL string) (tlsEndpoint, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return tlsEndpoint{}, errors.New("NAS URL must be an absolute http or https URL")
	}
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return tlsEndpoint{scheme: parsed.Scheme, host: parsed.Hostname(), address: net.JoinHostPort(parsed.Hostname(), port)}, nil
}

// NormalizeFingerprint accepts an optional colon-delimited SHA-256
// fingerprint and returns lowercase hexadecimal form.
func NormalizeFingerprint(value string) (string, error) {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), ":", ""))
	if value == "" {
		return "", nil
	}
	if len(value) != sha256.Size*2 {
		return "", errors.New("certificate fingerprint must be a SHA-256 fingerprint")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", errors.New("certificate fingerprint must contain only hexadecimal characters")
	}
	return value, nil
}
