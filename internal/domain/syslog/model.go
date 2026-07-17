// Package syslog holds the stable, DSM-version-independent model for reading DSM
// system logs (Log Center). It is read-only: dsmctl never mutates or clears logs.
package syslog

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// LogType* are the canonical DSM log categories accepted by the log_type
	// filter. DSM reports the same values in each entry's Type field.
	LogTypeSystem       = "system"
	LogTypeConnection   = "connection"
	LogTypeFileTransfer = "fileTransfer"

	// Level* are the normalized severities DSM reports on each entry.
	LevelInfo    = "info"
	LevelWarning = "warn"
	LevelError   = "error"
)

// Entry is one normalized DSM log record.
type Entry struct {
	Time    string `json:"time" jsonschema:"Local DSM timestamp, for example 2026/07/17 13:35:55"`
	Level   string `json:"level" jsonschema:"Normalized severity reported by DSM: info, warn, or error"`
	Type    string `json:"type,omitempty" jsonschema:"Canonical DSM log category such as system, connection, or fileTransfer"`
	Who     string `json:"who,omitempty" jsonschema:"Account or actor associated with the entry"`
	Message string `json:"message" jsonschema:"Human-readable log description"`
}

// State is a page of DSM log entries plus the whole-log severity counts DSM
// reports for the current filter.
type State struct {
	Total      int     `json:"total" jsonschema:"Total number of log entries matching the query before pagination"`
	InfoCount  int     `json:"info_count" jsonschema:"Number of informational entries reported by DSM"`
	WarnCount  int     `json:"warn_count" jsonschema:"Number of warning entries reported by DSM"`
	ErrorCount int     `json:"error_count" jsonschema:"Number of error entries reported by DSM"`
	Entries    []Entry `json:"entries" jsonschema:"Log entries for the requested page"`
}

// StateQuery selects and pages DSM log entries. Keyword, LogType, and the
// From/To time range are applied by DSM; Level is applied by dsmctl to the
// retrieved page because DSM does not expose a stable server-side severity
// filter. LogType defaults to the DSM system category when empty.
type StateQuery struct {
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum entries to return; defaults to a bounded page size"`
	Offset  int    `json:"offset,omitempty" jsonschema:"Number of newest entries to skip for pagination"`
	Keyword string `json:"keyword,omitempty" jsonschema:"Case-insensitive substring filter applied by DSM"`
	LogType string `json:"log_type,omitempty" jsonschema:"DSM log category filter; defaults to system. Also: connection, package, or fileTransfer"`
	Level   string `json:"level,omitempty" jsonschema:"Client-side severity filter over the retrieved page: info, warn, or error"`
	From    int64  `json:"from,omitempty" jsonschema:"Inclusive lower bound as a Unix time in seconds; entries older than this are excluded"`
	To      int64  `json:"to,omitempty" jsonschema:"Inclusive upper bound as a Unix time in seconds; requires From to take effect"`
}

// ParseTime converts a CLI/MCP time bound into a Unix time in seconds. It accepts
// an empty string (0, unset), a raw Unix-seconds integer, or a local-time
// timestamp in "2006-01-02" or "2006-01-02 15:04:05" form.
func ParseTime(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, nil
	}
	if seconds, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return seconds, nil
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, trimmed, time.Local); err == nil {
			return parsed.Unix(), nil
		}
	}
	return 0, fmt.Errorf("invalid time %q: use a Unix-seconds integer or a local timestamp such as 2006-01-02 or \"2006-01-02 15:04:05\"", value)
}

// Capabilities reports whether DSM log reading is available on the target.
type Capabilities struct {
	Read bool `json:"read" jsonschema:"Whether DSM system logs can be read"`
}
