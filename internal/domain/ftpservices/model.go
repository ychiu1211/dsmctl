// Package ftpservices models the DSM File Services "FTP" surface independently
// of DSM request field names. DSM groups three protocols on one page: plain FTP
// and FTP over TLS (FTPS) share one API (SYNO.Core.FileServ.FTP), and SFTP (file
// transfer over SSH) is a separate API (SYNO.Core.FileServ.FTP.SFTP). Each is an
// independent compatibility boundary, so SFTP may be unavailable while FTP is
// present.
package ftpservices

// State is the observed FTP and SFTP configuration. SFTP is a pointer because
// its backend is selected independently and may be absent; nil means "not
// exposed by this DSM backend".
type State struct {
	FTP  FTPState   `json:"ftp" jsonschema:"Plain FTP and FTPS service switches"`
	SFTP *SFTPState `json:"sftp,omitempty" jsonschema:"SFTP service switch and port, when the DSM backend exposes it"`
}

// FTPState is the plain-FTP and FTPS pair carried by SYNO.Core.FileServ.FTP.
// The two switches are independent: DSM can serve unencrypted FTP, FTPS, both,
// or neither.
type FTPState struct {
	Plain bool `json:"plain" jsonschema:"Whether unencrypted FTP is enabled"`
	FTPS  bool `json:"ftps" jsonschema:"Whether FTP over explicit TLS (FTPS) is enabled"`
}

// SFTPState is the SFTP switch and listening port.
type SFTPState struct {
	Enabled bool `json:"enabled" jsonschema:"Whether SFTP (file transfer over SSH) is enabled"`
	Port    int  `json:"port" jsonschema:"SFTP listening port"`
}

// Capabilities reports which FTP and SFTP operations the selected DSM backends
// expose.
type Capabilities struct {
	FTPRead  bool `json:"ftp_read" jsonschema:"Whether the FTP/FTPS switches can be read"`
	FTPSet   bool `json:"ftp_set" jsonschema:"Whether the FTP/FTPS switches can be changed through guarded plan/apply"`
	SFTPRead bool `json:"sftp_read" jsonschema:"Whether the SFTP switch and port can be read"`
	SFTPSet  bool `json:"sftp_set" jsonschema:"Whether the SFTP switch and port can be changed through guarded plan/apply"`
}

// Change is a patch: an omitted (nil) protocol or field preserves its current
// DSM value.
type Change struct {
	FTP  *FTPChange  `json:"ftp,omitempty" jsonschema:"Patch to the plain FTP and FTPS switches"`
	SFTP *SFTPChange `json:"sftp,omitempty" jsonschema:"Patch to the SFTP switch and port"`
}

// FTPChange patches the plain-FTP and FTPS switches. DSM's FTP set requires both
// switches on every write, so the facade fills omitted switches from the freshly
// read state before submitting.
type FTPChange struct {
	Plain *bool `json:"plain,omitempty" jsonschema:"Enable or disable unencrypted FTP"`
	FTPS  *bool `json:"ftps,omitempty" jsonschema:"Enable or disable FTP over explicit TLS (FTPS)"`
}

// SFTPChange patches the SFTP switch and port.
type SFTPChange struct {
	Enabled *bool `json:"enabled,omitempty" jsonschema:"Enable or disable SFTP"`
	Port    *int  `json:"port,omitempty" jsonschema:"SFTP listening port (1-65535)"`
}
