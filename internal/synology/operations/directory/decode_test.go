package directory

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/directory"
)

func TestDecodeDomainUnjoined(t *testing.T) {
	// The exact unjoined shape live-captured on DSM 7.3.
	state, err := decodeDomain(json.RawMessage(`{"enable_domain":false}`))
	if err != nil {
		t.Fatalf("decodeDomain: %v", err)
	}
	if state.Joined {
		t.Errorf("Joined = true, want false for an unjoined NAS")
	}
	if state.DomainFQDN != "" || state.Workgroup != "" {
		t.Errorf("unexpected joined identity on an unjoined NAS: %+v", state)
	}
}

func TestDecodeDomainJoined(t *testing.T) {
	state, err := decodeDomain(json.RawMessage(`{
		"enable_domain": true,
		"domain_fqdn": "ad.example.com",
		"workgroup": "EXAMPLE",
		"dns": "192.0.2.10",
		"dc": "dc1.ad.example.com",
		"ou": "OU=NAS,DC=ad,DC=example,DC=com",
		"status": "connected"
	}`))
	if err != nil {
		t.Fatalf("decodeDomain: %v", err)
	}
	if !state.Joined {
		t.Fatal("Joined = false, want true")
	}
	if state.DomainFQDN != "ad.example.com" || state.Workgroup != "EXAMPLE" ||
		state.DNSServer != "192.0.2.10" || state.DomainController != "dc1.ad.example.com" ||
		state.ConnectionStatus != "connected" {
		t.Errorf("joined identity mismatch: %+v", state)
	}
}

func TestDecodeDomainOptions(t *testing.T) {
	// Live-captured DSM 7.3 Domain.Conf.get shape.
	options, err := decodeDomainOptions(json.RawMessage(`{
		"buildDatabaseWithMembership": true,
		"direct_connect_trust": false,
		"disable_domain_admins": true,
		"domain_nested_group": 1,
		"enable_rpc_enum_usergroup": false,
		"enable_sync_time": false,
		"encrypt_ad_ldap": "sasl"
	}`))
	if err != nil {
		t.Fatalf("decodeDomainOptions: %v", err)
	}
	if !options.BuildDatabaseWithMembership || !options.DisableDomainAdmins ||
		options.DomainNestedGroupLevel != 1 || options.EncryptADLDAP != "sasl" {
		t.Errorf("options mismatch: %+v", options)
	}
}

func TestDecodeDomainSchedule(t *testing.T) {
	// Live-captured DSM 7.3 Domain.Schedule.get shape (sparse when unscheduled).
	schedule, err := decodeDomainSchedule(json.RawMessage(`{"date_type":2}`))
	if err != nil {
		t.Fatalf("decodeDomainSchedule: %v", err)
	}
	if schedule.DateType != 2 || schedule.Hour != nil {
		t.Errorf("schedule mismatch: %+v", schedule)
	}
}

func TestDecodeLDAPUnbound(t *testing.T) {
	// Live-captured DSM 7.3 LDAP.get v2 shape on an unbound NAS.
	state, err := decodeLDAP(json.RawMessage(`{
		"base_dn": "",
		"enable_cifs": false,
		"enable_client": false,
		"enable_idmap": false,
		"encryption": "no",
		"error": 2703,
		"expand_nested_groups": false,
		"is_syno_server": false,
		"ldap_schema": "rfc2307",
		"nested_group_level": 0,
		"profile": "standard",
		"server_address": "",
		"tls_reqcert": false,
		"update_min": 1440
	}`))
	if err != nil {
		t.Fatalf("decodeLDAP: %v", err)
	}
	if state.Bound {
		t.Errorf("Bound = true, want false for an unbound NAS")
	}
	if state.Schema != "rfc2307" || state.Profile != "standard" || state.ErrorCode != 2703 || state.UpdateMinutes != 1440 {
		t.Errorf("LDAP config mismatch: %+v", state)
	}
}

func TestDecodeLDAPBoundV1Host(t *testing.T) {
	// v1 uses host instead of server_address.
	state, err := decodeLDAP(json.RawMessage(`{
		"enable_client": true,
		"host": "ldap.example.com",
		"base_dn": "dc=example,dc=com",
		"encryption": "ssl",
		"profile": "standard"
	}`))
	if err != nil {
		t.Fatalf("decodeLDAP: %v", err)
	}
	if !state.Bound || state.ServerAddress != "ldap.example.com" || state.BaseDN != "dc=example,dc=com" {
		t.Errorf("v1 host fallback mismatch: %+v", state)
	}
}

func TestDecodeLDAPProfiles(t *testing.T) {
	profiles, err := decodeLDAPProfiles(json.RawMessage(`{"profiles":["standard","mac","domino","customized"]}`))
	if err != nil {
		t.Fatalf("decodeLDAPProfiles: %v", err)
	}
	if len(profiles) != 4 || profiles[0] != "standard" {
		t.Errorf("profiles mismatch: %v", profiles)
	}
}

func TestDecodeUsersEmpty(t *testing.T) {
	// Live-captured DSM 7.3 shape for User.list type=domain on an unjoined NAS.
	page, err := decodeUsers(json.RawMessage(`{"offset":0,"total":0,"users":[]}`), directory.ModeAD)
	if err != nil {
		t.Fatalf("decodeUsers: %v", err)
	}
	if page.Total != 0 || len(page.Users) != 0 || page.Mode != directory.ModeAD {
		t.Errorf("empty user page mismatch: %+v", page)
	}
	if page.Users == nil {
		t.Error("Users slice must be non-nil so JSON renders [] not null")
	}
}

func TestDecodeUsersPopulated(t *testing.T) {
	page, err := decodeUsers(json.RawMessage(`{
		"offset": 0,
		"total": 2,
		"users": [
			{"name": "testuser", "uid": 100001, "description": "Test User"},
			{"name": "svc-account", "uid": 100002}
		]
	}`), directory.ModeAD)
	if err != nil {
		t.Fatalf("decodeUsers: %v", err)
	}
	if page.Total != 2 || len(page.Users) != 2 {
		t.Fatalf("user page mismatch: %+v", page)
	}
	if page.Users[0].Name != "testuser" || page.Users[0].UID == nil || *page.Users[0].UID != 100001 {
		t.Errorf("user[0] mismatch: %+v", page.Users[0])
	}
	if page.Users[0].Source != directory.ModeAD {
		t.Errorf("user source = %q, want ad", page.Users[0].Source)
	}
	if page.Users[1].UID == nil || *page.Users[1].UID != 100002 {
		t.Errorf("user[1] uid mismatch: %+v", page.Users[1])
	}
}

func TestDecodeGroupsPopulated(t *testing.T) {
	page, err := decodeGroups(json.RawMessage(`{
		"offset": 0,
		"total": 1,
		"groups": [{"name": "domain admins", "gid": 512, "description": "Designated administrators"}]
	}`), directory.ModeLDAP)
	if err != nil {
		t.Fatalf("decodeGroups: %v", err)
	}
	if len(page.Groups) != 1 || page.Groups[0].Name != "domain admins" ||
		page.Groups[0].GID == nil || *page.Groups[0].GID != 512 || page.Groups[0].Source != directory.ModeLDAP {
		t.Errorf("group page mismatch: %+v", page)
	}
}

// TestDecodersNeverLeakSecrets injects password/credential canaries into every
// decoded shape and asserts the decoded model — serialized back to JSON —
// carries none of them. The AD join password and LDAP bind password are secrets
// and must never enter the model, so the decoders must read only the explicit
// non-secret keys, never a whole-object passthrough.
func TestDecodersNeverLeakSecrets(t *testing.T) {
	const (
		pwCanary   = "hunter2-SECRET"
		bindCanary = "bind-SECRET-9f3a"
	)
	secretKeys := `"password":"` + pwCanary + `","bind_pw":"` + bindCanary + `","passwd":"` + pwCanary +
		`","admin_password":"` + pwCanary + `","bind_password":"` + bindCanary + `","krb_keytab":"` + pwCanary + `"`

	cases := []struct {
		name   string
		encode func() (any, error)
	}{
		{"domain", func() (any, error) {
			return decodeDomain(json.RawMessage(`{"enable_domain":true,"domain_fqdn":"ad.example.com",` + secretKeys + `}`))
		}},
		{"domain-options", func() (any, error) {
			return decodeDomainOptions(json.RawMessage(`{"disable_domain_admins":true,` + secretKeys + `}`))
		}},
		{"domain-schedule", func() (any, error) {
			return decodeDomainSchedule(json.RawMessage(`{"date_type":1,` + secretKeys + `}`))
		}},
		{"ldap", func() (any, error) {
			return decodeLDAP(json.RawMessage(`{"enable_client":true,"server_address":"ldap.example.com","bind_dn":"cn=admin",` + secretKeys + `}`))
		}},
		{"users", func() (any, error) {
			return decodeUsers(json.RawMessage(`{"total":1,"users":[{"name":"testuser","uid":1,`+secretKeys+`}]}`), directory.ModeAD)
		}},
		{"groups", func() (any, error) {
			return decodeGroups(json.RawMessage(`{"total":1,"groups":[{"name":"grp","gid":1,`+secretKeys+`}]}`), directory.ModeAD)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decoded, err := tc.encode()
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			encoded, err := json.Marshal(decoded)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			blob := string(encoded)
			for _, canary := range []string{pwCanary, bindCanary} {
				if strings.Contains(blob, canary) {
					t.Errorf("decoded %s model leaked a secret canary %q: %s", tc.name, canary, blob)
				}
			}
			for _, key := range []string{"password", "bind_pw", "passwd", "keytab", "krb"} {
				if strings.Contains(strings.ToLower(blob), key) {
					t.Errorf("decoded %s model carries a secret-bearing key %q: %s", tc.name, key, blob)
				}
			}
		})
	}
}
