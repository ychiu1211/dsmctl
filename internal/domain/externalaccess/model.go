// Package externalaccess contains stable, read-only models for the DSM Control
// Panel → External Access surface: the Synology Account binding, QuickConnect,
// and DDNS. Each area owns a separate state type and a separate DSM API family,
// so one area being unavailable never disables the others.
//
// These models are deliberately free of any authentication material. DSM's
// account and QuickConnect reads return tokens such as `auth_key` and internal
// identifiers; those are never decoded into these types so a display or MCP
// path cannot leak them.
package externalaccess

// AccountState is the normalized Synology Account (MyDS) binding for a NAS. It
// reports whether an account is linked and activated, plus the non-secret
// account identity — never the account token.
type AccountState struct {
	LoggedIn  bool   `json:"logged_in" jsonschema:"Whether a Synology Account is currently signed in on the NAS"`
	Activated bool   `json:"activated" jsonschema:"Whether the linked Synology Account is activated"`
	Account   string `json:"account,omitempty" jsonschema:"Linked Synology Account identifier (email); empty when none"`
	MyDSID    string `json:"myds_id,omitempty" jsonschema:"Synology Account customer identifier; empty when none"`
	Serial    string `json:"serial,omitempty" jsonschema:"NAS serial number as reported by the account service"`
}

// QuickConnectService is one QuickConnect-reachable service and whether it is
// exposed externally.
type QuickConnectService struct {
	ID      string `json:"id" jsonschema:"QuickConnect service identifier, such as dsm_portal or file_sharing"`
	Enabled bool   `json:"enabled" jsonschema:"Whether the service is reachable through QuickConnect"`
}

// QuickConnectState is the normalized QuickConnect configuration and live
// connection status. ID is the user-facing QuickConnect alias (DSM's
// server_alias); the NAS is reachable at "<id>.<domain>" via relay and
// "<id>.<direct_domain>" directly.
type QuickConnectState struct {
	Enabled          bool                  `json:"enabled" jsonschema:"Whether QuickConnect is enabled"`
	ID               string                `json:"id,omitempty" jsonschema:"QuickConnect alias (server_alias); the external hostname label"`
	Region           string                `json:"region,omitempty" jsonschema:"QuickConnect registration region"`
	Domain           string                `json:"domain,omitempty" jsonschema:"QuickConnect relay domain, such as quickconnect.to"`
	DirectDomain     string                `json:"direct_domain,omitempty" jsonschema:"QuickConnect direct-connection domain, such as direct.quickconnect.to"`
	RelayEnabled     *bool                 `json:"relay_enabled,omitempty" jsonschema:"Whether relayed connections are allowed; null when the relay setting API is unavailable"`
	ConnectionStatus string                `json:"connection_status,omitempty" jsonschema:"Live QuickConnect connection status, such as connected"`
	AliasStatus      string                `json:"alias_status,omitempty" jsonschema:"Live QuickConnect alias registration status"`
	Services         []QuickConnectService `json:"services,omitempty" jsonschema:"Per-service external reachability; null when the permission API is unavailable"`
}

// ExternalAddress is one WAN address DSM detected for the NAS, used by DDNS to
// publish a reachable IP.
type ExternalAddress struct {
	IP   string `json:"ip,omitempty" jsonschema:"Detected external IPv4 address"`
	IPv6 string `json:"ipv6,omitempty" jsonschema:"Detected external IPv6 address"`
	Type string `json:"type,omitempty" jsonschema:"Address classification reported by DSM, such as WAN"`
}

// DDNSRecord is one configured Dynamic DNS hostname. Its fields are decoded
// tolerantly: the lab used to model this type has no configured record, so only
// fields DSM actually returns are populated and unknown extras are ignored.
type DDNSRecord struct {
	Hostname string `json:"hostname,omitempty" jsonschema:"Configured DDNS hostname"`
	Provider string `json:"provider,omitempty" jsonschema:"DDNS provider identifier, such as Synology"`
	Status   string `json:"status,omitempty" jsonschema:"Last DDNS update status reported by DSM"`
	IPv4     string `json:"ipv4,omitempty" jsonschema:"Published IPv4 address, when present"`
	IPv6     string `json:"ipv6,omitempty" jsonschema:"Published IPv6 address, when present"`
}

// DDNSState is the normalized DDNS view: the configured records plus the WAN
// addresses DSM detected. An empty Records slice means no DDNS hostname is
// configured.
type DDNSState struct {
	Records         []DDNSRecord      `json:"records" jsonschema:"Configured DDNS hostnames; empty when none"`
	NextUpdateTime  string            `json:"next_update_time,omitempty" jsonschema:"DSM's next scheduled DDNS update time, when reported"`
	ExternalAddress []ExternalAddress `json:"external_address" jsonschema:"WAN addresses DSM detected for the NAS"`
}

// PortForwardRouter is the router DSM is configured to program port-forwarding
// rules on (Control Panel → External Access → Router Configuration). All fields
// are empty when no router is paired.
type PortForwardRouter struct {
	Brand             string `json:"brand,omitempty" jsonschema:"Configured router brand; empty when no router is paired"`
	Model             string `json:"model,omitempty" jsonschema:"Configured router model"`
	Version           string `json:"version,omitempty" jsonschema:"Configured router firmware version"`
	SupportUPnP       string `json:"support_upnp,omitempty" jsonschema:"Whether the router supports UPnP, as reported by DSM"`
	SupportNATPMP     string `json:"support_natpmp,omitempty" jsonschema:"Whether the router supports NAT-PMP, as reported by DSM"`
	SupportChangePort bool   `json:"support_change_port" jsonschema:"Whether DSM can change the router management port"`
}

// PortForwardRule is one configured port-forwarding rule. Its fields are decoded
// tolerantly: the NAS used to model this type has no configured rule, so only
// fields DSM actually returns are populated and unknown extras are ignored.
type PortForwardRule struct {
	Description string `json:"description,omitempty" jsonschema:"Rule description or service name"`
	Protocol    string `json:"protocol,omitempty" jsonschema:"Forwarded protocol, such as TCP or UDP"`
	PublicPort  string `json:"public_port,omitempty" jsonschema:"External (router) port or range"`
	PrivatePort string `json:"private_port,omitempty" jsonschema:"Internal (NAS) port or range"`
}

// PortForwardState is the normalized Router Configuration view: the paired
// router and the configured port-forwarding rules. An empty Rules slice means
// no rule is configured.
type PortForwardState struct {
	Router PortForwardRouter `json:"router" jsonschema:"Paired router configuration; empty fields when none is paired"`
	Rules  []PortForwardRule `json:"rules" jsonschema:"Configured port-forwarding rules; empty when none"`
}

// QuickConnectChange is the patch-only intent for a guarded QuickConnect
// mutation. A nil field keeps the current DSM value. Only the relay toggle is
// modeled: it is a local reachability setting, unlike enabling QuickConnect or
// changing the alias, which re-register the NAS externally.
type QuickConnectChange struct {
	RelayEnabled *bool `json:"relay_enabled,omitempty" jsonschema:"Desired QuickConnect relay-allowed flag; omit to keep the current value"`
}

// Capabilities reports which External Access read areas are currently exposed
// for a NAS. Each is independent: a NAS may expose QuickConnect and DDNS while
// the account read is unavailable.
type Capabilities struct {
	Account         bool `json:"account" jsonschema:"Whether the Synology Account binding can be read"`
	QuickConnect    bool `json:"quickconnect" jsonschema:"Whether QuickConnect configuration can be read"`
	QuickConnectSet bool `json:"quickconnect_set" jsonschema:"Whether the guarded QuickConnect relay toggle is available"`
	DDNS            bool `json:"ddns" jsonschema:"Whether DDNS records can be read"`
	PortForward     bool `json:"port_forward" jsonschema:"Whether the router/port-forwarding view can be read"`
}
