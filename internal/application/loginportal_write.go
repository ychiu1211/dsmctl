package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/loginportal"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const loginPortalAPIVersion = "dsmctl.io/v1alpha1"

// The Login Portal guarded writes follow the module plan/apply contract: the plan
// records and hashes the complete observed state, apply re-reads and rejects a
// changed state, merges the patch into a freshly read config, performs the typed
// set/create/delete, and re-reads to verify the requested fields took effect.
//
// Risk model (from the work item): every DSM web-service change (port, HTTPS
// enable, HTTP->HTTPS redirect, HSTS, HTTP/2, customized domain/hostname) is HIGH
// because it changes how DSM itself is reached; an application-portal change and a
// reverse-proxy create/delete are medium.
//
// The never-break-the-current-session guard is the defining safeguard: dsmctl
// reaches DSM over one transport (scheme + port). A DSM-access change that would
// sever that transport — disabling HTTPS or moving the HTTPS port dsmctl is on,
// moving the HTTP port when on HTTP, forcing a redirect that bounces the current
// HTTP session, or enabling HSTS (browsers cache it) — is refused by default and
// requires an explicit AllowConnectivityBreak. The observed transport is hashed
// into the plan, so a later reconnection on a different endpoint makes a stale
// plan fail.

// ---- DSM web service ---------------------------------------------------------

// DSMWebServiceObserved bundles the DSM-access settings with the transport dsmctl
// is currently connected on. Both are hashed into the plan: the transport so the
// never-break guard's decision cannot be silently invalidated, and so a new
// connection endpoint invalidates a stale plan.
type DSMWebServiceObserved struct {
	Settings  synology.DSMWebService           `json:"settings" jsonschema:"Complete DSM web-service access settings observed during planning"`
	Transport synology.LoginPortalTransportInfo `json:"transport" jsonschema:"Scheme and port dsmctl is currently connected to DSM on"`
}

type DSMWebServicePlan struct {
	APIVersion          string                          `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                          `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                          `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             loginportal.DSMWebServiceChange `json:"request" jsonschema:"Validated patch-only DSM web-service intent"`
	Observed            DSMWebServiceObserved           `json:"observed" jsonschema:"Complete DSM web-service settings and current transport observed during planning"`
	ObservedFingerprint string                          `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed state"`
	Risk                string                          `json:"risk" jsonschema:"Plan risk level (always high for a DSM web-service change)"`
	Warnings            []string                        `json:"warnings" jsonschema:"Connectivity and posture warnings"`
	Summary             []string                        `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                          `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

type LoginPortalApplyResult struct {
	NAS       string                            `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash  string                            `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied   bool                              `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Operation synology.LoginPortalMutationResult `json:"operation" jsonschema:"Selected DSM mutation backend"`
}

func (s *Service) PlanDSMWebServiceChange(ctx context.Context, requestedNAS string, request loginportal.DSMWebServiceChange) (DSMWebServicePlan, error) {
	if err := validateDSMWebServiceShape(request); err != nil {
		return DSMWebServicePlan{}, err
	}
	name, client, err := s.loginPortalClient(ctx, requestedNAS)
	if err != nil {
		return DSMWebServicePlan{}, err
	}
	plan, err := planDSMWebServiceWithClient(ctx, name, client, request)
	if err != nil {
		return DSMWebServicePlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = dsmWebServicePlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyDSMWebServicePlan(ctx context.Context, plan DSMWebServicePlan, approvalHash string) (LoginPortalApplyResult, error) {
	if err := validateDSMWebServicePlan(plan, approvalHash); err != nil {
		return LoginPortalApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return LoginPortalApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return LoginPortalApplyResult{}, err
	}
	name, client, err := s.loginPortalClient(ctx, plan.NAS)
	if err != nil {
		return LoginPortalApplyResult{}, err
	}
	if name != plan.NAS {
		return LoginPortalApplyResult{}, fmt.Errorf("DSM web service plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyDSMWebServiceWithClient(ctx, client, plan)
}

func planDSMWebServiceWithClient(ctx context.Context, nas string, client loginPortalClient, request loginportal.DSMWebServiceChange) (DSMWebServicePlan, error) {
	capabilities, _, err := client.LoginPortalCapabilities(ctx)
	if err != nil {
		return DSMWebServicePlan{}, authenticationError(nas, err)
	}
	if !capabilities.DSMWebServiceRead || !capabilities.DSMWebServiceWrite {
		return DSMWebServicePlan{}, fmt.Errorf("NAS %q does not expose a verified DSM web-service read/write backend", nas)
	}
	if request.ExternalHostname != nil && !capabilities.ExternalDomainWrite {
		return DSMWebServicePlan{}, fmt.Errorf("NAS %q does not expose the customized external-hostname write backend", nas)
	}
	state, err := client.DSMWebService(ctx)
	if err != nil {
		return DSMWebServicePlan{}, authenticationError(nas, err)
	}
	if dsmWebServiceSatisfied(state, request) {
		return DSMWebServicePlan{}, fmt.Errorf("DSM web-service patch would not change the current configuration")
	}
	transport := client.LoginPortalTransport()
	breaks := dsmConnectivityBreaks(state, transport, request)
	if len(breaks) > 0 && !request.AllowConnectivityBreak {
		return DSMWebServicePlan{}, fmt.Errorf("this change would sever the transport dsmctl is connected on (%s port %d): %s; re-plan with allow_connectivity_break set (and an out-of-band recovery path ready) to proceed",
			transport.Scheme, transport.Port, strings.Join(breaks, "; "))
	}
	observed := DSMWebServiceObserved{Settings: state, Transport: transport}
	plan := DSMWebServicePlan{APIVersion: loginPortalAPIVersion, NAS: nas, Request: request, Observed: observed}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return DSMWebServicePlan{}, err
	}
	plan.Risk, plan.Warnings, plan.Summary = dsmWebServiceEffects(state, request, breaks)
	plan.Hash, err = dsmWebServicePlanHash(plan)
	if err != nil {
		return DSMWebServicePlan{}, err
	}
	return plan, nil
}

func applyDSMWebServiceWithClient(ctx context.Context, client loginPortalClient, plan DSMWebServicePlan) (LoginPortalApplyResult, error) {
	current, err := planDSMWebServiceWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return LoginPortalApplyResult{}, fmt.Errorf("DSM web service plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = dsmWebServicePlanHash(current)
	if err != nil {
		return LoginPortalApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return LoginPortalApplyResult{}, fmt.Errorf("DSM web service plan is stale; create a new plan")
	}
	operation, err := client.ApplyDSMWebServiceChange(ctx, plan.Request)
	if err != nil {
		return LoginPortalApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifyDSMWebServicePostcondition(ctx, client, plan.Request); err != nil {
		return LoginPortalApplyResult{}, fmt.Errorf("verify DSM web service change: %w", err)
	}
	return LoginPortalApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operation: operation}, nil
}

func validateDSMWebServiceShape(change loginportal.DSMWebServiceChange) error {
	if change.IsEmpty() {
		return fmt.Errorf("DSM web-service patch has no fields")
	}
	for _, p := range []struct {
		value *int
		name  string
	}{{change.HTTPPort, "http_port"}, {change.HTTPSPort, "https_port"}} {
		if p.value != nil && (*p.value < 1 || *p.value > 65535) {
			return fmt.Errorf("%s %d must be between 1 and 65535", p.name, *p.value)
		}
	}
	if change.HTTPPort != nil && change.HTTPSPort != nil && *change.HTTPPort == *change.HTTPSPort {
		return fmt.Errorf("http_port and https_port must differ")
	}
	if change.CustomDomainEnabled != nil && *change.CustomDomainEnabled {
		// enabling a customized domain needs a domain, either supplied now or
		// already configured; the plan's no-op/postcondition checks handle the rest.
		if change.CustomDomain != nil && strings.TrimSpace(*change.CustomDomain) == "" {
			return fmt.Errorf("custom_domain must not be empty when custom_domain_enabled is true")
		}
	}
	return nil
}

func dsmWebServiceSatisfied(state synology.DSMWebService, change loginportal.DSMWebServiceChange) bool {
	return (change.HTTPPort == nil || state.HTTPPort == *change.HTTPPort) &&
		(change.HTTPSPort == nil || state.HTTPSPort == *change.HTTPSPort) &&
		(change.HTTPSEnabled == nil || state.HTTPSEnabled == *change.HTTPSEnabled) &&
		(change.HTTPRedirectEnabled == nil || state.HTTPRedirectEnabled == *change.HTTPRedirectEnabled) &&
		(change.HSTSEnabled == nil || state.HSTSEnabled == *change.HSTSEnabled) &&
		(change.HTTP2Enabled == nil || state.HTTP2Enabled == *change.HTTP2Enabled) &&
		(change.CustomDomainEnabled == nil || state.CustomDomainEnabled == *change.CustomDomainEnabled) &&
		(change.CustomDomain == nil || state.CustomDomain == *change.CustomDomain) &&
		(change.ExternalHostname == nil || state.ExternalHostname == *change.ExternalHostname)
}

// dsmConnectivityBreaks lists the ways a change would sever the transport dsmctl
// is currently connected on. A non-empty result requires AllowConnectivityBreak.
func dsmConnectivityBreaks(state synology.DSMWebService, transport synology.LoginPortalTransportInfo, change loginportal.DSMWebServiceChange) []string {
	var breaks []string
	https := strings.EqualFold(transport.Scheme, "https")
	if https && change.HTTPSEnabled != nil && !*change.HTTPSEnabled {
		breaks = append(breaks, fmt.Sprintf("disabling HTTPS severs the current HTTPS session on port %d", transport.Port))
	}
	if https && change.HTTPSPort != nil && *change.HTTPSPort != transport.Port {
		breaks = append(breaks, fmt.Sprintf("moving the HTTPS port from %d to %d severs the current session", transport.Port, *change.HTTPSPort))
	}
	if !https && change.HTTPPort != nil && *change.HTTPPort != transport.Port {
		breaks = append(breaks, fmt.Sprintf("moving the HTTP port from %d to %d severs the current session", transport.Port, *change.HTTPPort))
	}
	if !https && change.HTTPRedirectEnabled != nil && *change.HTTPRedirectEnabled {
		breaks = append(breaks, "forcing HTTP->HTTPS redirect bounces the current HTTP session to HTTPS")
	}
	if change.HSTSEnabled != nil && *change.HSTSEnabled && !state.HSTSEnabled {
		breaks = append(breaks, "enabling HSTS is cached by browsers and can prevent falling back to HTTP")
	}
	return breaks
}

func dsmWebServiceEffects(state synology.DSMWebService, change loginportal.DSMWebServiceChange, breaks []string) (string, []string, []string) {
	warnings := []string{}
	summary := []string{}
	if change.HTTPPort != nil && *change.HTTPPort != state.HTTPPort {
		summary = append(summary, fmt.Sprintf("change the HTTP port from %d to %d", state.HTTPPort, *change.HTTPPort))
	}
	if change.HTTPSPort != nil && *change.HTTPSPort != state.HTTPSPort {
		summary = append(summary, fmt.Sprintf("change the HTTPS port from %d to %d", state.HTTPSPort, *change.HTTPSPort))
	}
	if change.HTTPSEnabled != nil && *change.HTTPSEnabled != state.HTTPSEnabled {
		if *change.HTTPSEnabled {
			summary = append(summary, "enable HTTPS")
		} else {
			summary = append(summary, "disable HTTPS")
			warnings = append(warnings, "disabling HTTPS removes encrypted access to DSM, weakening the security posture")
		}
	}
	if change.HTTPRedirectEnabled != nil && *change.HTTPRedirectEnabled != state.HTTPRedirectEnabled {
		summary = append(summary, fmt.Sprintf("set HTTP->HTTPS redirect to %t", *change.HTTPRedirectEnabled))
	}
	if change.HSTSEnabled != nil && *change.HSTSEnabled != state.HSTSEnabled {
		summary = append(summary, fmt.Sprintf("set HSTS to %t", *change.HSTSEnabled))
	}
	if change.HTTP2Enabled != nil && *change.HTTP2Enabled != state.HTTP2Enabled {
		summary = append(summary, fmt.Sprintf("set HTTP/2 to %t", *change.HTTP2Enabled))
	}
	if change.CustomDomainEnabled != nil && *change.CustomDomainEnabled != state.CustomDomainEnabled {
		summary = append(summary, fmt.Sprintf("set customized domain to %t", *change.CustomDomainEnabled))
	}
	if change.CustomDomain != nil && *change.CustomDomain != state.CustomDomain {
		summary = append(summary, fmt.Sprintf("set the customized domain to %q", *change.CustomDomain))
	}
	if change.ExternalHostname != nil && *change.ExternalHostname != state.ExternalHostname {
		summary = append(summary, fmt.Sprintf("set the external hostname to %q", *change.ExternalHostname))
	}
	if change.AllowConnectivityBreak {
		for _, b := range breaks {
			warnings = append(warnings, b+" (connectivity break acknowledged; an out-of-band recovery path is required)")
		}
	}
	// Every DSM web-service change is high risk: it changes how DSM is reached.
	return "high", warnings, summary
}

func verifyDSMWebServicePostcondition(ctx context.Context, client loginPortalClient, change loginportal.DSMWebServiceChange) error {
	state, err := client.DSMWebService(ctx)
	if err != nil {
		return err
	}
	checks := []struct {
		mismatch bool
		msg      string
	}{
		{change.HTTPPort != nil && state.HTTPPort != *change.HTTPPort, fmt.Sprintf("http_port is %d, want %v", state.HTTPPort, deref(change.HTTPPort))},
		{change.HTTPSPort != nil && state.HTTPSPort != *change.HTTPSPort, fmt.Sprintf("https_port is %d, want %v", state.HTTPSPort, deref(change.HTTPSPort))},
		{change.HTTPSEnabled != nil && state.HTTPSEnabled != *change.HTTPSEnabled, fmt.Sprintf("https_enabled is %t, want %v", state.HTTPSEnabled, derefBool(change.HTTPSEnabled))},
		{change.HTTPRedirectEnabled != nil && state.HTTPRedirectEnabled != *change.HTTPRedirectEnabled, fmt.Sprintf("http_redirect_enabled is %t, want %v", state.HTTPRedirectEnabled, derefBool(change.HTTPRedirectEnabled))},
		{change.HSTSEnabled != nil && state.HSTSEnabled != *change.HSTSEnabled, fmt.Sprintf("hsts_enabled is %t, want %v", state.HSTSEnabled, derefBool(change.HSTSEnabled))},
		{change.HTTP2Enabled != nil && state.HTTP2Enabled != *change.HTTP2Enabled, fmt.Sprintf("http2_enabled is %t, want %v", state.HTTP2Enabled, derefBool(change.HTTP2Enabled))},
		{change.CustomDomainEnabled != nil && state.CustomDomainEnabled != *change.CustomDomainEnabled, fmt.Sprintf("custom_domain_enabled is %t, want %v", state.CustomDomainEnabled, derefBool(change.CustomDomainEnabled))},
		{change.CustomDomain != nil && state.CustomDomain != *change.CustomDomain, fmt.Sprintf("custom_domain is %q, want %v", state.CustomDomain, derefStr(change.CustomDomain))},
		{change.ExternalHostname != nil && state.ExternalHostname != *change.ExternalHostname, fmt.Sprintf("external_hostname is %q, want %v", state.ExternalHostname, derefStr(change.ExternalHostname))},
	}
	for _, c := range checks {
		if c.mismatch {
			return fmt.Errorf("%s", c.msg)
		}
	}
	return nil
}

func validateDSMWebServicePlan(plan DSMWebServicePlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the DSM web service plan")
	}
	if plan.APIVersion != loginPortalAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid DSM web service plan metadata")
	}
	if err := validateDSMWebServiceShape(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("DSM web service plan observed state was modified")
	}
	expectedHash, err := dsmWebServicePlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("DSM web service plan contents were modified after planning")
	}
	return nil
}

func dsmWebServicePlanHash(plan DSMWebServicePlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

// ---- Application portal ------------------------------------------------------

type ApplicationPortalPlan struct {
	APIVersion          string                             `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                             `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                             `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             loginportal.ApplicationPortalChange `json:"request" jsonschema:"Validated patch-only application portal intent"`
	Observed            synology.ApplicationPortal          `json:"observed" jsonschema:"The target application's complete portal observed during planning"`
	ObservedFingerprint string                             `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the observed portal"`
	Risk                string                             `json:"risk" jsonschema:"Plan risk level: medium"`
	Warnings            []string                           `json:"warnings" jsonschema:"External-exposure warnings"`
	Summary             []string                           `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                             `json:"hash" jsonschema:"SHA-256 approval hash covering intent and observed portal"`
}

func (s *Service) PlanApplicationPortalChange(ctx context.Context, requestedNAS string, request loginportal.ApplicationPortalChange) (ApplicationPortalPlan, error) {
	if err := validateApplicationPortalShape(request); err != nil {
		return ApplicationPortalPlan{}, err
	}
	name, client, err := s.loginPortalClient(ctx, requestedNAS)
	if err != nil {
		return ApplicationPortalPlan{}, err
	}
	plan, err := planApplicationPortalWithClient(ctx, name, client, request)
	if err != nil {
		return ApplicationPortalPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = applicationPortalPlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyApplicationPortalPlan(ctx context.Context, plan ApplicationPortalPlan, approvalHash string) (LoginPortalApplyResult, error) {
	if err := validateApplicationPortalPlan(plan, approvalHash); err != nil {
		return LoginPortalApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return LoginPortalApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return LoginPortalApplyResult{}, err
	}
	name, client, err := s.loginPortalClient(ctx, plan.NAS)
	if err != nil {
		return LoginPortalApplyResult{}, err
	}
	if name != plan.NAS {
		return LoginPortalApplyResult{}, fmt.Errorf("application portal plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyApplicationPortalWithClient(ctx, client, plan)
}

func planApplicationPortalWithClient(ctx context.Context, nas string, client loginPortalClient, request loginportal.ApplicationPortalChange) (ApplicationPortalPlan, error) {
	capabilities, _, err := client.LoginPortalCapabilities(ctx)
	if err != nil {
		return ApplicationPortalPlan{}, authenticationError(nas, err)
	}
	if !capabilities.ApplicationPortalRead || !capabilities.ApplicationPortalWrite {
		return ApplicationPortalPlan{}, fmt.Errorf("NAS %q does not expose a verified application-portal read/write backend", nas)
	}
	portals, err := client.ApplicationPortals(ctx)
	if err != nil {
		return ApplicationPortalPlan{}, authenticationError(nas, err)
	}
	current, found := findPortal(portals, request.AppID)
	if !found {
		return ApplicationPortalPlan{}, fmt.Errorf("application %q is not present in the portal list", request.AppID)
	}
	if applicationPortalSatisfied(current, request) {
		return ApplicationPortalPlan{}, fmt.Errorf("application portal patch would not change the current configuration")
	}
	plan := ApplicationPortalPlan{APIVersion: loginPortalAPIVersion, NAS: nas, Request: request, Observed: current}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return ApplicationPortalPlan{}, err
	}
	plan.Risk, plan.Warnings, plan.Summary = applicationPortalEffects(current, request)
	plan.Hash, err = applicationPortalPlanHash(plan)
	if err != nil {
		return ApplicationPortalPlan{}, err
	}
	return plan, nil
}

func applyApplicationPortalWithClient(ctx context.Context, client loginPortalClient, plan ApplicationPortalPlan) (LoginPortalApplyResult, error) {
	current, err := planApplicationPortalWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return LoginPortalApplyResult{}, fmt.Errorf("application portal plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = applicationPortalPlanHash(current)
	if err != nil {
		return LoginPortalApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return LoginPortalApplyResult{}, fmt.Errorf("application portal plan is stale; create a new plan")
	}
	operation, err := client.ApplyApplicationPortalChange(ctx, plan.Request)
	if err != nil {
		return LoginPortalApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifyApplicationPortalPostcondition(ctx, client, plan.Request); err != nil {
		return LoginPortalApplyResult{}, fmt.Errorf("verify application portal change: %w", err)
	}
	return LoginPortalApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operation: operation}, nil
}

func validateApplicationPortalShape(change loginportal.ApplicationPortalChange) error {
	if strings.TrimSpace(change.AppID) == "" {
		return fmt.Errorf("application portal change requires an app_id")
	}
	if change.IsEmpty() {
		return fmt.Errorf("application portal patch has no changeable fields")
	}
	// DSM's AppPortal.set rejects an empty alias (live-verified: code 4100), so it
	// cannot clear an alias this way. Refuse an explicit clear with a clear message
	// rather than silently no-op it.
	if change.Alias != nil && strings.TrimSpace(*change.Alias) == "" {
		return fmt.Errorf("clearing an application portal alias is not supported (DSM's AppPortal.set rejects an empty alias); to set a different alias supply a non-empty value")
	}
	for _, p := range []struct {
		value *int
		name  string
	}{{change.HTTPPort, "http_port"}, {change.HTTPSPort, "https_port"}} {
		if p.value != nil && (*p.value < 1 || *p.value > 65535) {
			return fmt.Errorf("%s %d must be between 1 and 65535", p.name, *p.value)
		}
	}
	return nil
}

func applicationPortalSatisfied(current synology.ApplicationPortal, change loginportal.ApplicationPortalChange) bool {
	return (change.RedirectHTTPS == nil || current.RedirectHTTPS == *change.RedirectHTTPS) &&
		(change.Alias == nil || current.Alias == *change.Alias) &&
		(change.HTTPPort == nil || current.HTTPPort == *change.HTTPPort) &&
		(change.HTTPSPort == nil || current.HTTPSPort == *change.HTTPSPort)
}

func applicationPortalEffects(current synology.ApplicationPortal, change loginportal.ApplicationPortalChange) (string, []string, []string) {
	warnings := []string{}
	summary := []string{}
	if change.RedirectHTTPS != nil && *change.RedirectHTTPS != current.RedirectHTTPS {
		summary = append(summary, fmt.Sprintf("set HTTP->HTTPS redirect to %t for %s", *change.RedirectHTTPS, current.AppID))
	}
	if change.Alias != nil && *change.Alias != current.Alias {
		summary = append(summary, fmt.Sprintf("set the portal alias to %q", *change.Alias))
		if strings.TrimSpace(*change.Alias) != "" {
			warnings = append(warnings, fmt.Sprintf("a portal alias publishes %s at /%s, changing how (and whether) it is externally reachable", current.AppID, strings.TrimSpace(*change.Alias)))
		}
	}
	if change.HTTPPort != nil && *change.HTTPPort != current.HTTPPort {
		summary = append(summary, fmt.Sprintf("set the portal HTTP port to %d", *change.HTTPPort))
		warnings = append(warnings, fmt.Sprintf("a custom portal port publishes %s on a new port, changing its exposure", current.AppID))
	}
	if change.HTTPSPort != nil && *change.HTTPSPort != current.HTTPSPort {
		summary = append(summary, fmt.Sprintf("set the portal HTTPS port to %d", *change.HTTPSPort))
		warnings = append(warnings, fmt.Sprintf("a custom portal port publishes %s on a new port, changing its exposure", current.AppID))
	}
	return "medium", warnings, summary
}

func verifyApplicationPortalPostcondition(ctx context.Context, client loginPortalClient, change loginportal.ApplicationPortalChange) error {
	portals, err := client.ApplicationPortals(ctx)
	if err != nil {
		return err
	}
	current, found := findPortal(portals, change.AppID)
	if !found {
		return fmt.Errorf("application %q disappeared from the portal list", change.AppID)
	}
	if change.RedirectHTTPS != nil && current.RedirectHTTPS != *change.RedirectHTTPS {
		return fmt.Errorf("redirect_https is %t, want %t", current.RedirectHTTPS, *change.RedirectHTTPS)
	}
	if change.Alias != nil && current.Alias != *change.Alias {
		return fmt.Errorf("alias is %q, want %q", current.Alias, *change.Alias)
	}
	if change.HTTPPort != nil && current.HTTPPort != *change.HTTPPort {
		return fmt.Errorf("http_port is %d, want %d", current.HTTPPort, *change.HTTPPort)
	}
	if change.HTTPSPort != nil && current.HTTPSPort != *change.HTTPSPort {
		return fmt.Errorf("https_port is %d, want %d", current.HTTPSPort, *change.HTTPSPort)
	}
	return nil
}

func validateApplicationPortalPlan(plan ApplicationPortalPlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the application portal plan")
	}
	if plan.APIVersion != loginPortalAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid application portal plan metadata")
	}
	if err := validateApplicationPortalShape(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("application portal plan observed state was modified")
	}
	expectedHash, err := applicationPortalPlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("application portal plan contents were modified after planning")
	}
	return nil
}

func applicationPortalPlanHash(plan ApplicationPortalPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

func findPortal(portals synology.ApplicationPortals, appID string) (synology.ApplicationPortal, bool) {
	for _, portal := range portals.Portals {
		if portal.AppID == appID {
			return portal, true
		}
	}
	return synology.ApplicationPortal{}, false
}

// ---- Reverse proxy -----------------------------------------------------------

const (
	reverseProxyActionCreate = "create"
	reverseProxyActionDelete = "delete"
)

type ReverseProxyPlan struct {
	APIVersion          string                              `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                              `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                              `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Action              string                              `json:"action" jsonschema:"create or delete"`
	Create              *loginportal.ReverseProxyRuleCreate `json:"create,omitempty" jsonschema:"The rule to create (create action); header secrets stay in credential_ref, never resolved into the plan"`
	Delete              *loginportal.ReverseProxyRuleDelete `json:"delete,omitempty" jsonschema:"The rule uuid to delete (delete action)"`
	Observed            synology.ReverseProxyRules          `json:"observed" jsonschema:"The COMPLETE reverse-proxy rule set observed during planning (whole-set fingerprint)"`
	ObservedFingerprint string                              `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed rule set and intent"`
	Risk                string                              `json:"risk" jsonschema:"Plan risk level: medium"`
	Warnings            []string                            `json:"warnings" jsonschema:"External-exposure warnings"`
	Summary             []string                            `json:"summary" jsonschema:"Human-readable operation"`
	Hash                string                              `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed rule set"`
}

func (s *Service) PlanReverseProxyCreate(ctx context.Context, requestedNAS string, create loginportal.ReverseProxyRuleCreate) (ReverseProxyPlan, error) {
	if err := validateReverseProxyCreateShape(create); err != nil {
		return ReverseProxyPlan{}, err
	}
	name, client, err := s.loginPortalClient(ctx, requestedNAS)
	if err != nil {
		return ReverseProxyPlan{}, err
	}
	plan, err := planReverseProxyWithClient(ctx, name, client, reverseProxyActionCreate, &create, nil)
	if err != nil {
		return ReverseProxyPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = reverseProxyPlanHash(plan)
	}
	return plan, err
}

func (s *Service) PlanReverseProxyDelete(ctx context.Context, requestedNAS string, del loginportal.ReverseProxyRuleDelete) (ReverseProxyPlan, error) {
	if strings.TrimSpace(del.UUID) == "" {
		return ReverseProxyPlan{}, fmt.Errorf("reverse proxy delete requires a uuid")
	}
	name, client, err := s.loginPortalClient(ctx, requestedNAS)
	if err != nil {
		return ReverseProxyPlan{}, err
	}
	plan, err := planReverseProxyWithClient(ctx, name, client, reverseProxyActionDelete, nil, &del)
	if err != nil {
		return ReverseProxyPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = reverseProxyPlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyReverseProxyPlan(ctx context.Context, plan ReverseProxyPlan, approvalHash string) (LoginPortalApplyResult, error) {
	if err := validateReverseProxyPlan(plan, approvalHash); err != nil {
		return LoginPortalApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return LoginPortalApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return LoginPortalApplyResult{}, err
	}
	name, client, err := s.loginPortalClient(ctx, plan.NAS)
	if err != nil {
		return LoginPortalApplyResult{}, err
	}
	if name != plan.NAS {
		return LoginPortalApplyResult{}, fmt.Errorf("reverse proxy plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	// Resolve header secrets only now, at apply time; they never entered the plan.
	var headers []synology.ReverseProxyHeaderValue
	if plan.Action == reverseProxyActionCreate {
		headers, err = s.resolveReverseProxyHeaders(ctx, plan.Create)
		if err != nil {
			return LoginPortalApplyResult{}, err
		}
	}
	return applyReverseProxyWithClient(ctx, client, plan, headers)
}

func planReverseProxyWithClient(ctx context.Context, nas string, client loginPortalClient, action string, create *loginportal.ReverseProxyRuleCreate, del *loginportal.ReverseProxyRuleDelete) (ReverseProxyPlan, error) {
	capabilities, _, err := client.LoginPortalCapabilities(ctx)
	if err != nil {
		return ReverseProxyPlan{}, authenticationError(nas, err)
	}
	if !capabilities.ReverseProxyRead || !capabilities.ReverseProxyWrite {
		return ReverseProxyPlan{}, fmt.Errorf("NAS %q does not expose a verified reverse-proxy read/write backend", nas)
	}
	rules, err := client.ReverseProxyRules(ctx)
	if err != nil {
		return ReverseProxyPlan{}, authenticationError(nas, err)
	}
	plan := ReverseProxyPlan{APIVersion: loginPortalAPIVersion, NAS: nas, Action: action, Create: create, Delete: del, Observed: rules, Risk: "medium"}
	switch action {
	case reverseProxyActionCreate:
		if reverseProxyFrontendConflicts(rules, create.Frontend) {
			return ReverseProxyPlan{}, fmt.Errorf("a reverse-proxy rule already listens on %s:%d", create.Frontend.Hostname, create.Frontend.Port)
		}
		plan.Summary = []string{fmt.Sprintf("create a reverse-proxy rule %s://%s:%d -> %s://%s:%d",
			create.Frontend.Protocol, create.Frontend.Hostname, create.Frontend.Port,
			create.Backend.Protocol, create.Backend.Hostname, create.Backend.Port)}
		plan.Warnings = []string{fmt.Sprintf("a reverse-proxy rule can publish the internal service %s://%s:%d to callers that reach %s:%d", create.Backend.Protocol, create.Backend.Hostname, create.Backend.Port, create.Frontend.Hostname, create.Frontend.Port)}
	case reverseProxyActionDelete:
		if !reverseProxyRuleExists(rules, del.UUID) {
			return ReverseProxyPlan{}, fmt.Errorf("no reverse-proxy rule with uuid %q; nothing to delete", del.UUID)
		}
		plan.Summary = []string{fmt.Sprintf("delete reverse-proxy rule %s", del.UUID)}
		plan.Warnings = []string{}
	default:
		return ReverseProxyPlan{}, fmt.Errorf("unsupported reverse proxy action %q", action)
	}
	plan.ObservedFingerprint, err = hashJSON(reverseProxyFingerprintInput{Rules: rules, Action: action, Create: create, Delete: del})
	if err != nil {
		return ReverseProxyPlan{}, err
	}
	plan.Hash, err = reverseProxyPlanHash(plan)
	if err != nil {
		return ReverseProxyPlan{}, err
	}
	return plan, nil
}

// reverseProxyFingerprintInput binds the intent to the COMPLETE observed rule set
// so any concurrent create/delete/reorder by another session invalidates a stale
// plan (mirrors WI-049's multi-resource fingerprint).
type reverseProxyFingerprintInput struct {
	Rules  synology.ReverseProxyRules          `json:"rules"`
	Action string                              `json:"action"`
	Create *loginportal.ReverseProxyRuleCreate `json:"create,omitempty"`
	Delete *loginportal.ReverseProxyRuleDelete `json:"delete,omitempty"`
}

func applyReverseProxyWithClient(ctx context.Context, client loginPortalClient, plan ReverseProxyPlan, headers []synology.ReverseProxyHeaderValue) (LoginPortalApplyResult, error) {
	current, err := planReverseProxyWithClient(ctx, plan.NAS, client, plan.Action, plan.Create, plan.Delete)
	if err != nil {
		return LoginPortalApplyResult{}, fmt.Errorf("reverse proxy plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = reverseProxyPlanHash(current)
	if err != nil {
		return LoginPortalApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return LoginPortalApplyResult{}, fmt.Errorf("reverse proxy plan is stale; create a new plan")
	}
	var operation synology.LoginPortalMutationResult
	switch plan.Action {
	case reverseProxyActionCreate:
		operation, err = client.ApplyReverseProxyRuleCreate(ctx, *plan.Create, headers)
	case reverseProxyActionDelete:
		operation, err = client.ApplyReverseProxyRuleDelete(ctx, *plan.Delete)
	default:
		return LoginPortalApplyResult{}, fmt.Errorf("unsupported reverse proxy action %q", plan.Action)
	}
	if err != nil {
		return LoginPortalApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifyReverseProxyPostcondition(ctx, client, plan); err != nil {
		return LoginPortalApplyResult{}, fmt.Errorf("verify reverse proxy change: %w", err)
	}
	return LoginPortalApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operation: operation}, nil
}

func (s *Service) resolveReverseProxyHeaders(ctx context.Context, create *loginportal.ReverseProxyRuleCreate) ([]synology.ReverseProxyHeaderValue, error) {
	if create == nil {
		return nil, nil
	}
	headers := make([]synology.ReverseProxyHeaderValue, 0, len(create.CustomHeaders))
	for _, h := range create.CustomHeaders {
		value := h.Value
		if ref := strings.TrimSpace(h.CredentialRef); ref != "" {
			if s.secretReferences == nil {
				return nil, fmt.Errorf("header %q uses credential_ref but no secret resolver is configured", h.Name)
			}
			resolved, err := s.secretReferences.ResolveSecret(ctx, ref)
			if err != nil {
				return nil, fmt.Errorf("resolve credential_ref for header %q: %w", h.Name, err)
			}
			value = resolved
		}
		headers = append(headers, synology.ReverseProxyHeaderValue{Name: h.Name, Value: value})
	}
	return headers, nil
}

func validateReverseProxyCreateShape(create loginportal.ReverseProxyRuleCreate) error {
	if err := validateEndpoint("frontend", create.Frontend); err != nil {
		return err
	}
	if err := validateEndpoint("backend", create.Backend); err != nil {
		return err
	}
	for _, h := range create.CustomHeaders {
		if strings.TrimSpace(h.Name) == "" {
			return fmt.Errorf("a custom header requires a name")
		}
		if h.Value != "" && h.CredentialRef != "" {
			return fmt.Errorf("header %q must set either value or credential_ref, not both", h.Name)
		}
		if ref := strings.TrimSpace(h.CredentialRef); ref != "" {
			if !strings.HasPrefix(ref, "env:") && !strings.HasPrefix(ref, "vault:") {
				return fmt.Errorf("header %q credential_ref must use env:NAME or vault:<id>, not a literal secret", h.Name)
			}
		}
	}
	return nil
}

func validateEndpoint(side string, endpoint synology.ReverseProxyEndpoint) error {
	if strings.TrimSpace(endpoint.Hostname) == "" {
		return fmt.Errorf("%s hostname is required", side)
	}
	if endpoint.Port < 1 || endpoint.Port > 65535 {
		return fmt.Errorf("%s port %d must be between 1 and 65535", side, endpoint.Port)
	}
	switch strings.ToLower(strings.TrimSpace(endpoint.Protocol)) {
	case "http", "https":
	default:
		return fmt.Errorf("%s protocol %q must be http or https", side, endpoint.Protocol)
	}
	return nil
}

func reverseProxyRuleExists(rules synology.ReverseProxyRules, uuid string) bool {
	for _, rule := range rules.Rules {
		if strings.EqualFold(rule.UUID, uuid) {
			return true
		}
	}
	return false
}

func reverseProxyFrontendConflicts(rules synology.ReverseProxyRules, frontend synology.ReverseProxyEndpoint) bool {
	for _, rule := range rules.Rules {
		if strings.EqualFold(rule.Frontend.Hostname, frontend.Hostname) && rule.Frontend.Port == frontend.Port {
			return true
		}
	}
	return false
}

func verifyReverseProxyPostcondition(ctx context.Context, client loginPortalClient, plan ReverseProxyPlan) error {
	rules, err := client.ReverseProxyRules(ctx)
	if err != nil {
		return err
	}
	switch plan.Action {
	case reverseProxyActionCreate:
		if !reverseProxyFrontendConflicts(rules, plan.Create.Frontend) {
			return fmt.Errorf("no reverse-proxy rule listening on %s:%d after create", plan.Create.Frontend.Hostname, plan.Create.Frontend.Port)
		}
	case reverseProxyActionDelete:
		if reverseProxyRuleExists(rules, plan.Delete.UUID) {
			return fmt.Errorf("reverse-proxy rule %s is still present after delete", plan.Delete.UUID)
		}
	}
	return nil
}

func validateReverseProxyPlan(plan ReverseProxyPlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the reverse proxy plan")
	}
	if plan.APIVersion != loginPortalAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid reverse proxy plan metadata")
	}
	switch plan.Action {
	case reverseProxyActionCreate:
		if plan.Create == nil {
			return fmt.Errorf("reverse proxy create plan has no create intent")
		}
		if err := validateReverseProxyCreateShape(*plan.Create); err != nil {
			return err
		}
	case reverseProxyActionDelete:
		if plan.Delete == nil || strings.TrimSpace(plan.Delete.UUID) == "" {
			return fmt.Errorf("reverse proxy delete plan has no uuid")
		}
	default:
		return fmt.Errorf("unsupported reverse proxy action %q", plan.Action)
	}
	expectedFingerprint, err := hashJSON(reverseProxyFingerprintInput{Rules: plan.Observed, Action: plan.Action, Create: plan.Create, Delete: plan.Delete})
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("reverse proxy plan observed state was modified")
	}
	expectedHash, err := reverseProxyPlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("reverse proxy plan contents were modified after planning")
	}
	return nil
}

func reverseProxyPlanHash(plan ReverseProxyPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

// ---- small deref helpers -----------------------------------------------------

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func derefBool(p *bool) any {
	if p == nil {
		return nil
	}
	return *p
}

func derefStr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}
