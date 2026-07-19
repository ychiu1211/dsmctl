package synology

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/downloadstation"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	downloadstationops "github.com/ychiu1211/dsmctl/internal/synology/operations/downloadstation"
)

type DownloadStationServiceState = downloadstation.ServiceState
type DownloadStationTasks = downloadstation.Tasks
type DownloadStationTask = downloadstation.Task
type DownloadStationStatistics = downloadstation.Statistics
type DownloadStationSettings = downloadstation.Settings
type DownloadStationTaskChange = downloadstation.TaskChange
type DownloadStationTaskMutationResult = downloadstation.TaskMutationResult
type DownloadStationBTSettings = downloadstation.BTSettings
type DownloadStationSettingsChange = downloadstation.SettingsChange
type DownloadStationSettingsMutationResult = downloadstation.SettingsMutationResult
type DownloadStationCapabilities = downloadstation.Capabilities

func (c *Client) downloadStationEvidenceLocked() downloadstation.PackageEvidence {
	evidence := downloadstation.PackageEvidence{ID: downloadstationops.PackageID}
	if installed, ok := c.target.InstalledPackage(downloadstationops.PackageID); ok {
		evidence.Installed = true
		evidence.Version = installed.Version.Raw
		evidence.Running = installed.Running
	}
	return evidence
}

func downloadStationReadError(what string, evidence downloadstation.PackageEvidence, err error) error {
	if evidence.Installed && !evidence.Running {
		return fmt.Errorf("get Download Station %s: the DownloadStation package is installed but not running; start it with a package lifecycle plan and retry: %w", what, err)
	}
	return fmt.Errorf("get Download Station %s: %w", what, err)
}

// DownloadStationServiceState reads the Download Station service configuration.
func (c *Client) DownloadStationServiceState(ctx context.Context) (DownloadStationServiceState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, downloadstationops.APINames()...); err != nil {
		return DownloadStationServiceState{}, fmt.Errorf("prepare Download Station target: %w", err)
	}
	evidence := c.downloadStationEvidenceLocked()
	state, _, err := downloadstationops.ExecuteService(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return DownloadStationServiceState{}, downloadStationReadError("service configuration", evidence, err)
	}
	state.Package = evidence
	c.target.AddCapability(downloadstationops.ServiceReadCapabilityName)
	return state, nil
}

// DownloadStationTasks reads the download task list.
func (c *Client) DownloadStationTasks(ctx context.Context) (DownloadStationTasks, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, downloadstationops.APINames()...); err != nil {
		return DownloadStationTasks{}, fmt.Errorf("prepare Download Station target: %w", err)
	}
	evidence := c.downloadStationEvidenceLocked()
	tasks, _, err := downloadstationops.ExecuteTask(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return DownloadStationTasks{}, downloadStationReadError("tasks", evidence, err)
	}
	tasks.Package = evidence
	c.target.AddCapability(downloadstationops.TaskReadCapabilityName)
	return tasks, nil
}

// DownloadStationStatistics reads the aggregate transfer statistics.
func (c *Client) DownloadStationStatistics(ctx context.Context) (DownloadStationStatistics, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, downloadstationops.APINames()...); err != nil {
		return DownloadStationStatistics{}, fmt.Errorf("prepare Download Station target: %w", err)
	}
	evidence := c.downloadStationEvidenceLocked()
	stats, _, err := downloadstationops.ExecuteStatistic(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return DownloadStationStatistics{}, downloadStationReadError("statistics", evidence, err)
	}
	stats.Package = evidence
	c.target.AddCapability(downloadstationops.StatisticReadCapabilityName)
	return stats, nil
}

// DownloadStationSettings reads the full detailed configuration (BT, eMule,
// FTP/HTTP, NZB, auto-extraction, location, RSS, scheduler, and general) from
// the SYNO.DownloadStation2.Settings.* APIs.
func (c *Client) DownloadStationSettings(ctx context.Context) (DownloadStationSettings, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, downloadstationops.APINames()...); err != nil {
		return DownloadStationSettings{}, fmt.Errorf("prepare Download Station target: %w", err)
	}
	evidence := c.downloadStationEvidenceLocked()
	settings, _, err := downloadstationops.ExecuteSettings(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return DownloadStationSettings{}, downloadStationReadError("settings", evidence, err)
	}
	settings.Package = evidence
	c.target.AddCapability(downloadstationops.SettingsReadCapabilityName)
	return settings, nil
}

// ApplyDownloadStationTaskChange performs a guarded task mutation (create,
// pause, resume, or delete). The caller (application plan/apply) has already
// validated the change and confirmed the target tasks.
func (c *Client) ApplyDownloadStationTaskChange(ctx context.Context, change DownloadStationTaskChange) (DownloadStationTaskMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, downloadstationops.APINames()...); err != nil {
		return DownloadStationTaskMutationResult{}, fmt.Errorf("prepare Download Station mutation target: %w", err)
	}
	result, _, err := downloadstationops.ExecuteTaskWrite(ctx, c.target, lockedExecutor{client: c}, change)
	if err != nil {
		return DownloadStationTaskMutationResult{}, downloadStationReadError("task change", c.downloadStationEvidenceLocked(), err)
	}
	return result, nil
}

// DownloadStationSettingsGroup reads the current state of one settings group as
// raw JSON, so a guarded plan can bind to the complete group without the
// application layer needing a typed accessor per group.
func (c *Client) DownloadStationSettingsGroup(ctx context.Context, group string) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, downloadstationops.APINames()...); err != nil {
		return nil, fmt.Errorf("prepare Download Station target: %w", err)
	}
	evidence := c.downloadStationEvidenceLocked()
	switch group {
	case "bt":
		bt, _, err := downloadstationops.ExecuteBTGet(ctx, c.target, lockedExecutor{client: c})
		if err != nil {
			return nil, downloadStationReadError("BT settings", evidence, err)
		}
		return json.Marshal(bt)
	case "ftp_http":
		fh, _, err := downloadstationops.ExecuteFtpHttpGet(ctx, c.target, lockedExecutor{client: c})
		if err != nil {
			return nil, downloadStationReadError("FTP/HTTP settings", evidence, err)
		}
		return json.Marshal(fh)
	default:
		return nil, fmt.Errorf("unsupported settings group %q", group)
	}
}

// ApplyDownloadStationSettingsChange merges a settings-group patch into the
// freshly read full group object and submits it, so a field the caller did not
// specify is never reset.
func (c *Client) ApplyDownloadStationSettingsChange(ctx context.Context, change DownloadStationSettingsChange) (DownloadStationSettingsMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, downloadstationops.APINames()...); err != nil {
		return DownloadStationSettingsMutationResult{}, fmt.Errorf("prepare Download Station mutation target: %w", err)
	}
	evidence := c.downloadStationEvidenceLocked()
	switch {
	case change.BT != nil:
		current, _, err := downloadstationops.ExecuteBTGet(ctx, c.target, lockedExecutor{client: c})
		if err != nil {
			return DownloadStationSettingsMutationResult{}, downloadStationReadError("BT settings", evidence, err)
		}
		result, _, err := downloadstationops.ExecuteBTSet(ctx, c.target, lockedExecutor{client: c}, mergeBTSettings(current, *change.BT))
		if err != nil {
			return DownloadStationSettingsMutationResult{}, fmt.Errorf("apply Download Station BT settings: %w", err)
		}
		return result, nil
	case change.FtpHttp != nil:
		current, _, err := downloadstationops.ExecuteFtpHttpGet(ctx, c.target, lockedExecutor{client: c})
		if err != nil {
			return DownloadStationSettingsMutationResult{}, downloadStationReadError("FTP/HTTP settings", evidence, err)
		}
		result, _, err := downloadstationops.ExecuteFtpHttpSet(ctx, c.target, lockedExecutor{client: c}, mergeFtpHttpSettings(current, *change.FtpHttp))
		if err != nil {
			return DownloadStationSettingsMutationResult{}, fmt.Errorf("apply Download Station FTP/HTTP settings: %w", err)
		}
		return result, nil
	default:
		return DownloadStationSettingsMutationResult{}, fmt.Errorf("settings change has no supported group patch")
	}
}

func mergeFtpHttpSettings(current downloadstation.FtpHttpSettings, patch downloadstation.FtpHttpSettingsChange) downloadstation.FtpHttpSettings {
	desired := current
	if patch.MaxDownloadRate != nil {
		desired.MaxDownloadRate = *patch.MaxDownloadRate
	}
	if patch.EnableMaxConn != nil {
		desired.EnableMaxConn = *patch.EnableMaxConn
	}
	if patch.MaxConn != nil {
		desired.MaxConn = *patch.MaxConn
	}
	return desired
}

func mergeBTSettings(current downloadstation.BTSettings, patch downloadstation.BTSettingsChange) downloadstation.BTSettings {
	desired := current
	if patch.TCPPort != nil {
		desired.TCPPort = *patch.TCPPort
	}
	if patch.DHTPort != nil {
		desired.DHTPort = *patch.DHTPort
	}
	if patch.EnableDHT != nil {
		desired.EnableDHT = *patch.EnableDHT
	}
	if patch.EnablePortForwarding != nil {
		desired.EnablePortForwarding = *patch.EnablePortForwarding
	}
	if patch.EnablePreview != nil {
		desired.EnablePreview = *patch.EnablePreview
	}
	if patch.Encryption != nil {
		desired.Encryption = *patch.Encryption
	}
	if patch.MaxDownloadRate != nil {
		desired.MaxDownloadRate = *patch.MaxDownloadRate
	}
	if patch.MaxUploadRate != nil {
		desired.MaxUploadRate = *patch.MaxUploadRate
	}
	if patch.MaxPeer != nil {
		desired.MaxPeer = *patch.MaxPeer
	}
	if patch.SeedingRatio != nil {
		desired.SeedingRatio = *patch.SeedingRatio
	}
	if patch.SeedingInterval != nil {
		desired.SeedingInterval = *patch.SeedingInterval
	}
	if patch.EnableSeedingAutoRemove != nil {
		desired.EnableSeedingAutoRemove = *patch.EnableSeedingAutoRemove
	}
	return desired
}

// DownloadStationCapabilities reports the Download Station reads plus package
// evidence, each selected independently and gated on the installed package.
func (c *Client) DownloadStationCapabilities(ctx context.Context) (DownloadStationCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, downloadstationops.APINames()...); err != nil {
		return DownloadStationCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare Download Station capabilities target: %w", err)
	}
	selectors := []func(compatibility.Target) (compatibility.Selection, error){
		downloadstationops.SelectService,
		downloadstationops.SelectTask,
		downloadstationops.SelectStatistic,
		downloadstationops.SelectSettings,
		downloadstationops.SelectTaskWrite,
		downloadstationops.SelectSettingsWrite,
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selectOperation := range selectors {
		selection, err := selectOperation(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return DownloadStationCapabilities{}, CompatibilityReport{}, fmt.Errorf("select Download Station backend: %w", err)
		}
		selections = append(selections, selection)
	}
	supported := func(index int) bool { return index < len(selections) && selections[index].Supported }
	capabilityNames := []string{
		downloadstationops.ServiceReadCapabilityName,
		downloadstationops.TaskReadCapabilityName,
		downloadstationops.StatisticReadCapabilityName,
		downloadstationops.SettingsReadCapabilityName,
		downloadstationops.TaskWriteCapabilityName,
		downloadstationops.SettingsWriteCapabilityName,
	}
	for index, name := range capabilityNames {
		if supported(index) {
			c.target.AddCapability(name)
		}
	}
	capabilities := DownloadStationCapabilities{
		Module:        downloadstation.ModuleName,
		Package:       c.downloadStationEvidenceLocked(),
		ServiceRead:   supported(0),
		TaskRead:      supported(1),
		StatisticRead: supported(2),
		SettingsRead:  supported(3),
		TaskWrite:     supported(4),
		SettingsWrite: supported(5),
	}
	return capabilities, c.target.Report(selections...), nil
}
