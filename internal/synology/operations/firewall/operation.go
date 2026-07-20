// Package firewall implements the read-only DSM operations for the Control Panel
// > Security > Firewall surface. Each area is a separate DSM API (a separate
// compatibility boundary) and selects its own backend per operation, so a NAS
// missing one area leaves the others usable and reports it unsupported rather than
// erroring the whole module.
//
// Live-verified on DSM 7.3 (lab):
//   - Status: SYNO.Core.Security.Firewall v1 get → {enable_firewall, profile_name}.
//   - Profiles: SYNO.Core.Security.Firewall.Profile v1 list → {profile_names}.
//   - Adapters: SYNO.Core.Security.Firewall.Adapter v1 list → {adapter_names}.
//   - Rules per profile: SYNO.Core.Security.Firewall.Profile v1 get, param
//     name=<profile> → {<adapter>:{policy, rules}, name}. The per-adapter section
//     carries the default (no-match) policy and the ordered rule list.
//
// Not shipped this pass (see comments): SYNO.Core.Security.Firewall.Adapter get
// returns error 120 (lost parameters) for every parameter name tried, so its
// required parameter could not be discovered; the per-adapter policy is already
// available through Profile get, so Adapter get is not needed for reads and is
// not shipped rather than shipping a guessed decoder.
// SYNO.Core.Security.Firewall.Rules load (param adapter=<a>) is the write-slice's
// per-adapter loader; Profile get covers the read view. Profile.Apply is the
// write path (Slice B). Both are out of scope here.
package firewall

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/firewall"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	FirewallAPIName        = "SYNO.Core.Security.Firewall"
	FirewallProfileAPIName = "SYNO.Core.Security.Firewall.Profile"
	FirewallAdapterAPIName = "SYNO.Core.Security.Firewall.Adapter"
	// FirewallRulesAPIName and FirewallProfileApplyAPIName are the write-slice
	// APIs; they are probed only for capability presence detection here.
	FirewallRulesAPIName        = "SYNO.Core.Security.Firewall.Rules"
	FirewallProfileApplyAPIName = "SYNO.Core.Security.Firewall.Profile.Apply"

	StatusReadCapabilityName   = "firewall.status.read"
	ProfilesReadCapabilityName = "firewall.profiles.read"
	AdaptersReadCapabilityName = "firewall.adapters.read"
	RulesReadCapabilityName    = "firewall.rules.read"
)

// Input is the empty input for the parameterless reads.
type Input struct{}

// ProfileInput names the profile whose per-adapter rules to load.
type ProfileInput struct {
	Profile string
}

var statusOperation = compatibility.Operation[Input, firewall.Status]{
	Name: StatusReadCapabilityName,
	Variants: []compatibility.Variant[Input, firewall.Status]{
		{
			Name: "firewall-status-get-v1", API: FirewallAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(FirewallAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (firewall.Status, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: FirewallAPIName, Version: 1, Method: "get", ReadOnly: true})
				if err != nil {
					return firewall.Status{}, fmt.Errorf("call %s.get: %w", FirewallAPIName, err)
				}
				return decodeStatus(data)
			},
		},
	},
}

var profilesOperation = compatibility.Operation[Input, []string]{
	Name: ProfilesReadCapabilityName,
	Variants: []compatibility.Variant[Input, []string]{
		{
			Name: "firewall-profiles-list-v1", API: FirewallProfileAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(FirewallProfileAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) ([]string, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: FirewallProfileAPIName, Version: 1, Method: "list", ReadOnly: true})
				if err != nil {
					return nil, fmt.Errorf("call %s.list: %w", FirewallProfileAPIName, err)
				}
				return decodeNameList(data, "profile_names", "profiles")
			},
		},
	},
}

var adaptersOperation = compatibility.Operation[Input, []string]{
	Name: AdaptersReadCapabilityName,
	Variants: []compatibility.Variant[Input, []string]{
		{
			Name: "firewall-adapters-list-v1", API: FirewallAdapterAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(FirewallAdapterAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) ([]string, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: FirewallAdapterAPIName, Version: 1, Method: "list", ReadOnly: true})
				if err != nil {
					return nil, fmt.Errorf("call %s.list: %w", FirewallAdapterAPIName, err)
				}
				return decodeNameList(data, "adapter_names", "adapters")
			},
		},
	},
}

var profileRulesOperation = compatibility.Operation[ProfileInput, firewall.ProfileRules]{
	Name: RulesReadCapabilityName,
	Variants: []compatibility.Variant[ProfileInput, firewall.ProfileRules]{
		{
			Name: "firewall-profile-rules-get-v1", API: FirewallProfileAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(FirewallProfileAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input ProfileInput) (firewall.ProfileRules, error) {
				if input.Profile == "" {
					return firewall.ProfileRules{}, fmt.Errorf("a profile name is required to load firewall rules")
				}
				data, err := executor.Execute(ctx, compatibility.Request{
					API: FirewallProfileAPIName, Version: 1, Method: "get", ReadOnly: true,
					JSONParameters: map[string]any{"name": input.Profile},
				})
				if err != nil {
					return firewall.ProfileRules{}, fmt.Errorf("call %s.get name=%s: %w", FirewallProfileAPIName, input.Profile, err)
				}
				return decodeProfileRules(data, input.Profile)
			},
		},
	},
}

// APINames lists every DSM API this module reads or probes so the facade can
// discover them in one call before selecting variants. The write-slice APIs are
// included for capability presence detection even though they are not called.
func APINames() []string {
	return []string{
		FirewallAPIName,
		FirewallProfileAPIName,
		FirewallAdapterAPIName,
		FirewallRulesAPIName,
		FirewallProfileApplyAPIName,
	}
}

// SupportsMutationAPIs reports whether the firewall write-path APIs are advertised.
// Their contract is not implemented this pass, so this only feeds capability
// reporting (the read slice never calls them).
func SupportsMutationAPIs(target compatibility.Target) bool {
	return target.SupportsAPI(FirewallRulesAPIName, 1) && target.SupportsAPI(FirewallProfileApplyAPIName, 1)
}

func SelectStatus(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := statusOperation.Select(target)
	return selection, err
}

func SelectProfiles(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := profilesOperation.Select(target)
	return selection, err
}

func SelectAdapters(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := adaptersOperation.Select(target)
	return selection, err
}

func SelectRules(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := profileRulesOperation.Select(target)
	return selection, err
}

func ExecuteStatus(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (firewall.Status, compatibility.Selection, error) {
	return statusOperation.Run(ctx, target, executor, Input{})
}

func ExecuteProfiles(ctx context.Context, target compatibility.Target, executor compatibility.Executor) ([]string, compatibility.Selection, error) {
	return profilesOperation.Run(ctx, target, executor, Input{})
}

func ExecuteAdapters(ctx context.Context, target compatibility.Target, executor compatibility.Executor) ([]string, compatibility.Selection, error) {
	return adaptersOperation.Run(ctx, target, executor, Input{})
}

func ExecuteProfileRules(ctx context.Context, target compatibility.Target, executor compatibility.Executor, profile string) (firewall.ProfileRules, compatibility.Selection, error) {
	return profileRulesOperation.Run(ctx, target, executor, ProfileInput{Profile: profile})
}

// Select returns the selection for every read area so the facade can build a
// capability report in one call. Unsupported areas carry a diagnosable reason
// rather than an error.
func Select(target compatibility.Target) []compatibility.Selection {
	selections := make([]compatibility.Selection, 0, 4)
	for _, sel := range []func(compatibility.Target) (compatibility.Selection, error){
		SelectStatus, SelectProfiles, SelectAdapters, SelectRules,
	} {
		selection, _ := sel(target)
		selections = append(selections, selection)
	}
	return selections
}
