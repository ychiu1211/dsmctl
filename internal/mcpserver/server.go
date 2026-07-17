package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/domain/access"
	"github.com/ychiu1211/dsmctl/internal/domain/controlpanel"
	"github.com/ychiu1211/dsmctl/internal/domain/identity"
	"github.com/ychiu1211/dsmctl/internal/domain/san"
	"github.com/ychiu1211/dsmctl/internal/domain/share"
	"github.com/ychiu1211/dsmctl/internal/domain/storage"
	"github.com/ychiu1211/dsmctl/internal/domain/syslog"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

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

func New(service *application.Service, version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "dsmctl", Version: version}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_nas",
		Description: "List configured Synology NAS connection profiles. Passwords are never returned.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ listNASInput) (*mcp.CallToolResult, listNASOutput, error) {
		return nil, listNASOutput{NAS: service.ListNAS()}, nil
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
