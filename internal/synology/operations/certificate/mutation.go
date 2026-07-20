package certificate

// Guarded certificate WRITE wire surface. Everything here is isolated so a
// live-verification pass can correct a stale wire name in ONE place.
//
// LIVE-VERIFIED (WI-065, DSM 7.3): the multipart IMPORT is the `import` method on
// the PARENT api SYNO.Core.Certificate (entry.cgi, version 1) — NOT on
// SYNO.Core.Certificate.CRT, whose `import` is rejected with code 103 (method
// does not exist). A successful live import confirmed the import multipart
// file-part names (key/cert/inter_cert) and the form fields id/desc/_sid. The one
// import detail still WIRE-UNVERIFIED is `as_default` (DSM treated the string
// "false" as truthy and marked the new cert default) — see doCertificateImport.
// CRT `list` is live-verified.
//
// WIRE-UNVERIFIED (WI-065): the CRT set/delete/export method PARAM shapes (the
// delete `id`-vs-`ids` array, the `set` as_default keying, export `id`) and the
// SYNO.Core.Certificate.Service `set` method + `settings` array shape are the
// spec author's best knowledge (CRT set/delete methods are confirmed to EXIST
// live; their exact params are not) and MUST be re-confirmed against a throwaway
// DSMCTL_DUMP probe before those writes are trusted against a real NAS.

import (
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	// ServiceAPIName is the service→certificate binding family. Its list is code
	// 103 on the lab (bindings are read inline from CRT.list), but its `set` is
	// the binding write.
	ServiceAPIName = "SYNO.Core.Certificate.Service"

	// CRTImportAPIName is the api the multipart IMPORT posts to. LIVE-VERIFIED
	// (DSM 7.3): `import` is a method on the PARENT SYNO.Core.Certificate, not on
	// CRT — CRT/import returns code 103 (method does not exist), while the parent
	// import SUCCEEDED live. ONLY import moves to the parent api; list/set/delete/
	// export stay on CRTAPIName. The endpoint stays entry.cgi (shared with CRT).
	CRTImportAPIName = "SYNO.Core.Certificate"

	// Capability names for the guarded writes.
	ImportCapabilityName      = "certificate.import"
	SetDefaultCapabilityName  = "certificate.set_default"
	BindServiceCapabilityName = "certificate.bind_service"
	DeleteCapabilityName      = "certificate.delete"
	ExportCapabilityName      = "certificate.export"

	// CRT write method names. `import` is LIVE-VERIFIED (DSM 7.3) but posts to the
	// parent api (CRTImportAPIName), not CRT. The CRT set/delete methods are
	// confirmed to EXIST live; export is still WIRE-UNVERIFIED — re-confirm live.
	CRTImportMethod = "import"
	CRTSetMethod    = "set"
	CRTDeleteMethod = "delete"
	CRTExportMethod = "export"

	// WIRE-UNVERIFIED (WI-065): Service binding write method name.
	ServiceSetMethod = "set"

	// import multipart file-part + form field names. LIVE-VERIFIED (DSM 7.3): a
	// successful live import used exactly key/cert/inter_cert plus id/desc/_sid.
	// ImportFieldAsDefault is the exception — WIRE-UNVERIFIED (as_default): DSM
	// treats the string "false" as truthy, so doCertificateImport sends this part
	// ONLY when the caller wants the cert to become default; re-confirm live.
	ImportFieldKey        = "key"
	ImportFieldCert       = "cert"
	ImportFieldInterCert  = "inter_cert"
	ImportFieldID         = "id"
	ImportFieldDesc       = "desc"
	ImportFieldAsDefault  = "as_default"
	ImportFieldSettleTime = "settle_time"

	// WIRE-UNVERIFIED (WI-065): CRT set / delete / export parameter names.
	SetFieldID        = "id"
	SetFieldAsDefault = "as_default"
	SetFieldDesc      = "desc"
	DeleteFieldID     = "id"
	ExportFieldID     = "id"

	// WIRE-UNVERIFIED (WI-065): Service set parameter names. DSM binds a service
	// by posting a JSON `settings` array of {service, id} objects.
	ServiceSetFieldSettings = "settings"
)

// MutationAPINames lists every DSM API the guarded writes may use, for discovery
// in one call. CRT is reused from the read operation.
func MutationAPINames() []string {
	return []string{CRTAPIName, ServiceAPIName}
}

// SupportsCRTWrites reports whether the CRT family (import/set/delete/export) is
// advertised. Writes reuse the same CRT v1 the read selected.
func SupportsCRTWrites(target compatibility.Target) bool {
	info, ok := target.API(CRTAPIName)
	return ok && info.Supports(1)
}

// SupportsServiceBinding reports whether the Service binding family is advertised
// for the service→certificate write. It is an independent boundary: a NAS may
// advertise CRT but not Service.
func SupportsServiceBinding(target compatibility.Target) bool {
	info, ok := target.API(ServiceAPIName)
	return ok && info.Supports(1)
}
