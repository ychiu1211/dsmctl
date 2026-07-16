package application

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	// The embedded IANA database keeps time-zone validation deterministic on
	// hosts without a system zoneinfo directory, such as Windows.
	_ "time/tzdata"

	"github.com/ychiu1211/dsmctl/internal/domain/controlpanel"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const controlPanelTimeAPIVersion = "dsmctl.io/v1alpha1"

const maximumNTPServers = 8

const ntpSyntaxOnlyWarning = "NTP servers were validated for syntax only; dsmctl does not verify reachability or synchronization convergence"

var timeZonePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+\-]{0,63}(/[A-Za-z0-9_+\-]{1,64}){0,2}$`)
var dateFormatPattern = regexp.MustCompile(`^[Ymd]([./-])[Ymd]([./-])[Ymd]$`)
var timeFormatPattern = regexp.MustCompile(`^[Hh]:i( [Aa])?$`)
var ntpServerLabelPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

// ianaTimeZoneAreas lets DSM's bare-city vocabulary, such as "Taipei", be
// resolved against the embedded IANA database as "Asia/Taipei".
var ianaTimeZoneAreas = []string{
	"Africa", "America", "America/Argentina", "America/Indiana",
	"America/Kentucky", "America/North_Dakota", "Antarctica", "Arctic",
	"Asia", "Atlantic", "Australia", "Europe", "Indian", "Pacific", "Etc",
}

// ControlPanelTimePlan binds a validated patch-only time intent to the
// complete module state observed while planning. Time changes cannot destroy
// data, so unlike resource plans there is no destructive flag; the risk label
// and warnings carry the operational consequences.
type ControlPanelTimePlan struct {
	APIVersion          string                         `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                         `json:"nas" jsonschema:"NAS profile selected during planning"`
	Request             controlpanel.TimeChange        `json:"request" jsonschema:"Validated patch-only time module intent"`
	Observed            synology.ControlPanelTimeState `json:"observed" jsonschema:"Complete time module state observed during planning"`
	ObservedFingerprint string                         `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed module state"`
	Risk                string                         `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                       `json:"warnings" jsonschema:"Clock, time zone, and NTP disruption warnings"`
	Summary             []string                       `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                         `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

type ControlPanelTimeApplyResult struct {
	NAS       string                                  `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash  string                                  `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied   bool                                    `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Operation synology.ControlPanelTimeMutationResult `json:"operation" jsonschema:"Selected DSM mutation backend"`
	Warnings  []string                                `json:"warnings" jsonschema:"Post-apply caveats; a verified configuration does not imply NTP reachability"`
}

type controlPanelTimeClient interface {
	ControlPanelTimeState(context.Context) (synology.ControlPanelTimeState, error)
	ControlPanelTimeCapabilities(context.Context) (synology.ControlPanelTimeCapabilities, synology.CompatibilityReport, error)
	ApplyControlPanelTimeChange(context.Context, synology.ControlPanelTimeChange) (synology.ControlPanelTimeMutationResult, error)
}

func (s *Service) PlanControlPanelTimeChange(ctx context.Context, requestedNAS string, request controlpanel.TimeChange) (ControlPanelTimePlan, error) {
	if err := validateTimeChangeShape(request); err != nil {
		return ControlPanelTimePlan{}, err
	}
	name, client, err := s.controlPanelTimeClient(ctx, requestedNAS)
	if err != nil {
		return ControlPanelTimePlan{}, err
	}
	return planControlPanelTimeChangeWithClient(ctx, name, client, request)
}

func (s *Service) ApplyControlPanelTimePlan(ctx context.Context, plan ControlPanelTimePlan, approvalHash string) (ControlPanelTimeApplyResult, error) {
	if err := validateControlPanelTimePlan(plan, approvalHash); err != nil {
		return ControlPanelTimeApplyResult{}, err
	}
	name, client, err := s.controlPanelTimeClient(ctx, plan.NAS)
	if err != nil {
		return ControlPanelTimeApplyResult{}, err
	}
	if name != plan.NAS {
		return ControlPanelTimeApplyResult{}, fmt.Errorf("time plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyControlPanelTimePlanWithClient(ctx, client, plan)
}

func (s *Service) controlPanelTimeClient(ctx context.Context, requestedNAS string) (string, controlPanelTimeClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(controlPanelTimeClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement Control Panel time management")
	}
	return name, client, nil
}

func planControlPanelTimeChangeWithClient(ctx context.Context, nas string, client controlPanelTimeClient, request controlpanel.TimeChange) (ControlPanelTimePlan, error) {
	capabilities, _, err := client.ControlPanelTimeCapabilities(ctx)
	if err != nil {
		return ControlPanelTimePlan{}, authenticationError(nas, err)
	}
	if !capabilities.Read || !capabilities.Set {
		return ControlPanelTimePlan{}, fmt.Errorf("NAS %q does not expose a verified time read/set backend", nas)
	}
	state, err := client.ControlPanelTimeState(ctx)
	if err != nil {
		return ControlPanelTimePlan{}, authenticationError(nas, err)
	}
	if err := validateTimeChangeAgainstState(state, request); err != nil {
		return ControlPanelTimePlan{}, err
	}
	plan := ControlPanelTimePlan{APIVersion: controlPanelTimeAPIVersion, NAS: nas, Request: request, Observed: state}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return ControlPanelTimePlan{}, err
	}
	plan.Risk, plan.Warnings, plan.Summary = timePlanEffects(state, request)
	plan.Hash, err = controlPanelTimePlanHash(plan)
	if err != nil {
		return ControlPanelTimePlan{}, err
	}
	return plan, nil
}

func applyControlPanelTimePlanWithClient(ctx context.Context, client controlPanelTimeClient, plan ControlPanelTimePlan) (ControlPanelTimeApplyResult, error) {
	current, err := planControlPanelTimeChangeWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return ControlPanelTimeApplyResult{}, fmt.Errorf("time plan precondition no longer holds: %w", err)
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return ControlPanelTimeApplyResult{}, fmt.Errorf("time plan is stale; create a new plan")
	}
	operation, err := client.ApplyControlPanelTimeChange(ctx, plan.Request)
	if err != nil {
		return ControlPanelTimeApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifyControlPanelTimePostcondition(ctx, client, plan.Request); err != nil {
		return ControlPanelTimeApplyResult{}, fmt.Errorf("verify time change: %w", err)
	}
	return ControlPanelTimeApplyResult{
		NAS:       plan.NAS,
		PlanHash:  plan.Hash,
		Applied:   true,
		Operation: operation,
		Warnings:  timeApplyWarnings(plan.Request),
	}, nil
}

// validateTimeChangeShape rejects everything that is invalid regardless of
// the NAS's current configuration.
func validateTimeChangeShape(change controlpanel.TimeChange) error {
	if change.TimeZone == nil && change.DateFormat == nil && change.TimeFormat == nil && change.SynchronizationMode == nil && change.NTPServers == nil {
		return fmt.Errorf("time patch has no fields")
	}
	if change.TimeZone != nil && strings.TrimSpace(*change.TimeZone) == "" {
		return fmt.Errorf("time_zone must not be empty")
	}
	if change.SynchronizationMode != nil && *change.SynchronizationMode != controlpanel.TimeSynchronizationNTP {
		if *change.SynchronizationMode == controlpanel.TimeSynchronizationManual {
			return fmt.Errorf("switching to manual time synchronization is excluded from this contract because it requires wall-clock ownership; change it in DSM directly")
		}
		return fmt.Errorf("unsupported synchronization mode %q; only %q is accepted", *change.SynchronizationMode, controlpanel.TimeSynchronizationNTP)
	}
	if change.DateFormat != nil {
		if err := validateDateFormat(strings.TrimSpace(*change.DateFormat)); err != nil {
			return err
		}
	}
	if change.TimeFormat != nil {
		if err := validateTimeFormat(strings.TrimSpace(*change.TimeFormat)); err != nil {
			return err
		}
	}
	if change.NTPServers != nil {
		servers := *change.NTPServers
		if len(servers) == 0 {
			return fmt.Errorf("removing every NTP server would disable synchronization; provide at least one server, dsmctl never infers a replacement")
		}
		if len(servers) > maximumNTPServers {
			return fmt.Errorf("at most %d NTP servers are supported", maximumNTPServers)
		}
		seen := make(map[string]struct{}, len(servers))
		for _, server := range servers {
			trimmed := strings.TrimSpace(server)
			if err := validateNTPServer(trimmed); err != nil {
				return err
			}
			key := strings.ToLower(trimmed)
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("NTP server %q is listed more than once", trimmed)
			}
			seen[key] = struct{}{}
		}
	}
	return nil
}

// validateTimeChangeAgainstState enforces the rules that depend on the
// freshly observed module state.
func validateTimeChangeAgainstState(state synology.ControlPanelTimeState, change controlpanel.TimeChange) error {
	if change.TimeZone != nil {
		if err := validateTimeZone(strings.TrimSpace(*change.TimeZone), state.TimeZone); err != nil {
			return err
		}
	}
	effectiveMode := state.SynchronizationMode
	if change.SynchronizationMode != nil {
		effectiveMode = *change.SynchronizationMode
	}
	if effectiveMode != controlpanel.TimeSynchronizationNTP {
		if change.NTPServers != nil {
			return fmt.Errorf("setting ntp_servers requires synchronization_mode %q", controlpanel.TimeSynchronizationNTP)
		}
		return fmt.Errorf("NAS time is synchronized manually; include synchronization_mode %q in the same patch to enable NTP first, dsmctl never writes wall-clock time", controlpanel.TimeSynchronizationNTP)
	}
	effectiveServers := state.NTPServers
	if change.NTPServers != nil {
		effectiveServers = *change.NTPServers
	}
	if len(effectiveServers) == 0 {
		return fmt.Errorf("enabling NTP synchronization requires ntp_servers because the NAS has no configured server")
	}
	if timeChangeSatisfied(state, change) {
		return fmt.Errorf("time patch would not change the current state")
	}
	return nil
}

// validateTimeZone fails closed: a zone is accepted only when it matches the
// NAS's current configuration or resolves in the embedded IANA database,
// including DSM's bare-city vocabulary. DSM stays the authority on meaning;
// the postcondition verifies what DSM actually persisted.
func validateTimeZone(value, observed string) error {
	if value == observed {
		return nil
	}
	if timeZonePattern.MatchString(value) {
		if _, err := time.LoadLocation(value); err == nil {
			return nil
		}
		if !strings.Contains(value, "/") {
			for _, area := range ianaTimeZoneAreas {
				if _, err := time.LoadLocation(area + "/" + value); err == nil {
					return nil
				}
			}
		}
	}
	return fmt.Errorf("time zone %q cannot be validated against the embedded IANA time zone database or the current NAS configuration", value)
}

// validateDateFormat accepts DSM's PHP-style token grammar: Y, m, and d each
// exactly once with one consistent separator, such as Y-m-d or d/m/Y.
func validateDateFormat(value string) error {
	matches := dateFormatPattern.FindStringSubmatch(value)
	if matches == nil || matches[1] != matches[2] {
		return fmt.Errorf("unsupported date display format %q; expected DSM tokens such as Y-m-d or d/m/Y", value)
	}
	if !strings.Contains(value, "Y") || !strings.Contains(value, "m") || !strings.Contains(value, "d") {
		return fmt.Errorf("date display format %q must contain each of Y, m, and d exactly once", value)
	}
	return nil
}

func validateTimeFormat(value string) error {
	if !timeFormatPattern.MatchString(value) {
		return fmt.Errorf("unsupported time display format %q; expected DSM tokens such as H:i or h:i A", value)
	}
	return nil
}

// validateNTPServer checks syntax only: an IP address or an RFC 1123 host
// name. It never claims the server is reachable.
func validateNTPServer(value string) error {
	if value == "" {
		return fmt.Errorf("NTP server entries must not be empty")
	}
	if len(value) > 253 {
		return fmt.Errorf("NTP server %q exceeds 253 characters", value)
	}
	if net.ParseIP(value) != nil {
		return nil
	}
	for _, label := range strings.Split(value, ".") {
		if !ntpServerLabelPattern.MatchString(label) {
			return fmt.Errorf("NTP server %q is not a valid IP address or DNS host name", value)
		}
	}
	return nil
}

func timePlanEffects(state synology.ControlPanelTimeState, change controlpanel.TimeChange) (string, []string, []string) {
	warnings := []string{}
	summary := []string{}
	high := false
	if change.TimeZone != nil && strings.TrimSpace(*change.TimeZone) != state.TimeZone {
		summary = append(summary, fmt.Sprintf("change the time zone from %q to %q", state.TimeZone, strings.TrimSpace(*change.TimeZone)))
		warnings = append(warnings, "changing the time zone shifts local timestamps for schedules, logs, snapshots, and retention windows")
		high = true
	}
	if change.DateFormat != nil && strings.TrimSpace(*change.DateFormat) != state.DateFormat {
		summary = append(summary, fmt.Sprintf("set the date display format to %q", strings.TrimSpace(*change.DateFormat)))
	}
	if change.TimeFormat != nil && strings.TrimSpace(*change.TimeFormat) != state.TimeFormat {
		summary = append(summary, fmt.Sprintf("set the time display format to %q", strings.TrimSpace(*change.TimeFormat)))
	}
	enablesNTP := change.SynchronizationMode != nil && state.SynchronizationMode != controlpanel.TimeSynchronizationNTP
	if enablesNTP {
		summary = append(summary, "enable NTP time synchronization")
		warnings = append(warnings, "enabling NTP may immediately step the system clock to the synchronized time")
		high = true
	}
	if change.NTPServers != nil {
		desired := trimmedServers(*change.NTPServers)
		summary = append(summary, fmt.Sprintf("replace the ordered NTP server list with %s", strings.Join(desired, ", ")))
		if removed := missingServers(state.NTPServers, desired); len(removed) > 0 {
			warnings = append(warnings, fmt.Sprintf("removes NTP server(s) %s; a wrong replacement server means silent loss of synchronization", strings.Join(removed, ", ")))
			high = true
		}
	}
	if change.NTPServers != nil || enablesNTP {
		warnings = append(warnings, ntpSyntaxOnlyWarning)
	}
	risk := "medium"
	if high {
		risk = "high"
	}
	return risk, warnings, summary
}

func timeApplyWarnings(change controlpanel.TimeChange) []string {
	if change.NTPServers != nil || change.SynchronizationMode != nil {
		return []string{ntpSyntaxOnlyWarning}
	}
	return []string{}
}

func verifyControlPanelTimePostcondition(ctx context.Context, client controlPanelTimeClient, change controlpanel.TimeChange) error {
	state, err := client.ControlPanelTimeState(ctx)
	if err != nil {
		return err
	}
	if change.TimeZone != nil && state.TimeZone != strings.TrimSpace(*change.TimeZone) {
		return fmt.Errorf("time zone is %q, want %q", state.TimeZone, strings.TrimSpace(*change.TimeZone))
	}
	if change.DateFormat != nil && state.DateFormat != strings.TrimSpace(*change.DateFormat) {
		return fmt.Errorf("date display format is %q, want %q", state.DateFormat, strings.TrimSpace(*change.DateFormat))
	}
	if change.TimeFormat != nil && state.TimeFormat != strings.TrimSpace(*change.TimeFormat) {
		return fmt.Errorf("time display format is %q, want %q", state.TimeFormat, strings.TrimSpace(*change.TimeFormat))
	}
	if change.SynchronizationMode != nil && state.SynchronizationMode != *change.SynchronizationMode {
		return fmt.Errorf("synchronization mode is %q, want %q", state.SynchronizationMode, *change.SynchronizationMode)
	}
	if change.NTPServers != nil && !equalOrderedServers(state.NTPServers, trimmedServers(*change.NTPServers)) {
		return fmt.Errorf("NTP server list is [%s], want [%s]", strings.Join(state.NTPServers, ", "), strings.Join(trimmedServers(*change.NTPServers), ", "))
	}
	return nil
}

func validateControlPanelTimePlan(plan ControlPanelTimePlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the time plan")
	}
	if plan.APIVersion != controlPanelTimeAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid time plan metadata")
	}
	if err := validateTimeChangeShape(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("time plan observed state was modified")
	}
	expectedHash, err := controlPanelTimePlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("time plan contents were modified after planning")
	}
	return nil
}

func controlPanelTimePlanHash(plan ControlPanelTimePlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

// timeChangeSatisfied reports whether the observed state already fulfills
// every requested field, which makes the patch a no-op.
func timeChangeSatisfied(state synology.ControlPanelTimeState, change controlpanel.TimeChange) bool {
	return (change.TimeZone == nil || state.TimeZone == strings.TrimSpace(*change.TimeZone)) &&
		(change.DateFormat == nil || state.DateFormat == strings.TrimSpace(*change.DateFormat)) &&
		(change.TimeFormat == nil || state.TimeFormat == strings.TrimSpace(*change.TimeFormat)) &&
		(change.SynchronizationMode == nil || state.SynchronizationMode == *change.SynchronizationMode) &&
		(change.NTPServers == nil || equalOrderedServers(state.NTPServers, trimmedServers(*change.NTPServers)))
}

func trimmedServers(servers []string) []string {
	trimmed := make([]string, 0, len(servers))
	for _, server := range servers {
		trimmed = append(trimmed, strings.TrimSpace(server))
	}
	return trimmed
}

func equalOrderedServers(current, desired []string) bool {
	if len(current) != len(desired) {
		return false
	}
	for index := range current {
		if current[index] != desired[index] {
			return false
		}
	}
	return true
}

func missingServers(current, desired []string) []string {
	kept := make(map[string]struct{}, len(desired))
	for _, server := range desired {
		kept[strings.ToLower(server)] = struct{}{}
	}
	removed := []string{}
	for _, server := range current {
		if _, ok := kept[strings.ToLower(server)]; !ok {
			removed = append(removed, server)
		}
	}
	return removed
}

var _ controlPanelTimeClient = (*synology.Client)(nil)
