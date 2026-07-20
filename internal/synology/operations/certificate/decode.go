package certificate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ychiu1211/dsmctl/internal/domain/certificate"
)

// The decoder is strict about the response envelope and lenient about
// per-certificate fields (they vary across DSM releases). It never carries
// private-key material into the model — only the public metadata DSM reports.

// dsmCertTimeLayout is the format SYNO.Core.Certificate.CRT uses for
// valid_from/valid_till, e.g. "Mar 16 15:49:37 2027 GMT".
const dsmCertTimeLayout = "Jan _2 15:04:05 2006 MST"

func decodeCertificates(data json.RawMessage) (certificate.Certificates, error) {
	root, err := decodeObject(data, "certificate list")
	if err != nil {
		return certificate.Certificates{}, err
	}
	items, ok := objectList(root, "certificates", "certs", "data")
	if !ok {
		return certificate.Certificates{}, fmt.Errorf("decode certificate list: no certificate array among %s", availableKeys(root))
	}
	result := certificate.Certificates{Certificates: make([]certificate.Certificate, 0, len(items))}
	for index, item := range items {
		id := stringValue(item, "id")
		if id == "" {
			return certificate.Certificates{}, fmt.Errorf("decode certificate %d: no id field among %s", index, availableKeys(item))
		}
		cert := certificate.Certificate{
			ID:                 id,
			Description:        stringValue(item, "desc", "description"),
			KeyTypes:           stringValue(item, "key_types"),
			SignatureAlgorithm: stringValue(item, "signature_algorithm"),
			Subject:            decodeName(item, "subject"),
			Issuer:             decodeName(item, "issuer"),
			ValidFrom:          stringValue(item, "valid_from"),
			ValidTill:          stringValue(item, "valid_till"),
			SubjectAltNames:    subjectAltNames(item),
			Services:           decodeServices(item),
		}
		cert.IsDefault, _ = boolValue(item, "is_default")
		cert.IsBroken, _ = boolValue(item, "is_broken")
		cert.Renewable, _ = boolValue(item, "renewable")
		cert.UserDeletable, _ = boolValue(item, "user_deletable")
		// DSM has no explicit self-signed flag; a self-signed cert carries the
		// self_signed_cacrt_info block. Fall back to issuer == subject CN.
		if _, present := item["self_signed_cacrt_info"]; present {
			cert.SelfSigned = true
		} else if cert.Issuer.CommonName != "" && cert.Issuer.CommonName == cert.Subject.CommonName {
			cert.SelfSigned = true
		}
		cert.ValidFromUnix = parseCertTime(cert.ValidFrom)
		cert.ValidTillUnix = parseCertTime(cert.ValidTill)
		result.Certificates = append(result.Certificates, cert)
	}
	result.Total = intValue(root, "total")
	if result.Total == 0 {
		result.Total = len(result.Certificates)
	}
	return result, nil
}

func decodeName(item map[string]any, key string) certificate.Name {
	raw, ok := item[key].(map[string]any)
	if !ok {
		return certificate.Name{}
	}
	return certificate.Name{
		CommonName:   stringValue(raw, "common_name"),
		Organization: stringValue(raw, "organization"),
		Country:      stringValue(raw, "country"),
	}
}

func subjectAltNames(item map[string]any) []string {
	raw, ok := item["subject"].(map[string]any)
	if !ok {
		return nil
	}
	list, ok := raw["sub_alt_name"].([]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(list))
	for _, v := range list {
		if s, ok := v.(string); ok && s != "" {
			names = append(names, s)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func decodeServices(item map[string]any) []certificate.Service {
	list, ok := item["services"].([]any)
	if !ok {
		return nil
	}
	services := make([]certificate.Service, 0, len(list))
	for _, v := range list {
		svc, ok := v.(map[string]any)
		if !ok {
			continue
		}
		display := stringValue(svc, "display_name")
		if display == "" {
			display = stringValue(svc, "display_name_i18n")
		}
		isPkg, _ := boolValue(svc, "isPkg", "is_package")
		services = append(services, certificate.Service{
			Service:     stringValue(svc, "service"),
			Subscriber:  stringValue(svc, "subscriber"),
			Owner:       stringValue(svc, "owner"),
			DisplayName: display,
			IsPackage:   isPkg,
		})
	}
	if len(services) == 0 {
		return nil
	}
	return services
}

func parseCertTime(value string) int64 {
	if value == "" {
		return 0
	}
	if t, err := time.Parse(dsmCertTimeLayout, value); err == nil {
		return t.Unix()
	}
	return 0
}

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
