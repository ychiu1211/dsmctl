// Package resmon holds the stable, DSM-version-independent model for DSM
// Resource Monitor. It covers three things:
//
//   - current utilization (a volatile point-in-time snapshot),
//   - recorded utilization history, and
//   - the history-recording setting.
//
// Reads are read-only. Turning history recording on or off is a guarded
// plan/apply mutation modeled by RecordingChange. Domain field names are
// stable semantics, not raw DSM keys; decoders translate DSM's KB/rate/percent
// shapes at the boundary.
package resmon

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Dimension* are the canonical resource dimensions dsmctl normalizes from DSM
// Resource Monitor. DSM reports each under its own response group.
const (
	DimensionCPU     = "cpu"
	DimensionMemory  = "memory"
	DimensionNetwork = "network"
	DimensionDisk    = "disk"
	DimensionVolume  = "volume"
)

// Period* are the recorded-history windows DSM Resource Monitor exposes on DSM
// 7.x. DSM dropped the day window (real-time "current" covers the short term);
// the accepted server-side tokens are week, month, half_year, and year.
const (
	PeriodWeek     = "week"
	PeriodMonth    = "month"
	PeriodHalfYear = "half_year"
	PeriodYear     = "year"
)

// ErrHistoryRecordingDisabled is returned when DSM refuses a history read
// because resource history recording is turned off (DSM WebAPI error 1008,
// WEBAPI_CORE_SYSTEM_ERR_NOT_ENABLE_HISTORY). Enable recording first.
var ErrHistoryRecordingDisabled = errors.New("resource history recording is disabled on this NAS; enable it before reading history")

// Utilization is a point-in-time snapshot of DSM resource usage. It is
// volatile: two reads seconds apart differ. It is never planned against.
type Utilization struct {
	RecordingEnabled bool               `json:"recording_enabled" jsonschema:"Whether DSM is recording utilization history, when DSM reports it alongside the snapshot"`
	CPU              CPUUtilization     `json:"cpu" jsonschema:"Aggregate CPU load"`
	Memory           MemoryUtilization  `json:"memory" jsonschema:"Physical and swap memory usage"`
	Network          []NetworkInterface `json:"network" jsonschema:"Per-interface throughput; DSM includes a 'total' aggregate entry"`
	Disk             DiskUtilization    `json:"disk" jsonschema:"Aggregate and per-disk I/O"`
	Volumes          []VolumeIO         `json:"volumes" jsonschema:"Per-volume space I/O"`
}

// CPUUtilization is DSM's CPU breakdown. The *Percent fields are whole-percent
// values as reported by DSM; the load-average fields are DSM's run-queue
// averages as reported (not renormalized).
type CPUUtilization struct {
	Device        string `json:"device,omitempty" jsonschema:"DSM CPU device label, when present"`
	UserPercent   int    `json:"user_percent" jsonschema:"User-space CPU load percent as reported by DSM"`
	SystemPercent int    `json:"system_percent" jsonschema:"Kernel CPU load percent as reported by DSM"`
	OtherPercent  int    `json:"other_percent" jsonschema:"Other CPU load percent as reported by DSM"`
	LoadAverage1  int    `json:"load_average_1min" jsonschema:"1-minute load average as reported by DSM"`
	LoadAverage5  int    `json:"load_average_5min" jsonschema:"5-minute load average as reported by DSM"`
	LoadAverage15 int    `json:"load_average_15min" jsonschema:"15-minute load average as reported by DSM"`
}

// MemoryUtilization normalizes DSM's memory group. Byte fields are converted
// from DSM's kilobyte values at the decode boundary; percent fields are
// whole-percent values as reported by DSM.
type MemoryUtilization struct {
	RealUsagePercent int   `json:"real_usage_percent" jsonschema:"Physical memory usage percent as reported by DSM"`
	SwapUsagePercent int   `json:"swap_usage_percent" jsonschema:"Swap usage percent as reported by DSM"`
	TotalRealBytes   int64 `json:"total_real_bytes" jsonschema:"Total physical memory in bytes"`
	AvailRealBytes   int64 `json:"avail_real_bytes" jsonschema:"Available physical memory in bytes"`
	CachedBytes      int64 `json:"cached_bytes" jsonschema:"Cached memory in bytes"`
	BufferBytes      int64 `json:"buffer_bytes" jsonschema:"Buffer memory in bytes"`
	TotalSwapBytes   int64 `json:"total_swap_bytes" jsonschema:"Total swap in bytes"`
	AvailSwapBytes   int64 `json:"avail_swap_bytes" jsonschema:"Available swap in bytes"`
}

// NetworkInterface is throughput for one interface. DSM reports a synthetic
// entry with Device "total" that aggregates all interfaces.
type NetworkInterface struct {
	Device        string `json:"device" jsonschema:"Interface name, or 'total' for the aggregate DSM reports"`
	TxBytesPerSec int64  `json:"tx_bytes_per_sec" jsonschema:"Transmit rate in bytes per second"`
	RxBytesPerSec int64  `json:"rx_bytes_per_sec" jsonschema:"Receive rate in bytes per second"`
}

// DiskUtilization is aggregate plus per-disk I/O.
type DiskUtilization struct {
	Total DiskIO        `json:"total" jsonschema:"Aggregate disk I/O across all disks"`
	Disks []NamedDiskIO `json:"disks" jsonschema:"Per-disk I/O"`
}

// DiskIO is one disk's (or the aggregate's) I/O. Access fields are operations
// per second (IOPS); byte fields are bytes per second; utilization is a
// whole-percent busy value as reported by DSM.
type DiskIO struct {
	ReadOpsPerSec      int64 `json:"read_ops_per_sec" jsonschema:"Read operations per second"`
	WriteOpsPerSec     int64 `json:"write_ops_per_sec" jsonschema:"Write operations per second"`
	ReadBytesPerSec    int64 `json:"read_bytes_per_sec" jsonschema:"Read throughput in bytes per second"`
	WriteBytesPerSec   int64 `json:"write_bytes_per_sec" jsonschema:"Write throughput in bytes per second"`
	UtilizationPercent int   `json:"utilization_percent" jsonschema:"Disk busy percent as reported by DSM"`
}

// NamedDiskIO adds the DSM disk identity to a DiskIO reading.
type NamedDiskIO struct {
	Device      string `json:"device" jsonschema:"DSM disk device name, for example sda"`
	DisplayName string `json:"display_name,omitempty" jsonschema:"Human-readable disk name when DSM reports one"`
	DiskIO
}

// VolumeIO is one volume's space I/O.
type VolumeIO struct {
	Device      string `json:"device" jsonschema:"DSM volume device identifier"`
	DisplayName string `json:"display_name,omitempty" jsonschema:"Human-readable volume name when DSM reports one"`
	DiskIO
}

// History is recorded utilization over a time window. DSM records samples only
// while history recording is enabled; a disabled NAS yields
// ErrHistoryRecordingDisabled rather than an empty History.
type History struct {
	Period string          `json:"period" jsonschema:"Requested history window: week, month, half_year, or year"`
	Series []HistorySeries `json:"series" jsonschema:"One series per requested dimension, metric, and device"`
}

// HistorySeries is one metric's recorded samples for one dimension (optionally
// one device within it). DSM returns evenly-spaced samples spanning the window
// in its native order and carries no absolute timestamps, so the samples are
// exposed as an ordered value slice rather than timestamped points.
type HistorySeries struct {
	Dimension string    `json:"dimension" jsonschema:"Resource dimension: cpu, memory, network, disk, or volume"`
	Device    string    `json:"device,omitempty" jsonschema:"Device within the dimension when applicable, for example eth0, sda, or volume1"`
	Metric    string    `json:"metric" jsonschema:"Which value the samples carry, for example user_load, tx, or read_byte"`
	Values    []float64 `json:"values" jsonschema:"Evenly-spaced samples over the window, in the order DSM returns them"`
}

// HistoryQuery selects which dimensions and which window to read.
type HistoryQuery struct {
	Dimensions []string `json:"dimensions,omitempty" jsonschema:"Dimensions to read; defaults to all supported when empty"`
	Period     string   `json:"period,omitempty" jsonschema:"History window: day (default), week, month, or year"`
}

// RecordingSetting is DSM Resource Monitor's history-recording configuration.
type RecordingSetting struct {
	Enabled bool `json:"enabled" jsonschema:"Whether DSM records utilization history"`
}

// RecordingChange is a patch-only intent for the recording toggle. A nil field
// means "leave unchanged"; dsmctl re-sends observed values for any co-located
// DSM settings it does not own so they are never reset.
type RecordingChange struct {
	Enable *bool `json:"enable,omitempty" jsonschema:"Desired history-recording state; omit to leave unchanged"`
}

// Capabilities reports which Resource Monitor operations the target supports.
type Capabilities struct {
	Read          bool `json:"read" jsonschema:"Whether current utilization and history can be read"`
	RecordingRead bool `json:"recording_read" jsonschema:"Whether the history-recording setting can be read"`
	RecordingSet  bool `json:"recording_set" jsonschema:"Whether the history-recording toggle can be applied"`
}

// NormalizePeriod maps caller spellings to a canonical Period* value. An empty
// value defaults to PeriodWeek. DSM 7.x does not record a day window; callers
// wanting sub-week detail should read the real-time snapshot instead.
func NormalizePeriod(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return PeriodWeek, nil
	case "week", "weekly", "7day", "7days":
		return PeriodWeek, nil
	case "month", "monthly", "30day", "30days":
		return PeriodMonth, nil
	case "half_year", "halfyear", "half-year", "6month", "6months":
		return PeriodHalfYear, nil
	case "year", "yearly", "12month":
		return PeriodYear, nil
	case "day", "daily", "1day", "24h":
		return "", fmt.Errorf("DSM 7.x does not record a day history window; use week, month, half_year, or year, or read the real-time snapshot with 'resource-monitor current'")
	default:
		return "", fmt.Errorf("invalid history period %q: use week, month, half_year, or year", value)
	}
}

// NormalizeDimension maps caller spellings to a canonical Dimension* value.
func NormalizeDimension(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "cpu", "processor":
		return DimensionCPU, nil
	case "memory", "mem", "ram":
		return DimensionMemory, nil
	case "network", "net", "lan":
		return DimensionNetwork, nil
	case "disk", "disks":
		return DimensionDisk, nil
	case "volume", "volumes", "space":
		return DimensionVolume, nil
	default:
		return "", fmt.Errorf("invalid dimension %q: use cpu, memory, network, disk, or volume", value)
	}
}

// ParseTime converts a CLI/MCP time bound into a Unix time in seconds. It
// accepts an empty string (0, unset), a raw Unix-seconds integer, or a local
// timestamp in "2006-01-02" or "2006-01-02 15:04:05" form. It mirrors
// syslog.ParseTime so time handling is consistent across read modules.
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
