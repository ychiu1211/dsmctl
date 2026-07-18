package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/tftpservice"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	tftpserviceop "github.com/ychiu1211/dsmctl/internal/synology/operations/tftpservice"
)

type TFTPServiceState = tftpservice.State
type TFTPServiceCapabilities = tftpservice.Capabilities
type TFTPServiceChange = tftpservice.Change
type TFTPServiceMutationResult = tftpserviceop.MutationResult

// TFTPServiceState reads the TFTP service configuration.
func (c *Client) TFTPServiceState(ctx context.Context) (TFTPServiceState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, tftpserviceop.APINames()...); err != nil {
		return TFTPServiceState{}, fmt.Errorf("prepare TFTP service target: %w", err)
	}
	settings, _, err := tftpserviceop.ExecuteRead(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return TFTPServiceState{}, fmt.Errorf("get TFTP service settings: %w", err)
	}
	c.target.AddCapability(tftpserviceop.ReadCapabilityName)
	return tftpStateFromSettings(settings), nil
}

// TFTPServiceCapabilities reports the selected TFTP backend.
func (c *Client) TFTPServiceCapabilities(ctx context.Context) (TFTPServiceCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, tftpserviceop.APINames()...); err != nil {
		return TFTPServiceCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare TFTP service capabilities target: %w", err)
	}
	selectors := []func(compatibility.Target) (compatibility.Selection, error){
		tftpserviceop.SelectRead,
		tftpserviceop.SelectSet,
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selectOperation := range selectors {
		selection, err := selectOperation(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return TFTPServiceCapabilities{}, CompatibilityReport{}, fmt.Errorf("select TFTP service backend: %w", err)
		}
		selections = append(selections, selection)
	}
	supported := func(index int) bool { return index < len(selections) && selections[index].Supported }
	if supported(0) {
		c.target.AddCapability(tftpserviceop.ReadCapabilityName)
	}
	if supported(1) {
		c.target.AddCapability(tftpserviceop.SetCapabilityName)
	}
	capabilities := TFTPServiceCapabilities{Read: supported(0), Set: supported(1)}
	return capabilities, c.target.Report(selections...), nil
}

// ApplyTFTPServiceChange applies a partial patch: only the fields present in the
// change are sent, using DSM's set-side field names, so unspecified settings are
// preserved.
func (c *Client) ApplyTFTPServiceChange(ctx context.Context, change TFTPServiceChange) ([]TFTPServiceMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, tftpserviceop.APINames()...); err != nil {
		return nil, fmt.Errorf("prepare TFTP service mutation target: %w", err)
	}
	patch := tftpPatchFromChange(change)
	result, _, err := tftpserviceop.ExecuteSet(ctx, c.target, lockedExecutor{client: c}, patch)
	if err != nil {
		return nil, fmt.Errorf("apply TFTP service settings: %w", err)
	}
	return []TFTPServiceMutationResult{result}, nil
}

func tftpStateFromSettings(settings tftpserviceop.Settings) TFTPServiceState {
	permission := tftpservice.PermissionReadOnly
	if settings.AllowWrite {
		permission = tftpservice.PermissionReadWrite
	}
	return TFTPServiceState{
		Enabled:      settings.Enabled,
		RootPath:     settings.RootPath,
		Permission:   permission,
		LogEnabled:   settings.LogEnabled,
		ClientIPLow:  settings.ClientIPLow,
		ClientIPHigh: settings.ClientIPHigh,
		Timeout:      settings.Timeout,
	}
}

func tftpPatchFromChange(change TFTPServiceChange) tftpserviceop.Patch {
	patch := tftpserviceop.Patch{
		Enabled:    change.Enabled,
		RootPath:   change.RootPath,
		LogEnabled: change.LogEnabled,
		Timeout:    change.Timeout,
	}
	if change.Permission != nil {
		allowWrite := *change.Permission == tftpservice.PermissionReadWrite
		patch.AllowWrite = &allowWrite
	}
	return patch
}
