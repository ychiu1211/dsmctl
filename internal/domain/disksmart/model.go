// Package disksmart contains stable, read-only models for Storage Manager's
// per-physical-disk health and S.M.A.R.T. surface: each drive's health
// lifecycle (status, SSD remaining-life/wear, spare-block/bad-sector detail,
// spin-down capability), the full S.M.A.R.T. attribute table, the running or
// last self-test status, and the global disk-health warning thresholds.
//
// It deliberately complements — and does not duplicate — the storage inventory
// (internal/domain/storage), which carries only a coarse per-disk SMART status
// string and a temperature reading. WI-077 owns the attribute table, the
// self-test state, and the bad-sector/lifespan detail those inventory fields
// intentionally stop short of.
//
// Disk serial numbers are stable hardware identifiers: they are modeled here so
// a drive can be identified, but per the WI-002 evidence policy they must never
// enter committed fixtures or logs.
package disksmart

// DiskHealth is one physical disk's health, lifespan, and coarse self-test
// state, read from SYNO.Core.Storage.Disk.list. It carries the health-lifecycle
// detail (SSD wear, spare-block/bad-sector counters, spin-down capability) that
// the storage inventory's disk record intentionally omits.
type DiskHealth struct {
	ID             string `json:"id" jsonschema:"Stable kernel disk id such as sda; the plan/apply resource identifier"`
	Device         string `json:"device,omitempty" jsonschema:"Kernel device path such as /dev/sda"`
	Name           string `json:"name,omitempty" jsonschema:"DSM display name such as Drive 1"`
	Model          string `json:"model,omitempty" jsonschema:"Drive model string"`
	Firmware       string `json:"firmware,omitempty" jsonschema:"Drive firmware revision"`
	Vendor         string `json:"vendor,omitempty" jsonschema:"Drive vendor string"`
	Serial         string `json:"serial,omitempty" jsonschema:"Drive serial number; a stable hardware identifier that must never enter committed fixtures or logs"`
	Type           string `json:"type,omitempty" jsonschema:"Media type: SSD or HDD"`
	Interface      string `json:"interface,omitempty" jsonschema:"Bus/interface such as SATA, SAS, or NVMe"`
	Slot           string `json:"slot,omitempty" jsonschema:"Bay/slot identifier"`
	Unit           string `json:"unit,omitempty" jsonschema:"Enclosure the disk sits in, such as the main unit or an expansion unit"`
	Location       string `json:"location,omitempty" jsonschema:"Physical location label such as Main"`
	SizeBytes      uint64 `json:"size_bytes,omitempty" jsonschema:"Raw disk capacity in bytes"`
	TemperatureC   *int   `json:"temperature_c,omitempty" jsonschema:"Current disk temperature in Celsius, when reported"`
	Status         string `json:"status,omitempty" jsonschema:"Raw allocation status such as normal or not_use"`
	Health         string `json:"health,omitempty" jsonschema:"Normalized overall disk health such as normal, warning, or critical"`
	SMARTStatus    string `json:"smart_status,omitempty" jsonschema:"Coarse SMART status string reported alongside the disk list"`
	SMARTSupported bool   `json:"smart_supported" jsonschema:"Whether the disk supports SMART self-tests"`

	RemainingLifePercent        *int `json:"remaining_life_percent,omitempty" jsonschema:"Estimated SSD remaining life as a percentage, when reported"`
	RemainingLifeTrustable      bool `json:"remaining_life_trustable" jsonschema:"Whether DSM trusts the reported remaining-life estimate"`
	RemainingLifeDanger         bool `json:"remaining_life_danger" jsonschema:"Whether remaining life has crossed the danger threshold"`
	BelowRemainingLifeThreshold bool `json:"below_remaining_life_threshold" jsonschema:"Whether remaining life is below the configured warning threshold"`

	SpareBlockDaysLeft int  `json:"spare_block_days_left" jsonschema:"Estimated days until spare blocks are exhausted; 0 when not applicable"`
	SpareBlockCritical bool `json:"spare_block_critical" jsonschema:"Whether the spare-block estimate has crossed the critical threshold"`
	SpareBlockWarning  bool `json:"spare_block_warning" jsonschema:"Whether the spare-block estimate has crossed the warning threshold"`
	UncorrectableCount int  `json:"uncorrectable_count" jsonschema:"Uncorrectable sector/error count; -1 when DSM reports it as unavailable"`

	Testing         bool   `json:"testing" jsonschema:"Whether a SMART self-test is currently running on this disk"`
	TestingType     string `json:"testing_type,omitempty" jsonschema:"Kind of test in progress as DSM reports it, such as idle, quick, or extended"`
	TestingProgress string `json:"testing_progress,omitempty" jsonschema:"Free-form progress string for a running test"`
}

// HealthThresholds is the global disk-health warning configuration read from
// SYNO.Storage.CGI.HddMan.get. It is NAS-wide, not per-disk.
type HealthThresholds struct {
	BadSectorThresholdEnabled        bool `json:"bad_sector_threshold_enabled" jsonschema:"Whether the bad-sector count warning threshold is enabled"`
	RemainingLifeThresholdEnabled    bool `json:"remaining_life_threshold_enabled" jsonschema:"Whether the SSD remaining-life warning threshold is enabled"`
	RemainingLifeThresholdPercent    int  `json:"remaining_life_threshold_percent" jsonschema:"SSD remaining-life warning threshold as a percentage"`
	SpareBlockMonthsThresholdEnabled bool `json:"spare_block_months_threshold_enabled" jsonschema:"Whether the spare-block-months-left warning threshold is enabled"`
	SpareBlockMonthsThreshold        int  `json:"spare_block_months_threshold" jsonschema:"Spare-block-months-left warning threshold"`
	HealthReportEnabled              bool `json:"health_report_enabled" jsonschema:"Whether periodic disk health reports are enabled"`
	WriteDurabilityAssuranceEnabled  bool `json:"write_durability_assurance_enabled" jsonschema:"Whether Write Durability Assurance (WDDA) monitoring is enabled"`
}

// HealthState is the full per-NAS disk-health read: every installed disk plus
// the global warning thresholds when the HddMan config area is available.
type HealthState struct {
	Disks      []DiskHealth      `json:"disks" jsonschema:"Per-physical-disk health, lifespan, and coarse self-test state"`
	Thresholds *HealthThresholds `json:"thresholds,omitempty" jsonschema:"Global disk-health warning thresholds; absent when the configuration API is not exposed"`
}

// SMARTAttribute is one row of a disk's S.M.A.R.T. attribute table. DSM reports
// every field as a string (values are zero-padded and raw values can be
// composite such as 0/0), so they are preserved verbatim rather than parsed.
type SMARTAttribute struct {
	ID        string `json:"id" jsonschema:"SMART attribute id, such as 5"`
	Name      string `json:"name,omitempty" jsonschema:"SMART attribute name, such as Reallocated_Sector_Ct"`
	Current   string `json:"current,omitempty" jsonschema:"Current normalized value"`
	Worst     string `json:"worst,omitempty" jsonschema:"Worst normalized value ever recorded"`
	Threshold string `json:"threshold,omitempty" jsonschema:"Failure threshold for the normalized value"`
	Raw       string `json:"raw,omitempty" jsonschema:"Raw vendor value, preserved verbatim"`
	Status    string `json:"status,omitempty" jsonschema:"Per-attribute pass/fail status, such as OK"`
}

// SMARTTestStatus is one disk's self-test status, read from
// SYNO.Core.Storage.Disk.get_smart_test_log. LatestType is DSM's raw integer
// test-type code, preserved without interpretation.
type SMARTTestStatus struct {
	Testing          bool   `json:"testing" jsonschema:"Whether a self-test is currently running"`
	LatestResult     string `json:"latest_result,omitempty" jsonschema:"Result of the most recent self-test, such as completed"`
	LatestType       int    `json:"latest_type,omitempty" jsonschema:"DSM raw integer code for the most recent test type"`
	LatestTime       string `json:"latest_time,omitempty" jsonschema:"Timestamp of the most recent self-test, when reported"`
	Remaining        string `json:"remaining,omitempty" jsonschema:"Remaining time or percentage for a running test"`
	QuickEstimate    string `json:"quick_estimate,omitempty" jsonschema:"Estimated duration of a quick test as DSM reports it"`
	ExtendedEstimate string `json:"extended_estimate,omitempty" jsonschema:"Estimated duration of an extended test as DSM reports it"`
}

// DiskSMART is one disk's S.M.A.R.T. attribute table, health/test summary, and
// self-test status. A disk that exposes no attribute table (many enterprise
// SSDs, NVMe/SATADOM/M.2, and USB devices) is reported with NoSMARTData set and
// an empty Attributes list rather than erroring the whole read.
type DiskSMART struct {
	ID     string `json:"id" jsonschema:"Stable kernel disk id such as sda"`
	Device string `json:"device,omitempty" jsonschema:"Kernel device path such as /dev/sda"`
	Name   string `json:"name,omitempty" jsonschema:"DSM display name such as Drive 1"`
	Model  string `json:"model,omitempty" jsonschema:"Drive model string"`
	Serial string `json:"serial,omitempty" jsonschema:"Drive serial number; never emit to committed fixtures or logs"`
	Type   string `json:"type,omitempty" jsonschema:"Media type: SSD or HDD"`
	IsNVMe bool   `json:"is_nvme" jsonschema:"Whether the disk is an NVMe device"`
	IsSSD  bool   `json:"is_ssd" jsonschema:"Whether the disk is an SSD"`

	NoSMARTData bool `json:"no_smart_data" jsonschema:"True when the disk exposes no SMART attribute table; Attributes is then empty"`
	AbsenceCode int  `json:"absence_code,omitempty" jsonschema:"DSM error code returned when no SMART data is available, such as 117"`

	OverallStatus          string `json:"overall_status,omitempty" jsonschema:"Overall SMART health summary, such as normal"`
	SMARTInfoStatus        string `json:"smart_info_status,omitempty" jsonschema:"SMART info summary status, such as normal"`
	SMARTTestStatus        string `json:"smart_test_status,omitempty" jsonschema:"SMART self-test summary status, such as normal"`
	RemainingLifePercent   *int   `json:"remaining_life_percent,omitempty" jsonschema:"Estimated SSD remaining life as a percentage, when reported"`
	RemainingLifeAttribute string `json:"remaining_life_attribute,omitempty" jsonschema:"SMART attribute used to derive remaining life, such as Media_Wearout_Indicator"`

	AttributeCount int              `json:"attribute_count" jsonschema:"Number of SMART attributes reported"`
	Attributes     []SMARTAttribute `json:"attributes" jsonschema:"The SMART attribute table"`
	TestStatus     *SMARTTestStatus `json:"test_status,omitempty" jsonschema:"Current or last self-test status, when the test-log API is available"`
}

// SMARTState is the full per-NAS SMART attribute read across every installed
// disk.
type SMARTState struct {
	Disks []DiskSMART `json:"disks" jsonschema:"Per-disk SMART attribute tables, summaries, and self-test status"`
}

// Capabilities reports which disk-SMART read areas the NAS exposes. Each area
// selects its backend independently; a missing API family fails closed for its
// own area only.
type Capabilities struct {
	Health     bool `json:"health" jsonschema:"Whether per-disk health/lifespan/test-state can be read (SYNO.Core.Storage.Disk)"`
	Attributes bool `json:"attributes" jsonschema:"Whether per-disk SMART attribute tables can be read (SYNO.Storage.CGI.Smart)"`
	Thresholds bool `json:"thresholds" jsonschema:"Whether global disk-health warning thresholds can be read (SYNO.Storage.CGI.HddMan)"`
}
