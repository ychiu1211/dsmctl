package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ychiu1211/dsmctl/internal/domain/controlpanel"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const fileServicesAPIVersion = "dsmctl.io/v1alpha1"

var smbWorkgroupPattern = regexp.MustCompile(`^[\x20-\x7e]{1,15}$`)

type SMBStateResult struct {
	NAS string            `json:"nas" jsonschema:"NAS profile used for the request"`
	SMB synology.SMBState `json:"smb" jsonschema:"Normalized global SMB service configuration"`
}

type NFSStateResult struct {
	NAS string            `json:"nas" jsonschema:"NAS profile used for the request"`
	NFS synology.NFSState `json:"nfs" jsonschema:"Normalized global NFS service configuration"`
}

type FileServiceCapabilitiesResult struct {
	NAS          string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.FileServiceCapabilities `json:"capabilities" jsonschema:"Independently selected SMB and NFS operations"`
	Report       synology.CompatibilityReport     `json:"report" jsonschema:"Discovered APIs and selected File Services backends"`
}

type FileServiceObservedState struct {
	SMB *synology.SMBState `json:"smb,omitempty" jsonschema:"Complete SMB state observed during planning"`
	NFS *synology.NFSState `json:"nfs,omitempty" jsonschema:"Complete NFS state observed during planning"`
}

type FileServicePlan struct {
	APIVersion          string                                `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                                `json:"nas" jsonschema:"NAS profile selected during planning"`
	Request             controlpanel.FileServiceChangeRequest `json:"request" jsonschema:"Validated patch-only File Services intent"`
	Observed            FileServiceObservedState              `json:"observed" jsonschema:"Complete selected module state observed during planning"`
	ObservedFingerprint string                                `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed module state"`
	Destructive         bool                                  `json:"destructive" jsonschema:"Whether the plan disables a file service"`
	Risk                string                                `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                              `json:"warnings" jsonschema:"Service disruption and compatibility warnings"`
	Summary             []string                              `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                                `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

type FileServiceApplyResult struct {
	NAS       string                             `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash  string                             `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied   bool                               `json:"applied" jsonschema:"Whether DSM accepted and postcondition verification passed"`
	Operation synology.FileServiceMutationResult `json:"operation" jsonschema:"Selected DSM mutation backend"`
}

type fileServiceClient interface {
	SMBState(context.Context) (synology.SMBState, error)
	NFSState(context.Context) (synology.NFSState, error)
	FileServiceCapabilities(context.Context) (synology.FileServiceCapabilities, synology.CompatibilityReport, error)
	ApplyFileServiceChange(context.Context, synology.FileServiceChangeRequest) (synology.FileServiceMutationResult, error)
}

func (s *Service) GetSMBState(ctx context.Context, requestedNAS string) (SMBStateResult, error) {
	name, client, err := s.fileServiceClient(ctx, requestedNAS)
	if err != nil {
		return SMBStateResult{}, err
	}
	state, err := client.SMBState(ctx)
	if err != nil {
		return SMBStateResult{}, authenticationError(name, err)
	}
	return SMBStateResult{NAS: name, SMB: state}, nil
}

func (s *Service) GetNFSState(ctx context.Context, requestedNAS string) (NFSStateResult, error) {
	name, client, err := s.fileServiceClient(ctx, requestedNAS)
	if err != nil {
		return NFSStateResult{}, err
	}
	state, err := client.NFSState(ctx)
	if err != nil {
		return NFSStateResult{}, authenticationError(name, err)
	}
	return NFSStateResult{NAS: name, NFS: state}, nil
}

func (s *Service) GetFileServiceCapabilities(ctx context.Context, requestedNAS string) (FileServiceCapabilitiesResult, error) {
	name, client, err := s.fileServiceClient(ctx, requestedNAS)
	if err != nil {
		return FileServiceCapabilitiesResult{}, err
	}
	capabilities, report, err := client.FileServiceCapabilities(ctx)
	if err != nil {
		return FileServiceCapabilitiesResult{}, authenticationError(name, err)
	}
	return FileServiceCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) PlanFileServiceChange(ctx context.Context, requestedNAS string, request controlpanel.FileServiceChangeRequest) (FileServicePlan, error) {
	if err := validateFileServiceRequestShape(request); err != nil {
		return FileServicePlan{}, err
	}
	name, client, err := s.fileServiceClient(ctx, requestedNAS)
	if err != nil {
		return FileServicePlan{}, err
	}
	return planFileServiceChangeWithClient(ctx, name, client, request)
}

func (s *Service) ApplyFileServicePlan(ctx context.Context, plan FileServicePlan, approvalHash string) (FileServiceApplyResult, error) {
	if err := validateFileServicePlan(plan, approvalHash); err != nil {
		return FileServiceApplyResult{}, err
	}
	name, client, err := s.fileServiceClient(ctx, plan.NAS)
	if err != nil {
		return FileServiceApplyResult{}, err
	}
	if name != plan.NAS {
		return FileServiceApplyResult{}, fmt.Errorf("File Services plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyFileServicePlanWithClient(ctx, client, plan)
}

func applyFileServicePlanWithClient(ctx context.Context, client fileServiceClient, plan FileServicePlan) (FileServiceApplyResult, error) {
	current, err := planFileServiceChangeWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return FileServiceApplyResult{}, fmt.Errorf("File Services plan precondition no longer holds: %w", err)
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return FileServiceApplyResult{}, fmt.Errorf("File Services plan is stale; create a new plan")
	}
	operation, err := client.ApplyFileServiceChange(ctx, plan.Request)
	if err != nil {
		return FileServiceApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifyFileServicePostcondition(ctx, client, plan.Request); err != nil {
		return FileServiceApplyResult{}, fmt.Errorf("verify File Services change: %w", err)
	}
	return FileServiceApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operation: operation}, nil
}

func (s *Service) fileServiceClient(ctx context.Context, requestedNAS string) (string, fileServiceClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(fileServiceClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement File Services management")
	}
	return name, client, nil
}

func planFileServiceChangeWithClient(ctx context.Context, nas string, client fileServiceClient, request controlpanel.FileServiceChangeRequest) (FileServicePlan, error) {
	capabilities, _, err := client.FileServiceCapabilities(ctx)
	if err != nil {
		return FileServicePlan{}, authenticationError(nas, err)
	}
	plan := FileServicePlan{APIVersion: fileServicesAPIVersion, NAS: nas, Request: request}
	switch request.Protocol {
	case controlpanel.FileProtocolSMB:
		if !capabilities.SMB.Read || !capabilities.SMB.Set {
			return FileServicePlan{}, fmt.Errorf("NAS %q does not expose a verified SMB read/set backend", nas)
		}
		state, err := client.SMBState(ctx)
		if err != nil {
			return FileServicePlan{}, authenticationError(nas, err)
		}
		if err := validateSMBChange(state, *request.SMB); err != nil {
			return FileServicePlan{}, err
		}
		plan.Observed.SMB = &state
	case controlpanel.FileProtocolNFS:
		advanced := request.NFS.NFSv4Domain != nil
		if !capabilities.NFS.Read || (!advanced && !capabilities.NFS.Set) || (advanced && !capabilities.NFS.SetAdvanced) {
			return FileServicePlan{}, fmt.Errorf("NAS %q does not expose the required verified NFS read/set backend", nas)
		}
		state, err := client.NFSState(ctx)
		if err != nil {
			return FileServicePlan{}, authenticationError(nas, err)
		}
		if err := validateNFSChange(state, *request.NFS); err != nil {
			return FileServicePlan{}, err
		}
		plan.Observed.NFS = &state
	}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return FileServicePlan{}, err
	}
	plan.Destructive, plan.Warnings, plan.Summary = fileServicePlanEffects(plan)
	if plan.Destructive || len(plan.Warnings) > 0 {
		plan.Risk = "high"
	} else {
		plan.Risk = "medium"
	}
	plan.Hash, err = fileServicePlanHash(plan)
	if err != nil {
		return FileServicePlan{}, err
	}
	return plan, nil
}

func validateFileServiceRequestShape(request controlpanel.FileServiceChangeRequest) error {
	switch request.Protocol {
	case controlpanel.FileProtocolSMB:
		if request.SMB == nil || request.NFS != nil {
			return fmt.Errorf("SMB protocol requires only the smb patch")
		}
		if emptySMBChange(*request.SMB) {
			return fmt.Errorf("SMB patch has no fields")
		}
	case controlpanel.FileProtocolNFS:
		if request.NFS == nil || request.SMB != nil {
			return fmt.Errorf("NFS protocol requires only the nfs patch")
		}
		if emptyNFSChange(*request.NFS) {
			return fmt.Errorf("NFS patch has no fields")
		}
		if request.NFS.NFSv4Domain != nil && (request.NFS.Enabled != nil || request.NFS.MaximumProtocol != nil) {
			return fmt.Errorf("nfsv4_domain must be planned separately from NFS base settings")
		}
	default:
		return fmt.Errorf("unsupported file protocol %q", request.Protocol)
	}
	return nil
}

func validateSMBChange(state synology.SMBState, change controlpanel.SMBChange) error {
	if change.Workgroup != nil {
		value := strings.TrimSpace(*change.Workgroup)
		if !smbWorkgroupPattern.MatchString(value) {
			return fmt.Errorf("SMB workgroup must contain 1-15 printable ASCII characters")
		}
	}
	minimum, maximum := state.MinimumProtocol, state.MaximumProtocol
	if change.MinimumProtocol != nil {
		minimum = *change.MinimumProtocol
	}
	if change.MaximumProtocol != nil {
		maximum = *change.MaximumProtocol
	}
	if smbProtocolRank(minimum) < 0 || smbProtocolRank(maximum) < 0 {
		return fmt.Errorf("unsupported SMB protocol range %q-%q", minimum, maximum)
	}
	if smbProtocolRank(minimum) > smbProtocolRank(maximum) {
		return fmt.Errorf("SMB minimum protocol %q exceeds maximum protocol %q", minimum, maximum)
	}
	if minimum == controlpanel.SMBProtocol3 {
		return fmt.Errorf("DSM does not allow SMB3 as the minimum protocol")
	}
	if change.TransportEncryption != nil && !validSMBPolicy(*change.TransportEncryption) {
		return fmt.Errorf("unsupported SMB transport encryption policy %q", *change.TransportEncryption)
	}
	if maximum != controlpanel.SMBProtocol3 && change.TransportEncryption != nil {
		return fmt.Errorf("SMB transport encryption is configurable only when the maximum protocol is SMB3")
	}
	if change.ServerSigning != nil && !validSMBSigningPolicy(*change.ServerSigning) {
		return fmt.Errorf("unsupported SMB server signing policy %q", *change.ServerSigning)
	}
	if smbChangeMatches(state, change) {
		return fmt.Errorf("SMB patch would not change the current state")
	}
	return nil
}

func validateNFSChange(state synology.NFSState, change controlpanel.NFSChange) error {
	if change.NFSv4Domain != nil {
		value := strings.TrimSpace(*change.NFSv4Domain)
		if utf8.RuneCountInString(value) > 255 || strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("NFSv4 domain must be at most 255 characters without control line breaks")
		}
	}
	if change.MaximumProtocol != nil {
		if *change.MaximumProtocol != controlpanel.NFSProtocol3 && *change.MaximumProtocol != controlpanel.NFSProtocol4 && *change.MaximumProtocol != controlpanel.NFSProtocol4_1 {
			return fmt.Errorf("unsupported NFS maximum protocol %q", *change.MaximumProtocol)
		}
		if !containsNFSProtocol(state.SupportedProtocols, *change.MaximumProtocol) {
			return fmt.Errorf("NFS protocol %q is not advertised by this NAS", *change.MaximumProtocol)
		}
	}
	if nfsChangeMatches(state, change) {
		return fmt.Errorf("NFS patch would not change the current state")
	}
	return nil
}

func verifyFileServicePostcondition(ctx context.Context, client fileServiceClient, request controlpanel.FileServiceChangeRequest) error {
	switch request.Protocol {
	case controlpanel.FileProtocolSMB:
		state, err := client.SMBState(ctx)
		if err != nil {
			return err
		}
		if !smbChangeMatches(state, *request.SMB) {
			return fmt.Errorf("SMB state does not match the approved patch")
		}
	case controlpanel.FileProtocolNFS:
		state, err := client.NFSState(ctx)
		if err != nil {
			return err
		}
		if !nfsChangeMatches(state, *request.NFS) {
			return fmt.Errorf("NFS state does not match the approved patch")
		}
	}
	return nil
}

func fileServicePlanEffects(plan FileServicePlan) (bool, []string, []string) {
	warnings := []string{}
	summary := []string{}
	destructive := false
	if change := plan.Request.SMB; change != nil {
		if change.Enabled != nil {
			summary = append(summary, fmt.Sprintf("set SMB enabled to %t", *change.Enabled))
			if !*change.Enabled {
				destructive = true
				warnings = append(warnings, "disabling SMB disconnects SMB clients and stops new SMB access")
			}
		}
		if change.Workgroup != nil {
			summary = append(summary, fmt.Sprintf("set SMB workgroup to %q", strings.TrimSpace(*change.Workgroup)))
		}
		if change.MinimumProtocol != nil || change.MaximumProtocol != nil {
			summary = append(summary, "change the accepted SMB protocol range")
			warnings = append(warnings, "changing the SMB protocol range can disconnect or exclude clients")
		}
		if change.TransportEncryption != nil {
			summary = append(summary, fmt.Sprintf("set SMB transport encryption to %s", *change.TransportEncryption))
			warnings = append(warnings, "changing SMB transport encryption can reject incompatible clients")
		}
		if change.ServerSigning != nil {
			summary = append(summary, fmt.Sprintf("set SMB server signing to %s", *change.ServerSigning))
			warnings = append(warnings, "changing SMB signing can reject incompatible clients")
		}
		if change.OpportunisticLocking != nil {
			summary = append(summary, fmt.Sprintf("set SMB opportunistic locking to %t", *change.OpportunisticLocking))
		}
		if change.SMB2Leases != nil {
			summary = append(summary, fmt.Sprintf("set SMB2 leasing to %t", *change.SMB2Leases))
		}
		if change.DurableHandles != nil {
			summary = append(summary, fmt.Sprintf("set SMB durable handles to %t", *change.DurableHandles))
		}
		if change.LocalMasterBrowser != nil {
			summary = append(summary, fmt.Sprintf("set SMB local master browser to %t", *change.LocalMasterBrowser))
		}
	}
	if change := plan.Request.NFS; change != nil {
		if change.Enabled != nil {
			summary = append(summary, fmt.Sprintf("set NFS enabled to %t", *change.Enabled))
			if !*change.Enabled {
				destructive = true
				warnings = append(warnings, "disabling NFS interrupts NFS exports and clients")
			}
		}
		if change.MaximumProtocol != nil {
			summary = append(summary, fmt.Sprintf("set maximum NFS protocol to %s", *change.MaximumProtocol))
			warnings = append(warnings, "changing the maximum NFS protocol may interrupt or exclude NFS clients")
		}
		if change.NFSv4Domain != nil {
			summary = append(summary, fmt.Sprintf("set NFSv4 domain to %q", strings.TrimSpace(*change.NFSv4Domain)))
			warnings = append(warnings, "changing the NFSv4 domain can change ID mapping for active clients")
		}
	}
	return destructive, warnings, summary
}

func validateFileServicePlan(plan FileServicePlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the File Services plan")
	}
	if plan.APIVersion != fileServicesAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid File Services plan metadata")
	}
	if err := validateFileServiceRequestShape(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("File Services plan observed state was modified")
	}
	expectedHash, err := fileServicePlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("File Services plan contents were modified after planning")
	}
	return nil
}

func fileServicePlanHash(plan FileServicePlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

func emptySMBChange(change controlpanel.SMBChange) bool {
	return change.Enabled == nil && change.Workgroup == nil && change.MinimumProtocol == nil && change.MaximumProtocol == nil && change.TransportEncryption == nil && change.ServerSigning == nil &&
		change.OpportunisticLocking == nil && change.SMB2Leases == nil && change.DurableHandles == nil && change.LocalMasterBrowser == nil
}

func emptyNFSChange(change controlpanel.NFSChange) bool {
	return change.Enabled == nil && change.MaximumProtocol == nil && change.NFSv4Domain == nil
}

func smbProtocolRank(protocol controlpanel.SMBProtocol) int {
	switch protocol {
	case controlpanel.SMBProtocol1:
		return 0
	case controlpanel.SMBProtocol2:
		return 1
	case controlpanel.SMBProtocol2LargeMTU:
		return 2
	case controlpanel.SMBProtocol3:
		return 3
	default:
		return -1
	}
}

func validSMBPolicy(policy controlpanel.SMBPolicy) bool {
	return policy == controlpanel.SMBPolicyDisabled || policy == controlpanel.SMBPolicyAutomatic || policy == controlpanel.SMBPolicyRequired
}

func validSMBSigningPolicy(policy controlpanel.SMBSigningPolicy) bool {
	return policy == controlpanel.SMBSigningDisabledForSMB1 || policy == controlpanel.SMBSigningAutomatic || policy == controlpanel.SMBSigningRequired
}

func containsNFSProtocol(values []controlpanel.NFSProtocol, wanted controlpanel.NFSProtocol) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func smbChangeMatches(state synology.SMBState, change controlpanel.SMBChange) bool {
	return (change.Enabled == nil || state.Enabled == *change.Enabled) &&
		(change.Workgroup == nil || state.Workgroup == strings.TrimSpace(*change.Workgroup)) &&
		(change.MinimumProtocol == nil || state.MinimumProtocol == *change.MinimumProtocol) &&
		(change.MaximumProtocol == nil || state.MaximumProtocol == *change.MaximumProtocol) &&
		(change.TransportEncryption == nil || state.TransportEncryption == *change.TransportEncryption) &&
		(change.ServerSigning == nil || state.ServerSigning == *change.ServerSigning) &&
		(change.OpportunisticLocking == nil || state.OpportunisticLocking == *change.OpportunisticLocking) &&
		(change.SMB2Leases == nil || state.SMB2Leases == *change.SMB2Leases) &&
		(change.DurableHandles == nil || state.DurableHandles == *change.DurableHandles) &&
		(change.LocalMasterBrowser == nil || state.LocalMasterBrowser == *change.LocalMasterBrowser)
}

func nfsChangeMatches(state synology.NFSState, change controlpanel.NFSChange) bool {
	return (change.Enabled == nil || state.Enabled == *change.Enabled) &&
		(change.MaximumProtocol == nil || state.MaximumProtocol == *change.MaximumProtocol) &&
		(change.NFSv4Domain == nil || state.NFSv4Domain == strings.TrimSpace(*change.NFSv4Domain))
}

var _ fileServiceClient = (*synology.Client)(nil)
