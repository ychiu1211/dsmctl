package sharemutation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/share"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	ShareAPIName             = "SYNO.Core.Share"
	PermissionAPIName        = "SYNO.Core.Share.Permission"
	ShareCapabilityName      = "share.mutate"
	PermissionCapabilityName = "share.permissions.mutate"
	ShareOperationName       = "shares.mutate"
	PermissionOperationName  = "shares.permissions.mutate"
)

type ShareInput struct {
	Action string
	Change share.ShareChange
}

type PermissionInput struct {
	Change share.PermissionChange
}

type Result struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
	Name     string `json:"name"`
}

var shareOperation = compatibility.Operation[ShareInput, Result]{
	Name: ShareOperationName,
	Variants: []compatibility.Variant[ShareInput, Result]{
		{
			Name:     "core-share-mutation-v1",
			API:      ShareAPIName,
			Version:  1,
			Priority: 10,
			Match:    compatibility.APIVersion(ShareAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input ShareInput) (Result, error) {
				method, parameters, resultName, err := shareRequest(input)
				if err != nil {
					return Result{}, err
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: ShareAPIName, Version: 1, Method: method, Parameters: parameters}); err != nil {
					return Result{}, fmt.Errorf("call %s.%s v1: %w", ShareAPIName, method, err)
				}
				return Result{Resource: share.ResourceShare, Action: input.Action, Name: resultName}, nil
			},
		},
	},
}

var permissionOperation = compatibility.Operation[PermissionInput, Result]{
	Name: PermissionOperationName,
	Variants: []compatibility.Variant[PermissionInput, Result]{
		{
			Name:     "core-share-permission-mutation-v1",
			API:      PermissionAPIName,
			Version:  1,
			Priority: 10,
			Match:    compatibility.APIVersion(PermissionAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input PermissionInput) (Result, error) {
				method, parameters, err := permissionRequest(input.Change)
				if err != nil {
					return Result{}, err
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: PermissionAPIName, Version: 1, Method: method, Parameters: parameters}); err != nil {
					return Result{}, fmt.Errorf("call %s.%s v1: %w", PermissionAPIName, method, err)
				}
				return Result{Resource: share.ResourcePermission, Action: share.ActionSet, Name: input.Change.Principal}, nil
			},
		},
	},
}

func APINames() []string {
	return []string{PermissionAPIName, ShareAPIName}
}

func Select(target compatibility.Target) ([]compatibility.Selection, error) {
	_, shareSelection, shareErr := shareOperation.Select(target)
	if shareErr != nil && !compatibility.IsUnsupported(shareErr) {
		return nil, shareErr
	}
	_, permissionSelection, permissionErr := permissionOperation.Select(target)
	if permissionErr != nil && !compatibility.IsUnsupported(permissionErr) {
		return nil, permissionErr
	}
	return []compatibility.Selection{shareSelection, permissionSelection}, nil
}

func ExecuteShare(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input ShareInput) (Result, compatibility.Selection, error) {
	return shareOperation.Run(ctx, target, executor, input)
}

func ExecutePermission(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input PermissionInput) (Result, compatibility.Selection, error) {
	return permissionOperation.Run(ctx, target, executor, input)
}

func shareRequest(input ShareInput) (string, url.Values, string, error) {
	change := input.Change
	parameters := make(url.Values)
	resultName := change.Name
	switch input.Action {
	case share.ActionCreate:
		shareInfo := shareInfoValues(change, change.Name, false)
		encoded, err := json.Marshal(shareInfo)
		if err != nil {
			return "", nil, "", fmt.Errorf("encode shared-folder settings: %w", err)
		}
		parameters.Set("name", change.Name)
		parameters.Set("shareinfo", string(encoded))
		return "create", parameters, resultName, nil
	case share.ActionUpdate:
		if change.NewName != nil {
			resultName = strings.TrimSpace(*change.NewName)
		}
		shareInfo := shareInfoValues(change, resultName, true)
		encoded, err := json.Marshal(shareInfo)
		if err != nil {
			return "", nil, "", fmt.Errorf("encode shared-folder settings: %w", err)
		}
		parameters.Set("name", change.Name)
		parameters.Set("shareinfo", string(encoded))
		return "set", parameters, resultName, nil
	case share.ActionDelete:
		names, _ := json.Marshal([]string{change.Name})
		parameters.Set("name", string(names))
		return "delete", parameters, resultName, nil
	default:
		return "", nil, "", fmt.Errorf("unsupported shared-folder action %q", input.Action)
	}
}

func shareInfoValues(change share.ShareChange, resultName string, update bool) map[string]any {
	values := map[string]any{"name": resultName}
	if update {
		values["name_org"] = change.Name
	}
	if change.VolumePath != "" {
		values["vol_path"] = change.VolumePath
	}
	setOptional(values, "desc", change.Description)
	setOptional(values, "hidden", change.Hidden)
	setOptional(values, "enable_recycle_bin", change.RecycleBin)
	setOptional(values, "recycle_bin_admin_only", change.RecycleBinAdminOnly)
	setOptional(values, "hide_unreadable", change.HideUnreadable)
	setOptional(values, "enable_share_cow", change.EnableCOW)
	setOptional(values, "enable_share_compress", change.EnableCompression)
	if change.QuotaMiB != nil {
		values["share_quota"] = strconv.FormatUint(*change.QuotaMiB, 10)
	}
	return values
}

func permissionRequest(change share.PermissionChange) (string, url.Values, error) {
	_, userGroupType, err := permissionMethod(change.PrincipalType)
	if err != nil {
		return "", nil, err
	}
	permissions := make([]map[string]any, 0, len(change.Permissions))
	for _, grant := range change.Permissions {
		flags, err := accessFlags(grant.Access)
		if err != nil {
			return "", nil, fmt.Errorf("share %q: %w", grant.ShareName, err)
		}
		flags["name"] = grant.ShareName
		permissions = append(permissions, flags)
	}
	encoded, err := json.Marshal(permissions)
	if err != nil {
		return "", nil, fmt.Errorf("encode shared-folder permissions: %w", err)
	}
	parameters := url.Values{
		"name":            {change.Principal},
		"user_group_type": {userGroupType},
		"permissions":     {string(encoded)},
	}
	return "set_by_user_group", parameters, nil
}

func permissionMethod(principalType string) (string, string, error) {
	switch principalType {
	case share.PrincipalUser:
		return "set_by_user_group", "local_user", nil
	case share.PrincipalGroup:
		return "set_by_user_group", "local_group", nil
	default:
		return "", "", fmt.Errorf("unsupported principal type %q", principalType)
	}
}

func accessFlags(access string) (map[string]any, error) {
	flags := map[string]any{
		"is_readonly": false,
		"is_writable": false,
		"is_deny":     false,
		"is_custom":   false,
	}
	switch access {
	case share.AccessNone:
	case share.AccessRead:
		flags["is_readonly"] = true
	case share.AccessWrite:
		flags["is_writable"] = true
	case share.AccessDeny:
		flags["is_deny"] = true
	default:
		return nil, fmt.Errorf("unsupported permission access %q", access)
	}
	return flags, nil
}

func setOptional[T any](values map[string]any, key string, value *T) {
	if value != nil {
		values[key] = *value
	}
}
