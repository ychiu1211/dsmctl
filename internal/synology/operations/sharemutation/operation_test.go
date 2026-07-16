package sharemutation

import (
	"encoding/json"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/share"
)

func TestShareCreateAndUpdateEncodeNestedShareInfo(t *testing.T) {
	description := "Managed by dsmctl"
	hidden := true
	recycle := true
	adminOnly := true
	hideUnreadable := true
	cow := true
	compression := false
	quota := uint64(1024)
	method, parameters, resultName, err := shareRequest(ShareInput{
		Action: share.ActionCreate,
		Change: share.ShareChange{
			Name:                "dsmctl-data",
			VolumePath:          "/volume1",
			Description:         &description,
			Hidden:              &hidden,
			RecycleBin:          &recycle,
			RecycleBinAdminOnly: &adminOnly,
			HideUnreadable:      &hideUnreadable,
			EnableCOW:           &cow,
			EnableCompression:   &compression,
			QuotaMiB:            &quota,
		},
	})
	if err != nil || method != "create" || resultName != "dsmctl-data" || parameters.Get("name") != "dsmctl-data" {
		t.Fatalf("share create: method=%q params=%v result=%q err=%v", method, parameters, resultName, err)
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(parameters.Get("shareinfo")), &info); err != nil {
		t.Fatalf("decode shareinfo: %v", err)
	}
	if info["name"] != "dsmctl-data" || info["vol_path"] != "/volume1" || info["desc"] != description || info["share_quota"] != "1024" {
		t.Fatalf("shareinfo = %#v", info)
	}

	newName := "dsmctl-renamed"
	method, parameters, resultName, err = shareRequest(ShareInput{Action: share.ActionUpdate, Change: share.ShareChange{Name: "dsmctl-data", NewName: &newName, Hidden: &hidden}})
	if err != nil || method != "set" || resultName != newName {
		t.Fatalf("share update: method=%q result=%q err=%v", method, resultName, err)
	}
	if err := json.Unmarshal([]byte(parameters.Get("shareinfo")), &info); err != nil {
		t.Fatalf("decode update shareinfo: %v", err)
	}
	if info["name"] != newName || info["name_org"] != "dsmctl-data" {
		t.Fatalf("update shareinfo = %#v", info)
	}
}

func TestPermissionRequestMapsNormalizedAccess(t *testing.T) {
	method, parameters, err := permissionRequest(share.PermissionChange{
		PrincipalType: share.PrincipalUser,
		Principal:     "dsmctl-user",
		Permissions: []share.PermissionGrant{
			{ShareName: "read-only", Access: share.AccessRead},
			{ShareName: "write", Access: share.AccessWrite},
			{ShareName: "blocked", Access: share.AccessDeny},
			{ShareName: "unset", Access: share.AccessNone},
		},
	})
	if err != nil || method != "set_by_user_group" || parameters.Get("name") != "dsmctl-user" || parameters.Get("user_group_type") != "local_user" {
		t.Fatalf("permission request: method=%q params=%v err=%v", method, parameters, err)
	}
	var permissions []map[string]any
	if err := json.Unmarshal([]byte(parameters.Get("permissions")), &permissions); err != nil {
		t.Fatalf("decode permissions: %v", err)
	}
	if len(permissions) != 4 || permissions[0]["is_readonly"] != true || permissions[1]["is_writable"] != true || permissions[2]["is_deny"] != true {
		t.Fatalf("permissions = %#v", permissions)
	}
	for _, key := range []string{"is_readonly", "is_writable", "is_deny", "is_custom"} {
		if permissions[3][key] != false {
			t.Errorf("none permission %s = %#v", key, permissions[3][key])
		}
	}
}

func TestShareDeleteUsesExactNameArray(t *testing.T) {
	method, parameters, _, err := shareRequest(ShareInput{Action: share.ActionDelete, Change: share.ShareChange{Name: "dsmctl-delete"}})
	if err != nil || method != "delete" {
		t.Fatalf("share delete: method=%q err=%v", method, err)
	}
	var names []string
	if err := json.Unmarshal([]byte(parameters.Get("name")), &names); err != nil {
		t.Fatalf("decode names: %v", err)
	}
	if len(names) != 1 || names[0] != "dsmctl-delete" {
		t.Fatalf("names = %#v", names)
	}
}
