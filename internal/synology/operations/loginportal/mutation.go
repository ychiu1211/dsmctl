package loginportal

import (
	"context"
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// The write wire is live-verified on the DSM 7.3 lab (throwaway raw probes; the
// reverse-proxy round trip was created, read back, and deleted, reverting to the
// captured baseline). The confirmed set/create/delete shapes are:
//
//	SYNO.Core.Web.DSM           set    v1  {http_port, https_port, enable_https,
//	                                        enable_https_redirect, enable_hsts,
//	                                        enable_spdy(HTTP/2), enable_custom_domain, fqdn}
//	SYNO.Core.Web.DSM.External  set    v1  {hostname}
//	SYNO.Core.AppPortal         set    v1  {id, enable_redirect, alias?, http_port?, https_port?}
//	SYNO.Core.AppPortal.ReverseProxy create v1  {entry:{description, frontend, backend,
//	                                        customize_headers, proxy_*}}
//	SYNO.Core.AppPortal.ReverseProxy delete v1  {uuids:[UUID,...]}
//
// Corrections captured against the stale spec/source guesses:
//   - the DSM web-service SET is version 1 (the v1 GET is used because the v2 GET
//     drops enable_https/enable_hsts; the v1 SET accepts the full field set,
//     confirmed by a no-op set of the exact observed values). HTTP/2 is enable_spdy.
//   - AppPortal edit does NOT exist (method-existence 103); the method is set, and
//     the params are FLAT ({id, ...}) — a portal array is rejected (114).
//   - the reverse-proxy create takes a SINGLE object under the key "entry" (not a
//     batch array); frontend/backend protocol is a numeric enum (0=http, 1=https);
//     HSTS lives at frontend.https.hsts; the stored id key is uppercase "UUID";
//     delete takes params.uuids as an array. ReverseProxy set/get do NOT exist (103).
const (
	DSMWebServiceWriteCapabilityName     = "login_portal.dsm_web_service.write"
	ExternalDomainWriteCapabilityName    = "login_portal.external_domain.write"
	ApplicationPortalWriteCapabilityName = "login_portal.application_portal.write"
	ReverseProxyWriteCapabilityName      = "login_portal.reverse_proxy.write"
)

// MutationResult records the DSM backend that accepted a Login Portal write.
type MutationResult struct {
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

// DSMWebServiceSetInput is the complete desired DSM web-service state. The caller
// merges its patch into the freshly read state first, so a field it did not
// specify is never silently reset.
type DSMWebServiceSetInput struct {
	HTTPPort            int
	HTTPSPort           int
	HTTPSEnabled        bool
	HTTPRedirectEnabled bool
	HSTSEnabled         bool
	HTTP2Enabled        bool
	CustomDomainEnabled bool
	CustomDomain        string
}

// ExternalDomainSetInput is the complete desired external-hostname state.
type ExternalDomainSetInput struct {
	Hostname string
}

// ApplicationPortalSetInput is the complete desired state for one application
// portal, keyed by app id.
type ApplicationPortalSetInput struct {
	AppID         string
	RedirectHTTPS bool
	Alias         string
	HTTPPort      int
	HTTPSPort     int
}

// ReverseProxyHeaderInput is one resolved custom header. A secret value is
// resolved from its credential reference by the caller before it reaches here;
// this struct never carries a credential reference.
type ReverseProxyHeaderInput struct {
	Name  string
	Value string
}

// ReverseProxyCreateInput is one reverse-proxy rule to create. Protocols are the
// stable "http"/"https" names, mapped to DSM's numeric enum at the wire.
type ReverseProxyCreateInput struct {
	Description      string
	FrontendProtocol string
	FrontendHostname string
	FrontendPort     int
	FrontendHSTS     bool
	BackendProtocol  string
	BackendHostname  string
	BackendPort      int
	Headers          []ReverseProxyHeaderInput
}

// ReverseProxyDeleteInput is the set of uuids to delete (one, for this module).
type ReverseProxyDeleteInput struct {
	UUIDs []string
}

var dsmWebServiceSetOperation = compatibility.Operation[DSMWebServiceSetInput, MutationResult]{
	Name: DSMWebServiceWriteCapabilityName,
	Variants: []compatibility.Variant[DSMWebServiceSetInput, MutationResult]{
		{
			Name: "login-portal-web-dsm-set-v1", API: WebDSMAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(WebDSMAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, desired DSMWebServiceSetInput) (MutationResult, error) {
				params := map[string]any{
					"http_port":             desired.HTTPPort,
					"https_port":            desired.HTTPSPort,
					"enable_https":          desired.HTTPSEnabled,
					"enable_https_redirect": desired.HTTPRedirectEnabled,
					"enable_hsts":           desired.HSTSEnabled,
					"enable_spdy":           desired.HTTP2Enabled,
					"enable_custom_domain":  desired.CustomDomainEnabled,
					"fqdn":                  desired.CustomDomain,
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: WebDSMAPIName, Version: 1, Method: "set", JSONParameters: params}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set: %w", WebDSMAPIName, err)
				}
				return MutationResult{}, nil
			},
		},
	},
}

var externalDomainSetOperation = compatibility.Operation[ExternalDomainSetInput, MutationResult]{
	Name: ExternalDomainWriteCapabilityName,
	Variants: []compatibility.Variant[ExternalDomainSetInput, MutationResult]{
		{
			Name: "login-portal-web-dsm-external-set-v1", API: WebDSMExternalAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(WebDSMExternalAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, desired ExternalDomainSetInput) (MutationResult, error) {
				params := map[string]any{"hostname": desired.Hostname}
				if _, err := executor.Execute(ctx, compatibility.Request{API: WebDSMExternalAPIName, Version: 1, Method: "set", JSONParameters: params}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set: %w", WebDSMExternalAPIName, err)
				}
				return MutationResult{}, nil
			},
		},
	},
}

var applicationPortalSetOperation = compatibility.Operation[ApplicationPortalSetInput, MutationResult]{
	Name: ApplicationPortalWriteCapabilityName,
	Variants: []compatibility.Variant[ApplicationPortalSetInput, MutationResult]{
		{
			Name: "login-portal-appportal-set-v1", API: AppPortalAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(AppPortalAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, desired ApplicationPortalSetInput) (MutationResult, error) {
				id := strings.TrimSpace(desired.AppID)
				if id == "" {
					return MutationResult{}, fmt.Errorf("application portal set requires an app id")
				}
				// The set is flat and keyed by id (confirmed live). alias/ports are
				// sent only when configured so a redirect-only change never sends an
				// invalid zero port or an empty alias.
				params := map[string]any{"id": id, "enable_redirect": desired.RedirectHTTPS}
				if strings.TrimSpace(desired.Alias) != "" {
					params["alias"] = desired.Alias
				}
				if desired.HTTPPort > 0 {
					params["http_port"] = desired.HTTPPort
				}
				if desired.HTTPSPort > 0 {
					params["https_port"] = desired.HTTPSPort
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: AppPortalAPIName, Version: 1, Method: "set", JSONParameters: params}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set: %w", AppPortalAPIName, err)
				}
				return MutationResult{}, nil
			},
		},
	},
}

var reverseProxyCreateOperation = compatibility.Operation[ReverseProxyCreateInput, MutationResult]{
	Name: ReverseProxyWriteCapabilityName,
	Variants: []compatibility.Variant[ReverseProxyCreateInput, MutationResult]{
		{
			Name: "login-portal-reverse-proxy-create-v1", API: ReverseProxyAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(ReverseProxyAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, in ReverseProxyCreateInput) (MutationResult, error) {
				headers := make([]map[string]any, 0, len(in.Headers))
				for _, h := range in.Headers {
					headers = append(headers, map[string]any{"name": h.Name, "value": h.Value})
				}
				entry := map[string]any{
					"description": in.Description,
					"frontend": map[string]any{
						"protocol": protocolToInt(in.FrontendProtocol),
						"fqdn":     in.FrontendHostname,
						"port":     in.FrontendPort,
						"acl":      nil,
						"https":    map[string]any{"hsts": in.FrontendHSTS},
					},
					"backend": map[string]any{
						"protocol": protocolToInt(in.BackendProtocol),
						"fqdn":     in.BackendHostname,
						"port":     in.BackendPort,
					},
					"customize_headers":      headers,
					"proxy_connect_timeout":  60,
					"proxy_read_timeout":     60,
					"proxy_send_timeout":     60,
					"proxy_http_version":     1,
					"proxy_intercept_errors": false,
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: ReverseProxyAPIName, Version: 1, Method: "create", JSONParameters: map[string]any{"entry": entry}}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.create: %w", ReverseProxyAPIName, err)
				}
				return MutationResult{Method: "create"}, nil
			},
		},
	},
}

var reverseProxyDeleteOperation = compatibility.Operation[ReverseProxyDeleteInput, MutationResult]{
	Name: ReverseProxyWriteCapabilityName,
	Variants: []compatibility.Variant[ReverseProxyDeleteInput, MutationResult]{
		{
			Name: "login-portal-reverse-proxy-delete-v1", API: ReverseProxyAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(ReverseProxyAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, in ReverseProxyDeleteInput) (MutationResult, error) {
				if len(in.UUIDs) == 0 {
					return MutationResult{}, fmt.Errorf("reverse proxy delete requires at least one uuid")
				}
				params := map[string]any{"uuids": in.UUIDs}
				if _, err := executor.Execute(ctx, compatibility.Request{API: ReverseProxyAPIName, Version: 1, Method: "delete", JSONParameters: params}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.delete: %w", ReverseProxyAPIName, err)
				}
				return MutationResult{Method: "delete"}, nil
			},
		},
	},
}

// protocolToInt maps the stable "http"/"https" name to DSM's numeric protocol
// enum (0=http, 1=https). An unknown value defaults to http (0).
func protocolToInt(protocol string) int {
	if strings.EqualFold(strings.TrimSpace(protocol), "https") {
		return 1
	}
	return 0
}

func SelectDSMWebServiceSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := dsmWebServiceSetOperation.Select(target)
	return selection, err
}

func SelectExternalDomainSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := externalDomainSetOperation.Select(target)
	return selection, err
}

func SelectApplicationPortalSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := applicationPortalSetOperation.Select(target)
	return selection, err
}

func SelectReverseProxyWrite(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := reverseProxyCreateOperation.Select(target)
	return selection, err
}

// ExecuteDSMWebServiceSet submits the complete desired DSM web-service state.
func ExecuteDSMWebServiceSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, desired DSMWebServiceSetInput) (MutationResult, compatibility.Selection, error) {
	result, selection, err := dsmWebServiceSetOperation.Run(ctx, target, executor, desired)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

// ExecuteExternalDomainSet submits the complete desired external-hostname state.
func ExecuteExternalDomainSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, desired ExternalDomainSetInput) (MutationResult, compatibility.Selection, error) {
	result, selection, err := externalDomainSetOperation.Run(ctx, target, executor, desired)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

// ExecuteApplicationPortalSet submits the complete desired state for one app portal.
func ExecuteApplicationPortalSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, desired ApplicationPortalSetInput) (MutationResult, compatibility.Selection, error) {
	result, selection, err := applicationPortalSetOperation.Run(ctx, target, executor, desired)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

// ExecuteReverseProxyCreate creates one reverse-proxy rule (params.entry).
func ExecuteReverseProxyCreate(ctx context.Context, target compatibility.Target, executor compatibility.Executor, in ReverseProxyCreateInput) (MutationResult, compatibility.Selection, error) {
	result, selection, err := reverseProxyCreateOperation.Run(ctx, target, executor, in)
	if err == nil {
		result.Backend, result.API, result.Version = selection.Backend, selection.API, selection.Version
	}
	return result, selection, err
}

// ExecuteReverseProxyDelete deletes one or more reverse-proxy rules by uuid.
func ExecuteReverseProxyDelete(ctx context.Context, target compatibility.Target, executor compatibility.Executor, in ReverseProxyDeleteInput) (MutationResult, compatibility.Selection, error) {
	result, selection, err := reverseProxyDeleteOperation.Run(ctx, target, executor, in)
	if err == nil {
		result.Backend, result.API, result.Version = selection.Backend, selection.API, selection.Version
	}
	return result, selection, err
}
