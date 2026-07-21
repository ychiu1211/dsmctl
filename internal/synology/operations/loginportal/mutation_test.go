package loginportal

import (
	"context"
	"encoding/json"
	"testing"
)

// TestExecuteReverseProxyDecodesLiveShape locks in the DSM 7.3 live rule shape
// captured by the write-wire probe (a created rule read back): uppercase "UUID",
// numeric frontend/backend protocol (0=http, 1=https), and HSTS nested at
// frontend.https.hsts.
func TestExecuteReverseProxyDecodesLiveShape(t *testing.T) {
	const live = `{"entries":[{"UUID":"9c8fb9c0-8fb9-427d-bcbf-883af954b794","backend":{"fqdn":"127.0.0.1","port":1,"protocol":0},"customize_headers":[],"description":"dsmctl-probe","frontend":{"acl":null,"fqdn":"dsmctl-probe.invalid","https":{"hsts":true},"port":18443,"protocol":1},"proxy_connect_timeout":60,"proxy_http_version":1,"proxy_intercept_errors":false,"proxy_read_timeout":60,"proxy_send_timeout":60}]}`
	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		ReverseProxyAPIName + ".list": json.RawMessage(live),
	}}
	rules, _, err := ExecuteReverseProxyRules(context.Background(), lpTarget(), exec)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(rules.Rules) != 1 {
		t.Fatalf("rules = %#v", rules)
	}
	r := rules.Rules[0]
	if r.UUID != "9c8fb9c0-8fb9-427d-bcbf-883af954b794" {
		t.Fatalf("uuid = %q", r.UUID)
	}
	if r.Frontend.Protocol != "https" || r.Frontend.Hostname != "dsmctl-probe.invalid" || r.Frontend.Port != 18443 {
		t.Fatalf("frontend = %#v", r.Frontend)
	}
	if r.Backend.Protocol != "http" || r.Backend.Port != 1 {
		t.Fatalf("backend = %#v", r.Backend)
	}
	if !r.HSTSEnabled {
		t.Fatalf("hsts should decode from frontend.https.hsts: %#v", r)
	}
}

// These request-capture tests pin the live-verified write wire: the exact API,
// version, method, and JSON parameter shape sent to DSM for each mutation.

func TestExecuteDSMWebServiceSetRequest(t *testing.T) {
	exec := &recordingExecutor{}
	desired := DSMWebServiceSetInput{
		HTTPPort: 5000, HTTPSPort: 5001, HTTPSEnabled: true, HTTPRedirectEnabled: false,
		HSTSEnabled: false, HTTP2Enabled: true, CustomDomainEnabled: false, CustomDomain: "",
	}
	if _, _, err := ExecuteDSMWebServiceSet(context.Background(), lpTarget(), exec, desired); err != nil {
		t.Fatalf("ExecuteDSMWebServiceSet() error = %v", err)
	}
	req := exec.requests[0]
	if req.API != WebDSMAPIName || req.Version != 1 || req.Method != "set" {
		t.Fatalf("request = %#v", req)
	}
	p := req.JSONParameters
	if p["http_port"] != 5000 || p["https_port"] != 5001 || p["enable_https"] != true ||
		p["enable_https_redirect"] != false || p["enable_hsts"] != false || p["enable_spdy"] != true ||
		p["enable_custom_domain"] != false || p["fqdn"] != "" {
		t.Fatalf("params = %#v", p)
	}
}

func TestExecuteExternalDomainSetRequest(t *testing.T) {
	exec := &recordingExecutor{}
	if _, _, err := ExecuteExternalDomainSet(context.Background(), lpTarget(), exec, ExternalDomainSetInput{Hostname: "dsm.example.com"}); err != nil {
		t.Fatalf("error = %v", err)
	}
	req := exec.requests[0]
	if req.API != WebDSMExternalAPIName || req.Version != 1 || req.Method != "set" || req.JSONParameters["hostname"] != "dsm.example.com" {
		t.Fatalf("request = %#v", req)
	}
}

func TestExecuteApplicationPortalSetRequest(t *testing.T) {
	exec := &recordingExecutor{}
	// A redirect-only change must NOT send zero ports or an empty alias.
	if _, _, err := ExecuteApplicationPortalSet(context.Background(), lpTarget(), exec, ApplicationPortalSetInput{AppID: "SYNO.SDS.App.FileStation3.Instance", RedirectHTTPS: true}); err != nil {
		t.Fatalf("error = %v", err)
	}
	req := exec.requests[0]
	if req.API != AppPortalAPIName || req.Version != 1 || req.Method != "set" {
		t.Fatalf("request = %#v", req)
	}
	p := req.JSONParameters
	if p["id"] != "SYNO.SDS.App.FileStation3.Instance" || p["enable_redirect"] != true {
		t.Fatalf("params = %#v", p)
	}
	if _, ok := p["http_port"]; ok {
		t.Fatalf("redirect-only change must not send http_port: %#v", p)
	}
	if _, ok := p["alias"]; ok {
		t.Fatalf("redirect-only change must not send alias: %#v", p)
	}

	// A full custom-portal change sends alias + ports.
	exec2 := &recordingExecutor{}
	if _, _, err := ExecuteApplicationPortalSet(context.Background(), lpTarget(), exec2, ApplicationPortalSetInput{AppID: "app", RedirectHTTPS: false, Alias: "files", HTTPPort: 7000, HTTPSPort: 7001}); err != nil {
		t.Fatalf("error = %v", err)
	}
	p2 := exec2.requests[0].JSONParameters
	if p2["alias"] != "files" || p2["http_port"] != 7000 || p2["https_port"] != 7001 {
		t.Fatalf("params = %#v", p2)
	}
}

func TestExecuteReverseProxyCreateRequest(t *testing.T) {
	exec := &recordingExecutor{}
	in := ReverseProxyCreateInput{
		Description:      "media",
		FrontendProtocol: "https", FrontendHostname: "media.example.com", FrontendPort: 443, FrontendHSTS: true,
		BackendProtocol: "http", BackendHostname: "127.0.0.1", BackendPort: 8096,
		Headers: []ReverseProxyHeaderInput{{Name: "X-Real-IP", Value: "$remote_addr"}},
	}
	if _, _, err := ExecuteReverseProxyCreate(context.Background(), lpTarget(), exec, in); err != nil {
		t.Fatalf("error = %v", err)
	}
	req := exec.requests[0]
	if req.API != ReverseProxyAPIName || req.Version != 1 || req.Method != "create" {
		t.Fatalf("request = %#v", req)
	}
	entry, ok := req.JSONParameters["entry"].(map[string]any)
	if !ok {
		t.Fatalf("entry not a single object: %#v", req.JSONParameters)
	}
	if entry["description"] != "media" {
		t.Fatalf("entry = %#v", entry)
	}
	fe := entry["frontend"].(map[string]any)
	if fe["protocol"] != 1 || fe["fqdn"] != "media.example.com" || fe["port"] != 443 {
		t.Fatalf("frontend = %#v", fe)
	}
	if https := fe["https"].(map[string]any); https["hsts"] != true {
		t.Fatalf("frontend https = %#v", https)
	}
	be := entry["backend"].(map[string]any)
	if be["protocol"] != 0 || be["fqdn"] != "127.0.0.1" || be["port"] != 8096 {
		t.Fatalf("backend = %#v", be)
	}
	headers := entry["customize_headers"].([]map[string]any)
	if len(headers) != 1 || headers[0]["name"] != "X-Real-IP" || headers[0]["value"] != "$remote_addr" {
		t.Fatalf("headers = %#v", headers)
	}
}

func TestExecuteReverseProxyDeleteRequest(t *testing.T) {
	exec := &recordingExecutor{}
	if _, _, err := ExecuteReverseProxyDelete(context.Background(), lpTarget(), exec, ReverseProxyDeleteInput{UUIDs: []string{"9c8fb9c0-8fb9-427d-bcbf-883af954b794"}}); err != nil {
		t.Fatalf("error = %v", err)
	}
	req := exec.requests[0]
	if req.API != ReverseProxyAPIName || req.Version != 1 || req.Method != "delete" {
		t.Fatalf("request = %#v", req)
	}
	uuids, ok := req.JSONParameters["uuids"].([]string)
	if !ok || len(uuids) != 1 || uuids[0] != "9c8fb9c0-8fb9-427d-bcbf-883af954b794" {
		t.Fatalf("uuids = %#v", req.JSONParameters["uuids"])
	}
}

func TestExecuteReverseProxyDeleteRejectsEmpty(t *testing.T) {
	exec := &recordingExecutor{}
	if _, _, err := ExecuteReverseProxyDelete(context.Background(), lpTarget(), exec, ReverseProxyDeleteInput{}); err == nil {
		t.Fatal("expected error for empty uuids")
	}
}
