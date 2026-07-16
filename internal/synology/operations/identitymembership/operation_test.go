package identitymembership

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/identity"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type membershipExecutor struct{ requests []compatibility.Request }

func (executor *membershipExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	executor.requests = append(executor.requests, request)
	if request.Method == "list" && request.Parameters.Get("group") == "users" {
		return json.RawMessage(`{"users":[{"name":"alice"},{"name":"bob"}]}`), nil
	}
	if request.Method == "list" {
		return json.RawMessage(`{"users":[{"name":"alice"}]}`), nil
	}
	return json.RawMessage(`{}`), nil
}

func TestReadBuildsSortedMembershipsAndChangeUsesTypedArrays(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(APIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 1})
	executor := &membershipExecutor{}
	memberships, _, err := ExecuteRead(context.Background(), target, executor, ReadInput{
		Users: []identity.User{{Name: "alice"}, {Name: "bob"}}, Groups: []identity.Group{{Name: "users"}, {Name: "dev"}},
	})
	if err != nil {
		t.Fatalf("ExecuteRead() error = %v", err)
	}
	if len(memberships) != 2 || len(memberships[0].Groups) != 2 || memberships[0].Groups[0] != "dev" || memberships[0].Groups[1] != "users" || memberships[1].Groups[0] != "users" {
		t.Fatalf("memberships = %#v", memberships)
	}
	executor.requests = nil
	_, _, err = ExecuteChange(context.Background(), target, executor, ChangeInput{User: "alice", AddGroups: []string{"ops"}, RemoveGroups: []string{"dev"}})
	if err != nil {
		t.Fatalf("ExecuteChange() error = %v", err)
	}
	if len(executor.requests) != 2 {
		t.Fatalf("change requests = %d, want 2", len(executor.requests))
	}
	add, ok := executor.requests[0].JSONParameters["add_member"].([]string)
	if !ok || len(add) != 1 || add[0] != "alice" {
		t.Fatalf("add_member = %#v", executor.requests[0].JSONParameters["add_member"])
	}
	remove, ok := executor.requests[1].JSONParameters["remove_member"].([]string)
	if !ok || len(remove) != 1 || remove[0] != "alice" {
		t.Fatalf("remove_member = %#v", executor.requests[1].JSONParameters["remove_member"])
	}
}
