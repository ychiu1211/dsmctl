package mcpserver

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/domain/access"
	"github.com/ychiu1211/dsmctl/internal/domain/controlpanel"
	"github.com/ychiu1211/dsmctl/internal/domain/discovery"
	"github.com/ychiu1211/dsmctl/internal/domain/driveadmin"
	"github.com/ychiu1211/dsmctl/internal/domain/ftpservices"
	"github.com/ychiu1211/dsmctl/internal/domain/identity"
	"github.com/ychiu1211/dsmctl/internal/domain/nfsexport"
	"github.com/ychiu1211/dsmctl/internal/domain/packagecenter"
	"github.com/ychiu1211/dsmctl/internal/domain/photos"
	"github.com/ychiu1211/dsmctl/internal/domain/resmon"
	"github.com/ychiu1211/dsmctl/internal/domain/rsyncservice"
	"github.com/ychiu1211/dsmctl/internal/domain/san"
	"github.com/ychiu1211/dsmctl/internal/domain/servicediscovery"
	"github.com/ychiu1211/dsmctl/internal/domain/share"
	"github.com/ychiu1211/dsmctl/internal/domain/storage"
	"github.com/ychiu1211/dsmctl/internal/domain/syslog"
	"github.com/ychiu1211/dsmctl/internal/domain/tftpservice"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type discoverLANDevicesInput struct {
	TimeoutSeconds int `json:"timeout_seconds,omitempty" jsonschema:"How long to listen for device responses, in seconds; defaults to 3 and is capped at 60"`
}

type discoverLANDevicesOutput struct {
	Devices []discovery.Device `json:"devices" jsonschema:"Synology devices that answered the findhost broadcast, deduplicated by device"`
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
	ApprovalHash string                   `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved NFS export plan"`
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
	ApprovalHash string                          `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved service discovery plan"`
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
	ApprovalHash string                     `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved FTP services plan"`
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
	ApprovalHash string                     `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved TFTP service plan"`
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
	ApprovalHash string                `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved Photos plan"`
}

type applyPhotosPlanOutput struct {
	Result application.PhotosApplyResult `json:"result" jsonschema:"Photos mutation result after stale-state and postcondition checks"`
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

type getLogsOutput struct {
	NAS  string            `json:"nas" jsonschema:"NAS profile used for the request"`
	Logs synology.LogState `json:"logs" jsonschema:"Normalized DSM system log entries and severity counts"`
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

type getDriveAdminInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
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
	ApprovalHash string                     `json:"approval_hash" jsonschema:"Exact SHA-256 hash from the approved Drive config plan"`
}

type applyDriveConfigPlanOutput struct {
	Result application.DriveConfigApplyResult `json:"result" jsonschema:"Drive config mutation result after stale-state and postcondition checks"`
}

func New(service *application.Service, version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "dsmctl", Version: version}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_nas",
		Description: "List configured Synology NAS connection profiles. Passwords are never returned.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ listNASInput) (*mcp.CallToolResult, listNASOutput, error) {
		return nil, listNASOutput{NAS: service.ListNAS()}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "discover_lan_devices",
		Title:       "Discover Synology devices on the LAN",
		Description: "Broadcast a Synology findhost discovery query on the local network and return the Synology devices that answer: hostname, model, OS version, serial, IPv4 address(es), MAC, and self-reported state. Needs no configured NAS, credential, or DSM session, and mutates nothing — it only sends discovery query packets. It sees only devices in the local broadcast domain of the host running dsmctl.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input discoverLANDevicesInput) (*mcp.CallToolResult, discoverLANDevicesOutput, error) {
		result, err := service.DiscoverDevices(ctx, discovery.Query{Timeout: time.Duration(input.TimeoutSeconds) * time.Second})
		if err != nil {
			return nil, discoverLANDevicesOutput{}, err
		}
		return nil, discoverLANDevicesOutput{Devices: result.Devices}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_auth_status",
		Title:       "Get authentication status",
		Description: "Report, per configured NAS, whether a password and DSM trusted-device credential are stored in the OS credential store, the password environment variable name and whether it is set, and whether this process holds a DSM session. Never returns secret values, never accepts passwords or OTPs, and never contacts the NAS. If authentication material is missing, ask the user to run 'dsmctl auth login' in a terminal.",
		Annotations: localReadOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getAuthStatusInput) (*mcp.CallToolResult, getAuthStatusOutput, error) {
		result, err := service.GetAuthStatus(ctx, input.NAS)
		if err != nil {
			return nil, getAuthStatusOutput{}, err
		}
		return nil, getAuthStatusOutput{Statuses: result.Statuses}, nil
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
		Description: "Report which Package Center operations dsmctl supports on the selected NAS and the DSM backend for each. Install and update are deferred and always report false.",
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
		Description: "Validate a patch-only global-settings change or a package lifecycle action (start, stop, uninstall) and return an approval plan bound to the observed settings or package state. Uninstall is refused when DSM reports the package is not removable; install and update are deferred and rejected. This tool never mutates DSM.",
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
		Name:        "get_drive_admin_capabilities",
		Title:       "Get Drive Admin capabilities",
		Description: "Report which Synology Drive Admin operations dsmctl supports on the selected NAS, the backend selected for each, and the installed SynologyDrive package version and running state the selection used. The installed-package inventory is re-read first, so the evidence reflects this call. Team-folder changes are deferred and always report false.",
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
		Description: "List Synology Drive team folders from the admin perspective with their reported status. This tool never enables, disables, or changes team folders.",
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

func boolPointer(value bool) *bool {
	return &value
}
