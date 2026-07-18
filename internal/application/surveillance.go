package application

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology"
)

type SurveillanceCapabilitiesResult struct {
	NAS          string                            `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.SurveillanceCapabilities `json:"capabilities" jsonschema:"Surveillance operations exposed by dsmctl, with installed-package evidence"`
	Report       synology.CompatibilityReport      `json:"report" jsonschema:"Discovered APIs, installed packages, and selected Surveillance backends"`
}

type SurveillanceInfoResult struct {
	NAS  string                  `json:"nas" jsonschema:"NAS profile used for the request"`
	Info synology.SurveillanceInfo `json:"info" jsonschema:"Normalized Surveillance Station system information"`
}

type SurveillanceCamerasResult struct {
	NAS     string                     `json:"nas" jsonschema:"NAS profile used for the request"`
	Cameras synology.SurveillanceCameras `json:"cameras" jsonschema:"Configured cameras reported by Surveillance Station"`
}

type surveillanceClient interface {
	SurveillanceInfo(context.Context) (synology.SurveillanceInfo, error)
	SurveillanceCameras(context.Context) (synology.SurveillanceCameras, error)
	SurveillanceCapabilities(context.Context) (synology.SurveillanceCapabilities, synology.CompatibilityReport, error)
}

func (s *Service) GetSurveillanceCapabilities(ctx context.Context, requestedNAS string) (SurveillanceCapabilitiesResult, error) {
	name, client, err := s.surveillanceClient(ctx, requestedNAS)
	if err != nil {
		return SurveillanceCapabilitiesResult{}, err
	}
	capabilities, report, err := client.SurveillanceCapabilities(ctx)
	if err != nil {
		return SurveillanceCapabilitiesResult{}, authenticationError(name, err)
	}
	return SurveillanceCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetSurveillanceInfo(ctx context.Context, requestedNAS string) (SurveillanceInfoResult, error) {
	name, client, err := s.surveillanceClient(ctx, requestedNAS)
	if err != nil {
		return SurveillanceInfoResult{}, err
	}
	info, err := client.SurveillanceInfo(ctx)
	if err != nil {
		return SurveillanceInfoResult{}, authenticationError(name, err)
	}
	return SurveillanceInfoResult{NAS: name, Info: info}, nil
}

func (s *Service) GetSurveillanceCameras(ctx context.Context, requestedNAS string) (SurveillanceCamerasResult, error) {
	name, client, err := s.surveillanceClient(ctx, requestedNAS)
	if err != nil {
		return SurveillanceCamerasResult{}, err
	}
	cameras, err := client.SurveillanceCameras(ctx)
	if err != nil {
		return SurveillanceCamerasResult{}, authenticationError(name, err)
	}
	return SurveillanceCamerasResult{NAS: name, Cameras: cameras}, nil
}

func (s *Service) surveillanceClient(ctx context.Context, requestedNAS string) (string, surveillanceClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(surveillanceClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement Surveillance management")
	}
	return name, client, nil
}

var _ surveillanceClient = (*synology.Client)(nil)
