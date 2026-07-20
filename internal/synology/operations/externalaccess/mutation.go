package externalaccess

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/externalaccess"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// Capability names for the guarded External Access writes.
const (
	QuickConnectConfigSetCapabilityName     = "externalaccess.quickconnect.config.set"
	QuickConnectPermissionSetCapabilityName = "externalaccess.quickconnect.permission.set"
	DDNSSetCapabilityName                   = "externalaccess.ddns.set"
)

// MutationResult records the DSM backend that accepted a write.
type MutationResult struct {
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

// QuickConnectConfigSetInput carries the QuickConnect config fields to write via
// SYNO.Core.QuickConnect `set` v2. A nil pointer field is omitted from the
// request so DSM keeps its current value.
//
// NOTE: the enabled/alias/region write is NOT live-verified — the lab's alias is
// a real, globally-unique registered id that must not be changed for a test. The
// field names come from the DSM WebAPI source (webapi-QuickConnect conf); a wrong
// field fails the guarded apply's postcondition closed rather than corrupting
// state.
type QuickConnectConfigSetInput struct {
	Enabled     *bool
	ServerAlias *string
	Region      *string
}

// quickConnectConfigSetOp writes QuickConnect config via `set` on v2 (the general
// setter carrying enabled/server_alias/region).
var quickConnectConfigSetOp = compatibility.Operation[QuickConnectConfigSetInput, MutationResult]{
	Name: QuickConnectConfigSetCapabilityName,
	Variants: []compatibility.Variant[QuickConnectConfigSetInput, MutationResult]{
		{
			Name: "quickconnect-set-v2", API: QuickConnectAPI, Version: 2, Priority: 20,
			Match: compatibility.APIVersion(QuickConnectAPI, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, input QuickConnectConfigSetInput) (MutationResult, error) {
				params := map[string]any{}
				if input.Enabled != nil {
					params["enabled"] = *input.Enabled
				}
				if input.ServerAlias != nil {
					params["server_alias"] = *input.ServerAlias
				}
				if input.Region != nil {
					params["region"] = *input.Region
				}
				if len(params) == 0 {
					return MutationResult{}, fmt.Errorf("quickconnect config set: empty patch")
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: QuickConnectAPI, Version: 2, Method: "set", JSONParameters: params}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set v2: %w", QuickConnectAPI, err)
				}
				return MutationResult{}, nil
			},
		},
	},
}

// quickConnectPermissionSetOp writes the per-service external-exposure toggles
// via SYNO.Core.QuickConnect.Permission `set` v1. The services array carries
// {id, enabled} for each service being changed. This write IS live-verified: a
// per-service boolean is cleanly reversible and touches no globally-unique
// registration.
var quickConnectPermissionSetOp = compatibility.Operation[[]externalaccess.QuickConnectService, MutationResult]{
	Name: QuickConnectPermissionSetCapabilityName,
	Variants: []compatibility.Variant[[]externalaccess.QuickConnectService, MutationResult]{
		{
			Name: "quickconnect-permission-set-v1", API: QuickConnectPermAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(QuickConnectPermAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, services []externalaccess.QuickConnectService) (MutationResult, error) {
				if len(services) == 0 {
					return MutationResult{}, fmt.Errorf("quickconnect permission set: no services")
				}
				// The full desired list is passed as the slice itself: the client's
				// JSON-parameter encoder json.Marshals each value, so a slice becomes
				// the raw `[{...}]` array DSM expects. Pre-marshaling to a string would
				// double-encode it into a quoted string (DSM rejects it, code 2901 —
				// confirmed live).
				payload := make([]map[string]any, 0, len(services))
				for _, service := range services {
					payload = append(payload, map[string]any{"id": service.ID, "enabled": service.Enabled})
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: QuickConnectPermAPI, Version: 1, Method: "set", JSONParameters: map[string]any{"services": payload}}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set v1: %w", QuickConnectPermAPI, err)
				}
				return MutationResult{}, nil
			},
		},
	},
}

// DDNSRecordSetInput carries a DDNS record create/update/delete. Password is the
// plaintext resolved from a credential reference at apply time (never stored in
// the change/plan); an empty Password omits it (keeping the stored one on set).
//
// NOTE: DDNS record CRUD is NOT live-verified — the lab has no configured DDNS
// provider identity and creating a record registers a real public hostname. The
// field names come from the DSM WebAPI source (webapi-DDNS.h); a wrong field
// fails the guarded apply's postcondition closed.
type DDNSRecordSetInput struct {
	Action    externalaccess.DDNSAction
	Provider  string
	Hostname  string
	Username  string
	Password  string
	Enable    *bool
	Heartbeat *bool
	IPv6      *bool
}

var ddnsRecordSetOp = compatibility.Operation[DDNSRecordSetInput, MutationResult]{
	Name: DDNSSetCapabilityName,
	Variants: []compatibility.Variant[DDNSRecordSetInput, MutationResult]{
		{
			Name: "ddns-record-set-v1", API: DDNSRecordAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(DDNSRecordAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input DDNSRecordSetInput) (MutationResult, error) {
				method := string(input.Action)
				params := map[string]any{"provider": input.Provider, "hostname": input.Hostname}
				switch input.Action {
				case externalaccess.DDNSActionDelete:
					// Delete keys on provider + hostname; no credentials or flags.
				case externalaccess.DDNSActionCreate, externalaccess.DDNSActionUpdate:
					if input.Username != "" {
						params["username"] = input.Username
					}
					if input.Password != "" {
						params["passwd"] = input.Password
					}
					if input.Enable != nil {
						params["enable"] = *input.Enable
					}
					if input.Heartbeat != nil {
						params["heartbeat"] = *input.Heartbeat
					}
					if input.IPv6 != nil {
						params["ipv6"] = *input.IPv6
					}
				default:
					return MutationResult{}, fmt.Errorf("ddns record: unknown action %q", input.Action)
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: DDNSRecordAPI, Version: 1, Method: method, JSONParameters: params}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.%s v1: %w", DDNSRecordAPI, method, err)
				}
				return MutationResult{}, nil
			},
		},
	},
}

func SelectQuickConnectConfigSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := quickConnectConfigSetOp.Select(target)
	return selection, err
}

func SelectQuickConnectPermissionSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := quickConnectPermissionSetOp.Select(target)
	return selection, err
}

func SelectDDNSSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := ddnsRecordSetOp.Select(target)
	return selection, err
}

func ExecuteQuickConnectConfigSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input QuickConnectConfigSetInput) (MutationResult, compatibility.Selection, error) {
	result, selection, err := quickConnectConfigSetOp.Run(ctx, target, executor, input)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

func ExecuteQuickConnectPermissionSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, services []externalaccess.QuickConnectService) (MutationResult, compatibility.Selection, error) {
	result, selection, err := quickConnectPermissionSetOp.Run(ctx, target, executor, services)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

func ExecuteDDNSSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input DDNSRecordSetInput) (MutationResult, compatibility.Selection, error) {
	result, selection, err := ddnsRecordSetOp.Run(ctx, target, executor, input)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, string(input.Action)
	}
	return result, selection, err
}
