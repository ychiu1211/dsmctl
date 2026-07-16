package application

import (
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/identity"
	"github.com/ychiu1211/dsmctl/internal/domain/share"
)

func TestIdentityValidationProtectsBuiltInAndRenamedTargets(t *testing.T) {
	for _, request := range []identity.ChangeRequest{
		{Action: identity.ActionDelete, Resource: identity.ResourceUser, User: &identity.UserChange{Name: "admin"}},
		{Action: identity.ActionCreate, Resource: identity.ResourceGroup, Group: &identity.GroupChange{Name: "administrators"}},
		{Action: identity.ActionUpdate, Resource: identity.ResourceUser, User: &identity.UserChange{Name: "alice", NewName: stringPointer("root")}},
	} {
		if err := validateIdentityRequest(request); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("validateIdentityRequest(%#v) error = %v", request, err)
		}
	}
}

func TestIdentityValidationRequiresCredentialReferenceWithoutPlaintext(t *testing.T) {
	request := identity.ChangeRequest{Action: identity.ActionCreate, Resource: identity.ResourceUser, User: &identity.UserChange{Name: "dsmctl-user"}}
	if err := validateIdentityRequest(request); err == nil || !strings.Contains(err.Error(), "credential_ref") {
		t.Fatalf("validateIdentityRequest() error = %v", err)
	}
	request.User.CredentialRef = "plaintext-password"
	if err := validateIdentityRequest(request); err == nil || !strings.Contains(err.Error(), "env:NAME") {
		t.Fatalf("validateIdentityRequest() error = %v", err)
	}
	request.User.CredentialRef = "env:DSMCTL_NEW_USER_PASSWORD"
	if err := validateIdentityRequest(request); err != nil {
		t.Fatalf("validateIdentityRequest() error = %v", err)
	}
}

func TestShareValidationProtectsBuiltInsAndRejectsVolumeMove(t *testing.T) {
	requests := []share.ChangeRequest{
		{Action: share.ActionDelete, Resource: share.ResourceShare, Share: &share.ShareChange{Name: "homes"}},
		{Action: share.ActionUpdate, Resource: share.ResourceShare, Share: &share.ShareChange{Name: "projects", NewName: stringPointer("home")}},
		{Action: share.ActionUpdate, Resource: share.ResourceShare, Share: &share.ShareChange{Name: "projects", VolumePath: "/volume2", Description: stringPointer("move")}},
	}
	for _, request := range requests {
		if err := validateShareRequest(request); err == nil {
			t.Errorf("validateShareRequest(%#v) accepted an unsafe request", request)
		}
	}
}

func TestPlanHashDetectsRequestOrPreconditionChanges(t *testing.T) {
	plan := IdentityPlan{
		APIVersion: managementAPIVersion,
		NAS:        "office",
		Request: identity.ChangeRequest{
			Action:   identity.ActionDelete,
			Resource: identity.ResourceUser,
			User:     &identity.UserChange{Name: "dsmctl-test"},
		},
		Precondition: ChangePrecondition{ExpectedExists: true, ResourceID: "1026", Fingerprint: "before"},
		Destructive:  true,
		Risk:         "high",
		Summary:      []string{"delete user dsmctl-test"},
	}
	hash, err := identityPlanHash(plan)
	if err != nil {
		t.Fatalf("identityPlanHash() error = %v", err)
	}
	plan.Hash = hash
	if err := validateIdentityPlan(plan, hash); err != nil {
		t.Fatalf("validateIdentityPlan() error = %v", err)
	}
	plan.Precondition.ResourceID = "different"
	if err := validateIdentityPlan(plan, hash); err == nil || !strings.Contains(err.Error(), "hash is invalid") {
		t.Fatalf("tampered validateIdentityPlan() error = %v", err)
	}
}

func TestIdentityExtendedResourceValidation(t *testing.T) {
	valid := []identity.ChangeRequest{
		{Action: identity.ActionSet, Resource: identity.ResourceMembership, Membership: &identity.MembershipChange{User: "alice", Groups: []string{"users", "dev"}}},
		{Action: identity.ActionSet, Resource: identity.ResourceQuota, Quota: &identity.QuotaChange{PrincipalType: identity.PrincipalUser, Principal: "alice", Limits: []identity.QuotaLimitChange{{TargetType: identity.QuotaTargetVolume, Target: "/volume1", QuotaMiB: 1024}}}},
		{Action: identity.ActionSet, Resource: identity.ResourceApplicationPrivilege, ApplicationPrivilege: &identity.ApplicationPrivilegeChange{PrincipalType: identity.PrincipalGroup, Principal: "dev", Permissions: []identity.ApplicationPermissionChange{{ApplicationID: "SYNO.Desktop", Access: identity.ApplicationAccessDeny}}}},
	}
	for _, request := range valid {
		if err := validateIdentityRequest(request); err != nil {
			t.Errorf("validateIdentityRequest(%s) error = %v", request.Resource, err)
		}
	}

	invalid := []identity.ChangeRequest{
		{Action: identity.ActionSet, Resource: identity.ResourceMembership, Membership: &identity.MembershipChange{User: "alice", Groups: []string{"dev"}}},
		{Action: identity.ActionSet, Resource: identity.ResourceQuota, Quota: &identity.QuotaChange{PrincipalType: identity.PrincipalUser, Principal: "alice", Limits: []identity.QuotaLimitChange{{TargetType: identity.QuotaTargetVolume, Target: "/volume1", QuotaMiB: -1}}}},
		{Action: identity.ActionSet, Resource: identity.ResourceApplicationPrivilege, ApplicationPrivilege: &identity.ApplicationPrivilegeChange{PrincipalType: identity.PrincipalUser, Principal: "alice", Permissions: []identity.ApplicationPermissionChange{{ApplicationID: "SYNO.Desktop", Access: "custom"}}}},
	}
	for _, request := range invalid {
		if err := validateIdentityRequest(request); err == nil {
			t.Errorf("validateIdentityRequest(%#v) accepted unsafe input", request)
		}
	}
}

func stringPointer(value string) *string { return &value }
