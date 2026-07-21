package loginportal

// This file carries the WI-070 Slice B guarded-write intents. Every write here
// changes how a service (including DSM itself) is reached, so all of Slice B is
// classified high risk for the DSM web-service area and medium for the portal /
// reverse-proxy areas, and rides the hash-bound plan/apply contract.
//
// The DSM-access change is the single most dangerous: it can sever the very
// transport (scheme/port) dsmctl and the administrator use to reach DSM. The
// never-break-the-current-session guard (in the application layer) refuses such a
// change unless AllowConnectivityBreak is set, hashing the observed current
// transport (Transport) into the plan so a reconnection on a different endpoint
// invalidates a stale plan.

// Transport is the scheme/port dsmctl is currently connected to DSM on. It is
// hashed into a DSM-access plan so the never-break guard can tell whether a
// requested change would sever the current session's transport, and so a later
// reconnection on a different endpoint makes a stale plan fail.
type Transport struct {
	Scheme string `json:"scheme" jsonschema:"Transport scheme dsmctl is connected on (http or https)"`
	Port   int    `json:"port" jsonschema:"TCP port dsmctl is connected on"`
}

// DSMWebServiceChange is the patch-only intent for the guarded DSM web-service
// write (Login Portal > DSM tab). A nil field keeps the current DSM value; the
// apply path merges this patch into the freshly read state so an unspecified
// field is never silently reset. Every field here is HIGH risk: changing a port,
// the HTTPS enable, the HTTP->HTTPS redirect, HSTS, HTTP/2, or the customized
// domain/hostname changes how DSM is reached.
type DSMWebServiceChange struct {
	HTTPPort            *int    `json:"http_port,omitempty" jsonschema:"DSM HTTP port; omit to keep the current value"`
	HTTPSPort           *int    `json:"https_port,omitempty" jsonschema:"DSM HTTPS port; omit to keep the current value"`
	HTTPSEnabled        *bool   `json:"https_enabled,omitempty" jsonschema:"Whether HTTPS access to DSM is enabled; omit to keep the current setting"`
	HTTPRedirectEnabled *bool   `json:"http_redirect_enabled,omitempty" jsonschema:"Whether HTTP is force-redirected to HTTPS; omit to keep the current setting"`
	HSTSEnabled         *bool   `json:"hsts_enabled,omitempty" jsonschema:"Whether HSTS is enabled (browsers cache it); omit to keep the current setting"`
	HTTP2Enabled        *bool   `json:"http2_enabled,omitempty" jsonschema:"Whether HTTP/2 is enabled; omit to keep the current setting"`
	CustomDomainEnabled *bool   `json:"custom_domain_enabled,omitempty" jsonschema:"Whether a customized DSM domain is enabled; omit to keep the current setting"`
	CustomDomain        *string `json:"custom_domain,omitempty" jsonschema:"Customized DSM domain/FQDN; omit to keep the current value"`
	ExternalHostname    *string `json:"external_hostname,omitempty" jsonschema:"Customized external hostname (SYNO.Core.Web.DSM.External); omit to keep the current value"`

	// AllowConnectivityBreak is the explicit, logged acknowledgement required to
	// apply a change that would sever the transport dsmctl is currently connected
	// on (moving/disabling the current HTTPS port or scheme, forcing a redirect
	// that bounces the current session, or enabling HSTS). Without it the plan is
	// refused.
	AllowConnectivityBreak bool `json:"allow_connectivity_break,omitempty" jsonschema:"Explicit acknowledgement required for a change that could sever the current dsmctl transport"`
}

// IsEmpty reports whether the patch carries no field.
func (c DSMWebServiceChange) IsEmpty() bool {
	return c.HTTPPort == nil && c.HTTPSPort == nil && c.HTTPSEnabled == nil && c.HTTPRedirectEnabled == nil &&
		c.HSTSEnabled == nil && c.HTTP2Enabled == nil && c.CustomDomainEnabled == nil && c.CustomDomain == nil &&
		c.ExternalHostname == nil
}

// ApplicationPortalChange is the patch-only intent for one application portal
// write (Login Portal > Applications tab), keyed by the application id. A nil
// field keeps the current value; the apply path merges the patch into the freshly
// read portal so sibling fields are never reset.
type ApplicationPortalChange struct {
	AppID         string  `json:"app_id" jsonschema:"DSM application id to change (SYNO.Core.AppPortal id)"`
	RedirectHTTPS *bool   `json:"redirect_https,omitempty" jsonschema:"Whether the app portal force-redirects HTTP to HTTPS; omit to keep the current setting"`
	Alias         *string `json:"alias,omitempty" jsonschema:"Path-portal alias; empty string clears it; omit to keep the current value"`
	HTTPPort      *int    `json:"http_port,omitempty" jsonschema:"Custom portal HTTP port; omit to keep the current value"`
	HTTPSPort     *int    `json:"https_port,omitempty" jsonschema:"Custom portal HTTPS port; omit to keep the current value"`
}

// IsEmpty reports whether the patch carries no changeable field.
func (c ApplicationPortalChange) IsEmpty() bool {
	return c.RedirectHTTPS == nil && c.Alias == nil && c.HTTPPort == nil && c.HTTPSPort == nil
}

// ReverseProxyHeader is one custom/proxy header on a reverse-proxy rule. A secret
// value (e.g. an injected auth token) must be supplied through CredentialRef
// (env:NAME), resolved only at apply time and never present in the request, plan,
// hash, or logs. A non-secret value is carried literally in Value.
type ReverseProxyHeader struct {
	Name          string `json:"name" jsonschema:"Header name"`
	Value         string `json:"value,omitempty" jsonschema:"Literal (non-secret) header value; use credential_ref for a secret value"`
	CredentialRef string `json:"credential_ref,omitempty" jsonschema:"env:NAME reference resolved at apply time for a secret header value; never stored in the plan"`
}

// ReverseProxyRuleCreate is the intent to create one reverse-proxy rule (Login
// Portal > Advanced). The server assigns the uuid. Frontend/Backend protocols are
// stable "http"/"https" names mapped to DSM's numeric enum at the wire.
type ReverseProxyRuleCreate struct {
	Description   string               `json:"description,omitempty" jsonschema:"Human-readable rule description"`
	Frontend      ReverseProxyEndpoint `json:"frontend" jsonschema:"Source endpoint the rule listens on (protocol, hostname, port)"`
	FrontendHSTS  bool                 `json:"frontend_hsts,omitempty" jsonschema:"Whether HSTS is enabled on the HTTPS frontend"`
	Backend       ReverseProxyEndpoint `json:"backend" jsonschema:"Destination endpoint the rule forwards to (protocol, hostname, port)"`
	CustomHeaders []ReverseProxyHeader `json:"custom_headers,omitempty" jsonschema:"Custom/proxy headers; secret values use credential_ref"`
}

// ReverseProxyRuleDelete is the intent to delete one reverse-proxy rule by its
// server-assigned uuid.
type ReverseProxyRuleDelete struct {
	UUID string `json:"uuid" jsonschema:"Server-assigned rule id (UUID) to delete"`
}
