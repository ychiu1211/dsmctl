package identityquota

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"

	"github.com/ychiu1211/dsmctl/internal/domain/identity"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	APIName            = "SYNO.Core.Quota"
	ReadCapabilityName = "identity.quotas.read"
	SetCapabilityName  = "identity.quotas.set"
	ReadOperationName  = "identity.quotas.get"
	SetOperationName   = "identity.quotas.set"
)

type ReadInput struct{ PrincipalType, Principal string }
type SetInput struct {
	PrincipalType, Principal string
	Limits                   []identity.QuotaLimitChange
}
type Result struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
	Name     string `json:"name"`
}

var readOperation = compatibility.Operation[ReadInput, identity.PrincipalQuota]{
	Name: ReadOperationName,
	Variants: []compatibility.Variant[ReadInput, identity.PrincipalQuota]{
		{
			Name: "core-quota-get-v1", API: APIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(APIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input ReadInput) (identity.PrincipalQuota, error) {
				parameters := url.Values{"name": {input.Principal}, "support_share_quota": {"true"}}
				if input.PrincipalType == identity.PrincipalGroup {
					parameters.Set("subject_type", "group")
				}
				data, err := executor.Execute(ctx, compatibility.Request{API: APIName, Version: 1, Method: "get", Parameters: parameters})
				if err != nil {
					return identity.PrincipalQuota{}, fmt.Errorf("call %s.get v1 for %s %q: %w", APIName, input.PrincipalType, input.Principal, err)
				}
				limits, err := decodeLimits(data, input.PrincipalType)
				if err != nil {
					return identity.PrincipalQuota{}, err
				}
				return identity.PrincipalQuota{PrincipalType: input.PrincipalType, Principal: input.Principal, Limits: limits}, nil
			},
		},
	},
}

var setOperation = compatibility.Operation[SetInput, Result]{
	Name: SetOperationName,
	Variants: []compatibility.Variant[SetInput, Result]{
		{
			Name: "core-quota-set-v1", API: APIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(APIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input SetInput) (Result, error) {
				limits := make([]map[string]any, 0, len(input.Limits))
				for _, limit := range input.Limits {
					item := map[string]any{"quota": limit.QuotaMiB}
					item[limit.TargetType] = limit.Target
					limits = append(limits, item)
				}
				key := "user_quota"
				if input.PrincipalType == identity.PrincipalGroup {
					key = "group_quota"
				}
				_, err := executor.Execute(ctx, compatibility.Request{API: APIName, Version: 1, Method: "set", JSONParameters: map[string]any{"name": input.Principal, key: limits}})
				if err != nil {
					return Result{}, fmt.Errorf("call %s.set v1 for %s %q: %w", APIName, input.PrincipalType, input.Principal, err)
				}
				return Result{Resource: identity.ResourceQuota, Action: identity.ActionSet, Name: input.PrincipalType + ":" + input.Principal}, nil
			},
		},
	},
}

func decodeLimits(data json.RawMessage, principalType string) ([]identity.QuotaLimit, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil, fmt.Errorf("decode quota state: %w", err)
	}
	key := "user_quota"
	if principalType == identity.PrincipalGroup {
		key = "group_quota"
	}
	items, _ := object[key].([]any)
	if principalType == identity.PrincipalUser && items == nil {
		items, _ = object["quota"].([]any)
	}
	limits := make([]identity.QuotaLimit, 0)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item == nil {
			continue
		}
		volume, _ := item["volume"].(string)
		status, _ := item["quota_status"].(string)
		shares, hasShares := item["shares"].([]any)
		if hasShares {
			for _, rawShare := range shares {
				share, _ := rawShare.(map[string]any)
				if share == nil {
					continue
				}
				name, _ := share["name"].(string)
				limits = append(limits, identity.QuotaLimit{TargetType: identity.QuotaTargetShare, Target: name, Volume: volume, QuotaMiB: integer(share["quota"]), Status: status})
			}
			continue
		}
		limits = append(limits, identity.QuotaLimit{TargetType: identity.QuotaTargetVolume, Target: volume, QuotaMiB: integer(item["quota"]), Status: status})
	}
	sort.Slice(limits, func(i, j int) bool {
		if limits[i].TargetType == limits[j].TargetType {
			return limits[i].Target < limits[j].Target
		}
		return limits[i].TargetType < limits[j].TargetType
	})
	return limits, nil
}

func integer(value any) int64 {
	switch typed := value.(type) {
	case json.Number:
		result, _ := typed.Int64()
		return result
	case float64:
		return int64(typed)
	case int64:
		return typed
	default:
		return 0
	}
}

func APINames() []string { return []string{APIName} }
func Select(target compatibility.Target) ([]compatibility.Selection, error) {
	_, readSelection, readErr := readOperation.Select(target)
	if readErr != nil && !compatibility.IsUnsupported(readErr) {
		return nil, readErr
	}
	_, setSelection, setErr := setOperation.Select(target)
	if setErr != nil && !compatibility.IsUnsupported(setErr) {
		return nil, setErr
	}
	return []compatibility.Selection{readSelection, setSelection}, nil
}
func ExecuteRead(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input ReadInput) (identity.PrincipalQuota, compatibility.Selection, error) {
	return readOperation.Run(ctx, target, executor, input)
}
func ExecuteSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input SetInput) (Result, compatibility.Selection, error) {
	return setOperation.Run(ctx, target, executor, input)
}
