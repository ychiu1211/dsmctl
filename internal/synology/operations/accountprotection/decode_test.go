package accountprotection

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestDecodersDropInjectedSecrets is the WI-067 no-secret-leak acceptance
// criterion on the read path: a malicious or buggy DSM response that smuggles an
// OTP secret, recovery code, SID, or SynoToken into any account-protection read
// must have that material dropped by the field-whitelist decoders. Re-encoding
// each decoded model shows no trace of it.
func TestDecodersDropInjectedSecrets(t *testing.T) {
	const canary = "SECRETCANARY-must-not-survive-decode"
	inject := `"otp_secret":"` + canary + `","otp_seed":"` + canary + `","recovery_code":"` + canary +
		`","backup_code":"` + canary + `","sid":"` + canary + `","_sid":"` + canary + `","SynoToken":"` + canary + `","syno_token":"` + canary + `"`

	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		AutoBlockAPIName + ".get":             json.RawMessage(`{"enable":true,"attempts":10,"within_mins":5,"expire_day":0,` + inject + `}`),
		SmartBlockAPIName + ".get":            json.RawMessage(`{"enabled":true,"untrust_try":5,"untrust_minute":1,"untrust_lock":30,"trust_try":10,"trust_minute":1,"trust_lock":30,` + inject + `}`),
		EnforcePolicyAPIName + ".get":         json.RawMessage(`{"otp_enforce_option":"all",` + inject + `}`),
		AutoBlockRulesAPIName + ".list.allow": json.RawMessage(`{"ip_info":[{"ip":"198.51.100.4",` + inject + `}],"offset":0,"total":1,` + inject + `}`),
		AutoBlockRulesAPIName + ".list.deny":  json.RawMessage(`{"ip_info":[],"offset":0,"total":0}`),
	}}

	models := make([]any, 0, 4)
	autoBlock, _, err := ExecuteAutoBlock(context.Background(), apTarget(), exec)
	if err != nil {
		t.Fatal(err)
	}
	models = append(models, autoBlock)
	lists, _, err := ExecuteAutoBlockList(context.Background(), apTarget(), exec)
	if err != nil {
		t.Fatal(err)
	}
	models = append(models, lists)
	protection, _, err := ExecuteAccountProtection(context.Background(), apTarget(), exec)
	if err != nil {
		t.Fatal(err)
	}
	models = append(models, protection)
	policy, _, err := ExecuteEnforceTwoFactor(context.Background(), apTarget(), exec)
	if err != nil {
		t.Fatal(err)
	}
	models = append(models, policy)

	for _, model := range models {
		encoded, err := json.Marshal(model)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), canary) {
			t.Fatalf("decoded model carried injected secret material: %s", encoded)
		}
		for _, forbidden := range []string{"otp_secret", "recovery_code", "syno_token", "SynoToken", "_sid"} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("decoded model exposes a secret-bearing field %q: %s", forbidden, encoded)
			}
		}
	}

	// Sanity: the legitimate fields still decoded through the injection.
	if !autoBlock.Enabled || lists.Allow.Entries[0].IP != "198.51.100.4" || !protection.Enabled || policy.Option != "all" {
		t.Fatalf("legitimate fields lost: %#v %#v %#v %#v", autoBlock, lists, protection, policy)
	}
}
