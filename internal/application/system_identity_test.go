package application

import (
	"context"
	"testing"
)

func TestValidateHostname(t *testing.T) {
	valid := []string{"nas51", "DiskStation", "n", "a-b-c", "nas-01", "A0", repeatByte('x', 63)}
	for _, name := range valid {
		if err := validateHostname(name); err != nil {
			t.Errorf("validateHostname(%q) = %v, want nil", name, err)
		}
	}
	invalid := []string{"", "-nas", "nas-", "na s", "na_s", "na.s", "hej!", repeatByte('x', 64)}
	for _, name := range invalid {
		if err := validateHostname(name); err == nil {
			t.Errorf("validateHostname(%q) = nil, want error", name)
		}
	}
}

type fakeHostnameClient struct {
	current string
	set     string
}

func (f *fakeHostnameClient) GetServerName(context.Context) (string, error) { return f.current, nil }

func (f *fakeHostnameClient) SetServerName(_ context.Context, name string) (string, error) {
	f.set = name
	return name, nil
}

func TestPlanSystemHostnameHashBinding(t *testing.T) {
	client := &fakeHostnameClient{current: "DiskStation"}
	plan, err := planSystemHostnameWithClient(context.Background(), "lab", client, SystemHostnameChange{Hostname: "nas51"})
	if err != nil {
		t.Fatalf("planSystemHostnameWithClient() error = %v", err)
	}
	if plan.Hash == "" || plan.ObservedFingerprint == "" || plan.ObservedHostname != "DiskStation" {
		t.Fatalf("plan missing hashes/observed: %#v", plan)
	}
	if err := validateSystemHostnamePlan(plan, plan.Hash); err != nil {
		t.Fatalf("validateSystemHostnamePlan rejected a fresh plan: %v", err)
	}
	// Tampering the requested hostname must break the approval hash.
	tampered := plan
	tampered.Request.Hostname = "evil"
	if err := validateSystemHostnamePlan(tampered, plan.Hash); err == nil {
		t.Fatal("validateSystemHostnamePlan accepted a tampered hostname")
	}
	// A wrong approval hash is rejected.
	if err := validateSystemHostnamePlan(plan, "nope"); err == nil {
		t.Fatal("validateSystemHostnamePlan accepted a wrong approval hash")
	}
	// Planning a no-op (name already set) is refused.
	if _, err := planSystemHostnameWithClient(context.Background(), "lab", &fakeHostnameClient{current: "nas51"}, SystemHostnameChange{Hostname: "nas51"}); err == nil {
		t.Fatal("planSystemHostnameWithClient accepted a no-op rename")
	}
}

func repeatByte(ch byte, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = ch
	}
	return string(out)
}
