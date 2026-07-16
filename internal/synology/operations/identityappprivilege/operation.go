package identityappprivilege

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/identity"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	AppAPIName            = "SYNO.Core.AppPriv.App"
	RuleAPIName           = "SYNO.Core.AppPriv.Rule"
	ReadCapabilityName    = "identity.application_privileges.read"
	SetCapabilityName     = "identity.application_privileges.set"
	AppListOperationName  = "identity.applications.list"
	RuleReadOperationName = "identity.application_privileges.get"
	RuleSetOperationName  = "identity.application_privileges.set"
)

type PrincipalInput struct {
	PrincipalType string
	Principal     string
}
type SetInput struct {
	PrincipalType string
	Principal     string
	Permissions   []identity.ApplicationPermissionChange
}
type Result struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
	Name     string `json:"name"`
}

var appListOperation = compatibility.Operation[struct{}, []identity.Application]{
	Name: AppListOperationName,
	Variants: []compatibility.Variant[struct{}, []identity.Application]{
		{
			Name:     "core-apppriv-app-list-v2",
			API:      AppAPIName,
			Version:  2,
			Priority: 10,
			Match:    compatibility.APIVersion(AppAPIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ struct{}) ([]identity.Application, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: AppAPIName, Version: 2, Method: "list", Parameters: url.Values{"offset": {"0"}, "limit": {"-1"}}})
				if err != nil {
					return nil, fmt.Errorf("call %s.list v2: %w", AppAPIName, err)
				}
				return decodeApplications(data)
			},
		},
	},
}

var ruleReadOperation = compatibility.Operation[PrincipalInput, identity.ApplicationPrivilegeAssignment]{
	Name: RuleReadOperationName,
	Variants: []compatibility.Variant[PrincipalInput, identity.ApplicationPrivilegeAssignment]{
		{
			Name:     "core-apppriv-rule-get-v1",
			API:      RuleAPIName,
			Version:  1,
			Priority: 10,
			Match:    compatibility.APIVersion(RuleAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input PrincipalInput) (identity.ApplicationPrivilegeAssignment, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: RuleAPIName, Version: 1, Method: "get", Parameters: url.Values{"entity_type": {input.PrincipalType}, "entity_name": {input.Principal}}})
				if err != nil {
					return identity.ApplicationPrivilegeAssignment{}, fmt.Errorf("call %s.get v1 for %s %q: %w", RuleAPIName, input.PrincipalType, input.Principal, err)
				}
				permissions, err := decodePermissions(data)
				if err != nil {
					return identity.ApplicationPrivilegeAssignment{}, err
				}
				return identity.ApplicationPrivilegeAssignment{PrincipalType: input.PrincipalType, Principal: input.Principal, Permissions: permissions}, nil
			},
		},
	},
}

var ruleSetOperation = compatibility.Operation[SetInput, Result]{
	Name: RuleSetOperationName,
	Variants: []compatibility.Variant[SetInput, Result]{
		{
			Name:     "core-apppriv-rule-set-v1",
			API:      RuleAPIName,
			Version:  1,
			Priority: 10,
			Match:    compatibility.APIVersion(RuleAPIName, 1),
			Execute:  executeRuleSet,
		},
	},
}

func executeRuleSet(ctx context.Context, executor compatibility.Executor, input SetInput) (Result, error) {
	deletes := make([]map[string]any, 0)
	sets := make([]map[string]any, 0)
	for _, permission := range input.Permissions {
		rule := map[string]any{"entity_type": input.PrincipalType, "entity_name": input.Principal, "app_id": permission.ApplicationID}
		switch permission.Access {
		case identity.ApplicationAccessInherit:
			deletes = append(deletes, rule)
		case identity.ApplicationAccessAllow:
			rule["allow_ip"] = []string{"0.0.0.0"}
			rule["deny_ip"] = []string{}
			sets = append(sets, rule)
		case identity.ApplicationAccessDeny:
			rule["allow_ip"] = []string{}
			rule["deny_ip"] = []string{"0.0.0.0"}
			sets = append(sets, rule)
		}
	}
	if len(deletes) > 0 {
		if _, err := executor.Execute(ctx, compatibility.Request{API: RuleAPIName, Version: 1, Method: "delete", JSONParameters: map[string]any{"rules": deletes}}); err != nil {
			return Result{}, fmt.Errorf("call %s.delete v1: %w", RuleAPIName, err)
		}
	}
	if len(sets) > 0 {
		if _, err := executor.Execute(ctx, compatibility.Request{API: RuleAPIName, Version: 1, Method: "set", JSONParameters: map[string]any{"rules": sets}}); err != nil {
			return Result{}, fmt.Errorf("call %s.set v1: %w", RuleAPIName, err)
		}
	}
	return Result{Resource: identity.ResourceApplicationPrivilege, Action: identity.ActionSet, Name: input.PrincipalType + ":" + input.Principal}, nil
}

func decodeApplications(data json.RawMessage) ([]identity.Application, error) {
	var response struct {
		Applications []map[string]any `json:"applications"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode application inventory: %w", err)
	}
	result := make([]identity.Application, 0, len(response.Applications))
	for _, item := range response.Applications {
		id, _ := item["app_id"].(string)
		if id == "" {
			continue
		}
		result = append(result, identity.Application{ID: id, Name: displayName(item["name"]), GrantTypes: stringSlice(item["grant_type"]), SupportsIP: boolean(item["supportIP"])})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func decodePermissions(data json.RawMessage) ([]identity.ApplicationPermission, error) {
	var response struct {
		Rules []struct {
			AppID   string   `json:"app_id"`
			AllowIP []string `json:"allow_ip"`
			DenyIP  []string `json:"deny_ip"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode application privilege rules: %w", err)
	}
	result := make([]identity.ApplicationPermission, 0, len(response.Rules))
	for _, rule := range response.Rules {
		access := identity.ApplicationAccessCustom
		if len(rule.AllowIP) == 1 && rule.AllowIP[0] == "0.0.0.0" && len(rule.DenyIP) == 0 {
			access = identity.ApplicationAccessAllow
		}
		if len(rule.DenyIP) == 1 && rule.DenyIP[0] == "0.0.0.0" && len(rule.AllowIP) == 0 {
			access = identity.ApplicationAccessDeny
		}
		result = append(result, identity.ApplicationPermission{ApplicationID: rule.AppID, Access: access, AllowIP: rule.AllowIP, DenyIP: rule.DenyIP})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ApplicationID < result[j].ApplicationID })
	return result, nil
}

func displayName(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		for _, item := range typed {
			if name, ok := item.(string); ok && name != "" {
				return name
			}
		}
	}
	return ""
}
func stringSlice(value any) []string {
	switch typed := value.(type) {
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	case []string:
		return typed
	case string:
		if typed != "" {
			return strings.Split(typed, ",")
		}
	}
	return nil
}
func boolean(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed != 0
	case string:
		return typed == "true" || typed == "1"
	}
	return false
}

func APINames() []string { return []string{AppAPIName, RuleAPIName} }
func Select(target compatibility.Target) ([]compatibility.Selection, error) {
	selectors := []func(compatibility.Target) (compatibility.Selection, error){selectApps, selectRules, selectSet}
	result := make([]compatibility.Selection, 0, len(selectors))
	for _, selector := range selectors {
		selection, err := selector(target)
		result = append(result, selection)
		if err != nil && !compatibility.IsUnsupported(err) {
			return nil, err
		}
	}
	return result, nil
}
func selectApps(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := appListOperation.Select(target)
	return selection, err
}
func selectRules(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := ruleReadOperation.Select(target)
	return selection, err
}
func selectSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := ruleSetOperation.Select(target)
	return selection, err
}
func ExecuteApps(ctx context.Context, target compatibility.Target, executor compatibility.Executor) ([]identity.Application, compatibility.Selection, error) {
	return appListOperation.Run(ctx, target, executor, struct{}{})
}
func ExecuteRead(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input PrincipalInput) (identity.ApplicationPrivilegeAssignment, compatibility.Selection, error) {
	return ruleReadOperation.Run(ctx, target, executor, input)
}
func ExecuteSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input SetInput) (Result, compatibility.Selection, error) {
	return ruleSetOperation.Run(ctx, target, executor, input)
}
