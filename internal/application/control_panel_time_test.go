package application

import (
	"context"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/controlpanel"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type fakeControlPanelTimeClient struct {
	state        synology.ControlPanelTimeState
	capabilities synology.ControlPanelTimeCapabilities
	mutations    int
	persist      bool
	applyServers []string
}

func (client *fakeControlPanelTimeClient) ControlPanelTimeState(context.Context) (synology.ControlPanelTimeState, error) {
	state := client.state
	state.NTPServers = append([]string(nil), client.state.NTPServers...)
	return state, nil
}

func (client *fakeControlPanelTimeClient) ControlPanelTimeCapabilities(context.Context) (synology.ControlPanelTimeCapabilities, synology.CompatibilityReport, error) {
	return client.capabilities, synology.CompatibilityReport{}, nil
}

func (client *fakeControlPanelTimeClient) ApplyControlPanelTimeChange(_ context.Context, change synology.ControlPanelTimeChange) (synology.ControlPanelTimeMutationResult, error) {
	client.mutations++
	if change.NTPServers != nil {
		client.applyServers = append([]string(nil), *change.NTPServers...)
	}
	if client.persist {
		if change.TimeZone != nil {
			client.state.TimeZone = strings.TrimSpace(*change.TimeZone)
		}
		if change.DateFormat != nil {
			client.state.DateFormat = strings.TrimSpace(*change.DateFormat)
		}
		if change.TimeFormat != nil {
			client.state.TimeFormat = strings.TrimSpace(*change.TimeFormat)
		}
		if change.SynchronizationMode != nil {
			client.state.SynchronizationMode = *change.SynchronizationMode
		}
		if change.NTPServers != nil {
			client.state.NTPServers = append([]string(nil), *change.NTPServers...)
		}
	}
	return synology.ControlPanelTimeMutationResult{Backend: "core-region-ntp-v3", API: "SYNO.Core.Region.NTP", Version: 3, Method: "set"}, nil
}

func timeTestClient() *fakeControlPanelTimeClient {
	return &fakeControlPanelTimeClient{
		state: synology.ControlPanelTimeState{
			TimeZone:            "Taipei",
			DateFormat:          "Y-m-d",
			TimeFormat:          "H:i",
			SynchronizationMode: controlpanel.TimeSynchronizationNTP,
			NTPServers:          []string{"time.google.com", "pool.ntp.org"},
		},
		capabilities: synology.ControlPanelTimeCapabilities{Module: controlpanel.ModuleTime, Read: true, Set: true},
		persist:      true,
	}
}

func timeModePointer(value controlpanel.TimeSynchronizationMode) *controlpanel.TimeSynchronizationMode {
	return &value
}

func serversPointer(values ...string) *[]string { return &values }

func TestControlPanelTimePlanAndApplyFormatChange(t *testing.T) {
	client := timeTestClient()
	request := controlpanel.TimeChange{DateFormat: stringPointer("d/m/Y")}
	if err := validateTimeChangeShape(request); err != nil {
		t.Fatalf("validateTimeChangeShape() error = %v", err)
	}
	plan, err := planControlPanelTimeChangeWithClient(context.Background(), "office", client, request)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "medium" || len(plan.Warnings) != 0 || len(plan.Summary) != 1 {
		t.Fatalf("plan effects = %q %#v %#v", plan.Risk, plan.Warnings, plan.Summary)
	}
	if plan.ObservedFingerprint == "" || plan.Hash == "" || plan.APIVersion != controlPanelTimeAPIVersion {
		t.Fatalf("plan metadata = %#v", plan)
	}
	if err := validateControlPanelTimePlan(plan, plan.Hash); err != nil {
		t.Fatalf("validateControlPanelTimePlan() error = %v", err)
	}
	result, err := applyControlPanelTimePlanWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if !result.Applied || result.PlanHash != plan.Hash || client.mutations != 1 {
		t.Fatalf("result = %#v mutations = %d", result, client.mutations)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("format-only apply warnings = %#v", result.Warnings)
	}
	if client.state.DateFormat != "d/m/Y" {
		t.Fatalf("state after apply = %#v", client.state)
	}
}

func TestControlPanelTimeApplyRejectsStaleState(t *testing.T) {
	client := timeTestClient()
	plan, err := planControlPanelTimeChangeWithClient(context.Background(), "office", client, controlpanel.TimeChange{TimeZone: stringPointer("Berlin")})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	client.state.TimeFormat = "h:i A"
	if _, err := applyControlPanelTimePlanWithClient(context.Background(), client, plan); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("apply error = %v, want stale", err)
	}
	if client.mutations != 0 {
		t.Fatalf("mutations = %d, want 0", client.mutations)
	}
}

func TestControlPanelTimePlanHashRejectsTampering(t *testing.T) {
	client := timeTestClient()
	plan, err := planControlPanelTimeChangeWithClient(context.Background(), "office", client, controlpanel.TimeChange{TimeZone: stringPointer("Berlin")})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	tampered := plan
	tampered.Risk = "low"
	if err := validateControlPanelTimePlan(tampered, tampered.Hash); err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("validateControlPanelTimePlan() error = %v, want modified", err)
	}
}

func TestControlPanelTimeShapeValidationFailsClosed(t *testing.T) {
	tests := []struct {
		name    string
		request controlpanel.TimeChange
		want    string
	}{
		{name: "empty patch", request: controlpanel.TimeChange{}, want: "no fields"},
		{name: "manual mode", request: controlpanel.TimeChange{SynchronizationMode: timeModePointer(controlpanel.TimeSynchronizationManual)}, want: "manual time synchronization is excluded"},
		{name: "unknown mode", request: controlpanel.TimeChange{SynchronizationMode: timeModePointer("automatic")}, want: "unsupported synchronization mode"},
		{name: "empty time zone", request: controlpanel.TimeChange{TimeZone: stringPointer("  ")}, want: "must not be empty"},
		{name: "empty server list", request: controlpanel.TimeChange{NTPServers: serversPointer()}, want: "removing every NTP server"},
		{name: "too many servers", request: controlpanel.TimeChange{NTPServers: serversPointer("s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8", "s9")}, want: "at most 8"},
		{name: "duplicate servers", request: controlpanel.TimeChange{NTPServers: serversPointer("time.google.com", "TIME.google.com")}, want: "more than once"},
		{name: "comma in server", request: controlpanel.TimeChange{NTPServers: serversPointer("a,b")}, want: "not a valid IP address or DNS host name"},
		{name: "bad host label", request: controlpanel.TimeChange{NTPServers: serversPointer("-bad.example")}, want: "not a valid IP address or DNS host name"},
		{name: "bad date format", request: controlpanel.TimeChange{DateFormat: stringPointer("Y-m-d H:i")}, want: "unsupported date display format"},
		{name: "mixed separators", request: controlpanel.TimeChange{DateFormat: stringPointer("Y-m/d")}, want: "unsupported date display format"},
		{name: "repeated token", request: controlpanel.TimeChange{DateFormat: stringPointer("Y-m-m")}, want: "exactly once"},
		{name: "bad time format", request: controlpanel.TimeChange{TimeFormat: stringPointer("HH:mm")}, want: "unsupported time display format"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateTimeChangeShape(test.request); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateTimeChangeShape() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestControlPanelTimeZoneValidation(t *testing.T) {
	for _, value := range []string{"Taipei", "Berlin", "Asia/Taipei", "UTC", "New_York"} {
		if err := validateTimeZone(value, "Taipei"); err != nil {
			t.Fatalf("validateTimeZone(%q) error = %v", value, err)
		}
	}
	if err := validateTimeZone("current-config-only", "current-config-only"); err != nil {
		t.Fatalf("observed value fallback error = %v", err)
	}
	for _, value := range []string{"Nowhere_City", "../etc", "Taipei;rm"} {
		if err := validateTimeZone(value, "Taipei"); err == nil || !strings.Contains(err.Error(), "cannot be validated") {
			t.Fatalf("validateTimeZone(%q) error = %v, want rejection", value, err)
		}
	}
}

func TestControlPanelTimeManualModeRules(t *testing.T) {
	client := timeTestClient()
	client.state.SynchronizationMode = controlpanel.TimeSynchronizationManual
	client.state.NTPServers = []string{}

	if _, err := planControlPanelTimeChangeWithClient(context.Background(), "office", client, controlpanel.TimeChange{TimeZone: stringPointer("Berlin")}); err == nil || !strings.Contains(err.Error(), "synchronized manually") {
		t.Fatalf("manual-state plan error = %v", err)
	}
	if _, err := planControlPanelTimeChangeWithClient(context.Background(), "office", client, controlpanel.TimeChange{
		SynchronizationMode: timeModePointer(controlpanel.TimeSynchronizationNTP),
	}); err == nil || !strings.Contains(err.Error(), "requires ntp_servers") {
		t.Fatalf("enable-without-servers plan error = %v", err)
	}
	plan, err := planControlPanelTimeChangeWithClient(context.Background(), "office", client, controlpanel.TimeChange{
		SynchronizationMode: timeModePointer(controlpanel.TimeSynchronizationNTP),
		NTPServers:          serversPointer("time.google.com"),
	})
	if err != nil {
		t.Fatalf("enable plan error = %v", err)
	}
	if plan.Risk != "high" {
		t.Fatalf("enable plan risk = %q, want high", plan.Risk)
	}
	joined := strings.Join(plan.Warnings, "\n")
	if !strings.Contains(joined, "step the system clock") || !strings.Contains(joined, "syntax only") {
		t.Fatalf("enable plan warnings = %#v", plan.Warnings)
	}
	result, err := applyControlPanelTimePlanWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("enable apply error = %v", err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "syntax only") {
		t.Fatalf("enable apply warnings = %#v", result.Warnings)
	}
}

func TestControlPanelTimeServerRiskMatrix(t *testing.T) {
	tests := []struct {
		name        string
		servers     []string
		wantRisk    string
		wantRemoved bool
	}{
		{name: "replacement", servers: []string{"ntp.lab.example"}, wantRisk: "high", wantRemoved: true},
		{name: "removal", servers: []string{"time.google.com"}, wantRisk: "high", wantRemoved: true},
		{name: "append only", servers: []string{"time.google.com", "pool.ntp.org", "ntp.lab.example"}, wantRisk: "medium"},
		{name: "reorder only", servers: []string{"pool.ntp.org", "time.google.com"}, wantRisk: "medium"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := timeTestClient()
			plan, err := planControlPanelTimeChangeWithClient(context.Background(), "office", client, controlpanel.TimeChange{NTPServers: serversPointer(test.servers...)})
			if err != nil {
				t.Fatalf("plan error = %v", err)
			}
			if plan.Risk != test.wantRisk {
				t.Fatalf("risk = %q, want %q (warnings %#v)", plan.Risk, test.wantRisk, plan.Warnings)
			}
			joined := strings.Join(plan.Warnings, "\n")
			if !strings.Contains(joined, "syntax only") {
				t.Fatalf("warnings missing syntax-only note: %#v", plan.Warnings)
			}
			if test.wantRemoved != strings.Contains(joined, "removes NTP server") {
				t.Fatalf("warnings removal mismatch: %#v", plan.Warnings)
			}
		})
	}
}

func TestControlPanelTimeAppliesOrderedServerList(t *testing.T) {
	client := timeTestClient()
	plan, err := planControlPanelTimeChangeWithClient(context.Background(), "office", client, controlpanel.TimeChange{NTPServers: serversPointer("pool.ntp.org", "time.google.com")})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if _, err := applyControlPanelTimePlanWithClient(context.Background(), client, plan); err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if len(client.applyServers) != 2 || client.applyServers[0] != "pool.ntp.org" || client.applyServers[1] != "time.google.com" {
		t.Fatalf("applied server order = %#v", client.applyServers)
	}
}

func TestControlPanelTimeRejectsNoOpAndMissingBackend(t *testing.T) {
	client := timeTestClient()
	if _, err := planControlPanelTimeChangeWithClient(context.Background(), "office", client, controlpanel.TimeChange{TimeZone: stringPointer("Taipei")}); err == nil || !strings.Contains(err.Error(), "would not change") {
		t.Fatalf("no-op plan error = %v", err)
	}
	client.capabilities.Set = false
	if _, err := planControlPanelTimeChangeWithClient(context.Background(), "office", client, controlpanel.TimeChange{TimeZone: stringPointer("Berlin")}); err == nil || !strings.Contains(err.Error(), "verified time read/set backend") {
		t.Fatalf("missing backend plan error = %v", err)
	}
}

func TestControlPanelTimePostconditionNamesField(t *testing.T) {
	client := timeTestClient()
	client.persist = false
	plan, err := planControlPanelTimeChangeWithClient(context.Background(), "office", client, controlpanel.TimeChange{TimeZone: stringPointer("Berlin")})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	_, err = applyControlPanelTimePlanWithClient(context.Background(), client, plan)
	if err == nil || !strings.Contains(err.Error(), "verify time change") || !strings.Contains(err.Error(), "time zone") {
		t.Fatalf("apply error = %v, want named postcondition failure", err)
	}
}
