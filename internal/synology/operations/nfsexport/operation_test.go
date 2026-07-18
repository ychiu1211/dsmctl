package nfsexport

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/nfsexport"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type captureExecutor struct {
	responses map[string]json.RawMessage
	requests  []compatibility.Request
}

func (executor *captureExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	executor.requests = append(executor.requests, request)
	if response, ok := executor.responses[request.API+"."+request.Method]; ok {
		return response, nil
	}
	return json.RawMessage(`{}`), nil
}

func newTarget() compatibility.Target {
	target := compatibility.NewTarget()
	target.SetAPI(APIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 1})
	return target
}

func TestExportReadDecodesRules(t *testing.T) {
	target := newTarget()
	executor := &captureExecutor{responses: map[string]json.RawMessage{
		APIName + ".load": json.RawMessage(`{"rule":[
			{"id":"10.0.0.0/24","client":"10.0.0.0/24","privilege":"rw","root_squash":"root","security_flavor":{"sys":true,"kerberos":false,"kerberos_integrity":false,"kerberos_privacy":false},"async":true,"insecure":false,"crossmnt":true},
			{"id":"*","client":"*","privilege":"ro","root_squash":"all_guest","security_flavor":{"sys":false,"kerberos":true,"kerberos_integrity":false,"kerberos_privacy":false},"async":false,"insecure":1,"crossmnt":0}
		]}`),
	}}

	rules, selection, err := ExecuteRead(context.Background(), target, executor, ReadInput{Share: "backup"})
	if err != nil {
		t.Fatalf("ExecuteRead() error = %v", err)
	}
	if selection.Backend != "core-fileserv-nfs-shareprivilege-v1" {
		t.Fatalf("selection = %#v", selection)
	}
	want := []nfsexport.Rule{
		{Client: "10.0.0.0/24", Privilege: nfsexport.PrivilegeReadWrite, Squash: nfsexport.SquashNoMapping, Security: nfsexport.SecuritySys, Async: true, AllowNonprivilegedPorts: false, AllowSubfolderAccess: true},
		{Client: "*", Privilege: nfsexport.PrivilegeReadOnly, Squash: nfsexport.SquashAllToGuest, Security: nfsexport.SecurityKerberos, Async: false, AllowNonprivilegedPorts: true, AllowSubfolderAccess: false},
	}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("rules = %#v, want %#v", rules, want)
	}
	request := executor.requests[len(executor.requests)-1]
	if request.Method != "load" || request.Parameters.Get("share_name") != "backup" {
		t.Fatalf("load request = %#v", request)
	}
}

func TestExportSaveRequestShape(t *testing.T) {
	target := newTarget()
	executor := &captureExecutor{}

	_, _, err := ExecuteSet(context.Background(), target, executor, SaveInput{
		Share: "backup",
		Rules: []nfsexport.Rule{
			{Client: "10.0.0.0/24", Privilege: nfsexport.PrivilegeReadWrite, Squash: nfsexport.SquashRootToAdmin, Security: nfsexport.SecuritySys, Async: true, AllowNonprivilegedPorts: true, AllowSubfolderAccess: false},
			{Client: "192.168.1.5", Privilege: nfsexport.PrivilegeReadOnly, Squash: nfsexport.SquashNoMapping, Security: nfsexport.SecurityKerberosPrivacy, Async: false, AllowNonprivilegedPorts: false, AllowSubfolderAccess: true},
		},
		ExistingClients: map[string]struct{}{"10.0.0.0/24": {}},
	})
	if err != nil {
		t.Fatalf("ExecuteSet() error = %v", err)
	}
	request := executor.requests[len(executor.requests)-1]
	if request.Method != "save" || request.Parameters.Get("share_name") != "backup" {
		t.Fatalf("save request = %#v", request)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(request.Parameters.Get("rule")), &got); err != nil {
		t.Fatalf("save rule param is not a JSON array: %v", err)
	}
	want := []map[string]any{
		{"id": "10.0.0.0/24", "client": "10.0.0.0/24", "privilege": "rw", "root_squash": "admin", "security_flavor": map[string]any{"sys": true, "kerberos": false, "kerberos_integrity": false, "kerberos_privacy": false}, "async": true, "insecure": true, "crossmnt": false},
		{"id": "", "client": "192.168.1.5", "privilege": "ro", "root_squash": "root", "security_flavor": map[string]any{"sys": false, "kerberos": false, "kerberos_integrity": false, "kerberos_privacy": true}, "async": false, "insecure": false, "crossmnt": true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("save rule param = %#v, want %#v", got, want)
	}
}

func TestExportDecodeRejectsMalformed(t *testing.T) {
	if _, err := decodeRules(json.RawMessage(`{}`)); err == nil {
		t.Fatal("decodeRules() accepted a response with no rule field")
	}
	if _, err := decodeRules(json.RawMessage(`{"rule":[{"client":"*","privilege":"maybe","root_squash":"root","security_flavor":"sys","async":true,"insecure":false,"crossmnt":false}]}`)); err == nil {
		t.Fatal("decodeRules() accepted an unsupported privilege")
	}
	if _, err := decodeRules(json.RawMessage(`{"rule":[{"client":"","privilege":"rw","root_squash":"root","security_flavor":"sys","async":true,"insecure":false,"crossmnt":false}]}`)); err == nil {
		t.Fatal("decodeRules() accepted an empty client")
	}
}

func TestExportSetFailsClosedWithoutAPI(t *testing.T) {
	target := compatibility.NewTarget()
	selection, err := SelectSet(target)
	if err == nil || selection.Supported || !compatibility.IsUnsupported(err) {
		t.Fatalf("SelectSet() = %#v, %v", selection, err)
	}
}
