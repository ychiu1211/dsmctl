package identity

const (
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionDelete = "delete"
	ActionSet    = "set"

	ResourceUser                 = "user"
	ResourceGroup                = "group"
	ResourceMembership           = "membership"
	ResourceQuota                = "quota"
	ResourceApplicationPrivilege = "application_privilege"

	PrincipalUser  = "user"
	PrincipalGroup = "group"

	QuotaTargetVolume = "volume"
	QuotaTargetShare  = "share"

	ApplicationAccessAllow   = "allow"
	ApplicationAccessDeny    = "deny"
	ApplicationAccessInherit = "inherit"
	ApplicationAccessCustom  = "custom"
)

type State struct {
	Users                 []User                           `json:"users" jsonschema:"Local DSM user accounts"`
	Groups                []Group                          `json:"groups" jsonschema:"Local DSM groups"`
	Memberships           []Membership                     `json:"memberships,omitempty" jsonschema:"Requested user-to-group membership state"`
	Quotas                []PrincipalQuota                 `json:"quotas,omitempty" jsonschema:"Requested user or group quota state"`
	Applications          []Application                    `json:"applications,omitempty" jsonschema:"Applications addressable by application privilege rules"`
	ApplicationPrivileges []ApplicationPrivilegeAssignment `json:"application_privileges,omitempty" jsonschema:"Requested explicit application privilege rules"`
}

// StateQuery keeps expensive per-principal reads opt-in. Membership reads scale
// with the number of groups, while quota and application privilege reads scale
// with the selected principals.
type StateQuery struct {
	IncludeMemberships           bool   `json:"include_memberships,omitempty" jsonschema:"Include user-to-group memberships"`
	IncludeQuotas                bool   `json:"include_quotas,omitempty" jsonschema:"Include quota assignments"`
	IncludeApplicationPrivileges bool   `json:"include_application_privileges,omitempty" jsonschema:"Include applications and explicit privilege rules"`
	PrincipalType                string `json:"principal_type,omitempty" jsonschema:"Optional quota or application privilege principal type: user or group"`
	Principal                    string `json:"principal,omitempty" jsonschema:"Optional principal name; omit with principal_type to read every local principal"`
}

type User struct {
	ID                   string `json:"id,omitempty" jsonschema:"DSM user identifier"`
	Name                 string `json:"name" jsonschema:"DSM account name"`
	Description          string `json:"description,omitempty" jsonschema:"Account description"`
	Email                string `json:"email,omitempty" jsonschema:"Account email address"`
	Source               string `json:"source" jsonschema:"Identity source; local in the first milestone"`
	Expired              bool   `json:"expired" jsonschema:"Whether DSM reports the account as expired"`
	PasswordNeverExpires bool   `json:"password_never_expires" jsonschema:"Whether the account password never expires"`
	TwoFactorStatus      string `json:"two_factor_status,omitempty" jsonschema:"DSM two-factor authentication status"`
}

type Group struct {
	ID          string `json:"id,omitempty" jsonschema:"DSM group identifier when available"`
	Name        string `json:"name" jsonschema:"DSM group name"`
	Description string `json:"description,omitempty" jsonschema:"Group description"`
	Source      string `json:"source" jsonschema:"Identity source; local in the first milestone"`
}

type Membership struct {
	User   string   `json:"user" jsonschema:"Local DSM user name"`
	Groups []string `json:"groups" jsonschema:"Sorted group names containing the user"`
}

type PrincipalQuota struct {
	PrincipalType string       `json:"principal_type" jsonschema:"Quota principal type: user or group"`
	Principal     string       `json:"principal" jsonschema:"Local user or group name"`
	Limits        []QuotaLimit `json:"limits" jsonschema:"Volume and shared-folder quota limits; zero MiB means unlimited"`
}

type QuotaLimit struct {
	TargetType string `json:"target_type" jsonschema:"Quota target type: volume or share"`
	Target     string `json:"target" jsonschema:"Volume path or shared-folder name"`
	Volume     string `json:"volume,omitempty" jsonschema:"Containing volume path for a shared-folder quota"`
	QuotaMiB   int64  `json:"quota_mib" jsonschema:"Quota in MiB; zero means unlimited"`
	Status     string `json:"status,omitempty" jsonschema:"DSM quota compatibility status when reported"`
}

type Application struct {
	ID         string   `json:"id" jsonschema:"DSM application identifier used by privilege rules"`
	Name       string   `json:"name" jsonschema:"Human-readable application name"`
	GrantTypes []string `json:"grant_types,omitempty" jsonschema:"Identity sources accepted by the application"`
	SupportsIP bool     `json:"supports_ip" jsonschema:"Whether DSM reports IP-aware application rules"`
}

type ApplicationPrivilegeAssignment struct {
	PrincipalType string                  `json:"principal_type" jsonschema:"Privilege principal type: user or group"`
	Principal     string                  `json:"principal" jsonschema:"Local user or group name"`
	Permissions   []ApplicationPermission `json:"permissions" jsonschema:"Explicit application privilege rules; absent applications inherit"`
}

type ApplicationPermission struct {
	ApplicationID string   `json:"application_id" jsonschema:"DSM application identifier"`
	Access        string   `json:"access" jsonschema:"Explicit access: allow, deny, or custom"`
	AllowIP       []string `json:"allow_ip,omitempty" jsonschema:"Existing custom allow IP rules, preserved for inspection"`
	DenyIP        []string `json:"deny_ip,omitempty" jsonschema:"Existing custom deny IP rules, preserved for inspection"`
}

type Capabilities struct {
	InventoryRead            bool `json:"inventory_read" jsonschema:"Local users and groups can be read"`
	UserCreate               bool `json:"user_create" jsonschema:"Local users can be created through guarded plan/apply"`
	UserUpdate               bool `json:"user_update" jsonschema:"Local users can be updated through guarded plan/apply"`
	UserDelete               bool `json:"user_delete" jsonschema:"Local users can be deleted through guarded plan/apply"`
	GroupCreate              bool `json:"group_create" jsonschema:"Local groups can be created through guarded plan/apply"`
	GroupUpdate              bool `json:"group_update" jsonschema:"Local groups can be updated through guarded plan/apply"`
	GroupDelete              bool `json:"group_delete" jsonschema:"Local groups can be deleted through guarded plan/apply"`
	MembershipRead           bool `json:"membership_read" jsonschema:"User-to-group memberships can be read"`
	MembershipSet            bool `json:"membership_set" jsonschema:"User-to-group memberships can be changed through guarded plan/apply"`
	QuotaRead                bool `json:"quota_read" jsonschema:"User and group quotas can be read"`
	QuotaSet                 bool `json:"quota_set" jsonschema:"User and group quotas can be changed through guarded plan/apply"`
	ApplicationPrivilegeRead bool `json:"application_privilege_read" jsonschema:"Explicit application privilege rules can be read"`
	ApplicationPrivilegeSet  bool `json:"application_privilege_set" jsonschema:"Application privilege rules can be changed through guarded plan/apply"`
	Mutations                bool `json:"mutations" jsonschema:"Any identity mutation is currently exposed"`
}

// ChangeRequest is the stable application-level intent accepted by CLI and
// MCP planning. Secret values never appear here; user passwords are resolved
// from CredentialRef only while an approved plan is being applied.
type ChangeRequest struct {
	Action               string                      `json:"action" jsonschema:"Change action: create, update, delete, or set"`
	Resource             string                      `json:"resource" jsonschema:"Identity resource: user, group, membership, quota, or application_privilege"`
	User                 *UserChange                 `json:"user,omitempty" jsonschema:"User change when resource is user"`
	Group                *GroupChange                `json:"group,omitempty" jsonschema:"Group change when resource is group"`
	Membership           *MembershipChange           `json:"membership,omitempty" jsonschema:"Membership change when resource is membership"`
	Quota                *QuotaChange                `json:"quota,omitempty" jsonschema:"Quota change when resource is quota"`
	ApplicationPrivilege *ApplicationPrivilegeChange `json:"application_privilege,omitempty" jsonschema:"Application privilege change when resource is application_privilege"`
}

type UserChange struct {
	Name                 string  `json:"name" jsonschema:"Current or new DSM user name"`
	NewName              *string `json:"new_name,omitempty" jsonschema:"New user name for an update"`
	Description          *string `json:"description,omitempty" jsonschema:"Account description; empty clears it"`
	Email                *string `json:"email,omitempty" jsonschema:"Account email address; empty clears it"`
	Expired              *string `json:"expired,omitempty" jsonschema:"Account expiration: normal, now, or YYYY/M/D"`
	CannotChangePassword *bool   `json:"cannot_change_password,omitempty" jsonschema:"Prevent the user from changing their password"`
	PasswordNeverExpires *bool   `json:"password_never_expires,omitempty" jsonschema:"Prevent the account password from expiring"`
	CredentialRef        string  `json:"credential_ref,omitempty" jsonschema:"Password reference such as env:DSMCTL_NEW_USER_PASSWORD; never a plaintext password"`
}

type GroupChange struct {
	Name        string  `json:"name" jsonschema:"Current or new DSM group name"`
	NewName     *string `json:"new_name,omitempty" jsonschema:"New group name for an update"`
	Description *string `json:"description,omitempty" jsonschema:"Group description; empty clears it"`
}

// MembershipChange owns the complete direct group membership set for one
// local user. The mandatory users group must always be included.
type MembershipChange struct {
	User   string   `json:"user" jsonschema:"Local DSM user whose direct memberships are managed"`
	Groups []string `json:"groups" jsonschema:"Complete desired group set; must include users"`
}

type QuotaChange struct {
	PrincipalType string             `json:"principal_type" jsonschema:"Quota principal type: user or group"`
	Principal     string             `json:"principal" jsonschema:"Local user or group name"`
	Limits        []QuotaLimitChange `json:"limits" jsonschema:"Quota targets to update; unspecified targets remain unchanged"`
}

type QuotaLimitChange struct {
	TargetType string `json:"target_type" jsonschema:"Quota target type: volume or share"`
	Target     string `json:"target" jsonschema:"Volume path or shared-folder name"`
	QuotaMiB   int64  `json:"quota_mib" jsonschema:"Quota in MiB; zero removes the limit"`
}

type ApplicationPrivilegeChange struct {
	PrincipalType string                        `json:"principal_type" jsonschema:"Privilege principal type: user or group"`
	Principal     string                        `json:"principal" jsonschema:"Local user or group name"`
	Permissions   []ApplicationPermissionChange `json:"permissions" jsonschema:"Application rules to update; unspecified applications remain unchanged"`
}

type ApplicationPermissionChange struct {
	ApplicationID string `json:"application_id" jsonschema:"DSM application identifier"`
	Access        string `json:"access" jsonschema:"Desired explicit access: allow, deny, or inherit"`
}
