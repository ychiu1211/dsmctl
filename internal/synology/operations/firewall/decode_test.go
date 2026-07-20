package firewall

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeStatusRejectsUnknownShape(t *testing.T) {
	if _, err := decodeStatus(json.RawMessage(`{"unexpected":1}`)); err == nil || !strings.Contains(err.Error(), "no recognized fields") {
		t.Fatalf("error = %v", err)
	}
}

func TestDecodeStatusRejectsNonObject(t *testing.T) {
	// A JSON array fails to unmarshal into the object shape.
	if _, err := decodeStatus(json.RawMessage(`["default"]`)); err == nil || !strings.Contains(err.Error(), "decode firewall status") {
		t.Fatalf("error = %v", err)
	}
	// A JSON null is a non-object the decoder rejects explicitly.
	if _, err := decodeStatus(json.RawMessage(`null`)); err == nil || !strings.Contains(err.Error(), "not an object") {
		t.Fatalf("error = %v", err)
	}
}

func TestDecodeNameListRejectsMissingArray(t *testing.T) {
	// The expected key is present but not a string array.
	if _, err := decodeNameList(json.RawMessage(`{"profile_names":"default"}`), "profile_names"); err == nil || !strings.Contains(err.Error(), "no string array") {
		t.Fatalf("error = %v", err)
	}
	// No accepted key at all.
	if _, err := decodeNameList(json.RawMessage(`{"other":[]}`), "profile_names"); err == nil || !strings.Contains(err.Error(), "no string array") {
		t.Fatalf("error = %v", err)
	}
}

func TestDecodeProfileRulesRejectsUnrecognizedShape(t *testing.T) {
	// No "name" and no adapter section carrying a policy/rules field.
	if _, err := decodeProfileRules(json.RawMessage(`{"junk":123}`), ""); err == nil || !strings.Contains(err.Error(), "no name or adapter sections") {
		t.Fatalf("error = %v", err)
	}
}

func TestDecodeProfileRulesFallsBackToRequestedName(t *testing.T) {
	// A response with only an adapter section (no "name") still decodes, taking the
	// requested profile name.
	rules, err := decodeProfileRules(json.RawMessage(`{"global":{"policy":"allow","rules":null}}`), "custom")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if rules.Profile != "custom" || len(rules.Adapters) != 1 {
		t.Fatalf("rules = %#v", rules)
	}
	// A null rules array is an empty list, not a nil slice, and not an error.
	if rules.Adapters[0].Rules == nil || len(rules.Adapters[0].Rules) != 0 || rules.Adapters[0].Total != 0 {
		t.Fatalf("adapter = %#v", rules.Adapters[0])
	}
}

func TestDecodeProfileRulesSkipsNonObjectSections(t *testing.T) {
	// The "name" key and any non-object top-level value are not adapter sections.
	rules, err := decodeProfileRules(json.RawMessage(`{"name":"default","note":"hello","global":{"policy":"none","rules":[]}}`), "default")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(rules.Adapters) != 1 || rules.Adapters[0].Adapter != "global" {
		t.Fatalf("adapters = %#v", rules.Adapters)
	}
}

// TestDecodersDropUnexpectedFields proves the field-whitelist decoders never
// surface an unexpected field: a response that smuggles extra keys (e.g. a fake
// token) into a firewall read has them dropped, since only whitelisted fields are
// decoded into the typed model.
func TestDecodersDropUnexpectedFields(t *testing.T) {
	const canary = "CANARY-must-not-survive-decode"
	status, err := decodeStatus(json.RawMessage(`{"enable_firewall":true,"profile_name":"default","_sid":"` + canary + `","SynoToken":"` + canary + `"}`))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	rules, err := decodeProfileRules(json.RawMessage(`{"name":"custom","global":{"policy":"allow","secret":"`+canary+`","rules":[{"policy":"allow","token":"`+canary+`"}]}}`), "custom")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	for _, model := range []any{status, rules} {
		encoded, err := json.Marshal(model)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), canary) {
			t.Fatalf("decoded model carried unexpected field material: %s", encoded)
		}
	}
	// Sanity: the legitimate fields still decoded.
	if !status.Enabled || status.ActiveProfile != "default" || rules.Adapters[0].Policy != "allow" {
		t.Fatalf("legitimate fields lost: %#v %#v", status, rules)
	}
}
