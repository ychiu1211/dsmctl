package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/downloadstation"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	downloadstationops "github.com/ychiu1211/dsmctl/internal/synology/operations/downloadstation"
)

type DownloadStationServiceState = downloadstation.ServiceState
type DownloadStationTasks = downloadstation.Tasks
type DownloadStationStatistics = downloadstation.Statistics
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
	}
	return capabilities, c.target.Report(selections...), nil
}
