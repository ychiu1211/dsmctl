package firewall

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/firewall"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// The firewall write wire is live-verified on the DSM 7.3 lab (build 81168),
// cross-checked against the DSM admin_center UI JS and confirmed with a
// firewall-disabled, self-reverting probe:
//
//	SYNO.Core.Security.Firewall.Profile        set    v1  {profile:<full profile object>, profile_applying:bool}
//	SYNO.Core.Security.Firewall.Profile.Apply  start  v1  {name:<profile>, profile_applying:bool} -> {task_id}
//	SYNO.Core.Security.Firewall.Profile.Apply  status v1  {task_id} (poll until success)
//	SYNO.Core.Security.Firewall                set    v1  {set_type:"disable"}
//
// Ownership model (confirmed): Profile.set replaces the WHOLE profile object — the
// per-adapter default policy plus the complete ordered rule list — so rule
// create/delete/reorder and default-policy change are all expressed as full
// desired state for the target profile. profile_applying=false saves without
// activating; true saves and activates (enables the firewall with that profile).
// The rule object shape is confirmed live: {enable, name, policy, protocol,
// port_direction, port_group, ports, source_ip_group, source_ip, log}.
//
// Enabling the firewall / switching the active profile = Profile.Apply.start;
// disabling = Firewall.set set_type=disable. Firewall.Rules exposes only "load"
// (per-adapter read helper); it has no write method (every write method probed
// returned code 103), so it is never a write path.
const (
	ProfileWriteCapabilityName = "firewall.profile.write"
	EnableWriteCapabilityName  = "firewall.enable.write"

	// CurrentConnectionAPIName feeds the self-lockout guard with the operator's
	// active source IP. It is read best-effort and carries no session secret.
	CurrentConnectionAPIName = "SYNO.Core.CurrentConnection"

	firewallSetType = "set_type"
	profileApplying = "profile_applying"
)

// MutationResult records the DSM backend that accepted a firewall write.
type MutationResult struct {
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

// ProfileSetInput is a full-profile-replacement write plus the apply flag.
type ProfileSetInput struct {
	Profile  firewall.ProfileRules
	Activate bool
}

// EnableInput is a firewall enable/disable write. When Enabled it activates
// Profile via Profile.Apply.start; when not, it disables via Firewall.set.
type EnableInput struct {
	Enabled bool
	Profile string
}

var profileSetOperation = compatibility.Operation[ProfileSetInput, MutationResult]{
	Name: ProfileWriteCapabilityName,
	Variants: []compatibility.Variant[ProfileSetInput, MutationResult]{
		{
			Name: "firewall-profile-set-v1", API: FirewallProfileAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(FirewallProfileAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input ProfileSetInput) (MutationResult, error) {
				if input.Profile.Profile == "" {
					return MutationResult{}, fmt.Errorf("firewall profile set requires a profile name")
				}
				params := map[string]any{
					"profile":       encodeProfile(input.Profile),
					profileApplying: input.Activate,
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: FirewallProfileAPIName, Version: 1, Method: "set", JSONParameters: params}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set: %w", FirewallProfileAPIName, err)
				}
				return MutationResult{Method: "set"}, nil
			},
		},
	},
}

var enableOperation = compatibility.Operation[EnableInput, MutationResult]{
	Name: EnableWriteCapabilityName,
	Variants: []compatibility.Variant[EnableInput, MutationResult]{
		{
			Name: "firewall-enable-v1", API: FirewallAPIName, Version: 1, Priority: 10,
			// Enable goes through Profile.Apply.start, disable through Firewall.set;
			// both APIs are required for this capability.
			Match: compatibility.All(
				compatibility.APIVersion(FirewallAPIName, 1),
				compatibility.APIVersion(FirewallProfileApplyAPIName, 1),
			),
			Execute: func(ctx context.Context, executor compatibility.Executor, input EnableInput) (MutationResult, error) {
				if !input.Enabled {
					params := map[string]any{firewallSetType: "disable"}
					if _, err := executor.Execute(ctx, compatibility.Request{API: FirewallAPIName, Version: 1, Method: "set", JSONParameters: params}); err != nil {
						return MutationResult{}, fmt.Errorf("call %s.set(disable): %w", FirewallAPIName, err)
					}
					return MutationResult{API: FirewallAPIName, Method: "set"}, nil
				}
				if input.Profile == "" {
					return MutationResult{}, fmt.Errorf("enabling the firewall requires an active profile name")
				}
				params := map[string]any{"name": input.Profile, profileApplying: false}
				if _, err := executor.Execute(ctx, compatibility.Request{API: FirewallProfileApplyAPIName, Version: 1, Method: "start", JSONParameters: params}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.start: %w", FirewallProfileApplyAPIName, err)
				}
				return MutationResult{API: FirewallProfileApplyAPIName, Method: "start"}, nil
			},
		},
	},
}

func SelectProfileSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := profileSetOperation.Select(target)
	return selection, err
}

func SelectEnable(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := enableOperation.Select(target)
	return selection, err
}

// ExecuteProfileSet writes the complete desired profile. The caller merges its
// intent into a freshly read profile first so untouched adapters are preserved.
func ExecuteProfileSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input ProfileSetInput) (MutationResult, compatibility.Selection, error) {
	result, selection, err := profileSetOperation.Run(ctx, target, executor, input)
	if err == nil {
		result.Backend, result.API, result.Version = selection.Backend, selection.API, selection.Version
	}
	return result, selection, err
}

// ExecuteEnable enables (Profile.Apply.start) or disables (Firewall.set) the
// firewall.
func ExecuteEnable(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input EnableInput) (MutationResult, compatibility.Selection, error) {
	result, selection, err := enableOperation.Run(ctx, target, executor, input)
	if err == nil {
		result.Backend, result.Version = selection.Backend, selection.Version
	}
	return result, selection, err
}

// ExecuteCurrentConnection lists active connections so the self-lockout guard can
// determine the operator's management source IP. Best-effort: a NAS without the
// API yields no sources rather than an error, and it never reads a session secret.
func ExecuteCurrentConnection(ctx context.Context, target compatibility.Target, executor compatibility.Executor) ([]firewall.SessionSource, error) {
	if !target.SupportsAPI(CurrentConnectionAPIName, 1) {
		return nil, nil
	}
	data, err := executor.Execute(ctx, compatibility.Request{API: CurrentConnectionAPIName, Version: 1, Method: "list", ReadOnly: true})
	if err != nil {
		return nil, nil
	}
	sources, err := decodeCurrentConnection(data)
	if err != nil {
		return nil, nil
	}
	return sources, nil
}
