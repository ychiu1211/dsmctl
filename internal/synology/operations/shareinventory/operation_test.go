package shareinventory

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/identity"
	"github.com/ychiu1211/dsmctl/internal/domain/share"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type executorFunc func(context.Context, compatibility.Request) (json.RawMessage, error)

func (function executorFunc) Execute(ctx context.Context, request compatibility.Request) (json.RawMessage, error) {
	return function(ctx, request)
}

func TestExecuteNormalizesSharesAndOptInPermissionMatrix(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(ShareAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	target.SetAPI(PermissionAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})

	callCount := 0
	state, selections, err := Execute(context.Background(), target, executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		callCount++
		if request.Version != 1 {
			t.Fatalf("request = %#v", request)
		}
		switch request.API + "." + request.Method {
		case ShareAPIName + ".list":
			return fixture(t, "testdata/shares-v1.json"), nil
		case PermissionAPIName + ".list_by_user":
			if request.Parameters.Get("name") != "alice" || request.Parameters.Get("user_group_type") != "local_user" {
				t.Errorf("user permission params = %#v", request.Parameters)
			}
			return fixture(t, "testdata/permissions-user-v1.json"), nil
		case PermissionAPIName + ".list_by_group":
			if request.Parameters.Get("name") != "developers" || request.Parameters.Get("user_group_type") != "local_group" {
				t.Errorf("group permission params = %#v", request.Parameters)
			}
			return fixture(t, "testdata/permissions-group-v1.json"), nil
		default:
			t.Fatalf("unexpected request %#v", request)
			return nil, nil
		}
	}), Input{
		IncludePermissions: true,
		Users:              []identity.User{{Name: "alice"}},
		Groups:             []identity.Group{{Name: "developers"}},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if callCount != 3 || len(selections) != 2 || !InventorySupported(selections) || !PermissionsSupported(selections) {
		t.Fatalf("callCount=%d selections=%#v", callCount, selections)
	}
	if !state.PermissionsIncluded || len(state.Shares) != 2 {
		t.Fatalf("state = %#v", state)
	}
	projects := state.Shares[0]
	if projects.Name != "projects" || projects.QuotaBytes != 1099511627776 || len(projects.Permissions) != 2 {
		t.Fatalf("projects = %#v", projects)
	}
	if projects.Permissions[0].PrincipalType != share.PrincipalGroup || projects.Permissions[0].Access != share.AccessCustom {
		t.Fatalf("group permission = %#v", projects.Permissions[0])
	}
	if projects.Permissions[1].PrincipalType != share.PrincipalUser || projects.Permissions[1].Access != share.AccessWrite {
		t.Fatalf("user permission = %#v", projects.Permissions[1])
	}
}

func TestExecuteDoesNotReadPermissionsUnlessRequested(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(ShareAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	state, _, err := Execute(context.Background(), target, executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		if request.API != ShareAPIName || request.Method != "list" {
			t.Fatalf("unexpected request %#v", request)
		}
		return fixture(t, "testdata/shares-v1.json"), nil
	}), Input{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if state.PermissionsIncluded || len(state.Shares) != 2 {
		t.Fatalf("state = %#v", state)
	}
}

func fixture(t *testing.T, path string) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return data
}
