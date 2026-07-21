package application

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology"
)

type AutoBlockSettingsResult struct {
	NAS      string                     `json:"nas" jsonschema:"NAS profile used for the request"`
	Settings synology.AutoBlockSettings `json:"settings" jsonschema:"Auto Block configuration"`
}

type AutoBlockListsResult struct {
	NAS   string                  `json:"nas" jsonschema:"NAS profile used for the request"`
	Lists synology.AutoBlockLists `json:"lists" jsonschema:"Auto Block allow and block IP lists"`
}

type AccountProtectionResult struct {
	NAS        string                     `json:"nas" jsonschema:"NAS profile used for the request"`
	Protection synology.AccountProtection `json:"protection" jsonschema:"Account Protection thresholds"`
}

type EnforceTwoFactorResult struct {
	NAS    string                    `json:"nas" jsonschema:"NAS profile used for the request"`
	Policy synology.EnforceTwoFactor `json:"policy" jsonschema:"Enforced-2FA policy scope"`
}

type AccountProtectionCapabilitiesResult struct {
	NAS          string                                 `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.AccountProtectionCapabilities `json:"capabilities" jsonschema:"Account-protection reads currently exposed by dsmctl"`
	Report       synology.CompatibilityReport           `json:"report" jsonschema:"Discovered APIs and selected account-protection backends"`
}

type accountProtectionClient interface {
	AutoBlockSettings(context.Context) (synology.AutoBlockSettings, error)
	AutoBlockLists(context.Context) (synology.AutoBlockLists, error)
	AccountProtection(context.Context) (synology.AccountProtection, error)
	EnforceTwoFactor(context.Context) (synology.EnforceTwoFactor, error)
	AccountProtectionCapabilities(context.Context) (synology.AccountProtectionCapabilities, synology.CompatibilityReport, error)
	ActiveConnections(context.Context) ([]synology.ActiveConnection, error)
	ApplyAutoBlockChange(context.Context, synology.AutoBlockChange) (synology.AccountProtectionMutationResult, error)
	ApplyAccountProtectionChange(context.Context, synology.AccountProtectionChange) (synology.AccountProtectionMutationResult, error)
	ApplyEnforceTwoFactorChange(context.Context, synology.EnforceTwoFactorChange) (synology.AccountProtectionMutationResult, error)
	ApplyAutoBlockListEdit(context.Context, synology.IPListEdit) (synology.AccountProtectionMutationResult, error)
}

func (s *Service) GetAutoBlockSettings(ctx context.Context, requestedNAS string) (AutoBlockSettingsResult, error) {
	name, client, err := s.accountProtectionClient(ctx, requestedNAS)
	if err != nil {
		return AutoBlockSettingsResult{}, err
	}
	settings, err := client.AutoBlockSettings(ctx)
	if err != nil {
		return AutoBlockSettingsResult{}, authenticationError(name, err)
	}
	return AutoBlockSettingsResult{NAS: name, Settings: settings}, nil
}

func (s *Service) GetAutoBlockLists(ctx context.Context, requestedNAS string) (AutoBlockListsResult, error) {
	name, client, err := s.accountProtectionClient(ctx, requestedNAS)
	if err != nil {
		return AutoBlockListsResult{}, err
	}
	lists, err := client.AutoBlockLists(ctx)
	if err != nil {
		return AutoBlockListsResult{}, authenticationError(name, err)
	}
	return AutoBlockListsResult{NAS: name, Lists: lists}, nil
}

func (s *Service) GetAccountProtection(ctx context.Context, requestedNAS string) (AccountProtectionResult, error) {
	name, client, err := s.accountProtectionClient(ctx, requestedNAS)
	if err != nil {
		return AccountProtectionResult{}, err
	}
	protection, err := client.AccountProtection(ctx)
	if err != nil {
		return AccountProtectionResult{}, authenticationError(name, err)
	}
	return AccountProtectionResult{NAS: name, Protection: protection}, nil
}

func (s *Service) GetEnforceTwoFactor(ctx context.Context, requestedNAS string) (EnforceTwoFactorResult, error) {
	name, client, err := s.accountProtectionClient(ctx, requestedNAS)
	if err != nil {
		return EnforceTwoFactorResult{}, err
	}
	policy, err := client.EnforceTwoFactor(ctx)
	if err != nil {
		return EnforceTwoFactorResult{}, authenticationError(name, err)
	}
	return EnforceTwoFactorResult{NAS: name, Policy: policy}, nil
}

func (s *Service) GetAccountProtectionCapabilities(ctx context.Context, requestedNAS string) (AccountProtectionCapabilitiesResult, error) {
	name, client, err := s.accountProtectionClient(ctx, requestedNAS)
	if err != nil {
		return AccountProtectionCapabilitiesResult{}, err
	}
	capabilities, report, err := client.AccountProtectionCapabilities(ctx)
	if err != nil {
		return AccountProtectionCapabilitiesResult{}, authenticationError(name, err)
	}
	return AccountProtectionCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) accountProtectionClient(ctx context.Context, requestedNAS string) (string, accountProtectionClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(accountProtectionClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement account protection")
	}
	return name, client, nil
}

var _ accountProtectionClient = (*synology.Client)(nil)
