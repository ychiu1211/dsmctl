// Package nfsexport models per-shared-folder NFS export rules independently of
// DSM request field names. One shared folder owns an ordered, complete set of
// export rules; a change request always carries the full desired set because
// the DSM SYNO.Core.FileServ.NFS.SharePrivilege save method replaces the whole
// rule set for a shared folder.
package nfsexport

// Privilege is the access level an NFS export rule grants a client.
type Privilege string

const (
	PrivilegeReadWrite Privilege = "read_write"
	PrivilegeReadOnly  Privilege = "read_only"
)

// Squash is the UID/GID mapping DSM applies to a matching NFS client.
type Squash string

const (
	// SquashNoMapping keeps client identities unmapped (DSM value "root").
	SquashNoMapping Squash = "no_mapping"
	// SquashRootToAdmin maps the client root user to admin (DSM value "admin").
	SquashRootToAdmin Squash = "map_root_to_admin"
	// SquashRootToGuest maps the client root user to guest (DSM value "guest").
	SquashRootToGuest Squash = "map_root_to_guest"
	// SquashAllToAdmin maps every client user to admin (DSM value "all_admin").
	SquashAllToAdmin Squash = "map_all_to_admin"
	// SquashAllToGuest maps every client user to guest (DSM value "all_guest").
	SquashAllToGuest Squash = "map_all_to_guest"
)

// Security is the NFS security flavor a client must negotiate.
type Security string

const (
	SecuritySys               Security = "sys"
	SecurityKerberos          Security = "kerberos"
	SecurityKerberosIntegrity Security = "kerberos_integrity"
	SecurityKerberosPrivacy   Security = "kerberos_privacy"
)

// Rule is one normalized NFS export rule for a shared folder. Client is the
// stable identity of a rule within a shared folder.
type Rule struct {
	Client                  string    `json:"client" jsonschema:"NFS client pattern: hostname, IP, IP/mask, or a wildcard such as *"`
	Privilege               Privilege `json:"privilege" jsonschema:"Access level: read_write or read_only"`
	Squash                  Squash    `json:"squash" jsonschema:"UID/GID mapping: no_mapping, map_root_to_admin, map_root_to_guest, map_all_to_admin, or map_all_to_guest"`
	Security                Security  `json:"security" jsonschema:"NFS security flavor: sys, kerberos, kerberos_integrity, or kerberos_privacy"`
	Async                   bool      `json:"async" jsonschema:"Whether the export replies before writes reach stable storage"`
	AllowNonprivilegedPorts bool      `json:"allow_nonprivileged_ports" jsonschema:"Whether clients may connect from source ports above 1024"`
	AllowSubfolderAccess    bool      `json:"allow_subfolder_access" jsonschema:"Whether clients may mount and cross into mounted subfolders"`
}

// ShareExport is the complete observed NFS export configuration of one shared
// folder.
type ShareExport struct {
	Share string `json:"share" jsonschema:"Shared-folder name"`
	Rules []Rule `json:"rules" jsonschema:"Complete ordered NFS export rule set for the shared folder"`
}

// Capabilities reports which NFS export operations the selected DSM backend
// exposes.
type Capabilities struct {
	Read bool `json:"read" jsonschema:"Whether the export rule set can be read"`
	Set  bool `json:"set" jsonschema:"Whether the export rule set can be changed through guarded plan/apply"`
}

// ChangeRequest is the complete desired NFS export rule set for one shared
// folder. Rules replaces the shared folder's entire rule set; an empty slice
// removes every export rule.
type ChangeRequest struct {
	Share string `json:"share" jsonschema:"Shared-folder name whose NFS export rules are replaced"`
	Rules []Rule `json:"rules" jsonschema:"Complete desired NFS export rule set; replaces all existing rules for the shared folder"`
}
