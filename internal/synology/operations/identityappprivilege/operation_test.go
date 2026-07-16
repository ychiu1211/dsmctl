package identityappprivilege

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/identity"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type appPrivilegeExecutor struct{ requests []compatibility.Request }

func (executor *appPrivilegeExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	executor.requests = append(executor.requests, request)
	if request.API == AppAPIName {
		return json.RawMessage(`{"applications":[{"app_id":"SYNO.Desktop","name":["DSM"],"grant_type":["local"],"supportIP":true}]}`), nil
	}
	if request.Method == "get" {
		return json.RawMessage(`{"rules":[{"app_id":"SYNO.Desktop","allow_ip":[],"deny_ip":["0.0.0.0"]},{"app_id":"custom","allow_ip":["10.0.0.0/8"],"deny_ip":[]}]}`), nil
	}
	return json.RawMessage(`{}`), nil
}

func TestApplicationInventoryRulesAndPartialSet(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(AppAPIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 3})
	target.SetAPI(RuleAPIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 1})
	executor := &appPrivilegeExecutor{}
	applications, _, err := ExecuteApps(context.Background(), target, executor)
	if err != nil || len(applications) != 1 || applications[0].Name != "DSM" || !applications[0].SupportsIP {
		t.Fatalf("applications=%#v err=%v", applications, err)
	}
	assignment, _, err := ExecuteRead(context.Background(), target, executor, PrincipalInput{PrincipalType: identity.PrincipalUser, Principal: "alice"})
	if err != nil || len(assignment.Permissions) != 2 || assignment.Permissions[0].Access != identity.ApplicationAccessDeny || assignment.Permissions[1].Access != identity.ApplicationAccessCustom {
		t.Fatalf("assignment=%#v err=%v", assignment, err)
	}
	executor.requests = nil
	_, _, err = ExecuteSet(context.Background(), target, executor, SetInput{PrincipalType: identity.PrincipalUser, Principal: "alice", Permissions: []identity.ApplicationPermissionChange{
		{ApplicationID: "old", Access: identity.ApplicationAccessInherit}, {ApplicationID: "SYNO.Desktop", Access: identity.ApplicationAccessAllow},
	}})
	if err != nil || len(executor.requests) != 2 {
		t.Fatalf("set requests=%d err=%v", len(executor.requests), err)
	}
	if executor.requests[0].Method != "delete" || executor.requests[1].Method != "set" {
		t.Fatalf("methods=%q,%q", executor.requests[0].Method, executor.requests[1].Method)
	}
	rules, ok := executor.requests[1].JSONParameters["rules"].([]map[string]any)
	if !ok || len(rules) != 1 {
		t.Fatalf("set rules = %#v", executor.requests[1].JSONParameters["rules"])
	}
	allow := rules[0]["allow_ip"].([]string)
	deny := rules[0]["deny_ip"].([]string)
	if len(allow) != 1 || allow[0] != "0.0.0.0" || len(deny) != 0 {
		t.Fatalf("allow=%v deny=%v", allow, deny)
	}
}
