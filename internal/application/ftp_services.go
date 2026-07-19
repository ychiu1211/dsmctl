package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/ftpservices"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const ftpServicesAPIVersion = "dsmctl.io/v1alpha1"

type FTPServicesStateResult struct {
	NAS         string                    `json:"nas" jsonschema:"NAS profile used for the request"`
	FTPServices synology.FTPServicesState `json:"ftp_services" jsonschema:"Normalized FTP and SFTP configuration"`
}

type FTPServicesCapabilitiesResult struct {
	NAS          string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.FTPServicesCapabilities `json:"capabilities" jsonschema:"Selected FTP and SFTP operations"`
	Report       synology.CompatibilityReport     `json:"report" jsonschema:"Discovered APIs and selected FTP/SFTP backends"`
}

type FTPServicesPlan struct {
	APIVersion          string                    `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                    `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                    `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             ftpservices.Change        `json:"request" jsonschema:"Patch-only FTP/SFTP intent"`
	Observed            synology.FTPServicesState `json:"observed" jsonschema:"Complete FTP/SFTP state observed during planning"`
	ObservedFingerprint string                    `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed state"`
	Destructive         bool                      `json:"destructive" jsonschema:"Whether the plan disables a file-transfer service"`
	Risk                string                    `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                  `json:"warnings" jsonschema:"Service disruption and network-exposure warnings"`
	Summary             []string                  `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                    `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

type FTPServicesApplyResult struct {
	NAS        string                               `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash   string                               `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied    bool                                 `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Operations []synology.FTPServicesMutationResult `json:"operations" jsonschema:"Selected DSM mutation backends, one per changed protocol"`
}

type ftpServicesClient interface {
	FTPServicesState(context.Context) (synology.FTPServicesState, error)
	FTPServicesCapabilities(context.Context) (synology.FTPServicesCapabilities, synology.CompatibilityReport, error)
	ApplyFTPServicesChange(context.Context, ftpservices.Change) ([]synology.FTPServicesMutationResult, error)
}

func (s *Service) GetFTPServicesState(ctx context.Context, requestedNAS string) (FTPServicesStateResult, error) {
	name, client, err := s.ftpServicesClient(ctx, requestedNAS)
	if err != nil {
		return FTPServicesStateResult{}, err
	}
	state, err := client.FTPServicesState(ctx)
	if err != nil {
		return FTPServicesStateResult{}, authenticationError(name, err)
	}
	return FTPServicesStateResult{NAS: name, FTPServices: state}, nil
}

func (s *Service) GetFTPServicesCapabilities(ctx context.Context, requestedNAS string) (FTPServicesCapabilitiesResult, error) {
	name, client, err := s.ftpServicesClient(ctx, requestedNAS)
	if err != nil {
		return FTPServicesCapabilitiesResult{}, err
	}
	capabilities, report, err := client.FTPServicesCapabilities(ctx)
	if err != nil {
		return FTPServicesCapabilitiesResult{}, authenticationError(name, err)
	}
	return FTPServicesCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) PlanFTPServicesChange(ctx context.Context, requestedNAS string, request ftpservices.Change) (FTPServicesPlan, error) {
	if err := validateFTPServicesChange(request); err != nil {
		return FTPServicesPlan{}, err
	}
	name, client, err := s.ftpServicesClient(ctx, requestedNAS)
	if err != nil {
		return FTPServicesPlan{}, err
	}
	plan, err := planFTPServicesChangeWithClient(ctx, name, client, request)
	if err != nil {
		return FTPServicesPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = ftpServicesPlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyFTPServicesPlan(ctx context.Context, plan FTPServicesPlan, approvalHash string) (FTPServicesApplyResult, error) {
	if err := validateFTPServicesPlan(plan, approvalHash); err != nil {
		return FTPServicesApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return FTPServicesApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return FTPServicesApplyResult{}, err
	}
	name, client, err := s.ftpServicesClient(ctx, plan.NAS)
	if err != nil {
		return FTPServicesApplyResult{}, err
	}
	if name != plan.NAS {
		return FTPServicesApplyResult{}, fmt.Errorf("FTP services plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyFTPServicesPlanWithClient(ctx, client, plan)
}

func applyFTPServicesPlanWithClient(ctx context.Context, client ftpServicesClient, plan FTPServicesPlan) (FTPServicesApplyResult, error) {
	current, err := planFTPServicesChangeWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return FTPServicesApplyResult{}, fmt.Errorf("FTP services plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = ftpServicesPlanHash(current)
	if err != nil {
		return FTPServicesApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return FTPServicesApplyResult{}, fmt.Errorf("FTP services plan is stale; create a new plan")
	}
	operations, err := client.ApplyFTPServicesChange(ctx, plan.Request)
	if err != nil {
		return FTPServicesApplyResult{}, authenticationError(plan.NAS, err)
	}
	after, err := client.FTPServicesState(ctx)
	if err != nil {
		return FTPServicesApplyResult{}, fmt.Errorf("verify FTP services change: %w", err)
	}
	if !ftpServicesChangeMatches(after, plan.Request) {
		return FTPServicesApplyResult{}, fmt.Errorf("FTP services state does not match the approved patch")
	}
	return FTPServicesApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operations: operations}, nil
}

func (s *Service) ftpServicesClient(ctx context.Context, requestedNAS string) (string, ftpServicesClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(ftpServicesClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement FTP services management")
	}
	return name, client, nil
}

func planFTPServicesChangeWithClient(ctx context.Context, nas string, client ftpServicesClient, request ftpservices.Change) (FTPServicesPlan, error) {
	capabilities, _, err := client.FTPServicesCapabilities(ctx)
	if err != nil {
		return FTPServicesPlan{}, authenticationError(nas, err)
	}
	if request.FTP != nil {
		if !capabilities.FTPRead {
			return FTPServicesPlan{}, fmt.Errorf("NAS %q does not expose a verified FTP read backend", nas)
		}
		if !capabilities.FTPSet {
			return FTPServicesPlan{}, fmt.Errorf("NAS %q does not expose a verified FTP set backend", nas)
		}
	}
	if request.SFTP != nil {
		if !capabilities.SFTPRead {
			return FTPServicesPlan{}, fmt.Errorf("NAS %q does not expose a verified SFTP read backend", nas)
		}
		if !capabilities.SFTPSet {
			return FTPServicesPlan{}, fmt.Errorf("NAS %q does not expose a verified SFTP set backend", nas)
		}
	}
	observed, err := client.FTPServicesState(ctx)
	if err != nil {
		return FTPServicesPlan{}, authenticationError(nas, err)
	}
	if ftpServicesChangeMatches(observed, request) {
		return FTPServicesPlan{}, fmt.Errorf("FTP services patch would not change the current state")
	}
	plan := FTPServicesPlan{APIVersion: ftpServicesAPIVersion, NAS: nas, Request: request, Observed: observed}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return FTPServicesPlan{}, err
	}
	plan.Destructive, plan.Warnings, plan.Summary = ftpServicesPlanEffects(observed, request)
	if plan.Destructive || len(plan.Warnings) > 0 {
		plan.Risk = "high"
	} else {
		plan.Risk = "medium"
	}
	plan.Hash, err = ftpServicesPlanHash(plan)
	if err != nil {
		return FTPServicesPlan{}, err
	}
	return plan, nil
}

func validateFTPServicesChange(change ftpservices.Change) error {
	if change.FTP == nil && change.SFTP == nil {
		return fmt.Errorf("FTP services patch has no protocols")
	}
	if change.FTP != nil && change.FTP.Plain == nil && change.FTP.FTPS == nil {
		return fmt.Errorf("FTP patch has no fields")
	}
	if change.SFTP != nil {
		if change.SFTP.Enabled == nil && change.SFTP.Port == nil {
			return fmt.Errorf("SFTP patch has no fields")
		}
		if change.SFTP.Port != nil && (*change.SFTP.Port < 1 || *change.SFTP.Port > 65535) {
			return fmt.Errorf("SFTP port %d is out of range 1-65535", *change.SFTP.Port)
		}
	}
	return nil
}

func ftpServicesPlanEffects(observed synology.FTPServicesState, change ftpservices.Change) (bool, []string, []string) {
	warnings := []string{}
	summary := []string{}
	destructive := false
	if change.FTP != nil {
		if change.FTP.Plain != nil {
			summary = append(summary, fmt.Sprintf("set plain FTP to %t", *change.FTP.Plain))
			if *change.FTP.Plain {
				warnings = append(warnings, "enabling plain FTP transmits credentials and data without encryption")
			} else if observed.FTP.Plain {
				destructive = true
				warnings = append(warnings, "disabling plain FTP disconnects unencrypted FTP clients")
			}
		}
		if change.FTP.FTPS != nil {
			summary = append(summary, fmt.Sprintf("set FTPS to %t", *change.FTP.FTPS))
			if !*change.FTP.FTPS && observed.FTP.FTPS {
				destructive = true
				warnings = append(warnings, "disabling FTPS disconnects TLS FTP clients")
			}
		}
	}
	if change.SFTP != nil {
		if change.SFTP.Enabled != nil {
			summary = append(summary, fmt.Sprintf("set SFTP to %t", *change.SFTP.Enabled))
			if !*change.SFTP.Enabled && observed.SFTP != nil && observed.SFTP.Enabled {
				destructive = true
				warnings = append(warnings, "disabling SFTP disconnects SSH file-transfer clients")
			}
		}
		if change.SFTP.Port != nil {
			summary = append(summary, fmt.Sprintf("set SFTP port to %d", *change.SFTP.Port))
			if observed.SFTP != nil && observed.SFTP.Enabled && observed.SFTP.Port != *change.SFTP.Port {
				warnings = append(warnings, "changing the SFTP port disconnects clients using the old port")
			}
		}
	}
	return destructive, warnings, summary
}

func ftpServicesChangeMatches(state synology.FTPServicesState, change ftpservices.Change) bool {
	if change.FTP != nil {
		if change.FTP.Plain != nil && state.FTP.Plain != *change.FTP.Plain {
			return false
		}
		if change.FTP.FTPS != nil && state.FTP.FTPS != *change.FTP.FTPS {
			return false
		}
	}
	if change.SFTP != nil {
		if state.SFTP == nil {
			return false
		}
		if change.SFTP.Enabled != nil && state.SFTP.Enabled != *change.SFTP.Enabled {
			return false
		}
		if change.SFTP.Port != nil && state.SFTP.Port != *change.SFTP.Port {
			return false
		}
	}
	return true
}

func validateFTPServicesPlan(plan FTPServicesPlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the FTP services plan")
	}
	if plan.APIVersion != ftpServicesAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid FTP services plan metadata")
	}
	if err := validateFTPServicesChange(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("FTP services plan observed state was modified")
	}
	expectedHash, err := ftpServicesPlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("FTP services plan contents were modified after planning")
	}
	return nil
}

func ftpServicesPlanHash(plan FTPServicesPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

var _ ftpServicesClient = (*synology.Client)(nil)
