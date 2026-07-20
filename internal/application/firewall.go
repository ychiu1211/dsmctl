package application

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology"
)

type FirewallStatusResult struct {
	NAS    string                  `json:"nas" jsonschema:"NAS profile used for the request"`
	Status synology.FirewallStatus `json:"status" jsonschema:"Global firewall enable flag, active profile, and network adapters"`
}

type FirewallProfilesResult struct {
	NAS      string                     `json:"nas" jsonschema:"NAS profile used for the request"`
	Profiles []synology.FirewallProfile `json:"profiles" jsonschema:"Firewall profiles, with the active one marked"`
}

type FirewallRulesResult struct {
	NAS     string                   `json:"nas" jsonschema:"NAS profile used for the request"`
	RuleSet synology.FirewallRuleSet `json:"rule_set" jsonschema:"Per-adapter default policy and ordered rules for the requested profile(s)"`
}

type FirewallCapabilitiesResult struct {
	NAS          string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.FirewallCapabilities `json:"capabilities" jsonschema:"Firewall reads currently exposed by dsmctl"`
	Report       synology.CompatibilityReport  `json:"report" jsonschema:"Discovered APIs and selected firewall backends"`
}

type firewallClient interface {
	FirewallStatus(context.Context) (synology.FirewallStatus, error)
	FirewallProfiles(context.Context) ([]synology.FirewallProfile, error)
	FirewallRules(context.Context, string) (synology.FirewallRuleSet, error)
	FirewallCapabilities(context.Context) (synology.FirewallCapabilities, synology.CompatibilityReport, error)
}

func (s *Service) GetFirewallStatus(ctx context.Context, requestedNAS string) (FirewallStatusResult, error) {
	name, client, err := s.firewallClient(ctx, requestedNAS)
	if err != nil {
		return FirewallStatusResult{}, err
	}
	status, err := client.FirewallStatus(ctx)
	if err != nil {
		return FirewallStatusResult{}, authenticationError(name, err)
	}
	return FirewallStatusResult{NAS: name, Status: status}, nil
}

func (s *Service) GetFirewallProfiles(ctx context.Context, requestedNAS string) (FirewallProfilesResult, error) {
	name, client, err := s.firewallClient(ctx, requestedNAS)
	if err != nil {
		return FirewallProfilesResult{}, err
	}
	profiles, err := client.FirewallProfiles(ctx)
	if err != nil {
		return FirewallProfilesResult{}, authenticationError(name, err)
	}
	return FirewallProfilesResult{NAS: name, Profiles: profiles}, nil
}

func (s *Service) GetFirewallRules(ctx context.Context, requestedNAS, profile string) (FirewallRulesResult, error) {
	name, client, err := s.firewallClient(ctx, requestedNAS)
	if err != nil {
		return FirewallRulesResult{}, err
	}
	ruleSet, err := client.FirewallRules(ctx, profile)
	if err != nil {
		return FirewallRulesResult{}, authenticationError(name, err)
	}
	return FirewallRulesResult{NAS: name, RuleSet: ruleSet}, nil
}

func (s *Service) GetFirewallCapabilities(ctx context.Context, requestedNAS string) (FirewallCapabilitiesResult, error) {
	name, client, err := s.firewallClient(ctx, requestedNAS)
	if err != nil {
		return FirewallCapabilitiesResult{}, err
	}
	capabilities, report, err := client.FirewallCapabilities(ctx)
	if err != nil {
		return FirewallCapabilitiesResult{}, authenticationError(name, err)
	}
	return FirewallCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) firewallClient(ctx context.Context, requestedNAS string) (string, firewallClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(firewallClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement firewall")
	}
	return name, client, nil
}

var _ firewallClient = (*synology.Client)(nil)
