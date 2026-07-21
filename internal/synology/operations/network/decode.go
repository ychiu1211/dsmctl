package network

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/network"
)

// The decoders are strict about the response envelope (a malformed shape is an
// error, never a silently-empty success) and lenient about per-field presence,
// since DSM field sets vary across releases. They whitelist fields, so an
// unexpected response never smuggles surprise data into a model — and, crucially
// for the proxy area, the proxy password is never read into the model.

// decodeGeneral decodes SYNO.Core.Network get.
func decodeGeneral(data json.RawMessage) (network.General, error) {
	root, err := decodeObject(data, "network general")
	if err != nil {
		return network.General{}, err
	}
	if !hasAny(root, "server_name", "gateway", "dns_primary", "gateway_info") {
		return network.General{}, fmt.Errorf("decode network general: no recognized fields among %s", availableKeys(root))
	}
	general := network.General{
		Hostname:         stringValue(root, "server_name", "hostname", "server"),
		DefaultGatewayV4: stringValue(root, "gateway"),
		DefaultGatewayV6: stringValue(root, "v6gateway", "gateway_v6"),
		DNSPrimary:       stringValue(root, "dns_primary"),
		DNSSecondary:     stringValue(root, "dns_secondary"),
	}
	general.DNSManual, _ = boolValue(root, "dns_manual")
	general.UseDHCPDomain, _ = boolValue(root, "use_dhcp_domain")
	general.IPv4First, _ = boolValue(root, "ipv4_first")
	general.MultiGateway, _ = boolValue(root, "multi_gateway")
	general.ARPIgnore, _ = boolValue(root, "arp_ignore")
	general.IPConflictDetect, _ = boolValue(root, "enable_ip_conflict_detect")
	if info, ok := root["gateway_info"].(map[string]any); ok {
		general.DefaultGateway = network.GatewayInterface{
			Interface: stringValue(info, "ifname"),
			IP:        stringValue(info, "ip"),
			Netmask:   stringValue(info, "mask"),
			Status:    stringValue(info, "status"),
			Type:      stringValue(info, "type"),
		}
		general.DefaultGateway.UseDHCP, _ = boolValue(info, "use_dhcp")
	}
	return general, nil
}

// decodeProxy decodes SYNO.Core.Network.Proxy get. It deliberately NEVER reads
// the DSM "password" field: the proxy password is a secret and must not enter
// the domain model, results, logs, or MCP output (only presence/config).
func decodeProxy(data json.RawMessage) (network.ProxySettings, error) {
	root, err := decodeObject(data, "network proxy")
	if err != nil {
		return network.ProxySettings{}, err
	}
	if !hasAny(root, "enable", "http_host", "https_host", "enable_auth") {
		return network.ProxySettings{}, fmt.Errorf("decode network proxy: no recognized fields among %s", availableKeys(root))
	}
	proxy := network.ProxySettings{
		Supported: true,
		Username:  stringValue(root, "username"),
		HTTPHost:  stringValue(root, "http_host"),
		HTTPPort:  stringValue(root, "http_port"),
		HTTPSHost: stringValue(root, "https_host"),
		HTTPSPort: stringValue(root, "https_port"),
	}
	proxy.Enabled, _ = boolValue(root, "enable")
	proxy.AuthEnabled, _ = boolValue(root, "enable_auth")
	proxy.BypassLocal, _ = boolValue(root, "enable_bypass")
	proxy.DifferentHTTPS, _ = boolValue(root, "enable_different_host")
	return proxy, nil
}

// decodeInterfaces decodes SYNO.Core.Network.Ethernet list (or the sparser
// Interface list). Both return a bare JSON array of per-NIC objects. DSM has
// also been seen wrapping the array in {"interfaces":[...]} / {"ethernets":[...]}
// on some builds, so both shapes are accepted.
func decodeInterfaces(data json.RawMessage) ([]network.Interface, error) {
	items, err := decodeArray(data, "network interfaces", "interfaces", "ethernets", "ifaces")
	if err != nil {
		return nil, err
	}
	result := make([]network.Interface, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := stringValue(object, "ifname", "name")
		if name == "" {
			continue
		}
		iface := network.Interface{
			Name:         name,
			Type:         stringValue(object, "type"),
			IPv4:         stringValue(object, "ip"),
			Netmask:      stringValue(object, "mask"),
			GatewayV4:    stringValue(object, "gateway"),
			DNS:          stringValue(object, "dns"),
			MTU:          intValue(object, "mtu"),
			MTUConfig:    intValue(object, "mtu_config"),
			LinkStatus:   stringValue(object, "status"),
			SpeedMbps:    intValue(object, "speed"),
			MaxSpeedMbps: intValue(object, "max_supported_speed"),
			VLANID:       intValue(object, "vlan_id"),
			IPv6:         decodeIPv6List(object["ipv6"]),
		}
		iface.UseDHCP, _ = boolValue(object, "use_dhcp")
		iface.FullDuplex, _ = boolValue(object, "duplex")
		iface.IsDefaultGateway, _ = boolValue(object, "is_default_gateway")
		iface.VLANEnabled, _ = boolValue(object, "enable_vlan")
		result = append(result, iface)
	}
	return result, nil
}

// decodeIPv6List decodes the per-interface "ipv6" array. The array was empty on
// the lab (IPv6 off), so the element field names are wire-unverified; they are
// read tolerantly against the SYNO.Core.Network.IPv6 get shape (ip,
// prefix_length, type) plus common alternatives.
func decodeIPv6List(value any) []network.IPv6Address {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]network.IPv6Address, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		addr := network.IPv6Address{
			Address:      stringValue(object, "address", "ip", "addr"),
			PrefixLength: intValue(object, "prefix_length", "prefix", "prefixlen"),
			Type:         stringValue(object, "type", "mode"),
		}
		if addr.Address == "" && addr.Type == "" {
			continue
		}
		out = append(out, addr)
	}
	return out
}

// decodeBonds decodes SYNO.Core.Network.Bond list. The envelope (a bare array)
// is live-verified (empty on the lab). Per-bond Mode/Members field names are
// wire-unverified and read tolerantly.
func decodeBonds(data json.RawMessage) ([]network.Bond, error) {
	items, err := decodeArray(data, "network bonds", "bonds")
	if err != nil {
		return nil, err
	}
	result := make([]network.Bond, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := stringValue(object, "ifname", "name", "bond")
		if name == "" {
			continue
		}
		bond := network.Bond{
			Name:    name,
			Type:    stringValue(object, "type"),
			IPv4:    stringValue(object, "ip"),
			Netmask: stringValue(object, "mask"),
			Status:  stringValue(object, "status"),
			Mode:    stringValue(object, "bond_mode", "mode", "bonding_mode"),
			Members: decodeStringList(object, "slaves", "members", "eth_list", "servers"),
		}
		bond.UseDHCP, _ = boolValue(object, "use_dhcp")
		result = append(result, bond)
	}
	return result, nil
}

// decodeRoutes decodes SYNO.Core.Network.Router.Static.Route get. WIRE-UNVERIFIED:
// the lab returned code 4302 (no advanced routing), so the success shape is
// best-knowledge. It accepts a bare array or an {ipv4:[...],ipv6:[...]} /
// {routes:[...]} envelope and reads common field spellings tolerantly.
func decodeRoutes(data json.RawMessage) ([]network.Route, error) {
	root, err := decodeAny(data, "network routes")
	if err != nil {
		return nil, err
	}
	var result []network.Route
	switch typed := root.(type) {
	case []any:
		result = append(result, decodeRouteItems(typed, "")...)
	case map[string]any:
		if arr, ok := typed["ipv4"].([]any); ok {
			result = append(result, decodeRouteItems(arr, "ipv4")...)
		}
		if arr, ok := typed["ipv6"].([]any); ok {
			result = append(result, decodeRouteItems(arr, "ipv6")...)
		}
		for _, key := range []string{"routes", "rules", "list"} {
			if arr, ok := typed[key].([]any); ok {
				result = append(result, decodeRouteItems(arr, "")...)
			}
		}
	default:
		return nil, fmt.Errorf("decode network routes: unexpected response shape")
	}
	if result == nil {
		result = []network.Route{}
	}
	return result, nil
}

func decodeRouteItems(items []any, family string) []network.Route {
	out := make([]network.Route, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fam := family
		if fam == "" {
			fam = stringValue(object, "family", "ip_type")
		}
		out = append(out, network.Route{
			Destination: stringValue(object, "destination", "dest", "network", "ip"),
			Netmask:     stringValue(object, "netmask", "mask", "prefix"),
			Gateway:     stringValue(object, "gateway"),
			Interface:   stringValue(object, "interface", "ifname", "iface"),
			Family:      fam,
		})
	}
	return out
}

// --- shared lenient decoding helpers ---

func decodeObject(data json.RawMessage, what string) (map[string]any, error) {
	value, err := decodeAny(data, what)
	if err != nil {
		return nil, err
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("decode %s: response is not an object", what)
	}
	return root, nil
}

func decodeAny(data json.RawMessage, what string) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("decode %s: %w", what, err)
	}
	if root == nil {
		return nil, fmt.Errorf("decode %s: response is empty", what)
	}
	return root, nil
}

// decodeArray accepts either a bare JSON array or an object wrapping the array
// under one of wrapperKeys. A response that is neither is an error.
func decodeArray(data json.RawMessage, what string, wrapperKeys ...string) ([]any, error) {
	root, err := decodeAny(data, what)
	if err != nil {
		return nil, err
	}
	switch typed := root.(type) {
	case []any:
		return typed, nil
	case map[string]any:
		for _, key := range wrapperKeys {
			if arr, ok := typed[key].([]any); ok {
				return arr, nil
			}
		}
		return nil, fmt.Errorf("decode %s: object carries no array among %s", what, strings.Join(wrapperKeys, ", "))
	default:
		return nil, fmt.Errorf("decode %s: response is neither an array nor an object", what)
	}
}

func decodeStringList(values map[string]any, keys ...string) []string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		items, ok := value.([]any)
		if !ok {
			continue
		}
		out := make([]string, 0, len(items))
		for _, item := range items {
			switch typed := item.(type) {
			case string:
				if trimmed := strings.TrimSpace(typed); trimmed != "" {
					out = append(out, trimmed)
				}
			case map[string]any:
				if name := stringValue(typed, "ifname", "name"); name != "" {
					out = append(out, name)
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func hasAny(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := values[key]; ok {
			return true
		}
	}
	return false
}

func stringValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			if trimmed := strings.TrimSpace(typed); trimmed != "" {
				return trimmed
			}
		case json.Number:
			return typed.String()
		}
	}
	return ""
}

func intValue(values map[string]any, keys ...string) int {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case json.Number:
			if parsed, err := typed.Int64(); err == nil {
				return int(parsed)
			}
		case float64:
			return int(typed)
		case string:
			if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
				return parsed
			}
		}
	}
	return 0
}

func boolValue(values map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed, true
		case json.Number:
			return typed.String() != "0", true
		}
	}
	return false, false
}

func availableKeys(values map[string]any) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return "[" + strings.Join(keys, ", ") + "]"
}
