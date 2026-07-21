package application

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology"
)

type DSMWebServiceResult struct {
	NAS      string                 `json:"nas" jsonschema:"NAS profile used for the request"`
	Settings synology.DSMWebService `json:"settings" jsonschema:"DSM web-service access settings"`
}

type ApplicationPortalsResult struct {
	NAS     string                      `json:"nas" jsonschema:"NAS profile used for the request"`
	Portals synology.ApplicationPortals `json:"portals" jsonschema:"Per-application portal list"`
}

type ReverseProxyRulesResult struct {
	NAS   string                     `json:"nas" jsonschema:"NAS profile used for the request"`
	Rules synology.ReverseProxyRules `json:"rules" jsonschema:"Reverse-proxy rule list"`
}

type LoginPortalCapabilitiesResult struct {
	NAS          string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.LoginPortalCapabilities `json:"capabilities" jsonschema:"Login Portal reads currently exposed by dsmctl"`
	Report       synology.CompatibilityReport     `json:"report" jsonschema:"Discovered APIs and selected Login Portal backends"`
}

type loginPortalClient interface {
	DSMWebService(context.Context) (synology.DSMWebService, error)
	ApplicationPortals(context.Context) (synology.ApplicationPortals, error)
	ReverseProxyRules(context.Context) (synology.ReverseProxyRules, error)
	LoginPortalCapabilities(context.Context) (synology.LoginPortalCapabilities, synology.CompatibilityReport, error)
	LoginPortalTransport() synology.LoginPortalTransportInfo
	ApplyDSMWebServiceChange(context.Context, synology.DSMWebServiceChange) (synology.LoginPortalMutationResult, error)
	ApplyApplicationPortalChange(context.Context, synology.ApplicationPortalChange) (synology.LoginPortalMutationResult, error)
	ApplyReverseProxyRuleCreate(context.Context, synology.ReverseProxyRuleCreate, []synology.ReverseProxyHeaderValue) (synology.LoginPortalMutationResult, error)
	ApplyReverseProxyRuleDelete(context.Context, synology.ReverseProxyRuleDelete) (synology.LoginPortalMutationResult, error)
}

func (s *Service) GetDSMWebService(ctx context.Context, requestedNAS string) (DSMWebServiceResult, error) {
	name, client, err := s.loginPortalClient(ctx, requestedNAS)
	if err != nil {
		return DSMWebServiceResult{}, err
	}
	settings, err := client.DSMWebService(ctx)
	if err != nil {
		return DSMWebServiceResult{}, authenticationError(name, err)
	}
	return DSMWebServiceResult{NAS: name, Settings: settings}, nil
}

func (s *Service) GetApplicationPortals(ctx context.Context, requestedNAS string) (ApplicationPortalsResult, error) {
	name, client, err := s.loginPortalClient(ctx, requestedNAS)
	if err != nil {
		return ApplicationPortalsResult{}, err
	}
	portals, err := client.ApplicationPortals(ctx)
	if err != nil {
		return ApplicationPortalsResult{}, authenticationError(name, err)
	}
	return ApplicationPortalsResult{NAS: name, Portals: portals}, nil
}

func (s *Service) GetReverseProxyRules(ctx context.Context, requestedNAS string) (ReverseProxyRulesResult, error) {
	name, client, err := s.loginPortalClient(ctx, requestedNAS)
	if err != nil {
		return ReverseProxyRulesResult{}, err
	}
	rules, err := client.ReverseProxyRules(ctx)
	if err != nil {
		return ReverseProxyRulesResult{}, authenticationError(name, err)
	}
	return ReverseProxyRulesResult{NAS: name, Rules: rules}, nil
}

func (s *Service) GetLoginPortalCapabilities(ctx context.Context, requestedNAS string) (LoginPortalCapabilitiesResult, error) {
	name, client, err := s.loginPortalClient(ctx, requestedNAS)
	if err != nil {
		return LoginPortalCapabilitiesResult{}, err
	}
	capabilities, report, err := client.LoginPortalCapabilities(ctx)
	if err != nil {
		return LoginPortalCapabilitiesResult{}, authenticationError(name, err)
	}
	return LoginPortalCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) loginPortalClient(ctx context.Context, requestedNAS string) (string, loginPortalClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(loginPortalClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement login portal")
	}
	return name, client, nil
}

var _ loginPortalClient = (*synology.Client)(nil)
