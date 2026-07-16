package identityinventory

import (
	"context"
	"fmt"
	"net/url"
	"sort"

	"github.com/ychiu1211/dsmctl/internal/domain/identity"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	UserAPIName           = "SYNO.Core.User"
	GroupAPIName          = "SYNO.Core.Group"
	CapabilityName        = "identity.inventory"
	UserOperationName     = "identity.users.list"
	GroupOperationName    = "identity.groups.list"
	additionalUserFields  = `["description","email","expired","passwd_never_expire","2fa_status","uid"]`
	additionalGroupFields = `["description","gid"]`
)

type Input struct{}

var userOperation = compatibility.Operation[Input, []identity.User]{
	Name: UserOperationName,
	Variants: []compatibility.Variant[Input, []identity.User]{
		{
			Name:     "core-user-v1",
			API:      UserAPIName,
			Version:  1,
			Priority: 10,
			Match:    compatibility.APIVersion(UserAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) ([]identity.User, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API:     UserAPIName,
					Version: 1,
					Method:  "list",
					Parameters: url.Values{
						"offset":     {"0"},
						"limit":      {"-1"},
						"type":       {"local"},
						"additional": {additionalUserFields},
					},
				})
				if err != nil {
					return nil, fmt.Errorf("call %s.list v1: %w", UserAPIName, err)
				}
				return decodeUsers(data)
			},
		},
	},
}

var groupOperation = compatibility.Operation[Input, []identity.Group]{
	Name: GroupOperationName,
	Variants: []compatibility.Variant[Input, []identity.Group]{
		{
			Name:     "core-group-v1",
			API:      GroupAPIName,
			Version:  1,
			Priority: 10,
			Match:    compatibility.APIVersion(GroupAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) ([]identity.Group, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API:     GroupAPIName,
					Version: 1,
					Method:  "list",
					Parameters: url.Values{
						"offset":     {"0"},
						"limit":      {"-1"},
						"type":       {"local"},
						"additional": {additionalGroupFields},
					},
				})
				if err != nil {
					return nil, fmt.Errorf("call %s.list v1: %w", GroupAPIName, err)
				}
				groups, err := decodeGroups(data)
				if err != nil {
					return nil, err
				}
				for index := range groups {
					if groups[index].ID != "" {
						continue
					}
					detail, err := executor.Execute(ctx, compatibility.Request{
						API:     GroupAPIName,
						Version: 1,
						Method:  "get",
						Parameters: url.Values{
							"name": {groups[index].Name},
						},
					})
					if err != nil {
						return nil, fmt.Errorf("call %s.get v1 for %q: %w", GroupAPIName, groups[index].Name, err)
					}
					details, err := decodeGroups(detail)
					if err != nil {
						return nil, err
					}
					if len(details) != 1 || details[0].Name != groups[index].Name {
						return nil, fmt.Errorf("call %s.get v1 for %q returned no exact group", GroupAPIName, groups[index].Name)
					}
					groups[index] = details[0]
				}
				return groups, nil
			},
		},
	},
}

func APINames() []string {
	unique := make(map[string]struct{})
	for _, name := range append(userOperation.APINames(), groupOperation.APINames()...) {
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
	selections := make([]compatibility.Selection, 0, 2)
	for _, selectOperation := range []func(compatibility.Target) (compatibility.Selection, error){selectUsers, selectGroups} {
		selection, err := selectOperation(target)
		selections = append(selections, selection)
		if err != nil && !compatibility.IsUnsupported(err) {
			return nil, err
		}
	}
	return selections, nil
}

func Execute(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (identity.State, []compatibility.Selection, error) {
	users, userSelection, err := userOperation.Run(ctx, target, executor, Input{})
	if err != nil {
		return identity.State{}, []compatibility.Selection{userSelection}, err
	}
	groups, groupSelection, err := groupOperation.Run(ctx, target, executor, Input{})
	selections := []compatibility.Selection{userSelection, groupSelection}
	if err != nil {
		return identity.State{}, selections, err
	}
	return identity.State{Users: users, Groups: groups}, selections, nil
}

func selectUsers(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := userOperation.Select(target)
	return selection, err
}

func selectGroups(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := groupOperation.Select(target)
	return selection, err
}

func Supported(selections []compatibility.Selection) bool {
	return len(selections) == 2 && selections[0].Supported && selections[1].Supported
}
