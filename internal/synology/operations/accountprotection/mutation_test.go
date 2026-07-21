package accountprotection

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/accountprotection"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// TestExecuteAutoBlockSetSendsLiveWire captures the exact request the AutoBlock
// settings write sends: method set, version 1, with the live-verified raw field
// names {enable, attempts, within_mins, expire_day}.
func TestExecuteAutoBlockSetSendsLiveWire(t *testing.T) {
	exec := &recordingExecutor{}
	desired := accountprotection.AutoBlockSettings{Enabled: true, Attempts: 11, WithinMinutes: 6, ExpireEnabled: true, ExpireDays: 7}
	result, selection, err := ExecuteAutoBlockSet(context.Background(), apTarget(), exec, desired)
	if err != nil {
		t.Fatalf("ExecuteAutoBlockSet() error = %v", err)
	}
	if selection.Backend != "account-protection-autoblock-set-v1" || result.Method != "set" || result.API != AutoBlockAPIName {
		t.Fatalf("selection/result = %#v / %#v", selection, result)
	}
	req := exec.requests[0]
	if req.API != AutoBlockAPIName || req.Version != 1 || req.Method != "set" {
		t.Fatalf("request = %#v", req)
	}
	assertParam(t, req, "enable", true)
	assertParam(t, req, "attempts", 11)
	assertParam(t, req, "within_mins", 6)
	assertParam(t, req, "expire_day", 7)
	// The read-model field names (within_minutes, expire_days) must NOT appear.
	if _, ok := req.JSONParameters["within_minutes"]; ok {
		t.Fatalf("request leaked read-model field within_minutes: %#v", req.JSONParameters)
	}
	if _, ok := req.JSONParameters["expire_days"]; ok {
		t.Fatalf("request leaked read-model field expire_days: %#v", req.JSONParameters)
	}
}

func TestExecuteAccountProtectionSetSendsLiveWire(t *testing.T) {
	exec := &recordingExecutor{}
	desired := accountprotection.AccountProtection{
		Enabled: true, UntrustedAttempts: 6, UntrustedWithinMinutes: 1, UntrustedBlockMinutes: 30,
		TrustedAttempts: 10, TrustedWithinMinutes: 1, TrustedBlockMinutes: 30,
	}
	result, selection, err := ExecuteAccountProtectionSet(context.Background(), apTarget(), exec, desired)
	if err != nil {
		t.Fatalf("ExecuteAccountProtectionSet() error = %v", err)
	}
	if selection.Backend != "account-protection-smartblock-set-v1" || result.Method != "set" {
		t.Fatalf("selection/result = %#v / %#v", selection, result)
	}
	req := exec.requests[0]
	if req.API != SmartBlockAPIName || req.Version != 1 || req.Method != "set" {
		t.Fatalf("request = %#v", req)
	}
	assertParam(t, req, "enabled", true)
	assertParam(t, req, "untrust_try", 6)
	assertParam(t, req, "untrust_minute", 1)
	assertParam(t, req, "untrust_lock", 30)
	assertParam(t, req, "trust_try", 10)
	assertParam(t, req, "trust_minute", 1)
	assertParam(t, req, "trust_lock", 30)
	// SmartBlock's enable flag is "enabled", not AutoBlock's "enable".
	if _, ok := req.JSONParameters["enable"]; ok {
		t.Fatalf("SmartBlock set must use enabled, not enable: %#v", req.JSONParameters)
	}
}

func TestExecuteEnforceTwoFactorSetSendsLiveWire(t *testing.T) {
	exec := &recordingExecutor{}
	result, selection, err := ExecuteEnforceTwoFactorSet(context.Background(), apTarget(), exec, accountprotection.EnforceTwoFactor{Option: "none"})
	if err != nil {
		t.Fatalf("ExecuteEnforceTwoFactorSet() error = %v", err)
	}
	if selection.Backend != "account-protection-enforce-2fa-set-v1" || result.Method != "set" {
		t.Fatalf("selection/result = %#v / %#v", selection, result)
	}
	req := exec.requests[0]
	if req.API != EnforcePolicyAPIName || req.Version != 1 || req.Method != "set" {
		t.Fatalf("request = %#v", req)
	}
	assertParam(t, req, "otp_enforce_option", "none")
	// An empty option is rejected before any request is sent.
	exec2 := &recordingExecutor{}
	if _, _, err := ExecuteEnforceTwoFactorSet(context.Background(), apTarget(), exec2, accountprotection.EnforceTwoFactor{Option: ""}); err == nil {
		t.Fatalf("empty option should be rejected")
	}
	if len(exec2.requests) != 0 {
		t.Fatalf("no request should be sent for an empty option: %#v", exec2.requests)
	}
}

// TestExecuteIPListEditIsPatchOnly proves an add and a remove each touch exactly
// one entry via create/delete with {type, ip}, and never send a whole-list
// payload (no ip_info/ip_list array), so sibling entries can never be reset.
func TestExecuteIPListEditIsPatchOnly(t *testing.T) {
	cases := []struct {
		name       string
		edit       accountprotection.IPListEdit
		wantMethod string
		wantType   string
	}{
		{"block add", accountprotection.IPListEdit{Kind: accountprotection.KindBlock, IP: "192.0.2.1"}, "create", "deny"},
		{"block remove", accountprotection.IPListEdit{Kind: accountprotection.KindBlock, IP: "192.0.2.1", Remove: true}, "delete", "deny"},
		{"allow add", accountprotection.IPListEdit{Kind: accountprotection.KindAllow, IP: "192.0.2.9"}, "create", "allow"},
		{"allow remove", accountprotection.IPListEdit{Kind: accountprotection.KindAllow, IP: "192.0.2.9", Remove: true}, "delete", "allow"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec := &recordingExecutor{}
			result, selection, err := ExecuteIPListEdit(context.Background(), apTarget(), exec, tc.edit)
			if err != nil {
				t.Fatalf("ExecuteIPListEdit() error = %v", err)
			}
			if selection.Backend != "account-protection-autoblock-rules-edit-v1" || result.Method != tc.wantMethod {
				t.Fatalf("selection/result = %#v / %#v", selection, result)
			}
			if len(exec.requests) != 1 {
				t.Fatalf("expected exactly one request, got %d", len(exec.requests))
			}
			req := exec.requests[0]
			if req.API != AutoBlockRulesAPIName || req.Version != 1 || req.Method != tc.wantMethod {
				t.Fatalf("request = %#v", req)
			}
			assertParam(t, req, "type", tc.wantType)
			assertParam(t, req, "ip", tc.edit.IP)
			// The patch payload carries only {type, ip}: no whole-list array.
			if len(req.JSONParameters) != 2 {
				t.Fatalf("edit payload must be exactly {type, ip}: %#v", req.JSONParameters)
			}
			for _, banned := range []string{"ip_info", "ip_list", "rules", "list"} {
				if _, ok := req.JSONParameters[banned]; ok {
					t.Fatalf("edit must never send a whole-list field %q: %#v", banned, req.JSONParameters)
				}
			}
		})
	}
}

func TestExecuteIPListEditRejectsUnknownKind(t *testing.T) {
	exec := &recordingExecutor{}
	if _, _, err := ExecuteIPListEdit(context.Background(), apTarget(), exec, accountprotection.IPListEdit{Kind: "firewall", IP: "192.0.2.1"}); err == nil {
		t.Fatalf("unknown kind should be rejected")
	}
	if len(exec.requests) != 0 {
		t.Fatalf("no request should be sent for an unknown kind")
	}
}

func TestExecuteActiveConnectionsBestEffort(t *testing.T) {
	// Live-verified shape (SYNO.Core.CurrentConnection list): items[].from.
	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		ActiveConnectionsAPIName + ".list": json.RawMessage(`{"items":[{"from":"192.0.2.69","who":"testuser","is_current_connected":false},{"from":"","who":"blank"}],"total":2}`),
	}}
	connections, err := ExecuteActiveConnections(context.Background(), apTarget(), exec)
	if err != nil {
		t.Fatalf("ExecuteActiveConnections() error = %v", err)
	}
	if len(connections) != 1 || connections[0].From != "192.0.2.69" || connections[0].Who != "testuser" {
		t.Fatalf("connections = %#v", connections)
	}
	// A NAS without the API yields no connections and no error (best-effort).
	bare := compatibility.NewTarget()
	if got, err := ExecuteActiveConnections(context.Background(), bare, exec); err != nil || got != nil {
		t.Fatalf("missing API should yield nil,nil: got=%#v err=%v", got, err)
	}
}

func assertParam(t *testing.T, req compatibility.Request, key string, want any) {
	t.Helper()
	got, ok := req.JSONParameters[key]
	if !ok {
		t.Fatalf("request missing param %q: %#v", key, req.JSONParameters)
	}
	switch expected := want.(type) {
	case int:
		if asInt, ok := got.(int); !ok || asInt != expected {
			t.Fatalf("param %q = %#v, want int %d", key, got, expected)
		}
	default:
		if got != want {
			t.Fatalf("param %q = %#v, want %#v", key, got, want)
		}
	}
}
