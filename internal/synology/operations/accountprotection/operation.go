// Package accountprotection implements the read-only DSM operations for the
// Control Panel > Security > Account surface. Each area is a separate DSM API
// (a separate compatibility boundary) and selects its own backend per operation,
// so a NAS missing one area leaves the others usable and reports it unsupported
// rather than erroring the whole module.
//
// Live-verified on DSM 7.3 (lab). The actual API/field names corrected several
// stale spec guesses:
//   - Auto Block settings: SYNO.Core.Security.AutoBlock v1 get →
//     {enable, attempts, within_mins, expire_day} (spec guessed enabled/
//     within_minutes/expire_days plus a separate expire flag).
//   - Allow/block list: SYNO.Core.Security.AutoBlock.Rules v1 (plural "Rules",
//     not "Rule") list, requiring a type=deny|allow discriminator →
//     {ip_info, offset, total}.
//   - Account Protection: SYNO.Core.SmartBlock v1 get (DSM's internal name),
//     {enabled, untrust_try/minute/lock, trust_try/minute/lock}.
//   - Enforced 2FA: SYNO.Core.OTP.EnforcePolicy v1 get → {otp_enforce_option}.
//
// DoS protection (SYNO.Core.Security.DoS) is advertised on the lab but its read
// contract could not be live-verified this pass (get returns "lost parameters"
// for every interface-parameter name tried, and the exact name was not
// discoverable without codesearch/webman JS); its read is a deferred follow-on.
// Capability reporting still surfaces whether the DoS API is present.
package accountprotection

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/accountprotection"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	AutoBlockAPIName      = "SYNO.Core.Security.AutoBlock"
	AutoBlockRulesAPIName = "SYNO.Core.Security.AutoBlock.Rules"
	SmartBlockAPIName     = "SYNO.Core.SmartBlock"
	EnforcePolicyAPIName  = "SYNO.Core.OTP.EnforcePolicy"
	// DoSAPIName is advertised on DSM 7.3 but its read contract is unverified
	// this pass; it is used only for capability presence detection.
	DoSAPIName = "SYNO.Core.Security.DoS"

	AutoBlockReadCapabilityName         = "account_protection.autoblock.read"
	AutoBlockListReadCapabilityName     = "account_protection.autoblock_list.read"
	AccountProtectionReadCapabilityName = "account_protection.protection.read"
	EnforceTwoFactorReadCapabilityName  = "account_protection.enforce_2fa.read"

	// listPageLimit is a generous single-page fetch for the allow/block lists.
	// The lab lists are empty; paging past this is a follow-on concern.
	listPageLimit = 1000
)

// Input is the empty input for the parameterless reads.
type Input struct{}

var autoBlockOperation = compatibility.Operation[Input, accountprotection.AutoBlockSettings]{
	Name: AutoBlockReadCapabilityName,
	Variants: []compatibility.Variant[Input, accountprotection.AutoBlockSettings]{
		{
			Name: "account-protection-autoblock-get-v1", API: AutoBlockAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(AutoBlockAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (accountprotection.AutoBlockSettings, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: AutoBlockAPIName, Version: 1, Method: "get"})
				if err != nil {
					return accountprotection.AutoBlockSettings{}, fmt.Errorf("call %s.get: %w", AutoBlockAPIName, err)
				}
				return decodeAutoBlockSettings(data)
			},
		},
	},
}

var autoBlockListOperation = compatibility.Operation[Input, accountprotection.AutoBlockLists]{
	Name: AutoBlockListReadCapabilityName,
	Variants: []compatibility.Variant[Input, accountprotection.AutoBlockLists]{
		{
			Name: "account-protection-autoblock-rules-list-v1", API: AutoBlockRulesAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(AutoBlockRulesAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (accountprotection.AutoBlockLists, error) {
				allow, err := listIPRules(ctx, executor, "allow")
				if err != nil {
					return accountprotection.AutoBlockLists{}, err
				}
				block, err := listIPRules(ctx, executor, "deny")
				if err != nil {
					return accountprotection.AutoBlockLists{}, err
				}
				return accountprotection.AutoBlockLists{Allow: allow, Block: block}, nil
			},
		},
	},
}

// listIPRules reads one side of the allow/block list. DSM requires the type
// discriminator ("deny" for the block list, "allow" for the allow list); the
// domain kind is normalized to allow/block.
func listIPRules(ctx context.Context, executor compatibility.Executor, dsmType string) (accountprotection.IPList, error) {
	kind := "block"
	if dsmType == "allow" {
		kind = "allow"
	}
	data, err := executor.Execute(ctx, compatibility.Request{
		API: AutoBlockRulesAPIName, Version: 1, Method: "list",
		JSONParameters: map[string]any{
			"type":       dsmType,
			"offset":     0,
			"limit":      listPageLimit,
			"additional": []string{"reason", "record_time"},
		},
	})
	if err != nil {
		return accountprotection.IPList{}, fmt.Errorf("call %s.list type=%s: %w", AutoBlockRulesAPIName, dsmType, err)
	}
	return decodeIPList(data, kind)
}

var accountProtectionOperation = compatibility.Operation[Input, accountprotection.AccountProtection]{
	Name: AccountProtectionReadCapabilityName,
	Variants: []compatibility.Variant[Input, accountprotection.AccountProtection]{
		{
			Name: "account-protection-smartblock-get-v1", API: SmartBlockAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(SmartBlockAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (accountprotection.AccountProtection, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: SmartBlockAPIName, Version: 1, Method: "get"})
				if err != nil {
					return accountprotection.AccountProtection{}, fmt.Errorf("call %s.get: %w", SmartBlockAPIName, err)
				}
				return decodeAccountProtection(data)
			},
		},
	},
}

var enforceTwoFactorOperation = compatibility.Operation[Input, accountprotection.EnforceTwoFactor]{
	Name: EnforceTwoFactorReadCapabilityName,
	Variants: []compatibility.Variant[Input, accountprotection.EnforceTwoFactor]{
		{
			Name: "account-protection-enforce-2fa-get-v1", API: EnforcePolicyAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(EnforcePolicyAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (accountprotection.EnforceTwoFactor, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: EnforcePolicyAPIName, Version: 1, Method: "get"})
				if err != nil {
					return accountprotection.EnforceTwoFactor{}, fmt.Errorf("call %s.get: %w", EnforcePolicyAPIName, err)
				}
				return decodeEnforceTwoFactor(data)
			},
		},
	},
}

// APINames lists every DSM API this module reads or probes so the facade can
// discover them in one call before selecting variants. DoS is included for
// capability presence detection even though its read is deferred.
func APINames() []string {
	return []string{
		AutoBlockAPIName,
		AutoBlockRulesAPIName,
		SmartBlockAPIName,
		EnforcePolicyAPIName,
		DoSAPIName,
	}
}

// SupportsDoS reports whether the DoS-protection API is advertised. The DoS read
// contract is unverified this pass, so this only feeds capability reporting.
func SupportsDoS(target compatibility.Target) bool {
	return target.SupportsAPI(DoSAPIName, 1)
}

func SelectAutoBlock(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := autoBlockOperation.Select(target)
	return selection, err
}

func SelectAutoBlockList(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := autoBlockListOperation.Select(target)
	return selection, err
}

func SelectAccountProtection(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := accountProtectionOperation.Select(target)
	return selection, err
}

func SelectEnforceTwoFactor(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := enforceTwoFactorOperation.Select(target)
	return selection, err
}

func ExecuteAutoBlock(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (accountprotection.AutoBlockSettings, compatibility.Selection, error) {
	return autoBlockOperation.Run(ctx, target, executor, Input{})
}

func ExecuteAutoBlockList(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (accountprotection.AutoBlockLists, compatibility.Selection, error) {
	return autoBlockListOperation.Run(ctx, target, executor, Input{})
}

func ExecuteAccountProtection(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (accountprotection.AccountProtection, compatibility.Selection, error) {
	return accountProtectionOperation.Run(ctx, target, executor, Input{})
}

func ExecuteEnforceTwoFactor(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (accountprotection.EnforceTwoFactor, compatibility.Selection, error) {
	return enforceTwoFactorOperation.Run(ctx, target, executor, Input{})
}

// Select returns the selection for every read area so the facade can build a
// capability report in one call. Unsupported areas carry a diagnosable reason
// rather than an error.
func Select(target compatibility.Target) []compatibility.Selection {
	selections := make([]compatibility.Selection, 0, 4)
	for _, sel := range []func(compatibility.Target) (compatibility.Selection, error){
		SelectAutoBlock, SelectAutoBlockList, SelectAccountProtection, SelectEnforceTwoFactor,
	} {
		selection, _ := sel(target)
		selections = append(selections, selection)
	}
	return selections
}
