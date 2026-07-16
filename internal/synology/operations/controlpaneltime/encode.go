package controlpaneltime

import (
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/controlpanel"
)

// encodeTimeSet emits the complete desired configuration for the DSM set
// form. Every field is required because the caller submits merged state, so
// a missing value here is a contract violation rather than a field to skip.
// Volatile wall-clock parameters are never part of the request, and the
// encoder refuses to emit manual synchronization under any circumstances.
func encodeTimeSet(desired controlpanel.TimeState) (map[string]any, error) {
	timeZone := strings.TrimSpace(desired.TimeZone)
	if timeZone == "" {
		return nil, fmt.Errorf("time set requires a time zone")
	}
	dateFormat := strings.TrimSpace(desired.DateFormat)
	timeFormat := strings.TrimSpace(desired.TimeFormat)
	if dateFormat == "" || timeFormat == "" {
		return nil, fmt.Errorf("time set requires the date and time display formats")
	}
	if desired.SynchronizationMode != controlpanel.TimeSynchronizationNTP {
		return nil, fmt.Errorf("time set supports only NTP synchronization; manual mode owns the wall clock and is excluded")
	}
	if len(desired.NTPServers) == 0 {
		return nil, fmt.Errorf("time set requires at least one NTP server")
	}
	servers := make([]string, 0, len(desired.NTPServers))
	for _, server := range desired.NTPServers {
		trimmed := strings.TrimSpace(server)
		if trimmed == "" {
			return nil, fmt.Errorf("time set rejects an empty NTP server entry")
		}
		if strings.ContainsAny(trimmed, ", \t") {
			return nil, fmt.Errorf("NTP server %q must not contain commas or whitespace", server)
		}
		servers = append(servers, trimmed)
	}
	return map[string]any{
		"timezone":    timeZone,
		"date_format": dateFormat,
		"time_format": timeFormat,
		"enable_ntp":  string(controlpanel.TimeSynchronizationNTP),
		"server":      strings.Join(servers, ","),
	}, nil
}
