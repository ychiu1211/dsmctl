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

// Capabilities reports which account-protection reads dsmctl currently exposes
// for the selected NAS. Each read area is gated on its own DSM API so a NAS
// missing one still reports the others. Guarded writes are a deferred follow-on,
// so Mutations is always false in this slice.
type Capabilities struct {
	Module                string `json:"module" jsonschema:"Stable module name: account-protection"`
	AutoBlockRead         bool   `json:"autoblock_read" jsonschema:"Whether Auto Block settings can be read"`
	AutoBlockListRead     bool   `json:"autoblock_list_read" jsonschema:"Whether the Auto Block allow/block IP lists can be read"`
	AccountProtectionRead bool   `json:"account_protection_read" jsonschema:"Whether Account Protection thresholds can be read"`
	EnforceTwoFactorRead  bool   `json:"enforce_2fa_read" jsonschema:"Whether the enforced-2FA policy can be read"`
	DoSPresent            bool   `json:"dos_present" jsonschema:"Whether the DoS-protection API is advertised by this NAS; the DoS read is a deferred follow-on"`
	Mutations             bool   `json:"mutations" jsonschema:"Whether any guarded write is available (always false in the read slice)"`
}
