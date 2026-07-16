package identitymutation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/identity"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	UserAPIName         = "SYNO.Core.User"
	GroupAPIName        = "SYNO.Core.Group"
	UserCapabilityName  = "identity.users.mutate"
	GroupCapabilityName = "identity.groups.mutate"
	UserOperationName   = "identity.users.mutate"
	GroupOperationName  = "identity.groups.mutate"
)

type UserInput struct {
	Action   string
	Change   identity.UserChange
	Password string
}

type GroupInput struct {
	Action string
	Change identity.GroupChange
}

type Result struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
	Name     string `json:"name"`
}

var userOperation = compatibility.Operation[UserInput, Result]{
	Name: UserOperationName,
	Variants: []compatibility.Variant[UserInput, Result]{
		{
			Name:     "core-user-mutation-v1",
			API:      UserAPIName,
			Version:  1,
			Priority: 10,
			Match:    compatibility.APIVersion(UserAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input UserInput) (Result, error) {
				method, parameters, resultName, err := userRequest(input)
				if err != nil {
					return Result{}, err
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: UserAPIName, Version: 1, Method: method, Parameters: parameters}); err != nil {
					return Result{}, fmt.Errorf("call %s.%s v1: %w", UserAPIName, method, err)
				}
				return Result{Resource: identity.ResourceUser, Action: input.Action, Name: resultName}, nil
			},
		},
	},
}

var groupOperation = compatibility.Operation[GroupInput, Result]{
	Name: GroupOperationName,
	Variants: []compatibility.Variant[GroupInput, Result]{
		{
			Name:     "core-group-mutation-v1",
			API:      GroupAPIName,
			Version:  1,
			Priority: 10,
			Match:    compatibility.APIVersion(GroupAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input GroupInput) (Result, error) {
				method, parameters, resultName, err := groupRequest(input)
				if err != nil {
					return Result{}, err
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: GroupAPIName, Version: 1, Method: method, Parameters: parameters}); err != nil {
					return Result{}, fmt.Errorf("call %s.%s v1: %w", GroupAPIName, method, err)
				}
				return Result{Resource: identity.ResourceGroup, Action: input.Action, Name: resultName}, nil
			},
		},
	},
}

func APINames() []string {
	return []string{GroupAPIName, UserAPIName}
}

func Select(target compatibility.Target) ([]compatibility.Selection, error) {
	_, userSelection, userErr := userOperation.Select(target)
	if userErr != nil && !compatibility.IsUnsupported(userErr) {
		return nil, userErr
	}
	_, groupSelection, groupErr := groupOperation.Select(target)
	if groupErr != nil && !compatibility.IsUnsupported(groupErr) {
		return nil, groupErr
	}
	return []compatibility.Selection{userSelection, groupSelection}, nil
}

func ExecuteUser(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input UserInput) (Result, compatibility.Selection, error) {
	return userOperation.Run(ctx, target, executor, input)
}

func ExecuteGroup(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input GroupInput) (Result, compatibility.Selection, error) {
	return groupOperation.Run(ctx, target, executor, input)
}

func userRequest(input UserInput) (string, url.Values, string, error) {
	change := input.Change
	parameters := make(url.Values)
	resultName := change.Name
	switch input.Action {
	case identity.ActionCreate:
		if input.Password == "" {
			return "", nil, "", fmt.Errorf("password is required to create user %q", change.Name)
		}
		parameters.Set("name", change.Name)
		parameters.Set("password", input.Password)
		parameters.Set("notify_by_email", "false")
		setUserFields(parameters, change)
		return "create", parameters, resultName, nil
	case identity.ActionUpdate:
		parameters.Set("name", change.Name)
		if change.NewName != nil {
			resultName = strings.TrimSpace(*change.NewName)
			parameters.Set("new_name", resultName)
		} else {
			parameters.Set("new_name", change.Name)
		}
		if input.Password != "" {
			parameters.Set("password", input.Password)
		}
		setUserFields(parameters, change)
		return "set", parameters, resultName, nil
	case identity.ActionDelete:
		names, _ := json.Marshal([]string{change.Name})
		parameters.Set("name", string(names))
		return "delete", parameters, resultName, nil
	default:
		return "", nil, "", fmt.Errorf("unsupported user action %q", input.Action)
	}
}

func setUserFields(parameters url.Values, change identity.UserChange) {
	setOptionalString(parameters, "description", change.Description)
	setOptionalString(parameters, "email", change.Email)
	setOptionalString(parameters, "expired", change.Expired)
	setOptionalBool(parameters, "cannot_chg_passwd", change.CannotChangePassword)
	setOptionalBool(parameters, "passwd_never_expire", change.PasswordNeverExpires)
}

func groupRequest(input GroupInput) (string, url.Values, string, error) {
	change := input.Change
	parameters := make(url.Values)
	resultName := change.Name
	switch input.Action {
	case identity.ActionCreate:
		parameters.Set("name", change.Name)
		setOptionalString(parameters, "description", change.Description)
		return "create", parameters, resultName, nil
	case identity.ActionUpdate:
		parameters.Set("name", change.Name)
		if change.NewName != nil {
			resultName = strings.TrimSpace(*change.NewName)
			parameters.Set("new_name", resultName)
		} else {
			parameters.Set("new_name", change.Name)
		}
		setOptionalString(parameters, "description", change.Description)
		return "set", parameters, resultName, nil
	case identity.ActionDelete:
		names, _ := json.Marshal([]string{change.Name})
		parameters.Set("name", string(names))
		return "delete", parameters, resultName, nil
	default:
		return "", nil, "", fmt.Errorf("unsupported group action %q", input.Action)
	}
}

func setOptionalString(parameters url.Values, key string, value *string) {
	if value != nil {
		parameters.Set(key, *value)
	}
}

func setOptionalBool(parameters url.Values, key string, value *bool) {
	if value != nil {
		parameters.Set(key, strconv.FormatBool(*value))
	}
}
