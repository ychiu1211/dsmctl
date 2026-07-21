// Package kmip contains stable, read-only models for the Control Panel /
// Storage Manager KMIP (Key Management Interoperability Protocol) surface:
// whether this NAS acts as a KMIP client (escrowing encrypted-share keys to an
// external KMIP server) and/or as the local KMIP server (holding keys for other
// Synology devices), including connection status and the certificate identity in
// use.
//
// Live-verified against the lab (DSM 7.3-81168, DS3018xs): the family is a
// single combined config API, SYNO.Storage.CGI.KMIP v1, whose only read method
// is `get`. DSM exposes no split client/server API families and no separate
// escrowed-key or registered-client list method (every candidate list/enum
// method returned code 103 "method does not exist"). Both roles are therefore
// modeled from the one `get` response.
//
// SECRET HYGIENE: KMIP deals in cryptographic KEY MATERIAL. This module is
// read-only and models only explicit non-secret metadata — enable flags,
// hostnames, ports, a key-database location path, connection status, and the
// non-secret certificate-binding metadata DSM reports. It never reads private
// keys, wrapped/managed/escrowed key bytes, passphrases, pre-shared secrets, or
// client credentials into any model or log. The decoders read an explicit
// whitelist of keys, never a whole-object passthrough, and a no-leak canary test
// asserts no secret value or secret-bearing key survives a decode.
package kmip

// CertBinding is the non-secret certificate identity bound to the KMIP client or
// server TLS endpoint, decoded from the client_cert_info / server_cert_info
// object DSM returns. Bound is the reliably-derived signal (the object is present
// and non-null); the metadata fields are a best-effort non-secret whitelist.
//
// On the lab both cert_info objects were null (KMIP unconfigured), so the
// populated object's exact key names are wire-unverified — the decoder reads a
// conservative whitelist of the well-known DSM certificate-metadata keys and
// never reads any key/secret field. No private key or key material is ever read:
// a certificate "info" block carries identity metadata only, and the whitelist
// excludes anything key-bearing regardless.
type CertBinding struct {
	Bound       bool   `json:"bound" jsonschema:"Whether a certificate is bound to this KMIP endpoint"`
	ID          string `json:"id,omitempty" jsonschema:"Installed-certificate id the binding references, when reported"`
	Description string `json:"description,omitempty" jsonschema:"Certificate description/label, when reported"`
	Subject     string `json:"subject,omitempty" jsonschema:"Certificate subject common name, when reported"`
	Issuer      string `json:"issuer,omitempty" jsonschema:"Certificate issuer common name, when reported"`
	Fingerprint string `json:"fingerprint,omitempty" jsonschema:"Certificate fingerprint, when reported; an identity, never key material"`
	ValidTill   string `json:"valid_till,omitempty" jsonschema:"Certificate not-after as DSM reports it, when present"`
}

// ServerRole is the local KMIP server configuration: whether this NAS runs a
// KMIP server holding keys for other Synology devices, where that server keeps
// its key database, and the certificate identity it presents. The escrowed key
// bytes themselves are never surfaced (DSM exposes no list method, and none
// would be read).
type ServerRole struct {
	Enabled          bool         `json:"enabled" jsonschema:"Whether the local KMIP server is enabled (server_enable)"`
	DatabaseLocation string       `json:"database_location,omitempty" jsonschema:"Filesystem location of the KMIP server key database (kmip_db_loc); a path, never key material"`
	ListenPort       string       `json:"listen_port,omitempty" jsonschema:"KMIP server listening port DSM reports (kmip_server_port); default 5696"`
	Certificate      *CertBinding `json:"certificate,omitempty" jsonschema:"Certificate identity bound to the KMIP server, when present"`
}

// ClientRole is the external-KMS client configuration: whether this NAS points
// at an external KMIP server to escrow its own keys, the server it targets, the
// last-connection health, and the client certificate identity. No client
// credential, private key, or pre-shared secret is ever surfaced.
type ClientRole struct {
	Enabled         bool         `json:"enabled" jsonschema:"Whether the KMIP client is enabled (client_enable)"`
	ServerAddress   string       `json:"server_address,omitempty" jsonschema:"External KMIP server address the client targets (kmip_server)"`
	ServerPort      string       `json:"server_port,omitempty" jsonschema:"External KMIP server port the client targets (kmip_conn_server_port); default 5696"`
	ServerName      string       `json:"server_name,omitempty" jsonschema:"Description/identity of the connected KMIP server (kmip_conn_server_desc)"`
	ConnectionOK    bool         `json:"connection_ok" jsonschema:"Whether the client's last connection to the external server succeeded (conn_success)"`
	LastConnectedAt string       `json:"last_connected_at,omitempty" jsonschema:"Timestamp of the last successful client connection as DSM reports it (conn_time)"`
	Certificate     *CertBinding `json:"certificate,omitempty" jsonschema:"Certificate identity the client authenticates with, when present"`
}

// Status is the combined KMIP role/status read from SYNO.Storage.CGI.KMIP.get.
// Supported reflects DSM's own support_kmip flag: whether this NAS model/edition
// offers KMIP at all. Even when Supported is false the read succeeds and returns
// the (disabled) client/server state — the honest not-configured view.
type Status struct {
	Supported bool       `json:"supported" jsonschema:"Whether this NAS model/edition supports KMIP (DSM support_kmip flag)"`
	Mode      string     `json:"mode,omitempty" jsonschema:"KMIP mode DSM reports (kmip_mode), such as client or server; empty when unconfigured"`
	Server    ServerRole `json:"server" jsonschema:"Local KMIP server role"`
	Client    ClientRole `json:"client" jsonschema:"External KMIP client role"`
}

// Capabilities reports whether the KMIP read surface is available on this NAS.
// The SYNO.Storage.CGI.KMIP family is DSM-core (Storage Manager), so Read is
// normally true on DSM 7.x; Supported distinguishes a NAS that actually offers
// the KMIP feature from one that merely advertises the config API.
type Capabilities struct {
	Module    string `json:"module" jsonschema:"Stable module name: kmip"`
	Read      bool   `json:"read" jsonschema:"Whether KMIP status can be read (SYNO.Storage.CGI.KMIP.get)"`
	Supported bool   `json:"supported" jsonschema:"Whether this NAS reports KMIP as supported (support_kmip)"`
}
