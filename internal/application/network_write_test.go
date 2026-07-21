package application

import (
	"context"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/network"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

// fakeNetworkClient implements networkClient for the plan/apply and guard tests.
type fakeNetworkClient struct {
	general    synology.NetworkGeneral
	interfaces []synology.NetworkInterface
	caps       synology.NetworkCapabilities
	host       string
	port       int
	sources    []string
	persist    bool
	generalOps int
}

func (c *fakeNetworkClient) NetworkGeneral(context.Context) (synology.NetworkGeneral, error) {
	return c.general, nil
}
func (c *fakeNetworkClient) NetworkGeneralFresh(context.Context) (synology.NetworkGeneral, error) {
	return c.general, nil
}
func (c *fakeNetworkClient) NetworkInterfaces(context.Context) ([]synology.NetworkInterface, error) {
	return c.interfaces, nil
}
func (c *fakeNetworkClient) NetworkBonds(context.Context) ([]synology.NetworkBond, error) {
	return nil, nil
}
func (c *fakeNetworkClient) NetworkRoutes(context.Context) (synology.NetworkRouteTable, error) {
	return synology.NetworkRouteTable{}, nil
}
func (c *fakeNetworkClient) NetworkCapabilities(context.Context) (synology.NetworkCapabilities, synology.CompatibilityReport, error) {
	return c.caps, synology.CompatibilityReport{}, nil
}
func (c *fakeNetworkClient) NetworkTransportInfo() synology.NetworkTransport {
	return synology.NetworkTransport{Host: c.host, Port: c.port}
}
func (c *fakeNetworkClient) NetworkCurrentSources(context.Context) []string { return c.sources }
func (c *fakeNetworkClient) ApplyNetworkGeneralChange(_ context.Context, change synology.NetworkGeneralChange) (synology.NetworkMutationResult, error) {
	c.generalOps++
	if c.persist {
		c.general = network.MergeGeneral(c.general, change)
	}
	return synology.NetworkMutationResult{Backend: "network-general-set-v2", API: "SYNO.Core.Network", Version: 2, Method: "set"}, nil
}
func (c *fakeNetworkClient) ApplyNetworkInterfaceChange(_ context.Context, change synology.NetworkInterfaceChange) (synology.NetworkMutationResult, error) {
	return synology.NetworkMutationResult{}, synology.ErrNetworkInterfaceWriteUnverified
}

func networkTestClient() *fakeNetworkClient {
	return &fakeNetworkClient{
		general: synology.NetworkGeneral{
			Hostname: "test-nas", DefaultGatewayV4: "198.51.100.254", DNSPrimary: "203.0.113.253",
			DNSSecondary: "203.0.113.253", ARPIgnore: true, IPConflictDetect: true, UseDHCPDomain: true,
		},
		interfaces: []synology.NetworkInterface{
			{Name: "eth0", IPv4: "192.0.2.235", Netmask: "255.255.248.0", GatewayV4: "198.51.100.254", UseDHCP: true, MTU: 1500, LinkStatus: "connected"},
			{Name: "eth1", IPv4: "192.0.2.35", Netmask: "255.255.248.0", GatewayV4: "198.51.100.254", UseDHCP: true, MTU: 1500, LinkStatus: "connected"},
			{Name: "eth2", IPv4: "169.254.148.8", Netmask: "255.255.0.0", UseDHCP: true, MTU: 1500, LinkStatus: "disconnected"},
		},
		caps:    synology.NetworkCapabilities{Module: "network", GeneralRead: true, InterfacesRead: true, GeneralWrite: true, InterfaceWriteWireUnverified: true, Mutations: true},
		host:    "192.0.2.235",
		port:    5001,
		sources: []string{"192.0.2.69"},
		persist: true,
	}
}

func strp(s string) *string { return &s }
func ip(i int) *int         { return &i }
func bp(b bool) *bool       { return &b }

// ---- never-sever guard: required refuse/allow proofs (plan-only) ------------

func TestPlanRefusesManagementNICChange(t *testing.T) {
	client := networkTestClient()
	_, err := planNetworkInterfaceWithClient(context.Background(), "lab", client, network.InterfaceChange{Name: "eth0", IPv4: strp("192.0.2.240")})
	if err == nil || !strings.Contains(err.Error(), "never-sever guard refuses") {
		t.Fatalf("expected management-NIC refusal, got %v", err)
	}
}

func TestPlanRefusesDefaultGatewayChange(t *testing.T) {
	client := networkTestClient()
	_, err := planNetworkGeneralWithClient(context.Background(), "lab", client, network.GeneralChange{DefaultGatewayV4: strp("198.51.100.1")})
	if err == nil || !strings.Contains(err.Error(), "never-sever guard refuses") {
		t.Fatalf("expected default-gateway refusal, got %v", err)
	}
}

func TestPlanAllowsNonManagementNICChange(t *testing.T) {
	client := networkTestClient()
	plan, err := planNetworkInterfaceWithClient(context.Background(), "lab", client, network.InterfaceChange{Name: "eth1", MTU: ip(1400)})
	if err != nil {
		t.Fatalf("expected permit, got %v", err)
	}
	if plan.Guard.Protected || !plan.Guard.Allowed {
		t.Fatalf("guard = %#v", plan.Guard)
	}
	if plan.Risk != "medium" {
		t.Fatalf("non-management interface change should be medium, got %q", plan.Risk)
	}
	if !plan.WireUnverified {
		t.Fatalf("interface plan must flag the wire as unverified")
	}
}

func TestPlanManagementNICChangeOverridden(t *testing.T) {
	client := networkTestClient()
	plan, err := planNetworkInterfaceWithClient(context.Background(), "lab", client, network.InterfaceChange{Name: "eth0", IPv4: strp("192.0.2.240"), AllowConnectivityBreak: true})
	if err != nil {
		t.Fatalf("override should allow the plan, got %v", err)
	}
	if !plan.Guard.Overridden || plan.Risk != "high" {
		t.Fatalf("guard=%#v risk=%q", plan.Guard, plan.Risk)
	}
}

func TestInterfaceApplyRefusedWireUnverified(t *testing.T) {
	client := networkTestClient()
	// The plan must validate cleanly (guard permits a non-management change)...
	plan, err := planNetworkInterfaceWithClient(context.Background(), "lab", client, network.InterfaceChange{Name: "eth1", MTU: ip(1400)})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	plan.Hash, _ = networkInterfacePlanHash(plan)
	if err := validateNetworkInterfacePlan(plan, plan.Hash); err != nil {
		t.Fatalf("plan should validate: %v", err)
	}
	// ...but the live apply is refused because the interface-set wire is unverified.
	_, err = client.ApplyNetworkInterfaceChange(context.Background(), plan.Request)
	if err == nil || !strings.Contains(err.Error(), "wire-unverified") {
		t.Fatalf("interface apply must be refused as wire-unverified, got %v", err)
	}
}

// ---- ambiguous connection fails closed --------------------------------------

func TestPlanAmbiguousConnectionRefusesEveryInterface(t *testing.T) {
	client := networkTestClient()
	client.host = "nas.ddns.example" // hostname => ambiguous
	_, err := planNetworkInterfaceWithClient(context.Background(), "lab", client, network.InterfaceChange{Name: "eth1", MTU: ip(1400)})
	if err == nil || !strings.Contains(err.Error(), "never-sever guard refuses") {
		t.Fatalf("ambiguous connection must refuse even a non-management NIC, got %v", err)
	}
}

// ---- general write happy path: plan/apply/postcondition/staleness -----------

func TestGeneralPlanApplyHostname(t *testing.T) {
	client := networkTestClient()
	plan, err := planNetworkGeneralWithClient(context.Background(), "lab", client, network.GeneralChange{Hostname: strp("Renamed_NAS")})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Risk != "medium" {
		t.Fatalf("hostname change should be medium, got %q", plan.Risk)
	}
	plan.Hash, _ = networkGeneralPlanHash(plan)
	res, err := applyNetworkGeneralWithClient(context.Background(), client, plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Applied || client.generalOps != 1 {
		t.Fatalf("apply result=%#v ops=%d", res, client.generalOps)
	}
	if client.general.Hostname != "Renamed_NAS" {
		t.Fatalf("hostname not applied: %q", client.general.Hostname)
	}
}

func TestGeneralPlanStaleRejected(t *testing.T) {
	client := networkTestClient()
	plan, err := planNetworkGeneralWithClient(context.Background(), "lab", client, network.GeneralChange{Hostname: strp("Renamed_NAS")})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	plan.Hash, _ = networkGeneralPlanHash(plan)
	// mutate the observed state out from under the plan
	client.general.DNSPrimary = "9.9.9.9"
	_, err = applyNetworkGeneralWithClient(context.Background(), client, plan)
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected stale rejection, got %v", err)
	}
}

func TestGeneralPlanNoopRejected(t *testing.T) {
	client := networkTestClient()
	_, err := planNetworkGeneralWithClient(context.Background(), "lab", client, network.GeneralChange{Hostname: strp("test-nas")})
	if err == nil || !strings.Contains(err.Error(), "would not change") {
		t.Fatalf("expected no-op rejection, got %v", err)
	}
}
