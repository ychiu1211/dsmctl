package application

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/runtime"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type Service struct {
	config           *config.Config
	manager          *runtime.Manager
	secretReferences credentials.ReferenceResolver
}

type SystemInfoResult struct {
	NAS    string              `json:"nas" jsonschema:"NAS profile used for the request"`
	System synology.SystemInfo `json:"system" jsonschema:"System information returned by DSM"`
}

type AuthenticationResult struct {
	NAS string `json:"nas"`
}

type CompatibilityResult struct {
	NAS    string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Report synology.CompatibilityReport `json:"report" jsonschema:"Discovered DSM compatibility target and selected operation backends"`
}

type StorageStateResult struct {
	NAS     string                `json:"nas" jsonschema:"NAS profile used for the request"`
	Storage synology.StorageState `json:"storage" jsonschema:"Normalized disk, storage-pool, and volume inventory"`
}

type StorageCapabilitiesResult struct {
	NAS          string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.StorageCapabilities `json:"capabilities" jsonschema:"Storage operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs and selected storage compatibility backend"`
}

type IdentityStateResult struct {
	NAS      string                 `json:"nas" jsonschema:"NAS profile used for the request"`
	Identity synology.IdentityState `json:"identity" jsonschema:"Normalized local user and group inventory"`
}

type IdentityCapabilitiesResult struct {
	NAS          string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.IdentityCapabilities `json:"capabilities" jsonschema:"Account and group operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport  `json:"report" jsonschema:"Discovered APIs and selected identity compatibility backends"`
}

type ShareStateResult struct {
	NAS    string              `json:"nas" jsonschema:"NAS profile used for the request"`
	Shares synology.ShareState `json:"shares" jsonschema:"Normalized shared-folder inventory and optional permissions"`
}

type ShareCapabilitiesResult struct {
	NAS          string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.ShareCapabilities   `json:"capabilities" jsonschema:"Shared-folder operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs and selected shared-folder compatibility backends"`
}

type ServiceOption func(*Service)

func WithSecretReferenceResolver(resolver credentials.ReferenceResolver) ServiceOption {
	return func(service *Service) {
		if resolver != nil {
			service.secretReferences = resolver
		}
	}
}

func NewService(cfg *config.Config, manager *runtime.Manager, options ...ServiceOption) *Service {
	service := &Service{
		config:           cfg,
		manager:          manager,
		secretReferences: credentials.NewEnvironmentReferenceResolver(),
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) ListNAS() []config.Summary {
	return s.config.Summaries(credentials.DefaultEnvironmentVariable)
}

func (s *Service) GetSystemInfo(ctx context.Context, requestedNAS string) (SystemInfoResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return SystemInfoResult{}, err
	}
	info, err := client.SystemInfo(ctx)
	if err != nil {
		return SystemInfoResult{}, authenticationError(name, err)
	}
	return SystemInfoResult{NAS: name, System: info}, nil
}

func (s *Service) Authenticate(ctx context.Context, requestedNAS string) (AuthenticationResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return AuthenticationResult{}, err
	}
	if err := client.Authenticate(ctx); err != nil {
		return AuthenticationResult{}, authenticationError(name, err)
	}
	return AuthenticationResult{NAS: name}, nil
}

func (s *Service) GetCompatibility(ctx context.Context, requestedNAS string) (CompatibilityResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return CompatibilityResult{}, err
	}
	report, err := client.Compatibility(ctx)
	if err != nil {
		return CompatibilityResult{}, authenticationError(name, err)
	}
	return CompatibilityResult{NAS: name, Report: report}, nil
}

func (s *Service) GetStorageState(ctx context.Context, requestedNAS string) (StorageStateResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return StorageStateResult{}, err
	}
	state, err := client.StorageState(ctx)
	if err != nil {
		return StorageStateResult{}, authenticationError(name, err)
	}
	return StorageStateResult{NAS: name, Storage: state}, nil
}

func (s *Service) GetStorageCapabilities(ctx context.Context, requestedNAS string) (StorageCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return StorageCapabilitiesResult{}, err
	}
	capabilities, report, err := client.StorageCapabilities(ctx)
	if err != nil {
		return StorageCapabilitiesResult{}, authenticationError(name, err)
	}
	return StorageCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetIdentityState(ctx context.Context, requestedNAS string) (IdentityStateResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return IdentityStateResult{}, err
	}
	state, err := client.IdentityState(ctx)
	if err != nil {
		return IdentityStateResult{}, authenticationError(name, err)
	}
	return IdentityStateResult{NAS: name, Identity: state}, nil
}

func (s *Service) GetIdentityCapabilities(ctx context.Context, requestedNAS string) (IdentityCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return IdentityCapabilitiesResult{}, err
	}
	capabilities, report, err := client.IdentityCapabilities(ctx)
	if err != nil {
		return IdentityCapabilitiesResult{}, authenticationError(name, err)
	}
	return IdentityCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetShareState(ctx context.Context, requestedNAS string, includePermissions bool) (ShareStateResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return ShareStateResult{}, err
	}
	state, err := client.ShareState(ctx, includePermissions)
	if err != nil {
		return ShareStateResult{}, authenticationError(name, err)
	}
	return ShareStateResult{NAS: name, Shares: state}, nil
}

func (s *Service) GetShareCapabilities(ctx context.Context, requestedNAS string) (ShareCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return ShareCapabilitiesResult{}, err
	}
	capabilities, report, err := client.ShareCapabilities(ctx)
	if err != nil {
		return ShareCapabilitiesResult{}, authenticationError(name, err)
	}
	return ShareCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func authenticationError(name string, err error) error {
	if synology.IsOTPRequired(err) {
		return fmt.Errorf("NAS %q requires a one-time password; run 'dsmctl auth login --nas %s' in an interactive terminal first: %w", name, name, err)
	}
	return fmt.Errorf("NAS %q: %w", name, err)
}

func (s *Service) Close(ctx context.Context) error {
	return s.manager.Close(ctx)
}
