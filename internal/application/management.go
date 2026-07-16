package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/identity"
	"github.com/ychiu1211/dsmctl/internal/domain/share"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const managementAPIVersion = "dsmctl.io/v1alpha1"

var (
	managedNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	expirationPattern  = regexp.MustCompile(`^\d{4}/\d{1,2}/\d{1,2}$`)
)

type ChangePrecondition struct {
	ExpectedExists bool   `json:"expected_exists" jsonschema:"Whether the resource must exist when apply begins"`
	ResourceID     string `json:"resource_id,omitempty" jsonschema:"Stable DSM identifier observed during planning"`
	Fingerprint    string `json:"fingerprint,omitempty" jsonschema:"Hash of the observed resource state"`
}

type IdentityPlan struct {
	APIVersion   string                 `json:"api_version" jsonschema:"Plan schema version"`
	NAS          string                 `json:"nas" jsonschema:"NAS profile selected during planning"`
	Request      identity.ChangeRequest `json:"request" jsonschema:"Validated identity change intent"`
	Precondition ChangePrecondition     `json:"precondition" jsonschema:"Observed state that must still match during apply"`
	Destructive  bool                   `json:"destructive" jsonschema:"Whether the plan can remove or restrict access"`
	Risk         string                 `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Summary      []string               `json:"summary" jsonschema:"Human-readable actions the plan will perform"`
	Hash         string                 `json:"hash" jsonschema:"SHA-256 approval hash covering the plan and precondition"`
}

type SharePlan struct {
	APIVersion   string              `json:"api_version" jsonschema:"Plan schema version"`
	NAS          string              `json:"nas" jsonschema:"NAS profile selected during planning"`
	Request      share.ChangeRequest `json:"request" jsonschema:"Validated shared-folder change intent"`
	Precondition ChangePrecondition  `json:"precondition" jsonschema:"Observed state that must still match during apply"`
	Destructive  bool                `json:"destructive" jsonschema:"Whether the plan can remove data or restrict access"`
	Risk         string              `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Summary      []string            `json:"summary" jsonschema:"Human-readable actions the plan will perform"`
	Hash         string              `json:"hash" jsonschema:"SHA-256 approval hash covering the plan and precondition"`
}

type IdentityApplyResult struct {
	NAS       string                          `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash  string                          `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied   bool                            `json:"applied" jsonschema:"Whether DSM accepted and postcondition verification passed"`
	Operation synology.IdentityMutationResult `json:"operation" jsonschema:"Applied DSM identity operation"`
}

type ShareApplyResult struct {
	NAS       string                       `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash  string                       `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied   bool                         `json:"applied" jsonschema:"Whether DSM accepted and postcondition verification passed"`
	Operation synology.ShareMutationResult `json:"operation" jsonschema:"Applied DSM shared-folder operation"`
}

func (s *Service) PlanIdentityChange(ctx context.Context, requestedNAS string, request identity.ChangeRequest) (IdentityPlan, error) {
	if err := validateIdentityRequest(request); err != nil {
		return IdentityPlan{}, err
	}
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return IdentityPlan{}, err
	}
	query := identityQueryForRequest(request)
	state, err := client.IdentityState(ctx, query)
	if err != nil {
		return IdentityPlan{}, authenticationError(name, err)
	}
	precondition, err := identityPrecondition(state, request)
	if err != nil {
		return IdentityPlan{}, err
	}
	destructive := identityChangeIsDestructive(state, request)
	plan := IdentityPlan{
		APIVersion:   managementAPIVersion,
		NAS:          name,
		Request:      request,
		Precondition: precondition,
		Destructive:  destructive,
		Risk:         riskLevel(destructive),
		Summary:      identitySummary(state, request),
	}
	plan.Hash, err = identityPlanHash(plan)
	if err != nil {
		return IdentityPlan{}, err
	}
	return plan, nil
}

func (s *Service) ApplyIdentityPlan(ctx context.Context, plan IdentityPlan, approveHash string) (IdentityApplyResult, error) {
	if err := validateIdentityPlan(plan, approveHash); err != nil {
		return IdentityApplyResult{}, err
	}
	current, err := s.PlanIdentityChange(ctx, plan.NAS, plan.Request)
	if err != nil {
		return IdentityApplyResult{}, fmt.Errorf("identity plan precondition no longer holds: %w", err)
	}
	if current.Precondition != plan.Precondition {
		return IdentityApplyResult{}, fmt.Errorf("identity plan precondition changed; create a new plan")
	}
	if current.Hash != plan.Hash {
		return IdentityApplyResult{}, fmt.Errorf("identity plan is not the canonical plan for the current request and state; create a new plan")
	}
	_, client, err := s.manager.Client(ctx, plan.NAS)
	if err != nil {
		return IdentityApplyResult{}, err
	}
	password := ""
	if plan.Request.User != nil && plan.Request.User.CredentialRef != "" {
		password, err = s.secretReferences.ResolveSecret(ctx, plan.Request.User.CredentialRef)
		if err != nil {
			return IdentityApplyResult{}, fmt.Errorf("resolve user password reference: %w", err)
		}
	}
	operation, err := client.ApplyIdentityChange(ctx, plan.Request, password)
	if err != nil {
		return IdentityApplyResult{}, authenticationError(plan.NAS, err)
	}
	state, err := client.IdentityState(ctx, identityQueryForRequest(plan.Request))
	if err != nil {
		return IdentityApplyResult{}, fmt.Errorf("verify identity change: %w", err)
	}
	if err := verifyIdentityPostcondition(state, plan.Request); err != nil {
		return IdentityApplyResult{}, err
	}
	return IdentityApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operation: operation}, nil
}

func (s *Service) PlanShareChange(ctx context.Context, requestedNAS string, request share.ChangeRequest) (SharePlan, error) {
	if err := validateShareRequest(request); err != nil {
		return SharePlan{}, err
	}
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return SharePlan{}, err
	}
	includePermissions := request.Resource == share.ResourcePermission
	state, err := client.ShareState(ctx, includePermissions)
	if err != nil {
		return SharePlan{}, authenticationError(name, err)
	}
	var identities synology.IdentityState
	if includePermissions {
		identities, err = client.IdentityState(ctx)
		if err != nil {
			return SharePlan{}, authenticationError(name, err)
		}
	}
	precondition, err := sharePrecondition(state, identities, request)
	if err != nil {
		return SharePlan{}, err
	}
	destructive := request.Action == share.ActionDelete || request.Resource == share.ResourcePermission
	plan := SharePlan{
		APIVersion:   managementAPIVersion,
		NAS:          name,
		Request:      request,
		Precondition: precondition,
		Destructive:  destructive,
		Risk:         riskLevel(destructive),
		Summary:      shareSummary(request),
	}
	plan.Hash, err = sharePlanHash(plan)
	if err != nil {
		return SharePlan{}, err
	}
	return plan, nil
}

func (s *Service) ApplySharePlan(ctx context.Context, plan SharePlan, approveHash string) (ShareApplyResult, error) {
	if err := validateSharePlan(plan, approveHash); err != nil {
		return ShareApplyResult{}, err
	}
	current, err := s.PlanShareChange(ctx, plan.NAS, plan.Request)
	if err != nil {
		return ShareApplyResult{}, fmt.Errorf("shared-folder plan precondition no longer holds: %w", err)
	}
	if current.Precondition != plan.Precondition {
		return ShareApplyResult{}, fmt.Errorf("shared-folder plan precondition changed; create a new plan")
	}
	if current.Hash != plan.Hash {
		return ShareApplyResult{}, fmt.Errorf("shared-folder plan is not the canonical plan for the current request and state; create a new plan")
	}
	_, client, err := s.manager.Client(ctx, plan.NAS)
	if err != nil {
		return ShareApplyResult{}, err
	}
	operation, err := client.ApplyShareChange(ctx, plan.Request)
	if err != nil {
		return ShareApplyResult{}, authenticationError(plan.NAS, err)
	}
	state, err := client.ShareState(ctx, plan.Request.Resource == share.ResourcePermission)
	if err != nil {
		return ShareApplyResult{}, fmt.Errorf("verify shared-folder change: %w", err)
	}
	if err := verifySharePostcondition(state, plan.Request); err != nil {
		return ShareApplyResult{}, err
	}
	return ShareApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operation: operation}, nil
}

func identityPrecondition(state synology.IdentityState, request identity.ChangeRequest) (ChangePrecondition, error) {
	switch request.Resource {
	case identity.ResourceUser:
		current, found := findUser(state.Users, request.User.Name)
		if request.Action == identity.ActionCreate {
			if found {
				return ChangePrecondition{}, fmt.Errorf("user %q already exists", request.User.Name)
			}
			return ChangePrecondition{ExpectedExists: false}, nil
		}
		if !found {
			return ChangePrecondition{}, fmt.Errorf("user %q does not exist", request.User.Name)
		}
		return ChangePrecondition{ExpectedExists: true, ResourceID: current.ID, Fingerprint: fingerprint(current)}, nil
	case identity.ResourceGroup:
		current, found := findGroup(state.Groups, request.Group.Name)
		if request.Action == identity.ActionCreate {
			if found {
				return ChangePrecondition{}, fmt.Errorf("group %q already exists", request.Group.Name)
			}
			return ChangePrecondition{ExpectedExists: false}, nil
		}
		if !found {
			return ChangePrecondition{}, fmt.Errorf("group %q does not exist", request.Group.Name)
		}
		return ChangePrecondition{ExpectedExists: true, ResourceID: current.ID, Fingerprint: fingerprint(current)}, nil
	case identity.ResourceMembership:
		user, found := findUser(state.Users, request.Membership.User)
		if !found {
			return ChangePrecondition{}, fmt.Errorf("user %q does not exist", request.Membership.User)
		}
		for _, name := range request.Membership.Groups {
			if _, found := findGroup(state.Groups, name); !found {
				return ChangePrecondition{}, fmt.Errorf("group %q does not exist", name)
			}
		}
		membership, found := findMembership(state.Memberships, request.Membership.User)
		if !found {
			return ChangePrecondition{}, fmt.Errorf("membership state for user %q was not returned", request.Membership.User)
		}
		return ChangePrecondition{ExpectedExists: true, ResourceID: user.ID, Fingerprint: fingerprint(membership)}, nil
	case identity.ResourceQuota:
		resourceID, err := identityPrincipalID(state, request.Quota.PrincipalType, request.Quota.Principal)
		if err != nil {
			return ChangePrecondition{}, err
		}
		assignment, found := findQuotaAssignment(state.Quotas, request.Quota.PrincipalType, request.Quota.Principal)
		if !found {
			return ChangePrecondition{}, fmt.Errorf("quota state for %s %q was not returned", request.Quota.PrincipalType, request.Quota.Principal)
		}
		observed := make([]identity.QuotaLimit, 0, len(request.Quota.Limits))
		for _, desired := range request.Quota.Limits {
			limit, found := findQuotaLimit(assignment, desired.TargetType, desired.Target)
			if !found {
				return ChangePrecondition{}, fmt.Errorf("quota target %s %q does not exist or does not support per-principal quotas", desired.TargetType, desired.Target)
			}
			observed = append(observed, limit)
		}
		sort.Slice(observed, func(i, j int) bool {
			return observed[i].TargetType+":"+observed[i].Target < observed[j].TargetType+":"+observed[j].Target
		})
		return ChangePrecondition{ExpectedExists: true, ResourceID: resourceID, Fingerprint: fingerprint(observed)}, nil
	case identity.ResourceApplicationPrivilege:
		resourceID, err := identityPrincipalID(state, request.ApplicationPrivilege.PrincipalType, request.ApplicationPrivilege.Principal)
		if err != nil {
			return ChangePrecondition{}, err
		}
		assignment, found := findApplicationPrivilegeAssignment(state.ApplicationPrivileges, request.ApplicationPrivilege.PrincipalType, request.ApplicationPrivilege.Principal)
		if !found {
			return ChangePrecondition{}, fmt.Errorf("application privilege state for %s %q was not returned", request.ApplicationPrivilege.PrincipalType, request.ApplicationPrivilege.Principal)
		}
		observed := make([]identity.ApplicationPermission, 0, len(request.ApplicationPrivilege.Permissions))
		for _, desired := range request.ApplicationPrivilege.Permissions {
			if _, found := findApplication(state.Applications, desired.ApplicationID); !found {
				return ChangePrecondition{}, fmt.Errorf("application %q does not exist", desired.ApplicationID)
			}
			observed = append(observed, applicationPermissionFor(assignment, desired.ApplicationID))
		}
		sort.Slice(observed, func(i, j int) bool { return observed[i].ApplicationID < observed[j].ApplicationID })
		return ChangePrecondition{ExpectedExists: true, ResourceID: resourceID, Fingerprint: fingerprint(observed)}, nil
	default:
		return ChangePrecondition{}, fmt.Errorf("unsupported identity resource %q", request.Resource)
	}
}

func sharePrecondition(state synology.ShareState, identities synology.IdentityState, request share.ChangeRequest) (ChangePrecondition, error) {
	if request.Resource == share.ResourceShare {
		current, found := findShare(state.Shares, request.Share.Name)
		if request.Action == share.ActionCreate {
			if found {
				return ChangePrecondition{}, fmt.Errorf("shared folder %q already exists", request.Share.Name)
			}
			return ChangePrecondition{ExpectedExists: false}, nil
		}
		if !found {
			return ChangePrecondition{}, fmt.Errorf("shared folder %q does not exist", request.Share.Name)
		}
		return ChangePrecondition{ExpectedExists: true, ResourceID: current.UUID, Fingerprint: fingerprint(current)}, nil
	}
	change := request.Permission
	if !principalExists(identities, change.PrincipalType, change.Principal) {
		return ChangePrecondition{}, fmt.Errorf("%s %q does not exist", change.PrincipalType, change.Principal)
	}
	type observedPermission struct {
		ShareName string           `json:"share_name"`
		Binding   share.Permission `json:"binding"`
	}
	bindings := make([]observedPermission, 0, len(change.Permissions))
	for _, grant := range change.Permissions {
		folder, found := findShare(state.Shares, grant.ShareName)
		if !found {
			return ChangePrecondition{}, fmt.Errorf("shared folder %q does not exist", grant.ShareName)
		}
		bindings = append(bindings, observedPermission{
			ShareName: grant.ShareName,
			Binding:   permissionFor(folder, change.PrincipalType, change.Principal),
		})
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].ShareName < bindings[j].ShareName })
	return ChangePrecondition{ExpectedExists: true, ResourceID: change.PrincipalType + ":" + change.Principal, Fingerprint: fingerprint(bindings)}, nil
}

func validateIdentityRequest(request identity.ChangeRequest) error {
	switch request.Resource {
	case identity.ResourceUser:
		if request.Action != identity.ActionCreate && request.Action != identity.ActionUpdate && request.Action != identity.ActionDelete {
			return fmt.Errorf("user resource does not support action %q", request.Action)
		}
		if request.User == nil || request.Group != nil || request.Membership != nil || request.Quota != nil || request.ApplicationPrivilege != nil {
			return fmt.Errorf("user resource requires only the user object")
		}
		if err := validateManagedName("user", request.User.Name); err != nil {
			return err
		}
		if protectedUser(request.User.Name) {
			return fmt.Errorf("user %q is reserved and cannot be managed", request.User.Name)
		}
		if request.User.NewName != nil {
			if err := validateManagedName("new user", *request.User.NewName); err != nil {
				return err
			}
			if protectedUser(*request.User.NewName) {
				return fmt.Errorf("user %q is reserved and cannot be managed", *request.User.NewName)
			}
		}
		if request.Action == identity.ActionCreate && request.User.CredentialRef == "" {
			return fmt.Errorf("creating a user requires credential_ref")
		}
		if request.User.CredentialRef != "" && !strings.HasPrefix(request.User.CredentialRef, "env:") {
			return fmt.Errorf("credential_ref must use env:NAME")
		}
		if request.User.Expired != nil && *request.User.Expired != "normal" && *request.User.Expired != "now" && !expirationPattern.MatchString(*request.User.Expired) {
			return fmt.Errorf("expired must be normal, now, or YYYY/M/D")
		}
		if request.Action == identity.ActionUpdate && !userHasUpdates(*request.User) {
			return fmt.Errorf("user update contains no changed fields")
		}
	case identity.ResourceGroup:
		if request.Action != identity.ActionCreate && request.Action != identity.ActionUpdate && request.Action != identity.ActionDelete {
			return fmt.Errorf("group resource does not support action %q", request.Action)
		}
		if request.Group == nil || request.User != nil || request.Membership != nil || request.Quota != nil || request.ApplicationPrivilege != nil {
			return fmt.Errorf("group resource requires only the group object")
		}
		if err := validateManagedName("group", request.Group.Name); err != nil {
			return err
		}
		if protectedGroup(request.Group.Name) {
			return fmt.Errorf("group %q is reserved and cannot be managed", request.Group.Name)
		}
		if request.Group.NewName != nil {
			if err := validateManagedName("new group", *request.Group.NewName); err != nil {
				return err
			}
			if protectedGroup(*request.Group.NewName) {
				return fmt.Errorf("group %q is reserved and cannot be managed", *request.Group.NewName)
			}
		}
		if request.Action == identity.ActionUpdate && request.Group.NewName == nil && request.Group.Description == nil {
			return fmt.Errorf("group update contains no changed fields")
		}
	case identity.ResourceMembership:
		if request.Action != identity.ActionSet || request.Membership == nil || request.User != nil || request.Group != nil || request.Quota != nil || request.ApplicationPrivilege != nil {
			return fmt.Errorf("membership resource requires action set and only the membership object")
		}
		if err := validateManagedName("user", request.Membership.User); err != nil {
			return err
		}
		if protectedUser(request.Membership.User) {
			return fmt.Errorf("user %q is reserved and cannot be managed", request.Membership.User)
		}
		if len(request.Membership.Groups) == 0 {
			return fmt.Errorf("membership groups must include at least the mandatory users group")
		}
		seen := make(map[string]struct{})
		includesUsers := false
		for _, group := range request.Membership.Groups {
			if err := validateManagedName("group", group); err != nil {
				return err
			}
			key := strings.ToLower(group)
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("group %q appears more than once", group)
			}
			seen[key] = struct{}{}
			if strings.EqualFold(group, "users") {
				includesUsers = true
			}
		}
		if !includesUsers {
			return fmt.Errorf("membership groups must include the mandatory users group")
		}
	case identity.ResourceQuota:
		if request.Action != identity.ActionSet || request.Quota == nil || request.User != nil || request.Group != nil || request.Membership != nil || request.ApplicationPrivilege != nil {
			return fmt.Errorf("quota resource requires action set and only the quota object")
		}
		if err := validatePrincipal(request.Quota.PrincipalType, request.Quota.Principal); err != nil {
			return err
		}
		if len(request.Quota.Limits) == 0 {
			return fmt.Errorf("at least one quota limit is required")
		}
		seen := make(map[string]struct{})
		for _, limit := range request.Quota.Limits {
			if limit.QuotaMiB < 0 {
				return fmt.Errorf("quota for %q cannot be negative", limit.Target)
			}
			switch limit.TargetType {
			case identity.QuotaTargetVolume:
				if !strings.HasPrefix(limit.Target, "/volume") {
					return fmt.Errorf("volume quota target %q must begin with /volume", limit.Target)
				}
			case identity.QuotaTargetShare:
				if err := validateManagedName("shared folder", limit.Target); err != nil {
					return err
				}
			default:
				return fmt.Errorf("quota target_type must be volume or share")
			}
			key := limit.TargetType + ":" + strings.ToLower(limit.Target)
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("quota target %q appears more than once", limit.Target)
			}
			seen[key] = struct{}{}
		}
	case identity.ResourceApplicationPrivilege:
		if request.Action != identity.ActionSet || request.ApplicationPrivilege == nil || request.User != nil || request.Group != nil || request.Membership != nil || request.Quota != nil {
			return fmt.Errorf("application_privilege resource requires action set and only the application_privilege object")
		}
		if err := validatePrincipal(request.ApplicationPrivilege.PrincipalType, request.ApplicationPrivilege.Principal); err != nil {
			return err
		}
		if len(request.ApplicationPrivilege.Permissions) == 0 {
			return fmt.Errorf("at least one application permission is required")
		}
		seen := make(map[string]struct{})
		for _, permission := range request.ApplicationPrivilege.Permissions {
			if strings.TrimSpace(permission.ApplicationID) == "" {
				return fmt.Errorf("application_id is required")
			}
			key := strings.ToLower(permission.ApplicationID)
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("application %q appears more than once", permission.ApplicationID)
			}
			seen[key] = struct{}{}
			if permission.Access != identity.ApplicationAccessAllow && permission.Access != identity.ApplicationAccessDeny && permission.Access != identity.ApplicationAccessInherit {
				return fmt.Errorf("application %q access must be allow, deny, or inherit", permission.ApplicationID)
			}
		}
	default:
		return fmt.Errorf("unsupported identity resource %q", request.Resource)
	}
	return nil
}

func validateShareRequest(request share.ChangeRequest) error {
	switch request.Resource {
	case share.ResourceShare:
		if request.Share == nil || request.Permission != nil {
			return fmt.Errorf("share resource requires only the share object")
		}
		if request.Action != share.ActionCreate && request.Action != share.ActionUpdate && request.Action != share.ActionDelete {
			return fmt.Errorf("unsupported shared-folder action %q", request.Action)
		}
		if err := validateManagedName("shared folder", request.Share.Name); err != nil {
			return err
		}
		if protectedShare(request.Share.Name) {
			return fmt.Errorf("shared folder %q is reserved and cannot be managed", request.Share.Name)
		}
		if request.Share.NewName != nil {
			if err := validateManagedName("new shared folder", *request.Share.NewName); err != nil {
				return err
			}
			if protectedShare(*request.Share.NewName) {
				return fmt.Errorf("shared folder %q is reserved and cannot be managed", *request.Share.NewName)
			}
		}
		if request.Action == share.ActionCreate && !strings.HasPrefix(request.Share.VolumePath, "/volume") {
			return fmt.Errorf("creating a shared folder requires a volume_path beginning with /volume")
		}
		if request.Action == share.ActionUpdate && request.Share.VolumePath != "" {
			return fmt.Errorf("moving a shared folder between volumes is not supported")
		}
		if request.Action == share.ActionUpdate && !shareHasUpdates(*request.Share) {
			return fmt.Errorf("shared-folder update contains no changed fields")
		}
	case share.ResourcePermission:
		if request.Action != share.ActionSet || request.Permission == nil || request.Share != nil {
			return fmt.Errorf("permission resource requires action set and only the permission object")
		}
		if request.Permission.PrincipalType != share.PrincipalUser && request.Permission.PrincipalType != share.PrincipalGroup {
			return fmt.Errorf("principal_type must be user or group")
		}
		if err := validateManagedName("principal", request.Permission.Principal); err != nil {
			return err
		}
		if len(request.Permission.Permissions) == 0 {
			return fmt.Errorf("at least one permission change is required")
		}
		seen := make(map[string]struct{})
		for _, grant := range request.Permission.Permissions {
			if err := validateManagedName("shared folder", grant.ShareName); err != nil {
				return err
			}
			if _, duplicate := seen[grant.ShareName]; duplicate {
				return fmt.Errorf("shared folder %q appears more than once", grant.ShareName)
			}
			seen[grant.ShareName] = struct{}{}
			if grant.Access != share.AccessNone && grant.Access != share.AccessRead && grant.Access != share.AccessWrite && grant.Access != share.AccessDeny {
				return fmt.Errorf("share %q has unsupported access %q", grant.ShareName, grant.Access)
			}
		}
	default:
		return fmt.Errorf("unsupported share resource %q", request.Resource)
	}
	return nil
}

func validateIdentityPlan(plan IdentityPlan, approveHash string) error {
	if plan.APIVersion != managementAPIVersion {
		return fmt.Errorf("unsupported identity plan API version %q", plan.APIVersion)
	}
	if err := validateIdentityRequest(plan.Request); err != nil {
		return err
	}
	expected, err := identityPlanHash(plan)
	if err != nil {
		return err
	}
	if plan.Hash == "" || plan.Hash != expected {
		return fmt.Errorf("identity plan hash is invalid")
	}
	if approveHash != plan.Hash {
		return fmt.Errorf("approval hash does not match identity plan hash")
	}
	return nil
}

func validateSharePlan(plan SharePlan, approveHash string) error {
	if plan.APIVersion != managementAPIVersion {
		return fmt.Errorf("unsupported shared-folder plan API version %q", plan.APIVersion)
	}
	if err := validateShareRequest(plan.Request); err != nil {
		return err
	}
	expected, err := sharePlanHash(plan)
	if err != nil {
		return err
	}
	if plan.Hash == "" || plan.Hash != expected {
		return fmt.Errorf("shared-folder plan hash is invalid")
	}
	if approveHash != plan.Hash {
		return fmt.Errorf("approval hash does not match shared-folder plan hash")
	}
	return nil
}

func identityPlanHash(plan IdentityPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

func sharePlanHash(plan SharePlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

func hashJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode plan hash input: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

func fingerprint(value any) string {
	encoded, _ := json.Marshal(value)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

func verifyIdentityPostcondition(state synology.IdentityState, request identity.ChangeRequest) error {
	name := userNameAfter(request)
	if request.Resource == identity.ResourceUser {
		_, found := findUser(state.Users, name)
		if request.Action == identity.ActionDelete {
			found = hasUser(state.Users, request.User.Name)
			if found {
				return fmt.Errorf("user %q still exists after delete", request.User.Name)
			}
			return nil
		}
		if !found {
			return fmt.Errorf("user %q was not found after %s", name, request.Action)
		}
		current, _ := findUser(state.Users, name)
		if request.User.Description != nil && current.Description != *request.User.Description {
			return fmt.Errorf("user %q description was not applied", name)
		}
		if request.User.Email != nil && current.Email != *request.User.Email {
			return fmt.Errorf("user %q email was not applied", name)
		}
		if request.User.PasswordNeverExpires != nil && current.PasswordNeverExpires != *request.User.PasswordNeverExpires {
			return fmt.Errorf("user %q password expiration setting was not applied", name)
		}
		if request.User.Expired != nil {
			if *request.User.Expired == "normal" && current.Expired {
				return fmt.Errorf("user %q remains expired", name)
			}
			if *request.User.Expired == "now" && !current.Expired {
				return fmt.Errorf("user %q was not expired", name)
			}
		}
		return nil
	}
	switch request.Resource {
	case identity.ResourceGroup:
		name = groupNameAfter(request)
		if request.Action == identity.ActionDelete {
			if _, found := findGroup(state.Groups, request.Group.Name); found {
				return fmt.Errorf("group %q still exists after delete", request.Group.Name)
			}
			return nil
		}
		current, found := findGroup(state.Groups, name)
		if !found {
			return fmt.Errorf("group %q was not found after %s", name, request.Action)
		}
		if request.Group.Description != nil && current.Description != *request.Group.Description {
			return fmt.Errorf("group %q description was not applied", name)
		}
		return nil
	case identity.ResourceMembership:
		current, found := findMembership(state.Memberships, request.Membership.User)
		if !found {
			return fmt.Errorf("membership state for user %q was not returned after apply", request.Membership.User)
		}
		if !sameStringSet(current.Groups, request.Membership.Groups) {
			return fmt.Errorf("memberships for user %q are %v after apply, want %v", request.Membership.User, current.Groups, request.Membership.Groups)
		}
		return nil
	case identity.ResourceQuota:
		current, found := findQuotaAssignment(state.Quotas, request.Quota.PrincipalType, request.Quota.Principal)
		if !found {
			return fmt.Errorf("quota state for %s %q was not returned after apply", request.Quota.PrincipalType, request.Quota.Principal)
		}
		for _, desired := range request.Quota.Limits {
			actual := quotaLimitFor(current, desired.TargetType, desired.Target)
			if actual.QuotaMiB != desired.QuotaMiB {
				return fmt.Errorf("quota for %s %q on %s %q is %d MiB after apply, want %d MiB", request.Quota.PrincipalType, request.Quota.Principal, desired.TargetType, desired.Target, actual.QuotaMiB, desired.QuotaMiB)
			}
		}
		return nil
	case identity.ResourceApplicationPrivilege:
		current, found := findApplicationPrivilegeAssignment(state.ApplicationPrivileges, request.ApplicationPrivilege.PrincipalType, request.ApplicationPrivilege.Principal)
		if !found {
			return fmt.Errorf("application privilege state for %s %q was not returned after apply", request.ApplicationPrivilege.PrincipalType, request.ApplicationPrivilege.Principal)
		}
		for _, desired := range request.ApplicationPrivilege.Permissions {
			actual := applicationPermissionFor(current, desired.ApplicationID)
			if actual.Access != desired.Access {
				return fmt.Errorf("application %q access for %s %q is %q after apply, want %q", desired.ApplicationID, request.ApplicationPrivilege.PrincipalType, request.ApplicationPrivilege.Principal, actual.Access, desired.Access)
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported identity resource %q", request.Resource)
	}
}

func verifySharePostcondition(state synology.ShareState, request share.ChangeRequest) error {
	if request.Resource == share.ResourceShare {
		name := request.Share.Name
		if request.Share.NewName != nil {
			name = *request.Share.NewName
		}
		if request.Action == share.ActionDelete {
			if _, found := findShare(state.Shares, request.Share.Name); found {
				return fmt.Errorf("shared folder %q still exists after delete", request.Share.Name)
			}
			return nil
		}
		current, found := findShare(state.Shares, name)
		if !found {
			return fmt.Errorf("shared folder %q was not found after %s", name, request.Action)
		}
		if request.Share.Description != nil && current.Description != *request.Share.Description {
			return fmt.Errorf("shared folder %q description was not applied", name)
		}
		if request.Share.Hidden != nil && current.Hidden != *request.Share.Hidden {
			return fmt.Errorf("shared folder %q hidden setting was not applied", name)
		}
		if request.Share.QuotaMiB != nil && current.QuotaBytes != *request.Share.QuotaMiB*1024*1024 {
			return fmt.Errorf("shared folder %q quota was not applied", name)
		}
		if request.Action == share.ActionCreate && current.VolumePath != request.Share.VolumePath {
			return fmt.Errorf("shared folder %q was created on %q, want %q", name, current.VolumePath, request.Share.VolumePath)
		}
		return nil
	}
	for _, grant := range request.Permission.Permissions {
		folder, found := findShare(state.Shares, grant.ShareName)
		if !found {
			return fmt.Errorf("shared folder %q disappeared while verifying permissions", grant.ShareName)
		}
		binding := permissionFor(folder, request.Permission.PrincipalType, request.Permission.Principal)
		if binding.Access != grant.Access {
			return fmt.Errorf("permission for %s %q on %q is %q after apply, want %q", request.Permission.PrincipalType, request.Permission.Principal, grant.ShareName, binding.Access, grant.Access)
		}
	}
	return nil
}

func findUser(users []identity.User, name string) (identity.User, bool) {
	for _, user := range users {
		if strings.EqualFold(user.Name, name) {
			return user, true
		}
	}
	return identity.User{}, false
}

func hasUser(users []identity.User, name string) bool {
	_, found := findUser(users, name)
	return found
}

func findGroup(groups []identity.Group, name string) (identity.Group, bool) {
	for _, group := range groups {
		if strings.EqualFold(group.Name, name) {
			return group, true
		}
	}
	return identity.Group{}, false
}

func findMembership(memberships []identity.Membership, user string) (identity.Membership, bool) {
	for _, membership := range memberships {
		if strings.EqualFold(membership.User, user) {
			return membership, true
		}
	}
	return identity.Membership{}, false
}

func findQuotaAssignment(quotas []identity.PrincipalQuota, principalType, principal string) (identity.PrincipalQuota, bool) {
	for _, quota := range quotas {
		if quota.PrincipalType == principalType && strings.EqualFold(quota.Principal, principal) {
			return quota, true
		}
	}
	return identity.PrincipalQuota{}, false
}

func quotaLimitFor(quota identity.PrincipalQuota, targetType, target string) identity.QuotaLimit {
	limit, found := findQuotaLimit(quota, targetType, target)
	if found {
		return limit
	}
	return identity.QuotaLimit{TargetType: targetType, Target: target, QuotaMiB: 0}
}

func findQuotaLimit(quota identity.PrincipalQuota, targetType, target string) (identity.QuotaLimit, bool) {
	for _, limit := range quota.Limits {
		if limit.TargetType == targetType && strings.EqualFold(limit.Target, target) {
			return limit, true
		}
	}
	return identity.QuotaLimit{}, false
}

func findApplication(applications []identity.Application, id string) (identity.Application, bool) {
	for _, application := range applications {
		if strings.EqualFold(application.ID, id) {
			return application, true
		}
	}
	return identity.Application{}, false
}

func findApplicationPrivilegeAssignment(assignments []identity.ApplicationPrivilegeAssignment, principalType, principal string) (identity.ApplicationPrivilegeAssignment, bool) {
	for _, assignment := range assignments {
		if assignment.PrincipalType == principalType && strings.EqualFold(assignment.Principal, principal) {
			return assignment, true
		}
	}
	return identity.ApplicationPrivilegeAssignment{}, false
}

func applicationPermissionFor(assignment identity.ApplicationPrivilegeAssignment, applicationID string) identity.ApplicationPermission {
	for _, permission := range assignment.Permissions {
		if strings.EqualFold(permission.ApplicationID, applicationID) {
			return permission
		}
	}
	return identity.ApplicationPermission{ApplicationID: applicationID, Access: identity.ApplicationAccessInherit}
}

func findShare(shares []share.SharedFolder, name string) (share.SharedFolder, bool) {
	for _, folder := range shares {
		if strings.EqualFold(folder.Name, name) {
			return folder, true
		}
	}
	return share.SharedFolder{}, false
}

func permissionFor(folder share.SharedFolder, principalType, principal string) share.Permission {
	for _, permission := range folder.Permissions {
		if permission.PrincipalType == principalType && strings.EqualFold(permission.Principal, principal) {
			return permission
		}
	}
	return share.Permission{PrincipalType: principalType, Principal: principal, Access: share.AccessNone}
}

func principalExists(state synology.IdentityState, principalType, principal string) bool {
	if principalType == share.PrincipalUser {
		_, found := findUser(state.Users, principal)
		return found
	}
	_, found := findGroup(state.Groups, principal)
	return found
}

func identityPrincipalID(state synology.IdentityState, principalType, principal string) (string, error) {
	if principalType == identity.PrincipalUser {
		value, found := findUser(state.Users, principal)
		if !found {
			return "", fmt.Errorf("user %q does not exist", principal)
		}
		return value.ID, nil
	}
	value, found := findGroup(state.Groups, principal)
	if !found {
		return "", fmt.Errorf("group %q does not exist", principal)
	}
	return value.ID, nil
}

func validateManagedName(label, name string) error {
	if !managedNamePattern.MatchString(strings.TrimSpace(name)) {
		return fmt.Errorf("%s name %q must start with a letter or number and contain at most 64 letters, numbers, '.', '_' or '-'", label, name)
	}
	return nil
}

func validatePrincipal(principalType, principal string) error {
	if principalType != identity.PrincipalUser && principalType != identity.PrincipalGroup {
		return fmt.Errorf("principal_type must be user or group")
	}
	if err := validateManagedName("principal", principal); err != nil {
		return err
	}
	if principalType == identity.PrincipalUser && protectedUser(principal) {
		return fmt.Errorf("user %q is reserved and cannot be managed", principal)
	}
	return nil
}

func protectedUser(name string) bool {
	return stringInFold(name, "admin", "guest", "root")
}

func protectedGroup(name string) bool {
	return stringInFold(name, "administrators", "users", "http")
}

func protectedShare(name string) bool {
	return stringInFold(name, "home", "homes")
}

func stringInFold(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.EqualFold(value, candidate) {
			return true
		}
	}
	return false
}

func userHasUpdates(change identity.UserChange) bool {
	return change.NewName != nil || change.Description != nil || change.Email != nil || change.Expired != nil || change.CannotChangePassword != nil || change.PasswordNeverExpires != nil || change.CredentialRef != ""
}

func shareHasUpdates(change share.ShareChange) bool {
	return change.NewName != nil || change.Description != nil || change.Hidden != nil || change.RecycleBin != nil || change.RecycleBinAdminOnly != nil || change.HideUnreadable != nil || change.EnableCOW != nil || change.EnableCompression != nil || change.QuotaMiB != nil
}

func riskLevel(destructive bool) string {
	if destructive {
		return "high"
	}
	return "medium"
}

func identityQueryForRequest(request identity.ChangeRequest) identity.StateQuery {
	switch request.Resource {
	case identity.ResourceMembership:
		return identity.StateQuery{IncludeMemberships: true, PrincipalType: identity.PrincipalUser, Principal: request.Membership.User}
	case identity.ResourceQuota:
		return identity.StateQuery{IncludeQuotas: true, PrincipalType: request.Quota.PrincipalType, Principal: request.Quota.Principal}
	case identity.ResourceApplicationPrivilege:
		return identity.StateQuery{IncludeApplicationPrivileges: true, PrincipalType: request.ApplicationPrivilege.PrincipalType, Principal: request.ApplicationPrivilege.Principal}
	default:
		return identity.StateQuery{}
	}
}

func identityChangeIsDestructive(state synology.IdentityState, request identity.ChangeRequest) bool {
	if request.Action == identity.ActionDelete {
		return true
	}
	switch request.Resource {
	case identity.ResourceMembership:
		current, _ := findMembership(state.Memberships, request.Membership.User)
		_, remove := membershipDelta(current.Groups, request.Membership.Groups)
		return len(remove) > 0 || containsFold(request.Membership.Groups, "administrators")
	case identity.ResourceQuota:
		current, _ := findQuotaAssignment(state.Quotas, request.Quota.PrincipalType, request.Quota.Principal)
		for _, desired := range request.Quota.Limits {
			before := quotaLimitFor(current, desired.TargetType, desired.Target).QuotaMiB
			if desired.QuotaMiB > 0 && (before == 0 || desired.QuotaMiB < before) {
				return true
			}
		}
	case identity.ResourceApplicationPrivilege:
		return true
	}
	return false
}

func membershipDelta(current, desired []string) ([]string, []string) {
	currentSet := make(map[string]string)
	desiredSet := make(map[string]string)
	for _, value := range current {
		currentSet[strings.ToLower(value)] = value
	}
	for _, value := range desired {
		desiredSet[strings.ToLower(value)] = value
	}
	add := make([]string, 0)
	remove := make([]string, 0)
	for key, value := range desiredSet {
		if _, found := currentSet[key]; !found {
			add = append(add, value)
		}
	}
	for key, value := range currentSet {
		if _, found := desiredSet[key]; !found && !strings.EqualFold(value, "users") {
			remove = append(remove, value)
		}
	}
	sort.Strings(add)
	sort.Strings(remove)
	return add, remove
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftSet := make(map[string]struct{}, len(left))
	for _, value := range left {
		leftSet[strings.ToLower(value)] = struct{}{}
	}
	for _, value := range right {
		if _, found := leftSet[strings.ToLower(value)]; !found {
			return false
		}
	}
	return true
}

func containsFold(values []string, desired string) bool {
	for _, value := range values {
		if strings.EqualFold(value, desired) {
			return true
		}
	}
	return false
}

func identitySummary(state synology.IdentityState, request identity.ChangeRequest) []string {
	var action string
	switch request.Resource {
	case identity.ResourceUser:
		action = request.Action + " user " + request.User.Name
	case identity.ResourceGroup:
		action = request.Action + " group " + request.Group.Name
	case identity.ResourceMembership:
		current, _ := findMembership(state.Memberships, request.Membership.User)
		add, remove := membershipDelta(current.Groups, request.Membership.Groups)
		action = fmt.Sprintf("set memberships for user %s (add: %v; remove: %v)", request.Membership.User, add, remove)
	case identity.ResourceQuota:
		action = fmt.Sprintf("set %d quota target(s) for %s %s", len(request.Quota.Limits), request.Quota.PrincipalType, request.Quota.Principal)
	case identity.ResourceApplicationPrivilege:
		action = fmt.Sprintf("set %d application privilege rule(s) for %s %s", len(request.ApplicationPrivilege.Permissions), request.ApplicationPrivilege.PrincipalType, request.ApplicationPrivilege.Principal)
	default:
		action = request.Action + " " + request.Resource
	}
	return []string{action, "re-read identity state and verify the plan precondition before applying", "verify the resulting identity state after DSM accepts the change"}
}

func shareSummary(request share.ChangeRequest) []string {
	if request.Share != nil {
		return []string{request.Action + " shared folder " + request.Share.Name, "re-read shared-folder state and verify the plan precondition before applying", "verify the resulting shared-folder state after DSM accepts the change"}
	}
	return []string{"set permissions for " + request.Permission.PrincipalType + " " + request.Permission.Principal, "re-read the permission matrix and verify the plan precondition before applying", "verify each requested permission after DSM accepts the change"}
}

func userNameAfter(request identity.ChangeRequest) string {
	if request.User == nil {
		return ""
	}
	if request.User.NewName != nil {
		return *request.User.NewName
	}
	return request.User.Name
}

func groupNameAfter(request identity.ChangeRequest) string {
	if request.Group == nil {
		return ""
	}
	if request.Group.NewName != nil {
		return *request.Group.NewName
	}
	return request.Group.Name
}
