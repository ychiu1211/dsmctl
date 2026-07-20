// Package certificate contains stable, DSM-version-independent models for the
// Control Panel → Security → Certificate surface: the installed certificates,
// their public metadata, and which DSM services each one serves. WebAPI names
// and field names stay behind the operation package.
//
// The model carries public certificate metadata only. Private-key material is
// never decoded into these types, never returned by a read, and only ever
// supplied to a future guarded import through a credential reference.
package certificate

// ModuleName is the stable product-facing identifier for the module.
const ModuleName = "certificate"

// Name is a certificate distinguished-name subset (the fields DSM reports).
type Name struct {
	CommonName   string `json:"common_name,omitempty" jsonschema:"Common name (CN)"`
	Organization string `json:"organization,omitempty" jsonschema:"Organization (O)"`
	Country      string `json:"country,omitempty" jsonschema:"Country (C)"`
}

// Service is one DSM service (or package) that presents a certificate.
type Service struct {
	Service     string `json:"service" jsonschema:"Stable DSM service key, for example default (DSM desktop), ftpd, or a package id"`
	Subscriber  string `json:"subscriber,omitempty" jsonschema:"Subscriber that owns the binding"`
	Owner       string `json:"owner,omitempty" jsonschema:"Owning account, usually root"`
	DisplayName string `json:"display_name,omitempty" jsonschema:"Human-readable service name as shown in the Admin Console"`
	IsPackage   bool   `json:"is_package,omitempty" jsonschema:"Whether the service belongs to an installed package rather than DSM core"`
}

// Certificate is one installed certificate with its public metadata and the
// services bound to it. Drive's Admin Console reports the service bindings
// inline with each certificate, so no separate binding read is needed.
type Certificate struct {
	ID                 string    `json:"id" jsonschema:"Stable certificate identifier used by set/bind/delete"`
	Description        string    `json:"description,omitempty" jsonschema:"User description/label"`
	IsDefault          bool      `json:"is_default" jsonschema:"Whether this is the default certificate DSM presents"`
	IsBroken           bool      `json:"is_broken" jsonschema:"Whether DSM reports the certificate as broken (missing key, chain error)"`
	SelfSigned         bool      `json:"self_signed" jsonschema:"Whether the certificate is self-signed (DSM-generated)"`
	Renewable          bool      `json:"renewable" jsonschema:"Whether DSM can renew this certificate (Let's Encrypt / Synology-issued)"`
	KeyTypes           string    `json:"key_types,omitempty" jsonschema:"Key algorithm(s), for example RSA or RSA/ECC"`
	SignatureAlgorithm string    `json:"signature_algorithm,omitempty" jsonschema:"Signature algorithm, for example sha256WithRSAEncryption"`
	Subject            Name      `json:"subject" jsonschema:"Certificate subject"`
	SubjectAltNames    []string  `json:"subject_alt_names,omitempty" jsonschema:"Subject alternative names (SAN)"`
	Issuer             Name      `json:"issuer" jsonschema:"Certificate issuer"`
	ValidFrom          string    `json:"valid_from,omitempty" jsonschema:"Not-before, as reported by DSM"`
	ValidTill          string    `json:"valid_till,omitempty" jsonschema:"Not-after, as reported by DSM"`
	ValidFromUnix      int64     `json:"valid_from_unix,omitempty" jsonschema:"Not-before parsed to a Unix time in seconds, when parseable"`
	ValidTillUnix      int64     `json:"valid_till_unix,omitempty" jsonschema:"Not-after parsed to a Unix time in seconds, when parseable"`
	UserDeletable      bool      `json:"user_deletable" jsonschema:"Whether the certificate may be deleted"`
	Services           []Service `json:"services" jsonschema:"DSM services and packages bound to this certificate"`
}

// Certificates is the installed-certificate inventory.
type Certificates struct {
	Total        int           `json:"total" jsonschema:"Number of installed certificates"`
	Certificates []Certificate `json:"certificates" jsonschema:"Installed certificates with their bound services"`
}

// Capabilities reports which certificate operations dsmctl currently exposes.
// This slice is read-only; the guarded writes (import, set-default, service
// binding, delete) are modeled in the work item but deferred because every one
// can break admin TLS and needs explicit per-operation live authorization.
type Capabilities struct {
	Module           string `json:"module" jsonschema:"Stable module name: certificate"`
	CertificatesRead bool   `json:"certificates_read" jsonschema:"Whether the installed-certificate inventory can be read"`
}
