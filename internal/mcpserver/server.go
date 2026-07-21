package mcpserver

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/domain/access"
	"github.com/ychiu1211/dsmctl/internal/domain/certificate"
	"github.com/ychiu1211/dsmctl/internal/domain/controlpanel"
	"github.com/ychiu1211/dsmctl/internal/domain/discovery"
	"github.com/ychiu1211/dsmctl/internal/domain/downloadstation"
	"github.com/ychiu1211/dsmctl/internal/domain/driveadmin"
	"github.com/ychiu1211/dsmctl/internal/domain/externalaccess"
	"github.com/ychiu1211/dsmctl/internal/domain/filestation"
	"github.com/ychiu1211/dsmctl/internal/domain/ftpservices"
	"github.com/ychiu1211/dsmctl/internal/domain/identity"
	"github.com/ychiu1211/dsmctl/internal/domain/nfsexport"
	"github.com/ychiu1211/dsmctl/internal/domain/notification"
	"github.com/ychiu1211/dsmctl/internal/domain/office"
	"github.com/ychiu1211/dsmctl/internal/domain/packagecenter"
	"github.com/ychiu1211/dsmctl/internal/domain/photos"
	"github.com/ychiu1211/dsmctl/internal/domain/resmon"
	"github.com/ychiu1211/dsmctl/internal/domain/rsyncservice"
	"github.com/ychiu1211/dsmctl/internal/domain/san"
	"github.com/ychiu1211/dsmctl/internal/domain/accountprotection"
	firewalldomain "github.com/ychiu1211/dsmctl/internal/domain/firewall"
	networkdomain "github.com/ychiu1211/dsmctl/internal/domain/network"
	"github.com/ychiu1211/dsmctl/internal/domain/loginportal"
	"github.com/ychiu1211/dsmctl/internal/domain/securityadvisor"
	"github.com/ychiu1211/dsmctl/internal/domain/servicediscovery"
	"github.com/ychiu1211/dsmctl/internal/domain/share"
	"github.com/ychiu1211/dsmctl/internal/domain/storage"
	"github.com/ychiu1211/dsmctl/internal/domain/surveillance"
	"github.com/ychiu1211/dsmctl/internal/domain/syslog"
	"github.com/ychiu1211/dsmctl/internal/domain/terminalsnmp"
	"github.com/ychiu1211/dsmctl/internal/domain/tftpservice"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

// maxInlineFileDownload bounds how many bytes get_filestation_file_content will
// return inline; larger files must be streamed with the CLI.
const maxInlineFileDownload = 8 << 20

type discoverLANDevicesInput struct {
	TimeoutSeconds int  `json:"timeout_seconds,omitempty" jsonschema:"How long to listen for device responses, in seconds; defaults to 8 and is capped at 60"`
	Cached         bool `json:"cached,omitempty" jsonschema:"Return the saved cross-run set from previous scans without scanning again; timeout_seconds is ignored"`
}

type discoverLANDevicesOutput struct {
	Devices    []discovery.Device `json:"devices" jsonschema:"Synology devices that answered the findhost broadcast, deduplicated by device"`
	SavedTotal int                `json:"saved_total,omitempty" jsonschema:"Devices in the saved cross-run set after this scan was merged in; larger than the returned count when a scan under-counted under UDP-9999 contention"`
}

type listNASInput struct{}

type listNASOutput struct {
	NAS []config.Summary `json:"nas" jsonschema:"Configured NAS profiles"`
}

type getAuthStatusInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to report every configured profile"`
}

type getAuthStatusOutput struct {
	Statuses []application.AuthStatus `json:"statuses" jsonschema:"Per-NAS credential presence and in-process session status; secret values are never returned"`
}

type getSystemInfoInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getSystemInfoOutput struct {
	NAS    string              `json:"nas" jsonschema:"NAS profile used for the request"`
	System synology.SystemInfo `json:"system" jsonschema:"System information returned by DSM"`
}

type getCapabilitiesInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getCapabilitiesOutput struct {
	NAS    string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Report synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs, capabilities, quirks, and selected operation backends"`
}

type explainEffectiveAccessInput struct {
	NAS           string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	PrincipalType string `json:"principal_type" jsonschema:"Principal kind: user or group"`
	Principal     string `json:"principal" jsonschema:"Local DSM user or group name"`
	ResourceType  string `json:"resource_type" jsonschema:"Resource kind: share or application"`
	Resource      string `json:"resource" jsonschema:"Shared-folder name or application ID"`
}

type explainEffectiveAccessOutput struct {
	NAS         string             `json:"nas" jsonschema:"NAS profile used for the request"`
	Explanation access.Explanation `json:"explanation" jsonschema:"Effective access decision, evidence, and limitations"`
}

type getControlPanelTimeInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getControlPanelTimeStateOutput struct {
	NAS  string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	Time synology.ControlPanelTimeState `json:"time" jsonschema:"Normalized Control Panel time and NTP configuration"`
}

type getControlPanelTimeCapabilitiesOutput struct {
	NAS          string                                `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.ControlPanelTimeCapabilities `json:"capabilities" jsonschema:"Control Panel time module operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport          `json:"report" jsonschema:"Discovered API and selected time-module backend"`
}

type getExternalAccessInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getExternalAccessCapabilitiesOutput struct {
	NAS          string                              `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.ExternalAccessCapabilities `json:"capabilities" jsonschema:"External Access read areas currently exposed by dsmctl"`
	Report       synology.CompatibilityReport        `json:"report" jsonschema:"Discovered APIs and selected External Access backends"`
}

type getExternalAccessAccountOutput struct {
	NAS     string                              `json:"nas" jsonschema:"NAS profile used for the request"`
	Account synology.ExternalAccessAccountState `json:"account" jsonschema:"Normalized Synology Account binding without any account token"`
}

type getExternalAccessQuickConnectOutput struct {
	NAS          string                                   `json:"nas" jsonschema:"NAS profile used for the request"`
	QuickConnect synology.ExternalAccessQuickConnectState `json:"quickconnect" jsonschema:"Normalized QuickConnect configuration and live status"`
}

type getExternalAccessDDNSOutput struct {
	NAS  string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	DDNS synology.ExternalAccessDDNSState `json:"ddns" jsonschema:"Normalized DDNS records and detected external addresses"`
}

type getExternalAccessPortForwardOutput struct {
	NAS         string                                  `json:"nas" jsonschema:"NAS profile used for the request"`
	PortForward synology.ExternalAccessPortForwardState `json:"port_forward" jsonschema:"Normalized router configuration and port-forwarding rules"`
}

type planExternalAccessQuickConnectChangeInput struct {
	NAS     string                            `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request externalaccess.QuickConnectChange `json:"request" jsonschema:"Patch-only QuickConnect relay-toggle intent"`
}

type planExternalAccessQuickConnectChangeOutput struct {
	Plan application.ExternalAccessQuickConnectPlan `json:"plan" jsonschema:"Validated plan bound to the complete observed QuickConnect state and approval hash"`
}

type applyExternalAccessQuickConnectPlanInput struct {
	Plan         application.ExternalAccessQuickConnectPlan `json:"plan" jsonschema:"Approved QuickConnect plan produced by plan_external_access_quickconnect_change"`
	ApprovalHash string                                     `json:"approval_hash" jsonschema:"Exact SHA-256 approval hash from the plan"`
}

type applyExternalAccessQuickConnectPlanOutput struct {
	Result application.ExternalAccessQuickConnectApplyResult `json:"result" jsonschema:"Apply outcome including the selected DSM mutation backend"`
}

type planExternalAccessQuickConnectConfigChangeInput struct {
	NAS     string                                  `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request externalaccess.QuickConnectConfigChange `json:"request" jsonschema:"Patch-only QuickConnect enable/alias/region intent"`
}

type planExternalAccessQuickConnectConfigChangeOutput struct {
	Plan application.ExternalAccessQuickConnectConfigPlan `json:"plan" jsonschema:"Validated plan bound to the observed QuickConnect state and approval hash"`
}

type applyExternalAccessQuickConnectConfigPlanInput struct {
	Plan         application.ExternalAccessQuickConnectConfigPlan `json:"plan" jsonschema:"Approved plan produced by plan_external_access_quickconnect_config_change"`
	ApprovalHash string                                           `json:"approval_hash" jsonschema:"Exact SHA-256 approval hash from the plan"`
}

type applyExternalAccessQuickConnectConfigPlanOutput struct {
	Result application.ExternalAccessQuickConnectConfigApplyResult `json:"result" jsonschema:"Apply outcome including the selected DSM mutation backend"`
}

type planExternalAccessQuickConnectPermissionChangeInput struct {
	NAS     string                                      `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request externalaccess.QuickConnectPermissionChange `json:"request" jsonschema:"Per-service external-exposure intent"`
}

type planExternalAccessQuickConnectPermissionChangeOutput struct {
	Plan application.ExternalAccessQuickConnectPermissionPlan `json:"plan" jsonschema:"Validated plan bound to the observed QuickConnect state and approval hash"`
}

type applyExternalAccessQuickConnectPermissionPlanInput struct {
	Plan         application.ExternalAccessQuickConnectPermissionPlan `json:"plan" jsonschema:"Approved plan produced by plan_external_access_quickconnect_permission_change"`
	ApprovalHash string                                               `json:"approval_hash" jsonschema:"Exact SHA-256 approval hash from the plan"`
}

type applyExternalAccessQuickConnectPermissionPlanOutput struct {
	Result application.ExternalAccessQuickConnectPermissionApplyResult `json:"result" jsonschema:"Apply outcome including the selected DSM mutation backend"`
}

type planExternalAccessDDNSChangeInput struct {
	NAS     string                          `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request externalaccess.DDNSRecordChange `json:"request" jsonschema:"DDNS record create/set/delete intent; the password is a credential reference, never a value"`
}

type planExternalAccessDDNSChangeOutput struct {
	Plan application.ExternalAccessDDNSPlan `json:"plan" jsonschema:"Validated plan bound to the observed DDNS state and approval hash"`
}

type applyExternalAccessDDNSPlanInput struct {
	Plan         application.ExternalAccessDDNSPlan `json:"plan" jsonschema:"Approved plan produced by plan_external_access_ddns_change"`
	ApprovalHash string                             `json:"approval_hash" jsonschema:"Exact SHA-256 approval hash from the plan"`
}

type applyExternalAccessDDNSPlanOutput struct {
	Result application.ExternalAccessDDNSApplyResult `json:"result" jsonschema:"Apply outcome including the selected DSM mutation backend"`
}

type getDownloadStationInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getDownloadStationCapabilitiesOutput struct {
	NAS          string                               `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.DownloadStationCapabilities `json:"capabilities" jsonschema:"Download Station reads currently exposed by dsmctl"`
	Report       synology.CompatibilityReport         `json:"report" jsonschema:"Discovered APIs and selected Download Station backends"`
}

type getDownloadStationServiceOutput struct {
	NAS     string                               `json:"nas" jsonschema:"NAS profile used for the request"`
	Service synology.DownloadStationServiceState `json:"service" jsonschema:"Normalized Download Station service configuration"`
}

type getDownloadStationTasksOutput struct {
	NAS   string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	Tasks synology.DownloadStationTasks `json:"tasks" jsonschema:"Download task list"`
}

type getDownloadStationStatisticsOutput struct {
	NAS        string                             `json:"nas" jsonschema:"NAS profile used for the request"`
	Statistics synology.DownloadStationStatistics `json:"statistics" jsonschema:"Aggregate transfer statistics"`
}

type fileStationNASInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getFileStationCapabilitiesOutput struct {
	NAS          string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.FileStationCapabilities `json:"capabilities" jsonschema:"FileStation reads currently exposed by dsmctl"`
	Report       synology.CompatibilityReport     `json:"report" jsonschema:"Discovered APIs and selected FileStation backends"`
}

type getFileStationInfoOutput struct {
	NAS     string                      `json:"nas" jsonschema:"NAS profile used for the request"`
	Service synology.FileStationService `json:"service" jsonschema:"FileStation service information for the current session"`
}

type listFileStationSharesInput struct {
	NAS          string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	OnlyWritable bool   `json:"only_writable,omitempty" jsonschema:"Return only shared folders the current session can write"`
	Limit        int    `json:"limit,omitempty" jsonschema:"Maximum number of shared folders to return; 0 uses the DSM default"`
	Offset       int    `json:"offset,omitempty" jsonschema:"Offset of the first shared folder to return"`
}

type fileStationListingOutput struct {
	NAS     string                      `json:"nas" jsonschema:"NAS profile used for the request"`
	Listing synology.FileStationListing `json:"listing" jsonschema:"Shared-folder, directory, or virtual-folder listing"`
}

type listFileStationInput struct {
	NAS           string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Path          string `json:"path" jsonschema:"Absolute folder path to enumerate, for example /share/dir"`
	Limit         int    `json:"limit,omitempty" jsonschema:"Maximum number of entries to return; 0 uses the DSM default"`
	Offset        int    `json:"offset,omitempty" jsonschema:"Offset of the first entry to return"`
	SortBy        string `json:"sort_by,omitempty" jsonschema:"Sort key: name, size, mtime, atime, ctime, crtime, user, group, posix, or type"`
	SortDirection string `json:"sort_direction,omitempty" jsonschema:"Sort direction: asc or desc"`
	Pattern       string `json:"pattern,omitempty" jsonschema:"Glob pattern that entry names must match"`
	FileType      string `json:"file_type,omitempty" jsonschema:"Restrict to file, dir, or all (default)"`
}

type getFileStationEntryInfoInput struct {
	NAS   string   `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Paths []string `json:"paths" jsonschema:"Absolute paths whose information is requested"`
}

type fileStationEntryInfoOutput struct {
	NAS  string                   `json:"nas" jsonschema:"NAS profile used for the request"`
	Info synology.FileStationInfo `json:"info" jsonschema:"Requested entry details"`
}

type searchFileStationInput struct {
	NAS       string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Path      string `json:"path" jsonschema:"Absolute folder path to search within"`
	Pattern   string `json:"pattern,omitempty" jsonschema:"Glob pattern that entry names must match"`
	Extension string `json:"extension,omitempty" jsonschema:"File extension filter, without a leading dot"`
	FileType  string `json:"file_type,omitempty" jsonschema:"Restrict to file, dir, or all (default)"`
	Recursive bool   `json:"recursive,omitempty" jsonschema:"Search subdirectories recursively"`
}

type fileStationSearchOutput struct {
	NAS    string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Result synology.FileStationSearchResult `json:"result" jsonschema:"Completed search result"`
}

type fileStationDirSizeInput struct {
	NAS   string   `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Paths []string `json:"paths" jsonschema:"Absolute folder paths whose aggregate size is computed"`
}

type fileStationDirSizeOutput struct {
	NAS     string                      `json:"nas" jsonschema:"NAS profile used for the request"`
	DirSize synology.FileStationDirSize `json:"dir_size" jsonschema:"Aggregate directory size"`
}

type fileStationMD5Input struct {
	NAS  string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Path string `json:"path" jsonschema:"Absolute file path whose MD5 digest is computed"`
}

type fileStationMD5Output struct {
	NAS string                  `json:"nas" jsonschema:"NAS profile used for the request"`
	MD5 synology.FileStationMD5 `json:"md5" jsonschema:"Computed MD5 digest"`
}

type checkFileStationPermissionInput struct {
	NAS           string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Path          string `json:"path" jsonschema:"Absolute folder path where a write is probed"`
	Filename      string `json:"filename,omitempty" jsonschema:"Optional file name to probe within the folder"`
	Overwrite     bool   `json:"overwrite,omitempty" jsonschema:"Probe assuming an existing file would be overwritten"`
	CreateParents bool   `json:"create_parents,omitempty" jsonschema:"Probe assuming missing parent folders would be created"`
}

type fileStationPermissionOutput struct {
	NAS        string                              `json:"nas" jsonschema:"NAS profile used for the request"`
	Permission synology.FileStationPermissionCheck `json:"permission" jsonschema:"Write-permission probe result"`
}

type getFileStationFileContentInput struct {
	NAS      string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Path     string `json:"path" jsonschema:"Absolute file path to download"`
	MaxBytes int64  `json:"max_bytes,omitempty" jsonschema:"Maximum bytes to return inline; capped at 8 MiB regardless"`
}

type getFileStationFileContentOutput struct {
	NAS           string `json:"nas" jsonschema:"NAS profile used for the request"`
	Path          string `json:"path" jsonschema:"Downloaded NAS path"`
	Size          int64  `json:"size" jsonschema:"Number of bytes returned"`
	ContentType   string `json:"content_type,omitempty" jsonschema:"Content type reported by DSM"`
	Filename      string `json:"filename,omitempty" jsonschema:"File name reported by DSM"`
	ContentBase64 string `json:"content_base64" jsonschema:"Base64-encoded file content"`
}

type getFileStationThumbnailInput struct {
	NAS      string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Path     string `json:"path" jsonschema:"Absolute path of the image to thumbnail"`
	Size     string `json:"size,omitempty" jsonschema:"Thumbnail size: small (default), medium, large, or original"`
	Rotate   int    `json:"rotate,omitempty" jsonschema:"Rotation index 0-4 (0 = none)"`
	MaxBytes int64  `json:"max_bytes,omitempty" jsonschema:"Maximum bytes to return inline; capped at 8 MiB regardless"`
}

type getFileStationThumbnailOutput struct {
	NAS           string `json:"nas" jsonschema:"NAS profile used for the request"`
	Path          string `json:"path" jsonschema:"Thumbnailed NAS path"`
	Size          int64  `json:"size" jsonschema:"Number of bytes returned"`
	ContentType   string `json:"content_type,omitempty" jsonschema:"Content type reported by DSM"`
	ContentBase64 string `json:"content_base64" jsonschema:"Base64-encoded thumbnail image content"`
}

type getFileStationFavoritesOutput struct {
	NAS       string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	Favorites synology.FileStationFavorites `json:"favorites" jsonschema:"Personal FileStation favorites"`
}

type planFileStationChangeInput struct {
	NAS     string                    `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request filestation.ChangeRequest `json:"request" jsonschema:"FileStation mutation: create_folder, rename, copy, move, delete, compress, extract, or upload. Upload reads local_path on the machine running dsmctl"`
}

type planFileStationChangeOutput struct {
	Plan application.FilePlan `json:"plan" jsonschema:"Validated plan bound to the observed path state and approval hash"`
}

type applyFileStationPlanInput struct {
	Plan         application.FilePlan `json:"plan" jsonschema:"Unmodified plan returned by plan_filestation_change"`
	ApprovalHash string               `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved FileStation plan"`
}

type applyFileStationPlanOutput struct {
	Result application.FileApplyResult `json:"result" jsonschema:"FileStation mutation result after stale-state and postcondition checks"`
}

type getFileStationSharingLinksOutput struct {
	NAS   string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Links synology.FileStationSharingLinks `json:"links" jsonschema:"Public sharing links"`
}

type getFileStationBackgroundTasksOutput struct {
	NAS   string                              `json:"nas" jsonschema:"NAS profile used for the request"`
	Tasks synology.FileStationBackgroundTasks `json:"tasks" jsonschema:"Background file-operation tasks"`
}

type getDownloadStationSettingsOutput struct {
	NAS      string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Settings synology.DownloadStationSettings `json:"settings" jsonschema:"Full detailed Download Station configuration"`
}

type planDownloadStationTaskChangeInput struct {
	NAS     string                     `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request downloadstation.TaskChange `json:"request" jsonschema:"Task create/pause/resume/delete/edit intent"`
}

type planDownloadStationTaskChangeOutput struct {
	Plan application.DownloadStationTaskPlan `json:"plan" jsonschema:"Validated plan bound to the observed target tasks and approval hash"`
}

type applyDownloadStationTaskPlanInput struct {
	Plan         application.DownloadStationTaskPlan `json:"plan" jsonschema:"Approved task plan from plan_download_station_task_change"`
	ApprovalHash string                              `json:"approval_hash" jsonschema:"Exact SHA-256 approval hash from the plan"`
}

type applyDownloadStationTaskPlanOutput struct {
	Result application.DownloadStationTaskApplyResult `json:"result" jsonschema:"Apply outcome including the affected task ids"`
}

type planDownloadStationSettingsChangeInput struct {
	NAS     string                         `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request downloadstation.SettingsChange `json:"request" jsonschema:"Patch-only settings intent (exactly one group: BT, FTP/HTTP, RSS, location, scheduler, global, auto_extraction, or nzb)"`
}

type planDownloadStationSettingsChangeOutput struct {
	Plan application.DownloadStationSettingsPlan `json:"plan" jsonschema:"Validated plan bound to the complete observed settings group and approval hash"`
}

type applyDownloadStationSettingsPlanInput struct {
	Plan         application.DownloadStationSettingsPlan `json:"plan" jsonschema:"Approved settings plan from plan_download_station_settings_change"`
	ApprovalHash string                                  `json:"approval_hash" jsonschema:"Exact SHA-256 approval hash from the plan"`
}

type applyDownloadStationSettingsPlanOutput struct {
	Result application.DownloadStationSettingsApplyResult `json:"result" jsonschema:"Apply outcome including the selected DSM mutation backend"`
}

type getHyperBackupInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit for the default"`
}

type getHyperBackupCapabilitiesOutput struct {
	NAS          string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.HyperBackupCapabilities `json:"capabilities" jsonschema:"Hyper Backup reads and actions currently exposed by dsmctl"`
	Report       synology.CompatibilityReport     `json:"report" jsonschema:"Discovered APIs and selected Hyper Backup backends"`
}

type getHyperBackupTasksOutput struct {
	NAS   string                    `json:"nas" jsonschema:"NAS profile used for the request"`
	Tasks synology.HyperBackupTasks `json:"tasks" jsonschema:"Backup task list"`
}

type getHyperBackupTaskInput struct {
	NAS    string `json:"nas,omitempty" jsonschema:"NAS profile name; omit for the default"`
	TaskID int    `json:"task_id" jsonschema:"Backup task identifier from get_hyper_backup_tasks"`
}

type getHyperBackupTaskOutput struct {
	NAS  string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	Task synology.HyperBackupTaskDetail `json:"task" jsonschema:"Full task view: repository, transfer options, live status, destination reachability"`
}

type getHyperBackupVersionsInput struct {
	NAS    string `json:"nas,omitempty" jsonschema:"NAS profile name; omit for the default"`
	TaskID int    `json:"task_id" jsonschema:"Backup task identifier from get_hyper_backup_tasks"`
	Offset int    `json:"offset,omitempty" jsonschema:"Number of versions to skip"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum versions to return; default 50"`
}

type getHyperBackupVersionsOutput struct {
	NAS      string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Versions synology.HyperBackupVersions `json:"versions" jsonschema:"Backup versions of the task"`
}

type getHyperBackupLogsInput struct {
	NAS    string `json:"nas,omitempty" jsonschema:"NAS profile name; omit for the default"`
	Offset int    `json:"offset,omitempty" jsonschema:"Number of log entries to skip"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum log entries to return; default 50"`
}

type getHyperBackupLogsOutput struct {
	NAS  string                   `json:"nas" jsonschema:"NAS profile used for the request"`
	Logs synology.HyperBackupLogs `json:"logs" jsonschema:"Hyper Backup log feed page"`
}

type getHyperBackupApplicationsOutput struct {
	NAS          string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Applications synology.HyperBackupApplications `json:"applications" jsonschema:"Packages Hyper Backup can include in a backup task, with per-application eligibility"`
}

type getHyperBackupVaultOutput struct {
	NAS   string                    `json:"nas" jsonschema:"NAS profile used for the request"`
	Vault synology.HyperBackupVault `json:"vault" jsonschema:"Hyper Backup Vault view of this NAS as a backup destination"`
}

type planHyperBackupTaskChangeInput struct {
	NAS     string                        `json:"nas,omitempty" jsonschema:"NAS profile name; omit for the default"`
	Request synology.HyperBackupTaskChange `json:"request" jsonschema:"Task action intent: backup (run now) or cancel, plus the task_id"`
}

type planHyperBackupTaskChangeOutput struct {
	Plan application.HyperBackupTaskPlan `json:"plan" jsonschema:"Validated plan bound to the observed task state and approval hash"`
}

type applyHyperBackupTaskPlanInput struct {
	Plan         application.HyperBackupTaskPlan `json:"plan" jsonschema:"Approved task plan from plan_hyper_backup_task_change"`
	ApprovalHash string                          `json:"approval_hash" jsonschema:"Exact SHA-256 approval hash from the plan"`
}

type applyHyperBackupTaskPlanOutput struct {
	Result application.HyperBackupTaskApplyResult `json:"result" jsonschema:"Apply outcome including the DSM mutation backend used"`
}

type planControlPanelTimeChangeInput struct {
	NAS     string                  `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request controlpanel.TimeChange `json:"request" jsonschema:"Patch-only time zone, display format, or NTP intent"`
}

type planControlPanelTimeChangeOutput struct {
	Plan application.ControlPanelTimePlan `json:"plan" jsonschema:"Validated plan bound to the complete observed time state and approval hash"`
}

type applyControlPanelTimePlanInput struct {
	Plan         application.ControlPanelTimePlan `json:"plan" jsonschema:"Unmodified plan returned by plan_control_panel_time_change"`
	ApprovalHash string                           `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved time plan"`
}

type applyControlPanelTimePlanOutput struct {
	Result application.ControlPanelTimeApplyResult `json:"result" jsonschema:"Time mutation result after stale-state and postcondition checks"`
}

type planSystemHostnameChangeInput struct {
	NAS     string                           `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request application.SystemHostnameChange `json:"request" jsonschema:"New DSM server name (hostname)"`
}

type planSystemHostnameChangeOutput struct {
	Plan application.SystemHostnamePlan `json:"plan" jsonschema:"Validated plan bound to the observed server name and approval hash"`
}

type applySystemHostnamePlanInput struct {
	Plan         application.SystemHostnamePlan `json:"plan" jsonschema:"Unmodified plan returned by plan_system_hostname_change"`
	ApprovalHash string                         `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved hostname plan"`
}

type applySystemHostnamePlanOutput struct {
	Result application.SystemHostnameApplyResult `json:"result" jsonschema:"Hostname change result after stale-state and postcondition checks"`
}

type getFileServicesInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getSMBStateOutput struct {
	NAS string            `json:"nas" jsonschema:"NAS profile used for the request"`
	SMB synology.SMBState `json:"smb" jsonschema:"Normalized global SMB service configuration"`
}

type getNFSStateOutput struct {
	NAS string            `json:"nas" jsonschema:"NAS profile used for the request"`
	NFS synology.NFSState `json:"nfs" jsonschema:"Normalized global NFS service configuration"`
}

type getFileServiceCapabilitiesOutput struct {
	NAS          string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.FileServiceCapabilities `json:"capabilities" jsonschema:"Independently selected SMB and NFS operations"`
	Report       synology.CompatibilityReport     `json:"report" jsonschema:"Discovered APIs and selected File Services backends"`
}

type planFileServiceChangeInput struct {
	NAS     string                                `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request controlpanel.FileServiceChangeRequest `json:"request" jsonschema:"Patch-only SMB or NFS settings intent"`
}

type planFileServiceChangeOutput struct {
	Plan application.FileServicePlan `json:"plan" jsonschema:"Validated plan bound to the complete observed module state and approval hash"`
}

type applyFileServicePlanInput struct {
	Plan         application.FileServicePlan `json:"plan" jsonschema:"Unmodified plan returned by plan_file_service_change"`
	ApprovalHash string                      `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved File Services plan"`
}

type applyFileServicePlanOutput struct {
	Result application.FileServiceApplyResult `json:"result" jsonschema:"File Services mutation result after stale-state and postcondition checks"`
}

type getNFSExportStateInput struct {
	NAS   string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Share string `json:"share" jsonschema:"Shared-folder name whose NFS export rules are read"`
}

type getNFSExportStateOutput struct {
	NAS    string                  `json:"nas" jsonschema:"NAS profile used for the request"`
	Export synology.NFSShareExport `json:"export" jsonschema:"Complete NFS export rule set for the shared folder"`
}

type getNFSExportCapabilitiesOutput struct {
	NAS          string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.NFSExportCapabilities `json:"capabilities" jsonschema:"Selected NFS export read and set operations"`
	Report       synology.CompatibilityReport   `json:"report" jsonschema:"Discovered APIs and selected NFS export backend"`
}

type planNFSExportChangeInput struct {
	NAS     string                  `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request nfsexport.ChangeRequest `json:"request" jsonschema:"Complete desired NFS export rule set for one shared folder"`
}

type planNFSExportChangeOutput struct {
	Plan application.NFSExportPlan `json:"plan" jsonschema:"Validated plan bound to the complete observed rule set and approval hash"`
}

type applyNFSExportPlanInput struct {
	Plan         application.NFSExportPlan `json:"plan" jsonschema:"Unmodified plan returned by plan_nfs_export_change"`
	ApprovalHash string                    `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved NFS export plan"`
}

type applyNFSExportPlanOutput struct {
	Result application.NFSExportApplyResult `json:"result" jsonschema:"NFS export mutation result after stale-state and postcondition checks"`
}

type getServiceDiscoveryStateOutput struct {
	NAS              string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	ServiceDiscovery synology.ServiceDiscoveryState `json:"service_discovery" jsonschema:"Normalized service-discovery configuration"`
}

type getServiceDiscoveryCapabilitiesOutput struct {
	NAS          string                                `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.ServiceDiscoveryCapabilities `json:"capabilities" jsonschema:"Selected Time Machine and WS-Discovery operations"`
	Report       synology.CompatibilityReport          `json:"report" jsonschema:"Discovered APIs and selected service-discovery backends"`
}

type planServiceDiscoveryChangeInput struct {
	NAS     string                  `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request servicediscovery.Change `json:"request" jsonschema:"Patch-only service-discovery intent"`
}

type planServiceDiscoveryChangeOutput struct {
	Plan application.ServiceDiscoveryPlan `json:"plan" jsonschema:"Validated plan bound to the complete observed state and approval hash"`
}

type applyServiceDiscoveryPlanInput struct {
	Plan         application.ServiceDiscoveryPlan `json:"plan" jsonschema:"Unmodified plan returned by plan_service_discovery_change"`
	ApprovalHash string                           `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved service discovery plan"`
}

type applyServiceDiscoveryPlanOutput struct {
	Result application.ServiceDiscoveryApplyResult `json:"result" jsonschema:"Service discovery mutation result after stale-state and postcondition checks"`
}

type getFTPServicesStateOutput struct {
	NAS         string                    `json:"nas" jsonschema:"NAS profile used for the request"`
	FTPServices synology.FTPServicesState `json:"ftp_services" jsonschema:"Normalized FTP and SFTP configuration"`
}

type getFTPServicesCapabilitiesOutput struct {
	NAS          string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.FTPServicesCapabilities `json:"capabilities" jsonschema:"Selected FTP and SFTP operations"`
	Report       synology.CompatibilityReport     `json:"report" jsonschema:"Discovered APIs and selected FTP/SFTP backends"`
}

type planFTPServicesChangeInput struct {
	NAS     string             `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request ftpservices.Change `json:"request" jsonschema:"Patch-only FTP/SFTP intent"`
}

type planFTPServicesChangeOutput struct {
	Plan application.FTPServicesPlan `json:"plan" jsonschema:"Validated plan bound to the complete observed state and approval hash"`
}

type applyFTPServicesPlanInput struct {
	Plan         application.FTPServicesPlan `json:"plan" jsonschema:"Unmodified plan returned by plan_ftp_service_change"`
	ApprovalHash string                      `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved FTP services plan"`
}

type applyFTPServicesPlanOutput struct {
	Result application.FTPServicesApplyResult `json:"result" jsonschema:"FTP services mutation result after stale-state and postcondition checks"`
}

type getRsyncServiceStateOutput struct {
	NAS          string                     `json:"nas" jsonschema:"NAS profile used for the request"`
	RsyncService synology.RsyncServiceState `json:"rsync_service" jsonschema:"Normalized rsync-service configuration"`
}

type getRsyncServiceCapabilitiesOutput struct {
	NAS          string                            `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.RsyncServiceCapabilities `json:"capabilities" jsonschema:"Selected rsync-service operations"`
	Report       synology.CompatibilityReport      `json:"report" jsonschema:"Discovered APIs and selected rsync-service backend"`
}

type planRsyncServiceChangeInput struct {
	NAS     string              `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request rsyncservice.Change `json:"request" jsonschema:"Patch-only rsync-service intent"`
}

type planRsyncServiceChangeOutput struct {
	Plan application.RsyncServicePlan `json:"plan" jsonschema:"Validated plan bound to the complete observed state and approval hash"`
}

type applyRsyncServicePlanInput struct {
	Plan         application.RsyncServicePlan `json:"plan" jsonschema:"Unmodified plan returned by plan_rsync_service_change"`
	ApprovalHash string                       `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved rsync service plan"`
}

type applyRsyncServicePlanOutput struct {
	Result application.RsyncServiceApplyResult `json:"result" jsonschema:"rsync service mutation result after stale-state and postcondition checks"`
}

type getTFTPServiceStateOutput struct {
	NAS         string                    `json:"nas" jsonschema:"NAS profile used for the request"`
	TFTPService synology.TFTPServiceState `json:"tftp_service" jsonschema:"Normalized TFTP configuration"`
}

type getTFTPServiceCapabilitiesOutput struct {
	NAS          string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.TFTPServiceCapabilities `json:"capabilities" jsonschema:"Selected TFTP operations"`
	Report       synology.CompatibilityReport     `json:"report" jsonschema:"Discovered APIs and selected TFTP backend"`
}

type planTFTPServiceChangeInput struct {
	NAS     string             `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request tftpservice.Change `json:"request" jsonschema:"Patch-only TFTP intent"`
}

type planTFTPServiceChangeOutput struct {
	Plan application.TFTPServicePlan `json:"plan" jsonschema:"Validated plan bound to the complete observed state and approval hash"`
}

type applyTFTPServicePlanInput struct {
	Plan         application.TFTPServicePlan `json:"plan" jsonschema:"Unmodified plan returned by plan_tftp_service_change"`
	ApprovalHash string                      `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved TFTP service plan"`
}

type applyTFTPServicePlanOutput struct {
	Result application.TFTPServiceApplyResult `json:"result" jsonschema:"TFTP service mutation result after stale-state and postcondition checks"`
}

type getPhotosInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getPhotosSettingsOutput struct {
	NAS      string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Settings synology.PhotosAdminSettings `json:"settings" jsonschema:"Normalized Synology Photos administration settings"`
}

type getPhotosCapabilitiesOutput struct {
	NAS          string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.PhotosCapabilities  `json:"capabilities" jsonschema:"Selected Photos administration operations and package evidence"`
	Report       synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs and selected Photos backend"`
}

type planPhotosChangeInput struct {
	NAS     string             `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request photos.AdminChange `json:"request" jsonschema:"Patch-only Photos administration intent"`
}

type planPhotosChangeOutput struct {
	Plan application.PhotosPlan `json:"plan" jsonschema:"Validated plan bound to the complete observed settings and approval hash"`
}

type applyPhotosPlanInput struct {
	Plan         application.PhotosPlan `json:"plan" jsonschema:"Unmodified plan returned by plan_photos_change"`
	ApprovalHash string                 `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved Photos plan"`
}

type applyPhotosPlanOutput struct {
	Result application.PhotosApplyResult `json:"result" jsonschema:"Photos mutation result after stale-state and postcondition checks"`
}

type getOfficeInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getOfficeCapabilitiesOutput struct {
	NAS          string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.OfficeCapabilities  `json:"capabilities" jsonschema:"Selected Office settings operations and package evidence"`
	Report       synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs and selected Office backend"`
}

type getOfficeInfoOutput struct {
	NAS  string              `json:"nas" jsonschema:"NAS profile used for the request"`
	Info synology.OfficeInfo `json:"info" jsonschema:"Normalized Synology Office deployment info"`
}

type getOfficeSettingsOutput struct {
	NAS      string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	Settings synology.OfficeSystemSettings `json:"settings" jsonschema:"Normalized system-wide Synology Office settings"`
}

type getOfficePreferencesOutput struct {
	NAS         string                     `json:"nas" jsonschema:"NAS profile used for the request"`
	Preferences synology.OfficePreferences `json:"preferences" jsonschema:"Calling user's normalized Synology Office editor preferences"`
}

type getOfficeFontsOutput struct {
	NAS   string                `json:"nas" jsonschema:"NAS profile used for the request"`
	Fonts []synology.OfficeFont `json:"fonts" jsonschema:"Name-sorted Synology Office font inventory"`
}

type planOfficeChangeInput struct {
	NAS     string        `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request office.Change `json:"request" jsonschema:"Patch-only Office settings intent (exactly one scope: system, preferences, or fonts)"`
}

type planOfficeChangeOutput struct {
	Plan application.OfficePlan `json:"plan" jsonschema:"Validated plan bound to the complete observed scope state and approval hash"`
}

type applyOfficePlanInput struct {
	Plan         application.OfficePlan `json:"plan" jsonschema:"Unmodified plan returned by plan_office_change"`
	ApprovalHash string                 `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved Office plan"`
}

type applyOfficePlanOutput struct {
	Result application.OfficeApplyResult `json:"result" jsonschema:"Office mutation result after stale-state and postcondition checks"`
}

type getSurveillanceInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getSurveillanceCapabilitiesOutput struct {
	NAS          string                            `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.SurveillanceCapabilities `json:"capabilities" jsonschema:"Surveillance operations exposed by dsmctl, with installed-package evidence"`
	Report       synology.CompatibilityReport      `json:"report" jsonschema:"Discovered APIs, installed packages, and selected Surveillance backends"`
}

type getSurveillanceInfoOutput struct {
	NAS  string                    `json:"nas" jsonschema:"NAS profile used for the request"`
	Info synology.SurveillanceInfo `json:"info" jsonschema:"Normalized Surveillance Station system information"`
}

type getSurveillanceCamerasOutput struct {
	NAS     string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Cameras synology.SurveillanceCameras `json:"cameras" jsonschema:"Configured cameras reported by Surveillance Station"`
}

type getSurveillanceHomeModeOutput struct {
	NAS      string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	HomeMode synology.SurveillanceHomeMode `json:"home_mode" jsonschema:"Surveillance Station Home Mode state"`
}

type planSurveillanceHomeModeChangeInput struct {
	NAS     string                      `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request surveillance.HomeModeChange `json:"request" jsonschema:"Patch-only Home Mode intent"`
}

type planSurveillanceHomeModeChangeOutput struct {
	Plan application.SurveillanceHomeModePlan `json:"plan" jsonschema:"Validated plan bound to the observed Home Mode state and approval hash"`
}

type applySurveillanceHomeModePlanInput struct {
	Plan         application.SurveillanceHomeModePlan `json:"plan" jsonschema:"Unmodified plan returned by plan_surveillance_home_mode_change"`
	ApprovalHash string                               `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved Home Mode plan"`
}

type applySurveillanceHomeModePlanOutput struct {
	Result application.SurveillanceHomeModeApplyResult `json:"result" jsonschema:"Home Mode mutation result after stale-state and postcondition checks"`
}

type getStorageInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getStorageStateOutput struct {
	NAS     string                `json:"nas" jsonschema:"NAS profile used for the request"`
	Storage synology.StorageState `json:"storage" jsonschema:"Normalized disk, storage-pool, and volume inventory"`
}

type getStorageCapabilitiesOutput struct {
	NAS          string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.StorageCapabilities `json:"capabilities" jsonschema:"Storage operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs and selected storage compatibility backend"`
}

type planStorageChangeInput struct {
	NAS     string                `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request storage.ChangeRequest `json:"request" jsonschema:"Typed storage-pool or volume create, patch-only update, or stable-ID delete intent"`
}

type planStorageChangeOutput struct {
	Plan application.StoragePlan `json:"plan" jsonschema:"Validated storage plan including stable references, topology fingerprint, consequences, and approval hash"`
}

type applyStoragePlanInput struct {
	Plan         application.StoragePlan `json:"plan" jsonschema:"Unmodified plan returned by plan_storage_change"`
	ApprovalHash string                  `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved storage plan"`
}

type applyStoragePlanOutput struct {
	Result application.StorageApplyResult `json:"result" jsonschema:"Storage mutation result after postcondition verification"`
}

type getAccountInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getAccountStateInput struct {
	NAS                          string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	IncludeMemberships           bool   `json:"include_memberships,omitempty" jsonschema:"Include user-to-group memberships; adds one read per local group"`
	IncludeQuotas                bool   `json:"include_quotas,omitempty" jsonschema:"Include quota assignments for the selected principal or all principals"`
	IncludeApplicationPrivileges bool   `json:"include_application_privileges,omitempty" jsonschema:"Include applications and explicit privilege rules for the selected principal or all principals"`
	PrincipalType                string `json:"principal_type,omitempty" jsonschema:"Optional principal type filter: user or group"`
	Principal                    string `json:"principal,omitempty" jsonschema:"Optional principal name filter; principal_type is required"`
}

type getAccountStateOutput struct {
	NAS      string                 `json:"nas" jsonschema:"NAS profile used for the request"`
	Identity synology.IdentityState `json:"identity" jsonschema:"Normalized local user and group inventory"`
}

type getAccountCapabilitiesOutput struct {
	NAS          string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.IdentityCapabilities `json:"capabilities" jsonschema:"Identity management operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport  `json:"report" jsonschema:"Discovered APIs and selected identity compatibility backends"`
}

type planAccountChangeInput struct {
	NAS     string                 `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request identity.ChangeRequest `json:"request" jsonschema:"User, group, membership, quota, or application privilege intent; passwords must use an env:NAME credential reference"`
}

type planAccountChangeOutput struct {
	Plan application.IdentityPlan `json:"plan" jsonschema:"Validated account change plan including the approval hash and observed-state precondition"`
}

type applyAccountPlanInput struct {
	Plan         application.IdentityPlan `json:"plan" jsonschema:"Unmodified plan returned by plan_account_change"`
	ApprovalHash string                   `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved plan"`
}

type applyAccountPlanOutput struct {
	Result application.IdentityApplyResult `json:"result" jsonschema:"Account mutation result after postcondition verification"`
}

type getShareInput struct {
	NAS                string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	IncludePermissions bool   `json:"include_permissions,omitempty" jsonschema:"Expand the user/group permission matrix; causes additional read-only DSM calls"`
}

type getSANInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getSANStateOutput struct {
	NAS string            `json:"nas" jsonschema:"NAS profile used for the request"`
	SAN synology.SANState `json:"san" jsonschema:"Normalized iSCSI target, LUN, and mapping inventory"`
}

type getSANCapabilitiesOutput struct {
	NAS          string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.SANCapabilities     `json:"capabilities" jsonschema:"SAN inventory and management operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs and selected SAN compatibility backends"`
}

type getLogsInput struct {
	NAS     string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum number of log entries to return; defaults to a bounded page size"`
	Offset  int    `json:"offset,omitempty" jsonschema:"Number of newest entries to skip for pagination"`
	Level   string `json:"level,omitempty" jsonschema:"Client-side severity filter over the retrieved page: info, warn, or error"`
	Keyword string `json:"keyword,omitempty" jsonschema:"Case-insensitive substring filter applied by DSM"`
	LogType string `json:"log_type,omitempty" jsonschema:"DSM log category; defaults to system. Also: connection, package, or fileTransfer"`
	From    string `json:"from,omitempty" jsonschema:"Inclusive lower time bound: a local timestamp (2006-01-02 or 2006-01-02 15:04:05) or Unix seconds"`
	To      string `json:"to,omitempty" jsonschema:"Inclusive upper time bound (requires from): a local timestamp or Unix seconds"`
}

type getNotificationInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getNotificationHistoryInput struct {
	NAS    string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum number of notifications to return; defaults to a bounded page size"`
	Offset int    `json:"offset,omitempty" jsonschema:"Number of newest notifications to skip for pagination"`
	Level  string `json:"level,omitempty" jsonschema:"Severity filter applied by DSM: info, warn, or error"`
	From   string `json:"from,omitempty" jsonschema:"Inclusive lower time bound: a local timestamp (2006-01-02 or 2006-01-02 15:04:05) or Unix seconds"`
	To     string `json:"to,omitempty" jsonschema:"Inclusive upper time bound: a local timestamp or Unix seconds"`
	Lang   string `json:"lang,omitempty" jsonschema:"DSM string-table language for rendered titles/messages, such as enu (default) or cht"`
}

type getNotificationCapabilitiesOutput struct {
	NAS          string                            `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.NotificationCapabilities `json:"capabilities" jsonschema:"Notification read areas currently exposed by dsmctl"`
	Report       synology.CompatibilityReport      `json:"report" jsonschema:"Discovered APIs and selected notification compatibility backends"`
}

type getNotificationMailOutput struct {
	NAS  string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	Mail synology.NotificationMailState `json:"mail" jsonschema:"Normalized email notification channel without any password material"`
}

type getNotificationPushOutput struct {
	NAS  string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	Push synology.NotificationPushState `json:"push" jsonschema:"Normalized push notification channel without any device tokens"`
}

type getNotificationWebhookOutput struct {
	NAS     string                            `json:"nas" jsonschema:"NAS profile used for the request"`
	Webhook synology.NotificationWebhookState `json:"webhook" jsonschema:"Configured webhook providers without URLs or secrets"`
}

type getNotificationSMSOutput struct {
	NAS string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	SMS synology.NotificationSMSState `json:"sms" jsonschema:"Normalized SMS notification channel without provider auth material"`
}

type getNotificationRulesOutput struct {
	NAS   string                          `json:"nas" jsonschema:"NAS profile used for the request"`
	Rules synology.NotificationRulesState `json:"rules" jsonschema:"Notification event rule catalog per profile"`
}

type getNotificationDesktopOutput struct {
	NAS     string                            `json:"nas" jsonschema:"NAS profile used for the request"`
	Desktop synology.NotificationDesktopState `json:"desktop" jsonschema:"Per-category desktop notification toggles of the signed-in user"`
}

type getNotificationHistoryOutput struct {
	NAS     string                            `json:"nas" jsonschema:"NAS profile used for the request"`
	History synology.NotificationHistoryState `json:"history" jsonschema:"One page of the DSM notification history, newest first"`
}

type getDSMUpdateInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getDSMUpdateCapabilitiesOutput struct {
	NAS          string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.DSMUpdateCapabilities `json:"capabilities" jsonschema:"Update & Restore read areas currently exposed by dsmctl"`
	Report       synology.CompatibilityReport   `json:"report" jsonschema:"Discovered APIs and selected DSM update compatibility backends"`
}

type getDSMUpdateStatusOutput struct {
	NAS    string                   `json:"nas" jsonschema:"NAS profile used for the request"`
	Status synology.DSMUpdateStatus `json:"status" jsonschema:"Local DSM update state: installed version, whether an upgrade is allowed, and any in-progress state"`
}

type getDSMUpdateAvailableOutput struct {
	NAS       string                      `json:"nas" jsonschema:"NAS profile used for the request"`
	Available synology.DSMUpdateAvailable `json:"available" jsonschema:"Update-server offered-update check; availability is unknown when the update server is unreachable"`
}

type getDSMUpdatePolicyOutput struct {
	NAS    string                   `json:"nas" jsonschema:"NAS profile used for the request"`
	Policy synology.DSMUpdatePolicy `json:"policy" jsonschema:"DSM auto-update policy"`
}

type getDSMUpdateConfigBackupOutput struct {
	NAS          string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	ConfigBackup synology.DSMUpdateConfigBackup `json:"config_backup" jsonschema:"Configuration-backup status and history without any destination password"`
}

type getLogsOutput struct {
	NAS  string            `json:"nas" jsonschema:"NAS profile used for the request"`
	Logs synology.LogState `json:"logs" jsonschema:"Normalized DSM system log entries and severity counts"`
}

type getTaskSchedulerInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getTaskSchedulerCapabilitiesOutput struct {
	NAS          string                             `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.TaskSchedulerCapabilities `json:"capabilities" jsonschema:"Task Scheduler read areas currently exposed by dsmctl"`
	Report       synology.CompatibilityReport       `json:"report" jsonschema:"Discovered APIs and selected Task Scheduler compatibility backends"`
}

type getTaskSchedulerTasksOutput struct {
	NAS   string                               `json:"nas" jsonschema:"NAS profile used for the request"`
	Tasks synology.TaskSchedulerScheduledTasks `json:"tasks" jsonschema:"Scheduled-task inventory metadata; never a task's command or script body"`
}

type getTaskSchedulerTriggeredOutput struct {
	NAS   string                               `json:"nas" jsonschema:"NAS profile used for the request"`
	Tasks synology.TaskSchedulerTriggeredTasks `json:"tasks" jsonschema:"Triggered-task inventory metadata; never a task's command or script body"`
}

type getResourceMonitorInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getResourceMonitorHistoryInput struct {
	NAS        string   `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Period     string   `json:"period,omitempty" jsonschema:"History window: day (default), week, month, or year"`
	Dimensions []string `json:"dimensions,omitempty" jsonschema:"Limit to dimensions: cpu, memory, network, disk, volume; empty returns all"`
}

type getResourceMonitorStateOutput struct {
	NAS         string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Utilization synology.ResourceUtilization `json:"utilization" jsonschema:"Current normalized resource utilization snapshot"`
}

type getResourceMonitorHistoryOutput struct {
	NAS     string                   `json:"nas" jsonschema:"NAS profile used for the request"`
	History synology.ResourceHistory `json:"history" jsonschema:"Recorded utilization history series"`
}

type getResourceRecordingSettingOutput struct {
	NAS     string                            `json:"nas" jsonschema:"NAS profile used for the request"`
	Setting synology.ResourceRecordingSetting `json:"setting" jsonschema:"History-recording setting reported by DSM"`
}

type getResourceMonitorCapabilitiesOutput struct {
	NAS          string                               `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.ResourceMonitorCapabilities `json:"capabilities" jsonschema:"Resource Monitor operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport         `json:"report" jsonschema:"Discovered APIs and selected Resource Monitor compatibility backends"`
}

type getDiskSMARTInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getDiskSMARTCapabilitiesOutput struct {
	NAS          string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.DiskSMARTCapabilities `json:"capabilities" jsonschema:"Disk-SMART read areas currently exposed by dsmctl"`
	Report       synology.CompatibilityReport  `json:"report" jsonschema:"Discovered APIs and selected disk-SMART compatibility backends"`
}

type getDiskHealthOutput struct {
	NAS    string                   `json:"nas" jsonschema:"NAS profile used for the request"`
	Health synology.DiskHealthState `json:"health" jsonschema:"Per-disk health, lifespan, and coarse self-test state plus global warning thresholds"`
}

type getDiskSMARTAttributesOutput struct {
	NAS   string                  `json:"nas" jsonschema:"NAS profile used for the request"`
	SMART synology.DiskSMARTState `json:"smart" jsonschema:"Per-disk SMART attribute tables, summaries, and self-test status"`
}

type planResourceRecordingChangeInput struct {
	NAS     string                 `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request resmon.RecordingChange `json:"request" jsonschema:"History-recording toggle intent; set enable to true or false"`
}

type planResourceRecordingChangeOutput struct {
	Plan application.ResourceRecordingPlan `json:"plan" jsonschema:"Approval plan bound to the observed recording setting"`
}

type applyResourceRecordingPlanInput struct {
	Plan         application.ResourceRecordingPlan `json:"plan" jsonschema:"Unmodified plan returned by plan_resource_recording_change"`
	ApprovalHash string                            `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved recording plan"`
}

type applyResourceRecordingPlanOutput struct {
	Result application.ResourceRecordingApplyResult `json:"result" jsonschema:"Outcome after hash, stale-state, and postcondition verification"`
}

type getLogCapabilitiesOutput struct {
	NAS          string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.LogCapabilities     `json:"capabilities" jsonschema:"DSM log read operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs and selected log compatibility backend"`
}

type planSANChangeInput struct {
	NAS     string            `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request san.ChangeRequest `json:"request" jsonschema:"Typed target, LUN, or target-to-LUN mapping change intent; CHAP passwords must use env:NAME references"`
}

type planSANChangeOutput struct {
	Plan application.SANPlan `json:"plan" jsonschema:"Validated SAN plan with stable references, current-state fingerprints, warnings, and approval hash"`
}

type applySANPlanInput struct {
	Plan         application.SANPlan `json:"plan" jsonschema:"Unmodified plan returned by plan_san_change"`
	ApprovalHash string              `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved SAN plan"`
}

type applySANPlanOutput struct {
	Result application.SANApplyResult `json:"result" jsonschema:"SAN mutation result and post-apply or failure-state inventory"`
}

type getShareStateOutput struct {
	NAS    string              `json:"nas" jsonschema:"NAS profile used for the request"`
	Shares synology.ShareState `json:"shares" jsonschema:"Normalized shared-folder inventory and optional permissions"`
}

type getShareCapabilitiesOutput struct {
	NAS          string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.ShareCapabilities   `json:"capabilities" jsonschema:"Shared-folder operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs and selected shared-folder compatibility backends"`
}

type planShareChangeInput struct {
	NAS     string              `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request share.ChangeRequest `json:"request" jsonschema:"Shared-folder create, update, delete, or permission-setting intent"`
}

type planShareChangeOutput struct {
	Plan application.SharePlan `json:"plan" jsonschema:"Validated shared-folder plan including the approval hash and observed-state precondition"`
}

type applySharePlanInput struct {
	Plan         application.SharePlan `json:"plan" jsonschema:"Unmodified plan returned by plan_share_change"`
	ApprovalHash string                `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved plan"`
}

type applySharePlanOutput struct {
	Result application.ShareApplyResult `json:"result" jsonschema:"Shared-folder mutation result after postcondition verification"`
}

type getPackageInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getPackageCapabilitiesOutput struct {
	NAS          string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.PackageCapabilities `json:"capabilities" jsonschema:"Package Center operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs and selected Package Center backends"`
}

type getPackageStateOutput struct {
	NAS   string                `json:"nas" jsonschema:"NAS profile used for the request"`
	State synology.PackageState `json:"state" jsonschema:"Normalized installed-package inventory"`
}

type getPackageSettingsOutput struct {
	NAS      string                   `json:"nas" jsonschema:"NAS profile used for the request"`
	Settings synology.PackageSettings `json:"settings" jsonschema:"Normalized global Package Center settings"`
}

type planPackageChangeInput struct {
	NAS     string                      `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request packagecenter.ChangeRequest `json:"request" jsonschema:"Settings patch or package lifecycle intent; install and update are deferred and rejected"`
}

type planPackageChangeOutput struct {
	Plan application.PackagePlan `json:"plan" jsonschema:"Validated plan bound to the observed settings or package state and approval hash"`
}

type applyPackagePlanInput struct {
	Plan         application.PackagePlan `json:"plan" jsonschema:"Unmodified plan returned by plan_package_change"`
	ApprovalHash string                  `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved Package Center plan"`
}

type applyPackagePlanOutput struct {
	Result application.PackageApplyResult `json:"result" jsonschema:"Package Center mutation result after stale-state and postcondition checks"`
}

type getPackageAvailableInput struct {
	NAS         string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	UpdatesOnly bool   `json:"updates_only,omitempty" jsonschema:"Return only installed packages whose offered version is newer than the installed one"`
}

type getPackageAvailableOutput struct {
	NAS      string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Packages []packagecenter.AvailablePackage `json:"packages" jsonschema:"Packages offered by the online package server, cross-referenced with the installed inventory"`
}

type planPackageInstallInput struct {
	NAS       string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	PackageID string `json:"package_id" jsonschema:"Stable DSM package identifier to install, as listed by get_package_available"`
	// VolumePath selects where the package installs, e.g. /volume1.
	VolumePath string `json:"volume_path" jsonschema:"Target install volume path, for example /volume1"`
	// Pointers so an omitted field keeps the safe default (true), matching the CLI.
	RunAfterInstall *bool `json:"run_after_install,omitempty" jsonschema:"Start the package after install; defaults to true"`
	QuickInstall    *bool `json:"quick_install,omitempty" jsonschema:"Quick install with defaults (no configuration wizard); defaults to true"`
}

type planPackageInstallOutput struct {
	Plan application.PackageInstallPlan `json:"plan" jsonschema:"Resolved install intent (dependencies first) bound to an approval hash"`
}

type planPackageUpdateInput struct {
	NAS       string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	PackageID string `json:"package_id" jsonschema:"Stable DSM package identifier of an installed package with an available update"`
}

type applyPackageInstallPlanInput struct {
	Plan         application.PackageInstallPlan `json:"plan" jsonschema:"Unmodified plan returned by plan_package_install or plan_package_update"`
	ApprovalHash string                         `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved install plan"`
}

type applyPackageInstallPlanOutput struct {
	Result application.PackageInstallApplyResult `json:"result" jsonschema:"Per-package install outcomes confirmed by the inventory, in install order"`
}

type planPackageLocalInstallInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	// SPKPath is read on the dsmctl host, not the NAS.
	SPKPath string `json:"spk_path" jsonschema:"Path on the dsmctl host to the local .spk file to upload and install"`
	// VolumePath selects where the package installs, e.g. /volume1.
	VolumePath string `json:"volume_path" jsonschema:"Target install volume path, for example /volume1"`
	// Pointers so an omitted field keeps the safe default, matching the CLI.
	RunAfterInstall *bool `json:"run_after_install,omitempty" jsonschema:"Start the package after install; defaults to true"`
	AllowUnsigned   *bool `json:"allow_unsigned,omitempty" jsonschema:"Disable DSM code-signature enforcement to install a package not signed by Synology; defaults to false"`
}

type planPackageLocalInstallOutput struct {
	Plan application.PackageLocalInstallPlan `json:"plan" jsonschema:"Hash-bound local install intent, bound to the .spk file content (size + SHA-256)"`
}

type applyPackageLocalInstallPlanInput struct {
	Plan         application.PackageLocalInstallPlan `json:"plan" jsonschema:"Unmodified plan returned by plan_package_local_install"`
	ApprovalHash string                              `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved local install plan"`
}

type applyPackageLocalInstallPlanOutput struct {
	Result application.PackageLocalInstallApplyResult `json:"result" jsonschema:"Local install outcome confirmed by the inventory"`
}

type getDriveAdminInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getCertificateInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getCertificatesOutput struct {
	NAS          string                `json:"nas" jsonschema:"NAS profile used for the request"`
	Certificates synology.Certificates `json:"certificates" jsonschema:"Installed certificates with their bound services"`
}

type getCertificateCapabilitiesOutput struct {
	NAS          string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.CertificateCapabilities `json:"capabilities" jsonschema:"Certificate operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport     `json:"report" jsonschema:"Discovered APIs and selected certificate backend"`
}

type getTerminalSNMPInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getTerminalStateOutput struct {
	NAS      string                 `json:"nas" jsonschema:"NAS profile used for the request"`
	Terminal synology.TerminalState `json:"terminal" jsonschema:"Normalized Terminal (SSH/Telnet) state"`
}

type getSNMPStateOutput struct {
	NAS  string             `json:"nas" jsonschema:"NAS profile used for the request"`
	SNMP synology.SNMPState `json:"snmp" jsonschema:"Normalized SNMP state; carries no community string or SNMPv3 passwords"`
}

type getTerminalSNMPCapabilitiesOutput struct {
	NAS          string                            `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.TerminalSNMPCapabilities `json:"capabilities" jsonschema:"Terminal and SNMP reads and guarded writes currently exposed by dsmctl"`
	Report       synology.CompatibilityReport      `json:"report" jsonschema:"Discovered APIs and selected Terminal/SNMP backends"`
}

type planTerminalChangeInput struct {
	NAS     string                      `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request terminalsnmp.TerminalChange `json:"request" jsonschema:"Patch-only Terminal intent (ssh_enabled, ssh_port, telnet_enabled, console_forbidden)"`
}

type planTerminalChangeOutput struct {
	Plan application.TerminalChangePlan `json:"plan" jsonschema:"Validated plan bound to the complete observed Terminal state and approval hash"`
}

type applyTerminalPlanInput struct {
	Plan         application.TerminalChangePlan `json:"plan" jsonschema:"Unmodified plan returned by plan_terminal_change"`
	ApprovalHash string                         `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved plan"`
}

type terminalSNMPApplyOutput struct {
	Result application.TerminalSNMPApplyResult `json:"result" jsonschema:"Mutation result after stale-state and postcondition checks; carries no secret"`
}

type planSNMPChangeInput struct {
	NAS     string                  `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request terminalsnmp.SNMPChange `json:"request" jsonschema:"Patch-only SNMP intent. The read community is a secret referenced by community_credential_ref (env:NAME) and resolved only at apply time"`
}

type planSNMPChangeOutput struct {
	Plan application.SNMPChangePlan `json:"plan" jsonschema:"Validated plan bound to the complete observed SNMP state and approval hash; carries no community string or SNMPv3 password"`
}

type applySNMPPlanInput struct {
	Plan         application.SNMPChangePlan `json:"plan" jsonschema:"Unmodified plan returned by plan_snmp_change"`
	ApprovalHash string                     `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved plan"`
}

type planCertificateChangeInput struct {
	NAS     string                    `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request certificate.ChangeRequest `json:"request" jsonschema:"Certificate import, set_default, bind_service, or delete intent. The private key is referenced by env:NAME and resolved only at apply time"`
}

type planCertificateChangeOutput struct {
	Plan application.CertificatePlan `json:"plan" jsonschema:"Validated high-risk certificate plan including the approval hash and observed-state precondition; carries no private-key material"`
}

type applyCertificatePlanInput struct {
	Plan         application.CertificatePlan `json:"plan" jsonschema:"Unmodified plan returned by plan_certificate_change"`
	ApprovalHash string                      `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved plan"`
}

type applyCertificatePlanOutput struct {
	Result application.CertificateApplyResult `json:"result" jsonschema:"Certificate mutation result after postcondition verification; carries no private-key material"`
}

type exportCertificateInput struct {
	NAS       string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	CertID    string `json:"cert_id" jsonschema:"Certificate id to export"`
	LocalPath string `json:"local_path" jsonschema:"Local file path on the dsmctl host to write the archive to (contains the private key)"`
}

type exportCertificateOutput struct {
	Result application.ExportCertificateResult `json:"result" jsonschema:"Local file the archive was written to and its size; no key bytes are returned"`
}

type getSecurityAdvisorInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getSecurityAdvisorStatusOutput struct {
	NAS    string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	Status synology.SecurityAdvisorStatus `json:"status" jsonschema:"Normalized last-scan status and per-category findings"`
}

type getSecurityAdvisorScheduleOutput struct {
	NAS           string                                `json:"nas" jsonschema:"NAS profile used for the request"`
	Configuration synology.SecurityAdvisorConfiguration `json:"configuration" jsonschema:"Current scan schedule and security baseline"`
}

type getSecurityAdvisorCapabilitiesOutput struct {
	NAS          string                               `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.SecurityAdvisorCapabilities `json:"capabilities" jsonschema:"Security Advisor operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport         `json:"report" jsonschema:"Discovered APIs and selected Security Advisor backends"`
}

type planSecurityAdvisorScheduleChangeInput struct {
	NAS     string                         `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request securityadvisor.ScheduleChange `json:"request" jsonschema:"Patch-only scan schedule and security-baseline intent"`
}

type planSecurityAdvisorScheduleChangeOutput struct {
	Plan application.SecurityAdvisorSchedulePlan `json:"plan" jsonschema:"Validated plan bound to the complete observed configuration and approval hash"`
}

type applySecurityAdvisorSchedulePlanInput struct {
	Plan         application.SecurityAdvisorSchedulePlan `json:"plan" jsonschema:"Unmodified plan returned by plan_security_advisor_schedule_change"`
	ApprovalHash string                                  `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved security advisor plan"`
}

type applySecurityAdvisorSchedulePlanOutput struct {
	Result application.SecurityAdvisorScheduleApplyResult `json:"result" jsonschema:"Schedule + baseline mutation result after stale-state and postcondition checks"`
}

type runSecurityScanOutput struct {
	Result application.SecurityAdvisorScanActionResult `json:"result" jsonschema:"Result of triggering a full Security Advisor scan"`
}

type accountProtectionInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getAutoBlockSettingsOutput struct {
	NAS      string                     `json:"nas" jsonschema:"NAS profile used for the request"`
	Settings synology.AutoBlockSettings `json:"settings" jsonschema:"Auto Block configuration"`
}

type getAutoBlockListsOutput struct {
	NAS   string                  `json:"nas" jsonschema:"NAS profile used for the request"`
	Lists synology.AutoBlockLists `json:"lists" jsonschema:"Auto Block allow and block IP lists"`
}

type getAccountProtectionOutput struct {
	NAS        string                     `json:"nas" jsonschema:"NAS profile used for the request"`
	Protection synology.AccountProtection `json:"protection" jsonschema:"Account Protection thresholds"`
}

type getEnforceTwoFactorOutput struct {
	NAS    string                    `json:"nas" jsonschema:"NAS profile used for the request"`
	Policy synology.EnforceTwoFactor `json:"policy" jsonschema:"Enforced-2FA policy scope"`
}

type getAccountProtectionCapabilitiesOutput struct {
	NAS          string                                 `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.AccountProtectionCapabilities `json:"capabilities" jsonschema:"Account-protection reads and guarded writes currently exposed by dsmctl"`
	Report       synology.CompatibilityReport           `json:"report" jsonschema:"Discovered APIs and selected account-protection backends"`
}

type planAutoBlockChangeInput struct {
	NAS     string                            `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request accountprotection.AutoBlockChange `json:"request" jsonschema:"Patch-only Auto Block settings intent"`
}

type planAutoBlockChangeOutput struct {
	Plan application.AutoBlockSettingsPlan `json:"plan" jsonschema:"Validated plan bound to the complete observed settings and approval hash"`
}

type applyAutoBlockPlanInput struct {
	Plan         application.AutoBlockSettingsPlan `json:"plan" jsonschema:"Unmodified plan returned by plan_account_protection_autoblock_change"`
	ApprovalHash string                           `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved plan"`
}

type accountProtectionApplyOutput struct {
	Result application.AccountProtectionApplyResult `json:"result" jsonschema:"Mutation result after stale-state and postcondition checks"`
}

type planAccountProtectionThresholdsChangeInput struct {
	NAS     string                                    `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request accountprotection.AccountProtectionChange `json:"request" jsonschema:"Patch-only Account Protection thresholds intent"`
}

type planAccountProtectionThresholdsChangeOutput struct {
	Plan application.AccountProtectionThresholdsPlan `json:"plan" jsonschema:"Validated plan bound to the complete observed thresholds and approval hash"`
}

type applyAccountProtectionThresholdsPlanInput struct {
	Plan         application.AccountProtectionThresholdsPlan `json:"plan" jsonschema:"Unmodified plan returned by plan_account_protection_thresholds_change"`
	ApprovalHash string                                      `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved plan"`
}

type planEnforceTwoFactorChangeInput struct {
	NAS     string                                   `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request accountprotection.EnforceTwoFactorChange `json:"request" jsonschema:"Enforced-2FA policy scope intent (otp_enforce_option); enabling requires allow_lockout_override"`
}

type planEnforceTwoFactorChangeOutput struct {
	Plan application.EnforceTwoFactorPlan `json:"plan" jsonschema:"Validated plan bound to the observed policy and approval hash"`
}

type applyEnforceTwoFactorPlanInput struct {
	Plan         application.EnforceTwoFactorPlan `json:"plan" jsonschema:"Unmodified plan returned by plan_account_protection_enforce_2fa_change"`
	ApprovalHash string                          `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved plan"`
}

type planAutoBlockListChangeInput struct {
	NAS     string                       `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request accountprotection.IPListEdit `json:"request" jsonschema:"Single allow/block list add or remove; self-lockout edits require allow_lockout_override"`
}

type planAutoBlockListChangeOutput struct {
	Plan application.AutoBlockListPlan `json:"plan" jsonschema:"Validated plan bound to the complete observed lists, active sources, and approval hash"`
}

type applyAutoBlockListPlanInput struct {
	Plan         application.AutoBlockListPlan `json:"plan" jsonschema:"Unmodified plan returned by plan_account_protection_list_change"`
	ApprovalHash string                       `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved plan"`
}

type firewallInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type firewallRulesInput struct {
	NAS     string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Profile string `json:"profile,omitempty" jsonschema:"Limit to a single firewall profile by name; omit to read every profile"`
}

type getFirewallStatusOutput struct {
	NAS    string                  `json:"nas" jsonschema:"NAS profile used for the request"`
	Status synology.FirewallStatus `json:"status" jsonschema:"Global firewall enable flag, active profile, and network adapters"`
}

type getFirewallProfilesOutput struct {
	NAS      string                     `json:"nas" jsonschema:"NAS profile used for the request"`
	Profiles []synology.FirewallProfile `json:"profiles" jsonschema:"Firewall profiles, with the active one marked"`
}

type getFirewallRulesOutput struct {
	NAS     string                   `json:"nas" jsonschema:"NAS profile used for the request"`
	RuleSet synology.FirewallRuleSet `json:"rule_set" jsonschema:"Per-adapter default policy and ordered rules for the requested profile(s)"`
}

type getFirewallCapabilitiesOutput struct {
	NAS          string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.FirewallCapabilities `json:"capabilities" jsonschema:"Firewall reads and guarded writes currently exposed by dsmctl"`
	Report       synology.CompatibilityReport  `json:"report" jsonschema:"Discovered APIs and selected firewall backends"`
}

type planFirewallProfileChangeInput struct {
	NAS     string                 `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request firewalldomain.ProfileChange `json:"request" jsonschema:"Full-desired-state firewall profile change: the target profile, the desired adapter sections (default policy plus complete ordered rule list), whether to activate it, and the never-lockout override/keep_reachable"`
}

type planFirewallProfileChangeOutput struct {
	Plan application.FirewallProfilePlan `json:"plan" jsonschema:"Validated plan bound to the complete observed state, the management tuple, the guard decision, and the approval hash"`
}

type applyFirewallProfilePlanInput struct {
	Plan         application.FirewallProfilePlan `json:"plan" jsonschema:"Unmodified plan returned by plan_firewall_profile_change"`
	ApprovalHash string                          `json:"approval_hash" jsonschema:"Exact approval hash from the plan"`
}

type planFirewallEnableChangeInput struct {
	NAS     string                `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request firewalldomain.EnableChange `json:"request" jsonschema:"Firewall enable/disable intent: the desired enabled state, the profile to make active when enabling, and the never-lockout override/keep_reachable"`
}

type planFirewallEnableChangeOutput struct {
	Plan application.FirewallEnablePlan `json:"plan" jsonschema:"Validated plan bound to the complete observed state, the management tuple, the guard decision, and the approval hash"`
}

type applyFirewallEnablePlanInput struct {
	Plan         application.FirewallEnablePlan `json:"plan" jsonschema:"Unmodified plan returned by plan_firewall_enable_change"`
	ApprovalHash string                         `json:"approval_hash" jsonschema:"Exact approval hash from the plan"`
}

type firewallApplyOutput struct {
	Result application.FirewallApplyResult `json:"result" jsonschema:"Mutation result after stale-state, never-lockout, and postcondition checks"`
}

type networkInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getNetworkGeneralOutput struct {
	NAS     string                  `json:"nas" jsonschema:"NAS profile used for the request"`
	General synology.NetworkGeneral `json:"general" jsonschema:"General network settings: hostname, default gateway, DNS, and outbound proxy (the proxy password is never surfaced)"`
}

type getNetworkInterfacesOutput struct {
	NAS        string                      `json:"nas" jsonschema:"NAS profile used for the request"`
	Interfaces []synology.NetworkInterface `json:"interfaces" jsonschema:"Per-interface configuration and link status"`
}

type getNetworkBondsOutput struct {
	NAS   string                 `json:"nas" jsonschema:"NAS profile used for the request"`
	Bonds []synology.NetworkBond `json:"bonds" jsonschema:"Link-aggregation bonds with their mode and member NICs"`
}

type getNetworkRoutesOutput struct {
	NAS    string                     `json:"nas" jsonschema:"NAS profile used for the request"`
	Routes synology.NetworkRouteTable `json:"routes" jsonschema:"Static-route table; configured is false when advanced routing is not set up on the NAS"`
}

type getNetworkCapabilitiesOutput struct {
	NAS          string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.NetworkCapabilities `json:"capabilities" jsonschema:"Network reads and guarded writes currently exposed by dsmctl"`
	Report       synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs and selected network backends"`
}

type planNetworkGeneralChangeInput struct {
	NAS     string                      `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request networkdomain.GeneralChange `json:"request" jsonschema:"Patch-only general network change: any of hostname, default_gateway_v4, dns_primary, dns_secondary, ipv4_first, plus the allow_connectivity_break override. Omitted fields are preserved"`
}

type planNetworkGeneralChangeOutput struct {
	Plan application.NetworkGeneralPlan `json:"plan" jsonschema:"Validated plan bound to the complete observed general block, the resolved management path, the never-sever guard decision, and the approval hash"`
}

type applyNetworkGeneralPlanInput struct {
	Plan         application.NetworkGeneralPlan `json:"plan" jsonschema:"Unmodified plan returned by plan_network_general_change"`
	ApprovalHash string                         `json:"approval_hash" jsonschema:"Exact approval hash from the plan"`
}

type planNetworkInterfaceChangeInput struct {
	NAS     string                        `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request networkdomain.InterfaceChange `json:"request" jsonschema:"Patch-only interface change: the interface name plus any of ipv4, netmask, gateway_v4, use_dhcp, mtu, plus the allow_connectivity_break override. Omitted fields are preserved"`
}

type planNetworkInterfaceChangeOutput struct {
	Plan application.NetworkInterfacePlan `json:"plan" jsonschema:"Validated plan with the never-sever guard decision. NOTE: the interface-set wire is unverified, so the apply is refused"`
}

type applyNetworkInterfacePlanInput struct {
	Plan         application.NetworkInterfacePlan `json:"plan" jsonschema:"Unmodified plan returned by plan_network_interface_change"`
	ApprovalHash string                           `json:"approval_hash" jsonschema:"Exact approval hash from the plan"`
}

type networkApplyOutput struct {
	Result application.NetworkApplyResult `json:"result" jsonschema:"Mutation result after stale-state, never-sever, and postcondition checks"`
}

type loginPortalInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getDSMWebServiceOutput struct {
	NAS      string                 `json:"nas" jsonschema:"NAS profile used for the request"`
	Settings synology.DSMWebService `json:"settings" jsonschema:"DSM web-service access settings"`
}

type getApplicationPortalsOutput struct {
	NAS     string                      `json:"nas" jsonschema:"NAS profile used for the request"`
	Portals synology.ApplicationPortals `json:"portals" jsonschema:"Per-application portal list"`
}

type getReverseProxyRulesOutput struct {
	NAS   string                     `json:"nas" jsonschema:"NAS profile used for the request"`
	Rules synology.ReverseProxyRules `json:"rules" jsonschema:"Reverse-proxy rule list"`
}

type getLoginPortalCapabilitiesOutput struct {
	NAS          string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.LoginPortalCapabilities `json:"capabilities" jsonschema:"Login Portal reads and guarded writes currently exposed by dsmctl"`
	Report       synology.CompatibilityReport     `json:"report" jsonschema:"Discovered APIs and selected Login Portal backends"`
}

type planDSMWebServiceChangeInput struct {
	NAS     string                          `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request loginportal.DSMWebServiceChange `json:"request" jsonschema:"Patch-only DSM web-service intent; a change that would sever the current dsmctl transport needs allow_connectivity_break"`
}

type planDSMWebServiceChangeOutput struct {
	Plan application.DSMWebServicePlan `json:"plan" jsonschema:"Validated plan bound to the complete observed settings, current transport, and approval hash"`
}

type applyDSMWebServicePlanInput struct {
	Plan         application.DSMWebServicePlan `json:"plan" jsonschema:"Unmodified plan returned by plan_login_portal_dsm_change"`
	ApprovalHash string                       `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved plan"`
}

type loginPortalApplyOutput struct {
	Result application.LoginPortalApplyResult `json:"result" jsonschema:"Mutation result after stale-state and postcondition checks"`
}

type planApplicationPortalChangeInput struct {
	NAS     string                              `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request loginportal.ApplicationPortalChange `json:"request" jsonschema:"Patch-only application-portal intent keyed by app_id"`
}

type planApplicationPortalChangeOutput struct {
	Plan application.ApplicationPortalPlan `json:"plan" jsonschema:"Validated plan bound to the observed portal and approval hash"`
}

type applyApplicationPortalPlanInput struct {
	Plan         application.ApplicationPortalPlan `json:"plan" jsonschema:"Unmodified plan returned by plan_login_portal_application_change"`
	ApprovalHash string                           `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved plan"`
}

type planReverseProxyCreateInput struct {
	NAS     string                             `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request loginportal.ReverseProxyRuleCreate `json:"request" jsonschema:"Reverse-proxy rule to create; secret header values use credential_ref (env:NAME)"`
}

type planReverseProxyDeleteInput struct {
	NAS     string                             `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request loginportal.ReverseProxyRuleDelete `json:"request" jsonschema:"Reverse-proxy rule to delete, keyed by uuid"`
}

type planReverseProxyOutput struct {
	Plan application.ReverseProxyPlan `json:"plan" jsonschema:"Validated plan bound to the complete observed rule set and approval hash"`
}

type applyReverseProxyPlanInput struct {
	Plan         application.ReverseProxyPlan `json:"plan" jsonschema:"Unmodified plan returned by plan_login_portal_reverse_proxy_create or plan_login_portal_reverse_proxy_delete"`
	ApprovalHash string                       `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved plan"`
}

type getDriveAdminCapabilitiesOutput struct {
	NAS          string                          `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.DriveAdminCapabilities `json:"capabilities" jsonschema:"Drive Admin operations currently exposed by dsmctl, with installed-package evidence"`
	Report       synology.CompatibilityReport    `json:"report" jsonschema:"Discovered APIs, installed packages, and selected Drive Admin backends"`
}

type getDriveAdminStatusOutput struct {
	NAS    string                    `json:"nas" jsonschema:"NAS profile used for the request"`
	Status synology.DriveAdminStatus `json:"status" jsonschema:"Normalized Drive service status with installed-package evidence"`
}

type getDriveAdminConnectionsOutput struct {
	NAS         string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	Connections synology.DriveAdminConnections `json:"connections" jsonschema:"Active Drive client connections"`
}

type getDriveAdminTeamFoldersOutput struct {
	NAS         string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	TeamFolders synology.DriveAdminTeamFolders `json:"team_folders" jsonschema:"Drive team folders from the admin perspective"`
}

type getDriveAdminLogsInput struct {
	NAS        string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum entries to return; defaults to 100, maximum 1000"`
	Offset     int    `json:"offset,omitempty" jsonschema:"Number of newest entries to skip for pagination"`
	Keyword    string `json:"keyword,omitempty" jsonschema:"Substring filter applied by Drive"`
	Username   string `json:"username,omitempty" jsonschema:"Filter to one account name"`
	TeamFolder string `json:"team_folder,omitempty" jsonschema:"Filter to one Drive team folder by shared-folder name"`
	From       int64  `json:"from,omitempty" jsonschema:"Inclusive lower bound as a Unix time in seconds"`
	To         int64  `json:"to,omitempty" jsonschema:"Inclusive upper bound as a Unix time in seconds"`
}

type getDriveAdminLogsOutput struct {
	NAS string                 `json:"nas" jsonschema:"NAS profile used for the request"`
	Log synology.DriveAdminLog `json:"log" jsonschema:"Drive server log entries for the requested page"`
}

type getDriveConfigOutput struct {
	NAS    string                     `json:"nas" jsonschema:"NAS profile used for the request"`
	Config synology.DriveServerConfig `json:"config" jsonschema:"Normalized Drive server database configuration"`
}

type planDriveConfigChangeInput struct {
	NAS     string                        `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request driveadmin.ServerConfigChange `json:"request" jsonschema:"Patch-only Drive server config intent"`
}

type planDriveConfigChangeOutput struct {
	Plan application.DriveConfigPlan `json:"plan" jsonschema:"Validated plan bound to the complete observed config and approval hash"`
}

type applyDriveConfigPlanInput struct {
	Plan         application.DriveConfigPlan `json:"plan" jsonschema:"Unmodified plan returned by plan_drive_config_change"`
	ApprovalHash string                      `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved Drive config plan"`
}

type applyDriveConfigPlanOutput struct {
	Result application.DriveConfigApplyResult `json:"result" jsonschema:"Drive config mutation result after stale-state and postcondition checks"`
}

type getDriveTopFilesInput struct {
	NAS        string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	RankingBy  string `json:"ranking_by,omitempty" jsonschema:"Ranking source: both (default), preview, or download"`
	PeriodDays int    `json:"period_days,omitempty" jsonschema:"Days of history to rank; defaults to 1"`
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum files to return; defaults to 50, maximum 1000"`
	Offset     int    `json:"offset,omitempty" jsonschema:"Entries to skip for pagination"`
}

type getDriveConnectionSummaryOutput struct {
	NAS     string                          `json:"nas" jsonschema:"NAS profile used for the request"`
	Summary synology.DriveConnectionSummary `json:"summary" jsonschema:"Active Drive connection counts by client family"`
}

type getDriveDBUsageOutput struct {
	NAS   string                `json:"nas" jsonschema:"NAS profile used for the request"`
	Usage synology.DriveDBUsage `json:"usage" jsonschema:"Cached Drive database usage in bytes"`
}

type getDriveTopFilesOutput struct {
	NAS   string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Files synology.DriveTopAccessFiles `json:"files" jsonschema:"Top accessed files, most accessed first"`
}

type getDriveActivationOutput struct {
	NAS        string                   `json:"nas" jsonschema:"NAS profile used for the request"`
	Activation synology.DriveActivation `json:"activation" jsonschema:"Drive package activation state"`
}

type getDriveUsersInput struct {
	NAS        string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Type       string `json:"type,omitempty" jsonschema:"Account realm: local (default), domain, or ldap"`
	DomainName string `json:"domain_name,omitempty" jsonschema:"Domain to query when type is domain or ldap"`
}

type getDriveUsersOutput struct {
	NAS        string                      `json:"nas" jsonschema:"NAS profile used for the request"`
	Privileges synology.DrivePrivilegeList `json:"privileges" jsonschema:"Accounts with their Drive privilege state"`
}

type getDriveFilesInput struct {
	NAS            string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	TeamFolder     string `json:"team_folder,omitempty" jsonschema:"Team folder (shared-folder name) to browse; empty browses the signed-in account's My Drive"`
	Pattern        string `json:"pattern,omitempty" jsonschema:"Substring filter on the node name"`
	Recursive      bool   `json:"recursive,omitempty" jsonschema:"Search the whole view instead of one directory level"`
	ExcludeRemoved bool   `json:"exclude_removed,omitempty" jsonschema:"Hide removed entries (included by default — this is the rescue view)"`
	Limit          int    `json:"limit,omitempty" jsonschema:"Maximum nodes to return; defaults to 100, maximum 1000"`
	Offset         int    `json:"offset,omitempty" jsonschema:"Nodes to skip for pagination"`
}

type getDriveFilesOutput struct {
	NAS   string              `json:"nas" jsonschema:"NAS profile used for the request"`
	Nodes synology.DriveNodes `json:"nodes" jsonschema:"Drive view contents, including removed entries unless excluded"`
}

type getDriveFileVersionsInput struct {
	NAS        string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	TeamFolder string `json:"team_folder,omitempty" jsonschema:"Team folder (shared-folder name); empty targets the signed-in account's My Drive"`
	Path       string `json:"path" jsonschema:"Node path inside the Drive view, as returned by get_drive_files"`
}

type getDriveFileVersionsOutput struct {
	NAS      string                     `json:"nas" jsonschema:"NAS profile used for the request"`
	Versions synology.DriveNodeVersions `json:"versions" jsonschema:"Stored version history for the node"`
}

type getDriveLogExportInput struct {
	NAS        string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	TeamFolder string `json:"team_folder,omitempty" jsonschema:"Filter to one Drive team folder by shared-folder name"`
	Keyword    string `json:"keyword,omitempty" jsonschema:"Substring filter applied by Drive"`
	Username   string `json:"username,omitempty" jsonschema:"Filter to one account name"`
	From       int64  `json:"from,omitempty" jsonschema:"Inclusive lower bound as a Unix time in seconds"`
	To         int64  `json:"to,omitempty" jsonschema:"Inclusive upper bound as a Unix time in seconds"`
}

type getDriveLogExportOutput struct {
	NAS   string `json:"nas" jsonschema:"NAS profile used for the request"`
	CSV   string `json:"csv" jsonschema:"Exported Drive server log as CSV text"`
	Bytes int    `json:"bytes" jsonschema:"Size of the exported CSV in bytes"`
}

type planDriveRestoreInput struct {
	NAS     string                        `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request application.NodeRestoreChange `json:"request" jsonschema:"Node restore intent: team folder, node paths from get_drive_files, and options"`
}

type planDriveRestoreOutput struct {
	Plan application.DriveNodeRestorePlan `json:"plan" jsonschema:"Validated plan bound to the resolved node entries and approval hash"`
}

type applyDriveRestorePlanInput struct {
	Plan         application.DriveNodeRestorePlan `json:"plan" jsonschema:"Unmodified plan returned by plan_drive_restore"`
	ApprovalHash string                           `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved restore plan"`
}

type applyDriveRestorePlanOutput struct {
	Result application.DriveNodeRestoreApplyResult `json:"result" jsonschema:"Restore result after the task completes and the view is re-read"`
}

type planDriveConnectionKickInput struct {
	NAS       string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	SessionID string `json:"session_id" jsonschema:"Drive client session identifier exactly as listed by get_drive_admin_connections"`
}

type planDriveConnectionKickOutput struct {
	Plan application.DriveConnectionKickPlan `json:"plan" jsonschema:"Validated plan bound to the observed connection entry and approval hash"`
}

type applyDriveConnectionKickPlanInput struct {
	Plan         application.DriveConnectionKickPlan `json:"plan" jsonschema:"Unmodified plan returned by plan_drive_connection_kick"`
	ApprovalHash string                              `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved kick plan"`
}

type applyDriveConnectionKickPlanOutput struct {
	Result application.DriveConnectionKickApplyResult `json:"result" jsonschema:"Disconnect result after stale-state and postcondition checks"`
}

type planDriveTeamFolderChangeInput struct {
	NAS     string                      `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request driveadmin.TeamFolderChange `json:"request" jsonschema:"Team-folder intent: enable, disable, or set_versioning for one shared folder"`
}

type planDriveTeamFolderChangeOutput struct {
	Plan application.DriveTeamFolderPlan `json:"plan" jsonschema:"Validated plan bound to the observed team-folder entry and approval hash"`
}

type applyDriveTeamFolderPlanInput struct {
	Plan         application.DriveTeamFolderPlan `json:"plan" jsonschema:"Unmodified plan returned by plan_drive_team_folder_change"`
	ApprovalHash string                          `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved team-folder plan"`
}

type applyDriveTeamFolderPlanOutput struct {
	Result application.DriveTeamFolderApplyResult `json:"result" jsonschema:"Team-folder mutation result after stale-state and postcondition checks"`
}

func New(service *application.Service, version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "dsmctl", Version: version}, nil)

	// A single central hook classifies every failed tool result and attaches its
	// stable DSM error category, so no per-tool handler needs to know the
	// taxonomy. See categoryErrorMiddleware.
	server.AddReceivingMiddleware(categoryErrorMiddleware())

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_nas",
		Description: "List configured Synology NAS connection profiles. Passwords are never returned.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ listNASInput) (*mcp.CallToolResult, listNASOutput, error) {
		profiles, err := service.ListNASContext(ctx)
		if err != nil {
			return nil, listNASOutput{}, err
		}
		return nil, listNASOutput{NAS: profiles}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "discover_lan_devices",
		Title:       "Discover Synology devices on the LAN",
		Description: "Broadcast a Synology findhost discovery query on the local network and return the Synology devices that answer: hostname, model, OS version, serial, IPv4 address(es), MAC, and self-reported state. Re-broadcasts throughout the listen window and accumulates answers, so a longer timeout returns a more complete set when another findhost listener (e.g. Synology Assistant) is contending for UDP 9999. Each scan is merged into a saved cross-run set; pass cached=true to return that saved set without scanning. Needs no configured NAS, credential, or DSM session, and contacts no NAS — it only sends discovery query packets. It sees only devices in the local broadcast domain of the host running dsmctl.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input discoverLANDevicesInput) (*mcp.CallToolResult, discoverLANDevicesOutput, error) {
		if input.Cached {
			saved, err := service.CachedDiscoveries(ctx)
			if err != nil {
				return nil, discoverLANDevicesOutput{}, err
			}
			devices := make([]discovery.Device, len(saved.Devices))
			for i, record := range saved.Devices {
				devices[i] = record.Device
			}
			return nil, discoverLANDevicesOutput{Devices: devices, SavedTotal: len(saved.Devices)}, nil
		}
		result, err := service.DiscoverDevices(ctx, discovery.Query{Timeout: time.Duration(input.TimeoutSeconds) * time.Second})
		if err != nil {
			return nil, discoverLANDevicesOutput{}, err
		}
		return nil, discoverLANDevicesOutput{Devices: result.Devices, SavedTotal: result.SavedTotal}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_auth_status",
		Title:       "Get authentication status",
		Description: "Report, per configured NAS, whether a password, trusted-device credential, or web-login session is stored, the password environment fallback name and whether it is set, and whether this process holds a DSM session. Never returns secret values, never accepts passwords or OTPs, and never contacts the NAS. Missing authentication is enrolled through the local CLI or the gateway administration page.",
		Annotations: localReadOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getAuthStatusInput) (*mcp.CallToolResult, getAuthStatusOutput, error) {
		result, err := service.GetAuthStatus(ctx, input.NAS)
		if err != nil {
			return nil, getAuthStatusOutput{}, err
		}
		return nil, getAuthStatusOutput{Statuses: result.Statuses}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "provision_nas",
		Title:       "Provision a fresh NAS (create first administrator)",
		Description: "Create the first administrator on a factory-fresh NAS that is in its DSM setup window, behind an already-added, credential-less NAS profile. The administrator password is generated on the server and stored in the credential store; it is NEVER returned, logged, or accepted as input — retrieve it afterward only through the human-gated reveal. Refuses a profile that already holds a stored credential (so a grant cannot re-provision a set-up NAS). Remote callers need the nas.provision scope and the target NAS in their allowlist.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input application.ProvisionRequest) (*mcp.CallToolResult, application.ProvisionResult, error) {
		result, err := service.ProvisionNAS(ctx, input)
		if err != nil {
			return nil, application.ProvisionResult{}, err
		}
		return nil, result, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "provision_discovered_nas",
		Title:       "Provision a discovered, un-enrolled fresh NAS",
		Description: "Provision a factory-fresh NAS that has NO profile yet, by its LAN url (for example one just returned by discover_lan_devices). The server restricts the target to a private/loopback/link-local address, trusts the certificate it observes on first contact, creates a pinned profile, then creates the first administrator. The generated password is stored in the credential store and is NEVER returned or logged — retrieve it only through the human-gated reveal. The new profile is not added to any token's allowlist. Remote callers need the nas.provision scope; this is a LAN/VPN bootstrap and sends a generated password to the device, so grant it only to a trusted provisioning client.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input application.ProvisionRequest) (*mcp.CallToolResult, application.ProvisionResult, error) {
		result, err := service.ProvisionDiscoveredNAS(ctx, input)
		if err != nil {
			return nil, application.ProvisionResult{}, err
		}
		return nil, result, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "install_discovered_nas",
		Title:       "Detect and online-install DSM on a discovered device",
		Description: "Detect the DSM install state of a discovered LAN device (never installed, crashed, or migratable) by its Web Assistant url, and — with trigger=true — start an ONLINE DSM install (the device downloads DSM from Synology and reboots). This is DESTRUCTIVE: it erases the device's disks. It does not wait for the multi-minute install/reboot; re-call to check state, then create the first administrator with provision_discovered_nas. The offline .pat path (host downloads the image) is CLI-only. Remote callers need the nas.provision scope; the target is bounded to LAN/VPN addresses.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input application.InstallRequest) (*mcp.CallToolResult, application.InstallStatus, error) {
		result, err := service.InstallDiscoveredNAS(ctx, input)
		if err != nil {
			return nil, application.InstallStatus{}, err
		}
		return nil, result, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_system_info",
		Description: "Log in to a configured Synology NAS and return basic DSM system information.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getSystemInfoInput) (*mcp.CallToolResult, getSystemInfoOutput, error) {
		result, err := service.GetSystemInfo(ctx, input.NAS)
		if err != nil {
			return nil, getSystemInfoOutput{}, err
		}
		return nil, getSystemInfoOutput{NAS: result.NAS, System: result.System}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_capabilities",
		Description: "Discover the DSM target and report supported capabilities plus the version-specific backend selected for each operation.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getCapabilitiesInput) (*mcp.CallToolResult, getCapabilitiesOutput, error) {
		result, err := service.GetCompatibility(ctx, input.NAS)
		if err != nil {
			return nil, getCapabilitiesOutput{}, err
		}
		return nil, getCapabilitiesOutput{NAS: result.NAS, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "explain_effective_access",
		Title:       "Explain effective access",
		Description: "Explain one local user's or group's effective access to one shared folder or application using direct rules, memberships, group rules, and deny precedence. Custom ACLs and IP-specific rules return indeterminate rather than a guessed answer.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input explainEffectiveAccessInput) (*mcp.CallToolResult, explainEffectiveAccessOutput, error) {
		result, err := service.ExplainEffectiveAccess(ctx, input.NAS, access.Query{
			PrincipalType: input.PrincipalType,
			Principal:     input.Principal,
			ResourceType:  input.ResourceType,
			Resource:      input.Resource,
		})
		if err != nil {
			return nil, explainEffectiveAccessOutput{}, err
		}
		return nil, explainEffectiveAccessOutput{NAS: result.NAS, Explanation: result.Explanation}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_control_panel_time_capabilities",
		Title:       "Get Control Panel time capabilities",
		Description: "Report whether the focused time and NTP module can be read and changed, plus the DSM API version-specific backend selected for each operation.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getControlPanelTimeInput) (*mcp.CallToolResult, getControlPanelTimeCapabilitiesOutput, error) {
		result, err := service.GetControlPanelTimeCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getControlPanelTimeCapabilitiesOutput{}, err
		}
		return nil, getControlPanelTimeCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_control_panel_time_state",
		Title:       "Get Control Panel time state",
		Description: "Read normalized DSM time zone, date/time display formats, synchronization mode, and NTP servers. This tool never changes the clock or NTP configuration.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getControlPanelTimeInput) (*mcp.CallToolResult, getControlPanelTimeStateOutput, error) {
		result, err := service.GetControlPanelTimeState(ctx, input.NAS)
		if err != nil {
			return nil, getControlPanelTimeStateOutput{}, err
		}
		return nil, getControlPanelTimeStateOutput{NAS: result.NAS, Time: result.Time}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_external_access_capabilities",
		Title:       "Get External Access capabilities",
		Description: "Report which External Access read areas (Synology Account, QuickConnect, DDNS) are available for a NAS and the DSM API backend selected for each. Each area is independent.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getExternalAccessInput) (*mcp.CallToolResult, getExternalAccessCapabilitiesOutput, error) {
		result, err := service.GetExternalAccessCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getExternalAccessCapabilitiesOutput{}, err
		}
		return nil, getExternalAccessCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_external_access_account",
		Title:       "Get Synology Account binding",
		Description: "Read the Synology Account (MyDS) binding for a NAS: whether an account is signed in and activated, plus the non-secret account identifier, customer id, and serial. The account token is never returned. This tool never changes the binding.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getExternalAccessInput) (*mcp.CallToolResult, getExternalAccessAccountOutput, error) {
		result, err := service.GetExternalAccessAccount(ctx, input.NAS)
		if err != nil {
			return nil, getExternalAccessAccountOutput{}, err
		}
		return nil, getExternalAccessAccountOutput{NAS: result.NAS, Account: result.Account}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_external_access_quickconnect",
		Title:       "Get QuickConnect configuration",
		Description: "Read QuickConnect configuration and live status for a NAS: whether it is enabled, the QuickConnect ID and region, the relay setting, the connection status, and which services are exposed externally. This tool never changes QuickConnect.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getExternalAccessInput) (*mcp.CallToolResult, getExternalAccessQuickConnectOutput, error) {
		result, err := service.GetExternalAccessQuickConnect(ctx, input.NAS)
		if err != nil {
			return nil, getExternalAccessQuickConnectOutput{}, err
		}
		return nil, getExternalAccessQuickConnectOutput{NAS: result.NAS, QuickConnect: result.QuickConnect}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_external_access_ddns",
		Title:       "Get DDNS configuration",
		Description: "Read the configured Dynamic DNS records and the WAN addresses DSM detected for a NAS. This tool never changes DDNS records.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getExternalAccessInput) (*mcp.CallToolResult, getExternalAccessDDNSOutput, error) {
		result, err := service.GetExternalAccessDDNS(ctx, input.NAS)
		if err != nil {
			return nil, getExternalAccessDDNSOutput{}, err
		}
		return nil, getExternalAccessDDNSOutput{NAS: result.NAS, DDNS: result.DDNS}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_external_access_port_forward",
		Title:       "Get router and port-forwarding configuration",
		Description: "Read the paired router configuration and the configured port-forwarding rules for a NAS (Control Panel → External Access → Router Configuration). This tool never changes router or port-forwarding settings.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getExternalAccessInput) (*mcp.CallToolResult, getExternalAccessPortForwardOutput, error) {
		result, err := service.GetExternalAccessPortForward(ctx, input.NAS)
		if err != nil {
			return nil, getExternalAccessPortForwardOutput{}, err
		}
		return nil, getExternalAccessPortForwardOutput{NAS: result.NAS, PortForward: result.PortForward}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_external_access_quickconnect_change",
		Title:       "Plan a QuickConnect relay change",
		Description: "Validate a patch-only QuickConnect relay-toggle request and return an approval plan bound to the complete observed QuickConnect state. Only the relay flag is writable; enabling QuickConnect or changing the alias are not. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planExternalAccessQuickConnectChangeInput) (*mcp.CallToolResult, planExternalAccessQuickConnectChangeOutput, error) {
		plan, err := service.PlanExternalAccessQuickConnectChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planExternalAccessQuickConnectChangeOutput{}, err
		}
		return nil, planExternalAccessQuickConnectChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_external_access_quickconnect_plan",
		Title:       "Apply an approved QuickConnect plan",
		Description: "Apply an unmodified QuickConnect plan only while its approval hash and the complete observed QuickConnect state still match, then verify the relay setting. Toggling relay changes external reachability and is classified high risk.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyExternalAccessQuickConnectPlanInput) (*mcp.CallToolResult, applyExternalAccessQuickConnectPlanOutput, error) {
		result, err := service.ApplyExternalAccessQuickConnectPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyExternalAccessQuickConnectPlanOutput{}, err
		}
		return nil, applyExternalAccessQuickConnectPlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_external_access_quickconnect_config_change",
		Title:       "Plan a QuickConnect config change",
		Description: "Validate a patch-only QuickConnect enable/alias/region change and return a hash-bound approval plan. Always high risk: changing the alias re-registers a globally-unique external name and enabling/disabling changes public reachability. NOT live-verified against DSM; the guarded apply fails closed on a wrong field. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planExternalAccessQuickConnectConfigChangeInput) (*mcp.CallToolResult, planExternalAccessQuickConnectConfigChangeOutput, error) {
		plan, err := service.PlanExternalAccessQuickConnectConfigChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planExternalAccessQuickConnectConfigChangeOutput{}, err
		}
		return nil, planExternalAccessQuickConnectConfigChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_external_access_quickconnect_config_plan",
		Title:       "Apply an approved QuickConnect config plan",
		Description: "Apply an unmodified QuickConnect config plan only while its approval hash and observed state still match, then verify each changed field. High risk; changing the alias re-registers a globally-unique external name.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyExternalAccessQuickConnectConfigPlanInput) (*mcp.CallToolResult, applyExternalAccessQuickConnectConfigPlanOutput, error) {
		result, err := service.ApplyExternalAccessQuickConnectConfigPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyExternalAccessQuickConnectConfigPlanOutput{}, err
		}
		return nil, applyExternalAccessQuickConnectConfigPlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_external_access_quickconnect_permission_change",
		Title:       "Plan a QuickConnect per-service exposure change",
		Description: "Validate a per-service QuickConnect exposure change (which services are reachable via QuickConnect) and return a hash-bound approval plan. High risk: alters public reachability. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planExternalAccessQuickConnectPermissionChangeInput) (*mcp.CallToolResult, planExternalAccessQuickConnectPermissionChangeOutput, error) {
		plan, err := service.PlanExternalAccessQuickConnectPermissionChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planExternalAccessQuickConnectPermissionChangeOutput{}, err
		}
		return nil, planExternalAccessQuickConnectPermissionChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_external_access_quickconnect_permission_plan",
		Title:       "Apply an approved QuickConnect permission plan",
		Description: "Apply an unmodified QuickConnect per-service exposure plan only while its approval hash and observed state still match, then verify each service's exposure. High risk; alters public reachability.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyExternalAccessQuickConnectPermissionPlanInput) (*mcp.CallToolResult, applyExternalAccessQuickConnectPermissionPlanOutput, error) {
		result, err := service.ApplyExternalAccessQuickConnectPermissionPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyExternalAccessQuickConnectPermissionPlanOutput{}, err
		}
		return nil, applyExternalAccessQuickConnectPermissionPlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_external_access_ddns_change",
		Title:       "Plan a DDNS record change",
		Description: "Validate a DDNS record create/set/delete (keyed by provider + hostname; password via a credential reference, never a value) and return a hash-bound approval plan. Always high risk: creating a record publishes a public hostname pointing at the NAS. NOT live-verified against DSM; the guarded apply fails closed on a wrong field. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planExternalAccessDDNSChangeInput) (*mcp.CallToolResult, planExternalAccessDDNSChangeOutput, error) {
		plan, err := service.PlanExternalAccessDDNSChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planExternalAccessDDNSChangeOutput{}, err
		}
		return nil, planExternalAccessDDNSChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_external_access_ddns_plan",
		Title:       "Apply an approved DDNS record plan",
		Description: "Apply an unmodified DDNS record plan only while its approval hash and observed state still match, resolve the credential reference, and verify the record is present (create/set) or absent (delete). High risk; publishes or removes a public hostname.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyExternalAccessDDNSPlanInput) (*mcp.CallToolResult, applyExternalAccessDDNSPlanOutput, error) {
		result, err := service.ApplyExternalAccessDDNSPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyExternalAccessDDNSPlanOutput{}, err
		}
		return nil, applyExternalAccessDDNSPlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_download_station_capabilities",
		Title:       "Get Download Station capabilities",
		Description: "Report which Download Station reads are available for a NAS, the installed DownloadStation package evidence, and the DSM backend selected for each. Fails closed when the package is not installed.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDownloadStationInput) (*mcp.CallToolResult, getDownloadStationCapabilitiesOutput, error) {
		result, err := service.GetDownloadStationCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getDownloadStationCapabilitiesOutput{}, err
		}
		return nil, getDownloadStationCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_download_station_service",
		Title:       "Get Download Station service configuration",
		Description: "Read the Download Station service configuration for a NAS: version, manager flag, default destination, eMule/auto-unzip switches, per-protocol rate limits, and the bandwidth schedule. This tool never changes the configuration.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDownloadStationInput) (*mcp.CallToolResult, getDownloadStationServiceOutput, error) {
		result, err := service.GetDownloadStationService(ctx, input.NAS)
		if err != nil {
			return nil, getDownloadStationServiceOutput{}, err
		}
		return nil, getDownloadStationServiceOutput{NAS: result.NAS, Service: result.Service}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_download_station_tasks",
		Title:       "Get Download Station tasks",
		Description: "List the Download Station download tasks for a NAS with per-task type, title, size, status, and transfer speed. This tool never creates, pauses, resumes, or deletes tasks.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDownloadStationInput) (*mcp.CallToolResult, getDownloadStationTasksOutput, error) {
		result, err := service.GetDownloadStationTasks(ctx, input.NAS)
		if err != nil {
			return nil, getDownloadStationTasksOutput{}, err
		}
		return nil, getDownloadStationTasksOutput{NAS: result.NAS, Tasks: result.Tasks}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_download_station_statistics",
		Title:       "Get Download Station statistics",
		Description: "Read the current aggregate download and upload speed for a NAS's Download Station. This tool is read-only.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDownloadStationInput) (*mcp.CallToolResult, getDownloadStationStatisticsOutput, error) {
		result, err := service.GetDownloadStationStatistics(ctx, input.NAS)
		if err != nil {
			return nil, getDownloadStationStatisticsOutput{}, err
		}
		return nil, getDownloadStationStatisticsOutput{NAS: result.NAS, Statistics: result.Statistics}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_filestation_capabilities",
		Title:       "Get FileStation capabilities",
		Description: "Report which FileStation reads (info, list, search, directory size, MD5, virtual folders, permission check) are available for a NAS and the DSM API version-specific backend selected for each. FileStation is a core DSM surface, so no package is required.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input fileStationNASInput) (*mcp.CallToolResult, getFileStationCapabilitiesOutput, error) {
		result, err := service.GetFileStationCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getFileStationCapabilitiesOutput{}, err
		}
		return nil, getFileStationCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_filestation_info",
		Title:       "Get FileStation service information",
		Description: "Read FileStation-wide information for the current session on a NAS: host name, whether the account has manager rights, whether public file sharing is supported, and the supported virtual mount protocols. This tool is read-only.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input fileStationNASInput) (*mcp.CallToolResult, getFileStationInfoOutput, error) {
		result, err := service.GetFileStationInfo(ctx, input.NAS)
		if err != nil {
			return nil, getFileStationInfoOutput{}, err
		}
		return nil, getFileStationInfoOutput{NAS: result.NAS, Service: result.Service}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_filestation_shares",
		Title:       "List FileStation shared folders",
		Description: "List the shared folders visible to the current session on a NAS, each with its path, real volume path, size, owner, timestamps, and permission summary. This tool never changes anything.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input listFileStationSharesInput) (*mcp.CallToolResult, fileStationListingOutput, error) {
		result, err := service.ListFileStationShares(ctx, input.NAS, filestation.ListShareQuery{
			OnlyWritable: input.OnlyWritable,
			Limit:        input.Limit,
			Offset:       input.Offset,
		})
		if err != nil {
			return nil, fileStationListingOutput{}, err
		}
		return nil, fileStationListingOutput{NAS: result.NAS, Listing: result.Listing}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_filestation_directory",
		Title:       "List a FileStation directory",
		Description: "List the entries of one folder on a NAS with optional pattern, file-type, sort, and paging, returning each entry's path, size, owner, timestamps, and permission summary. This tool never changes anything.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input listFileStationInput) (*mcp.CallToolResult, fileStationListingOutput, error) {
		result, err := service.ListFileStationDirectory(ctx, input.NAS, filestation.ListQuery{
			Path:          input.Path,
			Limit:         input.Limit,
			Offset:        input.Offset,
			SortBy:        input.SortBy,
			SortDirection: input.SortDirection,
			Pattern:       input.Pattern,
			FileType:      input.FileType,
		})
		if err != nil {
			return nil, fileStationListingOutput{}, err
		}
		return nil, fileStationListingOutput{NAS: result.NAS, Listing: result.Listing}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_filestation_entry_info",
		Title:       "Get FileStation entry information",
		Description: "Read detailed information for one or more files or folders on a NAS by absolute path: type, real volume path, size, owner, timestamps, and permission summary. This tool is read-only.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getFileStationEntryInfoInput) (*mcp.CallToolResult, fileStationEntryInfoOutput, error) {
		result, err := service.GetFileStationEntryInfo(ctx, input.NAS, filestation.GetInfoQuery{Paths: input.Paths})
		if err != nil {
			return nil, fileStationEntryInfoOutput{}, err
		}
		return nil, fileStationEntryInfoOutput{NAS: result.NAS, Info: result.Info}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_filestation_search",
		Title:       "Search FileStation",
		Description: "Search a folder subtree on a NAS for entries matching a name pattern, extension, or file type, and return the completed result. The search runs as a background task that this tool starts, polls to completion, and cleans up. It never changes anything.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input searchFileStationInput) (*mcp.CallToolResult, fileStationSearchOutput, error) {
		result, err := service.SearchFileStation(ctx, input.NAS, filestation.SearchQuery{
			Path:      input.Path,
			Pattern:   input.Pattern,
			Extension: input.Extension,
			FileType:  input.FileType,
			Recursive: input.Recursive,
		})
		if err != nil {
			return nil, fileStationSearchOutput{}, err
		}
		return nil, fileStationSearchOutput{NAS: result.NAS, Result: result.Result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_filestation_directory_size",
		Title:       "Get FileStation directory size",
		Description: "Compute the aggregate size and file/directory counts of one or more folders on a NAS. The computation runs as a background task that this tool starts, polls to completion, and stops. It never changes anything.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input fileStationDirSizeInput) (*mcp.CallToolResult, fileStationDirSizeOutput, error) {
		result, err := service.GetFileStationDirSize(ctx, input.NAS, filestation.DirSizeQuery{Paths: input.Paths})
		if err != nil {
			return nil, fileStationDirSizeOutput{}, err
		}
		return nil, fileStationDirSizeOutput{NAS: result.NAS, DirSize: result.DirSize}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_filestation_md5",
		Title:       "Get FileStation file MD5",
		Description: "Compute the MD5 digest of a file on a NAS. The computation runs as a background task that this tool starts and polls to completion. It never changes anything.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input fileStationMD5Input) (*mcp.CallToolResult, fileStationMD5Output, error) {
		result, err := service.GetFileStationMD5(ctx, input.NAS, filestation.MD5Query{Path: input.Path})
		if err != nil {
			return nil, fileStationMD5Output{}, err
		}
		return nil, fileStationMD5Output{NAS: result.NAS, MD5: result.MD5}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_filestation_virtual_folders",
		Title:       "List FileStation virtual folders",
		Description: "List the mounted virtual folders (for example remote CIFS or NFS mounts) visible to the current session on a NAS. This tool is read-only.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input fileStationNASInput) (*mcp.CallToolResult, fileStationListingOutput, error) {
		result, err := service.ListFileStationVirtualFolders(ctx, input.NAS, filestation.VirtualFolderQuery{})
		if err != nil {
			return nil, fileStationListingOutput{}, err
		}
		return nil, fileStationListingOutput{NAS: result.NAS, Listing: result.Listing}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_filestation_write_permission",
		Title:       "Check FileStation write permission",
		Description: "Probe whether the current session may create or write at a path on a NAS, without creating or modifying any file. It is a read-only permission check.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input checkFileStationPermissionInput) (*mcp.CallToolResult, fileStationPermissionOutput, error) {
		result, err := service.CheckFileStationPermission(ctx, input.NAS, filestation.CheckPermissionQuery{
			Path:          input.Path,
			Filename:      input.Filename,
			Overwrite:     input.Overwrite,
			CreateParents: input.CreateParents,
		})
		if err != nil {
			return nil, fileStationPermissionOutput{}, err
		}
		return nil, fileStationPermissionOutput{NAS: result.NAS, Permission: result.Permission}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_filestation_file_content",
		Title:       "Download FileStation file content",
		Description: "Download a file from a NAS and return its content base64-encoded. A file larger than the 8 MiB inline limit is refused — stream it with the CLI 'file get' instead. This reads the NAS and never modifies it.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getFileStationFileContentInput) (*mcp.CallToolResult, getFileStationFileContentOutput, error) {
		limit := input.MaxBytes
		if limit <= 0 || limit > maxInlineFileDownload {
			limit = maxInlineFileDownload
		}
		data, meta, err := service.ReadFileStationFile(ctx, input.NAS, input.Path, limit)
		if err != nil {
			return nil, getFileStationFileContentOutput{}, err
		}
		return nil, getFileStationFileContentOutput{
			NAS:           meta.NAS,
			Path:          meta.Path,
			Size:          meta.Size,
			ContentType:   meta.ContentType,
			Filename:      meta.Filename,
			ContentBase64: base64.StdEncoding.EncodeToString(data),
		}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_filestation_thumbnail",
		Title:       "Download FileStation image thumbnail",
		Description: "Fetch an image thumbnail from a NAS and return it base64-encoded (size small/medium/large/original, optional rotation). A rendition larger than the 8 MiB inline limit is refused — stream it with the CLI 'file thumb' instead. This reads the NAS and never modifies it.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getFileStationThumbnailInput) (*mcp.CallToolResult, getFileStationThumbnailOutput, error) {
		limit := input.MaxBytes
		if limit <= 0 || limit > maxInlineFileDownload {
			limit = maxInlineFileDownload
		}
		size := input.Size
		if size == "" {
			size = "small"
		}
		switch size {
		case "small", "medium", "large", "original":
		default:
			return nil, getFileStationThumbnailOutput{}, fmt.Errorf("size must be small, medium, large, or original")
		}
		if input.Rotate < 0 || input.Rotate > 4 {
			return nil, getFileStationThumbnailOutput{}, fmt.Errorf("rotate must be between 0 and 4")
		}
		data, meta, err := service.ReadFileStationThumbnail(ctx, input.NAS, input.Path, size, input.Rotate, limit)
		if err != nil {
			return nil, getFileStationThumbnailOutput{}, err
		}
		return nil, getFileStationThumbnailOutput{
			NAS:           meta.NAS,
			Path:          meta.Path,
			Size:          meta.Size,
			ContentType:   meta.ContentType,
			ContentBase64: base64.StdEncoding.EncodeToString(data),
		}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_filestation_favorites",
		Title:       "List FileStation favorites",
		Description: "List the current session's personal FileStation sidebar favorites (name, path, status). This tool is read-only.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input fileStationNASInput) (*mcp.CallToolResult, getFileStationFavoritesOutput, error) {
		result, err := service.GetFileStationFavorites(ctx, input.NAS)
		if err != nil {
			return nil, getFileStationFavoritesOutput{}, err
		}
		return nil, getFileStationFavoritesOutput{NAS: result.NAS, Favorites: result.Favorites}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_filestation_sharing_links",
		Title:       "List FileStation sharing links",
		Description: "List the public sharing links on a NAS (id, path, public URL, password protection, status). Manage links with plan_filestation_change using the sharelink_create, sharelink_edit, sharelink_delete, and sharelink_clear_invalid actions. This tool is read-only.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input fileStationNASInput) (*mcp.CallToolResult, getFileStationSharingLinksOutput, error) {
		result, err := service.GetFileStationSharingLinks(ctx, input.NAS)
		if err != nil {
			return nil, getFileStationSharingLinksOutput{}, err
		}
		return nil, getFileStationSharingLinksOutput{NAS: result.NAS, Links: result.Links}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_filestation_background_tasks",
		Title:       "List FileStation background tasks",
		Description: "List in-progress or finished background file-operation tasks (copy, move, delete, compress, extract) on a NAS. This tool is read-only.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input fileStationNASInput) (*mcp.CallToolResult, getFileStationBackgroundTasksOutput, error) {
		result, err := service.GetFileStationBackgroundTasks(ctx, input.NAS)
		if err != nil {
			return nil, getFileStationBackgroundTasksOutput{}, err
		}
		return nil, getFileStationBackgroundTasksOutput{NAS: result.NAS, Tasks: result.Tasks}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_filestation_change",
		Title:       "Plan a FileStation change",
		Description: "Validate a FileStation mutation (create_folder, rename, copy, move, delete, compress, extract, upload, sharelink_create, sharelink_edit, sharelink_delete, sharelink_clear_invalid, or clear_finished_tasks) and return an approval plan bound to the observed state. The plan surfaces risk, warnings (data loss, overwrite, public exposure), and a hash. This tool never mutates the NAS. Move, delete, and sharelink_create (anonymous public URL) are high risk; upload reads local_path on the host running dsmctl.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planFileStationChangeInput) (*mcp.CallToolResult, planFileStationChangeOutput, error) {
		plan, err := service.PlanFileStationChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planFileStationChangeOutput{}, err
		}
		return nil, planFileStationChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_filestation_plan",
		Title:       "Apply an approved FileStation plan",
		Description: "Apply an unmodified FileStation plan only while its approval hash and the observed path state still match, then verify the postcondition (created/renamed/copied paths present, moved/deleted sources absent). Deletion is permanent and recursive. Archive passwords resolve from env:NAME references at apply time.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyFileStationPlanInput) (*mcp.CallToolResult, applyFileStationPlanOutput, error) {
		result, err := service.ApplyFileStationPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyFileStationPlanOutput{}, err
		}
		return nil, applyFileStationPlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_download_station_settings",
		Title:       "Get Download Station settings",
		Description: "Read the full detailed Download Station configuration for a NAS: BitTorrent (ports, DHT, encryption, peers, seeding), eMule, FTP/HTTP, NZB, auto-extraction, destination/watch-folder, RSS, and the bandwidth scheduler. The NZB password and auto-extraction passwords are never returned. This tool never changes settings.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDownloadStationInput) (*mcp.CallToolResult, getDownloadStationSettingsOutput, error) {
		result, err := service.GetDownloadStationSettings(ctx, input.NAS)
		if err != nil {
			return nil, getDownloadStationSettingsOutput{}, err
		}
		return nil, getDownloadStationSettingsOutput{NAS: result.NAS, Settings: result.Settings}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_download_station_task_change",
		Title:       "Plan a Download Station task change",
		Description: "Validate a task create/pause/resume/delete/edit request and return an approval plan. Control actions are bound to the observed target tasks (edit also to their destinations) so an apply fails if a target has since changed. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planDownloadStationTaskChangeInput) (*mcp.CallToolResult, planDownloadStationTaskChangeOutput, error) {
		plan, err := service.PlanDownloadStationTaskChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planDownloadStationTaskChangeOutput{}, err
		}
		return nil, planDownloadStationTaskChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_download_station_task_plan",
		Title:       "Apply an approved Download Station task plan",
		Description: "Apply an unmodified task plan only while its approval hash and observed target tasks still match, then verify the postcondition (created/paused/resumed/deleted). Creating or resuming makes the NAS fetch external content; deleting removes the task — these are high risk.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyDownloadStationTaskPlanInput) (*mcp.CallToolResult, applyDownloadStationTaskPlanOutput, error) {
		result, err := service.ApplyDownloadStationTaskPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyDownloadStationTaskPlanOutput{}, err
		}
		return nil, applyDownloadStationTaskPlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_download_station_settings_change",
		Title:       "Plan a Download Station settings change",
		Description: "Validate a patch-only Download Station settings change affecting exactly one group and return an approval plan bound to the complete observed group state. Supported groups: BT (ports, DHT, port forwarding, preview, encryption, rate limits, max peers, seeding), FTP/HTTP (max download rate, per-task connection limit), RSS (feed refresh interval), location (default destination, torrent/NZB watch folder), scheduler (alternative-rate schedule, max tasks), global (download volume, eMule and auto-extract toggles), auto_extraction (per-user extraction preferences), and nzb (Usenet news-server settings). Auto_extraction and nzb are partial sets that never touch their passwords. Note that the location default destination is a per-user binding DSM cannot clear once set. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planDownloadStationSettingsChangeInput) (*mcp.CallToolResult, planDownloadStationSettingsChangeOutput, error) {
		plan, err := service.PlanDownloadStationSettingsChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planDownloadStationSettingsChangeOutput{}, err
		}
		return nil, planDownloadStationSettingsChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_download_station_settings_plan",
		Title:       "Apply an approved Download Station settings plan",
		Description: "Apply an unmodified settings plan only while its approval hash and the complete observed settings group still match, merging the patch into the full group object and verifying each changed field. Enabling port forwarding increases external exposure and is high risk.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyDownloadStationSettingsPlanInput) (*mcp.CallToolResult, applyDownloadStationSettingsPlanOutput, error) {
		result, err := service.ApplyDownloadStationSettingsPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyDownloadStationSettingsPlanOutput{}, err
		}
		return nil, applyDownloadStationSettingsPlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_hyper_backup_capabilities",
		Title:       "Get Hyper Backup capabilities",
		Description: "Report which Hyper Backup reads and guarded actions are available for a NAS, the installed HyperBackup and HyperBackupVault package evidence, and the DSM backend selected for each. Fails closed when a package is not installed.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getHyperBackupInput) (*mcp.CallToolResult, getHyperBackupCapabilitiesOutput, error) {
		result, err := service.GetHyperBackupCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getHyperBackupCapabilitiesOutput{}, err
		}
		return nil, getHyperBackupCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_hyper_backup_tasks",
		Title:       "List Hyper Backup tasks",
		Description: "List the Hyper Backup tasks with state, live activity, last backup time and result, next scheduled run, and backed-up source folders. Requires the HyperBackup package.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getHyperBackupInput) (*mcp.CallToolResult, getHyperBackupTasksOutput, error) {
		result, err := service.GetHyperBackupTasks(ctx, input.NAS)
		if err != nil {
			return nil, getHyperBackupTasksOutput{}, err
		}
		return nil, getHyperBackupTasksOutput{NAS: result.NAS, Tasks: result.Tasks}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_hyper_backup_task",
		Title:       "Get one Hyper Backup task",
		Description: "Read one backup task's destination repository, transfer options, live status with progress while a run is active, and destination reachability.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getHyperBackupTaskInput) (*mcp.CallToolResult, getHyperBackupTaskOutput, error) {
		result, err := service.GetHyperBackupTaskDetail(ctx, input.NAS, input.TaskID)
		if err != nil {
			return nil, getHyperBackupTaskOutput{}, err
		}
		return nil, getHyperBackupTaskOutput{NAS: result.NAS, Task: result.Task}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_hyper_backup_versions",
		Title:       "List Hyper Backup versions",
		Description: "List the backup versions one task has produced, newest first, with completion status and rotation-lock state.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getHyperBackupVersionsInput) (*mcp.CallToolResult, getHyperBackupVersionsOutput, error) {
		result, err := service.GetHyperBackupVersions(ctx, input.NAS, input.TaskID, input.Offset, input.Limit)
		if err != nil {
			return nil, getHyperBackupVersionsOutput{}, err
		}
		return nil, getHyperBackupVersionsOutput{NAS: result.NAS, Versions: result.Versions}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_hyper_backup_logs",
		Title:       "Get Hyper Backup logs",
		Description: "Read a page of the Hyper Backup log feed (task runs, results, and configuration events) plus the feed-wide error/warning/info counts.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getHyperBackupLogsInput) (*mcp.CallToolResult, getHyperBackupLogsOutput, error) {
		result, err := service.GetHyperBackupLogs(ctx, input.NAS, input.Offset, input.Limit)
		if err != nil {
			return nil, getHyperBackupLogsOutput{}, err
		}
		return nil, getHyperBackupLogsOutput{NAS: result.NAS, Logs: result.Logs}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_hyper_backup_applications",
		Title:       "List Hyper Backup backupable applications",
		Description: "List the packages Hyper Backup can include in a backup task, with per-application eligibility (backupable or the reason it is not) and the identifiers a create request's applications list accepts.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getHyperBackupInput) (*mcp.CallToolResult, getHyperBackupApplicationsOutput, error) {
		result, err := service.GetHyperBackupApplications(ctx, input.NAS)
		if err != nil {
			return nil, getHyperBackupApplicationsOutput{}, err
		}
		return nil, getHyperBackupApplicationsOutput{NAS: result.NAS, Applications: result.Applications}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_hyper_backup_vault",
		Title:       "Get the Hyper Backup Vault view",
		Description: "Read the Hyper Backup Vault view of this NAS as a backup destination: the inbound targets stored here and the parallel-session limit. Requires the HyperBackupVault package.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getHyperBackupInput) (*mcp.CallToolResult, getHyperBackupVaultOutput, error) {
		result, err := service.GetHyperBackupVault(ctx, input.NAS)
		if err != nil {
			return nil, getHyperBackupVaultOutput{}, err
		}
		return nil, getHyperBackupVaultOutput{NAS: result.NAS, Vault: result.Vault}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_hyper_backup_task_change",
		Title:       "Plan a Hyper Backup task action",
		Description: "Validate a run-backup-now or cancel request for one backup task, or a create request for a new folder-backup task (destination: a local shared folder, another NAS known to dsmctl via target_nas, or an explicit host with a password_ref), and return an approval plan bound to the observed task state. Destination credentials are resolved only at apply and never enter the plan. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planHyperBackupTaskChangeInput) (*mcp.CallToolResult, planHyperBackupTaskChangeOutput, error) {
		plan, err := service.PlanHyperBackupTaskChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planHyperBackupTaskChangeOutput{}, err
		}
		return nil, planHyperBackupTaskChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_hyper_backup_task_plan",
		Title:       "Apply an approved Hyper Backup task plan",
		Description: "Apply an unmodified task plan only while its approval hash and the observed task state still match, then verify the postcondition (the run started, the running backup stopped, or the created task exists). Running a backup writes a new version to the destination; canceling records the interrupted run with result cancel; creating registers a repository, creates the destination directory, and stores the destination credential in Hyper Backup's configuration on the source NAS.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyHyperBackupTaskPlanInput) (*mcp.CallToolResult, applyHyperBackupTaskPlanOutput, error) {
		result, err := service.ApplyHyperBackupTaskPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyHyperBackupTaskPlanOutput{}, err
		}
		return nil, applyHyperBackupTaskPlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_control_panel_time_change",
		Title:       "Plan a Control Panel time change",
		Description: "Validate a patch-only time zone, display format, or NTP request and return an approval plan bound to the complete observed module state. Manual synchronization mode and wall-clock changes are rejected, and ntp_servers always replaces the whole ordered list. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planControlPanelTimeChangeInput) (*mcp.CallToolResult, planControlPanelTimeChangeOutput, error) {
		plan, err := service.PlanControlPanelTimeChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planControlPanelTimeChangeOutput{}, err
		}
		return nil, planControlPanelTimeChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_control_panel_time_plan",
		Title:       "Apply an approved Control Panel time plan",
		Description: "Apply an unmodified time plan only while its approval hash and the complete observed time state still match, then verify the normalized configuration. NTP servers are validated for syntax only; a verified configuration never implies reachability or synchronization convergence.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyControlPanelTimePlanInput) (*mcp.CallToolResult, applyControlPanelTimePlanOutput, error) {
		result, err := service.ApplyControlPanelTimePlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyControlPanelTimePlanOutput{}, err
		}
		return nil, applyControlPanelTimePlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_system_hostname_change",
		Title:       "Plan a DSM server-name (hostname) change",
		Description: "Validate a new DSM server name (hostname) against the current name and return a hash-bound approval plan. A no-op rename is refused. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planSystemHostnameChangeInput) (*mcp.CallToolResult, planSystemHostnameChangeOutput, error) {
		plan, err := service.PlanSystemHostname(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planSystemHostnameChangeOutput{}, err
		}
		return nil, planSystemHostnameChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_system_hostname_plan",
		Title:       "Apply an approved DSM server-name plan",
		Description: "Apply an unmodified hostname plan only while its approval hash and the observed server name still match, then verify DSM reports the requested name.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applySystemHostnamePlanInput) (*mcp.CallToolResult, applySystemHostnamePlanOutput, error) {
		result, err := service.ApplySystemHostnamePlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applySystemHostnamePlanOutput{}, err
		}
		return nil, applySystemHostnamePlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_file_service_capabilities",
		Title:       "Get SMB and NFS capabilities",
		Description: "Report independently selected SMB and NFS read, base-setting, and advanced-setting DSM backends.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getFileServicesInput) (*mcp.CallToolResult, getFileServiceCapabilitiesOutput, error) {
		result, err := service.GetFileServiceCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getFileServiceCapabilitiesOutput{}, err
		}
		return nil, getFileServiceCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_smb_state",
		Title:       "Get SMB state",
		Description: "Read the global SMB service, workgroup, protocol range, transport encryption, and signing policy without changing DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getFileServicesInput) (*mcp.CallToolResult, getSMBStateOutput, error) {
		result, err := service.GetSMBState(ctx, input.NAS)
		if err != nil {
			return nil, getSMBStateOutput{}, err
		}
		return nil, getSMBStateOutput{NAS: result.NAS, SMB: result.SMB}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_nfs_state",
		Title:       "Get NFS state",
		Description: "Read the global NFS service, highest enabled and supported protocols, and NFSv4 domain without changing DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getFileServicesInput) (*mcp.CallToolResult, getNFSStateOutput, error) {
		result, err := service.GetNFSState(ctx, input.NAS)
		if err != nil {
			return nil, getNFSStateOutput{}, err
		}
		return nil, getNFSStateOutput{NAS: result.NAS, NFS: result.NFS}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_file_service_change",
		Title:       "Plan an SMB or NFS change",
		Description: "Validate one patch-only SMB or NFS settings request and return a full-state-bound approval plan. NFSv4 domain changes are planned separately from NFS base settings.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planFileServiceChangeInput) (*mcp.CallToolResult, planFileServiceChangeOutput, error) {
		plan, err := service.PlanFileServiceChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planFileServiceChangeOutput{}, err
		}
		return nil, planFileServiceChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_file_service_plan",
		Title:       "Apply an approved SMB or NFS plan",
		Description: "Apply an unmodified File Services plan only while its approval hash and complete observed module state still match, then verify the requested postcondition.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyFileServicePlanInput) (*mcp.CallToolResult, applyFileServicePlanOutput, error) {
		result, err := service.ApplyFileServicePlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyFileServicePlanOutput{}, err
		}
		return nil, applyFileServicePlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_nfs_export_capabilities",
		Title:       "Get NFS export capabilities",
		Description: "Report whether per-shared-folder NFS export rules can be read and changed on the selected NAS, and the DSM backend selected.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getFileServicesInput) (*mcp.CallToolResult, getNFSExportCapabilitiesOutput, error) {
		result, err := service.GetNFSExportCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getNFSExportCapabilitiesOutput{}, err
		}
		return nil, getNFSExportCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_nfs_export_state",
		Title:       "Get NFS export rules",
		Description: "Read the complete NFS export rule set (client, privilege, squash, security, async) of one shared folder without changing DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getNFSExportStateInput) (*mcp.CallToolResult, getNFSExportStateOutput, error) {
		result, err := service.GetNFSExportState(ctx, input.NAS, input.Share)
		if err != nil {
			return nil, getNFSExportStateOutput{}, err
		}
		return nil, getNFSExportStateOutput{NAS: result.NAS, Export: result.Export}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_nfs_export_change",
		Title:       "Plan an NFS export change",
		Description: "Validate a complete desired NFS export rule set for one shared folder, read the current rules, and return a hash-bound approval plan. This tool never mutates DSM. The rule set fully replaces the shared folder's existing rules.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planNFSExportChangeInput) (*mcp.CallToolResult, planNFSExportChangeOutput, error) {
		plan, err := service.PlanNFSExportChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planNFSExportChangeOutput{}, err
		}
		return nil, planNFSExportChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_nfs_export_plan",
		Title:       "Apply an approved NFS export plan",
		Description: "Apply an unmodified NFS export plan only while its approval hash and complete observed rule set still match, then verify the resulting rules. The plan replaces the shared folder's entire NFS export rule set.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyNFSExportPlanInput) (*mcp.CallToolResult, applyNFSExportPlanOutput, error) {
		result, err := service.ApplyNFSExportPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyNFSExportPlanOutput{}, err
		}
		return nil, applyNFSExportPlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_service_discovery_capabilities",
		Title:       "Get service discovery capabilities",
		Description: "Report whether File Services Time Machine advertising and WS-Discovery can be read and changed on the selected NAS, and the DSM backend selected.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getFileServicesInput) (*mcp.CallToolResult, getServiceDiscoveryCapabilitiesOutput, error) {
		result, err := service.GetServiceDiscoveryCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getServiceDiscoveryCapabilitiesOutput{}, err
		}
		return nil, getServiceDiscoveryCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_service_discovery_state",
		Title:       "Get service discovery state",
		Description: "Read Time Machine advertising (over SMB and AFP) and WS-Discovery settings without changing DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getFileServicesInput) (*mcp.CallToolResult, getServiceDiscoveryStateOutput, error) {
		result, err := service.GetServiceDiscoveryState(ctx, input.NAS)
		if err != nil {
			return nil, getServiceDiscoveryStateOutput{}, err
		}
		return nil, getServiceDiscoveryStateOutput{NAS: result.NAS, ServiceDiscovery: result.ServiceDiscovery}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_service_discovery_change",
		Title:       "Plan a service discovery change",
		Description: "Validate one patch-only service-discovery request (Time Machine advertising, WS-Discovery), read the current state, and return a hash-bound approval plan. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planServiceDiscoveryChangeInput) (*mcp.CallToolResult, planServiceDiscoveryChangeOutput, error) {
		plan, err := service.PlanServiceDiscoveryChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planServiceDiscoveryChangeOutput{}, err
		}
		return nil, planServiceDiscoveryChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_service_discovery_plan",
		Title:       "Apply an approved service discovery plan",
		Description: "Apply an unmodified service-discovery plan only while its approval hash and complete observed state still match, then verify the requested postcondition.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyServiceDiscoveryPlanInput) (*mcp.CallToolResult, applyServiceDiscoveryPlanOutput, error) {
		result, err := service.ApplyServiceDiscoveryPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyServiceDiscoveryPlanOutput{}, err
		}
		return nil, applyServiceDiscoveryPlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_ftp_service_capabilities",
		Title:       "Get FTP service capabilities",
		Description: "Report whether FTP/FTPS and SFTP can be read and changed on the selected NAS, and the DSM backend selected for each.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getFileServicesInput) (*mcp.CallToolResult, getFTPServicesCapabilitiesOutput, error) {
		result, err := service.GetFTPServicesCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getFTPServicesCapabilitiesOutput{}, err
		}
		return nil, getFTPServicesCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_ftp_service_state",
		Title:       "Get FTP service state",
		Description: "Read the plain FTP, FTPS, and SFTP service switches (and the SFTP port) without changing DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getFileServicesInput) (*mcp.CallToolResult, getFTPServicesStateOutput, error) {
		result, err := service.GetFTPServicesState(ctx, input.NAS)
		if err != nil {
			return nil, getFTPServicesStateOutput{}, err
		}
		return nil, getFTPServicesStateOutput{NAS: result.NAS, FTPServices: result.FTPServices}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_ftp_service_change",
		Title:       "Plan an FTP service change",
		Description: "Validate one patch-only FTP/FTPS/SFTP request, read the current state, and return a hash-bound approval plan. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planFTPServicesChangeInput) (*mcp.CallToolResult, planFTPServicesChangeOutput, error) {
		plan, err := service.PlanFTPServicesChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planFTPServicesChangeOutput{}, err
		}
		return nil, planFTPServicesChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_ftp_service_plan",
		Title:       "Apply an approved FTP service plan",
		Description: "Apply an unmodified FTP/SFTP plan only while its approval hash and complete observed state still match, then verify the requested postcondition.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyFTPServicesPlanInput) (*mcp.CallToolResult, applyFTPServicesPlanOutput, error) {
		result, err := service.ApplyFTPServicesPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyFTPServicesPlanOutput{}, err
		}
		return nil, applyFTPServicesPlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_rsync_service_capabilities",
		Title:       "Get rsync service capabilities",
		Description: "Report whether the rsync network-backup service can be read and changed on the selected NAS, and the DSM backend selected.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getFileServicesInput) (*mcp.CallToolResult, getRsyncServiceCapabilitiesOutput, error) {
		result, err := service.GetRsyncServiceCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getRsyncServiceCapabilitiesOutput{}, err
		}
		return nil, getRsyncServiceCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_rsync_service_state",
		Title:       "Get rsync service state",
		Description: "Read the rsync network-backup service switch, rsync account switch, and shared SSH port without changing DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getFileServicesInput) (*mcp.CallToolResult, getRsyncServiceStateOutput, error) {
		result, err := service.GetRsyncServiceState(ctx, input.NAS)
		if err != nil {
			return nil, getRsyncServiceStateOutput{}, err
		}
		return nil, getRsyncServiceStateOutput{NAS: result.NAS, RsyncService: result.RsyncService}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_rsync_service_change",
		Title:       "Plan an rsync service change",
		Description: "Validate one patch-only rsync-service request, read the current state, and return a hash-bound approval plan. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planRsyncServiceChangeInput) (*mcp.CallToolResult, planRsyncServiceChangeOutput, error) {
		plan, err := service.PlanRsyncServiceChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planRsyncServiceChangeOutput{}, err
		}
		return nil, planRsyncServiceChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_rsync_service_plan",
		Title:       "Apply an approved rsync service plan",
		Description: "Apply an unmodified rsync-service plan only while its approval hash and complete observed state still match, then verify the requested postcondition.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyRsyncServicePlanInput) (*mcp.CallToolResult, applyRsyncServicePlanOutput, error) {
		result, err := service.ApplyRsyncServicePlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyRsyncServicePlanOutput{}, err
		}
		return nil, applyRsyncServicePlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_tftp_service_capabilities",
		Title:       "Get TFTP service capabilities",
		Description: "Report whether the TFTP service can be read and changed on the selected NAS, and the DSM backend selected.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getFileServicesInput) (*mcp.CallToolResult, getTFTPServiceCapabilitiesOutput, error) {
		result, err := service.GetTFTPServiceCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getTFTPServiceCapabilitiesOutput{}, err
		}
		return nil, getTFTPServiceCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_tftp_service_state",
		Title:       "Get TFTP service state",
		Description: "Read the TFTP service switch, root folder, permission, logging, allowed-client range, and timeout without changing DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getFileServicesInput) (*mcp.CallToolResult, getTFTPServiceStateOutput, error) {
		result, err := service.GetTFTPServiceState(ctx, input.NAS)
		if err != nil {
			return nil, getTFTPServiceStateOutput{}, err
		}
		return nil, getTFTPServiceStateOutput{NAS: result.NAS, TFTPService: result.TFTPService}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_tftp_service_change",
		Title:       "Plan a TFTP service change",
		Description: "Validate one patch-only TFTP request, read the current state, and return a hash-bound approval plan. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planTFTPServiceChangeInput) (*mcp.CallToolResult, planTFTPServiceChangeOutput, error) {
		plan, err := service.PlanTFTPServiceChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planTFTPServiceChangeOutput{}, err
		}
		return nil, planTFTPServiceChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_tftp_service_plan",
		Title:       "Apply an approved TFTP service plan",
		Description: "Apply an unmodified TFTP plan only while its approval hash and complete observed state still match, then verify the requested postcondition.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyTFTPServicePlanInput) (*mcp.CallToolResult, applyTFTPServicePlanOutput, error) {
		result, err := service.ApplyTFTPServicePlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyTFTPServicePlanOutput{}, err
		}
		return nil, applyTFTPServicePlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_photos_capabilities",
		Title:       "Get Synology Photos capabilities",
		Description: "Report whether Synology Photos administration settings can be read and changed on the selected NAS, plus the installed package evidence.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getPhotosInput) (*mcp.CallToolResult, getPhotosCapabilitiesOutput, error) {
		result, err := service.GetPhotosCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getPhotosCapabilitiesOutput{}, err
		}
		return nil, getPhotosCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_photos_settings",
		Title:       "Get Synology Photos settings",
		Description: "Read the Synology Photos administration settings (face/concept/similar grouping, user sharing, recycle bins, thumbnail size, excluded extensions) without changing DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getPhotosInput) (*mcp.CallToolResult, getPhotosSettingsOutput, error) {
		result, err := service.GetPhotosSettings(ctx, input.NAS)
		if err != nil {
			return nil, getPhotosSettingsOutput{}, err
		}
		return nil, getPhotosSettingsOutput{NAS: result.NAS, Settings: result.Settings}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_photos_change",
		Title:       "Plan a Synology Photos change",
		Description: "Validate one patch-only Synology Photos administration request, read the current settings, and return a hash-bound approval plan. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planPhotosChangeInput) (*mcp.CallToolResult, planPhotosChangeOutput, error) {
		plan, err := service.PlanPhotosChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planPhotosChangeOutput{}, err
		}
		return nil, planPhotosChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_photos_plan",
		Title:       "Apply an approved Synology Photos plan",
		Description: "Apply an unmodified Synology Photos plan only while its approval hash and complete observed settings still match, then verify the requested postcondition.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyPhotosPlanInput) (*mcp.CallToolResult, applyPhotosPlanOutput, error) {
		result, err := service.ApplyPhotosPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyPhotosPlanOutput{}, err
		}
		return nil, applyPhotosPlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_office_capabilities",
		Title:       "Get Synology Office capabilities",
		Description: "Report whether Synology Office settings can be read and changed on the selected NAS, plus the installed package evidence.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getOfficeInput) (*mcp.CallToolResult, getOfficeCapabilitiesOutput, error) {
		result, err := service.GetOfficeCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getOfficeCapabilitiesOutput{}, err
		}
		return nil, getOfficeCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_office_info",
		Title:       "Get Synology Office info",
		Description: "Read the Synology Office deployment info (version, whether the session user is an Office manager, document schema versions) without changing DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getOfficeInput) (*mcp.CallToolResult, getOfficeInfoOutput, error) {
		result, err := service.GetOfficeInfo(ctx, input.NAS)
		if err != nil {
			return nil, getOfficeInfoOutput{}, err
		}
		return nil, getOfficeInfoOutput{NAS: result.NAS, Info: result.Info}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_office_settings",
		Title:       "Get Synology Office system settings",
		Description: "Read the system-wide Synology Office settings (automatic version-history cleanup) without changing DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getOfficeInput) (*mcp.CallToolResult, getOfficeSettingsOutput, error) {
		result, err := service.GetOfficeSettings(ctx, input.NAS)
		if err != nil {
			return nil, getOfficeSettingsOutput{}, err
		}
		return nil, getOfficeSettingsOutput{NAS: result.NAS, Settings: result.Settings}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_office_preferences",
		Title:       "Get Synology Office preferences",
		Description: "Read the calling user's own Synology Office editor preferences (ruler, formula panel, default locale, AI languages) without changing DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getOfficeInput) (*mcp.CallToolResult, getOfficePreferencesOutput, error) {
		result, err := service.GetOfficePreferences(ctx, input.NAS)
		if err != nil {
			return nil, getOfficePreferencesOutput{}, err
		}
		return nil, getOfficePreferencesOutput{NAS: result.NAS, Preferences: result.Preferences}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_office_fonts",
		Title:       "List Synology Office fonts",
		Description: "List the Synology Office font inventory (name and localized display name) without changing DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getOfficeInput) (*mcp.CallToolResult, getOfficeFontsOutput, error) {
		result, err := service.GetOfficeFonts(ctx, input.NAS)
		if err != nil {
			return nil, getOfficeFontsOutput{}, err
		}
		return nil, getOfficeFontsOutput{NAS: result.NAS, Fonts: result.Fonts}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_office_change",
		Title:       "Plan a Synology Office settings change",
		Description: "Validate one patch-only Synology Office settings request (system scope, the calling user's preferences scope, or a custom-font registry action), read the current state, and return a hash-bound approval plan. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planOfficeChangeInput) (*mcp.CallToolResult, planOfficeChangeOutput, error) {
		plan, err := service.PlanOfficeChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planOfficeChangeOutput{}, err
		}
		return nil, planOfficeChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_office_plan",
		Title:       "Apply an approved Synology Office plan",
		Description: "Apply an unmodified Synology Office plan only while its approval hash and complete observed scope state still match, then verify the requested postcondition.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyOfficePlanInput) (*mcp.CallToolResult, applyOfficePlanOutput, error) {
		result, err := service.ApplyOfficePlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyOfficePlanOutput{}, err
		}
		return nil, applyOfficePlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_storage_capabilities",
		Title:       "Get storage capabilities",
		Description: "Report which storage inventory and guarded mutation operations dsmctl currently supports on a selected NAS and the DSM backend selected for each.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getStorageInput) (*mcp.CallToolResult, getStorageCapabilitiesOutput, error) {
		result, err := service.GetStorageCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getStorageCapabilitiesOutput{}, err
		}
		return nil, getStorageCapabilitiesOutput{
			NAS:          result.NAS,
			Capabilities: result.Capabilities,
			Report:       result.Report,
		}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_storage_state",
		Title:       "Get storage state",
		Description: "Read the normalized physical-disk, storage-pool, RAID type, volume, SSD cache, capacity, and health state from a selected NAS. This tool never changes storage.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getStorageInput) (*mcp.CallToolResult, getStorageStateOutput, error) {
		result, err := service.GetStorageState(ctx, input.NAS)
		if err != nil {
			return nil, getStorageStateOutput{}, err
		}
		return nil, getStorageStateOutput{NAS: result.NAS, Storage: result.Storage}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_storage_change",
		Title:       "Plan a storage change",
		Description: "Validate a typed storage-pool, volume, or SSD cache manifest and return a topology-, capacity-, and safety-state-bound approval plan without mutating DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planStorageChangeInput) (*mcp.CallToolResult, planStorageChangeOutput, error) {
		plan, err := service.PlanStorageChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planStorageChangeOutput{}, err
		}
		return nil, planStorageChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_storage_plan",
		Title:       "Apply an approved storage plan",
		Description: "Apply an unmodified storage plan only while its approval hash, stable IDs, and topology and safety fingerprints still match; then create, expand, or delete the planned storage pool, volume, or SSD cache and verify the postcondition. Storage-pool RAID migration and, where a DSM lacks the backend, SSD cache expand and mode conversion fail closed.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyStoragePlanInput) (*mcp.CallToolResult, applyStoragePlanOutput, error) {
		result, err := service.ApplyStoragePlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyStoragePlanOutput{}, err
		}
		return nil, applyStoragePlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_account_capabilities",
		Title:       "Get account capabilities",
		Description: "Report supported local DSM user, group, membership, quota, and application privilege operations on the selected NAS.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getAccountInput) (*mcp.CallToolResult, getAccountCapabilitiesOutput, error) {
		result, err := service.GetIdentityCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getAccountCapabilitiesOutput{}, err
		}
		return nil, getAccountCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_account_change",
		Title:       "Plan an account change",
		Description: "Validate a local DSM user, group, membership, quota, or application privilege request, read the relevant current state, and return a hash-bound approval plan. This tool never mutates DSM. User passwords are referenced as env:NAME and never embedded in the plan.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planAccountChangeInput) (*mcp.CallToolResult, planAccountChangeOutput, error) {
		plan, err := service.PlanIdentityChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planAccountChangeOutput{}, err
		}
		return nil, planAccountChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_account_plan",
		Title:       "Apply an approved account plan",
		Description: "Apply an unmodified account plan only when its approval hash and observed-state precondition still match, then verify the resulting DSM user, group, membership, quota, or application privilege state.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyAccountPlanInput) (*mcp.CallToolResult, applyAccountPlanOutput, error) {
		result, err := service.ApplyIdentityPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyAccountPlanOutput{}, err
		}
		return nil, applyAccountPlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_account_state",
		Title:       "Get account state",
		Description: "Read normalized local DSM users and groups, optionally expanding memberships, quotas, and explicit application privileges. Use a principal filter for quota or privilege reads on large systems. Passwords, password hashes, and authentication credentials are never returned.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getAccountStateInput) (*mcp.CallToolResult, getAccountStateOutput, error) {
		result, err := service.GetIdentityStateWithQuery(ctx, input.NAS, identity.StateQuery{
			IncludeMemberships: input.IncludeMemberships, IncludeQuotas: input.IncludeQuotas,
			IncludeApplicationPrivileges: input.IncludeApplicationPrivileges,
			PrincipalType:                input.PrincipalType, Principal: input.Principal,
		})
		if err != nil {
			return nil, getAccountStateOutput{}, err
		}
		return nil, getAccountStateOutput{NAS: result.NAS, Identity: result.Identity}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_san_capabilities",
		Title:       "Get SAN capabilities",
		Description: "Report SAN Manager inventory and guarded target, LUN, and mapping operation support plus the selected DSM API backend for each operation. This tool never changes SAN resources.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getSANInput) (*mcp.CallToolResult, getSANCapabilitiesOutput, error) {
		result, err := service.GetSANCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getSANCapabilitiesOutput{}, err
		}
		return nil, getSANCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_san_state",
		Title:       "Get SAN state",
		Description: "Read normalized iSCSI targets, LUNs, stable-ID mappings, provisioning, capacity, sessions, status, and health using two bulk DSM calls. This tool never mutates SAN Manager.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getSANInput) (*mcp.CallToolResult, getSANStateOutput, error) {
		result, err := service.GetSANState(ctx, input.NAS)
		if err != nil {
			return nil, getSANStateOutput{}, err
		}
		return nil, getSANStateOutput{NAS: result.NAS, SAN: result.SAN}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_log_capabilities",
		Title:       "Get DSM log capabilities",
		Description: "Report whether DSM system log reading is available on a selected NAS and the DSM backend selected for it.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getSANInput) (*mcp.CallToolResult, getLogCapabilitiesOutput, error) {
		result, err := service.GetLogCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getLogCapabilitiesOutput{}, err
		}
		return nil, getLogCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_logs",
		Title:       "Get DSM system logs",
		Description: "Read normalized DSM system log entries (SYNO.Core.SyslogClient.Log) with optional keyword, log-type, severity, and paging filters. Returns each entry's time, level, category, actor, and message plus whole-log severity counts. This tool never mutates or clears DSM logs.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getLogsInput) (*mcp.CallToolResult, getLogsOutput, error) {
		fromTime, err := syslog.ParseTime(input.From)
		if err != nil {
			return nil, getLogsOutput{}, err
		}
		toTime, err := syslog.ParseTime(input.To)
		if err != nil {
			return nil, getLogsOutput{}, err
		}
		result, err := service.GetLogState(ctx, input.NAS, syslog.StateQuery{
			Limit: input.Limit, Offset: input.Offset, Keyword: input.Keyword, LogType: input.LogType, Level: input.Level,
			From: fromTime, To: toTime,
		})
		if err != nil {
			return nil, getLogsOutput{}, err
		}
		return nil, getLogsOutput{NAS: result.NAS, Logs: result.Logs}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_notification_capabilities",
		Title:       "Get notification capabilities",
		Description: "Report which DSM notification read areas (email, push, webhook, SMS, rule catalog, desktop toggles, history) are available for a NAS and the DSM API backend selected for each. Each area is independent.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getNotificationInput) (*mcp.CallToolResult, getNotificationCapabilitiesOutput, error) {
		result, err := service.GetNotificationCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getNotificationCapabilitiesOutput{}, err
		}
		return nil, getNotificationCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_notification_mail",
		Title:       "Get email notification settings",
		Description: "Read the DSM email notification channel: whether it is enabled, the SMTP server/port/TLS/auth-user configuration, sender, subject prefix, recipients, and the Synology-relay email mode. The SMTP password is never returned. This tool never changes notification settings.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getNotificationInput) (*mcp.CallToolResult, getNotificationMailOutput, error) {
		result, err := service.GetNotificationMail(ctx, input.NAS)
		if err != nil {
			return nil, getNotificationMailOutput{}, err
		}
		return nil, getNotificationMailOutput{NAS: result.NAS, Mail: result.Mail}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_notification_push",
		Title:       "Get push notification settings",
		Description: "Read the DSM push notification channel: whether mobile push is enabled and which mobile devices or browsers are paired. Device push tokens are never returned. This tool never changes notification settings.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getNotificationInput) (*mcp.CallToolResult, getNotificationPushOutput, error) {
		result, err := service.GetNotificationPush(ctx, input.NAS)
		if err != nil {
			return nil, getNotificationPushOutput{}, err
		}
		return nil, getNotificationPushOutput{NAS: result.NAS, Push: result.Push}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_notification_webhook",
		Title:       "Get webhook notification providers",
		Description: "Read the configured DSM webhook notification providers (id, name, kind, enabled). Webhook URLs and secrets are never returned. This tool never changes notification settings.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getNotificationInput) (*mcp.CallToolResult, getNotificationWebhookOutput, error) {
		result, err := service.GetNotificationWebhook(ctx, input.NAS)
		if err != nil {
			return nil, getNotificationWebhookOutput{}, err
		}
		return nil, getNotificationWebhookOutput{NAS: result.NAS, Webhook: result.Webhook}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_notification_sms",
		Title:       "Get SMS notification settings",
		Description: "Read the DSM SMS notification channel: whether it is enabled, the selected provider, recipient phone numbers, and the provider catalog. Provider credentials and send-URL templates are never returned. This tool never changes notification settings.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getNotificationInput) (*mcp.CallToolResult, getNotificationSMSOutput, error) {
		result, err := service.GetNotificationSMS(ctx, input.NAS)
		if err != nil {
			return nil, getNotificationSMSOutput{}, err
		}
		return nil, getNotificationSMSOutput{NAS: result.NAS, SMS: result.SMS}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_notification_rules",
		Title:       "Get notification event rule catalog",
		Description: "Read the DSM notification event catalog per profile: every event key with its group, severity, title, source application, and warning threshold. Useful to check which events DSM can notify about. This tool never changes notification rules.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getNotificationInput) (*mcp.CallToolResult, getNotificationRulesOutput, error) {
		result, err := service.GetNotificationRules(ctx, input.NAS)
		if err != nil {
			return nil, getNotificationRulesOutput{}, err
		}
		return nil, getNotificationRulesOutput{NAS: result.NAS, Rules: result.Rules}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_notification_desktop",
		Title:       "Get desktop notification settings",
		Description: "Read the per-category DSM desktop notification toggles of the signed-in user (which categories show desktop notifications in DSM). This tool never changes notification settings.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getNotificationInput) (*mcp.CallToolResult, getNotificationDesktopOutput, error) {
		result, err := service.GetNotificationDesktop(ctx, input.NAS)
		if err != nil {
			return nil, getNotificationDesktopOutput{}, err
		}
		return nil, getNotificationDesktopOutput{NAS: result.NAS, Desktop: result.Desktop}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_notification_history",
		Title:       "Get notification history",
		Description: "Read delivered DSM notifications (the desktop bell feed), newest first, with optional severity, time-range, and paging filters applied by DSM. Each entry carries the raw event key plus a rendered human-readable title and message, so recent storage, package, security, and system problems are directly visible. This tool never deletes or marks notifications.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getNotificationHistoryInput) (*mcp.CallToolResult, getNotificationHistoryOutput, error) {
		fromTime, err := syslog.ParseTime(input.From)
		if err != nil {
			return nil, getNotificationHistoryOutput{}, err
		}
		toTime, err := syslog.ParseTime(input.To)
		if err != nil {
			return nil, getNotificationHistoryOutput{}, err
		}
		result, err := service.GetNotificationHistory(ctx, input.NAS, notification.HistoryQuery{
			Limit: input.Limit, Offset: input.Offset, Level: input.Level,
			From: fromTime, To: toTime, Lang: input.Lang,
		})
		if err != nil {
			return nil, getNotificationHistoryOutput{}, err
		}
		return nil, getNotificationHistoryOutput{NAS: result.NAS, History: result.History}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_task_scheduler_capabilities",
		Title:       "Get Task Scheduler capabilities",
		Description: "Report which DSM Task Scheduler read areas (scheduled tasks, triggered tasks) are available for a NAS and the DSM API backend selected for each. Scheduled and triggered tasks are independent DSM API families. Read-only.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getTaskSchedulerInput) (*mcp.CallToolResult, getTaskSchedulerCapabilitiesOutput, error) {
		result, err := service.GetTaskSchedulerCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getTaskSchedulerCapabilitiesOutput{}, err
		}
		return nil, getTaskSchedulerCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_task_scheduler_tasks",
		Title:       "List scheduled tasks",
		Description: "List DSM scheduled tasks (Control Panel > Task Scheduler): for each task its id, name, normalized type, enabled state, run-as identity (flagged when privileged/root), schedule, next run time, and last-run status. Inventory metadata only: a task's command or script body is never returned by this tool. This tool never creates, edits, enables, runs, or deletes a task.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getTaskSchedulerInput) (*mcp.CallToolResult, getTaskSchedulerTasksOutput, error) {
		result, err := service.GetTaskSchedulerScheduled(ctx, input.NAS)
		if err != nil {
			return nil, getTaskSchedulerTasksOutput{}, err
		}
		return nil, getTaskSchedulerTasksOutput{NAS: result.NAS, Tasks: result.Tasks}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_task_scheduler_triggered",
		Title:       "List triggered tasks",
		Description: "List DSM triggered tasks (boot-up, shutdown, and event-triggered tasks): for each its name, trigger event, enabled state, and run-as identity (flagged when privileged/root). Inventory metadata only: a task's command or script body is never returned. This tool never creates, edits, enables, runs, or deletes a task.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getTaskSchedulerInput) (*mcp.CallToolResult, getTaskSchedulerTriggeredOutput, error) {
		result, err := service.GetTaskSchedulerTriggered(ctx, input.NAS)
		if err != nil {
			return nil, getTaskSchedulerTriggeredOutput{}, err
		}
		return nil, getTaskSchedulerTriggeredOutput{NAS: result.NAS, Tasks: result.Tasks}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_dsm_update_capabilities",
		Title:       "Get DSM Update & Restore capabilities",
		Description: "Report which DSM Update & Restore read areas (local update status, update-server offered-update check, auto-update policy, configuration backup) are available for a NAS and the DSM API backend selected for each. Each area is independent. This tool never installs a DSM update or restores a configuration.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDSMUpdateInput) (*mcp.CallToolResult, getDSMUpdateCapabilitiesOutput, error) {
		result, err := service.GetDSMUpdateCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getDSMUpdateCapabilitiesOutput{}, err
		}
		return nil, getDSMUpdateCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_dsm_update_status",
		Title:       "Get DSM update status",
		Description: "Read the installed DSM version/build and the local update state (whether an upgrade is allowed and any in-progress download/install state). Side-effect-free: this does not contact the update server, install an update, or change any setting.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDSMUpdateInput) (*mcp.CallToolResult, getDSMUpdateStatusOutput, error) {
		result, err := service.GetDSMUpdateStatus(ctx, input.NAS)
		if err != nil {
			return nil, getDSMUpdateStatusOutput{}, err
		}
		return nil, getDSMUpdateStatusOutput{NAS: result.NAS, Status: result.Status}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_dsm_update_available",
		Title:       "Check for an available DSM update",
		Description: "Check the update server for an offered DSM update and report whether one is available, plus any offered-version and restart/criticality details DSM returns. This performs a network egress to Synology's update server; if the server is unreachable, availability is reported as unknown rather than failing. It never downloads or installs an update.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDSMUpdateInput) (*mcp.CallToolResult, getDSMUpdateAvailableOutput, error) {
		result, err := service.GetDSMUpdateAvailable(ctx, input.NAS)
		if err != nil {
			return nil, getDSMUpdateAvailableOutput{}, err
		}
		return nil, getDSMUpdateAvailableOutput{NAS: result.NAS, Available: result.Available}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_dsm_update_policy",
		Title:       "Get DSM auto-update policy",
		Description: "Read the DSM auto-update policy: whether automatic update is enabled, which updates are auto-installed (such as important/security only), whether updates auto-download, the update channel, and the scheduled maintenance window. This tool never changes the policy.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDSMUpdateInput) (*mcp.CallToolResult, getDSMUpdatePolicyOutput, error) {
		result, err := service.GetDSMUpdatePolicy(ctx, input.NAS)
		if err != nil {
			return nil, getDSMUpdatePolicyOutput{}, err
		}
		return nil, getDSMUpdatePolicyOutput{NAS: result.NAS, Policy: result.Policy}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_dsm_update_config_backup",
		Title:       "Get DSM configuration-backup status",
		Description: "Read the DSM configuration-backup status: whether scheduled backup to the Synology account is enabled, the destination account and encryption mode, the last-backup result, and the stored backup history (times, DSM versions, host/model). The destination account password is never returned. This tool never runs, changes, or restores a backup.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDSMUpdateInput) (*mcp.CallToolResult, getDSMUpdateConfigBackupOutput, error) {
		result, err := service.GetDSMUpdateConfigBackup(ctx, input.NAS)
		if err != nil {
			return nil, getDSMUpdateConfigBackupOutput{}, err
		}
		return nil, getDSMUpdateConfigBackupOutput{NAS: result.NAS, ConfigBackup: result.ConfigBackup}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_resource_monitor_capabilities",
		Title:       "Get Resource Monitor capabilities",
		Description: "Report whether current utilization and recorded history can be read and whether history recording can be toggled, plus the DSM backend selected for each operation.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getResourceMonitorInput) (*mcp.CallToolResult, getResourceMonitorCapabilitiesOutput, error) {
		result, err := service.GetResourceMonitorCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getResourceMonitorCapabilitiesOutput{}, err
		}
		return nil, getResourceMonitorCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_resource_monitor_state",
		Title:       "Get current resource utilization",
		Description: "Read DSM Resource Monitor's current CPU, memory, per-interface network, aggregate and per-disk I/O, and per-volume utilization (SYNO.Core.System.Utilization). This is a volatile snapshot and never changes DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getResourceMonitorInput) (*mcp.CallToolResult, getResourceMonitorStateOutput, error) {
		result, err := service.GetResourceMonitorState(ctx, input.NAS)
		if err != nil {
			return nil, getResourceMonitorStateOutput{}, err
		}
		return nil, getResourceMonitorStateOutput{NAS: result.NAS, Utilization: result.Utilization}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_resource_monitor_history",
		Title:       "Get recorded resource history",
		Description: "Read recorded utilization history per dimension over a day/week/month/year window. Requires history recording to be enabled; if it is off, this returns an error asking to enable recording first. This tool never changes DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getResourceMonitorHistoryInput) (*mcp.CallToolResult, getResourceMonitorHistoryOutput, error) {
		result, err := service.GetResourceMonitorHistory(ctx, input.NAS, resmon.HistoryQuery{Period: input.Period, Dimensions: input.Dimensions})
		if err != nil {
			return nil, getResourceMonitorHistoryOutput{}, err
		}
		return nil, getResourceMonitorHistoryOutput{NAS: result.NAS, History: result.History}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_resource_monitor_setting",
		Title:       "Get history-recording setting",
		Description: "Read whether DSM Resource Monitor history recording is enabled (SYNO.ResourceMonitor.Setting). This tool never changes DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getResourceMonitorInput) (*mcp.CallToolResult, getResourceRecordingSettingOutput, error) {
		result, err := service.GetResourceMonitorSetting(ctx, input.NAS)
		if err != nil {
			return nil, getResourceRecordingSettingOutput{}, err
		}
		return nil, getResourceRecordingSettingOutput{NAS: result.NAS, Setting: result.Setting}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_disk_smart_capabilities",
		Title:       "Get disk SMART capabilities",
		Description: "Report which per-disk health and S.M.A.R.T. read areas (disk health/lifespan, SMART attribute tables, global warning thresholds) are available for a NAS and the DSM API backend selected for each. Each area is gated independently.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDiskSMARTInput) (*mcp.CallToolResult, getDiskSMARTCapabilitiesOutput, error) {
		result, err := service.GetDiskSMARTCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getDiskSMARTCapabilitiesOutput{}, err
		}
		return nil, getDiskSMARTCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_disk_health",
		Title:       "Get per-disk health",
		Description: "Read Storage Manager's per-physical-disk health: overall health status, SSD remaining-life/wear, spare-block/bad-sector detail, temperature, whether a SMART self-test is running, and the global disk-health warning thresholds. This complements storage inventory, which carries no per-disk lifespan or self-test detail. This tool never changes DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDiskSMARTInput) (*mcp.CallToolResult, getDiskHealthOutput, error) {
		result, err := service.GetDiskHealth(ctx, input.NAS)
		if err != nil {
			return nil, getDiskHealthOutput{}, err
		}
		return nil, getDiskHealthOutput{NAS: result.NAS, Health: result.Health}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_disk_smart_attributes",
		Title:       "Get disk SMART attributes",
		Description: "Read the full S.M.A.R.T. attribute table (id, name, current/worst/threshold/raw values, pass-fail status) for each installed disk, plus a per-disk health summary and self-test status. A disk that exposes no attribute table (many enterprise SSDs, NVMe/SATADOM/M.2, and USB devices) is reported as having no SMART data rather than erroring. This tool never starts a SMART test or changes DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDiskSMARTInput) (*mcp.CallToolResult, getDiskSMARTAttributesOutput, error) {
		result, err := service.GetDiskSMARTAttributes(ctx, input.NAS)
		if err != nil {
			return nil, getDiskSMARTAttributesOutput{}, err
		}
		return nil, getDiskSMARTAttributesOutput{NAS: result.NAS, SMART: result.SMART}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_resource_recording_change",
		Title:       "Plan a history-recording change",
		Description: "Validate a request to turn DSM Resource Monitor history recording on or off and return an approval plan bound to the observed setting. Disabling stops collecting new history but keeps already-recorded samples. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planResourceRecordingChangeInput) (*mcp.CallToolResult, planResourceRecordingChangeOutput, error) {
		plan, err := service.PlanResourceRecordingChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planResourceRecordingChangeOutput{}, err
		}
		return nil, planResourceRecordingChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_resource_recording_plan",
		Title:       "Apply an approved history-recording plan",
		Description: "Apply an unmodified recording plan only while its approval hash and the observed setting still match, then verify the setting persisted. It re-sends the whole Resource Monitor setting object so co-located settings are never reset.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyResourceRecordingPlanInput) (*mcp.CallToolResult, applyResourceRecordingPlanOutput, error) {
		result, err := service.ApplyResourceRecordingPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyResourceRecordingPlanOutput{}, err
		}
		return nil, applyResourceRecordingPlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_san_change",
		Title:       "Plan a SAN change",
		Description: "Validate a typed target, LUN, or mapping intent against current SAN and backing-volume state, then return a hash-bound plan. This tool never mutates DSM and never resolves CHAP secret references.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planSANChangeInput) (*mcp.CallToolResult, planSANChangeOutput, error) {
		plan, err := service.PlanSANChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planSANChangeOutput{}, err
		}
		return nil, planSANChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_san_plan",
		Title:       "Apply an approved SAN plan",
		Description: "Apply an unmodified SAN plan only while its approval hash, stable IDs, mapping graph, sessions, and backing-volume preconditions still match; then verify the stable-ID postcondition and return current state.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applySANPlanInput) (*mcp.CallToolResult, applySANPlanOutput, error) {
		result, err := service.ApplySANPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applySANPlanOutput{Result: result}, err
		}
		return nil, applySANPlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_share_capabilities",
		Title:       "Get shared-folder capabilities",
		Description: "Report which DSM shared-folder inventory, permission, and mutation operations dsmctl supports on the selected NAS.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getAccountInput) (*mcp.CallToolResult, getShareCapabilitiesOutput, error) {
		result, err := service.GetShareCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getShareCapabilitiesOutput{}, err
		}
		return nil, getShareCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_share_change",
		Title:       "Plan a shared-folder change",
		Description: "Validate a shared-folder create, update, delete, or permission request, read the current state, and return a hash-bound approval plan. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planShareChangeInput) (*mcp.CallToolResult, planShareChangeOutput, error) {
		plan, err := service.PlanShareChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planShareChangeOutput{}, err
		}
		return nil, planShareChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_share_plan",
		Title:       "Apply an approved shared-folder plan",
		Description: "Apply an unmodified shared-folder plan only when its approval hash and observed-state precondition still match, then verify DSM. The plan may create, modify, delete, or change access.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applySharePlanInput) (*mcp.CallToolResult, applySharePlanOutput, error) {
		result, err := service.ApplySharePlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applySharePlanOutput{}, err
		}
		return nil, applySharePlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_share_state",
		Title:       "Get shared-folder state",
		Description: "Read normalized DSM shared folders. Set include_permissions only when the user/group permission matrix is needed because it requires additional read-only DSM calls.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getShareInput) (*mcp.CallToolResult, getShareStateOutput, error) {
		result, err := service.GetShareState(ctx, input.NAS, input.IncludePermissions)
		if err != nil {
			return nil, getShareStateOutput{}, err
		}
		return nil, getShareStateOutput{NAS: result.NAS, Shares: result.Shares}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_package_capabilities",
		Title:       "Get Package Center capabilities",
		Description: "Report which Package Center operations dsmctl supports on the selected NAS and the DSM backend for each. Update-apply is deferred and always reports false.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getPackageInput) (*mcp.CallToolResult, getPackageCapabilitiesOutput, error) {
		result, err := service.GetPackageCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getPackageCapabilitiesOutput{}, err
		}
		return nil, getPackageCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_package_state",
		Title:       "Get installed-package inventory",
		Description: "Read the normalized inventory of installed DSM packages: id, display name, version, run status, running flag, beta flag, install volume, and whether each package can be started, stopped, or uninstalled. This tool never changes packages.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getPackageInput) (*mcp.CallToolResult, getPackageStateOutput, error) {
		result, err := service.GetPackageState(ctx, input.NAS)
		if err != nil {
			return nil, getPackageStateOutput{}, err
		}
		return nil, getPackageStateOutput{NAS: result.NAS, State: result.State}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_package_settings",
		Title:       "Get Package Center settings",
		Description: "Read the global Package Center configuration: publisher trust level, automatic-update state, automatic-important-only state, beta channel state, and default install volume. This tool never changes settings.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getPackageInput) (*mcp.CallToolResult, getPackageSettingsOutput, error) {
		result, err := service.GetPackageSettings(ctx, input.NAS)
		if err != nil {
			return nil, getPackageSettingsOutput{}, err
		}
		return nil, getPackageSettingsOutput{NAS: result.NAS, Settings: result.Settings}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_package_change",
		Title:       "Plan a Package Center change",
		Description: "Validate a patch-only global-settings change or a package lifecycle action (start, stop, uninstall) and return an approval plan bound to the observed settings or package state. Uninstall is refused when DSM reports the package is not removable; online installs go through plan_package_install and updates through plan_package_update instead. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planPackageChangeInput) (*mcp.CallToolResult, planPackageChangeOutput, error) {
		plan, err := service.PlanPackageChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planPackageChangeOutput{}, err
		}
		return nil, planPackageChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_package_plan",
		Title:       "Apply an approved Package Center plan",
		Description: "Apply an unmodified Package Center plan only while its approval hash and the observed settings or package state still match, then verify the postcondition. Start, stop, and uninstall verify the terminal package state; a still-transitional package returns a not-yet-confirmed error rather than a false success.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyPackagePlanInput) (*mcp.CallToolResult, applyPackagePlanOutput, error) {
		result, err := service.ApplyPackagePlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyPackagePlanOutput{}, err
		}
		return nil, applyPackagePlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_package_available",
		Title:       "List packages offered by the online package server",
		Description: "Read Synology's online package catalog for the selected NAS: identifier, name, offered version, beta flag, size, dependencies, and whether each package is already installed or has an update available. Set updates_only to list only pending updates. This tool never installs anything.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getPackageAvailableInput) (*mcp.CallToolResult, getPackageAvailableOutput, error) {
		result, err := service.GetPackageCatalog(ctx, input.NAS)
		if err != nil {
			return nil, getPackageAvailableOutput{}, err
		}
		packages := result.Catalog.Packages
		if input.UpdatesOnly {
			filtered := make([]packagecenter.AvailablePackage, 0, len(packages))
			for _, pkg := range packages {
				if pkg.UpdateAvailable {
					filtered = append(filtered, pkg)
				}
			}
			packages = filtered
		}
		return nil, getPackageAvailableOutput{NAS: result.NAS, Packages: packages}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_package_install",
		Title:       "Plan a guarded online package install",
		Description: "Resolve one package against the online catalog and the installed inventory and return a hash-bound install plan: missing dependencies are listed as ordered steps before the target, an already-installed or not-offered package is rejected, and the plan is always high risk because installing downloads and runs third-party software. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planPackageInstallInput) (*mcp.CallToolResult, planPackageInstallOutput, error) {
		runAfterInstall, quickInstall := true, true
		if input.RunAfterInstall != nil {
			runAfterInstall = *input.RunAfterInstall
		}
		if input.QuickInstall != nil {
			quickInstall = *input.QuickInstall
		}
		plan, err := service.PlanPackageInstall(ctx, input.NAS, input.PackageID, input.VolumePath, runAfterInstall, quickInstall)
		if err != nil {
			return nil, planPackageInstallOutput{}, err
		}
		return nil, planPackageInstallOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_package_update",
		Title:       "Plan a guarded package update",
		Description: "Resolve an installed package against the online catalog and return a hash-bound update plan bound to the currently installed version: new dependencies are listed as ordered steps before the target, a package that is not installed or already at the offered version is rejected, and the plan is always high risk because an update downloads and runs third-party software and cannot be downgraded. Apply it with apply_package_install_plan. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planPackageUpdateInput) (*mcp.CallToolResult, planPackageInstallOutput, error) {
		plan, err := service.PlanPackageUpdate(ctx, input.NAS, input.PackageID)
		if err != nil {
			return nil, planPackageInstallOutput{}, err
		}
		return nil, planPackageInstallOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_package_install_plan",
		Title:       "Apply an approved package install or update plan",
		Description: "Install the packages in an unmodified install or update plan (dependencies first, target last) only with its exact approval hash. An update plan is additionally rejected when the installed version no longer matches the version it was planned against. DSM downloads each package from the online server and runs it; completion is confirmed against the installed-package inventory (an update completes when the inventory reports the offered version), and large packages can take minutes per step.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyPackageInstallPlanInput) (*mcp.CallToolResult, applyPackageInstallPlanOutput, error) {
		result, err := service.ApplyPackageInstallPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyPackageInstallPlanOutput{}, err
		}
		return nil, applyPackageInstallPlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_package_local_install",
		Title:       "Plan a guarded local (manual) package install",
		Description: "Read a local .spk file on the dsmctl host and return a hash-bound install plan bound to the exact file content (byte size + SHA-256), not the online catalog. The plan is always high risk because installing uploads and runs third-party software; apply it with apply_package_local_install_plan, which refuses a .spk that changed since planning. Set allow_unsigned to install a package DSM's code-signature policy would otherwise reject. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planPackageLocalInstallInput) (*mcp.CallToolResult, planPackageLocalInstallOutput, error) {
		runAfterInstall, allowUnsigned := true, false
		if input.RunAfterInstall != nil {
			runAfterInstall = *input.RunAfterInstall
		}
		if input.AllowUnsigned != nil {
			allowUnsigned = *input.AllowUnsigned
		}
		plan, err := service.PlanPackageLocalInstall(ctx, input.NAS, input.SPKPath, input.VolumePath, runAfterInstall, allowUnsigned)
		if err != nil {
			return nil, planPackageLocalInstallOutput{}, err
		}
		return nil, planPackageLocalInstallOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_package_local_install_plan",
		Title:       "Apply an approved local (manual) package install plan",
		Description: "Upload and install the .spk from an unmodified local install plan only with its exact approval hash, and only when the file on disk still matches the size and SHA-256 the plan was bound to. DSM extracts the uploaded package, installs it (or upgrades it when the same package is already installed), and completion is confirmed against the installed-package inventory; a failed install cleans up the uploaded temp file. Installing runs third-party code and can take minutes.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyPackageLocalInstallPlanInput) (*mcp.CallToolResult, applyPackageLocalInstallPlanOutput, error) {
		result, err := service.ApplyPackageLocalInstallPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyPackageLocalInstallPlanOutput{}, err
		}
		return nil, applyPackageLocalInstallPlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_certificate_capabilities",
		Title:       "Get certificate capabilities",
		Description: "Report which DSM certificate operations dsmctl supports on the selected NAS and the backend for each. This slice is read-only; guarded certificate writes are deferred.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getCertificateInput) (*mcp.CallToolResult, getCertificateCapabilitiesOutput, error) {
		result, err := service.GetCertificateCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getCertificateCapabilitiesOutput{}, err
		}
		return nil, getCertificateCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_certificates",
		Title:       "List DSM certificates",
		Description: "List the installed DSM certificates (Control Panel > Security > Certificate): subject, issuer, SANs, key type, validity with computed days-to-expiry, whether each is the default or broken, and which DSM services and packages each certificate serves. Returns public certificate metadata only — never private-key material. This tool never changes certificates.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getCertificateInput) (*mcp.CallToolResult, getCertificatesOutput, error) {
		result, err := service.GetCertificates(ctx, input.NAS)
		if err != nil {
			return nil, getCertificatesOutput{}, err
		}
		return nil, getCertificatesOutput{NAS: result.NAS, Certificates: result.Certificates}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_terminal_snmp_capabilities",
		Title:       "Get Terminal and SNMP capabilities",
		Description: "Report whether the Terminal (SSH/Telnet) and SNMP reads are supported on the selected NAS and the DSM backend for each. Terminal and SNMP are independent: one may be unsupported without disabling the other. This slice is read-only; guarded Terminal/SNMP writes are deferred.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getTerminalSNMPInput) (*mcp.CallToolResult, getTerminalSNMPCapabilitiesOutput, error) {
		result, err := service.GetTerminalSNMPCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getTerminalSNMPCapabilitiesOutput{}, err
		}
		return nil, getTerminalSNMPCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_terminal_state",
		Title:       "Get Terminal (SSH/Telnet) state",
		Description: "Read the Control Panel > Terminal & SNMP > Terminal tab: whether SSH and Telnet are enabled, on which TCP port SSH listens, and whether local console access is forbidden. Telnet is unauthenticated cleartext and deprecated. This tool never changes the NAS.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getTerminalSNMPInput) (*mcp.CallToolResult, getTerminalStateOutput, error) {
		result, err := service.GetTerminalState(ctx, input.NAS)
		if err != nil {
			return nil, getTerminalStateOutput{}, err
		}
		return nil, getTerminalStateOutput{NAS: result.NAS, Terminal: result.Terminal}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_snmp_state",
		Title:       "Get SNMP state",
		Description: "Read the Control Panel > Terminal & SNMP > SNMP tab: whether the SNMP service is enabled, which protocol versions (v1/v2c, v3) are on, the device location and contact, the SNMPv3 username, and whether a read community and a trap target are configured. Returns non-secret configuration only — the community string, the SNMPv3 auth/privacy passwords, and any trap community are never read or returned. This tool never changes the NAS.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getTerminalSNMPInput) (*mcp.CallToolResult, getSNMPStateOutput, error) {
		result, err := service.GetSNMPState(ctx, input.NAS)
		if err != nil {
			return nil, getSNMPStateOutput{}, err
		}
		return nil, getSNMPStateOutput{NAS: result.NAS, SNMP: result.SNMP}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_terminal_change",
		Title:       "Plan a Terminal (SSH/Telnet/console) change",
		Description: "Validate a patch-only Terminal change (ssh_enabled, ssh_port, telnet_enabled, console_forbidden) and return an approval plan bound to the complete observed Terminal state. Enabling SSH or Telnet, or disabling SSH, changes the remote-shell attack surface and is classified high risk; an SSH-port change warns to verify the matching firewall/port forward separately. dsmctl drives DSM over the WebAPI session (not SSH), so its own access survives. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planTerminalChangeInput) (*mcp.CallToolResult, planTerminalChangeOutput, error) {
		plan, err := service.PlanTerminalChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planTerminalChangeOutput{}, err
		}
		return nil, planTerminalChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_terminal_plan",
		Title:       "Apply an approved Terminal plan",
		Description: "Apply an unmodified Terminal plan only while its approval hash and the complete observed state still match, then re-read to verify every requested field took effect. The write is patch-only: unspecified switches are preserved by merging into a freshly read state.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyTerminalPlanInput) (*mcp.CallToolResult, terminalSNMPApplyOutput, error) {
		result, err := service.ApplyTerminalPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, terminalSNMPApplyOutput{}, err
		}
		return nil, terminalSNMPApplyOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_snmp_change",
		Title:       "Plan an SNMP change",
		Description: "Validate a patch-only SNMP change (enabled, v1_v2c_enabled, v3_enabled, location, contact, and the read community via community_credential_ref) and return an approval plan bound to the complete observed SNMP state. The read community is a SECRET supplied as community_credential_ref (env:NAME): only the reference name enters the plan and approval hash, never the community value. Every SNMP change is medium risk. Enabling SNMPv3 is not supported (its DSM credential write wire is unverified); only disabling v3 is available. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planSNMPChangeInput) (*mcp.CallToolResult, planSNMPChangeOutput, error) {
		plan, err := service.PlanSNMPChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planSNMPChangeOutput{}, err
		}
		return nil, planSNMPChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_snmp_plan",
		Title:       "Apply an approved SNMP plan",
		Description: "Apply an unmodified SNMP plan only while its approval hash and the complete observed state still match, then re-read to verify. When the plan sets the read community, the secret is resolved from its env:NAME reference only now and rides solely the SNMP set request body — never the plan, hash, result, or logs. The write is patch-only: unspecified fields are preserved by merging into a freshly read state.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applySNMPPlanInput) (*mcp.CallToolResult, terminalSNMPApplyOutput, error) {
		result, err := service.ApplySNMPPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, terminalSNMPApplyOutput{}, err
		}
		return nil, terminalSNMPApplyOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_certificate_change",
		Title:       "Plan a certificate change",
		Description: "Validate a high-risk certificate change (import a bring-your-own bundle, set the default certificate, bind a service, or delete a certificate), read the current certificate store, and return a hash-bound approval plan. The private key is supplied by a credential reference (env:NAME) and resolved to bytes only at apply time; it never enters the plan, the hash, the result, or any log. Import parses the leaf locally and rejects an expired leaf, a broken chain, or (for a DSM-service binding) a leaf that does not cover the connection host. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planCertificateChangeInput) (*mcp.CallToolResult, planCertificateChangeOutput, error) {
		plan, err := service.PlanCertificateChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planCertificateChangeOutput{}, err
		}
		return nil, planCertificateChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_certificate_plan",
		Title:       "Apply an approved certificate plan",
		Description: "Apply an unmodified certificate plan only when its approval hash and observed-state precondition still match, then verify DSM by re-reading the certificate store. Every certificate write is high risk: replacing or deleting the certificate that serves the current dsmctl session requires acknowledge_current_session in the plan, and dsmctl re-pins to the new leaf's fingerprint for the post-apply re-read. For an import, the private key is resolved from its env:NAME reference only now, validated against the leaf, streamed as a multipart part, and zeroized; it is never returned.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyCertificatePlanInput) (*mcp.CallToolResult, applyCertificatePlanOutput, error) {
		result, err := service.ApplyCertificatePlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyCertificatePlanOutput{}, err
		}
		return nil, applyCertificatePlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_certificate_export",
		Title:       "Export a certificate archive to a local file",
		Description: "Download the archive DSM produces for a certificate to a local file on the dsmctl host. WARNING: the archive CONTAINS the private key. No key bytes are returned over MCP — only the local path and size. This tool does not change the NAS but extracts secret material, so it is stripped from the read-only remote gateway.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input exportCertificateInput) (*mcp.CallToolResult, exportCertificateOutput, error) {
		result, err := service.ExportCertificate(ctx, input.NAS, input.CertID, input.LocalPath)
		if err != nil {
			return nil, exportCertificateOutput{}, err
		}
		return nil, exportCertificateOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_security_advisor_capabilities",
		Title:       "Get Security Advisor capabilities",
		Description: "Report which Security Advisor operations dsmctl supports on the selected NAS and the backend for each. Each SYNO.Core.SecurityScan.* API is an independent boundary, so the status/findings read, the schedule/baseline read, the guarded schedule/baseline write, and the run-scan action are reported separately and a NAS without Security Advisor reports them unsupported without erroring.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getSecurityAdvisorInput) (*mcp.CallToolResult, getSecurityAdvisorCapabilitiesOutput, error) {
		result, err := service.GetSecurityAdvisorCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getSecurityAdvisorCapabilitiesOutput{}, err
		}
		return nil, getSecurityAdvisorCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_security_advisor_status",
		Title:       "Get Security Advisor scan status and findings",
		Description: "Read the Security Advisor last-scan status and findings (Control Panel > Security > Security Advisor): whether a scan is running, overall progress and severity, the last scan time, and per-category results with a per-severity breakdown (danger, risk, warning, out-of-date, info) and pass/fail counts. Severity is normalized to a stable enum and an unrecognized value errors rather than being coerced. Descriptive audit output only — no session identity or credential is ever returned. This tool never triggers a scan and never changes the NAS.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getSecurityAdvisorInput) (*mcp.CallToolResult, getSecurityAdvisorStatusOutput, error) {
		result, err := service.GetSecurityAdvisorStatus(ctx, input.NAS)
		if err != nil {
			return nil, getSecurityAdvisorStatusOutput{}, err
		}
		return nil, getSecurityAdvisorStatusOutput{NAS: result.NAS, Status: result.Status}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_security_advisor_schedule",
		Title:       "Get Security Advisor schedule and baseline",
		Description: "Read the Security Advisor scan schedule and the active security baseline (for example home or company): whether a scheduled scan is enabled, its time and weekday, and the baseline group. This tool never changes the NAS; changing the schedule or baseline goes through plan_security_advisor_schedule_change and apply_security_advisor_schedule_plan.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getSecurityAdvisorInput) (*mcp.CallToolResult, getSecurityAdvisorScheduleOutput, error) {
		result, err := service.GetSecurityAdvisorSchedule(ctx, input.NAS)
		if err != nil {
			return nil, getSecurityAdvisorScheduleOutput{}, err
		}
		return nil, getSecurityAdvisorScheduleOutput{NAS: result.NAS, Configuration: result.Configuration}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_security_advisor_schedule_change",
		Title:       "Plan a Security Advisor schedule and baseline change",
		Description: "Validate a patch-only scan schedule and security-baseline change (enable/disable the scheduled scan, its weekday and time, and switch the baseline between home and company) and return an approval plan bound to the complete observed configuration. Loosening the audit — switching from the business (company) baseline to the home baseline, or disabling the scheduled scan — is classified high risk and named in the plan summary. The custom checklist baseline is not managed here. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planSecurityAdvisorScheduleChangeInput) (*mcp.CallToolResult, planSecurityAdvisorScheduleChangeOutput, error) {
		plan, err := service.PlanSecurityAdvisorScheduleChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planSecurityAdvisorScheduleChangeOutput{}, err
		}
		return nil, planSecurityAdvisorScheduleChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_security_advisor_schedule_plan",
		Title:       "Apply an approved Security Advisor schedule plan",
		Description: "Apply an unmodified Security Advisor schedule + baseline plan only while its approval hash and the complete observed configuration still match, then re-read to verify every requested field took effect. The write is patch-only: unspecified fields are preserved. DSM rejects the change while a scan is running.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applySecurityAdvisorSchedulePlanInput) (*mcp.CallToolResult, applySecurityAdvisorSchedulePlanOutput, error) {
		result, err := service.ApplySecurityAdvisorSchedulePlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applySecurityAdvisorSchedulePlanOutput{}, err
		}
		return nil, applySecurityAdvisorSchedulePlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "run_security_scan",
		Title:       "Run a Security Advisor scan now",
		Description: "Trigger a full Security Advisor scan on demand (Control Panel > Security > Security Advisor). A scan is CPU/IO-heavy on the NAS and changes no configuration; track its progress with get_security_advisor_status. This action is never invoked implicitly by a read and is classified low risk because it changes no security posture.",
		Annotations: actionAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getSecurityAdvisorInput) (*mcp.CallToolResult, runSecurityScanOutput, error) {
		result, err := service.RunSecurityScan(ctx, input.NAS)
		if err != nil {
			return nil, runSecurityScanOutput{}, err
		}
		return nil, runSecurityScanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_account_protection_capabilities",
		Title:       "Get account-protection capabilities",
		Description: "Report which Control Panel > Security > Account reads dsmctl supports on the selected NAS (Auto Block settings, the Auto Block allow/block IP lists, Account Protection thresholds, and the enforced-2FA policy) and the backend for each. Each area is an independent boundary: one being absent leaves the others usable. Also reports whether the DoS-protection API is advertised (its read is a deferred follow-on). This slice is read-only; guarded writes are deferred.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input accountProtectionInput) (*mcp.CallToolResult, getAccountProtectionCapabilitiesOutput, error) {
		result, err := service.GetAccountProtectionCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getAccountProtectionCapabilitiesOutput{}, err
		}
		return nil, getAccountProtectionCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_account_protection_autoblock",
		Title:       "Get Auto Block settings",
		Description: "Read the DSM Auto Block configuration (Control Panel > Security > Account > Auto Block): whether it is enabled, how many failed sign-in attempts within how many minutes trigger a block, and whether/after how many days a block expires. This tool never changes settings.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input accountProtectionInput) (*mcp.CallToolResult, getAutoBlockSettingsOutput, error) {
		result, err := service.GetAutoBlockSettings(ctx, input.NAS)
		if err != nil {
			return nil, getAutoBlockSettingsOutput{}, err
		}
		return nil, getAutoBlockSettingsOutput{NAS: result.NAS, Settings: result.Settings}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_account_protection_autoblock_list",
		Title:       "Get Auto Block allow/block lists",
		Description: "Read the DSM Auto Block allow and block IP lists (the addresses always permitted and always blocked). Returns each list's entries (IP or subnet, recorded time, and DSM-reported reason) and totals. This tool never changes the lists.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input accountProtectionInput) (*mcp.CallToolResult, getAutoBlockListsOutput, error) {
		result, err := service.GetAutoBlockLists(ctx, input.NAS)
		if err != nil {
			return nil, getAutoBlockListsOutput{}, err
		}
		return nil, getAutoBlockListsOutput{NAS: result.NAS, Lists: result.Lists}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_account_protection",
		Title:       "Get Account Protection thresholds",
		Description: "Read the DSM Account Protection policy (protect accounts by blocking untrusted clients after repeated failed sign-ins): whether it is enabled and the attempt/window/block-duration thresholds for untrusted and trusted clients. This tool never changes settings and never reads any user's OTP secret.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input accountProtectionInput) (*mcp.CallToolResult, getAccountProtectionOutput, error) {
		result, err := service.GetAccountProtection(ctx, input.NAS)
		if err != nil {
			return nil, getAccountProtectionOutput{}, err
		}
		return nil, getAccountProtectionOutput{NAS: result.NAS, Protection: result.Protection}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_account_protection_enforce_2fa",
		Title:       "Get enforced-2FA policy",
		Description: "Read the domain-wide enforced-2FA/MFA policy scope (Control Panel > Security > Account). Surfaces the enforcement scope only; it never reads any user's OTP secret, seed, or recovery codes. This tool never changes the policy.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input accountProtectionInput) (*mcp.CallToolResult, getEnforceTwoFactorOutput, error) {
		result, err := service.GetEnforceTwoFactor(ctx, input.NAS)
		if err != nil {
			return nil, getEnforceTwoFactorOutput{}, err
		}
		return nil, getEnforceTwoFactorOutput{NAS: result.NAS, Policy: result.Policy}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_account_protection_autoblock_change",
		Title:       "Plan an Auto Block settings change",
		Description: "Validate a patch-only Auto Block settings change (enabled, attempts, within_minutes, expire_enabled, expire_days) and return an approval plan bound to the complete observed settings. Disabling Auto Block, raising the attempt threshold, or lengthening the detection window weakens blocking and is classified high risk; DSM binds the thresholds only when Auto Block is enabled, which the postcondition re-read enforces. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planAutoBlockChangeInput) (*mcp.CallToolResult, planAutoBlockChangeOutput, error) {
		plan, err := service.PlanAutoBlockChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planAutoBlockChangeOutput{}, err
		}
		return nil, planAutoBlockChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_account_protection_autoblock_plan",
		Title:       "Apply an approved Auto Block settings plan",
		Description: "Apply an unmodified Auto Block settings plan only while its approval hash and the complete observed settings still match, then re-read to verify every requested field took effect. The write is patch-only: unspecified fields are preserved by merging into a freshly read state.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyAutoBlockPlanInput) (*mcp.CallToolResult, accountProtectionApplyOutput, error) {
		result, err := service.ApplyAutoBlockPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, accountProtectionApplyOutput{}, err
		}
		return nil, accountProtectionApplyOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_account_protection_list_change",
		Title:       "Plan an Auto Block allow/block list edit",
		Description: "Validate a patch-only add or remove of exactly one Auto Block allow/block list entry (keyed by ip + kind) and return an approval plan bound to the complete observed lists and the currently active connections. The edit never sends a whole-list payload, so sibling entries are untouched. Blocking an active source or a broad subnet, and removing an active source from the allow list, are self-lockout risks refused without allow_lockout_override; allow-listing a broad subnet is classified high risk. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planAutoBlockListChangeInput) (*mcp.CallToolResult, planAutoBlockListChangeOutput, error) {
		plan, err := service.PlanAutoBlockListChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planAutoBlockListChangeOutput{}, err
		}
		return nil, planAutoBlockListChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_account_protection_list_plan",
		Title:       "Apply an approved Auto Block list edit plan",
		Description: "Apply an unmodified Auto Block allow/block list edit plan only while its approval hash and the complete observed state (lists plus active connections) still match, then re-read to verify the single entry was added or removed. Exactly one entry is touched; sibling entries are never reset.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyAutoBlockListPlanInput) (*mcp.CallToolResult, accountProtectionApplyOutput, error) {
		result, err := service.ApplyAutoBlockListPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, accountProtectionApplyOutput{}, err
		}
		return nil, accountProtectionApplyOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_account_protection_thresholds_change",
		Title:       "Plan an Account Protection thresholds change",
		Description: "Validate a patch-only Account Protection (SmartBlock) thresholds change (enabled plus the untrusted/trusted attempt, window, and block-duration thresholds) and return an approval plan bound to the complete observed thresholds. Disabling Account Protection, raising an attempt threshold, or lengthening a detection window weakens blocking and is classified high risk. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planAccountProtectionThresholdsChangeInput) (*mcp.CallToolResult, planAccountProtectionThresholdsChangeOutput, error) {
		plan, err := service.PlanAccountProtectionThresholdsChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planAccountProtectionThresholdsChangeOutput{}, err
		}
		return nil, planAccountProtectionThresholdsChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_account_protection_thresholds_plan",
		Title:       "Apply an approved Account Protection thresholds plan",
		Description: "Apply an unmodified Account Protection thresholds plan only while its approval hash and the complete observed thresholds still match, then re-read to verify every requested field took effect. The write is patch-only: unspecified fields are preserved by merging into a freshly read state.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyAccountProtectionThresholdsPlanInput) (*mcp.CallToolResult, accountProtectionApplyOutput, error) {
		result, err := service.ApplyAccountProtectionThresholdsPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, accountProtectionApplyOutput{}, err
		}
		return nil, accountProtectionApplyOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_account_protection_enforce_2fa_change",
		Title:       "Plan an enforced-2FA policy change",
		Description: "Validate a change to the domain-wide enforced-2FA policy scope (otp_enforce_option) and return an approval plan bound to the observed policy. Every enforced-2FA change is high risk: enabling enforcement can lock out an administrator who has not enrolled 2FA and is refused without allow_lockout_override, and disabling it weakens the posture. This sets policy only; it never enrolls a user or reads any OTP secret. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planEnforceTwoFactorChangeInput) (*mcp.CallToolResult, planEnforceTwoFactorChangeOutput, error) {
		plan, err := service.PlanEnforceTwoFactorChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planEnforceTwoFactorChangeOutput{}, err
		}
		return nil, planEnforceTwoFactorChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_account_protection_enforce_2fa_plan",
		Title:       "Apply an approved enforced-2FA policy plan",
		Description: "Apply an unmodified enforced-2FA policy plan only while its approval hash and the observed policy still match, then re-read to verify the scope took effect. This sets the enforcement policy only; it never enrolls a user or touches any OTP secret.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyEnforceTwoFactorPlanInput) (*mcp.CallToolResult, accountProtectionApplyOutput, error) {
		result, err := service.ApplyEnforceTwoFactorPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, accountProtectionApplyOutput{}, err
		}
		return nil, accountProtectionApplyOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_firewall_capabilities",
		Title:       "Get firewall capabilities",
		Description: "Report which Control Panel > Security > Firewall reads dsmctl supports on the selected NAS (the global enable flag and active profile, the profile list, the network adapters, and each profile's per-adapter policy and ordered rules) and the backend for each. Each area is an independent boundary: one being absent leaves the others usable. Note: the per-rule field decoding is wire-unverified because the DSM-shipped profiles carry no rules by default. This slice is read-only; guarded writes are a deferred follow-on.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input firewallInput) (*mcp.CallToolResult, getFirewallCapabilitiesOutput, error) {
		result, err := service.GetFirewallCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getFirewallCapabilitiesOutput{}, err
		}
		return nil, getFirewallCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_firewall_status",
		Title:       "Get firewall status",
		Description: "Read the global DSM firewall state (Control Panel > Security > Firewall): whether the firewall is enabled, which firewall profile is currently active, and the network adapters (interfaces) the firewall knows about. This tool never changes settings.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input firewallInput) (*mcp.CallToolResult, getFirewallStatusOutput, error) {
		result, err := service.GetFirewallStatus(ctx, input.NAS)
		if err != nil {
			return nil, getFirewallStatusOutput{}, err
		}
		return nil, getFirewallStatusOutput{NAS: result.NAS, Status: result.Status}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_firewall_profiles",
		Title:       "Get firewall profiles",
		Description: "Read the DSM firewall profile list (each profile is a named rule group), marking which profile is currently active. This tool never changes settings.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input firewallInput) (*mcp.CallToolResult, getFirewallProfilesOutput, error) {
		result, err := service.GetFirewallProfiles(ctx, input.NAS)
		if err != nil {
			return nil, getFirewallProfilesOutput{}, err
		}
		return nil, getFirewallProfilesOutput{NAS: result.NAS, Profiles: result.Profiles}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_firewall_rules",
		Title:       "Get firewall rules",
		Description: "Read the DSM firewall rule view: for each profile (or the one named by the profile argument), the per-adapter default (no-match) policy and the ordered rule list in DSM first-match evaluation order. Per-rule fields (action, protocol, source, ports) are wire-unverified because the DSM-shipped profiles carry no rules by default. This tool never changes settings.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input firewallRulesInput) (*mcp.CallToolResult, getFirewallRulesOutput, error) {
		result, err := service.GetFirewallRules(ctx, input.NAS, input.Profile)
		if err != nil {
			return nil, getFirewallRulesOutput{}, err
		}
		return nil, getFirewallRulesOutput{NAS: result.NAS, RuleSet: result.RuleSet}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_network_capabilities",
		Title:       "Get network capabilities",
		Description: "Report which Control Panel > Network reads dsmctl supports on the selected NAS (general settings, per-interface config, bonds, static routes, outbound proxy, and traffic-control presence) and the backend for each. Each area is an independent boundary: one being absent leaves the others usable. Some areas are wire-unverified (bond mode/members, static-route fields, per-interface IPv6) because the lab lacked a bond, static routes, and IPv6; traffic-control is capability-detected only. This slice is read-only; the connectivity-affecting writes are a deferred, guarded follow-on.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input networkInput) (*mcp.CallToolResult, getNetworkCapabilitiesOutput, error) {
		result, err := service.GetNetworkCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getNetworkCapabilitiesOutput{}, err
		}
		return nil, getNetworkCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_network_general",
		Title:       "Get network general settings",
		Description: "Read the Control Panel > Network > General settings: hostname, IPv4/IPv6 default gateway (and the interface that carries it), configured DNS nameservers (and whether DNS is DHCP-supplied or manual), and the outbound HTTP/HTTPS proxy configuration. The proxy password is never surfaced. This tool never changes settings.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input networkInput) (*mcp.CallToolResult, getNetworkGeneralOutput, error) {
		result, err := service.GetNetworkGeneral(ctx, input.NAS)
		if err != nil {
			return nil, getNetworkGeneralOutput{}, err
		}
		return nil, getNetworkGeneralOutput{NAS: result.NAS, General: result.General}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_network_interfaces",
		Title:       "Get network interfaces",
		Description: "Read each network interface's configuration and link status (Control Panel > Network > Network Interface): logical name, type, IPv4 address/netmask/gateway, DHCP-vs-static, MTU (9000 indicates jumbo frames), negotiated speed and duplex, link status, whether it carries the default gateway, VLAN, and any IPv6 assignments. This tool never changes settings.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input networkInput) (*mcp.CallToolResult, getNetworkInterfacesOutput, error) {
		result, err := service.GetNetworkInterfaces(ctx, input.NAS)
		if err != nil {
			return nil, getNetworkInterfacesOutput{}, err
		}
		return nil, getNetworkInterfacesOutput{NAS: result.NAS, Interfaces: result.Interfaces}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_network_bonds",
		Title:       "Get network bonds",
		Description: "Read the link-aggregation (bonding) interfaces: each bond's name, address, status, bonding mode, and member NICs. Note: the per-bond mode and member field names are wire-unverified because the lab had no bond to confirm them against. This tool never changes settings.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input networkInput) (*mcp.CallToolResult, getNetworkBondsOutput, error) {
		result, err := service.GetNetworkBonds(ctx, input.NAS)
		if err != nil {
			return nil, getNetworkBondsOutput{}, err
		}
		return nil, getNetworkBondsOutput{NAS: result.NAS, Bonds: result.Bonds}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_network_routes",
		Title:       "Get network static routes",
		Description: "Read the static-route table: destination network, netmask/prefix, next-hop gateway, egress interface, and address family. On a NAS without advanced routing configured DSM reports no route table (configured is false). Note: the per-route field names are wire-unverified until confirmed against a NAS with static routes. This tool never changes settings.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input networkInput) (*mcp.CallToolResult, getNetworkRoutesOutput, error) {
		result, err := service.GetNetworkRoutes(ctx, input.NAS)
		if err != nil {
			return nil, getNetworkRoutesOutput{}, err
		}
		return nil, getNetworkRoutesOutput{NAS: result.NAS, Routes: result.Routes}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_network_general_change",
		Title:       "Plan a general network change (hostname, DNS, default gateway)",
		Description: "Validate a patch-only change to the Control Panel > Network > General settings (hostname, DNS nameservers, default gateway, IPv4-first) and return an approval plan bound to the complete observed general block and the resolved management path. The management path is the NIC whose IPv4 equals the address dsmctl connects to. A default-gateway change can sever the management path and is run through the mandatory never-sever guard: the plan is REFUSED unless allow_connectivity_break is set. Hostname and DNS changes are medium risk; a default-gateway change is high risk. Omitted fields are preserved (patch-only). This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planNetworkGeneralChangeInput) (*mcp.CallToolResult, planNetworkGeneralChangeOutput, error) {
		plan, err := service.PlanNetworkGeneralChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planNetworkGeneralChangeOutput{}, err
		}
		return nil, planNetworkGeneralChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_network_general_plan",
		Title:       "Apply an approved general network plan",
		Description: "Apply an unmodified general network plan only while its approval hash and the complete observed state (the general block and the resolved management path) still match, merging the patch into a freshly read general block (patch-only), then re-read to verify the named fields took effect. The never-sever guard is re-run before the write; a default-gateway change whose result would sever the management path is refused unless the plan carried allow_connectivity_break.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyNetworkGeneralPlanInput) (*mcp.CallToolResult, networkApplyOutput, error) {
		result, err := service.ApplyNetworkGeneralPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, networkApplyOutput{}, err
		}
		return nil, networkApplyOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_network_interface_change",
		Title:       "Plan a per-interface network change (plan-only; apply is wire-unverified)",
		Description: "Validate a patch-only change to one network interface (IPv4, netmask, gateway, DHCP, MTU) and return an approval plan. The mandatory never-sever guard identifies the management NIC as the interface whose IPv4 equals the address dsmctl connects to and REFUSES any change to it (IP/netmask/DHCP/MTU) — or ANY interface change when the connection is ambiguous (a hostname/DDNS/QuickConnect/NATed path where the on-NAS egress cannot be resolved) — unless allow_connectivity_break is set; a change to a non-management NIC is permitted (medium risk). NOTE: the DSM interface-set request shape is wire-unverified (SYNO.Core.Network.Ethernet set returns code 4302 for every probed body), so the apply is REFUSED; the plan and the guard still work. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planNetworkInterfaceChangeInput) (*mcp.CallToolResult, planNetworkInterfaceChangeOutput, error) {
		plan, err := service.PlanNetworkInterfaceChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planNetworkInterfaceChangeOutput{}, err
		}
		return nil, planNetworkInterfaceChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_network_interface_plan",
		Title:       "Apply a per-interface network plan (refused: wire unverified)",
		Description: "Validate an unmodified interface plan (hash, stale-state, and never-sever guard) and then REFUSE the live write: the SYNO.Core.Network.Ethernet set request shape is wire-unverified (DSM returns code 4302 for every known body), so interface reconfiguration is plan-only in this build. This tool is registered for surface completeness; it never mutates DSM.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyNetworkInterfacePlanInput) (*mcp.CallToolResult, networkApplyOutput, error) {
		result, err := service.ApplyNetworkInterfacePlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, networkApplyOutput{}, err
		}
		return nil, networkApplyOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_firewall_profile_change",
		Title:       "Plan a firewall profile change (rules + default policy)",
		Description: "Validate a full-desired-state change to a firewall profile's adapter sections (each adapter's default no-match policy and complete ordered rule list, expressing rule create/delete/reorder) and return an approval plan bound to the complete observed state and the operator's management connection tuple. A change that would take effect — activating a profile, or editing the active profile while the firewall is enabled — is run through the mandatory never-lockout guard: the resulting ruleset is evaluated (first-match, then adapter default, deferring an adapter's 'none' default to the all-interfaces section) against {the source IP the NAS sees for the current session, the DSM port, tcp}, and the plan is REFUSED if that would not provably ALLOW the session, unless allow_connectivity_break is set. When the source cannot be read from an active connection, keep_reachable must supply it or the guard fails closed. Every effect-taking firewall change is high risk. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planFirewallProfileChangeInput) (*mcp.CallToolResult, planFirewallProfileChangeOutput, error) {
		plan, err := service.PlanFirewallProfileChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planFirewallProfileChangeOutput{}, err
		}
		return nil, planFirewallProfileChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_firewall_profile_plan",
		Title:       "Apply an approved firewall profile plan",
		Description: "Apply an unmodified firewall profile plan only while its approval hash and the complete observed state (including the management connection tuple) still match, then re-read to verify the written profile matches the desired state. The never-lockout guard is re-run before the write; a change whose resulting active ruleset would deny the current session is refused unless the plan carried allow_connectivity_break. The write is full-desired-state for the target profile; untouched adapters are preserved by merging into a freshly read profile.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyFirewallProfilePlanInput) (*mcp.CallToolResult, firewallApplyOutput, error) {
		result, err := service.ApplyFirewallProfilePlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, firewallApplyOutput{}, err
		}
		return nil, firewallApplyOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_firewall_enable_change",
		Title:       "Plan a firewall enable/disable or active-profile switch",
		Description: "Validate a change to the global firewall enable flag and/or the active profile and return an approval plan bound to the complete observed state and the operator's management connection tuple. Enabling the firewall (or switching the active profile while enabled) runs the mandatory never-lockout guard against the profile that would become active: the plan is REFUSED if the resulting ruleset would deny the operator's session and allow_connectivity_break is not set, or if the session source cannot be determined and no keep_reachable is supplied (fail closed). Enabling or switching the active profile is high risk; disabling removes all filtering, cannot lock the operator out, and is medium risk. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planFirewallEnableChangeInput) (*mcp.CallToolResult, planFirewallEnableChangeOutput, error) {
		plan, err := service.PlanFirewallEnableChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planFirewallEnableChangeOutput{}, err
		}
		return nil, planFirewallEnableChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_firewall_enable_plan",
		Title:       "Apply an approved firewall enable/disable plan",
		Description: "Apply an unmodified firewall enable/disable plan only while its approval hash and the complete observed state (including the management connection tuple) still match, then re-read to verify the enable flag and active profile took effect. When enabling, the never-lockout guard is re-run before the write and refuses a change whose resulting active ruleset would deny the current session unless the plan carried allow_connectivity_break.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyFirewallEnablePlanInput) (*mcp.CallToolResult, firewallApplyOutput, error) {
		result, err := service.ApplyFirewallEnablePlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, firewallApplyOutput{}, err
		}
		return nil, firewallApplyOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_login_portal_capabilities",
		Title:       "Get Login Portal capabilities",
		Description: "Report which Control Panel > Login Portal reads dsmctl supports on the selected NAS (the DSM web-service access settings, the customized external hostname, the per-application portal list, and the reverse-proxy rule list) and the backend for each. Each area is an independent boundary: one being absent leaves the others usable. This slice is read-only; guarded writes are deferred.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input loginPortalInput) (*mcp.CallToolResult, getLoginPortalCapabilitiesOutput, error) {
		result, err := service.GetLoginPortalCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getLoginPortalCapabilitiesOutput{}, err
		}
		return nil, getLoginPortalCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_login_portal_dsm",
		Title:       "Get DSM web-service access settings",
		Description: "Read the Control Panel > Login Portal > DSM tab settings: DSM HTTP/HTTPS ports, whether HTTPS is enabled, whether HTTP is force-redirected to HTTPS, whether HSTS and HTTP/2 are enabled, and the customized domain / external hostname. These settings decide how DSM itself is reached. This tool never changes settings.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input loginPortalInput) (*mcp.CallToolResult, getDSMWebServiceOutput, error) {
		result, err := service.GetDSMWebService(ctx, input.NAS)
		if err != nil {
			return nil, getDSMWebServiceOutput{}, err
		}
		return nil, getDSMWebServiceOutput{NAS: result.NAS, Settings: result.Settings}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_login_portal_applications",
		Title:       "Get application portals",
		Description: "Read the Control Panel > Login Portal > Applications tab: the per-application portal list, each with its application id, title, whether its portal force-redirects HTTP to HTTPS, and (when a custom portal is configured) its alias and portal ports. This tool never changes settings.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input loginPortalInput) (*mcp.CallToolResult, getApplicationPortalsOutput, error) {
		result, err := service.GetApplicationPortals(ctx, input.NAS)
		if err != nil {
			return nil, getApplicationPortalsOutput{}, err
		}
		return nil, getApplicationPortalsOutput{NAS: result.NAS, Portals: result.Portals}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_login_portal_reverse_proxy",
		Title:       "Get reverse-proxy rules",
		Description: "Read the Control Panel > Login Portal > Advanced tab: the reverse-proxy rule list. Each rule reports its id, description, frontend (source protocol/host/port) and backend (destination protocol/host/port), whether HSTS/HTTP2 are enabled, whether a certificate is referenced (presence only, never key material), and how many custom headers are configured (count only, never header values). This tool never changes settings.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input loginPortalInput) (*mcp.CallToolResult, getReverseProxyRulesOutput, error) {
		result, err := service.GetReverseProxyRules(ctx, input.NAS)
		if err != nil {
			return nil, getReverseProxyRulesOutput{}, err
		}
		return nil, getReverseProxyRulesOutput{NAS: result.NAS, Rules: result.Rules}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_login_portal_dsm_change",
		Title:       "Plan a DSM web-service change",
		Description: "Validate a patch-only DSM web-service change (http_port, https_port, https_enabled, http_redirect_enabled, hsts_enabled, http2_enabled, custom_domain_enabled, custom_domain, external_hostname) and return an approval plan bound to the complete observed settings and the transport dsmctl is connected on. Every DSM web-service change is high risk because it changes how DSM itself is reached. The never-break-the-current-session guard refuses, without allow_connectivity_break, any change that would sever the current transport (moving/disabling the current HTTPS port or scheme, forcing a redirect that bounces the current HTTP session, or enabling HSTS which browsers cache). This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planDSMWebServiceChangeInput) (*mcp.CallToolResult, planDSMWebServiceChangeOutput, error) {
		plan, err := service.PlanDSMWebServiceChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planDSMWebServiceChangeOutput{}, err
		}
		return nil, planDSMWebServiceChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_login_portal_dsm_plan",
		Title:       "Apply an approved DSM web-service plan",
		Description: "Apply an unmodified DSM web-service plan only while its approval hash and the complete observed state (settings plus the current transport) still match, then re-read to verify every requested field took effect. The write is patch-only: unspecified fields are preserved by merging into a freshly read state. A change that would sever the transport dsmctl is connected on is refused unless the plan carried allow_connectivity_break, in which case the postcondition re-read fails loudly if DSM becomes unreachable.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyDSMWebServicePlanInput) (*mcp.CallToolResult, loginPortalApplyOutput, error) {
		result, err := service.ApplyDSMWebServicePlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, loginPortalApplyOutput{}, err
		}
		return nil, loginPortalApplyOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_login_portal_application_change",
		Title:       "Plan an application-portal change",
		Description: "Validate a patch-only application-portal change (redirect_https, alias, http_port, https_port) keyed by app_id and return an approval plan bound to the observed portal. Classified medium risk: an alias or custom port changes how (and whether) the application is reached. The write is patch-only; sibling fields are preserved. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planApplicationPortalChangeInput) (*mcp.CallToolResult, planApplicationPortalChangeOutput, error) {
		plan, err := service.PlanApplicationPortalChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planApplicationPortalChangeOutput{}, err
		}
		return nil, planApplicationPortalChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_login_portal_application_plan",
		Title:       "Apply an approved application-portal plan",
		Description: "Apply an unmodified application-portal plan only while its approval hash and the observed portal still match, then re-read to verify every requested field took effect. The write is patch-only: sibling fields are preserved by merging into a freshly read portal.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyApplicationPortalPlanInput) (*mcp.CallToolResult, loginPortalApplyOutput, error) {
		result, err := service.ApplyApplicationPortalPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, loginPortalApplyOutput{}, err
		}
		return nil, loginPortalApplyOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_login_portal_reverse_proxy_create",
		Title:       "Plan a reverse-proxy rule creation",
		Description: "Validate a reverse-proxy rule to create (description, frontend, backend, and optional custom headers) and return an approval plan bound to the COMPLETE observed rule set, so a concurrent edit invalidates a stale plan. A secret header value must use credential_ref (env:NAME or vault:<id>); it is resolved only at apply time and never stored in the plan or hash. Classified medium risk: a new rule can publish an internal service to callers that reach the frontend. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planReverseProxyCreateInput) (*mcp.CallToolResult, planReverseProxyOutput, error) {
		plan, err := service.PlanReverseProxyCreate(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planReverseProxyOutput{}, err
		}
		return nil, planReverseProxyOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_login_portal_reverse_proxy_delete",
		Title:       "Plan a reverse-proxy rule deletion",
		Description: "Validate a reverse-proxy rule to delete (keyed by uuid) and return an approval plan bound to the COMPLETE observed rule set, so a concurrent create/delete/reorder by another session invalidates a stale plan. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planReverseProxyDeleteInput) (*mcp.CallToolResult, planReverseProxyOutput, error) {
		plan, err := service.PlanReverseProxyDelete(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planReverseProxyOutput{}, err
		}
		return nil, planReverseProxyOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_login_portal_reverse_proxy_plan",
		Title:       "Apply an approved reverse-proxy plan",
		Description: "Apply an unmodified reverse-proxy create or delete plan only while its approval hash and the COMPLETE observed rule set still match, then re-read to verify the rule was created (a rule now listens on the frontend) or deleted (the uuid is gone). Secret header values are resolved from their credential_ref only now, at apply time.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyReverseProxyPlanInput) (*mcp.CallToolResult, loginPortalApplyOutput, error) {
		result, err := service.ApplyReverseProxyPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, loginPortalApplyOutput{}, err
		}
		return nil, loginPortalApplyOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_drive_admin_capabilities",
		Title:       "Get Drive Admin capabilities",
		Description: "Report which Synology Drive Admin operations dsmctl supports on the selected NAS, the backend selected for each, and the installed SynologyDrive package version and running state the selection used. The installed-package inventory is re-read first, so the evidence reflects this call.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDriveAdminInput) (*mcp.CallToolResult, getDriveAdminCapabilitiesOutput, error) {
		result, err := service.GetDriveAdminCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getDriveAdminCapabilitiesOutput{}, err
		}
		return nil, getDriveAdminCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_drive_admin_status",
		Title:       "Get Drive service status",
		Description: "Read the Synology Drive service status as reported by the Drive package, plus the installed package version and running state observed immediately before the read. This tool never changes the Drive service.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDriveAdminInput) (*mcp.CallToolResult, getDriveAdminStatusOutput, error) {
		result, err := service.GetDriveAdminStatus(ctx, input.NAS)
		if err != nil {
			return nil, getDriveAdminStatusOutput{}, err
		}
		return nil, getDriveAdminStatusOutput{NAS: result.NAS, Status: result.Status}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_drive_admin_connections",
		Title:       "List Drive client connections",
		Description: "List active Synology Drive client connections (user, device, client type, address) from the Drive Admin Console. This tool never disconnects clients.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDriveAdminInput) (*mcp.CallToolResult, getDriveAdminConnectionsOutput, error) {
		result, err := service.GetDriveAdminConnections(ctx, input.NAS)
		if err != nil {
			return nil, getDriveAdminConnectionsOutput{}, err
		}
		return nil, getDriveAdminConnectionsOutput{NAS: result.NAS, Connections: result.Connections}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_drive_admin_team_folders",
		Title:       "List Drive team folders",
		Description: "List Synology Drive team folders from the admin perspective: name, enabled flag, status, share type, and — for enabled team folders — the versioning settings (kept versions, rotation policy, retention days). This tool never enables, disables, or changes team folders; use plan_drive_team_folder_change for that.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDriveAdminInput) (*mcp.CallToolResult, getDriveAdminTeamFoldersOutput, error) {
		result, err := service.GetDriveAdminTeamFolders(ctx, input.NAS)
		if err != nil {
			return nil, getDriveAdminTeamFoldersOutput{}, err
		}
		return nil, getDriveAdminTeamFoldersOutput{NAS: result.NAS, TeamFolders: result.TeamFolders}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_drive_admin_logs",
		Title:       "List Drive server logs",
		Description: "Read Synology Drive server log entries with optional Drive-applied keyword, username, team-folder, and Unix-seconds time-range filters. Entries are Drive's structured event records (numeric event code, path, client, address), newest first and bounded by limit/offset; this tool never clears logs.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDriveAdminLogsInput) (*mcp.CallToolResult, getDriveAdminLogsOutput, error) {
		result, err := service.GetDriveAdminLog(ctx, input.NAS, driveadmin.LogQuery{
			Limit: input.Limit, Offset: input.Offset, Keyword: input.Keyword, Username: input.Username,
			TeamFolder: input.TeamFolder, From: input.From, To: input.To,
		})
		if err != nil {
			return nil, getDriveAdminLogsOutput{}, err
		}
		return nil, getDriveAdminLogsOutput{NAS: result.NAS, Log: result.Log}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_drive_log_export",
		Title:       "Export Drive server logs as CSV",
		Description: "Export the Synology Drive server log as CSV text, with the same optional keyword, username, team-folder, and Unix-seconds time-range filters as the log list. Returns the whole matching log as one document (for compliance or handover); this tool never clears logs.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDriveLogExportInput) (*mcp.CallToolResult, getDriveLogExportOutput, error) {
		result, err := service.ExportDriveLog(ctx, input.NAS, synology.DriveLogExportQuery{
			TeamFolder: input.TeamFolder, Keyword: input.Keyword, Username: input.Username, From: input.From, To: input.To,
		})
		if err != nil {
			return nil, getDriveLogExportOutput{}, err
		}
		return nil, getDriveLogExportOutput{NAS: result.NAS, CSV: string(result.CSV), Bytes: result.Bytes}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_surveillance_capabilities",
		Title:       "Get Surveillance Station capabilities",
		Description: "Report whether Surveillance Station system info and the camera list can be read on the selected NAS, plus the installed package evidence.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getSurveillanceInput) (*mcp.CallToolResult, getSurveillanceCapabilitiesOutput, error) {
		result, err := service.GetSurveillanceCapabilities(ctx, input.NAS)
		if err != nil {
			return nil, getSurveillanceCapabilitiesOutput{}, err
		}
		return nil, getSurveillanceCapabilitiesOutput{NAS: result.NAS, Capabilities: result.Capabilities, Report: result.Report}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_surveillance_info",
		Title:       "Get Surveillance Station info",
		Description: "Read Surveillance Station system information (version, hostname, camera count, max cameras, license count, timezone) without changing DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getSurveillanceInput) (*mcp.CallToolResult, getSurveillanceInfoOutput, error) {
		result, err := service.GetSurveillanceInfo(ctx, input.NAS)
		if err != nil {
			return nil, getSurveillanceInfoOutput{}, err
		}
		return nil, getSurveillanceInfoOutput{NAS: result.NAS, Info: result.Info}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_surveillance_cameras",
		Title:       "List Surveillance Station cameras",
		Description: "List the cameras configured in Surveillance Station (id, name, IP, vendor, model, enabled) without changing DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getSurveillanceInput) (*mcp.CallToolResult, getSurveillanceCamerasOutput, error) {
		result, err := service.GetSurveillanceCameras(ctx, input.NAS)
		if err != nil {
			return nil, getSurveillanceCamerasOutput{}, err
		}
		return nil, getSurveillanceCamerasOutput{NAS: result.NAS, Cameras: result.Cameras}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_surveillance_home_mode",
		Title:       "Get Surveillance Home Mode",
		Description: "Read whether Surveillance Station Home Mode is currently on without changing DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getSurveillanceInput) (*mcp.CallToolResult, getSurveillanceHomeModeOutput, error) {
		result, err := service.GetSurveillanceHomeMode(ctx, input.NAS)
		if err != nil {
			return nil, getSurveillanceHomeModeOutput{}, err
		}
		return nil, getSurveillanceHomeModeOutput{NAS: result.NAS, HomeMode: result.HomeMode}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_surveillance_home_mode_change",
		Title:       "Plan a Surveillance Home Mode change",
		Description: "Validate a patch-only Home Mode request, read the current state, and return a hash-bound approval plan. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planSurveillanceHomeModeChangeInput) (*mcp.CallToolResult, planSurveillanceHomeModeChangeOutput, error) {
		plan, err := service.PlanSurveillanceHomeModeChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planSurveillanceHomeModeChangeOutput{}, err
		}
		return nil, planSurveillanceHomeModeChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_surveillance_home_mode_plan",
		Title:       "Apply an approved Surveillance Home Mode plan",
		Description: "Apply an unmodified Home Mode plan only while its approval hash and observed state still match, then verify the requested postcondition.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applySurveillanceHomeModePlanInput) (*mcp.CallToolResult, applySurveillanceHomeModePlanOutput, error) {
		result, err := service.ApplySurveillanceHomeModePlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applySurveillanceHomeModePlanOutput{}, err
		}
		return nil, applySurveillanceHomeModePlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_drive_config",
		Title:       "Get Drive server config",
		Description: "Read the Synology Drive server database configuration: the database volume (read-only), whether the database is pinned in memory (vmtouch), and the reserved memory. Never changes DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDriveAdminInput) (*mcp.CallToolResult, getDriveConfigOutput, error) {
		result, err := service.GetDriveServerConfig(ctx, input.NAS)
		if err != nil {
			return nil, getDriveConfigOutput{}, err
		}
		return nil, getDriveConfigOutput{NAS: result.NAS, Config: result.Config}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_drive_config_change",
		Title:       "Plan a Drive server config change",
		Description: "Validate one patch-only Drive server database config request (the vmtouch memory-pinning pair), read the current config, and return a hash-bound approval plan. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planDriveConfigChangeInput) (*mcp.CallToolResult, planDriveConfigChangeOutput, error) {
		plan, err := service.PlanDriveConfigChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planDriveConfigChangeOutput{}, err
		}
		return nil, planDriveConfigChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_drive_config_plan",
		Title:       "Apply an approved Drive server config plan",
		Description: "Apply an unmodified Drive server config plan only while its approval hash and complete observed config still match, then verify the requested postcondition.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyDriveConfigPlanInput) (*mcp.CallToolResult, applyDriveConfigPlanOutput, error) {
		result, err := service.ApplyDriveConfigPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyDriveConfigPlanOutput{}, err
		}
		return nil, applyDriveConfigPlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_drive_connection_summary",
		Title:       "Get Drive connection summary",
		Description: "Read the Synology Drive Admin Console overview counters: active desktop sync clients, mobile clients, ShareSync server connections, and the total. This tool never changes DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDriveAdminInput) (*mcp.CallToolResult, getDriveConnectionSummaryOutput, error) {
		result, err := service.GetDriveConnectionSummary(ctx, input.NAS)
		if err != nil {
			return nil, getDriveConnectionSummaryOutput{}, err
		}
		return nil, getDriveConnectionSummaryOutput{NAS: result.NAS, Summary: result.Summary}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_drive_db_usage",
		Title:       "Get Drive database usage",
		Description: "Read Synology Drive's cached storage breakdown: version repository size, database size, Synology Office document size (bytes), and when the cache was calculated. This tool never recalculates or changes anything.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDriveAdminInput) (*mcp.CallToolResult, getDriveDBUsageOutput, error) {
		result, err := service.GetDriveDBUsage(ctx, input.NAS)
		if err != nil {
			return nil, getDriveDBUsageOutput{}, err
		}
		return nil, getDriveDBUsageOutput{NAS: result.NAS, Usage: result.Usage}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_drive_top_files",
		Title:       "Get Drive top accessed files",
		Description: "Read the Synology Drive Admin Console ranking of most accessed files over a recent period, optionally ranked by preview or download activity only. This tool never changes DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDriveTopFilesInput) (*mcp.CallToolResult, getDriveTopFilesOutput, error) {
		result, err := service.GetDriveTopAccessFiles(ctx, input.NAS, synology.DriveTopAccessQuery{
			RankingBy: input.RankingBy, PeriodDays: input.PeriodDays, Limit: input.Limit, Offset: input.Offset,
		})
		if err != nil {
			return nil, getDriveTopFilesOutput{}, err
		}
		return nil, getDriveTopFilesOutput{NAS: result.NAS, Files: result.Files}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_drive_activation",
		Title:       "Get Drive activation state",
		Description: "Read whether the Synology Drive package has completed its online activation (registration against the NAS serial number), and when. An unactivated Drive still serves clients; activating requires the Admin Console's online activation-code exchange, which dsmctl does not perform. This tool never changes DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDriveAdminInput) (*mcp.CallToolResult, getDriveActivationOutput, error) {
		result, err := service.GetDriveActivation(ctx, input.NAS)
		if err != nil {
			return nil, getDriveActivationOutput{}, err
		}
		return nil, getDriveActivationOutput{NAS: result.NAS, Activation: result.Activation}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_drive_users",
		Title:       "List accounts with their Drive privilege",
		Description: "List accounts in one realm (local by default, or a directory domain) with whether each may use Synology Drive, plus DSM account context (deactivated accounts and disabled home service). This tool never changes privileges.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDriveUsersInput) (*mcp.CallToolResult, getDriveUsersOutput, error) {
		result, err := service.GetDrivePrivileges(ctx, input.NAS, synology.DrivePrivilegeQuery{Type: input.Type, DomainName: input.DomainName})
		if err != nil {
			return nil, getDriveUsersOutput{}, err
		}
		return nil, getDriveUsersOutput{NAS: result.NAS, Privileges: result.Privileges}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_drive_files",
		Title:       "Browse a Drive view (rescue perspective)",
		Description: "Browse one Synology Drive view — a team folder or the signed-in account's My Drive — including removed entries by default, with each node's path, size, version count, and modification time. This is the admin rescue perspective for finding deleted files; it never changes anything.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDriveFilesInput) (*mcp.CallToolResult, getDriveFilesOutput, error) {
		result, err := service.GetDriveNodes(ctx, input.NAS, synology.DriveNodeQuery{
			TeamFolder: input.TeamFolder, Pattern: input.Pattern, Recursive: input.Recursive,
			ExcludeRemoved: input.ExcludeRemoved, Limit: input.Limit, Offset: input.Offset,
		})
		if err != nil {
			return nil, getDriveFilesOutput{}, err
		}
		return nil, getDriveFilesOutput{NAS: result.NAS, Nodes: result.Nodes}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_drive_file_versions",
		Title:       "List a Drive node's version history",
		Description: "List the stored versions of one file in a Synology Drive view (team folder or My Drive): when each version was stored and modified, its size, content hash, and which client stored it. This tool never restores or changes anything.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getDriveFileVersionsInput) (*mcp.CallToolResult, getDriveFileVersionsOutput, error) {
		result, err := service.GetDriveNodeVersions(ctx, input.NAS, synology.DriveNodeVersionQuery{TeamFolder: input.TeamFolder, Path: input.Path})
		if err != nil {
			return nil, getDriveFileVersionsOutput{}, err
		}
		return nil, getDriveFileVersionsOutput{NAS: result.NAS, Versions: result.Versions}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_drive_restore",
		Title:       "Plan a Drive node restore",
		Description: "Validate restoring a set of node paths (from get_drive_files, including removed entries) in one Synology Drive view and return an approval plan bound to the resolved nodes. Recovering removed nodes is additive (medium risk); restoring in place over a currently-present file overwrites its content (high risk). Set copy_to to restore into another folder instead. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planDriveRestoreInput) (*mcp.CallToolResult, planDriveRestoreOutput, error) {
		plan, err := service.PlanDriveNodeRestore(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planDriveRestoreOutput{}, err
		}
		return nil, planDriveRestoreOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_drive_restore_plan",
		Title:       "Apply an approved Drive node restore",
		Description: "Restore the nodes in an unmodified plan only while its approval hash and the resolved node entries still match. Drive runs the restore as an asynchronous task (one at a time); dsmctl polls it to completion and verifies the requested nodes are no longer removed by re-reading the view.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyDriveRestorePlanInput) (*mcp.CallToolResult, applyDriveRestorePlanOutput, error) {
		result, err := service.ApplyDriveNodeRestorePlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyDriveRestorePlanOutput{}, err
		}
		return nil, applyDriveRestorePlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_drive_connection_kick",
		Title:       "Plan a Drive client disconnect",
		Description: "Validate a disconnect of one Synology Drive client session (by the session_id listed in get_drive_admin_connections) and return an approval plan bound to the observed connection. The client must authenticate again to resume syncing; synced files stay on the device. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planDriveConnectionKickInput) (*mcp.CallToolResult, planDriveConnectionKickOutput, error) {
		plan, err := service.PlanDriveConnectionKick(ctx, input.NAS, driveadmin.ConnectionKick{SessionID: input.SessionID})
		if err != nil {
			return nil, planDriveConnectionKickOutput{}, err
		}
		return nil, planDriveConnectionKickOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_drive_connection_kick_plan",
		Title:       "Apply an approved Drive client disconnect",
		Description: "Disconnect the client session in an unmodified kick plan only while its approval hash and the observed connection still match, then verify the session left the connection list with bounded retries.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyDriveConnectionKickPlanInput) (*mcp.CallToolResult, applyDriveConnectionKickPlanOutput, error) {
		result, err := service.ApplyDriveConnectionKickPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyDriveConnectionKickPlanOutput{}, err
		}
		return nil, applyDriveConnectionKickPlanOutput{Result: result}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_drive_team_folder_change",
		Title:       "Plan a Drive team-folder change",
		Description: "Validate one Drive team-folder change — enable a shared folder as a team folder (max_versions required; version_policy fifo or smart required while versioning is on), disable it, or patch versioning on an enabled team folder — and return an approval plan bound to the observed entry. Disabling deletes Drive's team-folder database and stored versions (files remain) and reducing versioning prunes stored versions, so those plans are high risk. This tool never mutates DSM.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input planDriveTeamFolderChangeInput) (*mcp.CallToolResult, planDriveTeamFolderChangeOutput, error) {
		plan, err := service.PlanDriveTeamFolderChange(ctx, input.NAS, input.Request)
		if err != nil {
			return nil, planDriveTeamFolderChangeOutput{}, err
		}
		return nil, planDriveTeamFolderChangeOutput{Plan: plan}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_drive_team_folder_plan",
		Title:       "Apply an approved Drive team-folder plan",
		Description: "Apply an unmodified Drive team-folder plan only while its approval hash and the observed team-folder entry still match, then verify the postcondition against a re-read of the team-folder list with bounded retries. Drive silently skips ineligible shares, so a change Drive did not take effect returns an explicit not-yet-confirmed error instead of a false success.",
		Annotations: mutationAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyDriveTeamFolderPlanInput) (*mcp.CallToolResult, applyDriveTeamFolderPlanOutput, error) {
		result, err := service.ApplyDriveTeamFolderPlan(ctx, input.Plan, input.ApprovalHash)
		if err != nil {
			return nil, applyDriveTeamFolderPlanOutput{}, err
		}
		return nil, applyDriveTeamFolderPlanOutput{Result: result}, nil
	})

	return server
}

func readOnlyAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    true,
		DestructiveHint: boolPointer(false),
		IdempotentHint:  true,
		OpenWorldHint:   boolPointer(true),
	}
}

// localReadOnlyAnnotations marks a tool that reads only local process and
// OS-credential-store state and never contacts the NAS.
func localReadOnlyAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    true,
		DestructiveHint: boolPointer(false),
		IdempotentHint:  true,
		OpenWorldHint:   boolPointer(false),
	}
}

func mutationAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    false,
		DestructiveHint: boolPointer(true),
		IdempotentHint:  false,
		OpenWorldHint:   boolPointer(true),
	}
}

// actionAnnotations marks a load-heavy action that changes no persisted
// configuration (so it is not destructive) but is not a read and is not
// idempotent: running it again starts another scan. It is still stripped from
// the read-only gateway.
func actionAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    false,
		DestructiveHint: boolPointer(false),
		IdempotentHint:  false,
		OpenWorldHint:   boolPointer(true),
	}
}

func boolPointer(value bool) *bool {
	return &value
}
