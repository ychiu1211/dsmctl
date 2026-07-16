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
	config  *config.Config
	manager *runtime.Manager
}

type SystemInfoResult struct {
	NAS    string              `json:"nas" jsonschema:"NAS profile used for the request"`
	System synology.SystemInfo `json:"system" jsonschema:"System information returned by DSM"`
}

type AuthenticationResult struct {
	NAS string `json:"nas"`
}

func NewService(cfg *config.Config, manager *runtime.Manager) *Service {
	return &Service{config: cfg, manager: manager}
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

func authenticationError(name string, err error) error {
	if synology.IsOTPRequired(err) {
		return fmt.Errorf("NAS %q requires a one-time password; run 'dsmctl auth login --nas %s' in an interactive terminal first: %w", name, name, err)
	}
	return fmt.Errorf("NAS %q: %w", name, err)
}

func (s *Service) Close(ctx context.Context) error {
	return s.manager.Close(ctx)
}
