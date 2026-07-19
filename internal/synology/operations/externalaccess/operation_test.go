package externalaccess

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/externalaccess"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type executorFunc func(context.Context, compatibility.Request) (json.RawMessage, error)

func (function executorFunc) Execute(ctx context.Context, request compatibility.Request) (json.RawMessage, error) {
	return function(ctx, request)
}

// routeExecutor answers requests keyed by "API vN method"; an unrouted request
// fails the test so a read never silently returns an empty payload.
func routeExecutor(t *testing.T, routes map[string]string) executorFunc {
	t.Helper()
	return func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		key := fmt.Sprintf("%s v%d %s", request.API, request.Version, request.Method)
		body, ok := routes[key]
		if !ok {
			t.Fatalf("unexpected request %q", key)
		}
		return json.RawMessage(body), nil
	}
}

func targetWith(apis map[string][2]int) compatibility.Target {
	target := compatibility.NewTarget()
	for name, span := range apis {
		target.SetAPI(name, compatibility.APIInfo{Path: "entry.cgi", MinVersion: span[0], MaxVersion: span[1]})
	}
	return target
}

const (
	accountQueryV2Body     = `{"account":"a@b.com","activated":true,"auth_key":"SECRET","is_logged_in":true}`
	accountPackageV1Body   = `{"auth_key":"SECRET","myds_id":"511437","serial":"1790PXN037200","ds_major":"7"}`
	quickConnectGetBody    = `{"ddns_domain":"direct.quickconnect.to","domain":"quickconnect.to","enabled":true,"myds_account":"a@b.com","region":"tw","server_alias":"myalias","server_id":"087738683"}`
	quickConnectRelayBody  = `{"relay_enabled":true}`
	quickConnectStatusBody = `{"alias_status":"success","status":"connected"}`
	quickConnectPerm       = `{"services":[{"enabled":true,"id":"dsm_portal"},{"enabled":false,"id":"file_sharing"}]}`
	ddnsExtIPBody          = `[{"ip":"203.0.113.7","ipv6":"0:0:0:0:0:0:0:0","type":"WAN"}]`
	ddnsRecordsEmpty       = `{"next_update_time":"","records":[]}`
	ddnsRecordsPopulated   = `{"next_update_time":"2026-07-19 10:00","records":[{"hostname":"my.synology.me","provider":"Synology","status":"normal","ip":"203.0.113.7","ipv6":"::1"}]}`
)

func TestReadAccountComposesCoreAndPackage(t *testing.T) {
	target := targetWith(map[string][2]int{MyDSCenterAPI: {1, 2}, PackageMyDSAPI: {1, 1}})
	state, selection, err := ReadAccount(context.Background(), target, routeExecutor(t, map[string]string{
		"SYNO.Core.MyDSCenter v2 query": accountQueryV2Body,
		"SYNO.Core.Package.MyDS v1 get": accountPackageV1Body,
	}))
	if err != nil {
		t.Fatalf("ReadAccount() error = %v", err)
	}
	if !selection.Supported || selection.Version != 2 {
		t.Fatalf("selection = %#v", selection)
	}
	want := externalaccess.AccountState{LoggedIn: true, Activated: true, Account: "a@b.com", MyDSID: "511437", Serial: "1790PXN037200"}
	if !reflect.DeepEqual(state, want) {
		t.Fatalf("state = %#v, want %#v", state, want)
	}
}

func TestReadAccountSkipsPackageWhenAbsent(t *testing.T) {
	target := targetWith(map[string][2]int{MyDSCenterAPI: {1, 2}})
	state, _, err := ReadAccount(context.Background(), target, routeExecutor(t, map[string]string{
		"SYNO.Core.MyDSCenter v2 query": accountQueryV2Body,
	}))
	if err != nil {
		t.Fatalf("ReadAccount() error = %v", err)
	}
	if state.MyDSID != "" || state.Serial != "" || !state.LoggedIn {
		t.Fatalf("state = %#v", state)
	}
}

func TestReadQuickConnectFullComposition(t *testing.T) {
	target := targetWith(map[string][2]int{QuickConnectAPI: {1, 3}, QuickConnectPermAPI: {1, 1}})
	state, selection, err := ReadQuickConnect(context.Background(), target, routeExecutor(t, map[string]string{
		"SYNO.Core.QuickConnect v2 get":             quickConnectGetBody,
		"SYNO.Core.QuickConnect v3 get_misc_config": quickConnectRelayBody,
		"SYNO.Core.QuickConnect v1 status":          quickConnectStatusBody,
		"SYNO.Core.QuickConnect.Permission v1 get":  quickConnectPerm,
	}))
	if err != nil {
		t.Fatalf("ReadQuickConnect() error = %v", err)
	}
	if !selection.Supported || selection.Version != 2 {
		t.Fatalf("selection = %#v", selection)
	}
	if !state.Enabled || state.ID != "myalias" || state.Region != "tw" || state.Domain != "quickconnect.to" || state.DirectDomain != "direct.quickconnect.to" {
		t.Fatalf("config = %#v", state)
	}
	if state.RelayEnabled == nil || !*state.RelayEnabled {
		t.Fatalf("relay = %#v", state.RelayEnabled)
	}
	if state.ConnectionStatus != "connected" || state.AliasStatus != "success" {
		t.Fatalf("status = %#v", state)
	}
	wantServices := []externalaccess.QuickConnectService{{ID: "dsm_portal", Enabled: true}, {ID: "file_sharing", Enabled: false}}
	if !reflect.DeepEqual(state.Services, wantServices) {
		t.Fatalf("services = %#v", state.Services)
	}
}

func TestReadQuickConnectV1SkipsRelay(t *testing.T) {
	target := targetWith(map[string][2]int{QuickConnectAPI: {1, 1}})
	state, selection, err := ReadQuickConnect(context.Background(), target, routeExecutor(t, map[string]string{
		"SYNO.Core.QuickConnect v1 get":    quickConnectGetBody,
		"SYNO.Core.QuickConnect v1 status": quickConnectStatusBody,
	}))
	if err != nil {
		t.Fatalf("ReadQuickConnect() error = %v", err)
	}
	if selection.Version != 1 {
		t.Fatalf("selection = %#v", selection)
	}
	if state.RelayEnabled != nil {
		t.Fatalf("relay should be nil when v3 is absent, got %#v", state.RelayEnabled)
	}
	if state.Services != nil {
		t.Fatalf("services should be nil when the permission API is absent, got %#v", state.Services)
	}
}

func TestReadDDNSComposesRecordsAndExternalIP(t *testing.T) {
	target := targetWith(map[string][2]int{DDNSRecordAPI: {1, 1}, DDNSExtIPAPI: {1, 2}})
	state, _, err := ReadDDNS(context.Background(), target, routeExecutor(t, map[string]string{
		"SYNO.Core.DDNS.Record v1 list": ddnsRecordsPopulated,
		"SYNO.Core.DDNS.ExtIP v2 list":  ddnsExtIPBody,
	}))
	if err != nil {
		t.Fatalf("ReadDDNS() error = %v", err)
	}
	if len(state.Records) != 1 || state.Records[0].Hostname != "my.synology.me" || state.Records[0].Provider != "Synology" || state.Records[0].IPv4 != "203.0.113.7" || state.Records[0].IPv6 != "::1" {
		t.Fatalf("records = %#v", state.Records)
	}
	if state.NextUpdateTime != "2026-07-19 10:00" {
		t.Fatalf("next update = %q", state.NextUpdateTime)
	}
	if len(state.ExternalAddress) != 1 || state.ExternalAddress[0].IP != "203.0.113.7" || state.ExternalAddress[0].IPv6 != "" || state.ExternalAddress[0].Type != "WAN" {
		t.Fatalf("external = %#v", state.ExternalAddress)
	}
}

func TestReadDDNSEmptyRecords(t *testing.T) {
	target := targetWith(map[string][2]int{DDNSRecordAPI: {1, 1}, DDNSExtIPAPI: {1, 2}})
	state, _, err := ReadDDNS(context.Background(), target, routeExecutor(t, map[string]string{
		"SYNO.Core.DDNS.Record v1 list": ddnsRecordsEmpty,
		"SYNO.Core.DDNS.ExtIP v2 list":  ddnsExtIPBody,
	}))
	if err != nil {
		t.Fatalf("ReadDDNS() error = %v", err)
	}
	if len(state.Records) != 0 {
		t.Fatalf("records = %#v", state.Records)
	}
}

func TestQuickConnectRelaySetCapturesExactRequest(t *testing.T) {
	target := targetWith(map[string][2]int{QuickConnectAPI: {1, 3}})
	var captured compatibility.Request
	result, selection, err := ExecuteQuickConnectRelaySet(context.Background(), target, executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		captured = request
		return json.RawMessage(`{}`), nil
	}), false)
	if err != nil {
		t.Fatalf("ExecuteQuickConnectRelaySet() error = %v", err)
	}
	if selection.Version != 3 || result.API != QuickConnectAPI || result.Version != 3 || result.Method != "set_misc_config" {
		t.Fatalf("selection = %#v result = %#v", selection, result)
	}
	if captured.API != QuickConnectAPI || captured.Version != 3 || captured.Method != "set_misc_config" {
		t.Fatalf("captured = %#v", captured)
	}
	want := map[string]any{"relay_enabled": false}
	if len(captured.Parameters) != 0 || !reflect.DeepEqual(captured.JSONParameters, want) {
		t.Fatalf("captured parameters = %#v, want JSON %#v", captured, want)
	}
}

func TestQuickConnectRelaySetUnsupportedBelowV3(t *testing.T) {
	for _, maximum := range []int{1, 2} {
		target := targetWith(map[string][2]int{QuickConnectAPI: {1, maximum}})
		if selection, err := SelectQuickConnectRelaySet(target); err == nil || selection.Supported || !compatibility.IsUnsupported(err) {
			t.Fatalf("SelectQuickConnectRelaySet(max v%d) = %#v, %v", maximum, selection, err)
		}
		called := false
		_, selection, err := ExecuteQuickConnectRelaySet(context.Background(), target, executorFunc(func(context.Context, compatibility.Request) (json.RawMessage, error) {
			called = true
			return nil, nil
		}), true)
		if !compatibility.IsUnsupported(err) || selection.Supported || called {
			t.Fatalf("ExecuteQuickConnectRelaySet(max v%d) err = %v selection = %#v called = %v", maximum, err, selection, called)
		}
	}
}

func TestSelectorsAreUnsupportedWithoutAPIs(t *testing.T) {
	empty := compatibility.NewTarget()
	for name, selectArea := range map[string]func(compatibility.Target) (compatibility.Selection, error){
		"account":      SelectAccount,
		"quickconnect": SelectQuickConnect,
		"ddns":         SelectDDNS,
	} {
		selection, err := selectArea(empty)
		if !compatibility.IsUnsupported(err) || selection.Supported {
			t.Fatalf("%s selection = %#v, err = %v", name, selection, err)
		}
	}
}

func TestAPINamesCoverAllAreas(t *testing.T) {
	want := []string{
		DDNSExtIPAPI, DDNSRecordAPI, MyDSCenterAPI, PackageMyDSAPI,
		PortForwardConfAPI, PortForwardRulesAPI, QuickConnectAPI, QuickConnectPermAPI,
	}
	got := APINames()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("APINames() = %#v, want %#v", got, want)
	}
}

func TestReadPortForwardComposesRulesAndRouter(t *testing.T) {
	target := targetWith(map[string][2]int{PortForwardRulesAPI: {1, 1}, PortForwardConfAPI: {1, 1}})
	state, selection, err := ReadPortForward(context.Background(), target, routeExecutor(t, map[string]string{
		"SYNO.Core.PortForwarding.Rules v1 load":     `[{"description":"HTTPS","protocol":"TCP","public_port":"443","private_port":"5001"}]`,
		"SYNO.Core.PortForwarding.RouterConf v1 get": `{"router_brand":"ASUS","router_model":"RT-AC66U","router_version":"3.0","support_upnp":"yes","support_natpmp":true,"support_change_port":true}`,
	}))
	if err != nil {
		t.Fatalf("ReadPortForward() error = %v", err)
	}
	if !selection.Supported {
		t.Fatalf("selection = %#v", selection)
	}
	if len(state.Rules) != 1 || state.Rules[0].Description != "HTTPS" || state.Rules[0].Protocol != "TCP" || state.Rules[0].PublicPort != "443" || state.Rules[0].PrivatePort != "5001" {
		t.Fatalf("rules = %#v", state.Rules)
	}
	// support_natpmp arrives as a bool here and is normalized to "yes".
	if state.Router.Brand != "ASUS" || state.Router.SupportUPnP != "yes" || state.Router.SupportNATPMP != "yes" || !state.Router.SupportChangePort {
		t.Fatalf("router = %#v", state.Router)
	}
}

func TestReadPortForwardEmpty(t *testing.T) {
	target := targetWith(map[string][2]int{PortForwardRulesAPI: {1, 1}, PortForwardConfAPI: {1, 1}})
	state, _, err := ReadPortForward(context.Background(), target, routeExecutor(t, map[string]string{
		"SYNO.Core.PortForwarding.Rules v1 load":     `[]`,
		"SYNO.Core.PortForwarding.RouterConf v1 get": `{"router_brand":"","support_change_port":false,"support_upnp":""}`,
	}))
	if err != nil {
		t.Fatalf("ReadPortForward() error = %v", err)
	}
	if len(state.Rules) != 0 || state.Router.Brand != "" || state.Router.SupportChangePort {
		t.Fatalf("state = %#v", state)
	}
}

func TestDecodersRejectMalformedShapes(t *testing.T) {
	tests := []struct {
		name string
		fn   func(json.RawMessage) error
		data string
		want string
	}{
		{name: "account not object", fn: func(d json.RawMessage) error { _, e := decodeAccountCore(d); return e }, data: `[]`, want: "expected an object"},
		{name: "account missing is_logged_in", fn: func(d json.RawMessage) error { _, e := decodeAccountCore(d); return e }, data: `{"account":"a"}`, want: "is_logged_in"},
		{name: "package missing myds_id", fn: func(d json.RawMessage) error { _, e := decodeAccountPackage(d); return e }, data: `{"serial":"x"}`, want: "myds_id"},
		{name: "quickconnect missing enabled", fn: func(d json.RawMessage) error { _, e := decodeQuickConnectConfig(d); return e }, data: `{"server_alias":"x"}`, want: "enabled"},
		{name: "relay missing", fn: func(d json.RawMessage) error { _, e := decodeQuickConnectRelay(d); return e }, data: `{}`, want: "relay_enabled"},
		{name: "status missing", fn: func(d json.RawMessage) error { _, e := decodeQuickConnectStatus(d); return e }, data: `{"alias_status":"x"}`, want: "status"},
		{name: "permission missing services", fn: func(d json.RawMessage) error { _, e := decodeQuickConnectPermission(d); return e }, data: `{}`, want: "services"},
		{name: "extip not array", fn: func(d json.RawMessage) error { _, e := decodeDDNSExternalAddresses(d); return e }, data: `{}`, want: "expected an array"},
		{name: "routerconf not object", fn: func(d json.RawMessage) error { _, e := decodeRouterConf(d); return e }, data: `[]`, want: "expected an object"},
		{name: "routerconf missing anchor", fn: func(d json.RawMessage) error { _, e := decodeRouterConf(d); return e }, data: `{"support_upnp":""}`, want: "router_brand"},
		{name: "rules not array", fn: func(d json.RawMessage) error { _, e := decodePortForwardRules(d); return e }, data: `{}`, want: "expected an array"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.fn(json.RawMessage(test.data)); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestAccountDecodeIgnoresAuthKey(t *testing.T) {
	core, err := decodeAccountCore(json.RawMessage(accountQueryV2Body))
	if err != nil {
		t.Fatalf("decodeAccountCore() error = %v", err)
	}
	// The struct has no field for auth_key, so it can never be surfaced.
	if got := fmt.Sprintf("%#v", core); strings.Contains(got, "SECRET") {
		t.Fatalf("decoded account leaked the account token: %s", got)
	}
}
