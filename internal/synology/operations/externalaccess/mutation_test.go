package externalaccess

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/externalaccess"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// captureExecutor records the last request so the write encodings can be pinned.
type captureExecutor struct{ request compatibility.Request }

func (e *captureExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	e.request = request
	return json.RawMessage(`{}`), nil
}

func writeTarget() compatibility.Target {
	return targetWith(map[string][2]int{
		QuickConnectAPI:     {1, 3},
		QuickConnectPermAPI: {1, 1},
		DDNSRecordAPI:       {1, 1},
	})
}

func boolPtr(v bool) *bool       { return &v }
func strPtr(v string) *string    { return &v }

func TestQuickConnectConfigSetSendsOnlyPatchedFields(t *testing.T) {
	executor := &captureExecutor{}
	_, _, err := ExecuteQuickConnectConfigSet(context.Background(), writeTarget(), executor, QuickConnectConfigSetInput{
		ServerAlias: strPtr("dsmctl-e2e-alias"), Region: strPtr("tw"),
	})
	if err != nil {
		t.Fatalf("ExecuteQuickConnectConfigSet() error = %v", err)
	}
	req := executor.request
	want := map[string]any{"server_alias": "dsmctl-e2e-alias", "region": "tw"}
	if req.API != QuickConnectAPI || req.Version != 2 || req.Method != "set" || !reflect.DeepEqual(req.JSONParameters, want) {
		t.Fatalf("config set request = %#v, want method set with %#v", req, want)
	}
	if _, ok := req.JSONParameters["enabled"]; ok {
		t.Fatalf("config set leaked an unpatched enabled field: %#v", req.JSONParameters)
	}
}

func TestQuickConnectConfigSetRejectsEmptyPatch(t *testing.T) {
	if _, _, err := ExecuteQuickConnectConfigSet(context.Background(), writeTarget(), &captureExecutor{}, QuickConnectConfigSetInput{}); err == nil {
		t.Fatal("ExecuteQuickConnectConfigSet() accepted an empty patch")
	}
}

func TestQuickConnectPermissionSetEncodesServicesArray(t *testing.T) {
	executor := &captureExecutor{}
	_, _, err := ExecuteQuickConnectPermissionSet(context.Background(), writeTarget(), executor, []externalaccess.QuickConnectService{
		{ID: "dsm_portal", Enabled: false},
	})
	if err != nil {
		t.Fatalf("ExecuteQuickConnectPermissionSet() error = %v", err)
	}
	req := executor.request
	if req.API != QuickConnectPermAPI || req.Version != 1 || req.Method != "set" {
		t.Fatalf("permission set request = %#v", req)
	}
	// services is passed as the slice itself (the client json-encodes it to the
	// raw array DSM expects); a pre-marshaled string would double-encode.
	got, ok := req.JSONParameters["services"].([]map[string]any)
	if !ok || len(got) != 1 || got[0]["id"] != "dsm_portal" || got[0]["enabled"] != false {
		t.Fatalf("services param = %#v", req.JSONParameters["services"])
	}
}

func TestDDNSSetCreateSendsCredentialFieldsAndDeleteOmitsThem(t *testing.T) {
	createExec := &captureExecutor{}
	_, _, err := ExecuteDDNSSet(context.Background(), writeTarget(), createExec, DDNSRecordSetInput{
		Action: externalaccess.DDNSActionCreate, Provider: "Synology", Hostname: "e2e.synology.me",
		Username: "u", Password: "resolved-secret", Enable: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("ExecuteDDNSSet(create) error = %v", err)
	}
	req := createExec.request
	if req.API != DDNSRecordAPI || req.Method != "create" {
		t.Fatalf("ddns create request = %#v", req)
	}
	if req.JSONParameters["passwd"] != "resolved-secret" || req.JSONParameters["provider"] != "Synology" || req.JSONParameters["hostname"] != "e2e.synology.me" {
		t.Fatalf("ddns create params = %#v", req.JSONParameters)
	}

	deleteExec := &captureExecutor{}
	_, _, err = ExecuteDDNSSet(context.Background(), writeTarget(), deleteExec, DDNSRecordSetInput{
		Action: externalaccess.DDNSActionDelete, Provider: "Synology", Hostname: "e2e.synology.me",
	})
	if err != nil {
		t.Fatalf("ExecuteDDNSSet(delete) error = %v", err)
	}
	if deleteExec.request.Method != "delete" {
		t.Fatalf("ddns delete method = %q", deleteExec.request.Method)
	}
	if _, ok := deleteExec.request.JSONParameters["passwd"]; ok {
		t.Fatalf("ddns delete leaked a password field: %#v", deleteExec.request.JSONParameters)
	}
	if _, ok := deleteExec.request.JSONParameters["enable"]; ok {
		t.Fatalf("ddns delete leaked an enable field: %#v", deleteExec.request.JSONParameters)
	}
}

func TestWriteSelectorsFailClosedWithoutAPIs(t *testing.T) {
	empty := compatibility.NewTarget()
	for name, sel := range map[string]func(compatibility.Target) (compatibility.Selection, error){
		"config set":     SelectQuickConnectConfigSet,
		"permission set": SelectQuickConnectPermissionSet,
		"ddns set":       SelectDDNSSet,
	} {
		if selection, err := sel(empty); err == nil || selection.Supported || !compatibility.IsUnsupported(err) {
			t.Fatalf("%s without API = %#v, %v", name, selection, err)
		}
	}
}
