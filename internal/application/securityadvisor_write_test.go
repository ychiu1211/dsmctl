package application

import (
	"context"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/securityadvisor"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type fakeSecurityAdvisorClient struct {
	configuration synology.SecurityAdvisorConfiguration
	capabilities  synology.SecurityAdvisorCapabilities
	mutations     int
	scans         int
	persist       bool
	scanErr       error
}

func (c *fakeSecurityAdvisorClient) SecurityAdvisorStatus(context.Context) (synology.SecurityAdvisorStatus, error) {
	return synology.SecurityAdvisorStatus{}, nil
}

func (c *fakeSecurityAdvisorClient) SecurityAdvisorConfiguration(context.Context) (synology.SecurityAdvisorConfiguration, error) {
	return c.configuration, nil
}

func (c *fakeSecurityAdvisorClient) SecurityAdvisorCapabilities(context.Context) (synology.SecurityAdvisorCapabilities, synology.CompatibilityReport, error) {
	return c.capabilities, synology.CompatibilityReport{}, nil
}

func (c *fakeSecurityAdvisorClient) ApplySecurityAdvisorScheduleChange(_ context.Context, change synology.SecurityAdvisorScheduleChange) (synology.SecurityAdvisorMutationResult, error) {
	c.mutations++
	if c.persist {
		if change.Baseline != nil {
			c.configuration.Baseline = *change.Baseline
		}
		if change.ScheduleEnabled != nil {
			c.configuration.Schedule.Enabled = *change.ScheduleEnabled
		}
		if change.Hour != nil {
			c.configuration.Schedule.Hour = *change.Hour
		}
		if change.Minute != nil {
			c.configuration.Schedule.Minute = *change.Minute
		}
		if change.Weekday != nil {
			c.configuration.Schedule.Weekday = *change.Weekday
		}
	}
	return synology.SecurityAdvisorMutationResult{Backend: "securityscan-conf-set-v1", API: "SYNO.Core.SecurityScan.Conf", Version: 1, Method: "set"}, nil
}

func (c *fakeSecurityAdvisorClient) RunSecurityScan(context.Context) (synology.SecurityAdvisorScanResult, error) {
	if c.scanErr != nil {
		return synology.SecurityAdvisorScanResult{}, c.scanErr
	}
	c.scans++
	return synology.SecurityAdvisorScanResult{Backend: "securityscan-operation-start-v1", API: "SYNO.Core.SecurityScan.Operation", Version: 1, Method: "start", Started: true}, nil
}

func securityAdvisorTestClient() *fakeSecurityAdvisorClient {
	return &fakeSecurityAdvisorClient{
		configuration: newSAConfig(),
		capabilities: synology.SecurityAdvisorCapabilities{
			Module: securityadvisor.ModuleName, StatusRead: true, ScheduleRead: true, RunScan: true, ScheduleWrite: true,
		},
		persist: true,
	}
}

func newSAConfig() synology.SecurityAdvisorConfiguration {
	cfg := synology.SecurityAdvisorConfiguration{Baseline: securityadvisor.BaselineCompany}
	cfg.Schedule.Enabled = true
	cfg.Schedule.Hour = 2
	cfg.Schedule.Minute = 21
	cfg.Schedule.Weekday = "4"
	cfg.Schedule.TaskID = 2
	return cfg
}

func TestSecurityAdvisorPlanApplyMinuteChange(t *testing.T) {
	client := securityAdvisorTestClient()
	client.configuration = newSAConfig()
	request := securityadvisor.ScheduleChange{Minute: intPtr(30)}
	plan, err := planSecurityAdvisorScheduleWithClient(context.Background(), "lab", client, request)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "medium" || len(plan.Warnings) != 0 || len(plan.Summary) != 1 {
		t.Fatalf("plan effects = %q warnings=%#v summary=%#v", plan.Risk, plan.Warnings, plan.Summary)
	}
	if plan.ObservedFingerprint == "" || plan.Hash == "" || plan.APIVersion != securityAdvisorAPIVersion {
		t.Fatalf("plan metadata = %#v", plan)
	}
	if err := validateSecurityAdvisorSchedulePlan(plan, plan.Hash); err != nil {
		t.Fatalf("validate plan error = %v", err)
	}
	result, err := applySecurityAdvisorScheduleWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if !result.Applied || result.PlanHash != plan.Hash || client.mutations != 1 {
		t.Fatalf("result = %#v mutations = %d", result, client.mutations)
	}
	if client.configuration.Schedule.Minute != 30 {
		t.Fatalf("state after apply = %#v", client.configuration)
	}
}

func TestSecurityAdvisorLooseningIsHighRisk(t *testing.T) {
	t.Run("baseline company to home", func(t *testing.T) {
		client := securityAdvisorTestClient()
		client.configuration = newSAConfig()
		home := securityadvisor.BaselineHome
		plan, err := planSecurityAdvisorScheduleWithClient(context.Background(), "lab", client, securityadvisor.ScheduleChange{Baseline: &home})
		if err != nil {
			t.Fatalf("plan error = %v", err)
		}
		if plan.Risk != "high" {
			t.Fatalf("risk = %q, want high", plan.Risk)
		}
		if !strings.Contains(strings.Join(plan.Warnings, "\n"), "business baseline to the home baseline") {
			t.Fatalf("warnings = %#v", plan.Warnings)
		}
	})
	t.Run("disable schedule", func(t *testing.T) {
		client := securityAdvisorTestClient()
		client.configuration = newSAConfig()
		plan, err := planSecurityAdvisorScheduleWithClient(context.Background(), "lab", client, securityadvisor.ScheduleChange{ScheduleEnabled: boolPtr(false)})
		if err != nil {
			t.Fatalf("plan error = %v", err)
		}
		if plan.Risk != "high" {
			t.Fatalf("risk = %q, want high", plan.Risk)
		}
		if !strings.Contains(strings.Join(plan.Warnings, "\n"), "stops the NAS from being audited") {
			t.Fatalf("warnings = %#v", plan.Warnings)
		}
	})
}

func TestSecurityAdvisorTighteningIsMedium(t *testing.T) {
	client := securityAdvisorTestClient()
	cfg := newSAConfig()
	cfg.Baseline = securityadvisor.BaselineHome
	client.configuration = cfg
	company := securityadvisor.BaselineCompany
	plan, err := planSecurityAdvisorScheduleWithClient(context.Background(), "lab", client, securityadvisor.ScheduleChange{Baseline: &company})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "medium" || len(plan.Warnings) != 0 {
		t.Fatalf("tightening risk = %q warnings = %#v", plan.Risk, plan.Warnings)
	}
}

func TestSecurityAdvisorRejectsNoOpAndCustomBaseline(t *testing.T) {
	client := securityAdvisorTestClient()
	client.configuration = newSAConfig()
	if _, err := planSecurityAdvisorScheduleWithClient(context.Background(), "lab", client, securityadvisor.ScheduleChange{Minute: intPtr(21)}); err == nil || !strings.Contains(err.Error(), "would not change") {
		t.Fatalf("no-op plan error = %v", err)
	}
	custom := newSAConfig()
	custom.Baseline = securityadvisor.BaselineCustom
	client.configuration = custom
	if _, err := planSecurityAdvisorScheduleWithClient(context.Background(), "lab", client, securityadvisor.ScheduleChange{Minute: intPtr(30)}); err == nil || !strings.Contains(err.Error(), "custom checklist") {
		t.Fatalf("custom-baseline plan error = %v", err)
	}
}

func TestSecurityAdvisorApplyRejectsStaleState(t *testing.T) {
	client := securityAdvisorTestClient()
	client.configuration = newSAConfig()
	plan, err := planSecurityAdvisorScheduleWithClient(context.Background(), "lab", client, securityadvisor.ScheduleChange{Minute: intPtr(30)})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	client.configuration.Schedule.Hour = 5
	if _, err := applySecurityAdvisorScheduleWithClient(context.Background(), client, plan); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("apply error = %v, want stale", err)
	}
	if client.mutations != 0 {
		t.Fatalf("mutations = %d, want 0", client.mutations)
	}
}

func TestSecurityAdvisorPlanHashRejectsTampering(t *testing.T) {
	client := securityAdvisorTestClient()
	client.configuration = newSAConfig()
	plan, err := planSecurityAdvisorScheduleWithClient(context.Background(), "lab", client, securityadvisor.ScheduleChange{Minute: intPtr(30)})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	tampered := plan
	tampered.Risk = "low"
	if err := validateSecurityAdvisorSchedulePlan(tampered, tampered.Hash); err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("validate error = %v, want modified", err)
	}
}

func TestSecurityAdvisorPostconditionNamesField(t *testing.T) {
	client := securityAdvisorTestClient()
	client.configuration = newSAConfig()
	client.persist = false
	plan, err := planSecurityAdvisorScheduleWithClient(context.Background(), "lab", client, securityadvisor.ScheduleChange{Minute: intPtr(30)})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	_, err = applySecurityAdvisorScheduleWithClient(context.Background(), client, plan)
	if err == nil || !strings.Contains(err.Error(), "verify security advisor change") || !strings.Contains(err.Error(), "minute") {
		t.Fatalf("apply error = %v, want named postcondition failure", err)
	}
}

func TestSecurityAdvisorShapeValidationFailsClosed(t *testing.T) {
	home := securityadvisor.BaselineHome
	custom := securityadvisor.BaselineCustom
	unknown := "enterprise"
	tests := []struct {
		name    string
		request securityadvisor.ScheduleChange
		want    string
	}{
		{name: "empty patch", request: securityadvisor.ScheduleChange{}, want: "no fields"},
		{name: "custom baseline", request: securityadvisor.ScheduleChange{Baseline: &custom}, want: "custom checklist"},
		{name: "unknown baseline", request: securityadvisor.ScheduleChange{Baseline: &unknown}, want: "unsupported baseline"},
		{name: "hour range", request: securityadvisor.ScheduleChange{Hour: intPtr(24)}, want: "out of range"},
		{name: "minute range", request: securityadvisor.ScheduleChange{Minute: intPtr(-1)}, want: "out of range"},
		{name: "weekday bad", request: securityadvisor.ScheduleChange{Weekday: stringPointer("sunday")}, want: "weekday"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateSecurityAdvisorScheduleShape(test.request); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validate error = %v, want containing %q", err, test.want)
			}
		})
	}
	// A valid home baseline passes shape validation.
	if err := validateSecurityAdvisorScheduleShape(securityadvisor.ScheduleChange{Baseline: &home}); err != nil {
		t.Fatalf("valid home baseline rejected: %v", err)
	}
}

func TestSecurityAdvisorRunScanActionGatesOnCapability(t *testing.T) {
	client := securityAdvisorTestClient()
	result, err := runSecurityScanWithClient(context.Background(), "lab", client)
	if err != nil {
		t.Fatalf("run scan error = %v", err)
	}
	if !result.Scan.Started || client.scans != 1 || result.NAS != "lab" {
		t.Fatalf("run scan result = %#v scans = %d", result, client.scans)
	}
	// A NAS without the advertised run-scan backend refuses the action and never
	// calls the trigger.
	client.capabilities.RunScan = false
	if _, err := runSecurityScanWithClient(context.Background(), "lab", client); err == nil || !strings.Contains(err.Error(), "run-scan backend") {
		t.Fatalf("gated run scan error = %v", err)
	}
	if client.scans != 1 {
		t.Fatalf("scans = %d, want the gated call to be refused", client.scans)
	}
}

func TestSecurityAdvisorMissingWriteBackend(t *testing.T) {
	client := securityAdvisorTestClient()
	client.configuration = newSAConfig()
	client.capabilities.ScheduleWrite = false
	if _, err := planSecurityAdvisorScheduleWithClient(context.Background(), "lab", client, securityadvisor.ScheduleChange{Minute: intPtr(30)}); err == nil || !strings.Contains(err.Error(), "schedule read/write backend") {
		t.Fatalf("missing backend plan error = %v", err)
	}
}
