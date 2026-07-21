package accountprotection

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// recordingExecutor returns a canned response per (api, method) and records the
// last request it saw for each, so tests can assert both decode results and the
// exact request contract (method, version, params) sent to DSM.
type recordingExecutor struct {
	responses map[string]json.RawMessage
	requests  []compatibility.Request
}

func (e *recordingExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	e.requests = append(e.requests, request)
	key := request.API + "." + request.Method
	// Distinguish the two allow/block list calls by their type discriminator.
	if request.JSONParameters != nil {
		if t, ok := request.JSONParameters["type"].(string); ok {
			key += "." + t
		}
	}
	if resp, ok := e.responses[key]; ok {
		return resp, nil
	}
	return json.RawMessage(`{}`), nil
}

func apTarget() compatibility.Target {
	target := compatibility.NewTarget()
	target.SetAPI(AutoBlockAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	target.SetAPI(AutoBlockRulesAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	target.SetAPI(SmartBlockAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	target.SetAPI(EnforcePolicyAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	target.SetAPI(DoSAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	target.SetAPI(ActiveConnectionsAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	return target
}

func TestSelectorsRequireTheirAPI(t *testing.T) {
	full := apTarget()
	empty := compatibility.NewTarget()
	cases := []struct {
		name    string
		backend string
		selectF func(compatibility.Target) (compatibility.Selection, error)
	}{
		{"autoblock", "account-protection-autoblock-get-v1", SelectAutoBlock},
		{"autoblock-list", "account-protection-autoblock-rules-list-v1", SelectAutoBlockList},
		{"protection", "account-protection-smartblock-get-v1", SelectAccountProtection},
		{"enforce-2fa", "account-protection-enforce-2fa-get-v1", SelectEnforceTwoFactor},
	}
	for _, tc := range cases {
		selection, err := tc.selectF(full)
		if err != nil || !selection.Supported || selection.Backend != tc.backend {
			t.Fatalf("%s: selection=%#v err=%v", tc.name, selection, err)
		}
		// Fail closed when the API is absent, without affecting sibling areas.
		selection, err = tc.selectF(empty)
		if !compatibility.IsUnsupported(err) || selection.Supported {
			t.Fatalf("%s: expected unsupported, got selection=%#v err=%v", tc.name, selection, err)
		}
	}
}

// TestIndependentBoundaries proves one area being absent never disables another:
// a target with only SmartBlock reports account protection supported and the
// rest unsupported.
func TestIndependentBoundaries(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(SmartBlockAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	selections := Select(target)
	got := map[string]bool{}
	for _, s := range selections {
		got[s.Operation] = s.Supported
	}
	if !got[AccountProtectionReadCapabilityName] {
		t.Fatalf("account protection should be supported: %#v", selections)
	}
	for _, op := range []string{AutoBlockReadCapabilityName, AutoBlockListReadCapabilityName, EnforceTwoFactorReadCapabilityName} {
		if got[op] {
			t.Fatalf("%s should be unsupported when only SmartBlock is present: %#v", op, selections)
		}
	}
	if SupportsDoS(target) {
		t.Fatalf("DoS should be absent")
	}
}

func TestExecuteAutoBlockDecodesLiveShape(t *testing.T) {
	// Response captured live from DSM 7.3 (SYNO.Core.Security.AutoBlock get).
	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		AutoBlockAPIName + ".get": json.RawMessage(`{"attempts":10,"enable":true,"expire_day":7,"within_mins":5}`),
	}}
	settings, selection, err := ExecuteAutoBlock(context.Background(), apTarget(), exec)
	if err != nil {
		t.Fatalf("ExecuteAutoBlock() error = %v", err)
	}
	if selection.Backend != "account-protection-autoblock-get-v1" {
		t.Fatalf("backend = %q", selection.Backend)
	}
	if !settings.Enabled || settings.Attempts != 10 || settings.WithinMinutes != 5 || settings.ExpireDays != 7 || !settings.ExpireEnabled {
		t.Fatalf("settings = %#v", settings)
	}
	req := exec.requests[0]
	if req.API != AutoBlockAPIName || req.Version != 1 || req.Method != "get" {
		t.Fatalf("request = %#v", req)
	}
}

func TestExecuteAutoBlockDerivesExpireDisabled(t *testing.T) {
	// The live default: expiration off is reported as expire_day 0 with no flag.
	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		AutoBlockAPIName + ".get": json.RawMessage(`{"attempts":10,"enable":false,"expire_day":0,"within_mins":5}`),
	}}
	settings, _, err := ExecuteAutoBlock(context.Background(), apTarget(), exec)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if settings.ExpireEnabled {
		t.Fatalf("expire should be disabled when expire_day is 0: %#v", settings)
	}
}

func TestExecuteAutoBlockRejectsUnknownShape(t *testing.T) {
	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		AutoBlockAPIName + ".get": json.RawMessage(`{"unexpected":1}`),
	}}
	if _, _, err := ExecuteAutoBlock(context.Background(), apTarget(), exec); err == nil || !strings.Contains(err.Error(), "no recognized fields") {
		t.Fatalf("error = %v", err)
	}
}

func TestExecuteAutoBlockListSendsTypeDiscriminatorAndDecodes(t *testing.T) {
	// Envelope shape captured live (SYNO.Core.Security.AutoBlock.Rules list); the
	// lab lists were empty, so a synthetic block entry exercises the decoder.
	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		AutoBlockRulesAPIName + ".list.allow": json.RawMessage(`{"ip_info":[],"offset":0,"total":0}`),
		AutoBlockRulesAPIName + ".list.deny":  json.RawMessage(`{"ip_info":[{"ip":"203.0.113.7","reason":"manual","record_time":1700000000}],"offset":0,"total":1}`),
	}}
	lists, selection, err := ExecuteAutoBlockList(context.Background(), apTarget(), exec)
	if err != nil {
		t.Fatalf("ExecuteAutoBlockList() error = %v", err)
	}
	if selection.Backend != "account-protection-autoblock-rules-list-v1" {
		t.Fatalf("backend = %q", selection.Backend)
	}
	if lists.Allow.Kind != "allow" || lists.Allow.Total != 0 || len(lists.Allow.Entries) != 0 {
		t.Fatalf("allow list = %#v", lists.Allow)
	}
	if lists.Block.Kind != "block" || lists.Block.Total != 1 || len(lists.Block.Entries) != 1 {
		t.Fatalf("block list = %#v", lists.Block)
	}
	e := lists.Block.Entries[0]
	if e.IP != "203.0.113.7" || e.Reason != "manual" || e.RecordTime != 1700000000 {
		t.Fatalf("block entry = %#v", e)
	}
	// Assert both list calls sent the required type discriminator (allow first,
	// then deny) — the read must never omit it (DSM returns error 5100 without it).
	if len(exec.requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(exec.requests))
	}
	seen := map[string]bool{}
	for _, req := range exec.requests {
		if req.API != AutoBlockRulesAPIName || req.Method != "list" || req.Version != 1 {
			t.Fatalf("request = %#v", req)
		}
		typ, ok := req.JSONParameters["type"].(string)
		if !ok || (typ != "allow" && typ != "deny") {
			t.Fatalf("request missing type discriminator: %#v", req.JSONParameters)
		}
		seen[typ] = true
	}
	if !seen["allow"] || !seen["deny"] {
		t.Fatalf("both allow and deny lists must be requested: %#v", seen)
	}
}

func TestExecuteAutoBlockListRejectsUnknownShape(t *testing.T) {
	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		AutoBlockRulesAPIName + ".list.allow": json.RawMessage(`{"items":[]}`),
	}}
	if _, _, err := ExecuteAutoBlockList(context.Background(), apTarget(), exec); err == nil || !strings.Contains(err.Error(), "no ip_info array") {
		t.Fatalf("error = %v", err)
	}
}

func TestExecuteAccountProtectionDecodesLiveShape(t *testing.T) {
	// Captured live (SYNO.Core.SmartBlock get).
	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		SmartBlockAPIName + ".get": json.RawMessage(`{"enabled":true,"trust_lock":30,"trust_minute":1,"trust_try":10,"untrust_lock":30,"untrust_minute":1,"untrust_try":5}`),
	}}
	protection, selection, err := ExecuteAccountProtection(context.Background(), apTarget(), exec)
	if err != nil {
		t.Fatalf("ExecuteAccountProtection() error = %v", err)
	}
	if selection.Backend != "account-protection-smartblock-get-v1" {
		t.Fatalf("backend = %q", selection.Backend)
	}
	if !protection.Enabled ||
		protection.UntrustedAttempts != 5 || protection.UntrustedWithinMinutes != 1 || protection.UntrustedBlockMinutes != 30 ||
		protection.TrustedAttempts != 10 || protection.TrustedWithinMinutes != 1 || protection.TrustedBlockMinutes != 30 {
		t.Fatalf("protection = %#v", protection)
	}
}

func TestExecuteEnforceTwoFactorDecodesLiveShape(t *testing.T) {
	// Captured live (SYNO.Core.OTP.EnforcePolicy get) — the lab scope is "none".
	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		EnforcePolicyAPIName + ".get": json.RawMessage(`{"otp_enforce_option":"none"}`),
	}}
	policy, selection, err := ExecuteEnforceTwoFactor(context.Background(), apTarget(), exec)
	if err != nil {
		t.Fatalf("ExecuteEnforceTwoFactor() error = %v", err)
	}
	if selection.Backend != "account-protection-enforce-2fa-get-v1" {
		t.Fatalf("backend = %q", selection.Backend)
	}
	if policy.Option != "none" || policy.Enabled {
		t.Fatalf("policy = %#v", policy)
	}
	// A non-none scope is reported enabled.
	exec.responses[EnforcePolicyAPIName+".get"] = json.RawMessage(`{"otp_enforce_option":"all"}`)
	exec.requests = nil
	policy, _, err = ExecuteEnforceTwoFactor(context.Background(), apTarget(), exec)
	if err != nil || policy.Option != "all" || !policy.Enabled {
		t.Fatalf("policy = %#v err = %v", policy, err)
	}
}

func TestExecuteEnforceTwoFactorRejectsMissingOption(t *testing.T) {
	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		EnforcePolicyAPIName + ".get": json.RawMessage(`{"something_else":true}`),
	}}
	if _, _, err := ExecuteEnforceTwoFactor(context.Background(), apTarget(), exec); err == nil || !strings.Contains(err.Error(), "no otp_enforce_option") {
		t.Fatalf("error = %v", err)
	}
}
