package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/kmip"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	kmipops "github.com/ychiu1211/dsmctl/internal/synology/operations/kmip"
)

type KMIPStatus = kmip.Status
type KMIPCapabilities = kmip.Capabilities

// KMIPStatusState reads the Control Panel / Storage Manager KMIP status: whether
// this NAS runs a local KMIP server and/or acts as a KMIP client, the external
// server it targets, connection health, and the bound certificate identities.
// The single combined SYNO.Storage.CGI.KMIP.get is DSM-core; a NAS that reports
// support_kmip:"no" still reads successfully as the disabled state. No private
// key, escrowed key material, or client credential is ever read.
func (c *Client) KMIPStatusState(ctx context.Context) (KMIPStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, kmipops.APINames()...); err != nil {
		return KMIPStatus{}, fmt.Errorf("prepare KMIP target: %w", err)
	}
	status, selection, err := kmipops.ReadStatus(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return KMIPStatus{}, fmt.Errorf("read KMIP status: %w", err)
	}
	if selection.Supported {
		c.target.AddCapability(kmipops.ReadCapabilityName)
	}
	return status, nil
}

// KMIPCapabilitiesState reports whether the KMIP read surface is available on
// this NAS and whether the NAS itself advertises KMIP support. The read
// capability selects its backend independently and fails closed when the API
// family is absent.
func (c *Client) KMIPCapabilitiesState(ctx context.Context) (KMIPCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, kmipops.APINames()...); err != nil {
		return KMIPCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare KMIP capabilities target: %w", err)
	}
	selection, err := kmipops.Select(c.target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return KMIPCapabilities{}, CompatibilityReport{}, fmt.Errorf("select KMIP backend: %w", err)
	}
	capabilities := KMIPCapabilities{Module: "kmip", Read: selection.Supported}
	if selection.Supported {
		c.target.AddCapability(kmipops.ReadCapabilityName)
		// support_kmip is a field on the get response, not a separate API, so read
		// the status to report whether the NAS actually offers KMIP. A read failure
		// leaves Supported false without failing the capability probe.
		if status, _, statusErr := kmipops.ReadStatus(ctx, c.target, lockedExecutor{client: c}); statusErr == nil {
			capabilities.Supported = status.Supported
		}
	}
	return capabilities, c.target.Report(selection), nil
}
