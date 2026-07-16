package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/domain/identity"
	"github.com/ychiu1211/dsmctl/internal/domain/share"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type listNASInput struct{}

type listNASOutput struct {
	NAS []config.Summary `json:"nas" jsonschema:"Configured NAS profiles"`
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

type getAccountInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getAccountStateOutput struct {
	NAS      string                 `json:"nas" jsonschema:"NAS profile used for the request"`
	Identity synology.IdentityState `json:"identity" jsonschema:"Normalized local user and group inventory"`
}

type getAccountCapabilitiesOutput struct {
	NAS          string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.IdentityCapabilities `json:"capabilities" jsonschema:"Account and group operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport  `json:"report" jsonschema:"Discovered APIs and selected identity compatibility backends"`
}

type planAccountChangeInput struct {
	NAS     string                 `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
	Request identity.ChangeRequest `json:"request" jsonschema:"User or group create, update, or delete intent; passwords must use an env:NAME credential reference"`
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
		Name:        "get_storage_capabilities",
		Title:       "Get storage capabilities",
		Description: "Report which storage inventory and mutation operations dsmctl currently supports on a selected NAS. The first milestone is read-only.",
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
		Description: "Read the normalized physical-disk, storage-pool, RAID type, volume, capacity, and health state from a selected NAS. This tool never changes storage.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getStorageInput) (*mcp.CallToolResult, getStorageStateOutput, error) {
		result, err := service.GetStorageState(ctx, input.NAS)
		if err != nil {
			return nil, getStorageStateOutput{}, err
		}
		return nil, getStorageStateOutput{NAS: result.NAS, Storage: result.Storage}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_account_capabilities",
		Title:       "Get account capabilities",
		Description: "Report which local DSM user and group inventory or mutation operations dsmctl supports on the selected NAS.",
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
		Description: "Validate a local DSM user/group create, update, or delete request, read the current state, and return a hash-bound approval plan. This tool never mutates DSM. User passwords are referenced as env:NAME and never embedded in the plan.",
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
		Description: "Apply an unmodified account plan only when its approval hash and observed-state precondition still match, then verify the resulting DSM state. The plan may create, modify, or delete an account or group.",
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
		Description: "Read normalized local DSM users and groups. Passwords, password hashes, and authentication credentials are never returned.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getAccountInput) (*mcp.CallToolResult, getAccountStateOutput, error) {
		result, err := service.GetIdentityState(ctx, input.NAS)
		if err != nil {
			return nil, getAccountStateOutput{}, err
		}
		return nil, getAccountStateOutput{NAS: result.NAS, Identity: result.Identity}, nil
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
