// Package controlpanel contains stable models for focused DSM Control Panel
// modules. Each module owns a separate state type so adding a module does not
// grow a single, weakly typed settings object.
package controlpanel

// ModuleName is the stable product-facing identifier for a Control Panel
// module. It is independent of the DSM WebAPI used by an implementation.
type ModuleName string

const (
	// ModuleTime identifies regional time and NTP configuration.
	ModuleTime ModuleName = "time"
	// ModuleSMB identifies global Server Message Block service settings.
	ModuleSMB ModuleName = "smb"
	// ModuleNFS identifies global Network File System service settings.
	ModuleNFS ModuleName = "nfs"
)

// TimeSynchronizationMode describes how DSM maintains system time.
type TimeSynchronizationMode string

const (
	TimeSynchronizationManual TimeSynchronizationMode = "manual"
	TimeSynchronizationNTP    TimeSynchronizationMode = "ntp"
)

// TimeState is the normalized, read-only state of the Control Panel time
// module. Current wall-clock values are deliberately excluded: callers need
// configuration, not a volatile value that changes between reads.
type TimeState struct {
	TimeZone            string                  `json:"time_zone" jsonschema:"Configured DSM time zone identifier"`
	DateFormat          string                  `json:"date_format,omitempty" jsonschema:"Configured DSM date display format; unavailable on legacy API v1"`
	TimeFormat          string                  `json:"time_format,omitempty" jsonschema:"Configured DSM time display format; unavailable on legacy API v1"`
	SynchronizationMode TimeSynchronizationMode `json:"synchronization_mode" jsonschema:"System time source: manual or ntp"`
	NTPServers          []string                `json:"ntp_servers" jsonschema:"Configured NTP servers in DSM preference order"`
}

// TimeCapabilities reports the independently selectable operations currently
// available for the time module. Mutation flags remain false until a separate
// safety contract is implemented.
type TimeCapabilities struct {
	Module ModuleName `json:"module" jsonschema:"Stable Control Panel module name"`
	Read   bool       `json:"read" jsonschema:"Whether time and NTP configuration can be read"`
	Set    bool       `json:"set" jsonschema:"Whether guarded time and NTP configuration changes are available"`
}
