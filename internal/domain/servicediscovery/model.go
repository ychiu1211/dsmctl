// Package servicediscovery models the DSM File Services "service discovery"
// toggles independently of DSM request field names. Time Machine advertising
// (over SMB and AFP) is one DSM API and WS-Discovery is a separate one, so a
// backend can expose Time Machine advertising while WS-Discovery is absent.
package servicediscovery

// State is the observed service-discovery configuration. WSDiscovery is a
// pointer because the WS-Discovery backend is selected independently of the
// Time Machine backend and may be unavailable; nil means "not exposed by this
// DSM backend".
type State struct {
	SMBTimeMachine bool  `json:"smb_time_machine" jsonschema:"Whether DSM advertises Time Machine over SMB (Bonjour)"`
	AFPTimeMachine bool  `json:"afp_time_machine" jsonschema:"Whether DSM advertises Time Machine over AFP (Bonjour)"`
	WSDiscovery    *bool `json:"ws_discovery,omitempty" jsonschema:"Whether WS-Discovery is enabled, when the DSM backend exposes it"`
}

// Capabilities reports which service-discovery operations the selected DSM
// backends expose.
type Capabilities struct {
	Read        bool `json:"read" jsonschema:"Whether Time Machine advertising can be read"`
	Set         bool `json:"set" jsonschema:"Whether Time Machine advertising can be changed through guarded plan/apply"`
	WSDiscovery bool `json:"ws_discovery" jsonschema:"Whether WS-Discovery can be read and changed through guarded plan/apply"`
}

// Change is a patch: an omitted (nil) field preserves its current DSM value.
type Change struct {
	SMBTimeMachine *bool `json:"smb_time_machine,omitempty" jsonschema:"Enable or disable Time Machine advertising over SMB"`
	AFPTimeMachine *bool `json:"afp_time_machine,omitempty" jsonschema:"Enable or disable Time Machine advertising over AFP"`
	WSDiscovery    *bool `json:"ws_discovery,omitempty" jsonschema:"Enable or disable WS-Discovery"`
}
