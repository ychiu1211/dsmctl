// Package loginportal contains stable, DSM-version-independent models for the
// Control Panel > Login Portal surface: the DSM web-service access settings
// (ports, HTTPS, HTTP->HTTPS redirect, HSTS, HTTP/2, customized domain), the
// per-application portals, and the reverse-proxy rules. WebAPI names and field
// names stay behind the operation package.
//
// This is the read slice (WI-070 Slice A). Each of the three areas is a separate
// DSM API and a separate compatibility/failure boundary, so a NAS missing one
// still reports the others. The module reads only: it never surfaces certificate
// key material (certificate id only, presence-only where even the id is not
// available) and never surfaces a reverse-proxy header value (count only).
//
// Guarded writes (DSM port/HSTS/redirect, portal alias/port, reverse-proxy CRUD)
// are the deferred Slice-B follow-on; every write there is HIGH risk because it
// changes how DSM itself is reached.
package loginportal

// ModuleName is the stable product-facing identifier for the module.
const ModuleName = "login-portal"

// DSMWebService is the normalized Control Panel > Login Portal > DSM tab state:
// how DSM's own web service is reached. It is read from SYNO.Core.Web.DSM (get,
// v1 — v1 is used deliberately because DSM 7.3's v2 get omits enable_https and
// enable_hsts). The customized external hostname is an independently gated
// enrichment from the sibling SYNO.Core.Web.DSM.External api.
type DSMWebService struct {
	HTTPPort            int    `json:"http_port" jsonschema:"DSM HTTP port (DSM field http_port)"`
	HTTPSPort           int    `json:"https_port" jsonschema:"DSM HTTPS port (DSM field https_port)"`
	HTTPSEnabled        bool   `json:"https_enabled" jsonschema:"Whether HTTPS access to DSM is enabled (DSM field enable_https)"`
	HTTPRedirectEnabled bool   `json:"http_redirect_enabled" jsonschema:"Whether HTTP requests are force-redirected to HTTPS (DSM field enable_https_redirect)"`
	HSTSEnabled         bool   `json:"hsts_enabled" jsonschema:"Whether HTTP Strict Transport Security is enabled (DSM field enable_hsts)"`
	HTTP2Enabled        bool   `json:"http2_enabled" jsonschema:"Whether HTTP/2 is enabled (DSM field enable_spdy)"`
	CustomDomainEnabled bool   `json:"custom_domain_enabled" jsonschema:"Whether a customized domain is enabled for DSM (DSM field enable_custom_domain)"`
	CustomDomain        string `json:"custom_domain,omitempty" jsonschema:"Customized DSM domain/FQDN, when configured (DSM field fqdn)"`

	// ExternalDomainSupported reports whether the SYNO.Core.Web.DSM.External
	// sibling api was present; when false, ExternalHostname is not meaningful.
	ExternalDomainSupported bool   `json:"external_domain_supported" jsonschema:"Whether the customized-domain sibling API (SYNO.Core.Web.DSM.External) is available on this NAS"`
	ExternalHostname        string `json:"external_hostname,omitempty" jsonschema:"Customized external hostname, when configured (SYNO.Core.Web.DSM.External hostname)"`
}

// ApplicationPortal is one entry of the Login Portal > Applications tab: a DSM
// application and how it is reached through its own portal. On DSM 7.3 the read
// surfaces the app id, title, and per-app HTTP->HTTPS redirect; alias and custom
// portal ports are surfaced only when a custom portal is configured.
type ApplicationPortal struct {
	AppID         string `json:"app_id" jsonschema:"DSM application id (DSM field id)"`
	DisplayName   string `json:"display_name" jsonschema:"Human-readable application name (DSM field display_name)"`
	RedirectHTTPS bool   `json:"redirect_https" jsonschema:"Whether this application's portal force-redirects HTTP to HTTPS (DSM field enable_redirect)"`
	Alias         string `json:"alias,omitempty" jsonschema:"Path-portal alias, when a custom alias portal is configured"`
	HTTPPort      int    `json:"http_port,omitempty" jsonschema:"Custom portal HTTP port, when a port portal is configured"`
	HTTPSPort     int    `json:"https_port,omitempty" jsonschema:"Custom portal HTTPS port, when a port portal is configured"`
}

// ApplicationPortals is the full Login Portal > Applications list.
type ApplicationPortals struct {
	Total   int                 `json:"total" jsonschema:"Number of application portals returned"`
	Portals []ApplicationPortal `json:"portals" jsonschema:"Per-application portal entries"`
}

// ReverseProxyEndpoint is one side (frontend or backend) of a reverse-proxy
// rule: which protocol/host/port the rule listens on or forwards to.
type ReverseProxyEndpoint struct {
	Protocol string `json:"protocol,omitempty" jsonschema:"http or https"`
	Hostname string `json:"hostname,omitempty" jsonschema:"Hostname (may be empty for a wildcard/any host)"`
	Port     int    `json:"port,omitempty" jsonschema:"TCP port"`
}

// ReverseProxyRule is one Login Portal > Advanced reverse-proxy rule. Rules are
// keyed by the server-assigned uuid. Certificate key material is never surfaced
// (certificate id only, and only its presence when the id itself is not exposed),
// and custom header VALUES are never surfaced (count only) so an injected auth
// token cannot leak through this read.
type ReverseProxyRule struct {
	UUID               string               `json:"uuid" jsonschema:"Server-assigned rule id"`
	Description        string               `json:"description,omitempty" jsonschema:"Human-readable rule description"`
	Frontend           ReverseProxyEndpoint `json:"frontend" jsonschema:"Source endpoint the rule listens on"`
	Backend            ReverseProxyEndpoint `json:"backend" jsonschema:"Destination endpoint the rule forwards to"`
	HSTSEnabled        bool                 `json:"hsts_enabled" jsonschema:"Whether HSTS is enabled on the frontend"`
	HTTP2Enabled       bool                 `json:"http2_enabled" jsonschema:"Whether HTTP/2 is enabled on the frontend"`
	CertificatePresent bool                 `json:"certificate_present" jsonschema:"Whether a certificate is referenced by the frontend; never exposes key material"`
	CustomHeaderCount  int                  `json:"custom_header_count" jsonschema:"Number of custom/proxy headers configured; header values are never surfaced"`
}

// ReverseProxyRules is the full reverse-proxy rule set. The whole set is carried
// (not just one rule) so a plan/apply follow-on can fingerprint it.
type ReverseProxyRules struct {
	Total int                `json:"total" jsonschema:"Number of reverse-proxy rules configured"`
	Rules []ReverseProxyRule `json:"rules" jsonschema:"Reverse-proxy rules"`
}

// Capabilities reports which Login Portal reads and guarded writes dsmctl exposes
// for the selected NAS. Each area is gated on its own DSM API so a NAS missing
// one still reports the others.
type Capabilities struct {
	Module                 string `json:"module" jsonschema:"Stable module name: login-portal"`
	DSMWebServiceRead      bool   `json:"dsm_web_service_read" jsonschema:"Whether the DSM web-service access settings can be read"`
	ExternalDomainRead     bool   `json:"external_domain_read" jsonschema:"Whether the customized external-hostname setting can be read"`
	ApplicationPortalRead  bool   `json:"application_portal_read" jsonschema:"Whether the per-application portal list can be read"`
	ReverseProxyRead       bool   `json:"reverse_proxy_read" jsonschema:"Whether the reverse-proxy rule list can be read"`
	DSMWebServiceWrite     bool   `json:"dsm_web_service_write" jsonschema:"Whether the DSM web-service access settings can be changed"`
	ExternalDomainWrite    bool   `json:"external_domain_write" jsonschema:"Whether the customized external-hostname setting can be changed"`
	ApplicationPortalWrite bool   `json:"application_portal_write" jsonschema:"Whether an application portal can be changed"`
	ReverseProxyWrite      bool   `json:"reverse_proxy_write" jsonschema:"Whether reverse-proxy rules can be created or deleted"`
	Mutations              bool   `json:"mutations" jsonschema:"Whether any guarded write is available"`
}
