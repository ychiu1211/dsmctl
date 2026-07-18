package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/nfsexport"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const nfsExportAPIVersion = "dsmctl.io/v1alpha1"

var nfsExportClientPattern = regexp.MustCompile(`^[\x21-\x7e]{1,255}$`)

type NFSExportStateResult struct {
	NAS    string                  `json:"nas" jsonschema:"NAS profile used for the request"`
	Export synology.NFSShareExport `json:"export" jsonschema:"Complete NFS export rule set for the shared folder"`
}

type NFSExportCapabilitiesResult struct {
	NAS          string                          `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.NFSExportCapabilities  `json:"capabilities" jsonschema:"Selected NFS export read and set operations"`
	Report       synology.CompatibilityReport    `json:"report" jsonschema:"Discovered APIs and selected NFS export backend"`
}

type NFSExportPlan struct {
	APIVersion          string                  `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                  `json:"nas" jsonschema:"NAS profile selected during planning"`
	Request             nfsexport.ChangeRequest `json:"request" jsonschema:"Complete desired NFS export rule set for one shared folder"`
	Observed            synology.NFSShareExport  `json:"observed" jsonschema:"Complete NFS export rule set observed during planning"`
	ObservedFingerprint string                  `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed rule set"`
	Destructive         bool                    `json:"destructive" jsonschema:"Whether the plan removes an existing export rule"`
	Risk                string                  `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                `json:"warnings" jsonschema:"Network-exposure and disruption warnings"`
	Summary             []string                `json:"summary" jsonschema:"Human-readable rule-set changes"`
	Hash                string                  `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

type NFSExportApplyResult struct {
	NAS       string                           `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash  string                           `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied   bool                             `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Operation synology.NFSExportMutationResult `json:"operation" jsonschema:"Selected DSM mutation backend"`
}

type nfsExportClient interface {
	NFSExportState(context.Context, string) (synology.NFSShareExport, error)
	NFSExportCapabilities(context.Context) (synology.NFSExportCapabilities, synology.CompatibilityReport, error)
	ApplyNFSExportChange(context.Context, nfsexport.ChangeRequest) (synology.NFSExportMutationResult, error)
}

func (s *Service) GetNFSExportState(ctx context.Context, requestedNAS, share string) (NFSExportStateResult, error) {
	name, client, err := s.nfsExportClient(ctx, requestedNAS)
	if err != nil {
		return NFSExportStateResult{}, err
	}
	if strings.TrimSpace(share) == "" {
		return NFSExportStateResult{}, fmt.Errorf("shared-folder name is required")
	}
	export, err := client.NFSExportState(ctx, share)
	if err != nil {
		return NFSExportStateResult{}, authenticationError(name, err)
	}
	return NFSExportStateResult{NAS: name, Export: export}, nil
}

func (s *Service) GetNFSExportCapabilities(ctx context.Context, requestedNAS string) (NFSExportCapabilitiesResult, error) {
	name, client, err := s.nfsExportClient(ctx, requestedNAS)
	if err != nil {
		return NFSExportCapabilitiesResult{}, err
	}
	capabilities, report, err := client.NFSExportCapabilities(ctx)
	if err != nil {
		return NFSExportCapabilitiesResult{}, authenticationError(name, err)
	}
	return NFSExportCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) PlanNFSExportChange(ctx context.Context, requestedNAS string, request nfsexport.ChangeRequest) (NFSExportPlan, error) {
	if err := validateNFSExportRequest(request); err != nil {
		return NFSExportPlan{}, err
	}
	name, client, err := s.nfsExportClient(ctx, requestedNAS)
	if err != nil {
		return NFSExportPlan{}, err
	}
	return planNFSExportChangeWithClient(ctx, name, client, request)
}

func (s *Service) ApplyNFSExportPlan(ctx context.Context, plan NFSExportPlan, approvalHash string) (NFSExportApplyResult, error) {
	if err := validateNFSExportPlan(plan, approvalHash); err != nil {
		return NFSExportApplyResult{}, err
	}
	name, client, err := s.nfsExportClient(ctx, plan.NAS)
	if err != nil {
		return NFSExportApplyResult{}, err
	}
	if name != plan.NAS {
		return NFSExportApplyResult{}, fmt.Errorf("NFS export plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyNFSExportPlanWithClient(ctx, client, plan)
}

func applyNFSExportPlanWithClient(ctx context.Context, client nfsExportClient, plan NFSExportPlan) (NFSExportApplyResult, error) {
	current, err := planNFSExportChangeWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return NFSExportApplyResult{}, fmt.Errorf("NFS export plan precondition no longer holds: %w", err)
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return NFSExportApplyResult{}, fmt.Errorf("NFS export plan is stale; create a new plan")
	}
	operation, err := client.ApplyNFSExportChange(ctx, plan.Request)
	if err != nil {
		return NFSExportApplyResult{}, authenticationError(plan.NAS, err)
	}
	after, err := client.NFSExportState(ctx, plan.Request.Share)
	if err != nil {
		return NFSExportApplyResult{}, fmt.Errorf("verify NFS export change: %w", err)
	}
	if !exportRulesMatch(after.Rules, plan.Request.Rules) {
		return NFSExportApplyResult{}, fmt.Errorf("NFS export rules do not match the approved plan after apply")
	}
	return NFSExportApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operation: operation}, nil
}

func (s *Service) nfsExportClient(ctx context.Context, requestedNAS string) (string, nfsExportClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(nfsExportClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement NFS export management")
	}
	return name, client, nil
}

func planNFSExportChangeWithClient(ctx context.Context, nas string, client nfsExportClient, request nfsexport.ChangeRequest) (NFSExportPlan, error) {
	capabilities, _, err := client.NFSExportCapabilities(ctx)
	if err != nil {
		return NFSExportPlan{}, authenticationError(nas, err)
	}
	if !capabilities.Read || !capabilities.Set {
		return NFSExportPlan{}, fmt.Errorf("NAS %q does not expose a verified NFS export read/set backend", nas)
	}
	observed, err := client.NFSExportState(ctx, request.Share)
	if err != nil {
		return NFSExportPlan{}, authenticationError(nas, err)
	}
	if exportRulesMatch(observed.Rules, request.Rules) {
		return NFSExportPlan{}, fmt.Errorf("NFS export request would not change the current rule set")
	}
	plan := NFSExportPlan{APIVersion: nfsExportAPIVersion, NAS: nas, Request: request, Observed: observed}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return NFSExportPlan{}, err
	}
	plan.Destructive, plan.Warnings, plan.Summary = nfsExportPlanEffects(observed.Rules, request.Rules)
	if plan.Destructive || len(plan.Warnings) > 0 {
		plan.Risk = "high"
	} else {
		plan.Risk = "medium"
	}
	plan.Hash, err = nfsExportPlanHash(plan)
	if err != nil {
		return NFSExportPlan{}, err
	}
	return plan, nil
}

func validateNFSExportRequest(request nfsexport.ChangeRequest) error {
	if strings.TrimSpace(request.Share) == "" {
		return fmt.Errorf("NFS export request requires a shared-folder name")
	}
	seen := make(map[string]struct{}, len(request.Rules))
	for index, rule := range request.Rules {
		client := strings.TrimSpace(rule.Client)
		if !nfsExportClientPattern.MatchString(client) {
			return fmt.Errorf("rule %d: NFS client must be 1-255 printable ASCII characters without spaces", index)
		}
		key := strings.ToLower(client)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("rule %d: duplicate NFS client %q", index, client)
		}
		seen[key] = struct{}{}
		if !validPrivilege(rule.Privilege) {
			return fmt.Errorf("rule %d: unsupported privilege %q", index, rule.Privilege)
		}
		if !validSquash(rule.Squash) {
			return fmt.Errorf("rule %d: unsupported root squash %q", index, rule.Squash)
		}
		if !validSecurity(rule.Security) {
			return fmt.Errorf("rule %d: unsupported security flavor %q", index, rule.Security)
		}
	}
	return nil
}

func nfsExportPlanEffects(observed, desired []nfsexport.Rule) (bool, []string, []string) {
	warnings := []string{}
	summary := []string{}
	destructive := false

	observedByClient := indexRules(observed)
	desiredByClient := indexRules(desired)

	for _, rule := range desired {
		client := strings.TrimSpace(rule.Client)
		prior, existed := observedByClient[strings.ToLower(client)]
		if !existed {
			summary = append(summary, fmt.Sprintf("add NFS export rule for %q (%s)", client, rule.Privilege))
			warnings = append(warnings, exportExposureWarnings(rule, nil)...)
			continue
		}
		if prior != rule {
			summary = append(summary, fmt.Sprintf("modify NFS export rule for %q", client))
			warnings = append(warnings, exportExposureWarnings(rule, &prior)...)
		}
	}
	for _, rule := range observed {
		client := strings.TrimSpace(rule.Client)
		if _, kept := desiredByClient[strings.ToLower(client)]; !kept {
			summary = append(summary, fmt.Sprintf("remove NFS export rule for %q", client))
			destructive = true
			warnings = append(warnings, fmt.Sprintf("removing the NFS export rule for %q stops that client's access", client))
		}
	}
	return destructive, warnings, summary
}

// exportExposureWarnings flags a newly added or modified rule that broadens
// network exposure. prior is nil for an added rule.
func exportExposureWarnings(rule nfsexport.Rule, prior *nfsexport.Rule) []string {
	warnings := []string{}
	client := strings.TrimSpace(rule.Client)
	wildcard := strings.ContainsAny(client, "*?") || client == "0.0.0.0/0"
	if wildcard && rule.Privilege == nfsexport.PrivilegeReadWrite {
		warnings = append(warnings, fmt.Sprintf("rule %q grants read-write NFS access to any matching host", client))
	} else if wildcard {
		warnings = append(warnings, fmt.Sprintf("rule %q grants NFS access to any matching host", client))
	}
	if prior != nil && prior.Privilege == nfsexport.PrivilegeReadOnly && rule.Privilege == nfsexport.PrivilegeReadWrite {
		warnings = append(warnings, fmt.Sprintf("rule %q is broadened from read-only to read-write", client))
	}
	if rule.Security == nfsexport.SecuritySys && prior != nil && prior.Security != nfsexport.SecuritySys {
		warnings = append(warnings, fmt.Sprintf("rule %q lowers the security flavor to sys", client))
	}
	return warnings
}

func indexRules(rules []nfsexport.Rule) map[string]nfsexport.Rule {
	byClient := make(map[string]nfsexport.Rule, len(rules))
	for _, rule := range rules {
		byClient[strings.ToLower(strings.TrimSpace(rule.Client))] = rule
	}
	return byClient
}

// exportRulesMatch reports whether two rule sets are equal as client-keyed sets,
// independent of ordering.
func exportRulesMatch(left, right []nfsexport.Rule) bool {
	if len(left) != len(right) {
		return false
	}
	leftByClient := indexRules(left)
	rightByClient := indexRules(right)
	if len(leftByClient) != len(rightByClient) {
		return false
	}
	for client, rule := range leftByClient {
		other, ok := rightByClient[client]
		if !ok || other != rule {
			return false
		}
	}
	return true
}

func validateNFSExportPlan(plan NFSExportPlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the NFS export plan")
	}
	if plan.APIVersion != nfsExportAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid NFS export plan metadata")
	}
	if err := validateNFSExportRequest(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("NFS export plan observed state was modified")
	}
	expectedHash, err := nfsExportPlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("NFS export plan contents were modified after planning")
	}
	return nil
}

func nfsExportPlanHash(plan NFSExportPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

func validPrivilege(privilege nfsexport.Privilege) bool {
	return privilege == nfsexport.PrivilegeReadWrite || privilege == nfsexport.PrivilegeReadOnly
}

func validSquash(squash nfsexport.Squash) bool {
	switch squash {
	case nfsexport.SquashNoMapping, nfsexport.SquashRootToAdmin, nfsexport.SquashRootToGuest, nfsexport.SquashAllToAdmin, nfsexport.SquashAllToGuest:
		return true
	default:
		return false
	}
}

func validSecurity(security nfsexport.Security) bool {
	switch security {
	case nfsexport.SecuritySys, nfsexport.SecurityKerberos, nfsexport.SecurityKerberosIntegrity, nfsexport.SecurityKerberosPrivacy:
		return true
	default:
		return false
	}
}

var _ nfsExportClient = (*synology.Client)(nil)
