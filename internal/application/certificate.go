package application

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology"
)

type CertificatesResult struct {
	NAS          string                 `json:"nas" jsonschema:"NAS profile used for the request"`
	Certificates synology.Certificates  `json:"certificates" jsonschema:"Installed certificates with their bound services"`
}

type CertificateCapabilitiesResult struct {
	NAS          string                          `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.CertificateCapabilities `json:"capabilities" jsonschema:"Certificate operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport     `json:"report" jsonschema:"Discovered APIs and selected certificate backend"`
}

type certificateClient interface {
	Certificates(context.Context) (synology.Certificates, error)
	CertificateCapabilities(context.Context) (synology.CertificateCapabilities, synology.CompatibilityReport, error)
}

func (s *Service) GetCertificates(ctx context.Context, requestedNAS string) (CertificatesResult, error) {
	name, client, err := s.certificateClient(ctx, requestedNAS)
	if err != nil {
		return CertificatesResult{}, err
	}
	certs, err := client.Certificates(ctx)
	if err != nil {
		return CertificatesResult{}, authenticationError(name, err)
	}
	return CertificatesResult{NAS: name, Certificates: certs}, nil
}

func (s *Service) GetCertificateCapabilities(ctx context.Context, requestedNAS string) (CertificateCapabilitiesResult, error) {
	name, client, err := s.certificateClient(ctx, requestedNAS)
	if err != nil {
		return CertificateCapabilitiesResult{}, err
	}
	capabilities, report, err := client.CertificateCapabilities(ctx)
	if err != nil {
		return CertificateCapabilitiesResult{}, authenticationError(name, err)
	}
	return CertificateCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) certificateClient(ctx context.Context, requestedNAS string) (string, certificateClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(certificateClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement certificate management")
	}
	return name, client, nil
}

var _ certificateClient = (*synology.Client)(nil)
