// Package accountprotection contains stable, DSM-version-independent models for
// the Control Panel > Security > Account surface: Auto Block (settings plus the
// allow/block IP lists), Account Protection (DSM's internal "SmartBlock"), and
// the domain-wide enforced-2FA/MFA policy. WebAPI names and field names stay
// behind the operation package.
//
// This is the read slice (WI-067 Slice A). Every area is a separate DSM API and
// a separate compatibility/failure boundary, so a NAS missing one still reports
// the others. The module reads and (later) sets policy only: it never reads any
// user's OTP secret, seed, or recovery codes.
package accountprotection

// ModuleName is the stable product-facing identifier for the module.
const ModuleName = "account-protection"

// AutoBlockSettings is the normalized Auto Block configuration: "block a source
// after N failed sign-ins within M minutes, optionally expiring the block after
// D days". DSM stores it under SYNO.Core.Security.AutoBlock.
type AutoBlockSettings struct {
	Enabled       bool `json:"enabled" jsonschema:"Whether Auto Block is enabled (DSM field enable)"`
	Attempts      int  `json:"attempts" jsonschema:"Failed sign-in attempts that trigger a block (DSM field attempts)"`
	WithinMinutes int  `json:"within_minutes" jsonschema:"Window in minutes the attempts are counted over (DSM field within_mins)"`
	ExpireEnabled bool `json:"expire_enabled" jsonschema:"Whether a block expires automatically; derived from expire_day being greater than zero"`
	ExpireDays    int  `json:"expire_days" jsonschema:"Days after which a block expires when expiration is enabled (DSM field expire_day)"`
}

// IPRule is one entry of the Auto Block allow or block list. Only the public
// fields DSM reports are carried.
type IPRule struct {
	IP         string `json:"ip" jsonschema:"IP address or subnet the rule applies to"`
	Reason     string `json:"reason,omitempty" jsonschema:"DSM-reported reason the entry exists, when present"`
	RecordTime int64  `json:"record_time,omitempty" jsonschema:"Unix time the entry was recorded, when reported by DSM"`
}

// IPList is one side of the Auto Block allow/block list (kind is allow or block).
type IPList struct {
	Kind    string   `json:"kind" jsonschema:"Which list this is: allow or block"`
	Total   int      `json:"total" jsonschema:"Total entries DSM reports for this list"`
	Entries []IPRule `json:"entries" jsonschema:"Entries returned for this list"`
}

// AutoBlockLists bundles both sides of the Auto Block IP list. They are read
// with two SYNO.Core.Security.AutoBlock.Rules list calls (type=allow / deny).
type AutoBlockLists struct {
	Allow IPList `json:"allow" jsonschema:"The allow list (never auto-blocked)"`
	Block IPList `json:"block" jsonschema:"The block list (always blocked)"`
}

// AccountProtection is the "protect accounts by blocking untrusted clients after
// repeated failed sign-ins" policy. DSM stores it under SYNO.Core.SmartBlock and
// keeps separate thresholds for untrusted and trusted clients. The lock duration
// is the DSM-reported block length (untrust_lock / trust_lock).
type AccountProtection struct {
	Enabled                bool `json:"enabled" jsonschema:"Whether Account Protection is enabled"`
	UntrustedAttempts      int  `json:"untrusted_attempts" jsonschema:"Failed attempts from an untrusted client that trigger a block (DSM field untrust_try)"`
	UntrustedWithinMinutes int  `json:"untrusted_within_minutes" jsonschema:"Window in minutes for untrusted-client attempts (DSM field untrust_minute)"`
	UntrustedBlockMinutes  int  `json:"untrusted_block_minutes" jsonschema:"Block duration for an untrusted client, as reported by DSM (untrust_lock)"`
	TrustedAttempts        int  `json:"trusted_attempts" jsonschema:"Failed attempts from a trusted client that trigger a block (DSM field trust_try)"`
	TrustedWithinMinutes   int  `json:"trusted_within_minutes" jsonschema:"Window in minutes for trusted-client attempts (DSM field trust_minute)"`
	TrustedBlockMinutes    int  `json:"trusted_block_minutes" jsonschema:"Block duration for a trusted client, as reported by DSM (trust_lock)"`
}

// EnforceTwoFactor is the domain-wide enforced-2FA/MFA policy. It surfaces the
// enforcement scope only; it never reads any user's OTP secret or recovery code.
// DSM stores it under SYNO.Core.OTP.EnforcePolicy as otp_enforce_option.
type EnforceTwoFactor struct {
	Option  string `json:"option" jsonschema:"Raw DSM enforcement scope (otp_enforce_option), for example none"`
	Enabled bool   `json:"enabled" jsonschema:"Whether 2FA is enforced for anyone; true when the scope is not none"`
}

// Capabilities reports which account-protection reads and guarded writes dsmctl
// currently exposes for the selected NAS. Each area is gated on its own DSM API
// so a NAS missing one still reports the others. Mutations is true when any
// guarded write backend is available.
type Capabilities struct {
	Module                 string `json:"module" jsonschema:"Stable module name: account-protection"`
	AutoBlockRead          bool   `json:"autoblock_read" jsonschema:"Whether Auto Block settings can be read"`
	AutoBlockListRead      bool   `json:"autoblock_list_read" jsonschema:"Whether the Auto Block allow/block IP lists can be read"`
	AccountProtectionRead  bool   `json:"account_protection_read" jsonschema:"Whether Account Protection thresholds can be read"`
	EnforceTwoFactorRead   bool   `json:"enforce_2fa_read" jsonschema:"Whether the enforced-2FA policy can be read"`
	AutoBlockWrite         bool   `json:"autoblock_write" jsonschema:"Whether the guarded Auto Block settings write is available"`
	AutoBlockListWrite     bool   `json:"autoblock_list_write" jsonschema:"Whether the guarded Auto Block allow/block list add/remove is available"`
	AccountProtectionWrite bool   `json:"account_protection_write" jsonschema:"Whether the guarded Account Protection thresholds write is available"`
	EnforceTwoFactorWrite  bool   `json:"enforce_2fa_write" jsonschema:"Whether the guarded enforced-2FA policy write is available"`
	DoSPresent             bool   `json:"dos_present" jsonschema:"Whether the DoS-protection API is advertised by this NAS; the DoS read/write is a deferred follow-on (wire unverified)"`
	Mutations              bool   `json:"mutations" jsonschema:"Whether any guarded account-protection write is available"`
}

// AutoBlockChange is the patch-only intent for the guarded Auto Block settings
// write. A nil field keeps the currently configured DSM value; the apply path
// reads the complete current settings, merges this patch, and submits the whole
// merged configuration so an unspecified field is never silently reset. DSM only
// binds the attempt/window/expiry thresholds when Auto Block is enabled, so the
// postcondition re-read catches a threshold change requested while disabled.
type AutoBlockChange struct {
	Enabled       *bool `json:"enabled,omitempty" jsonschema:"Whether Auto Block is enabled; omit to keep the current setting"`
	Attempts      *int  `json:"attempts,omitempty" jsonschema:"Failed sign-in attempts that trigger a block; omit to keep the current value"`
	WithinMinutes *int  `json:"within_minutes,omitempty" jsonschema:"Window in minutes the attempts are counted over; omit to keep the current value"`
	ExpireEnabled *bool `json:"expire_enabled,omitempty" jsonschema:"Whether a block expires automatically; omit to keep the current setting"`
	ExpireDays    *int  `json:"expire_days,omitempty" jsonschema:"Days after which a block expires when expiration is enabled; omit to keep the current value"`
}

// IsEmpty reports whether the patch carries no fields.
func (c AutoBlockChange) IsEmpty() bool {
	return c.Enabled == nil && c.Attempts == nil && c.WithinMinutes == nil && c.ExpireEnabled == nil && c.ExpireDays == nil
}

// AccountProtectionChange is the patch-only intent for the guarded Account
// Protection (SmartBlock) thresholds write. A nil field keeps the current DSM
// value; the apply path merges this patch into the freshly read state.
type AccountProtectionChange struct {
	Enabled                *bool `json:"enabled,omitempty" jsonschema:"Whether Account Protection is enabled; omit to keep the current setting"`
	UntrustedAttempts      *int  `json:"untrusted_attempts,omitempty" jsonschema:"Failed attempts from an untrusted client that trigger a block; omit to keep the current value"`
	UntrustedWithinMinutes *int  `json:"untrusted_within_minutes,omitempty" jsonschema:"Window in minutes for untrusted-client attempts; omit to keep the current value"`
	UntrustedBlockMinutes  *int  `json:"untrusted_block_minutes,omitempty" jsonschema:"Block duration in minutes for an untrusted client; omit to keep the current value"`
	TrustedAttempts        *int  `json:"trusted_attempts,omitempty" jsonschema:"Failed attempts from a trusted client that trigger a block; omit to keep the current value"`
	TrustedWithinMinutes   *int  `json:"trusted_within_minutes,omitempty" jsonschema:"Window in minutes for trusted-client attempts; omit to keep the current value"`
	TrustedBlockMinutes    *int  `json:"trusted_block_minutes,omitempty" jsonschema:"Block duration in minutes for a trusted client; omit to keep the current value"`
}

// IsEmpty reports whether the patch carries no fields.
func (c AccountProtectionChange) IsEmpty() bool {
	return c.Enabled == nil && c.UntrustedAttempts == nil && c.UntrustedWithinMinutes == nil && c.UntrustedBlockMinutes == nil &&
		c.TrustedAttempts == nil && c.TrustedWithinMinutes == nil && c.TrustedBlockMinutes == nil
}

// EnforceTwoFactorChange is the intent for the guarded enforced-2FA policy write.
// It sets the domain-wide enforcement scope (otp_enforce_option) only; it never
// enrolls a user or touches any OTP secret. AllowLockoutOverride is the explicit,
// logged acknowledgement required to enable enforcement, which can lock out an
// admin who has not enrolled 2FA.
type EnforceTwoFactorChange struct {
	Option               *string `json:"option,omitempty" jsonschema:"Desired enforcement scope (otp_enforce_option), for example none; omit for no change"`
	AllowLockoutOverride bool    `json:"allow_lockout_override,omitempty" jsonschema:"Explicit acknowledgement required to enable enforcement, which can lock out an un-enrolled admin"`
}

// IsEmpty reports whether the patch carries no policy field.
func (c EnforceTwoFactorChange) IsEmpty() bool {
	return c.Option == nil
}

// IPListEdit is the patch-only intent for one Auto Block allow/block list edit.
// The module adds or removes exactly one named entry keyed by IP + Kind; it never
// sends a whole-list payload that could silently reset sibling entries.
// AllowLockoutOverride is the explicit acknowledgement required for an edit that
// could lock the operator or an active session out (blocking a currently active
// source, or removing an active source from the allow list).
type IPListEdit struct {
	Kind                 string `json:"kind" jsonschema:"Which list to edit: allow or block"`
	IP                   string `json:"ip" jsonschema:"IP address or CIDR subnet the entry applies to"`
	Remove               bool   `json:"remove,omitempty" jsonschema:"Remove the entry instead of adding it"`
	AllowLockoutOverride bool   `json:"allow_lockout_override,omitempty" jsonschema:"Explicit acknowledgement required for a self-lockout-capable edit"`
}

// ActiveConnection is one currently connected DSM client, used only to protect
// active sources from being blocked or removed from the allow list. It carries
// no session identity (no SID, SynoToken, or device id).
type ActiveConnection struct {
	From    string `json:"from" jsonschema:"Source IP of the active connection"`
	Who     string `json:"who,omitempty" jsonschema:"Account name of the active connection, when reported"`
	Current bool   `json:"current,omitempty" jsonschema:"Whether DSM marks this as the current connection"`
}

// KindAllow and KindBlock are the two Auto Block list kinds.
const (
	KindAllow = "allow"
	KindBlock = "block"
)
