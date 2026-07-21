package application

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/network"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const networkAPIVersion = "dsmctl.io/v1alpha1"

// The network guarded writes follow the module plan/apply contract: the plan
// records and hashes the complete observed state of the affected area (the full
// general block, or the full interface list) plus the resolved management path
// (the {management NIC name, its IP, the DSM port} the current transport rides),
// apply re-reads and rejects a changed state, merges the patch into a freshly
// read config, performs the typed write, and re-reads to verify.
//
// The defining safeguard is the never-sever-the-management-NIC guard. dsmctl
// reaches DSM at a fixed transport address; the management NIC is the one bearing
// that address (network.ResolveManagementPath). Any change to that NIC, or to the
// default gateway, or ANY interface change when the connection is ambiguous, is
// refused unless the intent carries allow_connectivity_break. Everything here is
// high risk when it touches the management path; a non-management interface change
// or a hostname/DNS change is medium.
//
// Interface writes are plan+guard ONLY in this build: the SYNO.Core.Network.Ethernet
// set request shape is wire-unverified (DSM returns code 4302 for every probed
// body), so the live apply is refused. The general write (SYNO.Core.Network set)
// is live-confirmed and fully applyable.

// NetworkApplyResult is returned by the network applies.
type NetworkApplyResult struct {
	NAS       string                        `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash  string                        `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied   bool                          `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Operation synology.NetworkMutationResult `json:"operation" jsonschema:"Selected DSM mutation backend"`
}

// resolveManagementPath reads the interfaces and general block and resolves which
// NIC carries the current transport.
func resolveManagementPath(ctx context.Context, client networkClient, general synology.NetworkGeneral) (synology.NetworkManagementPath, []synology.NetworkInterface, error) {
	interfaces, err := client.NetworkInterfaces(ctx)
	if err != nil {
		return synology.NetworkManagementPath{}, nil, err
	}
	transport := client.NetworkTransportInfo()
	path := network.ResolveManagementPath(transport, interfaces, general.DefaultGatewayV4)
	return path, interfaces, nil
}

// ---- general change ---------------------------------------------------------

type NetworkGeneralObserved struct {
	General          synology.NetworkGeneral       `json:"general" jsonschema:"Complete observed general block hashed into the plan"`
	ManagementPath   synology.NetworkManagementPath `json:"management_path" jsonschema:"Resolved management path: the NIC carrying the transport, the default gateway, and whether the connection is ambiguous"`
	ProtectedSources []string                      `json:"protected_sources" jsonschema:"Source IPs of all active connections at plan time"`
}

type NetworkGeneralPlan struct {
	APIVersion          string                       `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                       `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                       `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             network.GeneralChange        `json:"request" jsonschema:"Validated patch-only general change intent"`
	Observed            NetworkGeneralObserved       `json:"observed" jsonschema:"Complete observed state hashed into the plan"`
	ObservedFingerprint string                       `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed state"`
	Resulting           synology.NetworkGeneral      `json:"resulting" jsonschema:"The merged general block that will be written (the guard evaluated this)"`
	Guard               synology.NetworkGuardVerdict `json:"guard" jsonschema:"Never-sever guard decision"`
	Risk                string                       `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                     `json:"warnings" jsonschema:"Connectivity and posture warnings"`
	Summary             []string                     `json:"summary" jsonschema:"Human-readable operations"`
	Hash                string                       `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

func (s *Service) PlanNetworkGeneralChange(ctx context.Context, requestedNAS string, request network.GeneralChange) (NetworkGeneralPlan, error) {
	if err := validateGeneralChange(request); err != nil {
		return NetworkGeneralPlan{}, err
	}
	name, client, err := s.networkClient(ctx, requestedNAS)
	if err != nil {
		return NetworkGeneralPlan{}, err
	}
	plan, err := planNetworkGeneralWithClient(ctx, name, client, request)
	if err != nil {
		return NetworkGeneralPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = networkGeneralPlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyNetworkGeneralPlan(ctx context.Context, plan NetworkGeneralPlan, approvalHash string) (NetworkApplyResult, error) {
	if err := validateNetworkGeneralPlan(plan, approvalHash); err != nil {
		return NetworkApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return NetworkApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return NetworkApplyResult{}, err
	}
	name, client, err := s.networkClient(ctx, plan.NAS)
	if err != nil {
		return NetworkApplyResult{}, err
	}
	if name != plan.NAS {
		return NetworkApplyResult{}, fmt.Errorf("network general plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyNetworkGeneralWithClient(ctx, client, plan)
}

func planNetworkGeneralWithClient(ctx context.Context, nas string, client networkClient, request network.GeneralChange) (NetworkGeneralPlan, error) {
	capabilities, _, err := client.NetworkCapabilities(ctx)
	if err != nil {
		return NetworkGeneralPlan{}, authenticationError(nas, err)
	}
	if !capabilities.GeneralWrite {
		return NetworkGeneralPlan{}, fmt.Errorf("NAS %q does not expose a verified network general write backend", nas)
	}
	general, err := client.NetworkGeneralFresh(ctx)
	if err != nil {
		return NetworkGeneralPlan{}, authenticationError(nas, err)
	}
	if network.GeneralChangeIsNoop(general, request) {
		return NetworkGeneralPlan{}, fmt.Errorf("network general change would not change the current configuration")
	}
	path, _, err := resolveManagementPath(ctx, client, general)
	if err != nil {
		return NetworkGeneralPlan{}, authenticationError(nas, err)
	}
	verdict := network.EvaluateGeneralChange(path, general, request)
	if !verdict.Allowed {
		return NetworkGeneralPlan{}, fmt.Errorf(
			"the never-sever guard refuses this change: %s; re-plan with allow_connectivity_break set (and an out-of-band recovery path ready) to proceed", verdict.Reason)
	}
	resulting := network.MergeGeneral(general, request)
	plan := NetworkGeneralPlan{
		APIVersion: networkAPIVersion,
		NAS:        nas,
		Request:    request,
		Observed:   NetworkGeneralObserved{General: general, ManagementPath: path, ProtectedSources: client.NetworkCurrentSources(ctx)},
		Resulting:  resulting,
		Guard:      verdict,
	}
	plan.Risk, plan.Warnings, plan.Summary = networkGeneralEffects(general, request, verdict)
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return NetworkGeneralPlan{}, err
	}
	plan.Hash, err = networkGeneralPlanHash(plan)
	if err != nil {
		return NetworkGeneralPlan{}, err
	}
	return plan, nil
}

func applyNetworkGeneralWithClient(ctx context.Context, client networkClient, plan NetworkGeneralPlan) (NetworkApplyResult, error) {
	current, err := planNetworkGeneralWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return NetworkApplyResult{}, fmt.Errorf("network general plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = networkGeneralPlanHash(current)
	if err != nil {
		return NetworkApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return NetworkApplyResult{}, fmt.Errorf("network general plan is stale; create a new plan")
	}
	operation, err := client.ApplyNetworkGeneralChange(ctx, plan.Request)
	if err != nil {
		return NetworkApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifyNetworkGeneralPostcondition(ctx, client, plan); err != nil {
		return NetworkApplyResult{}, fmt.Errorf("verify network general change: %w", err)
	}
	return NetworkApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operation: operation}, nil
}

// verifyNetworkGeneralPostcondition re-reads the general block and confirms the
// fields the caller named actually took effect (DSM silently ignores some).
func verifyNetworkGeneralPostcondition(ctx context.Context, client networkClient, plan NetworkGeneralPlan) error {
	general, err := client.NetworkGeneralFresh(ctx)
	if err != nil {
		return err
	}
	req := plan.Request
	if req.Hostname != nil && !strings.EqualFold(general.Hostname, plan.Resulting.Hostname) {
		return fmt.Errorf("hostname is %q, want %q (DSM may have rejected the field); re-read and re-plan", general.Hostname, plan.Resulting.Hostname)
	}
	if req.DefaultGatewayV4 != nil && general.DefaultGatewayV4 != plan.Resulting.DefaultGatewayV4 {
		return fmt.Errorf("default gateway is %q, want %q; re-read and re-plan", general.DefaultGatewayV4, plan.Resulting.DefaultGatewayV4)
	}
	if req.DNSPrimary != nil && general.DNSPrimary != plan.Resulting.DNSPrimary {
		return fmt.Errorf("primary DNS is %q, want %q; re-read and re-plan", general.DNSPrimary, plan.Resulting.DNSPrimary)
	}
	if req.DNSSecondary != nil && general.DNSSecondary != plan.Resulting.DNSSecondary {
		return fmt.Errorf("secondary DNS is %q, want %q; re-read and re-plan", general.DNSSecondary, plan.Resulting.DNSSecondary)
	}
	return nil
}

func networkGeneralEffects(current synology.NetworkGeneral, request network.GeneralChange, verdict synology.NetworkGuardVerdict) (string, []string, []string) {
	summary := network.GeneralChangeFields(current, request)
	if len(summary) == 0 {
		summary = []string{"no general changes"}
	}
	warnings := []string{}
	risk := "medium"
	if verdict.Protected {
		risk = "high"
		if verdict.Overridden {
			warnings = append(warnings, fmt.Sprintf("allow_connectivity_break acknowledged: %s; an out-of-band recovery path is required", verdict.Reason))
		}
	}
	return risk, warnings, summary
}

func validateGeneralChange(change network.GeneralChange) error {
	named := change.Hostname != nil || change.DefaultGatewayV4 != nil || change.DNSPrimary != nil || change.DNSSecondary != nil || change.IPv4First != nil
	if !named {
		return fmt.Errorf("network general change requires at least one field (hostname, default_gateway_v4, dns_primary, dns_secondary, or ipv4_first)")
	}
	for label, value := range map[string]*string{"default_gateway_v4": change.DefaultGatewayV4, "dns_primary": change.DNSPrimary, "dns_secondary": change.DNSSecondary} {
		if value != nil {
			if v := strings.TrimSpace(*value); v != "" && net.ParseIP(v) == nil {
				return fmt.Errorf("%s %q is not a valid IP address", label, v)
			}
		}
	}
	if change.Hostname != nil && strings.TrimSpace(*change.Hostname) == "" {
		return fmt.Errorf("hostname must not be empty")
	}
	return nil
}

func validateNetworkGeneralPlan(plan NetworkGeneralPlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the network general plan")
	}
	if plan.APIVersion != networkAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid network general plan metadata")
	}
	if err := validateGeneralChange(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("network general plan observed state was modified")
	}
	expectedHash, err := networkGeneralPlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("network general plan contents were modified after planning")
	}
	return nil
}

func networkGeneralPlanHash(plan NetworkGeneralPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

// ---- interface change (plan + guard only; apply refused) --------------------

type NetworkInterfaceObserved struct {
	Interface        synology.NetworkInterface      `json:"interface" jsonschema:"Complete observed target interface hashed into the plan"`
	Interfaces       []synology.NetworkInterface    `json:"interfaces" jsonschema:"All observed interfaces (used to resolve the management path)"`
	ManagementPath   synology.NetworkManagementPath `json:"management_path" jsonschema:"Resolved management path"`
	ProtectedSources []string                       `json:"protected_sources" jsonschema:"Source IPs of all active connections at plan time"`
}

type NetworkInterfacePlan struct {
	APIVersion          string                       `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                       `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                       `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             network.InterfaceChange      `json:"request" jsonschema:"Validated patch-only interface change intent"`
	Observed            NetworkInterfaceObserved     `json:"observed" jsonschema:"Complete observed state hashed into the plan"`
	ObservedFingerprint string                       `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed state"`
	Resulting           synology.NetworkInterface    `json:"resulting" jsonschema:"The merged interface that would be written (the guard evaluated this)"`
	Guard               synology.NetworkGuardVerdict `json:"guard" jsonschema:"Never-sever guard decision"`
	Risk                string                       `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                     `json:"warnings" jsonschema:"Connectivity warnings"`
	Summary             []string                     `json:"summary" jsonschema:"Human-readable operations"`
	WireUnverified      bool                         `json:"wire_unverified" jsonschema:"Always true: the interface-set request shape is unverified, so the apply is refused"`
	Hash                string                       `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

func (s *Service) PlanNetworkInterfaceChange(ctx context.Context, requestedNAS string, request network.InterfaceChange) (NetworkInterfacePlan, error) {
	if err := validateInterfaceChange(request); err != nil {
		return NetworkInterfacePlan{}, err
	}
	name, client, err := s.networkClient(ctx, requestedNAS)
	if err != nil {
		return NetworkInterfacePlan{}, err
	}
	plan, err := planNetworkInterfaceWithClient(ctx, name, client, request)
	if err != nil {
		return NetworkInterfacePlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = networkInterfacePlanHash(plan)
	}
	return plan, err
}

// ApplyNetworkInterfacePlan validates the plan and guard, then refuses because the
// interface-set wire is unverified. It never sends an unconfirmed body to DSM.
func (s *Service) ApplyNetworkInterfacePlan(ctx context.Context, plan NetworkInterfacePlan, approvalHash string) (NetworkApplyResult, error) {
	if err := validateNetworkInterfacePlan(plan, approvalHash); err != nil {
		return NetworkApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return NetworkApplyResult{}, err
	}
	name, client, err := s.networkClient(ctx, plan.NAS)
	if err != nil {
		return NetworkApplyResult{}, err
	}
	if _, err := client.ApplyNetworkInterfaceChange(ctx, plan.Request); err != nil {
		return NetworkApplyResult{}, err
	}
	return NetworkApplyResult{NAS: name, PlanHash: plan.Hash}, nil
}

func planNetworkInterfaceWithClient(ctx context.Context, nas string, client networkClient, request network.InterfaceChange) (NetworkInterfacePlan, error) {
	capabilities, _, err := client.NetworkCapabilities(ctx)
	if err != nil {
		return NetworkInterfacePlan{}, authenticationError(nas, err)
	}
	if !capabilities.InterfacesRead {
		return NetworkInterfacePlan{}, fmt.Errorf("NAS %q does not expose a network interface read backend", nas)
	}
	general, err := client.NetworkGeneralFresh(ctx)
	if err != nil {
		return NetworkInterfacePlan{}, authenticationError(nas, err)
	}
	path, interfaces, err := resolveManagementPath(ctx, client, general)
	if err != nil {
		return NetworkInterfacePlan{}, authenticationError(nas, err)
	}
	var current synology.NetworkInterface
	found := false
	for _, iface := range interfaces {
		if strings.EqualFold(iface.Name, request.Name) {
			current = iface
			found = true
			break
		}
	}
	if !found {
		return NetworkInterfacePlan{}, fmt.Errorf("interface %q was not found on NAS %q", request.Name, nas)
	}
	if network.InterfaceChangeIsNoop(current, request) {
		return NetworkInterfacePlan{}, fmt.Errorf("interface change would not change the current configuration")
	}
	verdict := network.EvaluateInterfaceChange(path, current, request)
	if !verdict.Allowed {
		return NetworkInterfacePlan{}, fmt.Errorf(
			"the never-sever guard refuses this change: %s; re-plan with allow_connectivity_break set (and an out-of-band recovery path ready) to proceed", verdict.Reason)
	}
	resulting := network.MergeInterface(current, request)
	plan := NetworkInterfacePlan{
		APIVersion:     networkAPIVersion,
		NAS:            nas,
		Request:        request,
		Observed:       NetworkInterfaceObserved{Interface: current, Interfaces: interfaces, ManagementPath: path, ProtectedSources: client.NetworkCurrentSources(ctx)},
		Resulting:      resulting,
		Guard:          verdict,
		WireUnverified: true,
	}
	plan.Risk, plan.Warnings, plan.Summary = networkInterfaceEffects(current, request, verdict)
	plan.Warnings = append(plan.Warnings, "interface reconfiguration is plan-only in this build: the SYNO.Core.Network.Ethernet set request shape is wire-unverified (DSM returns code 4302), so the apply is refused")
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return NetworkInterfacePlan{}, err
	}
	plan.Hash, err = networkInterfacePlanHash(plan)
	if err != nil {
		return NetworkInterfacePlan{}, err
	}
	return plan, nil
}

func networkInterfaceEffects(current synology.NetworkInterface, request network.InterfaceChange, verdict synology.NetworkGuardVerdict) (string, []string, []string) {
	summary := network.InterfaceChangeFields(current, request)
	if len(summary) == 0 {
		summary = []string{"no interface changes"}
	}
	summary = append([]string{fmt.Sprintf("change interface %s", request.Name)}, summary...)
	warnings := []string{}
	risk := "medium"
	if verdict.Protected {
		risk = "high"
		if verdict.Overridden {
			warnings = append(warnings, fmt.Sprintf("allow_connectivity_break acknowledged: %s; an out-of-band recovery path is required", verdict.Reason))
		}
	}
	return risk, warnings, summary
}

func validateInterfaceChange(change network.InterfaceChange) error {
	if strings.TrimSpace(change.Name) == "" {
		return fmt.Errorf("interface change requires an interface name")
	}
	named := change.IPv4 != nil || change.Netmask != nil || change.GatewayV4 != nil || change.UseDHCP != nil || change.MTU != nil
	if !named {
		return fmt.Errorf("interface change requires at least one field (ipv4, netmask, gateway_v4, use_dhcp, or mtu)")
	}
	for label, value := range map[string]*string{"ipv4": change.IPv4, "netmask": change.Netmask, "gateway_v4": change.GatewayV4} {
		if value != nil {
			if v := strings.TrimSpace(*value); v != "" && net.ParseIP(v) == nil {
				return fmt.Errorf("%s %q is not a valid IP address", label, v)
			}
		}
	}
	if change.MTU != nil && (*change.MTU < 68 || *change.MTU > 9000) {
		return fmt.Errorf("mtu %d is out of range (68-9000)", *change.MTU)
	}
	return nil
}

func validateNetworkInterfacePlan(plan NetworkInterfacePlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the network interface plan")
	}
	if plan.APIVersion != networkAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid network interface plan metadata")
	}
	if err := validateInterfaceChange(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("network interface plan observed state was modified")
	}
	expectedHash, err := networkInterfacePlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("network interface plan contents were modified after planning")
	}
	return nil
}

func networkInterfacePlanHash(plan NetworkInterfacePlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}
