package kmip

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/kmip"
)

// decodeStatus normalizes SYNO.Storage.CGI.KMIP.get into the combined KMIP
// role/status. Every field name below was live-verified against the lab
// (DSM 7.3-81168): the get response is a flat object with client_enable,
// server_enable, support_kmip, conn_success, conn_time, kmip_mode, kmip_server,
// kmip_server_port, kmip_conn_server_desc, kmip_conn_server_port, kmip_db_loc,
// and client_cert_info / server_cert_info (null when no cert is bound).
//
// SECRET HYGIENE: this decoder reads only the explicit non-secret keys named
// here. It never reads a private key, escrowed/managed/wrapped key bytes, a
// pre-shared secret, a passphrase, or a client credential — none of which DSM
// returns on get, and none of which this whitelist includes. The certificate
// bindings decode via decodeCertBinding, which is likewise whitelist-only.
func decodeStatus(data json.RawMessage) (kmip.Status, error) {
	root, err := decodeObject(data)
	if err != nil {
		return kmip.Status{}, fmt.Errorf("decode KMIP status: %w", err)
	}
	status := kmip.Status{
		Supported: yesValue(root, "support_kmip"),
		Mode:      stringValue(root, "kmip_mode"),
		Server: kmip.ServerRole{
			Enabled:          boolValue(root, "server_enable"),
			DatabaseLocation: stringValue(root, "kmip_db_loc"),
			ListenPort:       stringValue(root, "kmip_server_port"),
			Certificate:      decodeCertBinding(root, "server_cert_info"),
		},
		Client: kmip.ClientRole{
			Enabled:         boolValue(root, "client_enable"),
			ServerAddress:   stringValue(root, "kmip_server"),
			ServerPort:      stringValue(root, "kmip_conn_server_port"),
			ServerName:      stringValue(root, "kmip_conn_server_desc"),
			ConnectionOK:    boolValue(root, "conn_success"),
			LastConnectedAt: stringValue(root, "conn_time"),
			Certificate:     decodeCertBinding(root, "client_cert_info"),
		},
	}
	return status, nil
}

// decodeCertBinding normalizes a client_cert_info / server_cert_info block into
// the non-secret certificate identity. A null or absent block yields nil (no
// certificate bound). When present, only the whitelisted non-secret metadata
// keys are read; the exact populated key names are wire-unverified (both blocks
// were null on the lab), so a conservative whitelist of the well-known DSM
// certificate-metadata keys is used and a whole-object passthrough is never
// performed — any key/secret-bearing field is ignored by construction.
func decodeCertBinding(root map[string]any, key string) *kmip.CertBinding {
	raw, ok := root[key].(map[string]any)
	if !ok {
		return nil
	}
	binding := &kmip.CertBinding{
		Bound:       true,
		ID:          stringValue(raw, "id", "cert_id", "certificate_id"),
		Description: stringValue(raw, "desc", "description", "cert_desc"),
		Subject:     subjectValue(raw, "subject", "common_name", "cn", "subject_cn"),
		Issuer:      subjectValue(raw, "issuer", "issuer_cn"),
		Fingerprint: stringValue(raw, "fingerprint", "finger_print", "sha256_fingerprint"),
		ValidTill:   stringValue(raw, "valid_till", "valid_to", "not_after"),
	}
	return binding
}

// subjectValue reads a subject/issuer that DSM may report either as a plain
// string or as an object carrying a common_name. Only the common name (an
// identity) is read; no other nested field is touched.
func subjectValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		switch typed := values[key].(type) {
		case string:
			if typed != "" {
				return typed
			}
		case map[string]any:
			if cn := stringValue(typed, "common_name", "cn"); cn != "" {
				return cn
			}
		}
	}
	return ""
}

// --- tolerant decode helpers (shared shape with the other read modules) ---

func decodeObject(data json.RawMessage) (map[string]any, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func stringValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			if typed != "" {
				return typed
			}
		case json.Number:
			return typed.String()
		case float64:
			return strconv.FormatFloat(typed, 'f', -1, 64)
		case bool:
			return strconv.FormatBool(typed)
		}
	}
	return ""
}

// yesValue reports whether a DSM string/bool flag is affirmative. DSM reports
// support_kmip as the string "yes"/"no"; a bool true or numeric non-zero is also
// treated as affirmative for tolerance across builds.
func yesValue(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		switch typed := values[key].(type) {
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "yes", "true", "1", "enabled":
				return true
			}
		case bool:
			return typed
		case json.Number:
			return typed.String() != "0" && typed.String() != ""
		case float64:
			return typed != 0
		}
	}
	return false
}

func boolValue(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		switch typed := values[key].(type) {
		case bool:
			return typed
		case json.Number:
			return typed.String() != "0" && typed.String() != ""
		case float64:
			return typed != 0
		case string:
			result, _ := strconv.ParseBool(typed)
			return result || typed == "1" || strings.EqualFold(typed, "yes")
		}
	}
	return false
}
