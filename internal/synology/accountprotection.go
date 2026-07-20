package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/accountprotection"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	apops "github.com/ychiu1211/dsmctl/internal/synology/operations/accountprotection"
)

type AutoBlockSettings = accountprotection.AutoBlockSettings
type AutoBlockLists = accountprotection.AutoBlockLists
type AccountProtection = accountprotection.AccountProtection
type EnforceTwoFactor = accountprotection.EnforceTwoFactor
type AccountProtectionCapabilities = accountprotection.Capabilities

// AutoBlockSettings reads the Auto Block configuration (Control Panel > Security
// > Account > Auto Block). Account protection is DSM core, so the plain
// compatibility target is used.
func (c *Client) AutoBlockSettings(ctx context.Context) (AutoBlockSettings, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, apops.APINames()...); err != nil {
		return AutoBlockSettings{}, fmt.Errorf("prepare account protection target: %w", err)
	}
	settings, _, err := apops.ExecuteAutoBlock(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return AutoBlockSettings{}, fmt.Errorf("get auto block settings: %w", err)
	}
	c.target.AddCapability(apops.AutoBlockReadCapabilityName)
	return settings, nil
}

// AutoBlockLists reads the Auto Block allow and block IP lists.
func (c *Client) AutoBlockLists(ctx context.Context) (AutoBlockLists, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, apops.APINames()...); err != nil {
		return AutoBlockLists{}, fmt.Errorf("prepare account protection target: %w", err)
	}
	lists, _, err := apops.ExecuteAutoBlockList(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return AutoBlockLists{}, fmt.Errorf("get auto block lists: %w", err)
	}
	c.target.AddCapability(apops.AutoBlockListReadCapabilityName)
	return lists, nil
}

// AccountProtection reads the Account Protection thresholds (DSM SmartBlock).
func (c *Client) AccountProtection(ctx context.Context) (AccountProtection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, apops.APINames()...); err != nil {
		return AccountProtection{}, fmt.Errorf("prepare account protection target: %w", err)
	}
	protection, _, err := apops.ExecuteAccountProtection(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return AccountProtection{}, fmt.Errorf("get account protection: %w", err)
	}
	c.target.AddCapability(apops.AccountProtectionReadCapabilityName)
	return protection, nil
}

// EnforceTwoFactor reads the domain-wide enforced-2FA policy scope. It never
// reads any user's OTP secret or recovery code.
func (c *Client) EnforceTwoFactor(ctx context.Context) (EnforceTwoFactor, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, apops.APINames()...); err != nil {
		return EnforceTwoFactor{}, fmt.Errorf("prepare account protection target: %w", err)
	}
	policy, _, err := apops.ExecuteEnforceTwoFactor(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return EnforceTwoFactor{}, fmt.Errorf("get enforce 2fa policy: %w", err)
	}
	c.target.AddCapability(apops.EnforceTwoFactorReadCapabilityName)
	return policy, nil
}

// AccountProtectionCapabilities reports which account-protection reads dsmctl
// exposes for the selected NAS, plus the discovered backends. Each area is an
// independent boundary: one being absent leaves the others usable.
func (c *Client) AccountProtectionCapabilities(ctx context.Context) (AccountProtectionCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, apops.APINames()...); err != nil {
		return AccountProtectionCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare account protection capabilities target: %w", err)
	}

	autoBlock, err := selectSupported(apops.SelectAutoBlock, c.target)
	if err != nil {
		return AccountProtectionCapabilities{}, CompatibilityReport{}, fmt.Errorf("select auto block backend: %w", err)
	}
	autoBlockList, err := selectSupported(apops.SelectAutoBlockList, c.target)
	if err != nil {
		return AccountProtectionCapabilities{}, CompatibilityReport{}, fmt.Errorf("select auto block list backend: %w", err)
	}
	protection, err := selectSupported(apops.SelectAccountProtection, c.target)
	if err != nil {
		return AccountProtectionCapabilities{}, CompatibilityReport{}, fmt.Errorf("select account protection backend: %w", err)
	}
	enforce, err := selectSupported(apops.SelectEnforceTwoFactor, c.target)
	if err != nil {
		return AccountProtectionCapabilities{}, CompatibilityReport{}, fmt.Errorf("select enforce 2fa backend: %w", err)
	}

	if autoBlock.Supported {
		c.target.AddCapability(apops.AutoBlockReadCapabilityName)
	}
	if autoBlockList.Supported {
		c.target.AddCapability(apops.AutoBlockListReadCapabilityName)
	}
	if protection.Supported {
		c.target.AddCapability(apops.AccountProtectionReadCapabilityName)
	}
	if enforce.Supported {
		c.target.AddCapability(apops.EnforceTwoFactorReadCapabilityName)
	}

	capabilities := AccountProtectionCapabilities{
		Module:                accountprotection.ModuleName,
		AutoBlockRead:         autoBlock.Supported,
		AutoBlockListRead:     autoBlockList.Supported,
		AccountProtectionRead: protection.Supported,
		EnforceTwoFactorRead:  enforce.Supported,
		DoSPresent:            apops.SupportsDoS(c.target),
		Mutations:             false,
	}
	return capabilities, c.target.Report(autoBlock, autoBlockList, protection, enforce), nil
}

// selectSupported runs one area selector and swallows the typed unsupported
// error into an unsupported selection so a missing area never fails the whole
// capability read.
func selectSupported(selector func(compatibility.Target) (compatibility.Selection, error), target compatibility.Target) (compatibility.Selection, error) {
	selection, err := selector(target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return compatibility.Selection{}, err
	}
	return selection, nil
}
