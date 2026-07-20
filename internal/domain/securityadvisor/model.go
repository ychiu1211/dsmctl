// Package securityadvisor contains stable, DSM-version-independent models for
// the Control Panel → Security → Security Advisor surface: the last-scan status,
// the per-category findings with their severity breakdown, and the current scan
// schedule and security baseline. WebAPI names and field names stay behind the
// operation package.
//
// This is the read-only slice (WI-068 Slice A). The load-heavy run-scan action
// (SYNO.Core.SecurityScan.Operation) and the guarded schedule/baseline write
// (SYNO.Core.SecurityScan.Conf set) are deferred to later, explicitly-authorized
// slices and are only reported as capabilities here, never executed.
//
// The model carries descriptive audit output only. Session identity (SIDs,
// SynoTokens) never enters these types, and no finding value is resolved back to
// a credential the finding references.
package securityadvisor

// ModuleName is the stable product-facing identifier for the module.
const ModuleName = "security-advisor"

// Severity is the normalized Security Advisor finding severity. DSM 7.3 reports
// the raw values safe/info/warning/outOfDate/risk/danger; the decoder maps them
// to this stable enum and rejects any value it does not recognize rather than
// silently coercing it.
type Severity string

const (
	// SeveritySafe means the check passed (no finding).
	SeveritySafe Severity = "safe"
	// SeverityInfo is an informational finding.
	SeverityInfo Severity = "info"
	// SeverityOutOfDate flags an out-of-date component (chiefly DSM/package updates).
	SeverityOutOfDate Severity = "out_of_date"
	// SeverityWarning is a warning-level finding.
	SeverityWarning Severity = "warning"
	// SeverityRisk is a risk-level finding.
	SeverityRisk Severity = "risk"
	// SeverityDanger is the most severe finding level.
	SeverityDanger Severity = "danger"
)

// Rank returns a best-effort ordering for display only (higher is more severe).
// DSM supplies the overall and per-category severity directly, so this ordering
// is never used to compute posture — only to sort findings for presentation.
func (s Severity) Rank() int {
	switch s {
	case SeverityDanger:
		return 5
	case SeverityRisk:
		return 4
	case SeverityWarning:
		return 3
	case SeverityOutOfDate:
		return 2
	case SeverityInfo:
		return 1
	case SeveritySafe:
		return 0
	default:
		return -1
	}
}

// SeverityCounts is the per-severity finding count within a category (the DSM
// "fail" object). Passing checks are not counted here; they are total minus the
// sum of these counts.
type SeverityCounts struct {
	Danger    int `json:"danger" jsonschema:"Number of danger-level findings"`
	Risk      int `json:"risk" jsonschema:"Number of risk-level findings"`
	Warning   int `json:"warning" jsonschema:"Number of warning-level findings"`
	OutOfDate int `json:"out_of_date" jsonschema:"Number of out-of-date findings"`
	Info      int `json:"info" jsonschema:"Number of informational findings"`
}

// Total is the number of findings across every severity in the breakdown.
func (c SeverityCounts) Total() int {
	return c.Danger + c.Risk + c.Warning + c.OutOfDate + c.Info
}

// CategoryResult is the scan outcome for one Security Advisor check category
// (malware, network, systemCheck, update, userInfo, …). DSM aggregates results
// at this granularity; the read API does not enumerate individual check titles.
type CategoryResult struct {
	Category     string         `json:"category" jsonschema:"Stable DSM category key, for example network or userInfo"`
	Total        int            `json:"total" jsonschema:"Total number of checks in this category"`
	Findings     int            `json:"findings" jsonschema:"Number of checks that produced a finding (non-safe)"`
	Passed       int            `json:"passed" jsonschema:"Number of checks that passed (total minus findings)"`
	FailSeverity Severity       `json:"fail_severity" jsonschema:"Highest finding severity in this category, or safe when none"`
	Counts       SeverityCounts `json:"counts" jsonschema:"Per-severity finding count breakdown"`
	Progress     int            `json:"progress" jsonschema:"Scan progress for this category (0-100)"`
	RunningItem  string         `json:"running_item,omitempty" jsonschema:"The check currently running in this category, when a scan is in progress"`
	WaitNum      int            `json:"wait_num" jsonschema:"Number of checks in this category still waiting to run"`
}

// Status is the normalized last-scan status plus the per-category findings. Both
// come from the same SYNO.Core.SecurityScan.Status read; the volatile per-poll
// fields (progress, running_item) are surfaced here rather than in the config
// state model.
type Status struct {
	Running         bool             `json:"running" jsonschema:"Whether a scan is currently in progress"`
	Progress        int              `json:"progress" jsonschema:"Overall scan progress (0-100)"`
	OverallSeverity Severity         `json:"overall_severity" jsonschema:"Highest severity across all categories from the last scan"`
	LastScanTime    int64            `json:"last_scan_time,omitempty" jsonschema:"Last scan completion time as a Unix timestamp in seconds, when known"`
	StartTime       string           `json:"start_time,omitempty" jsonschema:"Raw start time DSM reports for an in-progress scan"`
	TotalChecks     int              `json:"total_checks" jsonschema:"Total number of checks across all categories"`
	TotalFindings   int              `json:"total_findings" jsonschema:"Total number of findings across all categories"`
	Counts          SeverityCounts   `json:"counts" jsonschema:"Per-severity finding count aggregated across all categories"`
	Categories      []CategoryResult `json:"categories" jsonschema:"Per-category scan results, sorted by descending severity then name"`
}

// Schedule is the scheduled-scan configuration.
type Schedule struct {
	Enabled bool   `json:"enabled" jsonschema:"Whether a scheduled scan is enabled"`
	Hour    int    `json:"hour" jsonschema:"Scheduled hour (0-23)"`
	Minute  int    `json:"minute" jsonschema:"Scheduled minute (0-59)"`
	Weekday string `json:"weekday,omitempty" jsonschema:"DSM weekday selector for the scheduled scan, as reported (for example 4)"`
	TaskID  int    `json:"task_id,omitempty" jsonschema:"DSM task scheduler id backing the scheduled scan"`
}

// Configuration is the current scan schedule and security baseline.
type Configuration struct {
	Baseline string   `json:"baseline" jsonschema:"Active security baseline group, for example home or company"`
	Schedule Schedule `json:"schedule" jsonschema:"Scheduled-scan configuration"`
}

// Capabilities reports which Security Advisor operations dsmctl currently
// exposes for the selected NAS. Reads come from SYNO.Core.SecurityScan.Status
// (status + findings) and SYNO.Core.SecurityScan.Conf (schedule + baseline). The
// run-scan action and the schedule/baseline write are deferred slices; their
// availability is reported from the advertised APIs but they are not executed by
// this module.
type Capabilities struct {
	Module        string `json:"module" jsonschema:"Stable module name: security-advisor"`
	StatusRead    bool   `json:"status_read" jsonschema:"Whether the last-scan status and per-category findings can be read"`
	ScheduleRead  bool   `json:"schedule_read" jsonschema:"Whether the scan schedule and security baseline can be read"`
	RunScan       bool   `json:"run_scan" jsonschema:"Whether the run-scan action API is advertised (deferred: not executed by this slice)"`
	ScheduleWrite bool   `json:"schedule_write" jsonschema:"Whether the schedule/baseline write API is advertised (deferred: not executed by this slice)"`
}
