package network

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type recordingExecutor struct {
	responses map[string]json.RawMessage
	requests  []compatibility.Request
}

func (e *recordingExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	e.requests = append(e.requests, request)
	if resp, ok := e.responses[request.API+"."+request.Method]; ok {
		return resp, nil
	}
	return json.RawMessage(`{}`), nil
}

func (e *recordingExecutor) ExecuteScript(context.Context, compatibility.Request) ([]byte, error) {
	return nil, nil
}

// netTarget advertises every network API at the given max version.
func netTarget(maxVersion int) compatibility.Target {
	target := compatibility.NewTarget()
	for _, name := range APINames() {
		target.SetAPI(name, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: maxVersion})
	}
	return target
}

func TestSelectorsRequireTheirAPI(t *testing.T) {
	full := netTarget(2)
	empty := compatibility.NewTarget()
	cases := []struct {
		name    string
		backend string
		selectF func(compatibility.Target) (compatibility.Selection, error)
	}{
		{"general", "network-general-get-v2", SelectGeneral},
		{"interfaces", "network-ethernet-list-v2", SelectInterfaces},
		{"bonds", "network-bond-list-v2", SelectBonds},
		{"proxy", "network-proxy-get-v1", SelectProxy},
		{"routes", "network-static-route-get-v1", SelectRoutes},
		{"traffic", "network-traffic-control-detect-v1", SelectTrafficControl},
	}
	for _, tc := range cases {
		selection, err := tc.selectF(full)
		if err != nil || !selection.Supported || selection.Backend != tc.backend {
			t.Fatalf("%s: selection=%#v err=%v", tc.name, selection, err)
		}
		selection, err = tc.selectF(empty)
		if !compatibility.IsUnsupported(err) || selection.Supported {
			t.Fatalf("%s: expected unsupported, got selection=%#v err=%v", tc.name, selection, err)
		}
	}
}

// TestIndependentBoundaries proves one area being absent never disables another:
// a target with only the Ethernet API reports interfaces supported and general,
// bonds, routes, traffic unsupported.
func TestIndependentBoundaries(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(EthernetAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	if sel, err := SelectInterfaces(target); err != nil || !sel.Supported {
		t.Fatalf("interfaces should be supported: sel=%#v err=%v", sel, err)
	}
	for _, sel := range []func(compatibility.Target) (compatibility.Selection, error){SelectGeneral, SelectBonds, SelectRoutes, SelectTrafficControl} {
		if selection, err := sel(target); !compatibility.IsUnsupported(err) || selection.Supported {
			t.Fatalf("expected unsupported without its API: selection=%#v err=%v", selection, err)
		}
	}
}

// TestInterfacesPrefersEthernet proves the Ethernet backend is preferred over the
// sparser Interface backend, and that v2 outranks v1.
func TestInterfacesPrefersEthernet(t *testing.T) {
	// Only the sparse Interface API present: the fallback variant is selected.
	target := compatibility.NewTarget()
	target.SetAPI(InterfaceAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	sel, err := SelectInterfaces(target)
	if err != nil || sel.Backend != "network-interface-list-v1" {
		t.Fatalf("fallback selection = %#v err=%v", sel, err)
	}
	// v1-only Ethernet: the v1 Ethernet variant wins over the Interface fallback.
	target.SetAPI(EthernetAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	sel, _ = SelectInterfaces(target)
	if sel.Backend != "network-ethernet-list-v1" {
		t.Fatalf("v1 ethernet selection = %#v", sel)
	}
}

func TestExecuteGeneralDecodesLiveShape(t *testing.T) {
	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		NetworkAPIName + ".get": json.RawMessage(generalLive),
	}}
	general, selection, err := ExecuteGeneral(context.Background(), netTarget(2), exec)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if selection.Backend != "network-general-get-v2" || selection.Version != 2 {
		t.Fatalf("selection = %#v", selection)
	}
	if general.Hostname != "test-nas" || general.DefaultGateway.Interface != "eth0" {
		t.Fatalf("general = %#v", general)
	}
	req := exec.requests[0]
	if req.API != NetworkAPIName || req.Version != 2 || req.Method != "get" || !req.ReadOnly {
		t.Fatalf("request = %#v", req)
	}
}

func TestExecuteInterfacesDecodesLiveShape(t *testing.T) {
	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		EthernetAPIName + ".list": json.RawMessage(interfacesLive),
	}}
	ifaces, selection, err := ExecuteInterfaces(context.Background(), netTarget(2), exec)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if selection.Backend != "network-ethernet-list-v2" {
		t.Fatalf("selection = %#v", selection)
	}
	if len(ifaces) != 2 || ifaces[0].Name != "eth0" || ifaces[1].MTU != 9000 {
		t.Fatalf("ifaces = %#v", ifaces)
	}
	if exec.requests[0].Method != "list" || exec.requests[0].Version != 2 {
		t.Fatalf("request = %#v", exec.requests[0])
	}
}

func TestExecuteBondsDecodesEmptyLiveShape(t *testing.T) {
	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		BondAPIName + ".list": json.RawMessage(`[]`),
	}}
	bonds, _, err := ExecuteBonds(context.Background(), netTarget(2), exec)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(bonds) != 0 {
		t.Fatalf("bonds = %#v", bonds)
	}
}
