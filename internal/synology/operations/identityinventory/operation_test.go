package identityinventory

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type executorFunc func(context.Context, compatibility.Request) (json.RawMessage, error)

func (function executorFunc) Execute(ctx context.Context, request compatibility.Request) (json.RawMessage, error) {
	return function(ctx, request)
}

func TestExecuteSelectsV1AndNormalizesUsersAndGroups(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(UserAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	target.SetAPI(GroupAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})

	state, selections, err := Execute(context.Background(), target, executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		if request.Version != 1 {
			t.Fatalf("request = %#v", request)
		}
		switch request.API + "." + request.Method {
		case UserAPIName + ".list":
			if request.Parameters.Get("limit") != "-1" || request.Parameters.Get("type") != "local" {
				t.Fatalf("request = %#v", request)
			}
			if request.Parameters.Get("additional") != additionalUserFields {
				t.Errorf("user additional = %q", request.Parameters.Get("additional"))
			}
			return fixture(t, "testdata/users-v1.json"), nil
		case GroupAPIName + ".list":
			if request.Parameters.Get("limit") != "-1" || request.Parameters.Get("type") != "local" {
				t.Fatalf("request = %#v", request)
			}
			if request.Parameters.Get("additional") != additionalGroupFields {
				t.Errorf("group additional = %q", request.Parameters.Get("additional"))
			}
			return fixture(t, "testdata/groups-v1.json"), nil
		case GroupAPIName + ".get":
			if request.Parameters.Get("name") != "backup-operators" {
				t.Fatalf("group get request = %#v", request)
			}
			return json.RawMessage(`{"groups":[{"gid":65537,"name":"backup-operators","description":"Backup service accounts"}]}`), nil
		default:
			t.Fatalf("unexpected request %#v", request)
			return nil, nil
		}
	}))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(selections) != 2 || !Supported(selections) {
		t.Fatalf("selections = %#v", selections)
	}
	if len(state.Users) != 2 || state.Users[0].Name != "alice" || state.Users[0].ID != "1026" || !state.Users[0].PasswordNeverExpires {
		t.Fatalf("users = %#v", state.Users)
	}
	if state.Users[1].TwoFactorStatus != "disabled" || !state.Users[1].Expired {
		t.Fatalf("second user = %#v", state.Users[1])
	}
	if len(state.Groups) != 2 || state.Groups[0].Name != "administrators" || state.Groups[0].ID != "65536" {
		t.Fatalf("groups = %#v", state.Groups)
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
