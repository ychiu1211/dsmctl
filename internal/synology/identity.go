package synology

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/identity"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/identityappprivilege"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/identityinventory"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/identitymembership"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/identitymutation"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/identityquota"
)

type IdentityState = identity.State
type IdentityCapabilities = identity.Capabilities
type IdentityChangeRequest = identity.ChangeRequest
type IdentityMutationResult = identitymutation.Result

func (c *Client) IdentityState(ctx context.Context, queries ...identity.StateQuery) (IdentityState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	query := identity.StateQuery{}
	if len(queries) > 0 {
		query = queries[0]
	}
	apiNames := identityStateAPINames(query)
	if err := c.prepareCompatibilityTargetLocked(ctx, apiNames...); err != nil {
		return IdentityState{}, fmt.Errorf("prepare account inventory target: %w", err)
	}
	state, _, err := c.identityStateLocked(ctx, query)
	if err != nil {
		return IdentityState{}, err
	}
	return state, nil
}

func (c *Client) identityStateLocked(ctx context.Context, query identity.StateQuery) (IdentityState, []compatibility.Selection, error) {
	state, selections, err := identityinventory.Execute(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return IdentityState{}, selections, fmt.Errorf("get account inventory: %w", err)
	}
	c.target.AddCapability(identityinventory.CapabilityName)
	if query.IncludeMemberships {
		filter := ""
		if query.PrincipalType == identity.PrincipalUser {
			filter = query.Principal
		}
		memberships, selection, err := identitymembership.ExecuteRead(ctx, c.target, lockedExecutor{client: c}, identitymembership.ReadInput{Users: state.Users, Groups: state.Groups, User: filter})
		selections = append(selections, selection)
		if err != nil {
			return IdentityState{}, selections, fmt.Errorf("get group memberships: %w", err)
		}
		state.Memberships = memberships
		c.target.AddCapability(identitymembership.ReadCapabilityName)
	}
	if query.IncludeQuotas {
		targets, err := identityPrincipals(state, query.PrincipalType, query.Principal)
		if err != nil {
			return IdentityState{}, selections, err
		}
		for _, target := range targets {
			quota, selection, err := identityquota.ExecuteRead(ctx, c.target, lockedExecutor{client: c}, identityquota.ReadInput{PrincipalType: target.kind, Principal: target.name})
			selections = append(selections, selection)
			if err != nil {
				return IdentityState{}, selections, fmt.Errorf("get quota for %s %q: %w", target.kind, target.name, err)
			}
			state.Quotas = append(state.Quotas, quota)
		}
		c.target.AddCapability(identityquota.ReadCapabilityName)
	}
	if query.IncludeApplicationPrivileges {
		applications, selection, err := identityappprivilege.ExecuteApps(ctx, c.target, lockedExecutor{client: c})
		selections = append(selections, selection)
		if err != nil {
			return IdentityState{}, selections, fmt.Errorf("get application inventory: %w", err)
		}
		state.Applications = applications
		targets, err := identityPrincipals(state, query.PrincipalType, query.Principal)
		if err != nil {
			return IdentityState{}, selections, err
		}
		for _, target := range targets {
			assignment, selection, err := identityappprivilege.ExecuteRead(ctx, c.target, lockedExecutor{client: c}, identityappprivilege.PrincipalInput{PrincipalType: target.kind, Principal: target.name})
			selections = append(selections, selection)
			if err != nil {
				return IdentityState{}, selections, fmt.Errorf("get application privileges for %s %q: %w", target.kind, target.name, err)
			}
			state.ApplicationPrivileges = append(state.ApplicationPrivileges, assignment)
		}
		c.target.AddCapability(identityappprivilege.ReadCapabilityName)
	}
	return state, selections, nil
}

func (c *Client) IdentityCapabilities(ctx context.Context) (IdentityCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	apiNames := append(identityinventory.APINames(), identitymutation.APINames()...)
	apiNames = append(apiNames, identitymembership.APINames()...)
	apiNames = append(apiNames, identityquota.APINames()...)
	apiNames = append(apiNames, identityappprivilege.APINames()...)
	if err := c.prepareCompatibilityTargetLocked(ctx, apiNames...); err != nil {
		return IdentityCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare account capabilities target: %w", err)
	}
	selections, err := identityinventory.Select(c.target)
	if err != nil {
		return IdentityCapabilities{}, CompatibilityReport{}, fmt.Errorf("select account inventory backends: %w", err)
	}
	supported := identityinventory.Supported(selections)
	if supported {
		c.target.AddCapability(identityinventory.CapabilityName)
	}
	mutationSelections, err := identitymutation.Select(c.target)
	if err != nil {
		return IdentityCapabilities{}, CompatibilityReport{}, fmt.Errorf("select account mutation backends: %w", err)
	}
	userMutations := len(mutationSelections) > 0 && mutationSelections[0].Supported
	groupMutations := len(mutationSelections) > 1 && mutationSelections[1].Supported
	if userMutations {
		c.target.AddCapability(identitymutation.UserCapabilityName)
	}
	if groupMutations {
		c.target.AddCapability(identitymutation.GroupCapabilityName)
	}
	membershipSelections, err := identitymembership.Select(c.target)
	if err != nil {
		return IdentityCapabilities{}, CompatibilityReport{}, fmt.Errorf("select membership backends: %w", err)
	}
	membershipRead := selectionSupported(membershipSelections, 0)
	membershipSet := selectionSupported(membershipSelections, 1)
	quotaSelections, err := identityquota.Select(c.target)
	if err != nil {
		return IdentityCapabilities{}, CompatibilityReport{}, fmt.Errorf("select quota backends: %w", err)
	}
	quotaRead := selectionSupported(quotaSelections, 0)
	quotaSet := selectionSupported(quotaSelections, 1)
	appPrivilegeSelections, err := identityappprivilege.Select(c.target)
	if err != nil {
		return IdentityCapabilities{}, CompatibilityReport{}, fmt.Errorf("select application privilege backends: %w", err)
	}
	appPrivilegeRead := selectionSupported(appPrivilegeSelections, 0) && selectionSupported(appPrivilegeSelections, 1)
	appPrivilegeSet := selectionSupported(appPrivilegeSelections, 2)
	capabilities := IdentityCapabilities{
		InventoryRead:            supported,
		UserCreate:               userMutations,
		UserUpdate:               userMutations,
		UserDelete:               userMutations,
		GroupCreate:              groupMutations,
		GroupUpdate:              groupMutations,
		GroupDelete:              groupMutations,
		MembershipRead:           membershipRead,
		MembershipSet:            membershipSet,
		QuotaRead:                quotaRead,
		QuotaSet:                 quotaSet,
		ApplicationPrivilegeRead: appPrivilegeRead,
		ApplicationPrivilegeSet:  appPrivilegeSet,
		Mutations:                userMutations || groupMutations || membershipSet || quotaSet || appPrivilegeSet,
	}
	if membershipRead {
		c.target.AddCapability(identitymembership.ReadCapabilityName)
	}
	if membershipSet {
		c.target.AddCapability(identitymembership.SetCapabilityName)
	}
	if quotaRead {
		c.target.AddCapability(identityquota.ReadCapabilityName)
	}
	if quotaSet {
		c.target.AddCapability(identityquota.SetCapabilityName)
	}
	if appPrivilegeRead {
		c.target.AddCapability(identityappprivilege.ReadCapabilityName)
	}
	if appPrivilegeSet {
		c.target.AddCapability(identityappprivilege.SetCapabilityName)
	}
	c.updateDerivedCapabilitiesLocked()
	selections = append(selections, mutationSelections...)
	selections = append(selections, membershipSelections...)
	selections = append(selections, quotaSelections...)
	selections = append(selections, appPrivilegeSelections...)
	return capabilities, c.target.Report(selections...), nil
}

func (c *Client) ApplyIdentityChange(ctx context.Context, request identity.ChangeRequest, password string) (IdentityMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	apiNames := append(identitymutation.APINames(), identitymembership.APINames()...)
	apiNames = append(apiNames, identityquota.APINames()...)
	apiNames = append(apiNames, identityappprivilege.APINames()...)
	if err := c.prepareCompatibilityTargetLocked(ctx, apiNames...); err != nil {
		return IdentityMutationResult{}, fmt.Errorf("prepare account mutation target: %w", err)
	}
	switch request.Resource {
	case identity.ResourceUser:
		if request.User == nil {
			return IdentityMutationResult{}, fmt.Errorf("user change is required")
		}
		result, _, err := identitymutation.ExecuteUser(ctx, c.target, lockedExecutor{client: c}, identitymutation.UserInput{Action: request.Action, Change: *request.User, Password: password})
		if err != nil {
			return IdentityMutationResult{}, fmt.Errorf("apply user change: %w", err)
		}
		return result, nil
	case identity.ResourceGroup:
		if request.Group == nil {
			return IdentityMutationResult{}, fmt.Errorf("group change is required")
		}
		result, _, err := identitymutation.ExecuteGroup(ctx, c.target, lockedExecutor{client: c}, identitymutation.GroupInput{Action: request.Action, Change: *request.Group})
		if err != nil {
			return IdentityMutationResult{}, fmt.Errorf("apply group change: %w", err)
		}
		return result, nil
	case identity.ResourceMembership:
		if request.Membership == nil {
			return IdentityMutationResult{}, fmt.Errorf("membership change is required")
		}
		state, _, err := c.identityStateLocked(ctx, identity.StateQuery{IncludeMemberships: true, PrincipalType: identity.PrincipalUser, Principal: request.Membership.User})
		if err != nil {
			return IdentityMutationResult{}, fmt.Errorf("read current membership state: %w", err)
		}
		current := []string{}
		for _, membership := range state.Memberships {
			if strings.EqualFold(membership.User, request.Membership.User) {
				current = membership.Groups
				break
			}
		}
		add, remove := membershipDelta(current, request.Membership.Groups)
		result, _, err := identitymembership.ExecuteChange(ctx, c.target, lockedExecutor{client: c}, identitymembership.ChangeInput{User: request.Membership.User, AddGroups: add, RemoveGroups: remove})
		if err != nil {
			return IdentityMutationResult{}, fmt.Errorf("apply membership change: %w", err)
		}
		return IdentityMutationResult{Resource: result.Resource, Action: result.Action, Name: result.Name}, nil
	case identity.ResourceQuota:
		if request.Quota == nil {
			return IdentityMutationResult{}, fmt.Errorf("quota change is required")
		}
		result, _, err := identityquota.ExecuteSet(ctx, c.target, lockedExecutor{client: c}, identityquota.SetInput{PrincipalType: request.Quota.PrincipalType, Principal: request.Quota.Principal, Limits: request.Quota.Limits})
		if err != nil {
			return IdentityMutationResult{}, fmt.Errorf("apply quota change: %w", err)
		}
		return IdentityMutationResult{Resource: result.Resource, Action: result.Action, Name: result.Name}, nil
	case identity.ResourceApplicationPrivilege:
		if request.ApplicationPrivilege == nil {
			return IdentityMutationResult{}, fmt.Errorf("application privilege change is required")
		}
		result, _, err := identityappprivilege.ExecuteSet(ctx, c.target, lockedExecutor{client: c}, identityappprivilege.SetInput{PrincipalType: request.ApplicationPrivilege.PrincipalType, Principal: request.ApplicationPrivilege.Principal, Permissions: request.ApplicationPrivilege.Permissions})
		if err != nil {
			return IdentityMutationResult{}, fmt.Errorf("apply application privilege change: %w", err)
		}
		return IdentityMutationResult{Resource: result.Resource, Action: result.Action, Name: result.Name}, nil
	default:
		return IdentityMutationResult{}, fmt.Errorf("unsupported identity resource %q", request.Resource)
	}
}

type identityPrincipal struct{ kind, name string }

func identityPrincipals(state IdentityState, principalType, principal string) ([]identityPrincipal, error) {
	if principalType != "" && principalType != identity.PrincipalUser && principalType != identity.PrincipalGroup {
		return nil, fmt.Errorf("principal_type must be user or group")
	}
	if principal != "" && principalType == "" {
		return nil, fmt.Errorf("principal_type is required when principal is set")
	}
	result := make([]identityPrincipal, 0)
	if principalType == "" || principalType == identity.PrincipalUser {
		for _, user := range state.Users {
			if principal == "" || strings.EqualFold(principal, user.Name) {
				result = append(result, identityPrincipal{kind: identity.PrincipalUser, name: user.Name})
			}
		}
	}
	if principalType == "" || principalType == identity.PrincipalGroup {
		for _, group := range state.Groups {
			if principal == "" || strings.EqualFold(principal, group.Name) {
				result = append(result, identityPrincipal{kind: identity.PrincipalGroup, name: group.Name})
			}
		}
	}
	if principal != "" && len(result) == 0 {
		return nil, fmt.Errorf("%s %q does not exist", principalType, principal)
	}
	return result, nil
}

func identityStateAPINames(query identity.StateQuery) []string {
	result := append([]string{}, identityinventory.APINames()...)
	if query.IncludeMemberships {
		result = append(result, identitymembership.APINames()...)
	}
	if query.IncludeQuotas {
		result = append(result, identityquota.APINames()...)
	}
	if query.IncludeApplicationPrivileges {
		result = append(result, identityappprivilege.APINames()...)
	}
	return result
}

func selectionSupported(selections []compatibility.Selection, index int) bool {
	return index >= 0 && index < len(selections) && selections[index].Supported
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
