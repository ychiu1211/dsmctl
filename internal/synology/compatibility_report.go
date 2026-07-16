package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/storageinventory"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/systeminfo"
)

const (
	capabilityAuthSession       = "auth.session"
	capabilityAuthSynoToken     = "auth.syno_token"
	capabilityAuthTrustedDevice = "auth.trusted_device"
	quirkSessionCookieHeader    = "transport.session_cookie_and_token_header"
)

type CompatibilityReport = compatibility.Report

func (c *Client) Compatibility(ctx context.Context) (CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	apiNames := append([]string{authAPI}, storageinventory.APINames()...)
	if err := c.prepareCompatibilityTargetLocked(ctx, apiNames...); err != nil {
		return CompatibilityReport{}, fmt.Errorf("discover compatibility target: %w", err)
	}

	systemSelection, selectionErr := systeminfo.Select(c.target)
	if selectionErr != nil && !compatibility.IsUnsupported(selectionErr) {
		return CompatibilityReport{}, selectionErr
	}
	storageSelection, selectionErr := storageinventory.Select(c.target)
	if selectionErr != nil && !compatibility.IsUnsupported(selectionErr) {
		return CompatibilityReport{}, selectionErr
	}
	c.updateDerivedCapabilitiesLocked()
	return c.target.Report(systemSelection, storageSelection), nil
}

// prepareCompatibilityTargetLocked discovers all APIs used by an operation
// and bootstraps the DSM release through SystemInfo. New operation façades call
// this before selecting variants so DSM-range overrides are eligible on the
// first execution, not only after another command has already run.
func (c *Client) prepareCompatibilityTargetLocked(ctx context.Context, apiNames ...string) error {
	names := append(systeminfo.APINames(), apiNames...)
	if err := c.discoverAPIsLocked(ctx, names...); err != nil {
		return err
	}
	if !c.target.DSM.Known() {
		if _, err := c.systemInfoLocked(ctx); err != nil {
			return fmt.Errorf("bootstrap DSM compatibility target: %w", err)
		}
	}
	return nil
}

func (c *Client) updateDerivedCapabilitiesLocked() {
	if auth, ok := c.target.API(authAPI); ok {
		c.target.AddCapability(capabilityAuthSession)
		if auth.Supports(6) {
			c.target.AddCapability(capabilityAuthSynoToken)
			c.target.AddCapability(capabilityAuthTrustedDevice)
		}
	}
	if _, err := systeminfo.Select(c.target); err == nil {
		c.target.AddCapability(systeminfo.CapabilityName)
	}
	if _, err := storageinventory.Select(c.target); err == nil {
		c.target.AddCapability(storageinventory.CapabilityName)
	}
	// Sending session credentials in both documented parameters and the web UI
	// cookie/header locations is safe across tested DSM versions and fixes Core
	// APIs that reject body-only session parameters.
	c.target.AddQuirk(quirkSessionCookieHeader)
}
