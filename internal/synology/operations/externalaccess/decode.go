package externalaccess

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/externalaccess"
)

// unmarshalObject rejects an empty or non-object payload before decoding, so a
// silently changed DSM response shape fails loudly instead of yielding a zero
// value.
func unmarshalObject(data json.RawMessage, what string, destination any) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return fmt.Errorf("decode %s: empty response", what)
	}
	if trimmed[0] != '{' {
		return fmt.Errorf("decode %s: expected an object", what)
	}
	if err := json.Unmarshal(trimmed, destination); err != nil {
		return fmt.Errorf("decode %s: %w", what, err)
	}
	return nil
}

func decodeAccountCore(data json.RawMessage) (accountCore, error) {
	var resp struct {
		Account    *string `json:"account"`
		Activated  *bool   `json:"activated"`
		IsLoggedIn *bool   `json:"is_logged_in"`
	}
	if err := unmarshalObject(data, "Synology Account status", &resp); err != nil {
		return accountCore{}, err
	}
	if resp.IsLoggedIn == nil {
		return accountCore{}, errors.New("decode Synology Account status: required field \"is_logged_in\" is missing")
	}
	return accountCore{
		LoggedIn:  *resp.IsLoggedIn,
		Activated: resp.Activated != nil && *resp.Activated,
		Account:   strings.TrimSpace(deref(resp.Account)),
	}, nil
}

func decodeAccountPackage(data json.RawMessage) (accountPackage, error) {
	var resp struct {
		MyDSID *string `json:"myds_id"`
		Serial *string `json:"serial"`
	}
	if err := unmarshalObject(data, "Synology Account package info", &resp); err != nil {
		return accountPackage{}, err
	}
	if resp.MyDSID == nil {
		return accountPackage{}, errors.New("decode Synology Account package info: required field \"myds_id\" is missing")
	}
	return accountPackage{
		MyDSID: strings.TrimSpace(deref(resp.MyDSID)),
		Serial: strings.TrimSpace(deref(resp.Serial)),
	}, nil
}

func decodeQuickConnectConfig(data json.RawMessage) (quickConnectConfig, error) {
	var resp struct {
		Enabled     *bool   `json:"enabled"`
		ServerAlias *string `json:"server_alias"`
		Region      *string `json:"region"`
		Domain      *string `json:"domain"`
		DDNSDomain  *string `json:"ddns_domain"`
	}
	if err := unmarshalObject(data, "QuickConnect configuration", &resp); err != nil {
		return quickConnectConfig{}, err
	}
	if resp.Enabled == nil {
		return quickConnectConfig{}, errors.New("decode QuickConnect configuration: required field \"enabled\" is missing")
	}
	return quickConnectConfig{
		Enabled:      *resp.Enabled,
		ID:           strings.TrimSpace(deref(resp.ServerAlias)),
		Region:       strings.TrimSpace(deref(resp.Region)),
		Domain:       strings.TrimSpace(deref(resp.Domain)),
		DirectDomain: strings.TrimSpace(deref(resp.DDNSDomain)),
	}, nil
}

func decodeQuickConnectRelay(data json.RawMessage) (quickConnectRelay, error) {
	var resp struct {
		RelayEnabled *bool `json:"relay_enabled"`
	}
	if err := unmarshalObject(data, "QuickConnect relay setting", &resp); err != nil {
		return quickConnectRelay{}, err
	}
	if resp.RelayEnabled == nil {
		return quickConnectRelay{}, errors.New("decode QuickConnect relay setting: required field \"relay_enabled\" is missing")
	}
	return quickConnectRelay{RelayEnabled: *resp.RelayEnabled}, nil
}

func decodeQuickConnectStatus(data json.RawMessage) (quickConnectStatus, error) {
	var resp struct {
		Status      *string `json:"status"`
		AliasStatus *string `json:"alias_status"`
	}
	if err := unmarshalObject(data, "QuickConnect status", &resp); err != nil {
		return quickConnectStatus{}, err
	}
	if resp.Status == nil {
		return quickConnectStatus{}, errors.New("decode QuickConnect status: required field \"status\" is missing")
	}
	return quickConnectStatus{
		ConnectionStatus: strings.TrimSpace(deref(resp.Status)),
		AliasStatus:      strings.TrimSpace(deref(resp.AliasStatus)),
	}, nil
}

func decodeQuickConnectPermission(data json.RawMessage) ([]externalaccess.QuickConnectService, error) {
	var resp struct {
		Services *[]struct {
			ID      *string `json:"id"`
			Enabled *bool   `json:"enabled"`
		} `json:"services"`
	}
	if err := unmarshalObject(data, "QuickConnect permission", &resp); err != nil {
		return nil, err
	}
	if resp.Services == nil {
		return nil, errors.New("decode QuickConnect permission: required field \"services\" is missing")
	}
	services := make([]externalaccess.QuickConnectService, 0, len(*resp.Services))
	for _, entry := range *resp.Services {
		services = append(services, externalaccess.QuickConnectService{
			ID:      strings.TrimSpace(deref(entry.ID)),
			Enabled: entry.Enabled != nil && *entry.Enabled,
		})
	}
	return services, nil
}

// decodeDDNSRecords decodes the DDNS.Record list. The record entries are read
// tolerantly: the model NAS has no configured record, so each entry is decoded
// from whichever of the known keys DSM returns and unknown extras are ignored.
func decodeDDNSRecords(data json.RawMessage) (records []externalaccess.DDNSRecord, nextUpdate string, err error) {
	var resp struct {
		Records        *[]map[string]json.RawMessage `json:"records"`
		NextUpdateTime *string                       `json:"next_update_time"`
	}
	if err := unmarshalObject(data, "DDNS records", &resp); err != nil {
		return nil, "", err
	}
	if resp.Records == nil {
		return nil, "", errors.New("decode DDNS records: required field \"records\" is missing")
	}
	decoded := make([]externalaccess.DDNSRecord, 0, len(*resp.Records))
	for _, entry := range *resp.Records {
		decoded = append(decoded, externalaccess.DDNSRecord{
			Hostname: firstString(entry, "hostname", "record"),
			Provider: firstString(entry, "provider"),
			Status:   firstString(entry, "status"),
			IPv4:     firstString(entry, "ipv4", "ip", "external_ip"),
			IPv6:     firstString(entry, "ipv6"),
		})
	}
	return decoded, strings.TrimSpace(deref(resp.NextUpdateTime)), nil
}

func decodeDDNSExternalAddresses(data json.RawMessage) ([]externalaccess.ExternalAddress, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, errors.New("decode DDNS external IP: empty response")
	}
	if trimmed[0] != '[' {
		return nil, errors.New("decode DDNS external IP: expected an array")
	}
	var entries []struct {
		IP   *string `json:"ip"`
		IPv6 *string `json:"ipv6"`
		Type *string `json:"type"`
	}
	if err := json.Unmarshal(trimmed, &entries); err != nil {
		return nil, fmt.Errorf("decode DDNS external IP: %w", err)
	}
	addresses := make([]externalaccess.ExternalAddress, 0, len(entries))
	for _, entry := range entries {
		ipv6 := strings.TrimSpace(deref(entry.IPv6))
		if ipv6 == emptyIPv6 {
			ipv6 = ""
		}
		addresses = append(addresses, externalaccess.ExternalAddress{
			IP:   strings.TrimSpace(deref(entry.IP)),
			IPv6: ipv6,
			Type: strings.TrimSpace(deref(entry.Type)),
		})
	}
	return addresses, nil
}

func decodeRouterConf(data json.RawMessage) (externalaccess.PortForwardRouter, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return externalaccess.PortForwardRouter{}, errors.New("decode router configuration: expected an object")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return externalaccess.PortForwardRouter{}, fmt.Errorf("decode router configuration: %w", err)
	}
	if _, ok := raw["router_brand"]; !ok {
		return externalaccess.PortForwardRouter{}, errors.New("decode router configuration: required field \"router_brand\" is missing")
	}
	return externalaccess.PortForwardRouter{
		Brand:             firstString(raw, "router_brand"),
		Model:             firstString(raw, "router_model"),
		Version:           firstString(raw, "router_version"),
		SupportUPnP:       stringOrBool(raw, "support_upnp"),
		SupportNATPMP:     stringOrBool(raw, "support_natpmp"),
		SupportChangePort: boolValue(raw, "support_change_port"),
	}, nil
}

func decodePortForwardRules(data json.RawMessage) ([]externalaccess.PortForwardRule, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, errors.New("decode port-forwarding rules: expected an array")
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &entries); err != nil {
		return nil, fmt.Errorf("decode port-forwarding rules: %w", err)
	}
	rules := make([]externalaccess.PortForwardRule, 0, len(entries))
	for _, entry := range entries {
		rules = append(rules, externalaccess.PortForwardRule{
			Description: firstString(entry, "description", "desc", "name"),
			Protocol:    firstString(entry, "protocol", "proto"),
			PublicPort:  firstString(entry, "public_port", "src_port", "from_port"),
			PrivatePort: firstString(entry, "private_port", "dest_port", "to_port"),
		})
	}
	return rules, nil
}

// stringOrBool reads a DSM field that is a string on some releases ("yes"/"no")
// and a bool on others, normalizing a bool to "yes"/"no".
func stringOrBool(entry map[string]json.RawMessage, key string) string {
	raw, ok := entry[key]
	if !ok {
		return ""
	}
	raw = bytes.TrimSpace(raw)
	if bytes.Equal(raw, []byte("true")) {
		return "yes"
	}
	if bytes.Equal(raw, []byte("false")) {
		return "no"
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return strings.TrimSpace(value)
	}
	return ""
}

// boolValue reads a bool field, tolerating a string "true"/"false".
func boolValue(entry map[string]json.RawMessage, key string) bool {
	raw, ok := entry[key]
	if !ok {
		return false
	}
	raw = bytes.TrimSpace(raw)
	var asBool bool
	if err := json.Unmarshal(raw, &asBool); err == nil {
		return asBool
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.EqualFold(strings.TrimSpace(asString), "true")
	}
	return false
}

// emptyIPv6 is DSM's placeholder for "no IPv6 address"; it is normalized away.
const emptyIPv6 = "0:0:0:0:0:0:0:0"

func firstString(entry map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		raw, ok := entry[key]
		if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
