// Package securityadvisor contains stable, DSM-version-independent models for
// the Control Panel → Security → Security Advisor surface: the last-scan status,
// the per-category findings with their severity breakdown, and the current scan
// schedule and security baseline. WebAPI names and field names stay behind the
// operation package.
//
// The load-heavy run-scan action (SYNO.Core.SecurityScan.Operation start) and
// the guarded schedule/baseline write (SYNO.Core.SecurityScan.Conf set) are
// live-verified writes exposed here through the plan/apply contract and an
// explicit run action.
//
// The model carries descriptive audit output only. Session identity (SIDs,
// SynoTokens) never enters these types, and no finding value is resolved back to
// a credential the finding references.
package securityadvisor

// ModuleName is the stable product-facing identifier for the module.
const ModuleName = "security-advisor"

// Baseline identifies a Security Advisor security baseline group. The read
// surface can additionally report the custom checklist as "custom"; the guarded
// write only switches between the two managed baselines, because moving to or
// editing the custom checklist is per-check remediation owned by a separate work
// item, not this settings module.
const (
	// BaselineHome is the personal/home baseline (fewer, lighter checks).
	BaselineHome = "home"
	// BaselineCompany is the business/high-security baseline.
	BaselineCompany = "company"
	// BaselineCustom is the read-only marker for a user-customized checklist.
	BaselineCustom = "custom"
)

// ScheduleChange is the patch-only intent for the guarded schedule + baseline
// write. A nil field keeps the currently configured DSM value; the apply path
// reads the complete current Conf, merges this patch, and submits the whole
// merged configuration so an unspecified field is never silently reset. There
// are no secret fields, so this carries no credential reference.
type ScheduleChange struct {
	Baseline        *string `json:"baseline,omitempty" jsonschema:"Desired security baseline: home or company; omit to keep the current baseline"`
	ScheduleEnabled *bool   `json:"schedule_enabled,omitempty" jsonschema:"Whether the scheduled scan is enabled; omit to keep the current setting"`
	Hour            *int    `json:"hour,omitempty" jsonschema:"Scheduled hour 0-23; omit to keep the current hour"`
	Minute          *int    `json:"minute,omitempty" jsonschema:"Scheduled minute 0-59; omit to keep the current minute"`
	Weekday         *string `json:"weekday,omitempty" jsonschema:"DSM weekday selector (0-6) for the scheduled scan; omit to keep the current weekday"`
}

// IsEmpty reports whether the patch carries no fields.
func (c ScheduleChange) IsEmpty() bool {
	return c.Baseline == nil && c.ScheduleEnabled == nil && c.Hour == nil && c.Minute == nil && c.Weekday == nil
}

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
// (status + findings) and SYNO.Core.SecurityScan.Conf (schedule + baseline); the
// guarded schedule/baseline write rides SYNO.Core.SecurityScan.Conf set and the
// run-scan action rides SYNO.Core.SecurityScan.Operation start. Each is reported
// from its own advertised backend and fails closed when absent.
type Capabilities struct {
	Module        string `json:"module" jsonschema:"Stable module name: security-advisor"`
	StatusRead    bool   `json:"status_read" jsonschema:"Whether the last-scan status and per-category findings can be read"`
	ScheduleRead  bool   `json:"schedule_read" jsonschema:"Whether the scan schedule and security baseline can be read"`
	RunScan       bool   `json:"run_scan" jsonschema:"Whether the run-scan action is available (SYNO.Core.SecurityScan.Operation start)"`
	ScheduleWrite bool   `json:"schedule_write" jsonschema:"Whether the guarded schedule/baseline write is available (SYNO.Core.SecurityScan.Conf set)"`
}
