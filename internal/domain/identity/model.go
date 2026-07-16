package identity

const (
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionDelete = "delete"

	ResourceUser  = "user"
	ResourceGroup = "group"
)

type State struct {
	Users  []User  `json:"users" jsonschema:"Local DSM user accounts"`
	Groups []Group `json:"groups" jsonschema:"Local DSM groups"`
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

type Capabilities struct {
	InventoryRead bool `json:"inventory_read" jsonschema:"Local users and groups can be read"`
	UserCreate    bool `json:"user_create" jsonschema:"Local users can be created through guarded plan/apply"`
	UserUpdate    bool `json:"user_update" jsonschema:"Local users can be updated through guarded plan/apply"`
	UserDelete    bool `json:"user_delete" jsonschema:"Local users can be deleted through guarded plan/apply"`
	GroupCreate   bool `json:"group_create" jsonschema:"Local groups can be created through guarded plan/apply"`
	GroupUpdate   bool `json:"group_update" jsonschema:"Local groups can be updated through guarded plan/apply"`
	GroupDelete   bool `json:"group_delete" jsonschema:"Local groups can be deleted through guarded plan/apply"`
	Mutations     bool `json:"mutations" jsonschema:"Any account or group mutation is currently exposed"`
}

// ChangeRequest is the stable application-level intent accepted by CLI and
// MCP planning. Secret values never appear here; user passwords are resolved
// from CredentialRef only while an approved plan is being applied.
type ChangeRequest struct {
	Action   string       `json:"action" jsonschema:"Change action: create, update, or delete"`
	Resource string       `json:"resource" jsonschema:"Identity resource: user or group"`
	User     *UserChange  `json:"user,omitempty" jsonschema:"User change when resource is user"`
	Group    *GroupChange `json:"group,omitempty" jsonschema:"Group change when resource is group"`
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
