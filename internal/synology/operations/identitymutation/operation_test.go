package identitymutation

import (
	"encoding/json"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/identity"
)

func TestUserRequestCreateUsesPasswordOnlyInDSMParameters(t *testing.T) {
	description := "Automation account"
	email := "bot@example.com"
	expired := "normal"
	cannotChange := true
	neverExpires := true
	method, parameters, resultName, err := userRequest(UserInput{
		Action: identity.ActionCreate,
		Change: identity.UserChange{
			Name:                 "dsmctl-bot",
			Description:          &description,
			Email:                &email,
			Expired:              &expired,
			CannotChangePassword: &cannotChange,
			PasswordNeverExpires: &neverExpires,
			CredentialRef:        "env:DSMCTL_TEST_PASSWORD",
		},
		Password: "resolved-secret",
	})
	if err != nil {
		t.Fatalf("userRequest() error = %v", err)
	}
	if method != "create" || resultName != "dsmctl-bot" {
		t.Fatalf("method=%q resultName=%q", method, resultName)
	}
	for key, want := range map[string]string{
		"name":                "dsmctl-bot",
		"password":            "resolved-secret",
		"notify_by_email":     "false",
		"description":         description,
		"email":               email,
		"expired":             expired,
		"cannot_chg_passwd":   "true",
		"passwd_never_expire": "true",
	} {
		if got := parameters.Get(key); got != want {
			t.Errorf("parameter %s = %q, want %q", key, got, want)
		}
	}
	if parameters.Get("credential_ref") != "" {
		t.Fatal("credential_ref leaked into DSM parameters")
	}
}

func TestUserAndGroupUpdateAndDeleteRequests(t *testing.T) {
	newUserName := "new-user"
	method, parameters, resultName, err := userRequest(UserInput{Action: identity.ActionUpdate, Change: identity.UserChange{Name: "old-user", NewName: &newUserName}})
	if err != nil || method != "set" || resultName != newUserName || parameters.Get("name") != "old-user" || parameters.Get("new_name") != newUserName {
		t.Fatalf("user update: method=%q params=%v result=%q err=%v", method, parameters, resultName, err)
	}

	method, parameters, _, err = userRequest(UserInput{Action: identity.ActionDelete, Change: identity.UserChange{Name: "old-user"}})
	if err != nil || method != "delete" {
		t.Fatalf("user delete: method=%q err=%v", method, err)
	}
	assertNameArray(t, parameters.Get("name"), "old-user")

	description := "renamed group"
	newGroupName := "new-group"
	method, parameters, resultName, err = groupRequest(GroupInput{Action: identity.ActionUpdate, Change: identity.GroupChange{Name: "old-group", NewName: &newGroupName, Description: &description}})
	if err != nil || method != "set" || resultName != newGroupName || parameters.Get("new_name") != newGroupName || parameters.Get("description") != description {
		t.Fatalf("group update: method=%q params=%v result=%q err=%v", method, parameters, resultName, err)
	}
}

func TestUserCreateRejectsMissingResolvedPassword(t *testing.T) {
	_, _, _, err := userRequest(UserInput{Action: identity.ActionCreate, Change: identity.UserChange{Name: "missing-password"}})
	if err == nil {
		t.Fatal("userRequest() accepted an empty resolved password")
	}
}

func assertNameArray(t *testing.T, value, want string) {
	t.Helper()
	var names []string
	if err := json.Unmarshal([]byte(value), &names); err != nil {
		t.Fatalf("decode name array %q: %v", value, err)
	}
	if len(names) != 1 || names[0] != want {
		t.Fatalf("names = %#v, want [%q]", names, want)
	}
}
