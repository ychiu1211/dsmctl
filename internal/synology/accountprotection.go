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
type AutoBlockChange = accountprotection.AutoBlockChange
type AccountProtectionChange = accountprotection.AccountProtectionChange
type EnforceTwoFactorChange = accountprotection.EnforceTwoFactorChange
type IPListEdit = accountprotection.IPListEdit
type ActiveConnection = accountprotection.ActiveConnection
type AccountProtectionMutationResult = apops.MutationResult

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
	autoBlockSet, err := selectSupported(apops.SelectAutoBlockSet, c.target)
	if err != nil {
		return AccountProtectionCapabilities{}, CompatibilityReport{}, fmt.Errorf("select auto block write backend: %w", err)
	}
	listEdit, err := selectSupported(apops.SelectIPListEdit, c.target)
	if err != nil {
		return AccountProtectionCapabilities{}, CompatibilityReport{}, fmt.Errorf("select auto block list write backend: %w", err)
	}
	protectionSet, err := selectSupported(apops.SelectAccountProtectionSet, c.target)
	if err != nil {
		return AccountProtectionCapabilities{}, CompatibilityReport{}, fmt.Errorf("select account protection write backend: %w", err)
	}
	enforceSet, err := selectSupported(apops.SelectEnforceTwoFactorSet, c.target)
	if err != nil {
		return AccountProtectionCapabilities{}, CompatibilityReport{}, fmt.Errorf("select enforce 2fa write backend: %w", err)
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
	if autoBlockSet.Supported {
		c.target.AddCapability(apops.AutoBlockWriteCapabilityName)
	}
	if listEdit.Supported {
		c.target.AddCapability(apops.AutoBlockListWriteCapabilityName)
	}
	if protectionSet.Supported {
		c.target.AddCapability(apops.AccountProtectionWriteCapabilityName)
	}
	if enforceSet.Supported {
		c.target.AddCapability(apops.EnforceTwoFactorWriteCapabilityName)
	}

	capabilities := AccountProtectionCapabilities{
		Module:                 accountprotection.ModuleName,
		AutoBlockRead:          autoBlock.Supported,
		AutoBlockListRead:      autoBlockList.Supported,
		AccountProtectionRead:  protection.Supported,
		EnforceTwoFactorRead:   enforce.Supported,
		AutoBlockWrite:         autoBlockSet.Supported,
		AutoBlockListWrite:     listEdit.Supported,
		AccountProtectionWrite: protectionSet.Supported,
		EnforceTwoFactorWrite:  enforceSet.Supported,
		DoSPresent:             apops.SupportsDoS(c.target),
		Mutations:              autoBlockSet.Supported || listEdit.Supported || protectionSet.Supported || enforceSet.Supported,
	}
	return capabilities, c.target.Report(autoBlock, autoBlockList, protection, enforce, autoBlockSet, listEdit, protectionSet, enforceSet), nil
}

// ActiveConnections lists currently connected DSM clients so the write layer's
// self-lockout guardrail can protect active sources from being blocked or removed
// from the allow list. It is best-effort: a NAS without the API yields no
// connections rather than an error, and it never reads any session secret.
func (c *Client) ActiveConnections(ctx context.Context) ([]ActiveConnection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, apops.APINames()...); err != nil {
		return nil, fmt.Errorf("prepare account protection target: %w", err)
	}
	return apops.ExecuteActiveConnections(ctx, c.target, lockedExecutor{client: c})
}

// ApplyAutoBlockChange merges the patch into a freshly read complete Auto Block
// state and submits it as one set, so a field the caller did not specify can
// never be silently reset.
func (c *Client) ApplyAutoBlockChange(ctx context.Context, change AutoBlockChange) (AccountProtectionMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, apops.APINames()...); err != nil {
		return AccountProtectionMutationResult{}, fmt.Errorf("prepare account protection mutation target: %w", err)
	}
	current, _, err := apops.ExecuteAutoBlock(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return AccountProtectionMutationResult{}, fmt.Errorf("refresh auto block settings before apply: %w", err)
	}
	desired := mergeAutoBlockChange(current, change)
	result, _, err := apops.ExecuteAutoBlockSet(ctx, c.target, lockedExecutor{client: c}, desired)
	if err != nil {
		return AccountProtectionMutationResult{}, fmt.Errorf("apply auto block settings: %w", err)
	}
	return result, nil
}

// ApplyAccountProtectionChange merges the patch into a freshly read complete
// SmartBlock state and submits it as one set.
func (c *Client) ApplyAccountProtectionChange(ctx context.Context, change AccountProtectionChange) (AccountProtectionMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, apops.APINames()...); err != nil {
		return AccountProtectionMutationResult{}, fmt.Errorf("prepare account protection mutation target: %w", err)
	}
	current, _, err := apops.ExecuteAccountProtection(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return AccountProtectionMutationResult{}, fmt.Errorf("refresh account protection before apply: %w", err)
	}
	desired := mergeAccountProtectionChange(current, change)
	result, _, err := apops.ExecuteAccountProtectionSet(ctx, c.target, lockedExecutor{client: c}, desired)
	if err != nil {
		return AccountProtectionMutationResult{}, fmt.Errorf("apply account protection: %w", err)
	}
	return result, nil
}

// ApplyEnforceTwoFactorChange sets the domain-wide enforced-2FA policy scope. It
// never enrolls a user or reads any OTP secret.
func (c *Client) ApplyEnforceTwoFactorChange(ctx context.Context, change EnforceTwoFactorChange) (AccountProtectionMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, apops.APINames()...); err != nil {
		return AccountProtectionMutationResult{}, fmt.Errorf("prepare account protection mutation target: %w", err)
	}
	current, _, err := apops.ExecuteEnforceTwoFactor(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return AccountProtectionMutationResult{}, fmt.Errorf("refresh enforce 2fa policy before apply: %w", err)
	}
	desired := current
	if change.Option != nil {
		desired.Option = *change.Option
	}
	result, _, err := apops.ExecuteEnforceTwoFactorSet(ctx, c.target, lockedExecutor{client: c}, desired)
	if err != nil {
		return AccountProtectionMutationResult{}, fmt.Errorf("apply enforce 2fa policy: %w", err)
	}
	return result, nil
}

// ApplyAutoBlockListEdit adds or removes exactly one allow/block list entry. It
// never sends a whole-list payload, so sibling entries are untouched.
func (c *Client) ApplyAutoBlockListEdit(ctx context.Context, edit IPListEdit) (AccountProtectionMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, apops.APINames()...); err != nil {
		return AccountProtectionMutationResult{}, fmt.Errorf("prepare account protection mutation target: %w", err)
	}
	result, _, err := apops.ExecuteIPListEdit(ctx, c.target, lockedExecutor{client: c}, edit)
	if err != nil {
		return AccountProtectionMutationResult{}, fmt.Errorf("apply auto block list edit: %w", err)
	}
	return result, nil
}

func mergeAutoBlockChange(current AutoBlockSettings, change AutoBlockChange) AutoBlockSettings {
	desired := current
	if change.Enabled != nil {
		desired.Enabled = *change.Enabled
	}
	if change.Attempts != nil {
		desired.Attempts = *change.Attempts
	}
	if change.WithinMinutes != nil {
		desired.WithinMinutes = *change.WithinMinutes
	}
	if change.ExpireDays != nil {
		desired.ExpireDays = *change.ExpireDays
	}
	if change.ExpireEnabled != nil {
		desired.ExpireEnabled = *change.ExpireEnabled
		// DSM has no separate expire-enable flag on the wire: expiry is off when
		// expire_day is zero. Disabling expiration therefore zeroes the days, and
		// enabling it with no explicit day keeps whatever positive day is set (or
		// leaves zero, which the write layer validates against).
		if !desired.ExpireEnabled {
			desired.ExpireDays = 0
		}
	} else {
		desired.ExpireEnabled = desired.ExpireDays > 0
	}
	return desired
}

func mergeAccountProtectionChange(current AccountProtection, change AccountProtectionChange) AccountProtection {
	desired := current
	if change.Enabled != nil {
		desired.Enabled = *change.Enabled
	}
	if change.UntrustedAttempts != nil {
		desired.UntrustedAttempts = *change.UntrustedAttempts
	}
	if change.UntrustedWithinMinutes != nil {
		desired.UntrustedWithinMinutes = *change.UntrustedWithinMinutes
	}
	if change.UntrustedBlockMinutes != nil {
		desired.UntrustedBlockMinutes = *change.UntrustedBlockMinutes
	}
	if change.TrustedAttempts != nil {
		desired.TrustedAttempts = *change.TrustedAttempts
	}
	if change.TrustedWithinMinutes != nil {
		desired.TrustedWithinMinutes = *change.TrustedWithinMinutes
	}
	if change.TrustedBlockMinutes != nil {
		desired.TrustedBlockMinutes = *change.TrustedBlockMinutes
	}
	return desired
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
