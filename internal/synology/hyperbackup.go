package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/hyperbackup"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	hyperbackupops "github.com/ychiu1211/dsmctl/internal/synology/operations/hyperbackup"
)

type HyperBackupTasks = hyperbackup.Tasks
type HyperBackupTask = hyperbackup.Task
type HyperBackupTaskDetail = hyperbackup.TaskDetail
type HyperBackupTaskStatus = hyperbackup.TaskStatus
type HyperBackupVersions = hyperbackup.Versions
type HyperBackupLogs = hyperbackup.Logs
type HyperBackupVault = hyperbackup.Vault
type HyperBackupTaskChange = hyperbackup.TaskChange
type HyperBackupTaskMutationResult = hyperbackup.TaskMutationResult
type HyperBackupCapabilities = hyperbackup.Capabilities

func (c *Client) hyperBackupEvidenceLocked(packageID string) hyperbackup.PackageEvidence {
	evidence := hyperbackup.PackageEvidence{ID: packageID}
	if installed, ok := c.target.InstalledPackage(packageID); ok {
		evidence.Installed = true
		evidence.Version = installed.Version.Raw
		evidence.Running = installed.Running
	}
	return evidence
}

func hyperBackupReadError(what string, evidence hyperbackup.PackageEvidence, err error) error {
	if evidence.Installed && !evidence.Running {
		return fmt.Errorf("get Hyper Backup %s: the %s package is installed but not running; start it with a package lifecycle plan and retry: %w", what, evidence.ID, err)
	}
	return fmt.Errorf("get Hyper Backup %s: %w", what, err)
}

// HyperBackupTasks reads the backup task list.
func (c *Client) HyperBackupTasks(ctx context.Context) (HyperBackupTasks, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, hyperbackupops.APINames()...); err != nil {
		return HyperBackupTasks{}, fmt.Errorf("prepare Hyper Backup target: %w", err)
	}
	evidence := c.hyperBackupEvidenceLocked(hyperbackupops.PackageID)
	tasks, _, err := hyperbackupops.ExecuteTasks(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return HyperBackupTasks{}, hyperBackupReadError("tasks", evidence, err)
	}
	tasks.Package = evidence
	c.target.AddCapability(hyperbackupops.TaskReadCapabilityName)
	return tasks, nil
}

// HyperBackupTaskDetail reads one task's repository binding, transfer
// parameters, live status/progress, and destination reachability.
func (c *Client) HyperBackupTaskDetail(ctx context.Context, taskID int) (HyperBackupTaskDetail, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, hyperbackupops.APINames()...); err != nil {
		return HyperBackupTaskDetail{}, fmt.Errorf("prepare Hyper Backup target: %w", err)
	}
	evidence := c.hyperBackupEvidenceLocked(hyperbackupops.PackageID)
	detail, _, err := hyperbackupops.ExecuteDetail(ctx, c.target, lockedExecutor{client: c}, hyperbackupops.DetailInput{TaskID: taskID})
	if err != nil {
		return HyperBackupTaskDetail{}, hyperBackupReadError(fmt.Sprintf("task %d", taskID), evidence, err)
	}
	detail.Package = evidence
	c.target.AddCapability(hyperbackupops.DetailReadCapabilityName)
	return detail, nil
}

// HyperBackupTaskStatus reads one task's live status. The application layer
// uses it to bind and verify run/cancel plans.
func (c *Client) HyperBackupTaskStatus(ctx context.Context, taskID int) (HyperBackupTaskStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, hyperbackupops.APINames()...); err != nil {
		return HyperBackupTaskStatus{}, fmt.Errorf("prepare Hyper Backup target: %w", err)
	}
	evidence := c.hyperBackupEvidenceLocked(hyperbackupops.PackageID)
	status, _, err := hyperbackupops.ExecuteTaskStatus(ctx, c.target, lockedExecutor{client: c}, taskID)
	if err != nil {
		return HyperBackupTaskStatus{}, hyperBackupReadError(fmt.Sprintf("task %d status", taskID), evidence, err)
	}
	return status, nil
}

// HyperBackupVersions reads one page of a task's backup versions.
func (c *Client) HyperBackupVersions(ctx context.Context, taskID, offset, limit int) (HyperBackupVersions, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, hyperbackupops.APINames()...); err != nil {
		return HyperBackupVersions{}, fmt.Errorf("prepare Hyper Backup target: %w", err)
	}
	evidence := c.hyperBackupEvidenceLocked(hyperbackupops.PackageID)
	versions, _, err := hyperbackupops.ExecuteVersions(ctx, c.target, lockedExecutor{client: c}, hyperbackupops.VersionsInput{TaskID: taskID, Offset: offset, Limit: limit})
	if err != nil {
		return HyperBackupVersions{}, hyperBackupReadError(fmt.Sprintf("task %d versions", taskID), evidence, err)
	}
	versions.Package = evidence
	c.target.AddCapability(hyperbackupops.VersionReadCapabilityName)
	return versions, nil
}

// HyperBackupLogs reads one page of the Hyper Backup log feed.
func (c *Client) HyperBackupLogs(ctx context.Context, offset, limit int) (HyperBackupLogs, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, hyperbackupops.APINames()...); err != nil {
		return HyperBackupLogs{}, fmt.Errorf("prepare Hyper Backup target: %w", err)
	}
	evidence := c.hyperBackupEvidenceLocked(hyperbackupops.PackageID)
	logs, _, err := hyperbackupops.ExecuteLogs(ctx, c.target, lockedExecutor{client: c}, hyperbackupops.LogsInput{Offset: offset, Limit: limit})
	if err != nil {
		return HyperBackupLogs{}, hyperBackupReadError("logs", evidence, err)
	}
	logs.Package = evidence
	c.target.AddCapability(hyperbackupops.LogReadCapabilityName)
	return logs, nil
}

// HyperBackupVault reads the Hyper Backup Vault view (inbound targets and the
// parallel-session limit). It is gated on the HyperBackupVault package.
func (c *Client) HyperBackupVault(ctx context.Context) (HyperBackupVault, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, hyperbackupops.APINames()...); err != nil {
		return HyperBackupVault{}, fmt.Errorf("prepare Hyper Backup Vault target: %w", err)
	}
	evidence := c.hyperBackupEvidenceLocked(hyperbackupops.VaultPackageID)
	vault, _, err := hyperbackupops.ExecuteVault(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return HyperBackupVault{}, hyperBackupReadError("vault view", evidence, err)
	}
	vault.Package = evidence
	c.target.AddCapability(hyperbackupops.VaultReadCapabilityName)
	return vault, nil
}

// HyperBackupTaskSecrets carries the destination credential a task create
// resolved at apply time (from a dsmctl profile's stored credential or a
// credential reference). It exists only in memory for the DSM calls and is
// never part of plans, results, or logs.
type HyperBackupTaskSecrets struct {
	DestinationHost     string
	DestinationAccount  string
	DestinationPassword string
	DestinationShare    string
	DestinationPort     int
	TransferEncryption  bool
}

// ApplyHyperBackupTaskChange performs a guarded task action (backup now,
// cancel, or create). The caller (application plan/apply) has already
// validated the change, confirmed the target task's state, and resolved the
// destination credential for a create.
func (c *Client) ApplyHyperBackupTaskChange(ctx context.Context, change HyperBackupTaskChange, secrets HyperBackupTaskSecrets) (HyperBackupTaskMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, hyperbackupops.APINames()...); err != nil {
		return HyperBackupTaskMutationResult{}, fmt.Errorf("prepare Hyper Backup mutation target: %w", err)
	}
	if change.Action == hyperbackup.TaskActionCreate {
		if change.Create == nil {
			return HyperBackupTaskMutationResult{}, fmt.Errorf("a create action requires the create description")
		}
		input := hyperbackupops.TaskCreateInput{
			Spec:     *change.Create,
			Host:     secrets.DestinationHost,
			Account:  secrets.DestinationAccount,
			Password: secrets.DestinationPassword,
			Share:    secrets.DestinationShare,
			Port:     secrets.DestinationPort,
			SSL:      secrets.TransferEncryption,
		}
		result, _, err := hyperbackupops.ExecuteTaskCreate(ctx, c.target, lockedExecutor{client: c}, input)
		if err != nil {
			return HyperBackupTaskMutationResult{}, hyperBackupReadError("task create", c.hyperBackupEvidenceLocked(hyperbackupops.PackageID), err)
		}
		return result, nil
	}
	result, _, err := hyperbackupops.ExecuteTaskRun(ctx, c.target, lockedExecutor{client: c}, change)
	if err != nil {
		return HyperBackupTaskMutationResult{}, hyperBackupReadError("task action", c.hyperBackupEvidenceLocked(hyperbackupops.PackageID), err)
	}
	return result, nil
}

// HyperBackupCapabilities reports the Hyper Backup reads and actions dsmctl
// exposes for the selected NAS, each selected independently. The client side
// and the vault side gate on different packages, so a NAS with only one of
// the two reports the other side unsupported instead of erroring the module.
func (c *Client) HyperBackupCapabilities(ctx context.Context) (HyperBackupCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, hyperbackupops.APINames()...); err != nil {
		return HyperBackupCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare Hyper Backup capabilities target: %w", err)
	}
	selectors := []func(compatibility.Target) (compatibility.Selection, error){
		hyperbackupops.SelectTasks,
		hyperbackupops.SelectDetail,
		hyperbackupops.SelectVersions,
		hyperbackupops.SelectLogs,
		hyperbackupops.SelectVault,
		hyperbackupops.SelectTaskRun,
		hyperbackupops.SelectTaskCreate,
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selectOperation := range selectors {
		selection, err := selectOperation(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return HyperBackupCapabilities{}, CompatibilityReport{}, fmt.Errorf("select Hyper Backup backend: %w", err)
		}
		selections = append(selections, selection)
	}
	supported := func(index int) bool { return index < len(selections) && selections[index].Supported }
	capabilityNames := []string{
		hyperbackupops.TaskReadCapabilityName,
		hyperbackupops.DetailReadCapabilityName,
		hyperbackupops.VersionReadCapabilityName,
		hyperbackupops.LogReadCapabilityName,
		hyperbackupops.VaultReadCapabilityName,
		hyperbackupops.TaskRunCapabilityName,
		hyperbackupops.TaskCreateCapabilityName,
	}
	for index, name := range capabilityNames {
		if supported(index) {
			c.target.AddCapability(name)
		}
	}
	capabilities := HyperBackupCapabilities{
		Module:       hyperbackup.ModuleName,
		Package:      c.hyperBackupEvidenceLocked(hyperbackupops.PackageID),
		VaultPackage: c.hyperBackupEvidenceLocked(hyperbackupops.VaultPackageID),
		TaskRead:     supported(0),
		DetailRead:   supported(1),
		VersionRead:  supported(2),
		LogRead:      supported(3),
		VaultRead:    supported(4),
		TaskRun:      supported(5),
		TaskCreate:   supported(6),
	}
	return capabilities, c.target.Report(selections...), nil
}
