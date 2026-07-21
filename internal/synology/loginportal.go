package synology

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/loginportal"
	lpops "github.com/ychiu1211/dsmctl/internal/synology/operations/loginportal"
)

type DSMWebService = loginportal.DSMWebService
type ApplicationPortals = loginportal.ApplicationPortals
type ApplicationPortal = loginportal.ApplicationPortal
type ReverseProxyRules = loginportal.ReverseProxyRules
type ReverseProxyRule = loginportal.ReverseProxyRule
type ReverseProxyEndpoint = loginportal.ReverseProxyEndpoint
type LoginPortalCapabilities = loginportal.Capabilities
type LoginPortalTransportInfo = loginportal.Transport
type DSMWebServiceChange = loginportal.DSMWebServiceChange
type ApplicationPortalChange = loginportal.ApplicationPortalChange
type ReverseProxyRuleCreate = loginportal.ReverseProxyRuleCreate
type ReverseProxyRuleDelete = loginportal.ReverseProxyRuleDelete
type ReverseProxyHeaderValue = lpops.ReverseProxyHeaderInput
type LoginPortalMutationResult = lpops.MutationResult

// DSMWebService reads the Control Panel > Login Portal > DSM tab settings (DSM
// ports, HTTPS, HTTP->HTTPS redirect, HSTS, HTTP/2, customized domain). Login
// Portal is DSM core, so the plain compatibility target is used. The customized
// external hostname is an independent sibling API: it is folded in only when
// present, and its absence never fails the DSM-access read.
func (c *Client) DSMWebService(ctx context.Context) (DSMWebService, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, lpops.APINames()...); err != nil {
		return DSMWebService{}, fmt.Errorf("prepare login portal target: %w", err)
	}
	settings, _, err := lpops.ExecuteDSMWebService(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return DSMWebService{}, fmt.Errorf("get DSM web service settings: %w", err)
	}
	c.target.AddCapability(lpops.DSMWebServiceReadCapabilityName)
	if lpops.SupportsExternalDomain(c.target) {
		external, _, err := lpops.ExecuteExternalDomain(ctx, c.target, lockedExecutor{client: c})
		if err != nil {
			return DSMWebService{}, fmt.Errorf("get DSM external domain settings: %w", err)
		}
		settings.ExternalDomainSupported = external.ExternalDomainSupported
		settings.ExternalHostname = external.ExternalHostname
		c.target.AddCapability(lpops.ExternalDomainReadCapabilityName)
	}
	return settings, nil
}

// ApplicationPortals reads the Login Portal > Applications tab: the per-app
// portal list.
func (c *Client) ApplicationPortals(ctx context.Context) (ApplicationPortals, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, lpops.APINames()...); err != nil {
		return ApplicationPortals{}, fmt.Errorf("prepare login portal target: %w", err)
	}
	portals, _, err := lpops.ExecuteApplicationPortals(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return ApplicationPortals{}, fmt.Errorf("get application portals: %w", err)
	}
	c.target.AddCapability(lpops.ApplicationPortalReadCapabilityName)
	return portals, nil
}

// ReverseProxyRules reads the Login Portal > Advanced tab: the reverse-proxy
// rule list. The list envelope and rule count are live-verified; per-rule fields
// are decoded leniently and never surface certificate key material or header
// values.
func (c *Client) ReverseProxyRules(ctx context.Context) (ReverseProxyRules, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, lpops.APINames()...); err != nil {
		return ReverseProxyRules{}, fmt.Errorf("prepare login portal target: %w", err)
	}
	rules, _, err := lpops.ExecuteReverseProxyRules(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return ReverseProxyRules{}, fmt.Errorf("get reverse proxy rules: %w", err)
	}
	c.target.AddCapability(lpops.ReverseProxyReadCapabilityName)
	return rules, nil
}

// LoginPortalCapabilities reports which Login Portal reads dsmctl exposes for the
// selected NAS, plus the discovered backends. Each area is an independent
// boundary: one being absent leaves the others usable.
func (c *Client) LoginPortalCapabilities(ctx context.Context) (LoginPortalCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, lpops.APINames()...); err != nil {
		return LoginPortalCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare login portal capabilities target: %w", err)
	}

	dsmWeb, err := selectSupported(lpops.SelectDSMWebService, c.target)
	if err != nil {
		return LoginPortalCapabilities{}, CompatibilityReport{}, fmt.Errorf("select DSM web service backend: %w", err)
	}
	externalDomain, err := selectSupported(lpops.SelectExternalDomain, c.target)
	if err != nil {
		return LoginPortalCapabilities{}, CompatibilityReport{}, fmt.Errorf("select external domain backend: %w", err)
	}
	appPortal, err := selectSupported(lpops.SelectApplicationPortal, c.target)
	if err != nil {
		return LoginPortalCapabilities{}, CompatibilityReport{}, fmt.Errorf("select application portal backend: %w", err)
	}
	reverseProxy, err := selectSupported(lpops.SelectReverseProxy, c.target)
	if err != nil {
		return LoginPortalCapabilities{}, CompatibilityReport{}, fmt.Errorf("select reverse proxy backend: %w", err)
	}
	dsmWebSet, err := selectSupported(lpops.SelectDSMWebServiceSet, c.target)
	if err != nil {
		return LoginPortalCapabilities{}, CompatibilityReport{}, fmt.Errorf("select DSM web service write backend: %w", err)
	}
	externalDomainSet, err := selectSupported(lpops.SelectExternalDomainSet, c.target)
	if err != nil {
		return LoginPortalCapabilities{}, CompatibilityReport{}, fmt.Errorf("select external domain write backend: %w", err)
	}
	appPortalSet, err := selectSupported(lpops.SelectApplicationPortalSet, c.target)
	if err != nil {
		return LoginPortalCapabilities{}, CompatibilityReport{}, fmt.Errorf("select application portal write backend: %w", err)
	}
	reverseProxyWrite, err := selectSupported(lpops.SelectReverseProxyWrite, c.target)
	if err != nil {
		return LoginPortalCapabilities{}, CompatibilityReport{}, fmt.Errorf("select reverse proxy write backend: %w", err)
	}

	if dsmWeb.Supported {
		c.target.AddCapability(lpops.DSMWebServiceReadCapabilityName)
	}
	if externalDomain.Supported {
		c.target.AddCapability(lpops.ExternalDomainReadCapabilityName)
	}
	if appPortal.Supported {
		c.target.AddCapability(lpops.ApplicationPortalReadCapabilityName)
	}
	if reverseProxy.Supported {
		c.target.AddCapability(lpops.ReverseProxyReadCapabilityName)
	}
	if dsmWebSet.Supported {
		c.target.AddCapability(lpops.DSMWebServiceWriteCapabilityName)
	}
	if externalDomainSet.Supported {
		c.target.AddCapability(lpops.ExternalDomainWriteCapabilityName)
	}
	if appPortalSet.Supported {
		c.target.AddCapability(lpops.ApplicationPortalWriteCapabilityName)
	}
	if reverseProxyWrite.Supported {
		c.target.AddCapability(lpops.ReverseProxyWriteCapabilityName)
	}

	capabilities := LoginPortalCapabilities{
		Module:                 loginportal.ModuleName,
		DSMWebServiceRead:      dsmWeb.Supported,
		ExternalDomainRead:     externalDomain.Supported,
		ApplicationPortalRead:  appPortal.Supported,
		ReverseProxyRead:       reverseProxy.Supported,
		DSMWebServiceWrite:     dsmWebSet.Supported,
		ExternalDomainWrite:    externalDomainSet.Supported,
		ApplicationPortalWrite: appPortalSet.Supported,
		ReverseProxyWrite:      reverseProxyWrite.Supported,
		Mutations:              dsmWebSet.Supported || externalDomainSet.Supported || appPortalSet.Supported || reverseProxyWrite.Supported,
	}
	return capabilities, c.target.Report(dsmWeb, externalDomain, appPortal, reverseProxy, dsmWebSet, externalDomainSet, appPortalSet, reverseProxyWrite), nil
}

// LoginPortalTransport reports the scheme and port dsmctl is currently connected
// to DSM on. It is the ground truth for the never-break-the-current-session guard
// (the DSM-access write must not sever this transport without an override). The
// base URL is immutable after client construction, so no lock is needed.
func (c *Client) LoginPortalTransport() LoginPortalTransportInfo {
	scheme := strings.ToLower(c.baseURL.Scheme)
	port := 0
	if p := c.baseURL.Port(); p != "" {
		port, _ = strconv.Atoi(p)
	}
	if port == 0 {
		if scheme == "https" {
			port = 443
		} else {
			port = 80
		}
	}
	return LoginPortalTransportInfo{Scheme: scheme, Port: port}
}

// ApplyDSMWebServiceChange merges the patch into a freshly read complete DSM
// web-service state and submits it as one set, so a field the caller did not
// specify can never be silently reset. A customized external hostname is a sibling
// API written only when the change touches it and the API is present. dsmctl's own
// transport survival is enforced by the application-layer guard before this runs.
func (c *Client) ApplyDSMWebServiceChange(ctx context.Context, change DSMWebServiceChange) (LoginPortalMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, lpops.APINames()...); err != nil {
		return LoginPortalMutationResult{}, fmt.Errorf("prepare login portal mutation target: %w", err)
	}
	current, _, err := lpops.ExecuteDSMWebService(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return LoginPortalMutationResult{}, fmt.Errorf("refresh DSM web service before apply: %w", err)
	}
	if lpops.SupportsExternalDomain(c.target) {
		external, _, err := lpops.ExecuteExternalDomain(ctx, c.target, lockedExecutor{client: c})
		if err != nil {
			return LoginPortalMutationResult{}, fmt.Errorf("refresh DSM external hostname before apply: %w", err)
		}
		current.ExternalHostname = external.ExternalHostname
		current.ExternalDomainSupported = external.ExternalDomainSupported
	}
	desired := mergeDSMWebServiceChange(current, change)
	result, _, err := lpops.ExecuteDSMWebServiceSet(ctx, c.target, lockedExecutor{client: c}, desired)
	if err != nil {
		return LoginPortalMutationResult{}, fmt.Errorf("apply DSM web service settings: %w", err)
	}
	c.target.AddCapability(lpops.DSMWebServiceWriteCapabilityName)
	if change.ExternalHostname != nil {
		if !lpops.SupportsExternalDomain(c.target) {
			return LoginPortalMutationResult{}, fmt.Errorf("this NAS does not expose the customized external-hostname API")
		}
		if _, _, err := lpops.ExecuteExternalDomainSet(ctx, c.target, lockedExecutor{client: c}, lpops.ExternalDomainSetInput{Hostname: *change.ExternalHostname}); err != nil {
			return LoginPortalMutationResult{}, fmt.Errorf("apply DSM external hostname: %w", err)
		}
		c.target.AddCapability(lpops.ExternalDomainWriteCapabilityName)
	}
	return result, nil
}

// ApplyApplicationPortalChange merges the patch into the freshly read portal for
// the target app and submits it as one set, so sibling fields are preserved.
func (c *Client) ApplyApplicationPortalChange(ctx context.Context, change ApplicationPortalChange) (LoginPortalMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, lpops.APINames()...); err != nil {
		return LoginPortalMutationResult{}, fmt.Errorf("prepare login portal mutation target: %w", err)
	}
	portals, _, err := lpops.ExecuteApplicationPortals(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return LoginPortalMutationResult{}, fmt.Errorf("refresh application portals before apply: %w", err)
	}
	current, found := findApplicationPortal(portals, change.AppID)
	if !found {
		return LoginPortalMutationResult{}, fmt.Errorf("application %q is not present in the portal list", change.AppID)
	}
	desired := mergeApplicationPortalChange(current, change)
	result, _, err := lpops.ExecuteApplicationPortalSet(ctx, c.target, lockedExecutor{client: c}, desired)
	if err != nil {
		return LoginPortalMutationResult{}, fmt.Errorf("apply application portal: %w", err)
	}
	c.target.AddCapability(lpops.ApplicationPortalWriteCapabilityName)
	return result, nil
}

// ApplyReverseProxyRuleCreate creates one reverse-proxy rule. Header secrets are
// resolved by the caller and passed as headers; they never enter the plan or hash.
func (c *Client) ApplyReverseProxyRuleCreate(ctx context.Context, create ReverseProxyRuleCreate, headers []ReverseProxyHeaderValue) (LoginPortalMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, lpops.APINames()...); err != nil {
		return LoginPortalMutationResult{}, fmt.Errorf("prepare login portal mutation target: %w", err)
	}
	in := lpops.ReverseProxyCreateInput{
		Description:      create.Description,
		FrontendProtocol: create.Frontend.Protocol,
		FrontendHostname: create.Frontend.Hostname,
		FrontendPort:     create.Frontend.Port,
		FrontendHSTS:     create.FrontendHSTS,
		BackendProtocol:  create.Backend.Protocol,
		BackendHostname:  create.Backend.Hostname,
		BackendPort:      create.Backend.Port,
		Headers:          headers,
	}
	result, _, err := lpops.ExecuteReverseProxyCreate(ctx, c.target, lockedExecutor{client: c}, in)
	if err != nil {
		return LoginPortalMutationResult{}, fmt.Errorf("create reverse proxy rule: %w", err)
	}
	c.target.AddCapability(lpops.ReverseProxyWriteCapabilityName)
	return result, nil
}

// ApplyReverseProxyRuleDelete deletes one reverse-proxy rule by uuid.
func (c *Client) ApplyReverseProxyRuleDelete(ctx context.Context, del ReverseProxyRuleDelete) (LoginPortalMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, lpops.APINames()...); err != nil {
		return LoginPortalMutationResult{}, fmt.Errorf("prepare login portal mutation target: %w", err)
	}
	result, _, err := lpops.ExecuteReverseProxyDelete(ctx, c.target, lockedExecutor{client: c}, lpops.ReverseProxyDeleteInput{UUIDs: []string{del.UUID}})
	if err != nil {
		return LoginPortalMutationResult{}, fmt.Errorf("delete reverse proxy rule: %w", err)
	}
	c.target.AddCapability(lpops.ReverseProxyWriteCapabilityName)
	return result, nil
}

func mergeDSMWebServiceChange(current DSMWebService, change DSMWebServiceChange) lpops.DSMWebServiceSetInput {
	desired := lpops.DSMWebServiceSetInput{
		HTTPPort:            current.HTTPPort,
		HTTPSPort:           current.HTTPSPort,
		HTTPSEnabled:        current.HTTPSEnabled,
		HTTPRedirectEnabled: current.HTTPRedirectEnabled,
		HSTSEnabled:         current.HSTSEnabled,
		HTTP2Enabled:        current.HTTP2Enabled,
		CustomDomainEnabled: current.CustomDomainEnabled,
		CustomDomain:        current.CustomDomain,
	}
	if change.HTTPPort != nil {
		desired.HTTPPort = *change.HTTPPort
	}
	if change.HTTPSPort != nil {
		desired.HTTPSPort = *change.HTTPSPort
	}
	if change.HTTPSEnabled != nil {
		desired.HTTPSEnabled = *change.HTTPSEnabled
	}
	if change.HTTPRedirectEnabled != nil {
		desired.HTTPRedirectEnabled = *change.HTTPRedirectEnabled
	}
	if change.HSTSEnabled != nil {
		desired.HSTSEnabled = *change.HSTSEnabled
	}
	if change.HTTP2Enabled != nil {
		desired.HTTP2Enabled = *change.HTTP2Enabled
	}
	if change.CustomDomainEnabled != nil {
		desired.CustomDomainEnabled = *change.CustomDomainEnabled
	}
	if change.CustomDomain != nil {
		desired.CustomDomain = *change.CustomDomain
	}
	return desired
}

func mergeApplicationPortalChange(current loginportal.ApplicationPortal, change ApplicationPortalChange) lpops.ApplicationPortalSetInput {
	desired := lpops.ApplicationPortalSetInput{
		AppID:         current.AppID,
		RedirectHTTPS: current.RedirectHTTPS,
		Alias:         current.Alias,
		HTTPPort:      current.HTTPPort,
		HTTPSPort:     current.HTTPSPort,
	}
	if change.RedirectHTTPS != nil {
		desired.RedirectHTTPS = *change.RedirectHTTPS
	}
	if change.Alias != nil {
		desired.Alias = *change.Alias
	}
	if change.HTTPPort != nil {
		desired.HTTPPort = *change.HTTPPort
	}
	if change.HTTPSPort != nil {
		desired.HTTPSPort = *change.HTTPSPort
	}
	return desired
}

func findApplicationPortal(portals ApplicationPortals, appID string) (loginportal.ApplicationPortal, bool) {
	for _, portal := range portals.Portals {
		if portal.AppID == appID {
			return portal, true
		}
	}
	return loginportal.ApplicationPortal{}, false
}
