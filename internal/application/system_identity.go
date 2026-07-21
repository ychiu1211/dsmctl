package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

const systemHostnameAPIVersion = "dsmctl.io/v1alpha1"

// hostnamePattern is the DSM server-name (hostname) grammar: 1–63 characters,
// letters/digits/hyphen, not starting or ending with a hyphen.
var hostnamePattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

// SystemHostnameChange is the intent for setting the DSM server name (hostname).
type SystemHostnameChange struct {
	Hostname string `json:"hostname" jsonschema:"New DSM server name (hostname): 1-63 letters, digits, or hyphens, not starting or ending with a hyphen"`
}

// SystemHostnamePlan binds a validated hostname intent to the observed current
// name. Renaming cannot destroy data, so there is no destructive flag; the risk
// label and warnings carry the operational consequences.
type SystemHostnamePlan struct {
	APIVersion          string               `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string               `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64               `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             SystemHostnameChange `json:"request" jsonschema:"Validated hostname intent"`
	ObservedHostname    string               `json:"observed_hostname" jsonschema:"Server name observed during planning"`
	ObservedFingerprint string               `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the observed server name"`
	Risk                string               `json:"risk" jsonschema:"Plan risk level"`
	Warnings            []string             `json:"warnings" jsonschema:"Rename consequences"`
	Summary             []string             `json:"summary" jsonschema:"Human-readable summary of the rename"`
	Hash                string               `json:"hash" jsonschema:"SHA-256 approval hash covering intent and observed state"`
}

// SystemHostnameApplyResult reports the outcome of applying a hostname plan.
type SystemHostnameApplyResult struct {
	NAS      string `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash string `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied  bool   `json:"applied" jsonschema:"Whether DSM accepted the change and the postcondition verified"`
	Previous string `json:"previous" jsonschema:"Server name before the change"`
	Hostname string `json:"hostname" jsonschema:"Server name persisted and verified after the change"`
}

type systemHostnameClient interface {
	GetServerName(context.Context) (string, error)
	SetServerName(context.Context, string) (string, error)
}

// PlanSystemHostname validates a hostname change against the NAS's current name
// and emits a hash-bound approval plan.
func (s *Service) PlanSystemHostname(ctx context.Context, requestedNAS string, request SystemHostnameChange) (SystemHostnamePlan, error) {
	if err := validateHostname(request.Hostname); err != nil {
		return SystemHostnamePlan{}, err
	}
	name, client, err := s.systemHostnameClient(ctx, requestedNAS)
	if err != nil {
		return SystemHostnamePlan{}, err
	}
	plan, err := planSystemHostnameWithClient(ctx, name, client, request)
	if err != nil {
		return SystemHostnamePlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = systemHostnamePlanHash(plan)
	}
	return plan, err
}

// ApplySystemHostnamePlan re-checks the plan against fresh state, applies the
// rename, and verifies DSM reports the requested name.
func (s *Service) ApplySystemHostnamePlan(ctx context.Context, plan SystemHostnamePlan, approvalHash string) (SystemHostnameApplyResult, error) {
	if err := validateSystemHostnamePlan(plan, approvalHash); err != nil {
		return SystemHostnameApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return SystemHostnameApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return SystemHostnameApplyResult{}, err
	}
	name, client, err := s.systemHostnameClient(ctx, plan.NAS)
	if err != nil {
		return SystemHostnameApplyResult{}, err
	}
	if name != plan.NAS {
		return SystemHostnameApplyResult{}, fmt.Errorf("hostname plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	current, err := planSystemHostnameWithClient(ctx, name, client, plan.Request)
	if err != nil {
		return SystemHostnameApplyResult{}, fmt.Errorf("hostname plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	if current.Hash, err = systemHostnamePlanHash(current); err != nil {
		return SystemHostnameApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return SystemHostnameApplyResult{}, fmt.Errorf("hostname plan is stale; create a new plan")
	}
	after, err := client.SetServerName(ctx, plan.Request.Hostname)
	if err != nil {
		return SystemHostnameApplyResult{}, authenticationError(plan.NAS, err)
	}
	if strings.TrimSpace(after) != plan.Request.Hostname {
		return SystemHostnameApplyResult{}, fmt.Errorf("server name is %q after the change, want %q", after, plan.Request.Hostname)
	}
	return SystemHostnameApplyResult{
		NAS: plan.NAS, PlanHash: plan.Hash, Applied: true,
		Previous: plan.ObservedHostname, Hostname: after,
	}, nil
}

func (s *Service) systemHostnameClient(ctx context.Context, requestedNAS string) (string, systemHostnameClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(systemHostnameClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement server-name management")
	}
	return name, client, nil
}

func planSystemHostnameWithClient(ctx context.Context, nas string, client systemHostnameClient, request SystemHostnameChange) (SystemHostnamePlan, error) {
	observed, err := client.GetServerName(ctx)
	if err != nil {
		return SystemHostnamePlan{}, authenticationError(nas, err)
	}
	if strings.EqualFold(strings.TrimSpace(observed), request.Hostname) {
		return SystemHostnamePlan{}, fmt.Errorf("server name is already %q", request.Hostname)
	}
	plan := SystemHostnamePlan{APIVersion: systemHostnameAPIVersion, NAS: nas, Request: request, ObservedHostname: observed}
	plan.ObservedFingerprint, err = hashJSON(observed)
	if err != nil {
		return SystemHostnamePlan{}, err
	}
	plan.Risk = "medium"
	plan.Summary = []string{fmt.Sprintf("change the DSM server name from %q to %q", observed, request.Hostname)}
	plan.Warnings = []string{"renaming changes the NAS's network identity; clients, certificates, or bookmarks that reference the old name may need updating"}
	plan.Hash, err = systemHostnamePlanHash(plan)
	if err != nil {
		return SystemHostnamePlan{}, err
	}
	return plan, nil
}

func validateHostname(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("server name must not be empty")
	}
	if len(name) > 63 {
		return fmt.Errorf("server name %q exceeds 63 characters", name)
	}
	if !hostnamePattern.MatchString(name) {
		return fmt.Errorf("server name %q must be 1-63 letters, digits, or hyphens and may not start or end with a hyphen", name)
	}
	return nil
}

func validateSystemHostnamePlan(plan SystemHostnamePlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the hostname plan")
	}
	if plan.APIVersion != systemHostnameAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid hostname plan metadata")
	}
	if err := validateHostname(plan.Request.Hostname); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.ObservedHostname)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("hostname plan observed state was modified")
	}
	expectedHash, err := systemHostnamePlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("hostname plan contents were modified after planning")
	}
	return nil
}

func systemHostnamePlanHash(plan SystemHostnamePlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}
