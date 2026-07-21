package kmip

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDecodeStatusUnconfigured decodes the exact shape live-captured on the lab
// (DSM 7.3-81168), where the NAS reports KMIP as unsupported and both roles are
// disabled. The read must still succeed and report the honest disabled state.
func TestDecodeStatusUnconfigured(t *testing.T) {
	const live = `{"client_cert_info":null,"client_enable":false,"conn_success":false,` +
		`"conn_time":"","kmip_conn_server_desc":"","kmip_conn_server_port":"5696",` +
		`"kmip_db_loc":"","kmip_enabled":"","kmip_mode":"","kmip_server":"",` +
		`"kmip_server_port":"5696","server_cert_info":null,"server_enable":false,` +
		`"support_kmip":"no"}`
	status, err := decodeStatus(json.RawMessage(live))
	if err != nil {
		t.Fatalf("decodeStatus: %v", err)
	}
	if status.Supported {
		t.Errorf("Supported = true, want false for support_kmip:\"no\"")
	}
	if status.Server.Enabled || status.Client.Enabled {
		t.Errorf("roles should be disabled: %+v", status)
	}
	if status.Server.Certificate != nil || status.Client.Certificate != nil {
		t.Errorf("no certificate should be bound when cert_info is null: %+v", status)
	}
	if status.Server.ListenPort != "5696" || status.Client.ServerPort != "5696" {
		t.Errorf("port fields mismatch: %+v", status)
	}
	if status.Client.ConnectionOK {
		t.Errorf("ConnectionOK = true, want false")
	}
}

// TestDecodeStatusConfigured decodes a hypothetical fully-configured shape (both
// roles on, certificates bound). The populated cert_info shape is wire-unverified
// (null on the lab); this asserts the whitelist metadata decode and role wiring.
func TestDecodeStatusConfigured(t *testing.T) {
	const configured = `{
		"support_kmip": "yes",
		"kmip_mode": "server",
		"server_enable": true,
		"kmip_db_loc": "/volume1/@kmip",
		"kmip_server_port": "5696",
		"server_cert_info": {"id": "cert-srv-1", "desc": "KMIP server cert",
			"subject": {"common_name": "kmip.test-nas.example"}, "valid_till": "Mar 16 15:49:37 2030 GMT"},
		"client_enable": true,
		"kmip_server": "203.0.113.10",
		"kmip_conn_server_port": "5696",
		"kmip_conn_server_desc": "external-kms",
		"conn_success": true,
		"conn_time": "2030-01-01 12:00:00",
		"client_cert_info": {"id": "cert-cli-1", "subject": "cn=client.test-nas.example",
			"fingerprint": "AA:BB:CC:DD"}
	}`
	status, err := decodeStatus(json.RawMessage(configured))
	if err != nil {
		t.Fatalf("decodeStatus: %v", err)
	}
	if !status.Supported || status.Mode != "server" {
		t.Errorf("top-level mismatch: %+v", status)
	}
	if !status.Server.Enabled || status.Server.DatabaseLocation != "/volume1/@kmip" {
		t.Errorf("server role mismatch: %+v", status.Server)
	}
	if status.Server.Certificate == nil || !status.Server.Certificate.Bound ||
		status.Server.Certificate.ID != "cert-srv-1" ||
		status.Server.Certificate.Subject != "kmip.test-nas.example" {
		t.Errorf("server cert binding mismatch: %+v", status.Server.Certificate)
	}
	if !status.Client.Enabled || status.Client.ServerAddress != "203.0.113.10" ||
		status.Client.ServerName != "external-kms" || !status.Client.ConnectionOK {
		t.Errorf("client role mismatch: %+v", status.Client)
	}
	if status.Client.Certificate == nil || status.Client.Certificate.Subject != "cn=client.test-nas.example" ||
		status.Client.Certificate.Fingerprint != "AA:BB:CC:DD" {
		t.Errorf("client cert binding mismatch: %+v", status.Client.Certificate)
	}
}

// TestDecodeCertBindingNullAbsent asserts a null or absent cert_info yields nil.
func TestDecodeCertBindingNullAbsent(t *testing.T) {
	root := map[string]any{"server_cert_info": nil}
	if b := decodeCertBinding(root, "server_cert_info"); b != nil {
		t.Errorf("null cert_info should decode to nil, got %+v", b)
	}
	if b := decodeCertBinding(map[string]any{}, "client_cert_info"); b != nil {
		t.Errorf("absent cert_info should decode to nil, got %+v", b)
	}
}

// TestDecodersNeverLeakSecrets injects key-material / secret / passphrase /
// password / wrapped-key canaries into the root object AND into both certificate
// blocks, then asserts the decoded model — serialized back to JSON — carries
// neither the secret values nor any secret-bearing key. KMIP handles
// cryptographic key material, so the decoder must read only the explicit
// non-secret whitelist and never a whole-object passthrough.
func TestDecodersNeverLeakSecrets(t *testing.T) {
	const canary = "LEAK-CANARY-9f3a"
	secretPairs := `"private_key":"` + canary + `","key_material":"` + canary +
		`","secret":"` + canary + `","passphrase":"` + canary + `","password":"` + canary +
		`","wrapped_key":"` + canary + `","managed_key":"` + canary + `","pre_shared_key":"` + canary + `"`

	poisoned := `{
		"support_kmip": "yes",
		"kmip_mode": "client",
		"server_enable": true,
		"kmip_db_loc": "/volume1/@kmip",
		"server_cert_info": {"id":"c1","subject":{"common_name":"srv"},` + secretPairs + `},
		"client_enable": true,
		"kmip_server": "203.0.113.10",
		"client_cert_info": {"id":"c2","subject":"cli",` + secretPairs + `},
		` + secretPairs + `
	}`

	status, err := decodeStatus(json.RawMessage(poisoned))
	if err != nil {
		t.Fatalf("decodeStatus: %v", err)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	blob := string(encoded)
	if strings.Contains(blob, canary) {
		t.Errorf("decoded KMIP model leaked a secret canary: %s", blob)
	}
	for _, key := range []string{"private_key", "key_material", "secret", "passphrase", "password", "wrapped_key", "managed_key", "pre_shared_key"} {
		if strings.Contains(strings.ToLower(blob), key) {
			t.Errorf("decoded KMIP model carries a secret-bearing key %q: %s", key, blob)
		}
	}
}
