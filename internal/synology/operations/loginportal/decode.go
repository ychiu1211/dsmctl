package loginportal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/loginportal"
)

// The decoders are strict about the response envelope (a malformed shape is an
// error, never a silently-empty success) and lenient about per-field presence,
// since DSM field sets vary across releases and per configuration. No secret is
// ever decoded into a model: a reverse-proxy certificate is reported as presence
// only, and custom headers as a count only (never their values).

func decodeDSMWebService(data json.RawMessage) (loginportal.DSMWebService, error) {
	root, err := decodeObject(data, "DSM web service settings")
	if err != nil {
		return loginportal.DSMWebService{}, err
	}
	// A response carrying neither port is an unrecognized shape.
	if !hasAny(root, "http_port", "https_port") {
		return loginportal.DSMWebService{}, fmt.Errorf("decode DSM web service settings: no recognized fields among %s", availableKeys(root))
	}
	httpsEnabled, _ := boolValue(root, "enable_https")
	redirect, _ := boolValue(root, "enable_https_redirect")
	hsts, _ := boolValue(root, "enable_hsts")
	http2, _ := boolValue(root, "enable_spdy", "enable_http2")
	customDomain, _ := boolValue(root, "enable_custom_domain")
	return loginportal.DSMWebService{
		HTTPPort:            intValue(root, "http_port"),
		HTTPSPort:           intValue(root, "https_port"),
		HTTPSEnabled:        httpsEnabled,
		HTTPRedirectEnabled: redirect,
		HSTSEnabled:         hsts,
		HTTP2Enabled:        http2,
		CustomDomainEnabled: customDomain,
		CustomDomain:        stringValue(root, "fqdn", "custom_domain", "domain"),
	}, nil
}

func decodeExternalDomain(data json.RawMessage) (loginportal.DSMWebService, error) {
	root, err := decodeObject(data, "DSM external domain settings")
	if err != nil {
		return loginportal.DSMWebService{}, err
	}
	// The external get returns exactly {hostname}; the value may legitimately be
	// empty (no customized hostname configured), but the key must be present for
	// the shape to be recognized.
	if !hasAny(root, "hostname", "fqdn", "domain") {
		return loginportal.DSMWebService{}, fmt.Errorf("decode DSM external domain settings: no hostname among %s", availableKeys(root))
	}
	return loginportal.DSMWebService{
		ExternalDomainSupported: true,
		ExternalHostname:        stringValue(root, "hostname", "fqdn", "domain"),
	}, nil
}

func decodeApplicationPortals(data json.RawMessage) (loginportal.ApplicationPortals, error) {
	root, err := decodeObject(data, "application portals")
	if err != nil {
		return loginportal.ApplicationPortals{}, err
	}
	items, ok := objectList(root, "portal", "portals", "entries")
	if !ok {
		return loginportal.ApplicationPortals{}, fmt.Errorf("decode application portals: no portal array among %s", availableKeys(root))
	}
	portals := loginportal.ApplicationPortals{Portals: make([]loginportal.ApplicationPortal, 0, len(items))}
	for _, item := range items {
		id := stringValue(item, "id", "app_id", "appid")
		if id == "" {
			// An entry with no application id is unusable; skip it rather than
			// surface a blank portal.
			continue
		}
		redirect, _ := boolValue(item, "enable_redirect", "redirect", "enable_redirect_https")
		portals.Portals = append(portals.Portals, loginportal.ApplicationPortal{
			AppID:         id,
			DisplayName:   stringValue(item, "display_name", "title", "name"),
			RedirectHTTPS: redirect,
			Alias:         stringValue(item, "alias"),
			HTTPPort:      intValue(item, "http_port", "port"),
			HTTPSPort:     intValue(item, "https_port"),
		})
	}
	portals.Total = intValue(root, "total")
	if portals.Total == 0 {
		portals.Total = len(portals.Portals)
	}
	return portals, nil
}

func decodeReverseProxyRules(data json.RawMessage) (loginportal.ReverseProxyRules, error) {
	root, err := decodeObject(data, "reverse proxy rules")
	if err != nil {
		return loginportal.ReverseProxyRules{}, err
	}
	// The list envelope is the live-verified contract; a response with no entries
	// array is an unrecognized shape (an empty array is a valid, common result).
	rawEntries, present := root["entries"]
	if !present {
		if _, ok := root["rules"]; !ok {
			return loginportal.ReverseProxyRules{}, fmt.Errorf("decode reverse proxy rules: no entries array among %s", availableKeys(root))
		}
	}
	items, ok := objectList(root, "entries", "rules")
	if !ok {
		// entries present but not an array -> malformed. An absent-but-null entries
		// is treated as the empty set.
		if present && rawEntries != nil {
			return loginportal.ReverseProxyRules{}, fmt.Errorf("decode reverse proxy rules: entries is not an array")
		}
		items = nil
	}
	rules := loginportal.ReverseProxyRules{Rules: make([]loginportal.ReverseProxyRule, 0, len(items))}
	for _, item := range items {
		rules.Rules = append(rules.Rules, decodeReverseProxyRule(item))
	}
	rules.Total = intValue(root, "total")
	if rules.Total == 0 {
		rules.Total = len(rules.Rules)
	}
	return rules, nil
}

// decodeReverseProxyRule maps one reverse-proxy entry. The per-field key mapping
// is live-verified on DSM 7.3 (a probe rule was created, read back, and deleted):
// the stored id key is uppercase "UUID"; frontend/backend protocol is a numeric
// enum (0=http, 1=https); HSTS lives at frontend.https.hsts; there is no per-rule
// HTTP/2 field. Lenient decoding still tolerates the older spec-derived shape.
// Certificate key material is never surfaced (presence only) and header values
// are never surfaced (count only).
func decodeReverseProxyRule(item map[string]any) loginportal.ReverseProxyRule {
	hsts, _ := boolValue(item, "hsts", "enable_hsts", "frontend_hsts")
	http2, _ := boolValue(item, "http2", "enable_http2", "enable_spdy", "frontend_http2")
	rule := loginportal.ReverseProxyRule{
		UUID:               stringValue(item, "uuid", "id", "UUID"),
		Description:        stringValue(item, "description", "desc"),
		Frontend:           decodeEndpoint(item, "frontend", "fe"),
		Backend:            decodeEndpoint(item, "backend", "be"),
		HSTSEnabled:        hsts,
		HTTP2Enabled:       http2,
		CertificatePresent: certificatePresent(item),
		CustomHeaderCount:  headerCount(item),
	}
	// HSTS/HTTP2 may live inside the frontend sub-object. On DSM 7.3 HSTS is at
	// frontend.https.hsts; older/other builds may carry it flatter.
	if frontend, ok := objectValue(item, "frontend"); ok {
		if v, ok := boolValue(frontend, "hsts", "enable_hsts"); ok {
			rule.HSTSEnabled = v
		}
		if https, ok := objectValue(frontend, "https"); ok {
			if v, ok := boolValue(https, "hsts", "enable_hsts"); ok {
				rule.HSTSEnabled = v
			}
		}
		if v, ok := boolValue(frontend, "http2", "enable_http2", "enable_spdy"); ok {
			rule.HTTP2Enabled = v
		}
	}
	return rule
}

// decodeEndpoint reads one endpoint either as a nested object (item[side]) or
// from flat side_* / prefix_* keys.
func decodeEndpoint(item map[string]any, side, prefix string) loginportal.ReverseProxyEndpoint {
	if nested, ok := objectValue(item, side); ok {
		return loginportal.ReverseProxyEndpoint{
			Protocol: protocolString(nested, "protocol", "scheme"),
			Hostname: stringValue(nested, "fqdn", "hostname", "host", "servername", "server_name"),
			Port:     intValue(nested, "port"),
		}
	}
	return loginportal.ReverseProxyEndpoint{
		Protocol: protocolString(item, side+"_protocol", prefix+"_protocol"),
		Hostname: stringValue(item, side+"_fqdn", side+"_hostname", side+"_host", prefix+"_fqdn"),
		Port:     intValue(item, side+"_port", prefix+"_port"),
	}
}

// protocolString returns a stable "http"/"https" protocol name. DSM 7.3 stores
// the protocol as a numeric enum (0=http, 1=https); an already-string value
// ("http"/"https") is passed through so older shapes still decode.
func protocolString(values map[string]any, keys ...string) string {
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
		case bool:
			if typed {
				return "https"
			}
			return "http"
		default:
			switch intValueFromAny(value) {
			case 1:
				return "https"
			case 0:
				return "http"
			}
		}
	}
	return ""
}

// intValueFromAny extracts an int from a json.Number or float64, or returns -1.
func intValueFromAny(value any) int {
	switch typed := value.(type) {
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed)
		}
	case float64:
		return int(typed)
	}
	return -1
}

// certificatePresent reports only whether a certificate is referenced; it never
// returns the certificate id or any key material.
func certificatePresent(item map[string]any) bool {
	for _, key := range []string{"certificate", "cert", "certificate_id", "cert_id", "cert_name"} {
		if stringValue(item, key) != "" {
			return true
		}
	}
	if frontend, ok := objectValue(item, "frontend"); ok {
		for _, key := range []string{"certificate", "cert", "certificate_id", "cert_id", "cert_name"} {
			if stringValue(frontend, key) != "" {
				return true
			}
		}
	}
	return false
}

// headerCount reports only how many custom headers are configured; it never
// returns any header name or value (a header value can carry an injected token).
func headerCount(item map[string]any) int {
	for _, key := range []string{"customize_headers", "custom_headers", "headers", "proxy_headers"} {
		if arr, ok := arrayValue(item, key); ok {
			return len(arr)
		}
	}
	if proxy, ok := objectValue(item, "proxy_connection"); ok {
		for _, key := range []string{"customize_headers", "custom_headers", "headers"} {
			if arr, ok := arrayValue(proxy, key); ok {
				return len(arr)
			}
		}
	}
	return 0
}

// --- shared lenient decoding helpers (mirrors the accountprotection operation pkg) ---

func decodeObject(data json.RawMessage, what string) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var root map[string]any
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("decode %s: %w", what, err)
	}
	if root == nil {
		return nil, fmt.Errorf("decode %s: response is not an object", what)
	}
	return root, nil
}

func objectList(root map[string]any, keys ...string) ([]map[string]any, bool) {
	for _, key := range keys {
		value, ok := root[key]
		if !ok || value == nil {
			continue
		}
		items, ok := value.([]any)
		if !ok {
			continue
		}
		result := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if object, ok := item.(map[string]any); ok {
				result = append(result, object)
			}
		}
		return result, true
	}
	return nil, false
}

func objectValue(values map[string]any, keys ...string) (map[string]any, bool) {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		if object, ok := value.(map[string]any); ok {
			return object, true
		}
	}
	return nil, false
}

func arrayValue(values map[string]any, keys ...string) ([]any, bool) {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		if arr, ok := value.([]any); ok {
			return arr, true
		}
	}
	return nil, false
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
		if typed, ok := value.(bool); ok {
			return typed, true
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
