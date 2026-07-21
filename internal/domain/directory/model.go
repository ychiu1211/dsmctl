// Package directory contains stable, read-only models for the Control Panel
// Domain/LDAP surface: whether the NAS is joined to an Active Directory domain
// or bound to an LDAP server, the non-secret directory-client configuration,
// the periodic domain user/group sync schedule, and the synced domain/LDAP
// user and group lists.
//
// This is strictly a *directory client* view (WI-078): it reports the NAS's
// membership in an existing directory. It never models the Directory Server or
// LDAP Server packages (hosting a domain), nor per-principal privilege
// assignment (WI-007/WI-009 territory).
//
// SECRET HYGIENE: the AD domain-join password and the LDAP bind password are
// secrets and are never modeled here — DSM does not return them and the
// decoders never read them. Synced-principal password hashes, Kerberos keytab
// bytes, and machine-account material are likewise never surfaced. Only
// non-secret identity and configuration fields appear below.
package directory

// Mode is the directory-client mode: joined to an Active Directory domain,
// bound to an LDAP server, or neither. A NAS is normally in at most one mode.
type Mode string

const (
	// ModeNone means the NAS is neither AD-joined nor LDAP-bound.
	ModeNone Mode = "none"
	// ModeAD means the NAS is joined to an Active Directory domain.
	ModeAD Mode = "ad"
	// ModeLDAP means the NAS is bound to an LDAP server.
	ModeLDAP Mode = "ldap"
)

// DomainState is the Active Directory domain-membership status plus non-secret
// join configuration, read from SYNO.Core.Directory.Domain.get (v1),
// SYNO.Core.Directory.Domain.Conf.get, and
// SYNO.Core.Directory.Domain.Schedule.get.
//
// On an unjoined NAS only Joined is populated (DSM returns enable_domain:false
// and omits the identity fields); the joined-domain identity fields are
// populated by DSM only once the NAS has actually joined a domain.
type DomainState struct {
	Joined             bool            `json:"joined" jsonschema:"Whether the NAS is currently joined to an Active Directory domain"`
	DomainFQDN         string          `json:"domain_fqdn,omitempty" jsonschema:"Fully-qualified joined domain name, such as ad.example.com; populated only when joined"`
	Workgroup          string          `json:"workgroup,omitempty" jsonschema:"NetBIOS/workgroup name of the joined domain; populated only when joined"`
	DNSServer          string          `json:"dns_server,omitempty" jsonschema:"DNS server used to locate the domain; populated only when joined"`
	DomainController   string          `json:"domain_controller,omitempty" jsonschema:"Domain controller the NAS is bound to; populated only when joined"`
	OrganizationalUnit string          `json:"organizational_unit,omitempty" jsonschema:"Organizational unit the machine account sits in; populated only when joined"`
	ConnectionStatus   string          `json:"connection_status,omitempty" jsonschema:"Volatile domain connection status as DSM reports it; populated only when joined"`
	Options            DomainOptions   `json:"options" jsonschema:"Non-secret AD join options"`
	Schedule           *DomainSchedule `json:"schedule,omitempty" jsonschema:"Periodic domain user/group sync schedule, when the schedule API is available"`
}

// DomainOptions is the non-secret AD join-option set read from
// SYNO.Core.Directory.Domain.Conf.get. These are toggles configured at join
// time; none is a secret.
type DomainOptions struct {
	BuildDatabaseWithMembership bool   `json:"build_database_with_membership" jsonschema:"Whether DSM builds the local database with domain group membership"`
	DirectConnectTrust          bool   `json:"direct_connect_trust" jsonschema:"Whether trusted-domain direct connection is enabled"`
	DisableDomainAdmins         bool   `json:"disable_domain_admins" jsonschema:"Whether domain administrators are denied DSM administration"`
	DomainNestedGroupLevel      int    `json:"domain_nested_group_level" jsonschema:"Nested domain-group expansion level"`
	EnableRPCEnumUserGroup      bool   `json:"enable_rpc_enum_user_group" jsonschema:"Whether RPC enumeration of domain users/groups is enabled"`
	EnableSyncTime              bool   `json:"enable_sync_time" jsonschema:"Whether the NAS syncs its clock with the domain"`
	EncryptADLDAP               string `json:"encrypt_ad_ldap,omitempty" jsonschema:"AD LDAP transport encryption mode, such as sasl"`
}

// DomainSchedule is the periodic domain user/group sync schedule read from
// SYNO.Core.Directory.Domain.Schedule.get. DSM reports the raw schedule shape;
// DateType is preserved without interpretation.
type DomainSchedule struct {
	Enabled  bool `json:"enabled" jsonschema:"Whether periodic domain user/group sync is enabled"`
	DateType int  `json:"date_type" jsonschema:"DSM raw schedule date-type code"`
	Hour     *int `json:"hour,omitempty" jsonschema:"Scheduled run hour, when reported"`
	Minute   *int `json:"minute,omitempty" jsonschema:"Scheduled run minute, when reported"`
	Weekday  *int `json:"weekday,omitempty" jsonschema:"Scheduled weekday, when reported"`
	MonthDay *int `json:"month_day,omitempty" jsonschema:"Scheduled day of month, when reported"`
}

// LDAPState is the LDAP client bind status plus non-secret configuration, read
// from SYNO.Core.Directory.LDAP.get (v2 preferred, v1 fallback) and enriched
// with the available profile list from SYNO.Core.Directory.LDAP.Profile.list.
//
// The LDAP bind password is a secret: DSM never returns it and it is never
// modeled here. BindDN is a distinguished name (an identity, not a secret) and
// is surfaced when DSM reports it.
type LDAPState struct {
	Bound              bool     `json:"bound" jsonschema:"Whether the LDAP client is enabled and bound to a server"`
	ServerAddress      string   `json:"server_address,omitempty" jsonschema:"LDAP server address"`
	BaseDN             string   `json:"base_dn,omitempty" jsonschema:"Search base distinguished name"`
	BindDN             string   `json:"bind_dn,omitempty" jsonschema:"Bind (manager) distinguished name; an identity, never a secret"`
	Encryption         string   `json:"encryption,omitempty" jsonschema:"Transport encryption: no, StartTLS/tls, or SSL/LDAPS"`
	Profile            string   `json:"profile,omitempty" jsonschema:"Selected LDAP schema profile, such as standard, mac, domino, or customized"`
	Schema             string   `json:"schema,omitempty" jsonschema:"LDAP schema in use, such as rfc2307"`
	IsSynologyServer   bool     `json:"is_synology_server" jsonschema:"Whether the bound server is a Synology Directory Server LDAP"`
	ExpandNestedGroups bool     `json:"expand_nested_groups" jsonschema:"Whether nested LDAP groups are expanded"`
	NestedGroupLevel   int      `json:"nested_group_level" jsonschema:"Nested LDAP-group expansion level"`
	RequireTLSCert     bool     `json:"require_tls_cert" jsonschema:"Whether the LDAP server TLS certificate is required/verified"`
	UpdateMinutes      int      `json:"update_minutes" jsonschema:"Periodic sync interval in minutes"`
	EnableCIFS         bool     `json:"enable_cifs" jsonschema:"Whether LDAP CIFS/SMB support is enabled"`
	EnableIDMap        bool     `json:"enable_idmap" jsonschema:"Whether LDAP ID mapping is enabled"`
	ErrorCode          int      `json:"error_code,omitempty" jsonschema:"DSM LDAP status/error code, such as 2703 for not connected"`
	AvailableProfiles  []string `json:"available_profiles,omitempty" jsonschema:"LDAP schema profiles this NAS offers"`
}

// Status is the combined directory-client status: the derived mode plus each
// area's state. Domain and LDAP are independent failure boundaries; an area
// whose API family is absent is reported as nil (not supported) without
// disabling the other.
type Status struct {
	Mode   Mode         `json:"mode" jsonschema:"Directory-client mode: ad, ldap, or none"`
	Domain *DomainState `json:"domain,omitempty" jsonschema:"Active Directory domain status; absent when the Domain API family is not exposed"`
	LDAP   *LDAPState   `json:"ldap,omitempty" jsonschema:"LDAP client status; absent when the LDAP API family is not exposed"`
}

// DomainUser is one synced domain/LDAP user. Only non-secret identity fields
// are modeled; no password hash, Kerberos keytab, or machine-account material
// is ever surfaced.
type DomainUser struct {
	Name        string `json:"name" jsonschema:"Synced user account name"`
	UID         *int   `json:"uid,omitempty" jsonschema:"Numeric user id assigned by DSM, when reported"`
	Description string `json:"description,omitempty" jsonschema:"User description, when reported"`
	Source      Mode   `json:"source" jsonschema:"Directory the user is synced from: ad or ldap"`
}

// DomainGroup is one synced domain/LDAP group.
type DomainGroup struct {
	Name        string `json:"name" jsonschema:"Synced group name"`
	GID         *int   `json:"gid,omitempty" jsonschema:"Numeric group id assigned by DSM, when reported"`
	Description string `json:"description,omitempty" jsonschema:"Group description, when reported"`
	Source      Mode   `json:"source" jsonschema:"Directory the group is synced from: ad or ldap"`
}

// DirectoryUsers is a page of synced domain/LDAP users. It is read-only: these
// principals are owned by the domain/LDAP server, not by DSM. When the NAS is
// in ModeNone the list is empty.
type DirectoryUsers struct {
	Mode   Mode         `json:"mode" jsonschema:"Directory mode the list was read for: ad, ldap, or none"`
	Total  int          `json:"total" jsonschema:"Total synced users DSM reports for the mode"`
	Offset int          `json:"offset" jsonschema:"Offset of the returned page"`
	Users  []DomainUser `json:"users" jsonschema:"Synced domain/LDAP users; empty when the NAS is not joined/bound"`
}

// DirectoryGroups is a page of synced domain/LDAP groups.
type DirectoryGroups struct {
	Mode   Mode          `json:"mode" jsonschema:"Directory mode the list was read for: ad, ldap, or none"`
	Total  int           `json:"total" jsonschema:"Total synced groups DSM reports for the mode"`
	Offset int           `json:"offset" jsonschema:"Offset of the returned page"`
	Groups []DomainGroup `json:"groups" jsonschema:"Synced domain/LDAP groups; empty when the NAS is not joined/bound"`
}

// Capabilities reports which directory read areas the NAS exposes. Each area
// selects its backend independently; a missing API family fails closed for its
// own area only.
type Capabilities struct {
	Domain bool `json:"domain" jsonschema:"Whether AD domain status can be read (SYNO.Core.Directory.Domain)"`
	LDAP   bool `json:"ldap" jsonschema:"Whether LDAP client status can be read (SYNO.Core.Directory.LDAP)"`
	Users  bool `json:"users" jsonschema:"Whether synced domain/LDAP users can be listed (SYNO.Core.User)"`
	Groups bool `json:"groups" jsonschema:"Whether synced domain/LDAP groups can be listed (SYNO.Core.Group)"`
}
