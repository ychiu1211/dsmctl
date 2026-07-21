package application

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/loginportal"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type fakeLoginPortalClient struct {
	dsm          synology.DSMWebService
	transport    synology.LoginPortalTransportInfo
	portals      synology.ApplicationPortals
	rules        synology.ReverseProxyRules
	capabilities synology.LoginPortalCapabilities
	persist      bool
	mutations    int
	lastHeaders  []synology.ReverseProxyHeaderValue
	nextUUID     string
}

func (c *fakeLoginPortalClient) DSMWebService(context.Context) (synology.DSMWebService, error) {
	return c.dsm, nil
}
func (c *fakeLoginPortalClient) ApplicationPortals(context.Context) (synology.ApplicationPortals, error) {
	return c.portals, nil
}
func (c *fakeLoginPortalClient) ReverseProxyRules(context.Context) (synology.ReverseProxyRules, error) {
	return c.rules, nil
}
func (c *fakeLoginPortalClient) LoginPortalCapabilities(context.Context) (synology.LoginPortalCapabilities, synology.CompatibilityReport, error) {
	return c.capabilities, synology.CompatibilityReport{}, nil
}
func (c *fakeLoginPortalClient) LoginPortalTransport() synology.LoginPortalTransportInfo {
	return c.transport
}

func (c *fakeLoginPortalClient) ApplyDSMWebServiceChange(_ context.Context, change synology.DSMWebServiceChange) (synology.LoginPortalMutationResult, error) {
	c.mutations++
	if c.persist {
		if change.HTTPPort != nil {
			c.dsm.HTTPPort = *change.HTTPPort
		}
		if change.HTTPSPort != nil {
			c.dsm.HTTPSPort = *change.HTTPSPort
		}
		if change.HTTPSEnabled != nil {
			c.dsm.HTTPSEnabled = *change.HTTPSEnabled
		}
		if change.HTTPRedirectEnabled != nil {
			c.dsm.HTTPRedirectEnabled = *change.HTTPRedirectEnabled
		}
		if change.HSTSEnabled != nil {
			c.dsm.HSTSEnabled = *change.HSTSEnabled
		}
		if change.HTTP2Enabled != nil {
			c.dsm.HTTP2Enabled = *change.HTTP2Enabled
		}
		if change.CustomDomainEnabled != nil {
			c.dsm.CustomDomainEnabled = *change.CustomDomainEnabled
		}
		if change.CustomDomain != nil {
			c.dsm.CustomDomain = *change.CustomDomain
		}
		if change.ExternalHostname != nil {
			c.dsm.ExternalHostname = *change.ExternalHostname
		}
	}
	return synology.LoginPortalMutationResult{Backend: "login-portal-web-dsm-set-v1", API: "SYNO.Core.Web.DSM", Version: 1, Method: "set"}, nil
}

func (c *fakeLoginPortalClient) ApplyApplicationPortalChange(_ context.Context, change synology.ApplicationPortalChange) (synology.LoginPortalMutationResult, error) {
	c.mutations++
	if c.persist {
		for i := range c.portals.Portals {
			if c.portals.Portals[i].AppID != change.AppID {
				continue
			}
			if change.RedirectHTTPS != nil {
				c.portals.Portals[i].RedirectHTTPS = *change.RedirectHTTPS
			}
			if change.Alias != nil {
				c.portals.Portals[i].Alias = *change.Alias
			}
			if change.HTTPPort != nil {
				c.portals.Portals[i].HTTPPort = *change.HTTPPort
			}
			if change.HTTPSPort != nil {
				c.portals.Portals[i].HTTPSPort = *change.HTTPSPort
			}
		}
	}
	return synology.LoginPortalMutationResult{Backend: "login-portal-appportal-set-v1", API: "SYNO.Core.AppPortal", Version: 1, Method: "set"}, nil
}

func (c *fakeLoginPortalClient) ApplyReverseProxyRuleCreate(_ context.Context, create synology.ReverseProxyRuleCreate, headers []synology.ReverseProxyHeaderValue) (synology.LoginPortalMutationResult, error) {
	c.mutations++
	c.lastHeaders = headers
	if c.persist {
		uuid := c.nextUUID
		if uuid == "" {
			uuid = "rp-new"
		}
		c.rules.Rules = append(c.rules.Rules, synology.ReverseProxyRule{
			UUID:        uuid,
			Description: create.Description,
			Frontend:    create.Frontend,
			Backend:     create.Backend,
			HSTSEnabled: create.FrontendHSTS,
		})
		c.rules.Total = len(c.rules.Rules)
	}
	return synology.LoginPortalMutationResult{Backend: "login-portal-reverse-proxy-create-v1", API: "SYNO.Core.AppPortal.ReverseProxy", Version: 1, Method: "create"}, nil
}

func (c *fakeLoginPortalClient) ApplyReverseProxyRuleDelete(_ context.Context, del synology.ReverseProxyRuleDelete) (synology.LoginPortalMutationResult, error) {
	c.mutations++
	if c.persist {
		kept := c.rules.Rules[:0]
		for _, r := range c.rules.Rules {
			if r.UUID != del.UUID {
				kept = append(kept, r)
			}
		}
		c.rules.Rules = kept
		c.rules.Total = len(c.rules.Rules)
	}
	return synology.LoginPortalMutationResult{Backend: "login-portal-reverse-proxy-delete-v1", API: "SYNO.Core.AppPortal.ReverseProxy", Version: 1, Method: "delete"}, nil
}

func loginPortalTestClient() *fakeLoginPortalClient {
	return &fakeLoginPortalClient{
		dsm: synology.DSMWebService{
			HTTPPort: 5000, HTTPSPort: 5001, HTTPSEnabled: true, HTTPRedirectEnabled: false,
			HSTSEnabled: false, HTTP2Enabled: true, CustomDomainEnabled: false,
			ExternalDomainSupported: true, ExternalHostname: "",
		},
		transport: synology.LoginPortalTransportInfo{Scheme: "https", Port: 5001},
		portals: synology.ApplicationPortals{Total: 1, Portals: []synology.ApplicationPortal{
			{AppID: "SYNO.SDS.App.FileStation3.Instance", DisplayName: "File Station", RedirectHTTPS: false},
		}},
		rules: synology.ReverseProxyRules{},
		capabilities: synology.LoginPortalCapabilities{
			Module: loginportal.ModuleName, DSMWebServiceRead: true, ExternalDomainRead: true,
			ApplicationPortalRead: true, ReverseProxyRead: true,
			DSMWebServiceWrite: true, ExternalDomainWrite: true, ApplicationPortalWrite: true, ReverseProxyWrite: true,
			Mutations: true,
		},
		persist: true,
	}
}

// ---- DSM web service + never-break guard ----

func TestDSMWebServicePlanApplyToggleHTTP2IsHigh(t *testing.T) {
	client := loginPortalTestClient()
	// Toggling HTTP/2 does not touch the current transport; it is still HIGH.
	plan, err := planDSMWebServiceWithClient(context.Background(), "lab", client, loginportal.DSMWebServiceChange{HTTP2Enabled: boolPtr(false)})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "high" || plan.Hash == "" || plan.ObservedFingerprint == "" {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.Observed.Transport.Scheme != "https" || plan.Observed.Transport.Port != 5001 {
		t.Fatalf("plan must hash the current transport: %#v", plan.Observed.Transport)
	}
	result, err := applyDSMWebServiceWithClient(context.Background(), client, plan)
	if err != nil || !result.Applied || client.dsm.HTTP2Enabled {
		t.Fatalf("result = %#v state = %#v err = %v", result, client.dsm, err)
	}
}

func TestDSMWebServiceNeverBreakGuardRefusesPortMove(t *testing.T) {
	client := loginPortalTestClient() // transport https:5001
	// Moving the HTTPS port dsmctl is connected on is refused without the override.
	_, err := planDSMWebServiceWithClient(context.Background(), "lab", client, loginportal.DSMWebServiceChange{HTTPSPort: intPtr(5051)})
	if err == nil || !strings.Contains(err.Error(), "allow_connectivity_break") || !strings.Contains(err.Error(), "sever") {
		t.Fatalf("port-move guard error = %v", err)
	}
	// With the explicit override it proceeds and the plan records the break in warnings.
	plan, err := planDSMWebServiceWithClient(context.Background(), "lab", client, loginportal.DSMWebServiceChange{HTTPSPort: intPtr(5051), AllowConnectivityBreak: true})
	if err != nil {
		t.Fatalf("override plan error = %v", err)
	}
	if plan.Risk != "high" || !strings.Contains(strings.Join(plan.Warnings, "\n"), "sever") {
		t.Fatalf("override plan = %#v", plan)
	}
}

func TestDSMWebServiceNeverBreakGuardRefusesDisableHTTPS(t *testing.T) {
	client := loginPortalTestClient() // connected over https
	_, err := planDSMWebServiceWithClient(context.Background(), "lab", client, loginportal.DSMWebServiceChange{HTTPSEnabled: boolPtr(false)})
	if err == nil || !strings.Contains(err.Error(), "allow_connectivity_break") {
		t.Fatalf("disable-https guard error = %v", err)
	}
}

func TestDSMWebServiceNeverBreakGuardHSTSIsConservative(t *testing.T) {
	client := loginPortalTestClient() // hsts currently false
	_, err := planDSMWebServiceWithClient(context.Background(), "lab", client, loginportal.DSMWebServiceChange{HSTSEnabled: boolPtr(true)})
	if err == nil || !strings.Contains(err.Error(), "allow_connectivity_break") {
		t.Fatalf("enable-hsts guard error = %v", err)
	}
}

func TestDSMWebServicePortMoveToSamePortIsNotABreak(t *testing.T) {
	client := loginPortalTestClient() // transport https:5001
	// Setting https_port to the SAME 5001 is a no-op for that field; combine with a
	// real change so the plan is not empty. This must NOT trip the guard.
	plan, err := planDSMWebServiceWithClient(context.Background(), "lab", client, loginportal.DSMWebServiceChange{HTTPSPort: intPtr(5001), HTTP2Enabled: boolPtr(false)})
	if err != nil {
		t.Fatalf("same-port plan error = %v", err)
	}
	if plan.Risk != "high" {
		t.Fatalf("risk = %q", plan.Risk)
	}
}

func TestDSMWebServiceRejectsNoOpAndBadShape(t *testing.T) {
	client := loginPortalTestClient()
	if _, err := planDSMWebServiceWithClient(context.Background(), "lab", client, loginportal.DSMWebServiceChange{HTTPPort: intPtr(5000)}); err == nil || !strings.Contains(err.Error(), "would not change") {
		t.Fatalf("no-op error = %v", err)
	}
	if err := validateDSMWebServiceShape(loginportal.DSMWebServiceChange{}); err == nil || !strings.Contains(err.Error(), "no fields") {
		t.Fatalf("empty shape error = %v", err)
	}
	if err := validateDSMWebServiceShape(loginportal.DSMWebServiceChange{HTTPSPort: intPtr(70000)}); err == nil || !strings.Contains(err.Error(), "between 1 and 65535") {
		t.Fatalf("port range error = %v", err)
	}
	if err := validateDSMWebServiceShape(loginportal.DSMWebServiceChange{HTTPPort: intPtr(5000), HTTPSPort: intPtr(5000)}); err == nil || !strings.Contains(err.Error(), "must differ") {
		t.Fatalf("same-port shape error = %v", err)
	}
}

func TestDSMWebServiceApplyRejectsStaleAndTampering(t *testing.T) {
	client := loginPortalTestClient()
	plan, err := planDSMWebServiceWithClient(context.Background(), "lab", client, loginportal.DSMWebServiceChange{HTTP2Enabled: boolPtr(false)})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	tampered := plan
	tampered.Risk = "low"
	if err := validateDSMWebServicePlan(tampered, tampered.Hash); err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("tamper error = %v", err)
	}
	// A new active connection endpoint after planning invalidates the transport-bound plan.
	client.transport = synology.LoginPortalTransportInfo{Scheme: "https", Port: 5443}
	if _, err := applyDSMWebServiceWithClient(context.Background(), client, plan); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale-on-transport-change error = %v", err)
	}
}

func TestDSMWebServicePostconditionCatchesIgnoredField(t *testing.T) {
	client := loginPortalTestClient()
	client.persist = false // DSM silently ignores the change
	plan, err := planDSMWebServiceWithClient(context.Background(), "lab", client, loginportal.DSMWebServiceChange{HTTP2Enabled: boolPtr(false)})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if _, err := applyDSMWebServiceWithClient(context.Background(), client, plan); err == nil || !strings.Contains(err.Error(), "verify DSM web service change") || !strings.Contains(err.Error(), "http2_enabled") {
		t.Fatalf("postcondition error = %v", err)
	}
}

func TestDSMWebServiceMissingWriteBackend(t *testing.T) {
	client := loginPortalTestClient()
	client.capabilities.DSMWebServiceWrite = false
	if _, err := planDSMWebServiceWithClient(context.Background(), "lab", client, loginportal.DSMWebServiceChange{HTTP2Enabled: boolPtr(false)}); err == nil || !strings.Contains(err.Error(), "DSM web-service read/write backend") {
		t.Fatalf("missing backend error = %v", err)
	}
}

func TestDSMWebServiceExternalHostnameNeedsWriteBackend(t *testing.T) {
	client := loginPortalTestClient()
	client.capabilities.ExternalDomainWrite = false
	if _, err := planDSMWebServiceWithClient(context.Background(), "lab", client, loginportal.DSMWebServiceChange{ExternalHostname: stringPointer("dsm.example.com")}); err == nil || !strings.Contains(err.Error(), "external-hostname write backend") {
		t.Fatalf("external write backend error = %v", err)
	}
}

// ---- Application portal ----

func TestApplicationPortalPlanApplyIsMedium(t *testing.T) {
	client := loginPortalTestClient()
	plan, err := planApplicationPortalWithClient(context.Background(), "lab", client, loginportal.ApplicationPortalChange{AppID: "SYNO.SDS.App.FileStation3.Instance", RedirectHTTPS: boolPtr(true)})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "medium" {
		t.Fatalf("risk = %q", plan.Risk)
	}
	result, err := applyApplicationPortalWithClient(context.Background(), client, plan)
	if err != nil || !result.Applied || !client.portals.Portals[0].RedirectHTTPS {
		t.Fatalf("result = %#v state = %#v err = %v", result, client.portals, err)
	}
}

func TestApplicationPortalRejectsUnknownApp(t *testing.T) {
	client := loginPortalTestClient()
	if _, err := planApplicationPortalWithClient(context.Background(), "lab", client, loginportal.ApplicationPortalChange{AppID: "SYNO.SDS.Missing", RedirectHTTPS: boolPtr(true)}); err == nil || !strings.Contains(err.Error(), "not present in the portal list") {
		t.Fatalf("unknown-app error = %v", err)
	}
}

func TestApplicationPortalAliasWarnsExposure(t *testing.T) {
	client := loginPortalTestClient()
	plan, err := planApplicationPortalWithClient(context.Background(), "lab", client, loginportal.ApplicationPortalChange{AppID: "SYNO.SDS.App.FileStation3.Instance", Alias: stringPointer("files")})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if !strings.Contains(strings.Join(plan.Warnings, "\n"), "publishes") {
		t.Fatalf("expected exposure warning: %#v", plan.Warnings)
	}
}

// ---- Reverse proxy ----

func reverseProxyCreate() loginportal.ReverseProxyRuleCreate {
	return loginportal.ReverseProxyRuleCreate{
		Description: "media",
		Frontend:    loginportal.ReverseProxyEndpoint{Protocol: "https", Hostname: "media.example.com", Port: 8443},
		Backend:     loginportal.ReverseProxyEndpoint{Protocol: "http", Hostname: "127.0.0.1", Port: 8096},
	}
}

func TestReverseProxyCreatePlanApplyIsMedium(t *testing.T) {
	client := loginPortalTestClient()
	client.nextUUID = "rp-1"
	plan, err := planReverseProxyWithClient(context.Background(), "lab", client, reverseProxyActionCreate, ptrCreate(reverseProxyCreate()), nil)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if plan.Risk != "medium" || !strings.Contains(strings.Join(plan.Warnings, "\n"), "publish") {
		t.Fatalf("plan = %#v", plan)
	}
	result, err := applyReverseProxyWithClient(context.Background(), client, plan, nil)
	if err != nil || !result.Applied || len(client.rules.Rules) != 1 {
		t.Fatalf("result = %#v rules = %#v err = %v", result, client.rules, err)
	}
}

func TestReverseProxyDeletePlanApply(t *testing.T) {
	client := loginPortalTestClient()
	client.rules = synology.ReverseProxyRules{Total: 1, Rules: []synology.ReverseProxyRule{{UUID: "rp-1", Description: "media"}}}
	plan, err := planReverseProxyWithClient(context.Background(), "lab", client, reverseProxyActionDelete, nil, &loginportal.ReverseProxyRuleDelete{UUID: "rp-1"})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	result, err := applyReverseProxyWithClient(context.Background(), client, plan, nil)
	if err != nil || !result.Applied || len(client.rules.Rules) != 0 {
		t.Fatalf("result = %#v rules = %#v err = %v", result, client.rules, err)
	}
}

func TestReverseProxyDeleteRejectsUnknownUUID(t *testing.T) {
	client := loginPortalTestClient()
	if _, err := planReverseProxyWithClient(context.Background(), "lab", client, reverseProxyActionDelete, nil, &loginportal.ReverseProxyRuleDelete{UUID: "ghost"}); err == nil || !strings.Contains(err.Error(), "nothing to delete") {
		t.Fatalf("unknown-uuid error = %v", err)
	}
}

func TestReverseProxyCreateConflict(t *testing.T) {
	client := loginPortalTestClient()
	client.rules = synology.ReverseProxyRules{Total: 1, Rules: []synology.ReverseProxyRule{
		{UUID: "rp-1", Frontend: synology.ReverseProxyEndpoint{Hostname: "media.example.com", Port: 8443}},
	}}
	if _, err := planReverseProxyWithClient(context.Background(), "lab", client, reverseProxyActionCreate, ptrCreate(reverseProxyCreate()), nil); err == nil || !strings.Contains(err.Error(), "already listens on") {
		t.Fatalf("conflict error = %v", err)
	}
}

func TestReverseProxyPlanStaleWhenRuleSetChanges(t *testing.T) {
	client := loginPortalTestClient()
	plan, err := planReverseProxyWithClient(context.Background(), "lab", client, reverseProxyActionCreate, ptrCreate(reverseProxyCreate()), nil)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	// Another session added a rule after planning: the whole-set fingerprint changes.
	client.rules = synology.ReverseProxyRules{Total: 1, Rules: []synology.ReverseProxyRule{{UUID: "other", Frontend: synology.ReverseProxyEndpoint{Hostname: "other.example.com", Port: 9443}}}}
	if _, err := applyReverseProxyWithClient(context.Background(), client, plan, nil); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale-on-ruleset-change error = %v", err)
	}
}

func TestReverseProxyCreateShapeValidation(t *testing.T) {
	cases := []struct {
		name string
		in   loginportal.ReverseProxyRuleCreate
		want string
	}{
		{"no frontend host", loginportal.ReverseProxyRuleCreate{Frontend: loginportal.ReverseProxyEndpoint{Protocol: "https", Port: 443}, Backend: loginportal.ReverseProxyEndpoint{Protocol: "http", Hostname: "h", Port: 80}}, "frontend hostname is required"},
		{"bad backend port", loginportal.ReverseProxyRuleCreate{Frontend: loginportal.ReverseProxyEndpoint{Protocol: "https", Hostname: "h", Port: 443}, Backend: loginportal.ReverseProxyEndpoint{Protocol: "http", Hostname: "h", Port: 0}}, "port 0 must be between"},
		{"bad protocol", loginportal.ReverseProxyRuleCreate{Frontend: loginportal.ReverseProxyEndpoint{Protocol: "ftp", Hostname: "h", Port: 443}, Backend: loginportal.ReverseProxyEndpoint{Protocol: "http", Hostname: "h", Port: 80}}, "must be http or https"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateReverseProxyCreateShape(tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("shape error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestReverseProxyHeaderCredentialRefValidation(t *testing.T) {
	create := reverseProxyCreate()
	create.CustomHeaders = []loginportal.ReverseProxyHeader{{Name: "X-Auth", CredentialRef: "literal-secret"}}
	if err := validateReverseProxyCreateShape(create); err == nil || !strings.Contains(err.Error(), "env:NAME or vault") {
		t.Fatalf("credential_ref validation error = %v", err)
	}
	create.CustomHeaders = []loginportal.ReverseProxyHeader{{Name: "X-Auth", Value: "v", CredentialRef: "env:X"}}
	if err := validateReverseProxyCreateShape(create); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("value+ref validation error = %v", err)
	}
}

// ---- secret hygiene ----

func TestLoginPortalReverseProxyPlanCarriesNoResolvedSecret(t *testing.T) {
	client := loginPortalTestClient()
	create := reverseProxyCreate()
	create.CustomHeaders = []loginportal.ReverseProxyHeader{{Name: "X-Auth-Token", CredentialRef: "env:RP_TOKEN"}}
	plan, err := planReverseProxyWithClient(context.Background(), "lab", client, reverseProxyActionCreate, &create, nil)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal error = %v", err)
	}
	// The plan keeps the reference, never a resolved secret.
	if !strings.Contains(string(encoded), "env:RP_TOKEN") {
		t.Fatalf("plan should keep the credential_ref: %s", encoded)
	}
	for _, forbidden := range []string{"synotoken", "\"sid\"", "password", "secret-value"} {
		if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
			t.Fatalf("plan JSON leaked %q: %s", forbidden, encoded)
		}
	}
}

func ptrCreate(c loginportal.ReverseProxyRuleCreate) *loginportal.ReverseProxyRuleCreate {
	return &c
}
