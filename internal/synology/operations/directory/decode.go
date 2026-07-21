package directory

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/directory"
)

// decodeDomain normalizes SYNO.Core.Directory.Domain.get into the AD
// domain-membership state. The join flag (enable_domain) was live-verified on
// DSM 7.3; the joined-identity fields are read tolerantly because an unjoined
// NAS omits them.
//
// SECRET HYGIENE: no field here reads a password or credential. DSM does not
// return the join password on get, and the decoder deliberately reads only the
// non-secret identity/config keys named below.
func decodeDomain(data json.RawMessage) (directory.DomainState, error) {
	root, err := decodeObject(data)
	if err != nil {
		return directory.DomainState{}, fmt.Errorf("decode domain status: %w", err)
	}
	return directory.DomainState{
		Joined:             boolValue(root, "enable_domain", "joined", "enabled"),
		DomainFQDN:         stringValue(root, "domain_fqdn", "domainname", "domain_name", "realm", "fqdn"),
		Workgroup:          stringValue(root, "workgroup", "netbios", "nmbns"),
		DNSServer:          stringValue(root, "dns", "dns_server", "domain_dns"),
		DomainController:   stringValue(root, "dc", "domain_controller", "pdc"),
		OrganizationalUnit: stringValue(root, "ou", "organizational_unit"),
		ConnectionStatus:   stringValue(root, "status", "connection_status", "conn_status"),
	}, nil
}

// decodeDomainOptions normalizes SYNO.Core.Directory.Domain.Conf.get, the
// non-secret AD join-option toggles. Live-verified on DSM 7.3.
func decodeDomainOptions(data json.RawMessage) (directory.DomainOptions, error) {
	root, err := decodeObject(data)
	if err != nil {
		return directory.DomainOptions{}, fmt.Errorf("decode domain options: %w", err)
	}
	return directory.DomainOptions{
		BuildDatabaseWithMembership: boolValue(root, "buildDatabaseWithMembership"),
		DirectConnectTrust:          boolValue(root, "direct_connect_trust"),
		DisableDomainAdmins:         boolValue(root, "disable_domain_admins"),
		DomainNestedGroupLevel:      intValueOr(root, 0, "domain_nested_group"),
		EnableRPCEnumUserGroup:      boolValue(root, "enable_rpc_enum_usergroup"),
		EnableSyncTime:              boolValue(root, "enable_sync_time"),
		EncryptADLDAP:               stringValue(root, "encrypt_ad_ldap"),
	}, nil
}

// decodeDomainSchedule normalizes SYNO.Core.Directory.Domain.Schedule.get. DSM
// reports a sparse shape on an unscheduled NAS; every field is read tolerantly.
func decodeDomainSchedule(data json.RawMessage) (directory.DomainSchedule, error) {
	root, err := decodeObject(data)
	if err != nil {
		return directory.DomainSchedule{}, fmt.Errorf("decode domain schedule: %w", err)
	}
	schedule := directory.DomainSchedule{
		Enabled:  boolValue(root, "enable", "enabled"),
		DateType: intValueOr(root, 0, "date_type"),
	}
	if hour, ok := intValue(root, "hour", "run_hour"); ok {
		schedule.Hour = &hour
	}
	if minute, ok := intValue(root, "minute", "run_min", "min"); ok {
		schedule.Minute = &minute
	}
	if weekday, ok := intValue(root, "weekday", "week", "run_weekday"); ok {
		schedule.Weekday = &weekday
	}
	if monthDay, ok := intValue(root, "monthday", "run_day", "date"); ok {
		schedule.MonthDay = &monthDay
	}
	return schedule, nil
}

// decodeLDAP normalizes SYNO.Core.Directory.LDAP.get into the LDAP client
// state. The v2 shape (server_address, expand_nested_groups) was live-verified
// on DSM 7.3; the v1 shape carries host instead of server_address, so both keys
// are read.
//
// SECRET HYGIENE: DSM never returns the LDAP bind password on get, and this
// decoder reads only the non-secret configuration keys named below. BindDN is a
// distinguished name (identity), not a secret.
func decodeLDAP(data json.RawMessage) (directory.LDAPState, error) {
	root, err := decodeObject(data)
	if err != nil {
		return directory.LDAPState{}, fmt.Errorf("decode LDAP status: %w", err)
	}
	return directory.LDAPState{
		Bound:              boolValue(root, "enable_client", "enabled", "bound"),
		ServerAddress:      stringValue(root, "server_address", "host"),
		BaseDN:             stringValue(root, "base_dn"),
		BindDN:             stringValue(root, "bind_dn", "manager_dn", "root_dn"),
		Encryption:         stringValue(root, "encryption"),
		Profile:            stringValue(root, "profile"),
		Schema:             stringValue(root, "ldap_schema"),
		IsSynologyServer:   boolValue(root, "is_syno_server"),
		ExpandNestedGroups: boolValue(root, "expand_nested_groups"),
		NestedGroupLevel:   intValueOr(root, 0, "nested_group_level"),
		RequireTLSCert:     boolValue(root, "tls_reqcert"),
		UpdateMinutes:      intValueOr(root, 0, "update_min"),
		EnableCIFS:         boolValue(root, "enable_cifs"),
		EnableIDMap:        boolValue(root, "enable_idmap"),
		ErrorCode:          intValueOr(root, 0, "error"),
	}, nil
}

// decodeLDAPProfiles normalizes SYNO.Core.Directory.LDAP.Profile.list into the
// list of offered schema profiles.
func decodeLDAPProfiles(data json.RawMessage) ([]string, error) {
	root, err := decodeObject(data)
	if err != nil {
		return nil, fmt.Errorf("decode LDAP profiles: %w", err)
	}
	raw, ok := root["profiles"].([]any)
	if !ok {
		return nil, nil
	}
	profiles := make([]string, 0, len(raw))
	for _, item := range raw {
		if name, ok := item.(string); ok && name != "" {
			profiles = append(profiles, name)
		}
	}
	return profiles, nil
}

// decodeUsers normalizes SYNO.Core.User.list into the synced-user page. Only
// non-secret identity fields are read; no password hash or keytab field is
// touched even if DSM were to include one.
func decodeUsers(data json.RawMessage, source directory.Mode) (directory.DirectoryUsers, error) {
	root, err := decodeObject(data)
	if err != nil {
		return directory.DirectoryUsers{}, fmt.Errorf("decode directory users: %w", err)
	}
	page := directory.DirectoryUsers{
		Mode:   source,
		Total:  intValueOr(root, 0, "total"),
		Offset: intValueOr(root, 0, "offset"),
		Users:  []directory.DomainUser{},
	}
	for _, item := range objectList(root, "users") {
		user := directory.DomainUser{
			Name:        stringValue(item, "name"),
			Description: stringValue(item, "description"),
			Source:      source,
		}
		if uid, ok := intValue(item, "uid"); ok {
			user.UID = &uid
		}
		page.Users = append(page.Users, user)
	}
	return page, nil
}

// decodeGroups normalizes SYNO.Core.Group.list into the synced-group page.
func decodeGroups(data json.RawMessage, source directory.Mode) (directory.DirectoryGroups, error) {
	root, err := decodeObject(data)
	if err != nil {
		return directory.DirectoryGroups{}, fmt.Errorf("decode directory groups: %w", err)
	}
	page := directory.DirectoryGroups{
		Mode:   source,
		Total:  intValueOr(root, 0, "total"),
		Offset: intValueOr(root, 0, "offset"),
		Groups: []directory.DomainGroup{},
	}
	for _, item := range objectList(root, "groups") {
		group := directory.DomainGroup{
			Name:        stringValue(item, "name"),
			Description: stringValue(item, "description"),
			Source:      source,
		}
		if gid, ok := intValue(item, "gid"); ok {
			group.GID = &gid
		}
		page.Groups = append(page.Groups, group)
	}
	return page, nil
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

func objectList(values map[string]any, keys ...string) []map[string]any {
	for _, key := range keys {
		items, ok := values[key].([]any)
		if !ok {
			continue
		}
		result := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if object, ok := item.(map[string]any); ok {
				result = append(result, object)
			}
		}
		return result
	}
	return nil
}

func stringValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			return typed
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

// intValue reports the first present integer-valued key and whether one was
// found, so a caller can distinguish an absent field from a real zero.
func intValue(values map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		switch typed := values[key].(type) {
		case json.Number:
			if result, err := strconv.Atoi(typed.String()); err == nil {
				return result, true
			}
			if result, err := typed.Float64(); err == nil {
				return int(result), true
			}
		case float64:
			return int(typed), true
		case string:
			if result, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
				return result, true
			}
		}
	}
	return 0, false
}

func intValueOr(values map[string]any, fallback int, keys ...string) int {
	if result, ok := intValue(values, keys...); ok {
		return result
	}
	return fallback
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
			return result || typed == "1"
		}
	}
	return false
}
