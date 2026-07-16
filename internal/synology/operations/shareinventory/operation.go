package shareinventory

import (
	"context"
	"fmt"
	"net/url"
	"sort"

	"github.com/ychiu1211/dsmctl/internal/domain/identity"
	"github.com/ychiu1211/dsmctl/internal/domain/share"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	ShareAPIName               = "SYNO.Core.Share"
	PermissionAPIName          = "SYNO.Core.Share.Permission"
	InventoryCapabilityName    = "share.inventory"
	PermissionCapabilityName   = "share.permissions"
	ShareOperationName         = "shares.list"
	PermissionOperationName    = "shares.permissions.list"
	additionalShareFields      = `["hidden","encryption","is_aclmode","unite_permission","support_snapshot","share_quota"]`
	additionalPermissionFields = `["hidden","encryption","is_aclmode"]`
	shareTypes                 = `["dec","local","usb","sata","cluster","c2","cold_storage","worm"]`
)

type ShareInput struct{}

type PermissionInput struct {
	PrincipalType string
	Principal     string
}

type Input struct {
	IncludePermissions bool
	Users              []identity.User
	Groups             []identity.Group
}

type permissionResult struct {
	ShareName string
	Binding   share.Permission
}

var shareOperation = compatibility.Operation[ShareInput, []share.SharedFolder]{
	Name: ShareOperationName,
	Variants: []compatibility.Variant[ShareInput, []share.SharedFolder]{
		{
			Name:     "core-share-v1",
			API:      ShareAPIName,
			Version:  1,
			Priority: 10,
			Match:    compatibility.APIVersion(ShareAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ ShareInput) ([]share.SharedFolder, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API:     ShareAPIName,
					Version: 1,
					Method:  "list",
					Parameters: url.Values{
						"offset":     {"0"},
						"limit":      {"-1"},
						"additional": {additionalShareFields},
					},
				})
				if err != nil {
					return nil, fmt.Errorf("call %s.list v1: %w", ShareAPIName, err)
				}
				return decodeShares(data)
			},
		},
	},
}

var permissionOperation = compatibility.Operation[PermissionInput, []permissionResult]{
	Name: PermissionOperationName,
	Variants: []compatibility.Variant[PermissionInput, []permissionResult]{
		{
			Name:     "core-share-permission-v1",
			API:      PermissionAPIName,
			Version:  1,
			Priority: 10,
			Match:    compatibility.APIVersion(PermissionAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input PermissionInput) ([]permissionResult, error) {
				method, userGroupType, err := permissionMethod(input.PrincipalType)
				if err != nil {
					return nil, err
				}
				if input.Principal == "" {
					return nil, fmt.Errorf("principal name is required")
				}
				data, err := executor.Execute(ctx, compatibility.Request{
					API:     PermissionAPIName,
					Version: 1,
					Method:  method,
					Parameters: url.Values{
						"name":            {input.Principal},
						"user_group_type": {userGroupType},
						"share_type":      {shareTypes},
						"additional":      {additionalPermissionFields},
					},
				})
				if err != nil {
					return nil, fmt.Errorf("call %s.%s v1: %w", PermissionAPIName, method, err)
				}
				return decodePermissions(data, input)
			},
		},
	},
}

func APINames() []string {
	unique := make(map[string]struct{})
	for _, name := range append(shareOperation.APINames(), permissionOperation.APINames()...) {
		unique[name] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for name := range unique {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func Select(target compatibility.Target) ([]compatibility.Selection, error) {
	shareSelection, shareErr := selectShares(target)
	if shareErr != nil && !compatibility.IsUnsupported(shareErr) {
		return nil, shareErr
	}
	permissionSelection, permissionErr := selectPermissions(target)
	if permissionErr != nil && !compatibility.IsUnsupported(permissionErr) {
		return nil, permissionErr
	}
	return []compatibility.Selection{shareSelection, permissionSelection}, nil
}

func Execute(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input Input) (share.State, []compatibility.Selection, error) {
	shares, shareSelection, err := shareOperation.Run(ctx, target, executor, ShareInput{})
	if err != nil {
		return share.State{}, []compatibility.Selection{shareSelection}, err
	}
	permissionSelection, permissionErr := selectPermissions(target)
	selections := []compatibility.Selection{shareSelection, permissionSelection}
	if !input.IncludePermissions {
		return share.State{Shares: shares, PermissionsIncluded: false}, selections, nil
	}
	if permissionErr != nil {
		return share.State{}, selections, permissionErr
	}

	byName := make(map[string]int, len(shares))
	for index := range shares {
		shares[index].Permissions = make([]share.Permission, 0)
		byName[shares[index].Name] = index
	}
	for _, user := range input.Users {
		if err := appendPrincipalPermissions(ctx, target, executor, byName, shares, share.PrincipalUser, user.Name); err != nil {
			return share.State{}, selections, err
		}
	}
	for _, group := range input.Groups {
		if err := appendPrincipalPermissions(ctx, target, executor, byName, shares, share.PrincipalGroup, group.Name); err != nil {
			return share.State{}, selections, err
		}
	}
	for index := range shares {
		sort.Slice(shares[index].Permissions, func(left, right int) bool {
			if shares[index].Permissions[left].PrincipalType != shares[index].Permissions[right].PrincipalType {
				return shares[index].Permissions[left].PrincipalType < shares[index].Permissions[right].PrincipalType
			}
			return shares[index].Permissions[left].Principal < shares[index].Permissions[right].Principal
		})
	}
	return share.State{Shares: shares, PermissionsIncluded: true}, selections, nil
}

func appendPrincipalPermissions(ctx context.Context, target compatibility.Target, executor compatibility.Executor, byName map[string]int, shares []share.SharedFolder, principalType, principal string) error {
	results, _, err := permissionOperation.Run(ctx, target, executor, PermissionInput{PrincipalType: principalType, Principal: principal})
	if err != nil {
		return err
	}
	for _, result := range results {
		if index, ok := byName[result.ShareName]; ok {
			shares[index].Permissions = append(shares[index].Permissions, result.Binding)
		}
	}
	return nil
}

func permissionMethod(principalType string) (method, userGroupType string, err error) {
	switch principalType {
	case share.PrincipalUser:
		return "list_by_user", "local_user", nil
	case share.PrincipalGroup:
		return "list_by_group", "local_group", nil
	default:
		return "", "", fmt.Errorf("unsupported principal type %q", principalType)
	}
}

func selectShares(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := shareOperation.Select(target)
	return selection, err
}

func selectPermissions(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := permissionOperation.Select(target)
	return selection, err
}

func InventorySupported(selections []compatibility.Selection) bool {
	return len(selections) > 0 && selections[0].Supported
}

func PermissionsSupported(selections []compatibility.Selection) bool {
	return len(selections) > 1 && selections[1].Supported
}
