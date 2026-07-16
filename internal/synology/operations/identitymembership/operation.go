package identitymembership

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
	APIName             = "SYNO.Core.Group.Member"
	ReadCapabilityName  = "identity.memberships.read"
	SetCapabilityName   = "identity.memberships.set"
	ReadOperationName   = "identity.memberships.list"
	ChangeOperationName = "identity.memberships.set"
)

type ReadInput struct {
	Users  []identity.User
	Groups []identity.Group
	User   string
}

type ChangeInput struct {
	User         string
	AddGroups    []string
	RemoveGroups []string
}

type Result struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
	Name     string `json:"name"`
}

var readOperation = compatibility.Operation[ReadInput, []identity.Membership]{
	Name: ReadOperationName,
	Variants: []compatibility.Variant[ReadInput, []identity.Membership]{
		{
			Name: "core-group-member-list-v1", API: APIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(APIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input ReadInput) ([]identity.Membership, error) {
				memberships := make(map[string][]string)
				canonical := make(map[string]string)
				for _, user := range input.Users {
					if input.User != "" && !strings.EqualFold(input.User, user.Name) {
						continue
					}
					memberships[strings.ToLower(user.Name)] = []string{}
					canonical[strings.ToLower(user.Name)] = user.Name
				}
				for _, group := range input.Groups {
					data, err := executor.Execute(ctx, compatibility.Request{API: APIName, Version: 1, Method: "list", Parameters: url.Values{
						"group": {group.Name}, "ingroup": {"true"},
					}})
					if err != nil {
						return nil, fmt.Errorf("call %s.list v1 for group %q: %w", APIName, group.Name, err)
					}
					names, err := decodeMemberNames(data)
					if err != nil {
						return nil, fmt.Errorf("decode members of group %q: %w", group.Name, err)
					}
					for _, name := range names {
						key := strings.ToLower(name)
						if _, selected := memberships[key]; selected {
							memberships[key] = append(memberships[key], group.Name)
						}
					}
				}
				result := make([]identity.Membership, 0, len(memberships))
				for key, groups := range memberships {
					sort.Strings(groups)
					result = append(result, identity.Membership{User: canonical[key], Groups: groups})
				}
				sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i].User) < strings.ToLower(result[j].User) })
				return result, nil
			},
		},
	},
}

var changeOperation = compatibility.Operation[ChangeInput, Result]{
	Name: ChangeOperationName,
	Variants: []compatibility.Variant[ChangeInput, Result]{
		{
			Name: "core-group-member-change-v1", API: APIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(APIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input ChangeInput) (Result, error) {
				for _, group := range input.AddGroups {
					if err := changeGroup(ctx, executor, group, []string{input.User}, []string{}); err != nil {
						return Result{}, err
					}
				}
				for _, group := range input.RemoveGroups {
					if err := changeGroup(ctx, executor, group, []string{}, []string{input.User}); err != nil {
						return Result{}, err
					}
				}
				return Result{Resource: identity.ResourceMembership, Action: identity.ActionSet, Name: input.User}, nil
			},
		},
	},
}

func changeGroup(ctx context.Context, executor compatibility.Executor, group string, add, remove []string) error {
	_, err := executor.Execute(ctx, compatibility.Request{API: APIName, Version: 1, Method: "change", JSONParameters: map[string]any{
		"group": group, "add_member": add, "remove_member": remove,
	}})
	if err != nil {
		return fmt.Errorf("call %s.change v1 for group %q: %w", APIName, group, err)
	}
	return nil
}

func decodeMemberNames(data json.RawMessage) ([]string, error) {
	var response struct {
		Users []struct {
			Name string `json:"name"`
		} `json:"users"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	result := make([]string, 0, len(response.Users))
	for _, user := range response.Users {
		if user.Name != "" {
			result = append(result, user.Name)
		}
	}
	return result, nil
}

func APINames() []string { return []string{APIName} }

func Select(target compatibility.Target) ([]compatibility.Selection, error) {
	_, readSelection, readErr := readOperation.Select(target)
	if readErr != nil && !compatibility.IsUnsupported(readErr) {
		return nil, readErr
	}
	_, setSelection, setErr := changeOperation.Select(target)
	if setErr != nil && !compatibility.IsUnsupported(setErr) {
		return nil, setErr
	}
	return []compatibility.Selection{readSelection, setSelection}, nil
}

func ExecuteRead(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input ReadInput) ([]identity.Membership, compatibility.Selection, error) {
	return readOperation.Run(ctx, target, executor, input)
}

func ExecuteChange(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input ChangeInput) (Result, compatibility.Selection, error) {
	return changeOperation.Run(ctx, target, executor, input)
}
