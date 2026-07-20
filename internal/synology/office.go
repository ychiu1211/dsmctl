package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/office"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	officeops "github.com/ychiu1211/dsmctl/internal/synology/operations/office"
)

type OfficeInfo = office.Info
type OfficeSystemSettings = office.SystemSettings
type OfficePreferences = office.Preferences
type OfficeFont = office.Font
type OfficeCapabilities = office.Capabilities
type OfficeSystemChange = office.SystemChange
type OfficePreferencesChange = office.PreferencesChange
type OfficeFontChange = office.FontChange
type OfficeMutationResult = officeops.MutationResult

func (c *Client) officeEvidenceLocked() office.PackageEvidence {
	evidence := office.PackageEvidence{ID: officeops.PackageID}
	if installed, ok := c.target.InstalledPackage(officeops.PackageID); ok {
		evidence.Installed = true
		evidence.Version = installed.Version.Raw
		evidence.Running = installed.Running
	}
	return evidence
}

func officeReadError(what string, evidence office.PackageEvidence, err error) error {
	if evidence.Installed && !evidence.Running {
		return fmt.Errorf("get %s: the Synology Office package (Spreadsheet) is installed but not running; start it with a package lifecycle plan and retry: %w", what, err)
	}
	return fmt.Errorf("get %s: %w", what, err)
}

// OfficeInfo reads the Synology Office deployment information for the session
// user.
func (c *Client) OfficeInfo(ctx context.Context) (OfficeInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, officeops.APINames()...); err != nil {
		return OfficeInfo{}, fmt.Errorf("prepare Office target: %w", err)
	}
	info, _, err := officeops.ExecuteInfoRead(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return OfficeInfo{}, officeReadError("Office info", c.officeEvidenceLocked(), err)
	}
	c.target.AddCapability(officeops.InfoReadCapabilityName)
	return info, nil
}

// OfficeSystemSettings reads the system-wide Synology Office configuration.
func (c *Client) OfficeSystemSettings(ctx context.Context) (OfficeSystemSettings, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, officeops.APINames()...); err != nil {
		return OfficeSystemSettings{}, fmt.Errorf("prepare Office target: %w", err)
	}
	settings, _, err := officeops.ExecuteSystemRead(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return OfficeSystemSettings{}, officeReadError("Office system settings", c.officeEvidenceLocked(), err)
	}
	c.target.AddCapability(officeops.SystemReadCapabilityName)
	return settings, nil
}

// OfficePreferences reads the calling user's own Synology Office editor
// preferences.
func (c *Client) OfficePreferences(ctx context.Context) (OfficePreferences, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, officeops.APINames()...); err != nil {
		return OfficePreferences{}, fmt.Errorf("prepare Office target: %w", err)
	}
	preferences, _, err := officeops.ExecutePreferencesRead(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return OfficePreferences{}, officeReadError("Office preferences", c.officeEvidenceLocked(), err)
	}
	c.target.AddCapability(officeops.PreferencesReadCapabilityName)
	return preferences, nil
}

// OfficeFonts lists the Synology Office font inventory.
func (c *Client) OfficeFonts(ctx context.Context) ([]OfficeFont, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, officeops.APINames()...); err != nil {
		return nil, fmt.Errorf("prepare Office target: %w", err)
	}
	fonts, _, err := officeops.ExecuteFontsRead(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return nil, officeReadError("Office fonts", c.officeEvidenceLocked(), err)
	}
	c.target.AddCapability(officeops.FontsReadCapabilityName)
	return fonts, nil
}

// OfficeCapabilities reports the Office settings operations plus the installed
// package evidence the selection used.
func (c *Client) OfficeCapabilities(ctx context.Context) (OfficeCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, officeops.APINames()...); err != nil {
		return OfficeCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare Office capabilities target: %w", err)
	}
	type selector struct {
		selectOperation func(compatibility.Target) (compatibility.Selection, error)
		capability      string
	}
	selectors := []selector{
		{officeops.SelectInfoRead, officeops.InfoReadCapabilityName},
		{officeops.SelectSystemRead, officeops.SystemReadCapabilityName},
		{officeops.SelectSystemSet, officeops.SystemSetCapabilityName},
		{officeops.SelectPreferencesRead, officeops.PreferencesReadCapabilityName},
		{officeops.SelectPreferencesSet, officeops.PreferencesSetCapabilityName},
		{officeops.SelectFontsRead, officeops.FontsReadCapabilityName},
		{officeops.SelectFontsSet, officeops.FontsSetCapabilityName},
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, entry := range selectors {
		selection, err := entry.selectOperation(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return OfficeCapabilities{}, CompatibilityReport{}, fmt.Errorf("select Office backend: %w", err)
		}
		if selection.Supported {
			c.target.AddCapability(entry.capability)
		}
		selections = append(selections, selection)
	}
	supported := func(index int) bool { return index < len(selections) && selections[index].Supported }
	capabilities := OfficeCapabilities{
		Module:          office.ModuleName,
		InfoRead:        supported(0),
		SystemRead:      supported(1),
		SystemSet:       supported(2),
		PreferencesRead: supported(3),
		PreferencesSet:  supported(4),
		FontsRead:       supported(5),
		FontsSet:        supported(6),
		Package:         c.officeEvidenceLocked(),
	}
	return capabilities, c.target.Report(selections...), nil
}

// ApplyOfficeSystemChange applies a partial patch of the system-wide settings:
// only the fields present in the change are sent, so unspecified settings are
// preserved.
func (c *Client) ApplyOfficeSystemChange(ctx context.Context, change OfficeSystemChange) (OfficeMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, officeops.APINames()...); err != nil {
		return OfficeMutationResult{}, fmt.Errorf("prepare Office mutation target: %w", err)
	}
	result, _, err := officeops.ExecuteSystemSet(ctx, c.target, lockedExecutor{client: c}, change)
	if err != nil {
		return OfficeMutationResult{}, fmt.Errorf("apply Office system settings: %w", err)
	}
	return result, nil
}

// ApplyOfficePreferencesChange applies a partial patch of the calling user's
// own editor preferences.
func (c *Client) ApplyOfficePreferencesChange(ctx context.Context, change OfficePreferencesChange) (OfficeMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, officeops.APINames()...); err != nil {
		return OfficeMutationResult{}, fmt.Errorf("prepare Office mutation target: %w", err)
	}
	result, _, err := officeops.ExecutePreferencesSet(ctx, c.target, lockedExecutor{client: c}, change)
	if err != nil {
		return OfficeMutationResult{}, fmt.Errorf("apply Office preferences: %w", err)
	}
	return result, nil
}

// ApplyOfficeFontChange applies one custom-font registry action.
func (c *Client) ApplyOfficeFontChange(ctx context.Context, change OfficeFontChange) (OfficeMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, officeops.APINames()...); err != nil {
		return OfficeMutationResult{}, fmt.Errorf("prepare Office mutation target: %w", err)
	}
	result, _, err := officeops.ExecuteFontsSet(ctx, c.target, lockedExecutor{client: c}, change)
	if err != nil {
		return OfficeMutationResult{}, fmt.Errorf("apply Office font change: %w", err)
	}
	return result, nil
}
