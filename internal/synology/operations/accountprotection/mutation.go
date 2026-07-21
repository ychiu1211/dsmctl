package accountprotection

import (
	"context"
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/accountprotection"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// The write wire is live-verified on the DSM 7.3 lab (throwaway raw probes, each
// mutation reverted to baseline). The confirmed set/rule shapes are:
//
//	SYNO.Core.Security.AutoBlock        set    v1  {enable(bool), attempts, within_mins, expire_day}
//	SYNO.Core.Security.AutoBlock.Rules  create v1  {type: "deny"|"allow", ip}   (add one entry)
//	SYNO.Core.Security.AutoBlock.Rules  delete v1  {type: "deny"|"allow", ip}   (remove one entry)
//	SYNO.Core.SmartBlock                set    v1  {enabled(bool), untrust_try/minute/lock, trust_try/minute/lock}
//	SYNO.Core.OTP.EnforcePolicy         set    v1  {otp_enforce_option}
//
// Corrections captured against the stale spec/source guesses:
//   - the AutoBlock/SmartBlock write field names mirror the get names exactly
//     (get/set symmetry), but AutoBlock only *binds* the attempt/window/expiry
//     thresholds when enable is true — a threshold change requested while
//     disabled is silently ignored, which the postcondition re-read catches.
//   - the allow/block list add method is "create" and the remove method is
//     "delete" (spec guessed add/remove); both are keyed by {type, ip} and touch
//     exactly one entry, so a whole-list payload is never sent.
//   - SmartBlock's enable flag is "enabled" (not "enable"); AutoBlock's is
//     "enable".
//
// SYNO.Core.Security.DoS is advertised (maxVersion 2) but neither its get nor its
// set parameter shape was discoverable this pass (get returns code 114 "lost
// parameters" for every interface-parameter name tried, and the exact name was
// not discoverable without codesearch/webman JS). Its write is left
// capability-only and WIRE-UNVERIFIED; no DoS mutation is exposed.
const (
	AutoBlockWriteCapabilityName         = "account_protection.autoblock.write"
	AutoBlockListWriteCapabilityName     = "account_protection.autoblock_list.write"
	AccountProtectionWriteCapabilityName = "account_protection.protection.write"
	EnforceTwoFactorWriteCapabilityName  = "account_protection.enforce_2fa.write"

	// ActiveConnectionsAPIName lists currently connected clients; it feeds the
	// self-lockout guardrail (protect active sources) and is read best-effort.
	ActiveConnectionsAPIName = "SYNO.Core.CurrentConnection"

	rulesCreateMethod = "create"
	rulesDeleteMethod = "delete"
)

// MutationResult records the DSM backend that accepted an account-protection
// write.
type MutationResult struct {
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

var autoBlockSetOperation = compatibility.Operation[accountprotection.AutoBlockSettings, MutationResult]{
	Name: AutoBlockWriteCapabilityName,
	Variants: []compatibility.Variant[accountprotection.AutoBlockSettings, MutationResult]{
		{
			Name: "account-protection-autoblock-set-v1", API: AutoBlockAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(AutoBlockAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, desired accountprotection.AutoBlockSettings) (MutationResult, error) {
				params := map[string]any{
					"enable":      desired.Enabled,
					"attempts":    desired.Attempts,
					"within_mins": desired.WithinMinutes,
					"expire_day":  desired.ExpireDays,
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: AutoBlockAPIName, Version: 1, Method: "set", JSONParameters: params}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set: %w", AutoBlockAPIName, err)
				}
				return MutationResult{}, nil
			},
		},
	},
}

var accountProtectionSetOperation = compatibility.Operation[accountprotection.AccountProtection, MutationResult]{
	Name: AccountProtectionWriteCapabilityName,
	Variants: []compatibility.Variant[accountprotection.AccountProtection, MutationResult]{
		{
			Name: "account-protection-smartblock-set-v1", API: SmartBlockAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(SmartBlockAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, desired accountprotection.AccountProtection) (MutationResult, error) {
				params := map[string]any{
					"enabled":        desired.Enabled,
					"untrust_try":    desired.UntrustedAttempts,
					"untrust_minute": desired.UntrustedWithinMinutes,
					"untrust_lock":   desired.UntrustedBlockMinutes,
					"trust_try":      desired.TrustedAttempts,
					"trust_minute":   desired.TrustedWithinMinutes,
					"trust_lock":     desired.TrustedBlockMinutes,
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: SmartBlockAPIName, Version: 1, Method: "set", JSONParameters: params}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set: %w", SmartBlockAPIName, err)
				}
				return MutationResult{}, nil
			},
		},
	},
}

var enforceTwoFactorSetOperation = compatibility.Operation[accountprotection.EnforceTwoFactor, MutationResult]{
	Name: EnforceTwoFactorWriteCapabilityName,
	Variants: []compatibility.Variant[accountprotection.EnforceTwoFactor, MutationResult]{
		{
			Name: "account-protection-enforce-2fa-set-v1", API: EnforcePolicyAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(EnforcePolicyAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, desired accountprotection.EnforceTwoFactor) (MutationResult, error) {
				option := strings.TrimSpace(desired.Option)
				if option == "" {
					return MutationResult{}, fmt.Errorf("enforce 2fa set requires an otp_enforce_option")
				}
				params := map[string]any{"otp_enforce_option": option}
				if _, err := executor.Execute(ctx, compatibility.Request{API: EnforcePolicyAPIName, Version: 1, Method: "set", JSONParameters: params}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set: %w", EnforcePolicyAPIName, err)
				}
				return MutationResult{}, nil
			},
		},
	},
}

var ipListEditOperation = compatibility.Operation[accountprotection.IPListEdit, MutationResult]{
	Name: AutoBlockListWriteCapabilityName,
	Variants: []compatibility.Variant[accountprotection.IPListEdit, MutationResult]{
		{
			Name: "account-protection-autoblock-rules-edit-v1", API: AutoBlockRulesAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(AutoBlockRulesAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, edit accountprotection.IPListEdit) (MutationResult, error) {
				dsmType, err := listType(edit.Kind)
				if err != nil {
					return MutationResult{}, err
				}
				ip := strings.TrimSpace(edit.IP)
				if ip == "" {
					return MutationResult{}, fmt.Errorf("auto block list edit requires an ip")
				}
				method := rulesCreateMethod
				if edit.Remove {
					method = rulesDeleteMethod
				}
				params := map[string]any{"type": dsmType, "ip": ip}
				if _, err := executor.Execute(ctx, compatibility.Request{API: AutoBlockRulesAPIName, Version: 1, Method: method, JSONParameters: params}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.%s: %w", AutoBlockRulesAPIName, method, err)
				}
				return MutationResult{Method: method}, nil
			},
		},
	},
}

// listType maps the domain kind (allow/block) onto the DSM discriminator
// (allow/deny) used by the Rules API.
func listType(kind string) (string, error) {
	switch kind {
	case accountprotection.KindAllow:
		return "allow", nil
	case accountprotection.KindBlock:
		return "deny", nil
	default:
		return "", fmt.Errorf("auto block list edit rejects unknown kind %q", kind)
	}
}

func SelectAutoBlockSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := autoBlockSetOperation.Select(target)
	return selection, err
}

func SelectAccountProtectionSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := accountProtectionSetOperation.Select(target)
	return selection, err
}

func SelectEnforceTwoFactorSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := enforceTwoFactorSetOperation.Select(target)
	return selection, err
}

func SelectIPListEdit(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := ipListEditOperation.Select(target)
	return selection, err
}

// ExecuteAutoBlockSet submits the complete desired Auto Block settings. The
// caller merges its patch into the freshly read state first so an unspecified
// field can never be silently reset by DSM.
func ExecuteAutoBlockSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, desired accountprotection.AutoBlockSettings) (MutationResult, compatibility.Selection, error) {
	result, selection, err := autoBlockSetOperation.Run(ctx, target, executor, desired)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

// ExecuteAccountProtectionSet submits the complete desired Account Protection
// thresholds.
func ExecuteAccountProtectionSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, desired accountprotection.AccountProtection) (MutationResult, compatibility.Selection, error) {
	result, selection, err := accountProtectionSetOperation.Run(ctx, target, executor, desired)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

// ExecuteEnforceTwoFactorSet submits the desired enforced-2FA policy scope.
func ExecuteEnforceTwoFactorSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, desired accountprotection.EnforceTwoFactor) (MutationResult, compatibility.Selection, error) {
	result, selection, err := enforceTwoFactorSetOperation.Run(ctx, target, executor, desired)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

// ExecuteIPListEdit adds or removes exactly one allow/block list entry.
func ExecuteIPListEdit(ctx context.Context, target compatibility.Target, executor compatibility.Executor, edit accountprotection.IPListEdit) (MutationResult, compatibility.Selection, error) {
	result, selection, err := ipListEditOperation.Run(ctx, target, executor, edit)
	if err == nil {
		result.Backend, result.API, result.Version = selection.Backend, selection.API, selection.Version
	}
	return result, selection, err
}

// ExecuteActiveConnections lists currently connected clients so the self-lockout
// guardrail can protect active sources. It is best-effort: a NAS without the API
// (or a transient failure) yields no protected sources rather than an error.
func ExecuteActiveConnections(ctx context.Context, target compatibility.Target, executor compatibility.Executor) ([]accountprotection.ActiveConnection, error) {
	if !target.SupportsAPI(ActiveConnectionsAPIName, 1) {
		return nil, nil
	}
	data, err := executor.Execute(ctx, compatibility.Request{API: ActiveConnectionsAPIName, Version: 1, Method: "list"})
	if err != nil {
		return nil, nil
	}
	connections, err := decodeActiveConnections(data)
	if err != nil {
		return nil, nil
	}
	return connections, nil
}
