package application

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology"
)

type SecurityAdvisorStatusResult struct {
	NAS    string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	Status synology.SecurityAdvisorStatus `json:"status" jsonschema:"Normalized last-scan status and per-category findings"`
}

type SecurityAdvisorScheduleResult struct {
	NAS           string                                `json:"nas" jsonschema:"NAS profile used for the request"`
	Configuration synology.SecurityAdvisorConfiguration `json:"configuration" jsonschema:"Current scan schedule and security baseline"`
}

type SecurityAdvisorCapabilitiesResult struct {
	NAS          string                               `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.SecurityAdvisorCapabilities `json:"capabilities" jsonschema:"Security Advisor operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport         `json:"report" jsonschema:"Discovered APIs and selected Security Advisor backends"`
}

type securityAdvisorClient interface {
	SecurityAdvisorStatus(context.Context) (synology.SecurityAdvisorStatus, error)
	SecurityAdvisorConfiguration(context.Context) (synology.SecurityAdvisorConfiguration, error)
	SecurityAdvisorCapabilities(context.Context) (synology.SecurityAdvisorCapabilities, synology.CompatibilityReport, error)
	ApplySecurityAdvisorScheduleChange(context.Context, synology.SecurityAdvisorScheduleChange) (synology.SecurityAdvisorMutationResult, error)
	RunSecurityScan(context.Context) (synology.SecurityAdvisorScanResult, error)
}

func (s *Service) GetSecurityAdvisorStatus(ctx context.Context, requestedNAS string) (SecurityAdvisorStatusResult, error) {
	name, client, err := s.securityAdvisorClient(ctx, requestedNAS)
	if err != nil {
		return SecurityAdvisorStatusResult{}, err
	}
	status, err := client.SecurityAdvisorStatus(ctx)
	if err != nil {
		return SecurityAdvisorStatusResult{}, authenticationError(name, err)
	}
	return SecurityAdvisorStatusResult{NAS: name, Status: status}, nil
}

func (s *Service) GetSecurityAdvisorSchedule(ctx context.Context, requestedNAS string) (SecurityAdvisorScheduleResult, error) {
	name, client, err := s.securityAdvisorClient(ctx, requestedNAS)
	if err != nil {
		return SecurityAdvisorScheduleResult{}, err
	}
	configuration, err := client.SecurityAdvisorConfiguration(ctx)
	if err != nil {
		return SecurityAdvisorScheduleResult{}, authenticationError(name, err)
	}
	return SecurityAdvisorScheduleResult{NAS: name, Configuration: configuration}, nil
}

func (s *Service) GetSecurityAdvisorCapabilities(ctx context.Context, requestedNAS string) (SecurityAdvisorCapabilitiesResult, error) {
	name, client, err := s.securityAdvisorClient(ctx, requestedNAS)
	if err != nil {
		return SecurityAdvisorCapabilitiesResult{}, err
	}
	capabilities, report, err := client.SecurityAdvisorCapabilities(ctx)
	if err != nil {
		return SecurityAdvisorCapabilitiesResult{}, authenticationError(name, err)
	}
	return SecurityAdvisorCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) securityAdvisorClient(ctx context.Context, requestedNAS string) (string, securityAdvisorClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(securityAdvisorClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement security advisor")
	}
	return name, client, nil
}

var _ securityAdvisorClient = (*synology.Client)(nil)
