package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/certificate"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	certops "github.com/ychiu1211/dsmctl/internal/synology/operations/certificate"
)

type Certificates = certificate.Certificates
type CertificateCapabilities = certificate.Capabilities

// Certificates reads the installed-certificate inventory with each
// certificate's bound services. Certificate management is DSM core, so the
// plain compatibility target (not the package-scoped one) is used.
func (c *Client) Certificates(ctx context.Context) (Certificates, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, certops.APINames()...); err != nil {
		return Certificates{}, fmt.Errorf("prepare certificate target: %w", err)
	}
	certs, _, err := certops.ExecuteCertificates(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return Certificates{}, fmt.Errorf("get certificates: %w", err)
	}
	c.target.AddCapability(certops.CertificatesReadCapabilityName)
	return certs, nil
}

// CertificateCapabilities reports the certificate operations dsmctl exposes for
// the selected NAS, plus the discovered backends.
func (c *Client) CertificateCapabilities(ctx context.Context) (CertificateCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, certops.APINames()...); err != nil {
		return CertificateCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare certificate capabilities target: %w", err)
	}
	selection, err := certops.SelectCertificates(c.target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return CertificateCapabilities{}, CompatibilityReport{}, fmt.Errorf("select certificate backend: %w", err)
	}
	if selection.Supported {
		c.target.AddCapability(certops.CertificatesReadCapabilityName)
	}
	capabilities := CertificateCapabilities{
		Module:           certificate.ModuleName,
		CertificatesRead: selection.Supported,
	}
	return capabilities, c.target.Report(selection), nil
}
