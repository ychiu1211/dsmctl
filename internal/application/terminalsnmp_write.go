package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/terminalsnmp"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const terminalSNMPAPIVersion = "dsmctl.io/v1alpha1"

// The terminal-snmp guarded writes follow the module plan/apply contract: the
// plan records and hashes the complete observed state (SNMP state carries NO
// secret), apply re-reads and rejects a changed state, merges the patch into a
// freshly read config, performs the typed set, and re-reads to verify the
// requested fields took effect.
//
// Risk model (from the work item): enabling SSH or Telnet, or disabling SSH,
// changes the human remote-shell attack surface and is HIGH. An SSH-port change,
// a console-access toggle, and every SNMP change (enable, versions, community, or
// device info) are medium. dsmctl drives DSM over the WebAPI session (not SSH),
// so its own connectivity survives any Terminal change.
//
// SECRET: the SNMP read community is supplied by community_credential_ref
// (env:NAME), resolved to bytes ONLY at apply time and passed to the client for
// the set request body alone. The reference NAME (never the secret value) is all
// that enters the plan, the approval hash, the result, or a log line.

// ---- Terminal (SSH / Telnet / console) --------------------------------------

type TerminalChangePlan struct {
	APIVersion          string                      `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                      `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                      `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             terminalsnmp.TerminalChange `json:"request" jsonschema:"Validated patch-only Terminal intent"`
	Observed            synology.TerminalState      `json:"observed" jsonschema:"Complete Terminal state observed during planning"`
	ObservedFingerprint string                      `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed state"`
	Risk                string                      `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                    `json:"warnings" jsonschema:"Remote-access exposure and lockout warnings"`
	Summary             []string                    `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                      `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

// TerminalSNMPApplyResult is the outcome of applying a Terminal or SNMP plan. It
// never carries any secret (no community string or SNMPv3 password).
type TerminalSNMPApplyResult struct {
	NAS       string                             `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash  string                             `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied   bool                               `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Operation synology.TerminalSNMPMutationResult `json:"operation" jsonschema:"Selected DSM mutation backend; carries no secret"`
}

func (s *Service) PlanTerminalChange(ctx context.Context, requestedNAS string, request terminalsnmp.TerminalChange) (TerminalChangePlan, error) {
	if err := validateTerminalShape(request); err != nil {
		return TerminalChangePlan{}, err
	}
	name, client, err := s.terminalSNMPClient(ctx, requestedNAS)
	if err != nil {
		return TerminalChangePlan{}, err
	}
	plan, err := planTerminalWithClient(ctx, name, client, request)
	if err != nil {
		return TerminalChangePlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = terminalPlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyTerminalPlan(ctx context.Context, plan TerminalChangePlan, approvalHash string) (TerminalSNMPApplyResult, error) {
	if err := validateTerminalPlan(plan, approvalHash); err != nil {
		return TerminalSNMPApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return TerminalSNMPApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return TerminalSNMPApplyResult{}, err
	}
	name, client, err := s.terminalSNMPClient(ctx, plan.NAS)
	if err != nil {
		return TerminalSNMPApplyResult{}, err
	}
	if name != plan.NAS {
		return TerminalSNMPApplyResult{}, fmt.Errorf("terminal plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyTerminalPlanWithClient(ctx, client, plan)
}

func applyTerminalPlanWithClient(ctx context.Context, client terminalSNMPClient, plan TerminalChangePlan) (TerminalSNMPApplyResult, error) {
	current, err := planTerminalWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return TerminalSNMPApplyResult{}, fmt.Errorf("terminal plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = terminalPlanHash(current)
	if err != nil {
		return TerminalSNMPApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return TerminalSNMPApplyResult{}, fmt.Errorf("terminal plan is stale; create a new plan")
	}
	operation, err := client.ApplyTerminalChange(ctx, plan.Request)
	if err != nil {
		return TerminalSNMPApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifyTerminalPostcondition(ctx, client, plan.Request); err != nil {
		return TerminalSNMPApplyResult{}, fmt.Errorf("verify terminal change: %w", err)
	}
	return TerminalSNMPApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operation: operation}, nil
}

func planTerminalWithClient(ctx context.Context, nas string, client terminalSNMPClient, request terminalsnmp.TerminalChange) (TerminalChangePlan, error) {
	if err := validateTerminalShape(request); err != nil {
		return TerminalChangePlan{}, err
	}
	capabilities, _, err := client.TerminalSNMPCapabilities(ctx)
	if err != nil {
		return TerminalChangePlan{}, authenticationError(nas, err)
	}
	if !capabilities.TerminalRead || !capabilities.TerminalWrite {
		return TerminalChangePlan{}, fmt.Errorf("NAS %q does not expose a verified Terminal read/write backend", nas)
	}
	state, err := client.TerminalState(ctx)
	if err != nil {
		return TerminalChangePlan{}, authenticationError(nas, err)
	}
	if terminalSatisfied(state, request) {
		return TerminalChangePlan{}, fmt.Errorf("terminal patch would not change the current configuration")
	}
	plan := TerminalChangePlan{APIVersion: terminalSNMPAPIVersion, NAS: nas, Request: request, Observed: state}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return TerminalChangePlan{}, err
	}
	plan.Risk, plan.Warnings, plan.Summary = terminalEffects(state, request)
	plan.Hash, err = terminalPlanHash(plan)
	if err != nil {
		return TerminalChangePlan{}, err
	}
	return plan, nil
}

func validateTerminalShape(change terminalsnmp.TerminalChange) error {
	if change.IsEmpty() {
		return fmt.Errorf("terminal patch has no fields")
	}
	if change.SSHPort != nil && (*change.SSHPort < 1 || *change.SSHPort > 65535) {
		return fmt.Errorf("ssh_port %d must be between 1 and 65535", *change.SSHPort)
	}
	return nil
}

func terminalSatisfied(state synology.TerminalState, change terminalsnmp.TerminalChange) bool {
	return (change.SSHEnabled == nil || state.SSHEnabled == *change.SSHEnabled) &&
		(change.SSHPort == nil || state.SSHPort == *change.SSHPort) &&
		(change.TelnetEnabled == nil || state.TelnetEnabled == *change.TelnetEnabled) &&
		(change.ConsoleForbidden == nil || state.ConsoleForbidden == *change.ConsoleForbidden)
}

func terminalEffects(state synology.TerminalState, change terminalsnmp.TerminalChange) (string, []string, []string) {
	warnings := []string{}
	summary := []string{}
	high := false
	if change.SSHEnabled != nil && *change.SSHEnabled != state.SSHEnabled {
		if *change.SSHEnabled {
			summary = append(summary, "enable SSH")
			warnings = append(warnings, "enabling SSH opens a remote shell on this NAS, widening the remote-management attack surface")
			high = true
		} else {
			summary = append(summary, "disable SSH")
			warnings = append(warnings, "disabling SSH removes remote-shell access and can strand any administrator relying on SSH (dsmctl itself uses the WebAPI session, not SSH, so its own access survives)")
			high = true
		}
	}
	if change.TelnetEnabled != nil && *change.TelnetEnabled != state.TelnetEnabled {
		if *change.TelnetEnabled {
			summary = append(summary, "enable Telnet (unauthenticated cleartext, deprecated)")
			warnings = append(warnings, "enabling Telnet exposes an unauthenticated cleartext remote shell (deprecated); prefer SSH")
			high = true
		} else {
			summary = append(summary, "disable Telnet")
		}
	}
	if change.SSHPort != nil && *change.SSHPort != state.SSHPort {
		summary = append(summary, fmt.Sprintf("move SSH from port %d to port %d", state.SSHPort, *change.SSHPort))
		warnings = append(warnings, fmt.Sprintf("moving SSH to port %d can break a firewall rule or upstream port forward still pinned to port %d; verify the matching firewall/port-forward change separately (out of scope here)", *change.SSHPort, state.SSHPort))
	}
	if change.ConsoleForbidden != nil && *change.ConsoleForbidden != state.ConsoleForbidden {
		if *change.ConsoleForbidden {
			summary = append(summary, "forbid local console access")
		} else {
			summary = append(summary, "allow local console access")
			warnings = append(warnings, "allowing local console access widens physical-access exposure")
		}
	}
	risk := "medium"
	if high {
		risk = "high"
	}
	return risk, warnings, summary
}

func verifyTerminalPostcondition(ctx context.Context, client terminalSNMPClient, change terminalsnmp.TerminalChange) error {
	state, err := client.TerminalState(ctx)
	if err != nil {
		return err
	}
	if change.SSHEnabled != nil && state.SSHEnabled != *change.SSHEnabled {
		return fmt.Errorf("ssh_enabled is %t, want %t", state.SSHEnabled, *change.SSHEnabled)
	}
	if change.SSHPort != nil && state.SSHPort != *change.SSHPort {
		return fmt.Errorf("ssh_port is %d, want %d", state.SSHPort, *change.SSHPort)
	}
	if change.TelnetEnabled != nil && state.TelnetEnabled != *change.TelnetEnabled {
		return fmt.Errorf("telnet_enabled is %t, want %t", state.TelnetEnabled, *change.TelnetEnabled)
	}
	if change.ConsoleForbidden != nil && state.ConsoleForbidden != *change.ConsoleForbidden {
		return fmt.Errorf("console_forbidden is %t, want %t", state.ConsoleForbidden, *change.ConsoleForbidden)
	}
	return nil
}

func validateTerminalPlan(plan TerminalChangePlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the terminal plan")
	}
	if plan.APIVersion != terminalSNMPAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid terminal plan metadata")
	}
	if err := validateTerminalShape(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("terminal plan observed state was modified")
	}
	expectedHash, err := terminalPlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("terminal plan contents were modified after planning")
	}
	return nil
}

func terminalPlanHash(plan TerminalChangePlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

// ---- SNMP -------------------------------------------------------------------

type SNMPChangePlan struct {
	APIVersion          string                  `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                  `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                  `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             terminalsnmp.SNMPChange `json:"request" jsonschema:"Validated patch-only SNMP intent. community_credential_ref carries only the env:NAME reference, never the secret"`
	Observed            synology.SNMPState      `json:"observed" jsonschema:"Complete SNMP state observed during planning; carries no community string or SNMPv3 password"`
	ObservedFingerprint string                  `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed state"`
	Risk                string                  `json:"risk" jsonschema:"Plan risk level (medium for every SNMP change)"`
	Warnings            []string                `json:"warnings" jsonschema:"Exposure warnings"`
	Summary             []string                `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                  `json:"hash" jsonschema:"SHA-256 approval hash covering intent (including the credential reference NAME) and full observed state; never any secret value"`
}

func (s *Service) PlanSNMPChange(ctx context.Context, requestedNAS string, request terminalsnmp.SNMPChange) (SNMPChangePlan, error) {
	if err := validateSNMPShape(request); err != nil {
		return SNMPChangePlan{}, err
	}
	name, client, err := s.terminalSNMPClient(ctx, requestedNAS)
	if err != nil {
		return SNMPChangePlan{}, err
	}
	plan, err := planSNMPWithClient(ctx, name, client, request)
	if err != nil {
		return SNMPChangePlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = snmpPlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplySNMPPlan(ctx context.Context, plan SNMPChangePlan, approvalHash string) (TerminalSNMPApplyResult, error) {
	if err := validateSNMPPlan(plan, approvalHash); err != nil {
		return TerminalSNMPApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return TerminalSNMPApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return TerminalSNMPApplyResult{}, err
	}
	name, client, err := s.terminalSNMPClient(ctx, plan.NAS)
	if err != nil {
		return TerminalSNMPApplyResult{}, err
	}
	if name != plan.NAS {
		return TerminalSNMPApplyResult{}, fmt.Errorf("snmp plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return s.applySNMPPlanWithClient(ctx, client, plan)
}

func (s *Service) applySNMPPlanWithClient(ctx context.Context, client terminalSNMPClient, plan SNMPChangePlan) (TerminalSNMPApplyResult, error) {
	current, err := planSNMPWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return TerminalSNMPApplyResult{}, fmt.Errorf("snmp plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = snmpPlanHash(current)
	if err != nil {
		return TerminalSNMPApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return TerminalSNMPApplyResult{}, fmt.Errorf("snmp plan is stale; create a new plan")
	}
	// Resolve the community SECRET ONLY here, at apply time. It never touched the
	// plan, the hash, or any log line; it lives in community until the set returns
	// and is zeroized immediately after.
	var community []byte
	if ref := strings.TrimSpace(plan.Request.CommunityCredentialRef); ref != "" {
		secret, resolveErr := s.secretReferences.ResolveSecret(ctx, ref)
		if resolveErr != nil {
			return TerminalSNMPApplyResult{}, fmt.Errorf("resolve SNMP community reference: %w", resolveErr)
		}
		community = []byte(secret)
		defer zeroize(community)
	}
	operation, err := client.ApplySNMPChange(ctx, plan.Request, community)
	if err != nil {
		return TerminalSNMPApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifySNMPPostcondition(ctx, client, plan.Request); err != nil {
		return TerminalSNMPApplyResult{}, fmt.Errorf("verify snmp change: %w", err)
	}
	return TerminalSNMPApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operation: operation}, nil
}

func planSNMPWithClient(ctx context.Context, nas string, client terminalSNMPClient, request terminalsnmp.SNMPChange) (SNMPChangePlan, error) {
	if err := validateSNMPShape(request); err != nil {
		return SNMPChangePlan{}, err
	}
	capabilities, _, err := client.TerminalSNMPCapabilities(ctx)
	if err != nil {
		return SNMPChangePlan{}, authenticationError(nas, err)
	}
	if !capabilities.SNMPRead || !capabilities.SNMPWrite {
		return SNMPChangePlan{}, fmt.Errorf("NAS %q does not expose a verified SNMP read/write backend", nas)
	}
	state, err := client.SNMPState(ctx)
	if err != nil {
		return SNMPChangePlan{}, authenticationError(nas, err)
	}
	if err := validateSNMPAgainstState(state, request); err != nil {
		return SNMPChangePlan{}, err
	}
	if snmpSatisfied(state, request) {
		return SNMPChangePlan{}, fmt.Errorf("snmp patch would not change the current configuration")
	}
	plan := SNMPChangePlan{APIVersion: terminalSNMPAPIVersion, NAS: nas, Request: request, Observed: state}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return SNMPChangePlan{}, err
	}
	plan.Risk, plan.Warnings, plan.Summary = snmpEffects(state, request)
	plan.Hash, err = snmpPlanHash(plan)
	if err != nil {
		return SNMPChangePlan{}, err
	}
	return plan, nil
}

func validateSNMPShape(change terminalsnmp.SNMPChange) error {
	if change.IsEmpty() {
		return fmt.Errorf("snmp patch has no fields")
	}
	// Enabling SNMPv3 is WIRE-UNVERIFIED (the v3 auth/privacy password set-field
	// names could not be confirmed live — DSM returns code 2202 for every
	// candidate). Only disabling v3 is supported.
	if change.V3Enabled != nil && *change.V3Enabled {
		return fmt.Errorf("enabling SNMPv3 is not supported: the DSM v3 auth/privacy credential write wire is unverified (WIRE-UNVERIFIED); only disabling v3 is available")
	}
	if ref := strings.TrimSpace(change.CommunityCredentialRef); ref != "" && !strings.HasPrefix(ref, "env:") {
		return fmt.Errorf("community_credential_ref must be an env:NAME reference, not a literal community string")
	}
	return nil
}

// validateSNMPAgainstState rejects, at plan time, a patch that would leave SNMPv1/v2c
// enabled with no community configured and none supplied — DSM rejects that set
// with code 2202, so it is caught before apply rather than as an opaque failure.
func validateSNMPAgainstState(state synology.SNMPState, change terminalsnmp.SNMPChange) error {
	v1v2cEnabled := state.V1V2cEnabled
	if change.V1V2cEnabled != nil {
		v1v2cEnabled = *change.V1V2cEnabled
	}
	serviceEnabled := state.Enabled
	if change.Enabled != nil {
		serviceEnabled = *change.Enabled
	}
	suppliesCommunity := strings.TrimSpace(change.CommunityCredentialRef) != ""
	if serviceEnabled && v1v2cEnabled && !state.CommunityConfigured && !suppliesCommunity {
		return fmt.Errorf("enabling SNMPv1/v2c requires a read community, and none is configured; supply community_credential_ref (env:NAME)")
	}
	return nil
}

func snmpSatisfied(state synology.SNMPState, change terminalsnmp.SNMPChange) bool {
	// Supplying a community reference is always a change intent: the current
	// community value is never read back, so it cannot be compared for equality.
	if strings.TrimSpace(change.CommunityCredentialRef) != "" {
		return false
	}
	return (change.Enabled == nil || state.Enabled == *change.Enabled) &&
		(change.V1V2cEnabled == nil || state.V1V2cEnabled == *change.V1V2cEnabled) &&
		(change.V3Enabled == nil || state.V3Enabled == *change.V3Enabled) &&
		(change.Location == nil || state.Location == *change.Location) &&
		(change.Contact == nil || state.Contact == *change.Contact)
}

func snmpEffects(state synology.SNMPState, change terminalsnmp.SNMPChange) (string, []string, []string) {
	warnings := []string{}
	summary := []string{}
	if change.Enabled != nil && *change.Enabled != state.Enabled {
		if *change.Enabled {
			summary = append(summary, "enable SNMP")
			warnings = append(warnings, "enabling SNMP opens a queryable management channel on this NAS")
		} else {
			summary = append(summary, "disable SNMP")
		}
	}
	if change.V1V2cEnabled != nil && *change.V1V2cEnabled != state.V1V2cEnabled {
		if *change.V1V2cEnabled {
			summary = append(summary, "enable SNMPv1/v2c")
		} else {
			summary = append(summary, "disable SNMPv1/v2c")
		}
	}
	if change.V3Enabled != nil && *change.V3Enabled != state.V3Enabled {
		// Only disable reaches here (enable is rejected at validation).
		summary = append(summary, "disable SNMPv3")
	}
	if strings.TrimSpace(change.CommunityCredentialRef) != "" {
		summary = append(summary, fmt.Sprintf("set the SNMP read community (secret via %s)", change.CommunityCredentialRef))
		warnings = append(warnings, "the read community is a shared secret; anyone who knows it can query this NAS over SNMP")
	}
	if change.Location != nil && *change.Location != state.Location {
		summary = append(summary, fmt.Sprintf("set device location to %q", *change.Location))
	}
	if change.Contact != nil && *change.Contact != state.Contact {
		summary = append(summary, fmt.Sprintf("set device contact to %q", *change.Contact))
	}
	// Every SNMP change is medium risk (the work item caps SNMP at medium).
	return "medium", warnings, summary
}

func verifySNMPPostcondition(ctx context.Context, client terminalSNMPClient, change terminalsnmp.SNMPChange) error {
	state, err := client.SNMPState(ctx)
	if err != nil {
		return err
	}
	if change.Enabled != nil && state.Enabled != *change.Enabled {
		return fmt.Errorf("enabled is %t, want %t", state.Enabled, *change.Enabled)
	}
	if change.V1V2cEnabled != nil && state.V1V2cEnabled != *change.V1V2cEnabled {
		return fmt.Errorf("v1_v2c_enabled is %t, want %t", state.V1V2cEnabled, *change.V1V2cEnabled)
	}
	if change.V3Enabled != nil && state.V3Enabled != *change.V3Enabled {
		return fmt.Errorf("v3_enabled is %t, want %t", state.V3Enabled, *change.V3Enabled)
	}
	// location/contact are verified only for a non-empty target: DSM silently
	// ignores an empty-string (clear) write while the service is disabled, so a
	// requested clear cannot be asserted here without a false failure.
	if change.Location != nil && *change.Location != "" && state.Location != *change.Location {
		return fmt.Errorf("location is %q, want %q", state.Location, *change.Location)
	}
	if change.Contact != nil && *change.Contact != "" && state.Contact != *change.Contact {
		return fmt.Errorf("contact is %q, want %q", state.Contact, *change.Contact)
	}
	// A supplied community must leave a community configured. The value itself is
	// never read back (secret); only its presence is verified.
	if strings.TrimSpace(change.CommunityCredentialRef) != "" && !state.CommunityConfigured {
		return fmt.Errorf("SNMP read community is not configured after apply")
	}
	return nil
}

func validateSNMPPlan(plan SNMPChangePlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the snmp plan")
	}
	if plan.APIVersion != terminalSNMPAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid snmp plan metadata")
	}
	if err := validateSNMPShape(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("snmp plan observed state was modified")
	}
	expectedHash, err := snmpPlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("snmp plan contents were modified after planning")
	}
	return nil
}

func snmpPlanHash(plan SNMPChangePlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}
