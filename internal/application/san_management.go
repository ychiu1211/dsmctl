package application

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/ychiu1211/dsmctl/internal/domain/san"
	"github.com/ychiu1211/dsmctl/internal/domain/storage"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const minimumLUNSizeBytes = uint64(1 << 30)

type SANPlan struct {
	APIVersion        string              `json:"api_version" jsonschema:"Plan schema version"`
	NAS               string              `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision   uint64              `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request           san.ChangeRequest   `json:"request" jsonschema:"Validated and canonical SAN intent"`
	Precondition      ChangePrecondition  `json:"precondition" jsonschema:"Observed stable resource or mapping state that must still match"`
	References        SANStableReferences `json:"references" jsonschema:"Stable DSM identifiers and resolved volume path used by the operation"`
	StateFingerprint  string              `json:"state_fingerprint" jsonschema:"Hash of normalized target, LUN, and mapping state"`
	VolumeFingerprint string              `json:"volume_fingerprint,omitempty" jsonschema:"Hash of the referenced backing volume state"`
	Destructive       bool                `json:"destructive" jsonschema:"Whether the operation removes a LUN, target, or mapping"`
	Risk              string              `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings          []string            `json:"warnings" jsonschema:"SAN-specific safety warnings"`
	Summary           []string            `json:"summary" jsonschema:"Human-readable actions and verification steps"`
	Hash              string              `json:"hash" jsonschema:"SHA-256 approval hash covering the canonical plan"`
}

type SANStableReferences struct {
	ResourceID        string `json:"resource_id,omitempty" jsonschema:"Stable target ID, LUN UUID, or mapping composite ID"`
	TargetID          string `json:"target_id,omitempty" jsonschema:"Stable DSM target ID"`
	LUNID             string `json:"lun_id,omitempty" jsonschema:"Stable LUN UUID"`
	BackingVolumeID   string `json:"backing_volume_id,omitempty" jsonschema:"Stable DSM volume ID"`
	BackingVolumePath string `json:"backing_volume_path,omitempty" jsonschema:"DSM path resolved from the stable volume ID"`
	BackingFileSystem string `json:"backing_file_system,omitempty" jsonschema:"Filesystem observed on the referenced volume"`
}

type SANApplyResult struct {
	NAS              string                     `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash         string                     `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied          bool                       `json:"applied" jsonschema:"Whether DSM accepted the mutation and the postcondition was verified"`
	ResourceID       string                     `json:"resource_id,omitempty" jsonschema:"Stable ID returned or confirmed by DSM"`
	Operation        string                     `json:"operation,omitempty" jsonschema:"Selected semantic mutation operation"`
	Retryable        bool                       `json:"retryable" jsonschema:"Whether current state can be inspected and a fresh plan safely retried"`
	StateFingerprint string                     `json:"state_fingerprint,omitempty" jsonschema:"Current SAN state fingerprint after an uncertain or successful apply"`
	SAN              synology.SANState          `json:"san" jsonschema:"Post-apply or failure-state SAN inventory"`
	Mutation         synology.SANMutationResult `json:"mutation,omitempty" jsonschema:"Typed backend mutation result"`
}

type SANApplyError struct {
	Operation        string
	Cause            error
	StateFingerprint string
	ResourceExists   bool
	MappingExists    bool
	Retryable        bool
}

func (err *SANApplyError) Error() string {
	return fmt.Sprintf("SAN apply %s did not reach a verified postcondition: %v; retryable=%t current_state=%s resource_exists=%t mapping_exists=%t",
		err.Operation, err.Cause, err.Retryable, err.StateFingerprint, err.ResourceExists, err.MappingExists)
}

func (err *SANApplyError) Unwrap() error { return err.Cause }

func (s *Service) PlanSANChange(ctx context.Context, requestedNAS string, request san.ChangeRequest) (SANPlan, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return SANPlan{}, err
	}
	state, err := client.SANState(ctx)
	if err != nil {
		return SANPlan{}, authenticationError(name, err)
	}
	var storageState synology.StorageState
	if request.Resource == san.ResourceLUN {
		storageState, err = client.StorageState(ctx)
		if err != nil {
			return SANPlan{}, authenticationError(name, err)
		}
	}
	plan, err := BuildSANPlan(name, state, storageState, request)
	if err != nil {
		return SANPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = sanPlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplySANPlan(ctx context.Context, plan SANPlan, approvalHash string) (SANApplyResult, error) {
	if err := validateSANPlan(plan, approvalHash); err != nil {
		return SANApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return SANApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return SANApplyResult{}, err
	}
	name, client, err := s.manager.Client(ctx, plan.NAS)
	if err != nil {
		return SANApplyResult{}, err
	}
	state, err := client.SANState(ctx)
	if err != nil {
		return SANApplyResult{}, authenticationError(name, err)
	}
	var storageState synology.StorageState
	if plan.Request.Resource == san.ResourceLUN {
		storageState, err = client.StorageState(ctx)
		if err != nil {
			return SANApplyResult{}, authenticationError(name, err)
		}
	}
	replanned, err := BuildSANPlan(name, state, storageState, plan.Request)
	if err != nil {
		return SANApplyResult{}, fmt.Errorf("revalidate SAN plan: %w", err)
	}
	replanned.ProfileRevision = plan.ProfileRevision
	replanned.Hash, err = sanPlanHash(replanned)
	if err != nil {
		return SANApplyResult{}, err
	}
	if replanned.Hash != plan.Hash || !reflect.DeepEqual(replanned.Precondition, plan.Precondition) || !reflect.DeepEqual(replanned.References, plan.References) {
		return SANApplyResult{}, errors.New("SAN state changed after planning; generate and approve a fresh plan")
	}

	input := synology.SANMutationInput{Request: plan.Request}
	if plan.Request.Resource == san.ResourceLUN {
		input.BackingVolumePath = plan.References.BackingVolumePath
		input.BackingFileSystem = plan.References.BackingFileSystem
		if plan.Request.LUN.NewBackingVolumeID != nil {
			path := plan.References.BackingVolumePath
			input.NewBackingVolumePath = &path
		}
	}
	if plan.Request.Resource == san.ResourceTarget {
		mode := plan.Request.Target.Authentication
		if plan.Request.Target.NewAuthentication != nil {
			mode = *plan.Request.Target.NewAuthentication
		}
		if mode == san.AuthenticationCHAP || mode == san.AuthenticationMutualCHAP {
			input.CHAPPassword, err = s.secretReferences.ResolveSecret(ctx, plan.Request.Target.CHAPPasswordRef)
			if err != nil {
				return SANApplyResult{}, fmt.Errorf("resolve CHAP password: %w", err)
			}
			if err := validateCHAPPassword(input.CHAPPassword); err != nil {
				return SANApplyResult{}, err
			}
		}
		if mode == san.AuthenticationMutualCHAP {
			input.MutualCHAPPassword, err = s.secretReferences.ResolveSecret(ctx, plan.Request.Target.MutualCHAPPasswordRef)
			if err != nil {
				return SANApplyResult{}, fmt.Errorf("resolve mutual CHAP password: %w", err)
			}
			if err := validateCHAPPassword(input.MutualCHAPPassword); err != nil {
				return SANApplyResult{}, err
			}
			if input.CHAPPassword == input.MutualCHAPPassword {
				return SANApplyResult{}, errors.New("CHAP and mutual CHAP passwords must differ")
			}
		}
	}

	mutation, err := client.ApplySANChange(ctx, input)
	if err != nil {
		return s.sanFailureResult(ctx, name, client, plan, mutation.Operation, err)
	}
	postState, verifyErr := waitForSANPostcondition(ctx, client, plan, mutation.ResourceID)
	result := SANApplyResult{
		NAS: name, PlanHash: plan.Hash, Applied: verifyErr == nil, ResourceID: mutation.ResourceID,
		Operation: mutation.Operation, Retryable: verifyErr != nil, SAN: postState, Mutation: mutation,
		StateFingerprint: fingerprint(postState),
	}
	if verifyErr != nil {
		exists, mapped := sanResourceState(postState, plan.Request, mutation.ResourceID)
		return result, &SANApplyError{Operation: mutation.Operation, Cause: verifyErr, StateFingerprint: result.StateFingerprint, ResourceExists: exists, MappingExists: mapped, Retryable: true}
	}
	return result, nil
}

func (s *Service) sanFailureResult(ctx context.Context, name string, client interface {
	SANState(context.Context) (synology.SANState, error)
}, plan SANPlan, operation string, cause error) (SANApplyResult, error) {
	current, readErr := client.SANState(ctx)
	if readErr != nil {
		cause = errors.Join(cause, fmt.Errorf("re-read SAN state after failure: %w", readErr))
	}
	fingerprintValue := fingerprint(current)
	exists, mapped := sanResourceState(current, plan.Request, "")
	result := SANApplyResult{NAS: name, PlanHash: plan.Hash, Applied: false, Operation: operation, Retryable: readErr == nil, SAN: current, StateFingerprint: fingerprintValue}
	return result, &SANApplyError{Operation: operation, Cause: cause, StateFingerprint: fingerprintValue, ResourceExists: exists, MappingExists: mapped, Retryable: readErr == nil}
}

func BuildSANPlan(nas string, state san.State, storageState storage.State, request san.ChangeRequest) (SANPlan, error) {
	nas = strings.TrimSpace(nas)
	if nas == "" {
		return SANPlan{}, errors.New("NAS profile is required")
	}
	canonical, err := canonicalSANRequest(request)
	if err != nil {
		return SANPlan{}, err
	}
	normalized, err := normalizeSANState(state)
	if err != nil {
		return SANPlan{}, err
	}
	plan := SANPlan{APIVersion: managementAPIVersion, NAS: nas, Request: canonical, StateFingerprint: fingerprint(normalized)}
	if err := populateSANPlan(&plan, normalized, storageState); err != nil {
		return SANPlan{}, err
	}
	plan.Hash, err = sanPlanHash(plan)
	if err != nil {
		return SANPlan{}, err
	}
	return plan, nil
}

func populateSANPlan(plan *SANPlan, state san.State, storageState storage.State) error {
	request := plan.Request
	plan.Warnings = []string{"Apply re-reads SAN and referenced volume state, rejects stale plans, and verifies the stable-ID postcondition."}
	plan.Risk = "medium"
	switch request.Resource {
	case san.ResourceTarget:
		return populateTargetPlan(plan, state)
	case san.ResourceLUN:
		return populateLUNPlan(plan, state, storageState)
	case san.ResourceMapping:
		return populateMappingPlan(plan, state)
	default:
		return fmt.Errorf("unsupported SAN resource %q", request.Resource)
	}
}

func populateTargetPlan(plan *SANPlan, state san.State) error {
	request, change := plan.Request, plan.Request.Target
	if change == nil {
		return errors.New("target resource requires target intent")
	}
	switch request.Action {
	case san.ActionCreate:
		if findTargetByName(state, change.Name) != nil {
			return fmt.Errorf("target name %q already exists", change.Name)
		}
		if findTargetByIQN(state, change.IQN) != nil {
			return fmt.Errorf("target IQN %q already exists", change.IQN)
		}
		plan.Precondition = ChangePrecondition{ExpectedExists: false}
		plan.Summary = []string{fmt.Sprintf("Create unmapped iSCSI target %q with %s authentication.", change.Name, change.Authentication)}
	case san.ActionUpdate, san.ActionDelete:
		target := findTarget(state, change.ID)
		if target == nil {
			return fmt.Errorf("target stable ID %q does not exist", change.ID)
		}
		mappings := mappingsForTarget(state, target.ID)
		plan.Precondition = ChangePrecondition{ExpectedExists: true, ResourceID: target.ID, Fingerprint: fingerprint(struct {
			Target   san.Target
			Mappings []san.Mapping
		}{*target, mappings})}
		plan.References = SANStableReferences{ResourceID: target.ID, TargetID: target.ID}
		if request.Action == san.ActionDelete {
			if target.ConnectedSessions != 0 {
				return fmt.Errorf("target %q has %d active session(s)", target.ID, target.ConnectedSessions)
			}
			if len(mappings) != 0 {
				return fmt.Errorf("target %q has %d mapping(s); detach them explicitly first", target.ID, len(mappings))
			}
			plan.Destructive, plan.Risk = true, "high"
			plan.Warnings = append(plan.Warnings, "Target deletion never deletes mapped LUNs; this plan is valid only while the target remains unmapped and disconnected.")
			plan.Summary = []string{fmt.Sprintf("Delete disconnected, unmapped target %s.", target.ID)}
		} else {
			if target.ConnectedSessions != 0 {
				return fmt.Errorf("target %q has active sessions; disconnect initiators before changing target settings", target.ID)
			}
			if change.NewName != nil {
				if existing := findTargetByName(state, *change.NewName); existing != nil && existing.ID != target.ID {
					return fmt.Errorf("target name %q already exists", *change.NewName)
				}
			}
			if change.NewIQN != nil {
				if existing := findTargetByIQN(state, *change.NewIQN); existing != nil && existing.ID != target.ID {
					return fmt.Errorf("target IQN %q already exists", *change.NewIQN)
				}
			}
			plan.Summary = []string{fmt.Sprintf("Patch settings for disconnected target %s.", target.ID)}
		}
	default:
		return fmt.Errorf("target does not support action %q", request.Action)
	}
	return nil
}

func populateLUNPlan(plan *SANPlan, state san.State, storageState storage.State) error {
	request, change := plan.Request, plan.Request.LUN
	if change == nil {
		return errors.New("lun resource requires lun intent")
	}
	switch request.Action {
	case san.ActionCreate:
		if findLUNByName(state, change.Name) != nil {
			return fmt.Errorf("LUN name %q already exists", change.Name)
		}
		volume, err := validateBackingVolume(storageState, change.BackingVolumeID, change.SizeBytes)
		if err != nil {
			return err
		}
		plan.Precondition = ChangePrecondition{ExpectedExists: false}
		setPlanVolume(plan, *volume)
		plan.Summary = []string{fmt.Sprintf("Create an unmapped %s LUN %q of %d bytes on volume %s.", change.Provisioning, change.Name, change.SizeBytes, volume.ID)}
	case san.ActionUpdate, san.ActionDelete:
		lun := findLUN(state, change.ID)
		if lun == nil {
			return fmt.Errorf("LUN stable UUID %q does not exist", change.ID)
		}
		mappings := mappingsForLUN(state, lun.ID)
		plan.Precondition = ChangePrecondition{ExpectedExists: true, ResourceID: lun.ID, Fingerprint: fingerprint(struct {
			LUN      san.LUN
			Mappings []san.Mapping
		}{*lun, mappings})}
		plan.References = SANStableReferences{ResourceID: lun.ID, LUNID: lun.ID}
		if request.Action == san.ActionDelete {
			if lun.Mapped || len(mappings) != 0 {
				return fmt.Errorf("LUN %q is mapped; detach every mapping explicitly before delete", lun.ID)
			}
			plan.Destructive, plan.Risk = true, "high"
			plan.Warnings = append(plan.Warnings, "Deleting a LUN permanently destroys its block storage. Apply rechecks the exact stable UUID and unmapped state.")
			plan.Summary = []string{fmt.Sprintf("Permanently delete unmapped LUN %s (%q).", lun.ID, lun.Name)}
			return nil
		}
		if activeTargetsForLUN(state, lun.ID) != 0 {
			return fmt.Errorf("LUN %q is reachable through a target with active sessions", lun.ID)
		}
		if change.NewName != nil {
			if existing := findLUNByName(state, *change.NewName); existing != nil && existing.ID != lun.ID {
				return fmt.Errorf("LUN name %q already exists", *change.NewName)
			}
		}
		if change.NewSizeBytes != nil {
			if *change.NewSizeBytes <= lun.SizeBytes {
				return fmt.Errorf("new LUN size %d must exceed observed size %d", *change.NewSizeBytes, lun.SizeBytes)
			}
		}
		volumeID := ""
		needed := uint64(0)
		if change.NewBackingVolumeID != nil {
			volumeID = *change.NewBackingVolumeID
			needed = lun.SizeBytes
			if change.NewSizeBytes != nil {
				needed = *change.NewSizeBytes
			}
		} else if change.NewSizeBytes != nil {
			volumeID = volumeIDForPath(storageState, lun.BackingLocation)
			if volumeID == "" {
				return fmt.Errorf("cannot resolve current LUN backing path %q to a stable volume ID", lun.BackingLocation)
			}
			needed = *change.NewSizeBytes - lun.SizeBytes
		}
		if volumeID != "" {
			volume, err := validateBackingVolume(storageState, volumeID, needed)
			if err != nil {
				return err
			}
			setPlanVolume(plan, *volume)
			plan.References.ResourceID, plan.References.LUNID = lun.ID, lun.ID
		}
		plan.Summary = []string{fmt.Sprintf("Patch disconnected LUN %s without changing provisioning type.", lun.ID)}
	default:
		return fmt.Errorf("lun does not support action %q", request.Action)
	}
	return nil
}

func populateMappingPlan(plan *SANPlan, state san.State) error {
	request, change := plan.Request, plan.Request.Mapping
	if change == nil {
		return errors.New("mapping resource requires mapping intent")
	}
	target := findTarget(state, change.TargetID)
	if target == nil {
		return fmt.Errorf("target stable ID %q does not exist", change.TargetID)
	}
	lun := findLUN(state, change.LUNID)
	if lun == nil {
		return fmt.Errorf("LUN stable UUID %q does not exist", change.LUNID)
	}
	if target.ConnectedSessions != 0 {
		return fmt.Errorf("target %q has active sessions; disconnect initiators before changing mappings", target.ID)
	}
	exists := hasMapping(state, target.ID, lun.ID)
	if request.Action == san.ActionAttach && exists {
		return fmt.Errorf("mapping %s:%s already exists", target.ID, lun.ID)
	}
	if request.Action == san.ActionDetach && !exists {
		return fmt.Errorf("mapping %s:%s does not exist", target.ID, lun.ID)
	}
	if request.Action != san.ActionAttach && request.Action != san.ActionDetach {
		return fmt.Errorf("mapping does not support action %q", request.Action)
	}
	resourceID := target.ID + ":" + lun.ID
	plan.Precondition = ChangePrecondition{ExpectedExists: exists, ResourceID: resourceID, Fingerprint: fingerprint(struct {
		Target san.Target
		LUN    san.LUN
		Exists bool
	}{*target, *lun, exists})}
	plan.References = SANStableReferences{ResourceID: resourceID, TargetID: target.ID, LUNID: lun.ID}
	plan.Destructive = request.Action == san.ActionDetach
	if plan.Destructive {
		plan.Risk = "high"
		plan.Warnings = append(plan.Warnings, "Detaching a mapping can remove host access but never deletes either endpoint.")
	}
	action := request.Action
	if action != "" {
		action = strings.ToUpper(action[:1]) + action[1:]
	}
	plan.Summary = []string{fmt.Sprintf("%s mapping between target %s and LUN %s without deleting either endpoint.", action, target.ID, lun.ID)}
	return nil
}

func canonicalSANRequest(request san.ChangeRequest) (san.ChangeRequest, error) {
	canonical := san.ChangeRequest{Action: strings.ToLower(strings.TrimSpace(request.Action)), Resource: strings.ToLower(strings.TrimSpace(request.Resource))}
	count := 0
	if request.Target != nil {
		count++
		change := *request.Target
		change.ID, change.Name, change.IQN = strings.TrimSpace(change.ID), strings.TrimSpace(change.Name), strings.TrimSpace(change.IQN)
		change.Authentication = canonicalAuthentication(change.Authentication)
		change.CHAPUser, change.CHAPPasswordRef = strings.TrimSpace(change.CHAPUser), strings.TrimSpace(change.CHAPPasswordRef)
		change.MutualCHAPUser, change.MutualCHAPPasswordRef = strings.TrimSpace(change.MutualCHAPUser), strings.TrimSpace(change.MutualCHAPPasswordRef)
		trimStringPointer(change.NewName)
		trimStringPointer(change.NewIQN)
		if change.NewAuthentication != nil {
			value := canonicalAuthentication(*change.NewAuthentication)
			change.NewAuthentication = &value
		}
		canonical.Target = &change
	}
	if request.LUN != nil {
		count++
		change := *request.LUN
		change.ID, change.Name, change.BackingVolumeID = strings.TrimSpace(change.ID), strings.TrimSpace(change.Name), strings.TrimSpace(change.BackingVolumeID)
		change.Provisioning = strings.ToLower(strings.TrimSpace(change.Provisioning))
		trimStringPointer(change.NewName)
		if change.NewBackingVolumeID != nil {
			trimStringPointer(change.NewBackingVolumeID)
		}
		canonical.LUN = &change
	}
	if request.Mapping != nil {
		count++
		change := *request.Mapping
		change.TargetID, change.LUNID = strings.TrimSpace(change.TargetID), strings.TrimSpace(change.LUNID)
		canonical.Mapping = &change
	}
	if count != 1 {
		return san.ChangeRequest{}, errors.New("SAN request must contain exactly one target, lun, or mapping intent")
	}
	if err := validateCanonicalSANRequest(canonical); err != nil {
		return san.ChangeRequest{}, err
	}
	return canonical, nil
}

func validateCanonicalSANRequest(request san.ChangeRequest) error {
	switch request.Resource {
	case san.ResourceTarget:
		if request.Target == nil || request.LUN != nil || request.Mapping != nil {
			return errors.New("target resource must contain only target intent")
		}
		return validateTargetIntent(request.Action, request.Target)
	case san.ResourceLUN:
		if request.LUN == nil || request.Target != nil || request.Mapping != nil {
			return errors.New("lun resource must contain only lun intent")
		}
		return validateLUNIntent(request.Action, request.LUN)
	case san.ResourceMapping:
		if request.Mapping == nil || request.Target != nil || request.LUN != nil {
			return errors.New("mapping resource must contain only mapping intent")
		}
		if request.Action != san.ActionAttach && request.Action != san.ActionDetach {
			return errors.New("mapping action must be attach or detach")
		}
		if request.Mapping.TargetID == "" || request.Mapping.LUNID == "" {
			return errors.New("mapping requires stable target_id and lun_id")
		}
		return nil
	default:
		return fmt.Errorf("unsupported SAN resource %q", request.Resource)
	}
}

func validateTargetIntent(action string, change *san.TargetChange) error {
	switch action {
	case san.ActionCreate:
		if change.ID != "" || change.NewName != nil || change.NewIQN != nil || change.NewAuthentication != nil || change.Enabled != nil {
			return errors.New("target create owns initial fields and cannot contain stable ID or update fields")
		}
		if err := validateSANName("target", change.Name); err != nil {
			return err
		}
		if err := validateIQN(change.IQN); err != nil {
			return err
		}
		return validateAuthentication(change.Authentication, change)
	case san.ActionUpdate:
		if change.ID == "" {
			return errors.New("target update requires stable id")
		}
		if change.Name != "" || change.IQN != "" || change.Authentication != "" {
			return errors.New("target update is patch-only; use new_* fields")
		}
		properties := change.NewName != nil || change.NewIQN != nil || change.NewAuthentication != nil
		if change.Enabled != nil && properties {
			return errors.New("target enabled patch must be planned separately from property updates")
		}
		if change.Enabled == nil && !properties {
			return errors.New("target update requires at least one patch field")
		}
		if change.NewName != nil {
			if err := validateSANName("target", *change.NewName); err != nil {
				return err
			}
		}
		if change.NewIQN != nil {
			if err := validateIQN(*change.NewIQN); err != nil {
				return err
			}
		}
		if change.NewAuthentication != nil {
			return validateAuthentication(*change.NewAuthentication, change)
		}
		if hasAuthenticationFields(change) {
			return errors.New("CHAP fields require new_authentication")
		}
		return nil
	case san.ActionDelete:
		if change.ID == "" {
			return errors.New("target delete requires stable id")
		}
		if change.Name != "" || change.IQN != "" || change.Authentication != "" || change.NewName != nil || change.NewIQN != nil || change.NewAuthentication != nil || change.Enabled != nil || hasAuthenticationFields(change) {
			return errors.New("target delete accepts only stable id")
		}
		return nil
	default:
		return fmt.Errorf("target action must be create, update, or delete")
	}
}

func validateLUNIntent(action string, change *san.LUNChange) error {
	switch action {
	case san.ActionCreate:
		if change.ID != "" || change.NewName != nil || change.NewDescription != nil || change.NewBackingVolumeID != nil || change.NewSizeBytes != nil {
			return errors.New("LUN create owns initial fields and cannot contain stable ID or update fields")
		}
		if err := validateSANName("LUN", change.Name); err != nil {
			return err
		}
		if change.BackingVolumeID == "" {
			return errors.New("LUN create requires backing_volume_id")
		}
		if err := validateLUNSize(change.SizeBytes); err != nil {
			return err
		}
		if change.Provisioning != san.ProvisioningThin && change.Provisioning != san.ProvisioningThick {
			return fmt.Errorf("LUN provisioning must be thin or thick")
		}
		return nil
	case san.ActionUpdate:
		if change.ID == "" {
			return errors.New("LUN update requires stable UUID")
		}
		if change.Name != "" || change.Description != "" || change.BackingVolumeID != "" || change.SizeBytes != 0 || change.Provisioning != "" {
			return errors.New("LUN update is patch-only; use new_* fields")
		}
		if change.NewName == nil && change.NewDescription == nil && change.NewBackingVolumeID == nil && change.NewSizeBytes == nil {
			return errors.New("LUN update requires at least one patch field")
		}
		if change.NewName != nil {
			if err := validateSANName("LUN", *change.NewName); err != nil {
				return err
			}
		}
		if change.NewBackingVolumeID != nil && *change.NewBackingVolumeID == "" {
			return errors.New("new_backing_volume_id cannot be empty")
		}
		if change.NewSizeBytes != nil {
			return validateLUNSize(*change.NewSizeBytes)
		}
		return nil
	case san.ActionDelete:
		if change.ID == "" {
			return errors.New("LUN delete requires stable UUID")
		}
		if change.Name != "" || change.Description != "" || change.BackingVolumeID != "" || change.SizeBytes != 0 || change.Provisioning != "" || change.NewName != nil || change.NewDescription != nil || change.NewBackingVolumeID != nil || change.NewSizeBytes != nil {
			return errors.New("LUN delete accepts only stable UUID")
		}
		return nil
	default:
		return errors.New("LUN action must be create, update, or delete")
	}
}

func validateAuthentication(mode string, change *san.TargetChange) error {
	switch mode {
	case san.AuthenticationNone:
		if hasAuthenticationFields(change) {
			return errors.New("none authentication cannot contain CHAP fields")
		}
	case san.AuthenticationCHAP:
		if change.CHAPUser == "" || !validSecretReference(change.CHAPPasswordRef) {
			return errors.New("CHAP authentication requires chap_user and chap_password_ref using env:NAME or vault:<id>")
		}
		if change.MutualCHAPUser != "" || change.MutualCHAPPasswordRef != "" {
			return errors.New("CHAP authentication cannot contain mutual CHAP fields")
		}
	case san.AuthenticationMutualCHAP:
		if change.CHAPUser == "" || change.MutualCHAPUser == "" || !validSecretReference(change.CHAPPasswordRef) || !validSecretReference(change.MutualCHAPPasswordRef) {
			return errors.New("mutual_chap requires both usernames and both password references using env:NAME or vault:<id>")
		}
	default:
		return fmt.Errorf("unsupported target authentication %q", mode)
	}
	return nil
}

func validateCHAPPassword(password string) error {
	if len(password) < 12 || len(password) > 16 {
		return errors.New("CHAP password resolved at apply must be 12 to 16 characters")
	}
	for _, character := range password {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%&^*", character) {
			return errors.New("CHAP password contains a character DSM does not accept")
		}
	}
	return nil
}

func validateSANPlan(plan SANPlan, approvalHash string) error {
	if plan.APIVersion != managementAPIVersion {
		return fmt.Errorf("unsupported SAN plan API version %q", plan.APIVersion)
	}
	canonical, err := canonicalSANRequest(plan.Request)
	if err != nil {
		return err
	}
	if fingerprint(canonical) != fingerprint(plan.Request) {
		return errors.New("SAN plan request is not canonical")
	}
	expected, err := sanPlanHash(plan)
	if err != nil {
		return err
	}
	if plan.Hash == "" || plan.Hash != expected {
		return errors.New("SAN plan hash is invalid")
	}
	if approvalHash != plan.Hash {
		return errors.New("approval hash does not match SAN plan hash")
	}
	return nil
}

func sanPlanHash(plan SANPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

func normalizeSANState(state san.State) (san.State, error) {
	normalized := san.State{Targets: append([]san.Target(nil), state.Targets...), LUNs: append([]san.LUN(nil), state.LUNs...), Mappings: append([]san.Mapping(nil), state.Mappings...)}
	targetIDs, lunIDs := map[string]struct{}{}, map[string]struct{}{}
	for _, target := range normalized.Targets {
		if strings.TrimSpace(target.ID) == "" {
			return san.State{}, errors.New("SAN inventory contains target without stable ID")
		}
		if _, duplicate := targetIDs[target.ID]; duplicate {
			return san.State{}, fmt.Errorf("SAN inventory contains duplicate target ID %q", target.ID)
		}
		targetIDs[target.ID] = struct{}{}
	}
	for _, lun := range normalized.LUNs {
		if strings.TrimSpace(lun.ID) == "" {
			return san.State{}, errors.New("SAN inventory contains LUN without stable UUID")
		}
		if _, duplicate := lunIDs[lun.ID]; duplicate {
			return san.State{}, fmt.Errorf("SAN inventory contains duplicate LUN UUID %q", lun.ID)
		}
		lunIDs[lun.ID] = struct{}{}
	}
	seenMappings := map[string]struct{}{}
	for _, mapping := range normalized.Mappings {
		if _, ok := targetIDs[mapping.TargetID]; !ok {
			return san.State{}, fmt.Errorf("mapping references unknown target ID %q", mapping.TargetID)
		}
		if _, ok := lunIDs[mapping.LUNID]; !ok {
			return san.State{}, fmt.Errorf("mapping references unknown LUN UUID %q", mapping.LUNID)
		}
		key := mapping.TargetID + "\x00" + mapping.LUNID
		if _, duplicate := seenMappings[key]; duplicate {
			return san.State{}, fmt.Errorf("SAN inventory contains duplicate mapping %s:%s", mapping.TargetID, mapping.LUNID)
		}
		seenMappings[key] = struct{}{}
	}
	sort.Slice(normalized.Targets, func(i, j int) bool { return normalized.Targets[i].ID < normalized.Targets[j].ID })
	sort.Slice(normalized.LUNs, func(i, j int) bool { return normalized.LUNs[i].ID < normalized.LUNs[j].ID })
	sort.Slice(normalized.Mappings, func(i, j int) bool {
		if normalized.Mappings[i].TargetID == normalized.Mappings[j].TargetID {
			return normalized.Mappings[i].LUNID < normalized.Mappings[j].LUNID
		}
		return normalized.Mappings[i].TargetID < normalized.Mappings[j].TargetID
	})
	return normalized, nil
}

func validateBackingVolume(state storage.State, id string, needed uint64) (*storage.Volume, error) {
	for index := range state.Volumes {
		volume := &state.Volumes[index]
		if volume.ID != id {
			continue
		}
		if volume.Path == "" {
			return nil, fmt.Errorf("backing volume %q has no DSM path in inventory", id)
		}
		if volume.ReadOnly || !strings.EqualFold(volume.Status, "normal") {
			return nil, fmt.Errorf("backing volume %q is not normal and writable", id)
		}
		if volume.FileSystem != "btrfs" && volume.FileSystem != "ext4" {
			return nil, fmt.Errorf("backing volume %q has unsupported filesystem %q", id, volume.FileSystem)
		}
		if needed > volume.AvailableBytes {
			return nil, fmt.Errorf("backing volume %q has %d available bytes, need %d", id, volume.AvailableBytes, needed)
		}
		return volume, nil
	}
	return nil, fmt.Errorf("backing volume stable ID %q does not exist", id)
}

func setPlanVolume(plan *SANPlan, volume storage.Volume) {
	plan.References.BackingVolumeID = volume.ID
	plan.References.BackingVolumePath = volume.Path
	plan.References.BackingFileSystem = volume.FileSystem
	plan.VolumeFingerprint = fingerprint(struct {
		ID, Path, FileSystem, Status string
		AvailableBytes               uint64
		ReadOnly                     bool
	}{volume.ID, volume.Path, volume.FileSystem, volume.Status, volume.AvailableBytes, volume.ReadOnly})
}

func waitForSANPostcondition(ctx context.Context, client interface {
	SANState(context.Context) (synology.SANState, error)
}, plan SANPlan, resourceID string) (synology.SANState, error) {
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var lastState synology.SANState
	var lastErr error
	for {
		state, err := client.SANState(ctx)
		if err == nil {
			lastState = state
			lastErr = verifySANPostcondition(state, plan, resourceID)
			if lastErr == nil {
				return state, nil
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return lastState, errors.Join(ctx.Err(), lastErr)
		case <-deadline.C:
			return lastState, fmt.Errorf("timed out waiting for SAN postcondition: %w", lastErr)
		case <-ticker.C:
		}
	}
}

func verifySANPostcondition(state san.State, plan SANPlan, resourceID string) error {
	request := plan.Request
	switch request.Resource {
	case san.ResourceTarget:
		change := request.Target
		if request.Action == san.ActionCreate {
			target := findTarget(state, resourceID)
			if target == nil || target.Name != change.Name || target.IQN != change.IQN || target.Authentication != change.Authentication {
				return errors.New("created target stable ID and requested settings are not visible")
			}
			return nil
		}
		target := findTarget(state, change.ID)
		if request.Action == san.ActionDelete {
			if target != nil {
				return errors.New("deleted target stable ID is still visible")
			}
			return nil
		}
		if target == nil {
			return errors.New("updated target stable ID is absent")
		}
		if (change.NewName != nil && target.Name != *change.NewName) ||
			(change.NewIQN != nil && target.IQN != *change.NewIQN) ||
			(change.NewAuthentication != nil && target.Authentication != *change.NewAuthentication) ||
			(change.Enabled != nil && target.Enabled != *change.Enabled) {
			return errors.New("updated target settings do not match the requested patch")
		}
	case san.ResourceLUN:
		change := request.LUN
		if request.Action == san.ActionCreate {
			lun := findLUN(state, resourceID)
			if lun == nil || lun.Name != change.Name || lun.SizeBytes != change.SizeBytes || lun.Provisioning != change.Provisioning ||
				lun.BackingLocation != plan.References.BackingVolumePath || lun.Mapped {
				return errors.New("created LUN stable ID, settings, or unmapped state are not visible")
			}
			return nil
		}
		lun := findLUN(state, change.ID)
		if request.Action == san.ActionDelete {
			if lun != nil {
				return errors.New("deleted LUN stable UUID is still visible")
			}
			return nil
		}
		if lun == nil {
			return errors.New("updated LUN stable UUID is absent")
		}
		if (change.NewName != nil && lun.Name != *change.NewName) ||
			(change.NewDescription != nil && lun.Description != *change.NewDescription) ||
			(change.NewSizeBytes != nil && lun.SizeBytes != *change.NewSizeBytes) ||
			(change.NewBackingVolumeID != nil && lun.BackingLocation != plan.References.BackingVolumePath) {
			return errors.New("updated LUN settings do not match the requested patch")
		}
	case san.ResourceMapping:
		if findTarget(state, request.Mapping.TargetID) == nil || findLUN(state, request.Mapping.LUNID) == nil {
			return errors.New("mapping operation removed an endpoint")
		}
		exists := hasMapping(state, request.Mapping.TargetID, request.Mapping.LUNID)
		if (request.Action == san.ActionAttach && !exists) || (request.Action == san.ActionDetach && exists) {
			return errors.New("mapping graph does not match the requested operation")
		}
	}
	return nil
}

func sanResourceState(state san.State, request san.ChangeRequest, resourceID string) (bool, bool) {
	switch request.Resource {
	case san.ResourceTarget:
		id := request.Target.ID
		if request.Action == san.ActionCreate {
			id = resourceID
			if id == "" {
				if target := findTargetByName(state, request.Target.Name); target != nil {
					id = target.ID
				}
			}
		}
		return findTarget(state, id) != nil, len(mappingsForTarget(state, id)) != 0
	case san.ResourceLUN:
		id := request.LUN.ID
		if request.Action == san.ActionCreate {
			id = resourceID
			if id == "" {
				if lun := findLUNByName(state, request.LUN.Name); lun != nil {
					id = lun.ID
				}
			}
		}
		return findLUN(state, id) != nil, len(mappingsForLUN(state, id)) != 0
	case san.ResourceMapping:
		return findTarget(state, request.Mapping.TargetID) != nil && findLUN(state, request.Mapping.LUNID) != nil, hasMapping(state, request.Mapping.TargetID, request.Mapping.LUNID)
	}
	return false, false
}

func findTarget(state san.State, id string) *san.Target {
	for index := range state.Targets {
		if state.Targets[index].ID == id {
			return &state.Targets[index]
		}
	}
	return nil
}

func findTargetByName(state san.State, name string) *san.Target {
	for index := range state.Targets {
		if strings.EqualFold(state.Targets[index].Name, name) {
			return &state.Targets[index]
		}
	}
	return nil
}

func findTargetByIQN(state san.State, iqn string) *san.Target {
	for index := range state.Targets {
		if strings.EqualFold(state.Targets[index].IQN, iqn) {
			return &state.Targets[index]
		}
	}
	return nil
}

func findLUN(state san.State, id string) *san.LUN {
	for index := range state.LUNs {
		if state.LUNs[index].ID == id {
			return &state.LUNs[index]
		}
	}
	return nil
}

func findLUNByName(state san.State, name string) *san.LUN {
	for index := range state.LUNs {
		if strings.EqualFold(state.LUNs[index].Name, name) {
			return &state.LUNs[index]
		}
	}
	return nil
}

func hasMapping(state san.State, targetID, lunID string) bool {
	for _, mapping := range state.Mappings {
		if mapping.TargetID == targetID && mapping.LUNID == lunID {
			return true
		}
	}
	return false
}

func mappingsForTarget(state san.State, id string) []san.Mapping {
	var result []san.Mapping
	for _, mapping := range state.Mappings {
		if mapping.TargetID == id {
			result = append(result, mapping)
		}
	}
	return result
}

func mappingsForLUN(state san.State, id string) []san.Mapping {
	var result []san.Mapping
	for _, mapping := range state.Mappings {
		if mapping.LUNID == id {
			result = append(result, mapping)
		}
	}
	return result
}

func activeTargetsForLUN(state san.State, id string) int {
	count := 0
	for _, mapping := range mappingsForLUN(state, id) {
		if target := findTarget(state, mapping.TargetID); target != nil && target.ConnectedSessions != 0 {
			count++
		}
	}
	return count
}

func volumeIDForPath(state storage.State, path string) string {
	for _, volume := range state.Volumes {
		if volume.Path == path {
			return volume.ID
		}
	}
	return ""
}

func validateSANName(resource, name string) error {
	if name == "" {
		return fmt.Errorf("%s name is required", resource)
	}
	if len(name) > 128 {
		return fmt.Errorf("%s name exceeds 128 bytes", resource)
	}
	for _, character := range name {
		if character < 0x20 || character == 0x7f {
			return fmt.Errorf("%s name contains a control character", resource)
		}
	}
	return nil
}

func validateIQN(value string) error {
	if value == "" || len(value) > 128 || !strings.HasPrefix(strings.ToLower(value), "iqn.") {
		return errors.New("target IQN must begin with iqn. and be at most 128 bytes")
	}
	return nil
}

func validateLUNSize(value uint64) error {
	if value < minimumLUNSizeBytes || value%minimumLUNSizeBytes != 0 {
		return fmt.Errorf("LUN size must be a whole GiB and at least %d bytes", minimumLUNSizeBytes)
	}
	return nil
}

func canonicalAuthentication(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func hasAuthenticationFields(change *san.TargetChange) bool {
	return change.CHAPUser != "" || change.CHAPPasswordRef != "" || change.MutualCHAPUser != "" || change.MutualCHAPPasswordRef != ""
}

func trimStringPointer(value *string) {
	if value != nil {
		*value = strings.TrimSpace(*value)
	}
}
