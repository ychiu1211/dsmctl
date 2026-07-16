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
	state, err := client.IdentityState(ctx)
	if err != nil {
		return IdentityPlan{}, authenticationError(name, err)
	}
	precondition, err := identityPrecondition(state, request)
	if err != nil {
		return IdentityPlan{}, err
	}
	destructive := request.Action == identity.ActionDelete
	plan := IdentityPlan{
		APIVersion:   managementAPIVersion,
		NAS:          name,
		Request:      request,
		Precondition: precondition,
		Destructive:  destructive,
		Risk:         riskLevel(destructive),
		Summary:      identitySummary(request),
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
	state, err := client.IdentityState(ctx)
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
	if request.Resource == identity.ResourceUser {
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
	}
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
	if request.Action != identity.ActionCreate && request.Action != identity.ActionUpdate && request.Action != identity.ActionDelete {
		return fmt.Errorf("unsupported identity action %q", request.Action)
	}
	switch request.Resource {
	case identity.ResourceUser:
		if request.User == nil || request.Group != nil {
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
		if request.Group == nil || request.User != nil {
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

func validateManagedName(label, name string) error {
	if !managedNamePattern.MatchString(strings.TrimSpace(name)) {
		return fmt.Errorf("%s name %q must start with a letter or number and contain at most 64 letters, numbers, '.', '_' or '-'", label, name)
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

func identitySummary(request identity.ChangeRequest) []string {
	name := request.Resource
	if request.User != nil {
		name += " " + request.User.Name
	} else {
		name += " " + request.Group.Name
	}
	return []string{request.Action + " " + name, "re-read identity state and verify the plan precondition before applying", "verify the resulting identity state after DSM accepts the change"}
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
